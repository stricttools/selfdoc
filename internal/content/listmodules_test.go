package content

import (
	"path/filepath"
	"strings"
	"testing"
)

// pythonProject writes a small Python package and returns its base directory.
func pythonProject(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	write(t, filepath.Join(base, "mylib", "__init__.py"), `"""My library."""`+"\n")
	write(t, filepath.Join(base, "mylib", "core.py"),
		`"""Core module for the library. It does the work."""`+"\n")
	write(t, filepath.Join(base, "mylib", "config.py"),
		`"""Config loader for mylib.json."""`+"\n")
	return base
}

// pythonConfig declares one Python source entry at mylib/.
func pythonConfig() map[string]any {
	return map[string]any{"source": []any{
		map[string]any{"path": "mylib/", "language": "python"},
	}}
}

func TestListModulesPerFile(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := pythonProject(t)

	rendered, ok := resolve(t, "list-modules",
		map[string]string{"path": "mylib/"}, nil, base, pythonConfig())
	if !ok {
		t.Fatal("list-modules is a content directive")
	}
	wants(t, rendered,
		"- **mylib.config** (`mylib/config.py`): Config loader for mylib.json.",
		"- **mylib.core** (`mylib/core.py`): Core module for the library.",
	)
	// A package's __init__ is the package's own module, named for the
	// package rather than for the file.
	wants(t, rendered, "- **mylib** (`mylib/__init__.py`): My library.")
	// Modules render in name order.
	if strings.Index(rendered, "mylib.config") > strings.Index(rendered, "mylib.core") {
		t.Error("the modules must render in name order")
	}
}

func TestListModulesExcludesPythonTestFiles(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := pythonProject(t)
	write(t, filepath.Join(base, "mylib", "test_core.py"), `"""Tests for core."""`+"\n")
	write(t, filepath.Join(base, "mylib", "core_test.py"), `"""More tests."""`+"\n")

	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "mylib/"}, nil, base, pythonConfig())
	wants(t, rendered, "core.py")
	rejects(t, rendered, "test_core", "core_test")
}

func TestListModulesHonorsGenExclude(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := pythonProject(t)
	write(t, filepath.Join(base, "mylib", "testdata", "sample.py"),
		`"""A fixture."""`+"\n")

	config := pythonConfig()
	config["gen"] = map[string]any{"exclude": []any{"testdata"}}
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "mylib/"}, nil, base, config)
	wants(t, rendered, "core.py")
	rejects(t, rendered, "testdata")
}

func TestListModulesMissingPathAttribute(t *testing.T) {
	isolate(t)
	rendered, _ := resolve(t, "list-modules", nil, nil, pythonProject(t), pythonConfig())
	wants(t, rendered, "requires a path attribute")
}

func TestListModulesMissingDirectory(t *testing.T) {
	isolate(t)
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "nonexistent/"}, nil,
		pythonProject(t), pythonConfig())
	wants(t, rendered, "not found")
}

func TestListModulesWithoutConfig(t *testing.T) {
	isolate(t)
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "mylib/"}, nil, pythonProject(t), nil)
	wants(t, rendered, "requires project config")
}

// goProject writes several Go packages, each with a package comment and a test
// file that no listing shows.
func goProject(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	packages := map[string][2]string{
		"cmd/server":      {"server", "Package server provides the HTTP server."},
		"cmd/cli":         {"cli", "Package cli is the command-line interface."},
		"internal/config": {"config", "Package config handles configuration loading."},
		"internal/db":     {"db", "Package db provides database access."},
		"internal/auth":   {"auth", "Package auth handles authentication."},
		"pkg/api":         {"api", "Package api defines the public API types."},
		"pkg/middleware":  {"middleware", "Package middleware provides HTTP middleware."},
	}
	for path, declaration := range packages {
		name, doc := declaration[0], declaration[1]
		write(t, filepath.Join(base, path, "main.go"),
			"// "+doc+"\npackage "+name+"\n\nfunc init() {}\n")
		write(t, filepath.Join(base, path, "main_test.go"),
			"package "+name+"\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n")
	}
	return base
}

// goConfig declares the three Go source roots goProject writes.
func goConfig() map[string]any {
	return map[string]any{"source": []any{
		map[string]any{"path": "cmd/", "language": "go"},
		map[string]any{"path": "internal/", "language": "go"},
		map[string]any{"path": "pkg/", "language": "go"},
	}}
}

func TestListModulesGroupsGoByPackage(t *testing.T) {
	isolate(t)
	base := goProject(t)

	cases := []struct {
		path    string
		bullets int
		wants   []string
	}{
		{"cmd/", 2, []string{
			"- **cmd/cli**: Package cli is the command-line interface.",
			"- **cmd/server**: Package server provides the HTTP server.",
		}},
		{"internal/", 3, []string{
			"**internal/auth**", "**internal/config**", "**internal/db**",
		}},
		{"pkg/", 2, []string{"**pkg/api**", "**pkg/middleware**"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			rendered, _ := resolve(t, "list-modules",
				map[string]string{"path": testCase.path}, nil, base, goConfig())
			bullets := 0
			for _, line := range strings.Split(rendered, "\n") {
				if strings.HasPrefix(line, "- ") {
					bullets++
				}
			}
			if bullets != testCase.bullets {
				t.Errorf("got %d bullets, want %d:\n%s",
					bullets, testCase.bullets, rendered)
			}
			wants(t, rendered, testCase.wants...)
			// A test file is not a module of the project.
			rejects(t, rendered, "_test.go")
		})
	}
}

func TestListModulesGoExcludePrunesPackages(t *testing.T) {
	// The generated pages honor gen.exclude, so a listing that did not
	// honor the same patterns would advertise modules -- an analyzer's
	// fixture tree -- that the site has no page for.
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "internal", "engine", "engine.go"),
		"// Package engine renders things.\npackage engine\n")
	write(t, filepath.Join(base, "internal", "lint", "testdata", "src", "fix", "fix.go"),
		"// Package fix is an analyzer fixture.\npackage fix\n")

	config := map[string]any{
		"source": []any{map[string]any{"path": "internal/", "language": "go"}},
		"gen":    map[string]any{"exclude": []any{"testdata"}},
	}
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "internal/"}, nil, base, config)
	wants(t, rendered, "**internal/engine**")
	rejects(t, rendered, "testdata")
}

func TestListModulesSkipsGoToolchainIgnoredDirectories(t *testing.T) {
	// cmd/go never builds a directory named testdata or vendor, nor one
	// whose name begins with "." or "_", so no listing advertises one --
	// with no gen.exclude entry naming it.
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "internal", "engine", "engine.go"),
		"// Package engine renders things.\npackage engine\n")
	write(t, filepath.Join(base, "internal", "lint", "testdata", "fix", "fix.go"),
		"// Package fix is an analyzer fixture.\npackage fix\n")
	write(t, filepath.Join(base, "internal", "vendor", "dep", "dep.go"),
		"// Package dep is vendored.\npackage dep\n")
	write(t, filepath.Join(base, "internal", "_scratch", "scratch.go"),
		"// Package scratch is ignored.\npackage scratch\n")

	config := map[string]any{
		"source": []any{map[string]any{"path": "internal/", "language": "go"}},
	}
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "internal/"}, nil, base, config)
	wants(t, rendered, "**internal/engine**")
	rejects(t, rendered, "testdata", "vendor", "_scratch")
}

func TestListModulesPicksTheEntryMatchingThePath(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "cmd", "main.go"),
		"// Package main provides the entry point.\npackage main\n\nfunc main() {}\n")
	write(t, filepath.Join(base, "lib", "__init__.py"), `"""Library module."""`+"\n")
	write(t, filepath.Join(base, "lib", "util.py"), `"""Utility functions."""`+"\n")

	config := map[string]any{"source": []any{
		map[string]any{"path": "lib/", "language": "python"},
		map[string]any{"path": "cmd/", "language": "go"},
	}}

	// The Go path groups by package and reads no Python file.
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "cmd/"}, nil, base, config)
	wants(t, rendered, "**cmd**", "Package main provides the entry point.")
	rejects(t, rendered, ".py")

	// The Python path lists its files and reads no Go file.
	rendered, _ = resolve(t, "list-modules",
		map[string]string{"path": "lib/"}, nil, base, config)
	wants(t, rendered, "util.py")
	rejects(t, rendered, ".go")
}

func TestListModulesGroupsTypeScriptByDirectory(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "src", "index.ts"),
		"/** The entry point. */\nexport const x = 1;\n")
	write(t, filepath.Join(base, "src", "util", "dates.ts"),
		"/** Date helpers. */\nexport const y = 2;\n")
	write(t, filepath.Join(base, "src", "util", "dates.test.ts"),
		"test('x', () => {});\n")

	config := map[string]any{"source": []any{
		map[string]any{"path": "src/", "language": "typescript"},
	}}
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "src/"}, nil, base, config)
	wants(t, rendered, "**src/**", "**src/util/**",
		"- **src/index**", "- **src/util/dates**")
	// A test file is not a module of the project.
	rejects(t, rendered, "dates.test")
}

func TestListModulesFilesTrue(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "cmd", "main.go"),
		"// Package main provides entry point.\npackage main\n\nfunc main() {}\n")
	write(t, filepath.Join(base, "cmd", "util.go"),
		"package main\n\nfunc helper() {}\n")

	config := map[string]any{"source": []any{
		map[string]any{"path": "cmd/", "language": "go"},
	}}
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "cmd/", "files": "true"}, nil, base, config)
	wants(t, rendered, "main.go", "util.go")
}

func TestListModulesUnsupportedLanguage(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "src", "main.rs"), "fn main() {}\n")
	config := map[string]any{"source": []any{
		map[string]any{"path": "src/", "language": "rust"},
	}}

	t.Run("is a hard error naming the escape hatch", func(t *testing.T) {
		_, _, err := ResolveContent("list-modules",
			map[string]string{"path": "src/"}, nil, base, config)
		if err == nil {
			t.Fatal("a language with no extractor must be refused")
		}
		wants(t, err.Error(), "language 'rust' has no module extractor", "files=true")
	})

	t.Run("files=true does not raise", func(t *testing.T) {
		// The stub claims no file extensions, so nothing matches and the
		// listing says so instead of crashing.
		rendered, _ := resolve(t, "list-modules",
			map[string]string{"path": "src/", "files": "true"}, nil, base, config)
		wants(t, rendered, "no modules found")
	})
}

func TestListModulesCodelessProjectIsAHardError(t *testing.T) {
	// The missing-source check runs before the directory check: a codeless
	// project usually has no source directory either, so testing the
	// directory first would render a placeholder note and let the build
	// exit 0 -- the silent no-op the hard error exists to prevent.
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "src", "keep.txt"), "")

	for _, name := range []string{"with the directory present", "with no directory at all"} {
		directory := base
		if strings.Contains(name, "no directory") {
			directory = t.TempDir()
		}
		t.Run(name, func(t *testing.T) {
			_, _, err := ResolveContent("list-modules",
				map[string]string{"path": "src/"}, nil, directory,
				map[string]any{})
			if err == nil {
				t.Fatal("a codeless project must be refused")
			}
			wants(t, err.Error(), "declares no 'source' entries", ":-: list-modules")
			// ":::" is the section-content marker, not a directive prefix.
			rejects(t, err.Error(), ":::")
		})
	}
}

func TestFileToModuleName(t *testing.T) {
	cases := []struct {
		relPath  string
		language string
		want     string
		named    bool
	}{
		{"mylib/config.py", "python", "mylib.config", true},
		{"mylib/sub/core.py", "python", "mylib.sub.core", true},
		{"mylib/__init__.py", "python", "mylib", true},
		{"__init__.py", "python", "", false},
		{"pkg/handler.go", "go", "pkg/handler", true},
		{"src/index.ts", "typescript", "src/index", true},
	}
	for _, testCase := range cases {
		got, named := fileToModuleName(testCase.relPath, testCase.language)
		if got != testCase.want || named != testCase.named {
			t.Errorf("fileToModuleName(%q, %q) = (%q, %v), want (%q, %v)",
				testCase.relPath, testCase.language, got, named,
				testCase.want, testCase.named)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		filename string
		language string
		want     bool
	}{
		{"handler_test.go", "go", true},
		{"handler.go", "go", false},
		{"test_core.py", "python", true},
		{"core_test.py", "python", true},
		{"core.py", "python", false},
		{"dates.test.ts", "typescript", true},
		{"dates.spec.tsx", "typescript", true},
		{"dates.ts", "typescript", false},
		{"main.rs", "rust", false},
	}
	for _, testCase := range cases {
		if got := isTestFile(testCase.filename, testCase.language); got != testCase.want {
			t.Errorf("isTestFile(%q, %q) = %v, want %v",
				testCase.filename, testCase.language, got, testCase.want)
		}
	}
}

func TestListModulesSkipsTheScratchDirectoriesAtTheProjectRoot(t *testing.T) {
	// experiments/ and screenshots/ at the project root hold throwaway
	// probes, never the project's source; a directory of the same name
	// deeper down is ordinary source.
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "engine", "engine.go"),
		"// Package engine renders things.\npackage engine\n")
	write(t, filepath.Join(base, "engine", "experiments", "experiments.go"),
		"// Package experiments runs trials.\npackage experiments\n")
	write(t, filepath.Join(base, "experiments", "probe", "probe.go"),
		"// Package probe is a throwaway probe.\npackage probe\n")
	write(t, filepath.Join(base, "screenshots", "shot.go"),
		"// Package shot is a throwaway probe.\npackage shot\n")

	config := map[string]any{
		"source": []any{map[string]any{"path": ".", "language": "go"}},
	}
	rendered, _ := resolve(t, "list-modules",
		map[string]string{"path": "."}, nil, base, config)
	wants(t, rendered, "**engine**", "**engine/experiments**")
	rejects(t, rendered, "probe", "shot", "screenshots")
}
