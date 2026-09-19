package quality

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/layout"
)

// MarkdownLOC returns the project's Markdown line and file counts.
//
// It walks projectPath counting lines in every .md file, skipping:
//
//   - the directories in [SkipDirs] plus todo/ (planning notes are not
//     documentation) and every directory in submodulePaths;
//   - the filenames in [SkipMarkdownFiles] (generated changelogs);
//   - every path in rootFileTemplates -- the docs/_README.md style templates
//     named by root_files in selfdoc.json. Their generated output (README.md,
//     CLAUDE.md) sits at the project root and is counted instead, so skipping
//     the template avoids counting the same prose twice. Paths are matched
//     relative to projectPath, as selfdoc.json spells them.
//
// Files that cannot be read are skipped rather than counted as empty.
func MarkdownLOC(projectPath string, submodulePaths []string, rootFileTemplates []string) (int, int) {
	docLOC, docFiles := 0, 0
	submoduleDirs := submoduleSet(projectPath, submodulePaths)
	templates := make(map[string]bool, len(rootFileTemplates))
	for _, template := range rootFileTemplates {
		templates[template] = true
	}

	walk(projectPath, submoduleDirs, func(dir, name, full string) {
		if !strings.HasSuffix(name, ".md") || SkipMarkdownFiles[name] {
			return
		}
		if len(templates) > 0 {
			if relative, err := filepath.Rel(projectPath, full); err == nil && templates[relative] {
				return
			}
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return
		}
		docLOC += countLines(content)
		docFiles++
	})

	return docLOC, docFiles
}

// testDirs is every directory name whose contents are test code at any depth.
var testDirs = map[string]bool{
	"tests": true, "test": true, "__tests__": true, "testing": true,
}

// testFileSuffixes is every filename tail that marks a test file outside a
// test directory.
var testFileSuffixes = []string{
	"_test.go", "_test.py",
	".test.ts", ".test.js", ".spec.ts", ".spec.js",
	".test.tsx", ".test.jsx", ".spec.tsx", ".spec.jsx",
}

// TestLOC returns the total line count of the project's test code.
//
// It walks projectPath (skipping [SkipDirs], todo/ and every directory in
// submodulePaths) and counts lines in files that have a [CodeExtensions]
// extension AND look like tests -- meaning they sit under a
// tests/test/__tests__/testing directory at any depth, or are named
// conftest.py, test_*.py, *_test.py, *_test.go, or *.test./*.spec. for
// js/ts/jsx/tsx.
//
// [ScoreProject] subtracts this from the dirstat code total so the doc ratio
// is measured against production source only, and a large test suite neither
// inflates nor deflates a project's grade.
func TestLOC(projectPath string, submodulePaths []string) int {
	submoduleDirs := submoduleSet(projectPath, submodulePaths)
	total := 0

	walk(projectPath, submoduleDirs, func(dir, name, full string) {
		if !CodeExtensions[extensionOf(name)] {
			return
		}
		if !isTestFile(projectPath, dir, name) {
			return
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return
		}
		total += countLines(content)
	})

	return total
}

// isTestFile reports whether the file named name in dir is test code.
func isTestFile(projectPath, dir, name string) bool {
	if relative, err := filepath.Rel(projectPath, dir); err == nil && relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			if testDirs[part] {
				return true
			}
		}
	}
	if name == "conftest.py" {
		return true
	}
	if strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py") {
		return true
	}
	for _, suffix := range testFileSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// extensionOf reproduces the Python expression the test-file scan reads a
// file's kind with: the text after the last dot, lowercased, or the whole
// lowercased filename when there is no dot -- which is how "Makefile" and
// "Dockerfile" are recognized as code.
func extensionOf(name string) string {
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		return strings.ToLower(name[index+1:])
	}
	return strings.ToLower(name)
}

// Adoption describes a project's selfdoc adoption, as read from selfdoc.json.
// Everything but HasSelfdoc is meaningless when HasSelfdoc is false, and the
// machine payload omits those members exactly then.
type Adoption struct {
	// HasSelfdoc is whether selfdoc.json exists and parses.
	HasSelfdoc bool
	// AutoREADME is whether root_files names a _README.md template.
	AutoREADME bool
	// AutoCLAUDE is whether root_files names a _CLAUDE.md template.
	AutoCLAUDE bool
	// CustomDirectives is how many entries the directives map declares.
	CustomDirectives int
	// HasPosts is whether a non-empty posts section exists.
	HasPosts bool
	// DirectiveCount is how many lines across the configured docs directory
	// (_build excluded) carry a :-:, :<: or :>: marker. It counts marker
	// LINES, not directives: a line holding two markers counts once, and an
	// open/close block counts twice.
	DirectiveCount int
}

// SelfdocInfo describes the project's selfdoc adoption, as read from
// selfdoc.json. A missing or unparsable selfdoc.json answers with HasSelfdoc
// false and nothing else.
//
// These flags are what [ComputeTier] climbs its ladder on.
func SelfdocInfo(projectPath string) Adoption {
	content, err := os.ReadFile(filepath.Join(projectPath, "selfdoc.json"))
	if err != nil {
		return Adoption{}
	}
	var config map[string]any
	if err := json.Unmarshal(content, &config); err != nil {
		return Adoption{}
	}

	info := Adoption{HasSelfdoc: true}
	for _, template := range stringList(config["root_files"]) {
		if strings.HasSuffix(template, "_README.md") {
			info.AutoREADME = true
		}
		if strings.HasSuffix(template, "_CLAUDE.md") {
			info.AutoCLAUDE = true
		}
	}
	if directives, ok := config["directives"].(map[string]any); ok {
		info.CustomDirectives = len(directives)
	}
	info.HasPosts = truthy(config["posts"])

	docsRel := layout.DocsRel
	if declared, ok := config["docs"].(string); ok {
		docsRel = declared
	}
	for _, root := range []string{docsRel, layout.GeneratedPagesRel} {
		docsDir := filepath.Join(projectPath, filepath.FromSlash(root))
		stat, err := os.Stat(docsDir)
		if err != nil || !stat.IsDir() {
			continue
		}
		walkDocs(docsDir, func(full string) {
			content, err := os.ReadFile(full)
			if err != nil {
				return
			}
			for _, line := range bytes.SplitAfter(content, []byte("\n")) {
				if len(line) == 0 {
					continue
				}
				if bytes.Contains(line, []byte(":-:")) ||
					bytes.Contains(line, []byte(":<:")) ||
					bytes.Contains(line, []byte(":>:")) {
					info.DirectiveCount++
				}
			}
		})
	}

	return info
}

// ComputeTier returns the maturity tier 0-5 for a project (see [Tiers]).
//
// The rungs are cumulative and evaluated in order, so the tier is the last
// satisfied requirement: any Markdown at all (1), selfdoc.json present (2), a
// generated README template in root_files (3), at least one directive used in
// the docs (4), and custom directives or blog posts configured (5).
func ComputeTier(docLOC int, info Adoption) int {
	if docLOC == 0 {
		return 0
	}
	if !info.HasSelfdoc {
		return 1
	}
	if !info.AutoREADME {
		return 2
	}
	if info.DirectiveCount == 0 {
		return 3
	}
	if info.CustomDirectives == 0 && !info.HasPosts {
		return 4
	}
	return 5
}

// ContentGrade grades a documentation-to-source line ratio as A-F.
//
// Cut-offs, applied to the ratio (doc LOC / non-test source LOC): 0.30 or more
// is an A, 0.15 a B, 0.05 a C, 0.01 a D, and anything below that an F. A nil
// ratio -- meaning there was no source to compare against -- grades as "-"
// rather than F, so an empty project is not marked as failing.
func ContentGrade(ratio *float64) string {
	if ratio == nil {
		return "-"
	}
	switch {
	case *ratio >= 0.30:
		return "A"
	case *ratio >= 0.15:
		return "B"
	case *ratio >= 0.05:
		return "C"
	case *ratio >= 0.01:
		return "D"
	default:
		return "F"
	}
}

// submoduleSet is the absolute directories the walks step over, one per
// declared submodule.
func submoduleSet(projectPath string, submodulePaths []string) map[string]bool {
	dirs := make(map[string]bool, len(submodulePaths))
	for _, relative := range submodulePaths {
		dirs[filepath.Join(projectPath, filepath.FromSlash(relative))] = true
	}
	return dirs
}

// walk visits every file under root, stepping over the skipped directory
// names, todo/ and the submodule directories. The visitor receives the
// holding directory, the filename and the full path.
func walk(root string, submoduleDirs map[string]bool, visit func(dir, name, full string)) {
	var descend func(dir string)
	descend = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			full := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if SkipDirs[entry.Name()] || entry.Name() == "todo" || submoduleDirs[full] {
					continue
				}
				descend(full)
				continue
			}
			visit(dir, entry.Name(), full)
		}
	}
	descend(root)
}

// walkDocs visits every Markdown file under the docs directory, stepping over
// the build output.
func walkDocs(root string, visit func(full string)) {
	var descend func(dir string)
	descend = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			full := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if entry.Name() == "_build" {
					continue
				}
				descend(full)
				continue
			}
			if strings.HasSuffix(entry.Name(), ".md") {
				visit(full)
			}
		}
	}
	descend(root)
}

// countLines counts the lines of content the way Python's readlines() does: a
// final line with no newline still counts, and an empty file has no lines.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	lines := bytes.Count(content, []byte("\n"))
	if content[len(content)-1] != '\n' {
		lines++
	}
	return lines
}

// stringList reads a decoded JSON array of strings, ignoring any member that
// is not a string.
func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	strs := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			strs = append(strs, text)
		}
	}
	return strs
}

// truthy reports whether a decoded JSON value is truthy the way Python's
// bool() judges it: a present non-empty, non-zero, non-false value.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
