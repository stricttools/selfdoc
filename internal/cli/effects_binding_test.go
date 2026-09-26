package cli

import (
	"sort"
	"strings"
	"testing"
)

// Command classification and the consent set, pinned.
//
// Three guarantees this file holds:
//
//  1. Every command carries the classification it was deliberately given.
//     strictcli makes the effect declaration mandatory, so a missing one is a
//     registration error and can never reach here -- but a WRONG one is
//     silent. The table below is the reviewed judgement, and changing a row
//     has to be a deliberate edit to this file. "read_only" means the command
//     performs no user-visible or consequential mutation: it may read the
//     filesystem and shell out to declared reads, and nothing else.
//
//  2. The consequential commands are a reviewed set. The framework prompts
//     for those and no others; mutating does not imply a prompt. The set below
//     is pinned in both directions, so adding one is a deliberate edit to this
//     file rather than a passing thought at a registration site.
//
//  3. No command redeclares a framework-reserved flag name.
//
// The Python's third guarantee -- that every handler carries the effects
// decorator -- has no counterpart here: the effects handle is an explicit
// parameter threaded from the context into every engine call, so a handler
// that did not take one would not compile.

// commandEffects is the reviewed classification. The comment on each mutating
// row names the mutation.
var commandEffects = map[string]string{
	// writes selfdoc.json and the starter page, then auto-commits them
	"init": "mutating",
	// prints the layout declaration, reading nothing but the binary itself
	"layout.dump": "read_only",
	// reads the repository's tool-state directory and reports on it
	"layout.validate": "read_only",
	// moves selfdoc's directories off the previous root, writes the derived
	// ignore file and an empty vocabulary, rewrites selfdoc.json and the
	// generated root files' headers, then auto-commits
	"layout.migrate": "mutating",
	// each edits the project's terms file or review list, then auto-commits
	"vocabulary.accept":  "mutating",
	"vocabulary.reject":  "mutating",
	"vocabulary.remove":  "mutating",
	"vocabulary.approve": "mutating",
	"vocabulary.drop":    "mutating",
	// writes the whole site output tree and the content-hash store, then
	// auto-commits the store
	"build": "mutating",
	// --drafts rebuilds the site before serving, which writes the output tree
	"serve": "mutating",
	// wrangler deploy, or a force-push of the remote gh-pages branch
	"deploy": "mutating",
	// rewrites the content-hash store (the staleness baseline) and
	// auto-commits it -- an app-level cache write is an ordinary mutation
	"check": "mutating",
	// advances the stored staleness/drift baselines, then auto-commits
	"baseline.accept": "mutating",
	// writes generated doc pages and the read-only root files, deletes stale
	// generated pages, updates hashes + manifest, then auto-commits
	"gen": "mutating",
	// runs the configured scripts under bwrap and writes their data outputs
	"gen-data": "mutating",
	// reads every sibling project's docs tree and prints the spelling findings
	"spell-corpus": "read_only",
	// reads the tree and prints metrics
	"quality": "read_only",
	// writes the scaffolded post file
	"blog.post.new": "mutating",
	// reads the posts directory and prints
	"blog.post.list": "read_only",
	// writes the generated post and updates the manifest
	"blog.post.generate": "mutating",
	// pushes built HTML to the assembly repo via the Git Data API and
	// dispatches a workflow that republishes the live site
	"blog.post.publish": "mutating",
	// builds this project's docs into its local output tree, then pushes that
	// tree, its manifest and its membership record into the assembly repo via
	// the Git Data API -- deleting, in the same commit, every page the project
	// published before and no longer builds -- and dispatches a shared-only
	// workflow that republishes the live site
	"blog.publish-docs": "mutating",
	// creates a GitHub repo, a Cloudflare Pages project, and repo secrets
	"assembly.init": "mutating",
	// repository_dispatch against the assembly repo
	"assembly.push": "mutating",
	// queries workflow runs and prints them
	"assembly.status": "read_only",
	// repository_dispatch for every registered project
	"assembly.rebuild": "mutating",
	// one commit on the assembly repo that drops the project's [[project]]
	// block from the roster, drops its entry from the membership record and
	// deletes every path it owns, then dispatches a shared-only rebuild
	"assembly.retire": "mutating",
	// prints the _redirects content to stdout
	"assembly.redirects": "read_only",
	// writes the assembled site's shared files
	"assembly.generate-shared": "mutating",
	// rewrites the assembly checkout's tree and pushes the deploy commit
	"assembly.integrate": "mutating",
	// builds every named local checkout and writes a whole preview site tree
	// into the output directory, then serves it on loopback. Nothing leaves
	// the machine, but the tree on disk is a real mutation of a real path
	"assembly.preview": "mutating",
	// reads a built assembly checkout and reports what is wrong with it. The
	// outbound-link results it produces are handed back to the caller rather
	// than written: the deploy persists them, this command does not
	"assembly.verify": "read_only",
	// commits the regenerated deploy workflow to the assembly repo
	"assembly.sync-workflow": "mutating",
	"assembly.republish-all": "mutating",
	// reads the editor registry and prints one line per entry
	"blog.editor.list-repos": "read_only",
	// serves the authoring app on loopback, whose PUT writes an edited post
	// into the registered repository's working tree. Previews write nothing --
	// they go through the in-memory render path -- so the save is the whole of
	// the mutation, and it is deliberately not consequential: a save is an
	// interactive act the author has just performed, not something to
	// interrupt them for at launch
	"blog.editor.serve": "mutating",
}

// consequentialCommands is the reviewed consent set. A command belongs here
// when its effects are worth interrupting someone for -- not merely because
// they mutate. The line: an effect qualifies when it is destructive on a
// remote, creates a named external resource that rerunning cannot un-create,
// or makes something public that was not public before. Re-deriving
// already-public content from an already-public source does not qualify, which
// is why `assembly push` and `assembly rebuild` are absent despite carrying
// escaping process-mutating grants.
var consequentialCommands = map[string]bool{
	// Cloudflare Pages goes live on landing; the GitHub Pages provider
	// force-pushes gh-pages, so the previous published tree is gone from the
	// remote. Neither is undone by rerunning.
	"deploy": true,
	// Locally-authored, previously-private posts become publicly readable.
	"blog.post.publish": true,
	// Same line as `blog post publish`, for documentation instead of posts: the
	// working tree becomes publicly readable with no tag and no release in
	// between, and it also deletes -- a page the project no longer builds
	// disappears for readers in the same commit.
	"blog.publish-docs": true,
	// `blog publish-docs` for every project on the roster in one pass: every
	// checkout's working tree becomes publicly readable, and each publish
	// deletes the pages its project no longer builds.
	"assembly.republish-all": true,
	// Creates a GitHub repository, claims a *.pages.dev subdomain, and writes
	// deployment credentials into repo secrets -- three named external
	// resources, none of them un-created by a rerun.
	"assembly.init": true,
	// Deletes a project's whole published section from the live site. Nothing
	// else in the tree removes public content, and rerunning cannot put it
	// back: the pages are gone from the branch the site serves.
	"assembly.retire": true,
}

// walk maps a dotted command path to the command's schema entry, over the
// whole registered tree.
func walk(t *testing.T) map[string]map[string]any {
	t.Helper()
	schema := New(Options{}).DumpSchemaDict()
	found := map[string]map[string]any{}

	var visit func(container map[string]any, prefix string)
	visit = func(container map[string]any, prefix string) {
		if commands, ok := container["commands"].(map[string]any); ok {
			for name, raw := range commands {
				if entry, ok := raw.(map[string]any); ok {
					found[prefix+name] = entry
				}
			}
		}
		if groups, ok := container["groups"].(map[string]any); ok {
			for name, raw := range groups {
				if group, ok := raw.(map[string]any); ok {
					visit(group, prefix+name+".")
				}
			}
		}
	}
	visit(schema, "")
	return found
}

func TestClassificationTable(t *testing.T) {
	commands := walk(t)
	for path, effect := range commandEffects {
		entry, ok := commands[path]
		if !ok {
			t.Errorf("command %q is gone", path)
			continue
		}
		if entry["effect"] != effect {
			t.Errorf("%q is classified %v, the reviewed table says %q",
				path, entry["effect"], effect)
		}
	}
}

func TestNoCommandEscapesTheTable(t *testing.T) {
	var unreviewed []string
	for path := range walk(t) {
		if _, ok := commandEffects[path]; !ok {
			unreviewed = append(unreviewed, path)
		}
	}
	sort.Strings(unreviewed)
	if len(unreviewed) > 0 {
		t.Errorf("unreviewed commands: %v -- add each to commandEffects with "+
			"the mutation it performs, or read_only", unreviewed)
	}
}

func TestConsequentialSetIsExactlyTheReviewedOne(t *testing.T) {
	actual := map[string]bool{}
	for path, entry := range walk(t) {
		if consequential, _ := entry["consequential"].(bool); consequential {
			actual[path] = true
		}
	}
	for path := range consequentialCommands {
		if !actual[path] {
			t.Errorf("%q lost its consequential declaration, which removes a "+
				"human-authority requirement silently while the command keeps working", path)
		}
	}
	for path := range actual {
		if !consequentialCommands[path] {
			t.Errorf("%q became consequential without being added to the "+
				"reviewed set -- justify the change in this file's comment first", path)
		}
	}
}

func TestRoutineMutatingCommandsDoNotPrompt(t *testing.T) {
	// These are invoked bare by release pipelines with no terminal, so a
	// confirmation requirement on any of them is a hard breakage rather than
	// an inconvenience.
	commands := walk(t)
	for _, path := range []string{
		"gen", "check", "build", "baseline.accept",
		"blog.post.new", "assembly.push",
	} {
		entry, ok := commands[path]
		if !ok {
			t.Fatalf("command %q is gone", path)
		}
		if consequential, _ := entry["consequential"].(bool); consequential {
			t.Errorf("%q became consequential; release pipelines invoke it "+
				"with no terminal and would hard-error", path)
		}
	}
}

func TestReservedQuartetIsNotRedeclared(t *testing.T) {
	// The quartet is dry-run/approve-consequential/quiet/verbose, and "yes"
	// stays separately banned even though it owns no framework flag -- a
	// private --yes would restate --approve-consequential in the very
	// spelling the rename removed.
	reserved := map[string]bool{
		"dry-run": true, "approve-consequential": true,
		"yes": true, "quiet": true, "verbose": true,
	}
	for path, entry := range walk(t) {
		flags, _ := entry["flags"].([]any)
		for _, raw := range flags {
			flag, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := flag["name"].(string)
			if reserved[name] {
				t.Errorf("%q declares reserved flag %q", path, name)
			}
		}
	}
}

func TestOnlyTheThreeUnpreviewableCommandsRefuseDryRun(t *testing.T) {
	// A command that cannot honestly preview declares the refusal instead of
	// rendering one. These three are the whole set, and each one's reason
	// says what a preview could not show.
	expected := map[string]string{
		"assembly.integrate": "reads what the step before it wrote",
		"assembly.preview":   "look at",
		"blog.editor.serve":  "at the keyboard",
	}
	for path, entry := range walk(t) {
		supported, declared := entry["dry_run_supported"]
		refuses := declared && supported == false
		reason, wanted := expected[path]
		if refuses != wanted {
			t.Errorf("%q dry-run refusal is %v, want %v", path, refuses, wanted)
			continue
		}
		if !wanted {
			continue
		}
		text, _ := entry["dry_run_unsupported_reason"].(string)
		if !strings.Contains(text, reason) {
			t.Errorf("%q refusal reason does not say %q: %s", path, reason, text)
		}
	}
}

func TestAConsequentialCommandRefusesOnNonInteractiveStdin(t *testing.T) {
	// The requirement fires BEFORE dispatch, so no deploy is attempted here.
	isolate(t)
	result := runCLI(t, t.TempDir(), "deploy")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "stdin is not interactive") {
		t.Errorf("the refusal is not the framework's: %s", result.Stderr)
	}
}

func TestANonConsequentialCommandRunsWithoutAConsentFlag(t *testing.T) {
	// The counterpart: `check` reaches its handler with no terminal and no
	// flag, and fails on its own terms rather than at a confirmation, which
	// is what proves no confirmation is there.
	isolate(t)
	result := runCLI(t, t.TempDir(), "check", "--no-auto-commit")
	if strings.Contains(result.Stderr, "stdin is not interactive") {
		t.Errorf("check asked for confirmation: %s", result.Stderr)
	}
	if strings.Contains(result.Stderr, "--approve-consequential") {
		t.Errorf("check named the consent flag: %s", result.Stderr)
	}
}
