package check

import (
	"fmt"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors/python"
)

// pythonSyntaxVerdict is what parsing a fenced Python block concluded.
type pythonSyntaxVerdict struct {
	// Status is "ok" or "syntax".
	Status string
	// Message is what EXAMPLE001 reports after its own prefix, set only for
	// the "syntax" status.
	Message string
	// Line is the 1-based line within the snippet the error sits on.
	Line int
}

// checkPythonSyntax parses source with the Python grammar.
//
// The message is always "invalid syntax". EXAMPLE001 used to report the
// interpreter's own SyntaxError text, which named the cause ("'(' was never
// closed"); no parser but CPython's produces those strings, and a documentation
// check that needs an interpreter installed to report anything is worse than
// one that names the line without naming the cause.
//
// An indentation problem is not reported at all, which is what the rule always
// intended: a snippet lifted out of a function is indented, and its indentation
// is not a defect. That used to be an IndentationError caught by name; the
// grammar simply admits every such fragment, so the exemption is structural.
//
// The handle is unused -- the parse runs in-process and spawns nothing -- and
// stays in the signature because every lint rule is handed one.
func checkPythonSyntax(source string, _ *effects.Handle) (pythonSyntaxVerdict, error) {
	line, failed, err := python.FirstSyntaxErrorLine([]byte(source))
	if err != nil {
		return pythonSyntaxVerdict{}, fmt.Errorf("checking a Python example: %w", err)
	}
	if !failed {
		return pythonSyntaxVerdict{Status: "ok"}, nil
	}
	return pythonSyntaxVerdict{Status: "syntax", Message: "invalid syntax", Line: line}, nil
}
