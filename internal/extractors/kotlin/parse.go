package kotlin

import (
	"regexp"
	"strings"
	"unicode"

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

// modifiers matches the run of Kotlin modifiers a declaration keyword can
// carry. None of them says anything about visibility, so they are consumed
// before the declaration itself is read; visibility is decided separately,
// because in Kotlin a declaration with no visibility keyword is public.
const modifiers = `(?:(?:actual|expect|external|inline|infix|operator|tailrec|suspend|` +
	`abstract|final|open|override|const|lateinit|sealed|data|inner|` +
	`annotation|value|enum|companion|crossinline|noinline|reified|vararg)` + spaceClass + `+)*`

var (
	// typeRe matches a class, object or interface declaration.
	typeRe = regexp.MustCompile(`^` + modifiers + `(?:class|object|interface)` +
		spaceClass + `+(` + wordClass + `+)`)

	// funcRe matches a function declaration, with its type parameters.
	funcRe = regexp.MustCompile(`^` + modifiers + `fun` + spaceClass +
		`+(?:<[^>]+>` + spaceClass + `+)?(` + wordClass + `+)`)

	// propRe matches a val or var property declaration.
	propRe = regexp.MustCompile(`^` + modifiers + `(?:val|var)` + spaceClass +
		`+(` + wordClass + `+)`)

	// typealiasRe matches a typealias declaration.
	typealiasRe = regexp.MustCompile(`^` + modifiers + `typealias` + spaceClass +
		`+(` + wordClass + `+)`)

	// restrictedRe matches the visibility keywords that keep a declaration off
	// a reference page.
	restrictedRe = regexp.MustCompile(`^(?:private|protected|internal)` + spaceClass + `+`)

	// dataClassRe matches a data class declaration that opens its primary
	// constructor on the same line.
	dataClassRe = regexp.MustCompile(`^data` + spaceClass + `+class` + spaceClass +
		`+(` + wordClass + `+)` + spaceClass + `*\(`)

	// singleLineKDocRe matches a KDoc block written on one line.
	singleLineKDocRe = regexp.MustCompile(`^/\*\*` + spaceClass + `*(.*?)` + spaceClass + `*\*/$`)

	// bodyOpenerRe matches a function's body opener and everything after it.
	bodyOpenerRe = regexp.MustCompile(spaceClass + `*\{.*$`)

	// trailingBraceRe matches the body opener a type or property declaration
	// line ends with.
	trailingBraceRe = regexp.MustCompile(spaceClass + `*\{[^}]*$`)

	// returnTypeTailRe matches the body opener or expression body that follows
	// a declared return type.
	returnTypeTailRe = regexp.MustCompile(spaceClass + `*[{=].*$`)

	// constructorParamRe matches one primary-constructor parameter:
	// [val|var] name: Type [= default].
	constructorParamRe = regexp.MustCompile(`^(?:(?:val|var)` + spaceClass + `+)?` +
		`(` + wordClass + `+)` + spaceClass + `*:` + spaceClass + `*` +
		`([^=]+?)` +
		`(?:` + spaceClass + `*=` + spaceClass + `*(.*))?$`)
)

// declaration is one public declaration read out of Kotlin source. Its kind is
// "type", "func" or "prop", which is the order a reference page renders them
// in.
type declaration struct {
	kind      string
	name      string
	signature string
	doc       string
}

// constructorField is one primary-constructor parameter of a data class.
type constructorField struct {
	name         string
	fieldType    string
	defaultValue string
	comment      string
}

// dataClass is a data class and the fields its primary constructor declares.
type dataClass struct {
	name   string
	doc    string
	fields []constructorField
}

// visibilityState carries the one piece of state the line scanners keep: an
// @PublishedApi annotation makes the internal declaration under it public, and
// it applies to the next declaration only.
type visibilityState struct {
	publishedAPI bool
}

// workLine answers what a scanner should match a line against, and whether the
// line is skipped entirely.
//
// A comment or a blank line is skipped without spending the annotation, so a
// KDoc block between @PublishedApi and its declaration does not lose it. A
// restricted declaration is skipped unless the annotation published it. The
// visibility keyword itself is removed, because the declaration patterns read
// modifiers and not visibility.
func (state *visibilityState) workLine(stripped string) (work string, skip bool) {
	if stripped == "@PublishedApi" {
		state.publishedAPI = true
		return "", true
	}

	if strings.HasPrefix(stripped, "//") ||
		strings.HasPrefix(stripped, "/*") ||
		strings.HasPrefix(stripped, "*") {
		return "", true
	}

	if stripped == "" {
		return "", true
	}

	if restrictedRe.MatchString(stripped) && !state.publishedAPI {
		state.publishedAPI = false
		return "", true
	}

	work = stripped
	if state.publishedAPI && strings.HasPrefix(work, "internal ") {
		work = strip(work[len("internal "):])
	} else if strings.HasPrefix(work, "public ") {
		work = strip(work[len("public "):])
	}

	state.publishedAPI = false
	return work, false
}

// extractPublicSymbols lists the symbols a Kotlin file exports.
//
// A declaration with no visibility keyword is public, so what the scanner looks
// for is the three keywords that take one off the list -- private, protected
// and internal -- with @PublishedApi internal the one exception.
func extractPublicSymbols(source string) []string {
	var symbols []string
	state := &visibilityState{}

	for _, line := range strings.Split(source, "\n") {
		work, skip := state.workLine(strip(line))
		if skip {
			continue
		}

		// A trailing comment is not part of the declaration.
		if commentIdx := strings.Index(work, "//"); commentIdx >= 0 {
			work = strip(work[:commentIdx])
		}

		for _, pattern := range []*regexp.Regexp{typeRe, funcRe, typealiasRe, propRe} {
			if m := pattern.FindStringSubmatch(work); m != nil {
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

// extractPubDeclarations lists the public declarations Kotlin source carries,
// each with its cleaned signature and its rendered KDoc.
func extractPubDeclarations(source string) []declaration {
	lines := strings.Split(source, "\n")
	var declarations []declaration
	seenNames := map[string]bool{}
	state := &visibilityState{}

	for i, line := range lines {
		stripped := strip(line)

		// A package or import statement declares nothing, and spends the
		// annotation above it.
		if strings.HasPrefix(stripped, "package ") || strings.HasPrefix(stripped, "import ") {
			state.publishedAPI = false
			continue
		}

		work, skip := state.workLine(stripped)
		if skip {
			continue
		}

		add := func(kind, name, signature string) {
			if seenNames[name] {
				return
			}
			seenNames[name] = true
			declarations = append(declarations, declaration{
				kind:      kind,
				name:      name,
				signature: signature,
				doc:       parseKDoc(extractKDocBlock(lines, i)),
			})
		}

		if m := typeRe.FindStringSubmatch(work); m != nil {
			add("type", m[1], cleanTypeSignature(stripped))
			continue
		}
		if m := typealiasRe.FindStringSubmatch(work); m != nil {
			add("type", m[1], rstrip(stripped))
			continue
		}
		if m := funcRe.FindStringSubmatch(work); m != nil {
			add("func", m[1], extractFuncSignature(lines, i))
			continue
		}
		if m := propRe.FindStringSubmatch(work); m != nil {
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
		if i > startIdx && line == "}" {
			// The enclosing scope's closing brace -- an interface body, say --
			// is not part of the signature.
			break
		}
		sigParts = append(sigParts, line)
		if strings.Contains(line, "{") {
			break
		}
		if i > startIdx && !strings.HasSuffix(line, ",") && !strings.HasSuffix(line, "(") {
			break
		}
	}

	sig := strings.Join(sigParts, " ")
	sig = bodyOpenerRe.ReplaceAllString(sig, "")
	return rstrip(sig)
}

// cleanTypeSignature drops the body opener a type declaration line ends with.
func cleanTypeSignature(line string) string {
	return rstrip(trailingBraceRe.ReplaceAllString(line, ""))
}

// cleanPropSignature drops the accessor block a computed property opens.
func cleanPropSignature(line string) string {
	return rstrip(trailingBraceRe.ReplaceAllString(line, ""))
}

// extractDataClassFields lists the data classes in Kotlin source with their
// primary-constructor fields, each field's description coming from the KDoc
// @property tag that names it.
func extractDataClassFields(source string) []dataClass {
	lines := strings.Split(source, "\n")
	var dataClasses []dataClass

	for i := 0; i < len(lines); i++ {
		stripped := strip(lines[i])

		if restrictedRe.MatchString(stripped) {
			continue
		}

		work := stripped
		if strings.HasPrefix(work, "public ") {
			work = strip(work[len("public "):])
		}

		m := dataClassRe.FindStringSubmatch(work)
		if m == nil {
			continue
		}

		docText := extractKDocBlock(lines, i)
		propDocs := map[string]string{}
		if docText != "" {
			propDocs = kdocPropertyDocs(docText)
		}

		constructorText := collectConstructorText(lines, i, stripped)
		inner := constructorText[1:]
		if closeIdx := strings.LastIndex(inner, ")"); closeIdx >= 0 {
			inner = inner[:closeIdx]
		}

		var fields []constructorField
		for _, param := range splitConstructorParams(inner) {
			if field, ok := parseConstructorParam(strip(param), propDocs); ok {
				fields = append(fields, field)
			}
		}

		dataClasses = append(dataClasses, dataClass{
			name:   m[1],
			doc:    parseKDoc(docText),
			fields: fields,
		})
	}

	return dataClasses
}

// collectConstructorText reads a primary constructor's text off the
// declaration line and the lines it continues onto, until the parentheses
// balance.
func collectConstructorText(lines []string, declLineIdx int, stripped string) string {
	constructorText := stripped[strings.Index(stripped, "("):]

	parenDepth := 0
	for _, ch := range constructorText {
		if ch == '(' {
			parenDepth++
		} else if ch == ')' {
			parenDepth--
		}
	}

	j := declLineIdx
	for parenDepth > 0 && j+1 < len(lines) {
		j++
		next := strip(lines[j])
		constructorText += " " + next
		for _, ch := range next {
			if ch == '(' {
				parenDepth++
			} else if ch == ')' {
				parenDepth--
			}
		}
	}

	return constructorText
}

// splitConstructorParams splits a parameter list on the commas that separate
// parameters, leaving the ones inside a generic argument list alone.
func splitConstructorParams(text string) []string {
	var params []string
	depth := 0
	var current strings.Builder

	for _, ch := range text {
		switch ch {
		case '<', '(':
			depth++
			current.WriteRune(ch)
		case '>', ')':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				params = append(params, current.String())
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		if remaining := strip(current.String()); remaining != "" {
			params = append(params, remaining)
		}
	}

	return params
}

// parseConstructorParam reads one parameter: [val|var] name: Type [= default].
func parseConstructorParam(paramText string, propDocs map[string]string) (constructorField, bool) {
	m := constructorParamRe.FindStringSubmatch(strip(paramText))
	if m == nil {
		return constructorField{}, false
	}

	name := m[1]
	return constructorField{
		name:         name,
		fieldType:    strip(m[2]),
		defaultValue: strip(m[3]),
		comment:      propDocs[name],
	}, true
}

// formatDataClassTable renders a data class's fields as the schema table.
func formatDataClassTable(class dataClass) (string, error) {
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

// isWordRune reports whether r is a word character by Python's \w rule for
// text patterns -- a letter, a digit or an underscore, in any script -- which
// is the class the patterns in this package are written against.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}
