package svelte

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// propsTableHeaders are the columns a properties table carries.
var propsTableHeaders = []string{"Prop", "Type", "Default", "Bindable"}

// resolveSveltePath resolves a directive's path argument to a component file:
// the path as written, relative to the base directory and to each declared
// source path, then with the .svelte extension appended. It returns the empty
// string when nothing resolves.
func resolveSveltePath(pathArg string, sourcePaths []string, baseDir string) string {
	var candidates []string

	candidates = append(candidates, util.PathJoin(baseDir, pathArg))
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg))
	}

	if _, ext := splitExt(pathArg); ext != ".svelte" {
		candidates = append(candidates, util.PathJoin(baseDir, pathArg+".svelte"))
		for _, sp := range sourcePaths {
			candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg+".svelte"))
		}
	}

	for _, candidate := range candidates {
		if extractors.IsFile(candidate) {
			return candidate
		}
	}
	return ""
}

// propRow renders one property as a table row. The type and the default are
// code spans when the component declares them and empty cells when it does
// not, so an undeclared default is blank rather than an empty code span.
func propRow(p prop) []string {
	typeCell := ""
	if p.Type != "" {
		typeCell = "`" + p.Type + "`"
	}
	defaultCell := ""
	if p.Default != "" {
		defaultCell = "`" + p.Default + "`"
	}
	bindable := "No"
	if p.Bindable {
		bindable = "Yes"
	}
	return []string{"`" + p.Name + "`", typeCell, defaultCell, bindable}
}

// componentProps are the properties a component declares, from whichever of
// the two declaration forms it uses: the $props() rune first, and "export let"
// when the rune found none.
func componentProps(instance string) []prop {
	props := extractProps(instance)
	if len(props) == 0 {
		props = extractLegacyProps(instance)
	}
	return props
}

// handleRef renders a component's reference documentation: its name, its own
// documentation, its properties, its instance exports and its module exports
// -- or one named property or export on its own.
//
// The sections are always in that order, and each is omitted when the
// component declares nothing for it.
func handleRef(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError("ref requires a file path argument"), nil
	}

	filepath := resolveSveltePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("component '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	componentName, _ := splitExt(baseName(filepath))

	blocks := extractScriptBlocks(source)
	componentDocText := extractComponentDoc(source)
	props := componentProps(blocks.Instance)
	instanceExports := extractExports(blocks.Instance)
	moduleExports := extractExports(blocks.Module)

	if target != nil && *target != "" {
		for _, p := range props {
			if p.Name != *target {
				continue
			}
			table, err := extractors.RenderTable(propsTableHeaders, [][]string{propRow(p)})
			if err != nil {
				return "", err
			}
			return strings.Join([]string{
				extractors.SymbolHeading(3, p.Name), "", table,
			}, "\n"), nil
		}
		for _, group := range [][]export{instanceExports, moduleExports} {
			for _, exp := range group {
				if exp.Name != *target {
					continue
				}
				return strings.Join([]string{
					extractors.SymbolHeading(3, exp.Name),
					"",
					"```typescript\n" + exp.Signature + "\n```",
				}, "\n"), nil
			}
		}
		return extractors.FormatError("symbol '" + *target + "' not found in '" + path + "'"), nil
	}

	parts := []string{extractors.SymbolHeading(2, componentName)}

	if componentDocText != "" {
		parts = append(parts, "", extractors.DemoteDocHeadings(componentDocText, 2))
	}

	if len(props) > 0 {
		rows := make([][]string, 0, len(props))
		for _, p := range props {
			rows = append(rows, propRow(p))
		}
		table, err := extractors.RenderTable(propsTableHeaders, rows)
		if err != nil {
			return "", err
		}
		parts = append(parts, "", "### Props", "", table)
	}

	for _, section := range []struct {
		heading string
		exports []export
	}{
		{heading: "### Instance Exports", exports: instanceExports},
		{heading: "### Module Exports", exports: moduleExports},
	} {
		if len(section.exports) == 0 {
			continue
		}
		parts = append(parts, "", section.heading)
		for _, exp := range section.exports {
			parts = append(parts,
				"",
				extractors.SymbolHeading(4, exp.Name),
				"",
				"```typescript\n"+exp.Signature+"\n```",
			)
		}
	}

	return strings.Join(parts, "\n"), nil
}

// handleProseDesc renders only a component's own documentation, without the
// properties and exports :::ref adds.
func handleProseDesc(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError("prose-desc requires a file path argument"), nil
	}

	filepath := resolveSveltePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("component '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	doc := extractComponentDoc(source)
	if doc == "" {
		return extractors.FormatError("no component-level JSDoc found in '" + path + "'"), nil
	}

	return doc, nil
}

// handleTableSchema renders a component's properties as a table. A JSON or
// TOML path is a config file rather than a component, and is rendered by the
// shared config handler.
func handleTableSchema(
	path string,
	_ *string,
	body []string,
	sourcePaths []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError("table-schema requires a file path argument"), nil
	}

	if _, ext := splitExt(path); strings.ToLower(ext) == ".json" || strings.ToLower(ext) == ".toml" {
		return extractors.HandleTableConfig(path, nil, body, sourcePaths, baseDir, attrs)
	}

	filepath := resolveSveltePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("component '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	props := componentProps(extractScriptBlocks(source).Instance)
	if len(props) == 0 {
		return extractors.FormatError("no props found in '" + path + "'"), nil
	}

	rows := make([][]string, 0, len(props))
	for _, p := range props {
		rows = append(rows, propRow(p))
	}
	return extractors.RenderTable(propsTableHeaders, rows)
}
