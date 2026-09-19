package zig

import (
	"os"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// kindOrder is the order a reference page renders the declaration kinds in:
// the types and values a reader looks up first, then the functions.
var kindOrder = []string{"const", "var", "fn"}

// sourceFile is one file a directive reads, named by its base name.
type sourceFile struct {
	Name    string
	Content string
}

// resolveZigPath resolves a directive's path argument to a Zig source file or
// to a directory of them: each declared source path as a prefix, then the base
// directory, and the .zig extension appended to each candidate that is not
// itself a file. It returns the empty string when nothing resolves.
//
// A directory counts only when it actually contains Zig source, so a
// same-named directory beside the file the author meant does not shadow it.
func resolveZigPath(pathArg string, sourcePaths []string, baseDir string) string {
	var candidates []string
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, pathArg))
	}
	candidates = append(candidates, util.PathJoin(baseDir, pathArg))

	for _, candidate := range candidates {
		if extractors.IsDir(candidate) && containsZigSource(candidate) {
			return candidate
		}
		if extractors.IsFile(candidate) {
			return candidate
		}
		if withExtension := candidate + ".zig"; extractors.IsFile(withExtension) {
			return withExtension
		}
	}

	return ""
}

// containsZigSource reports whether dir holds at least one .zig file.
func containsZigSource(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".zig") {
			return true
		}
	}
	return false
}

// resolveFilePath resolves a path argument to a file, relative to the base
// directory or to a declared source path. It appends no extension: the
// directives that use it name a file.
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

// zigFilesIn lists the .zig files in dir, sorted by name.
func zigFilesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".zig") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// handleRef renders a file's or a directory's reference documentation: the
// module doc, then every public declaration grouped by kind -- or one named
// declaration on its own.
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

	resolved := resolveZigPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	var files []sourceFile
	if extractors.IsDir(resolved) {
		names := zigFilesIn(resolved)
		if len(names) == 0 {
			return extractors.FormatError("no .zig files in '" + path + "'"), nil
		}
		for _, name := range names {
			content, err := extractors.ReadSource(util.PathJoin(resolved, name))
			if err != nil {
				content = ""
			}
			files = append(files, sourceFile{Name: name, Content: content})
		}
	} else {
		content, err := extractors.ReadSource(resolved)
		if err != nil {
			return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
		}
		files = []sourceFile{{Name: baseName(resolved), Content: content}}
	}

	parts := []string{extractors.SymbolHeading(2, path)}

	// The module doc comes from the first file that has one.
	for _, file := range files {
		if moduleDoc := extractModuleDoc(file.Content); moduleDoc != "" {
			parts = append(parts, "", extractors.FormatDocstring(moduleDoc, 2))
			break
		}
	}

	var declarations []declaration
	for _, file := range files {
		declarations = append(declarations, extractPubDeclarations(file.Content)...)
	}

	if target != nil && *target != "" {
		for _, decl := range declarations {
			if decl.Name != *target {
				continue
			}
			out := []string{
				extractors.SymbolHeading(3, decl.Name),
				"",
				"```zig\n" + decl.Signature + "\n```",
			}
			if decl.Doc != "" {
				out = append(out, "", extractors.FormatDocstring(decl.Doc, 3))
			}
			return strings.Join(out, "\n"), nil
		}
		return extractors.FormatError("symbol '" + *target + "' not found in '" + path + "'"), nil
	}

	for _, kind := range kindOrder {
		for _, decl := range declarations {
			if decl.Kind != kind {
				continue
			}
			parts = append(parts,
				"",
				extractors.SymbolHeading(3, decl.Name),
				"",
				"```zig\n"+decl.Signature+"\n```",
			)
			if decl.Doc != "" {
				parts = append(parts, "", extractors.FormatDocstring(decl.Doc, 3))
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

// handleProseDesc renders only the module documentation, without the
// declaration list :::ref adds.
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

	resolved := resolveZigPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	if extractors.IsDir(resolved) {
		for _, name := range zigFilesIn(resolved) {
			content, err := extractors.ReadSource(util.PathJoin(resolved, name))
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

// handleTableSchema renders a struct's fields as a table, or every public
// struct in the file when the directive names none. A JSON or TOML path is a
// config file rather than Zig source, and is rendered by the shared config
// handler.
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

	structs := extractStructs(source)
	if len(structs) == 0 {
		return extractors.FormatError("no struct types found in '" + path + "'"), nil
	}

	if target != nil && *target != "" {
		for _, s := range structs {
			if s.Name == *target {
				return formatStructTable(s)
			}
		}
		return extractors.FormatError("struct '" + *target + "' not found in '" + path + "'"), nil
	}

	var results []string
	for _, s := range structs {
		results = append(results, extractors.SymbolHeading(3, s.Name), "")
		if s.Doc != "" {
			results = append(results, extractors.DemoteDocHeadings(s.Doc, 3), "")
		}
		table, err := formatStructTable(s)
		if err != nil {
			return "", err
		}
		results = append(results, table)
	}
	return strings.Join(results, "\n"), nil
}

// handleCodeTest renders a file's test blocks, or one of them.
func handleCodeTest(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::code-test requires a file path argument"), nil
	}

	// A Zig test is named by a string literal, so the quotes are dropped from
	// the target whether or not the directive wrote them.
	var targetName *string
	if target != nil && *target != "" {
		trimmed := strings.Trim(*target, `"`)
		targetName = &trimmed
	}

	fullPath := resolveFilePath(path, sourcePaths, baseDir)
	if fullPath == "" {
		return extractors.FormatError("test file '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	if targetName == nil {
		tests := extractAllTestBlocks(source)
		if len(tests) == 0 {
			return extractors.FormatError("no test blocks found in '" + path + "'"), nil
		}
		results := make([]string, 0, len(tests))
		for _, test := range tests {
			results = append(results, "```zig\n"+test.Source+"\n```")
		}
		return strings.Join(results, "\n\n"), nil
	}

	extracted, ok := extractTestBlock(source, *targetName)
	if !ok {
		return extractors.FormatError(
			"test '" + *targetName + "' not found in '" + path + "'"), nil
	}
	return "```zig\n" + extracted + "\n```", nil
}
