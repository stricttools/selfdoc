package vocabulary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// project is a repository that has granted selfdoc every directory, with the
// given terms file (none when empty).
func project(t *testing.T, terms string) string {
	t.Helper()
	hygiene.Isolate(t)
	dir := testproject.Dir(t)
	if terms != "" {
		testproject.WriteText(t, layout.Path(dir, layout.TermsRel), terms)
	}
	return dir
}

func readTerms(t *testing.T, dir string) string {
	t.Helper()
	return testproject.ReadText(t, layout.Path(dir, layout.TermsRel))
}

func TestTheBaselineIsAValidEmptyList(t *testing.T) {
	hygiene.Isolate(t)
	baseline, err := LoadBaseline()
	if err != nil {
		t.Fatalf("the embedded baseline does not load: %v", err)
	}
	if len(baseline.Accepted) != 0 || len(baseline.Rejected) != 0 {
		t.Errorf("baseline = %+v, want no entries", baseline)
	}
}

func TestAMissingTermsFileIsAnEmptyVocabulary(t *testing.T) {
	dir := project(t, "")
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vocab.Project.Accepted)+len(vocab.Project.Rejected)+len(vocab.Pending) != 0 {
		t.Errorf("vocabulary = %+v, want nothing", vocab)
	}
}

func TestTheTermsFileIsReadWithItsLines(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "frobnitz"
meaning = "The widget that frobs."
aliases = ["frobnitzes"]

[[rejected]]
pattern = "blast radius"
kind = "phrase"
reason = "Say what is affected."
`)
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vocab.Project.Accepted) != 1 || vocab.Project.Accepted[0].Line != 3 {
		t.Fatalf("accepted = %+v, want one entry on line 3", vocab.Project.Accepted)
	}
	if len(vocab.Project.Rejected) != 1 || vocab.Project.Rejected[0].Line != 8 {
		t.Fatalf("rejected = %+v, want one entry on line 8", vocab.Project.Rejected)
	}
	spell := vocab.SpellVocab()
	for _, word := range []string{"frobnitz", "frobnitzes"} {
		if !spell.Has(word) {
			t.Errorf("the spell vocabulary lacks %q", word)
		}
	}
}

func TestAnInvalidTermsFileIsRefusedByName(t *testing.T) {
	for name, terms := range map[string]string{
		"an accepted word with no meaning": EmptyTerms + "\n[[accepted]]\nword = \"x\"\n",
		"an unknown kind":                  EmptyTerms + "\n[[rejected]]\npattern = \"x\"\nkind = \"regex\"\nreason = \"r\"\n",
		"an unknown key":                   EmptyTerms + "\n[[accepted]]\nword = \"x\"\nmeaning = \"m\"\nnote = \"n\"\n",
		"a word with a trailing space":     EmptyTerms + "\n[[accepted]]\nword = \"x \"\nmeaning = \"m\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := project(t, terms)
			_, err := Load(dir)
			if err == nil || !strings.Contains(err.Error(), layout.TermsRel) {
				t.Errorf("err = %v, want a refusal naming %s", err, layout.TermsRel)
			}
		})
	}
}

// A word both accepted and rejected has no right verdict, so the load refuses
// it, naming both entries; the remedy the refusal names clears it.
func TestAWordBothAcceptedAndRejectedIsALoadError(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "Leverage"
meaning = "The lever's advantage."

[[rejected]]
pattern = "leverage"
kind = "word"
reason = "Say use."
`)
	_, err := Load(dir)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want the conflict refusal", err)
	}
	for _, want := range []string{layout.TermsRel + ":3", layout.TermsRel + ":7", "selfdoc vocabulary remove Leverage"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}

	if _, err := Remove(effects.Unbound(), dir, "Leverage"); err != nil {
		t.Fatalf("the remedy failed: %v", err)
	}
	if _, err := Accept(effects.Unbound(), dir, "Leverage", "The lever's advantage."); err != nil {
		t.Fatalf("adding back the right entry failed: %v", err)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("the remedy did not clear the conflict: %v", err)
	}
}

func TestAcceptInsertsInSortedPositionAndKeepsComments(t *testing.T) {
	dir := project(t, `# The project's words.
format_version = 1

# Comes first.
[[accepted]]
word = "alpha"
meaning = "The first."

# Comes last.
[[accepted]]
word = "Zulu"
meaning = "The last."

[[rejected]]
pattern = "leverage"
kind = "word"
reason = "Say use."
`)
	edit, err := Accept(effects.Unbound(), dir, "mike", "The middle.")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if len(edit.Files) != 1 || edit.Files[0] != layout.TermsRel {
		t.Errorf("files = %v", edit.Files)
	}
	got := readTerms(t, dir)
	for _, want := range []string{"# The project's words.", "# Comes first.", "# Comes last.", `meaning = "The middle."`} {
		if !strings.Contains(got, want) {
			t.Errorf("the file lost %q:\n%s", want, got)
		}
	}
	alpha, mike, zulu := strings.Index(got, `"alpha"`), strings.Index(got, `"mike"`), strings.Index(got, `"Zulu"`)
	if !(alpha < mike && mike < zulu) {
		t.Errorf("the new entry is not in sorted position:\n%s", got)
	}
	if !strings.Contains(got, "meaning = \"The first.\"\n\n[[accepted]]\nword = \"mike\"") {
		t.Errorf("the new entry is not set apart like the others:\n%s", got)
	}
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("the edited file does not load: %v", err)
	}
	if problems := UnsortedEntries(vocab.Project, vocab.Pending); len(problems) != 0 {
		t.Errorf("the edited file is unsorted: %v", problems)
	}
}

func TestAcceptCreatesTheTermsFile(t *testing.T) {
	dir := project(t, "")
	if _, err := Accept(effects.Unbound(), dir, "frobnitz", "The widget that frobs."); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	got := readTerms(t, dir)
	want := "format_version = 1\n\n[[accepted]]\nword = \"frobnitz\"\nmeaning = \"The widget that frobs.\"\n"
	if got != want {
		t.Errorf("terms.toml =\n%s\nwant\n%s", got, want)
	}
	info, err := os.Stat(layout.Path(dir, layout.TermsRel))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("the new file's mode = %v (%v), want 0644", info.Mode().Perm(), err)
	}
}

func TestAcceptRefusesDuplicatesAndConflicts(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "frobnitz"
meaning = "m"
aliases = ["frobs"]

[[rejected]]
pattern = "-shaped"
kind = "suffix"
reason = "Say what it is."
`)
	for word, want := range map[string]string{
		"Frobnitz":       "already accepted",
		"FROBS":          "already accepted",
		"diamond-shaped": "rejected as a suffix",
	} {
		if _, err := Accept(effects.Unbound(), dir, word, "m"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Accept(%q) = %v, want a refusal carrying %q", word, err, want)
		}
	}
}

func TestRejectRefusesDuplicatesAndConflicts(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "misshaped"
meaning = "m"

[[rejected]]
pattern = "leverage"
kind = "word"
reason = "Say use."
`)
	if _, err := Reject(effects.Unbound(), dir, "Leverage", KindWord, "r"); err == nil || !strings.Contains(err.Error(), "already rejected") {
		t.Errorf("a duplicate rejection = %v", err)
	}
	if _, err := Reject(effects.Unbound(), dir, "shaped", KindSuffix, "r"); err == nil || !strings.Contains(err.Error(), "misshaped") {
		t.Errorf("a rejection covering an accepted word = %v", err)
	}
	if _, err := Reject(effects.Unbound(), dir, "blast radius", KindPhrase, "Say what is affected."); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	got := readTerms(t, dir)
	if strings.Index(got, `"blast radius"`) > strings.Index(got, `"leverage"`) {
		t.Errorf("the new rejection is not in sorted position:\n%s", got)
	}
}

func TestRemoveDeletesTheEntryAndRefusesWhatIsNotThere(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "alpha"
meaning = "a"

[[accepted]]
word = "beta"
meaning = "b"
`)
	if _, err := Remove(effects.Unbound(), dir, "ALPHA"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got := readTerms(t, dir)
	if strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("terms.toml after removing alpha:\n%s", got)
	}
	if _, err := Remove(effects.Unbound(), dir, "gamma"); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("removing an absent word = %v", err)
	}
}

func review(t *testing.T, dir, content string) {
	t.Helper()
	testproject.WriteText(t, layout.Path(dir, layout.ReviewRel), content)
}

const pendingFrobnitz = EmptyReview + `
[[pending]]
word = "frobnitz"
meaning = "The widget that frobs."
confidence = 0.8
evidence = ["stricttools/docs/index.md:3: The frobnitz frobs."]
`

func TestApproveMovesThePendingWordWithItsMeaning(t *testing.T) {
	dir := project(t, "")
	review(t, dir, pendingFrobnitz)
	edit, err := Approve(effects.Unbound(), dir, "Frobnitz", "")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if strings.Join(edit.Files, ",") != layout.TermsRel+","+layout.ReviewRel {
		t.Errorf("files = %v", edit.Files)
	}
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vocab.Pending) != 0 {
		t.Errorf("the word is still pending: %+v", vocab.Pending)
	}
	if len(vocab.Project.Accepted) != 1 || vocab.Project.Accepted[0].Meaning != "The widget that frobs." {
		t.Errorf("accepted = %+v", vocab.Project.Accepted)
	}
}

func TestApproveWithACorrectedMeaning(t *testing.T) {
	dir := project(t, "")
	review(t, dir, pendingFrobnitz)
	if _, err := Approve(effects.Unbound(), dir, "frobnitz", "The part that frobs."); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vocab.Project.Accepted) != 1 || vocab.Project.Accepted[0].Meaning != "The part that frobs." {
		t.Errorf("accepted = %+v", vocab.Project.Accepted)
	}
}

func TestDropDeletesThePendingWord(t *testing.T) {
	dir := project(t, "")
	review(t, dir, pendingFrobnitz)
	if _, err := Drop(effects.Unbound(), dir, "frobnitz"); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vocab.Pending)+len(vocab.Project.Accepted) != 0 {
		t.Errorf("vocabulary after drop = %+v", vocab)
	}
	if _, err := Drop(effects.Unbound(), dir, "frobnitz"); err == nil || !strings.Contains(err.Error(), "not pending") {
		t.Errorf("dropping an absent word = %v", err)
	}
}

// A pending word is resolved by approving it, not by accepting it beside the
// proposal; the refusal names the approve command, which then succeeds.
func TestAcceptRefusesAPendingWordAndNamesApprove(t *testing.T) {
	dir := project(t, "")
	review(t, dir, pendingFrobnitz)
	_, err := Accept(effects.Unbound(), dir, "frobnitz", "m")
	if err == nil || !strings.Contains(err.Error(), "selfdoc vocabulary approve frobnitz") {
		t.Fatalf("err = %v, want a refusal naming approve", err)
	}
	if _, err := Approve(effects.Unbound(), dir, "frobnitz", ""); err != nil {
		t.Errorf("the named remedy failed: %v", err)
	}
}

func TestAnInvalidReviewListIsRefused(t *testing.T) {
	dir := project(t, "")
	review(t, dir, EmptyReview+"\n[[pending]]\nword = \"x\"\nmeaning = \"m\"\nconfidence = 1.5\nevidence = [\"a:1: x\"]\n")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), layout.ReviewRel) {
		t.Errorf("err = %v, want a refusal naming %s", err, layout.ReviewRel)
	}
}

func TestMatcherKinds(t *testing.T) {
	hygiene.Isolate(t)
	for _, testCase := range []struct {
		kind, pattern, text string
		want             []string
	}{
		{KindWord, "leverage", "We Leverage it; leveraged is fine.", []string{"Leverage"}},
		{KindPhrase, "blast radius", "the blast\tradius and blast-radius", []string{"blast\tradius"}},
		{KindSuffix, "-shaped", "diamond-shaped, -shaped alone", []string{"-shaped"}},
		{KindPrefix, "pre", "prefix pre preamble", []string{"pre", "pre"}},
	} {
		matcher := NewMatcher(Rejected{Pattern: testCase.pattern, Kind: testCase.kind})
		var got []string
		for _, match := range matcher.Find(testCase.text) {
			got = append(got, match.Text)
		}
		if strings.Join(got, "|") != strings.Join(testCase.want, "|") {
			t.Errorf("%s %q in %q = %v, want %v", testCase.kind, testCase.pattern, testCase.text, got, testCase.want)
		}
	}
}

func TestUnsortedEntriesAreFound(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "beta"
meaning = "b"

[[accepted]]
word = "Alpha"
meaning = "a"
`)
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	problems := UnsortedEntries(vocab.Project, vocab.Pending)
	if len(problems) != 1 || problems[0].Line != 7 || !strings.Contains(problems[0].Message, `"Alpha"`) {
		t.Errorf("problems = %+v, want the out-of-order entry on line 7", problems)
	}
}

func TestDuplicateEntriesAreFound(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "alpha"
meaning = "a"

[[accepted]]
word = "ALPHA"
meaning = "a again"
`)
	vocab, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	problems := DuplicateEntries(vocab)
	if len(problems) != 1 || problems[0].Line != 7 {
		t.Fatalf("problems = %+v, want the second entry on line 7", problems)
	}
	// The remedy the problem names clears it.
	if _, err := Remove(effects.Unbound(), dir, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(effects.Unbound(), dir, "alpha", "a"); err != nil {
		t.Fatal(err)
	}
	vocab, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if problems := DuplicateEntries(vocab); len(problems) != 0 {
		t.Errorf("the remedy did not clear the duplicate: %+v", problems)
	}
}

// The spell check reads the baseline and the project's file and nothing
// outside the repository: a machine-wide list in the home directory is never
// consulted.
func TestNoMachineInputIsRead(t *testing.T) {
	dir := project(t, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	testproject.WriteText(t, filepath.Join(home, "Projects", "ark", "spelling-accept.txt"), "frobnitz\n")
	vocab, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if vocab.SpellVocab().Has("frobnitz") {
		t.Error("a word in a machine-wide list was accepted")
	}
}

func TestRemoveLeavesTheFileLookingHandWritten(t *testing.T) {
	dir := project(t, EmptyTerms+`
[[accepted]]
word = "alpha"
meaning = "a"

# The middle one.
[[accepted]]
word = "beta"
meaning = "b"

[[accepted]]
word = "gamma"
meaning = "g"
`)
	if _, err := Remove(effects.Unbound(), dir, "beta"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	want := EmptyTerms + `
[[accepted]]
word = "alpha"
meaning = "a"

[[accepted]]
word = "gamma"
meaning = "g"
`
	if got := readTerms(t, dir); got != want {
		t.Errorf("terms.toml =\n%s\nwant\n%s", got, want)
	}
}
