// Package python resolves selfdoc's directives against Python source.
//
// # What the pages are built from
//
// Every Python reference page selfdoc has ever produced was rendered from the
// stdlib ast module: ast.unparse decides how an annotation, a default value and
// a base class read, and the tree's child order decides what order the page
// lists symbols in. Both are reproduced here over a tree-sitter parse -- a
// pure-Go one, with the Python grammar's tables read from the parser library's
// embedded blob -- so documenting a Python project needs no interpreter on the
// machine and no cgo in the build.
//
// The split inside the package follows that: parse.go reads the tree into the
// shape ast has (docstrings, signatures, line spans, __all__, re-export
// statements, the dataclass and pydantic predicates), unparse.go reproduces
// ast.unparse's rendering of an expression, and handlers.go does everything a
// reader sees -- which symbols are skipped, how the Markdown is assembled, how
// docstring sections are formatted, which parameters count as documented.
//
// A parse is cached per file for the life of the extractor, because one page
// asks about the same module several times. A file that cannot be read or does
// not parse answers empty or renders an error marker, as it always has: that is
// a property of the file, not of the machine.
package python

import (
	"os"
	"strings"
	"sync"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/util"
)

// Extractor reads Python source through an in-process tree-sitter parse.
type Extractor struct {
	extractors.Base

	mu       sync.Mutex
	analyses map[string]*analysis
}

// New builds the Python extractor. Reading a Python file is a file read and a
// parse, and neither spawns anything.
func New() extractors.Extractor {
	extractor := &Extractor{analyses: map[string]*analysis{}}
	extractor.Base = extractors.NewBase("python", map[string]extractors.Handler{
		"ref":          extractor.handleModule,
		"code-test":    extractor.handleTest,
		"table-schema": extractor.handleSchema,
		"code-help":    extractor.handleCLI,
		"table-config": extractors.HandleTableConfig,
		"prose-desc":   extractor.handleProseDesc,
	})
	return extractor
}

func init() { extractors.Register("python", New) }

// Detect reports whether dir carries a Python project's marker files.
func (e *Extractor) Detect(dir string) bool {
	return extractors.IsFile(util.PathJoin(dir, "pyproject.toml")) ||
		extractors.IsFile(util.PathJoin(dir, "setup.py"))
}

// FileExtensions is the single extension Python owns.
func (e *Extractor) FileExtensions() []string { return []string{".py"} }

// ResolvePath resolves a dotted module path, a package path or a file path to
// a .py file.
func (e *Extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveModulePath(pathArg, sourcePaths, baseDir)
}

// PublicSymbols lists the symbols a module exports.
//
// A module that defines __all__ as a literal list or tuple of strings is taken
// at its word -- those names ARE its public API, underscore-prefixed ones
// included. Otherwise the heuristic applies: top-level functions and classes
// whose name does not begin with an underscore.
func (e *Extractor) PublicSymbols(file string) ([]string, error) {
	parsed, err := e.analyze(file)
	if err != nil {
		return nil, err
	}
	if !parsed.usable() {
		return nil, nil
	}
	if parsed.Document.AllNames != nil {
		return parsed.Document.AllNames, nil
	}
	var symbols []string
	for _, declaration := range parsed.Document.Declarations {
		if strings.HasPrefix(declaration.Name, "_") {
			continue
		}
		symbols = append(symbols, declaration.Name)
	}
	return symbols, nil
}

// ModuleDocstring is a module's own docstring, with soft-wrapped prose joined.
func (e *Extractor) ModuleDocstring(path string) (string, error) {
	parsed, err := e.analyze(path)
	if err != nil {
		return "", err
	}
	if !parsed.usable() {
		return "", nil
	}
	return prose.JoinWrappedLines(parsed.Document.docstring()), nil
}

// SymbolDetails reports one symbol's parameters and return value.
//
// A dotted name selects a member of a class (MyClass.my_method); a plain name
// is looked for among the top-level functions and classes first, and then
// among each class's methods, in the order the file declares them.
func (e *Extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	parsed, err := e.analyze(file)
	if err != nil {
		return nil, err
	}
	if !parsed.usable() {
		return nil, nil
	}
	declarations := parsed.Document.Declarations

	if typeName, memberName, dotted := cutLast(symbol, "."); dotted {
		for _, node := range declarations {
			if node.Kind != "class" || node.Name != typeName {
				continue
			}
			for _, member := range node.Members {
				switch {
				case member.Kind == "function" && member.Name == memberName:
					return buildSymbolDetails(member), nil
				case member.Kind == "class" && member.Name == memberName:
					return classSymbolDetails(member), nil
				}
			}
			return nil, nil
		}
		return nil, nil
	}

	for _, node := range declarations {
		if node.Kind == "function" {
			if node.Name == symbol {
				return buildSymbolDetails(node), nil
			}
			continue
		}
		if node.Name == symbol {
			return classSymbolDetails(node), nil
		}
		for _, member := range node.Members {
			if member.Kind == "function" && member.Name == symbol {
				return buildSymbolDetails(member), nil
			}
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

// analysis is one file as this package sees it: the bytes it read and the
// document read out of their syntax tree.
type analysis struct {
	// Source is the file's text, empty when ReadError is set.
	Source string
	// ReadError is why the file could not be read, nil when it was.
	ReadError error
	// Document is what the tree said, nil when ReadError is set.
	Document *document
}

// usable reports whether the file was read and parsed, which is the
// precondition for every question about its contents.
func (a *analysis) usable() bool {
	return a.ReadError == nil && a.Document != nil && a.Document.SyntaxError == nil
}

// analyze reads a file and parses it, caching the result.
//
// The cache is per extractor and keyed by the path as the caller spelled it.
// One reference page asks about the same module for its docstring, its symbol
// list and each symbol's details, and a fresh parse per question would
// dominate the build.
func (e *Extractor) analyze(filePath string) (*analysis, error) {
	e.mu.Lock()
	cached, ok := e.analyses[filePath]
	e.mu.Unlock()
	if ok {
		return cached, nil
	}

	data, readErr := os.ReadFile(filePath)
	if readErr != nil {
		result := &analysis{ReadError: readErr}
		e.remember(filePath, result)
		return result, nil
	}

	parsed, err := parseDocument(filePath, data)
	if err != nil {
		return nil, err
	}
	result := &analysis{Source: string(data), Document: parsed}
	e.remember(filePath, result)
	return result, nil
}

func (e *Extractor) remember(filePath string, result *analysis) {
	e.mu.Lock()
	e.analyses[filePath] = result
	e.mu.Unlock()
}

// document is what one file's syntax tree says about it.
type document struct {
	// SyntaxError is the rendering of the parse failure, nil when the file
	// parsed.
	SyntaxError *string
	// Docstring is the module docstring, nil when the module has none.
	Docstring *string
	// AllNames is the module's __all__ when it is a literal list or tuple of
	// strings. It is nil both when there is no __all__ and when there is one
	// that is not such a literal -- the two cases the heuristic covers alike.
	AllNames []string
	// Declarations are the module's top-level functions and classes, in source
	// order.
	Declarations []declaration
	// Reexports are the module-level re-export statements and constants, in
	// source order, including the ones nested one level inside a top-level try
	// or if.
	Reexports []reexport
	// CLIConstants are the module-level HELP and USAGE string constants.
	CLIConstants []cliConstant
}

// docstring is the module docstring, empty when there is none.
func (d *document) docstring() string {
	if d.Docstring == nil {
		return ""
	}
	return *d.Docstring
}

// declaration is one function or class read out of the tree.
type declaration struct {
	// Kind is "function" or "class".
	Kind string
	// Name is the declared name.
	Name string
	// IsAsync reports an "async def", which the rendered signature spells out.
	IsAsync bool
	// Doc is the declaration's own docstring, nil when it has none.
	Doc *string
	// Signature is the parenthesized parameter list and return annotation, as
	// ast.unparse renders their parts. Functions only.
	Signature string
	// ClassSignature is the "class Name(Base):" line. Classes only.
	ClassSignature string
	// Lineno and EndLineno are the declaration's inclusive one-based line span,
	// which is what a code-test directive slices out of the source.
	Lineno    int
	EndLineno int
	// IsDataclass and IsPydantic are the two syntactic predicates that decide
	// whether a docstring-less class renders a field table. Classes only.
	IsDataclass bool
	IsPydantic  bool
	// Fields are the class's annotated assignments. Classes only.
	Fields []field
	// Members are the class's own functions and classes, in source order.
	// Classes only.
	Members []declaration
	// Params are the parameters a symbol-details report names. Functions only.
	Params []paramInfo
	// ReturnType is the declared return annotation, nil when there is none.
	// Functions only.
	ReturnType *string
}

// documented is the declaration's docstring, empty when it has none. An empty
// docstring and an absent one are the same thing to every caller here, which
// is why this collapses them.
func (d *declaration) documented() string {
	if d.Doc == nil {
		return ""
	}
	return *d.Doc
}

// field is one annotated assignment in a class body.
type field struct {
	// Name is the field name.
	Name string
	// Type is the unparsed annotation.
	Type string
	// Default is the unparsed default value, empty when the field has none.
	Default string
	// Lineno is the field's one-based line, which is where an inline comment
	// documenting it would be.
	Lineno int
}

// reexport is one module-level re-export or constant, with the source line that
// declares it.
type reexport struct {
	Name string
	Stub string
}

// cliConstant is one module-level HELP or USAGE string.
type cliConstant struct {
	Name  string
	Value string
}

// paramInfo is one parameter as the tree declares it, before this package
// decides whether the documentation covers it.
type paramInfo struct {
	// Name carries the variadic or keyword prefix the signature writes.
	Name string
	// Type is the unparsed annotation, nil when the parameter has none.
	Type *string
}

// buildSymbolDetails reports a function's parameters and return value, marking
// each against what its own docstring documents.
func buildSymbolDetails(node declaration) *extractors.SymbolDetails {
	sections := extractors.ParseDocstringSections(node.documented())

	documentedNames := map[string]bool{}
	for _, param := range sections.Params {
		documentedNames[param.Name] = true
	}

	params := make([]extractors.SymbolParam, 0, len(node.Params))
	for _, param := range node.Params {
		bare := strings.TrimLeft(param.Name, "*")
		params = append(params, extractors.SymbolParam{
			Name:       param.Name,
			Type:       param.Type,
			Documented: documentedNames[bare] || documentedNames[param.Name],
		})
	}

	return &extractors.SymbolDetails{
		Params:           params,
		ReturnType:       node.ReturnType,
		ReturnDocumented: sections.Returns != nil,
	}
}

// classSymbolDetails reports a class's constructor as the class's own
// parameters. A class with no __init__ takes none, and there is nothing about
// its return value left to document.
func classSymbolDetails(node declaration) *extractors.SymbolDetails {
	for _, member := range node.Members {
		if member.Kind == "function" && !member.IsAsync && member.Name == "__init__" {
			return buildSymbolDetails(member)
		}
	}
	return &extractors.SymbolDetails{ReturnDocumented: true}
}

// resolveModulePath resolves a module argument to a .py file.
//
// A dotted path is tried as a module and then as a package under each declared
// source path and then under the base directory; a path already ending in .py
// is tried as a file last.
func resolveModulePath(arg string, sourcePaths []string, baseDir string) string {
	dottedAsPath := strings.ReplaceAll(arg, ".", "/") + ".py"
	dottedAsPackage := strings.ReplaceAll(arg, ".", "/") + "/__init__.py"

	var candidates []string
	for _, sourcePath := range sourcePaths {
		candidates = append(candidates,
			util.PathJoin(baseDir, sourcePath, dottedAsPath),
			util.PathJoin(baseDir, sourcePath, dottedAsPackage),
		)
	}
	candidates = append(candidates,
		util.PathJoin(baseDir, dottedAsPath),
		util.PathJoin(baseDir, dottedAsPackage),
	)

	if strings.HasSuffix(arg, ".py") {
		candidates = append(candidates, util.PathJoin(baseDir, arg))
		for _, sourcePath := range sourcePaths {
			candidates = append(candidates, util.PathJoin(baseDir, sourcePath, arg))
		}
	}

	for _, candidate := range candidates {
		if extractors.IsFile(candidate) {
			return candidate
		}
	}

	return ""
}
