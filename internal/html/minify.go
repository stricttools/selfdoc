package html

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

var (
	jsBlockCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	jsLineCommentRE  = regexp.MustCompile(`(?m)^[ \t]*//.*$`)
	jsSpaceRunRE     = regexp.MustCompile(`[ \t]+`)
	jsStructuralRE   = regexp.MustCompile(pySpaceClass + `*([{}();,=])` + pySpaceClass + `*`)
	jsBlankRunRE     = regexp.MustCompile(`\n{2,}`)
)

// MinifyJS removes comments from JavaScript and collapses its whitespace.
//
// The approach is conservative: it does not break a URL containing "//"
// and it keeps a single space between identifiers so two of them cannot
// merge into one.
//
// A line whose first non-whitespace characters are "//" is a comment, full
// stop -- no script this package ships carries a multi-line string
// literal, so there is nothing else it could be. It is stripped
// unconditionally. The quote check applies only to a "//" that follows code
// on the same line, where it really might be inside a string.
//
// That distinction is not a nicety. The check used to apply to line-initial
// comments too, so a comment containing an apostrophe -- "the framework's
// combobox shape" -- was left in place, and the whitespace collapse below
// then pulled the FOLLOWING statement up onto the comment's line and
// commented it out, along with every block after it in the same assembled
// script. The symptom was a page whose scripts simply did not run, with no
// error anywhere.
func MinifyJS(jsText string) string {
	jsText = jsBlockCommentRE.ReplaceAllString(jsText, "")
	jsText = jsLineCommentRE.ReplaceAllString(jsText, "")
	jsText = stripTrailingLineComments(jsText)
	jsText = jsSpaceRunRE.ReplaceAllString(jsText, " ")
	jsText = jsStructuralRE.ReplaceAllString(jsText, "${1}")
	jsText = jsBlankRunRE.ReplaceAllString(jsText, "\n")
	return util.PythonStrip(jsText)
}

// stripTrailingLineComments removes a "//" comment that follows code on the
// same line, when the rest of that line carries no quote character.
//
// This is the hand-written form of the lookbehind-plus-lookahead pattern
// `(?m)(?<=[;{}])[ \t]*//(?!.*['"]).*$`, which RE2 cannot express: the
// match must start immediately after a ";", "{" or "}", and the remainder
// of the line after the "//" must contain neither an apostrophe nor a
// double quote -- the check that keeps a "//" inside a string literal, most
// often a URL, from being read as a comment.
//
// Like the regex substitution it replaces, the scan is left to right and
// non-overlapping, resuming at the end of each removed comment, and the
// terminating newline is never part of the match.
func stripTrailingLineComments(s string) string {
	var b strings.Builder
	last := 0
	for i := 1; i < len(s); {
		switch s[i-1] {
		case ';', '{', '}':
		default:
			i++
			continue
		}
		k := i
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		if k+1 >= len(s) || s[k] != '/' || s[k+1] != '/' {
			i++
			continue
		}
		lineEnd := len(s)
		if nl := strings.IndexByte(s[k:], '\n'); nl >= 0 {
			lineEnd = k + nl
		}
		if strings.ContainsAny(s[k+2:lineEnd], "'\"") {
			i++
			continue
		}
		b.WriteString(s[last:i])
		last = lineEnd
		i = lineEnd
	}
	b.WriteString(s[last:])
	return b.String()
}
