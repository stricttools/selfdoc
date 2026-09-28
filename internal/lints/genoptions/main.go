// Command genoptions writes selfdoc's options registry, internal/lints/options.toml,
// from the lint registry: one option per lint. Run it from the repository root
// after changing internal/lints/lints.toml:
//
//	go run ./internal/lints/genoptions
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/stricttools/selfdoc/internal/lints"
)

func main() {
	registry, err := lints.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	target := filepath.Join("internal", "lints", "options.toml")
	if err := os.WriteFile(target, []byte(lints.RenderOptionsRegistry(registry)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote", target)
}
