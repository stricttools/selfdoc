package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/cli/faketool"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/testisolation/go/hygiene"
)

// fakeToolBinary is the fake external tool this test binary built for itself:
// one plain, uninstrumented executable, built once by [TestMain] and installed
// under each tool's own name by the tests that need it.
var fakeToolBinary string

// fakeRepoGHBinary is the assembly suite's stateful fake gh, built once by
// TestMain.
var fakeRepoGHBinary string

// TestMain builds the fake external tool before any test runs.
//
// Every command that reaches GitHub or Cloudflare does it by running "gh" or
// "npx", so the seam a test replaces is the executable rather than a function:
// an executable at the front of PATH shadows the real tool, and the call the
// command makes is a real subprocess through the real effects handle.
//
// The fake is a program of its own, in the faketool package, compiled here
// without the race detector. It used to be this very binary re-running itself,
// which made every call a command made pay the race runtime's start-up cost.
func TestMain(m *testing.M) {
	if os.Getenv(runAsCLIEnv) != "" {
		New(Options{}).Run()
		return
	}
	dir, err := os.MkdirTemp("", "selfdoc-fake-tool-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating the fake tool's build directory: %v\n", err)
		os.Exit(1)
	}
	fakeToolBinary = filepath.Join(dir, "faketool")
	build := exec.Command("go", "build", "-o", fakeToolBinary,
		"github.com/stricttools/selfdoc/internal/cli/faketoolcmd")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building the fake tool: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	// The assembly suite's fake gh keeps a repository's state across calls,
	// which is what a run publishing several projects and reading back what it
	// wrote needs.
	fakeRepoGHBinary = filepath.Join(dir, "fakegh")
	build = exec.Command("go", "build", "-o", fakeRepoGHBinary,
		"github.com/stricttools/selfdoc/internal/blog/assembly/fakeghcmd")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building the fake gh: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runAsCLIEnv turns this test binary into the real command-line application,
// which is how the two properties that only exist outside App.Test are
// asserted: the confirmation a consequential command demands at a terminal
// (App.Test behaves as if consent were given and never prompts), and the
// schema --dump-schema writes to the working directory.
const runAsCLIEnv = "SELFDOC_CLI_RUN_AS_CLI"

// cliProcess is one real invocation of the application as a subprocess.
type cliProcess struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runCLI runs the application as a real subprocess with dir as its working
// directory and a non-interactive standard input.
func runCLI(t *testing.T, dir string, argv ...string) cliProcess {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer devnull.Close()

	command := exec.Command(binary, argv...)
	command.Dir = dir
	command.Stdin = devnull
	command.Env = append(os.Environ(), runAsCLIEnv+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running the application: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return cliProcess{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
}

// The fakes' protocol types, under the names the assertions read them by.
type (
	// toolReply is one scripted answer. Match is a substring of the joined
	// argv; an empty Match answers anything.
	toolReply = faketool.Reply
	// toolCall is one recorded invocation of a fake tool.
	toolCall = faketool.Call
)

// fakeTools is a set of fake executables at the front of PATH, plus the state
// they record into.
type fakeTools struct {
	t   *testing.T
	dir string
}

// newFakeTools installs fakes for the named executables and returns the handle
// a test drives them through. The state directory reaches each fake through
// the environment the command's subprocess inherits.
func newFakeTools(t *testing.T, names ...string) *fakeTools {
	t.Helper()
	bin := isolate(t)
	state := t.TempDir()
	for _, name := range names {
		installFakeTool(t, filepath.Join(bin, name))
	}
	t.Setenv(faketool.StateEnv, state)
	return &fakeTools{t: t, dir: state}
}

// installFakeTool puts the prebuilt fake at path, by hard link where the two
// paths share a filesystem and by copy otherwise. The name it is installed
// under is the tool name it records, so each fake must be its own file.
func installFakeTool(t *testing.T, path string) {
	t.Helper()
	if err := os.Link(fakeToolBinary, path); err == nil {
		return
	}
	data, err := os.ReadFile(fakeToolBinary)
	if err != nil {
		t.Fatalf("reading the fake tool: %v", err)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("installing the fake tool: %v", err)
	}
}

// Reply scripts the answers, matched against each call's joined argv in order.
func (f *fakeTools) Reply(replies ...toolReply) {
	f.t.Helper()
	data, err := json.Marshal(replies)
	if err != nil {
		f.t.Fatalf("encoding the replies: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "replies.json"), data, 0o644); err != nil {
		f.t.Fatalf("writing the replies: %v", err)
	}
}

// Calls returns every invocation recorded so far, in order.
func (f *fakeTools) Calls() []toolCall {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, "calls.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		f.t.Fatalf("reading the recorded calls: %v", err)
	}
	var calls []toolCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var call toolCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			f.t.Fatalf("decoding a recorded call: %v", err)
		}
		calls = append(calls, call)
	}
	return calls
}

// Matching returns every recorded call whose joined argv contains needle.
func (f *fakeTools) Matching(needle string) []toolCall {
	f.t.Helper()
	var found []toolCall
	for _, call := range f.Calls() {
		if strings.Contains(call.Joined(), needle) {
			found = append(found, call)
		}
	}
	return found
}

// isolate binds the environment isolation floor and returns a directory at the
// FRONT of PATH, so a fake tool written there shadows any real one. The
// inherited PATH stays behind it, so git and python3 stay reachable.
//
// Nothing that calls this may call t.Parallel: hygiene mutates process-wide
// variables.
func isolate(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return bin
}

// testVersion is the version the application under test reports. The binary
// reads its own from the repository's VERSION file; a test states one so the
// paths that need a real version -- the toolchain pin a generated deploy
// workflow installs -- have one without reading the repository.
const testVersion = "9.9.9"

// newApp builds the application under test, pointed at dir.
func newApp(t *testing.T, dir string) *strictcli.App {
	t.Helper()
	return New(Options{Dir: dir, Version: testVersion})
}

// run invokes the application with argv and returns the framework's result.
func run(t *testing.T, dir string, argv ...string) strictcli.Result {
	t.Helper()
	return newApp(t, dir).Test(argv)
}

// runWith invokes the application built from the given options, which is how
// a test states the stubbed registries a toolchain-pin check reads.
//
// A test that states no version gets testVersion, so only a test that cares
// about the version has to name one.
func runWith(t *testing.T, opts Options, argv ...string) strictcli.Result {
	t.Helper()
	if opts.Version == "" {
		opts.Version = testVersion
	}
	return New(opts).Test(argv)
}

// payloadOf decodes the envelope a --json run wrote and returns its payload.
func payloadOf(t *testing.T, result strictcli.Result) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("decoding the envelope: %v\nstdout: %s\nstderr: %s",
			err, result.Stdout, result.Stderr)
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok {
		t.Fatalf("the envelope carries no object payload: %s", result.Stdout)
	}
	return payload
}

// writeText writes a file, creating its parents.
func writeText(t *testing.T, path, content string) {
	t.Helper()
	testproject.WriteText(t, path, content)
}

// readText reads a file's whole contents.
func readText(t *testing.T, path string) string {
	t.Helper()
	return testproject.ReadText(t, path)
}

// readJSON decodes a JSON file into a generic tree.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return value
}

// exists reports whether path names an existing file or directory.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
