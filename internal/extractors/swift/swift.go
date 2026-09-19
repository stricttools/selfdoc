// Package swift resolves selfdoc's directives against Swift source.
//
// Nothing from the Swift toolchain is required: the file is read with patterns.
// The four directives it serves are ref, prose-desc, table-schema and
// table-config.
//
// # Explicit visibility
//
// Swift's default visibility is internal, so what belongs on a reference page
// is what says public or open, and that is what the scanners look for. The one
// place the keyword is not required is inside a public struct, where a field is
// public without restating it -- and inside a symbol a page asked about by
// name, where the question settles what is being documented.
//
// # Doc comments
//
// A /// block documents the declaration directly beneath it, and Swift's own
// item syntax is rendered: the individual "- Parameter name:" items and the
// "- Parameters:" block with its indented sub-items accumulate into one
// section, "- Returns:" and "- Throws:" render bold labels, every callout
// keyword from Note to TODO renders its own, and a “Symbol“ reference becomes
// the code span the rest of the site writes.
package swift

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// Extractor reads Swift source.
type Extractor struct {
	extractors.Base
}

// New builds the Swift extractor.
func New() extractors.Extractor {
	extractor := &Extractor{}
	extractor.Base = extractors.NewBase("swift", map[string]extractors.Handler{
		"ref":          handleRef,
		"prose-desc":   handleProseDesc,
		"table-schema": handleTableSchema,
		"table-config": extractors.HandleTableConfig,
	})
	return extractor
}

func init() { extractors.Register("swift", New) }

// Detect reports whether dir is a Swift package, by its manifest.
func (e *Extractor) Detect(dir string) bool {
	return extractors.IsFile(filepath.Join(dir, "Package.swift"))
}

// FileExtensions is the one extension Swift source carries.
func (e *Extractor) FileExtensions() []string { return []string{".swift"} }

// ResolvePath resolves a directive's path argument to a Swift file or
// directory.
func (e *Extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveSwiftPath(pathArg, sourcePaths, baseDir)
}

// PublicSymbols lists the public and open symbols a Swift file exports.
func (e *Extractor) PublicSymbols(file string) ([]string, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}
	return extractPublicSymbols(source), nil
}

// ModuleDocstring is the module-level doc comment at the top of a Swift file.
func (e *Extractor) ModuleDocstring(path string) (string, error) {
	source, err := extractors.ReadSource(path)
	if err != nil {
		return "", nil
	}
	return extractModuleDoc(source), nil
}

// SymbolDetails reports what a Swift file says about one function's parameters
// and return value. A dotted name selects a member of a type
// ("Router.handle").
func (e *Extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	if dotIdx := strings.LastIndex(symbol, "."); dotIdx >= 0 {
		return dottedSymbolDetails(source, symbol[:dotIdx], symbol[dotIdx+1:]), nil
	}

	lines := strings.Split(source, "\n")
	funcPattern := memberFuncPattern(symbol)

	for i, line := range lines {
		stripped := strip(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if funcPattern.MatchString(stripped) {
			return symbolDetails(lines, i), nil
		}
	}

	return nil, nil
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// resolveSwiftPath resolves a path argument to a Swift source file or a
// directory of them, trying each declared source path as a prefix and then the
// base directory, and admitting a path written without its .swift extension.
func resolveSwiftPath(pathArg string, sourcePaths []string, baseDir string) string {
	var candidates []string
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg))
	}
	candidates = append(candidates, util.PathJoin(baseDir, pathArg))

	for _, candidate := range candidates {
		if extractors.IsDir(candidate) && hasSwiftFile(candidate) {
			return candidate
		}
		if extractors.IsFile(candidate) {
			return candidate
		}
		if swiftCandidate := candidate + ".swift"; extractors.IsFile(swiftCandidate) {
			return swiftCandidate
		}
	}

	return ""
}

// resolveFilePath resolves a path argument to a file, the base directory
// first. It is what the schema directive uses, which reads one file and never
// a directory.
func resolveFilePath(filePath string, sourcePaths []string, baseDir string) string {
	candidates := []string{util.PathJoin(baseDir, filePath)}
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, filePath))
	}

	for _, candidate := range candidates {
		if extractors.IsFile(candidate) {
			return candidate
		}
	}
	return ""
}

// hasSwiftFile reports whether a directory holds any Swift source.
func hasSwiftFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".swift") {
			return true
		}
	}
	return false
}

// swiftFilesIn lists the Swift file names in a directory, sorted.
func swiftFilesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".swift") {
			names = append(names, entry.Name())
		}
	}
	// os.ReadDir already sorts by file name.
	return names
}
