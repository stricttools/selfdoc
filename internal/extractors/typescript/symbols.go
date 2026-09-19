package typescript

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// tsParam is one parameter read out of a parameter list, before the
// documentation status is decided.
type tsParam struct {
	Name string
	Type *string
}

// dottedSymbolDetails resolves a dotted symbol like "Router.handle" to the
// details of that member.
//
// It finds the class or interface declaring the type, takes its
// brace-delimited body, locates the member inside it, and then re-locates the
// member in the whole source so the JSDoc scan sees the text above it.
func dottedSymbolDetails(source, symbolName string) *extractors.SymbolDetails {
	dot := strings.LastIndex(symbolName, ".")
	typeName, memberName := symbolName[:dot], symbolName[dot+1:]

	typeRe := regexp.MustCompile(
		`(?:export` + pySpace + `+)?(?:abstract` + pySpace + `+)?(?:class|interface)` +
			pySpace + `+` + regexp.QuoteMeta(typeName) + `(?:` + pySpace + `|[<{])`)
	typeMatch := typeRe.FindStringIndex(source)
	if typeMatch == nil {
		return nil
	}

	bracePos := strings.Index(source[typeMatch[0]:], "{")
	if bracePos == -1 {
		return nil
	}
	bracePos += typeMatch[0]

	body, ok := extractors.ExtractBraceBlock(source, bracePos)
	if !ok {
		return nil
	}

	methodRe := regexp.MustCompile(
		`(?:(?:public|private|protected)` + pySpace + `+)?` +
			`(?:static` + pySpace + `+)?` +
			`(?:async` + pySpace + `+)?` +
			regexp.QuoteMeta(memberName) + pySpace + `*\(`)
	methodMatch := methodRe.FindStringIndex(body)
	if methodMatch == nil {
		return nil
	}

	absStart := bracePos + 1 + methodMatch[0]
	absMatch := methodRe.FindStringIndex(source[absStart:])
	if absMatch == nil {
		return nil
	}

	return tsSymbolDetails(source, absStart+absMatch[0])
}

// tsSymbolDetails reads the parameters, the return type and the documentation
// status of the declaration that starts at declStart.
func tsSymbolDetails(source string, declStart int) *extractors.SymbolDetails {
	parenStart := strings.Index(source[declStart:], "(")
	if parenStart == -1 {
		return nil
	}
	parenStart += declStart

	parenEnd, ok := findMatchingParen(source, parenStart)
	if !ok {
		return nil
	}

	params := parseTSParams(source[parenStart+1 : parenEnd])
	returnType := extractTSReturnType(source[parenEnd:])

	documented := map[string]bool{}
	returnDocumented := false
	if jsdoc := findJSDocBefore(source, declStart); jsdoc != nil {
		for _, p := range jsdoc.Params {
			documented[p.Name] = true
		}
		returnDocumented = jsdoc.Returns != nil
	}

	out := make([]extractors.SymbolParam, 0, len(params))
	for _, p := range params {
		out = append(out, extractors.SymbolParam{
			Name:       p.Name,
			Type:       p.Type,
			Documented: documented[strings.TrimLeft(p.Name, ".")],
		})
	}

	return &extractors.SymbolDetails{
		Params:           out,
		ReturnType:       returnType,
		ReturnDocumented: returnDocumented,
	}
}

// findMatchingParen is the index of the ")" that closes the "(" at openPos,
// with string literals skipped so a paren inside one does not change the
// depth. It reports false when the parens do not match.
func findMatchingParen(source string, openPos int) (int, bool) {
	depth := 0
	i := openPos
	for i < len(source) {
		ch := source[i]
		switch {
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return i, true
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote := ch
			i++
			for i < len(source) {
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == quote {
					break
				}
				i++
			}
		}
		i++
	}
	return 0, false
}

// parseTSParams parses the text between "(" and ")" into the parameters it
// declares, dropping default values and the optional marker and keeping a rest
// parameter's "..." prefix.
func parseTSParams(paramStr string) []tsParam {
	paramStr = pyStrip(paramStr)
	if paramStr == "" {
		return nil
	}

	var result []tsParam
	for _, part := range splitTSParams(paramStr) {
		part = pyStrip(part)
		if part == "" {
			continue
		}

		if eqIdx := findTopLevelEq(part); eqIdx >= 0 {
			part = pyStrip(part[:eqIdx])
		}

		restPrefix := ""
		if strings.HasPrefix(part, "...") {
			restPrefix = "..."
			part = part[3:]
		}

		colonIdx := findTopLevelColon(part)
		if colonIdx > 0 {
			name := restPrefix + strings.TrimRight(pyStrip(part[:colonIdx]), "?")
			ptype := pyStrip(part[colonIdx+1:])
			var typePtr *string
			if ptype != "" {
				typePtr = &ptype
			}
			result = append(result, tsParam{Name: name, Type: typePtr})
			continue
		}
		result = append(result, tsParam{
			Name: restPrefix + strings.TrimRight(pyStrip(part), "?"),
		})
	}

	return result
}

// splitTSParams splits a parameter list on the commas at nesting depth zero.
func splitTSParams(s string) []string {
	var parts []string
	depth := 0
	var current strings.Builder
	for _, ch := range s {
		switch ch {
		case '(', '<', '[', '{':
			depth++
			current.WriteRune(ch)
		case ')', '>', ']', '}':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// findTopLevelEq is the index of the first "=" at nesting depth zero that is
// neither an arrow nor an equality operator, or -1.
func findTopLevelEq(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '<', '[', '{':
			depth++
		case ')', '>', ']', '}':
			depth--
		case '=':
			if depth == 0 {
				if i+1 < len(s) && (s[i+1] == '=' || s[i+1] == '>') {
					continue
				}
				return i
			}
		}
	}
	return -1
}

// findTopLevelColon is the index of the first ":" at nesting depth zero, or -1.
func findTopLevelColon(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '<', '[', '{':
			depth++
		case ')', '>', ']', '}':
			depth--
		case ':':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractTSReturnType reads the return type out of the text after a parameter
// list's closing paren: the annotation between ":" and the body's "{" or the
// end of the line. It returns nil when the declaration annotates none.
func extractTSReturnType(afterParen string) *string {
	text := pyLStrip(strings.TrimLeft(afterParen, ")"))
	if !strings.HasPrefix(text, ":") {
		return nil
	}
	text = pyLStrip(text[1:])

	depth := 0
	var result strings.Builder
	for _, ch := range text {
		if depth == 0 && (ch == '{' || ch == '\n') {
			break
		}
		switch ch {
		case '(', '<', '[':
			depth++
		case ')', '>', ']':
			depth--
		}
		result.WriteRune(ch)
	}

	returnType := pyStrip(result.String())
	if returnType == "" {
		return nil
	}
	return &returnType
}
