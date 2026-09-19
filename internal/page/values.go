package page

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/util"
)

// fmString reads a string frontmatter value, answering "" for a key the
// document does not carry and for one whose value is not a string.
//
// Every frontmatter key this package reads is consumed as a string by the
// Python it replaces -- a title, a type, a description -- and the Python
// would raise on a non-string where it escapes or concatenates one. Reading
// only strings turns that crash into the absent-value branch.
func fmString(meta util.Frontmatter, key string) string {
	s, _ := meta[key].(string)
	return s
}

// fmBoolTrue reports whether the value of key is the boolean true, which is
// the identity test Python's `is True` performs: a truthy string or a nonzero
// number does not answer yes.
func fmBoolTrue(meta util.Frontmatter, key string) bool {
	b, ok := meta[key].(bool)
	return ok && b
}

// fmNumberOK reads a numeric frontmatter value, reporting whether the key
// carries one.
//
// Python's isinstance(value, (int, float)) accepts a bool, because bool is a
// subclass of int there -- so a boolean read out of a numeric key is the number
// 1 or 0 rather than a non-number. That is reproduced. The frontmatter schema
// types `nav_order` as an integer, so a block never brings one here; a
// frontmatter map assembled in memory still can.
//
// A plain int is accepted beside an int64 for the same reason the config
// validator accepts one: a document assembled in memory carries whichever the
// caller wrote.
func fmNumberOK(meta util.Frontmatter, key string) (float64, bool) {
	switch v := meta[key].(type) {
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case float64:
		return v, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// fmNumber reads a numeric frontmatter value, answering 0 for a key the
// document does not carry and for one whose value is not a number.
func fmNumber(meta util.Frontmatter, key string) float64 {
	n, _ := fmNumberOK(meta, key)
	return n
}

// fmStrings reads a frontmatter value as a list of strings.
//
// A bare string is one item, an empty string is no items, and a list is
// itself -- the coercion the page tags go through, where "tags: release" and
// "tags: [release]" mean the same thing.
func fmStrings(meta util.Frontmatter, key string) []string {
	switch v := meta[key].(type) {
	case []string:
		return append([]string(nil), v...)
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

// pyStr renders v the way Python's str() does, for the frontmatter values
// that reach a comparison or a template as text rather than as their own
// type.
func pyStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return util.PythonFloatRepr(t)
	}
	return fmt.Sprint(v)
}

// pythonCapitalize reproduces Python's str.capitalize(): the first character
// title-cased and every other character lower-cased.
//
// It is not str.title(): a hyphen or an underscore is uncased, so "get-started"
// capitalizes to "Get-started" where title-casing would give "Get-Started".
// The breadcrumb and BreadcrumbList emitters name directory segments with it.
func pythonCapitalize(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return util.TitleCase(string(r)) + pythonLower(s[size:])
}

// pythonLower reproduces Python's str.lower(), which differs from
// strings.ToLower in one code point: the Latin capital I with dot above
// lowercases to an "i" plus a combining dot rather than to a bare "i".
func pythonLower(s string) string {
	if !strings.ContainsRune(s, 0x0130) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	for _, r := range s {
		if r == 0x0130 {
			b.WriteString("i̇")
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// parseISODate parses an ISO date the way Python's
// strptime(value, "%Y-%m-%d") does, reporting whether it is one.
//
// Python's month and day directives accept an unpadded field -- "2026-1-5" is
// a date there while time.Parse's own "2006-01-02" layout refuses it -- while
// its year directive takes exactly four digits. So the fields are read as
// digit runs of those widths and validated by round-tripping the constructed
// date, which is what rejects a February 30th.
func parseISODate(value string) (time.Time, bool) {
	parts := strings.Split(value, "-")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	widths := [3][2]int{{4, 4}, {1, 2}, {1, 2}}
	var nums [3]int
	for i, part := range parts {
		if len(part) < widths[i][0] || len(part) > widths[i][1] {
			return time.Time{}, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return time.Time{}, false
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return time.Time{}, false
		}
		nums[i] = n
	}
	if nums[1] < 1 || nums[1] > 12 || nums[2] < 1 || nums[2] > 31 {
		return time.Time{}, false
	}
	t := time.Date(nums[0], time.Month(nums[1]), nums[2], 0, 0, 0, 0, time.UTC)
	if t.Year() != nums[0] || int(t.Month()) != nums[1] || t.Day() != nums[2] {
		return time.Time{}, false
	}
	return t, true
}

// formatDateModified renders an ISO date the way the page footer and the
// content header show it -- "June 29, 2026" -- and returns the value
// unchanged when it is not a date this build can read, which is what the
// Python's except branch does.
func formatDateModified(value string) string {
	if t, ok := parseISODate(value); ok {
		return util.FormatDateLong(t)
	}
	return value
}
