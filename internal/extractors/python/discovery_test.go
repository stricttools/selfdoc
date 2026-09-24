package python

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/testisolation/go/hygiene"
)

// Every expectation in this file is a recorded observation of the Python
// implementation's own answer for the same source.

// writeFile puts content in a fresh temporary directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPublicSymbols(t *testing.T) {
	hygiene.Isolate(t)
	extractor := newExtractor()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "public top-level functions and classes",
			source: "def greet(): pass\ndef _private(): pass\nclass Widget: pass\n" +
				"class _Internal: pass\nasync def fetch_data(): pass\n",
			want: []string{"greet", "Widget", "fetch_data"},
		},
		{
			name:   "a literal __all__ is taken at its word",
			source: "__all__ = [\"Foo\", \"bar\"]\nclass Foo: pass\ndef bar(): pass\nclass Baz: pass\n",
			want:   []string{"Foo", "bar"},
		},
		{
			name:   "__all__ may name a private symbol",
			source: "__all__ = [\"_private_helper\", \"Public\"]\ndef _private_helper(): pass\nclass Public: pass\n",
			want:   []string{"_private_helper", "Public"},
		},
		{
			name: "a non-literal __all__ falls back to the heuristic",
			source: "__all__ = some_function()\ndef greet(): pass\ndef _hidden(): pass\n" +
				"class Widget: pass\n",
			want: []string{"greet", "Widget"},
		},
		{
			name:   "an empty __all__ exports nothing",
			source: "__all__ = []\ndef greet(): pass\nclass Widget: pass\n",
			want:   nil,
		},
		{
			name:   "a tuple __all__ counts",
			source: "__all__ = (\"Foo\", \"Bar\")\nclass Foo: pass\nclass Bar: pass\nclass Baz: pass\n",
			want:   []string{"Foo", "Bar"},
		},
		{
			name:   "a file that does not parse exports nothing",
			source: "def broken(\n",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.PublicSymbols(writeFile(t, "mod.py", tt.source))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("PublicSymbols = %#v, want %#v", got, tt.want)
			}
		})
	}

	t.Run("a missing file exports nothing", func(t *testing.T) {
		got, err := extractor.PublicSymbols(filepath.Join(t.TempDir(), "nope.py"))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("PublicSymbols = %#v, want none", got)
		}
	})
}

func TestModuleDocstring(t *testing.T) {
	hygiene.Isolate(t)
	extractor := newExtractor()

	const module = `"""Loads and validates the tool configuration from disk, applying
defaults and reporting any errors clearly to the caller.

Example:

    cfg = load()
    print(cfg.value)
"""

def load():
    pass
`

	got, err := extractor.ModuleDocstring(writeFile(t, "config.py", module))
	if err != nil {
		t.Fatal(err)
	}
	want := "Loads and validates the tool configuration from disk, applying defaults " +
		"and reporting any errors clearly to the caller.\n\nExample:\n\n" +
		"    cfg = load()\n    print(cfg.value)"
	if got != want {
		t.Fatalf("ModuleDocstring =\n%q\nwant\n%q", got, want)
	}

	// The wrapped first sentence is one line, so the unit-picker sees a whole
	// sentence -- the property this normalization exists for.
	wantSentence := "Loads and validates the tool configuration from disk, applying " +
		"defaults and reporting any errors clearly to the caller."
	if sentence := prose.FirstSentence(got); sentence != wantSentence {
		t.Fatalf("FirstSentence = %q, want %q", sentence, wantSentence)
	}

	for _, tt := range []struct{ name, source string }{
		{"a module with no docstring", "x = 1\n"},
		{"a file that does not parse", "def broken(\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.ModuleDocstring(writeFile(t, "mod.py", tt.source))
			if err != nil {
				t.Fatal(err)
			}
			if got != "" {
				t.Fatalf("ModuleDocstring = %q, want empty", got)
			}
		})
	}

	t.Run("a missing file has no docstring", func(t *testing.T) {
		got, err := extractor.ModuleDocstring(filepath.Join(t.TempDir(), "nope.py"))
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Fatalf("ModuleDocstring = %q, want empty", got)
		}
	})
}

func TestSymbolDetails(t *testing.T) {
	hygiene.Isolate(t)
	extractor := newExtractor()

	tests := []struct {
		name             string
		source           string
		symbol           string
		wantNil          bool
		wantParams       []extractors.SymbolParam
		wantReturnType   *string
		wantReturnDocced bool
	}{
		{
			name: "all params documented",
			source: "def greet(name: str, loud: bool = False) -> str:\n" +
				"    \"\"\"Say hello.\n\n    Args:\n        name: The name.\n" +
				"        loud: Whether to shout.\n\n    Returns:\n        The greeting string.\n    \"\"\"\n" +
				"    pass\n",
			symbol: "greet",
			wantParams: []extractors.SymbolParam{
				{Name: "name", Type: strptr("str"), Documented: true},
				{Name: "loud", Type: strptr("bool"), Documented: true},
			},
			wantReturnType:   strptr("str"),
			wantReturnDocced: true,
		},
		{
			name: "an undocumented param is reported as such",
			source: "def process(x: int, y: int, z: int) -> int:\n" +
				"    \"\"\"Process values.\n\n    Args:\n        x: The first value.\n    \"\"\"\n" +
				"    return x + y + z\n",
			symbol: "process",
			wantParams: []extractors.SymbolParam{
				{Name: "x", Type: strptr("int"), Documented: true},
				{Name: "y", Type: strptr("int")},
				{Name: "z", Type: strptr("int")},
			},
			wantReturnType: strptr("int"),
		},
		{
			name: "a returns section marks the return documented",
			source: "def compute(a: int) -> float:\n    \"\"\"Compute something.\n\n" +
				"    Args:\n        a: Input.\n\n    Returns:\n        The result as float.\n    \"\"\"\n" +
				"    return float(a)\n",
			symbol:           "compute",
			wantParams:       []extractors.SymbolParam{{Name: "a", Type: strptr("int"), Documented: true}},
			wantReturnType:   strptr("float"),
			wantReturnDocced: true,
		},
		{
			name:           "no annotations at all",
			source:         "def do_stuff(x):\n    \"\"\"Do stuff.\"\"\"\n    pass\n",
			symbol:         "do_stuff",
			wantParams:     []extractors.SymbolParam{{Name: "x"}},
			wantReturnType: nil,
		},
		{
			name: "self is dropped from a method",
			source: "class MyClass:\n    def method(self, x: int) -> None:\n" +
				"        \"\"\"A method.\n\n        Args:\n            x: The value.\n        \"\"\"\n        pass\n",
			symbol:         "method",
			wantParams:     []extractors.SymbolParam{{Name: "x", Type: strptr("int"), Documented: true}},
			wantReturnType: strptr("None"),
		},
		{
			name: "cls is dropped from a classmethod",
			source: "class MyClass:\n    @classmethod\n    def from_value(cls, v: str) -> \"MyClass\":\n" +
				"        \"\"\"Create from value.\n\n        Args:\n            v: The value.\n        \"\"\"\n        pass\n",
			symbol:         "from_value",
			wantParams:     []extractors.SymbolParam{{Name: "v", Type: strptr("str"), Documented: true}},
			wantReturnType: strptr("'MyClass'"),
		},
		{
			name: "a class reports its constructor's params",
			source: "class Widget:\n    \"\"\"A widget.\"\"\"\n\n" +
				"    def __init__(self, name: str, size: int = 10):\n" +
				"        \"\"\"Create a widget.\n\n        Args:\n            name: The widget name.\n" +
				"            size: The widget size.\n        \"\"\"\n        self.name = name\n",
			symbol: "Widget",
			wantParams: []extractors.SymbolParam{
				{Name: "name", Type: strptr("str"), Documented: true},
				{Name: "size", Type: strptr("int"), Documented: true},
			},
		},
		{
			name:             "a class with no constructor takes no params",
			source:           "class Empty:\n    \"\"\"An empty class.\"\"\"\n    pass\n",
			symbol:           "Empty",
			wantParams:       nil,
			wantReturnDocced: true,
		},
		{
			name: "variadic and keyword params keep their prefixes",
			source: "def flex(*args, **kwargs):\n    \"\"\"Flexible.\n\n    Args:\n" +
				"        *args: Positional arguments.\n        **kwargs: Keyword arguments.\n    \"\"\"\n",
			symbol: "flex",
			wantParams: []extractors.SymbolParam{
				{Name: "*args", Documented: true},
				{Name: "**kwargs", Documented: true},
			},
		},
		{
			name: "a dotted name selects a class member",
			source: "class MyClass:\n    \"\"\"A sample class.\"\"\"\n\n" +
				"    def my_method(self, x: int, y: str = \"hi\") -> bool:\n" +
				"        \"\"\"Do something.\n\n        Args:\n            x: First arg.\n" +
				"            y: Second arg.\n\n        Returns:\n            True if ok.\n        \"\"\"\n" +
				"        return True\n",
			symbol: "MyClass.my_method",
			wantParams: []extractors.SymbolParam{
				{Name: "x", Type: strptr("int"), Documented: true},
				{Name: "y", Type: strptr("str"), Documented: true},
			},
			wantReturnType:   strptr("bool"),
			wantReturnDocced: true,
		},
		{
			name:    "a dotted name whose class is absent",
			source:  "class Other:\n    def method(self):\n        pass\n",
			symbol:  "Missing.method",
			wantNil: true,
		},
		{
			name:    "a dotted name whose member is absent",
			source:  "class MyClass:\n    def existing(self):\n        pass\n",
			symbol:  "MyClass.nonexistent",
			wantNil: true,
		},
		{
			name:    "an absent symbol",
			source:  "def existing():\n    pass\n",
			symbol:  "nonexistent",
			wantNil: true,
		},
		{
			name:    "a file that does not parse",
			source:  "def broken(\n",
			symbol:  "broken",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.SymbolDetails(writeFile(t, "mod.py", tt.source), tt.symbol)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("SymbolDetails = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("SymbolDetails = nil")
			}
			if !sameParams(got.Params, tt.wantParams) {
				t.Errorf("params = %s, want %s", showParams(got.Params), showParams(tt.wantParams))
			}
			if !sameStringPtr(got.ReturnType, tt.wantReturnType) {
				t.Errorf("return type = %v, want %v", showPtr(got.ReturnType), showPtr(tt.wantReturnType))
			}
			if got.ReturnDocumented != tt.wantReturnDocced {
				t.Errorf("return documented = %v, want %v", got.ReturnDocumented, tt.wantReturnDocced)
			}
		})
	}

	t.Run("a missing file has no symbols", func(t *testing.T) {
		got, err := extractor.SymbolDetails(filepath.Join(t.TempDir(), "nope.py"), "anything")
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("SymbolDetails = %#v, want nil", got)
		}
	})
}

// TestAnalysisIsCachedPerFile pins the property that keeps a build from
// spawning one interpreter per question about the same module.
func TestAnalysisIsCachedPerFile(t *testing.T) {
	hygiene.Isolate(t)

	path := writeFile(t, "mod.py", "\"\"\"Doc.\"\"\"\n\n\ndef f():\n    pass\n")
	extractor := New().(*Extractor)

	if _, err := extractor.ModuleDocstring(path); err != nil {
		t.Fatal(err)
	}
	if _, err := extractor.PublicSymbols(path); err != nil {
		t.Fatal(err)
	}

	extractor.mu.Lock()
	count := len(extractor.analyses)
	extractor.mu.Unlock()
	if count != 1 {
		t.Fatalf("cached analyses = %d, want 1", count)
	}
}

func strptr(s string) *string { return &s }

func sameStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameParams(got, want []extractors.SymbolParam) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Name != want[i].Name ||
			got[i].Documented != want[i].Documented ||
			!sameStringPtr(got[i].Type, want[i].Type) {
			return false
		}
	}
	return true
}

func showPtr(p *string) string {
	if p == nil {
		return "<none>"
	}
	return *p
}

func showParams(params []extractors.SymbolParam) string {
	out := "["
	for i, p := range params {
		if i > 0 {
			out += " "
		}
		out += p.Name + ":" + showPtr(p.Type)
		if p.Documented {
			out += "(documented)"
		}
	}
	return out + "]"
}
