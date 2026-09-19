// Package zig reads Zig source for selfdoc.
//
// It answers directives by scanning source text with regular expressions
// rather than by parsing it: no Zig toolchain is required, and a file that
// does not compile still documents. What it reads out of a file is the
// module's //! documentation, the public declarations -- functions including
// the extern, export and inline forms, constants whatever their right side is,
// and variables -- with the /// comment block above each, the fields of a
// public struct, and the file's test blocks.
package zig

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// extractor reads Zig source.
type extractor struct {
	extractors.Base
}

// New builds the Zig extractor. It spawns nothing and writes nothing, it only
// reads source files.
func New() extractors.Extractor {
	e := &extractor{}
	e.Base = extractors.NewBase("zig", map[string]extractors.Handler{
		"ref":          handleRef,
		"prose-desc":   handleProseDesc,
		"table-schema": handleTableSchema,
		"code-test":    handleCodeTest,
		"table-config": extractors.HandleTableConfig,
	})
	return e
}

func init() {
	extractors.Register("zig", New)
}

// Detect reports whether dir is a Zig project, by its build script or that
// script's dependency manifest.
func (e *extractor) Detect(dir string) bool {
	return extractors.IsFile(util.PathJoin(dir, "build.zig")) ||
		extractors.IsFile(util.PathJoin(dir, "build.zig.zon"))
}

// ResolvePath resolves a directive's path argument to a Zig source file or to
// a directory of them.
func (e *extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveZigPath(pathArg, sourcePaths, baseDir)
}

// FileExtensions lists the extensions the build walks a Zig source tree for.
func (e *extractor) FileExtensions() []string {
	return []string{".zig"}
}

var (
	// pubFnSymbol matches a public function declaration's name.
	pubFnSymbol = regexp.MustCompile(
		`^pub` + pySpace + `+(?:extern` + pySpace + `+|export` + pySpace + `+|inline` +
			pySpace + `+)?fn` + pySpace + `+(` + pyWord + `+)` + pySpace + `*\(`)

	// pubConstSymbol matches a public constant declaration's name, whatever
	// its right side is: a value, a struct, an enum, a union or an error set.
	pubConstSymbol = regexp.MustCompile(
		`^pub` + pySpace + `+const` + pySpace + `+(` + pyWord + `+)` + pySpace + `*[=:]`)

	// pubVarSymbol matches a public variable declaration's name.
	pubVarSymbol = regexp.MustCompile(
		`^pub` + pySpace + `+var` + pySpace + `+(` + pyWord + `+)`)
)

// PublicSymbols lists the public declarations of a Zig source file, in source
// order.
//
// A private declaration -- one without "pub" -- is not public API and is not
// reported, and neither is a test block.
func (e *extractor) PublicSymbols(file string) ([]string, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	var symbols []string
	seen := map[string]bool{}

	for _, line := range strings.Split(source, "\n") {
		stripped := pyStrip(line)

		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if strings.HasPrefix(stripped, "test ") {
			continue
		}
		if commentIdx := strings.Index(stripped, "//"); commentIdx >= 0 {
			stripped = pyStrip(stripped[:commentIdx])
		}

		for _, pattern := range []*regexp.Regexp{pubFnSymbol, pubConstSymbol, pubVarSymbol} {
			m := pattern.FindStringSubmatch(stripped)
			if m == nil {
				continue
			}
			if !seen[m[1]] {
				seen[m[1]] = true
				symbols = append(symbols, m[1])
			}
			break
		}
	}

	return symbols, nil
}

// ModuleDocstring is a file's //! module documentation.
func (e *extractor) ModuleDocstring(path string) (string, error) {
	source, err := extractors.ReadSource(path)
	if err != nil {
		return "", nil
	}
	return extractModuleDoc(source), nil
}

// SymbolDetails reports what a file says about one function's parameters and
// return value. A dotted name selects a member of a container type
// ("Config.init").
func (e *extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	if strings.Contains(symbol, ".") {
		return dottedSymbolDetails(source, symbol), nil
	}

	lines := strings.Split(source, "\n")
	pattern := fnPattern(symbol)
	for i, line := range lines {
		stripped := pyStrip(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if pattern.MatchString(stripped) {
			return zigSymbolDetails(lines, i), nil
		}
	}

	return nil, nil
}
