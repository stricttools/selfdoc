package python

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// handleModule resolves a ref directive: a module's docstring, its functions
// and its classes, or one named symbol out of it.
func (e *Extractor) handleModule(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::module requires a module path argument"), nil
	}

	filePath := resolveModulePath(path, sourcePaths, baseDir)
	if filePath == "" {
		return extractors.FormatError("module '" + path + "' not found"), nil
	}

	parsed, marker, err := e.parsedOrMarker(filePath, path)
	if err != nil || marker != "" {
		return marker, err
	}

	if target != nil && *target != "" {
		return e.moduleTarget(parsed, path, *target)
	}

	// The display name comes from the dotted path the directive wrote, not from
	// the file it resolved to.
	moduleName := strings.ReplaceAll(path, "/", ".")
	moduleName = strings.TrimSuffix(moduleName, ".py")
	moduleName = strings.TrimSuffix(moduleName, ".__init__")

	parts := []string{extractors.SymbolHeading(2, moduleName)}

	if doc := parsed.Document.docstring(); doc != "" {
		parts = append(parts, "", extractors.FormatDocstring(doc, 2))
	}

	emitted := map[string]bool{}
	for _, node := range parsed.Document.Declarations {
		if node.Kind == "class" {
			markdown, ok, err := formatClass(node)
			if err != nil {
				return "", err
			}
			if ok {
				parts = append(parts, "", markdown)
				emitted[node.Name] = true
			}
			continue
		}
		if markdown, ok := formatFunction(node, 3); ok {
			parts = append(parts, "", markdown)
			emitted[node.Name] = true
		}
	}

	// A package's re-exports (from ._impl import X) and module-level constants
	// (__version__) that __all__ names but no local definition covers.
	for _, entry := range formatReexports(parsed.Document, emitted) {
		parts = append(parts, "", entry)
	}

	return strings.Join(parts, "\n"), nil
}

// moduleTarget renders the one symbol a ref directive named.
func (e *Extractor) moduleTarget(parsed *analysis, path, target string) (string, error) {
	undocumented := extractors.FormatError(
		"symbol '" + target + "' in '" + path + "' has no documentation")

	for _, node := range parsed.Document.Declarations {
		if node.Name != target {
			continue
		}
		if node.Kind == "class" {
			markdown, ok, err := formatClass(node)
			if err != nil {
				return "", err
			}
			if ok {
				return markdown, nil
			}
			return undocumented, nil
		}
		if markdown, ok := formatFunction(node, 3); ok {
			return markdown, nil
		}
		return undocumented, nil
	}

	// Not a locally defined class or function -- it may still be a re-export
	// (from ._impl import X) or a module-level constant (__version__).
	for _, entry := range parsed.Document.Reexports {
		if entry.Name == target {
			return extractors.SymbolHeading(3, target) + "\n\n```python\n" + entry.Stub + "\n```", nil
		}
	}
	return extractors.FormatError("symbol '" + target + "' not found in '" + path + "'"), nil
}

// formatReexports emits a heading and stub for each __all__ name no rendered
// definition already covers.
//
// This is stricter than the TypeScript extractor's re-export handling, on
// purpose: membership in a literal __all__ is the requirement, so an incidental
// "import os" cannot reach the page. A module with no literal __all__ emits
// nothing here.
func formatReexports(document *document, emitted map[string]bool) []string {
	if len(document.AllNames) == 0 {
		return nil
	}
	wanted := map[string]bool{}
	for _, name := range document.AllNames {
		if !emitted[name] {
			wanted[name] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	var entries []string
	seen := map[string]bool{}
	for _, entry := range document.Reexports {
		if !wanted[entry.Name] || seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true
		entries = append(entries,
			extractors.SymbolHeading(3, entry.Name)+"\n\n```python\n"+entry.Stub+"\n```")
	}
	return entries
}

// formatFunction renders a function or method. It declines a private item with
// no docstring: an underscore-prefixed name the author did not document is not
// something a reference page is for.
func formatFunction(node declaration, headingLevel int) (string, bool) {
	docstring := node.documented()

	if strings.HasPrefix(node.Name, "_") && docstring == "" {
		return "", false
	}

	keyword := "def"
	if node.IsAsync {
		keyword = "async def"
	}

	parts := []string{
		extractors.SymbolHeading(headingLevel, node.Name),
		"",
		"```python\n" + keyword + " " + node.Name + node.Signature + "\n```",
	}

	if docstring != "" {
		parts = append(parts, "", extractors.FormatDocstring(docstring, headingLevel))
	}

	return strings.Join(parts, "\n"), true
}

// formatClass renders a class and its methods.
//
// A dataclass or pydantic model with no docstring and no methods is rendered
// with a field table, so its name reaches coverage and the page says something
// useful. A public class is never dropped: with no docstring, no methods and no
// recognized field shape it still gets a heading and its signature line, since
// a class named in __all__ is public API by declaration.
//
// It declines only a private class with no docstring.
func formatClass(node declaration) (string, bool, error) {
	docstring := node.documented()

	if strings.HasPrefix(node.Name, "_") && docstring == "" {
		return "", false, nil
	}

	var methods []string
	for _, member := range node.Members {
		if member.Kind != "function" {
			continue
		}
		if markdown, ok := formatFunction(member, 4); ok {
			methods = append(methods, markdown)
		}
	}

	fieldTable := ""
	if docstring == "" && len(methods) == 0 && (node.IsDataclass || node.IsPydantic) {
		table, ok, err := formatDataclassFields(node)
		if err != nil {
			return "", false, err
		}
		if ok {
			fieldTable = table
		}
	}

	parts := []string{extractors.SymbolHeading(3, node.Name)}

	if docstring != "" {
		parts = append(parts, "", extractors.FormatDocstring(docstring, 3))
	}

	switch {
	case fieldTable != "":
		parts = append(parts, "", fieldTable)
	case docstring == "" && len(methods) == 0:
		// An empty class, or one whose field shape is not recognized: emit the
		// signature rather than dropping the class from the page.
		parts = append(parts, "", "```python\n"+node.ClassSignature+"\n```")
	}

	parts = append(parts, prefixEach("", methods)...)

	return strings.Join(parts, "\n"), true, nil
}

// prefixEach interleaves separator before every entry, which is how the
// rendered parts get their blank lines.
func prefixEach(separator string, entries []string) []string {
	out := make([]string, 0, len(entries)*2)
	for _, entry := range entries {
		out = append(out, separator, entry)
	}
	return out
}

// formatDataclassFields renders a class's annotated fields as a field table,
// declining when it declares none.
func formatDataclassFields(node declaration) (string, bool, error) {
	var rows [][]string
	for _, f := range node.Fields {
		if strings.HasPrefix(f.Name, "_") {
			continue
		}
		defaultCell := ""
		if f.Default != "" {
			defaultCell = "`" + f.Default + "`"
		}
		rows = append(rows, []string{"`" + f.Name + "`", "`" + f.Type + "`", defaultCell})
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	table, err := extractors.RenderTable([]string{"Field", "Type", "Default"}, rows)
	if err != nil {
		return "", false, err
	}
	return table, true, nil
}

// handleTest resolves a code-test directive: a whole test file, or one test
// function or class out of it.
func (e *Extractor) handleTest(
	path string,
	target *string,
	_ []string,
	_ []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::test requires a file path argument"), nil
	}

	fullPath := util.PathJoin(baseDir, path)
	if !extractors.IsFile(fullPath) {
		return extractors.FormatError("test file '" + path + "' not found"), nil
	}

	parsed, err := e.analyze(fullPath)
	if err != nil {
		return "", err
	}
	if parsed.ReadError != nil {
		return extractors.FormatError(
			"cannot read '" + path + "': " + parsed.ReadError.Error()), nil
	}

	if target == nil {
		return "```python\n" + pyRStrip(parsed.Source) + "\n```", nil
	}

	if parsed.Document.SyntaxError != nil {
		return extractors.FormatError(
			"syntax error in '" + path + "': " + *parsed.Document.SyntaxError), nil
	}

	sourceLines := strings.Split(parsed.Source, "\n")

	for _, node := range parsed.Document.Declarations {
		if node.Name == *target {
			return "```python\n" + extractNodeSource(sourceLines, node) + "\n```", nil
		}
	}

	return extractors.FormatError("'" + *target + "' not found in '" + path + "'"), nil
}

// extractNodeSource slices a declaration's own lines out of the file and
// removes their common indentation, so a method reads as a standalone block.
func extractNodeSource(sourceLines []string, node declaration) string {
	start := node.Lineno - 1
	end := node.EndLineno
	if start < 0 {
		start = 0
	}
	if end > len(sourceLines) {
		end = len(sourceLines)
	}
	if start > end {
		start = end
	}
	return extractors.Dedent(strings.Join(sourceLines[start:end], "\n"))
}

// handleSchema resolves a table-schema directive: a JSON document's keys, or a
// class's annotated fields.
func (e *Extractor) handleSchema(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::schema requires an argument"), nil
	}

	if strings.HasSuffix(path, ".json") {
		fullPath := util.PathJoin(baseDir, path)
		if !extractors.IsFile(fullPath) {
			return extractors.FormatError("JSON file '" + path + "' not found"), nil
		}
		return extractors.ConfigFromJSON(fullPath, path, extractors.ExcludeKeysFromAttrs(attrs))
	}

	if target == nil {
		return extractors.FormatError(
			":::schema for Python requires 'module_path ClassName' format"), nil
	}

	return e.schemaFromClass(path, *target, sourcePaths, baseDir)
}

// schemaFromClass renders one class's annotated fields as a table.
func (e *Extractor) schemaFromClass(
	modulePath, className string,
	sourcePaths []string,
	baseDir string,
) (string, error) {
	filePath := resolveModulePath(modulePath, sourcePaths, baseDir)
	if filePath == "" {
		return extractors.FormatError("module '" + modulePath + "' not found"), nil
	}

	parsed, marker, err := e.parsedOrMarker(filePath, modulePath)
	if err != nil || marker != "" {
		return marker, err
	}

	for _, node := range parsed.Document.Declarations {
		if node.Kind == "class" && node.Name == className {
			return extractClassFields(node, parsed.Source)
		}
	}

	return extractors.FormatError(
		"class '" + className + "' not found in '" + modulePath + "'"), nil
}

// extractClassFields renders a class's annotated fields with the descriptions
// their inline comments carry.
func extractClassFields(node declaration, source string) (string, error) {
	sourceLines := strings.Split(source, "\n")
	var rows [][]string

	for _, f := range node.Fields {
		if strings.HasPrefix(f.Name, "_") {
			continue
		}
		defaultCell := ""
		if f.Default != "" {
			defaultCell = "`" + f.Default + "`"
		}
		rows = append(rows, []string{
			"`" + f.Name + "`",
			"`" + f.Type + "`",
			defaultCell,
			inlineComment(sourceLines, f.Lineno),
		})
	}

	if len(rows) == 0 {
		return extractors.FormatError("no fields found in class '" + node.Name + "'"), nil
	}

	return extractors.RenderTable([]string{"Field", "Type", "Default", "Description"}, rows)
}

// inlineComment reads the trailing "# ..." comment off a one-based source line,
// skipping a "#" that sits inside a string literal.
func inlineComment(sourceLines []string, lineno int) string {
	if lineno < 1 || lineno > len(sourceLines) {
		return ""
	}
	line := sourceLines[lineno-1]

	inString := false
	var quoteChar byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '"' || ch == '\'' {
			if i == 0 || line[i-1] != '\\' {
				if !inString {
					inString = true
					quoteChar = ch
				} else if ch == quoteChar {
					inString = false
				}
			}
			continue
		}
		if ch == '#' && !inString {
			return pyStrip(line[i+1:])
		}
	}
	return ""
}

// handleCLI resolves a code-help directive: a module's docstring followed by
// its HELP and USAGE constants.
func (e *Extractor) handleCLI(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::cli requires a module path argument"), nil
	}

	filePath := resolveModulePath(path, sourcePaths, baseDir)
	if filePath == "" {
		return extractors.FormatError("module '" + path + "' not found"), nil
	}

	parsed, marker, err := e.parsedOrMarker(filePath, path)
	if err != nil || marker != "" {
		return marker, err
	}

	var parts []string

	if doc := parsed.Document.docstring(); doc != "" {
		parts = append(parts, doc)
	}

	for _, constant := range parsed.Document.CLIConstants {
		parts = append(parts, "```\n"+pyStrip(constant.Value)+"\n```")
	}

	if len(parts) == 0 {
		return extractors.FormatError("no CLI documentation found in '" + path + "'"), nil
	}

	return strings.Join(parts, "\n\n"), nil
}

// handleProseDesc resolves a prose-desc directive: a module's docstring alone,
// with no symbol list under it.
func (e *Extractor) handleProseDesc(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::prose-desc requires a module path argument"), nil
	}

	filePath := resolveModulePath(path, sourcePaths, baseDir)
	if filePath == "" {
		return extractors.FormatError("module '" + path + "' not found"), nil
	}

	parsed, marker, err := e.parsedOrMarker(filePath, path)
	if err != nil || marker != "" {
		return marker, err
	}

	doc := parsed.Document.docstring()
	if doc == "" {
		return extractors.FormatError("no docstring found in '" + path + "'"), nil
	}

	return extractors.FormatDocstring(doc, 1), nil
}

// parsedOrMarker analyzes a resolved file for a directive, returning the error
// marker the directive renders when the file cannot be read or does not parse.
// displayPath is the path the directive wrote, which is what the marker names.
func (e *Extractor) parsedOrMarker(filePath, displayPath string) (*analysis, string, error) {
	parsed, err := e.analyze(filePath)
	if err != nil {
		return nil, "", err
	}
	if parsed.ReadError != nil {
		return nil, extractors.FormatError(
			"cannot read '" + displayPath + "': " + parsed.ReadError.Error()), nil
	}
	if parsed.Document.SyntaxError != nil {
		return nil, extractors.FormatError(
			"syntax error in '" + displayPath + "': " + *parsed.Document.SyntaxError), nil
	}
	return parsed, "", nil
}
