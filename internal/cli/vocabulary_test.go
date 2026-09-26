package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// The vocabulary commands, driven the way an agent drives them: the remedy a
// check diagnostic prints is run as printed, and the diagnostic clears.

// vocabularyCLIProject is a project whose one page says body.
func vocabularyCLIProject(t *testing.T, body string) string {
	t.Helper()
	dir := testproject.Make(t, nil)
	writeText(t, filepath.Join(testproject.DocsDir(dir), "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \"A page of ordinary prose that says something "+
			"concrete about the project and its documentation for the reader.\"\n+++\n\n# Home\n\n"+body+"\n")
	return dir
}

// diagnosticsOf runs the check and returns its stderr and stdout together.
func diagnosticsOf(t *testing.T, dir string) string {
	t.Helper()
	result := run(t, dir, "check", "--no-auto-commit")
	return result.Stdout + result.Stderr
}

// remedyCommandPattern finds a quoted selfdoc vocabulary command in a
// diagnostic.
var remedyCommandPattern = regexp.MustCompile(`'(selfdoc vocabulary [^']+)'`)

// runRemedy runs the first printed vocabulary command whose text starts with
// prefix, with every <text> placeholder filled in, and fails unless it
// succeeds.
func runRemedy(t *testing.T, dir, diagnostics, prefix string) {
	t.Helper()
	for _, match := range remedyCommandPattern.FindAllStringSubmatch(diagnostics, -1) {
		if !strings.HasPrefix(match[1], prefix) {
			continue
		}
		argv := strings.Fields(strings.ReplaceAll(match[1], "<text>", "Filled-in."))[1:]
		argv = append(argv, "--no-auto-commit")
		if result := run(t, dir, argv...); result.ExitCode != 0 {
			t.Fatalf("the printed remedy %q failed:\n%s", match[1], result.Stderr)
		}
		return
	}
	t.Fatalf("no printed remedy starts with %q in:\n%s", prefix, diagnostics)
}

func TestTheSpellRemedyRunsAsPrintedAndClears(t *testing.T) {
	isolate(t)
	dir := vocabularyCLIProject(t, "The frobnitz turns.")
	diagnostics := diagnosticsOf(t, dir)
	if !strings.Contains(diagnostics, "SPELL001") {
		t.Fatalf("no SPELL001:\n%s", diagnostics)
	}
	runRemedy(t, dir, diagnostics, "selfdoc vocabulary accept frobnitz")
	if after := diagnosticsOf(t, dir); strings.Contains(after, "SPELL001") {
		t.Errorf("SPELL001 is still reported:\n%s", after)
	}
	terms := readText(t, layout.Path(dir, layout.TermsRel))
	if !strings.Contains(terms, `word = "frobnitz"`) || !strings.Contains(terms, `meaning = "Filled-in."`) {
		t.Errorf("terms.toml =\n%s", terms)
	}
}

func TestThePendingRemediesRunAsPrintedAndClear(t *testing.T) {
	for _, prefix := range []string{
		"selfdoc vocabulary approve frobnitz --meaning",
		"selfdoc vocabulary approve frobnitz",
	} {
		t.Run(prefix, func(t *testing.T) {
			isolate(t)
			dir := vocabularyCLIProject(t, "The frobnitz turns.")
			writeText(t, layout.Path(dir, layout.ReviewRel), vocabulary.EmptyReview+`
[[pending]]
word = "frobnitz"
meaning = "The widget that turns."
confidence = 0.9
evidence = ["stricttools/docs/index.md:9: The frobnitz turns."]
`)
			diagnostics := diagnosticsOf(t, dir)
			runRemedy(t, dir, diagnostics, prefix)
			if after := diagnosticsOf(t, dir); strings.Contains(after, "SPELL001") {
				t.Errorf("SPELL001 is still reported:\n%s", after)
			}
		})
	}
}

func TestTheDropRemedyRunsAsPrinted(t *testing.T) {
	isolate(t)
	dir := vocabularyCLIProject(t, "The frobnitz turns.")
	writeText(t, layout.Path(dir, layout.ReviewRel), vocabulary.EmptyReview+`
[[pending]]
word = "frobnitz"
meaning = "The widget that turns."
confidence = 0.2
evidence = ["stricttools/docs/index.md:9: The frobnitz turns."]
`)
	runRemedy(t, dir, diagnosticsOf(t, dir), "selfdoc vocabulary drop frobnitz")
	if review := readText(t, layout.Path(dir, layout.ReviewRel)); strings.Contains(review, "frobnitz") {
		t.Errorf("review.toml still carries the word:\n%s", review)
	}
}

func TestTheRemoveRemediesRunAsPrintedAndClear(t *testing.T) {
	isolate(t)
	dir := vocabularyCLIProject(t, "We leverage plain words.")
	if result := run(t, dir, "vocabulary", "accept", "frobnitz", "--meaning", "The widget.", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("accept failed:\n%s", result.Stderr)
	}
	if result := run(t, dir, "vocabulary", "reject", "leverage", "--kind", "word", "--reason", "Say use.", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("reject failed:\n%s", result.Stderr)
	}
	diagnostics := diagnosticsOf(t, dir)
	for _, code := range []string{"VOCAB001", "VOCAB004"} {
		if !strings.Contains(diagnostics, code) {
			t.Fatalf("no %s:\n%s", code, diagnostics)
		}
	}
	runRemedy(t, dir, diagnostics, "selfdoc vocabulary remove frobnitz")
	runRemedy(t, dir, diagnostics, "selfdoc vocabulary remove leverage")
	after := diagnosticsOf(t, dir)
	for _, code := range []string{"VOCAB001", "VOCAB004"} {
		if strings.Contains(after, code) {
			t.Errorf("%s is still reported:\n%s", code, after)
		}
	}
}

func TestVocabularyCommandsRefuseByName(t *testing.T) {
	isolate(t)
	dir := vocabularyCLIProject(t, "Plain words.")
	for _, testCase := range []struct {
		argv []string
		want string
	}{
		{[]string{"vocabulary", "accept", "frobnitz", "--no-auto-commit"}, "meaning"},
		{[]string{"vocabulary", "reject", "x", "--kind", "regex", "--reason", "r", "--no-auto-commit"}, "regex"},
		{[]string{"vocabulary", "remove", "absent", "--no-auto-commit"}, "no entry"},
		{[]string{"vocabulary", "approve", "absent", "--no-auto-commit"}, "not pending"},
		{[]string{"vocabulary", "drop", "absent", "--no-auto-commit"}, "not pending"},
	} {
		result := run(t, dir, testCase.argv...)
		if result.ExitCode == 0 || !strings.Contains(result.Stderr, testCase.want) {
			t.Errorf("%v: exit %d, stderr:\n%s", testCase.argv, result.ExitCode, result.Stderr)
		}
	}
}

func TestVocabularyDryRunWritesNothing(t *testing.T) {
	isolate(t)
	dir := vocabularyCLIProject(t, "The frobnitz turns.")
	result := run(t, dir, "vocabulary", "accept", "frobnitz", "--meaning", "The widget.", "--dry-run")
	if result.ExitCode != 0 {
		t.Fatalf("the dry run failed:\n%s", result.Stderr)
	}
	if exists(layout.Path(dir, layout.TermsRel)) {
		t.Error("the dry run wrote the terms file")
	}
}
