// Command fakeghcmd is the executable the assembly suite installs as "gh" at
// the front of PATH. The suite builds it once per test binary; every gh call a
// test makes then starts this small uninstrumented program instead of the
// suite's own race-instrumented binary.
package main

import (
	"fmt"
	"os"

	"github.com/stricttools/selfdoc/internal/blog/assembly/fakegh"
)

func main() {
	dir := os.Getenv(fakegh.StateEnv)
	if dir == "" {
		fmt.Fprintf(os.Stderr, "fake gh: %s is unset\n", fakegh.StateEnv)
		os.Exit(93)
	}
	os.Exit(fakegh.Run(dir, os.Args[1:]))
}
