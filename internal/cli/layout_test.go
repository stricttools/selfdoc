package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// `selfdoc layout dump` publishes selfdoc's claim on a repository's tool-state
// directory, and `selfdoc layout validate` holds one repository to it.

func TestLayoutDumpCarriesEveryClaimedDirectory(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "--json", "layout", "dump")
	if result.ExitCode != 0 {
		t.Fatalf("layout dump exited %d: %s", result.ExitCode, result.Stderr)
	}
	payload := payloadOf(t, result)
	if payload["tool"] != layout.Owner || payload["root"] != layout.Root {
		t.Errorf("the dump names %v at %v", payload["tool"], payload["root"])
	}
	if payload["manifest_file"] != layout.ManifestFileName {
		t.Errorf("manifest_file = %v", payload["manifest_file"])
	}

	directories, ok := payload["directories"].([]any)
	if !ok || len(directories) != len(layout.Declared()) {
		t.Fatalf("directories = %#v, want one entry per claimed directory", payload["directories"])
	}
	seen := map[string]map[string]any{}
	for _, raw := range directories {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("a directory entry is %T, want an object", raw)
		}
		name, _ := entry["name"].(string)
		seen[name] = entry
	}
	for _, declared := range layout.Declared() {
		entry, present := seen[declared.Name]
		if !present {
			t.Errorf("the dump omits %s", declared.Name)
			continue
		}
		if entry["side"] != string(declared.Side) {
			t.Errorf("%s side = %v, want %v", declared.Name, entry["side"], declared.Side)
		}
		if entry["commitment"] != string(declared.Commitment) {
			t.Errorf("%s commitment = %v, want %v",
				declared.Name, entry["commitment"], declared.Commitment)
		}
		if entry["path"] != layout.Root+"/"+declared.Name {
			t.Errorf("%s path = %v", declared.Name, entry["path"])
		}
		if description, _ := entry["description"].(string); description == "" {
			t.Errorf("%s carries no description", declared.Name)
		}
		if entry["manifest_path"] != layout.DirectoryManifestRel(declared.Name) {
			t.Errorf("%s manifest_path = %v", declared.Name, entry["manifest_path"])
		}
		if entry["manifest_content"] != layout.DirectoryManifestContent(layout.Owner) {
			t.Errorf("%s manifest_content = %q", declared.Name, entry["manifest_content"])
		}
		names, ok := entry["deprecated_names"].([]any)
		if !ok {
			t.Errorf("%s deprecated_names = %#v, want a list", declared.Name, entry["deprecated_names"])
			continue
		}
		if len(names) != len(declared.DeprecatedNames) {
			t.Errorf("%s deprecated_names = %v, want %v",
				declared.Name, names, declared.DeprecatedNames)
		}
	}
}

// The human rendering is the same document, so a reader of the printed output
// reads the published contract.
func TestLayoutDumpPrintsTheDeclarationAsJSON(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "layout", "dump")
	if result.ExitCode != 0 {
		t.Fatalf("layout dump exited %d: %s", result.ExitCode, result.Stderr)
	}
	for _, want := range []string{
		`"tool": "` + layout.Owner + `"`,
		`"name": "` + layout.DocsStateName + `"`,
		`"manifest_file": "` + layout.ManifestFileName + `"`,
		`"commitment": "uncommitted"`,
		layout.DeprecatedRoot + "/",
	} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("the printed declaration lacks %q:\n%s", want, result.Stdout)
		}
	}
}

func TestLayoutValidateRefusesARepositoryWithNoLayout(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "layout", "validate")
	if result.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", result.ExitCode)
	}
	for _, want := range []string{
		layout.Root,
		layout.ManifestFileName,
		strings.TrimRight(layout.DirectoryManifestContent(layout.Owner), "\n"),
	} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, result.Stderr)
		}
	}
}

func TestLayoutValidatePassesOnABuiltProject(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	if result := run(t, dir, "build", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("build exited %d: %s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	result := run(t, dir, "--json", "layout", "validate")
	if result.ExitCode != 0 {
		t.Fatalf("layout validate exited %d: %s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	payload := payloadOf(t, result)
	if payload["ok"] != true {
		t.Errorf("payload = %#v, want a clean layout", payload)
	}
}

// A stale derived ignore file is refused, and the remedy the refusal names --
// running the build -- clears it.
func TestLayoutValidateRefusesAStaleIgnoreFileAndTheBuildClearsIt(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	if result := run(t, dir, "build", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("build exited %d: %s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	writeText(t, filepath.Join(dir, layout.Root, layout.IgnoreFileName), "# BEGIN othertool\nx/\n# END othertool\n")

	stale := run(t, dir, "layout", "validate")
	if stale.ExitCode != 1 {
		t.Fatalf("a stale ignore file validated: %s", stale.Stdout)
	}
	if !strings.Contains(stale.Stderr, "selfdoc build") {
		t.Errorf("the refusal does not name the remedy:\n%s", stale.Stderr)
	}

	if result := run(t, dir, "build", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("the remedy failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if fixed := run(t, dir, "layout", "validate"); fixed.ExitCode != 0 {
		t.Errorf("the remedy did not clear the refusal: %s\n%s", fixed.Stdout, fixed.Stderr)
	}
}

// A repository that has not been moved is refused by every command that reads
// project state, with the move script named.
func TestEveryProjectCommandRefusesTheOldLayout(t *testing.T) {
	isolate(t)
	dir := testproject.Dir(t)
	testproject.WriteJSON(t, filepath.Join(dir, "selfdoc.json"), testproject.DefaultConfig(map[string]any{
		"docs":   "docs/",
		"output": "docs/_build/",
	}))
	writeText(t, filepath.Join(dir, "docs", "index.md"), "# Home\n")

	for _, argv := range [][]string{
		{"build", "--no-auto-commit"},
		{"check"},
		{"gen", "--no-auto-commit"},
		{"quality"},
	} {
		result := run(t, dir, argv...)
		if result.ExitCode == 0 {
			t.Errorf("%v accepted a repository on the old layout", argv)
			continue
		}
		if !strings.Contains(result.Stderr, layout.MoveScript) {
			t.Errorf("%v does not name the move script:\n%s", argv, result.Stderr)
		}
		if !strings.Contains(result.Stderr, layout.Root) {
			t.Errorf("%v does not name the new layout:\n%s", argv, result.Stderr)
		}
	}
}
