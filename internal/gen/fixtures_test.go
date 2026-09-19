package gen

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
)

// TestGeneratedBytesMatchThePython asserts that this package emits the same
// bytes as the Python selfdoc.gen it replaces, for every file one gen run
// produces.
//
// The fixture projects under testdata/fixture/ are the shared input: the
// Python implementation was run over a copy of each one once, and everything
// it produced is committed under testdata/expected/<fixture>/ (the recorder
// that did so needed the retired Python packages and is gone). This test
// copies the SAME fixture, runs the Go implementation, and compares byte for byte -- frontmatter, the em dash in a
// description template, the seed hashes in the store, all of it.
//
// The scratch directory is named "project" in both, because a source path of
// "." makes the project directory's own basename part of the generated index's
// description.
func TestGeneratedBytesMatchThePython(t *testing.T) {
	isolate(t)

	cases := []struct {
		fixture string
		// needsPython is true when seeding a description requires reading a
		// Python module docstring, which runs through python3.
		needsPython bool
	}{
		{fixture: "python", needsPython: true},
		{fixture: "go", needsPython: false},
		{fixture: "rootfiles", needsPython: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.fixture, func(t *testing.T) {
			if testCase.needsPython {
				requirePython3(t)
			}
			project := filepath.Join(t.TempDir(), "project")
			copyTree(t, filepath.Join("testdata", "fixture", testCase.fixture), project)

			cfg := loadConfig(t, project)
			if source, _ := cfg["source"].([]any); len(source) > 0 {
				generate(t, cfg, project)
			}
			if _, err := GenerateRootFiles(cfg, project, "", effects.Unbound()); err != nil {
				t.Fatalf("GenerateRootFiles: %v", err)
			}

			expectedRoot := filepath.Join("testdata", "expected", testCase.fixture)
			expected := expectedFiles(t, expectedRoot)
			if len(expected) == 0 {
				t.Fatalf("no recorded output under %s", expectedRoot)
			}
			for _, rel := range expected {
				want := read(t, filepath.Join(expectedRoot, rel))
				gotPath := filepath.Join(project, rel)
				got, err := os.ReadFile(gotPath)
				if err != nil {
					t.Errorf("%s: the Python wrote this file and the Go port did not: %v", rel, err)
					continue
				}
				if string(got) != want {
					t.Errorf("%s differs from the Python's output\n--- python ---\n%s\n--- go ---\n%s",
						rel, want, string(got))
				}
			}

			// The recorded set is exhaustive for the docs directory, so a
			// page the Go port writes and the Python did not is a failure
			// too.
			recorded := map[string]bool{}
			for _, rel := range expected {
				recorded[rel] = true
			}
			generatedRel := layout.GeneratedPagesRel
			for name := range mdFilesIn(t, layout.Path(project, generatedRel)) {
				if strings.HasPrefix(name, "_") {
					// A root-file template is an input, not an output.
					continue
				}
				if !recorded[generatedRel+"/"+name] {
					t.Errorf("%s/%s was generated but the Python wrote no such page",
						generatedRel, name)
				}
			}
		})
	}
}

// expectedFiles lists the recorded output files under root, as slash-separated
// paths relative to it.
func expectedFiles(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return found
}
