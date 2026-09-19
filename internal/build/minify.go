package build

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

var (
	// cssCommentPattern matches a CSS comment, newlines included.
	cssCommentPattern = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// whitespaceRunPattern matches a run of Python-whitespace characters.
	whitespaceRunPattern = regexp.MustCompile(util.PythonSpaceClass + `+`)
	// cssPunctuationPattern matches a CSS separator and the whitespace
	// around it.
	cssPunctuationPattern = regexp.MustCompile(
		util.PythonSpaceClass + `*([{}:;,])` + util.PythonSpaceClass + `*`)
	// htmlCommentPattern matches an HTML comment, newlines included.
	htmlCommentPattern = regexp.MustCompile(`(?s)<!--.*?-->`)
	// interTagWhitespacePattern matches the whitespace between two tags.
	interTagWhitespacePattern = regexp.MustCompile(`>` + util.PythonSpaceClass + `+<`)
	// preservedElementPattern matches an element whose content keeps its
	// whitespace.
	preservedElementPattern = regexp.MustCompile(
		`(?si)<(?:pre|code|script|textarea)\b[^>]*>.*?</(?:pre|code|script|textarea)>`)
)

// MinifyCSS minifies CSS by removing comments, collapsing whitespace, and
// trimming the ends.
//
// A regular-expression pass, which is enough for well-formed CSS and is what
// every generated stylesheet has been shipped through.
func MinifyCSS(cssText string) string {
	cssText = cssCommentPattern.ReplaceAllString(cssText, "")
	cssText = whitespaceRunPattern.ReplaceAllString(cssText, " ")
	cssText = cssPunctuationPattern.ReplaceAllString(cssText, "$1")
	cssText = strings.ReplaceAll(cssText, ";}", "}")
	return util.PythonStrip(cssText)
}

// CriticalCSSMarker separates a theme stylesheet's critical part -- the styles
// the first paint needs -- from the rest, which a page loads asynchronously.
//
// The themes package states the same marker for its own split of the embedded
// stylesheets, and the two have to be equal: the sheet this function splits is
// the one that package composed.
const CriticalCSSMarker = "/* --- NON-CRITICAL --- */"

// ExtractCriticalCSS splits a theme stylesheet into its critical part and the
// whole sheet.
//
// The critical part is everything above [CriticalCSSMarker], right-trimmed. A
// sheet carrying no marker is treated as critical in full, which is the safe
// answer: a page then inlines more than it needs rather than painting unstyled.
func ExtractCriticalCSS(fullCSS string) (critical, full string) {
	if before, _, found := strings.Cut(fullCSS, CriticalCSSMarker); found {
		return util.PythonRStrip(before), fullCSS
	}
	return fullCSS, fullCSS
}

// MinifyHTML minifies HTML by removing comments and collapsing the whitespace
// between tags and inside text nodes.
//
// The content of pre, code, script and textarea elements is preserved
// verbatim: those are the elements whose whitespace is meaningful.
func MinifyHTML(htmlText string) string {
	var out strings.Builder
	last := 0
	for _, m := range preservedElementPattern.FindAllStringIndex(htmlText, -1) {
		out.WriteString(minifySegment(htmlText[last:m[0]]))
		out.WriteString(htmlText[m[0]:m[1]])
		last = m[1]
	}
	out.WriteString(minifySegment(htmlText[last:]))
	return out.String()
}

// minifySegment minifies one stretch of HTML that carries no preserved
// element.
func minifySegment(part string) string {
	part = htmlCommentPattern.ReplaceAllString(part, "")
	part = interTagWhitespacePattern.ReplaceAllString(part, "> <")
	return whitespaceRunPattern.ReplaceAllString(part, " ")
}
