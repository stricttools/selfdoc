package kotlin

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// extractReturnType reads a function's declared return type off its signature:
// the type that follows the parameter list's closing parenthesis, up to the
// body opener or the expression body. A function that declares none answers
// nothing.
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
	if !strings.HasPrefix(afterParams, ":") {
		return nil
	}

	typeText := strip(afterParams[1:])
	typeText = strip(returnTypeTailRe.ReplaceAllString(typeText, ""))

	if typeText == "" {
		return nil
	}
	return &typeText
}

// funcSymbolDetails builds the details for the function declared at
// declLineIdx: its parameters, its return type, and which of them the doc
// comment above it documents.
func funcSymbolDetails(lines []string, declLineIdx int) *extractors.SymbolDetails {
	sig := extractFuncSignature(lines, declLineIdx)
	kdocText := extractKDocBlock(lines, declLineIdx)

	returnDocumented := false
	if kdocText != "" {
		returnDocumented = hasReturnDoc(kdocText)
	}

	parenStart := strings.Index(sig, "(")
	if parenStart < 0 {
		return &extractors.SymbolDetails{
			ReturnType:       extractReturnType(sig),
			ReturnDocumented: returnDocumented,
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

	documentedNames := map[string]bool{}
	if kdocText != "" {
		documentedNames = kdocParamNames(kdocText)
	}

	return &extractors.SymbolDetails{
		Params:           constructorParams(inner, documentedNames),
		ReturnType:       extractReturnType(sig),
		ReturnDocumented: returnDocumented,
	}
}

// constructorParams reads a parameter list into the protocol's parameter
// records, marking each one the doc comment names.
func constructorParams(inner string, documentedNames map[string]bool) []extractors.SymbolParam {
	var params []extractors.SymbolParam
	for _, paramText := range splitConstructorParams(inner) {
		paramText = strip(paramText)
		if paramText == "" {
			continue
		}
		field, ok := parseConstructorParam(paramText, nil)
		if !ok {
			continue
		}
		param := extractors.SymbolParam{
			Name:       field.name,
			Documented: documentedNames[field.name],
		}
		if field.fieldType != "" {
			fieldType := field.fieldType
			param.Type = &fieldType
		}
		params = append(params, param)
	}
	return params
}

// dataClassSymbolDetails builds the details for a data class, whose parameters
// are its primary constructor's.
//
// A data class declares no return value, and its documentation is reported as
// covering one, because there is nothing for an author to write.
func dataClassSymbolDetails(lines []string, declLineIdx int, stripped string) *extractors.SymbolDetails {
	kdocText := extractKDocBlock(lines, declLineIdx)

	constructorText := collectConstructorText(lines, declLineIdx, stripped)
	inner := constructorText[1:]
	if closeIdx := strings.LastIndex(inner, ")"); closeIdx >= 0 {
		inner = inner[:closeIdx]
	}

	documentedNames := map[string]bool{}
	if kdocText != "" {
		documentedNames = kdocParamNames(kdocText)
	}

	return &extractors.SymbolDetails{
		Params:           constructorParams(inner, documentedNames),
		ReturnDocumented: true,
	}
}

// dottedSymbolDetails resolves a dotted symbol -- UserService.findUser -- to a
// member function of a class, an object or an interface.
func dottedSymbolDetails(source, symbolName string) *extractors.SymbolDetails {
	dotIdx := strings.LastIndex(symbolName, ".")
	typeName := symbolName[:dotIdx]
	memberName := symbolName[dotIdx+1:]

	typeRe := regexp.MustCompile(
		`(?:(?:sealed|abstract|data|inner|open|enum|annotation|value)` + spaceClass + `+)*` +
			`(?:class|object|interface)` + spaceClass + `+` +
			regexp.QuoteMeta(typeName) + `(?:` + spaceClass + `|[<({]|$)`,
	)
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
		`(?:(?:public|private|protected|internal|override|open|final|abstract)` + spaceClass + `+)*` +
			`fun` + spaceClass + `+(?:<[^>]+>` + spaceClass + `+)?` +
			regexp.QuoteMeta(memberName) + spaceClass + `*\(`,
	)
	methodMatch := methodRe.FindStringIndex(body)
	if methodMatch == nil {
		return nil
	}

	// The body's offset into the file puts the member back on its own line, so
	// the line-based helpers read the same text they would for a top-level
	// function.
	absStart := bracePos + 1 + methodMatch[0]
	declLineIdx := strings.Count(source[:absStart], "\n")

	return funcSymbolDetails(strings.Split(source, "\n"), declLineIdx)
}
