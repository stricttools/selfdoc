package gitcommit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/testisolation/go/hygiene"
)

// isolate binds the environment isolation floor -- a throwaway HOME, an empty
// global git config with a throwaway identity, no ambient credentials -- and
// returns a directory at the FRONT of PATH.
//
// A fake tool written there shadows the real one, which this package needs:
// rlsbl and safegit are installed on the developer's machine, and without the
// shadow every test below would hand the commit to the real tool inside a
// throwaway repository. The system directories stay behind it so git itself
// stays reachable.
//
// Nothing here calls t.Parallel: hygiene mutates process-wide variables.
func isolate(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return bin
}

// fakeTool writes an executable shell script named name into dir. The tests
// use it for rlsbl and safegit: what is under test is the argv selfdoc hands
// the chosen tool, not the tool's own behavior.
func fakeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
}

// recorder writes a fake commit tool that appends its whole argv to a log file
// and succeeds. The returned function reads back one string per invocation,
// the argv joined by spaces.
func recorder(t *testing.T, bin, name string) func() []string {
	t.Helper()
	log := filepath.Join(t.TempDir(), name+".argv")
	fakeTool(t, bin, name, "for a in \"$@\"; do printf '%s\\n' \"$a\" >> "+
		quote(log)+"; done; printf '\\n' >> "+quote(log))
	return func() []string {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatalf("reading %s: %v", log, err)
		}
		var argvs []string
		for _, block := range strings.Split(strings.TrimSuffix(string(data), "\n\n"), "\n\n") {
			if block != "" {
				argvs = append(argvs, strings.Join(strings.Split(block, "\n"), " "))
			}
		}
		return argvs
	}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// initRepo initializes a git repository at path with one commit, so HEAD
// exists. The identity comes from hygiene's throwaway global config.
func initRepo(t *testing.T, path string) {
	t.Helper()
	run(t, path, "init")
	if err := os.WriteFile(filepath.Join(path, "README"), []byte("init"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run(t, path, "add", "README")
	run(t, path, "commit", "-m", "initial")
}

// run executes a real git command in dir and fails the test if it does not
// succeed. It is the test's own scaffolding, not part of the code under test.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := effects.Unbound().Run(
		append([]string{"git"}, args...),
		effects.Cwd(dir),
		effects.CaptureOutput(),
	)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("git %s exited %d: %s", strings.Join(args, " "), res.ExitCode, res.Stderr)
	}
	return string(res.Stdout)
}

// write creates a file under dir, making parent directories as needed.
func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// commit is the shorthand every test below calls: AutoCommit with an unbound
// handle, failing on the error a live run cannot produce.
func commit(t *testing.T, files []string, message, cwd string) (bool, bool) {
	t.Helper()
	committed, unsettled, err := AutoCommit(files, message, cwd, effects.Unbound())
	if err != nil {
		t.Fatalf("AutoCommit: %v", err)
	}
	return committed, unsettled
}

// --- the guards ------------------------------------------------------------

func TestAutoCommitDeclines(t *testing.T) {
	tests := []struct {
		name string
		// setup prepares the repository and returns the files to commit.
		setup func(t *testing.T, dir string) []string
		guard bool
	}{
		{
			name: "the loop guard is set",
			setup: func(t *testing.T, dir string) []string {
				initRepo(t, dir)
				write(t, dir, "file.txt", "data")
				return []string{"file.txt"}
			},
			guard: true,
		},
		{
			name: "the directory is not a git repository",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "file.txt", "data")
				return []string{"file.txt"}
			},
		},
		{
			name: "a tracked file has no changes",
			setup: func(t *testing.T, dir string) []string {
				initRepo(t, dir)
				return []string{"README"}
			},
		},
		{
			name: "the only file named does not exist",
			setup: func(t *testing.T, dir string) []string {
				initRepo(t, dir)
				return []string{"absent.txt"}
			},
		},
		{
			name: "every untracked candidate is gitignored",
			setup: func(t *testing.T, dir string) []string {
				initRepo(t, dir)
				write(t, dir, ".gitignore", "*.log\ncache/\n")
				run(t, dir, "add", ".gitignore")
				run(t, dir, "commit", "-m", "add gitignore")
				write(t, dir, "debug.log", "log data")
				write(t, dir, "cache/data.bin", "cached")
				return []string{"debug.log", "cache/data.bin"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			dir := t.TempDir()
			files := tt.setup(t, dir)
			if tt.guard {
				t.Setenv(guardVar, "1")
			}
			committed, unsettled := commit(t, files, "msg", dir)
			if committed || unsettled {
				t.Errorf("committed = %v, unsettled = %v, want both false", committed, unsettled)
			}
		})
	}
}

// --- classification, against a real repository -----------------------------

func TestAutoCommitClassification(t *testing.T) {
	tests := []struct {
		name string
		// setup prepares the repository and returns the files to commit.
		setup func(t *testing.T, dir string) []string
		// check runs after a successful commit.
		check func(t *testing.T, dir string)
	}{
		{
			name: "an untracked file",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "newfile.txt", "new content")
				return []string{"newfile.txt"}
			},
			check: func(t *testing.T, dir string) {
				assertTracked(t, dir, "newfile.txt")
			},
		},
		{
			name: "a modified tracked file",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "README", "changed")
				return []string{"README"}
			},
			check: func(t *testing.T, dir string) {
				if got := run(t, dir, "show", "HEAD:README"); got != "changed" {
					t.Errorf("committed README = %q, want %q", got, "changed")
				}
			},
		},
		{
			name: "several files at once",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "a.txt", "aaa")
				write(t, dir, "b.txt", "bbb")
				return []string{"a.txt", "b.txt"}
			},
			check: func(t *testing.T, dir string) {
				assertTracked(t, dir, "a.txt")
				assertTracked(t, dir, "b.txt")
			},
		},
		{
			name: "a deletion of a tracked file",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "to_delete.txt", "will be deleted")
				run(t, dir, "add", "to_delete.txt")
				run(t, dir, "commit", "-m", "add file")
				if err := os.Remove(filepath.Join(dir, "to_delete.txt")); err != nil {
					t.Fatalf("Remove: %v", err)
				}
				return []string{"to_delete.txt"}
			},
			check: func(t *testing.T, dir string) {
				assertNotTracked(t, dir, "to_delete.txt")
			},
		},
		{
			name: "a modification, a deletion and a new file together",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "file1.txt", "original")
				write(t, dir, "file2.txt", "to delete")
				run(t, dir, "add", "file1.txt", "file2.txt")
				run(t, dir, "commit", "-m", "add files")
				write(t, dir, "file1.txt", "modified")
				if err := os.Remove(filepath.Join(dir, "file2.txt")); err != nil {
					t.Fatalf("Remove: %v", err)
				}
				write(t, dir, "file3.txt", "brand new")
				return []string{"file1.txt", "file2.txt", "file3.txt"}
			},
			check: func(t *testing.T, dir string) {
				if got := run(t, dir, "show", "HEAD:file1.txt"); got != "modified" {
					t.Errorf("committed file1.txt = %q, want %q", got, "modified")
				}
				assertNotTracked(t, dir, "file2.txt")
				assertTracked(t, dir, "file3.txt")
			},
		},
		{
			name: "a filename that looks like a git option",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "--hierarchical", "tricky content")
				return []string{"--hierarchical"}
			},
			check: func(t *testing.T, dir string) {
				log := run(t, dir, "log", "--oneline", "--", "--hierarchical")
				if !strings.Contains(log, "msg") {
					t.Errorf("log for the tricky file = %q", log)
				}
			},
		},
		{
			name: "a tracked file matching a gitignore pattern",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "config.log", "original")
				run(t, dir, "add", "config.log")
				run(t, dir, "commit", "-m", "add config.log")
				write(t, dir, ".gitignore", "*.log\n")
				run(t, dir, "add", ".gitignore")
				run(t, dir, "commit", "-m", "add gitignore")
				write(t, dir, "config.log", "modified")
				return []string{"config.log"}
			},
			check: func(t *testing.T, dir string) {
				if got := run(t, dir, "show", "HEAD:config.log"); got != "modified" {
					t.Errorf("committed config.log = %q, want %q", got, "modified")
				}
			},
		},
		{
			name: "a gitignored file mixed in with a legitimate one",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, ".gitignore", ".stricttools/docs-cache/\n")
				run(t, dir, "add", ".gitignore")
				run(t, dir, "commit", "-m", "add gitignore")
				write(t, dir, ".stricttools/docs/index.md", "# Hello\n")
				write(t, dir, ".stricttools/docs-cache/build/index.html", "<html></html>\n")
				return []string{
					".stricttools/docs/index.md",
					".stricttools/docs-cache/build/index.html",
				}
			},
			check: func(t *testing.T, dir string) {
				assertTracked(t, dir, ".stricttools/docs/index.md")
				assertNotTracked(t, dir, ".stricttools/docs-cache/build/index.html")
			},
		},
		{
			name: "an absolute path naming a file in the repository",
			setup: func(t *testing.T, dir string) []string {
				write(t, dir, "abs.txt", "absolute")
				return []string{filepath.Join(dir, "abs.txt")}
			},
			check: func(t *testing.T, dir string) {
				assertTracked(t, dir, "abs.txt")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			dir := t.TempDir()
			initRepo(t, dir)
			files := tt.setup(t, dir)

			committed, unsettled := commit(t, files, "msg", dir)
			if !committed || unsettled {
				t.Fatalf("committed = %v, unsettled = %v, want true and false", committed, unsettled)
			}
			tt.check(t, dir)
		})
	}
}

func assertTracked(t *testing.T, dir, file string) {
	t.Helper()
	if out := run(t, dir, "ls-files", "--", file); strings.TrimSpace(out) == "" {
		t.Errorf("%s is not tracked after the commit", file)
	}
}

func assertNotTracked(t *testing.T, dir, file string) {
	t.Helper()
	if out := run(t, dir, "ls-files", "--", file); strings.TrimSpace(out) != "" {
		t.Errorf("%s is tracked after the commit, want it absent", file)
	}
}

// --- tool selection --------------------------------------------------------

// TestAutoCommitToolSelection covers the order rlsbl, safegit, plain git, and
// the argv each receives: the message after -m, then the "--" separator, then
// the files -- and no confirmation flag.
//
// strictcli prompts only for commands that declare themselves consequential;
// neither `rlsbl commit` nor `safegit commit` does, because a commit is
// ordinary, undoable work. `--yes` is worse than unnecessary -- it is a banned
// flag name in strictcli, so passing it would be an unknown-flag error that
// left the regenerated files uncommitted while the command still exited 0.
func TestAutoCommitToolSelection(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		wantTool  string
	}{
		{name: "rlsbl wins when both are installed", installed: []string{"rlsbl", "safegit"}, wantTool: "rlsbl"},
		{name: "safegit is next", installed: []string{"safegit"}, wantTool: "safegit"},
		{name: "plain git is the fallback", installed: nil, wantTool: "git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := isolate(t)
			readers := make(map[string]func() []string)
			for _, tool := range tt.installed {
				readers[tool] = recorder(t, bin, tool)
			}
			dir := t.TempDir()
			initRepo(t, dir)
			write(t, dir, "file.txt", "data")

			committed, unsettled := commit(t, []string{"file.txt"}, "add file", dir)
			if !committed || unsettled {
				t.Fatalf("committed = %v, unsettled = %v, want true and false", committed, unsettled)
			}

			if tt.wantTool == "git" {
				// No fake tool ran, so the real git did the work.
				assertTracked(t, dir, "file.txt")
				return
			}
			argvs := readers[tt.wantTool]()
			if len(argvs) != 1 {
				t.Fatalf("%s invocations = %v, want one", tt.wantTool, argvs)
			}
			if argvs[0] != "commit -m add file -- file.txt" {
				t.Errorf("%s argv = %q", tt.wantTool, argvs[0])
			}
			for _, banned := range []string{"--yes", "--approve-consequential"} {
				if strings.Contains(argvs[0], banned) {
					t.Errorf("%s argv carries %s: %q", tt.wantTool, banned, argvs[0])
				}
			}
			// The tool that lost the selection was never called.
			for tool, read := range readers {
				if tool != tt.wantTool && len(read()) != 0 {
					t.Errorf("%s was invoked as well", tool)
				}
			}
		})
	}
}

// TestAutoCommitFiltersGitignoredBeforeTheTool covers the order of the two
// steps: the gitignore filter runs before the commit tool, so the tool never
// sees a path git would refuse.
func TestAutoCommitFiltersGitignoredBeforeTheTool(t *testing.T) {
	bin := isolate(t)
	read := recorder(t, bin, "rlsbl")
	dir := t.TempDir()
	initRepo(t, dir)
	write(t, dir, ".gitignore", "*.cache\n")
	run(t, dir, "add", ".gitignore")
	run(t, dir, "commit", "-m", "add gitignore")
	write(t, dir, "doc.md", "# Doc")
	write(t, dir, "build.cache", "cached")

	committed, _ := commit(t, []string{"doc.md", "build.cache"}, "test commit", dir)
	if !committed {
		t.Fatal("committed = false, want true")
	}
	argvs := read()
	if len(argvs) != 1 {
		t.Fatalf("rlsbl invocations = %v, want one", argvs)
	}
	if !strings.Contains(argvs[0], "doc.md") {
		t.Errorf("rlsbl argv omits doc.md: %q", argvs[0])
	}
	if strings.Contains(argvs[0], "build.cache") {
		t.Errorf("rlsbl argv carries the gitignored file: %q", argvs[0])
	}
}

// TestAutoCommitPassesDeletionsToTheTool covers the one classification the
// tool cannot re-derive: a tracked file that is gone from disk is handed over
// as a path to commit, not dropped for being absent.
func TestAutoCommitPassesDeletionsToTheTool(t *testing.T) {
	for _, tool := range []string{"rlsbl", "safegit"} {
		t.Run(tool, func(t *testing.T) {
			bin := isolate(t)
			read := recorder(t, bin, tool)
			dir := t.TempDir()
			initRepo(t, dir)
			write(t, dir, "gone.txt", "will vanish")
			run(t, dir, "add", "gone.txt")
			run(t, dir, "commit", "-m", "add gone")
			if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
				t.Fatalf("Remove: %v", err)
			}

			committed, _ := commit(t, []string{"gone.txt"}, "rm gone", dir)
			if !committed {
				t.Fatal("committed = false, want true")
			}
			argvs := read()
			if len(argvs) != 1 || !strings.Contains(argvs[0], "gone.txt") {
				t.Fatalf("%s argv = %v, want one naming gone.txt", tool, argvs)
			}
		})
	}
}

// TestAutoCommitForwardsToolStderr covers the failing-tool path: the tool's
// own stderr reaches this process's stderr and nothing is reported committed.
func TestAutoCommitForwardsToolStderr(t *testing.T) {
	bin := isolate(t)
	fakeTool(t, bin, "rlsbl", "echo \"error: pathspec 'file.txt' did not match\" >&2; exit 1")
	dir := t.TempDir()
	initRepo(t, dir)
	write(t, dir, "file.txt", "data")

	captured := captureStderr(t, func() {
		if committed, _ := commit(t, []string{"file.txt"}, "msg", dir); committed {
			t.Error("committed = true, want false")
		}
	})
	if !strings.Contains(captured, "error: pathspec") {
		t.Errorf("stderr = %q, want the tool's own message", captured)
	}
}

// captureStderr redirects this process's stderr for the duration of body and
// returns what was written. os.Stderr is read at every call site, so no
// production seam is needed to observe it.
func captureStderr(t *testing.T, body func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	body()
	os.Stderr = original
	writer.Close()
	captured := <-done
	reader.Close()
	return captured
}

// TestAutoCommitDashSeparator covers the "--" separator on every argv, which
// is what keeps a file named like an option from being read as one.
func TestAutoCommitDashSeparator(t *testing.T) {
	bin := isolate(t)
	read := recorder(t, bin, "rlsbl")
	dir := t.TempDir()
	initRepo(t, dir)
	write(t, dir, "file.txt", "data")

	if committed, _ := commit(t, []string{"file.txt"}, "msg", dir); !committed {
		t.Fatal("committed = false, want true")
	}
	argv := read()[0]
	fields := strings.Split(argv, " ")
	sep := -1
	for i, field := range fields {
		if field == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(fields) || fields[sep+1] != "file.txt" {
		t.Errorf("argv = %q, want the files after a -- separator", argv)
	}
}

// TestAutoCommitChildCarriesTheLoopGuard covers the environment the commit
// tool receives: the guard variable is set, so a hook that runs selfdoc
// underneath it declines instead of recursing.
func TestAutoCommitChildCarriesTheLoopGuard(t *testing.T) {
	bin := isolate(t)
	log := filepath.Join(t.TempDir(), "env")
	fakeTool(t, bin, "rlsbl", "printf '%s' \"$SELFDOC_AUTO_COMMIT\" > "+quote(log))
	t.Setenv("SELFDOC_TEST_INHERITED", "inherited")
	dir := t.TempDir()
	initRepo(t, dir)
	write(t, dir, "file.txt", "data")

	if committed, _ := commit(t, []string{"file.txt"}, "msg", dir); !committed {
		t.Fatal("committed = false, want true")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading the recorded environment: %v", err)
	}
	if string(data) != "1" {
		t.Errorf("SELFDOC_AUTO_COMMIT in the child = %q, want \"1\"", data)
	}
}

// TestAutoCommitChildInheritsTheEnvironment covers the rest of that
// environment: the guard is added to this process's variables rather than
// replacing them, so a tool that reads HOME or PATH still works.
func TestAutoCommitChildInheritsTheEnvironment(t *testing.T) {
	bin := isolate(t)
	log := filepath.Join(t.TempDir(), "env")
	fakeTool(t, bin, "rlsbl", "printf '%s' \"$SELFDOC_TEST_INHERITED\" > "+quote(log))
	t.Setenv("SELFDOC_TEST_INHERITED", "inherited")
	dir := t.TempDir()
	initRepo(t, dir)
	write(t, dir, "file.txt", "data")

	if committed, _ := commit(t, []string{"file.txt"}, "msg", dir); !committed {
		t.Fatal("committed = false, want true")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading the recorded environment: %v", err)
	}
	if string(data) != "inherited" {
		t.Errorf("inherited variable in the child = %q, want \"inherited\"", data)
	}
}

// --- preview mode ----------------------------------------------------------

// TestAutoCommitUnderPreview covers the dry-run path for both the external
// tool and the plain-git fallback: the commit is recorded, nothing is
// committed, and the effect log names the commit that would happen.
func TestAutoCommitUnderPreview(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		wantVerbs []string
	}{
		{
			name:      "through an external tool",
			installed: "rlsbl",
			wantVerbs: []string{"rlsbl commit -m msg -- file.txt"},
		},
		{
			name:      "through plain git",
			wantVerbs: []string{"git add -- file.txt", "git commit -m msg -- file.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := isolate(t)
			var read func() []string
			if tt.installed != "" {
				read = recorder(t, bin, tt.installed)
			}
			dir := t.TempDir()
			initRepo(t, dir)
			write(t, dir, "file.txt", "data")

			var committed, unsettled bool
			var autoErr error
			app := strictcli.NewApp("gitcommittest", "0.0.0", "harness for auto-commit previews")
			app.Command("do", "run auto-commit",
				func(ctx *strictcli.Context, _ map[string]any) strictcli.Outcome {
					committed, unsettled, autoErr = AutoCommit(
						[]string{"file.txt"}, "msg", dir, effects.FromContext(ctx),
					)
					return strictcli.Exit(0)
				},
				strictcli.WithEffect(strictcli.EffectMutating),
			)
			app.Test([]string{"do", "--dry-run"})

			if autoErr != nil {
				t.Fatalf("AutoCommit: %v", autoErr)
			}
			if committed || !unsettled {
				t.Fatalf("committed = %v, unsettled = %v, want false and true", committed, unsettled)
			}
			if read != nil && len(read()) != 0 {
				t.Error("the recorded tool ran anyway")
			}
			assertNotTracked(t, dir, "file.txt")

			var details []string
			for _, record := range app.EffectLog() {
				details = append(details, record["detail"].(string))
			}
			joined := strings.Join(details, "\n")
			for _, want := range tt.wantVerbs {
				if !strings.Contains(joined, want) {
					t.Errorf("the effect log does not name %q:\n%s", want, joined)
				}
			}
		})
	}
}

// TestAutoCommitPreviewReadsStillExecute covers the read declarations: the
// probes that classify the files run in preview mode too, so a preview of a
// tree with nothing to commit records nothing at all.
func TestAutoCommitPreviewReadsStillExecute(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	initRepo(t, dir)

	var committed, unsettled bool
	app := strictcli.NewApp("gitcommittest", "0.0.0", "harness for auto-commit previews")
	app.Command("do", "run auto-commit",
		func(ctx *strictcli.Context, _ map[string]any) strictcli.Outcome {
			committed, unsettled, _ = AutoCommit(
				[]string{"README"}, "msg", dir, effects.FromContext(ctx),
			)
			return strictcli.Exit(0)
		},
		strictcli.WithEffect(strictcli.EffectMutating),
	)
	app.Test([]string{"do", "--dry-run"})

	if committed || unsettled {
		t.Fatalf("committed = %v, unsettled = %v, want both false", committed, unsettled)
	}
	if log := app.EffectLog(); len(log) != 0 {
		t.Errorf("the preview recorded %d effects, want none", len(log))
	}
}
