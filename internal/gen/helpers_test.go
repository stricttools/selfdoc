package gen

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/staleness"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/testisolation/go/hygiene"

	// The language packages register their extractors, which is what links
	// a language into a binary. A build links them through internal/cli; the
	// suite does it here.
	_ "github.com/stricttools/selfdoc/internal/extractors/golang"
	_ "github.com/stricttools/selfdoc/internal/extractors/python"
	_ "github.com/stricttools/selfdoc/internal/extractors/typescript"
)

// genDir is where gen writes its pages: the generated root, which the build
// merges with the handwritten one.
func genDir(dir string) string { return layout.Path(dir, layout.GeneratedPagesRel) }

// handDir is a project's handwritten docs root.
func handDir(dir string) string { return layout.Path(dir, layout.DocsRel) }

// owners writes the ownership manifests gen needs before it may write into any
// directory under the tool-state directory.
func owners(t *testing.T, dir string) {
	t.Helper()
	for _, claimed := range layout.Declared() {
		write(t, layout.DirectoryManifestPath(dir, claimed.Name),
			layout.DirectoryManifestContent(layout.Owner))
	}
}

func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// requirePython3 skips the test when no interpreter is on PATH. Python's
// module docstrings are read by an embedded driver run under python3, so a
// machine without one cannot seed a Python page's description.
func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH: Python docstrings are read through it")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// writeConfig writes a project's selfdoc.json and returns the document read
// back off disk, which is what a command's config load hands the engine.
func writeConfig(t *testing.T, dir string, document map[string]any) map[string]any {
	t.Helper()
	serialized, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	write(t, filepath.Join(dir, "selfdoc.json"), string(serialized))
	return loadConfig(t, dir)
}

// loadConfig reads a project's selfdoc.json in the value shapes the validator
// and every engine function expect, with no defaults applied -- the same raw
// document the Python tests handed generate_docs.
func loadConfig(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "selfdoc.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	document, err := config.DecodeDocument(data)
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	object, ok := document.(map[string]any)
	if !ok {
		t.Fatalf("config is %T, want an object", document)
	}
	return object
}

// generate runs GenerateDocs against a project, failing the test on an error.
func generate(t *testing.T, cfg map[string]any, dir string) GenResult {
	t.Helper()
	result, err := GenerateDocs(cfg, dir, "", effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateDocs: %v", err)
	}
	return result
}

// pythonProject is a minimal Python project: three modules and a docs
// directory.
func pythonProject(t *testing.T) (string, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	cfg := writeConfig(t, dir, map[string]any{
		"source":        []any{map[string]any{"path": "mylib/", "language": "python"}},
		"docs":          ".stricttools/docs/",
		"output":        ".stricttools/docs-cache/build/",
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
	})
	write(t, filepath.Join(dir, "mylib", "__init__.py"), `"""My library."""`+"\n")
	write(t, filepath.Join(dir, "mylib", "core.py"), `"""Core module."""`+"\ndef main(): pass\n")
	write(t, filepath.Join(dir, "mylib", "utils.py"), `"""Utilities."""`+"\ndef helper(): pass\n")
	owners(t, dir)
	mkdir(t, filepath.Join(dir, ".stricttools", "docs"))
	return dir, cfg
}

// goProject is a Go project with a root package, two sub-packages (one of
// them multi-file with a doc.go and a test file), and a command package.
func goProject(t *testing.T) (string, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module github.com/user/mygoapp\n\ngo 1.21\n")
	cfg := writeConfig(t, dir, map[string]any{
		"source":        []any{map[string]any{"path": ".", "language": "go"}},
		"docs":          ".stricttools/docs/",
		"output":        ".stricttools/docs-cache/build/",
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
	})
	write(t, filepath.Join(dir, "main.go"),
		"// mygoapp is the entry point for the application.\npackage main\n\nfunc main() {}\n")
	write(t, filepath.Join(dir, "utils.go"), "package main\n\nfunc Helper() {}\n")
	write(t, filepath.Join(dir, "internal", "models", "models.go"),
		"// Package models defines data structures.\npackage models\n\ntype User struct {\n\tName string\n}\n")
	write(t, filepath.Join(dir, "internal", "commit", "doc.go"),
		"// Package commit handles git commit operations.\npackage commit\n")
	write(t, filepath.Join(dir, "internal", "commit", "commit.go"),
		"package commit\n\nfunc Create() error { return nil }\n")
	write(t, filepath.Join(dir, "internal", "commit", "types.go"),
		"package commit\n\ntype Commit struct {\n\tHash string\n}\n")
	write(t, filepath.Join(dir, "internal", "commit", "commit_test.go"),
		"package commit\n\nimport \"testing\"\n\nfunc TestCreate(t *testing.T) {}\n")
	write(t, filepath.Join(dir, "cmd", "myapp", "main.go"), "package main\n\nfunc main() {}\n")
	owners(t, dir)
	mkdir(t, filepath.Join(dir, ".stricttools", "docs"))
	return dir, cfg
}

// multiLanguageProject declares both a Python and a Go source path.
func multiLanguageProject(t *testing.T) (string, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	cfg := writeConfig(t, dir, map[string]any{
		"source": []any{
			map[string]any{"path": "pylib/", "language": "python"},
			map[string]any{"path": "golib/", "language": "go"},
		},
		"docs":          ".stricttools/docs/",
		"output":        ".stricttools/docs-cache/build/",
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
	})
	write(t, filepath.Join(dir, "pylib", "__init__.py"), `"""Python library."""`+"\n")
	write(t, filepath.Join(dir, "pylib", "core.py"), `"""Core Python module."""`+"\ndef main(): pass\n")
	write(t, filepath.Join(dir, "golib", "handler.go"),
		"// Package golib provides handlers.\npackage golib\n\nfunc Handle() {}\n")
	write(t, filepath.Join(dir, "go.mod"), "module github.com/user/multiproj\n\ngo 1.21\n")
	owners(t, dir)
	mkdir(t, filepath.Join(dir, ".stricttools", "docs"))
	return dir, cfg
}

// multiGoSourceProject declares two Go source paths, each with a root
// package, which is the shape that used to make one of them disappear.
func multiGoSourceProject(t *testing.T) (string, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	cfg := writeConfig(t, dir, map[string]any{
		"source": []any{
			map[string]any{"path": "router/", "language": "go"},
			map[string]any{"path": "sdk/", "language": "go"},
		},
		"docs":          ".stricttools/docs/",
		"output":        ".stricttools/docs-cache/build/",
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
	})
	write(t, filepath.Join(dir, "go.mod"), "module github.com/user/myproject\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "router", "router.go"),
		"// Package router provides HTTP routing.\npackage router\n\nfunc Route() {}\n")
	write(t, filepath.Join(dir, "sdk", "client.go"),
		"// Package sdk provides the client SDK.\npackage sdk\n\nfunc NewClient() {}\n")
	owners(t, dir)
	mkdir(t, filepath.Join(dir, ".stricttools", "docs"))
	return dir, cfg
}

// pageDescription is a page's frontmatter description.
func pageDescription(t *testing.T, path string) string {
	t.Helper()
	block, err := util.ReadFrontmatter(read(t, path), path, util.KindPage)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	value, _ := block.Values["description"].(string)
	return value
}

// rewriteDescription replaces a generated page's description, optionally
// leaving the `seeded = true` marker behind -- which is the stale-marker trap a
// hand edit used to fall into.
func rewriteDescription(t *testing.T, path, description string, keepSeeded bool) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	var out []string
	for _, line := range strings.Split(read(t, path), "\n") {
		switch {
		case strings.HasPrefix(line, "description = \""):
			out = append(out, `description = "`+description+`"`)
		case strings.TrimSpace(line) == "seeded = true" && !keepSeeded:
		default:
			out = append(out, line)
		}
	}
	write(t, path, strings.Join(out, "\n"))
}

// writeIndexPage writes a gen-index.md carrying the given description, which
// is how the legacy-residue cases are set up.
func writeIndexPage(t *testing.T, path, description string, seeded bool) {
	t.Helper()
	seededLine := ""
	if seeded {
		seededLine = "seeded = true\n"
	}
	write(t, path, "+++\n"+
		"title = \"API Reference\"\n"+
		`description = "`+description+`"`+"\n"+
		"generated = true\n"+
		seededLine+
		"nav_group = \"API Reference\"\n"+
		"nav_order = 90\n"+
		"+++\n"+
		"<!-- generated by selfdoc gen, do not edit -->\n"+
		"\n"+
		"# API Reference\n")
}

// loadStore reads a project's hash store.
func loadStore(t *testing.T, dir string) staleness.Store {
	t.Helper()
	store, err := staleness.LoadHashes(dir)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}
	return store
}

// names is a set of the given strings, for membership assertions.
func names(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	return set
}

// mdFilesIn lists the Markdown files in a directory.
func mdFilesIn(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	found := map[string]bool{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") {
			found[entry.Name()] = true
		}
	}
	return found
}

// copyTree copies a directory tree, which is how a committed fixture project
// becomes a writable project for one test.
func copyTree(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", source, err)
	}
}
