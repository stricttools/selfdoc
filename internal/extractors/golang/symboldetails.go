package golang

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

var (
	signatureRE = regexp.MustCompile(
		`^func\s+` +
			`(?:\([^)]*\)\s+)?` + // optional receiver
			identifier + `\s*` + // function name
			`\((.*)`, // opening paren and the rest
	)

	// variadicParamRE matches "name ...Type". Its type class is the one the
	// Python pattern used, which deliberately excludes braces -- so
	// "args ...interface{}" does not match here and is split by the general
	// name-then-type rule below, which gives the same answer.
	variadicParamRE = regexp.MustCompile(`^(` + identifier + `)\s+(\.\.\.[\p{L}\p{N}_.*\[\]]+)$`)

	returnMentionRE = regexp.MustCompile(`(?i)\breturn`)
)

// goSymbolDetails reads a function declaration's parameters and return type,
// and asks its doc comment whether each is documented.
//
// "Documented" is a word search: a parameter counts as documented when its name
// appears as a whole word anywhere in the doc comment, and the return value
// counts when the comment contains the word "return" in any case. Go doc
// comments have no Args or Returns sections to read, so there is nothing more
// precise to ask.
func goSymbolDetails(lines []string, declLineIdx int) *extractors.SymbolDetails {
	// The signature may span several lines; gather until it is closed.
	signature := pyStrip(lines[declLineIdx])
	j := declLineIdx + 1
	for j < len(lines) && !strings.Contains(signature, "{") && !strings.HasSuffix(pyRStrip(signature), ")") {
		signature += " " + pyStrip(lines[j])
		j++
	}
	// One more line, if the opening brace is still not in sight.
	if j < len(lines) && !strings.Contains(signature, "{") {
		signature += " " + pyStrip(lines[j])
	}

	docText := collectCommentBlockAbove(lines, declLineIdx)

	m := signatureRE.FindStringSubmatch(signature)
	if m == nil {
		return &extractors.SymbolDetails{}
	}

	paramString, afterParams := splitAtMatchingParen(m[1])
	params := parseGoParams(paramString)

	for i := range params {
		if docText == "" {
			continue
		}
		word := regexp.MustCompile(`\b` + regexp.QuoteMeta(params[i].Name) + `\b`)
		params[i].Documented = word.MatchString(docText)
	}

	return &extractors.SymbolDetails{
		Params:           params,
		ReturnType:       extractGoReturnType(afterParams),
		ReturnDocumented: docText != "" && returnMentionRE.MatchString(docText),
	}
}

// splitAtMatchingParen splits the text after a parameter list's opening paren
// into the list itself and whatever follows its closing paren.
func splitAtMatchingParen(s string) (string, string) {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[:i], s[i+1:]
			}
		}
	}
	return s, ""
}

// parseGoParams parses a Go parameter list.
//
// Grouped parameters are what make this more than a split: in "a, b int, sep
// string" only "b" and "sep" carry a type, and "a" takes the type of the next
// group that has one. So the list is walked right to left, carrying the last
// type seen backwards.
func parseGoParams(paramString string) []extractors.SymbolParam {
	paramString = pyStrip(paramString)
	if paramString == "" {
		return nil
	}

	segments := splitGoParams(paramString)

	type parsed struct {
		name     string
		typeName *string
	}
	var entries []parsed
	for _, segment := range segments {
		segment = pyStrip(segment)
		if segment == "" {
			continue
		}
		name, typeName := parseGoParamSegment(segment)
		entries = append(entries, parsed{name: name, typeName: typeName})
	}

	var currentType *string
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].typeName != nil {
			currentType = entries[i].typeName
		} else {
			entries[i].typeName = currentType
		}
	}

	params := make([]extractors.SymbolParam, 0, len(entries))
	for _, entry := range entries {
		params = append(params, extractors.SymbolParam{Name: entry.name, Type: entry.typeName})
	}
	return params
}

// splitGoParams splits a parameter list on its top-level commas, so a function
// type's own parameter list stays in one piece.
func splitGoParams(paramString string) []string {
	var segments []string
	depth := 0
	var current strings.Builder
	for i := 0; i < len(paramString); i++ {
		ch := paramString[i]
		switch {
		case ch == '(':
			depth++
			current.WriteByte(ch)
		case ch == ')':
			depth--
			current.WriteByte(ch)
		case ch == ',' && depth == 0:
			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// parseGoParamSegment splits one parameter segment into its name and its type,
// reporting a nil type for a segment that is only a name -- a member of a
// group whose type is written once, at the end.
func parseGoParamSegment(segment string) (string, *string) {
	segment = pyStrip(segment)

	if m := variadicParamRE.FindStringSubmatch(segment); m != nil {
		variadic := m[2]
		return m[1], &variadic
	}

	// The first token is the name and the rest is the type, which may be any
	// of *pkg.Type, []Type, map[K]V or func(...).
	parts := splitWhitespaceOnce(segment)
	if len(parts) == 2 {
		typeName := parts[1]
		return parts[0], &typeName
	}

	return segment, nil
}

// extractGoReturnType reads the return type out of the text after a parameter
// list's closing paren: a parenthesized group is taken whole, a bare type runs
// up to the opening brace, and an immediate brace means the function returns
// nothing.
func extractGoReturnType(afterParams string) *string {
	s := pyStrip(afterParams)
	if s == "" || strings.HasPrefix(s, "{") {
		return nil
	}

	if strings.HasPrefix(s, "(") {
		depth := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					group := s[:i+1]
					return &group
				}
			}
		}
		return nil
	}

	if braceIdx := strings.Index(s, "{"); braceIdx >= 0 {
		single := pyStrip(s[:braceIdx])
		if single == "" {
			return nil
		}
		return &single
	}
	single := pyStrip(s)
	if single == "" {
		return nil
	}
	return &single
}
