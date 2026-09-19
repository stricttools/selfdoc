// Package golang resolves selfdoc's directives against Go source.
//
// It is a line scanner built on regular expressions, not a parse of the
// language: no Go toolchain is required, and a package that does not compile
// still documents. That is a deliberate trade, and it has consequences a reader
// of the generated pages can see -- a capitalized field key inside a composite
// literal in a var block is counted as an exported symbol, for instance. Those
// behaviors are reproduced here rather than fixed, because the pages, the
// coverage numbers and the stored description hashes of every Go project
// selfdoc documents were all produced by this scanner. Replacing it with a
// go/ast walk is its own change, with its own diff to review.
package golang

import (
	"os"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/util"
)

// exported is a regexp fragment matching an exported Go identifier: an
// uppercase ASCII letter and then identifier characters.
const exported = `[A-Z]` + extractors.PyWordClass + `*`

// identifier is a regexp fragment matching any Go identifier character run.
const identifier = extractors.PyWordClass + `+`

// The patterns the symbol scan tries against each line, in the order it tries
// them. A method is matched first, so its receiver joins its name.
var (
	goMethodRE = regexp.MustCompile(`^func\s+\(\s*` + identifier + `\s+\*?(` + identifier + `)\s*\)\s+(` + exported + `)\s*\(`)
	goFuncRE   = regexp.MustCompile(`^func\s+(` + exported + `)\s*\(`)
	goTypeRE   = regexp.MustCompile(`^type\s+(` + exported + `)\s+`)
	goVarRE    = regexp.MustCompile(`^var\s+(` + exported + `)`)
	goConstRE  = regexp.MustCompile(`^const\s+(` + exported + `)`)

	blockCommentRE = regexp.MustCompile(`/\*.*?\*/`)
	blockSymbolRE  = regexp.MustCompile(`^(` + exported + `)`)
)

// Extractor reads Go source by scanning its lines.
type Extractor struct {
	extractors.Base
}

// New builds the Go extractor. Every answer comes from reading files, which
// is not an effect.
func New() extractors.Extractor {
	extractor := &Extractor{}
	extractor.Base = extractors.NewBase("go", map[string]extractors.Handler{
		"ref":          extractor.handleModule,
		"code-test":    extractor.handleTest,
		"table-schema": extractor.handleSchema,
		"code-help":    extractor.handleCLI,
		"table-config": extractors.HandleTableConfig,
		"prose-desc":   extractor.handleProseDesc,
	})
	return extractor
}

func init() { extractors.Register("go", New) }

// Detect reports whether dir carries a Go module's marker file.
func (e *Extractor) Detect(dir string) bool {
	return extractors.IsFile(util.PathJoin(dir, "go.mod"))
}

// FileExtensions is the single extension Go owns.
func (e *Extractor) FileExtensions() []string { return []string{".go"} }

// ResolvePath resolves a package path to its directory. Go's unit of
// documentation is the package, so this returns a directory where the other
// extractors return a file.
func (e *Extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolvePackageDir(pathArg, sourcePaths, baseDir)
}

// PublicSymbols lists the exported symbols a Go file declares.
//
// A method is named by its receiver type (Server.Handle) so two types' methods
// of the same name do not collide. Lines inside line and block comments are
// skipped, and const and var blocks are scanned for their members.
func (e *Extractor) PublicSymbols(file string) ([]string, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}

	lines := strings.Split(string(source), "\n")
	var symbols []string
	seen := map[string]bool{}
	inBlockComment := false
	inConstVarBlock := false

	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		symbols = append(symbols, name)
	}

	for _, line := range lines {
		stripped := pyStrip(line)

		if inBlockComment {
			if index := strings.Index(stripped, "*/"); index >= 0 {
				inBlockComment = false
				stripped = pyStrip(stripped[index+2:])
				if stripped == "" {
					continue
				}
			} else {
				continue
			}
		}

		if open := strings.Index(stripped, "/*"); open >= 0 {
			if strings.Contains(stripped[open+2:], "*/") {
				stripped = pyStrip(blockCommentRE.ReplaceAllString(stripped, ""))
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

		if !inConstVarBlock {
			if strings.HasPrefix(stripped, "const (") || strings.HasPrefix(stripped, "var (") {
				inConstVarBlock = true
				continue
			}
		}

		if inConstVarBlock {
			if strings.HasPrefix(stripped, ")") {
				inConstVarBlock = false
				continue
			}
			// Inside a block, an exported member is one that starts with an
			// uppercase letter.
			if m := blockSymbolRE.FindStringSubmatch(stripped); m != nil {
				add(m[1])
			}
			continue
		}

		if m := goMethodRE.FindStringSubmatch(stripped); m != nil {
			add(m[1] + "." + m[2])
			continue
		}
		for _, pattern := range []*regexp.Regexp{goFuncRE, goTypeRE, goVarRE, goConstRE} {
			if m := pattern.FindStringSubmatch(stripped); m != nil {
				add(m[1])
				break
			}
		}
	}

	return symbols, nil
}

// ModuleDocstring is a Go package's doc comment, with soft-wrapped prose
// joined.
//
// path may be a directory -- what ResolvePath returns -- or a single .go file,
// whose directory is read instead. Test files are skipped.
func (e *Extractor) ModuleDocstring(path string) (string, error) {
	var packageDir string
	switch {
	case extractors.IsDir(path):
		packageDir = path
	case extractors.IsFile(path):
		packageDir = dirOf(path)
	default:
		return "", nil
	}

	names, err := nonTestGoFiles(packageDir)
	if err != nil {
		return "", nil
	}

	contents := newFileContents()
	for _, name := range names {
		data, err := os.ReadFile(util.PathJoin(packageDir, name))
		if err != nil {
			continue
		}
		contents.add(name, string(data))
	}
	if contents.len() == 0 {
		return "", nil
	}

	_, doc := extractPackageDoc(contents)
	return prose.JoinWrappedLines(doc), nil
}

// The symbol-details lookup builds its pattern around the name it is looking
// for, so only these prefixes are constant.
const (
	funcOfAnyReceiverPrefix = `^func\s+(?:\(.*?\)\s+)?`
	methodOfReceiverPrefix  = `^func\s+\(\s*` + identifier + `\s+\*?(` + identifier + `)\s*\)\s+`
)

// SymbolDetails reports a Go function's or method's parameters and return type,
// and whether its doc comment covers them.
//
// file may be a directory -- what ResolvePath returns -- in which case every
// non-test .go file in it is scanned in name order. A dotted name (Server.Handle)
// requires the receiver type to match; a plain name matches any function or
// method so spelled.
func (e *Extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	typeName, methodName, dotted := cutLast(symbol, ".")
	if !dotted {
		methodName = symbol
	}

	var goFiles []string
	switch {
	case extractors.IsDir(file):
		names, err := nonTestGoFiles(file)
		if err != nil {
			return nil, nil
		}
		for _, name := range names {
			goFiles = append(goFiles, util.PathJoin(file, name))
		}
	case extractors.IsFile(file):
		goFiles = []string{file}
	default:
		return nil, nil
	}

	var pattern *regexp.Regexp
	if dotted {
		pattern = regexp.MustCompile(methodOfReceiverPrefix + regexp.QuoteMeta(methodName) + `\s*\(`)
	} else {
		pattern = regexp.MustCompile(funcOfAnyReceiverPrefix + regexp.QuoteMeta(methodName) + `\s*\(`)
	}

	for _, goFile := range goFiles {
		source, err := os.ReadFile(goFile)
		if err != nil {
			continue
		}
		lines := strings.Split(string(source), "\n")
		for i, line := range lines {
			stripped := pyStrip(line)
			m := pattern.FindStringSubmatch(stripped)
			if m == nil {
				continue
			}
			if dotted && m[1] != typeName {
				continue
			}
			return goSymbolDetails(lines, i), nil
		}
	}

	return nil, nil
}

// cutLast splits s at the last occurrence of sep, the operation Python's
// str.rsplit(sep, 1) performs.
func cutLast(s, sep string) (before, after string, found bool) {
	index := strings.LastIndex(s, sep)
	if index < 0 {
		return s, "", false
	}
	return s[:index], s[index+len(sep):], true
}

// dirOf is the directory part of a path, the value Python's os.path.dirname
// reports.
func dirOf(path string) string {
	index := strings.LastIndex(path, "/")
	if index < 0 {
		return ""
	}
	if index == 0 {
		return "/"
	}
	return path[:index]
}

// nonTestGoFiles lists a directory's .go entries that are not test files, in
// name order.
//
// Python read the directory with os.listdir and sorted the result at each of
// the three call sites that needed order; one call site -- the package doc
// lookup -- did not sort, so which of several documented files answered
// depended on the filesystem's own order. Go's os.ReadDir is always sorted, so
// that lookup is deterministic here where it was not there.
func nonTestGoFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			names = append(names, name)
		}
	}
	return names, nil
}

// resolvePackageDir resolves a package path argument to a directory that holds
// at least one .go file, trying each declared source path as a prefix and then
// the base directory.
func resolvePackageDir(arg string, sourcePaths []string, baseDir string) string {
	var candidates []string
	for _, sourcePath := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sourcePath, arg))
	}
	candidates = append(candidates, util.PathJoin(baseDir, arg))

	for _, candidate := range candidates {
		if !extractors.IsDir(candidate) {
			continue
		}
		entries, err := os.ReadDir(candidate)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".go") {
				return candidate
			}
		}
	}

	return ""
}

// resolveFilePath resolves a file path against the base directory and then
// against each declared source path.
func resolveFilePath(filePath string, sourcePaths []string, baseDir string) string {
	candidates := []string{util.PathJoin(baseDir, filePath)}
	for _, sourcePath := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sourcePath, filePath))
	}
	for _, candidate := range candidates {
		if extractors.IsFile(candidate) {
			return candidate
		}
	}
	return ""
}
