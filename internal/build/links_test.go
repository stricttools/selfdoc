package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// linkedPages is every internal link shape a page can write: a sibling, a
// sibling with an anchor, a same-page anchor, the root index, a subdirectory
// page, a page one level up, and a page two levels up.
var linkedPages = map[string]string{
	"index.md": "# Fixture\n\n" +
		"Root page linking [the guide](guide.md), " +
		"[a section of it](guide.md#section-two) and " +
		"[the API](reference/api.md).\n",
	"guide.md": "+++\ntitle = \"Guide\"\n+++\n\n" +
		"# Guide\n\n" +
		"A sibling: [checks](checks.md). " +
		"With an anchor: [detail](checks.md#detail). " +
		"Itself: [below](#section-two). " +
		"Home: [index](index.md). " +
		"Down: [notes](reference/deep/notes.md).\n\n" +
		"## Section two\n\nSecond section.\n",
	"checks.md": "+++\ntitle = \"Checks\"\n+++\n\n" +
		"# Checks\n\nBack to [the guide](guide.md).\n\n" +
		"## Detail\n\nThe detail.\n",
	"reference/api.md": "+++\ntitle = \"API\"\n+++\n\n" +
		"# API\n\n" +
		"Up: [guide](../guide.md). Down: [notes](deep/notes.md).\n",
	"reference/deep/notes.md": "+++\ntitle = \"Notes\"\n+++\n\n" +
		"# Notes\n\n" +
		"Up: [api](../api.md). Root: [index](../../index.md).\n",
}

// legacyPages write their links in the pre-directory-addressing ".html" form,
// which an immutable git tag can carry and the working tree may not.
var legacyPages = map[string]string{
	"index.md": "# Fixture\n\nRoot page linking [the guide](guide.html).\n",
	"guide.md": "+++\ntitle = \"Guide\"\n+++\n\n" +
		"# Guide\n\n" +
		"A sibling: [checks](checks.html). " +
		"With an anchor: [detail](checks.html#detail). " +
		"Home: [index](index.html). " +
		"Down: [notes](reference/deep/notes.html).\n",
	"checks.md": "+++\ntitle = \"Checks\"\n+++\n\n" +
		"# Checks\n\nBack to [the guide](guide.html).\n\n" +
		"## Detail\n\nThe detail.\n",
	"reference/deep/notes.md": "+++\ntitle = \"Notes\"\n+++\n\n" +
		"# Notes\n\nUp: [index](../../index.html).\n",
}

// bodyHrefs lists the hrefs inside one built page's article, which is the
// region the page's own prose wrote.
func bodyHrefs(t *testing.T, s site, outputKey string) []string {
	t.Helper()
	html := s.page(t, outputKey)
	_, after, found := strings.Cut(html, "<article")
	if !found {
		t.Fatalf("%s carries no article element", outputKey)
	}
	body, _, _ := strings.Cut(after, "</article>")
	var hrefs []string
	for _, match := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		hrefs = append(hrefs, match[1])
	}
	return hrefs
}

func TestBuildRewritesMarkdownLinks(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{Docs: linkedPages})

	t.Run("every reference the build emitted resolves", func(t *testing.T) {
		lints, err := resolution.CheckOutputResolution(built.output, "https://example.com", "", nil)
		if err != nil {
			t.Fatalf("CheckOutputResolution: %v", err)
		}
		if len(lints) != 0 {
			for _, lint := range lints {
				t.Errorf("%s: %s", lint.Code(), lint.Message())
			}
		}
	})

	t.Run("a sibling link hops out of the page's own directory", func(t *testing.T) {
		// guide.md is emitted at guide/index.html, so the bare "checks/"
		// the source writes would resolve to guide/checks/ and name
		// nothing.
		hrefs := bodyHrefs(t, built, "guide/index.html")
		if !contains(hrefs, "../checks/") {
			t.Errorf("the guide's sibling link is %v, want ../checks/", hrefs)
		}
		if contains(hrefs, "checks/") {
			t.Error("the guide writes a sibling link that resolves inside its own directory")
		}
	})

	t.Run("a sibling anchor keeps its fragment", func(t *testing.T) {
		hrefs := bodyHrefs(t, built, "guide/index.html")
		if !contains(hrefs, "../checks/#detail") {
			t.Errorf("the guide's anchored link is %v, want ../checks/#detail", hrefs)
		}
		for _, href := range hrefs {
			if strings.HasSuffix(href, ".md#detail") {
				t.Errorf("the link %q was not rewritten", href)
			}
		}
	})

	t.Run("a same-page anchor is left alone", func(t *testing.T) {
		if !contains(bodyHrefs(t, built, "guide/index.html"), "#section-two") {
			t.Error("the guide's own-page anchor was rewritten")
		}
	})

	t.Run("the root index is reached from a page one level down", func(t *testing.T) {
		if !contains(bodyHrefs(t, built, "guide/index.html"), "../index.html") {
			t.Error("the guide does not reach the root index")
		}
	})

	t.Run("the root page links its children with no hop", func(t *testing.T) {
		hrefs := bodyHrefs(t, built, "index.html")
		for _, want := range []string{"guide/", "guide/#section-two", "reference/api/"} {
			if !contains(hrefs, want) {
				t.Errorf("the root page's links are %v, want one naming %q", hrefs, want)
			}
		}
	})

	t.Run("a subdirectory page links up and down", func(t *testing.T) {
		hrefs := bodyHrefs(t, built, "reference/api/index.html")
		for _, want := range []string{"../../guide/", "../deep/notes/"} {
			if !contains(hrefs, want) {
				t.Errorf("the API page's links are %v, want one naming %q", hrefs, want)
			}
		}
		// notes.md is emitted at reference/deep/notes/, so its
		// source-level hop out gains one level.
		deep := bodyHrefs(t, built, "reference/deep/notes/index.html")
		for _, want := range []string{"../../api/", "../../../index.html"} {
			if !contains(deep, want) {
				t.Errorf("the notes page's links are %v, want one naming %q", deep, want)
			}
		}
	})

	t.Run("a Markdown link inside a code block is not an address", func(t *testing.T) {
		withCode := map[string]string{}
		for relPath, content := range linkedPages {
			withCode[relPath] = content
		}
		withCode["checks.md"] += "\n```\n[example](thing.md)\n```\n"
		codeBuilt := buildFixture(t, fixture{Docs: withCode})
		assertCarries(t, "checks/index.html",
			codeBuilt.page(t, "checks/index.html"), "[example](thing.md)")
	})
}

func TestBuildRendersLegacyLinksInAnArchiveOnly(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("an archive's pre-scheme links are mapped at render time", func(t *testing.T) {
		// A tag is immutable, so a page it carries cannot be edited to use
		// the current link form.
		dir := twoTagProject(t, legacyPages, linkedPages)
		written, err := Build(Options{DirPath: dir, Stdout: &discard{}}, effects.Unbound())
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		built := site{dir: dir, output: filepath.Join(dir, ".stricttools", "docs-cache", "build"), written: written}

		if !built.exists("v/0.1.0/guide/index.html") {
			t.Fatal("the archived guide was not built")
		}
		lints, err := resolution.CheckOutputResolution(built.output, "https://example.com", "", nil)
		if err != nil {
			t.Fatalf("CheckOutputResolution: %v", err)
		}
		for _, lint := range lints {
			t.Errorf("%s: %s", lint.Code(), lint.Message())
		}
		hrefs := bodyHrefs(t, built, "v/0.1.0/guide/index.html")
		for _, want := range []string{
			"../checks/", "../checks/#detail", "../index.html", "../reference/deep/notes/",
		} {
			if !contains(hrefs, want) {
				t.Errorf("the archived guide's links are %v, want one naming %q", hrefs, want)
			}
		}
	})

	t.Run("the working tree gets no such tolerance", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: legacyPages})
		lints, err := resolution.CheckOutputResolution(built.output, "https://example.com", "", nil)
		if err != nil {
			t.Fatalf("CheckOutputResolution: %v", err)
		}
		if len(lints) == 0 {
			t.Fatal("a legacy .html link in the working tree was not reported")
		}
		named := false
		for _, lint := range lints {
			if lint.Code() != "LINK001" {
				t.Errorf("the report carries %s, want only LINK001", lint.Code())
			}
			if strings.Contains(lint.Message(), "checks.html") {
				named = true
			}
		}
		if !named {
			t.Error("no report names the broken reference")
		}
	})
}

// twoTagProject writes a project whose v0.1.0 tag holds archivedPages and
// whose working tree holds currentPages, so one build renders both an archive
// and a current version.
func twoTagProject(t *testing.T, archivedPages, currentPages map[string]string) string {
	t.Helper()
	dir := testproject.Make(t, map[string]any{
		"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/",
		"version":  "0.2.0",
		"versions": []any{map[string]any{"version": "0.1.0"}, map[string]any{"version": "0.2.0"}},
	})
	lay := func(pages map[string]string) {
		t.Helper()
		docsDir := filepath.Join(dir, ".stricttools", "docs")
		existing, err := markdownFilesUnder(docsDir)
		if err != nil {
			t.Fatalf("listing the docs tree: %v", err)
		}
		for _, path := range existing {
			if err := os.Remove(path); err != nil {
				t.Fatalf("clearing %s: %v", path, err)
			}
		}
		for relPath, content := range pages {
			testproject.WriteText(t, filepath.Join(docsDir, filepath.FromSlash(relPath)), content)
		}
	}

	lay(archivedPages)
	testproject.Git(t, dir, "init")
	testproject.Git(t, dir, "add", ".")
	testproject.Git(t, dir, "commit", "-m", "0.1.0")
	testproject.Git(t, dir, "tag", "v0.1.0")

	lay(currentPages)
	testproject.Git(t, dir, "add", "-A")
	testproject.Git(t, dir, "commit", "-m", "0.2.0")
	testproject.Git(t, dir, "tag", "v0.2.0")
	return dir
}
