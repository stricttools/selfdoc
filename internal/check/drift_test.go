package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/ownership"
)

// driftProject writes a one-module project whose module docstring and page
// description are the given ones. Re-calling it rewrites both.
func driftProject(t *testing.T, root, docstring, description string) {
	t.Helper()
	writeConfig(t, root, pythonProjectConfig())
	write(t, filepath.Join(root, "mylib", "__init__.py"),
		`"""`+docstring+`"""

def greet(name):
    """Say hello."""
    return f'Hello, {name}'
`)
	write(t, filepath.Join(root, "stricttools", "docs", "mylib.md"),
		"+++\ndescription = \""+description+"\"\n+++\n# My Library\n\n"+
			":-: ref path=\"mylib\"\n")
}

func TestDRIFT001SourceDocstringChanged(t *testing.T) {
	isolate(t)
	root := t.TempDir()

	driftProject(t, root, "Original docstring.", "Library docs")
	if got := withCode(checkFixture(t, root).Lints, "DRIFT001"); len(got) != 0 {
		t.Fatalf("the first run reported drift: %v", messagesOf(got))
	}

	driftProject(t, root, "Completely rewritten docstring.", "Library docs")
	drift := withCode(checkFixture(t, root).Lints, "DRIFT001")
	if len(drift) != 1 {
		t.Fatalf("DRIFT001 count = %d, want 1: %v", len(drift), messagesOf(drift))
	}
	if !strings.Contains(drift[0].Message(), "source docstrings changed") {
		t.Errorf("message = %q", drift[0].Message())
	}
	if drift[0].Severity() != "error" {
		t.Errorf("severity = %q, want error", drift[0].Severity())
	}
	// The remedy the message names has to be the command that clears it.
	if !strings.Contains(drift[0].Message(), "selfdoc baseline accept mylib.md") {
		t.Errorf("message = %q, want it to name the accept command for this page",
			drift[0].Message())
	}
}

func TestDRIFT001HintNamesAWorkingRemedy(t *testing.T) {
	isolate(t)
	root := t.TempDir()

	driftProject(t, root, "Original docstring.", "Library docs")
	checkFixture(t, root)
	driftProject(t, root, "Completely rewritten docstring.", "Library docs")
	drift := withCode(checkFixture(t, root).Lints, "DRIFT001")
	if len(drift) != 1 {
		t.Fatalf("DRIFT001 count = %d, want 1", len(drift))
	}

	// Perform the remedy the diagnostic prescribes and assert the error
	// clears: a prescription nobody executes is how a remedy that does not
	// work ships.
	if _, err := AcceptBaselines([]string{drift[0].File()}, root, nil, handle()); err != nil {
		t.Fatalf("the prescribed accept failed: %v", err)
	}
	if got := withCode(checkFixture(t, root).Lints, "DRIFT001"); len(got) != 0 {
		t.Errorf("drift persisted after the prescribed remedy: %v", messagesOf(got))
	}
}

// dualDriftProject writes a two-module project with one hand-written page and
// one machine-owned page, each referencing its own module.
func dualDriftProject(t *testing.T, root, firstDoc, secondDoc string) {
	t.Helper()
	writeConfig(t, root, pythonProjectConfig())
	write(t, filepath.Join(root, "mylib", "__init__.py"), `"""`+firstDoc+`"""`+"\n")
	write(t, filepath.Join(root, "mylib", "other.py"), `"""`+secondDoc+`"""`+"\n")
	write(t, filepath.Join(root, "stricttools", "docs", "mylib.md"),
		"+++\ndescription = \"Hand-written index\"\n+++\n# My Library\n\n"+
			":-: ref path=\"mylib\"\n")
	// The machine-owned page carries the current module template as its
	// description, which the ownership predicate recognizes with no
	// recorded seed hash.
	machineDescription := strings.ReplaceAll(
		ownership.ModuleDescTemplate, "{module}", "mylib.other",
	)
	write(t, filepath.Join(root, "stricttools", "docs", "mylib-other.md"),
		"+++\ntitle = \"mylib.other\"\ndescription = \""+machineDescription+"\"\n"+
			"generated = true\nseeded = true\n+++\n# mylib.other\n\n"+
			":-: ref path=\"mylib.other\"\n")
}

func TestDRIFT001SkeletonExemptionIsScoped(t *testing.T) {
	isolate(t)
	root := t.TempDir()

	dualDriftProject(t, root, "Orig one.", "Orig two.")
	checkFixture(t, root)

	dualDriftProject(t, root, "Rewritten one.", "Rewritten two.")
	driftFiles := map[string]bool{}
	for _, diagnostic := range withCode(checkFixture(t, root).Lints, "DRIFT001") {
		driftFiles[diagnostic.File()] = true
	}
	if !driftFiles["mylib.md"] {
		t.Error("the hand-written page did not report drift")
	}
	if driftFiles["mylib-other.md"] {
		t.Error("the machine-owned page reported drift, which it cannot fix")
	}

	// The exempt page's baseline advanced, so a re-check keeps it clean.
	for _, diagnostic := range withCode(checkFixture(t, root).Lints, "DRIFT001") {
		if diagnostic.File() == "mylib-other.md" {
			t.Error("the machine-owned page's baseline did not advance")
		}
	}
}

func TestDRIFT001Silences(t *testing.T) {
	for _, testCase := range []struct {
		name string
		// setup writes the project's first state, and rewrite its
		// second; a nil rewrite means nothing changes between runs.
		setup   func(t *testing.T, root string)
		rewrite func(t *testing.T, root string)
	}{
		{
			name: "both the docstring and the description changed",
			setup: func(t *testing.T, root string) {
				driftProject(t, root, "Original docstring.", "Original desc")
			},
			rewrite: func(t *testing.T, root string) {
				driftProject(t, root, "New docstring.", "New desc")
			},
		},
		{
			name: "the source is unchanged",
			setup: func(t *testing.T, root string) {
				driftProject(t, root, "Stable docstring.", "Stable desc")
			},
		},
		{
			name: "a page with no ref directive",
			setup: func(t *testing.T, root string) {
				writeConfig(t, root, configForSource(
					map[string]any{"path": "src/", "language": "python"},
				))
				write(t, filepath.Join(root, "src", "__init__.py"), `"""Module."""`+"\n")
				write(t, filepath.Join(root, "stricttools", "docs", "guide.md"),
					"+++\ndescription = \"A guide\"\n+++\n# Guide\n\nSome content.\n")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			isolate(t)
			root := t.TempDir()
			testCase.setup(t, root)
			checkFixture(t, root)
			if testCase.rewrite != nil {
				testCase.rewrite(t, root)
			}
			if got := withCode(checkFixture(t, root).Lints, "DRIFT001"); len(got) != 0 {
				t.Errorf("DRIFT001 fired: %v", messagesOf(got))
			}
		})
	}
}

func TestDRIFT001FirstRunIsSilent(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	driftProject(t, root, "Initial docstring.", "Desc")
	if got := withCode(checkFixture(t, root).Lints, "DRIFT001"); len(got) != 0 {
		t.Errorf("the first run reported drift: %v", messagesOf(got))
	}
}
