package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// vocabularyPage is a page whose one sentence of prose is body.
func vocabularyPage(body string) string {
	return "+++\ntitle = \"Home\"\ndescription = \"A page of ordinary prose " +
		"that says something concrete about the project and its " +
		"documentation for the reader.\"\n+++\n\n# Home\n\n" + body + "\n"
}

// vocabularyProject is a project with one page and the given terms file (none
// when empty), which has granted selfdoc every directory so the vocabulary
// commands may write.
func vocabularyProject(t *testing.T, body, terms string) string {
	t.Helper()
	root := spellProject(t, map[string]string{"index.md": vocabularyPage(body)})
	testproject.Manifests(t, root)
	if terms != "" {
		write(t, layout.Path(root, layout.TermsRel), terms)
	}
	return root
}

// onlyLint returns the one diagnostic of a code, failing when there is not
// exactly one.
func onlyLint(t *testing.T, root, code string) string {
	t.Helper()
	matching := withCode(checkFixture(t, root).Lints, code)
	if len(matching) != 1 {
		t.Fatalf("%s count = %d, want 1: %v", code, len(matching), messagesOf(matching))
	}
	return matching[0].Message()
}

// noLint fails when a run reports any diagnostic of a code.
func noLint(t *testing.T, root, code string) {
	t.Helper()
	if matching := withCode(checkFixture(t, root).Lints, code); len(matching) != 0 {
		t.Errorf("%s fired: %v", code, messagesOf(matching))
	}
}

// SPELL001's remedy names the accept command and the project's terms file,
// and running it clears the finding.
func TestSpellRemedyNamesAcceptAndClears(t *testing.T) {
	root := vocabularyProject(t, "The frobnitz turns.", "")
	message := onlyLint(t, root, "SPELL001")
	for _, want := range []string{"selfdoc vocabulary accept frobnitz --meaning", layout.TermsRel} {
		if !strings.Contains(message, want) {
			t.Errorf("the remedy does not carry %q: %s", want, message)
		}
	}
	if _, err := vocabulary.Accept(effects.Unbound(), root, "frobnitz", "The widget that turns."); err != nil {
		t.Fatalf("the remedy failed: %v", err)
	}
	noLint(t, root, "SPELL001")
}

// The spell check reads no file outside the repository: a machine-wide list
// in the home directory accepts nothing.
func TestSpellCheckReadsNoMachineList(t *testing.T) {
	root := vocabularyProject(t, "The frobnitz turns.", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, "Projects", "ark", "spelling-accept.txt"), "frobnitz\n")
	onlyLint(t, root, "SPELL001")
}

// A word pending review is still unknown; its remedy names the review
// commands, and each of them resolves it.
func TestAPendingWordIsUnknownAndItsRemedyNamesTheReviewCommands(t *testing.T) {
	pending := vocabulary.EmptyReview + `
[[pending]]
word = "frobnitz"
meaning = "The widget that turns."
confidence = 0.7
evidence = ["stricttools/docs/index.md:7: The frobnitz turns."]
`
	for _, testCase := range []struct {
		name   string
		remedy func(root string) error
		spell  bool
	}{
		{"approve", func(root string) error {
			_, err := vocabulary.Approve(effects.Unbound(), root, "frobnitz", "")
			return err
		}, false},
		{"approve with a corrected meaning", func(root string) error {
			_, err := vocabulary.Approve(effects.Unbound(), root, "frobnitz", "The part that turns.")
			return err
		}, false},
		{"drop", func(root string) error {
			_, err := vocabulary.Drop(effects.Unbound(), root, "frobnitz")
			return err
		}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := vocabularyProject(t, "The frobnitz turns.", "")
			write(t, layout.Path(root, layout.ReviewRel), pending)
			message := onlyLint(t, root, "SPELL001")
			for _, want := range []string{
				layout.ReviewRel,
				"selfdoc vocabulary approve frobnitz'",
				"selfdoc vocabulary approve frobnitz --meaning",
				"selfdoc vocabulary drop frobnitz",
			} {
				if !strings.Contains(message, want) {
					t.Errorf("the remedy does not carry %q: %s", want, message)
				}
			}
			if err := testCase.remedy(root); err != nil {
				t.Fatalf("the remedy failed: %v", err)
			}
			if testCase.spell {
				// Dropped, the word is an ordinary unknown word again,
				// with the accept remedy.
				if message := onlyLint(t, root, "SPELL001"); !strings.Contains(message, "selfdoc vocabulary accept frobnitz") {
					t.Errorf("after the drop, the remedy is %s", message)
				}
				return
			}
			noLint(t, root, "SPELL001")
		})
	}
}

func TestAnUnusedAcceptedWordIsReportedAndItsRemedyClears(t *testing.T) {
	root := vocabularyProject(t, "Plain words only.", vocabulary.EmptyTerms+`
[[accepted]]
word = "frobnitz"
meaning = "The widget that turns."
`)
	message := onlyLint(t, root, "VOCAB001")
	if !strings.Contains(message, "selfdoc vocabulary remove frobnitz") {
		t.Errorf("the remedy does not name the remove command: %s", message)
	}
	if _, err := vocabulary.Remove(effects.Unbound(), root, "frobnitz"); err != nil {
		t.Fatalf("the remedy failed: %v", err)
	}
	noLint(t, root, "VOCAB001")
}

func TestAnAcceptedWordAPageUsesThroughAnAliasIsUsed(t *testing.T) {
	root := vocabularyProject(t, "The Frobnitzes turn.", vocabulary.EmptyTerms+`
[[accepted]]
word = "frobnitz"
meaning = "The widget that turns."
aliases = ["frobnitzes"]
`)
	noLint(t, root, "VOCAB001")
	noLint(t, root, "SPELL001")
}

func TestADuplicateEntryIsReportedAndItsRemedyClears(t *testing.T) {
	root := vocabularyProject(t, "The frobnitz turns.", vocabulary.EmptyTerms+`
[[accepted]]
word = "frobnitz"
meaning = "The widget that turns."

[[accepted]]
word = "Frobnitz"
meaning = "The same widget."
`)
	message := onlyLint(t, root, "VOCAB002")
	if !strings.Contains(message, "selfdoc vocabulary remove Frobnitz") {
		t.Errorf("the remedy does not name the remove command: %s", message)
	}
	if _, err := vocabulary.Remove(effects.Unbound(), root, "Frobnitz"); err != nil {
		t.Fatalf("the remedy failed: %v", err)
	}
	if _, err := vocabulary.Accept(effects.Unbound(), root, "frobnitz", "The widget that turns."); err != nil {
		t.Fatalf("adding the entry back failed: %v", err)
	}
	noLint(t, root, "VOCAB002")
}

func TestAnAcceptedWordARejectedSuffixCoversIsReportedAndItsRemedyClears(t *testing.T) {
	root := vocabularyProject(t, "The diamond-shaped part turns.", vocabulary.EmptyTerms+`
[[accepted]]
word = "diamond-shaped"
meaning = "Shaped like a diamond."

[[rejected]]
pattern = "-shaped"
kind = "suffix"
reason = "Say what the thing is."
`)
	message := onlyLint(t, root, "VOCAB003")
	for _, want := range []string{"selfdoc vocabulary remove diamond-shaped", "selfdoc vocabulary remove -shaped"} {
		if !strings.Contains(message, want) {
			t.Errorf("the remedy does not carry %q: %s", want, message)
		}
	}
	if _, err := vocabulary.Remove(effects.Unbound(), root, "diamond-shaped"); err != nil {
		t.Fatalf("the remedy failed: %v", err)
	}
	noLint(t, root, "VOCAB003")
}

func TestARejectedTermInAPageIsReportedAndItsRemedyClears(t *testing.T) {
	root := vocabularyProject(t, "We leverage the frobnitz, not `leverage` in code.", vocabulary.EmptyTerms+`
[[accepted]]
word = "frobnitz"
meaning = "The widget that turns."

[[rejected]]
pattern = "leverage"
kind = "word"
reason = "Say use."
`)
	message := onlyLint(t, root, "VOCAB004")
	for _, want := range []string{"'leverage' (col 4)", "Say use.", "selfdoc vocabulary remove leverage"} {
		if !strings.Contains(message, want) {
			t.Errorf("the diagnostic does not carry %q: %s", want, message)
		}
	}
	if _, err := vocabulary.Remove(effects.Unbound(), root, "leverage"); err != nil {
		t.Fatalf("the remedy failed: %v", err)
	}
	noLint(t, root, "VOCAB004")
}

func TestAnUnsortedArrayIsReported(t *testing.T) {
	root := vocabularyProject(t, "The frobnitz and the widget turn.", vocabulary.EmptyTerms+`
[[accepted]]
word = "zeta"
meaning = "The last."

[[accepted]]
word = "frobnitz"
meaning = "The widget that turns."
`)
	matching := withCode(checkFixture(t, root).Lints, "VOCAB005")
	if len(matching) != 1 || !strings.Contains(matching[0].Message(), `"frobnitz" sorts before "zeta"`) {
		t.Fatalf("VOCAB005 = %v", messagesOf(matching))
	}
	if line := matching[0].Line(); line == nil || *line != 7 {
		t.Errorf("VOCAB005 line = %v, want 7", line)
	}
}

// A word both accepted and rejected stops the check before any page is
// judged, naming both entries.
func TestAConflictingVocabularyStopsTheCheck(t *testing.T) {
	root := vocabularyProject(t, "Plain words.", vocabulary.EmptyTerms+`
[[accepted]]
word = "leverage"
meaning = "The lever's advantage."

[[rejected]]
pattern = "leverage"
kind = "word"
reason = "Say use."
`)
	_, err := CheckDocs(root, nil, false, "", "", handle())
	if err == nil || !strings.Contains(err.Error(), "both accepted") {
		t.Fatalf("err = %v, want the conflict refusal", err)
	}
}
