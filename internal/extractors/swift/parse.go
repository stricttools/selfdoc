package swift

import (
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
// the four ASCII separators to the Unicode whitespace set.
const (
	wordChars  = util.PythonWordChars
	spaceChars = util.PythonSpaceChars
	wordClass  = "[" + wordChars + "]"
	spaceClass = "[" + spaceChars + "]"
)

// strip is Python's str.strip() with no argument.
func strip(s string) string { return util.PythonStrip(s) }

// rstrip is Python's str.rstrip() with no argument.
func rstrip(s string) string { return util.PythonRStrip(s) }

// lstripLen is the number of leading whitespace characters of s, which is how
// the doc-comment scanners tell an indented continuation line from a new item.
func lstripLen(s string) int {
	return len([]rune(s)) - len([]rune(util.PythonLStrip(s)))
}

// typeKeywords are the keywords a Swift type declaration opens with.
const typeKeywords = `class|struct|enum|protocol|actor`

// The declaration patterns. Swift's visibility is explicit, so a reference page
// renders what says public or open and nothing else.
var (
	// funcRe matches a public or open function declaration.
	funcRe = regexp.MustCompile(`^(?:public|open)` + spaceClass + `+(?:(?:static|class|final)` +
		spaceClass + `+)*func` + spaceClass + `+(` + wordClass + `+)`)

	// typeRe matches a public or open class, struct, enum, protocol or actor.
	typeRe = regexp.MustCompile(`^(?:public|open)` + spaceClass + `+(?:final` + spaceClass +
		`+)?(?:` + typeKeywords + `)` + spaceClass + `+(` + wordClass + `+)`)

	// typealiasRe matches a public typealias.
	typealiasRe = regexp.MustCompile(`^(?:public|open)` + spaceClass + `+typealias` +
		spaceClass + `+(` + wordClass + `+)`)

	// propRe matches a public or open var or let.
	propRe = regexp.MustCompile(`^(?:public|open)` + spaceClass + `+(?:(?:static|class)` +
		spaceClass + `+)?(?:var|let)` + spaceClass + `+(` + wordClass + `+)`)

	// typeDeclRe matches a type declaration whether or not it says public,
	// which is what a dotted symbol is resolved against: a member of an
	// internal type is still a symbol a page can ask about.
	typeDeclRe = regexp.MustCompile(`^(?:(?:public|open)` + spaceClass + `+)?(?:final` +
		spaceClass + `+)?(?:` + typeKeywords + `)` + spaceClass + `+(` + wordClass + `+)`)

	// structDeclRe matches a public struct that opens its body on the same
	// line, which is what the schema table reads.
	structDeclRe = regexp.MustCompile(`^(?:public|open)` + spaceClass + `+(?:final` + spaceClass +
		`+)?struct` + spaceClass + `+(` + wordClass + `+)` + spaceClass + `*(?::[^{]*)?` +
		spaceClass + `*\{`)

	// structFieldRe matches one field of a struct body:
	// [public|open] [static|class] (var|let) name: Type [= default] [// comment]
	structFieldRe = regexp.MustCompile(`^(?:(?:public|open)` + spaceClass + `+)?` +
		`(?:(?:static|class)` + spaceClass + `+)?` +
		`(var|let)` + spaceClass + `+` +
		`(` + wordClass + `+)` + spaceClass + `*:` + spaceClass + `*` +
		`([^=/{]+?)` +
		`(?:` + spaceClass + `*=` + spaceClass + `*([^/{]*?))?` +
		spaceClass + `*` +
		`(?://` + spaceClass + `*(.*))?` +
		spaceClass + `*$`)

	// bodyOpenerRe matches a function's body opener and everything after it.
	bodyOpenerRe = regexp.MustCompile(spaceClass + `*\{.*$`)

	// trailingBraceRe matches the body opener a type or property declaration
	// line ends with.
	trailingBraceRe = regexp.MustCompile(spaceClass + `*\{[^}]*$`)

	// throwsPrefixRe matches the throws clause that can stand between a
	// parameter list and a return type.
	throwsPrefixRe = regexp.MustCompile(`^(?:throws|rethrows)` + spaceClass + `*`)

	// returnArrowRe matches a declared return type.
	returnArrowRe = regexp.MustCompile(`^->` + spaceClass + `*(.+)$`)

	// whereClauseRe matches a generic where clause after a return type.
	whereClauseRe = regexp.MustCompile(spaceClass + `+where` + spaceClass + `+.*$`)
)

// declaration is one public declaration read out of Swift source. Its kind is
// "type", "func" or "prop", which is the order a reference page renders them
// in.
type declaration struct {
	kind      string
	name      string
	signature string
	doc       string
}

// structField is one field of a Swift struct.
type structField struct {
	name         string
	fieldType    string
	defaultValue string
	comment      string
}

// structInfo is a struct and the fields the schema table renders for it.
type structInfo struct {
	name   string
	doc    string
	fields []structField
}

// extractPublicSymbols lists the public and open symbols a Swift file exports:
// functions, the five kinds of type, typealiases, vars and lets.
func extractPublicSymbols(source string) []string {
	var symbols []string

	for _, line := range strings.Split(source, "\n") {
		stripped := strip(line)

		if strings.HasPrefix(stripped, "//") {
			continue
		}

		// A trailing comment is not part of the declaration.
		if commentIdx := strings.Index(stripped, "//"); commentIdx >= 0 {
			stripped = strip(stripped[:commentIdx])
		}

		for _, pattern := range []*regexp.Regexp{funcRe, typeRe, typealiasRe, propRe} {
			if m := pattern.FindStringSubmatch(stripped); m != nil {
				appendSymbol(&symbols, m[1])
				break
			}
		}
	}

	return symbols
}

// appendSymbol records a not-yet-seen name.
func appendSymbol(symbols *[]string, name string) {
	for _, seen := range *symbols {
		if seen == name {
			return
		}
	}
	*symbols = append(*symbols, name)
}

// extractPubDeclarations lists the public and open declarations Swift source
// carries, each with its cleaned signature and its rendered doc comment.
func extractPubDeclarations(source string) []declaration {
	lines := strings.Split(source, "\n")
	var declarations []declaration
	seenNames := map[string]bool{}

	for i, line := range lines {
		stripped := strip(line)

		if strings.HasPrefix(stripped, "//") || stripped == "" {
			continue
		}

		add := func(kind, name, signature string) {
			if seenNames[name] {
				return
			}
			seenNames[name] = true
			docText := extractors.CollectCommentLinesAbove(lines, i, "///", false)
			declarations = append(declarations, declaration{
				kind:      kind,
				name:      name,
				signature: signature,
				doc:       parseDocComment(docText),
			})
		}

		if m := typeRe.FindStringSubmatch(stripped); m != nil {
			add("type", m[1], cleanTypeSignature(stripped))
			continue
		}
		if m := typealiasRe.FindStringSubmatch(stripped); m != nil {
			add("type", m[1], rstrip(stripped))
			continue
		}
		if m := funcRe.FindStringSubmatch(stripped); m != nil {
			add("func", m[1], extractFuncSignature(lines, i))
			continue
		}
		if m := propRe.FindStringSubmatch(stripped); m != nil {
			add("prop", m[1], cleanPropSignature(stripped))
			continue
		}
	}

	return declarations
}

// extractFuncSignature reads a function's signature off its declaration line,
// following it across the lines its parameter list spans and dropping the body.
func extractFuncSignature(lines []string, startIdx int) string {
	var sigParts []string

	end := startIdx + 10
	if end > len(lines) {
		end = len(lines)
	}
	for i := startIdx; i < end; i++ {
		line := strip(lines[i])
		sigParts = append(sigParts, line)
		if strings.Contains(line, "{") {
			break
		}
		// A line that neither continues the parameter list nor opens a body
		// ends the signature.
		if i > startIdx && !strings.HasSuffix(line, ",") && !strings.HasSuffix(line, "(") {
			break
		}
	}

	sig := strings.Join(sigParts, " ")
	sig = bodyOpenerRe.ReplaceAllString(sig, "")
	return rstrip(sig)
}

// cleanTypeSignature drops the body opener a type declaration line ends with,
// keeping the inheritance and conformance list, which is what a reader of the
// page needs.
func cleanTypeSignature(line string) string {
	return rstrip(trailingBraceRe.ReplaceAllString(line, ""))
}

// cleanPropSignature drops the accessor block a computed property opens.
func cleanPropSignature(line string) string {
	return rstrip(trailingBraceRe.ReplaceAllString(line, ""))
}

// extractStructFields lists the public structs in Swift source with their
// fields.
//
// A field inside a public struct is public without saying so, so the field
// scanner does not ask for the keyword; it asks only that the field sit at the
// top level of the struct's own body.
func extractStructFields(source string) []structInfo {
	lines := strings.Split(source, "\n")
	var structs []structInfo

	for i := 0; i < len(lines); i++ {
		stripped := strip(lines[i])

		m := structDeclRe.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}

		var fields []structField
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
				if field, ok := parseStructField(fieldLine, lines, j); ok {
					fields = append(fields, field)
				}
			}
		}

		structs = append(structs, structInfo{
			name:   m[1],
			doc:    parseDocComment(extractors.CollectCommentLinesAbove(lines, i, "///", false)),
			fields: fields,
		})
	}

	return structs
}

// notAFieldPrefixes are the line openings that declare something other than a
// field, and which the field scanner therefore refuses.
var notAFieldPrefixes = []string{
	"//", "func ", "public func ", "open func ", "private ", "internal ",
	"init(", "public init(", "case ",
}

// parseStructField reads one field declaration off a struct-body line.
func parseStructField(fieldLine string, lines []string, lineIdx int) (structField, bool) {
	if fieldLine == "" {
		return structField{}, false
	}
	for _, prefix := range notAFieldPrefixes {
		if strings.HasPrefix(fieldLine, prefix) {
			return structField{}, false
		}
	}

	m := structFieldRe.FindStringSubmatch(fieldLine)
	if m == nil {
		return structField{}, false
	}

	inlineComment := strip(m[5])
	description := inlineComment
	if description == "" {
		description = extractors.CollectCommentLinesAbove(lines, lineIdx, "///", false)
	}

	return structField{
		name:         m[2],
		fieldType:    strip(m[3]),
		defaultValue: strip(m[4]),
		comment:      description,
	}, true
}

// formatStructTable renders a struct's fields as the schema table.
func formatStructTable(info structInfo) (string, error) {
	rows := make([][]string, 0, len(info.fields))
	for _, field := range info.fields {
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

// extractModuleDoc reads the module-level doc comment: the run of /// lines at
// the very top of the file, before any declaration or import.
func extractModuleDoc(source string) string {
	var docLines []string

	for _, line := range strings.Split(source, "\n") {
		stripped := strip(line)
		switch {
		case strings.HasPrefix(stripped, "///"):
			text := stripped[3:]
			text = strings.TrimPrefix(text, " ")
			docLines = append(docLines, text)
		case len(docLines) > 0:
			// The module doc is contiguous, so the first line that is not a
			// /// comment ends it.
			return strings.Join(docLines, "\n")
		case stripped != "" && !strings.HasPrefix(stripped, "//"):
			// Code before any /// comment means the file has no module doc.
			return ""
		}
	}

	if len(docLines) == 0 {
		return ""
	}
	return strings.Join(docLines, "\n")
}
