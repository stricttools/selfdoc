package check

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// twoTierProject writes a project whose sources export three symbols across
// two modules, with no pages: each case writes the pages it needs.
func twoTierProject(t *testing.T) string {
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
`)
	write(t, filepath.Join(root, "mylib", "utils.py"),
		`"""Utility functions."""

def helper():
    """Help."""
    pass
`)
	write(t, filepath.Join(root, ".stricttools", "docs", ".keep"), "")
	return root
}

// skeletonPage is a generated, machine-seeded page referencing one module.
func skeletonPage(title, modulePath string) string {
	return "+++\ntitle = \"" + title + "\"\ndescription = \"" + title +
		"\"\ngenerated = true\nseeded = true\n+++\n# " + title + "\n\n" +
		":-: ref path=\"" + modulePath + "\"\n"
}

func TestTwoTierCoverage(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		pages          map[string]string
		wantTotal      int
		wantReferenced int
		wantDocumented int
	}{
		{
			name: "a hand-written page documents what it references",
			pages: map[string]string{
				"api.md": "+++\ndescription = \"Comprehensive guide to the API with examples.\"\n+++\n" +
					"# API\n\n:-: ref path=\"mylib\"\n\n:-: ref path=\"mylib.utils\"\n",
			},
			wantTotal: 3, wantReferenced: 3, wantDocumented: 3,
		},
		{
			name: "a skeleton page references without documenting",
			pages: map[string]string{
				"mylib.md":       skeletonPage("mylib", "mylib"),
				"mylib-utils.md": skeletonPage("mylib.utils", "mylib.utils"),
			},
			wantTotal: 3, wantReferenced: 3, wantDocumented: 0,
		},
		{
			name: "a mix counts each page on its own terms",
			pages: map[string]string{
				"mylib.md": skeletonPage("mylib", "mylib"),
				"utils.md": "+++\ndescription = \"Everything the utility module offers a caller.\"\n+++\n" +
					"# Utils\n\n:-: ref path=\"mylib.utils\"\n",
			},
			wantTotal: 3, wantReferenced: 3, wantDocumented: 1,
		},
		{
			name: "a generated page with a customized description documents",
			pages: map[string]string{
				"mylib.md": "+++\ntitle = \"mylib\"\ndescription = \"A hand-written account of " +
					"what this module is for.\"\ngenerated = true\n+++\n# mylib\n\n" +
					":-: ref path=\"mylib\"\n",
			},
			wantTotal: 3, wantReferenced: 2, wantDocumented: 2,
		},
		{
			name:      "no pages references nothing",
			pages:     map[string]string{},
			wantTotal: 3, wantReferenced: 0, wantDocumented: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := twoTierProject(t)
			for relPath, content := range testCase.pages {
				write(t, filepath.Join(root, ".stricttools", "docs", relPath), content)
			}
			result := checkFixture(t, root)
			if result.Coverage == nil {
				t.Fatal("coverage was not measured")
			}
			if result.Coverage.Total != testCase.wantTotal {
				t.Errorf("total = %d, want %d", result.Coverage.Total, testCase.wantTotal)
			}
			if result.Coverage.ReferencedCount != testCase.wantReferenced {
				t.Errorf("referenced = %d, want %d; symbols %v",
					result.Coverage.ReferencedCount, testCase.wantReferenced,
					result.Coverage.ReferencedSymbols)
			}
			if result.Coverage.DocumentedCount != testCase.wantDocumented {
				t.Errorf("documented = %d, want %d; symbols %v",
					result.Coverage.DocumentedCount, testCase.wantDocumented,
					result.Coverage.DocumentedSymbols)
			}
		})
	}
}

func TestCoverageExcludesTestFiles(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		files     map[string]string
		wantTotal int
	}{
		{
			name: "a Python test module",
			files: map[string]string{
				"mylib/test_helpers.py": "\"\"\"Tests.\"\"\"\n\ndef test_it():\n    pass\n",
				"mylib/conftest.py":     "\"\"\"Fixtures.\"\"\"\n\ndef fixture_it():\n    pass\n",
			},
			wantTotal: 3,
		},
		{
			name: "a tests directory",
			files: map[string]string{
				"mylib/tests/helpers.py": "\"\"\"Helpers.\"\"\"\n\ndef helper_two():\n    pass\n",
			},
			wantTotal: 3,
		},
		{
			name: "a test and a __tests__ directory",
			files: map[string]string{
				"mylib/test/one.py":      "\"\"\"One.\"\"\"\n\ndef one():\n    pass\n",
				"mylib/__tests__/two.py": "\"\"\"Two.\"\"\"\n\ndef two():\n    pass\n",
			},
			wantTotal: 3,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := twoTierProject(t)
			for relPath, content := range testCase.files {
				write(t, filepath.Join(root, relPath), content)
			}
			result := checkFixture(t, root)
			if result.Coverage == nil {
				t.Fatal("coverage was not measured")
			}
			if result.Coverage.Total != testCase.wantTotal {
				t.Errorf("total = %d, want %d; unreferenced %v",
					result.Coverage.Total, testCase.wantTotal,
					result.Coverage.UnreferencedSymbols)
			}
		})
	}
}

func TestCoverageRespectsGenExclude(t *testing.T) {
	root := twoTierProject(t)
	config := pythonProjectConfig()
	config["gen"] = map[string]any{"exclude": []any{"mylib.utils"}}
	writeConfig(t, root, config)

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.Total != 2 {
		t.Errorf("total = %d, want 2 (the excluded module's symbol is not counted): %v",
			result.Coverage.Total, result.Coverage.UnreferencedSymbols)
	}
}

func TestGoCoverageIsMeasuredPerPackage(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "pkg/", "language": "go"},
	))
	write(t, filepath.Join(root, "pkg", "handler.go"),
		`// Package pkg handles things.
package pkg

// Handle handles one thing.
func Handle(name string) string { return name }

// Widget is a widget.
type Widget struct{}
`)
	write(t, filepath.Join(root, "pkg", "handler_test.go"),
		`package pkg

// TestHandle is a test and is not part of the public surface.
func TestHandle(t *testing.T) {}
`)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"+++\ndescription = \"Every exported name of the handler package, in one page.\"\n+++\n"+
			"# API\n\n:-: ref path=\"pkg\"\n")

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.Total != 2 {
		t.Errorf("total = %d, want 2 (Handle and Widget): referenced %v, unreferenced %v",
			result.Coverage.Total, result.Coverage.ReferencedSymbols,
			result.Coverage.UnreferencedSymbols)
	}
	if result.Coverage.ReferencedCount != 2 {
		t.Errorf("referenced = %d, want 2: unreferenced %v",
			result.Coverage.ReferencedCount, result.Coverage.UnreferencedSymbols)
	}
}

func TestGoCoverageSkipsToolchainIgnoredDirectories(t *testing.T) {
	// A directory the Go toolchain ignores is not part of the package
	// graph, so its exported names are not a public surface coverage can
	// hold the docs to -- and no page is generated for one either.
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "pkg/", "language": "go"},
	))
	write(t, filepath.Join(root, "pkg", "handler.go"),
		`// Package pkg handles things.
package pkg

// Handle handles one thing.
func Handle(name string) string { return name }
`)
	write(t, filepath.Join(root, "pkg", "testdata", "fixture", "fixture.go"),
		`// Package fixture is test data.
package fixture

// Fixture is not a public symbol of this project.
func Fixture() {}
`)
	write(t, filepath.Join(root, "pkg", "vendor", "dep", "dep.go"),
		`// Package dep is vendored.
package dep

// Dep is not a public symbol of this project.
func Dep() {}
`)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"+++\ndescription = \"Every exported name of the handler package, in one page.\"\n+++\n"+
			"# API\n\n:-: ref path=\"pkg\"\n")

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.Total != 1 {
		t.Errorf("total = %d, want 1 (Handle alone): referenced %v, unreferenced %v",
			result.Coverage.Total, result.Coverage.ReferencedSymbols,
			result.Coverage.UnreferencedSymbols)
	}
}

func TestTypeScriptCoverageSkipsSpecFiles(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "src/", "language": "typescript"},
	))
	write(t, filepath.Join(root, "src", "index.ts"),
		`/** Greets someone. */
export function greet(name: string): string { return name; }

/** A widget. */
export class Widget {}
`)
	write(t, filepath.Join(root, "src", "index.spec.ts"),
		`export function specHelper(): void {}
`)
	write(t, filepath.Join(root, "src", "index.test.ts"),
		`export function testHelper(): void {}
`)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"+++\ndescription = \"Every exported name of the source module, in one page.\"\n+++\n"+
			"# API\n\n:-: ref path=\"src/index\"\n")

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.Total != 2 {
		t.Errorf("total = %d, want 2 (greet and Widget): %v",
			result.Coverage.Total, result.Coverage.UnreferencedSymbols)
	}
}

func TestLANG001ForUnsupportedLanguage(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "src/", "language": "cobol"},
	))
	write(t, filepath.Join(root, "src", ".keep"), "")
	write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
		"+++\ndescription = \"A guide covering everything the project does for a reader.\"\n+++\n"+
			"# Guide\n\nText.\n")

	result := checkFixture(t, root)

	if !hasCode(result.Lints, "LANG001") {
		t.Fatalf("LANG001 missing; got %v", codes(result.Lints))
	}
	diagnostic := withCode(result.Lints, "LANG001")[0]
	if diagnostic.Severity() != "error" {
		t.Errorf("severity = %q, want error", diagnostic.Severity())
	}
}

func TestSupportedLanguageHasNoLANG001(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
		"+++\ndescription = \"A guide covering everything the project does for a reader.\"\n+++\n"+
			"# Guide\n\nText.\n")

	result := checkFixture(t, root)

	if hasCode(result.Lints, "LANG001") {
		t.Error("LANG001 fired for a language selfdoc supports")
	}
}

func TestXREF002MissingSourceFile(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"+++\ndescription = \"Every public function of the library, with its signature.\"\n+++\n"+
			"# API\n\n:-: ref path=\"mylib\"\n")

	result := checkFixture(t, root)

	if hasCode(result.Lints, "XREF002") {
		t.Errorf("XREF002 fired for a path that is on disk: %v",
			messagesOf(withCode(result.Lints, "XREF002")))
	}
}

// TestSkeletonOnlySymbolsNameThePageAndTheSeededDescription asserts that the
// report's skeleton-only section names the page whose description is the cause
// and the edit that fixes it.
//
// The cause of a skeleton-only symbol is a property of the PAGE -- its
// frontmatter declares `generated = true` and `seeded = true`, so its
// description is the one selfdoc emitted and nobody rewrote -- and not a
// property of the symbol's own doc comment. A section that lists only symbols
// reads as a list of under-documented code, and sends the reader to rewrite
// doc comments that were already complete.
func TestSkeletonOnlySymbolsNameThePageAndTheSeededDescription(t *testing.T) {
	root := twoTierProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "mylib.md"),
		skeletonPage("mylib", "mylib"))
	write(t, filepath.Join(root, ".stricttools", "docs", "mylib-utils.md"),
		skeletonPage("mylib.utils", "mylib.utils"))
	result := checkFixture(t, root)
	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}

	var out bytes.Buffer
	PrintResults(&out, result, false)
	report := out.String()

	index := strings.Index(report, "Skeleton-only symbols:")
	if index < 0 {
		t.Fatalf("the report has no skeleton-only section:\n%s", report)
	}
	// The section runs to the blank line that opens the lint block, so the
	// assertions below cannot be satisfied by text elsewhere in the report.
	section := report[index:]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end]
	}
	report = section
	for _, want := range []string{
		// The pages whose descriptions are the cause.
		"mylib.md",
		"mylib-utils.md",
		// The property that made them skeletons, spelled as the page
		// declares it.
		"seeded = true",
		// What to edit, and what not to.
		"description",
		"doc comment",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the skeleton-only section does not say %q:\n%s", want, report)
		}
	}
}
