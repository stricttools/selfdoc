package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/excludes"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/util"
)

// testPatterns are the test-file patterns per language family. A test file is
// not a module of the project, so no listing shows one.
var testPatterns = map[string][]string{
	"go":     {"_test.go"},
	"python": {"test_", "_test.py"},
	"typescript": {
		".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
		".test.js", ".spec.js", ".test.jsx", ".spec.jsx",
	},
}

// packageLanguages group by directory (package) rather than by file.
var packageLanguages = map[string]bool{"go": true}

// dirGroupLanguages group by directory for readability but still show
// individual files.
var dirGroupLanguages = map[string]bool{"typescript": true}

// isTestFile reports whether a filename matches the language's test-file
// patterns.
func isTestFile(filename, language string) bool {
	for _, pattern := range testPatterns[language] {
		if language == "python" && pattern == "test_" {
			if strings.HasPrefix(filename, "test_") {
				return true
			}
			continue
		}
		if strings.HasSuffix(filename, pattern) {
			return true
		}
	}
	return false
}

// ErrNoSourceEntries is returned by the directives that read source code when
// the project declares none.
//
// A placeholder note would hide the fact that the page lost the listing it
// asked for, and a codeless project usually has no source directory on disk
// either -- so the missing-source check runs before the directory check, or
// the build would render "directory not found" and exit 0.
var ErrNoSourceEntries = errors.New("selfdoc.json declares no 'source' entries")

// ResolveListModules lists source modules grouped by the language's natural
// unit: one bullet per package for Go, per file grouped by directory for
// TypeScript and JavaScript, and per file for Python and everything else.
//
// With files=true the per-file listing is used whatever the language, which is
// also the only listing an unsupported language can have.
func ResolveListModules(
	attrs map[string]string,
	config map[string]any,
	baseDir string,
) (string, error) {
	directivePath := attrs["path"]
	if directivePath == "" {
		return marker("list-modules requires a path attribute"), nil
	}

	// This check runs before the directory check on purpose: see
	// ErrNoSourceEntries.
	sourceEntries, err := extractors.ResolveSourceEntries(config)
	if err != nil {
		return "", err
	}
	if len(sourceEntries) == 0 {
		return "", fmt.Errorf(
			"Directive :-: list-modules reads source code, but %w. Either "+
				"remove the directive, or declare the code it should read: "+
				`"source": [{"path": "src/", "language": "python"}]`,
			ErrNoSourceEntries,
		)
	}

	fullPath := util.ResolveDirectivePath(baseDir, directivePath)
	if info, err := os.Stat(fullPath); err != nil || !info.IsDir() {
		return marker("directory '%s' not found", directivePath), nil
	}

	// Match the directive's path to a source entry.
	matched := sourceEntries[0]
	for _, entry := range sourceEntries {
		entryPath := strings.TrimRight(entry.Path, "/")
		wanted := strings.TrimRight(directivePath, "/")
		if strings.HasPrefix(wanted, entryPath) || strings.HasPrefix(entryPath, wanted) {
			matched = entry
			break
		}
	}

	language := matched.Language
	// The entry already carries its extractor, which is the stub for a
	// language selfdoc has none for -- and a KNOWN language whose package
	// is not linked into this binary has already been refused by
	// ResolveSourceEntries, so the stub here always means "unsupported
	// language" rather than "wiring mistake".
	extractor := matched.Extractor
	supported := extractors.IsKnownLanguage(language)

	// A listing covers what the generated pages cover: the same defaults
	// plus the project's own gen.exclude patterns. Without this a listing
	// advertises modules -- an analyzer's fixture tree, a vendored copy --
	// that the site has no page for.
	excludePatterns := excludes.PatternsFor(config)

	useFiles := strings.ToLower(attrs["files"]) == "true"

	if !supported {
		if useFiles {
			// Per-file listing with no module documentation, for a
			// language selfdoc cannot read.
			return listModulesFiles(
				fullPath, baseDir, language, extractor, excludePatterns,
			)
		}
		return "", fmt.Errorf(
			"language '%s' has no module extractor"+
				" -- use `list-modules files=true` for per-file listing",
			language,
		)
	}

	if useFiles {
		return listModulesFiles(
			fullPath, baseDir, language, extractor, excludePatterns,
		)
	}

	if packageLanguages[language] {
		return listModulesByPackage(
			fullPath, baseDir, directivePath, language, extractor, excludePatterns,
		)
	}
	if dirGroupLanguages[language] {
		return listModulesByDirGroup(
			fullPath, baseDir, language, extractor, excludePatterns,
		)
	}
	return listModulesPerFile(
		fullPath, baseDir, language, extractor, excludePatterns,
	)
}

// module is one listed module: its name, the path it was read from and the
// first sentence of its own documentation.
type module struct {
	name    string
	relPath string
	summary string
}

// bulletLines renders one bullet per module, in name order.
func bulletLines(modules []module) []string {
	lines := make([]string, 0, len(modules))
	for _, entry := range modules {
		if entry.summary != "" {
			lines = append(lines, fmt.Sprintf(
				"- **%s** (`%s`): %s", entry.name, entry.relPath, entry.summary,
			))
			continue
		}
		lines = append(lines, fmt.Sprintf("- **%s** (`%s`)", entry.name, entry.relPath))
	}
	return lines
}

// collectModules walks root collecting the files this language owns, skipping
// the excluded ones. skipTests drops the language's test files, which every
// listing but the files=true one does.
func collectModules(
	root, baseDir, language string,
	extractor extractors.Extractor,
	excludePatterns []string,
	skipTests bool,
) ([]module, error) {
	extensions := map[string]bool{}
	for _, ext := range extractor.FileExtensions() {
		extensions[ext] = true
	}

	var modules []module
	err := walkSource(baseDir, root, func(dirPath string, entries []fs.DirEntry) error {
		for _, entry := range entries {
			name := entry.Name()
			if skipTests && isTestFile(name, language) {
				continue
			}
			if !extensions[filepath.Ext(name)] {
				continue
			}
			filePath := filepath.Join(dirPath, name)
			if excludes.IsExcluded(relativeTo(root, filePath), excludePatterns) {
				continue
			}
			relPath := relativeTo(baseDir, filePath)
			moduleName, named := fileToModuleName(relPath, language)
			if !named {
				continue
			}
			raw, err := extractor.ModuleDocstring(filePath)
			if err != nil {
				return err
			}
			summary := ""
			if raw != "" {
				summary = prose.FirstSentence(raw)
			}
			modules = append(modules, module{
				name: moduleName, relPath: relPath, summary: summary,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(modules, func(i, j int) bool {
		return modules[i].name < modules[j].name
	})
	return modules, nil
}

// listModulesFiles is the per-file listing files=true asks for: every file the
// language owns, test files included.
func listModulesFiles(
	root, baseDir, language string,
	extractor extractors.Extractor,
	excludePatterns []string,
) (string, error) {
	modules, err := collectModules(
		root, baseDir, language, extractor, excludePatterns, false,
	)
	if err != nil {
		return "", err
	}
	if len(modules) == 0 {
		return marker("no modules found in '%s'", relativeTo(baseDir, root)), nil
	}
	return strings.Join(bulletLines(modules), "\n"), nil
}

// listModulesPerFile is the per-file listing with test files excluded and each
// module's own documentation, used for Python and any language that is neither
// package-grouped nor directory-grouped.
func listModulesPerFile(
	root, baseDir, language string,
	extractor extractors.Extractor,
	excludePatterns []string,
) (string, error) {
	modules, err := collectModules(
		root, baseDir, language, extractor, excludePatterns, true,
	)
	if err != nil {
		return "", err
	}
	if len(modules) == 0 {
		return marker("no modules found in '%s'", relativeTo(baseDir, root)), nil
	}
	return strings.Join(bulletLines(modules), "\n"), nil
}

// listModulesByPackage groups by package directory, which is the natural unit
// in Go: each directory holding source files becomes one bullet, summarized by
// the package's own documentation.
func listModulesByPackage(
	root, baseDir, directivePath, language string,
	extractor extractors.Extractor,
	excludePatterns []string,
) (string, error) {
	extensions := map[string]bool{}
	for _, ext := range extractor.FileExtensions() {
		extensions[ext] = true
	}

	var packageDirs []string
	err := walkSource(baseDir, root, func(dirPath string, entries []fs.DirEntry) error {
		relDir := relativeTo(root, dirPath)
		if relDir != "." && excludes.IsExcluded(relDir, excludePatterns) {
			return nil
		}
		// The Go toolchain ignores testdata, vendor, and any directory
		// whose name begins with "." or "_", so no package is there to
		// list.
		if language == "go" && excludes.GoToolchainIgnoresPath(relDir) {
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			if isTestFile(name, language) {
				continue
			}
			if !extensions[filepath.Ext(name)] {
				continue
			}
			candidate := name
			if relDir != "." {
				candidate = path.Join(relDir, name)
			}
			if excludes.IsExcluded(candidate, excludePatterns) {
				continue
			}
			packageDirs = append(packageDirs, dirPath)
			return nil
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if len(packageDirs) == 0 {
		return marker("no modules found in '%s'", directivePath), nil
	}

	sort.Strings(packageDirs)
	lines := make([]string, 0, len(packageDirs))
	for _, packageDir := range packageDirs {
		name := relativeTo(baseDir, packageDir)
		raw, err := extractor.ModuleDocstring(packageDir)
		if err != nil {
			return "", err
		}
		if raw != "" {
			if summary := prose.FirstSentence(raw); summary != "" {
				lines = append(lines, fmt.Sprintf("- **%s**: %s", name, summary))
				continue
			}
		}
		lines = append(lines, fmt.Sprintf("- **%s**", name))
	}
	return strings.Join(lines, "\n"), nil
}

// listModulesByDirGroup lists files individually but organizes them under
// directory headings, which is how a TypeScript or JavaScript tree reads.
func listModulesByDirGroup(
	root, baseDir, language string,
	extractor extractors.Extractor,
	excludePatterns []string,
) (string, error) {
	modules, err := collectModules(
		root, baseDir, language, extractor, excludePatterns, true,
	)
	if err != nil {
		return "", err
	}
	if len(modules) == 0 {
		return marker("no modules found in '%s'", relativeTo(baseDir, root)), nil
	}

	grouped := map[string][]module{}
	for _, entry := range modules {
		dir := path.Dir(entry.relPath)
		grouped[dir] = append(grouped[dir], entry)
	}
	dirs := make([]string, 0, len(grouped))
	for dir := range grouped {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var lines []string
	for _, dir := range dirs {
		entries := grouped[dir]
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].name < entries[j].name
		})
		// A directory heading only says something when there is more than
		// one directory.
		if len(dirs) > 1 {
			lines = append(lines, "**"+dir+"/**", "")
		}
		lines = append(lines, bulletLines(entries)...)
		if len(dirs) > 1 {
			lines = append(lines, "")
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n"), nil
}

// fileToModuleName converts a relative file path to a module name:
// "selfdoc/config.py" becomes "selfdoc.config" for Python, and
// "pkg/handler.go" becomes "pkg/handler" for everything else.
//
// The second result is false for a Python package's own __init__ at the top of
// the walk, which names no module of its own.
func fileToModuleName(relPath, language string) (string, bool) {
	root := strings.TrimSuffix(relPath, filepath.Ext(relPath))
	if language != "python" {
		return filepath.ToSlash(root), true
	}
	if strings.HasSuffix(root, "/__init__") {
		root = root[:strings.LastIndex(root, "/__init__")]
	} else if root == "__init__" {
		root = ""
	}
	if root == "" {
		return "", false
	}
	return strings.ReplaceAll(filepath.ToSlash(root), "/", "."), true
}

// walkSource walks root depth-first, calling visit once per directory with
// that directory's file entries, and pruning the directories no source walk
// descends into.
//
// The scratch directories at projectRoot's top level are pruned too. The root
// itself is never pruned: a directive may legitimately point at a directory
// whose name would be skipped as a child.
func walkSource(projectRoot, root string, visit func(dirPath string, files []fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(dirPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if dirPath != root && (excludes.ShouldSkipDir(entry.Name()) ||
			excludes.IsScratchDir(projectRoot, dirPath)) {
			return fs.SkipDir
		}
		children, err := os.ReadDir(dirPath)
		if err != nil {
			return err
		}
		files := make([]fs.DirEntry, 0, len(children))
		for _, child := range children {
			if !child.IsDir() {
				files = append(files, child)
			}
		}
		return visit(dirPath, files)
	})
}

// relativeTo is target relative to from, in posix spelling -- what Python's
// os.path.relpath answers for the paths this package builds.
func relativeTo(from, target string) string {
	rel, err := filepath.Rel(from, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}
