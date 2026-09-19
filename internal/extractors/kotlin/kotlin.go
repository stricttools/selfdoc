// Package kotlin resolves selfdoc's directives against Kotlin source.
//
// Nothing from the Kotlin toolchain is required: the file is read with
// patterns. The four directives it serves are ref, prose-desc, table-schema
// and table-config.
//
// # Public by default
//
// Kotlin's default visibility is public, so a declaration carrying no
// visibility keyword belongs on a reference page. What the scanner looks for is
// therefore the three keywords that take a declaration off it -- private,
// protected and internal -- with one exception: an internal declaration marked
// @PublishedApi is part of the published API by definition, and is rendered.
//
// # KDoc
//
// A KDoc block documents the declaration directly beneath it and nothing else:
// a blank line between them breaks the association, which is Kotlin's own rule
// and the one the scanner enforces. Every tag the language documents is
// rendered -- the @param and @property tags accumulate into their own sections,
// the rest render a bold label -- and the square-bracket links become code
// spans.
package kotlin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// Extractor reads Kotlin source.
type Extractor struct {
	extractors.Base
}

// New builds the Kotlin extractor.
func New() extractors.Extractor {
	extractor := &Extractor{}
	extractor.Base = extractors.NewBase("kotlin", map[string]extractors.Handler{
		"ref":          handleRef,
		"prose-desc":   handleProseDesc,
		"table-schema": handleTableSchema,
		"table-config": extractors.HandleTableConfig,
	})
	return extractor
}

func init() { extractors.Register("kotlin", New) }

// Detect reports whether dir is a Kotlin project, by either Gradle build
// script.
func (e *Extractor) Detect(dir string) bool {
	return extractors.IsFile(filepath.Join(dir, "build.gradle.kts")) ||
		extractors.IsFile(filepath.Join(dir, "build.gradle"))
}

// FileExtensions is the one extension Kotlin source carries.
func (e *Extractor) FileExtensions() []string { return []string{".kt"} }

// ResolvePath resolves a directive's path argument to a Kotlin file or
// directory.
func (e *Extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveKotlinPath(pathArg, sourcePaths, baseDir)
}

// PublicSymbols lists the symbols a Kotlin file exports.
func (e *Extractor) PublicSymbols(file string) ([]string, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}
	return extractPublicSymbols(source), nil
}

// ModuleDocstring is the module-level KDoc comment at the top of a Kotlin file.
func (e *Extractor) ModuleDocstring(path string) (string, error) {
	source, err := extractors.ReadSource(path)
	if err != nil {
		return "", nil
	}
	return extractModuleDoc(source), nil
}

// SymbolDetails reports what a Kotlin file says about one symbol's parameters
// and return value. A data class answers with its primary constructor, and a
// dotted name selects a member of a class, an object or an interface
// ("UserService.findUser").
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

		if restrictedRe.MatchString(stripped) {
			continue
		}

		work := stripped
		if strings.HasPrefix(work, "public ") {
			work = strip(work[len("public "):])
		}

		if m := dataClassRe.FindStringSubmatch(work); m != nil && m[1] == symbol {
			return dataClassSymbolDetails(lines, i, stripped), nil
		}

		if m := funcRe.FindStringSubmatch(work); m != nil && m[1] == symbol {
			return funcSymbolDetails(lines, i), nil
		}
	}

	return nil, nil
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// resolveKotlinPath resolves a path argument to a Kotlin source file or a
// directory of them, trying each declared source path as a prefix and then the
// base directory, and admitting a path written without its .kt extension.
func resolveKotlinPath(pathArg string, sourcePaths []string, baseDir string) string {
	var candidates []string
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg))
	}
	candidates = append(candidates, util.PathJoin(baseDir, pathArg))

	for _, candidate := range candidates {
		if extractors.IsDir(candidate) && hasKotlinFile(candidate) {
			return candidate
		}
		if extractors.IsFile(candidate) {
			return candidate
		}
		if ktCandidate := candidate + ".kt"; extractors.IsFile(ktCandidate) {
			return ktCandidate
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

// hasKotlinFile reports whether a directory holds any Kotlin source.
func hasKotlinFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".kt") {
			return true
		}
	}
	return false
}

// kotlinFilesIn lists the Kotlin file names in a directory, sorted.
func kotlinFilesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".kt") {
			names = append(names, entry.Name())
		}
	}
	// os.ReadDir already sorts by file name.
	return names
}
