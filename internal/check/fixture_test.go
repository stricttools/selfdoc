package check

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/testisolation/go/hygiene"

	// Every language the check's fixtures declare has to be linked in, or
	// resolution answers with a stub and the coverage tests measure
	// nothing.
	_ "github.com/stricttools/selfdoc/internal/extractors/golang"
	_ "github.com/stricttools/selfdoc/internal/extractors/python"
	_ "github.com/stricttools/selfdoc/internal/extractors/typescript"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// isolate binds the test-environment isolation floor: a throwaway HOME (so
// nothing outside the fixture can change a verdict), an isolated git identity,
// and no ambient credentials.
func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// handle is the effects handle every engine call in these tests runs under:
// unbound, so every write and every subprocess executes directly.
func handle() *effects.Handle { return effects.Unbound() }

// write writes content to path, creating its directory.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeConfig writes a selfdoc.json carrying the given document, and the
// ownership declaration that lets selfdoc create its own directories in the
// project -- a project is a repository, and the rows are the repository's
// grant.
func writeConfig(t *testing.T, dir string, document map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	write(t, filepath.Join(dir, "selfdoc.json"), string(encoded))
	// The two generated directories are the ones a check writes into, so
	// they are the ones a fixture grants. The handwritten directories --
	// docs, posts, vocabulary -- are granted by the test that wants one,
	// because a manifest is what makes the directory exist and several
	// cases here are about a project that has none.
	for _, granted := range []string{layout.DocsStateName, layout.DocsCacheName} {
		write(t, layout.DirectoryManifestPath(dir, granted),
			layout.DirectoryManifestContent(layout.Owner))
	}
}

// pythonProjectConfig is the config document the Python fixture writes.
func pythonProjectConfig() map[string]any {
	return configForSource(map[string]any{"path": "mylib/", "language": "python"})
}

// configForSource is the minimal valid config document for a project whose
// sources are the given entries. Every field selfdoc requires is present, so a
// fixture declares only what its own case is about.
func configForSource(entries ...map[string]any) map[string]any {
	source := make([]any, 0, len(entries))
	for _, entry := range entries {
		source = append(source, entry)
	}
	return map[string]any{
		"version":       "1.0.0",
		"source":        source,
		"docs":          "stricttools/docs/",
		"output":        "stricttools/.docs-cache/build/",
		"base_url":      "https://example.com",
		"author":        map[string]any{"name": "Test Author", "url": "https://author.example"},
		"search_engine": "pagefind",
	}
}

// pythonProject creates a minimal Python project with a selfdoc.json, source
// files carrying public symbols, and an empty docs directory. It returns the
// project root.
func pythonProject(t *testing.T) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, pythonProjectConfig())

	write(t, filepath.Join(root, "mylib", "__init__.py"),
		`"""My library."""

def greet(name):
    """Say hello."""
    return f'Hello, {name}'

def farewell(name):
    """Say goodbye."""
    return f'Goodbye, {name}'

class Widget:
    """A widget."""
    pass

def _private():
    pass
`)
	write(t, filepath.Join(root, "mylib", "utils.py"),
		`"""Utility functions."""

def helper():
    """Help."""
    pass
`)
	if err := os.MkdirAll(filepath.Join(root, "stricttools", "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	return root
}

// lintFixture is a minimal project for the lint rules: a docs directory and
// the config document the rules read, with no source files written.
type lintFixture struct {
	// Root is the project root.
	Root string
	// DocsDir is the project's docs directory.
	DocsDir string
	// Config is the config document the rules are run with.
	Config map[string]any
}

// lintProject creates the lint fixture.
func lintProject(t *testing.T) lintFixture {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	docsDir := filepath.Join(root, "stricttools", "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	return lintFixture{Root: root, DocsDir: docsDir, Config: pythonProjectConfig()}
}

// buildAllDocs builds the lint rules' page slice out of a docs directory,
// without resolving any directive: the rules read the raw body, so a fixture
// that only exercises them needs no resolver.
func buildAllDocs(t *testing.T, docsDir string) map[string]docs.Doc {
	t.Helper()
	allDocs := map[string]docs.Doc{}
	err := filepath.WalkDir(docsDir, func(walked string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") ||
			strings.HasPrefix(entry.Name(), "_") {
			return nil
		}
		raw, err := os.ReadFile(walked)
		if err != nil {
			return err
		}
		content := string(raw)
		relPath, err := filepath.Rel(docsDir, walked)
		if err != nil {
			return err
		}
		block, err := util.ReadFrontmatter(content, filepath.ToSlash(relPath), util.KindPage)
		if err != nil {
			return err
		}
		metadata, body := block.Values, block.Body
		allDocs[filepath.ToSlash(relPath)] = docs.Doc{
			Frontmatter:      metadata,
			Resolved:         "",
			Raw:              body,
			FrontmatterLines: len(strings.Split(content, "\n")) - len(strings.Split(body, "\n")),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", docsDir, err)
	}
	return allDocs
}

// runLintsOn runs the lint rules over a fixture's docs directory.
func runLintsOn(t *testing.T, fixture lintFixture, resolved []ResolvedDirective) []lints.LintResult {
	t.Helper()
	vocab, err := vocabulary.Load(fixture.Root)
	if err != nil {
		t.Fatalf("vocabulary.Load: %v", err)
	}
	results, err := runLints(
		buildAllDocs(t, fixture.DocsDir), fixture.Root, fixture.DocsDir,
		fixture.Config, resolved, vocab, handle(),
	)
	if err != nil {
		t.Fatalf("runLints: %v", err)
	}
	return results
}

// codes returns the lint codes of a diagnostic list, in order.
func codes(diagnostics []lints.LintResult) []string {
	found := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		found = append(found, diagnostic.Code())
	}
	return found
}

// withCode returns the diagnostics carrying code.
func withCode(diagnostics []lints.LintResult, code string) []lints.LintResult {
	var matching []lints.LintResult
	for _, diagnostic := range diagnostics {
		if diagnostic.Code() == code {
			matching = append(matching, diagnostic)
		}
	}
	return matching
}

// hasCode reports whether any diagnostic carries code.
func hasCode(diagnostics []lints.LintResult, code string) bool {
	return len(withCode(diagnostics, code)) > 0
}

// requirePython skips the test when python3 is not on this machine.
//
// Nothing this package checks needs an interpreter any more -- the Python
// extractor and the example-syntax rule both parse in-process. What still
// needs one is the pair of conformance tests that run CPython as the oracle
// they are measured against.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed, so the CPython conformance tests cannot run")
	}
}
