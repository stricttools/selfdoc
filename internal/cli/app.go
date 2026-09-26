// Package cli registers selfdoc's whole command tree on one strictcli
// application.
//
// One binary carries what used to be two Python CLIs, so every cross-CLI
// refusal they made is gone: `build --target unified` builds a unified site
// here rather than naming another package, and `check` answers for every
// project kind. What each command does, refuses and prints is otherwise the
// Python's, verbatim.
//
// Nothing here changes the process's working directory. The directory a
// command operates on is Options.Dir, which the editor's publish path sets
// explicitly so an in-process invocation can act on a repository that is not
// the one the editor was started in.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/smm-h/strictcli/go/strictcli"

	// The language packages register their extractors from init, which is
	// what links a language into the binary. Every command that resolves a
	// code directive needs all nine, so the blank imports live beside the
	// command tree rather than in one entry point that a test never builds.
	_ "github.com/stricttools/selfdoc/internal/extractors/dart"
	_ "github.com/stricttools/selfdoc/internal/extractors/golang"
	_ "github.com/stricttools/selfdoc/internal/extractors/kotlin"
	_ "github.com/stricttools/selfdoc/internal/extractors/python"
	_ "github.com/stricttools/selfdoc/internal/extractors/sql"
	_ "github.com/stricttools/selfdoc/internal/extractors/svelte"
	_ "github.com/stricttools/selfdoc/internal/extractors/swift"
	_ "github.com/stricttools/selfdoc/internal/extractors/typescript"
	_ "github.com/stricttools/selfdoc/internal/extractors/zig"
)

// AppHelp is the one-line description `selfdoc --help` prints.
const AppHelp = "Code-aware static site generator with directive-based content extraction"

// Options is everything one application is built from.
//
// Only Version comes from the binary itself; every other member has a working
// zero value. Those members exist for the two callers that are not the
// binary: the editor's publish path, which runs a command in-process against
// a stated directory and collects its output, and the suite, which stubs the
// two registries a toolchain pin is checked against.
type Options struct {
	// Dir is the project directory every command operates on. Empty means
	// the process's own working directory, spelled ".".
	Dir string
	// Stdout and Stderr are where a command's human output goes. Nil means
	// the process's own streams, read at write time so strictcli's Test can
	// capture them.
	Stdout io.Writer
	Stderr io.Writer
	// Registry is how a toolchain pin is checked for publication. The zero
	// value reads the real registries.
	Registry assembly.Registry
	// Version is the binary's own release version: what `selfdoc --version`
	// prints, and the selfdoc pin a generated deploy workflow installs when
	// no version is named explicitly. The binary reads it from the
	// repository's VERSION file, embedded at compile time, and hands it in
	// here -- nothing under internal/ reads that file. Empty states no
	// version, and the paths that need a real one refuse rather than
	// inventing one.
	Version string
}

// cli is the application under construction: its options, and the strictcli
// app the handlers are registered on.
type cli struct {
	opts Options
	app  *strictcli.App
}

func (c *cli) dir() string {
	if c.opts.Dir == "" {
		return "."
	}
	return c.opts.Dir
}

// out and errOut resolve the destination at WRITE time rather than at
// construction. strictcli's Test swaps os.Stdout for a pipe around one
// dispatch, and a writer captured at construction would miss the swap.
func (c *cli) out() io.Writer {
	if c.opts.Stdout != nil {
		return c.opts.Stdout
	}
	return os.Stdout
}

func (c *cli) errOut() io.Writer {
	if c.opts.Stderr != nil {
		return c.opts.Stderr
	}
	return os.Stderr
}

// color reports whether the human report may carry ANSI escapes: the
// destination is the process's own stdout and it is a terminal. The Python
// decided this once at import time from sys.stdout, which made the report
// untestable and wrong for any writer that was not that stream.
func (c *cli) color() bool {
	if c.opts.Stdout != nil {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (c *cli) printf(format string, args ...any) {
	fmt.Fprintf(c.out(), format, args...)
}

func (c *cli) println(args ...any) {
	fmt.Fprintln(c.out(), args...)
}

func (c *cli) eprintf(format string, args ...any) {
	fmt.Fprintf(c.errOut(), format, args...)
}

// fail prints err as a refusal and exits 1, which is what the Python's _fail
// did at every user-error site in both CLIs.
func (c *cli) fail(err error) strictcli.Outcome {
	c.eprintf("Error: %s\n", err)
	return strictcli.Exit(1)
}

// failf prints an already-worded refusal to stderr and exits 1, for the sites
// that word their own message rather than forwarding an error's.
func (c *cli) failf(format string, args ...any) strictcli.Outcome {
	c.eprintf(format+"\n", args...)
	return strictcli.Exit(1)
}

// loadConfig reads the project config. A present-but-unusable selfdoc.json is
// a user error at every command, so the refusal is worded once here.
func (c *cli) loadConfig() (config.Config, strictcli.Outcome, bool) {
	cfg, err := config.Load(c.dir())
	if err != nil {
		return nil, c.fail(err), false
	}
	return cfg, strictcli.Exit(0), true
}

// requireConfig is loadConfig plus the refusal every command that cannot work
// without a project makes when selfdoc.json is absent.
func (c *cli) requireConfig() (config.Config, strictcli.Outcome, bool) {
	cfg, outcome, ok := c.loadConfig()
	if !ok {
		return nil, outcome, false
	}
	if cfg == nil {
		return nil, c.failf("Error: No selfdoc.json found. Run 'selfdoc init' first."), false
	}
	return cfg, strictcli.Exit(0), true
}

// absentMeans resolves an optional flag's absence to the fallback its help
// declares.
//
// strictcli's mutating-default ban forbids Default() on any flag of a mutating
// command: a value the framework picks is a value the framework writes. The
// opt-out booleans (--auto-commit, --drafts) and the convenience scalars
// (--port, --assembly-dir, --branch, --attempts, --target) therefore declare
// Optional() and name their fallback in their own help text. This is the one
// place absence becomes that fallback, so no downstream branch ever sees a
// zero value it would read as a decision.
func absentMeans[T any](kwargs map[string]any, name string, fallback T) T {
	if v, ok := strictcli.GetOpt[T](kwargs, name); ok {
		return v
	}
	return fallback
}

// optString is the empty-string spelling of an absent optional string flag,
// which is how every engine function here spells "no override".
func optString(kwargs map[string]any, name string) string {
	return absentMeans(kwargs, name, "")
}

// stringList reads a repeatable string flag's accumulated values.
func stringList(kwargs map[string]any, name string) []string {
	raw, ok := kwargs[name]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// configString reads a nested string out of a loaded config, empty when any
// level is absent or is not what it has to be.
func configString(cfg config.Config, path ...string) string {
	var current any = map[string]any(cfg)
	for _, key := range path {
		table, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = table[key]
		if !ok {
			return ""
		}
	}
	s, _ := current.(string)
	return s
}

// configTable reads a nested table out of a loaded config, nil when absent.
func configTable(cfg config.Config, key string) map[string]any {
	if cfg == nil {
		return nil
	}
	table, _ := cfg[key].(map[string]any)
	return table
}

// ignoreCodesFrom merges the flag's validated suppression set with the
// project's own lint_ignore, which was validated at config load.
func ignoreCodesFrom(flagCodes map[string]struct{}, cfg config.Config) map[string]struct{} {
	codes := map[string]struct{}{}
	for code := range flagCodes {
		codes[code] = struct{}{}
	}
	if cfg != nil {
		if declared, ok := cfg["lint_ignore"].([]any); ok {
			for _, entry := range declared {
				if s, ok := entry.(string); ok {
					codes[s] = struct{}{}
				}
			}
		}
	}
	return codes
}

// New builds the selfdoc application.
//
// Installing the lint-code validator is part of building the binary rather
// than optional: internal/config keeps the check as a seam so it stays
// loadable without the lint registry, and while the seam is nil a config's
// lint_ignore list is accepted as written.
func New(opts Options) *strictcli.App {
	config.LintCodeValidator = lints.ValidateLintCodes

	c := &cli{opts: opts}
	c.app = strictcli.NewApp("selfdoc", opts.Version, AppHelp)

	c.registerInit()
	c.registerBuild()
	c.registerServe()
	c.registerDeploy()
	c.registerCheck()
	c.registerBaseline()
	c.registerGen()
	c.registerLayout()
	c.registerVocabulary()
	c.registerGenData()
	c.registerSpellCorpus()
	c.registerQuality()
	c.registerAssembly()
	c.registerBlog()

	return c.app
}
