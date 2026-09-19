package python

// Python literal decoding, and the repr() spellings ast.unparse writes for a
// constant.
//
// Every reference page renders annotations, defaults and base classes the way
// ast.unparse renders them, and ast.unparse renders a constant through repr().
// So a literal read out of the concrete syntax tree is first decoded to its
// value -- escapes resolved, digit separators dropped, the base applied -- and
// then written back out in repr()'s own spelling, which is rarely the spelling
// the author typed: 0x1f is 31, 1e10 is 10000000000.0, and "it's" is "it's"
// with double quotes because repr switches quotes rather than escaping.

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/util"
)

// infLiteral is what ast.unparse substitutes for an infinity, which has no
// literal spelling of its own. A NaN becomes the difference of two of them.
const infLiteral = "1e309"

// literalPrefix reads a string literal's prefix letters, its quote run and the
// text between the quotes.
func literalPrefix(text string) (prefix, quote, body string, ok bool) {
	index := strings.IndexAny(text, "'\"")
	if index < 0 {
		return "", "", "", false
	}
	prefix = strings.ToLower(text[:index])
	rest := text[index:]
	switch {
	case strings.HasPrefix(rest, `"""`):
		quote = `"""`
	case strings.HasPrefix(rest, "'''"):
		quote = "'''"
	default:
		quote = rest[:1]
	}
	body = rest[len(quote):]
	body = strings.TrimSuffix(body, quote)
	return prefix, quote, body, true
}

// decodeEscapes resolves the escape sequences in a string literal's body.
//
// A raw literal carries its backslashes literally, and a bytes literal has no
// \u, \U or \N escape. An escape Python does not recognize is left standing,
// backslash and all, which is what Python does with, say, \d in a regular
// expression written without the r prefix.
func decodeEscapes(body string, raw, isBytes bool) string {
	if raw {
		return body
	}
	var out strings.Builder
	for i := 0; i < len(body); {
		ch := body[i]
		if ch != '\\' || i+1 >= len(body) {
			out.WriteByte(ch)
			i++
			continue
		}
		next := body[i+1]
		i += 2
		switch next {
		case '\n':
			// A line continuation contributes nothing.
		case '\\':
			out.WriteByte('\\')
		case '\'':
			out.WriteByte('\'')
		case '"':
			out.WriteByte('"')
		case 'a':
			out.WriteByte(7)
		case 'b':
			out.WriteByte(8)
		case 'f':
			out.WriteByte(12)
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'v':
			out.WriteByte(11)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			digits := string(next)
			for len(digits) < 3 && i < len(body) && body[i] >= '0' && body[i] <= '7' {
				digits += string(body[i])
				i++
			}
			value, err := strconv.ParseUint(digits, 8, 32)
			if err != nil {
				out.WriteString("\\" + digits)
				break
			}
			if isBytes {
				out.WriteByte(byte(value))
			} else {
				out.WriteRune(rune(value))
			}
		case 'x':
			if i+2 <= len(body) {
				if value, err := strconv.ParseUint(body[i:i+2], 16, 32); err == nil {
					i += 2
					if isBytes {
						out.WriteByte(byte(value))
					} else {
						out.WriteRune(rune(value))
					}
					break
				}
			}
			out.WriteString(`\x`)
		case 'u', 'U':
			width := 4
			if next == 'U' {
				width = 8
			}
			if isBytes {
				out.WriteByte('\\')
				out.WriteByte(next)
				break
			}
			if i+width <= len(body) {
				if value, err := strconv.ParseUint(body[i:i+width], 16, 64); err == nil {
					i += width
					out.WriteRune(rune(value))
					break
				}
			}
			out.WriteByte('\\')
			out.WriteByte(next)
		default:
			// Not an escape Python recognizes: the backslash stands.
			out.WriteByte('\\')
			out.WriteByte(next)
		}
	}
	return out.String()
}

// reprString is repr() for a str, which is what ast.unparse writes for a
// string constant.
func reprString(value string) string { return util.PythonRepr(value) }

// reprBytes is repr() for a bytes object: the b prefix, repr's own quote
// choice, and a \xNN escape for every byte outside printable ASCII.
func reprBytes(value string) string {
	quote := byte('\'')
	if strings.ContainsRune(value, '\'') && !strings.ContainsRune(value, '"') {
		quote = '"'
	}
	var out strings.Builder
	out.WriteByte('b')
	out.WriteByte(quote)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch == quote || ch == '\\':
			out.WriteByte('\\')
			out.WriteByte(ch)
		case ch == '\t':
			out.WriteString(`\t`)
		case ch == '\n':
			out.WriteString(`\n`)
		case ch == '\r':
			out.WriteString(`\r`)
		case ch >= 0x20 && ch < 0x7f:
			out.WriteByte(ch)
		default:
			fmt.Fprintf(&out, `\x%02x`, ch)
		}
	}
	out.WriteByte(quote)
	return out.String()
}

// reprInteger renders an integer literal the way repr(int) does: base ten, no
// digit separators, however the author spelled it.
func reprInteger(text string) string {
	cleaned := strings.ReplaceAll(text, "_", "")
	negative := false
	if strings.HasPrefix(cleaned, "-") {
		negative, cleaned = true, cleaned[1:]
	}
	base := 10
	if len(cleaned) > 2 && cleaned[0] == '0' {
		switch cleaned[1] {
		case 'x', 'X':
			base, cleaned = 16, cleaned[2:]
		case 'o', 'O':
			base, cleaned = 8, cleaned[2:]
		case 'b', 'B':
			base, cleaned = 2, cleaned[2:]
		}
	}
	value, ok := new(big.Int).SetString(cleaned, base)
	if !ok {
		return text
	}
	if negative {
		value.Neg(value)
	}
	return value.String()
}

// reprFloat renders a float the way ast.unparse writes one: repr(float), with
// the infinities and NaN spelled as the overflowing literal they parse back
// from.
func reprFloat(value float64) string {
	switch {
	case math.IsInf(value, 1):
		return infLiteral
	case math.IsInf(value, -1):
		return "-" + infLiteral
	case math.IsNaN(value):
		return "(" + infLiteral + "-" + infLiteral + ")"
	}
	return formatDouble(value, true)
}

// reprComplex renders an imaginary literal the way repr(complex) does. A
// literal has no real part, and the imaginary part carries no forced
// fractional digit -- repr(1j) is "1j", not "1.0j".
func reprComplex(imaginary float64) string {
	switch {
	case math.IsInf(imaginary, 1):
		return infLiteral + "j"
	case math.IsInf(imaginary, -1):
		return "-" + infLiteral + "j"
	case math.IsNaN(imaginary):
		return "(" + infLiteral + "-" + infLiteral + ")j"
	}
	return formatDouble(imaginary, false) + "j"
}

// formatDouble is CPython's shortest-round-trip double formatting.
//
// The digits are the shortest decimal string that reads back as the same
// double, and the notation follows the decimal point's position: exponential
// when it sits at or below -4 or above 16, fixed otherwise. addDot0 is the
// flag repr(float) sets and complex formatting does not, which is why 1.0
// prints as "1.0" but 1j prints as "1j".
//
// This is deliberately not [util.PythonFloatRepr]: that one answers for
// json.dumps, where an infinity is the word Infinity and a fixed rendering
// always carries a fractional part.
func formatDouble(value float64, addDot0 bool) string {
	sign := ""
	if math.Signbit(value) {
		sign, value = "-", -value
	}
	if value == 0 {
		if addDot0 {
			return sign + "0.0"
		}
		return sign + "0"
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	separator := strings.IndexByte(scientific, 'e')
	digits := strings.Replace(scientific[:separator], ".", "", 1)
	exponent, err := strconv.Atoi(scientific[separator+1:])
	if err != nil {
		// Unreachable: FormatFloat's 'e' form always carries a signed
		// decimal exponent.
		return sign + scientific
	}
	// decpt is the decimal point's position within the digit string: the
	// value is 0.<digits> * 10^decpt.
	decpt := exponent + 1

	if decpt <= -4 || decpt > 16 {
		mantissa := digits[:1]
		if len(digits) > 1 {
			mantissa += "." + digits[1:]
		}
		expSign := "+"
		magnitude := decpt - 1
		if magnitude < 0 {
			expSign, magnitude = "-", -magnitude
		}
		return fmt.Sprintf("%s%se%s%02d", sign, mantissa, expSign, magnitude)
	}
	switch {
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		out := sign + digits + strings.Repeat("0", decpt-len(digits))
		if addDot0 {
			out += ".0"
		}
		return out
	default:
		return sign + digits[:decpt] + "." + digits[decpt:]
	}
}

// numberLiteral renders a numeric literal's value in repr()'s spelling.
func numberLiteral(text string) string {
	cleaned := strings.ReplaceAll(text, "_", "")
	if strings.HasSuffix(cleaned, "j") || strings.HasSuffix(cleaned, "J") {
		value, err := strconv.ParseFloat(cleaned[:len(cleaned)-1], 64)
		if err != nil {
			return text
		}
		return reprComplex(value)
	}
	if isIntegerLiteral(cleaned) {
		return reprInteger(cleaned)
	}
	value, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return text
	}
	return reprFloat(value)
}

// isIntegerLiteral reports whether a numeric literal names an integer: no
// point, no exponent, or a base prefix that forbids both.
func isIntegerLiteral(cleaned string) bool {
	if len(cleaned) > 2 && cleaned[0] == '0' {
		switch cleaned[1] {
		case 'x', 'X', 'o', 'O', 'b', 'B':
			return true
		}
	}
	return !strings.ContainsAny(cleaned, ".eE")
}

// singleQuotes and multiQuotes are the quote runs ast.unparse chooses between
// when it writes an f-string.
var (
	singleQuotes = []string{"'", `"`}
	multiQuotes  = []string{"'''", `"""`}
	allQuotes    = []string{"'", `"`, "'''", `"""`}
)

// strLiteralHelper is CPython's _str_literal_helper: it escapes what must be
// escaped and reports which quote runs remain usable for the result.
//
// escapeSpecialWhitespace is the flag the f-string writer sets, and it decides
// whether a newline or a tab is written through or escaped.
func strLiteralHelper(
	text string, quoteTypes []string, escapeSpecialWhitespace bool,
) (string, []string) {
	var escaped strings.Builder
	for _, r := range text {
		if !escapeSpecialWhitespace && (r == '\n' || r == '\t') {
			escaped.WriteRune(r)
			continue
		}
		if r == '\\' || !isPrintable(r) {
			escaped.WriteString(unicodeEscape(r))
			continue
		}
		escaped.WriteRune(r)
	}
	result := escaped.String()

	possible := quoteTypes
	if strings.Contains(result, "\n") {
		possible = keepQuotes(possible, multiQuotes)
	}
	possible = dropContained(possible, result)
	if len(possible) == 0 {
		// Nothing fits: fall back to repr on the original text, keeping a
		// quote run from the requested set when repr chose one of them.
		quoted := reprString(text)
		quote := quoted[:1]
		for _, candidate := range quoteTypes {
			if strings.Contains(candidate, quoted[:1]) {
				quote = candidate
				break
			}
		}
		return quoted[1 : len(quoted)-1], []string{quote}
	}
	if result != "" {
		// Prefer '''"''' over """\"""": a quote run whose character is the
		// text's own last character sorts last.
		last := result[len(result)-1]
		stable := make([]string, 0, len(possible))
		for _, candidate := range possible {
			if candidate[0] != last {
				stable = append(stable, candidate)
			}
		}
		for _, candidate := range possible {
			if candidate[0] == last {
				stable = append(stable, candidate)
			}
		}
		possible = stable
		if possible[0][0] == last {
			result = result[:len(result)-1] + "\\" + string(last)
		}
	}
	return result, possible
}

// keepQuotes is the intersection of two quote-run lists, in the first's order.
func keepQuotes(quotes, allowed []string) []string {
	var kept []string
	for _, quote := range quotes {
		for _, candidate := range allowed {
			if quote == candidate {
				kept = append(kept, quote)
			}
		}
	}
	return kept
}

// dropContained removes every quote run the text already carries.
func dropContained(quotes []string, text string) []string {
	var kept []string
	for _, quote := range quotes {
		if !strings.Contains(text, quote) {
			kept = append(kept, quote)
		}
	}
	return kept
}

// isPrintable is Python's str.isprintable for one character: everything except
// the Other and Separator categories, with the ASCII space the one exception --
// which is what [unicode.IsPrint] already answers.
func isPrintable(r rune) bool { return unicode.IsPrint(r) }

// unicodeEscape is bytes.decode("unicode_escape")'s inverse for one character,
// which is what CPython's f-string writer escapes with.
func unicodeEscape(r rune) string {
	switch r {
	case '\\':
		return `\\`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	}
	switch {
	case r < 0x100:
		return fmt.Sprintf(`\x%02x`, r)
	case r < 0x10000:
		return fmt.Sprintf(`\u%04x`, r)
	default:
		return fmt.Sprintf(`\U%08x`, r)
	}
}

// cleanDoc is inspect.cleandoc, the normalization ast.get_docstring applies:
// tabs expanded, the first line stripped of leading spaces, the common indent
// of every later non-blank line removed, and the blank lines at either end
// dropped.
func cleanDoc(doc string) string {
	lines := strings.Split(expandTabs(doc), "\n")

	margin := -1
	for _, line := range lines[1:] {
		content := strings.TrimLeft(line, " ")
		if content == "" {
			continue
		}
		indent := len(line) - len(content)
		if margin < 0 || indent < margin {
			margin = indent
		}
	}
	if len(lines) > 0 {
		lines[0] = strings.TrimLeft(lines[0], " ")
	}
	if margin > 0 {
		for i := 1; i < len(lines); i++ {
			if len(lines[i]) > margin {
				lines[i] = lines[i][margin:]
			} else {
				lines[i] = ""
			}
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// expandTabs is Python's str.expandtabs() with its default tab size of eight:
// a tab advances to the next multiple of eight, counted from the line's start.
func expandTabs(text string) string {
	var out strings.Builder
	column := 0
	for _, r := range text {
		switch r {
		case '\t':
			width := 8 - column%8
			out.WriteString(strings.Repeat(" ", width))
			column += width
		case '\n', '\r':
			out.WriteRune(r)
			column = 0
		default:
			out.WriteRune(r)
			column++
		}
	}
	return out.String()
}
