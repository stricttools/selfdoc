// Command faketoolcmd is the executable the cli suite installs at the front of
// PATH under each external tool's name. The suite builds it once per test
// binary; every call a command makes then starts this small uninstrumented
// program instead of the suite's own race-instrumented binary.
package main

import (
	"fmt"
	"os"

	"github.com/stricttools/selfdoc/internal/cli/faketool"
)

func main() {
	dir := os.Getenv(faketool.StateEnv)
	if dir == "" {
		fmt.Fprintf(os.Stderr, "fake tool: %s is unset\n", faketool.StateEnv)
		os.Exit(93)
	}
	os.Exit(faketool.Run(dir, os.Args[0], os.Args[1:]))
}
