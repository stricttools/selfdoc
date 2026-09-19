package site

import (
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// unknownKeys returns the keys of table that allowed does not name, sorted --
// the spelling every "declares unknown key(s)" refusal names them in.
func unknownKeys(table map[string]any, allowed []string) []string {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	unknown := make([]string, 0)
	for key := range table {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// joinReprs renders keys the way Python's ", ".join(repr(k) for k in keys)
// does, because the refusals quote every key they name.
func joinReprs(keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, util.PythonRepr(key))
	}
	return strings.Join(parts, ", ")
}

// asList normalizes a decoded TOML array into a slice of elements. The decoder
// answers an array of tables as []map[string]any and every other array as
// []any, and both spell the same document shape Python's tomllib returns as
// one list of dicts.
//
// An absent key -- a nil value -- is an empty list, which is what Python's
// data.get("project", []) answers and what makes a roster with no [[project]]
// block legal.
func asList(value any) ([]any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, true
	case []any:
		return typed, true
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items, true
	default:
		return nil, false
	}
}

// asTable narrows a decoded value to a TOML table.
func asTable(value any) (map[string]any, bool) {
	table, ok := value.(map[string]any)
	return table, ok
}

// pythonTruthy reports whether value is truthy by Python's rules, which is
// what every `if not item.get(field)` required-key check in the ported
// refusals tested. An absent key, an empty string, a zero number, false, and
// an empty container are each falsy.
func pythonTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case []map[string]any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// containsString reports whether items holds needle.
func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

// hasAnySuffix reports whether name ends with one of suffixes -- Python's
// str.endswith over a tuple.
func hasAnySuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// sortedStrings returns a sorted copy of values with duplicates kept, for the
// call sites that render a list into a message.
func sortedStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	return out
}

// sortedUnique returns the distinct members of values, sorted -- Python's
// sorted({...}).
func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
