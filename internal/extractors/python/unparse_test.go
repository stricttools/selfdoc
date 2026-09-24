package python

import (
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// TestUnparseRenderings is the parity check for the expression renderer: every
// expectation is what CPython's own ast.unparse printed for the same
// expression, recorded rather than reasoned about.
//
// The rendering is reached the way a page reaches it -- through the
// module-level re-export stub an assignment produces -- so the test exercises
// the real path rather than a helper written for it.
func TestUnparseRenderings(t *testing.T) {
	hygiene.Isolate(t)

	cases := []struct{ expression, want string }{
		{`{}`, `{}`},
		{`[1, 2]`, `[1, 2]`},
		{`(1,)`, `(1,)`},
		{`(1, 2)`, `(1, 2)`},
		{`{1, 2}`, `{1, 2}`},
		{`{'a': 1, **rest}`, `{'a': 1, **rest}`},
		{"'it\\'s'", `"it's"`},
		{`"double"`, `'double'`},
		{"b'\\x00\\xff'", "b'\\x00\\xff'"},
		{"rb'\\d+'", "b'\\\\d+'"},
		{`'a' 'b'`, `'ab'`},
		{`1_000`, `1000`},
		{`0x1F`, `31`},
		{`0o17`, `15`},
		{`0b1010`, `10`},
		{`1.5e10`, `15000000000.0`},
		{`-1.5e10`, `-15000000000.0`},
		{`0.0001`, `0.0001`},
		{`0.00001`, `1e-05`},
		{`1e16`, `1e+16`},
		{`1e15`, `1000000000000000.0`},
		{`1j`, `1j`},
		{`1.5e10j`, `15000000000j`},
		{`True`, `True`},
		{`False`, `False`},
		{`None`, `None`},
		{`...`, `...`},
		{`not True`, `not True`},
		{`-x`, `-x`},
		{`+x`, `+x`},
		{`~x`, `~x`},
		{`1 + 2 * 3`, `1 + 2 * 3`},
		{`(1 + 2) * 3`, `(1 + 2) * 3`},
		{`2 ** 3 ** 4`, `2 ** 3 ** 4`},
		{`(2 ** 3) ** 4`, `(2 ** 3) ** 4`},
		{`a | b | c`, `a | b | c`},
		{`a | (b | c)`, `a | (b | c)`},
		{`a and b and c`, `a and b and c`},
		{`a and (b or c)`, `a and (b or c)`},
		{`a or b and c`, `a or (b and c)`},
		{`a < b < c`, `a < b < c`},
		{`a is not b`, `a is not b`},
		{`a not in b`, `a not in b`},
		{`x if y else z`, `x if y else z`},
		{`(x if y else z) if w else v`, `(x if y else z) if w else v`},
		{`lambda: 1`, `lambda: 1`},
		{`lambda x, y=2, *args, **kw: x`, `lambda x, y=2, *args, **kw: x`},
		{`lambda x: (yield)`, `lambda x: (yield)`},
		{`dict[str, int]`, `dict[str, int]`},
		{`Optional[list[tuple[int, ...]]]`, `Optional[list[tuple[int, ...]]]`},
		{`Callable[[int, str], bool]`, `Callable[[int, str], bool]`},
		{`x[1:2]`, `x[1:2]`},
		{`x[::3]`, `x[::3]`},
		{`x[1:2, ::3]`, `x[1:2, ::3]`},
		{`x[(1, 2)]`, `x[1, 2]`},
		{`A.B.C(1, key=2, *args, **kw)`, `A.B.C(1, *args, key=2, **kw)`},
		{`(3571).to_bytes(2, 'little')`, `3571 .to_bytes(2, 'little')`},
		{`f(x for x in y)`, `f((x for x in y))`},
		{`[i for i in y if i]`, `[i for i in y if i]`},
		{`{k: v for k, v in items}`, `{k: v for k, v in items}`},
		{`{i for i in y}`, `{i for i in y}`},
		{`(i for i in y)`, `(i for i in y)`},
		{`*rest`, `*rest`},
		{`await coro()`, `await coro()`},
		{`f'hi {name!r:>{w}} there'`, `f'hi {name!r:>{w}} there'`},
		{`f"it's {x}"`, `f"it's {x}"`},
		{`f'{a}{b}'`, `f'{a}{b}'`},
		{`(x := 1)`, `(x := 1)`},
		{`frozenset[str] | None | str`, `frozenset[str] | None | str`},
		{`int | None`, `int | None`},
		{`-1 ** 2`, `-1 ** 2`},
	}

	for _, c := range cases {
		t.Run(c.expression, func(t *testing.T) {
			if got := renderAssignedValue(t, c.expression); got != c.want {
				t.Errorf("unparse(%s) = %s, want %s", c.expression, got, c.want)
			}
		})
	}
}

// renderAssignedValue parses "x = <expression>" and reports how the value was
// rendered.
func renderAssignedValue(t *testing.T, expression string) string {
	t.Helper()
	parsed, err := parseDocument("<test>", []byte("x = "+expression+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SyntaxError != nil {
		t.Fatalf("the expression did not parse: %s", *parsed.SyntaxError)
	}
	if len(parsed.Reexports) != 1 {
		t.Fatalf("the assignment produced %d stubs", len(parsed.Reexports))
	}
	return strings.TrimPrefix(parsed.Reexports[0].Stub, "x = ")
}

// fixtureSource is the module TestDeclarationRenderings reads. Every
// expectation below it is what the Python implementation printed for this
// exact text.
const fixtureSource = `"""Module.

    An indented continuation, and trailing blank lines.

"""


@dataclass
class Widget(Base, metaclass=Meta):

    size: int = 3
    label: str | None = None

    async def resize(self, factor: float=1.0, /, *scales: int, strict: bool=True, **rest: Any) -> "Widget":
        """Resize."""


class Plain:
    pass
`

// TestDeclarationRenderings pins the signature line, the class line, the field
// table's values, the line spans and the docstring normalization.
func TestDeclarationRenderings(t *testing.T) {
	hygiene.Isolate(t)

	parsed, err := parseDocument("fixture.py", []byte(fixtureSource))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SyntaxError != nil {
		t.Fatalf("the fixture did not parse: %s", *parsed.SyntaxError)
	}

	wantDoc := "Module.\n\nAn indented continuation, and trailing blank lines."
	if parsed.Docstring == nil || *parsed.Docstring != wantDoc {
		t.Errorf("module docstring = %q, want %q", showPtr(parsed.Docstring), wantDoc)
	}
	if len(parsed.Declarations) != 2 {
		t.Fatalf("declarations = %d, want 2", len(parsed.Declarations))
	}

	widget := parsed.Declarations[0]
	if !widget.IsDataclass {
		t.Error("the @dataclass decorator was not recognized")
	}
	if widget.IsPydantic {
		t.Error("a dataclass was reported as a pydantic model")
	}
	if got, want := widget.ClassSignature, "class Widget(Base, metaclass=Meta):"; got != want {
		t.Errorf("class signature = %q, want %q", got, want)
	}
	if widget.Lineno != 9 || widget.EndLineno != 15 {
		t.Errorf("class span = %d-%d, want 9-15", widget.Lineno, widget.EndLineno)
	}
	wantFields := []field{
		{Name: "size", Type: "int", Default: "3", Lineno: 11},
		{Name: "label", Type: "str | None", Default: "None", Lineno: 12},
	}
	if len(widget.Fields) != len(wantFields) {
		t.Fatalf("fields = %#v", widget.Fields)
	}
	for i, want := range wantFields {
		if widget.Fields[i] != want {
			t.Errorf("field %d = %#v, want %#v", i, widget.Fields[i], want)
		}
	}

	if len(widget.Members) != 1 {
		t.Fatalf("members = %d, want 1", len(widget.Members))
	}
	resize := widget.Members[0]
	if !resize.IsAsync {
		t.Error("the async keyword was lost")
	}
	if resize.documented() != "Resize." {
		t.Errorf("method docstring = %q", resize.documented())
	}
	wantSignature := "(self, factor: float=1.0, /, *scales: int, strict: bool=True, " +
		"**rest: Any) -> 'Widget'"
	if resize.Signature != wantSignature {
		t.Errorf("signature = %q, want %q", resize.Signature, wantSignature)
	}
	if resize.ReturnType == nil || *resize.ReturnType != "'Widget'" {
		t.Errorf("return type = %q", showPtr(resize.ReturnType))
	}
	if resize.Lineno != 14 || resize.EndLineno != 15 {
		t.Errorf("method span = %d-%d, want 14-15", resize.Lineno, resize.EndLineno)
	}
	wantParams := []paramInfo{
		{Name: "factor", Type: strptr("float")},
		{Name: "*scales", Type: strptr("int")},
		{Name: "strict", Type: strptr("bool")},
		{Name: "**rest", Type: strptr("Any")},
	}
	if len(resize.Params) != len(wantParams) {
		t.Fatalf("params = %#v", resize.Params)
	}
	for i, want := range wantParams {
		if resize.Params[i].Name != want.Name ||
			!sameStringPtr(resize.Params[i].Type, want.Type) {
			t.Errorf("param %d = %q %q, want %q %q",
				i, resize.Params[i].Name, showPtr(resize.Params[i].Type),
				want.Name, showPtr(want.Type))
		}
	}

	plain := parsed.Declarations[1]
	if got, want := plain.ClassSignature, "class Plain:"; got != want {
		t.Errorf("class signature = %q, want %q", got, want)
	}
	if plain.Lineno != 18 || plain.EndLineno != 19 {
		t.Errorf("class span = %d-%d, want 18-19", plain.Lineno, plain.EndLineno)
	}
}

// TestADanglingDecoratorIsASyntaxError pins the one refusal the grammar does
// not make for us: it accepts a decorator that no definition follows, and
// CPython does not, so a snippet that ends with a bare @decorator line is
// reported rather than silently accepted.
func TestADanglingDecoratorIsASyntaxError(t *testing.T) {
	hygiene.Isolate(t)

	source := "def positive(value):\n    return value > 0\n\n@flag(\"port\", validate=positive)\n"
	line, failed, err := FirstSyntaxErrorLine([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Fatal("a decorator with nothing to decorate was accepted")
	}
	if line != 4 {
		t.Errorf("line = %d, want 4", line)
	}

	line, failed, err = FirstSyntaxErrorLine([]byte("@flag(\"port\")\ndef run():\n    pass\n"))
	if err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Errorf("a decorated definition was reported as a syntax error at line %d", line)
	}
}

// TestSyntaxErrorNamesTheFileAndLine pins what a file that does not parse
// answers, which every directive turns into its own marker.
//
// The message is not the interpreter's -- no parser but CPython's produces
// those strings -- but its shape is, so a page that reports one reads the same.
func TestSyntaxErrorNamesTheFileAndLine(t *testing.T) {
	hygiene.Isolate(t)

	parsed, err := parseDocument("bad.py", []byte("def ok():\n    pass\n\n\ndef broken(\n"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SyntaxError == nil {
		t.Fatal("a file that does not parse reported no error")
	}
	if got, want := *parsed.SyntaxError, "invalid syntax (bad.py, line 5)"; got != want {
		t.Errorf("syntax error = %q, want %q", got, want)
	}
	if len(parsed.Declarations) != 0 {
		t.Error("a file that does not parse reported declarations")
	}
}

// TestReexportAndConstantRenderings pins the module-level statements a package
// page shows: the import lines, the constants, and the two nesting levels the
// reader descends into.
func TestReexportAndConstantRenderings(t *testing.T) {
	hygiene.Isolate(t)

	const source = `from __future__ import annotations
from . import sibling
from ..pkg.sub import thing as renamed
from .impl import *
try:
    from ._fast import Engine
except ImportError:
    Engine = None
if TYPE_CHECKING:
    from .types import Shape
elif OTHER:
    from .types import Hidden
VERSION: str = "1.2.3"
A = B = 5
HELP = """Usage: tool [options]"""
`
	parsed, err := parseDocument("pkg.py", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SyntaxError != nil {
		t.Fatalf("the fixture did not parse: %s", *parsed.SyntaxError)
	}

	want := []reexport{
		{Name: "annotations", Stub: "from __future__ import annotations"},
		{Name: "sibling", Stub: "from . import sibling"},
		{Name: "renamed", Stub: "from ..pkg.sub import thing as renamed"},
		{Name: "Engine", Stub: "from ._fast import Engine"},
		{Name: "Engine", Stub: "Engine = None"},
		{Name: "Shape", Stub: "from .types import Shape"},
		{Name: "VERSION", Stub: "VERSION: str = '1.2.3'"},
		{Name: "A", Stub: "A = B = 5"},
		{Name: "B", Stub: "A = B = 5"},
		{Name: "HELP", Stub: "HELP = 'Usage: tool [options]'"},
	}
	if len(parsed.Reexports) != len(want) {
		t.Fatalf("reexports = %#v", parsed.Reexports)
	}
	for i, entry := range want {
		if parsed.Reexports[i] != entry {
			t.Errorf("reexport %d = %#v, want %#v", i, parsed.Reexports[i], entry)
		}
	}

	if len(parsed.CLIConstants) != 1 ||
		parsed.CLIConstants[0] != (cliConstant{Name: "HELP", Value: "Usage: tool [options]"}) {
		t.Errorf("cli constants = %#v", parsed.CLIConstants)
	}
}
