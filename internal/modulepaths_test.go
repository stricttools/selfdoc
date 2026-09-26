// Package internal_test holds module-wide invariants about the repository's
// own file layout -- facts that belong to no single package.
package internal_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reservedWindowsNames is the set golang.org/x/mod/module refuses as a path
// element when it builds a module zip. A source file carrying one of these
// names makes the module unfetchable through the Go module proxy: the tag can
// exist, the GitHub Release can exist, and `go install ...@v0` still fails
// with `"aux" disallowed as path element component on Windows`. Renaming the
// offending file is the only repair -- a retraction cannot make a published
// version fetchable.
var reservedWindowsNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
}

// skippedDirs are directories whose contents are build output, caches or the
// git database rather than module content.
var skippedDirs = map[string]bool{
	".git":         true,
	"stricttools": true,
	"bin":          true,
	"dist":         true,
}

// isReserved reports whether a single path element is one of the names Go
// refuses. The comparison is case-insensitive and ignores everything from the
// first dot onward, matching module.CheckFilePath: `aux.go`, `AUX`, and
// `Aux.tar.gz` are all refused.
func isReserved(elem string) bool {
	short := elem
	if i := strings.Index(short, "."); i >= 0 {
		short = short[:i]
	}
	for _, bad := range reservedWindowsNames {
		if strings.EqualFold(short, bad) {
			return true
		}
	}
	return false
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

func TestNoWindowsReservedPathComponents(t *testing.T) {
	root := moduleRoot(t)
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return fs.SkipDir
			}
			if filepath.ToSlash(rel) == "docs/_build" {
				return fs.SkipDir
			}
		}
		if isReserved(d.Name()) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	for _, rel := range offenders {
		t.Errorf("%s carries a path component reserved on Windows; the Go module proxy refuses to build a module zip containing it, so `go install` fails for every version that ships this path. Rename it (e.g. aux.go -> auxiliary.go).", rel)
	}
}

func TestIsReservedRecognizesTheRefusedNames(t *testing.T) {
	refused := []string{"aux.go", "AUX", "Aux.tar.gz", "nul", "com1.txt", "LPT9", "con.md", "prn"}
	for _, name := range refused {
		if !isReserved(name) {
			t.Errorf("isReserved(%q) = false, want true", name)
		}
	}
	allowed := []string{"auxiliary.go", "aux_test.go", "com0.go", "lpt10.go", "context.go", "nullable.go", "console"}
	for _, name := range allowed {
		if isReserved(name) {
			t.Errorf("isReserved(%q) = true, want false", name)
		}
	}
}
