package zig

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// returnsWord matches the word a doc comment says a return value with.
var returnsWord = regexp.MustCompile(
	`(?i)(?:^|` + pyNonWord + `)returns?(?:` + pyNonWord + `|$)`)

// fnPattern matches a function declaration named name, public or not, with any
// of the three modifiers Zig puts before "fn".
func fnPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(
		`^(?:pub` + pySpace + `+)?(?:extern` + pySpace + `+|export` + pySpace + `+|inline` +
			pySpace + `+)?fn` + pySpace + `+` + regexp.QuoteMeta(name) + pySpace + `*\(`)
}

// containerPattern matches the declaration of a public container type --
// struct, enum or union, including a tagged one -- up to its opening brace.
func containerPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(
		`^pub` + pySpace + `+const` + pySpace + `+` + regexp.QuoteMeta(name) + pySpace + `*=` +
			pySpace + `*(?:struct|enum|union)(?:` + pySpace + `*\(.*?\))?` + pySpace + `*\{`)
}

// dottedSymbolDetails resolves a dotted name like "Config.init" to the details
// of that member function.
//
// It finds the container's declaration, takes its brace-delimited body, and
// looks for the member inside it -- so a function of the same name declared
// elsewhere in the file is not mistaken for this type's.
func dottedSymbolDetails(source, symbolName string) *extractors.SymbolDetails {
	dot := strings.LastIndex(symbolName, ".")
	typeName, memberName := symbolName[:dot], symbolName[dot+1:]

	containerRe := containerPattern(typeName)
	memberRe := fnPattern(memberName)

	lines := strings.Split(source, "\n")
	for i, line := range lines {
		stripped := pyStrip(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if !containerRe.MatchString(stripped) {
			continue
		}

		// Locate the container's opening brace in the whole source, so the
		// brace scan sees the nesting the file really has.
		lineStart := 0
		for _, previous := range lines[:i] {
			lineStart += len(previous) + 1
		}
		braceOffset := strings.Index(lines[i], "{")
		if braceOffset < 0 {
			return nil
		}

		body, ok := extractors.ExtractBraceBlock(source, lineStart+braceOffset)
		if !ok {
			return nil
		}

		bodyLines := strings.Split(body, "\n")
		for j, bodyLine := range bodyLines {
			bodyStripped := pyStrip(bodyLine)
			if strings.HasPrefix(bodyStripped, "//") {
				continue
			}
			if memberRe.MatchString(bodyStripped) {
				return zigSymbolDetails(bodyLines, j)
			}
		}

		return nil
	}

	return nil
}

// zigParam is one parameter read out of a function declaration.
type zigParam struct {
	Name string
	Type *string
}

// parseZigParams parses the text between a function's parentheses into its
// parameters.
//
// The comptime keyword is dropped, a parameter with no type annotation is not
// a parameter the documentation can describe and is skipped, and the receiver
// -- spelled "self" -- is skipped too, because it is the type itself rather
// than something a caller passes.
func parseZigParams(paramStr string) []zigParam {
	var params []zigParam
	for _, part := range strings.Split(paramStr, ",") {
		part = pyStrip(part)
		if part == "" {
			continue
		}
		part = strings.TrimPrefix(part, "comptime ")

		colon := strings.Index(part, ":")
		if colon < 0 {
			continue
		}
		name := pyStrip(part[:colon])
		typeStr := pyStrip(part[colon+1:])
		if name == "self" {
			continue
		}
		var typePtr *string
		if typeStr != "" {
			typePtr = &typeStr
		}
		params = append(params, zigParam{Name: name, Type: typePtr})
	}
	return params
}

// zigSymbolDetails reads the parameters, the return type and the documentation
// status of the function declared at declLineIdx.
//
// Zig has no structured doc-comment tags, so a parameter counts as documented
// when the doc comment names it, and the return value counts as documented
// when the comment uses the word "return" or "returns".
func zigSymbolDetails(lines []string, declLineIdx int) *extractors.SymbolDetails {
	// A declaration whose parentheses cannot be found answers with no
	// parameters rather than with nothing at all: the symbol is there, its
	// signature is just unreadable.
	empty := &extractors.SymbolDetails{Params: []extractors.SymbolParam{}}

	sig := extractFnSignature(lines, declLineIdx)

	openIdx := strings.Index(sig, "(")
	if openIdx == -1 {
		return empty
	}

	depth := 0
	closeIdx := -1
	for i := openIdx; i < len(sig); i++ {
		switch sig[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return empty
	}

	rawParams := parseZigParams(sig[openIdx+1 : closeIdx])

	var returnType *string
	if returnTypeStr := pyStrip(sig[closeIdx+1:]); returnTypeStr != "" {
		returnType = &returnTypeStr
	}

	docText := collectDocCommentAbove(lines, declLineIdx)

	params := make([]extractors.SymbolParam, 0, len(rawParams))
	for _, p := range rawParams {
		params = append(params, extractors.SymbolParam{
			Name:       p.Name,
			Type:       p.Type,
			Documented: docText != "" && wordPattern(p.Name).MatchString(docText),
		})
	}

	return &extractors.SymbolDetails{
		Params:           params,
		ReturnType:       returnType,
		ReturnDocumented: docText != "" && returnsWord.MatchString(docText),
	}
}
