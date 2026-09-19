package dart

import (
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// kindOrder is the order the ref directive renders declarations in: the types
// first, then the values, then the functions. A reference page for a Dart
// library reads as its author grouped it rather than as the file happens to be
// laid out.
var kindOrder = []string{
	"class", "mixin", "enum", "extension_type", "typedef", "const", "var", "function",
}

// sourceFile is one file the ref directive reads, kept in the order the
// directive collected them.
type sourceFile struct {
	name    string
	content string
}

// handleRef renders a library's own documentation and every public declaration
// it carries, its part files' and its exports' included.
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

	resolved := resolveDartPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	isDir := extractors.IsDir(resolved)
	var files []sourceFile

	if isDir {
		dartFiles := dartFilesIn(resolved)
		if len(dartFiles) == 0 {
			return extractors.FormatError("no .dart files in '" + path + "'"), nil
		}
		for _, df := range dartFiles {
			content, err := extractors.ReadSource(util.PathJoin(resolved, df))
			if err != nil {
				content = ""
			}
			files = append(files, sourceFile{name: df, content: content})
		}
	} else {
		if isGeneratedFile(resolved) {
			return extractors.FormatError("'" + path + "' is a generated file"), nil
		}
		content, err := extractors.ReadSource(resolved)
		if err != nil {
			return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
		}
		files = []sourceFile{{name: filepath.Base(resolved), content: content}}
	}

	var parts []string
	parts = append(parts, extractors.SymbolHeading(2, path))

	// The library doc comes from the first file that carries one.
	for _, file := range files {
		if libDoc := extractLibraryDoc(file.content); libDoc != "" {
			parts = append(parts, "", parseDartDoc(libDoc, 2))
			break
		}
	}

	// The declarations come from every file, in file-name order.
	var declarations []declaration
	for _, file := range sortedByName(files) {
		declarations = append(declarations, extractDeclarations(file.content)...)
	}

	// A single file also contributes its part files' and its exports'
	// declarations, with a local declaration shadowing a re-exported name.
	if !isDir {
		declarations = append(declarations, followPartsDeclarations(resolved, files[0].content)...)

		if baseDirRef := findProjectRoot(resolved); baseDirRef != "" {
			exportDecls := followExportsDeclarations(
				resolved, files[0].content, baseDirRef, map[string]bool{},
			)
			localNames := map[string]bool{}
			for _, decl := range declarations {
				localNames[decl.name] = true
			}
			for _, decl := range exportDecls {
				if !localNames[decl.name] {
					declarations = append(declarations, decl)
				}
			}
		}
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
			parts = append(parts, "```dart\n"+decl.signature+"\n```")
			if decl.doc != "" {
				parts = append(parts, "", parseDartDoc(decl.doc, 3))
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
		"```dart\n" + decl.signature + "\n```",
	}
	if decl.doc != "" {
		parts = append(parts, "", parseDartDoc(decl.doc, 3))
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

// handleProseDesc renders a library's own doc comment and nothing else.
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

	resolved := resolveDartPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	if extractors.IsDir(resolved) {
		for _, df := range dartFilesIn(resolved) {
			content, err := extractors.ReadSource(util.PathJoin(resolved, df))
			if err != nil || content == "" {
				continue
			}
			if doc := extractLibraryDoc(content); doc != "" {
				return parseDartDoc(doc, 1), nil
			}
		}
		return extractors.FormatError("no library doc comment found in '" + path + "'"), nil
	}

	content, err := extractors.ReadSource(resolved)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}
	doc := extractLibraryDoc(content)
	if doc == "" {
		return extractors.FormatError("no library doc comment found in '" + path + "'"), nil
	}
	return parseDartDoc(doc, 1), nil
}

// handleTableSchema renders a class's fields as a table. A JSON or TOML path is
// a config file rather than Dart source, and is rendered as one.
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

	fullPath := resolveDartPath(path, sourcePaths, baseDir)
	if fullPath == "" || extractors.IsDir(fullPath) {
		return extractors.FormatError("file '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	classes := extractClassFields(source)
	if len(classes) == 0 {
		return extractors.FormatError("no classes with fields found in '" + path + "'"), nil
	}

	if target != nil && *target != "" {
		for _, class := range classes {
			if class.name == *target {
				return formatClassTable(class)
			}
		}
		return extractors.FormatError(
			"class '" + *target + "' not found in '" + path + "'",
		), nil
	}

	var results []string
	for _, class := range classes {
		results = append(results, extractors.SymbolHeading(3, class.name), "")
		if class.doc != "" {
			results = append(results, extractors.DemoteDocHeadings(class.doc, 3), "")
		}
		table, err := formatClassTable(class)
		if err != nil {
			return "", err
		}
		results = append(results, table)
	}
	return strings.Join(results, "\n"), nil
}
