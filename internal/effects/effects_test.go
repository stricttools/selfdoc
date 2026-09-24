package effects

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/testisolation/go/hygiene"
)

// dispatch runs body through a mutating strictcli command and returns the
// dispatch's structured effect log plus the framework's captured result. With
// dryRun true the handler's handle previews; otherwise it executes.
//
// The whole point of the harness is that nothing is mocked: the handle under
// test is the one a real dispatch hands a real command handler.
func dispatch(t *testing.T, dryRun bool, body func(h *Handle)) ([]map[string]interface{}, strictcli.Result) {
	t.Helper()
	app := strictcli.NewApp("effectstest", "0.0.0", "harness for the effects handle")
	app.Command("do", "run the test body",
		func(ctx *strictcli.Context, _ map[string]interface{}) strictcli.Outcome {
			body(FromContext(ctx))
			return strictcli.Exit(0)
		},
		strictcli.WithEffect(strictcli.EffectMutating),
	)
	argv := []string{"do"}
	if dryRun {
		argv = append(argv, "--dry-run")
	}
	result := app.Test(argv)
	return app.EffectLog(), result
}

// verbs renders one effect-log entry per line as "verb: detail", the form the
// assertions below compare against.
func verbs(log []map[string]interface{}) []string {
	out := make([]string, 0, len(log))
	for _, record := range log {
		out = append(out, record["verb"].(string)+": "+record["detail"].(string))
	}
	return out
}

func equalLines(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// --- process effects, live -------------------------------------------------

func TestRunLive(t *testing.T) {
	hygiene.Isolate(t)
	tests := []struct {
		name     string
		argv     []string
		options  []Option
		wantExit int
		wantOut  string
		wantErr  bool
	}{
		{
			name:     "captured stdout",
			argv:     []string{"/bin/sh", "-c", "printf hello"},
			options:  []Option{CaptureOutput()},
			wantExit: 0,
			wantOut:  "hello",
		},
		{
			name:     "a non-zero exit is reported, not raised",
			argv:     []string{"/bin/sh", "-c", "exit 3"},
			options:  []Option{CaptureOutput()},
			wantExit: 3,
		},
		{
			name:    "Check turns a non-zero exit into an error",
			argv:    []string{"/bin/sh", "-c", "exit 3"},
			options: []Option{CaptureOutput(), Check()},
			wantErr: true,
		},
		{
			name:     "stdin payload",
			argv:     []string{"/bin/cat"},
			options:  []Option{CaptureOutput(), Stdin([]byte("payload"))},
			wantExit: 0,
			wantOut:  "payload",
		},
		{
			name:     "a declared read executes",
			argv:     []string{"/bin/sh", "-c", "printf observed"},
			options:  []Option{CaptureOutput(), Read()},
			wantExit: 0,
			wantOut:  "observed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Unbound().Run(tt.argv, tt.options...)
			if tt.wantErr {
				var exitErr *ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("Run error = %v, want an *ExitError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Unsettled {
				t.Error("a live run reported Unsettled")
			}
			if res.ExitCode != tt.wantExit {
				t.Errorf("exit = %d, want %d", res.ExitCode, tt.wantExit)
			}
			if string(res.Stdout) != tt.wantOut {
				t.Errorf("stdout = %q, want %q", res.Stdout, tt.wantOut)
			}
		})
	}
}

// TestRunLiveEnvReplaces covers Env's Python semantics: the map IS the child's
// environment, so an inherited variable is gone. (The shell still supplies its
// own default PATH, which is why the assertion names a variable of our own.)
func TestRunLiveEnvReplaces(t *testing.T) {
	hygiene.Isolate(t)
	t.Setenv("SELFDOC_TEST_INHERITED", "inherited")

	res, err := Unbound().Run(
		[]string{"/bin/sh", "-c", `printf "%s|%s" "$ONLY" "$SELFDOC_TEST_INHERITED"`},
		CaptureOutput(), Env(map[string]string{"ONLY": "value"}),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Stdout) != "value|" {
		t.Errorf("stdout = %q, want %q", res.Stdout, "value|")
	}

	inheriting, err := Unbound().Run(
		[]string{"/bin/sh", "-c", `printf %s "$SELFDOC_TEST_INHERITED"`},
		CaptureOutput(),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(inheriting.Stdout) != "inherited" {
		t.Errorf("without Env, stdout = %q, want %q", inheriting.Stdout, "inherited")
	}
}

func TestRunLiveCwd(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	res, err := Unbound().Run([]string{"/bin/sh", "-c", "pwd"}, CaptureOutput(), Cwd(dir))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// macOS and some Linux temp roots are symlinked, so the child's own view of
	// the directory is what gets compared.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.StdoutString() != want {
		t.Errorf("pwd = %q, want %q", res.StdoutString(), want)
	}
}

func TestRunTimeout(t *testing.T) {
	hygiene.Isolate(t)
	start := time.Now()
	_, err := Unbound().Run([]string{"/bin/sh", "-c", "sleep 30"}, Timeout(150*time.Millisecond))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run error = %v, want one wrapping ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the deadline was not enforced: the run took %s", elapsed)
	}
	if !strings.Contains(err.Error(), "sleep 30") {
		t.Errorf("the error does not name the command: %v", err)
	}
}

func TestRunRejectsUnacceptedOption(t *testing.T) {
	t.Parallel()
	if _, err := Unbound().Pipeline([][]string{{"true"}, {"true"}}, Check()); err == nil {
		t.Error("Pipeline accepted the check option, which it does not declare")
	}
	if _, err := Unbound().Run(nil, CaptureOutput()); err == nil {
		t.Error("Run accepted an empty argv")
	}
}

func TestPipelineLive(t *testing.T) {
	hygiene.Isolate(t)
	res, err := Unbound().Pipeline([][]string{
		{"/bin/sh", "-c", "printf 'a\\nb\\nc\\n'"},
		{"/bin/sh", "-c", "wc -l >/dev/null; exit 7"},
	})
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit = %d, want 7 (the last stage's status)", res.ExitCode)
	}
	if _, err := Unbound().Pipeline([][]string{{"true"}}); err == nil {
		t.Error("Pipeline accepted a single stage")
	}
}

func TestPipelineLiveStderr(t *testing.T) {
	hygiene.Isolate(t)
	res, err := Unbound().Pipeline([][]string{
		{"/bin/sh", "-c", "printf data"},
		{"/bin/sh", "-c", "cat >/dev/null; printf complaint >&2"},
	})
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}
	if string(res.Stderr) != "complaint" {
		t.Errorf("stderr = %q, want %q", res.Stderr, "complaint")
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"git", "git"},
		{"--depth=1", "--depth=1"},
		{"refs/heads/main", "refs/heads/main"},
		{"a b", "'a b'"},
		{"", "''"},
		{"it's", `'it'\''s'`},
		{"$HOME", "'$HOME'"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := ShellQuote(tt.in); got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// --- filesystem effects, live ----------------------------------------------

func TestFilesystemLive(t *testing.T) {
	t.Parallel()
	h := Unbound()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	if err := h.Write(path, []byte("one"), ModeDefault); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "one" {
		t.Fatalf("after Write: %q, %v", content, err)
	}
	if err := h.Write(path, []byte("two"), 0o600); err != nil {
		t.Fatalf("Write with a mode: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o, want 0600", info.Mode().Perm())
	}

	if err := h.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	renamed := filepath.Join(dir, "renamed.txt")
	if err := h.Rename(path, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	copied := filepath.Join(dir, "copied.txt")
	if err := h.CopyFile(renamed, copied); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if content, _ := os.ReadFile(copied); string(content) != "two" {
		t.Errorf("CopyFile content = %q, want %q", content, "two")
	}
	if err := h.Remove(copied); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := h.Remove(copied); err == nil {
		t.Error("Remove of a missing path reported no error")
	}
	if err := h.RemoveIfExists(copied); err != nil {
		t.Errorf("RemoveIfExists of a missing path: %v", err)
	}

	nested := filepath.Join(dir, "a", "b", "c")
	if err := h.Mkdir(nested); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := h.Mkdir(nested); err == nil {
		t.Error("Mkdir of an existing path reported no error")
	}
	if err := h.MkdirAll(nested); err != nil {
		t.Errorf("MkdirAll of an existing path: %v", err)
	}
	if err := h.Rmdir(nested); err != nil {
		t.Fatalf("Rmdir: %v", err)
	}
	if err := h.RmTree(filepath.Join(dir, "a")); err != nil {
		t.Fatalf("RmTree: %v", err)
	}
	if err := h.RmTree(filepath.Join(dir, "a")); err == nil {
		t.Error("RmTree of a missing path reported no error")
	}
	if err := h.RmTreeIgnoreErrors(filepath.Join(dir, "a")); err != nil {
		t.Errorf("RmTreeIgnoreErrors of a missing path: %v", err)
	}
}

func TestOpenWriteAndAppendLive(t *testing.T) {
	t.Parallel()
	h := Unbound()
	path := filepath.Join(t.TempDir(), "stream.txt")

	w, err := h.OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	for _, chunk := range []string{"a", "b", "c"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "abc" {
		t.Errorf("content = %q, want %q", content, "abc")
	}

	a, err := h.OpenAppend(path)
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if _, err := a.Write([]byte("d")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "abcd" {
		t.Errorf("content = %q, want %q", content, "abcd")
	}
}

// TestAtomicWriteOverReadOnlyTarget is why live mode keeps the temp file and
// the rename: a plain write to a 0444 file fails, and the generated root files
// are 0444 by design.
func TestAtomicWriteOverReadOnlyTarget(t *testing.T) {
	t.Parallel()
	h := Unbound()
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(path, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0o444); err == nil {
		t.Skip("this filesystem lets a plain write through a 0444 file; the case cannot be observed here")
	}
	if err := h.AtomicWrite(path, []byte("new"), 0o444); err != nil {
		t.Fatalf("AtomicWrite over a 0444 target: %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "new" {
		t.Errorf("content = %q, want %q", content, "new")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Errorf("mode = %04o, want 0444", info.Mode().Perm())
	}
	// No temporary file is left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only the target", names)
	}
}

func TestCopyTreeLive(t *testing.T) {
	t.Parallel()
	h := Unbound()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top.txt"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "deep.txt"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := h.CopyTree(src, dst, false); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	if content, _ := os.ReadFile(filepath.Join(dst, "nested", "deep.txt")); string(content) != "d" {
		t.Errorf("nested content = %q, want %q", content, "d")
	}
	if err := h.CopyTree(src, dst, false); !errors.Is(err, fs.ErrExist) {
		t.Errorf("CopyTree over an existing destination = %v, want an exists error", err)
	}
	if err := h.CopyTree(src, dst, true); err != nil {
		t.Errorf("CopyTree with dirsExistOK: %v", err)
	}
}

// --- preview mode ----------------------------------------------------------

func TestUnboundHandleNeverPreviews(t *testing.T) {
	t.Parallel()
	if Unbound().Previewing() {
		t.Error("an unbound handle reported that it previews")
	}
	if FromContext(nil).Previewing() {
		t.Error("FromContext(nil) reported that it previews")
	}
}

// TestBoundHandleOutsideDryRunExecutes is the other half of the mode rule: a
// bound handle on a live run performs its effects.
func TestBoundHandleOutsideDryRunExecutes(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "written.txt")
	log, result := dispatch(t, false, func(h *Handle) {
		if h.Previewing() {
			t.Error("a bound handle outside --dry-run reported that it previews")
		}
		if err := h.Write(path, []byte("live"), ModeDefault); err != nil {
			t.Errorf("Write: %v", err)
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "live" {
		t.Errorf("the file was not written: %q, %v", content, err)
	}
	// A live run executes directly and mints nothing, so the dispatch's effect
	// log stays empty -- the same split the Python chokepoint had.
	if len(log) != 0 {
		t.Errorf("a live run recorded %v, want nothing", verbs(log))
	}
}

func TestRunPreviewIsRecorded(t *testing.T) {
	hygiene.Isolate(t)
	marker := filepath.Join(t.TempDir(), "marker")
	log, result := dispatch(t, true, func(h *Handle) {
		if !h.Previewing() {
			t.Error("a bound handle under --dry-run reported that it does not preview")
		}
		res, err := h.Run([]string{"/bin/sh", "-c", "touch " + marker}, CaptureOutput())
		if err != nil {
			t.Errorf("Run: %v", err)
			return
		}
		if !res.Unsettled {
			t.Error("a recorded run did not report Unsettled")
		}
		if res.Stdout != nil || res.ExitCode != 0 {
			t.Errorf("a recorded run carried a payload: %+v", res)
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the recorded command ran")
	}
	want := []string{"run: /bin/sh -c touch " + marker}
	if got := verbs(log); !equalLines(got, want) {
		t.Errorf("log = %v, want %v", got, want)
	}
}

// TestReadExemptionUnderPreview is the "reads are never effects" rule: a
// declared read executes even while previewing, and leaves no record.
func TestReadExemptionUnderPreview(t *testing.T) {
	hygiene.Isolate(t)
	log, result := dispatch(t, true, func(h *Handle) {
		res, err := h.Run([]string{"/bin/sh", "-c", "printf observed"}, CaptureOutput(), Read())
		if err != nil {
			t.Errorf("Run: %v", err)
			return
		}
		if res.Unsettled {
			t.Error("a declared read reported Unsettled under --dry-run")
		}
		if string(res.Stdout) != "observed" {
			t.Errorf("stdout = %q, want %q", res.Stdout, "observed")
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	if len(log) != 0 {
		t.Errorf("a declared read was recorded: %v", verbs(log))
	}
}

func TestPipelinePreviewIsOneShellLine(t *testing.T) {
	hygiene.Isolate(t)
	log, result := dispatch(t, true, func(h *Handle) {
		res, err := h.Pipeline([][]string{
			{"git", "archive", "--format=tar", "HEAD"},
			{"tar", "-x", "-C", "/tmp/out dir"},
		})
		if err != nil {
			t.Errorf("Pipeline: %v", err)
			return
		}
		if !res.Unsettled {
			t.Error("a recorded pipeline did not report Unsettled")
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	want := []string{
		"run: /bin/sh -c git archive --format=tar HEAD | tar -x -C '/tmp/out dir'",
	}
	if got := verbs(log); !equalLines(got, want) {
		t.Errorf("log = %v, want %v", got, want)
	}
}

func TestFilesystemPreviewRecordsAndChangesNothing(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	other := filepath.Join(dir, "other.txt")
	log, result := dispatch(t, true, func(h *Handle) {
		for _, step := range []struct {
			name string
			run  func() error
		}{
			{"Write", func() error { return h.Write(path, []byte("hello"), ModeDefault) }},
			{"AtomicWrite", func() error { return h.AtomicWrite(path, []byte("bye"), 0o444) }},
			{"Mkdir", func() error { return h.Mkdir(filepath.Join(dir, "sub")) }},
			{"Chmod", func() error { return h.Chmod(path, 0o600) }},
			{"Rename", func() error { return h.Rename(path, other) }},
			{"Remove", func() error { return h.Remove(other) }},
			{"RmTree", func() error { return h.RmTree(dir) }},
		} {
			if err := step.run(); err != nil {
				t.Errorf("%s: %v", step.name, err)
			}
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	want := []string{
		"write: " + path + " (5 bytes)",
		"write: " + path + " (3 bytes)",
		"chmod: " + path + " 0444",
		"mkdir: " + filepath.Join(dir, "sub"),
		"chmod: " + path + " 0600",
		"rename: " + path + " -> " + other,
		"remove: " + other,
		"remove: " + dir,
	}
	if got := verbs(log); !equalLines(got, want) {
		t.Errorf("log =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the preview touched the filesystem: %v", entries)
	}
}

// TestOpenWritePreviewMintsOneWrite covers the streaming case: the content
// accumulates and Close records a single write carrying the whole byte count.
func TestOpenWritePreviewMintsOneWrite(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.txt")
	appended := filepath.Join(dir, "appended.txt")
	if err := os.WriteFile(appended, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	log, result := dispatch(t, true, func(h *Handle) {
		w, err := h.OpenWrite(path)
		if err != nil {
			t.Errorf("OpenWrite: %v", err)
			return
		}
		for _, chunk := range []string{"one", "two"} {
			if _, err := w.Write([]byte(chunk)); err != nil {
				t.Errorf("Write: %v", err)
			}
		}
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		// A second Close is a no-op, so a deferred close cannot double-record.
		if err := w.Close(); err != nil {
			t.Errorf("second Close: %v", err)
		}

		a, err := h.OpenAppend(appended)
		if err != nil {
			t.Errorf("OpenAppend: %v", err)
			return
		}
		if _, err := a.Write([]byte("!")); err != nil {
			t.Errorf("Write: %v", err)
		}
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	want := []string{
		"write: " + path + " (6 bytes)",
		// The recorded append carries the whole resulting file.
		"write: " + appended + " (9 bytes)",
	}
	if got := verbs(log); !equalLines(got, want) {
		t.Errorf("log = %v, want %v", got, want)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the streaming preview wrote the file")
	}
	if content, _ := os.ReadFile(appended); string(content) != "existing" {
		t.Errorf("the appending preview changed the file: %q", content)
	}
}

func TestCopyTreePreviewNamesEveryPath(t *testing.T) {
	hygiene.Isolate(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "deep.txt"), []byte("dd"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	log, result := dispatch(t, true, func(h *Handle) {
		if err := h.CopyTree(src, dst, false); err != nil {
			t.Errorf("CopyTree: %v", err)
		}
	})
	if result.ExitCode != 0 {
		t.Fatalf("dispatch exit = %d, stderr = %s", result.ExitCode, result.Stderr)
	}
	want := []string{
		"mkdir: " + dst,
		"mkdir: " + filepath.Join(dst, "nested"),
		"write: " + filepath.Join(dst, "nested", "deep.txt") + " (2 bytes)",
	}
	if got := verbs(log); !equalLines(got, want) {
		t.Errorf("log = %v, want %v", got, want)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("the preview created the destination")
	}
}
