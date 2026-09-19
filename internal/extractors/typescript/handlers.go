package typescript

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// tsJSExtensions are the file extensions this extractor recognizes as
// TypeScript or JavaScript source when it resolves a path or strips an
// extension off a display name.
//
// It is a longer list than FileExtensions reports -- the module-family and
// CommonJS spellings are here and not there -- because the two answer
// different questions: this one asks "did the author already write an
// extension", and FileExtensions declares the extensions the build walks a
// source tree for. The Python this ports keeps the same two lists with the
// same disagreement.
//
// The order is longest first, so stripping an extension off "bundle.mts"
// leaves "bundle" rather than "bundle.m": ".mts" and ".ts" are both suffixes
// of it, and the longer one is the extension. (The Python iterates a set here,
// whose order varies between interpreter runs, so it strips one or the other
// unpredictably.)
var tsJSExtensions = []string{
	".tsx", ".jsx", ".mts", ".mjs", ".cts", ".cjs", ".ts", ".js",
}

// resolveExtensions are the extensions path resolution appends to an argument
// that carries none, in order.
var resolveExtensions = []string{".ts", ".tsx", ".js", ".jsx"}

// isTSJSExtension reports whether ext is one of the recognized extensions.
func isTSJSExtension(ext string) bool {
	for _, candidate := range tsJSExtensions {
		if candidate == ext {
			return true
		}
	}
	return false
}

// resolveFilePath resolves a directive's path argument to a source file on
// disk: the path as written, relative to the base directory and to each
// declared source path, then with each known extension appended, then as a
// directory carrying an index file. It returns the empty string when nothing
// resolves.
func resolveFilePath(arg string, sourcePaths []string, baseDir string) string {
	var candidates []string

	candidates = append(candidates, util.PathJoin(baseDir, arg))
	for _, sp := range sourcePaths {
		candidates = append(candidates, util.PathJoin(baseDir, sp, arg))
	}

	if _, ext := splitExt(arg); !isTSJSExtension(ext) {
		for _, tryExt := range resolveExtensions {
			candidates = append(candidates, util.PathJoin(baseDir, arg+tryExt))
			for _, sp := range sourcePaths {
				candidates = append(candidates, util.PathJoin(baseDir, sp, arg+tryExt))
			}
			candidates = append(candidates, util.PathJoin(baseDir, arg, "index"+tryExt))
			for _, sp := range sourcePaths {
				candidates = append(candidates, util.PathJoin(baseDir, sp, arg, "index"+tryExt))
			}
		}
	}

	for _, candidate := range candidates {
		if extractors.IsFile(candidate) {
			return candidate
		}
	}
	return ""
}

// codeLanguage is the language a code fence declares for a source file: a .ts
// or .tsx file is TypeScript, everything else this extractor reads is
// JavaScript.
func codeLanguage(filepath string) string {
	if strings.HasSuffix(filepath, ".ts") || strings.HasSuffix(filepath, ".tsx") {
		return "typescript"
	}
	return "javascript"
}

// handleModule renders a module's reference documentation: its module-level
// JSDoc and every declaration it exports, or one named export on its own.
func handleModule(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::module requires a file path argument"), nil
	}

	filepath := resolveFilePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("module '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	displayName := strings.ReplaceAll(path, `\`, "/")
	for _, ext := range tsJSExtensions {
		if strings.HasSuffix(displayName, ext) {
			displayName = displayName[:len(displayName)-len(ext)]
			break
		}
	}

	parts := []string{extractors.SymbolHeading(2, displayName)}

	if moduleJSDoc := extractModuleJSDoc(source); moduleJSDoc != nil {
		parts = append(parts, "", extractors.DemoteDocHeadings(moduleJSDoc.Description, 2))
	}

	exports := extractExports(source)
	lang := codeLanguage(filepath)

	if target != nil && *target != "" {
		for _, export := range exports {
			if export.Name != *target {
				continue
			}
			out := []string{
				extractors.SymbolHeading(3, export.Name),
				"",
				"```" + lang + "\n" + export.Signature + "\n```",
			}
			if export.JSDoc != nil {
				out = append(out, "", formatJSDocAsMarkdown(export.JSDoc, 3))
			}
			return strings.Join(out, "\n"), nil
		}
		return extractors.FormatError("symbol '" + *target + "' not found in '" + path + "'"), nil
	}

	for _, export := range exports {
		parts = append(parts,
			"",
			extractors.SymbolHeading(3, export.Name),
			"",
			"```"+lang+"\n"+export.Signature+"\n```",
		)
		if export.JSDoc != nil {
			parts = append(parts, "", formatJSDocAsMarkdown(export.JSDoc, 3))
		}
	}

	return strings.Join(parts, "\n"), nil
}

// handleTest renders a test file, or one describe/it/test block out of it.
func handleTest(
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

	fullPath := util.PathJoin(baseDir, path)
	if !extractors.IsFile(fullPath) {
		fullPath = resolveFilePath(path, sourcePaths, baseDir)
		if fullPath == "" {
			return extractors.FormatError("test file '" + path + "' not found"), nil
		}
	}

	source, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	lang := codeLanguage(fullPath)

	if target == nil {
		return "```" + lang + "\n" + pyRStrip(source) + "\n```", nil
	}

	block, ok := extractTestBlock(source, *target)
	if !ok {
		return extractors.FormatError("'" + *target + "' not found in '" + path + "'"), nil
	}
	return "```" + lang + "\n" + block + "\n```", nil
}

// extractTestBlock is the source of the describe, it or test block named
// targetName, dedented.
//
// The block's end is found by walking forward from the call's opening paren
// with a depth count that skips string literals, template literals with their
// interpolations, and both comment forms.
func extractTestBlock(source, targetName string) (string, bool) {
	pattern := regexp.MustCompile(
		`(describe|it|test)` + pySpace + `*\(` + pySpace + `*(?:['"])` +
			regexp.QuoteMeta(targetName) + `(?:['"])`)
	match := pattern.FindStringIndex(source)
	if match == nil {
		return "", false
	}

	start := match[0]
	pos := match[1]
	parenDepth := 1
	inString := false
	var stringChar byte
	inTemplate := false
	templateDepth := 0

	for pos < len(source) && parenDepth > 0 {
		ch := source[pos]

		if inString && ch == '\\' {
			pos += 2
			continue
		}

		if inString {
			if ch == stringChar {
				inString = false
			}
			pos++
			continue
		}

		if inTemplate {
			if ch == '\\' {
				pos += 2
				continue
			}
			if ch == '$' && pos+1 < len(source) && source[pos+1] == '{' {
				templateDepth++
				pos += 2
				continue
			}
			if ch == '}' && templateDepth > 0 {
				templateDepth--
				pos++
				continue
			}
			if ch == '`' && templateDepth == 0 {
				inTemplate = false
			}
			pos++
			continue
		}

		if ch == '\'' || ch == '"' {
			inString = true
			stringChar = ch
			pos++
			continue
		}
		if ch == '`' {
			inTemplate = true
			templateDepth = 0
			pos++
			continue
		}

		if ch == '/' && pos+1 < len(source) {
			next := source[pos+1]
			if next == '/' {
				if nl := strings.IndexByte(source[pos:], '\n'); nl != -1 {
					pos += nl + 1
				} else {
					pos = len(source)
				}
				continue
			}
			if next == '*' {
				if end := strings.Index(source[pos+2:], "*/"); end != -1 {
					pos += 2 + end + 2
				} else {
					pos = len(source)
				}
				continue
			}
		}

		switch ch {
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		}

		pos++
	}

	end := pos
	if strings.HasPrefix(pyLStrip(source[end:]), ";") {
		end += strings.IndexByte(source[end:], ';') + 1
	}

	return dedentBlock(source[start:end]), true
}

// dedentBlock removes the common indentation from a block of source, measured
// over the lines that carry something.
func dedentBlock(block string) string {
	lines := strings.Split(block, "\n")
	minIndent := -1
	for _, line := range lines {
		if pyStrip(line) == "" {
			continue
		}
		indent := len([]rune(line)) - len([]rune(pyLStrip(line)))
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return strings.Join(lines, "\n")
	}
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) >= minIndent {
			lines[i] = string(runes[minIndent:])
		}
	}
	return strings.Join(lines, "\n")
}

// handleSchema renders an interface or type declaration's fields as a table,
// or a JSON document's keys when the path names one.
func handleSchema(
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

	filepath := resolveFilePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("file '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	return schemaFromTS(source, target, path)
}

var (
	// interfacePattern matches an interface declaration up to its body brace.
	interfacePattern = regexp.MustCompile(
		`(?:export` + pySpace + `+)?interface` + pySpace + `+(` + pyWord + `+)(?:<[^>]*>)?` +
			pySpace + `*(?:extends` + pySpace + `+[^{]*)?\{`)

	// typePattern matches a type alias whose right side is an object type.
	typePattern = regexp.MustCompile(
		`(?:export` + pySpace + `+)?type` + pySpace + `+(` + pyWord + `+)(?:<[^>]*>)?` +
			pySpace + `*=` + pySpace + `*\{`)
)

// schemaTarget is one interface or type declaration a schema directive could
// render.
type schemaTarget struct {
	Name string
	// End is the byte offset just past the declaration's opening brace.
	End int
}

// schemaFromTS renders the fields of typeName -- or of the first declaration
// in the file when typeName is nil -- as the Field/Type/Description table.
func schemaFromTS(source string, typeName *string, displayPath string) (string, error) {
	var targets []schemaTarget

	for _, match := range interfacePattern.FindAllStringSubmatchIndex(source, -1) {
		name := source[match[2]:match[3]]
		if typeName == nil || name == *typeName {
			targets = append(targets, schemaTarget{Name: name, End: match[1]})
		}
	}
	for _, match := range typePattern.FindAllStringSubmatchIndex(source, -1) {
		name := source[match[2]:match[3]]
		if typeName == nil || name == *typeName {
			targets = append(targets, schemaTarget{Name: name, End: match[1]})
		}
	}

	if len(targets) == 0 {
		if typeName != nil {
			return extractors.FormatError(
				"type '" + *typeName + "' not found in '" + displayPath + "'"), nil
		}
		return extractors.FormatError(
			"no interfaces or types found in '" + displayPath + "'"), nil
	}

	chosen := targets[0]
	bodyText, ok := extractors.ExtractBraceBlock(source, chosen.End-1)
	if !ok {
		return extractors.FormatError("could not parse body of '" + chosen.Name + "'"), nil
	}

	fields := parseInterfaceFields(bodyText)
	if len(fields) == 0 {
		return extractors.FormatError("no fields found in '" + chosen.Name + "'"), nil
	}

	rows := make([][]string, 0, len(fields))
	for _, field := range fields {
		rows = append(rows, []string{
			"`" + field.Name + "`",
			"`" + field.Type + "`",
			field.Description,
		})
	}

	return extractors.RenderTable([]string{"Field", "Type", "Description"}, rows)
}

// interfaceField is one field of an interface or object type.
type interfaceField struct {
	Name        string
	Type        string
	Description string
}

// fieldPattern matches a field declaration: an optional readonly marker, the
// name, an optional question mark, and the type up to the end of the line.
var fieldPattern = regexp.MustCompile(
	`^(?:readonly` + pySpace + `+)?(` + pyWord + `+)(\?)?:` + pySpace + `*(.+?)(?:;|,)?` +
		pySpace + `*$`)

// parseInterfaceFields parses the body of an interface or object type into its
// fields, taking each field's description from the JSDoc block or the comment
// line above it, or from a comment on its own line.
func parseInterfaceFields(body string) []interfaceField {
	var fields []interfaceField
	lines := strings.Split(body, "\n")
	pendingJSDoc := ""

	for i := 0; i < len(lines); i++ {
		line := pyStrip(lines[i])

		if strings.HasPrefix(line, "/**") {
			jsdocLines := []string{line}
			if !strings.Contains(line, "*/") {
				i++
				for i < len(lines) {
					jsdocLines = append(jsdocLines, lines[i])
					if strings.Contains(lines[i], "*/") {
						break
					}
					i++
				}
			}
			jsdocText := strings.Join(jsdocLines, "\n")
			if m := jsdocInline.FindStringSubmatch(jsdocText); m != nil {
				pendingJSDoc = ParseJSDocText(m[1]).Description
			}
			continue
		}

		if strings.HasPrefix(line, "//") {
			pendingJSDoc = pyStrip(line[2:])
			continue
		}

		fieldMatch := fieldPattern.FindStringSubmatch(line)
		if fieldMatch == nil {
			if line != "" {
				pendingJSDoc = ""
			}
			continue
		}

		fieldName := fieldMatch[1]
		optional := fieldMatch[2] == "?"
		fieldType := pyStrip(fieldMatch[3])

		inlineComment := ""
		if commentIdx, ok := findInlineComment(line); ok {
			inlineComment = pyStrip(line[commentIdx:])
			if m := fieldPattern.FindStringSubmatch(pyStrip(line[:commentIdx])); m != nil {
				fieldType = pyStrip(m[3])
			}
		}

		if optional {
			fieldType += " (optional)"
		}

		description := pendingJSDoc
		if description == "" {
			description = inlineComment
		}
		pendingJSDoc = ""

		fields = append(fields, interfaceField{
			Name:        fieldName,
			Type:        fieldType,
			Description: description,
		})
	}

	return fields
}

// findInlineComment is the index of a line's trailing "//" comment, ignoring
// one inside a string literal. It reports false when the line carries none.
func findInlineComment(line string) (int, bool) {
	inString := false
	var stringChar byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inString {
			if ch == '\\' {
				i++
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			continue
		}
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return i, true
		}
	}
	return 0, false
}

// helpConstant matches a help or usage string constant in any of the three
// quotings JavaScript writes.
var helpConstant = regexp.MustCompile(
	`(?i)(?:const|let|var)` + pySpace + `+(help|usage|HELP|USAGE)` + pySpace + `*=` +
		pySpace + `*(?:` +
		`'([^']*(?:\\'[^']*)*)'` +
		`|"([^"]*(?:\\"[^"]*)*)"` +
		"|`([^`]*(?:\\\\`[^`]*)*)`" +
		`)`)

// handleCLI renders a file's command-line documentation: its module-level
// JSDoc, then every help or usage string constant it declares.
func handleCLI(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::cli requires a file path argument"), nil
	}

	filepath := resolveFilePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("module '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	var parts []string

	if moduleJSDoc := extractModuleJSDoc(source); moduleJSDoc != nil && moduleJSDoc.Description != "" {
		parts = append(parts, moduleJSDoc.Description)
	}

	for _, match := range helpConstant.FindAllStringSubmatch(source, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value == "" {
			value = match[4]
		}
		if value != "" {
			parts = append(parts, "```\n"+pyStrip(value)+"\n```")
		}
	}

	if len(parts) == 0 {
		return extractors.FormatError("no CLI documentation found in '" + path + "'"), nil
	}

	return strings.Join(parts, "\n\n"), nil
}

// handleConfig renders a config file as a table, adding the JSONC form -- JSON
// with comments -- to the formats the shared handler reads.
func handleConfig(
	path string,
	_ *string,
	body []string,
	sourcePaths []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path != "" {
		if _, ext := splitExt(path); strings.ToLower(ext) == ".jsonc" {
			fullPath := util.PathJoin(baseDir, path)
			if !extractors.IsFile(fullPath) {
				return extractors.FormatError("config file '" + path + "' not found"), nil
			}
			return configFromJSONC(fullPath, path, extractors.ExcludeKeysFromAttrs(attrs))
		}
	}
	return extractors.HandleTableConfig(path, nil, body, sourcePaths, baseDir, attrs)
}

// configFromJSONC renders a JSONC file as the Key/Type/Value table, stripping
// its comments before the document is parsed.
func configFromJSONC(fullPath, displayPath string, excludeKeys []string) (string, error) {
	raw, err := extractors.ReadSource(fullPath)
	if err != nil {
		return extractors.FormatError("cannot read '" + displayPath + "': " + err.Error()), nil
	}
	return extractors.ConfigTableFromJSONText(
		[]byte(stripJSONCComments(raw)), displayPath, excludeKeys)
}

// trailingComma matches the comma before a closing bracket that JSON refuses
// and JSONC allows.
var trailingComma = regexp.MustCompile(`,` + pySpace + `*([}\]])`)

// stripJSONCComments removes JSONC's line and block comments and its trailing
// commas, leaving string literals alone.
func stripJSONCComments(text string) string {
	var result strings.Builder
	inString := false
	escape := false
	i := 0
	for i < len(text) {
		ch := text[i]
		if escape {
			result.WriteByte(ch)
			escape = false
			i++
			continue
		}
		if inString {
			if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			result.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' {
			inString = true
			result.WriteByte(ch)
			i++
			continue
		}
		if ch == '/' && i+1 < len(text) {
			next := text[i+1]
			if next == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
				result.WriteByte('\n')
				continue
			}
			if next == '*' {
				i += 2
				for i < len(text) && !(text[i] == '*' && i+1 < len(text) && text[i+1] == '/') {
					i++
				}
				i += 2
				continue
			}
		}
		result.WriteByte(ch)
		i++
	}

	return trailingComma.ReplaceAllString(result.String(), "$1")
}

// handleProseDesc renders only a module's own JSDoc description, without the
// list of declarations :::module adds.
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

	filepath := resolveFilePath(path, sourcePaths, baseDir)
	if filepath == "" {
		return extractors.FormatError("module '" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(filepath)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	moduleJSDoc := extractModuleJSDoc(source)
	if moduleJSDoc == nil || moduleJSDoc.Description == "" {
		return extractors.FormatError("no module-level JSDoc found in '" + path + "'"), nil
	}

	return moduleJSDoc.Description, nil
}
