package dart

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/tables"
	"github.com/stricttools/selfdoc/internal/util"
)

// The two character classes the ported patterns are written against.
//
// Go's own \w and \s are ASCII-only, and the patterns came from Python, whose
// \w matches a letter, a digit or an underscore in any script and whose \s adds
// the four ASCII separators to the Unicode whitespace set. The classes are
// spelled out as their members so a pattern can compose them with further
// characters, which a nested class cannot do.
const (
	wordChars  = util.PythonWordChars
	spaceChars = util.PythonSpaceChars
	wordClass  = "[" + wordChars + "]"
	spaceClass = "[" + spaceChars + "]"
)

// strip is Python's str.strip() with no argument.
func strip(s string) string { return util.PythonStrip(s) }

// The library-level directives a file can carry.
var (
	// partRe matches a part directive: part 'path/to/file.dart';
	partRe = regexp.MustCompile(`^part` + spaceClass + `+['"]([^'"]+)['"];`)

	// exportRe matches an export directive, with its optional conditional
	// variant and its optional show/hide combinator:
	//
	//	export 'path.dart';
	//	export 'a.dart' if (dart.library.io) 'b.dart';
	//	export 'src/models.dart' show User, Product;
	exportRe = regexp.MustCompile(
		`^export` + spaceClass + `+['"]([^'"]+)['"]` + spaceClass + `*` +
			`(?:if` + spaceClass + `*\([^)]+\)` + spaceClass + `*['"]([^'"]+)['"]` + spaceClass + `*)?` +
			`(?:(show|hide)` + spaceClass + `+([` + wordChars + spaceChars + `,]+))?` + spaceClass + `*;`,
	)
)

// The declaration patterns, each matching one kind of top-level declaration.
var (
	// classRe matches every class modifier combination but sealed:
	// [abstract] [base|interface|final] [mixin] class Name.
	classRe = regexp.MustCompile(
		`^(?:abstract` + spaceClass + `+)?(?:(?:base|interface|final)` + spaceClass + `+)?` +
			`(?:mixin` + spaceClass + `+)?class` + spaceClass + `+(` + wordClass + `+)`,
	)

	// sealedClassRe matches a sealed class, which classRe does not admit and
	// which therefore has to be tried first.
	sealedClassRe = regexp.MustCompile(`^sealed` + spaceClass + `+class` + spaceClass + `+(` + wordClass + `+)`)

	// mixinRe matches a pure mixin -- [base] mixin Name -- and also matches
	// "mixin class", which the caller rejects by name.
	mixinRe = regexp.MustCompile(`^(?:base` + spaceClass + `+)?mixin` + spaceClass + `+(` + wordClass + `+)`)

	// enumRe matches an enum declaration.
	enumRe = regexp.MustCompile(`^enum` + spaceClass + `+(` + wordClass + `+)`)

	// extensionTypeRe matches an extension type declaration.
	extensionTypeRe = regexp.MustCompile(
		`^extension` + spaceClass + `+type` + spaceClass + `+(` + wordClass + `+)`,
	)

	// typedefRe matches a typedef declaration.
	typedefRe = regexp.MustCompile(`^typedef` + spaceClass + `+(` + wordClass + `+)`)

	// constRe matches a top-level const or final: const [Type] name = ...;
	constRe = regexp.MustCompile(
		`^(?:const|final)` + spaceClass + `+(?:` + wordClass + `[` + wordChars + `<>?,` + spaceChars + `]*` +
			spaceClass + `+)?(` + wordClass + `+)` + spaceClass + `*[=;]`,
	)

	// varRe matches a top-level var: [Type] var name.
	varRe = regexp.MustCompile(
		`^(?:(?:` + wordClass + `[` + wordChars + `<>?,` + spaceChars + `]*` + spaceClass + `+)?)?var` +
			spaceClass + `+(` + wordClass + `+)`,
	)

	// funcRe matches a top-level function: [ReturnType] name( or name<...>(.
	funcRe = regexp.MustCompile(
		`^(?:[` + wordChars + `<>\[\]?,.` + spaceChars + `]+` + spaceClass + `+)(` + wordClass + `+)` +
			spaceClass + `*(?:<[^>]*>` + spaceClass + `*)?\(`,
	)

	// blockCommentRe matches a block comment opened and closed on one line.
	blockCommentRe = regexp.MustCompile(`/\*.*?\*/`)

	// bodyOpenerRe matches the body opener a signature line ends with.
	bodyOpenerRe = regexp.MustCompile(spaceClass + `*\{.*$`)

	// classFieldRe matches a field declaration inside a class body:
	// [late] [final|const|var|static] [Type] name [= default]; [// comment]
	classFieldRe = regexp.MustCompile(
		`^(?:late` + spaceClass + `+)?(?:(?:final|const|var|static)` + spaceClass + `+)?` +
			`([` + wordChars + `<>?,` + spaceChars + `]+?)` + spaceClass + `+` +
			`(` + wordClass + `+)` + spaceClass + `*` +
			`(?:=` + spaceClass + `*([^;]+?))?` + spaceClass + `*;` + spaceClass + `*` +
			`(?://` + spaceClass + `*(.*))?` +
			`$`,
	)

	// crossReferenceRe matches a Dart doc-comment cross-reference, [Name].
	// The Python pattern refuses a markdown link with a negative lookahead,
	// which RE2 cannot express, so the caller checks the byte after the match.
	crossReferenceRe = regexp.MustCompile(`\[(` + wordClass + `+)\]`)

	// returnDocRe matches the word the Dart convention documents a return value
	// with. Python wrote \bReturns?\b; the boundaries are spelled out here
	// because they have to be Unicode-aware, and the pattern is only ever asked
	// whether it matches.
	returnDocRe = regexp.MustCompile(
		`(?:^|[^` + wordChars + `])Returns?(?:[^` + wordChars + `]|$)`,
	)

	// trailingNameRe matches the identifier a parameter declaration ends with.
	trailingNameRe = regexp.MustCompile(`(` + wordClass + `+)` + spaceClass + `*$`)
)

// dartKeywords are the words a function-name match must not be, since the
// fallback function pattern matches any "<words> name(" line.
var dartKeywords = map[string]bool{
	"import": true, "export": true, "part": true, "library": true, "if": true,
	"for": true, "while": true, "do": true, "switch": true, "return": true,
	"throw": true, "assert": true, "await": true, "yield": true, "try": true,
	"catch": true, "finally": true, "new": true, "const": true, "final": true,
	"var": true, "void": true, "class": true, "enum": true, "mixin": true,
	"sealed": true, "abstract": true, "base": true, "interface": true,
	"extension": true, "typedef": true, "true": true, "false": true,
	"null": true, "super": true, "this": true,
}

// declaration is one public top-level declaration read out of Dart source.
type declaration struct {
	kind      string
	name      string
	signature string
	doc       string
}

// classField is one field of a Dart class, as the schema table reports it.
type classField struct {
	name         string
	fieldType    string
	defaultValue string
	comment      string
}

// classInfo is a class and the fields the schema table renders for it.
type classInfo struct {
	name   string
	doc    string
	fields []classField
}

// isGeneratedFile reports whether a Dart file is generated -- model.g.dart,
// model.freezed.dart -- by the two dots its name carries.
func isGeneratedFile(filePath string) bool {
	basename := filepath.Base(filePath)
	parts := strings.Split(basename, ".")
	return len(parts) >= 3 && parts[len(parts)-1] == "dart"
}

// extractSymbolFromLine appends the public symbol a top-level declaration line
// declares, if it declares one, to symbols.
func extractSymbolFromLine(stripped string, symbols *[]string) {
	// The specific patterns come first, in priority order.
	for _, pattern := range []*regexp.Regexp{
		sealedClassRe, // must come before the class pattern
		classRe,
		enumRe,
		extensionTypeRe,
		typedefRe,
	} {
		if m := pattern.FindStringSubmatch(stripped); m != nil {
			appendSymbol(symbols, m[1])
			return
		}
	}

	// A pure mixin, which is a "mixin class" when the name reads "class" --
	// that line is the class pattern's.
	if m := mixinRe.FindStringSubmatch(stripped); m != nil {
		if m[1] != "class" {
			appendSymbol(symbols, m[1])
		}
		return
	}

	if m := constRe.FindStringSubmatch(stripped); m != nil {
		appendSymbol(symbols, m[1])
		return
	}

	if m := varRe.FindStringSubmatch(stripped); m != nil {
		appendSymbol(symbols, m[1])
		return
	}

	// The function pattern is the fallback, so a keyword-led line is rejected.
	if m := funcRe.FindStringSubmatch(stripped); m != nil {
		if !dartKeywords[m[1]] {
			appendSymbol(symbols, m[1])
		}
		return
	}
}

// appendSymbol records a public, not-yet-seen name.
func appendSymbol(symbols *[]string, name string) {
	if strings.HasPrefix(name, "_") {
		return
	}
	for _, seen := range *symbols {
		if seen == name {
			return
		}
	}
	*symbols = append(*symbols, name)
}

// stripBlockComments answers what is left of one source line once the block
// comment it opens, closes or sits inside is removed.
//
// consumed reports that the line has nothing left to read, and inBlock carries
// the scanner's state into the next line.
func stripBlockComments(stripped string, inBlock bool) (rest string, consumed bool, stillInBlock bool) {
	if inBlock {
		idx := strings.Index(stripped, "*/")
		if idx < 0 {
			return "", true, true
		}
		stripped = strip(stripped[idx+2:])
		if stripped == "" {
			return "", true, false
		}
		inBlock = false
	}

	if idx := strings.Index(stripped, "/*"); idx >= 0 {
		if strings.Contains(stripped[idx+2:], "*/") {
			stripped = strip(blockCommentRe.ReplaceAllString(stripped, ""))
			if stripped == "" {
				return "", true, false
			}
		} else {
			stripped = strip(stripped[:idx])
			if stripped == "" {
				return "", true, true
			}
			return stripped, false, true
		}
	}

	return stripped, false, inBlock
}

// extractPublicSymbols lists the public top-level symbols Dart source declares,
// in source order.
func extractPublicSymbols(source string) []string {
	lines := strings.Split(source, "\n")
	var symbols []string
	inBlockComment := false
	braceDepth := 0

	for _, line := range lines {
		stripped := strip(line)

		rest, consumed, inBlock := stripBlockComments(stripped, inBlockComment)
		inBlockComment = inBlock
		if consumed {
			continue
		}
		stripped = rest

		if strings.HasPrefix(stripped, "//") {
			continue
		}

		// Only a top-level line declares anything; the declaration line itself
		// is read before its own brace is counted.
		if braceDepth == 0 {
			extractSymbolFromLine(stripped, &symbols)
		}

		braceDepth += strings.Count(stripped, "{") - strings.Count(stripped, "}")
		if braceDepth < 0 {
			braceDepth = 0
		}
	}

	return symbols
}

// parseDartDoc renders a Dart doc comment as Markdown.
//
// A [Name] cross-reference becomes a code span, unless it is a markdown link's
// label -- the Python pattern refuses one with a negative lookahead, and this
// checks the byte after the bracket instead. Everything else, including the
// {@macro} family of tags, passes through to the shared docstring formatter.
func parseDartDoc(text string, baseLevel int) string {
	var b strings.Builder
	last := 0
	for _, match := range crossReferenceRe.FindAllStringSubmatchIndex(text, -1) {
		end := match[1]
		if end < len(text) && text[end] == '(' {
			continue
		}
		b.WriteString(text[last:match[0]])
		b.WriteString("`" + text[match[2]:match[3]] + "`")
		last = end
	}
	b.WriteString(text[last:])
	return extractors.FormatDocstring(b.String(), baseLevel)
}

// extractLibraryDoc reads the library-level doc comment at the top of a Dart
// file: the leading /// lines, crossing blank lines and the library, import,
// export and part directives, stopping at the first line that is none of them.
func extractLibraryDoc(source string) string {
	var docLines []string

	for _, line := range strings.Split(source, "\n") {
		stripped := strip(line)
		switch {
		case strings.HasPrefix(stripped, "///"):
			text := stripped[3:]
			text = strings.TrimPrefix(text, " ")
			docLines = append(docLines, text)
		case strings.HasPrefix(stripped, "//"):
			// A regular comment: skip it and keep looking.
			continue
		case stripped == "":
			if len(docLines) > 0 {
				docLines = append(docLines, "")
			}
			continue
		case strings.HasPrefix(stripped, "library "),
			strings.HasPrefix(stripped, "import "),
			strings.HasPrefix(stripped, "export "),
			strings.HasPrefix(stripped, "part "):
			continue
		default:
			// Anything else ends the library doc.
			return joinDocLines(docLines)
		}
	}

	return joinDocLines(docLines)
}

// joinDocLines drops the trailing blank lines a library doc collected and
// joins what is left.
func joinDocLines(docLines []string) string {
	for len(docLines) > 0 && docLines[len(docLines)-1] == "" {
		docLines = docLines[:len(docLines)-1]
	}
	if len(docLines) == 0 {
		return ""
	}
	return strings.Join(docLines, "\n")
}

// extractDeclarations lists the public top-level declarations Dart source
// carries, in source order.
func extractDeclarations(source string) []declaration {
	lines := strings.Split(source, "\n")
	var declarations []declaration
	seenNames := map[string]bool{}
	inBlockComment := false
	braceDepth := 0

	for i, line := range lines {
		stripped := strip(line)

		rest, consumed, inBlock := stripBlockComments(stripped, inBlockComment)
		inBlockComment = inBlock
		if consumed {
			continue
		}
		stripped = rest

		if strings.HasPrefix(stripped, "//") {
			continue
		}

		if braceDepth == 0 {
			if decl, ok := tryParseDeclaration(stripped, lines, i, seenNames); ok {
				declarations = append(declarations, decl)
			}
		}

		braceDepth += strings.Count(stripped, "{") - strings.Count(stripped, "}")
		if braceDepth < 0 {
			braceDepth = 0
		}
	}

	return declarations
}

// tryParseDeclaration reads one top-level declaration off a line.
func tryParseDeclaration(
	stripped string,
	lines []string,
	lineIdx int,
	seenNames map[string]bool,
) (declaration, bool) {
	if m := sealedClassRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "class", stripped, lines, lineIdx, seenNames)
	}
	// The class pattern covers abstract, base, interface, final and mixin class.
	if m := classRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "class", stripped, lines, lineIdx, seenNames)
	}
	if m := enumRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "enum", stripped, lines, lineIdx, seenNames)
	}
	if m := extensionTypeRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "extension_type", stripped, lines, lineIdx, seenNames)
	}
	if m := typedefRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "typedef", stripped, lines, lineIdx, seenNames)
	}
	if m := mixinRe.FindStringSubmatch(stripped); m != nil && m[1] != "class" {
		return makeDecl(m[1], "mixin", stripped, lines, lineIdx, seenNames)
	}
	if m := constRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "const", stripped, lines, lineIdx, seenNames)
	}
	if m := varRe.FindStringSubmatch(stripped); m != nil {
		return makeDecl(m[1], "var", stripped, lines, lineIdx, seenNames)
	}
	if m := funcRe.FindStringSubmatch(stripped); m != nil && !dartKeywords[m[1]] {
		return makeDecl(m[1], "function", stripped, lines, lineIdx, seenNames)
	}

	return declaration{}, false
}

// makeDecl builds the declaration for a public, not-yet-seen name.
func makeDecl(
	name, kind, stripped string,
	lines []string,
	lineIdx int,
	seenNames map[string]bool,
) (declaration, bool) {
	if strings.HasPrefix(name, "_") || seenNames[name] {
		return declaration{}, false
	}
	seenNames[name] = true

	doc := extractors.CollectCommentLinesAbove(lines, lineIdx, "///", true)
	return declaration{
		kind:      kind,
		name:      name,
		signature: cleanDartSignature(stripped),
		doc:       doc,
	}, true
}

// cleanDartSignature strips a declaration line's body opener and everything
// after it, and the semicolon a bodyless declaration ends with.
func cleanDartSignature(line string) string {
	line = bodyOpenerRe.ReplaceAllString(line, "")
	line = strings.TrimRight(line, ";")
	return util.PythonRStrip(line)
}

// extractClassFields lists the classes in Dart source that have fields, with
// the fields the schema table renders.
func extractClassFields(source string) []classInfo {
	lines := strings.Split(source, "\n")
	var classes []classInfo

	for i := 0; i < len(lines); i++ {
		stripped := strip(lines[i])

		m := sealedClassRe.FindStringSubmatch(stripped)
		if m == nil {
			m = classRe.FindStringSubmatch(stripped)
		}
		if m == nil || !strings.Contains(stripped, "{") {
			continue
		}

		className := m[1]
		if strings.HasPrefix(className, "_") {
			continue
		}
		doc := extractors.CollectCommentLinesAbove(lines, i, "///", true)

		var fields []classField
		braceDepth := 1
		for j := i + 1; j < len(lines) && braceDepth > 0; j++ {
			fieldLine := strip(lines[j])
			for _, ch := range fieldLine {
				if ch == '{' {
					braceDepth++
				} else if ch == '}' {
					braceDepth--
				}
			}

			if braceDepth == 1 {
				if field, ok := parseClassField(fieldLine, lines, j); ok {
					fields = append(fields, field)
				}
			}
		}

		if len(fields) > 0 {
			classes = append(classes, classInfo{name: className, doc: doc, fields: fields})
		}
	}

	return classes
}

// parseClassField reads one field declaration off a class-body line.
func parseClassField(fieldLine string, lines []string, lineIdx int) (classField, bool) {
	if fieldLine == "" || strings.HasPrefix(fieldLine, "//") || strings.HasPrefix(fieldLine, "@") {
		return classField{}, false
	}

	// A parameter list means this is a method, not a field.
	if strings.Contains(fieldLine, "(") && strings.Contains(fieldLine, ")") {
		return classField{}, false
	}

	m := classFieldRe.FindStringSubmatch(fieldLine)
	if m == nil {
		return classField{}, false
	}

	name := m[2]
	if strings.HasPrefix(name, "_") {
		return classField{}, false
	}

	inlineComment := strip(m[4])
	description := inlineComment
	if description == "" {
		description = extractors.CollectCommentLinesAbove(lines, lineIdx, "///", false)
	}

	return classField{
		name:         name,
		fieldType:    strip(m[1]),
		defaultValue: strip(m[3]),
		comment:      description,
	}, true
}

// formatClassTable renders a class's fields as the schema table.
func formatClassTable(class classInfo) (string, error) {
	rows := make([][]string, 0, len(class.fields))
	for _, field := range class.fields {
		defaultDisplay := ""
		if field.defaultValue != "" {
			defaultDisplay = "`" + field.defaultValue + "`"
		}
		rows = append(rows, []string{
			"`" + field.name + "`",
			"`" + field.fieldType + "`",
			defaultDisplay,
			field.comment,
		})
	}
	return tables.RenderMarkdownTable(
		[]string{"Field", "Type", "Default", "Description"}, rows, nil, false,
	)
}
