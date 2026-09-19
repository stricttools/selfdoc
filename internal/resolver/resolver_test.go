package resolver

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/stricttest/go/hygiene"

	// The language packages register their extractors, which is what links
	// a language into a binary. A build links them through internal/cli; the
	// suite does it here.
	_ "github.com/stricttools/selfdoc/internal/extractors/golang"
	_ "github.com/stricttools/selfdoc/internal/extractors/python"
	_ "github.com/stricttools/selfdoc/internal/extractors/zig"
)

func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// requirePython3 skips the test when no interpreter is on PATH. The custom
// directive contract is Python's, so a project's own script is loaded and
// called through python3.
func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH: a custom directive is called through it")
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

// makeConfig is a minimal config, with the given keys merged over it.
func makeConfig(overrides map[string]any) map[string]any {
	config := map[string]any{
		"source": []any{
			map[string]any{"path": "src/", "language": "python"},
		},
		"docs":       ".stricttools/docs/",
		"output":     ".stricttools/docs-cache/build/",
		"deploy":     nil,
		"directives": map[string]any{},
	}
	for key, value := range overrides {
		config[key] = value
	}
	return config
}

// newResolver builds a resolver, failing the test when it cannot.
func newResolver(t *testing.T, config map[string]any, baseDir string) *Resolver {
	t.Helper()
	resolver, err := MakeResolver(config, baseDir, effects.Unbound())
	if err != nil {
		t.Fatalf("MakeResolver: %v", err)
	}
	return resolver
}

// mustResolve resolves a directive, failing the test on an error.
func mustResolve(
	t *testing.T, resolver *Resolver, name string, attrs map[string]string,
	body []string,
) string {
	t.Helper()
	rendered, err := resolver.Resolve(name, attrs, body)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", name, err)
	}
	return rendered
}

func wants(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, want := range substrings {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func rejects(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, unwanted := range substrings {
		if strings.Contains(content, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, content)
		}
	}
}

// -- Single-language dispatch ----------------------------------------------

func TestResolverDispatchesASingleLanguage(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "src", "__init__.py"),
		"\"\"\"My module.\"\"\"\n\ndef greet():\n    \"\"\"Say hi.\"\"\"\n    pass\n")

	resolver := newResolver(t, makeConfig(nil), base)
	wants(t, mustResolve(t, resolver, "ref", map[string]string{"path": "src"}, nil), "greet")
}

func TestResolverSetsLastSourceEntry(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "src", "__init__.py"),
		"\"\"\"Module.\"\"\"\n\ndef func():\n    \"\"\"Doc.\"\"\"\n    pass\n")

	resolver := newResolver(t, makeConfig(nil), base)
	if resolver.LastSourceEntry != nil {
		t.Fatal("nothing has been resolved yet")
	}

	mustResolve(t, resolver, "ref", map[string]string{"path": "src"}, nil)
	if resolver.LastSourceEntry == nil {
		t.Fatal("a resolved path must record its source entry")
	}
	if resolver.LastSourceEntry.Language != "python" {
		t.Errorf("language = %q", resolver.LastSourceEntry.Language)
	}
	if resolver.LastSourceEntry.Path != "src/" {
		t.Errorf("path = %q", resolver.LastSourceEntry.Path)
	}
}

func TestResolverClearsLastSourceEntryOnAContentDirective(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "src", "__init__.py"), "\"\"\"Module.\"\"\"\n")

	resolver := newResolver(t, makeConfig(nil), base)
	mustResolve(t, resolver, "list-glossary", nil, []string{"**Term**: Def"})
	if resolver.LastSourceEntry != nil {
		t.Fatal("a content directive belongs to no language")
	}
}

// -- Multi-language dispatch -----------------------------------------------

func TestResolverDispatchesToTheMatchingLanguage(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "pylib", "__init__.py"),
		"\"\"\"Python lib.\"\"\"\n\ndef py_func():\n    \"\"\"A function.\"\"\"\n    pass\n")
	write(t, filepath.Join(base, "golib", "main.go"),
		"package main\n\n// Hello says hello.\nfunc Hello() {}\n")

	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{
		map[string]any{"path": "pylib/", "language": "python"},
		map[string]any{"path": "golib/", "language": "go"},
	}}), base)

	wants(t, mustResolve(t, resolver, "ref", map[string]string{"path": "pylib"}, nil), "py_func")
	wants(t, mustResolve(t, resolver, "ref", map[string]string{"path": "golib"}, nil), "Hello")
}

func TestResolverRefusesAnAmbiguousPath(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	// Both the Go and the Zig extractor resolve a directory holding their
	// own files, so "." resolves in both.
	write(t, filepath.Join(base, "main.go"),
		"package main\n\n// RootFunc does something.\nfunc RootFunc() {}\n")
	write(t, filepath.Join(base, "main.zig"),
		"//! Root module.\npub fn rootZig() void {}\n")

	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{
		map[string]any{"path": ".", "language": "go"},
		map[string]any{"path": ".", "language": "zig"},
	}}), base)

	_, err := resolver.Resolve("ref", map[string]string{"path": "."}, nil)
	if err == nil {
		t.Fatal("a path that resolves in two languages must be refused")
	}
	wants(t, err.Error(), "Ambiguous", "go", "zig")
}

func TestResolverLangAttributeDisambiguates(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "main.go"),
		"package main\n\n// RootFunc does something.\nfunc RootFunc() {}\n")
	write(t, filepath.Join(base, "main.zig"),
		"//! Root module.\npub fn rootZig() void {}\n")

	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{
		map[string]any{"path": ".", "language": "go"},
		map[string]any{"path": ".", "language": "zig"},
	}}), base)

	wants(t, mustResolve(t, resolver, "ref",
		map[string]string{"path": ".", "lang": "go"}, nil), "RootFunc")
	wants(t, mustResolve(t, resolver, "ref",
		map[string]string{"path": ".", "lang": "zig"}, nil), "rootZig")

	// The elected language is the one recorded.
	if resolver.LastSourceEntry == nil || resolver.LastSourceEntry.Language != "zig" {
		t.Fatalf("last source entry = %+v", resolver.LastSourceEntry)
	}
}

func TestResolverLangAttributeNamingAnUnconfiguredLanguage(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "main.go"), "package main\n\nfunc Hello() {}\n")
	write(t, filepath.Join(base, "main.zig"), "pub fn hello() void {}\n")

	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{
		map[string]any{"path": ".", "language": "go"},
		map[string]any{"path": ".", "language": "zig"},
	}}), base)

	rendered := mustResolve(t, resolver, "ref",
		map[string]string{"path": ".", "lang": "python"}, nil)
	wants(t, rendered, "lang='python' not found in configured source languages",
		"go, zig")
}

func TestResolverZeroMatchesReportsThroughTheFirstGroup(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "pylib", "__init__.py"), "\"\"\"Module.\"\"\"\n")
	write(t, filepath.Join(base, "golib", "main.go"), "package main\n")

	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{
		map[string]any{"path": "pylib/", "language": "python"},
		map[string]any{"path": "golib/", "language": "go"},
	}}), base)

	rendered := mustResolve(t, resolver, "ref",
		map[string]string{"path": "nonexistent.module"}, nil)
	wants(t, rendered, "not found")
}

// -- The codeless project --------------------------------------------------

func TestResolverCodelessProjectIsAHardError(t *testing.T) {
	// A page that asks for an API reference must not silently become a page
	// that has none.
	isolate(t)
	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{}}), t.TempDir())
	_, err := resolver.Resolve("ref", map[string]string{"path": "src"}, nil)
	if err == nil {
		t.Fatal("a code directive in a codeless project must be refused")
	}
	wants(t, err.Error(), "Directive :-: ref extracts from source code",
		"declares no 'source' entries")
}

// -- Unsupported and unknown -----------------------------------------------

func TestResolverUnsupportedLanguageRendersTheMarker(t *testing.T) {
	isolate(t)
	resolver := newResolver(t, makeConfig(map[string]any{"source": []any{
		map[string]any{"path": "src/", "language": "rust"},
	}}), t.TempDir())
	wants(t, mustResolve(t, resolver, "ref", map[string]string{"path": "foo"}, nil),
		"no extractor for 'rust'")
}

func TestResolverUnknownDirectiveRendersTheMarker(t *testing.T) {
	isolate(t)
	requirePython3(t)
	resolver := newResolver(t, makeConfig(nil), t.TempDir())
	wants(t, mustResolve(t, resolver, "nonexistent-directive", nil, nil),
		"unknown directive")
}

func TestResolverDispatchesEveryCodeDirectiveName(t *testing.T) {
	// Each of these goes through the language extractor and finds no file;
	// what the test pins is that the name reached a handler rather than the
	// unknown-directive marker.
	isolate(t)
	requirePython3(t)
	resolver := newResolver(t, makeConfig(nil), t.TempDir())
	cases := map[string]string{
		"ref":          "some.module",
		"code-test":    "test_file.py",
		"table-schema": "schema.json",
		"code-help":    "cli_module",
		"table-config": "config.json",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			rendered := mustResolve(t, resolver, name, map[string]string{"path": path}, nil)
			wants(t, rendered, "not found")
			rejects(t, rendered, "unknown directive")
		})
	}
}

func TestResolverResolvesContentDirectives(t *testing.T) {
	isolate(t)
	resolver := newResolver(t, makeConfig(nil), t.TempDir())

	rendered := mustResolve(t, resolver, "list-glossary", nil, []string{
		"**Term1**: Definition one",
		"**Term2**: Definition two",
	})
	wants(t, rendered, `<div class="glossary">`,
		"<dt><dfn>Term1</dfn></dt>", "<dd>Definition one</dd>",
		"<dt><dfn>Term2</dfn></dt>", "<dd>Definition two</dd>")

	wants(t, mustResolve(t, resolver, "list-glossary", nil, nil),
		`<div class="glossary"><dl></dl></div>`)
}

// -- Custom directives -----------------------------------------------------

// customResolver writes a script at filename and returns a resolver that maps
// name to it.
func customResolver(t *testing.T, name, filename, source string) (*Resolver, string) {
	t.Helper()
	base := t.TempDir()
	write(t, filepath.Join(base, filename), source)
	config := makeConfig(map[string]any{
		"directives": map[string]any{name: filename},
	})
	return newResolver(t, config, base), base
}

func TestCustomDirectiveIsCalledWithItsArguments(t *testing.T) {
	isolate(t)
	requirePython3(t)

	t.Run("attrs", func(t *testing.T) {
		resolver, _ := customResolver(t, "api", "extract_api.py",
			"def resolve(attrs, config, body):\n"+
				"    return f'API for {attrs.get(\"path\", \"\")}'\n")
		if got := mustResolve(t, resolver, "api", map[string]string{"path": "v2"}, nil); got != "API for v2" {
			t.Fatalf("rendered = %q", got)
		}
	})

	t.Run("config", func(t *testing.T) {
		base := t.TempDir()
		write(t, filepath.Join(base, "check_config.py"),
			"def resolve(attrs, config, body):\n"+
				"    return f'lang={config[\"language\"]}'\n")
		config := makeConfig(map[string]any{
			"language":   "go",
			"directives": map[string]any{"check": "check_config.py"},
		})
		resolver := newResolver(t, config, base)
		if got := mustResolve(t, resolver, "check", nil, nil); got != "lang=go" {
			t.Fatalf("rendered = %q", got)
		}
	})

	t.Run("body", func(t *testing.T) {
		resolver, _ := customResolver(t, "echo", "with_body.py",
			"def resolve(attrs, config, body):\n    return '\\n'.join(body)\n")
		got := mustResolve(t, resolver, "echo", nil, []string{"line1", "line2"})
		if got != "line1\nline2" {
			t.Fatalf("rendered = %q", got)
		}
	})
}

func TestCustomDirectiveReturnsMarkdownVerbatim(t *testing.T) {
	isolate(t)
	requirePython3(t)
	resolver, _ := customResolver(t, "table", "table_gen.py",
		"def resolve(attrs, config, body):\n"+
			"    return '| Col A | Col B |\\n|---|---|\\n| 1 | 2 |'\n")
	rendered := mustResolve(t, resolver, "table", nil, nil)
	wants(t, rendered, "| Col A | Col B |", "| 1 | 2 |")
}

func TestCustomDirectiveOverridesABuiltIn(t *testing.T) {
	isolate(t)
	requirePython3(t)
	resolver, _ := customResolver(t, "ref", "my_module.py",
		"def resolve(attrs, config, body):\n"+
			"    return f'CUSTOM module: {attrs.get(\"path\", \"\")}'\n")
	got := mustResolve(t, resolver, "ref", map[string]string{"path": "selfdoc.config"}, nil)
	if got != "CUSTOM module: selfdoc.config" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestCustomDirectiveFailuresAreHardErrors(t *testing.T) {
	// The Python this replaces swallowed each of these into an inline note
	// on the page, where a reader would find "custom directive 'x' failed"
	// in place of the content and the build would still exit 0.
	isolate(t)
	requirePython3(t)

	t.Run("a missing script", func(t *testing.T) {
		config := makeConfig(map[string]any{
			"directives": map[string]any{"bad": "nonexistent.py"},
		})
		resolver := newResolver(t, config, t.TempDir())
		_, err := resolver.Resolve("bad", nil, nil)
		if err == nil {
			t.Fatal("a missing script must be refused")
		}
		wants(t, err.Error(), "custom directive 'bad' failed",
			"custom directive script not found", "nonexistent.py")
	})

	t.Run("a script that raises", func(t *testing.T) {
		resolver, _ := customResolver(t, "broken", "broken.py",
			"def resolve(attrs, config, body):\n"+
				"    raise ValueError('something went wrong')\n")
		_, err := resolver.Resolve("broken", nil, nil)
		if err == nil {
			t.Fatal("a script that raises must be refused")
		}
		wants(t, err.Error(), "custom directive 'broken' failed",
			"something went wrong")
	})

	t.Run("a script that fails to import", func(t *testing.T) {
		resolver, _ := customResolver(t, "importer", "importer.py",
			"raise RuntimeError('import time failure')\n"+
				"def resolve(attrs, config, body):\n    return ''\n")
		_, err := resolver.Resolve("importer", nil, nil)
		if err == nil {
			t.Fatal("a script that fails to load must be refused")
		}
		wants(t, err.Error(), "custom directive 'importer' failed",
			"import time failure")
	})

	t.Run("a script with no callable resolve", func(t *testing.T) {
		resolver, _ := customResolver(t, "nope", "no_resolve.py",
			"def something_else():\n    pass\n")
		_, err := resolver.Resolve("nope", nil, nil)
		if err == nil {
			t.Fatal("a script with no resolve must be refused")
		}
		wants(t, err.Error(), "custom directive 'nope' failed",
			"no callable 'resolve'")
	})
}

func TestANonPythonScriptIsExecutedDirectly(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	script := filepath.Join(base, "render.sh")
	write(t, script, "#!/bin/sh\ncat > /dev/null\necho 'from the shell'\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	config := makeConfig(map[string]any{
		"directives": map[string]any{"shell": "render.sh"},
	})
	resolver := newResolver(t, config, base)
	got := mustResolve(t, resolver, "shell", nil, nil)
	if got != "from the shell\n" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestANonPythonScriptReadsThePayloadOnStandardInput(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	script := filepath.Join(base, "echo.sh")
	write(t, script, "#!/bin/sh\nexec cat\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	config := makeConfig(map[string]any{
		"directives": map[string]any{"payload": "echo.sh"},
	})
	resolver := newResolver(t, config, base)
	got := mustResolve(t, resolver, "payload", map[string]string{"key": "value"}, []string{"a"})
	wants(t, got, `"attrs":{"key":"value"}`, `"body":["a"]`, `"base_dir":`)
}

func TestANonPythonScriptThatFailsIsAHardError(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	script := filepath.Join(base, "fail.sh")
	write(t, script, "#!/bin/sh\ncat > /dev/null\necho 'it broke' >&2\nexit 3\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	config := makeConfig(map[string]any{
		"directives": map[string]any{"failing": "fail.sh"},
	})
	resolver := newResolver(t, config, base)
	_, err := resolver.Resolve("failing", nil, nil)
	if err == nil {
		t.Fatal("a script that exits non-zero must be refused")
	}
	wants(t, err.Error(), "custom directive 'failing' failed", "it broke")
}

func TestANonCustomDirectiveFallsThrough(t *testing.T) {
	isolate(t)
	requirePython3(t)
	resolver := newResolver(t, makeConfig(nil), t.TempDir())
	rendered := mustResolve(t, resolver, "ref",
		map[string]string{"path": "nonexistent.module"}, nil)
	rejects(t, rendered, "custom directive")
}

// -- Directives compiled into the binary ----------------------------------

func TestABuiltinDirectiveResolvesInProcess(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	config := makeConfig(map[string]any{
		"directives": map[string]any{
			"projects-cards": BuiltinDirective(
				func(attrs map[string]string, body []string) (string, error) {
					return "cards for " + attrs["slug"] +
						" with " + strings.Join(body, "|"), nil
				},
			),
		},
	})
	resolver := newResolver(t, config, base)
	got := mustResolve(t, resolver, "projects-cards",
		map[string]string{"slug": "alpha"}, []string{"a", "b"})
	if got != "cards for alpha with a|b" {
		t.Fatalf("rendered %q", got)
	}
}

func TestABuiltinDirectivesErrorIsTheResolversError(t *testing.T) {
	isolate(t)
	config := makeConfig(map[string]any{
		"directives": map[string]any{
			"blog-highlights": BuiltinDirective(
				func(map[string]string, []string) (string, error) {
					return "", errors.New("requires limit")
				},
			),
		},
	})
	resolver := newResolver(t, config, t.TempDir())
	_, err := resolver.Resolve("blog-highlights", nil, nil)
	if err == nil {
		t.Fatal("a built-in directive that refuses must refuse the resolve")
	}
	wants(t, err.Error(), "requires limit")
}

func TestABuiltinDirectiveIsCheckedBeforeAContentDirective(t *testing.T) {
	isolate(t)
	config := makeConfig(map[string]any{
		"directives": map[string]any{
			"list-tree": BuiltinDirective(
				func(map[string]string, []string) (string, error) {
					return "not a tree", nil
				},
			),
		},
	})
	resolver := newResolver(t, config, t.TempDir())
	// The content directives are resolved first, so a registered name that
	// collides with one never reaches the registration -- which is what
	// keeps a project from redefining the framework's own vocabulary.
	got := mustResolve(t, resolver, "list-tree",
		map[string]string{"path": "."}, nil)
	rejects(t, got, "not a tree")
}

func TestABuiltinDirectiveIsCheckedBeforeASameNamedScript(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "shim.py"),
		"def resolve(attrs, config, body):\n    return 'from the script'\n")
	config := makeConfig(map[string]any{
		"directives": map[string]any{
			"projects-cards": BuiltinDirective(
				func(map[string]string, []string) (string, error) {
					return "from the binary", nil
				},
			),
		},
	})
	// One name, both spellings: the mapping holds the function, and a
	// script of the same name declared beside it cannot shadow it.
	config["directives"].(map[string]any)["blog-highlights"] = "shim.py"
	resolver := newResolver(t, config, base)
	if got := mustResolve(t, resolver, "projects-cards", nil, nil); got != "from the binary" {
		t.Fatalf("rendered %q", got)
	}
}

func TestAScriptBesideABuiltinStillGetsItsPayload(t *testing.T) {
	isolate(t)
	requirePython3(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "shim.py"),
		"import json\n\n\n"+
			"def resolve(attrs, config, body):\n"+
			"    return json.dumps(sorted(config['directives']))\n")
	config := makeConfig(map[string]any{
		"directives": map[string]any{
			"projects-cards": BuiltinDirective(
				func(map[string]string, []string) (string, error) {
					return "from the binary", nil
				},
			),
			"names": "shim.py",
		},
	})
	resolver := newResolver(t, config, base)
	// A function has no JSON form, so the payload names the scripts alone.
	// Encoding the registration would fail the script that asked for none
	// of it.
	got := mustResolve(t, resolver, "names", nil, nil)
	wants(t, got, `["names"]`)
	rejects(t, got, "projects-cards")
}
