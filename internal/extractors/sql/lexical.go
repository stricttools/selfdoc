package sql

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/util"
)

// The two character classes the ported patterns are written against.
//
// Go's own \w and \s are ASCII-only, and the patterns came from Python, whose
// \w matches a letter, a digit or an underscore in any script and whose \s adds
// the four ASCII separators to the Unicode whitespace set.
const (
	wordChars  = util.PythonWordChars
	spaceChars = util.PythonSpaceChars
	wordClass  = "[" + wordChars + "]"
	spaceClass = "[" + spaceChars + "]"
)

// isSpaceByte reports whether one byte of a DDL document is whitespace. SQL's
// own syntax is ASCII, so a byte that begins a multi-byte sequence belongs to
// an identifier or a string literal rather than to the whitespace between
// tokens.
func isSpaceByte(b byte) bool {
	return b < 0x80 && util.IsPythonSpace(rune(b))
}

// isWordRune reports whether r is a word character by Python's \w rule for
// text patterns: a letter, a digit or an underscore, in any script.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// isPyAlnum reports whether r is alphanumeric by Python's str.isalnum rule,
// which the default-value scanner tests a keyword's boundary with. The
// underscore is deliberately not alphanumeric.
func isPyAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r)
}

// strip is Python's str.strip() with no argument.
func strip(s string) string { return util.PythonStrip(s) }

// lstrip is Python's str.lstrip() with no argument.
func lstrip(s string) string { return util.PythonLStrip(s) }

// fields splits s on runs of whitespace and drops the empty parts, which is
// what Python's str.split() with no argument does.
func fields(s string) []string { return util.PythonFields(s) }

// dollarTagRe matches a dollar-quote delimiter at the start of the text:
// $$ or $tag$.
var dollarTagRe = regexp.MustCompile(`^\$(` + wordClass + `*)\$`)

// stripComments removes SQL's line comments (--) and block comments (/* */).
//
// String literals are kept whole, both single-quoted and dollar-quoted, so a
// comment marker inside a literal is not mistaken for a comment. Everything
// that reads the document's structure reads this rather than the raw text; the
// COMMENT ON statements are real SQL and are read from it too.
func stripComments(source string) string {
	var result strings.Builder
	i := 0
	n := len(source)

	for i < n {
		switch {
		case source[i] == '\'':
			end := skipSingleQuoted(source, i)
			result.WriteString(source[i:end])
			i = end

		case source[i] == '$':
			if m := dollarTagRe.FindString(source[i:]); m != "" {
				tag := m
				endPos := strings.Index(source[i+len(tag):], tag)
				if endPos >= 0 {
					end := i + len(tag) + endPos + len(tag)
					result.WriteString(source[i:end])
					i = end
				} else {
					result.WriteByte(source[i])
					i++
				}
			} else {
				result.WriteByte(source[i])
				i++
			}

		case strings.HasPrefix(source[i:], "--"):
			eol := strings.Index(source[i:], "\n")
			if eol < 0 {
				return result.String()
			}
			i += eol // the newline itself is kept

		case strings.HasPrefix(source[i:], "/*"):
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				return result.String()
			}
			i += 2 + end + 2

		default:
			result.WriteByte(source[i])
			i++
		}
	}

	return result.String()
}

// skipSingleQuoted is the index just past the single-quoted string that starts
// at start, with ” read as an embedded quote. An unterminated string runs to
// the end of the document.
func skipSingleQuoted(source string, start int) int {
	i := start + 1
	n := len(source)
	for i < n {
		if source[i] == '\'' {
			if i+1 < n && source[i+1] == '\'' {
				i += 2 // an escaped quote
				continue
			}
			return i + 1 // the end of the string
		}
		i++
	}
	return n // unterminated
}

// splitTopLevel splits a comma-separated list on the commas at paren depth
// zero, so a parameterized type or a function call keeps its own commas. Each
// part is stripped.
func splitTopLevel(text string) []string {
	var parts []string
	depth := 0
	var current strings.Builder

	for i := 0; i < len(text); i++ {
		switch ch := text[i]; ch {
		case '(':
			depth++
			current.WriteByte(ch)
		case ')':
			depth--
			current.WriteByte(ch)
		case ',':
			if depth == 0 {
				parts = append(parts, strip(current.String()))
				current.Reset()
			} else {
				current.WriteByte(ch)
			}
		default:
			current.WriteByte(ch)
		}
	}

	if trailing := strip(current.String()); trailing != "" {
		parts = append(parts, trailing)
	}
	return parts
}

// matchingParen is the index of the parenthesis that closes the one at
// openPos. An unbalanced list answers openPos, which is what the Python this
// ports did.
func matchingParen(text string, openPos int) int {
	depth := 0
	end := openPos
	for idx := openPos; idx < len(text); idx++ {
		if text[idx] == '(' {
			depth++
		} else if text[idx] == ')' {
			depth--
			if depth == 0 {
				return idx
			}
		}
	}
	return end
}

// wordBoundaryBefore reports whether a word starting at pos has a boundary in
// front of it, by Python's Unicode-aware \b.
func wordBoundaryBefore(text string, pos int) bool {
	if pos <= 0 {
		return true
	}
	for i := pos - 1; i >= 0; i-- {
		if text[i]&0xC0 != 0x80 {
			return !isWordRune([]rune(text[i:pos])[0])
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
	return !isWordRune([]rune(text[pos:])[0])
}

// bareNullMatches lists the byte ranges of every NULL that stands as a word and
// is not the NULL of a NOT NULL.
//
// The Python pattern was a negative lookbehind -- (?<!\bNOT\s)\bNULL\b -- which
// RE2 cannot express, so the four characters in front of each candidate are
// read directly. What it decides is a column's nullability: an explicit NULL
// says the column accepts one, and the NULL inside NOT NULL says the opposite,
// so the two must not be confused.
func bareNullMatches(text string) [][2]int {
	var out [][2]int
	for i := 0; i+4 <= len(text); {
		if !strings.EqualFold(text[i:i+4], "NULL") {
			i++
			continue
		}
		if !wordBoundaryBefore(text, i) || !wordBoundaryAfter(text, i+4) {
			i++
			continue
		}
		if precededByNot(text, i) {
			i += 4
			continue
		}
		out = append(out, [2]int{i, i + 4})
		i += 4
	}
	return out
}

// precededByNot reports whether the NULL at pos is preceded by the word NOT and
// one whitespace character, which is the lookbehind the Python pattern wrote.
func precededByNot(text string, pos int) bool {
	if pos < 4 {
		return false
	}
	if !strings.EqualFold(text[pos-4:pos-1], "NOT") {
		return false
	}
	if !isSpaceByte(text[pos-1]) {
		return false
	}
	return wordBoundaryBefore(text, pos-4)
}

// hasBareNull reports whether text carries an explicit NULL.
func hasBareNull(text string) bool { return len(bareNullMatches(text)) > 0 }

// removeBareNulls deletes every explicit NULL from text.
func removeBareNulls(text string) string {
	matches := bareNullMatches(text)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, match := range matches {
		b.WriteString(text[last:match[0]])
		last = match[1]
	}
	b.WriteString(text[last:])
	return b.String()
}
