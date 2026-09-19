package html

import (
	"strings"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/util"
)

// pySpaceClass is this package's short spelling of the character class
// Python's re module gives `\s` when it matches a str.
//
// Go's own `\s` is the five ASCII characters plus the space, so a regex
// ported verbatim would stop recognizing the vertical tab, the information
// separators, NEL, the no-break space and the Unicode space separators --
// every one of which turns up in prose that reaches this package through a
// pasted document. Patterns that read prose use this class; patterns that
// read markup this package itself emitted use Go's `\s`, because that
// markup is ASCII by construction.
const pySpaceClass = util.PythonSpaceClass

// pyCapitalize renders s the way Python's str.capitalize() does: the first
// character upper-cased and every later one lower-cased.
func pyCapitalize(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + strings.ToLower(s[size:])
}

// runesBack returns the offset n runes before pos in s, clamped at 0.
//
// Python's slicing counts characters, so a lookback window of "200
// characters" is not 200 bytes once a page carries anything outside ASCII.
func runesBack(s string, pos, n int) int {
	i := pos
	for k := 0; k < n && i > 0; k++ {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
	}
	return i
}
