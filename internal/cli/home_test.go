package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// homeSiteProject is the assembly's home project as its author keeps it: an
// unversioned site whose front page carries the site-level directives and
// whose docs declare the curated listing they render.
func homeSiteProject(t *testing.T, overrides map[string]any) string {
	t.Helper()
	dir := testproject.Dir(t)
	config := map[string]any{
		"name":          "Home",
		"base_url":      "https://example.com",
		"author":        testproject.Author(),
		"search_engine": "pagefind",
		"unversioned":   true,
		"locales": []any{map[string]any{
			"code": "en", "label": "English", "default": true,
		}},
		"topology": map[string]any{"slug": "home"},
		"assembly": map[string]any{"repo": "owner/assembly"},
	}
	for key, value := range overrides {
		if value == nil {
			delete(config, key)
			continue
		}
		config[key] = value
	}
	testproject.WriteJSON(t, filepath.Join(dir, "selfdoc.json"), config)
	writeText(t, filepath.Join(dir, "stricttools", "docs", "projects.toml"),
		"[[category]]\nname = \"Frameworks\"\n"+
			"[[category.project]]\nslug = \"alpha\"\n"+
			"blurb = \"Does the alpha thing.\"\n")
	writeHomeFrontPage(t, dir, "Prose the author wrote.")
	return dir
}

// writeHomeFrontPage writes the home project's front page with the given
// prose, keeping its description and its site-level markers. The body is a
// parameter so a test can change what the page says without touching what it
// claims to say, which is the shape STALE001 reports.
func writeHomeFrontPage(t *testing.T, dir, prose string) {
	t.Helper()
	writeText(t, filepath.Join(dir, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Front page\"\ndescription = \"The front page of the site.\"\n+++\n\n"+
			"# Me\n\n"+prose+"\n\n"+
			":-: projects-cards\n\n"+
			`:-: blog-highlights limit="3"`+"\n")
}

// assemblyReplies are the answers a fake gh gives as the assembly repository:
// its roster, and one project manifest under manifests/.
func assemblyReplies(t *testing.T, home string) []toolReply {
	t.Helper()
	roster := site.RenderRoster([]site.RosterEntry{
		{Slug: "home", Repo: "owner/home"},
		{Slug: "alpha", Repo: "owner/alpha"},
	}, home)
	alpha := map[string]any{
		"schema_version": 2,
		"name":           "Alpha",
		"slug":           "alpha",
		"version":        "1.0.0",
		"description":    "Alpha docs",
		"language":       "python",
		"base_url":       "https://example.com/alpha",
		"author": map[string]any{
			"name": "Test Author", "url": "https://author.example",
		},
		"pages": []any{map[string]any{"path": "index.md", "title": "Home"}},
		"posts": []any{map[string]any{
			"slug": "hello", "title": "Hello", "date": "2024-06-01",
			"path": "blog/hello.md", "tags": []any{},
		}},
		"last_gen": "2024-01-01T00:00:00+00:00",
	}
	document, err := json.Marshal(alpha)
	if err != nil {
		t.Fatalf("rendering the fixture manifest: %v", err)
	}
	tree, err := json.Marshal(map[string]any{
		"truncated": false,
		"tree": []any{
			map[string]any{"path": "roster.toml", "type": "blob", "sha": "r1"},
			map[string]any{"path": "manifests/alpha.json", "type": "blob", "sha": "a1"},
			map[string]any{"path": "manifests/alpha-files.json", "type": "blob", "sha": "a2"},
		},
	})
	if err != nil {
		t.Fatalf("rendering the fixture tree: %v", err)
	}
	return []toolReply{
		{Match: "contents/roster.toml", Stdout: encoded(roster)},
		{Match: "contents/manifests/alpha.json", Stdout: encoded(string(document))},
		{Match: "git/ref/heads/main", Stdout: "commit-sha\n"},
		{Match: "git/commits/commit-sha", Stdout: "tree-sha\n"},
		{Match: "git/trees/tree-sha", Stdout: string(tree) + "\n"},
	}
}

// serveAssembly scripts a fake gh that answers as the assembly repository and
// nothing more: every call it does not recognize answers empty.
func serveAssembly(t *testing.T, tools *fakeTools, home string) {
	t.Helper()
	tools.Reply(append(assemblyReplies(t, home), toolReply{Match: "", Stdout: ""})...)
}

// servePush scripts the assembly reads plus the Git Data API writes one
// publish makes.
func servePush(t *testing.T, tools *fakeTools, home string) {
	t.Helper()
	tools.Reply(append(assemblyReplies(t, home),
		toolReply{Match: "POST /repos/owner/assembly/git/blobs", Stdout: "blob-sha\n"},
		toolReply{Match: "POST /repos/owner/assembly/git/trees", Stdout: "new-tree-sha\n"},
		toolReply{Match: "POST /repos/owner/assembly/git/commits", Stdout: "new-commit-sha\n"},
		toolReply{Match: "PATCH", Stdout: "new-commit-sha\n"},
		toolReply{Match: "", Stdout: ""},
	)...)
}

// The home project's pages carry markers no single project's build can answer.
// A check that never reached the assembly refused them as unknown directives.
func TestCheckResolvesTheSiteDirectivesOfTheHomeProject(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := homeSiteProject(t, nil)
	serveAssembly(t, tools, "home")

	// A built page as a home build leaves it: the region the front page's
	// marker rendered into, carrying the links the assembled site resolves.
	writeText(t, filepath.Join(dir, "stricttools", ".docs-cache", "build", "index.html"),
		"<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n"+
			`<link rel="canonical" href="https://example.com/">`+"\n"+
			"</head>\n<body>\n"+
			`<selfdoc-region data-directive="projects-cards">`+"\n"+
			`<a href="alpha/">Alpha</a>`+"\n"+
			"</selfdoc-region>\n</body>\n</html>\n")

	result := run(t, dir, "check", "--no-auto-commit")
	report := result.Stdout + result.Stderr
	if strings.Contains(report, "Unknown directive") {
		t.Fatalf("the site directives were refused as unknown:\n%s", report)
	}
	if !strings.Contains(report, "2 directive(s): 2 OK, 0 FAILED") {
		t.Fatalf("the site directives did not resolve:\n%s", report)
	}
	// The cards link into other projects' subtrees, which only the assembled
	// site holds. This project's own build writes none of them, and the
	// check does not report them as references it should have written.
	if strings.Contains(report, "LINK001") {
		t.Errorf("the region's cross-project links were reported:\n%s", report)
	}
	// The card's version badge comes from the assembly's manifest for
	// another project, which is the whole reason this check reaches the
	// assembly at all.
	if calls := tools.Matching("contents/manifests/alpha.json"); len(calls) != 1 {
		t.Errorf("the assembly's manifests were read %d time(s)", len(calls))
	}
}

// A project with no assembly to read from is told so, by the directive's own
// name and the block that would have to declare it.
func TestCheckNamesTheMissingAssemblyBlockForASiteDirective(t *testing.T) {
	isolate(t)
	dir := homeSiteProject(t, map[string]any{"assembly": nil})

	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatalf("check passed with an unresolvable site directive:\n%s", result.Stdout)
	}
	report := result.Stdout + result.Stderr
	for _, want := range []string{"projects-cards", "'assembly'"} {
		if !strings.Contains(report, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, report)
		}
	}
}

// A project the roster does not name home is told which project does.
func TestCheckRefusesASiteDirectiveOnANonHomeProject(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := homeSiteProject(t, nil)
	serveAssembly(t, tools, "someone-else")

	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatalf("check passed on a project that is not home:\n%s", result.Stdout)
	}
	report := result.Stdout + result.Stderr
	for _, want := range []string{"projects-cards", "someone-else"} {
		if !strings.Contains(report, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, report)
		}
	}
}

// The home project's front page carries the site-level directives and its
// content root is the site root. Publishing it as though it were an ordinary
// project refused the directives as unknown and, had it built, would have
// filed the whole site under /home/.
func TestPublishDocsBuildsAndPlacesTheHomeProjectAsHome(t *testing.T) {
	tools := newFakeTools(t, "gh", "pagefind")
	dir := homeSiteProject(t, nil)
	servePush(t, tools, "home")

	result := run(t, dir, "blog", "publish-docs")
	if result.ExitCode != 0 {
		t.Fatalf("publish-docs exited %d\n%s\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	// The build resolved the site-level directives against the assembly's
	// manifests, which is what only a home build can do.
	front := readText(t, filepath.Join(dir, "stricttools", ".docs-cache", "build", "index.html"))
	for _, want := range []string{"projects-cards", "Does the alpha thing."} {
		if !strings.Contains(front, want) {
			t.Errorf("the built front page does not carry %q", want)
		}
	}

	// Its pages address the site root, not a subtree under its slug. The
	// tree request is the one call that states every path the commit writes.
	trees := tools.Matching("git/trees --jq")
	if len(trees) != 1 {
		t.Fatalf("the publish created %d tree(s)", len(trees))
	}
	uploaded := trees[0].Input
	if strings.Contains(uploaded, `"path":"site/home/index.html"`) {
		t.Errorf("the home project was filed under its own slug:\n%s", uploaded)
	}
	if !strings.Contains(uploaded, `"path":"site/index.html"`) {
		t.Errorf("the home project's front page is not at the site root:\n%s", uploaded)
	}
	if !strings.Contains(uploaded, `"path":"manifests/home-listing.json"`) {
		t.Errorf("the curated listing did not travel with the publish:\n%s", uploaded)
	}
}

// Accepting a reviewed baseline on a home page resolves the same site-level
// markers the check resolves. It registered none of them, so every page of the
// home project was refused as carrying an unknown directive.
func TestBaselineAcceptResolvesTheSiteDirectivesOfTheHomeProject(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := homeSiteProject(t, nil)
	serveAssembly(t, tools, "home")

	// Establish the baseline, then rewrite the front page's prose while
	// leaving its description alone: that is the STALE001 dead end
	// `baseline accept` exists to clear.
	run(t, dir, "check", "--no-auto-commit")
	writeHomeFrontPage(t, dir, "Completely rewritten prose about the site.")
	stale := staleIdentifiers(t, dir)
	if len(stale) != 1 {
		t.Fatalf("expected one stale page, got %v", stale)
	}

	result := run(t, dir, "baseline", "accept", stale[0], "--no-auto-commit")
	report := result.Stdout + result.Stderr
	if strings.Contains(report, "Unknown directive") {
		t.Fatalf("the site directives were refused as unknown:\n%s", report)
	}
	if result.ExitCode != 0 {
		t.Fatalf("baseline accept exited %d\n%s", result.ExitCode, report)
	}
	if after := staleIdentifiers(t, dir); len(after) != 0 {
		t.Errorf("STALE001 survived the acceptance: %v", after)
	}
}

// gen hashes every page it resolves, so it reaches the same markers. Without
// them registered it refused the home project outright.
func TestGenResolvesTheSiteDirectivesOfTheHomeProject(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := homeSiteProject(t, nil)
	serveAssembly(t, tools, "home")

	result := run(t, dir, "gen", "--no-auto-commit")
	report := result.Stdout + result.Stderr
	if strings.Contains(report, "Unknown directive") {
		t.Fatalf("the site directives were refused as unknown:\n%s", report)
	}
	if result.ExitCode != 0 {
		t.Fatalf("gen exited %d\n%s", result.ExitCode, report)
	}
}

// The release-post generator writes a project manifest from every resolved
// page when the project has none, which is the third path into the home
// project's markers.
func TestPostGenerateResolvesTheSiteDirectivesOfTheHomeProject(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := homeSiteProject(t, nil)
	serveAssembly(t, tools, "home")

	result := run(t, dir, "blog", "post", "generate",
		"--from-release", "--version", "1.2.3")
	report := result.Stdout + result.Stderr
	if strings.Contains(report, "Unknown directive") {
		t.Fatalf("the site directives were refused as unknown:\n%s", report)
	}
	if result.ExitCode != 0 {
		t.Fatalf("post generate exited %d\n%s", result.ExitCode, report)
	}
}
