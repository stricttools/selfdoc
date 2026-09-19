package zig

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// declaration is one public declaration read out of Zig source.
type declaration struct {
	// Kind is "const", "var" or "fn".
	Kind string
	Name string
	// Signature is the declaration as a reference page renders it, with the
	// body stripped.
	Signature string
	// Doc is the /// comment block above it, empty when it has none.
	Doc string
}

var (
	// pubFn matches a public function declaration, including the three
	// modifiers Zig puts between "pub" and "fn".
	pubFn = regexp.MustCompile(
		`^pub` + pySpace + `+((?:extern|export|inline)` + pySpace + `+)?fn` + pySpace + `+(` +
			pyWord + `+)` + pySpace + `*(\(.*)`)

	// pubConst matches a public constant declaration, whatever its right side
	// is -- a value, a struct, an enum, a union or an error set.
	pubConst = regexp.MustCompile(`^pub` + pySpace + `+const` + pySpace + `+(` + pyWord + `+)` + pySpace + `*(.*)`)

	// pubVar matches a public variable declaration.
	pubVar = regexp.MustCompile(`^pub` + pySpace + `+var` + pySpace + `+(` + pyWord + `+)` + pySpace + `*(.*)`)

	// signatureBody matches the body a declaration's signature drops.
	signatureBody = regexp.MustCompile(pySpace + `*\{.*$`)

	// unclosedBody matches a body opener with no closing brace after it on
	// the same line, which is what a multi-line struct or enum declaration
	// ends with.
	unclosedBody = regexp.MustCompile(pySpace + `*\{[^}]*$`)
)

// extractPubDeclarations lists every public declaration in a Zig source file,
// in source order, with the doc comment above each.
//
// A name is reported once: the first declaration wins, so a conditional
// re-declaration later in the file does not produce a second entry. Test
// blocks and comment lines are skipped.
func extractPubDeclarations(source string) []declaration {
	lines := strings.Split(source, "\n")
	var declarations []declaration
	seen := map[string]bool{}

	for i, line := range lines {
		stripped := pyStrip(line)

		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if strings.HasPrefix(stripped, "test ") {
			continue
		}

		if m := pubFn.FindStringSubmatch(stripped); m != nil {
			name := m[2]
			if !seen[name] {
				seen[name] = true
				declarations = append(declarations, declaration{
					Kind:      "fn",
					Name:      name,
					Signature: extractFnSignature(lines, i),
					Doc:       collectDocCommentAbove(lines, i),
				})
			}
			continue
		}

		if m := pubConst.FindStringSubmatch(stripped); m != nil {
			name := m[1]
			if !seen[name] {
				seen[name] = true
				declarations = append(declarations, declaration{
					Kind:      "const",
					Name:      name,
					Signature: cleanSignature(stripped),
					Doc:       collectDocCommentAbove(lines, i),
				})
			}
			continue
		}

		if m := pubVar.FindStringSubmatch(stripped); m != nil {
			name := m[1]
			if !seen[name] {
				seen[name] = true
				declarations = append(declarations, declaration{
					Kind:      "var",
					Name:      name,
					Signature: cleanSignature(stripped),
					Doc:       collectDocCommentAbove(lines, i),
				})
			}
			continue
		}
	}

	return declarations
}

// extractFnSignature is a function declaration's signature, joined from up to
// five lines -- enough for a wrapped parameter list -- and ending where the
// body begins.
func extractFnSignature(lines []string, startIdx int) string {
	var sigParts []string
	end := startIdx + 5
	if end > len(lines) {
		end = len(lines)
	}
	for i := startIdx; i < end; i++ {
		line := pyStrip(lines[i])
		sigParts = append(sigParts, line)
		if strings.Contains(line, "{") || strings.Contains(line, ";") {
			break
		}
	}

	sig := strings.Join(sigParts, " ")
	sig = signatureBody.ReplaceAllString(sig, "")
	return pyStrip(strings.TrimRight(sig, ";"))
}

// cleanSignature is a constant or variable declaration's line, with a
// multi-line body's opening brace and a trailing semicolon dropped.
func cleanSignature(line string) string {
	line = unclosedBody.ReplaceAllString(line, "")
	return pyStrip(strings.TrimRight(line, ";"))
}

// extractModuleDoc is a file's module documentation: the run of //! comment
// lines at the top, with the marker and one following space removed.
//
// The run must be contiguous and must come before any code. A blank line or a
// // comment before it is allowed; anything else means the file has no module
// documentation.
func extractModuleDoc(source string) string {
	var docLines []string

	for _, line := range strings.Split(source, "\n") {
		stripped := pyStrip(line)
		switch {
		case strings.HasPrefix(stripped, "//!"):
			text := stripped[3:]
			text = strings.TrimPrefix(text, " ")
			docLines = append(docLines, text)
		case len(docLines) > 0:
			// The module doc is one contiguous run at the top of the file.
			return strings.Join(docLines, "\n")
		case stripped != "" && !strings.HasPrefix(stripped, "//"):
			// Code before any //! line: this file has no module doc.
			return ""
		}
	}

	return strings.Join(docLines, "\n")
}

// collectDocCommentAbove is the /// comment block immediately above the line
// at targetLineIdx, with blank lines between the block and the declaration
// crossed.
func collectDocCommentAbove(lines []string, targetLineIdx int) string {
	return extractors.CollectCommentLinesAbove(lines, targetLineIdx, "///", true)
}
