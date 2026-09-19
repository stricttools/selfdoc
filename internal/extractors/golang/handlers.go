package golang

import (
	"os"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// kindOrder is the order a reference page groups a package's declarations in.
var kindOrder = []string{"const", "var", "type", "func", "method"}

// handleModule resolves a ref directive: a package's doc comment and its
// exported declarations, or one named declaration out of it.
func (e *Extractor) handleModule(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::module requires a package path argument"), nil
	}

	packageDir := resolvePackageDir(path, sourcePaths, baseDir)
	if packageDir == "" {
		return extractors.FormatError("package '" + path + "' not found"), nil
	}

	goFiles, err := nonTestGoFiles(packageDir)
	if err != nil || len(goFiles) == 0 {
		return extractors.FormatError("no .go files in '" + path + "'"), nil
	}

	contents := newFileContents()
	for _, name := range goFiles {
		data, readErr := os.ReadFile(util.PathJoin(packageDir, name))
		if readErr != nil {
			contents.add(name, "")
			continue
		}
		contents.add(name, string(data))
	}

	_, packageDoc := extractPackageDoc(contents)

	var declarations []goDeclaration
	for _, name := range goFiles {
		declarations = append(declarations, extractExportedDeclarations(contents.get(name))...)
	}

	if target != nil && *target != "" {
		for _, declaration := range declarations {
			if declaration.Name != *target {
				continue
			}
			parts := []string{
				extractors.SymbolHeading(3, declaration.Name),
				"",
				"```go\n" + declaration.Signature + "\n```",
			}
			if declaration.Doc != "" {
				parts = append(parts, "", extractors.FormatDocstring(declaration.Doc, 3))
			}
			return strings.Join(parts, "\n"), nil
		}
		return extractors.FormatError("symbol '" + *target + "' not found in '" + path + "'"), nil
	}

	parts := []string{extractors.SymbolHeading(2, path)}

	if packageDoc != "" {
		parts = append(parts, "", extractors.FormatDocstring(packageDoc, 2))
	}

	for _, kind := range kindOrder {
		for _, declaration := range declarations {
			if declaration.Kind != kind {
				continue
			}
			parts = append(parts,
				"",
				extractors.SymbolHeading(3, declaration.Name),
				"",
				"```go\n"+declaration.Signature+"\n```",
			)
			if declaration.Doc != "" {
				parts = append(parts, "", extractors.FormatDocstring(declaration.Doc, 3))
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

// handleTest resolves a code-test directive: a whole Go test file, or one
// function out of it with its doc comment.
func (e *Extractor) handleTest(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::test requires a file path argument"), nil
	}

	fullPath := resolveFilePath(path, sourcePaths, baseDir)
	if fullPath == "" {
		return extractors.FormatError("test file '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	if target == nil {
		return "```go\n" + pyRStrip(source) + "\n```", nil
	}

	extracted, ok := extractGoFunction(source, *target)
	if !ok {
		return extractors.FormatError("'" + *target + "' not found in '" + path + "'"), nil
	}

	return "```go\n" + extracted + "\n```", nil
}

// handleSchema resolves a table-schema directive: a struct's fields as a table,
// or -- for a config file path -- the config table.
func (e *Extractor) handleSchema(
	path string,
	target *string,
	body []string,
	sourcePaths []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::schema requires a file path argument"), nil
	}

	// A JSON, TOML or YAML path names a config file, not Go source. The target
	// is deliberately dropped: it named a Go type, and appending it to the path
	// would make a path no file has.
	for _, ext := range []string{".json", ".toml", ".yaml", ".yml"} {
		if strings.HasSuffix(path, ext) {
			return extractors.HandleTableConfig(path, nil, body, sourcePaths, baseDir, attrs)
		}
	}

	fullPath := resolveFilePath(path, sourcePaths, baseDir)
	if fullPath == "" {
		return extractors.FormatError("file '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	structs := extractStructs(source)

	if len(structs) == 0 {
		return extractors.FormatError("no struct types found in '" + path + "'"), nil
	}

	if target != nil && *target != "" {
		for _, structInfo := range structs {
			if structInfo.Name == *target {
				return formatStructTable(structInfo)
			}
		}
		return extractors.FormatError("struct '" + *target + "' not found in '" + path + "'"), nil
	}

	var results []string
	for _, structInfo := range structs {
		results = append(results, extractors.SymbolHeading(3, structInfo.Name), "")
		if structInfo.Doc != "" {
			results = append(results, extractors.DemoteDocHeadings(structInfo.Doc, 3), "")
		}
		table, err := formatStructTable(structInfo)
		if err != nil {
			return "", err
		}
		results = append(results, table)
	}
	return strings.Join(results, "\n"), nil
}

// handleProseDesc resolves a prose-desc directive: a package's doc comment
// alone, with no declaration list under it.
func (e *Extractor) handleProseDesc(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::prose-desc requires a package path argument"), nil
	}

	packageDir := resolvePackageDir(path, sourcePaths, baseDir)
	if packageDir == "" {
		return extractors.FormatError("package '" + path + "' not found"), nil
	}

	goFiles, err := nonTestGoFiles(packageDir)
	if err != nil || len(goFiles) == 0 {
		return extractors.FormatError("no .go files in '" + path + "'"), nil
	}

	contents := newFileContents()
	for _, name := range goFiles {
		data, readErr := os.ReadFile(util.PathJoin(packageDir, name))
		if readErr != nil {
			contents.add(name, "")
			continue
		}
		contents.add(name, string(data))
	}

	_, packageDoc := extractPackageDoc(contents)

	if packageDoc == "" {
		return extractors.FormatError("no package doc comment found in '" + path + "'"), nil
	}

	return extractors.FormatDocstring(packageDoc, 1), nil
}

// extractGoFunction slices a named function out of Go source, with the doc
// comment above it, by counting braces to find where its body ends.
func extractGoFunction(source, funcName string) (string, bool) {
	lines := strings.Split(source, "\n")
	pattern := funcNamePattern(funcName)

	for i, line := range lines {
		if !pattern.MatchString(pyStrip(line)) {
			continue
		}

		// Walk up over blank lines and then over the doc comment.
		docStart := i
		j := i - 1
		for j >= 0 && pyStrip(lines[j]) == "" {
			j--
		}
		for j >= 0 && strings.HasPrefix(pyStrip(lines[j]), "//") {
			docStart = j
			j--
		}

		braceCount := 0
		funcEnd := i
		started := false

		for k := i; k < len(lines); k++ {
			for _, ch := range lines[k] {
				if ch == '{' {
					braceCount++
					started = true
				} else if ch == '}' {
					braceCount--
				}
			}
			if started && braceCount == 0 {
				funcEnd = k
				break
			}
		}

		return strings.Join(lines[docStart:funcEnd+1], "\n"), true
	}

	return "", false
}
