package sql

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

var (
	// paramModeRe matches the mode keyword a function parameter can open with.
	paramModeRe = regexp.MustCompile(`(?i)^(INOUT|IN|OUT)\b`)

	// returnsRe matches the RETURNS keyword of a function definition.
	returnsRe = regexp.MustCompile(`(?i)\bRETURNS` + spaceClass + `+`)

	// returnsEndRe matches a word that ends the RETURNS type clause.
	returnsEndRe = regexp.MustCompile(`(?i)^(?:AS|LANGUAGE|BEGIN|IMMUTABLE|STABLE|VOLATILE|STRICT|` +
		`SECURITY|COST|ROWS|SET|CALLED|RETURNS|PARALLEL)$`)
)

// functionSymbolDetails builds the details for a CREATE FUNCTION: its
// parameters, its return type, and whether a COMMENT ON FUNCTION documents it.
//
// It answers nothing for a name the document does not create a function under,
// which includes every table, view and type -- those declare no parameters and
// return nothing.
func functionSymbolDetails(
	cleanSource, funcName string,
	comments *commentSet,
) *extractors.SymbolDetails {
	for _, m := range createFunctionRe.FindAllStringSubmatchIndex(cleanSource, -1) {
		matchedName := cleanSource[m[4]:m[5]]
		if !strings.EqualFold(matchedName, funcName) {
			continue
		}

		parenStart := m[1] - 1 // the opening parenthesis the match ends on
		closeParen := matchingParen(cleanSource, parenStart)

		params := parseFunctionParams(cleanSource[parenStart+1 : closeParen])
		returnType := parseReturnsClause(cleanSource[closeParen+1:])

		return &extractors.SymbolDetails{
			Params:           params,
			ReturnType:       returnType,
			ReturnDocumented: hasFunctionComment(comments, funcName),
		}
	}

	return nil
}

// parseFunctionParams reads a function's parameter list.
//
// Every parameter is reported undocumented: SQL has no per-parameter
// documentation to read, since COMMENT ON FUNCTION documents the function as a
// whole.
func parseFunctionParams(paramsText string) []extractors.SymbolParam {
	var params []extractors.SymbolParam

	for _, part := range splitTopLevel(paramsText) {
		part = strip(part)
		if part == "" {
			continue
		}

		if mode := paramModeRe.FindString(part); mode != "" {
			part = strip(part[len(mode):])
		}

		tokens := fields(part)
		if len(tokens) == 0 {
			continue
		}

		param := extractors.SymbolParam{Name: tokens[0]}

		// Everything after the name is the type, up to a default value.
		var typeTokens []string
		for _, token := range tokens[1:] {
			if strings.EqualFold(token, "DEFAULT") {
				break
			}
			typeTokens = append(typeTokens, token)
		}
		if len(typeTokens) > 0 {
			paramType := strings.Join(typeTokens, " ")
			param.Type = &paramType
		}

		params = append(params, param)
	}

	return params
}

// parseReturnsClause reads the return type out of what follows a function's
// parameter list: the words after RETURNS, up to a keyword that begins the
// function's body or its attributes, a dollar-quote or the statement's
// semicolon.
func parseReturnsClause(afterParams string) *string {
	returnsMatch := returnsRe.FindStringIndex(afterParams)
	if returnsMatch == nil {
		return nil
	}

	rest := afterParams[returnsMatch[1]:]

	var typeTokens []string
	i := 0
	n := len(rest)

	for i < n {
		for i < n && isSpaceByte(rest[i]) {
			i++
		}
		if i >= n {
			break
		}

		if rest[i] == '$' || rest[i] == ';' {
			break
		}

		wordStart := i
		for i < n && !isSpaceByte(rest[i]) && rest[i] != '$' && rest[i] != ';' {
			i++
		}
		word := rest[wordStart:i]

		if word == "" {
			break
		}

		if returnsEndRe.MatchString(word) {
			break
		}

		typeTokens = append(typeTokens, word)
	}

	if len(typeTokens) == 0 {
		return nil
	}
	returnType := strings.Join(typeTokens, " ")
	return &returnType
}
