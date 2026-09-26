package check

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/staleness"
)

// baselineProject writes a minimal project with one source module and an empty
// docs directory, with the given locales declared.
func baselineProject(t *testing.T, locales []any) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	projectConfig := configForSource(
		map[string]any{"path": "src/", "language": "python"},
	)
	if locales != nil {
		projectConfig["locales"] = locales
	}
	writeConfig(t, root, projectConfig)
	write(t, filepath.Join(root, "src", "__init__.py"), "")
	write(t, filepath.Join(root, "stricttools", "docs", ".keep"), "")
	return root
}

// writePage writes one docs page with the given description and body.
func writePage(t *testing.T, root, description, body, name string) {
	t.Helper()
	write(t, filepath.Join(root, "stricttools", "docs", name),
		"+++\ndescription = \""+description+"\"\n+++\n# Page\n\n"+body+"\n")
}

// staleCount is how many STALE001 diagnostics a run produced.
func staleCount(result *CheckResult) int {
	return len(withCode(result.Lints, "STALE001"))
}

func TestStaleFreezeIsSticky(t *testing.T) {
	root := baselineProject(t, nil)

	writePage(t, root, "Original description", "Original content here.", "page.md")
	if got := staleCount(checkFixture(t, root)); got != 0 {
		t.Fatalf("a new page reported %d STALE001", got)
	}

	writePage(t, root, "Original description", "Completely rewritten content.", "page.md")
	if got := staleCount(checkFixture(t, root)); got != 1 {
		t.Fatalf("changed content with an unchanged description reported %d STALE001", got)
	}

	// The baseline stays frozen, so the error persists -- the dead end
	// baseline accept exists to break.
	if got := staleCount(checkFixture(t, root)); got != 1 {
		t.Errorf("a second run reported %d STALE001, want the frozen baseline to hold", got)
	}
}

func TestAcceptClearsStaleness(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)
	writePage(t, root, "Original description", "Completely rewritten content.", "page.md")
	if got := staleCount(checkFixture(t, root)); got != 1 {
		t.Fatalf("STALE001 count = %d, want 1", got)
	}

	accepted, err := AcceptBaselines([]string{"page.md"}, root, nil, handle())
	if err != nil {
		t.Fatalf("AcceptBaselines: %v", err)
	}
	if len(accepted) != 1 || accepted[0].Page != "page.md" || accepted[0].Code != "STALE001" {
		t.Fatalf("accepted = %+v", accepted)
	}

	if got := staleCount(checkFixture(t, root)); got != 0 {
		t.Errorf("STALE001 count after accept = %d, want 0", got)
	}
}

func TestAcceptAdvancesBaselineToCurrentHashes(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)
	writePage(t, root, "Original description", "Brand new body text.", "page.md")
	checkFixture(t, root)

	if _, err := AcceptBaselines([]string{"page.md"}, root, nil, handle()); err != nil {
		t.Fatalf("AcceptBaselines: %v", err)
	}

	stored, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}
	wantContent := staleness.ComputeContentHash("# Page\n\nBrand new body text.\n")
	wantDescription := staleness.ComputeDescriptionHash("Original description")
	if stored["page.md"].Content != wantContent {
		t.Errorf("content hash = %q, want %q", stored["page.md"].Content, wantContent)
	}
	if stored["page.md"].Description != wantDescription {
		t.Errorf("description hash = %q, want %q",
			stored["page.md"].Description, wantDescription)
	}
}

// TestAcceptKeepsTheSeedHash: accept advances the fields a finding is about
// and merges them into the stored entry rather than replacing it. gen's
// seed_hash -- the record of the machine-written description that makes the
// page machine-owned -- is not something accept measures, so it survives.
func TestAcceptKeepsTheSeedHash(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)

	stored, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}
	entry := stored["page.md"]
	entry.SeedHash = "seedseedseed"
	stored["page.md"] = entry
	if err := staleness.SaveHashes(stored, root, handle()); err != nil {
		t.Fatalf("SaveHashes: %v", err)
	}

	writePage(t, root, "Original description", "Brand new body text.", "page.md")
	checkFixture(t, root)
	if _, err := AcceptBaselines([]string{"page.md"}, root, nil, handle()); err != nil {
		t.Fatalf("AcceptBaselines: %v", err)
	}

	after, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}
	if after["page.md"].SeedHash != "seedseedseed" {
		t.Errorf("seed_hash = %q after accept, want it kept", after["page.md"].SeedHash)
	}
	if want := staleness.ComputeContentHash("# Page\n\nBrand new body text.\n"); after["page.md"].Content != want {
		t.Errorf("content hash not advanced: %q", after["page.md"].Content)
	}
}

// TestAcceptAfterADescriptionEditSaysWhatHappened drives the remedy the
// finding prints, in the order the finding invites: edit the description, then
// run accept.
//
// Editing the description clears the finding on its own, so accept then has
// nothing to accept and refuses. The refusal has to say that, rather than
// reading as though the review was rejected.
func TestAcceptAfterADescriptionEditSaysWhatHappened(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)
	writePage(t, root, "Original description", "Completely rewritten content.", "page.md")
	if got := staleCount(checkFixture(t, root)); got != 1 {
		t.Fatalf("STALE001 count = %d, want 1", got)
	}

	// The remedy: the description is rewritten to describe the new content.
	writePage(t, root, "A description of the rewritten content",
		"Completely rewritten content.", "page.md")

	_, err := AcceptBaselines([]string{"page.md"}, root, nil, handle())
	if err == nil {
		t.Fatal("accept succeeded on a page with no outstanding finding")
	}
	message := err.Error()
	for _, want := range []string{
		"description",
		"cleared",
		"nothing to accept",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, message)
		}
	}
}

// TestTheFindingHintOffersBothCourses asserts that the remedy a drift finding
// prints names both ways out and says which one makes the other unnecessary.
//
// Printing "then run `selfdoc baseline accept <page>`" alone reads as a step to
// take after any review, including a review that ended in a description edit --
// and accept refuses in that state.
func TestTheFindingHintOffersBothCourses(t *testing.T) {
	hint := staleness.BaselineAcceptHint("en/index.md", "docstrings")
	for _, want := range []string{
		"edit",
		"description",
		"clears",
		"selfdoc baseline accept en/index.md",
		"docstrings",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint does not say %q:\n%s", want, hint)
		}
	}
}

func TestAcceptRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name string
		// establishBaseline runs a check before accepting, recording a
		// baseline.
		establishBaseline bool
		// makeStale rewrites the body so the page is frozen in error.
		makeStale bool
		pages     []string
		fragment  string
	}{
		{
			name:              "a page that is not stale",
			establishBaseline: true,
			pages:             []string{"page.md"},
			fragment:          "nothing to accept",
		},
		{
			name:              "a page that is not in the project",
			establishBaseline: true,
			pages:             []string{"does-not-exist.md"},
			fragment:          "not a documentation page",
		},
		{
			name:     "a page with no baseline recorded yet",
			pages:    []string{"page.md"},
			fragment: "has no baseline yet",
		},
		{
			name:              "no page named at all",
			establishBaseline: true,
			pages:             nil,
			fragment:          "Name at least one page",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := baselineProject(t, nil)
			writePage(t, root, "Original description", "Original content here.", "page.md")
			if testCase.establishBaseline {
				checkFixture(t, root)
			}
			if testCase.makeStale {
				writePage(t, root, "Original description", "Rewritten content.", "page.md")
				checkFixture(t, root)
			}

			_, err := AcceptBaselines(testCase.pages, root, nil, handle())
			if err == nil {
				t.Fatal("the accept was not refused")
			}
			var acceptError *AcceptError
			if !errors.As(err, &acceptError) {
				t.Fatalf("error type = %T, want *AcceptError: %v", err, err)
			}
			if !strings.Contains(err.Error(), testCase.fragment) {
				t.Errorf("error = %q, want it to carry %q", err, testCase.fragment)
			}
		})
	}
}

func TestAcceptIsAllOrNothing(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)
	writePage(t, root, "Original description", "Rewritten content.", "page.md")
	checkFixture(t, root)

	before, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}

	if _, err := AcceptBaselines(
		[]string{"page.md", "bogus.md"}, root, nil, handle(),
	); err == nil {
		t.Fatal("an accept naming an unknown page was not refused")
	}

	after, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}
	if after["page.md"].Content != before["page.md"].Content {
		t.Error("the valid page's baseline was advanced despite the refusal")
	}
}

func TestAcceptMultiplePagesInOneCall(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Desc A original", "Body A original.", "a.md")
	writePage(t, root, "Desc B original", "Body B original.", "b.md")
	checkFixture(t, root)

	writePage(t, root, "Desc A original", "Body A rewritten.", "a.md")
	writePage(t, root, "Desc B original", "Body B rewritten.", "b.md")
	if got := staleCount(checkFixture(t, root)); got != 2 {
		t.Fatalf("STALE001 count = %d, want 2", got)
	}

	accepted, err := AcceptBaselines([]string{"a.md", "b.md"}, root, nil, handle())
	if err != nil {
		t.Fatalf("AcceptBaselines: %v", err)
	}
	pages := map[string]bool{}
	for _, entry := range accepted {
		pages[entry.Page] = true
	}
	if !pages["a.md"] || !pages["b.md"] {
		t.Errorf("accepted = %+v, want both pages", accepted)
	}
	if got := staleCount(checkFixture(t, root)); got != 0 {
		t.Errorf("STALE001 count after accept = %d, want 0", got)
	}
}

func TestAcceptUsesLocalePrefixedIdentifiers(t *testing.T) {
	locales := []any{map[string]any{"code": "en", "label": "English", "default": true}}
	root := baselineProject(t, locales)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)
	writePage(t, root, "Original description", "Rewritten content.", "page.md")

	result := checkFixture(t, root)
	stale := withCode(result.Lints, "STALE001")
	if len(stale) != 1 {
		t.Fatalf("STALE001 count = %d, want 1", len(stale))
	}
	if stale[0].File() != "en/page.md" {
		t.Errorf("file = %q, want the locale-prefixed identifier", stale[0].File())
	}

	if _, err := AcceptBaselines([]string{"page.md"}, root, nil, handle()); err == nil {
		t.Error("the bare identifier was accepted")
	}
	accepted, err := AcceptBaselines([]string{"en/page.md"}, root, nil, handle())
	if err != nil {
		t.Fatalf("AcceptBaselines: %v", err)
	}
	if len(accepted) != 1 || accepted[0].Page != "en/page.md" {
		t.Fatalf("accepted = %+v", accepted)
	}
	if got := staleCount(checkFixture(t, root)); got != 0 {
		t.Errorf("STALE001 count after accept = %d, want 0", got)
	}
}

func TestComputeStalenessStateWritesNothing(t *testing.T) {
	root := baselineProject(t, nil)
	writePage(t, root, "Original description", "Original content here.", "page.md")
	checkFixture(t, root)
	writePage(t, root, "Original description", "Rewritten content.", "page.md")
	checkFixture(t, root)

	before, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}

	state, err := ComputeStalenessState(root, nil, handle())
	if err != nil {
		t.Fatalf("ComputeStalenessState: %v", err)
	}
	if state.ErrorPages["page.md"] != "STALE001" {
		t.Errorf("error pages = %v, want page.md frozen in STALE001", state.ErrorPages)
	}
	if _, present := state.Current["page.md"]; !present {
		t.Error("the current hashes do not carry the page")
	}

	after, err := staleness.LoadHashes(root)
	if err != nil {
		t.Fatalf("LoadHashes: %v", err)
	}
	if after["page.md"].Content != before["page.md"].Content {
		t.Error("computing the staleness state wrote to the store")
	}
}
