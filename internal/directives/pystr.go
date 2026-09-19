package directives

import "github.com/stricttools/selfdoc/internal/util"

// The three classes below are this package's short spellings of Python's `\s`,
// `\S` and `\w` for a str pattern, which the marker patterns are built from.
// Go's own three are ASCII-only, so using them would make a line separated by
// a no-break space parse as one token where Python split it in two.
const (
	pySpaceClass    = util.PythonSpaceClass
	pyNotSpaceClass = util.PythonNonSpaceClass
	pyWordClass     = util.PythonWordClass
)
