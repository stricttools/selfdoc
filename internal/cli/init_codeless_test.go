package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// `selfdoc init` on a project with no source code.
//
// A portfolio or personal site is pure content: markdown pages, no code to
// extract from. Init must produce a configuration for such a project that the
// loader accepts and the build consumes without any hand-editing.

const (
	fixtureBaseURL    = "https://example.com"
	fixtureAuthorName = "Test Author"
	fixtureAuthorURL  = "https://author.example"
)

// codelessProject is a directory containing only markdown -- no code, no
// manifests.
func codelessProject(t *testing.T) string {
	t.Helper()
	dir := testproject.Dir(t)
	writeText(t, filepath.Join(dir, ".stricttools", "docs", "about.md"),
		"+++\ntitle = \"About\"\n"+
			"description = \"A short page about this site and the person who writes it.\"\n"+
			"+++\n\n# About\n\nThis page has no code behind it.\n")
	return dir
}

// initProject runs `init` with the fixture's author and base URL.
func initProject(t *testing.T, dir string, extra ...string) (exitCode int, stdout, stderr string) {
	t.Helper()
	argv := append([]string{"init",
		"--base-url", fixtureBaseURL,
		"--author-name", fixtureAuthorName,
		"--author-url", fixtureAuthorURL,
		"--no-auto-commit"}, extra...)
	result := run(t, dir, argv...)
	return result.ExitCode, result.Stdout, result.Stderr
}

func TestInitAcceptsACodelessProject(t *testing.T) {
	isolate(t)
	dir := codelessProject(t)
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init refused a codeless project: %s", stderr)
	}

	config := readJSON(t, filepath.Join(dir, "selfdoc.json"))
	// No source declaration at all -- the project has nothing to extract from.
	if _, declared := config["source"]; declared {
		t.Errorf("a codeless project's config declares source: %v", config["source"])
	}
	if config["base_url"] != fixtureBaseURL {
		t.Errorf("base_url is %v", config["base_url"])
	}
	// Versioned at 0.0.0, never "unversioned": true (see init_layout_test.go).
	assertVersionedAt(t, dir, "0.0.0")
}

func TestInitWritesTheDeclaredAuthor(t *testing.T) {
	// Nothing about a person is inferable from a directory, so the two facts
	// are inputs and land in the file verbatim.
	isolate(t)
	dir := codelessProject(t)
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}
	author := readJSON(t, filepath.Join(dir, "selfdoc.json"))["author"].(map[string]any)
	if author["name"] != fixtureAuthorName || author["url"] != fixtureAuthorURL {
		t.Errorf("author is %v", author)
	}
}

func TestInitRefusesAnEmptyAuthorName(t *testing.T) {
	isolate(t)
	dir := codelessProject(t)
	result := run(t, dir, "init",
		"--base-url", fixtureBaseURL, "--author-name", "  ",
		"--author-url", fixtureAuthorURL, "--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatal("init accepted a blank author name")
	}
	if exists(filepath.Join(dir, "selfdoc.json")) {
		t.Error("a refused init wrote a config")
	}
}

func TestInitRefusesAnEmptyAuthorURL(t *testing.T) {
	isolate(t)
	dir := codelessProject(t)
	result := run(t, dir, "init",
		"--base-url", fixtureBaseURL, "--author-name", fixtureAuthorName,
		"--author-url", "", "--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatal("init accepted a blank author URL")
	}
	if exists(filepath.Join(dir, "selfdoc.json")) {
		t.Error("a refused init wrote a config")
	}
}

func TestTheCodelessStarterHasNoCodeDirective(t *testing.T) {
	isolate(t)
	dir := codelessProject(t)
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}
	index := readText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"))
	if strings.Contains(index, ":-: ref") {
		t.Errorf("the starter page carries an extraction directive:\n%s", index)
	}
	if strings.Contains(index, "API Reference") {
		t.Errorf("the starter page carries an API section:\n%s", index)
	}
}

func TestACodelessProjectBuilds(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	dir := codelessProject(t)
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}

	run(t, dir, "build", "--no-auto-commit")

	out := filepath.Join(dir, ".stricttools", "docs-cache", "build")
	if !exists(filepath.Join(out, "index.html")) {
		t.Error("no index.html written")
	}
	if !exists(filepath.Join(out, "about", "index.html")) {
		t.Error("no about page written")
	}
	if !strings.Contains(readText(t, filepath.Join(out, "index.html")), "<!DOCTYPE html>") {
		t.Error("index.html is not a document")
	}
}

func TestGenSkipsReferencePagesForACodelessProject(t *testing.T) {
	isolate(t)
	dir := codelessProject(t)
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}

	result := run(t, dir, "gen", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("gen failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if exists(filepath.Join(dir, ".stricttools", "docs", "gen-index.md")) {
		t.Error("gen wrote an API index for a project with no source")
	}
	if !strings.Contains(result.Stdout, "source") {
		t.Errorf("gen did not say why it skipped the reference pages:\n%s", result.Stdout)
	}
}

func TestInitEmitsALoadableConfigForACodeProject(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := testproject.Dir(t)
	writeText(t, filepath.Join(dir, "pyproject.toml"),
		"[project]\nname = \"testproj\"\nversion = \"2.3.4\"\n")
	writeText(t, filepath.Join(dir, "testproj", "__init__.py"), "\"\"\"Test package.\"\"\"\n")

	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}

	config := readJSON(t, filepath.Join(dir, "selfdoc.json"))
	versions := config["versions"].([]any)
	if len(versions) != 1 || versions[0].(map[string]any)["version"] != "2.3.4" {
		t.Errorf("versions is %v, want the manifest's version", versions)
	}
	locales := config["locales"].([]any)
	if len(locales) != 1 {
		t.Fatalf("locales is %v", locales)
	}
	locale := locales[0].(map[string]any)
	if locale["code"] != "en" || locale["label"] != "English" || locale["default"] != true {
		t.Errorf("locale is %v", locale)
	}

	run(t, dir, "build", "--no-auto-commit")
	if !exists(filepath.Join(dir, ".stricttools", "docs-cache", "build", "index.html")) {
		t.Error("the scaffolded project does not build")
	}
}
