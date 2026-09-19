// Package typescript reads TypeScript and JavaScript source for selfdoc.
//
// It answers directives by scanning source text with regular expressions
// rather than by parsing it: no TypeScript toolchain is required, and a file
// that does not compile still documents. What it reads out of a file is the
// module's own JSDoc block, the declarations it exports (functions, classes,
// interfaces, type aliases, enums and constants, plus re-export lists), an
// interface's or object type's fields, the describe/it/test blocks of a test
// file, and the help or usage string constants of a command-line entry point.
//
// The JSDoc parser is exported, because the Svelte extractor documents a
// component from the JSDoc block inside its <script> element and parsing the
// same comment dialect twice would let the two drift.
package typescript

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// extractor reads TypeScript and JavaScript source.
type extractor struct {
	extractors.Base
}

// New builds the TypeScript extractor. It spawns nothing and writes nothing,
// it only reads source files.
func New() extractors.Extractor {
	e := &extractor{}
	e.Base = extractors.NewBase("typescript", map[string]extractors.Handler{
		"ref":          handleModule,
		"code-test":    handleTest,
		"table-schema": handleSchema,
		"code-help":    handleCLI,
		"table-config": handleConfig,
		"prose-desc":   handleProseDesc,
	})
	return e
}

func init() {
	extractors.Register("typescript", New)
}

// Detect reports whether dir is a TypeScript project, by its tsconfig.json.
func (e *extractor) Detect(dir string) bool {
	return extractors.IsFile(util.PathJoin(dir, "tsconfig.json"))
}

// ResolvePath resolves a directive's path argument to a source file.
func (e *extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveFilePath(pathArg, sourcePaths, baseDir)
}

// FileExtensions lists the extensions the build walks a TypeScript source tree
// for.
//
// It is deliberately shorter than the list path resolution recognizes -- see
// tsJSExtensions, which also carries the module-family and CommonJS
// spellings.
func (e *extractor) FileExtensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx"}
}

var (
	// tsNamedFunc matches an exported function declaration.
	tsNamedFunc = regexp.MustCompile(
		`^export` + pySpace + `+(?:async` + pySpace + `+)?function` + pySpace + `+(` + pyWord + `+)`)
	// tsClass matches an exported class declaration.
	tsClass = regexp.MustCompile(`^export` + pySpace + `+class` + pySpace + `+(` + pyWord + `+)`)
	// tsVar matches an exported variable declaration.
	tsVar = regexp.MustCompile(
		`^export` + pySpace + `+(?:const|let|var)` + pySpace + `+(` + pyWord + `+)`)
	// tsType matches an exported interface, type alias or enum declaration.
	tsType = regexp.MustCompile(
		`^export` + pySpace + `+(?:interface|type|enum)` + pySpace + `+(` + pyWord + `+)`)
	// tsDefault matches a default-exported function or class.
	tsDefault = regexp.MustCompile(
		`^export` + pySpace + `+default` + pySpace + `+(?:function|class)` + pySpace + `+(` + pyWord + `+)`)
	// tsReexport matches a re-export list.
	tsReexport = regexp.MustCompile(`^export` + pySpace + `*\{([^}]+)\}`)
	// blockComment matches a block comment that opens and closes on one line.
	blockComment = regexp.MustCompile(`/\*.*?\*/`)
)

// PublicSymbols lists the symbols a source file exports, in source order.
//
// Comment text is removed first -- both forms, including a block comment that
// spans lines -- so a commented-out export is not reported as one.
func (e *extractor) PublicSymbols(file string) ([]string, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	var symbols []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		symbols = append(symbols, name)
	}

	inBlockComment := false
	for _, line := range strings.Split(source, "\n") {
		stripped := pyStrip(line)

		if inBlockComment {
			idx := strings.Index(stripped, "*/")
			if idx < 0 {
				continue
			}
			inBlockComment = false
			stripped = pyStrip(stripped[idx+2:])
			if stripped == "" {
				continue
			}
		}

		if open := strings.Index(stripped, "/*"); open >= 0 {
			if strings.Contains(stripped[open+2:], "*/") {
				stripped = pyStrip(blockComment.ReplaceAllString(stripped, ""))
				if stripped == "" {
					continue
				}
			} else {
				inBlockComment = true
				stripped = pyStrip(stripped[:open])
				if stripped == "" {
					continue
				}
			}
		}

		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if commentIdx := strings.Index(stripped, "//"); commentIdx >= 0 {
			stripped = pyStrip(stripped[:commentIdx])
		}

		if m := tsReexport.FindStringSubmatch(stripped); m != nil {
			for _, namePart := range strings.Split(m[1], ",") {
				namePart = pyStrip(namePart)
				if idx := strings.LastIndex(namePart, " as "); idx >= 0 {
					namePart = pyStrip(namePart[idx+len(" as "):])
				}
				add(namePart)
			}
			continue
		}

		if m := tsDefault.FindStringSubmatch(stripped); m != nil {
			add(m[1])
			continue
		}

		for _, pattern := range []*regexp.Regexp{tsNamedFunc, tsClass, tsVar, tsType} {
			if m := pattern.FindStringSubmatch(stripped); m != nil {
				add(m[1])
				break
			}
		}
	}

	return symbols, nil
}

// ModuleDocstring is the module-level JSDoc description of a source file.
func (e *extractor) ModuleDocstring(path string) (string, error) {
	source, err := extractors.ReadSource(path)
	if err != nil {
		return "", nil
	}
	jsdoc := extractModuleJSDoc(source)
	if jsdoc == nil {
		return "", nil
	}
	return jsdoc.Description, nil
}

// SymbolDetails reports what a source file says about one symbol's parameters
// and return value. A dotted name selects a member of a class or interface
// ("Router.handle").
func (e *extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	if strings.Contains(symbol, ".") {
		return dottedSymbolDetails(source, symbol), nil
	}

	funcRe := regexp.MustCompile(
		`(?:export` + pySpace + `+)?(?:default` + pySpace + `+)?(?:async` + pySpace + `+)?function` +
			pySpace + `+` + regexp.QuoteMeta(symbol) + pySpace + `*\(`)
	match := funcRe.FindStringIndex(source)
	if match == nil {
		return nil, nil
	}

	return tsSymbolDetails(source, match[0]), nil
}
