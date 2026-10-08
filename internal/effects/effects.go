// Package effects is the single authorized surface for effectful calls in
// selfdoc production code.
//
// Every subprocess launch and every filesystem mutation made by the engine
// packages goes through a [Handle]. Nothing else may call os/exec, os.WriteFile,
// os.Rename, os.MkdirAll, os.RemoveAll or their siblings directly.
//
// Why a chokepoint: selfdoc rides strictcli's effects regime, where every
// mutation is declared, previewable under --dry-run, and recorded into the
// would-do log. selfdoc force-pushes a gh-pages branch, deploys to Cloudflare
// Pages, creates GitHub repositories, sets repository secrets and auto-commits
// to the user's working tree -- a --dry-run that executed any of that would be
// worse than no dry run at all. With every effect funnelled through this one
// package the regime is adapted in one file instead of at ~150 call sites.
//
// # No ambient binding
//
// There is no package-level handle and no contextvar equivalent. A command
// handler builds its handle with [FromContext] and passes it explicitly to
// every engine function that mutates or spawns; a library caller -- the build
// pipeline, the check helpers, the test suite -- builds an [Unbound] handle,
// which executes everything directly.
//
// # The mode rule (declared, never inferred)
//
//   - Preview mode -- a handle built from a context whose --dry-run flag was
//     passed. Every mutating operation is minted on the strictcli effects
//     handle: recorded, never executed. A [Result] then reports
//     Unsettled true, and the filesystem operations report no error while
//     having changed nothing.
//   - Live mode -- an unbound handle, or a bound handle outside --dry-run. The
//     operations execute directly, with their full selfdoc semantics:
//     per-call timeouts, byte captures, stdin payload streaming,
//     [Handle.AtomicWrite]'s temp-file-plus-rename (the only way to rewrite the
//     0444 generated root files), and the missing-is-an-error distinctions call
//     sites branch on. strictcli's closed method set expresses none of those,
//     so routing a live run through it would silently drop a hang guard or a
//     permission-preserving rename.
//
// The split is by mode, decided before anything runs, and identical on every
// invocation -- it is not a fallback: nothing here ever tries the strictcli
// handle, fails, and retries elsewhere.
//
// # Reads are never effects
//
// [Read] marks a subprocess run as a declared read. A declared read executes
// in every mode and is never minted, never recorded and never logged -- the
// same treatment strictcli gives an allowlisted observe, and for the same
// reason: a preview that could not look at the world would have nothing to
// preview. It is declared per call site rather than through an app-level
// proc_observe_allowlist because the argv cannot classify these: `gh api` is
// both a GET of repository contents and a POST of a workflow dispatch, and
// `git` is both rev-parse and push --force.
package effects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stricttools/strictcli/go/strictcli"
)

// ModeDefault is the file mode operand meaning "no explicit mode": the file is
// created with 0644 as modified by the process umask, and no chmod is recorded
// in a preview. It is the counterpart of the Python surface's permissions=None.
const ModeDefault fs.FileMode = 0

// dirMode is the mode directories are created with. selfdoc never varies it,
// so it is not an operand anywhere on this surface.
const dirMode fs.FileMode = 0o755

// writeMode is the mode a whole-content write creates a new file with when the
// caller passes [ModeDefault].
const writeMode fs.FileMode = 0o644

// ErrTimeout is the sentinel every timed-out subprocess error wraps.
var ErrTimeout = errors.New("timed out")

// ExitError is returned by [Handle.Run] when the caller declared [Check] and
// the child exited non-zero. It is the counterpart of Python's
// subprocess.CalledProcessError.
type ExitError struct {
	// Argv is the command that failed.
	Argv []string
	// ExitCode is the child's exit status.
	ExitCode int
	// Stdout and Stderr are the captured streams, nil when the run did not
	// capture them.
	Stdout []byte
	Stderr []byte
}

// Error renders the failed command and its exit status.
func (e *ExitError) Error() string {
	return fmt.Sprintf("command %q exited with status %d", strings.Join(e.Argv, " "), e.ExitCode)
}

// Result is the outcome of [Handle.Run] or [Handle.Pipeline].
type Result struct {
	// Argv is the command that was run, or -- for a pipeline -- the first
	// stage's command.
	Argv []string
	// ExitCode is the child's exit status. Meaningless when Unsettled.
	ExitCode int
	// Stdout and Stderr are the captured streams. Both are nil unless the
	// caller declared [CaptureOutput], and always nil when Unsettled.
	Stdout []byte
	Stderr []byte
	// Unsettled is true when the run was recorded rather than performed. The
	// exit code and the streams then stand for nothing: a caller that needs
	// them must check this field and decline, which is the honest outcome when
	// nothing ran.
	Unsettled bool
}

// StdoutString is Stdout decoded as text with one trailing newline removed --
// the form call sites forward into a later command's argv.
func (r Result) StdoutString() string {
	return strings.TrimSuffix(string(r.Stdout), "\n")
}

// StderrString is Stderr decoded as text with one trailing newline removed.
func (r Result) StderrString() string {
	return strings.TrimSuffix(string(r.Stderr), "\n")
}

// Handle is the effects handle an engine function receives as an explicit
// parameter. Build one with [FromContext] inside a command handler, or with
// [Unbound] for a library call.
//
// A Handle is immutable after construction and safe to share across
// goroutines to the extent the underlying operations are.
type Handle struct {
	fx     *strictcli.Effects
	dryRun bool
	// ctx is the dispatch whose output streamed children and engine progress
	// go to; nil on an unbound handle.
	ctx *strictcli.Context
}

// FromContext builds the handle for a command dispatch.
//
// The returned handle previews when ctx was invoked with --dry-run and
// executes directly otherwise. A nil ctx yields an [Unbound] handle, so a test
// that dispatches nothing still gets a working handle.
func FromContext(ctx *strictcli.Context) *Handle {
	if ctx == nil {
		return Unbound()
	}
	return &Handle{fx: ctx.Effects(), dryRun: ctx.DryRun(), ctx: ctx}
}

// Live returns a handle that executes everything directly, as an unbound
// handle does, while its output still goes to h's dispatch. A dry run uses it
// for work that runs for real because it writes only output the run owns.
func (h *Handle) Live() *Handle {
	if h == nil {
		return Unbound()
	}
	return &Handle{ctx: h.ctx}
}

// Unbound builds a handle with no strictcli effects handle behind it: every
// operation executes directly. This is the library path -- the build pipeline,
// the check helpers and unit tests all call the engine outside a command
// dispatch, and there is nothing to mint on there.
func Unbound() *Handle {
	return &Handle{}
}

// Previewing reports whether this handle records mutations instead of
// performing them.
func (h *Handle) Previewing() bool {
	return h != nil && h.fx != nil && h.dryRun
}

// mint returns the strictcli handle to record on, or nil to execute directly.
func (h *Handle) mint() *strictcli.Effects {
	if h == nil || !h.dryRun {
		return nil
	}
	return h.fx
}

// --- options ---------------------------------------------------------------

// Option is one trailing option on a [Handle.Run] or [Handle.Pipeline] call.
// An option a method does not accept is a call-time error: silently ignoring
// one is the single outcome a declare-everything surface cannot have.
type Option struct {
	name  string
	apply func(*opts)
}

// opts is the resolved option state of one call.
type opts struct {
	cwd           string
	env           map[string]string
	hasEnv        bool
	timeout       time.Duration
	check         bool
	capture       bool
	stdin         []byte
	read          bool
	resource      string
	hasResource   bool
	skipIfCurrent string
	hasSkip       bool
	grant         string
	hasGrant      bool
	stream        bool
	hasStream     bool
}

// Cwd sets the working directory of the child process.
func Cwd(dir string) Option {
	return Option{name: "cwd", apply: func(o *opts) { o.cwd = dir }}
}

// Env sets the complete environment of the child process, replacing the
// inherited one -- the semantics of Python's subprocess env argument, which
// every selfdoc call site was written against. Omit it to inherit.
//
// A preview renders the entries as overrides instead, because strictcli's
// recorded env merges over the inherited environment; nothing executes there,
// so the difference is confined to the rendered line.
func Env(env map[string]string) Option {
	return Option{name: "env", apply: func(o *opts) { o.env, o.hasEnv = env, true }}
}

// Timeout kills the child and returns an error wrapping [ErrTimeout] when it
// has not exited within d. Zero means no deadline.
func Timeout(d time.Duration) Option {
	return Option{name: "timeout", apply: func(o *opts) { o.timeout = d }}
}

// Check makes a non-zero exit an [ExitError] instead of a [Result]. Off by
// default, matching the Python surface.
func Check() Option {
	return Option{name: "check", apply: func(o *opts) { o.check = true }}
}

// CaptureOutput captures the child's stdout and stderr into the [Result]
// instead of letting it inherit this process's streams.
func CaptureOutput() Option {
	return Option{name: "capture_output", apply: func(o *opts) { o.capture = true }}
}

// Stdin writes payload to the child's standard input and closes it.
func Stdin(payload []byte) Option {
	return Option{name: "input", apply: func(o *opts) { o.stdin = payload }}
}

// Read declares this run an observation: it changes nothing, so it executes in
// every mode and is never recorded.
func Read() Option {
	return Option{name: "read", apply: func(o *opts) { o.read = true }}
}

// Resource declares an opaque token naming what this run produces. Preview
// metadata only.
func Resource(token string) Option {
	return Option{name: "resource", apply: func(o *opts) { o.resource, o.hasResource = token, true }}
}

// SkipIfCurrent declares the token a preview annotates the recorded line with,
// spelling out that the handler skips this step when the resource is current.
func SkipIfCurrent(token string) Option {
	return Option{name: "skip_if_current", apply: func(o *opts) { o.skipIfCurrent, o.hasSkip = token, true }}
}

// Grant names a grant declared on the running command, whose reason is
// rendered beside the recorded step in the preview.
func Grant(name string) Option {
	return Option{name: "grant", apply: func(o *opts) { o.grant, o.hasGrant = name, true }}
}

// Stream overrides whether a recorded run is rendered as inheriting this
// process's streams. Without it a recorded run streams unless the caller
// declared [CaptureOutput].
func Stream(stream bool) Option {
	return Option{name: "stream", apply: func(o *opts) { o.stream, o.hasStream = stream, true }}
}

var (
	acceptedRun      = optionSet("cwd", "env", "timeout", "check", "capture_output", "input", "read", "resource", "skip_if_current", "grant", "stream")
	acceptedPipeline = optionSet("cwd", "timeout", "read", "resource", "skip_if_current", "grant")
)

func optionSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func resolve(method string, in []Option, accepted map[string]bool) (opts, error) {
	var o opts
	for _, opt := range in {
		if opt.apply == nil {
			return o, fmt.Errorf("effects: %s received a zero-valued Option", method)
		}
		if !accepted[opt.name] {
			return o, fmt.Errorf("effects: %s does not accept the %q option", method, opt.name)
		}
		opt.apply(&o)
	}
	return o, nil
}

// mintOptions renders the option state as the strictcli options a recorded run
// carries.
func (o opts) mintOptions(withEnv bool) []strictcli.EffectOption {
	out := []strictcli.EffectOption{strictcli.Check(false)}
	if o.cwd != "" {
		out = append(out, strictcli.Cwd(o.cwd))
	}
	if withEnv && o.hasEnv {
		out = append(out, strictcli.EffectEnv(o.env))
	}
	stream := !o.capture
	if o.hasStream {
		stream = o.stream
	}
	out = append(out, strictcli.Stream(stream))
	if o.hasResource {
		out = append(out, strictcli.Resource(o.resource))
	}
	if o.hasSkip {
		out = append(out, strictcli.SkipIfCurrent(o.skipIfCurrent))
	}
	if o.hasGrant {
		out = append(out, strictcli.UseGrant(o.grant))
	}
	return out
}

// --- process effects -------------------------------------------------------

// Run runs a command to completion and returns its [Result].
//
// In preview mode a declared read ([Read]) still executes and returns a real
// result; anything else is recorded and returns a result whose Unsettled field
// is true, standing in for the run that did not happen.
func (h *Handle) Run(argv []string, options ...Option) (Result, error) {
	o, err := resolve("Run", options, acceptedRun)
	if err != nil {
		return Result{}, err
	}
	if len(argv) == 0 {
		return Result{}, errors.New("effects: Run requires a non-empty argv")
	}
	fx := h.mint()
	if fx == nil || o.read {
		return directRun(h, argv, o)
	}
	completed, err := fx.Run(toOperands(argv), o.mintOptions(true)...)
	if err != nil {
		return Result{}, err
	}
	return fromCompleted(completed, argv, o)
}

// Pipeline runs the stages of argvs as one shell pipeline -- stage N's stdout
// feeds stage N+1's stdin -- and returns the last stage's result.
//
// strictcli's closed method set has no pipeline, so a preview records the
// whole chain as the one /bin/sh -c invocation that performs it: a faithful
// rendering of the work, not an invented one. Live mode keeps the real
// multi-process pipeline, which is what streams a `git archive` into `tar`
// without buffering the whole tree in memory.
//
// A declared read ([Read]) executes in every mode, as it does on [Handle.Run]:
// a pipeline whose only write lands in a directory its caller owns and removes
// changes nothing anyone else can observe.
//
// The last stage's stdout and every stage's stderr are captured into the
// result.
func (h *Handle) Pipeline(argvs [][]string, options ...Option) (Result, error) {
	o, err := resolve("Pipeline", options, acceptedPipeline)
	if err != nil {
		return Result{}, err
	}
	if len(argvs) < 2 {
		return Result{}, errors.New("effects: Pipeline requires at least two stages")
	}
	for i, argv := range argvs {
		if len(argv) == 0 {
			return Result{}, fmt.Errorf("effects: Pipeline stage %d has an empty argv", i)
		}
	}
	if fx := h.mint(); fx != nil && !o.read {
		stages := make([]string, 0, len(argvs))
		for _, argv := range argvs {
			parts := make([]string, 0, len(argv))
			for _, token := range argv {
				parts = append(parts, ShellQuote(token))
			}
			stages = append(stages, strings.Join(parts, " "))
		}
		shellForm := strings.Join(stages, " | ")
		shell := []string{"/bin/sh", "-c", shellForm}
		mintOpts := o.mintOptions(false)
		completed, err := fx.Run(toOperands(shell), mintOpts...)
		if err != nil {
			return Result{}, err
		}
		return fromCompleted(completed, shell, o)
	}
	return directPipeline(argvs, o)
}

// ShellQuote quotes token for a rendered /bin/sh -c line, leaving a token made
// only of characters no shell reinterprets unquoted.
func ShellQuote(token string) string {
	if token != "" && !strings.ContainsFunc(token, needsQuoting) {
		return token
	}
	return "'" + strings.ReplaceAll(token, "'", `'\''`) + "'"
}

// needsQuoting reports whether r forces [ShellQuote] to quote its token. The
// safe set mirrors the Python implementation's character class.
func needsQuoting(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	case strings.ContainsRune("-_./=:@^{}", r):
		return false
	default:
		return true
	}
}

func toOperands(argv []string) []interface{} {
	out := make([]interface{}, 0, len(argv))
	for _, token := range argv {
		out = append(out, token)
	}
	return out
}

// fromCompleted adapts a strictcli Completed to a [Result]. An unsettled
// carrier -- the recorded case -- becomes a result whose Unsettled field is
// true; nothing ran, so there is no exit code to test and no Check to apply.
func fromCompleted(c strictcli.Completed, argv []string, o opts) (Result, error) {
	exit, out, errText, settled := extract(c)
	if !settled {
		return Result{Argv: argv, Unsettled: true}, nil
	}
	res := Result{Argv: argv, ExitCode: exit}
	if o.capture {
		res.Stdout, res.Stderr = []byte(out), []byte(errText)
	}
	if o.check && exit != 0 {
		return res, &ExitError{Argv: argv, ExitCode: exit, Stdout: res.Stdout, Stderr: res.Stderr}
	}
	return res, nil
}

// extract reads a strictcli Completed's payload. Its extractors panic with the
// framework's truncation value when the carrier is unsettled, so the recover
// here is how an unsettled carrier is recognized without the framework
// exporting a predicate.
func extract(c strictcli.Completed) (exit int, stdout, stderr string, settled bool) {
	defer func() {
		if recover() != nil {
			exit, stdout, stderr, settled = 0, "", "", false
		}
	}()
	return c.ExitCode(), c.Stdout(), c.Stderr(), true
}

// directRun executes argv with the full subprocess semantics selfdoc relies
// on: a deadline, an optional stdin payload, and byte captures. A run that
// does not capture streams its output to h's dispatch, or, on a handle with no
// dispatch behind it, is captured all the same.
func directRun(h *Handle, argv []string, o opts) (Result, error) {
	cmd, cancel, deadline := command(argv, o)
	defer cancel()

	var outBuf, errBuf bytes.Buffer
	capture := o.capture
	if !capture {
		if stdout, stderr, ok := h.streams(); ok {
			cmd.Stdout, cmd.Stderr = stdout, stderr
			defer stdout.Flush()
			defer stderr.Flush()
		} else {
			capture = true
		}
	}
	if capture {
		cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	}
	if o.stdin != nil {
		cmd.Stdin = bytes.NewReader(o.stdin)
	}

	runErr := cmd.Run()
	if err := timeoutError(argv, o, deadline, runErr); err != nil {
		return Result{}, err
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return Result{}, runErr
	}
	res := Result{Argv: argv, ExitCode: cmd.ProcessState.ExitCode()}
	if capture {
		res.Stdout, res.Stderr = outBuf.Bytes(), errBuf.Bytes()
	}
	if o.check && res.ExitCode != 0 {
		return res, &ExitError{Argv: argv, ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}
	}
	return res, nil
}

// directPipeline executes the real multi-process pipeline. The last stage's
// stdout and every stage's stderr are captured into the [Result].
func directPipeline(argvs [][]string, o opts) (Result, error) {
	last := len(argvs) - 1
	cmds := make([]*exec.Cmd, len(argvs))
	cancels := make([]func(), 0, len(argvs))
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	// Each stage writes its own stderr buffer: the stages run at once, and a
	// bytes.Buffer is not safe for concurrent writes.
	errBufs := make([]bytes.Buffer, len(argvs))
	var deadline time.Time
	for i, argv := range argvs {
		cmd, cancel, d := command(argv, o)
		cancels = append(cancels, cancel)
		deadline = d
		cmd.Stderr = &errBufs[i]
		cmds[i] = cmd
	}
	for i := 0; i < last; i++ {
		pipe, err := cmds[i].StdoutPipe()
		if err != nil {
			return Result{}, err
		}
		cmds[i+1].Stdin = pipe
	}
	var outBuf bytes.Buffer
	cmds[last].Stdout = &outBuf

	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			return Result{}, err
		}
	}
	// Wait on the last stage first: the earlier stages see their reader go
	// away and exit, which is what keeps a pipeline from deadlocking on a
	// consumer that stops reading.
	waitErr := cmds[last].Wait()
	for i := 0; i < last; i++ {
		_ = cmds[i].Wait()
	}
	if err := timeoutError(argvs[last], o, deadline, waitErr); err != nil {
		return Result{}, err
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return Result{}, waitErr
	}
	return Result{
		Argv:     argvs[0],
		ExitCode: cmds[last].ProcessState.ExitCode(),
		Stdout:   outBuf.Bytes(),
		Stderr:   bytes.Join(stderrs(errBufs), nil),
	}, nil
}

// stderrs is each stage's captured stderr, in stage order.
func stderrs(bufs []bytes.Buffer) [][]byte {
	out := make([][]byte, len(bufs))
	for i := range bufs {
		out[i] = bufs[i].Bytes()
	}
	return out
}

// command builds the exec.Cmd for one stage, with the deadline the timeout
// option asks for. The returned time is zero when no deadline was set.
func command(argv []string, o opts) (*exec.Cmd, func(), time.Time) {
	if o.timeout <= 0 {
		cmd := exec.Command(argv[0], argv[1:]...)
		applyEnv(cmd, o)
		return cmd, func() {}, time.Time{}
	}
	deadline := time.Now().Add(o.timeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	applyEnv(cmd, o)
	return cmd, cancel, deadline
}

func applyEnv(cmd *exec.Cmd, o opts) {
	cmd.Dir = o.cwd
	if !o.hasEnv {
		return
	}
	// The Python surface's env is the child's complete environment, not an
	// overlay: every call site was written expecting a replacement.
	entries := make([]string, 0, len(o.env))
	for k, v := range o.env {
		entries = append(entries, k+"="+v)
	}
	cmd.Env = entries
}

// timeoutError turns a deadline overrun into an error wrapping [ErrTimeout].
// exec reports a killed child as a plain exit error, so the deadline is
// consulted rather than the error's text.
func timeoutError(argv []string, o opts, deadline time.Time, runErr error) error {
	if runErr == nil || deadline.IsZero() || time.Now().Before(deadline) {
		return nil
	}
	return fmt.Errorf("%w after %s: %s", ErrTimeout, o.timeout, strings.Join(argv, " "))
}

// --- filesystem effects ----------------------------------------------------

// Write writes content to path, truncating any existing file.
//
// Pass [ModeDefault] as mode to leave the mode to the process umask; any other
// value is applied to the file, and recorded as a chmod in a preview.
func (h *Handle) Write(path string, content []byte, mode fs.FileMode) error {
	if fx := h.mint(); fx != nil {
		if _, err := fx.Write(path, content); err != nil {
			return err
		}
		return mintChmod(fx, path, mode)
	}
	create := writeMode
	if mode != ModeDefault {
		create = mode
	}
	if err := os.WriteFile(path, content, create); err != nil {
		return err
	}
	if mode == ModeDefault {
		return nil
	}
	// WriteFile's mode applies only to a file it creates, so an existing
	// target keeps its old mode without this.
	return os.Chmod(path, mode)
}

// AtomicWrite writes content to path atomically: the bytes land in a sibling
// temporary file that is renamed over the target in one directory operation.
//
// A crash mid-write can therefore never leave a truncated file. Because the
// rename is a directory operation it also succeeds when path itself is
// read-only (the 0444 generated root files) with no unlock step -- which is
// why live mode keeps the temp-file dance instead of routing through a plain
// write.
//
// Pass [ModeDefault] as mode to leave the mode alone.
func (h *Handle) AtomicWrite(path string, content []byte, mode fs.FileMode) error {
	if fx := h.mint(); fx != nil {
		if _, err := fx.Write(path, content); err != nil {
			return err
		}
		return mintChmod(fx, path, mode)
	}
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	// ModeDefault leaves an existing file's mode as it was; a file this call
	// creates gets writeMode as modified by the umask, as Write gives it.
	if mode == ModeDefault {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	tmp, err := createSibling(dir)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := tmp.Write(content); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if mode != ModeDefault {
		if err := os.Chmod(tmpPath, mode); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// createSibling creates a new temporary file in dir with writeMode, which the
// kernel narrows by the umask. os.CreateTemp is not used because it always
// creates 0600, and the rename would carry that mode onto the target.
func createSibling(dir string) (*os.File, error) {
	for range 10000 {
		name := filepath.Join(dir, strconv.FormatUint(rand.Uint64(), 36)+".tmp")
		file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, writeMode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return file, err
	}
	return nil, fmt.Errorf("creating a temporary file in %s: every name tried already exists", dir)
}

// mintChmod records the chmod an explicit mode asks for.
func mintChmod(fx *strictcli.Effects, path string, mode fs.FileMode) error {
	if mode == ModeDefault {
		return nil
	}
	_, err := fx.Chmod(path, int(mode.Perm()))
	return err
}

// OpenWrite opens path for writing, truncating any existing file, and returns
// the stream. Use it for writers that produce their bytes incrementally;
// whole-content writers should prefer [Handle.Write] or [Handle.AtomicWrite].
//
// strictcli's closed method set has no streaming write, so in preview mode the
// content accumulates in memory and Close mints the single resulting write,
// carrying the byte count the file would have had. Close must therefore be
// called -- and its error checked -- on every path.
func (h *Handle) OpenWrite(path string) (io.WriteCloser, error) {
	if fx := h.mint(); fx != nil {
		return &recordedWriter{fx: fx, path: path}, nil
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, writeMode)
}

// OpenAppend opens path for appending, creating it when missing, and returns
// the stream.
//
// strictcli's closed method set has no append, so in preview mode the recorded
// write carries the whole resulting file -- the existing bytes plus the
// appended ones -- and the preview's byte count is the real one.
func (h *Handle) OpenAppend(path string) (io.WriteCloser, error) {
	if fx := h.mint(); fx != nil {
		return &recordedWriter{fx: fx, path: path, append: true}, nil
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, writeMode)
}

// recordedWriter is the preview-mode sink that mints one write when closed.
type recordedWriter struct {
	fx     *strictcli.Effects
	path   string
	append bool
	buf    bytes.Buffer
	closed bool
}

// Write accumulates p in memory.
func (w *recordedWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fs.ErrClosed
	}
	return w.buf.Write(p)
}

// Close mints the single write standing for everything that was streamed.
func (w *recordedWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	content := w.buf.Bytes()
	if w.append {
		if existing, err := os.ReadFile(w.path); err == nil {
			content = append(existing, content...)
		}
	}
	_, err := w.fx.Write(w.path, content)
	return err
}

// Mkdir creates path and any missing parents. An already-existing path is an
// error, matching Python's os.makedirs default.
func (h *Handle) Mkdir(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Mkdir(path)
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrExist}
	}
	return os.MkdirAll(path, dirMode)
}

// MkdirAll creates path and any missing parents. An already-existing path is
// not an error.
func (h *Handle) MkdirAll(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Mkdir(path)
		return err
	}
	return os.MkdirAll(path, dirMode)
}

// Remove deletes the file or symlink at path. A missing path is an error.
func (h *Handle) Remove(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Remove(path)
		return err
	}
	return os.Remove(path)
}

// RemoveIfExists deletes the file or symlink at path. A missing path is not an
// error.
func (h *Handle) RemoveIfExists(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Remove(path)
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Rmdir removes the empty directory at path.
func (h *Handle) Rmdir(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Remove(path)
		return err
	}
	return os.Remove(path)
}

// RmTree recursively deletes the directory tree at path. A missing path is an
// error, matching Python's shutil.rmtree default.
func (h *Handle) RmTree(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Remove(path)
		return err
	}
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// RmTreeIgnoreErrors recursively deletes the directory tree at path, ignoring
// filesystem errors -- including a missing path. The returned error is only
// ever a preview-mode recording failure.
func (h *Handle) RmTreeIgnoreErrors(path string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Remove(path)
		return err
	}
	_ = os.RemoveAll(path)
	return nil
}

// Chmod sets the permission bits of path.
func (h *Handle) Chmod(path string, mode fs.FileMode) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Chmod(path, int(mode.Perm()))
		return err
	}
	return os.Chmod(path, mode)
}

// Rename moves src to dst.
func (h *Handle) Rename(src, dst string) error {
	if fx := h.mint(); fx != nil {
		_, err := fx.Rename(src, dst)
		return err
	}
	return os.Rename(src, dst)
}

// CopyFile copies src to dst, preserving src's permission bits.
func (h *Handle) CopyFile(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if fx := h.mint(); fx != nil {
		// Reading the source is not an effect; writing the destination is the
		// one that gets recorded.
		_, err := fx.Write(dst, content)
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, content, info.Mode().Perm())
}

// CopyTree recursively copies the directory tree src to dst. With dirsExistOK
// false an already-existing dst is an error.
//
// A preview records one mkdir plus one write per file, so it names every path
// the copy would create rather than one opaque "copy tree" line.
func (h *Handle) CopyTree(src, dst string, dirsExistOK bool) error {
	if !dirsExistOK {
		if _, err := os.Lstat(dst); err == nil {
			return &fs.PathError{Op: "copytree", Path: dst, Err: fs.ErrExist}
		}
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}
		if entry.IsDir() {
			return h.MkdirAll(target)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			dest, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if fx := h.mint(); fx != nil {
				_, err := fx.Write(target, []byte(dest))
				return err
			}
			return os.Symlink(dest, target)
		}
		return h.CopyFile(path, target)
	})
}
