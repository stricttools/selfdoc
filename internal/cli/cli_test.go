package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// requirePython3 skips a test when no interpreter is on PATH: the Python
// extractor reads a module's symbols through an embedded driver run under
// python3, so a machine without one cannot resolve a `ref` directive.
func requirePython3(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH: the Python extractor runs through it")
	}
	return path
}

// requirePagefind makes a Pagefind installation reachable, skipping the test
// when there is none: without one the build refuses and every project-level
// check reports SEARCH001, which is a different run than the one under test.
//
// It runs BEFORE the isolation floor, because the shim it may write names an
// interpreter under the real home directory.
func requirePagefind(t *testing.T) {
	t.Helper()
	testproject.RequirePagefind(t)
}

// pythonProject creates the minimal Python project `selfdoc init` detects: a
// manifest that names a version, and a package for it to point at.
func pythonProject(t *testing.T) string {
	t.Helper()
	dir := testproject.Dir(t)
	writeText(t, filepath.Join(dir, "pyproject.toml"),
		"[project]\nname = \"testproj\"\nversion = \"1.0.0\"\n")
	writeText(t, filepath.Join(dir, "testproj", "__init__.py"), "")
	return dir
}

// initialized runs `init` on a fresh Python project and returns its directory.
func initialized(t *testing.T) string {
	t.Helper()
	dir := pythonProject(t)
	result := run(t, dir, "init",
		"--base-url", "https://example.com",
		"--author-name", "Test Author",
		"--author-url", "https://author.example",
		"--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("init failed: %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	return dir
}

func TestInitCreatesConfigAndDocs(t *testing.T) {
	isolate(t)
	dir := initialized(t)

	config := readJSON(t, filepath.Join(dir, "selfdoc.json"))
	source, ok := config["source"].([]any)
	if !ok || len(source) == 0 {
		t.Fatalf("no source entries: %v", config["source"])
	}
	found := false
	for _, raw := range source {
		entry := raw.(map[string]any)
		if entry["path"] == "testproj/" && entry["language"] == "python" {
			found = true
		}
	}
	if !found {
		t.Errorf("source does not name the package: %v", source)
	}
	if config["docs"] != ".stricttools/docs/" {
		t.Errorf("docs is %v", config["docs"])
	}
	if config["output"] != ".stricttools/docs-cache/build/" {
		t.Errorf("output is %v", config["output"])
	}

	content := readText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"))
	if !strings.Contains(content, "testproj") {
		t.Errorf("starter page does not name the project:\n%s", content)
	}
	if !strings.Contains(content, `:-: ref path="testproj" lang="python"`) {
		t.Errorf("starter page carries no ref directive:\n%s", content)
	}
}

func TestInitIndexHasFrontmatter(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	content := readText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"))

	if !strings.HasPrefix(content, "+++\n") {
		t.Fatalf("starter page has no frontmatter:\n%s", content)
	}
	parts := strings.SplitN(content, "+++\n", 3)
	if len(parts) < 3 {
		t.Fatalf("frontmatter must have an opening and a closing +++:\n%s", content)
	}
	block := parts[1]
	if !strings.Contains(block, "description = \"Documentation for ") {
		t.Errorf("frontmatter carries no description:\n%s", block)
	}
	dateLine := ""
	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		if strings.HasPrefix(line, "date = ") {
			dateLine = strings.TrimPrefix(line, "date = ")
		}
	}
	if dateLine == "" {
		t.Fatalf("frontmatter carries no date:\n%s", block)
	}
	if _, err := time.Parse("2006-01-02", dateLine); err != nil {
		t.Errorf("date %q is not an ISO date: %v", dateLine, err)
	}
}

func TestInitAbortsIfConfigExists(t *testing.T) {
	isolate(t)
	dir := pythonProject(t)
	writeText(t, filepath.Join(dir, "selfdoc.json"), "{}")

	result := run(t, dir, "init",
		"--base-url", "https://example.com",
		"--author-name", "Test Author",
		"--author-url", "https://author.example",
		"--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatalf("init overwrote an existing config")
	}
	if !strings.Contains(result.Stdout, "already exists") {
		t.Errorf("no refusal printed: %q", result.Stdout)
	}
}

func TestInitDetectsMultipleLanguages(t *testing.T) {
	isolate(t)
	dir := testproject.Dir(t)
	writeText(t, filepath.Join(dir, "pyproject.toml"),
		"[project]\nname = \"polyglot\"\nversion = \"0.1.0\"\n")
	writeText(t, filepath.Join(dir, "go.mod"), "module example.com/polyglot\n")
	writeText(t, filepath.Join(dir, "polyglot", "__init__.py"), "")
	testMkdir(t, filepath.Join(dir, "cmd"))

	result := run(t, dir, "init",
		"--base-url", "https://example.com",
		"--author-name", "Test Author",
		"--author-url", "https://author.example",
		"--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("init failed: %s\n%s", result.Stdout, result.Stderr)
	}

	config := readJSON(t, filepath.Join(dir, "selfdoc.json"))
	languages := map[string]int{}
	for _, raw := range config["source"].([]any) {
		languages[raw.(map[string]any)["language"].(string)]++
	}
	if languages["python"] == 0 {
		t.Errorf("python not detected: %v", config["source"])
	}
	if languages["go"] == 0 {
		t.Errorf("go not detected: %v", config["source"])
	}
}

func TestBuildProducesOutput(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	// The build may exit non-zero on the starter template's lints; the
	// output files are still written before the lint pass.
	run(t, dir, "build", "--no-auto-commit")

	index := filepath.Join(dir, ".stricttools", "docs-cache", "build", "index.html")
	if !exists(index) {
		t.Fatalf("no index.html written")
	}
	if !strings.Contains(readText(t, index), "<!DOCTYPE html>") {
		t.Errorf("index.html is not a document")
	}
}

func TestBuildShowsSEOWarnings(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	result := run(t, dir, "build", "--no-auto-commit")
	if !strings.Contains(result.Stdout, "Built") {
		t.Errorf("no build summary printed:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "warning:") {
		t.Errorf("no compiler-style lint line printed:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "SEO") {
		t.Errorf("no SEO lint printed:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "SEO warning(s) found") {
		t.Errorf("no warning count printed:\n%s", result.Stdout)
	}
}

func TestBuildExitsOneOnErrors(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	// Removing the description from the frontmatter triggers SEO006, whose
	// severity is error.
	writeText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"), "# Test\n\nContent.\n")

	result := run(t, dir, "build", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "error:") {
		t.Errorf("no error-severity line printed:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "SEO006") {
		t.Errorf("SEO006 not reported:\n%s", result.Stdout)
	}
}

func TestBuildWithoutInitFails(t *testing.T) {
	isolate(t)
	dir := testproject.Dir(t)
	result := run(t, dir, "build", "--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatalf("build succeeded with no selfdoc.json:\n%s", result.Stdout)
	}
}

func TestCheckFindsDirectives(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	result := run(t, dir, "check", "--no-auto-commit")
	for _, want := range []string{"OK", "ref", "directive(s)"} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("report does not mention %q:\n%s", want, result.Stdout)
		}
	}
}

func TestCheckAlwaysRunsSEOLints(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("exit code is %d, want 0\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "SEO") {
		t.Errorf("no SEO lint reported:\n%s", result.Stdout)
	}
}

func TestCheckRejectsAnUnregisteredIgnoreCode(t *testing.T) {
	isolate(t)
	dir := initialized(t)

	result := run(t, dir, "check", "--ignore", "SEO0O8", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "SEO0O8") {
		t.Errorf("the refusal does not name the typo:\n%s", result.Stderr)
	}
}

func TestCheckAcceptsARegisteredIgnoreCode(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	result := run(t, dir, "check", "--ignore", "SEO009", "--no-auto-commit")
	if strings.Contains(result.Stdout, "SEO009") {
		t.Errorf("the suppressed rule still reported:\n%s", result.Stdout)
	}
}

func TestCheckExitsOneOnErrors(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	writeText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"), "# Test\n\nContent.\n")

	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "SEO006") {
		t.Errorf("SEO006 not reported:\n%s", result.Stdout)
	}
}

func TestCheckExitsOneOnABrokenValidatedExample(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	python := requirePython3(t)
	dir := initialized(t)

	configPath := filepath.Join(dir, "selfdoc.json")
	config := readJSON(t, configPath)
	config["examples"] = map[string]any{"python": python + " {file}"}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encoding the config: %v", err)
	}
	writeText(t, configPath, string(data))

	page := func(name, snippet, summary string) {
		writeText(t, filepath.Join(dir, ".stricttools", "docs", name),
			"+++\ntitle = \""+name+"\"\ndescription = \""+summary+"\"\n+++\n\n"+
				"# "+name+"\n\n```python validate\n"+snippet+"```\n")
	}
	page("good.md",
		"def greet(name):\n    return 'Hello, ' + name\n\nprint(greet('world'))\n",
		"A page whose executable example runs to completion without any error")
	page("broken.md",
		"def greet(name):\n    return 'Hello, ' + name\n\ngreet()\n",
		"A page whose executable example parses fine but raises when it runs")

	result := run(t, dir, "check", "--json", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}

	payload := payloadOf(t, result)
	if payload["exit_code"] != float64(1) {
		t.Errorf("payload exit_code is %v", payload["exit_code"])
	}

	var errors []map[string]any
	for _, raw := range payload["lints"].([]any) {
		lint := raw.(map[string]any)
		if lint["severity"] == "error" {
			errors = append(errors, lint)
		}
	}
	if len(errors) != 1 {
		t.Fatalf("expected exactly one error-severity lint, got %d: %v", len(errors), errors)
	}
	if errors[0]["code"] != "EXAMPLE002" {
		t.Errorf("the error is %v, want EXAMPLE002", errors[0]["code"])
	}
	if errors[0]["file"] != "broken.md" {
		t.Errorf("the error names %v, want broken.md", errors[0]["file"])
	}
	if !strings.Contains(errors[0]["message"].(string), "TypeError") {
		t.Errorf("the message does not carry the runtime failure: %v", errors[0]["message"])
	}
}

// testMkdir creates a directory and its parents.
func testMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}
