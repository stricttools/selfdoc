package swift

import (
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// kindOrder is the order the ref directive renders declarations in: the types
// first, then the functions, then the properties.
var kindOrder = []string{"type", "func", "prop"}

// sourceFile is one file the ref directive reads, kept in the order the
// directive collected them.
type sourceFile struct {
	name    string
	content string
}

// handleRef renders a file's module documentation and every public or open
// declaration it carries, each with its doc comment.
func handleRef(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::ref requires a file path argument"), nil
	}

	resolved := resolveSwiftPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	var files []sourceFile
	if extractors.IsDir(resolved) {
		swiftFiles := swiftFilesIn(resolved)
		if len(swiftFiles) == 0 {
			return extractors.FormatError("no .swift files in '" + path + "'"), nil
		}
		for _, sf := range swiftFiles {
			content, err := extractors.ReadSource(util.PathJoin(resolved, sf))
			if err != nil {
				content = ""
			}
			files = append(files, sourceFile{name: sf, content: content})
		}
	} else {
		content, err := extractors.ReadSource(resolved)
		if err != nil {
			return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
		}
		files = []sourceFile{{name: filepath.Base(resolved), content: content}}
	}

	var parts []string
	parts = append(parts, extractors.SymbolHeading(2, path))

	// The module doc comes from the first file that carries one.
	for _, file := range files {
		if moduleDoc := extractModuleDoc(file.content); moduleDoc != "" {
			parts = append(parts, "", extractors.FormatDocstring(moduleDoc, 2))
			break
		}
	}

	var declarations []declaration
	for _, file := range sortedByName(files) {
		declarations = append(declarations, extractPubDeclarations(file.content)...)
	}

	if target != nil && *target != "" {
		for _, decl := range declarations {
			if decl.name == *target {
				return renderDeclaration(decl), nil
			}
		}
		return extractors.FormatError(
			"symbol '" + *target + "' not found in '" + path + "'",
		), nil
	}

	for _, kind := range kindOrder {
		for _, decl := range declarations {
			if decl.kind != kind {
				continue
			}
			parts = append(parts, "", extractors.SymbolHeading(3, decl.name), "")
			parts = append(parts, "```swift\n"+decl.signature+"\n```")
			if decl.doc != "" {
				parts = append(parts, "", extractors.DemoteDocHeadings(decl.doc, 3))
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

// renderDeclaration renders one declaration, which is what a ref directive
// naming a target emits.
func renderDeclaration(decl declaration) string {
	parts := []string{
		extractors.SymbolHeading(3, decl.name),
		"",
		"```swift\n" + decl.signature + "\n```",
	}
	if decl.doc != "" {
		parts = append(parts, "", extractors.DemoteDocHeadings(decl.doc, 3))
	}
	return strings.Join(parts, "\n")
}

// sortedByName orders files by name, the order the declarations are read in.
func sortedByName(files []sourceFile) []sourceFile {
	ordered := make([]sourceFile, len(files))
	copy(ordered, files)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].name < ordered[j-1].name; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	return ordered
}

// handleProseDesc renders a file's module doc comment and nothing else.
func handleProseDesc(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::prose-desc requires a file path argument"), nil
	}

	resolved := resolveSwiftPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	if extractors.IsDir(resolved) {
		for _, sf := range swiftFilesIn(resolved) {
			content, err := extractors.ReadSource(util.PathJoin(resolved, sf))
			if err != nil || content == "" {
				continue
			}
			if doc := extractModuleDoc(content); doc != "" {
				return extractors.FormatDocstring(doc, 1), nil
			}
		}
		return extractors.FormatError("no module doc comment found in '" + path + "'"), nil
	}

	content, err := extractors.ReadSource(resolved)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}
	doc := extractModuleDoc(content)
	if doc == "" {
		return extractors.FormatError("no module doc comment found in '" + path + "'"), nil
	}
	return extractors.FormatDocstring(doc, 1), nil
}

// handleTableSchema renders a struct's fields as a table. A JSON or TOML path
// is a config file rather than Swift source, and is rendered as one.
func handleTableSchema(
	path string,
	target *string,
	body []string,
	sourcePaths []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::table-schema requires a file path argument"), nil
	}

	if strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".toml") {
		return extractors.HandleTableConfig(path, nil, body, sourcePaths, baseDir, attrs)
	}

	fullPath := resolveFilePath(path, sourcePaths, baseDir)
	if fullPath == "" {
		return extractors.FormatError("file '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	structs := extractStructFields(source)
	if len(structs) == 0 {
		return extractors.FormatError("no struct types found in '" + path + "'"), nil
	}

	if target != nil && *target != "" {
		for _, info := range structs {
			if info.name == *target {
				return formatStructTable(info)
			}
		}
		return extractors.FormatError(
			"struct '" + *target + "' not found in '" + path + "'",
		), nil
	}

	var results []string
	for _, info := range structs {
		results = append(results, extractors.SymbolHeading(3, info.name), "")
		if info.doc != "" {
			results = append(results, extractors.DemoteDocHeadings(info.doc, 3), "")
		}
		table, err := formatStructTable(info)
		if err != nil {
			return "", err
		}
		results = append(results, table)
	}
	return strings.Join(results, "\n"), nil
}
