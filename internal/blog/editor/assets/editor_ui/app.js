// selfdoc editor — the authoring shell.
//
// A tinymoon app: mountShell builds the frame (sidebar, topbar, theme, router),
// one route per registry entry, and each route's view is a post list beside a
// two-pane editor. The editing surface is tinymoon's createEditor — a real
// textarea over a decoration underlay.
//
// The preview pane is an iframe pointed at /preview/<repo>/blog/<slug>/, which
// the server answers with the exact bytes publishing this buffer would produce
// and, for everything the page links to, with the repository's build output. It
// is not a second renderer and not an approximation: it is the publish render
// of an unsaved buffer, which is the whole reason the in-memory render path
// exists.
//
// Three assistance lanes ride on the same debounced pause in typing:
//
//   * the preview, POSTed and broadcast back on the event stream;
//   * spelling marks, painted in the underlay through setDecorations();
//   * lint findings, shown BOTH as gutter markers and as a real list of
//     buttons. The gutter lane is aria-hidden and nothing in it is focusable,
//     so a caller that surfaces diagnostics there owes them somewhere
//     keyboard-reachable too — that pairing is the component's a11y contract,
//     not an extra.
//
// Nothing here writes to a working tree except the explicit save (Ctrl-S or the
// Save button), which PUTs the buffer. Typing only ever POSTs a preview and an
// analysis, neither of which writes anything.
//
// Publishing is the one action that reaches the world. It is never taken from
// a keystroke: the button asks the server what the command declares itself to
// be, renders that declaration in a consent dialog, and only a confirmed
// dialog sends the consent. There is no don't-ask-again — every publish asks.

import { el } from "/tinymoon/js/dom.js";
import { mountShell } from "/tinymoon/js/shell.js";
import { createView } from "/tinymoon/js/view.js";
import { loadingBlock, emptyBlock, errorBlock } from "/tinymoon/js/states.js";
import { toast } from "/tinymoon/js/toast.js";
import { openModal } from "/tinymoon/js/modal.js";
import { createSettings, cycleTheme } from "/tinymoon/js/settings.js";
import { createEditor } from "/tinymoon/js/editor.js";

// How long typing has to pause before a preview is asked for. Long enough that
// a fast typist does not render every keystroke, short enough to feel live.
const PREVIEW_DEBOUNCE_MS = 300;

// The analysis pause is longer than the preview's: spelling and the lint rules
// read the whole project, and a finding that arrives a moment later is still
// useful, where a preview that lags is immediately noticeable.
const ANALYSIS_DEBOUNCE_MS = 450;

// The completion trigger. A link target is what sits between "](" and the
// closing paren, so the token INCLUDES the "](" — which is what makes the
// popup open the moment a link is opened, before anything is typed into it.
// (A pattern that matched only the target would yield an empty token there,
// and the component treats an empty token as "nothing to complete".)
const LINK_TOKEN = /\]\([^)\s]*$/;

// Severity to the classes tinymoon's editor tier ships. A lint's severity is
// the registry's answer for its code; the editor only paints it.
const MARKER_CLASS = {
  error: "tm-editor-marker-error",
  warning: "tm-editor-marker-warn",
  info: "tm-editor-marker-info",
};
const MARKER_GLYPH = { error: "!", warning: "!", info: "i" };

const settings = createSettings({
  storageKey: "tm-settings",
  defaults: { theme: "system" },
});

// ---------------------------------------------------------------- transport

async function call(path, opts) {
  const res = await fetch(path, opts);
  const text = await res.text();
  let body = null;
  if (text) {
    try { body = JSON.parse(text); } catch (e) { body = null; }
  }
  if (!res.ok) {
    throw new Error((body && body.error) || text || `HTTP ${res.status}`);
  }
  return body;
}

const getJSON = (path) => call(path, { method: "GET" });

const putText = (path, content) => call(path, {
  method: "PUT",
  headers: { "Content-Type": "text/markdown; charset=utf-8" },
  body: content,
});

const postText = (path, content) => call(path, {
  method: "POST",
  headers: { "Content-Type": "text/markdown; charset=utf-8" },
  body: content,
});

const postJSON = (path, payload) => call(path, {
  method: "POST",
  headers: { "Content-Type": "application/json; charset=utf-8" },
  body: JSON.stringify(payload),
});

// One event stream for the whole app. Views register interest by path; the
// stream itself is opened once and never per view.
const previewListeners = new Set();

function openStream() {
  const source = new EventSource("/events");
  source.addEventListener("preview", (event) => {
    let payload = null;
    try { payload = JSON.parse(event.data); } catch (e) { return; }
    for (const listener of [...previewListeners]) listener(payload);
  });
  source.addEventListener("error", () => {
    // EventSource reconnects on its own; nothing to do but stay quiet about
    // the gap. A hard failure surfaces the next time a request is made.
  });
  return source;
}

// -------------------------------------------------------------------- view

function repoView(repo) {
  let listHost = null;
  let editorHost = null;
  let frame = null;
  let statusLabel = null;
  let saveButton = null;
  let publishButton = null;
  let findingsHost = null;
  let findingsCount = null;
  let editor = null;
  let posts = [];
  let currentPath = null;
  let pendingPath = null;
  let previewTimer = null;
  let analysisTimer = null;
  let analysisSeq = 0;
  let savedText = "";

  function setStatus(text, kind) {
    if (!statusLabel) return;
    statusLabel.textContent = text;
    statusLabel.dataset.kind = kind || "idle";
  }

  function markDirty() {
    const dirty = editor && editor.get() !== savedText;
    if (saveButton) saveButton.disabled = !dirty;
    setStatus(dirty ? "unsaved changes" : "saved", dirty ? "dirty" : "idle");
  }

  function highlightSelection() {
    if (!listHost) return;
    for (const button of listHost.querySelectorAll("[data-post]")) {
      button.setAttribute(
        "aria-current", button.dataset.post === currentPath ? "true" : "false",
      );
    }
  }

  function renderList() {
    listHost.replaceChildren();
    if (!posts.length) {
      listHost.appendChild(emptyBlock({
        title: "No posts",
        note: `${repo.name} has no posts yet.`,
      }));
      return;
    }
    for (const post of posts) {
      const button = el("button", "ed-post");
      button.type = "button";
      button.dataset.post = post.path;
      button.appendChild(el("span", "ed-post-title", post.title));
      const meta = el("span", "ed-post-meta");
      meta.appendChild(el("span", "ed-post-date", post.date));
      if (post.draft) meta.appendChild(el("span", "ed-post-draft", "draft"));
      button.appendChild(meta);
      button.addEventListener("click", () => {
        location.hash = `#/${repo.name}/${post.path}`;
      });
      listHost.appendChild(button);
    }
    highlightSelection();
  }

  async function loadPosts() {
    listHost.replaceChildren(loadingBlock({ note: "Reading posts…" }));
    try {
      const body = await getJSON(`/api/repos/${repo.name}/posts`);
      posts = body.posts;
      renderList();
    } catch (err) {
      posts = [];
      listHost.replaceChildren(errorBlock({
        title: "Cannot list posts",
        note: err.message,
      }));
    }
  }

  function mountEditor(content) {
    if (editor) editor.destroy();
    editorHost.replaceChildren();
    editor = createEditor({
      name: `source-${repo.name}`,
      label: "Post source",
      value: content,
      rows: 24,
      spellcheck: false,
      gutter: true,
      tokenPattern: LINK_TOKEN,
      completionLabel: "Link targets",
      onChange: () => {
        markDirty();
        schedulePreview();
        scheduleAnalysis();
      },
      onCompletionContext: (ctx) => linkCompletions(ctx),
      onGutterClick: (marker) => {
        editor.setSelection(editor.lineOffset(marker.line));
      },
    });
    editor.setDecorations([]);
    editor.setGutterMarkers([]);
    editorHost.appendChild(editor.el);
    renderFindings([], []);
  }

  // -- assistance ----------------------------------------------------------

  // The token carries its "](" prefix (see LINK_TOKEN), so the query is what
  // follows it and the accepted text has to put the whole thing back. The
  // closing paren is written only when there is not one already at the caret:
  // a textarea pairs nothing on its own, so "[label](" is the usual state,
  // but a link whose parens were typed in full must not gain a second one.
  async function linkCompletions(ctx) {
    const query = ctx.token.slice(2);
    const closed = ctx.value[ctx.caret] === ")";
    const body = await getJSON(
      `/api/link-targets?q=${encodeURIComponent(query)}`,
    );
    return body.targets.map((target) => ({
      label: target.kind === "section"
        ? `${target.page_title} › ${target.title}`
        : target.title,
      hint: `${target.repo} · ${target.address}`,
      value: `](${target.href}${closed ? "" : ")"}`,
    }));
  }

  function scheduleAnalysis() {
    if (analysisTimer) clearTimeout(analysisTimer);
    analysisTimer = setTimeout(requestAnalysis, ANALYSIS_DEBOUNCE_MS);
  }

  async function requestAnalysis() {
    if (!editor || !currentPath) return;
    const path = currentPath;
    const mine = ++analysisSeq;
    let body = null;
    try {
      body = await postText(
        `/api/repos/${repo.name}/analysis?path=${encodeURIComponent(path)}`,
        editor.get(),
      );
    } catch (err) {
      // A stale answer must not overwrite a newer one, and neither must a
      // stale failure.
      if (mine !== analysisSeq || path !== currentPath) return;
      renderFindings([], [], err.message);
      return;
    }
    if (mine !== analysisSeq || path !== currentPath || !editor) return;

    editor.setDecorations(body.spelling.map((finding) => ({
      from: finding.from,
      to: finding.to,
      class: "tm-deco-spell",
      data: finding,
    })));
    editor.setGutterMarkers(
      body.lints
        .filter((lint) => lint.line != null)
        .map((lint) => ({
          line: lint.line,
          class: MARKER_CLASS[lint.severity] || "tm-editor-marker-info",
          label: MARKER_GLYPH[lint.severity] || "i",
          data: lint,
        })),
    );
    renderFindings(body.spelling, body.lints);
  }

  // The gutter is presentational: aria-hidden, nothing focusable. Every
  // finding therefore also gets a real button here, which moves the caret to
  // what it describes.
  function renderFindings(spelling, lints, error) {
    if (!findingsHost) return;
    findingsHost.replaceChildren();

    if (error) {
      findingsCount.textContent = "analysis unavailable";
      findingsHost.appendChild(el("span", "ed-finding-note", error));
      return;
    }

    const total = spelling.length + lints.length;
    findingsCount.textContent = total === 0
      ? "no findings"
      : `${total} finding${total === 1 ? "" : "s"}`;
    if (total === 0) {
      findingsHost.appendChild(el("span", "ed-finding-note", "Nothing to fix."));
      return;
    }

    for (const lint of lints) {
      const where = lint.line == null ? "page" : `line ${lint.line}`;
      const button = el("button", "btn ed-finding",
        `${lint.name} · ${where}: ${lint.message}`);
      button.type = "button";
      button.dataset.severity = lint.severity;
      button.addEventListener("click", () => {
        if (lint.line != null) editor.setSelection(editor.lineOffset(lint.line));
        else editor.focus();
      });
      findingsHost.appendChild(button);
    }
    for (const finding of spelling) {
      const button = el("button", "btn ed-finding",
        `spelling · line ${finding.line}: ${finding.message}`);
      button.type = "button";
      button.dataset.severity = "spelling";
      button.addEventListener("click", () => {
        editor.setSelection(finding.from, finding.to);
      });
      findingsHost.appendChild(button);
    }
  }

  function schedulePreview() {
    if (previewTimer) clearTimeout(previewTimer);
    previewTimer = setTimeout(requestPreview, PREVIEW_DEBOUNCE_MS);
  }

  async function requestPreview() {
    if (!editor || !currentPath) return;
    const path = currentPath;
    try {
      await postText(
        `/api/repos/${repo.name}/preview?path=${encodeURIComponent(path)}`,
        editor.get(),
      );
      // The rendered page arrives on the event stream, not in this response:
      // one channel drives the pane, so a preview triggered anywhere shows up
      // the same way.
    } catch (err) {
      setStatus(err.message, "error");
    }
  }

  async function openPost(path) {
    currentPath = path;
    highlightSelection();
    editorHost.replaceChildren(loadingBlock({ note: "Opening…" }));
    try {
      const body = await getJSON(
        `/api/repos/${repo.name}/document?path=${encodeURIComponent(path)}`,
      );
      savedText = body.content;
      mountEditor(body.content);
      markDirty();
      requestPreview();
      requestAnalysis();
    } catch (err) {
      editorHost.replaceChildren(errorBlock({
        title: "Cannot open post",
        note: err.message,
      }));
    }
  }

  async function save() {
    if (!editor || !currentPath) return;
    const content = editor.get();
    try {
      await putText(
        `/api/repos/${repo.name}/document?path=${encodeURIComponent(currentPath)}`,
        content,
      );
      savedText = content;
      markDirty();
      toast(`Saved ${currentPath}`, "ok");
      loadPosts();
    } catch (err) {
      setStatus(err.message, "error");
      toast(err.message, "error");
    }
  }

  // -- publish -------------------------------------------------------------

  // Everything the dialog states is read off the server's answer: the
  // command's declared classification, the grants it declares, the scope
  // sentence the command owns, and the list of posts computed from the
  // project's own discovery. Nothing about the operation is described here.
  function consentBody(descriptor, plan) {
    const wrap = el("div", "ed-consent");

    wrap.appendChild(el("p", "ed-consent-scope", descriptor.scope_note));

    const facts = el("dl", "ed-consent-facts");
    const fact = (term, value) => {
      facts.appendChild(el("dt", null, term));
      facts.appendChild(el("dd", "mono", value));
    };
    fact("Command", descriptor.command);
    fact("Effect", descriptor.effect);
    fact("Consequential", descriptor.consequential ? "yes" : "no");
    fact("Scope", descriptor.scope);
    fact("Repository", `${plan.repo} (${plan.path})`);
    if (plan.assembly) fact("Assembly", plan.assembly);
    wrap.appendChild(facts);

    wrap.appendChild(el("p", "ed-consent-note", descriptor.help));

    for (const grant of descriptor.grants) {
      wrap.appendChild(el("p", "ed-consent-grant",
        `${grant.name} (${grant.kind}): ${grant.reason}`));
    }

    wrap.appendChild(el("h4", null,
      `Will publish ${plan.publishing.length} post(s)`));
    if (plan.publishing.length === 0) {
      wrap.appendChild(el("p", "ed-consent-note",
        "This repository declares no non-draft posts."));
    } else {
      const list = el("ul", "ed-consent-list");
      for (const post of plan.publishing) {
        list.appendChild(el("li", null, `${post.date} — ${post.title} (${post.slug})`));
      }
      wrap.appendChild(list);
    }

    if (plan.withheld.length) {
      wrap.appendChild(el("h4", null,
        `Stays unpublished: ${plan.withheld.length} draft(s)`));
      const drafts = el("ul", "ed-consent-list");
      for (const post of plan.withheld) {
        drafts.appendChild(el("li", null, `${post.title} (${post.path})`));
      }
      wrap.appendChild(drafts);
    }

    return wrap;
  }

  function showResult(title, text) {
    const body = el("div", "ed-consent");
    body.appendChild(el("pre", "ed-result", text || "(no output)"));
    openModal({ title, body });
  }

  async function askToPublish() {
    let surface = null;
    try {
      surface = await getJSON(`/api/repos/${repo.name}/publish`);
    } catch (err) {
      toast(err.message, "error");
      return;
    }

    const confirm = el("button", "btn danger");
    confirm.type = "button";
    confirm.textContent = `Publish ${surface.plan.publishing.length} post(s)`;
    const cancel = el("button", "btn");
    cancel.type = "button";
    cancel.textContent = "Cancel";

    const close = openModal({
      title: `Publish ${repo.name}`,
      body: consentBody(surface.descriptor, surface.plan),
      actions: [cancel, confirm],
    });

    cancel.addEventListener("click", () => close());
    confirm.addEventListener("click", async () => {
      close();
      publishButton.disabled = true;
      setStatus("publishing…", "dirty");
      try {
        // The consent the human just gave, carried on the call. The server
        // hands it to strictcli untouched; a call without it is refused by
        // the framework, not by anything here.
        const result = await postJSON(`/api/repos/${repo.name}/publish`, {
          approve_consequential: true,
        });
        showResult(`Published ${repo.name}`, result.stdout + result.stderr);
        toast(`Published ${repo.name}`, "ok");
        setStatus("published", "idle");
      } catch (err) {
        showResult(`Publish refused — ${repo.name}`, err.message);
        setStatus(err.message, "error");
      } finally {
        publishButton.disabled = false;
        markDirty();
      }
    });
  }

  function onPreview(payload) {
    if (payload.repo !== repo.name || payload.path !== currentPath) return;
    // Re-point rather than reload: the URL carries the post's published
    // address, which changes when the buffer's slug does.
    frame.src = `${payload.url}?t=${Date.now()}`;
    setStatus(editor && editor.get() !== savedText
      ? "unsaved changes" : "saved",
      editor && editor.get() !== savedText ? "dirty" : "idle");
  }

  function buildUnserved(ctx) {
    ctx.root.appendChild(errorBlock({
      title: "Remote entries are not served yet",
      note: `${repo.name} points at ${repo.repo} @ ${repo.ref}. The registry `
        + `validates remote entries in full, but serving one is not `
        + `implemented.`,
    }));
  }

  function build(ctx) {
    if (!repo.served) {
      buildUnserved(ctx);
      return;
    }

    const wrap = el("div", "ed-wrap");

    const side = el("aside", "ed-side");
    side.appendChild(el("h2", "ed-side-title", "Posts"));
    listHost = el("div", "ed-list");
    side.appendChild(listHost);
    wrap.appendChild(side);

    const main = el("div", "ed-main");

    const bar = el("div", "ed-bar");
    statusLabel = el("span", "ed-status", "no post open");
    const actions = el("div", "ed-actions");
    saveButton = el("button", "btn ed-save");
    saveButton.type = "button";
    saveButton.textContent = "Save";
    saveButton.disabled = true;
    saveButton.addEventListener("click", save);
    publishButton = el("button", "btn ed-publish");
    publishButton.type = "button";
    // Named for what the command does: it publishes every non-draft post of
    // the repository, so the label names the repository and never the one
    // document that happens to be open.
    publishButton.textContent = "Publish repository…";
    publishButton.addEventListener("click", askToPublish);
    actions.appendChild(saveButton);
    actions.appendChild(publishButton);
    bar.appendChild(statusLabel);
    bar.appendChild(actions);
    main.appendChild(bar);

    const panes = el("div", "ed-panes");
    const left = el("div", "ed-left");
    editorHost = el("div", "ed-editor");
    editorHost.appendChild(emptyBlock({
      title: "No post open",
      note: "Pick a post from the list.",
    }));
    left.appendChild(editorHost);

    const findings = el("section", "ed-findings");
    const findingsHead = el("div", "ed-findings-head");
    findingsHead.appendChild(el("h3", "ed-findings-title", "Findings"));
    findingsCount = el("span", "ed-findings-count", "no post open");
    findingsHead.appendChild(findingsCount);
    findings.appendChild(findingsHead);
    findingsHost = el("div", "ed-finding-list");
    findings.appendChild(findingsHost);
    left.appendChild(findings);

    frame = el("iframe", "ed-preview");
    frame.setAttribute("title", "Preview");
    panes.appendChild(left);
    panes.appendChild(frame);
    main.appendChild(panes);

    wrap.appendChild(main);
    ctx.root.appendChild(wrap);

    previewListeners.add(onPreview);
    ctx.root.addEventListener("keydown", (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        save();
      }
    });

    loadPosts();
    if (pendingPath) {
      const path = pendingPath;
      pendingPath = null;
      openPost(path);
    }
  }

  return createView({
    build,
    refresh() {
      if (!repo.served) return;
      if (pendingPath && pendingPath !== currentPath) {
        const path = pendingPath;
        pendingPath = null;
        openPost(path);
      }
    },
    setSub(sub) {
      if (!sub) return;
      pendingPath = sub;
    },
  });
}

// -------------------------------------------------------------------- boot

async function boot() {
  let repos = [];
  try {
    repos = (await getJSON("/api/repos")).repos;
  } catch (err) {
    document.body.appendChild(errorBlock({
      title: "Cannot read the registry",
      note: err.message,
    }));
    return;
  }

  if (!repos.length) {
    document.body.appendChild(emptyBlock({
      title: "The registry is empty",
      note: "Add a [[repo]] block to the registry file and restart.",
    }));
    return;
  }

  const routes = {};
  for (const repo of repos) {
    routes[repo.name] = {
      title: repo.name,
      icon: repo.served ? "docs" : "warn",
      tip: repo.served ? repo.path : `${repo.repo} @ ${repo.ref} (not served)`,
      view: () => repoView(repo),
    };
  }

  mountShell({
    root: document.body,
    brand: {
      name: "selfdoc",
      logoHTML: '<div class="wordmark">self<b>doc</b></div>'
        + '<div class="tagline">editor</div>',
    },
    routes,
    defaultRoute: repos[0].name,
    topbarActions: [
      { icon: "moon", tip: "Cycle theme", onClick: () => cycleTheme(settings) },
    ],
  });

  openStream();
}

boot();
