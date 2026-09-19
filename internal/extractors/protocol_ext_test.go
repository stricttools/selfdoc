package extractors_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/extractors/golang"
	"github.com/stricttools/selfdoc/internal/extractors/python"
)

// This file exercises the registry with real language packages linked in,
// which is what an in-package test cannot do: the language packages import the
// registry, so only an external test package can import them back.
//
// Two of the nine languages exist so far. As each of the remaining seven is
// ported, add its import here and extend the tables below -- the detection
// cases in particular, since a known language whose package is not linked in
// is reported as an error rather than skipped.

// compileTimeAssertions pin that both ported extractors satisfy the protocol.
var (
	_ extractors.Extractor = python.New()
	_ extractors.Extractor = golang.New()
)

func TestRegisteredLanguages(t *testing.T) {
	registered := map[string]bool{}
	for _, name := range extractors.Registered() {
		registered[name] = true
	}
	for _, name := range []string{"python", "go"} {
		if !registered[name] {
			t.Errorf("%q is not registered even though its package is imported here", name)
		}
	}
}

func TestLookupBuildsTheRightExtractor(t *testing.T) {
	for _, name := range []string{"python", "go"} {
		extractor, ok, err := extractors.Lookup(name)
		if err != nil || !ok {
			t.Fatalf("Lookup(%q) = (_, %v, %v)", name, ok, err)
		}
		if extractor.Name() != name {
			t.Errorf("Lookup(%q) built an extractor named %q", name, extractor.Name())
		}
	}
}

func TestDetectLanguageWithRealPackages(t *testing.T) {
	tests := []struct {
		name    string
		markers []string
		want    string
	}{
		{"python", []string{"pyproject.toml"}, "python"},
		{"python via setup.py", []string{"setup.py"}, "python"},
		{"go", []string{"go.mod"}, "go"},
		{"python beats go", []string{"pyproject.toml", "go.mod"}, "python"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, marker := range tt.markers {
				if err := os.WriteFile(filepath.Join(dir, marker), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := extractors.DetectLanguage(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("DetectLanguage = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetectionReportsAnUnlinkedLanguage pins the strictness the registry was
// built for: reaching a known language whose package is absent is a wiring
// mistake, and it is reported rather than skipped over.
func TestDetectionReportsAnUnlinkedLanguage(t *testing.T) {
	_, err := extractors.DetectLanguage(t.TempDir())
	if err == nil {
		t.Skip("every known language is linked in now: this test has served its purpose")
	}
	if !strings.Contains(err.Error(), "not linked into this binary") {
		t.Fatalf("error does not name the cause: %v", err)
	}
}

func TestResolveSourceEntriesWithRealPackages(t *testing.T) {
	config := map[string]any{"source": []any{
		map[string]any{"path": "mylib/", "language": "python"},
		map[string]any{"path": ".", "language": "go"},
		map[string]any{"path": "lib/", "language": "ruby"},
	}}

	entries, err := extractors.ResolveSourceEntries(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Extractor.Name() != "python" || entries[1].Extractor.Name() != "go" {
		t.Errorf("entries resolved to %q and %q",
			entries[0].Extractor.Name(), entries[1].Extractor.Name())
	}

	// An unsupported language gets the stub, which renders its own marker.
	markdown, err := entries[2].Extractor.Extract("ref", map[string]string{"path": "X"}, nil, nil, ".")
	if err != nil {
		t.Fatal(err)
	}
	if want := "> *[selfdoc: no extractor for 'ruby']*"; markdown != want {
		t.Fatalf("stub extract = %q, want %q", markdown, want)
	}
}

// TestFileExtensionsAreDisjoint pins that no two languages claim the same
// extension, which is what lets a file's extension pick its extractor.
func TestFileExtensionsAreDisjoint(t *testing.T) {
	owner := map[string]string{}
	for _, name := range extractors.Registered() {
		extractor, ok, err := extractors.Lookup(name)
		if err != nil || !ok {
			t.Fatalf("Lookup(%q) = (_, %v, %v)", name, ok, err)
		}
		for _, ext := range extractor.FileExtensions() {
			if previous, taken := owner[ext]; taken {
				t.Errorf("%q is claimed by both %q and %q", ext, previous, name)
			}
			owner[ext] = name
		}
	}
}
