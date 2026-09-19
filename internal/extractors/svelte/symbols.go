package svelte

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/extractors/typescript"
)

// symbolDetailsFromScript reads what a script block says about one exported
// function's parameters and return value, or nil when the block does not
// export that function.
func symbolDetailsFromScript(scriptContent, symbolName string) *extractors.SymbolDetails {
	funcRe := regexp.MustCompile(
		`export` + pySpace + `+(?:async` + pySpace + `+)?function` + pySpace + `+` +
			regexp.QuoteMeta(symbolName) + pySpace + `*(\([^)]*\)(?:` + pySpace + `*:` +
			pySpace + `*[^{;]+)?)`)
	match := funcRe.FindStringSubmatchIndex(scriptContent)
	if match == nil {
		return nil
	}

	paramsAndReturn := pyStrip(scriptContent[match[2]:match[3]])

	parenEnd := strings.Index(paramsAndReturn, ")")
	if parenEnd < 0 {
		return nil
	}
	params := parseFuncParams(paramsAndReturn[1:parenEnd])

	var returnType *string
	afterParen := pyStrip(paramsAndReturn[parenEnd+1:])
	if strings.HasPrefix(afterParen, ":") {
		if annotation := pyStrip(afterParen[1:]); annotation != "" {
			returnType = &annotation
		}
	}

	documented := map[string]bool{}
	returnDocumented := false
	if raw, ok := findJSDocBefore(scriptContent, match[0]); ok {
		parsed := typescript.ParseJSDocText(raw)
		for _, p := range parsed.Params {
			documented[p.Name] = true
		}
		returnDocumented = parsed.Returns != nil && *parsed.Returns != ""
	}

	out := make([]extractors.SymbolParam, 0, len(params))
	for _, p := range params {
		out = append(out, extractors.SymbolParam{
			Name:       p.Name,
			Type:       p.Type,
			Documented: documented[p.Name],
		})
	}

	return &extractors.SymbolDetails{
		Params:           out,
		ReturnType:       returnType,
		ReturnDocumented: returnDocumented,
	}
}

// funcParam is one parameter of an exported function.
type funcParam struct {
	Name string
	Type *string
}

// parseFuncParams parses a TypeScript parameter list into its parameters,
// dropping default values and the optional marker.
func parseFuncParams(paramsStr string) []funcParam {
	paramsStr = pyStrip(paramsStr)
	if paramsStr == "" {
		return nil
	}

	var result []funcParam
	for _, part := range splitParams(paramsStr) {
		part = pyStrip(part)
		if part == "" {
			continue
		}

		if eqIdx := findTopLevelEq(part); eqIdx >= 0 {
			part = pyStrip(part[:eqIdx])
		}

		if colonIdx := strings.Index(part, ":"); colonIdx > 0 {
			name := strings.TrimRight(pyStrip(part[:colonIdx]), "?")
			var typePtr *string
			if ptype := pyStrip(part[colonIdx+1:]); ptype != "" {
				typePtr = &ptype
			}
			result = append(result, funcParam{Name: name, Type: typePtr})
			continue
		}
		result = append(result, funcParam{Name: strings.TrimRight(pyStrip(part), "?")})
	}

	return result
}

// splitParams splits a parameter list on the commas at nesting depth zero.
func splitParams(s string) []string {
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

// findTopLevelEq is the index of the first "=" at nesting depth zero, or -1.
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
				return i
			}
		}
	}
	return -1
}

// findJSDocBefore is the raw content of the JSDoc block that ends right before
// pos, with only whitespace between the block and pos. It reports false when
// there is none.
func findJSDocBefore(scriptContent string, pos int) (string, bool) {
	if pos < 0 || pos > len(scriptContent) {
		return "", false
	}
	before := pyRStrip(scriptContent[:pos])
	if !strings.HasSuffix(before, "*/") {
		return "", false
	}
	start := strings.LastIndex(before, "/**")
	if start < 0 {
		return "", false
	}
	return before[start+3 : len(before)-2], true
}
