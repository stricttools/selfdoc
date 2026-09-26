package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/layout"
)

// `selfdoc init` in a repository that has never met selfdoc.
//
// Init is where a repository grants selfdoc its directories, so it writes the
// ownership manifests of the directories a new project needs -- docs, the
// generated state, the cache and the vocabulary -- rather than refusing for
// want of the first one and leaving the others for a later command to refuse
// over.

// initManifested are the directories init grants selfdoc.
var initManifested = []string{layout.DocsName, layout.DocsStateName, layout.DocsCacheName, layout.VocabularyName}

func TestInitWritesTheOwnershipManifestsOfAFreshRepository(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, "README.md"), "# fresh\n")

	if code, stdout, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init refused a fresh repository:\n%s\n%s", stdout, stderr)
	}
	for _, name := range initManifested {
		manifest, err := layout.ReadDirectoryManifest(dir, name)
		if err != nil {
			t.Fatalf("no readable manifest for %s: %v", name, err)
		}
		if manifest.Owner != layout.Owner {
			t.Errorf("%s names %q as its owner", name, manifest.Owner)
		}
	}
	if !exists(filepath.Join(dir, "stricttools", layout.IgnoreFileName)) {
		t.Error("init wrote no derived ignore file")
	}

	// The project init produced is one the next command accepts as it stands.
	if result := run(t, dir, "gen", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen refused the initialized project:\n%s\n%s", result.Stdout, result.Stderr)
	}
}

func TestInitKeepsAManifestThatAlreadyNamesSelfdoc(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	path := layout.DirectoryManifestPath(dir, layout.DocsName)
	writeText(t, path, layout.DirectoryManifestContent(layout.Owner))

	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init refused: %s", stderr)
	}
	if got := readText(t, path); got != layout.DirectoryManifestContent(layout.Owner) {
		t.Errorf("the existing manifest was rewritten: %q", got)
	}
}

func TestInitRefusesADirectoryAnotherToolOwns(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	writeText(t, layout.DirectoryManifestPath(dir, layout.DocsStateName),
		layout.DirectoryManifestContent("othertool"))

	code, _, stderr := initProject(t, dir)
	if code == 0 {
		t.Fatal("init took over a directory another tool owns")
	}
	if !strings.Contains(stderr, "othertool") {
		t.Errorf("the refusal does not name the owner: %s", stderr)
	}
	if exists(filepath.Join(dir, "selfdoc.json")) {
		t.Error("a refused init wrote a config")
	}
}

// Init writes the versioned form: `version` and `versions` at the project's
// current version, and 0.0.0 for a project that states none yet. It never
// writes "unversioned": true, which gen refuses once the project has source.

func TestInitDeclaresTheManifestVersion(t *testing.T) {
	isolate(t)
	dir := testprojectWithPyproject(t, "2.3.4")
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}
	assertVersionedAt(t, dir, "2.3.4")
}

func TestInitDeclaresZeroForAProjectStatingNoVersion(t *testing.T) {
	isolate(t)
	dir := testprojectWithPyproject(t, "")
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init refused a project that states no version yet: %s", stderr)
	}
	assertVersionedAt(t, dir, "0.0.0")
}

func TestInitDeclaresZeroForACodelessProject(t *testing.T) {
	isolate(t)
	dir := codelessProject(t)
	if code, _, stderr := initProject(t, dir); code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}
	assertVersionedAt(t, dir, "0.0.0")

	// Source added later: gen accepts the project as init left it.
	writeText(t, filepath.Join(dir, "pkg", "__init__.py"), "\"\"\"A package.\"\"\"\n")
	if result := run(t, dir, "gen", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen refused:\n%s\n%s", result.Stdout, result.Stderr)
	}
}

func testprojectWithPyproject(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	body := "[project]\nname = \"testproj\"\n"
	if version != "" {
		body += "version = \"" + version + "\"\n"
	}
	writeText(t, filepath.Join(dir, "pyproject.toml"), body)
	writeText(t, filepath.Join(dir, "testproj", "__init__.py"), "\"\"\"Test package.\"\"\"\n")
	return dir
}

func assertVersionedAt(t *testing.T, dir, want string) {
	t.Helper()
	config := readJSON(t, filepath.Join(dir, "selfdoc.json"))
	if _, declared := config["unversioned"]; declared {
		t.Errorf("init wrote unversioned: %v", config["unversioned"])
	}
	if config["version"] != want {
		t.Errorf("version is %v, want %s", config["version"], want)
	}
	versions, _ := config["versions"].([]any)
	if len(versions) != 1 || versions[0].(map[string]any)["version"] != want {
		t.Errorf("versions is %v, want [{version: %s}]", config["versions"], want)
	}
}
