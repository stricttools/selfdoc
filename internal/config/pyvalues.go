package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/stricttools/selfdoc/internal/util"
)

// DecodeDocument decodes a selfdoc.json document into the value shapes the
// validator expects, matching Python's json module: an object becomes a
// map[string]any, an array a []any, an integer literal an int64, a number
// carrying a fraction or an exponent a float64, a string a string,
// true/false a bool, and null a nil any.
//
// Trailing content after the first value is an error, as it is for
// json.load. An integer literal too large for an int64 becomes a float64 --
// the one place Python's arbitrary-precision int cannot be reproduced -- so
// such a value fails an integer field's type check instead of passing it.
func DecodeDocument(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("extra data after the top-level value")
		}
		return nil, err
	}
	return pythonizeNumbers(raw), nil
}

// pythonizeNumbers rewrites every json.Number in a decoded document into the
// int64 or float64 Python's decoder would have produced.
func pythonizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			t[k] = pythonizeNumbers(item)
		}
		return t
	case []any:
		for i, item := range t {
			t[i] = pythonizeNumbers(item)
		}
		return t
	case json.Number:
		s := t.String()
		if !strings.ContainsAny(s, ".eE") {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return s
		}
		return f
	default:
		return v
	}
}

// asInt reports whether v is one of the integer shapes a document can carry,
// which deliberately excludes a bool -- Python's isinstance(value, int) is
// true for a bool, and every integer check in the validator rules one out.
func asInt(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	default:
		return 0, false
	}
}

// asFloat reports whether v is a number, accepting an integer and coercing
// it, the way the validator's float fields do.
func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

// formatBound renders a numeric bound for a diagnostic the way Python
// renders it inside an f-string, including "None" for an absent bound --
// which the integer range message prints verbatim when only one bound is
// declared.
func formatBound(v any) string {
	if v == nil {
		return "None"
	}
	return util.PythonRepr(v)
}

var patternCache sync.Map // Python pattern string -> *regexp.Regexp

// matchPattern reports whether s matches the Python regular expression
// pattern the way re.match does: anchored at the start of the string, free
// at the end unless the pattern says otherwise.
//
// A pattern the translator cannot express in RE2 panics rather than
// silently accepting or rejecting: every pattern in this package's schema is
// a compile-time constant, so an untranslatable one is a bug in the schema,
// not something a document can provoke.
func matchPattern(pattern, s string) bool {
	if cached, ok := patternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp).MatchString(s)
	}
	translated, err := pythonPatternToRE2(pattern)
	if err != nil {
		panic(fmt.Sprintf("config: schema pattern %q cannot be translated: %v", pattern, err))
	}
	re, err := regexp.Compile(`\A(?:` + translated + `)`)
	if err != nil {
		panic(fmt.Sprintf("config: schema pattern %q translated to an invalid RE2 expression: %v", pattern, err))
	}
	patternCache.Store(pattern, re)
	return re.MatchString(s)
}

// pythonPatternToRE2 rewrites the Python regular-expression constructs this
// schema uses into their RE2 equivalents.
//
// Two rewrites are not cosmetic. Python's \d, \w and \s are Unicode-aware on
// a str pattern while Go's are ASCII-only, so each becomes its Unicode class.
// Python's "$" outside MULTILINE matches at the end of the string OR just
// before a trailing newline, which RE2 cannot say with a lookahead, so a
// trailing "$" becomes "\n?\z".
//
// Anything else Python-specific -- a backreference, a lookaround, a "$" that
// is not at the end -- is refused rather than approximated. The rule for those is a hand-written
// scanner, not a nearly-right regex.
func pythonPatternToRE2(pattern string) (string, error) {
	var b strings.Builder
	inClass := false
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '\\':
			if i+1 >= len(runes) {
				return "", fmt.Errorf("trailing backslash")
			}
			i++
			switch runes[i] {
			case 'd':
				if inClass {
					b.WriteString(`\p{Nd}`)
				} else {
					b.WriteString(`[\p{Nd}]`)
				}
			case 'w':
				if inClass {
					b.WriteString(`\p{L}\p{N}_`)
				} else {
					b.WriteString(`[\p{L}\p{N}_]`)
				}
			case 's':
				if inClass {
					b.WriteString(`\t\n\f\r \x0b\p{Z}`)
				} else {
					b.WriteString(`[\t\n\f\r \x0b\p{Z}]`)
				}
			case 'D', 'W', 'S':
				if inClass {
					return "", fmt.Errorf(`negated class shorthand \%c inside a character class`, runes[i])
				}
				switch runes[i] {
				case 'D':
					b.WriteString(`[^\p{Nd}]`)
				case 'W':
					b.WriteString(`[^\p{L}\p{N}_]`)
				case 'S':
					b.WriteString(`[^\t\n\f\r \x0b\p{Z}]`)
				}
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				return "", fmt.Errorf("backreference is not expressible in RE2")
			default:
				b.WriteByte('\\')
				b.WriteRune(runes[i])
			}
		case '[':
			if inClass {
				b.WriteString(`\[`)
			} else {
				inClass = true
				b.WriteByte('[')
			}
		case ']':
			inClass = false
			b.WriteByte(']')
		case '$':
			if inClass {
				b.WriteString(`\$`)
			} else if i == len(runes)-1 {
				b.WriteString(`\n?\z`)
			} else {
				return "", fmt.Errorf(`"$" away from the end of the pattern`)
			}
		case '(':
			if inClass {
				b.WriteString(`\(`)
				break
			}
			if strings.HasPrefix(string(runes[i:]), "(?=") ||
				strings.HasPrefix(string(runes[i:]), "(?!") ||
				strings.HasPrefix(string(runes[i:]), "(?<") {
				return "", fmt.Errorf("lookaround is not expressible in RE2")
			}
			b.WriteByte('(')
		default:
			b.WriteRune(c)
		}
	}
	if inClass {
		return "", fmt.Errorf("unterminated character class")
	}
	return b.String(), nil
}

// posixBasename returns everything after the last slash, the way Python's
// os.path.basename does on POSIX -- including the empty string for an empty
// input, where path.Base would answer ".".
func posixBasename(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
