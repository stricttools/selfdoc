// Package testproject builds the fixture projects the engine's tests run
// against.
//
// Every suite that exercises a whole build needs a project on disk: a
// selfdoc.json the loader accepts, a source file, and a docs tree. The
// factories here are the Go form of the Python suite's fixture factories, so
// the build, the check, the blog and the command tests all start from the same
// shapes rather than each writing its own almost-identical project.
//
// # No dependency on testing
//
// The helpers take [TB], the slice of testing.TB they use, so importing this
// package does not pull the testing flag set into anything that links it.
package testproject

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stricttools/selfdoc/internal/layout"
)

// TB is the slice of testing.TB these helpers use.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
	Setenv(key, value string)
	TempDir() string
}

// DefaultPrefix is the mount prefix the default fixture config emits its
// current version at.
//
// Empty: one locale drops the locale segment and the current version carries
// no version segment, so the pages sit at the output root.
const DefaultPrefix = ""

// Author returns the author block every fixture project declares.
//
// The block is required in selfdoc.json and is what every page's structured
// data names, so a fixture without one is not a project the loader accepts.
func Author() map[string]any {
	return map[string]any{
		"name":    "Test Author",
		"url":     "https://author.example",
		"same_as": []any{"https://github.com/testauthor"},
	}
}

// DefaultConfig returns the minimal valid selfdoc config, with the required
// versions and locales arrays, and the given overrides applied over it.
func DefaultConfig(overrides map[string]any) map[string]any {
	config := map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"version":       "1.0.0",
		"versions":      []any{map[string]any{"version": "1.0.0"}},
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
		"search_engine": "pagefind",
		"author":        Author(),
	}
	for key, value := range overrides {
		config[key] = value
	}
	return config
}

// Make creates a minimal selfdoc project under a fresh directory and returns
// its path.
//
// The project carries a selfdoc.json built from DefaultConfig plus overrides,
// one Python source file, and one docs page.
func Make(t TB, overrides map[string]any) string {
	t.Helper()
	projectDir := filepath.Join(t.TempDir(), "project")
	MkdirAll(t, projectDir)

	WriteJSON(t, filepath.Join(projectDir, "selfdoc.json"), DefaultConfig(overrides))
	Manifests(t, projectDir)
	WriteText(t, filepath.Join(projectDir, "src", "__init__.py"), `"""Example package."""`+"\n")
	WriteText(t, filepath.Join(DocsDir(projectDir), "index.md"),
		"# Test Project\n\nWelcome to the docs.\n")
	return projectDir
}

// MakeVersioned creates a selfdoc project with one git tag per version and
// returns its path.
//
// Each version's tag stands at a commit whose docs/index.md names that
// version, so a multi-version build really does read different content out of
// each tag.
func MakeVersioned(t TB, versions []string, overrides map[string]any) string {
	t.Helper()
	if len(versions) == 0 {
		t.Fatalf("MakeVersioned needs at least one version")
	}
	projectDir := filepath.Join(t.TempDir(), "versioned")
	MkdirAll(t, projectDir)

	versionEntries := make([]any, 0, len(versions))
	for _, version := range versions {
		versionEntries = append(versionEntries, map[string]any{"version": version})
	}
	config := map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
		"author":        Author(),
		"version":       versions[len(versions)-1],
		"versions":      versionEntries,
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
	}
	for key, value := range overrides {
		config[key] = value
	}
	WriteJSON(t, filepath.Join(projectDir, "selfdoc.json"), config)
	Manifests(t, projectDir)
	WriteText(t, filepath.Join(projectDir, "src", "__init__.py"), `"""Example package."""`+"\n")
	WriteText(t, filepath.Join(DocsDir(projectDir), "index.md"),
		"# Test Project\n\nInitial content.\n")

	Git(t, projectDir, "init")
	Git(t, projectDir, "add", ".")
	Git(t, projectDir, "commit", "-m", "initial")

	for _, version := range versions {
		WriteText(t, filepath.Join(DocsDir(projectDir), "index.md"),
			fmt.Sprintf("# Test Project\n\nDocumentation for version %s.\n", version))
		Git(t, projectDir, "add", layout.DocsRel+"/index.md")
		Git(t, projectDir, "commit", "-m", "docs for "+version)
		Git(t, projectDir, "tag", "v"+version)
	}
	return projectDir
}

// MakeLocalized creates a selfdoc project with one docs directory per locale
// and returns its path.
//
// Each locale entry is a config locale object: "code" and "label" are
// required, "default" and "rtl" optional.
func MakeLocalized(t TB, locales []map[string]any, overrides map[string]any) string {
	t.Helper()
	projectDir := filepath.Join(t.TempDir(), "localized")
	MkdirAll(t, projectDir)

	localeEntries := make([]any, 0, len(locales))
	for _, locale := range locales {
		localeEntries = append(localeEntries, locale)
	}
	config := map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
		"author":        Author(),
		"version":       "1.0.0",
		"locales":       localeEntries,
		"versions":      []any{map[string]any{"version": "1.0.0"}},
	}
	for key, value := range overrides {
		config[key] = value
	}
	WriteJSON(t, filepath.Join(projectDir, "selfdoc.json"), config)
	Manifests(t, projectDir)
	WriteText(t, filepath.Join(projectDir, "src", "__init__.py"), `"""Example package."""`+"\n")

	for _, locale := range locales {
		code, _ := locale["code"].(string)
		label, _ := locale["label"].(string)
		WriteText(t, filepath.Join(DocsDir(projectDir), code, "index.md"),
			fmt.Sprintf("# Test Project (%s)\n\nWelcome — %s.\n", label, label))
	}
	return projectDir
}

// Manifests writes the ownership manifest every directory selfdoc claims needs
// before selfdoc may write into it.
//
// selfdoc never writes a manifest: the file is the permission, and granting it
// is the repository's own act. A fixture project is a repository, so it grants
// every one of them here.
func Manifests(t TB, projectDir string) {
	t.Helper()
	for _, claimed := range layout.Declared() {
		WriteText(t, layout.DirectoryManifestPath(projectDir, claimed.Name),
			layout.DirectoryManifestContent(layout.Owner))
	}
}

// Dir is a fresh project directory with nothing in it but the ownership
// manifests selfdoc needs before it may write into any of its own directories.
//
// It is what a test that exercises one piece of the engine starts from: a
// repository that has granted selfdoc its manifests, and no pages, config or
// source beyond what the test writes itself.
func Dir(t TB) string {
	t.Helper()
	dir := t.TempDir()
	Manifests(t, dir)
	return dir
}

// DocsDir is a fixture project's handwritten docs directory.
func DocsDir(projectDir string) string {
	return layout.Path(projectDir, layout.DocsRel)
}

// GeneratedPagesDir is a fixture project's generated pages directory: the
// second docs root, which the build merges with the handwritten one.
func GeneratedPagesDir(projectDir string) string {
	return layout.Path(projectDir, layout.GeneratedPagesRel)
}

// WriteText writes text to path, creating the parent directories.
func WriteText(t TB, path, text string) {
	t.Helper()
	MkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// WriteJSON writes value to path as indented JSON, creating the parent
// directories.
func WriteJSON(t TB, path string, value any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encoding %s: %v", path, err)
	}
	WriteText(t, path, string(encoded)+"\n")
}

// ReadText reads a file and returns its contents, failing the test when it
// cannot be read.
func ReadText(t TB, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// MkdirAll creates a directory and its parents, failing the test when it
// cannot.
func MkdirAll(t TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
}

// Git runs a git command in dir, failing the test when it exits non-zero.
//
// No identity is injected: the isolation floor owns the git identity and the
// throwaway global config for the whole test, so a second source could only
// drift from it.
func Git(t TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}

// pagefindOnce memoizes the search for a working Pagefind installation, which
// costs a subprocess per candidate.
var (
	pagefindOnce    sync.Once
	pagefindCommand []string
)

// startupPath is PATH as the test binary was started with, captured before any
// test narrows it.
//
// A suite that pins PATH to a fixed set of system directories -- so a fake
// tool at the front shadows the real one and nothing else is reachable --
// hides a Pagefind installed under the developer's own bin directory. The
// search runs against this value instead of the live one, so where a test
// points PATH decides nothing about whether Pagefind is found.
var startupPath = os.Getenv("PATH")

// RequirePagefind skips the test unless a Pagefind installation the build can
// reach is available, and makes it reachable when it is installed somewhere
// the build would not look.
//
// The build runs "python3 -m pagefind" and then "pagefind". Whatever answered
// the search is reached through a "pagefind" shim -- a one-line script naming
// the absolute command -- written into a directory prepended to PATH, which is
// the second thing the build tries, so nothing about the build changes. The
// shim is what makes a Pagefind outside the system directories reachable from
// a test that has narrowed PATH to shadow an external tool.
//
// Every candidate is probed with HOME and the XDG base directories pointed at
// a directory that does not exist, because the isolation floor repoints them
// before the build runs. A Pagefind installed into the user site directory
// answers "python3 -m pagefind" in the developer's own shell and then
// disappears once HOME moves, so a probe run under the real HOME would report
// a Pagefind the build cannot reach and the test would fail instead of
// skipping.
//
// It must be called before the test calls T.Parallel, because it sets an
// environment variable.
func RequirePagefind(t TB) {
	t.Helper()
	pagefindOnce.Do(func() { pagefindCommand = findPagefind() })
	if pagefindCommand == nil {
		t.Skipf("pagefind is not installed: the build indexes its output with " +
			"'python3 -m pagefind' or 'pagefind', and neither answered. " +
			"Install with 'pip install pagefind[bin]' or 'npm install -g pagefind'.")
		return
	}
	shimDir := filepath.Join(t.TempDir(), "pagefind-shim")
	MkdirAll(t, shimDir)
	shim := filepath.Join(shimDir, "pagefind")
	script := "#!/bin/sh\nexec " + strings.Join(pagefindCommand, " ") + ` "$@"` + "\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the pagefind shim: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// findPagefind returns an argv prefix that runs Pagefind under the isolation
// floor's environment, or nil when none does.
//
// Every candidate's program is an absolute path, resolved against
// [startupPath] rather than against whatever PATH the calling test has set, so
// the answer is the same whether the search runs before or after a suite
// narrows PATH.
func findPagefind() []string {
	var candidates [][]string
	if interpreter := lookInStartupPath("python3"); interpreter != "" {
		candidates = append(candidates, []string{interpreter, "-m", "pagefind"})
	}
	if binary := lookInStartupPath("pagefind"); binary != "" {
		candidates = append(candidates, []string{binary})
	}
	for _, interpreter := range uvToolInterpreters() {
		candidates = append(candidates, []string{interpreter, "-m", "pagefind"})
	}
	for _, candidate := range candidates {
		cmd := exec.Command(candidate[0], append(candidate[1:], "--version")...)
		cmd.Env = homelessEnv()
		if err := cmd.Run(); err == nil {
			return candidate
		}
	}
	return nil
}

// lookInStartupPath returns the absolute path of an executable named name on
// [startupPath], or "" when no directory on it carries one.
func lookInStartupPath(name string) string {
	for _, dir := range filepath.SplitList(startupPath) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

// unreachableHome is a path no home directory occupies. It is what the
// Pagefind probe points HOME at, so that a user-site installation is invisible
// to it exactly as it is invisible to an isolated test's build.
const unreachableHome = "/nonexistent/selfdoc-pagefind-probe"

// homelessEnv is the current environment with HOME, USERPROFILE and the four
// XDG base directories pointed at [unreachableHome], which is what the
// isolation floor does to them before a test builds anything.
func homelessEnv() []string {
	moved := map[string]string{
		"HOME":            unreachableHome,
		"USERPROFILE":     unreachableHome,
		"XDG_CONFIG_HOME": filepath.Join(unreachableHome, ".config"),
		"XDG_DATA_HOME":   filepath.Join(unreachableHome, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(unreachableHome, ".cache"),
		"XDG_STATE_HOME":  filepath.Join(unreachableHome, ".local", "state"),
	}
	environment := make([]string, 0, len(os.Environ())+len(moved))
	for _, entry := range os.Environ() {
		if name, _, found := strings.Cut(entry, "="); found {
			if _, moving := moved[name]; moving {
				continue
			}
		}
		environment = append(environment, entry)
	}
	for name, value := range moved {
		environment = append(environment, name+"="+value)
	}
	return environment
}

// uvToolInterpreters lists the Python interpreters uv installed for its tools,
// which is where a pip-installed Pagefind ends up on a machine whose system
// interpreter does not carry it.
//
// The home directory is read from the password database rather than from HOME,
// because the isolation floor has already replaced HOME with a throwaway
// directory by the time a test calls this.
func uvToolInterpreters() []string {
	account, err := user.Current()
	if err != nil || account.HomeDir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(
		account.HomeDir, ".local", "share", "uv", "tools", "*", "bin", "python3"))
	if err != nil {
		return nil
	}
	return matches
}

// UnifiedProject is one constituent project a unified fixture carries: its
// directory name, which is also its mount slug, and the language its source
// entry declares. An empty language means Python.
type UnifiedProject struct {
	Name     string
	Language string
}

// MakeUnified creates a monorepo whose docs-site unifies several constituent
// projects, and returns the docs-site's path.
//
// The layout is the one a real rlsbl workspace has: every project, the
// docs-site included, is a directory under "monorepo/packages/", so each
// constituent is addressed from the docs-site as "../<name>" -- which is what
// the unified config's relative paths are resolved against. Each constituent
// carries its own selfdoc.json with its own base URL and one docs page; the
// docs-site carries the "unified" block naming them all, plus the versions and
// locales arrays the unified build's passes are driven by.
//
// overrides are applied over the docs-site's config, so a test can state a
// different base URL, extra versions or a per-version project pinning without
// rebuilding the whole fixture.
func MakeUnified(t TB, projects []UnifiedProject, overrides map[string]any) string {
	t.Helper()
	packagesDir := filepath.Join(t.TempDir(), "monorepo", "packages")
	MkdirAll(t, packagesDir)

	unifiedEntries := make([]any, 0, len(projects))
	for _, project := range projects {
		language := project.Language
		if language == "" {
			language = "python"
		}
		projectDir := filepath.Join(packagesDir, project.Name)
		WriteJSON(t, filepath.Join(projectDir, "selfdoc.json"), map[string]any{
			"source":        []any{map[string]any{"path": "src/", "language": language}},
			"base_url":      "https://example.com/" + project.Name,
			"search_engine": "pagefind",
			"author":        Author(),
			"version":       "1.0.0",
		})
		Manifests(t, projectDir)
		WriteText(t, filepath.Join(projectDir, "src", "__init__.py"),
			`"""`+project.Name+` package."""`+"\n")
		WriteText(t, filepath.Join(DocsDir(projectDir), "index.md"),
			fmt.Sprintf("# %s\n\nDocs for %s.\n", project.Name, project.Name))
		unifiedEntries = append(unifiedEntries, map[string]any{"path": "../" + project.Name})
	}

	docsSiteDir := filepath.Join(packagesDir, "docs-site")
	docsSiteConfig := map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"search_engine": "pagefind",
		"author":        Author(),
		"unified":       map[string]any{"projects": unifiedEntries},
		"version":       "1.0.0",
		"versions":      []any{map[string]any{"version": "1.0.0"}},
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
	}
	for key, value := range overrides {
		docsSiteConfig[key] = value
	}
	WriteJSON(t, filepath.Join(docsSiteDir, "selfdoc.json"), docsSiteConfig)
	Manifests(t, docsSiteDir)
	// The docs-site needs a source entry of its own: the config requires
	// one, and it is a project like any other.
	WriteText(t, filepath.Join(docsSiteDir, "src", "__init__.py"),
		`"""Docs-site placeholder."""`+"\n")
	WriteText(t, filepath.Join(DocsDir(docsSiteDir), "index.md"),
		"# Unified Docs\n\nLanding page for the monorepo.\n")
	return docsSiteDir
}
