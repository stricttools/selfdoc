package python

import (
	"github.com/stricttools/selfdoc/internal/util"
)

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string { return util.PythonStrip(s) }

// pyRStrip is Python's str.rstrip() with no argument.
func pyRStrip(s string) string { return util.PythonRStrip(s) }
