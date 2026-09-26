package gendata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/testisolation/go/hygiene"
)

// isolate binds the environment isolation floor and returns a directory at
// the FRONT of PATH, so a fake tool written there shadows the real one on the
// developer's machine (bwrap is commonly installed). The system directories
// stay on PATH behind it, because the fake tools are shell scripts and some of
// them run ordinary utilities.
//
// Nothing here calls t.Parallel: hygiene mutates process-wide variables.
func isolate(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	bin := testproject.Dir(t)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return bin
}

// isolateWithoutTools binds the isolation floor and points PATH at an empty
// directory, so nothing at all is installed as far as the code under test can
// see.
func isolateWithoutTools(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
	t.Setenv("PATH", t.TempDir())
}

// fakeTool writes an executable shell script named name into dir and returns
// its path. The tests use it to stand in for bwrap: the real sandbox needs
// user namespaces the suite cannot assume, and what these tests verify is the
// argv selfdoc builds and how it reads the run's outcome.
func fakeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
	return path
}

// script builds a well-formed declaration the way a decoded selfdoc.json
// carries one: every value an any, every array an []any.
func script(command, output string, mounts ...string) map[string]any {
	items := make([]any, 0, len(mounts))
	for _, mount := range mounts {
		items = append(items, mount)
	}
	return map[string]any{"command": command, "output": output, "mounts": items}
}

// config wraps declarations into the gen_data section of a selfdoc.json object.
func config(scripts ...map[string]any) map[string]any {
	items := make([]any, 0, len(scripts))
	for _, s := range scripts {
		items = append(items, s)
	}
	return map[string]any{"gen_data": map[string]any{"scripts": items}}
}

// --- no scripts to run -----------------------------------------------------

func TestGenerateDataWithNoScripts(t *testing.T) {
	isolateWithoutTools(t)
	tests := []struct {
		name   string
		config map[string]any
	}{
		{"no gen_data key", map[string]any{}},
		{"empty gen_data", map[string]any{"gen_data": map[string]any{}}},
		{"empty scripts list", config()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateData(tt.config, testproject.Dir(t), effects.Unbound())
			if err != nil {
				t.Fatalf("GenerateData: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("generated = %v, want none", got)
			}
		})
	}
}

// TestGenerateDataWithNoScriptsCreatesNothing covers the ordering: a config
// with nothing to run must not create the output directory or look for bwrap.
func TestGenerateDataWithNoScriptsCreatesNothing(t *testing.T) {
	isolateWithoutTools(t)
	base := testproject.Dir(t)
	if _, err := GenerateData(map[string]any{}, base, effects.Unbound()); err != nil {
		t.Fatalf("GenerateData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "stricttools", ".docs-state", "data")); !os.IsNotExist(err) {
		t.Errorf("the data directory exists after a run with no scripts (stat err = %v)", err)
	}
}

// --- missing bwrap ---------------------------------------------------------

func TestMissingBwrap(t *testing.T) {
	isolateWithoutTools(t)
	_, err := GenerateData(
		config(script("python3 scripts/test.py", "test.json", "src/")),
		testproject.Dir(t),
		effects.Unbound(),
	)
	var genErr *Error
	if !errors.As(err, &genErr) {
		t.Fatalf("GenerateData error = %v, want a *gendata.Error", err)
	}
	for _, want := range []string{
		"gen-data requires bubblewrap",
		"sudo dnf install bubblewrap (Fedora)",
		"sudo apt install bubblewrap (Debian/Ubuntu)",
	} {
		if !strings.Contains(genErr.Message, want) {
			t.Errorf("message %q does not contain %q", genErr.Message, want)
		}
	}
}

// --- script declaration validation -----------------------------------------

func TestValidateScript(t *testing.T) {
	tests := []struct {
		name    string
		script  map[string]any
		wantErr string
	}{
		{
			name:    "missing command",
			script:  map[string]any{"output": "out.json", "mounts": []any{}},
			wantErr: "script declaration missing required field(s): command",
		},
		{
			name:    "missing output",
			script:  map[string]any{"command": "echo hi", "mounts": []any{}},
			wantErr: "script declaration missing required field(s): output",
		},
		{
			name:    "missing mounts",
			script:  map[string]any{"command": "echo hi", "output": "out.json"},
			wantErr: "script declaration missing required field(s): mounts",
		},
		{
			name:    "missing every field, named in declaration order",
			script:  map[string]any{},
			wantErr: "script declaration missing required field(s): command, output, mounts",
		},
		{
			name:    "mounts is not a list",
			script:  map[string]any{"command": "echo hi", "output": "out.json", "mounts": "src/"},
			wantErr: "'mounts' must be a list of paths",
		},
		{
			name:   "a valid declaration",
			script: script("python3 test.py", "result.json", "src/"),
		},
		{
			name:   "mounts written as a Go string slice",
			script: map[string]any{"command": "echo hi", "output": "out.json", "mounts": []string{"src/"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScript(tt.script)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateScript: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validateScript error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// --- the bwrap argv --------------------------------------------------------

// TestBuildBwrapCommandWholeArgv pins the entire argument list, which is the
// sandbox's contract: an option that moves is an option that no longer applies
// to what follows it.
func TestBuildBwrapCommandWholeArgv(t *testing.T) {
	cmd, err := buildBwrapCommand(
		script("python3 scripts/extract.py", "targets.json", "selfdoc/", "docs/"),
		"/project", "/project/stricttools/.docs-state/data",
	)
	if err != nil {
		t.Fatalf("buildBwrapCommand: %v", err)
	}

	want := []string{
		"bwrap", "--die-with-parent", "--unshare-all", "--clearenv",
		"--ro-bind", "/project/selfdoc", "/project/selfdoc",
		"--ro-bind", "/project/docs", "/project/docs",
		"--bind", "/project/stricttools/.docs-state/data", "/project/stricttools/.docs-state/data",
	}
	for _, sysPath := range systemPaths {
		if _, err := os.Stat(sysPath); err == nil {
			want = append(want, "--ro-bind", sysPath, sysPath)
		}
	}
	want = append(want,
		"--proc", "/proc", "--dev", "/dev",
		"--chdir", "/project",
		"--", "python3", "scripts/extract.py",
	)

	if strings.Join(cmd, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n%v\nwant\n%v", cmd, want)
	}
}

func TestBuildBwrapCommandDetails(t *testing.T) {
	tests := []struct {
		name   string
		script map[string]any
		base   string
		output string
		check  func(t *testing.T, cmd []string)
	}{
		{
			name:   "a relative base directory is made absolute",
			script: script("echo hi", "out.json", "src/"),
			base:   ".",
			output: "stricttools/.docs-state/data",
			check: func(t *testing.T, cmd []string) {
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatalf("Getwd: %v", err)
				}
				assertFollows(t, cmd, "--chdir", cwd)
				assertBind(t, cmd, "--ro-bind", filepath.Join(cwd, "src"))
				assertBind(t, cmd, "--bind", filepath.Join(cwd, "stricttools", ".docs-state", "data"))
			},
		},
		{
			name:   "an absolute mount replaces the base directory",
			script: script("echo hi", "out.json", "/opt/shared"),
			base:   "/project",
			output: "/project/stricttools/.docs-state/data",
			check: func(t *testing.T, cmd []string) {
				assertBind(t, cmd, "--ro-bind", "/opt/shared")
			},
		},
		{
			name:   "a command is split on arbitrary whitespace",
			script: script("python3   -m  tool\tgo", "out.json"),
			base:   "/project",
			output: "/project/stricttools/.docs-state/data",
			check: func(t *testing.T, cmd []string) {
				sep := indexOf(t, cmd, "--")
				got := strings.Join(cmd[sep+1:], "|")
				if got != "python3|-m|tool|go" {
					t.Errorf("command tokens = %q", got)
				}
			},
		},
		{
			name:   "no mounts declared",
			script: script("echo hi", "out.json"),
			base:   "/project",
			output: "/project/stricttools/.docs-state/data",
			check: func(t *testing.T, cmd []string) {
				assertBind(t, cmd, "--bind", "/project/stricttools/.docs-state/data")
				if cmd[4] != "--bind" {
					t.Errorf("cmd[4] = %q, want --bind right after the isolation flags", cmd[4])
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := buildBwrapCommand(tt.script, tt.base, tt.output)
			if err != nil {
				t.Fatalf("buildBwrapCommand: %v", err)
			}
			tt.check(t, cmd)
		})
	}
}

// assertBind fails unless cmd carries flag followed by path twice -- the
// source and the destination of a bind, which selfdoc always sets equal.
func assertBind(t *testing.T, cmd []string, flag, path string) {
	t.Helper()
	for i, arg := range cmd {
		if arg != flag || i+2 >= len(cmd) || cmd[i+1] != path {
			continue
		}
		if cmd[i+2] != path {
			t.Errorf("%s %s destination = %q, want %q", flag, path, cmd[i+2], path)
		}
		return
	}
	t.Errorf("no %s %s %s in %v", flag, path, path, cmd)
}

// assertFollows fails unless cmd carries flag immediately followed by operand.
func assertFollows(t *testing.T, cmd []string, flag, operand string) {
	t.Helper()
	for i, arg := range cmd {
		if arg == flag && i+1 < len(cmd) && cmd[i+1] == operand {
			return
		}
	}
	t.Errorf("no %s %s in %v", flag, operand, cmd)
}

func indexOf(t *testing.T, cmd []string, want string) int {
	t.Helper()
	for i, arg := range cmd {
		if arg == want {
			return i
		}
	}
	t.Fatalf("no %q in %v", want, cmd)
	return -1
}

// --- output validation -----------------------------------------------------

func TestValidateOutput(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		write   bool
		wantErr string
	}{
		{name: "valid JSON", file: "data.json", content: `{"key": "value"}`, write: true},
		{name: "invalid JSON", file: "data.json", content: "{invalid json", write: true, wantErr: "is not valid JSON"},
		{name: "valid CSV", file: "data.csv", content: "name,value\na,1\nb,2\n", write: true},
		{name: "quoted CSV with embedded newlines", file: "data.csv", content: "a,\"b\nc\"\n", write: true},
		{name: "CSV with a NUL byte", file: "data.csv", content: "a,b\x00\n", write: true, wantErr: "line contains NUL"},
		{name: "unreadable file", file: "missing.json", wantErr: "cannot read output file"},
		{name: "unknown extension is not checked", file: "data.txt", content: "just some text", write: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.file)
			if tt.write {
				if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}
			err := validateOutput(path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateOutput: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateOutput error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidateOutputAcceptsWhatPythonAccepted covers the two shapes Go's own
// csv reader rejects and Python's does not: a bare quote inside a field, and
// rows of differing length.
func TestValidateOutputAcceptsWhatPythonAccepted(t *testing.T) {
	for _, content := range []string{
		"name,value\na,b\"c\n",
		"a,b,c\n1,2\n3,4,5,6\n",
	} {
		path := filepath.Join(t.TempDir(), "data.csv")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := validateOutput(path); err != nil {
			t.Errorf("validateOutput(%q) = %v, want nil", content, err)
		}
	}
}

func TestValidateOutputRejectsAnOversizedField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.csv")
	content := strings.Repeat("x", pythonCSVFieldLimit+1) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := validateOutput(path)
	if err == nil || !strings.Contains(err.Error(), "field larger than field limit (131072)") {
		t.Fatalf("validateOutput error = %v, want the field-limit refusal", err)
	}
}

// --- the whole flow --------------------------------------------------------

func TestGenerateData(t *testing.T) {
	tests := []struct {
		name string
		// bwrap is the fake sandbox runner's body. $out names the output
		// directory, which the test substitutes before writing the script.
		bwrap   string
		scripts []map[string]any
		wantLen int
		wantErr string
	}{
		{
			name:    "a successful run returns the produced path",
			bwrap:   `printf '{"targets": []}' > "$out/targets.json"`,
			scripts: []map[string]any{script("python3 scripts/extract.py", "targets.json", "selfdoc/")},
			wantLen: 1,
		},
		{
			name:    "a failing script names its exit code and stderr",
			bwrap:   `echo some error >&2; exit 1`,
			scripts: []map[string]any{script("python3 scripts/fail.py", "out.json")},
			wantErr: "script failed with exit code 1: python3 scripts/fail.py\nstderr: some error\n",
		},
		{
			name:    "a script that produces nothing is an error",
			bwrap:   `exit 0`,
			scripts: []map[string]any{script("python3 scripts/noop.py", "missing.json")},
			wantErr: "script did not produce expected output file:",
		},
		{
			name:    "invalid output is rejected",
			bwrap:   `printf '{oops' > "$out/bad.json"`,
			scripts: []map[string]any{script("python3 scripts/bad.py", "bad.json")},
			wantErr: "is not valid JSON",
		},
		{
			name:  "every declared script runs, in order",
			bwrap: `printf '{}' > "$out/${last%.py}.json"`,
			scripts: []map[string]any{
				script("python3 a.py", "a.json"),
				script("python3 b.py", "b.json"),
			},
			wantLen: 2,
		},
		{
			name:    "a declaration is validated before the sandbox runs",
			bwrap:   `exit 0`,
			scripts: []map[string]any{{"command": "echo hi", "output": "out.json"}},
			wantErr: "script declaration missing required field(s): mounts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := isolate(t)
			base := testproject.Dir(t)
			out := filepath.Join(base, "stricttools", ".docs-state", "data")
			// The fake runner receives bwrap's own argv. It exports the
			// output directory as $out and the last argument -- the script
			// file the sandboxed command names -- as $last, which is enough
			// for the multi-script case to tell its two runs apart without
			// parsing the sandbox flags.
			body := `out=` + shellQuote(out) + "\n" +
				`for a in "$@"; do last=$a; done` + "\n" + tt.bwrap
			fakeTool(t, bin, "bwrap", body)

			got, err := GenerateData(config(tt.scripts...), base, effects.Unbound())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GenerateData error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GenerateData: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("generated = %v, want %d paths", got, tt.wantLen)
			}
			for _, path := range got {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("generated path %s: %v", path, err)
				}
			}
		})
	}
}

// shellQuote renders s as a single-quoted shell word for the fake runner.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestGenerateDataCreatesTheOutputDirectory covers the makedirs that runs
// before the first script, so a script can write into a directory no one
// created by hand.
func TestGenerateDataCreatesTheOutputDirectory(t *testing.T) {
	bin := isolate(t)
	base := testproject.Dir(t)
	out := filepath.Join(base, "stricttools", ".docs-state", "data")
	fakeTool(t, bin, "bwrap", `printf '[]' > `+shellQuote(filepath.Join(out, "data.json")))

	got, err := GenerateData(config(script("python3 scripts/extract.py", "data.json")), base, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateData: %v", err)
	}
	if info, err := os.Stat(out); err != nil || !info.IsDir() {
		t.Fatalf("output directory: stat err = %v", err)
	}
	want := []string{filepath.Join(out, "data.json")}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("generated = %v, want %v", got, want)
	}
}

// TestGenerateDataReturnsPathsUnderTheGivenBase covers the return shape: the
// paths are the base directory joined with stricttools/.docs-state/data and the declared
// output name, so a relative base yields relative paths.
func TestGenerateDataReturnsPathsUnderTheGivenBase(t *testing.T) {
	bin := isolate(t)
	base := testproject.Dir(t)
	out := filepath.Join(base, "stricttools", ".docs-state", "data")
	fakeTool(t, bin, "bwrap", `printf '{}' > `+shellQuote(filepath.Join(out, "x.json")))

	hygiene.Chdir(t, base)

	got, err := GenerateData(config(script("python3 x.py", "x.json")), ".", effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateData: %v", err)
	}
	if len(got) != 1 || got[0] != "./stricttools/.docs-state/data/x.json" {
		t.Errorf("generated = %v, want [./stricttools/.docs-state/data/x.json]", got)
	}
}

// TestGenerateDataTimeout covers the mapping of a timed-out sandbox onto the
// message that names the limit. scriptTimeout is shortened here rather than
// waiting a real minute; the message the port emits is a fixed string, so it
// still reads 60 seconds.
func TestGenerateDataTimeout(t *testing.T) {
	bin := isolate(t)
	base := testproject.Dir(t)
	fakeTool(t, bin, "bwrap", "exec sleep 30")

	original := scriptTimeout
	scriptTimeout = 100 * time.Millisecond
	t.Cleanup(func() { scriptTimeout = original })

	_, err := GenerateData(config(script("python3 scripts/slow.py", "out.json")), base, effects.Unbound())
	want := "script timed out after 60 seconds: python3 scripts/slow.py"
	if err == nil || err.Error() != want {
		t.Fatalf("GenerateData error = %v, want %q", err, want)
	}
}

// TestGenerateDataUnderPreview covers the dry-run path: every sandbox run is
// recorded, the output directory is not created, no output is validated, and
// the returned paths name the files the scripts would have produced.
func TestGenerateDataUnderPreview(t *testing.T) {
	bin := isolate(t)
	base := testproject.Dir(t)
	fakeTool(t, bin, "bwrap", `printf 'ran' > `+shellQuote(filepath.Join(base, "ran")))

	var got []string
	var genErr error
	app := strictcli.NewApp("gendatatest", "0.0.0", "harness for gendata previews")
	app.Command("do", "run gen-data",
		func(ctx *strictcli.Context, _ map[string]any) strictcli.Outcome {
			got, genErr = GenerateData(
				config(script("python3 scripts/extract.py", "targets.json", "selfdoc/")),
				base,
				effects.FromContext(ctx),
			)
			return strictcli.Exit(0)
		},
		strictcli.WithEffect(strictcli.EffectMutating),
	)
	app.Test([]string{"do", "--dry-run"})

	if genErr != nil {
		t.Fatalf("GenerateData: %v", genErr)
	}
	want := filepath.Join(base, "stricttools", ".docs-state", "data", "targets.json")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("generated = %v, want [%s]", got, want)
	}
	if _, err := os.Stat(filepath.Join(base, "ran")); !os.IsNotExist(err) {
		t.Errorf("the recorded sandbox ran anyway (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(base, "stricttools", ".docs-state", "data")); !os.IsNotExist(err) {
		t.Errorf("the output directory was created in a preview (stat err = %v)", err)
	}
	log := app.EffectLog()
	if len(log) == 0 {
		t.Fatal("the preview recorded nothing")
	}
	var rendered []string
	for _, record := range log {
		rendered = append(rendered, fmt.Sprintf("%v: %v", record["verb"], record["detail"]))
	}
	joined := strings.Join(rendered, "\n")
	if !strings.Contains(joined, "bwrap") {
		t.Errorf("the effect log names no sandbox run:\n%s", joined)
	}
}

// TestGenerateDataMalformedShapes covers the declarations a decoded
// selfdoc.json could carry that the Python turned into an uncaught TypeError.
func TestGenerateDataMalformedShapes(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr string
	}{
		{
			name:    "gen_data is not an object",
			config:  map[string]any{"gen_data": "scripts"},
			wantErr: "'gen_data' must be an object",
		},
		{
			name:    "scripts is not a list",
			config:  map[string]any{"gen_data": map[string]any{"scripts": "one"}},
			wantErr: "'gen_data.scripts' must be a list of script declarations",
		},
		{
			name:    "a script declaration is not an object",
			config:  map[string]any{"gen_data": map[string]any{"scripts": []any{"echo hi"}}},
			wantErr: "script declaration 0 must be an object",
		},
		{
			name: "command is not a string",
			config: map[string]any{"gen_data": map[string]any{"scripts": []any{
				map[string]any{"command": 7, "output": "out.json", "mounts": []any{}},
			}}},
			wantErr: "'command' must be a string",
		},
		{
			name: "a mount is not a string",
			config: map[string]any{"gen_data": map[string]any{"scripts": []any{
				map[string]any{"command": "echo hi", "output": "out.json", "mounts": []any{7}},
			}}},
			wantErr: "mount 0 must be a path string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := isolate(t)
			fakeTool(t, bin, "bwrap", "exit 0")
			_, err := GenerateData(tt.config, testproject.Dir(t), effects.Unbound())
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("GenerateData error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
