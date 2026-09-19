package swift

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// memberFuncPattern matches a function declaration by name, whether or not it
// says public: a symbol a page asks about by name is answered whatever its
// visibility, because the question was asked about that symbol.
func memberFuncPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`^(?:(?:public|open)` + spaceClass + `+)?` +
		`(?:(?:static|class|final)` + spaceClass + `+)*func` + spaceClass + `+` +
		regexp.QuoteMeta(name) + spaceClass + `*\(`)
}

// parsedParam is one parameter's name and declared type, nil type when the
// declaration gives none.
type parsedParam struct {
	name      string
	paramType *string
}

// parseParams reads a Swift parameter list -- the text between the parentheses
// of a signature -- into its parameters.
//
// Swift writes a parameter as "label name: Type", with "_" for a parameter
// called without a label, and the name a caller does not see is the one the
// body uses and the one the documentation names: the second token wins.
func parseParams(paramStr string) []parsedParam {
	var params []parsedParam
	if strip(paramStr) == "" {
		return params
	}

	for _, part := range splitParams(paramStr) {
		part = strip(part)
		if part == "" {
			continue
		}

		// A default value is not part of the declaration.
		if eqIdx := findTopLevelChar(part, '='); eqIdx >= 0 {
			part = strip(part[:eqIdx])
		}

		colonIdx := findTopLevelChar(part, ':')
		if colonIdx < 0 {
			// No type annotation, which a closure parameter can do.
			name := strip(part)
			if name == "self" {
				continue
			}
			params = append(params, parsedParam{name: name})
			continue
		}

		namePart := strip(part[:colonIdx])
		typePart := strip(part[colonIdx+1:])

		tokens := util.PythonFields(namePart)
		var name string
		switch {
		case len(tokens) == 2:
			// "label name" or "_ name": the internal name is the second.
			name = tokens[1]
		case len(tokens) == 1:
			name = tokens[0]
		case len(tokens) > 0:
			name = tokens[len(tokens)-1]
		default:
			name = namePart
		}

		if name == "self" {
			continue
		}

		param := parsedParam{name: name}
		if typePart != "" {
			// The attributes a type carries -- @escaping, @autoclosure -- are
			// part of what the declaration says, and are kept.
			paramType := typePart
			param.paramType = &paramType
		}
		params = append(params, param)
	}

	return params
}

// splitParams splits a parameter list on the commas that separate parameters,
// leaving the ones inside a nested type alone.
func splitParams(s string) []string {
	var parts []string
	depth := 0
	var current strings.Builder

	for _, ch := range s {
		switch ch {
		case '(', '<', '[':
			depth++
			current.WriteRune(ch)
		case ')', '>', ']':
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

// findTopLevelChar is the index of the first target character at nesting depth
// zero, or -1.
func findTopLevelChar(s string, target byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '(' || ch == '<' || ch == '[':
			depth++
		case ch == ')' || ch == '>' || ch == ']':
			depth--
		case ch == target && depth == 0:
			return i
		}
	}
	return -1
}

// extractReturnType reads a function's declared return type off its signature:
// what follows the arrow after the parameter list, with a throws clause in
// front of it and a where clause behind it both removed.
func extractReturnType(signature string) *string {
	parenDepth := 0
	lastClose := -1
	for idx := 0; idx < len(signature); idx++ {
		if signature[idx] == '(' {
			parenDepth++
		} else if signature[idx] == ')' {
			parenDepth--
			if parenDepth == 0 {
				lastClose = idx
				break
			}
		}
	}

	if lastClose < 0 {
		return nil
	}

	afterParams := strip(signature[lastClose+1:])
	afterParams = strip(throwsPrefixRe.ReplaceAllString(afterParams, ""))

	arrowMatch := returnArrowRe.FindStringSubmatch(afterParams)
	if arrowMatch == nil {
		return nil
	}

	returnType := strip(arrowMatch[1])
	returnType = strip(bodyOpenerRe.ReplaceAllString(returnType, ""))
	returnType = strip(whereClauseRe.ReplaceAllString(returnType, ""))

	if returnType == "" {
		return nil
	}
	return &returnType
}

// symbolDetails builds the details for the function declared at declLineIdx:
// its parameters, its return type, and which of them the doc comment above it
// documents.
func symbolDetails(lines []string, declLineIdx int) *extractors.SymbolDetails {
	sig := extractFuncSignature(lines, declLineIdx)
	docText := extractors.CollectCommentLinesAbove(lines, declLineIdx, "///", false)

	parenStart := strings.Index(sig, "(")
	if parenStart < 0 {
		return &extractors.SymbolDetails{
			ReturnType:       extractReturnType(sig),
			ReturnDocumented: hasReturnDoc(docText),
		}
	}

	parenDepth := 0
	parenEnd := -1
	for idx := parenStart; idx < len(sig); idx++ {
		if sig[idx] == '(' {
			parenDepth++
		} else if sig[idx] == ')' {
			parenDepth--
			if parenDepth == 0 {
				parenEnd = idx
				break
			}
		}
	}

	var inner string
	if parenEnd < 0 {
		inner = sig[parenStart+1:]
	} else {
		inner = sig[parenStart+1 : parenEnd]
	}

	documentedNames := docParamNames(docText)

	var params []extractors.SymbolParam
	for _, parsed := range parseParams(inner) {
		params = append(params, extractors.SymbolParam{
			Name:       parsed.name,
			Type:       parsed.paramType,
			Documented: documentedNames[parsed.name],
		})
	}

	return &extractors.SymbolDetails{
		Params:           params,
		ReturnType:       extractReturnType(sig),
		ReturnDocumented: hasReturnDoc(docText),
	}
}

// dottedSymbolDetails resolves a dotted symbol -- Router.handle -- to a member
// function of a class, struct, enum, protocol or actor.
func dottedSymbolDetails(source, typeName, memberName string) *extractors.SymbolDetails {
	lines := strings.Split(source, "\n")

	for i, line := range lines {
		stripped := strip(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}

		m := typeDeclRe.FindStringSubmatch(stripped)
		if m == nil || m[1] != typeName {
			continue
		}

		bracePos, ok := findOpenBrace(source, lines, i)
		if !ok {
			return nil
		}

		body, ok := extractors.ExtractBraceBlock(source, bracePos)
		if !ok {
			return nil
		}

		bodyLines := strings.Split(body, "\n")
		funcPattern := memberFuncPattern(memberName)
		for j, bodyLine := range bodyLines {
			bodyStripped := strip(bodyLine)
			if strings.HasPrefix(bodyStripped, "//") {
				continue
			}
			if funcPattern.MatchString(bodyStripped) {
				return symbolDetails(bodyLines, j)
			}
		}

		return nil
	}

	return nil
}

// findOpenBrace is the offset of the brace that opens a declaration's body,
// looked for on the declaration line and the nine lines after it.
func findOpenBrace(source string, lines []string, declLineIdx int) (int, bool) {
	offset := 0
	for k := 0; k < declLineIdx; k++ {
		offset += len(lines[k]) + 1
	}

	endOffset := offset
	end := declLineIdx + 10
	if end > len(lines) {
		end = len(lines)
	}
	for k := declLineIdx; k < end; k++ {
		endOffset += len(lines[k]) + 1
	}
	if endOffset > len(source) {
		endOffset = len(source)
	}

	for pos := offset; pos < endOffset; pos++ {
		if source[pos] == '{' {
			return pos, true
		}
	}

	return 0, false
}
