package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/testisolation/go/hygiene"
)

// validated is a repository whose layout is exactly as declared: every claimed
// directory present with its manifest, the ignore file derived, nothing else
// inside.
func validated(t *testing.T) string {
	t.Helper()
	dir := owned(t)
	for _, declared := range Declared() {
		if err := EnsureDir(effects.Unbound(), dir, Root+"/"+declared.Name); err != nil {
			t.Fatalf("EnsureDir %s: %v", declared.Name, err)
		}
	}
	problems, err := Validate(dir)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("a freshly laid out repository has problems: %v", problems)
	}
	return dir
}

// problemsOf validates a repository and returns the problems of one check.
func problemsOf(t *testing.T, dir, check string) []Problem {
	t.Helper()
	problems, err := Validate(dir)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var matching []Problem
	for _, problem := range problems {
		if problem.Check == check {
			matching = append(matching, problem)
		}
	}
	return matching
}

// toolOnPath puts an executable of the given name at the front of PATH, so a
// manifest naming it names a tool this machine has.
func toolOnPath(t *testing.T, name string) {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing the stub tool: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestValidateRefusesARepositoryWithNoLayoutAtAll(t *testing.T) {
	hygiene.Isolate(t)
	_, err := Validate(t.TempDir())
	if err == nil {
		t.Fatal("a repository with no tool-state directory validated")
	}
	for _, want := range []string{Root, ManifestFileName, `owner = "selfdoc"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

func TestValidateReportsADirectoryWithNoManifest(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	if err := os.MkdirAll(filepath.Join(dir, Root, "unclaimed"), 0o755); err != nil {
		t.Fatal(err)
	}
	problems := problemsOf(t, dir, CheckOwnership)
	if len(problems) != 1 {
		t.Fatalf("ownership problems = %v, want the unowned directory's one", problems)
	}
	for _, want := range []string{Root + "/unclaimed/" + ManifestFileName, `owner = "<tool>"`} {
		if !strings.Contains(problems[0].Message, want) {
			t.Errorf("the problem does not carry %q: %s", want, problems[0].Message)
		}
	}
}

// A directory selfdoc claims is a directory selfdoc owns: a manifest naming
// anyone else there is a defect, however real that other tool is.
func TestValidateReportsAClaimedDirectoryAnotherToolOwns(t *testing.T) {
	hygiene.Isolate(t)
	toolOnPath(t, "othertool")
	dir := validated(t)
	grant(t, dir, PostsName, "othertool")

	problems := problemsOf(t, dir, CheckOwnership)
	if len(problems) != 1 {
		t.Fatalf("ownership problems = %v, want the claimed directory's one", problems)
	}
	for _, want := range []string{filepath.Join(Root, PostsName), "othertool", `owner = "selfdoc"`} {
		if !strings.Contains(problems[0].Message, want) {
			t.Errorf("the problem does not carry %q: %s", want, problems[0].Message)
		}
	}
}

// An owner is selfdoc itself or a name PATH answers with an executable. A
// directory selfdoc does not claim may be owned by any such tool.
func TestValidateAcceptsAnUnclaimedDirectoryOwnedByAToolOnPath(t *testing.T) {
	hygiene.Isolate(t)
	toolOnPath(t, "othertool")
	dir := validated(t)
	grant(t, dir, "other-state", "othertool")

	if problems := problemsOf(t, dir, CheckOwnership); len(problems) != 0 {
		t.Errorf("ownership problems = %v, want none for a tool this machine has", problems)
	}
}

func TestValidateReportsAnOwnerThisMachineDoesNotHave(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	grant(t, dir, "other-state", "no-such-tool-on-this-machine")

	problems := problemsOf(t, dir, CheckOwnership)
	if len(problems) != 1 {
		t.Fatalf("ownership problems = %v, want the unknown owner's one", problems)
	}
	for _, want := range []string{"no-such-tool-on-this-machine", "PATH"} {
		if !strings.Contains(problems[0].Message, want) {
			t.Errorf("the problem does not carry %q: %s", want, problems[0].Message)
		}
	}
}

func TestValidateReportsAFileWhereADirectoryBelongs(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	write(t, filepath.Join(dir, Root, "notes.txt"), "loose\n")

	problems := problemsOf(t, dir, CheckOwnership)
	if len(problems) != 1 {
		t.Fatalf("ownership problems = %v, want the loose file's one", problems)
	}
	if !strings.Contains(problems[0].Message, "notes.txt") {
		t.Errorf("the problem does not name the file: %s", problems[0].Message)
	}
}

func TestValidateReportsAGeneratedPageInAHandwrittenDirectory(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	write(t, filepath.Join(Path(dir, DocsRel), "api.md"),
		"+++\ntitle = \"API\"\n+++\n"+GeneratedMarkerPrefix+", do not edit -->\n\n# API\n")
	problems := problemsOf(t, dir, CheckSide)
	if len(problems) != 1 {
		t.Fatalf("side problems = %v, want the generated page's one", problems)
	}
	for _, want := range []string{"docs/api.md", GeneratedPagesRel} {
		if !strings.Contains(problems[0].Message, want) {
			t.Errorf("the problem does not carry %q: %s", want, problems[0].Message)
		}
	}
}

func TestValidateReportsAHandwrittenPageInAGeneratedDirectory(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	write(t, filepath.Join(Path(dir, GeneratedPagesRel), "notes.md"),
		"+++\ntitle = \"Notes\"\n+++\n\n# Notes\n")
	problems := problemsOf(t, dir, CheckSide)
	if len(problems) != 1 {
		t.Fatalf("side problems = %v, want the handwritten page's one", problems)
	}
	for _, want := range []string{"notes.md", DocsRel} {
		if !strings.Contains(problems[0].Message, want) {
			t.Errorf("the problem does not carry %q: %s", want, problems[0].Message)
		}
	}
}

func TestValidateReportsAHiddenEntry(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	write(t, filepath.Join(Path(dir, DocsRel), ".notes.md"), "# Hidden\n")
	if err := os.MkdirAll(filepath.Join(dir, Root, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	problems := problemsOf(t, dir, CheckHidden)
	if len(problems) != 2 {
		t.Fatalf("hidden problems = %v, want both dotted entries", problems)
	}
	joined := problems[0].Message + problems[1].Message
	for _, want := range []string{".cache", ".notes.md"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the problems do not name %q: %s", want, joined)
		}
	}
	// The derived ignore file is the one hidden entry that is allowed.
	if strings.Count(joined, IgnoreFileName+" starts with a dot") != 0 {
		t.Errorf("the derived ignore file was reported: %s", joined)
	}
	// A dotted entry breaks one rule, not two: the ownership rule leaves it
	// to the rule it actually breaks.
	if ownership := problemsOf(t, dir, CheckOwnership); len(ownership) != 0 {
		t.Errorf("ownership problems = %v, want the dotted entry reported once", ownership)
	}
}

func TestValidateReportsAStaleIgnoreFile(t *testing.T) {
	hygiene.Isolate(t)
	dir := validated(t)
	write(t, IgnorePath(dir), "# BEGIN othertool\nother/\n# END othertool\n")
	problems := problemsOf(t, dir, CheckIgnore)
	if len(problems) != 1 {
		t.Fatalf("ignore problems = %v, want the stale file's one", problems)
	}
	if !strings.Contains(problems[0].Message, DocsCacheName+"/*") {
		t.Errorf("the problem does not carry the content it should hold: %s", problems[0].Message)
	}

	// The remedy: writing the file through the layout clears the problem,
	// and the other tool's lines are still there.
	if err := WriteIgnore(effects.Unbound(), dir); err != nil {
		t.Fatalf("WriteIgnore: %v", err)
	}
	if remaining := problemsOf(t, dir, CheckIgnore); len(remaining) != 0 {
		t.Errorf("the remedy did not clear the problem: %v", remaining)
	}
	if ignore := read(t, IgnorePath(dir)); !strings.Contains(ignore, "other/") {
		t.Errorf("the other tool's lines were dropped:\n%s", ignore)
	}
}
