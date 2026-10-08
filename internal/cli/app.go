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
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/strictcli/go/strictcli"

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
	// Stdout and Stderr collect a command's human output, for the editor's
	// publish path, which calls the command through strictcli's App.Call (a
	// call whose context discards what it writes). Nil, as the binary and
	// the suite leave them, sends the output through the dispatch's strictcli
	// context: the answer through ctx.Out, a refusal through ctx.Error.
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
// app the handlers are registered on. Each dispatch runs on a copy of it made
// by bind, which carries that dispatch's output writers.
type cli struct {
	opts Options
	app  *strictcli.App

	// stdout, stderr, and warnings are the bound dispatch's writers; nil on
	// the copy commands are registered from.
	stdout   io.Writer
	stderr   io.Writer
	warnings io.Writer
	flushers []*effects.LineWriter
}

// handler adapts a command's handler to run on a copy of c bound to the
// dispatch's context, so everything the command prints goes through it.
func (c *cli) handler(
	h func(*cli, *strictcli.Context, map[string]any) strictcli.Outcome,
) func(*strictcli.Context, map[string]any) strictcli.Outcome {
	return func(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
		d := c.bind(ctx)
		defer d.flush()
		return h(d, ctx, kwargs)
	}
}

// bind returns a copy of c whose output goes to ctx: the answer through
// ctx.Out, refusals through ctx.Error, and warnings through ctx.Warn, each a
// line at a time. Options.Stdout and Options.Stderr, when set, collect the
// output instead.
func (c *cli) bind(ctx *strictcli.Context) *cli {
	d := *c
	d.flushers = nil
	if c.opts.Stdout != nil {
		d.stdout = c.opts.Stdout
	} else {
		d.stdout = d.lines(ctx.Out)
	}
	if c.opts.Stderr != nil {
		d.stderr, d.warnings = c.opts.Stderr, c.opts.Stderr
	} else {
		// A blank line carries nothing as an error or a warning.
		d.stderr = d.lines(nonBlank(ctx.Error))
		d.warnings = d.lines(nonBlank(ctx.Warn))
	}
	return &d
}

func (c *cli) lines(emit func(string)) io.Writer {
	w := effects.NewLineWriter(emit)
	c.flushers = append(c.flushers, w)
	return w
}

func nonBlank(emit func(string)) func(string) {
	return func(line string) {
		if strings.TrimSpace(line) != "" {
			emit(line)
		}
	}
}

// flush emits the partial lines the dispatch's writers still hold.
func (c *cli) flush() {
	for _, w := range c.flushers {
		w.Flush()
	}
}

func (c *cli) dir() string {
	if c.opts.Dir == "" {
		return "."
	}
	return c.opts.Dir
}

// out is the dispatch's answer writer, errOut its refusal writer, and
// warnOut its warning writer. Each panics outside a dispatch, where there is
// no one to write to.
func (c *cli) out() io.Writer {
	if c.stdout == nil {
		panic("selfdoc: command output written outside a dispatch")
	}
	return c.stdout
}

func (c *cli) errOut() io.Writer {
	if c.stderr == nil {
		panic("selfdoc: command output written outside a dispatch")
	}
	return c.stderr
}

func (c *cli) warnOut() io.Writer {
	if c.warnings == nil {
		panic("selfdoc: command output written outside a dispatch")
	}
	return c.warnings
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

func (c *cli) warnf(format string, args ...any) {
	fmt.Fprintf(c.warnOut(), format+"\n", args...)
}

// fail prints err as a refusal and exits 1, which is what the Python's _fail
// did at every user-error site in both CLIs.
func (c *cli) fail(err error) strictcli.Outcome {
	c.eprintf("%s\n", err)
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
		return nil, c.failf("No selfdoc.json found. Run 'selfdoc init' first."), false
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

// New builds the selfdoc application.
func New(opts Options) *strictcli.App {
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
	c.registerOptions()
	c.registerGenData()
	c.registerSpellCorpus()
	c.registerQuality()
	c.registerAssembly()
	c.registerBlog()

	c.refreshIndexOnMutatingCommands()

	return c.app
}
