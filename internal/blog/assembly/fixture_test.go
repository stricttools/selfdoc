package assembly

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/assembly/fakegh"
	"github.com/smm-h/stricttest/go/hygiene"
)

// fakeGHBinary is the fake gh this test binary built for itself: one plain,
// uninstrumented executable, built once by [TestMain] and installed as "gh" by
// every test that needs one.
var fakeGHBinary string

// TestMain builds the fake gh before any test runs.
//
// Every operation in this package that reaches GitHub does it by running "gh
// api", so the seam a test replaces is the executable rather than a function:
// an executable at the front of PATH shadows the real gh, and the call the
// code under test makes is a real subprocess through the real effects handle.
// The fake is therefore ordinary Go code with the whole Git Data API's
// bookkeeping in it -- persistent blobs, a real git blob hash per path, base64
// payloads -- rather than a shell script pretending to be one.
//
// That Go code is a program of its own, in the fakegh package, compiled here
// without the race detector. It used to be this very binary re-running itself,
// which made every one of the suite's hundreds of gh calls pay the race
// runtime's start-up cost and took `go test -race` on this package to the edge
// of the per-binary timeout.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "selfdoc-fake-gh-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating the fake gh's build directory: %v\n", err)
		os.Exit(1)
	}
	fakeGHBinary = filepath.Join(dir, "gh")
	build := exec.Command("go", "build", "-o", fakeGHBinary,
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

// The fake's protocol types, under the names the assertions read them by.
type (
	// ghCallRecord is one recorded invocation of the fake gh.
	ghCallRecord = fakegh.CallRecord
	// ghResponse is one scripted answer.
	ghResponse = fakegh.Response
	// ghFailure injects a failure into repo mode for every call whose argv
	// contains its Match.
	ghFailure = fakegh.Failure
)

// fakeGH is a fake gh at the front of PATH, with the state the fake keeps.
type fakeGH struct {
	t   *testing.T
	dir string
}

// newFakeGH installs a fake gh in a directory at the front of PATH and returns
// the handle a test drives it through.
//
// It binds the environment isolation floor first: a throwaway HOME, an empty
// global git config with a throwaway identity, only the file:// git transport,
// and no ambient credentials. Nothing here calls t.Parallel, because hygiene
// mutates process-wide variables. The state directory reaches the fake through
// the environment the code under test's subprocess inherits.
func newFakeGH(t *testing.T) *fakeGH {
	t.Helper()
	bin := isolate(t)
	state := t.TempDir()
	installFakeGH(t, filepath.Join(bin, "gh"))
	t.Setenv(fakegh.StateEnv, state)
	return &fakeGH{t: t, dir: state}
}

// installFakeGH puts the prebuilt fake gh at path, by hard link where the two
// paths share a filesystem and by copy otherwise.
func installFakeGH(t *testing.T, path string) {
	t.Helper()
	if err := os.Link(fakeGHBinary, path); err == nil {
		return
	}
	data, err := os.ReadFile(fakeGHBinary)
	if err != nil {
		t.Fatalf("reading the fake gh: %v", err)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("installing the fake gh: %v", err)
	}
}

// isolate binds the environment isolation floor -- a throwaway HOME, an empty
// global git config with a throwaway identity, only the file:// git transport,
// no ambient credentials -- and returns a directory at the FRONT of PATH, so a
// fake tool written there shadows any real one. The system directories stay
// behind it, so git stays reachable.
//
// Nothing that calls this may call t.Parallel: hygiene mutates process-wide
// variables.
func isolate(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return bin
}

// shellQuote single-quotes a token for a fake tool's shell wrapper.
func shellQuote(token string) string {
	return "'" + strings.ReplaceAll(token, "'", `'\''`) + "'"
}

// path is a file inside the fake's state directory.
func (f *fakeGH) path(name string) string { return filepath.Join(f.dir, name) }

// writeJSON writes a state file.
func (f *fakeGH) writeJSON(name string, value any) {
	f.t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Fatalf("encoding %s: %v", name, err)
	}
	if err := os.WriteFile(f.path(name), data, 0o644); err != nil {
		f.t.Fatalf("writing %s: %v", name, err)
	}
}

// Script puts the fake in scripted mode: it answers each call from responses
// in order, whatever the call asked for, and fails the run when a call arrives
// with no answer left.
func (f *fakeGH) Script(responses ...ghResponse) {
	f.t.Helper()
	f.writeJSON("script.json", responses)
}

// Blobs seeds repo mode with the files the branch already holds.
func (f *fakeGH) Blobs(blobs map[string][]byte) {
	f.t.Helper()
	encoded := map[string]string{}
	for path, data := range blobs {
		encoded[path] = base64.StdEncoding.EncodeToString(data)
	}
	f.writeJSON("blobs.json", encoded)
}

// Truncate makes the Trees API report a tree too large to return, which is the
// one answer that cannot be read as an empty repository.
func (f *fakeGH) Truncate() {
	f.t.Helper()
	f.writeJSON("truncated.json", true)
}

// Fail injects a failure for every call whose joined argv contains match.
func (f *fakeGH) Fail(match string, code int, stderr string) {
	f.t.Helper()
	failures := f.failures()
	failures = append(failures, ghFailure{Match: match, Code: code, Stderr: stderr})
	f.writeJSON("failures.json", failures)
}

// failures is the injected failure list.
func (f *fakeGH) failures() []ghFailure {
	data, err := os.ReadFile(f.path("failures.json"))
	if err != nil {
		return nil
	}
	var failures []ghFailure
	if err := json.Unmarshal(data, &failures); err != nil {
		f.t.Fatalf("decoding failures.json: %v", err)
	}
	return failures
}

// Calls is every invocation the fake recorded, in order.
func (f *fakeGH) Calls() []ghCallRecord {
	f.t.Helper()
	data, err := os.ReadFile(f.path("calls.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatalf("reading the call log: %v", err)
	}
	var calls []ghCallRecord
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var call ghCallRecord
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			f.t.Fatalf("decoding a call log line: %v", err)
		}
		calls = append(calls, call)
	}
	return calls
}

// Content is the bytes repo mode holds at a path, and whether it holds it.
func (f *fakeGH) Content(path string) ([]byte, bool) {
	f.t.Helper()
	blobs := f.storedBlobs()
	encoded, ok := blobs[path]
	if !ok {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		f.t.Fatalf("decoding the stored blob at %s: %v", path, err)
	}
	return data, true
}

// Text is the text repo mode holds at a path, failing the test when it holds
// nothing there.
func (f *fakeGH) Text(path string) string {
	f.t.Helper()
	data, ok := f.Content(path)
	if !ok {
		f.t.Fatalf("the fake remote holds nothing at %s; it holds %v",
			path, f.Paths())
	}
	return string(data)
}

// Paths is every path repo mode holds, sorted.
func (f *fakeGH) Paths() []string {
	f.t.Helper()
	blobs := f.storedBlobs()
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// storedBlobs is repo mode's file table.
func (f *fakeGH) storedBlobs() map[string]string {
	f.t.Helper()
	data, err := os.ReadFile(f.path("blobs.json"))
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	if err != nil {
		f.t.Fatalf("reading the fake remote's blobs: %v", err)
	}
	blobs := map[string]string{}
	if err := json.Unmarshal(data, &blobs); err != nil {
		f.t.Fatalf("decoding the fake remote's blobs: %v", err)
	}
	return blobs
}

// Commits is how many commits repo mode created.
func (f *fakeGH) Commits() int { return f.counter("commits") }

// Uploads is how many blobs repo mode accepted.
func (f *fakeGH) Uploads() int { return f.counter("uploads") }

// counter reads one of repo mode's counters.
func (f *fakeGH) counter(name string) int {
	f.t.Helper()
	data, err := os.ReadFile(f.path(name + ".count"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		f.t.Fatalf("reading the %s counter: %v", name, err)
	}
	count := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count); err != nil {
		f.t.Fatalf("decoding the %s counter: %v", name, err)
	}
	return count
}
