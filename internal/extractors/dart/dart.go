// Package dart resolves selfdoc's directives against Dart source.
//
// Nothing from the Dart toolchain is required: the file is read with patterns,
// which is what lets a documentation build run anywhere the repository is
// checked out. The four directives it serves are ref, prose-desc, table-schema
// and table-config.
//
// # What the scanner knows about Dart
//
// A library's public surface is not what one file declares. A part file's
// declarations belong to the library that declares the part, and a barrel file
// re-exports what it exports -- transitively, through show and hide
// combinators, and through both arms of a conditional export. Both are followed,
// with a visited set so a circular export answers instead of hanging, and local
// declarations shadow a re-exported name of the same spelling.
//
// Generated files are refused at every level -- as the directive's own target,
// as a part, and as an export target -- because a name.generator.dart file is a
// build artifact whose contents restate what the hand-written file already says.
// The test is the file name: two dots before the extension.
package dart

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// Extractor reads Dart source.
type Extractor struct {
	extractors.Base
}

// New builds the Dart extractor.
func New() extractors.Extractor {
	extractor := &Extractor{}
	extractor.Base = extractors.NewBase("dart", map[string]extractors.Handler{
		"ref":          handleRef,
		"prose-desc":   handleProseDesc,
		"table-schema": handleTableSchema,
		"table-config": extractors.HandleTableConfig,
	})
	return extractor
}

func init() { extractors.Register("dart", New) }

// Detect reports whether dir is a Dart package, by its pubspec.
func (e *Extractor) Detect(dir string) bool {
	return extractors.IsFile(filepath.Join(dir, "pubspec.yaml"))
}

// FileExtensions is the one extension Dart source carries.
func (e *Extractor) FileExtensions() []string { return []string{".dart"} }

// ResolvePath resolves a directive's path argument to a Dart file or directory.
func (e *Extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveDartPath(pathArg, sourcePaths, baseDir)
}

// PublicSymbols lists what a Dart library exports: its own public top-level
// declarations, then the ones its part files declare, then the ones it
// re-exports. A generated file exports nothing.
func (e *Extractor) PublicSymbols(file string) ([]string, error) {
	if isGeneratedFile(file) {
		return nil, nil
	}
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	symbols := extractPublicSymbols(source)

	for _, sym := range followParts(file, source) {
		appendSymbol(&symbols, sym)
	}

	if baseDir := findProjectRoot(file); baseDir != "" {
		for _, sym := range followExports(file, source, baseDir, map[string]bool{}) {
			appendSymbol(&symbols, sym)
		}
	}

	return symbols, nil
}

// ModuleDocstring is the library-level doc comment at the top of a Dart file.
func (e *Extractor) ModuleDocstring(path string) (string, error) {
	source, err := extractors.ReadSource(path)
	if err != nil {
		return "", nil
	}
	return extractLibraryDoc(source), nil
}

// SymbolDetails reports what a Dart file says about one function's parameters
// and return value. A dotted name selects a member of a class, an abstract
// class or a mixin ("UserRepository.findById").
func (e *Extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	if strings.Contains(symbol, ".") {
		return dottedSymbolDetails(source, symbol), nil
	}

	lines := strings.Split(source, "\n")
	for i, line := range lines {
		stripped := strip(line)

		if strings.HasPrefix(stripped, "//") || strings.HasPrefix(stripped, "/*") {
			continue
		}

		m := funcRe.FindStringSubmatch(stripped)
		if m != nil && m[1] == symbol && !dartKeywords[m[1]] {
			return dartSymbolDetails(lines, i, symbol), nil
		}
	}

	return nil, nil
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// readPackageName reads the package name out of a pubspec.
func readPackageName(baseDir string) string {
	data, err := os.ReadFile(filepath.Join(baseDir, "pubspec.yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strip(line)
		if strings.HasPrefix(line, "name:") {
			return strings.Trim(strip(line[5:]), "'\"")
		}
	}
	return ""
}

// resolvePackagePath resolves a package: import to a file under lib/.
//
// package:pkg_name/foo.dart is lib/foo.dart of the package named pkg_name, so a
// name that is not this package's resolves to nothing.
func resolvePackagePath(pathArg, baseDir string) string {
	rest := pathArg[len("package:"):]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return ""
	}
	pkgName := rest[:slashIdx]
	filePath := rest[slashIdx+1:]

	actualName := readPackageName(baseDir)
	if actualName == "" || actualName != pkgName {
		return ""
	}

	candidate := util.PathJoin(baseDir, "lib", filePath)
	if extractors.IsFile(candidate) {
		return candidate
	}
	return ""
}

// resolveDartPath resolves a path argument to a Dart source file or a directory
// of them, trying each declared source path as a prefix and then the base
// directory, and admitting a path written without its .dart extension.
func resolveDartPath(pathArg string, sourcePaths []string, baseDir string) string {
	if strings.HasPrefix(pathArg, "package:") {
		return resolvePackagePath(pathArg, baseDir)
	}

	var candidates []string
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg))
	}
	candidates = append(candidates, util.PathJoin(baseDir, pathArg))

	for _, candidate := range candidates {
		if extractors.IsDir(candidate) && hasDartFile(candidate) {
			return candidate
		}
		if extractors.IsFile(candidate) {
			return candidate
		}
		if dartCandidate := candidate + ".dart"; extractors.IsFile(dartCandidate) {
			return dartCandidate
		}
	}

	return ""
}

// hasDartFile reports whether a directory holds any Dart source.
func hasDartFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".dart") {
			return true
		}
	}
	return false
}

// dartFilesIn lists the non-generated Dart file names in a directory, sorted.
func dartFilesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".dart") && !isGeneratedFile(name) {
			names = append(names, name)
		}
	}
	// os.ReadDir already sorts by file name, which is the order Python's
	// sorted() puts the same names in.
	return names
}

// findProjectRoot walks up from a file to the directory carrying the pubspec.
func findProjectRoot(filePath string) string {
	absolute, err := filepath.Abs(filePath)
	if err != nil {
		absolute = filePath
	}
	current := filepath.Dir(absolute)
	for i := 0; i < 20; i++ { // safety limit
		if extractors.IsFile(filepath.Join(current, "pubspec.yaml")) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

// ---------------------------------------------------------------------------
// Part file following
// ---------------------------------------------------------------------------

// partPaths lists the part files a library declares, resolved against the
// library's own directory, with the generated ones dropped.
func partPaths(filePath, source string) []string {
	libDir := filepath.Dir(filePath)
	var paths []string
	for _, line := range strings.Split(source, "\n") {
		m := partRe.FindStringSubmatch(strip(line))
		if m == nil {
			continue
		}
		fullPartPath := filepath.Clean(util.PathJoin(libDir, m[1]))
		if isGeneratedFile(fullPartPath) {
			continue
		}
		paths = append(paths, fullPartPath)
	}
	return paths
}

// followParts lists the public symbols a library's part files declare.
func followParts(filePath, source string) []string {
	var symbols []string
	for _, partPath := range partPaths(filePath, source) {
		partSource, err := extractors.ReadSource(partPath)
		if err != nil {
			continue
		}
		for _, sym := range extractPublicSymbols(partSource) {
			appendSymbol(&symbols, sym)
		}
	}
	return symbols
}

// followPartsDeclarations lists the declarations a library's part files carry.
func followPartsDeclarations(filePath, source string) []declaration {
	var declarations []declaration
	for _, partPath := range partPaths(filePath, source) {
		partSource, err := extractors.ReadSource(partPath)
		if err != nil {
			continue
		}
		declarations = append(declarations, extractDeclarations(partSource)...)
	}
	return declarations
}

// ---------------------------------------------------------------------------
// Export following
// ---------------------------------------------------------------------------

// exportDirective is one export line: the paths it names and the combinator
// that filters what they contribute.
type exportDirective struct {
	paths      []string
	combinator string
	names      map[string]bool
}

// exportDirectives reads the export lines out of a file. A conditional export
// contributes both of its paths, since either one can be the one a consumer
// compiles against.
func exportDirectives(source string) []exportDirective {
	var directives []exportDirective
	for _, line := range strings.Split(source, "\n") {
		m := exportRe.FindStringSubmatch(strip(line))
		if m == nil {
			continue
		}

		paths := []string{m[1]}
		if m[2] != "" {
			paths = append(paths, m[2])
		}

		names := map[string]bool{}
		if m[3] != "" && m[4] != "" {
			for _, n := range strings.Split(m[4], ",") {
				names[strip(n)] = true
			}
		}

		directives = append(directives, exportDirective{
			paths:      paths,
			combinator: m[3],
			names:      names,
		})
	}
	return directives
}

// visit records a file as seen and reports whether it already was, so a
// circular export terminates.
func visit(visited map[string]bool, filePath string) bool {
	absolute, err := filepath.Abs(filePath)
	if err != nil {
		absolute = filePath
	}
	if visited[absolute] {
		return false
	}
	visited[absolute] = true
	return true
}

// resolveExportPath resolves one export target against the exporting file's
// directory, refusing a missing file and a generated one.
func resolveExportPath(exportPath, fileDir, baseDir string) string {
	var fullPath string
	if strings.HasPrefix(exportPath, "package:") {
		fullPath = resolvePackagePath(exportPath, baseDir)
	} else {
		fullPath = filepath.Clean(util.PathJoin(fileDir, exportPath))
	}

	if fullPath == "" || !extractors.IsFile(fullPath) {
		return ""
	}
	if isGeneratedFile(fullPath) {
		return ""
	}
	return fullPath
}

// followExports lists the symbols a file re-exports, transitively.
func followExports(filePath, source, baseDir string, visited map[string]bool) []string {
	if !visit(visited, filePath) {
		return nil
	}

	fileDir := filepath.Dir(filePath)
	var symbols []string

	for _, directive := range exportDirectives(source) {
		for _, exportPath := range directive.paths {
			exported := resolveAndExtractExports(exportPath, fileDir, baseDir, visited)

			switch directive.combinator {
			case "show":
				exported = filterNames(exported, directive.names, true)
			case "hide":
				exported = filterNames(exported, directive.names, false)
			}

			for _, sym := range exported {
				appendSymbol(&symbols, sym)
			}
		}
	}

	return symbols
}

// filterNames keeps the names a show combinator lists, or drops the ones a hide
// combinator lists.
func filterNames(symbols []string, names map[string]bool, keep bool) []string {
	var out []string
	for _, sym := range symbols {
		if names[sym] == keep {
			out = append(out, sym)
		}
	}
	return out
}

// resolveAndExtractExports lists what one export target contributes: its own
// declarations, its part files' and, transitively, its own exports'.
func resolveAndExtractExports(exportPath, fileDir, baseDir string, visited map[string]bool) []string {
	fullPath := resolveExportPath(exportPath, fileDir, baseDir)
	if fullPath == "" {
		return nil
	}

	targetSource, err := extractors.ReadSource(fullPath)
	if err != nil {
		return nil
	}

	directSymbols := extractPublicSymbols(targetSource)

	for _, sym := range followParts(fullPath, targetSource) {
		appendSymbol(&directSymbols, sym)
	}
	for _, sym := range followExports(fullPath, targetSource, baseDir, visited) {
		appendSymbol(&directSymbols, sym)
	}

	return directSymbols
}

// followExportsDeclarations is followExports for whole declarations, which is
// what the ref directive renders.
func followExportsDeclarations(
	filePath, source, baseDir string,
	visited map[string]bool,
) []declaration {
	if !visit(visited, filePath) {
		return nil
	}

	fileDir := filepath.Dir(filePath)
	var declarations []declaration
	seenNames := map[string]bool{}

	for _, directive := range exportDirectives(source) {
		for _, exportPath := range directive.paths {
			decls := resolveAndExtractExportDeclarations(exportPath, fileDir, baseDir, visited)

			switch directive.combinator {
			case "show":
				decls = filterDeclarations(decls, directive.names, true)
			case "hide":
				decls = filterDeclarations(decls, directive.names, false)
			}

			for _, decl := range decls {
				if !seenNames[decl.name] {
					seenNames[decl.name] = true
					declarations = append(declarations, decl)
				}
			}
		}
	}

	return declarations
}

// filterDeclarations applies a show or hide combinator to declarations.
func filterDeclarations(decls []declaration, names map[string]bool, keep bool) []declaration {
	var out []declaration
	for _, decl := range decls {
		if names[decl.name] == keep {
			out = append(out, decl)
		}
	}
	return out
}

// resolveAndExtractExportDeclarations lists the declarations one export target
// contributes, its part files' and its own exports' included.
func resolveAndExtractExportDeclarations(
	exportPath, fileDir, baseDir string,
	visited map[string]bool,
) []declaration {
	fullPath := resolveExportPath(exportPath, fileDir, baseDir)
	if fullPath == "" {
		return nil
	}

	targetSource, err := extractors.ReadSource(fullPath)
	if err != nil {
		return nil
	}

	decls := extractDeclarations(targetSource)
	decls = append(decls, followPartsDeclarations(fullPath, targetSource)...)
	decls = append(decls, followExportsDeclarations(fullPath, targetSource, baseDir, visited)...)
	return decls
}
