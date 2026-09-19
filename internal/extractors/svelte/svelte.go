// Package svelte reads Svelte component source for selfdoc.
//
// It answers directives by scanning a .svelte file's <script> blocks with
// regular expressions: no Svelte compiler is required, and a component that
// does not compile still documents. What it reads out of a component is the
// component's own JSDoc block, the properties it accepts -- through the Svelte
// 5 $props() rune, including the $bindable() marker, or through the "export
// let" declarations Svelte 3 and 4 use -- and the functions and constants its
// instance and module scripts export.
//
// JSDoc blocks are parsed by the TypeScript extractor's parser rather than by
// a second one here: a component's doc comment is a JSDoc block, and two
// parsers for one comment dialect would drift.
package svelte

import (
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// extractor reads Svelte component source.
type extractor struct {
	extractors.Base
}

// New builds the Svelte extractor. It spawns nothing and writes nothing, it
// only reads source files.
func New() extractors.Extractor {
	e := &extractor{}
	e.Base = extractors.NewBase("svelte", map[string]extractors.Handler{
		"ref":          handleRef,
		"prose-desc":   handleProseDesc,
		"table-schema": handleTableSchema,
		"table-config": extractors.HandleTableConfig,
	})
	return e
}

func init() {
	extractors.Register("svelte", New)
}

// Detect reports whether dir is a Svelte project, by its svelte.config.js or
// svelte.config.ts.
func (e *extractor) Detect(dir string) bool {
	return extractors.IsFile(util.PathJoin(dir, "svelte.config.js")) ||
		extractors.IsFile(util.PathJoin(dir, "svelte.config.ts"))
}

// ResolvePath resolves a directive's path argument to a component file.
func (e *extractor) ResolvePath(pathArg string, sourcePaths []string, baseDir string) string {
	return resolveSveltePath(pathArg, sourcePaths, baseDir)
}

// FileExtensions lists the extensions the build walks a Svelte source tree
// for.
func (e *extractor) FileExtensions() []string {
	return []string{".svelte"}
}

// PublicSymbols lists what a component exposes: its own name, taken from the
// filename, then its properties, its instance exports and its module exports.
//
// The component's name comes first because the component itself is the thing a
// page documents; the rest are the ways a consumer addresses it.
func (e *extractor) PublicSymbols(file string) ([]string, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	componentName, _ := splitExt(baseName(file))
	symbols := []string{componentName}
	seen := map[string]bool{componentName: true}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		symbols = append(symbols, name)
	}

	blocks := extractScriptBlocks(source)

	for _, p := range componentProps(blocks.Instance) {
		add(p.Name)
	}
	for _, exp := range extractExports(blocks.Instance) {
		add(exp.Name)
	}
	for _, exp := range extractExports(blocks.Module) {
		add(exp.Name)
	}

	return symbols, nil
}

// ModuleDocstring is the component's own documentation.
func (e *extractor) ModuleDocstring(path string) (string, error) {
	source, err := extractors.ReadSource(path)
	if err != nil {
		return "", nil
	}
	return extractComponentDoc(source), nil
}

// SymbolDetails reports what a component says about one exported function's
// parameters and return value, looking in the instance script and then in the
// module script. A component's properties are not functions, so a property
// name answers with nothing.
func (e *extractor) SymbolDetails(file, symbol string) (*extractors.SymbolDetails, error) {
	source, err := extractors.ReadSource(file)
	if err != nil {
		return nil, nil
	}

	blocks := extractScriptBlocks(source)
	for _, scriptContent := range []string{blocks.Instance, blocks.Module} {
		if scriptContent == "" {
			continue
		}
		if details := symbolDetailsFromScript(scriptContent, symbol); details != nil {
			return details, nil
		}
	}

	return nil, nil
}
