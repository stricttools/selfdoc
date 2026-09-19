package quality

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/stricttest/go/hygiene"
)

// isolate binds the environment-isolation floor and puts an empty directory at
// the front of PATH, so the only dirstat any test can reach is the one it
// writes there itself. The directory is returned.
//
// Nothing here calls t.Parallel: hygiene mutates process-wide variables, and
// so does PATH.
func isolate(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	bin := t.TempDir()
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

// fakeDirstat writes an executable shell script named dirstat into dir. The
// real binary is not a dependency of this suite: what these tests verify is
// the argv selfdoc builds and how it reads the answer.
func fakeDirstat(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "dirstat")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing the fake dirstat: %v", err)
	}
}

// envelope wraps a scan document in the strictcli machine envelope dirstat
// answers with.
func envelope(payload string) string {
	return `{"interface_version":1,"app":"dirstat","app_version":"0.3.0",` +
		`"command":"scan","exit_code":0,"payload":` + payload + `,` +
		`"dry_run":false,"preview":[],"preview_error":null,"diagnostics":[]}`
}

// answering writes a fake dirstat that prints text and exits zero.
func answering(t *testing.T, dir, text string) {
	t.Helper()
	fakeDirstat(t, dir, "cat <<'ANSWER'\n"+text+"\nANSWER")
}

func TestReadsThePayloadOfTheEnvelope(t *testing.T) {
	// The scan document is the envelope's payload member.
	bin := isolate(t)
	project := t.TempDir()
	answering(t, bin, envelope(`{"root":"/x","method":"hybrid","summary":{},"groups":[
		{"format":"py","text":true,"count":3,"total_loc":120},
		{"format":"md","text":true,"count":1,"total_loc":900}]}`))

	loc, files, err := CodeLOC(project, nil, effects.Unbound())
	if err != nil {
		t.Fatalf("CodeLOC: %v", err)
	}
	if loc != 120 || files != 3 {
		t.Errorf("CodeLOC = (%d, %d), want (120, 3)", loc, files)
	}
}

func TestTheArgvIsTheScanTheReportNeeds(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	record := filepath.Join(t.TempDir(), "argv")
	fakeDirstat(t, bin, `printf '%s\n' "$*" > `+record+`
cat <<'ANSWER'
`+envelope(`{"groups":[]}`)+`
ANSWER`)

	if _, _, err := CodeLOC(project, nil, effects.Unbound()); err != nil {
		t.Fatalf("CodeLOC: %v", err)
	}
	recorded, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("reading the recorded argv: %v", err)
	}
	want := "scan " + project + " --json --type text --stats count --stats total-loc\n"
	if string(recorded) != want {
		t.Errorf("argv = %q, want %q", recorded, want)
	}
}

func TestANonzeroExitIsAHardError(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	fakeDirstat(t, bin, "echo 'error: unknown flag --output' >&2\nexit 2")

	_, _, err := CodeLOC(project, nil, effects.Unbound())
	want := "dirstat scan of " + project + " exited 2: error: unknown flag --output"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	var dirstatError *DirstatError
	if !errors.As(err, &dirstatError) {
		t.Fatalf("error type = %T, want *quality.DirstatError", err)
	}
}

func TestANonzeroExitWithNoOutputStillSaysSomething(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	fakeDirstat(t, bin, "exit 3")

	_, _, err := CodeLOC(project, nil, effects.Unbound())
	want := "dirstat scan of " + project + " exited 3: no error output"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestMalformedAnswersAreHardErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "output that is not JSON",
			body: "not json at all",
			want: "produced output that is not valid JSON: ",
		},
		{
			name: "an envelope that is not an object",
			body: "[]",
			want: "did not answer with an envelope carrying a payload",
		},
		{
			name: "an envelope with no payload member",
			body: `{"interface_version":1}`,
			want: "did not answer with an envelope carrying a payload",
		},
		{
			name: "an envelope whose payload is null",
			body: envelope("null"),
			want: "answered with an empty payload",
		},
		{
			name: "a payload with no groups array",
			body: envelope(`{"root":"/x"}`),
			want: "answered with no groups array",
		},
		{
			name: "a payload whose groups are not an array",
			body: envelope(`{"groups":{}}`),
			want: "answered with no groups array",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			bin := isolate(t)
			project := t.TempDir()
			answering(t, bin, testCase.body)

			_, _, err := CodeLOC(project, nil, effects.Unbound())
			if err == nil {
				t.Fatalf("an answer of %q was accepted", testCase.body)
			}
			var dirstatError *DirstatError
			if !errors.As(err, &dirstatError) {
				t.Fatalf("error type = %T, want *quality.DirstatError", err)
			}
			want := "dirstat scan of " + project + " " + testCase.want
			if !strings.HasPrefix(err.Error(), want) {
				t.Errorf("error = %q, want a message starting %q", err, want)
			}
		})
	}
}

func TestATimeoutIsAHardError(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	fakeDirstat(t, bin, "exec sleep 5")

	previous := scanTimeout
	scanTimeout = 50 * time.Millisecond
	t.Cleanup(func() { scanTimeout = previous })

	_, _, err := CodeLOC(project, nil, effects.Unbound())
	want := "dirstat scan of " + project + " timed out after 60s"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestAnUninstalledDirstatIsAHardError(t *testing.T) {
	isolateWithoutTools(t)
	project := t.TempDir()

	_, _, err := CodeLOC(project, nil, effects.Unbound())
	if err == nil {
		t.Fatal("a scan with no dirstat installed succeeded")
	}
	var dirstatError *DirstatError
	if !errors.As(err, &dirstatError) {
		t.Fatalf("error type = %T, want *quality.DirstatError", err)
	}
	if !strings.HasPrefix(err.Error(), "dirstat is not installed: ") {
		t.Errorf("error = %q, want a message starting \"dirstat is not installed: \"", err)
	}
}

func TestAFailingSubmoduleSubtractionIsAHardError(t *testing.T) {
	// The inner scan is not a lesser scan: a broken one falsifies the total.
	bin := isolate(t)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "vendor"), 0o755); err != nil {
		t.Fatalf("writing the submodule: %v", err)
	}
	fakeDirstat(t, bin, `case "$2" in
*/vendor) echo boom >&2; exit 1 ;;
*) cat <<'ANSWER'
`+envelope(`{"groups":[{"format":"py","text":true,"count":2,"total_loc":200}]}`)+`
ANSWER
;;
esac`)

	_, _, err := CodeLOC(project, []string{"vendor"}, effects.Unbound())
	if err == nil {
		t.Fatal("a failing submodule scan was swallowed")
	}
	var dirstatError *DirstatError
	if !errors.As(err, &dirstatError) {
		t.Fatalf("error type = %T, want *quality.DirstatError", err)
	}
}

func TestASubmoduleSubtractionReducesTheTotals(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "vendor"), 0o755); err != nil {
		t.Fatalf("writing the submodule: %v", err)
	}
	fakeDirstat(t, bin, `case "$2" in
*/vendor) cat <<'INNER'
`+envelope(`{"groups":[{"format":"py","text":true,"count":1,"total_loc":50}]}`)+`
INNER
;;
*) cat <<'OUTER'
`+envelope(`{"groups":[{"format":"py","text":true,"count":4,"total_loc":200}]}`)+`
OUTER
;;
esac`)

	loc, files, err := CodeLOC(project, []string{"vendor"}, effects.Unbound())
	if err != nil {
		t.Fatalf("CodeLOC: %v", err)
	}
	if loc != 150 || files != 3 {
		t.Errorf("CodeLOC = (%d, %d), want (150, 3)", loc, files)
	}
}

func TestASubmodulePathThatIsNotThereIsNotScanned(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	fakeDirstat(t, bin, `case "$2" in
*/vendor) echo boom >&2; exit 1 ;;
*) cat <<'ANSWER'
`+envelope(`{"groups":[{"format":"go","text":true,"count":2,"total_loc":200}]}`)+`
ANSWER
;;
esac`)

	loc, files, err := CodeLOC(project, []string{"vendor"}, effects.Unbound())
	if err != nil {
		t.Fatalf("CodeLOC: %v", err)
	}
	if loc != 200 || files != 2 {
		t.Errorf("CodeLOC = (%d, %d), want (200, 2)", loc, files)
	}
}

func TestOnlyCodeFormatsCountAsCode(t *testing.T) {
	bin := isolate(t)
	project := t.TempDir()
	answering(t, bin, envelope(`{"groups":[
		{"format":"GO","count":2,"total_loc":100},
		{"format":"md","count":9,"total_loc":9000},
		{"format":"json","count":3,"total_loc":300},
		{"format":"","count":1,"total_loc":7},
		{"format":"makefile","count":1,"total_loc":20}]}`))

	loc, files, err := CodeLOC(project, nil, effects.Unbound())
	if err != nil {
		t.Fatalf("CodeLOC: %v", err)
	}
	if loc != 120 || files != 3 {
		t.Errorf("CodeLOC = (%d, %d), want (120, 3) -- only go and makefile are code", loc, files)
	}
}

func TestCheckDirstatRefusesOnlyAnAbsentBinary(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		isolateWithoutTools(t)
		err := CheckDirstat(effects.Unbound())
		var missing *DirstatMissingError
		if !errors.As(err, &missing) {
			t.Fatalf("error = %v (%T), want *quality.DirstatMissingError", err, err)
		}
		want := "error: dirstat is not installed\n" +
			"install: go install github.com/smm-h/dirstat/cmd/dirstat@v0"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err, want)
		}
	})

	t.Run("present but failing its own probe", func(t *testing.T) {
		// A non-zero exit still proves the binary exists.
		bin := isolate(t)
		fakeDirstat(t, bin, "echo 'no such command' >&2\nexit 64")
		if err := CheckDirstat(effects.Unbound()); err != nil {
			t.Errorf("CheckDirstat = %v, want nil", err)
		}
	})

	t.Run("present", func(t *testing.T) {
		bin := isolate(t)
		fakeDirstat(t, bin, "exit 0")
		if err := CheckDirstat(effects.Unbound()); err != nil {
			t.Errorf("CheckDirstat = %v, want nil", err)
		}
	})
}
