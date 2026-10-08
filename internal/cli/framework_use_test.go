package cli

import (
	"strings"
	"testing"
)

// TestNoOutputOutsideTheFramework runs strictcli's framework-use lint over the
// whole program and refuses every write to the process's stdout or stderr
// outside the framework (os.Stdout, os.Stderr, fmt.Print*, log.Print*, and
// the print builtins), in the command packages and every engine package they
// import. strictcli's test helper captures none of those writes, and its
// runtime guard fails a --json run on a byte written to stdout outside it, so
// a command's output goes through its context (ctx.Out, ctx.Error, ctx.Warn,
// ctx.Info) and a streamed child's output through the effects handle.
func TestNoOutputOutsideTheFramework(t *testing.T) {
	// The lint scans the module the working directory roots.
	t.Chdir("../..")
	result := New(Options{}).Test([]string{"--lint-framework-use"})
	if result.Stderr != "" {
		t.Fatalf("the lint refused to run: %s", result.Stderr)
	}
	var writes []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.Contains(line, ": stdout-write: ") || strings.Contains(line, ": stderr-write: ") {
			writes = append(writes, line)
		}
	}
	if len(writes) > 0 {
		t.Errorf("output written outside the framework:\n%s", strings.Join(writes, "\n"))
	}
}
