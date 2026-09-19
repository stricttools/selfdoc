package dart

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// isWordRune reports whether r is a word character by Python's \w rule for
// text patterns -- a letter, a digit or an underscore, in any script -- which
// is the class the patterns in this package are written against.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// removeWord deletes every occurrence of word that stands on its own, which is
// what Python's re.sub(r"\b" + word + r"\b", "", text) does. The boundaries are
// tested against the original text, so a removal never creates an occurrence.
func removeWord(text, word string) string {
	var b strings.Builder
	last := 0
	for offset := 0; ; {
		idx := strings.Index(text[offset:], word)
		if idx < 0 {
			break
		}
		start := offset + idx
		end := start + len(word)
		if wordBoundaryBefore(text, start) && wordBoundaryAfter(text, end) {
			b.WriteString(text[last:start])
			last = end
		}
		offset = start + 1
	}
	b.WriteString(text[last:])
	return b.String()
}

// wordBoundaryBefore reports whether a word starting at pos has a boundary in
// front of it.
func wordBoundaryBefore(text string, pos int) bool {
	if pos == 0 {
		return true
	}
	for i := pos - 1; i >= 0; i-- {
		if isRuneStart(text[i]) {
			r := []rune(text[i:pos])
			return !isWordRune(r[0])
		}
	}
	return true
}

// wordBoundaryAfter reports whether a word ending at pos has a boundary behind
// it.
func wordBoundaryAfter(text string, pos int) bool {
	if pos >= len(text) {
		return true
	}
	r := []rune(text[pos:])
	return !isWordRune(r[0])
}

// isRuneStart reports whether b begins a UTF-8 sequence.
func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// extractFuncSignature reads a function's signature off its declaration line,
// following it across lines until the parentheses balance, and then drops
// everything after the closing parenthesis -- the body opener and the
// async/sync modifiers.
func extractFuncSignature(lines []string, startIdx int) string {
	var sigParts []string
	parenDepth := 0
	seenParen := false

	end := startIdx + 20
	if end > len(lines) {
		end = len(lines)
	}
	for i := startIdx; i < end; i++ {
		line := strip(lines[i])
		sigParts = append(sigParts, line)

		for _, ch := range line {
			if ch == '(' {
				parenDepth++
				seenParen = true
			} else if ch == ')' {
				parenDepth--
			}
		}

		if seenParen && parenDepth == 0 {
			break
		}
	}

	sig := strings.Join(sigParts, " ")

	depth := 0
	closePos := -1
	for idx := 0; idx < len(sig); idx++ {
		switch sig[idx] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closePos = idx
			}
		}
		if closePos >= 0 {
			break
		}
	}

	if closePos >= 0 {
		sig = sig[:closePos+1]
	}

	return util.PythonRStrip(sig)
}

// splitParams splits a parameter list on the commas that separate parameters,
// leaving the ones inside a nested type or default value alone.
func splitParams(text string) []string {
	var params []string
	depth := 0
	var current strings.Builder

	for _, ch := range text {
		switch ch {
		case '<', '(', '[', '{':
			depth++
			current.WriteRune(ch)
		case '>', ')', ']', '}':
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

// parsedParam is one parameter's name and declared type, nil type when the
// declaration gives none.
type parsedParam struct {
	name      string
	paramType *string
}

// parseDartParam reads one Dart parameter: positional or named, required or
// optional, nullable, defaulted, covariant or of a function type. A this.x or
// super.x constructor shorthand declares no parameter of its own.
func parseDartParam(paramText string) *parsedParam {
	text := strip(paramText)
	if text == "" {
		return nil
	}

	// Drop the default value, which is whatever follows the last top-level '='.
	depth := 0
	lastEq := -1
	for idx := 0; idx < len(text); idx++ {
		switch text[idx] {
		case '<', '(', '[', '{':
			depth++
		case '>', ')', ']', '}':
			depth--
		case '=':
			if depth == 0 {
				lastEq = idx
			}
		}
	}
	if lastEq >= 0 {
		text = strip(text[:lastEq])
	}

	text = stripKeyword(text, "required ")
	text = stripKeyword(text, "covariant ")

	if strings.HasPrefix(text, "this.") || strings.HasPrefix(text, "super.") {
		return nil
	}

	// The name is the last identifier of the declaration.
	m := trailingNameRe.FindStringSubmatchIndex(text)
	if m == nil {
		return nil
	}

	name := text[m[2]:m[3]]
	paramType := strip(text[:m[0]])
	if paramType == "" {
		return &parsedParam{name: name}
	}
	return &parsedParam{name: name, paramType: &paramType}
}

// stripKeyword drops a leading keyword and the space after it.
func stripKeyword(text, keyword string) string {
	if strings.HasPrefix(text, keyword) {
		return strip(text[len(keyword):])
	}
	return text
}

// extractDartReturnType reads a function's return type, which in Dart precedes
// the name: "Future<List<Item>> fetchItems(...)" returns Future<List<Item>>.
func extractDartReturnType(signature, funcName string) *string {
	pattern := regexp.MustCompile(
		`(?:^|[^` + wordChars + `])(` + regexp.QuoteMeta(funcName) + spaceClass +
			`*(?:<[^>]*>` + spaceClass + `*)?\()`,
	)
	m := pattern.FindStringSubmatchIndex(signature)
	if m == nil {
		return nil
	}

	before := strip(signature[:m[2]])
	if before == "" {
		return nil
	}

	for _, modifier := range []string{"static", "external", "abstract"} {
		before = strip(removeWord(before, modifier))
	}

	if before == "" {
		return nil
	}
	return &before
}

// isParamDocumented reports whether a Dart doc comment documents a parameter,
// which the convention writes as [paramName] -- and which a markdown link,
// [text](url), is not.
func isParamDocumented(paramName, docText string) bool {
	if docText == "" {
		return false
	}
	needle := "[" + paramName + "]"
	for offset := 0; ; {
		idx := strings.Index(docText[offset:], needle)
		if idx < 0 {
			return false
		}
		end := offset + idx + len(needle)
		if end >= len(docText) || docText[end] != '(' {
			return true
		}
		offset = offset + idx + 1
	}
}

// hasDartReturnDoc reports whether a doc comment says what the function
// returns, by the word the convention uses.
func hasDartReturnDoc(docText string) bool {
	if docText == "" {
		return false
	}
	return returnDocRe.MatchString(docText)
}

// stripSectionBrackets removes Dart's named-parameter braces and
// optional-positional brackets.
//
// They mark sections rather than nest anything, so they have to go before the
// list is split on commas -- otherwise every parameter inside one counts as
// nested and the split never happens:
//
//	"String a, {required String b, int c}" -> "String a, required String b, int c"
//	"String a, [int b = 0, String? c]"     -> "String a, int b = 0, String? c"
func stripSectionBrackets(inner string) string {
	var b strings.Builder
	for _, ch := range inner {
		switch ch {
		case '{', '}', '[', ']':
			continue
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// dartSymbolDetails builds the details for the function declared at
// declLineIdx.
func dartSymbolDetails(lines []string, declLineIdx int, funcName string) *extractors.SymbolDetails {
	sig := extractFuncSignature(lines, declLineIdx)
	docText := extractors.CollectCommentLinesAbove(lines, declLineIdx, "///", true)

	parenStart := strings.Index(sig, "(")
	if parenStart < 0 {
		return &extractors.SymbolDetails{
			ReturnType:       extractDartReturnType(sig, funcName),
			ReturnDocumented: hasDartReturnDoc(docText),
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

	inner = stripSectionBrackets(strip(inner))

	var params []extractors.SymbolParam
	for _, paramText := range splitParams(inner) {
		paramText = strip(paramText)
		if paramText == "" {
			continue
		}
		parsed := parseDartParam(paramText)
		if parsed == nil {
			continue
		}
		params = append(params, extractors.SymbolParam{
			Name:       parsed.name,
			Type:       parsed.paramType,
			Documented: isParamDocumented(parsed.name, docText),
		})
	}

	return &extractors.SymbolDetails{
		Params:           params,
		ReturnType:       extractDartReturnType(sig, funcName),
		ReturnDocumented: hasDartReturnDoc(docText),
	}
}

// dottedSymbolDetails resolves a dotted symbol -- UserRepository.findById -- to
// the details of a member of a class, an abstract class or a mixin.
func dottedSymbolDetails(source, symbolName string) *extractors.SymbolDetails {
	dotIdx := strings.LastIndex(symbolName, ".")
	typeName := symbolName[:dotIdx]
	memberName := symbolName[dotIdx+1:]

	typeRe := regexp.MustCompile(
		`(?:abstract` + spaceClass + `+)?(?:(?:base|interface|final|sealed)` + spaceClass + `+)?` +
			`(?:mixin` + spaceClass + `+)?(?:class|mixin)` + spaceClass + `+` +
			regexp.QuoteMeta(typeName) + `(?:` + spaceClass + `|[<{])`,
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

	// A Dart method reads "ReturnType methodName(" or just "methodName(".
	bodyLines := strings.Split(body, "\n")
	for i, line := range bodyLines {
		stripped := strip(line)

		if strings.HasPrefix(stripped, "//") || strings.HasPrefix(stripped, "/*") {
			continue
		}

		m := funcRe.FindStringSubmatch(stripped)
		if m != nil && m[1] == memberName && !dartKeywords[m[1]] {
			return dartSymbolDetails(bodyLines, i, memberName)
		}
	}

	return nil
}
