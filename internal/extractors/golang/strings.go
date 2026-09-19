package golang

import (
	"github.com/stricttools/selfdoc/internal/util"
)

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string { return util.PythonStrip(s) }

// pyRStrip is Python's str.rstrip() with no argument.
func pyRStrip(s string) string { return util.PythonRStrip(s) }

// splitWhitespaceOnce is Python's str.split(None, 1): leading whitespace is
// skipped, the split happens at the first run of whitespace, and the remainder
// keeps whatever trailing whitespace it had.
func splitWhitespaceOnce(s string) []string {
	rest := util.PythonLStrip(s)
	if rest == "" {
		return nil
	}
	for i, r := range rest {
		if util.IsPythonSpace(r) {
			tail := util.PythonLStrip(rest[i:])
			if tail == "" {
				return []string{rest[:i]}
			}
			return []string{rest[:i], tail}
		}
	}
	return []string{rest}
}
