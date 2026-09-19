package golang

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/smm-h/stricttest/go/hygiene"
)

// Every expectation in this file is a recorded observation of the Python
// implementation's own answer for the same source.

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
			name: "functions, types, vars and consts, exported only",
			source: "package main\n\nfunc Hello() {}\nfunc hello() {}\n" +
				"type Config struct {}\ntype config struct {}\n" +
				"var MaxRetries int\nconst DefaultTimeout = 30\n",
			want: []string{"Hello", "Config", "MaxRetries", "DefaultTimeout"},
		},
		{
			name:   "a const block's members",
			source: "package exitcodes\n\nconst (\n\tExitSuccess = 0\n\tExitGeneral = 1\n)\n",
			want:   []string{"ExitSuccess", "ExitGeneral"},
		},
		{
			name:   "a var block's members",
			source: "package config\n\nvar (\n\tDefaultTimeout int = 30\n\tMaxRetries int = 3\n)\n",
			want:   []string{"DefaultTimeout", "MaxRetries"},
		},
		{
			name: "single and block forms together",
			source: "package main\n\nconst SingleConst = 42\n\n" +
				"const (\n\tBlockConst1 = 1\n\tBlockConst2 = 2\n)\n\n" +
				"var SingleVar int\n\nvar (\n\tBlockVar1 string = \"hello\"\n)\n\n" +
				"func ExportedFunc() {}\n",
			want: []string{"SingleConst", "BlockConst1", "BlockConst2", "SingleVar", "BlockVar1", "ExportedFunc"},
		},
		{
			name:   "an unexported member of a block is skipped",
			source: "package exitcodes\n\nconst (\n\texitSuccess = 0\n\tExitGeneral = 1\n)\n",
			want:   []string{"ExitGeneral"},
		},
		{
			name: "comments inside a block are not members",
			source: "package codes\n\nconst (\n\t// ExitSuccess is the success code.\n" +
				"\tExitSuccess = 0\n\t// internal comment\n\tExitGeneral = 1\n)\n",
			want: []string{"ExitSuccess", "ExitGeneral"},
		},
		{
			name:   "iota and blank members are not symbols",
			source: "package main\n\nconst (\n\t_ = iota\n\tExitSuccess\n\tExitGeneral\n\t_reserved\n)\n",
			want:   []string{"ExitSuccess", "ExitGeneral"},
		},
		{
			name:   "a method is named by its receiver",
			source: "package main\n\ntype Server struct{}\n\nfunc (s *Server) Handle() {}\n",
			want:   []string{"Server", "Server.Handle"},
		},
		{
			name: "two types' methods of the same name do not collide",
			source: "package main\n\ntype Server struct{}\nfunc (s *Server) Run() {}\n\n" +
				"type Client struct{}\nfunc (c *Client) Run() {}\n",
			want: []string{"Server", "Server.Run", "Client", "Client.Run"},
		},
		{
			name:   "a declaration inside a block comment is skipped",
			source: "package main\n\n/*\nfunc NotReal() {}\n*/\nfunc Real() {}\n",
			want:   []string{"Real"},
		},
		{
			name:   "an inline block comment is stripped",
			source: "package main\n\nfunc /*x*/ Inline() {}\n",
			want:   []string{"Inline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.PublicSymbols(writeFile(t, "example.go", tt.source))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("PublicSymbols = %#v, want %#v", got, tt.want)
			}
		})
	}

	t.Run("a whole package file", func(t *testing.T) {
		got, err := extractor.PublicSymbols(filepath.Join(fixture(t), "internal/commit/commit.go"))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{
			"ExitCASExhausted", "ExitWriteTree", "DefaultTimeout",
			"CommitError", "CommitError.Error", "Pipeline", "Pipeline.Execute", "NewPipeline",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("PublicSymbols = %#v, want %#v", got, want)
		}
	})

	t.Run("a missing file has no symbols", func(t *testing.T) {
		got, err := extractor.PublicSymbols(filepath.Join(t.TempDir(), "nope.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("PublicSymbols = %#v, want none", got)
		}
	})
}

func TestSymbolDetails(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()
	packageDir := filepath.Join(base, "pkg")

	tests := []struct {
		name             string
		symbol           string
		wantNil          bool
		wantParams       []extractors.SymbolParam
		wantReturnType   *string
		wantReturnDocced bool
	}{
		{
			name:   "grouped params share the type written once",
			symbol: "Merge",
			wantParams: []extractors.SymbolParam{
				{Name: "a", Type: strptr("int"), Documented: true},
				{Name: "b", Type: strptr("int"), Documented: true},
				{Name: "sep", Type: strptr("string"), Documented: true},
			},
			wantReturnType: strptr("string"),
		},
		{
			name:             "a method's receiver is not a parameter",
			symbol:           "Handle",
			wantParams:       []extractors.SymbolParam{{Name: "req", Type: strptr("*http.Request")}},
			wantReturnType:   strptr("(int, error)"),
			wantReturnDocced: true,
		},
		{
			name:   "a variadic param keeps its ellipsis",
			symbol: "Printf",
			wantParams: []extractors.SymbolParam{
				{Name: "format", Type: strptr("string")},
				{Name: "args", Type: strptr("...interface{}")},
			},
			wantReturnType: nil,
		},
		{
			name:           "no return type",
			symbol:         "NoDoc",
			wantParams:     []extractors.SymbolParam{{Name: "x", Type: strptr("int")}},
			wantReturnType: nil,
		},
		{
			name:             "a dotted name selects the method of that receiver",
			symbol:           "Server.Handle",
			wantParams:       []extractors.SymbolParam{{Name: "req", Type: strptr("*http.Request")}},
			wantReturnType:   strptr("(int, error)"),
			wantReturnDocced: true,
		},
		{
			name:    "a dotted name with the wrong receiver",
			symbol:  "WrongType.Handle",
			wantNil: true,
		},
		{
			name:    "an absent symbol",
			symbol:  "DoesNotExist",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.SymbolDetails(packageDir, tt.symbol)
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
				t.Errorf("return type = %s, want %s", showPtr(got.ReturnType), showPtr(tt.wantReturnType))
			}
			if got.ReturnDocumented != tt.wantReturnDocced {
				t.Errorf("return documented = %v, want %v", got.ReturnDocumented, tt.wantReturnDocced)
			}
		})
	}

	t.Run("a value receiver and no params", func(t *testing.T) {
		dir := t.TempDir()
		source := "package math\n\n// Vec2 is a 2D vector.\ntype Vec2 struct {\n    X, Y float64\n}\n\n" +
			"// Length returns the magnitude of the vector.\nfunc (v Vec2) Length() float64 {\n    return 0\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "vec.go"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := extractor.SymbolDetails(dir, "Vec2.Length")
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatal("SymbolDetails = nil")
		}
		if len(got.Params) != 0 {
			t.Errorf("params = %s, want none", showParams(got.Params))
		}
		if !sameStringPtr(got.ReturnType, strptr("float64")) {
			t.Errorf("return type = %s, want float64", showPtr(got.ReturnType))
		}
	})

	t.Run("a single file rather than a directory", func(t *testing.T) {
		got, err := extractor.SymbolDetails(filepath.Join(packageDir, "server.go"), "Merge")
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || len(got.Params) != 3 {
			t.Fatalf("SymbolDetails = %#v", got)
		}
	})

	t.Run("a path that is neither a file nor a directory", func(t *testing.T) {
		got, err := extractor.SymbolDetails(filepath.Join(base, "nope"), "Merge")
		if err != nil || got != nil {
			t.Fatalf("SymbolDetails = (%#v, %v), want nil", got, err)
		}
	})
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
