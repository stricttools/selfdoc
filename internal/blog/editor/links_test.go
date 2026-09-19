package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// Cross-repository link targets, and the addresses they insert.
//
// A post is a site citizen: it is emitted at "blog/<post-slug>/" on the site
// root while a project's documentation is served under that project's own
// slug. So the link a post writes to reach a project page is neither the link
// that project's own pages use between themselves nor a site-absolute path --
// it is the project-mounted address, reached from two directories down, and it
// has to reach the renderer untouched.
//
// The last assertion here is the one that carries the most: an inserted link
// is put through the real render path and its target is resolved the way the
// deploy's own reference check resolves one. Anything less would be testing
// string concatenation.

// A post written to carry no lint findings at all, with a description long
// enough that the description-length rule has nothing to say.
const linkDescription = "A post written to carry no lint findings at all, with a description " +
	"long enough that the description-length rule has nothing to say."

// manifestHeading is one heading a fixture manifest declares.
type manifestHeading struct {
	level  int
	text   string
	anchor string
}

// manifestPage is one page a fixture manifest declares.
type manifestPage struct {
	path     string
	title    string
	headings []manifestHeading
}

// fixtureManifest assembles a manifest document the way a build writes one.
func fixtureManifest(slug, name string, pages []manifestPage) map[string]any {
	declared := []any{}
	for _, page := range pages {
		headings := []any{}
		for _, heading := range page.headings {
			headings = append(headings, map[string]any{
				"level": heading.level, "text": heading.text, "anchor": heading.anchor,
			})
		}
		declared = append(declared, map[string]any{
			"path": page.path, "title": page.title, "type": "doc",
			"headings": headings,
		})
	}
	return map[string]any{
		"schema_version": 1,
		"name":           name,
		"slug":           slug,
		"version":        "1.0.0",
		"description":    name + " docs",
		"language":       "python",
		"base_url":       "https://docs.example.com/" + slug,
		"pages":          declared,
		"posts":          []any{},
		"last_gen":       "2024-01-01T00:00:00+00:00",
	}
}

// alphaManifest and betaManifest are the two projects the fixtures register.
func alphaManifest() map[string]any {
	return fixtureManifest("alpha", "Alpha", []manifestPage{
		{path: "index.md", title: "Alpha"},
		{path: "guide.md", title: "Alpha Guide", headings: []manifestHeading{
			{1, "Alpha Guide", "alpha-guide"},
			{2, "Getting started", "getting-started"},
		}},
	})
}

func betaManifest() map[string]any {
	return fixtureManifest("beta", "Beta", []manifestPage{
		{path: "api/reference.md", title: "Beta Reference", headings: []manifestHeading{
			{2, "Types", "types"},
		}},
	})
}

// writeManifest writes one project manifest under a project root.
func writeManifest(t *testing.T, root string, manifest map[string]any) string {
	t.Helper()
	path := filepath.Join(root, ".stricttools", "docs-state", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// bareProject is a directory with nothing in it: a registered repository that
// has never been built.
func bareProject(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return root
}

// twoRepos registers two projects, each with a manifest.
func twoRepos(t *testing.T) *TargetIndex {
	t.Helper()
	alpha := bareProject(t, "alpha")
	beta := bareProject(t, "beta")
	writeManifest(t, alpha, alphaManifest())
	writeManifest(t, beta, betaManifest())
	return NewTargetIndex(writeRegistry(t, local("alpha", alpha), local("beta", beta)))
}

// targetsOf returns one manifest's targets, failing on a refusal.
func targetsOf(manifest map[string]any, repoName string) []Target {
	return ManifestTargets(manifest, repoName)
}

// search runs one query over an index, failing on a refusal.
func search(t *testing.T, index *TargetIndex, query string, limit int) []Target {
	t.Helper()
	found, err := index.Search(query, limit)
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	return found
}

// allTargets returns every target an index offers, failing on a refusal.
func allTargets(t *testing.T, index *TargetIndex) []Target {
	t.Helper()
	found, err := index.AllTargets()
	if err != nil {
		t.Fatalf("AllTargets: %v", err)
	}
	return found
}

// -- the address ------------------------------------------------------------

func TestTheHopIsDerived(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("a post sits two directories below the site root", func(t *testing.T) {
		if PostDepth != 2 {
			t.Errorf("PostDepth = %d, want 2", PostDepth)
		}
		if ToSiteRoot != "../../" {
			t.Errorf("ToSiteRoot = %q, want ../../", ToSiteRoot)
		}
	})

	t.Run("the hop is read off the post address itself", func(t *testing.T) {
		emitted := shared.TargetOutputPath(shared.PostTarget("some-post"))
		want := strings.Repeat("../", len(strings.Split(emitted, "/"))-1)
		if ToSiteRoot != want {
			t.Errorf("ToSiteRoot = %q, want %q", ToSiteRoot, want)
		}
	})

	t.Run("a page link is the project-mounted address", func(t *testing.T) {
		if got := TargetHref("alpha", "guide.md", ""); got != "../../alpha/guide/" {
			t.Errorf("href = %q", got)
		}
	})

	t.Run("a project's index is its own mount", func(t *testing.T) {
		if got := TargetHref("alpha", "index.md", ""); got != "../../alpha/" {
			t.Errorf("href = %q", got)
		}
	})

	t.Run("a nested page keeps its directories", func(t *testing.T) {
		if got := TargetHref("beta", "api/reference.md", ""); got != "../../beta/api/reference/" {
			t.Errorf("href = %q", got)
		}
	})

	t.Run("a section carries its anchor", func(t *testing.T) {
		got := TargetHref("alpha", "guide.md", "getting-started")
		if got != "../../alpha/guide/#getting-started" {
			t.Errorf("href = %q", got)
		}
	})

	t.Run("the address is the one the assembly serves", func(t *testing.T) {
		want := ToSiteRoot + shared.PageTarget("alpha", "guide.md", false)
		if got := TargetHref("alpha", "guide.md", ""); got != want {
			t.Errorf("href = %q, want %q", got, want)
		}
	})
}

// -- what a manifest offers -------------------------------------------------

func TestManifestTargets(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("every page is offered with its title and address", func(t *testing.T) {
		var got []string
		for _, target := range targetsOf(alphaManifest(), "alpha-repo") {
			if target.Kind == "page" {
				got = append(got, target.Title+"|"+target.Address)
			}
		}
		want := []string{"Alpha|alpha/", "Alpha Guide|alpha/guide/"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("pages = %v, want %v", got, want)
		}
	})

	t.Run("every heading is offered as a section", func(t *testing.T) {
		var got []string
		for _, target := range targetsOf(alphaManifest(), "alpha-repo") {
			if target.Kind == "section" {
				got = append(got, target.Title+"|"+target.Address)
			}
		}
		want := []string{
			"Alpha Guide|alpha/guide/#alpha-guide",
			"Getting started|alpha/guide/#getting-started",
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("sections = %v, want %v", got, want)
		}
	})

	t.Run("a section names the page it is on", func(t *testing.T) {
		sections := []Target{}
		for _, target := range targetsOf(betaManifest(), "beta-repo") {
			if target.Kind == "section" {
				sections = append(sections, target)
			}
		}
		if len(sections) != 1 {
			t.Fatalf("sections = %v, want one", sections)
		}
		if sections[0].PageTitle == nil || *sections[0].PageTitle != "Beta Reference" {
			t.Errorf("page_title = %v", sections[0].PageTitle)
		}
		if sections[0].Page != "api/reference.md" {
			t.Errorf("page = %q", sections[0].Page)
		}
		if sections[0].Level == nil || *sections[0].Level != 2 {
			t.Errorf("level = %v", sections[0].Level)
		}
	})

	t.Run("a target carries the repository it came from", func(t *testing.T) {
		for _, target := range targetsOf(betaManifest(), "beta-repo") {
			if target.Repo != "beta-repo" {
				t.Errorf("repo = %q", target.Repo)
			}
			if target.Slug != "beta" {
				t.Errorf("slug = %q", target.Slug)
			}
		}
	})

	t.Run("a heading without an anchor is not a target", func(t *testing.T) {
		manifest := fixtureManifest("gamma", "Gamma", []manifestPage{
			{path: "g.md", title: "G", headings: []manifestHeading{{2, "No anchor", ""}}},
		})
		kinds := []string{}
		for _, target := range targetsOf(manifest, "g") {
			kinds = append(kinds, target.Kind)
		}
		if strings.Join(kinds, ",") != "page" {
			t.Errorf("kinds = %v, want only a page", kinds)
		}
	})

	t.Run("a page target carries neither page_title nor level", func(t *testing.T) {
		for _, target := range targetsOf(alphaManifest(), "alpha") {
			if target.Kind != "page" {
				continue
			}
			body, err := encodeJSON(target)
			if err != nil {
				t.Fatalf("encoding a target: %v", err)
			}
			if strings.Contains(string(body), "page_title") ||
				strings.Contains(string(body), "level") {
				t.Errorf("a page target reads %s", body)
			}
		}
	})
}

func TestLoadingAManifest(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("an absent manifest is absence not an error", func(t *testing.T) {
		manifest, err := LoadManifest(filepath.Join(t.TempDir(), "nope.json"))
		if err != nil {
			t.Fatalf("LoadManifest: %v", err)
		}
		if manifest != nil {
			t.Errorf("manifest = %v, want none", manifest)
		}
	})

	t.Run("a malformed manifest is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("writing: %v", err)
		}
		_, err := LoadManifest(path)
		var manifestError *ManifestError
		if err == nil {
			t.Fatal("want a refusal")
		}
		if !asManifestError(err, &manifestError) {
			t.Fatalf("err = %T (%v), want *ManifestError", err, err)
		}
		if !strings.Contains(manifestError.Message, "readable manifest") {
			t.Errorf("message = %q", manifestError.Message)
		}
	})
}

// asManifestError is errors.As for a manifest refusal, spelled out so the
// test reads like the assertion it is.
func asManifestError(err error, target **ManifestError) bool {
	manifestError, ok := err.(*ManifestError)
	if ok {
		*target = manifestError
	}
	return ok
}

// -- the index across every repository --------------------------------------

func TestTargetIndex(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("every registered repository contributes", func(t *testing.T) {
		repos := map[string]bool{}
		for _, target := range allTargets(t, twoRepos(t)) {
			repos[target.Repo] = true
		}
		if len(repos) != 2 || !repos["alpha"] || !repos["beta"] {
			t.Errorf("repos = %v", repos)
		}
	})

	t.Run("a query matches a title", func(t *testing.T) {
		found := search(t, twoRepos(t), "getting", 40)
		if len(found) != 1 || found[0].Title != "Getting started" {
			t.Errorf("found = %v", found)
		}
	})

	t.Run("a query matches an address", func(t *testing.T) {
		for _, target := range search(t, twoRepos(t), "beta/api", 40) {
			if !strings.Contains(target.Address, "beta") {
				t.Errorf("address = %q", target.Address)
			}
		}
	})

	t.Run("the match is case insensitive", func(t *testing.T) {
		if len(search(t, twoRepos(t), "GUIDE", 40)) == 0 {
			t.Error("an upper-case query matched nothing")
		}
	})

	t.Run("an empty query offers everything", func(t *testing.T) {
		index := twoRepos(t)
		if len(search(t, index, "", 40)) != len(allTargets(t, index)) {
			t.Error("an empty query did not offer everything")
		}
	})

	t.Run("the limit is honoured", func(t *testing.T) {
		if got := len(search(t, twoRepos(t), "", 2)); got != 2 {
			t.Errorf("found %d targets, want 2", got)
		}
	})

	t.Run("a repository with no manifest offers nothing", func(t *testing.T) {
		bare := bareProject(t, "bare")
		index := NewTargetIndex(writeRegistry(t, local("bare", bare)))
		if found := allTargets(t, index); len(found) != 0 {
			t.Errorf("found = %v, want nothing", found)
		}
	})

	t.Run("a rewritten manifest is picked up", func(t *testing.T) {
		alpha := bareProject(t, "alpha")
		writeManifest(t, alpha, alphaManifest())
		index := NewTargetIndex(writeRegistry(t, local("alpha", alpha)))
		if found := search(t, index, "Rebuilt", 40); len(found) != 0 {
			t.Fatalf("found = %v, want nothing yet", found)
		}

		rebuilt := fixtureManifest("alpha", "Alpha", []manifestPage{
			{path: "new.md", title: "Rebuilt Page"},
		})
		writeManifest(t, alpha, rebuilt)
		found := search(t, index, "Rebuilt", 40)
		if len(found) != 1 || found[0].Title != "Rebuilt Page" {
			t.Errorf("found = %v", found)
		}
	})

	t.Run("a remote entry contributes nothing", func(t *testing.T) {
		index := NewTargetIndex(writeRegistry(t, remote(t, "afar")))
		if found := allTargets(t, index); len(found) != 0 {
			t.Errorf("found = %v, want nothing", found)
		}
	})
}

// -- the endpoint -----------------------------------------------------------

func TestTheLinkTargetEndpoint(t *testing.T) {
	hygiene.Isolate(t)
	alpha := bareProject(t, "alpha")
	beta := bareProject(t, "beta")
	writeManifest(t, alpha, alphaManifest())
	writeManifest(t, beta, betaManifest())
	reg := writeRegistry(t, local("alpha", alpha), local("beta", beta))
	port := serveEditor(t, newState(t, reg, nil))

	// targetsFrom reads the targets one query answered with.
	targetsFrom := func(t *testing.T, path string) (int, []map[string]any) {
		t.Helper()
		status, body := requestJSON(t, port, "GET", path, "")
		raw, _ := body["targets"].([]any)
		targets := []map[string]any{}
		for _, item := range raw {
			target, _ := item.(map[string]any)
			targets = append(targets, target)
		}
		return status, targets
	}

	t.Run("it answers targets from every repository", func(t *testing.T) {
		status, targets := targetsFrom(t, "/api/link-targets")
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		repos := map[string]bool{}
		for _, target := range targets {
			repos[target["repo"].(string)] = true
		}
		if len(repos) != 2 || !repos["alpha"] || !repos["beta"] {
			t.Errorf("repos = %v", repos)
		}
	})

	t.Run("a link trigger offers pages and sections across two repos", func(t *testing.T) {
		_, targets := targetsFrom(t, "/api/link-targets")
		seen := map[string]bool{}
		for _, target := range targets {
			seen[target["repo"].(string)+"/"+target["kind"].(string)] = true
		}
		for _, want := range []string{"alpha/page", "alpha/section", "beta/page", "beta/section"} {
			if !seen[want] {
				t.Errorf("no %s target was offered", want)
			}
		}
	})

	t.Run("the query filters", func(t *testing.T) {
		_, targets := targetsFrom(t, "/api/link-targets?q=Types")
		if len(targets) != 1 || targets[0]["title"] != "Types" {
			t.Errorf("targets = %v", targets)
		}
	})

	t.Run("the limit is honoured", func(t *testing.T) {
		_, targets := targetsFrom(t, "/api/link-targets?limit=1")
		if len(targets) != 1 {
			t.Errorf("targets = %v, want one", targets)
		}
	})

	t.Run("a nonsense limit is refused", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET", "/api/link-targets?limit=lots", "")
		if status != 400 {
			t.Fatalf("status = %d, want 400", status)
		}
		if !strings.Contains(errorText(t, body), "whole number") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("a limit below one is refused", func(t *testing.T) {
		status, body := requestJSON(t, port, "GET", "/api/link-targets?limit=0", "")
		if status != 400 {
			t.Fatalf("status = %d, want 400", status)
		}
		if !strings.Contains(errorText(t, body), "at least 1") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})
}

// -- the inserted link, through the renderer --------------------------------

// hrefsIn returns every href a rendered page emits.
func hrefsIn(html string) []string {
	pattern := regexp.MustCompile(`<a [^>]*href="([^"]+)"`)
	found := []string{}
	for _, match := range pattern.FindAllStringSubmatch(html, -1) {
		found = append(found, match[1])
	}
	return found
}

// renderWithLinks renders a post carrying one link per target, through the
// real render path.
func renderWithLinks(t *testing.T, targets []Target) string {
	t.Helper()
	links := []string{}
	for _, target := range targets {
		links = append(links, "See ["+target.Title+"]("+target.Href+").")
	}
	content := "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"description = \"" + linkDescription + "\"\n" +
		"tags = [\"release\"]\ndraft = false\ndirectives = false\n+++\n" +
		"# Hello World\n\n" + strings.Join(links, "\n\n") + "\n"

	project := makeProject(t, map[string]string{postHelloName: content})
	entry := entryOf(t, "writer", project)
	html, err := RenderPreview(entry, postHelloName, content, effects.Unbound())
	if err != nil {
		t.Fatalf("RenderPreview: %v", err)
	}
	return html
}

// targetWithAddress returns the one target of a manifest at an address.
func targetWithAddress(t *testing.T, manifest map[string]any, repo, address string) Target {
	t.Helper()
	for _, target := range targetsOf(manifest, repo) {
		if target.Address == address {
			return target
		}
	}
	t.Fatalf("no target at %q", address)
	return Target{}
}

func TestAnInsertedLinkResolves(t *testing.T) {
	// The done-when: what the completion inserts is what the site serves.
	// The link is put through the render path -- the publish renderer over an
	// unsaved buffer, which is what the preview pane shows -- and its target
	// is then resolved from the post's OWN emitted address with the same
	// function the deploy's reference check uses. Landing on the file the
	// target project's page is emitted at is what "resolves" means.
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a page link from two repositories resolves", func(t *testing.T) {
		targets := []Target{
			targetWithAddress(t, alphaManifest(), "alpha", "alpha/guide/"),
			targetWithAddress(t, betaManifest(), "beta", "beta/api/reference/"),
		}
		emitted := hrefsIn(renderWithLinks(t, targets))
		for _, target := range targets {
			if !contains(emitted, target.Href) {
				t.Errorf("the renderer did not emit %q verbatim: %v", target.Href, emitted)
			}
			resolved, ok := resolution.ReferenceTarget(
				shared.TargetOutputPath(shared.PostTarget("hello-world")), target.Href)
			if !ok {
				t.Fatalf("%q addresses nothing", target.Href)
			}
			if want := shared.TargetOutputPath(target.Address); resolved != want {
				t.Errorf("%q resolved to %q, want %q", target.Href, resolved, want)
			}
		}
	})

	t.Run("a section link resolves to the page that carries the anchor", func(t *testing.T) {
		target := Target{}
		for _, candidate := range targetsOf(alphaManifest(), "alpha") {
			if candidate.Anchor == "getting-started" {
				target = candidate
			}
		}
		if target.Href == "" {
			t.Fatal("the fixture manifest declares no getting-started anchor")
		}
		if !contains(hrefsIn(renderWithLinks(t, []Target{target})), target.Href) {
			t.Errorf("the renderer did not emit %q verbatim", target.Href)
		}
		resolved, ok := resolution.ReferenceTarget(
			shared.TargetOutputPath(shared.PostTarget("hello-world")), target.Href)
		if !ok {
			t.Fatalf("%q addresses nothing", target.Href)
		}
		// The resolution drops the fragment: an anchor is a position on a
		// page, and the page is what has to exist.
		want := shared.TargetOutputPath(shared.PageTarget("alpha", "guide.md", false))
		if resolved != want {
			t.Errorf("resolved to %q, want %q", resolved, want)
		}
	})

	t.Run("the link is not rewritten into something else", func(t *testing.T) {
		// A directory URL is not a .md link, so nothing rewrites it.
		target := targetWithAddress(t, alphaManifest(), "alpha", "alpha/")
		if !contains(hrefsIn(renderWithLinks(t, []Target{target})), "../../alpha/") {
			t.Error("the emitted link is not the inserted one")
		}
	})

	t.Run("the link is never origin absolute", func(t *testing.T) {
		// The site has to resolve under any mount point.
		for _, target := range targetsOf(alphaManifest(), "alpha") {
			if strings.HasPrefix(target.Href, "/") {
				t.Errorf("href = %q, want a document-relative link", target.Href)
			}
			if strings.Contains(target.Href, "://") {
				t.Errorf("href = %q, want a document-relative link", target.Href)
			}
		}
	})
}

// contains reports whether a list holds one value.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
