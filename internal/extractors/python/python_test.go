package python

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/smm-h/stricttest/go/hygiene"
)

// Every expectation in this file is a recorded observation: the strings were
// printed by the Python implementation itself, over the same fixture, by
// scripts/probe_python_extractor.py.

const corePy = `"""Core module for mylib.

Provides essential utilities.
"""


def greet(name: str, loud: bool = False) -> str:
    """Say hello to someone.

    Args:
        name: The person to greet.
        loud: Whether to shout.
    """
    msg = f"Hello, {name}!"
    return msg.upper() if loud else msg


def _private_helper():
    # No docstring -- should be skipped
    pass


def _documented_private(x: int) -> int:
    """A private function that has a docstring -- should be included."""
    return x * 2


class Processor:
    """Processes items in a pipeline."""

    def run(self, items: list) -> list:
        """Run the pipeline on items."""
        return [self._transform(i) for i in items]

    def _transform(self, item):
        # No docstring -- skipped
        return item

    def _special_transform(self, item):
        """Internal but documented transform."""
        return item
`

const settingsPy = `"""Settings module."""

from dataclasses import dataclass


@dataclass
class Settings:
    host: str = "localhost"
    port: int = 8080
    debug: bool = False
    _internal: str = "hidden"
`

const modelsPy = `"""Data models."""

from dataclasses import dataclass


@dataclass
class Config:
    """Application configuration."""

    host: str = "localhost"  # Server hostname
    port: int = 8080  # Server port
    debug: bool = False  # Enable debug mode
`

const cliPy = `"""Command-line interface for mylib."""

HELP = """
Usage: mylib [options] <command>

Commands:
    run     Run the processor
    check   Check configuration
"""
`

const testCorePy = `"""Tests for mylib.core."""

import pytest


def test_greet_basic():
    """Test basic greeting."""
    from mylib.core import greet
    assert greet("World") == "Hello, World!"


class TestProcessor:
    """Tests for the Processor class."""

    def test_run_empty(self):
        from mylib.core import Processor
        p = Processor()
        assert p.run([]) == []
`

const pkgInitPy = `"""Package pkg."""

import os
from ._impl import Foo
from ._impl import Bar as Baz

__version__ = "1.2.3"

__all__ = ["Foo", "Baz", "__version__"]
`

// fixture writes the sample project every test in this file reads and returns
// its base directory.
func fixture(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	files := map[string]string{
		"mylib/core.py":      corePy,
		"mylib/__init__.py":  "\"\"\"mylib package.\"\"\"\n",
		"mylib/settings.py":  settingsPy,
		"mylib/models.py":    modelsPy,
		"mylib/cli.py":       cliPy,
		"tests/test_core.py": testCorePy,
		"plain.py":           "class Plain:\n    pass\n",
		"pydantic_models.py": "from pydantic import BaseModel\n\n\nclass Params(BaseModel):\n    name: str\n    count: int = 0\n",
		"pkg/_impl.py":       "class Foo:\n    pass\n\n\nclass Bar:\n    pass\n",
		"pkg/__init__.py":    pkgInitPy,
		"bad.py":             "def broken(\n",
		"schema.json":        `{"name":"mylib","version":"1.0.0","debug":true,"port":8080}`,
		"config.toml":        "[server]\nhost = \"localhost\"\nport = 3000\n",
		"app.ini":            "[section]\nkey = value\n",
	}
	for name, content := range files {
		full := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func newExtractor() extractors.Extractor { return New() }

var sourcePaths = []string{"mylib/"}

// TestRefRendersTheWholeModule is the strongest parity check in this file: one
// module whose docstring, public function, documented private function, class
// and methods all reach the page, compared byte for byte.
func TestRefRendersTheWholeModule(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)

	got, err := newExtractor().Extract("ref", map[string]string{"path": "core"}, nil, sourcePaths, base)
	if err != nil {
		t.Fatal(err)
	}
	want := "## `core`\n" +
		"\n" +
		"Core module for mylib.\n" +
		"\n" +
		"Provides essential utilities.\n" +
		"\n" +
		"### `greet`\n" +
		"\n" +
		"```python\n" +
		"def greet(name: str, loud: bool=False) -> str\n" +
		"```\n" +
		"\n" +
		"Say hello to someone.\n" +
		"\n" +
		"**Args:**\n" +
		"\n" +
		"- `name`: The person to greet.\n" +
		"- `loud`: Whether to shout.\n" +
		"\n" +
		"### `_documented_private`\n" +
		"\n" +
		"```python\n" +
		"def _documented_private(x: int) -> int\n" +
		"```\n" +
		"\n" +
		"A private function that has a docstring -- should be included.\n" +
		"\n" +
		"### `Processor`\n" +
		"\n" +
		"Processes items in a pipeline.\n" +
		"\n" +
		"#### `run`\n" +
		"\n" +
		"```python\n" +
		"def run(self, items: list) -> list\n" +
		"```\n" +
		"\n" +
		"Run the pipeline on items.\n" +
		"\n" +
		"#### `_special_transform`\n" +
		"\n" +
		"```python\n" +
		"def _special_transform(self, item)\n" +
		"```\n" +
		"\n" +
		"Internal but documented transform."
	if got != want {
		t.Fatalf("ref =\n%s\n\nwant\n%s", got, want)
	}
	if strings.Contains(got, "_private_helper") {
		t.Error("an undocumented private function reached the page")
	}
	if strings.Contains(got, "#### `_transform`") {
		t.Error("an undocumented private method reached the page")
	}
}

func TestRefExactRenderings(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	tests := []struct {
		name        string
		attrs       map[string]string
		sourcePaths []string
		want        string
	}{
		{
			name:        "a dataclass with no docstring renders a field table",
			attrs:       map[string]string{"path": "settings"},
			sourcePaths: sourcePaths,
			want: "## `settings`\n\nSettings module.\n\n### `Settings`\n\n" +
				"| Field | Type | Default |\n| --- | --- | --- |\n" +
				"| `host` | `str` | `'localhost'` |\n" +
				"| `port` | `int` | `8080` |\n" +
				"| `debug` | `bool` | `False` |",
		},
		{
			name:  "an empty class still gets a heading and its signature",
			attrs: map[string]string{"path": "plain.py"},
			want:  "## `plain`\n\n### `Plain`\n\n```python\nclass Plain:\n```",
		},
		{
			name:  "a pydantic model with no docstring renders a field table",
			attrs: map[string]string{"path": "pydantic_models.py"},
			want: "## `pydantic_models`\n\n### `Params`\n\n" +
				"| Field | Type | Default |\n| --- | --- | --- |\n" +
				"| `name` | `str` |  |\n| `count` | `int` | `0` |",
		},
		{
			name:  "a package's __all__ re-exports and constants are emitted",
			attrs: map[string]string{"path": "pkg"},
			want: "## `pkg`\n\nPackage pkg.\n\n### `Foo`\n\n```python\nfrom ._impl import Foo\n```\n\n" +
				"### `Baz`\n\n```python\nfrom ._impl import Bar as Baz\n```\n\n" +
				"### `__version__`\n\n```python\n__version__ = '1.2.3'\n```",
		},
		{
			name:  "a target resolves a re-export",
			attrs: map[string]string{"path": "pkg", "target": "Foo"},
			want:  "### `Foo`\n\n```python\nfrom ._impl import Foo\n```",
		},
		{
			name:        "a target renders only that symbol",
			attrs:       map[string]string{"path": "core", "target": "greet"},
			sourcePaths: sourcePaths,
			want: "### `greet`\n\n```python\ndef greet(name: str, loud: bool=False) -> str\n```\n\n" +
				"Say hello to someone.\n\n**Args:**\n\n" +
				"- `name`: The person to greet.\n- `loud`: Whether to shout.",
		},
		{
			name:        "a dotted path resolves against the base directory",
			attrs:       map[string]string{"path": "mylib.core", "target": "greet"},
			sourcePaths: sourcePaths,
			want: "### `greet`\n\n```python\ndef greet(name: str, loud: bool=False) -> str\n```\n\n" +
				"Say hello to someone.\n\n**Args:**\n\n" +
				"- `name`: The person to greet.\n- `loud`: Whether to shout.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract("ref", tt.attrs, nil, tt.sourcePaths, base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("ref =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestOtherDirectivesExactRenderings(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	tests := []struct {
		name      string
		directive string
		attrs     map[string]string
		want      string
	}{
		{
			name:      "code-help emits the docstring and the HELP constant",
			directive: "code-help",
			attrs:     map[string]string{"path": "cli"},
			want: "Command-line interface for mylib.\n\n```\nUsage: mylib [options] <command>\n\n" +
				"Commands:\n    run     Run the processor\n    check   Check configuration\n```",
		},
		{
			name:      "prose-desc emits the docstring alone",
			directive: "prose-desc",
			attrs:     map[string]string{"path": "core"},
			want:      "Core module for mylib.\n\nProvides essential utilities.",
		},
		{
			name:      "table-schema reads a class's fields and its inline comments",
			directive: "table-schema",
			attrs:     map[string]string{"path": "models", "target": "Config"},
			want: "| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
				"| `host` | `str` | `'localhost'` | Server hostname |\n" +
				"| `port` | `int` | `8080` | Server port |\n" +
				"| `debug` | `bool` | `False` | Enable debug mode |",
		},
		{
			name:      "code-test with no target fences the whole file",
			directive: "code-test",
			attrs:     map[string]string{"path": "tests/test_core.py"},
			want: "```python\n\"\"\"Tests for mylib.core.\"\"\"\n\nimport pytest\n\n\n" +
				"def test_greet_basic():\n    \"\"\"Test basic greeting.\"\"\"\n" +
				"    from mylib.core import greet\n    assert greet(\"World\") == \"Hello, World!\"\n\n\n" +
				"class TestProcessor:\n    \"\"\"Tests for the Processor class.\"\"\"\n\n" +
				"    def test_run_empty(self):\n        from mylib.core import Processor\n" +
				"        p = Processor()\n        assert p.run([]) == []\n```",
		},
		{
			name:      "code-test extracts one function",
			directive: "code-test",
			attrs:     map[string]string{"path": "tests/test_core.py", "target": "test_greet_basic"},
			want: "```python\ndef test_greet_basic():\n    \"\"\"Test basic greeting.\"\"\"\n" +
				"    from mylib.core import greet\n    assert greet(\"World\") == \"Hello, World!\"\n```",
		},
		{
			name:      "code-test extracts one class and dedents it",
			directive: "code-test",
			attrs:     map[string]string{"path": "tests/test_core.py", "target": "TestProcessor"},
			want: "```python\nclass TestProcessor:\n    \"\"\"Tests for the Processor class.\"\"\"\n\n" +
				"    def test_run_empty(self):\n        from mylib.core import Processor\n" +
				"        p = Processor()\n        assert p.run([]) == []\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract(tt.directive, tt.attrs, nil, sourcePaths, base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("%s =\n%q\nwant\n%q", tt.directive, got, tt.want)
			}
		})
	}
}

func TestErrorMarkers(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	tests := []struct {
		name      string
		directive string
		attrs     map[string]string
		want      string
	}{
		{"ref with no path", "ref", map[string]string{}, "> *[selfdoc: :::module requires a module path argument]*"},
		{"ref with a missing module", "ref", map[string]string{"path": "nonexistent.module"}, "> *[selfdoc: module 'nonexistent.module' not found]*"},
		{"ref with a missing target", "ref", map[string]string{"path": "core", "target": "nope"}, "> *[selfdoc: symbol 'nope' not found in 'core']*"},
		{"code-test with no path", "code-test", map[string]string{}, "> *[selfdoc: :::test requires a file path argument]*"},
		{"code-test with a missing file", "code-test", map[string]string{"path": "tests/nope.py"}, "> *[selfdoc: test file 'tests/nope.py' not found]*"},
		{"code-test with a missing target", "code-test", map[string]string{"path": "tests/test_core.py", "target": "nope"}, "> *[selfdoc: 'nope' not found in 'tests/test_core.py']*"},
		{"table-schema with no path", "table-schema", map[string]string{}, "> *[selfdoc: :::schema requires an argument]*"},
		{"table-schema with no target on a module", "table-schema", map[string]string{"path": "core"}, "> *[selfdoc: :::schema for Python requires 'module_path ClassName' format]*"},
		{"table-schema with a missing class", "table-schema", map[string]string{"path": "core", "target": "NoSuchClass"}, "> *[selfdoc: class 'NoSuchClass' not found in 'core']*"},
		{"table-schema with a missing json file", "table-schema", map[string]string{"path": "missing.json"}, "> *[selfdoc: JSON file 'missing.json' not found]*"},
		{"code-help with no path", "code-help", map[string]string{}, "> *[selfdoc: :::cli requires a module path argument]*"},
		{"code-help with a missing module", "code-help", map[string]string{"path": "nonexistent"}, "> *[selfdoc: module 'nonexistent' not found]*"},
		{"code-help with no CLI documentation", "code-help", map[string]string{"path": "plain.py"}, "> *[selfdoc: no CLI documentation found in 'plain.py']*"},
		{"prose-desc with no path", "prose-desc", map[string]string{}, "> *[selfdoc: :::prose-desc requires a module path argument]*"},
		{"prose-desc with no docstring", "prose-desc", map[string]string{"path": "plain.py"}, "> *[selfdoc: no docstring found in 'plain.py']*"},
		{"table-config with no path", "table-config", map[string]string{}, "> *[selfdoc: table-config requires a file path argument]*"},
		{"table-config with a missing file", "table-config", map[string]string{"path": "missing.json"}, "> *[selfdoc: config file 'missing.json' not found]*"},
		{"an unknown directive", "nonsense", map[string]string{"path": "core"}, "> *[selfdoc: unknown directive 'nonsense' for python extractor]*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract(tt.directive, tt.attrs, nil, sourcePaths, base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.directive, got, tt.want)
			}
		})
	}
}

func TestSyntaxErrorRendersAMarker(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)

	got, err := newExtractor().Extract("ref", map[string]string{"path": "bad.py"}, nil, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "> *[selfdoc: syntax error in 'bad.py': ") {
		t.Fatalf("ref = %q, want a syntax-error marker", got)
	}
}

func TestConfigDirectives(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	tests := []struct {
		name      string
		directive string
		attrs     map[string]string
		want      string
	}{
		{
			name:      "table-schema tabulates a JSON document",
			directive: "table-schema",
			attrs:     map[string]string{"path": "schema.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `name` | string | `\"mylib\"` |\n| `version` | string | `\"1.0.0\"` |\n" +
				"| `debug` | boolean | `true` |\n| `port` | integer | `8080` |",
		},
		{
			name:      "table-schema honors exclude",
			directive: "table-schema",
			attrs:     map[string]string{"path": "schema.json", "exclude": "version, debug"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `name` | string | `\"mylib\"` |\n| `port` | integer | `8080` |",
		},
		{
			name:      "table-schema reports an exclude key the document lacks",
			directive: "table-schema",
			attrs:     map[string]string{"path": "schema.json", "exclude": "nonexistent"},
			want:      "> *[selfdoc: exclude key 'nonexistent' not found in 'schema.json']*",
		},
		{
			name:      "table-config tabulates a TOML document",
			directive: "table-config",
			attrs:     map[string]string{"path": "config.toml"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `server.host` | string | `\"localhost\"` |\n| `server.port` | integer | `3000` |",
		},
		{
			name:      "table-config fences an unsupported format",
			directive: "table-config",
			attrs:     map[string]string{"path": "app.ini"},
			want:      "```\n[section]\nkey = value\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract(tt.directive, tt.attrs, nil, sourcePaths, base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.directive, got, tt.want)
			}
		})
	}
}

func TestNameDetectAndExtensions(t *testing.T) {
	hygiene.Isolate(t)
	extractor := newExtractor()

	if got := extractor.Name(); got != "python" {
		t.Errorf("Name = %q, want python", got)
	}
	if got := extractor.FileExtensions(); !reflect.DeepEqual(got, []string{".py"}) {
		t.Errorf("FileExtensions = %#v", got)
	}

	empty := t.TempDir()
	if extractor.Detect(empty) {
		t.Error("an empty directory was detected as a Python project")
	}

	withPyproject := t.TempDir()
	if err := os.WriteFile(filepath.Join(withPyproject, "pyproject.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !extractor.Detect(withPyproject) {
		t.Error("pyproject.toml was not detected")
	}

	withSetup := t.TempDir()
	if err := os.WriteFile(filepath.Join(withSetup, "setup.py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !extractor.Detect(withSetup) {
		t.Error("setup.py was not detected")
	}
}

func TestResolvePath(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	got := extractor.ResolvePath("core", sourcePaths, base)
	if !strings.HasSuffix(got, "core.py") {
		t.Fatalf("ResolvePath = %q, want a path ending in core.py", got)
	}
	if got := extractor.ResolvePath("nonexistent", sourcePaths, base); got != "" {
		t.Fatalf("ResolvePath = %q, want empty", got)
	}
	// A package resolves to its __init__.py.
	if got := extractor.ResolvePath("pkg", nil, base); !strings.HasSuffix(got, filepath.Join("pkg", "__init__.py")) {
		t.Fatalf("ResolvePath(pkg) = %q", got)
	}
}
