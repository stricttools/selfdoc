package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// The editor's inline assistance: spelling marks and lint marks on a buffer.
//
// Two properties carry everything here. The first is that neither lane is a
// second opinion: the words come from the shared spelling engine (same masks,
// same vendored list, same machine-local accept list) and the marks come from
// the project's real lint rules over the check's own post slice, so a finding
// on screen is a finding the check reports. The second is the coordinate
// change -- the engines answer in lines and columns, the editor paints flat
// character offsets over the buffer -- which is the one thing the editor adds
// and therefore the one thing that can be wrong.
//
// Analysing a buffer writes nothing, for the same reason previewing one
// writes nothing: the buffer is unsaved, and looking at it must not decide
// that it is saved.

// Long enough to clear SEO009's description floor, so a post the rules have
// nothing to say about really produces no findings.
const analysisDescription = "A post written to carry no lint findings at all, with a description " +
	"long enough that the description-length rule has nothing to say about " +
	"it either."

// Between 30 and 80 words, which is the band SEO007 holds every first
// paragraph to.
const analysisBody = "This post exists so that the editor has something to analyse, and it " +
	"says enough to clear the first-paragraph length rule without saying " +
	"anything a reader would have to think about. It is filler, written " +
	"plainly, and it is here only to give the lint rules a page shaped " +
	"exactly like a real one so that a clean buffer really does come back " +
	"with nothing at all to report."

// analysisPost assembles one post source around a body.
func analysisPost(body string) string {
	return analysisPostAs("Hello World", "hello-world", false, body)
}

// analysisPostAs assembles one post source with a stated title, slug and
// draft answer.
func analysisPostAs(title, slug string, draft bool, body string) string {
	return fmt.Sprintf(
		"+++\ntitle = \"%s\"\ndate = 2024-01-15\nslug = \"%s\"\ndescription = \"%s\"\n"+
			"tags = [\"release\"]\ndraft = %t\ndirectives = false\n+++\n%s",
		title, slug, analysisDescription, draft, body,
	)
}

// cleanPost is the buffer the rules have nothing to say about.
var cleanPost = analysisPost("# Hello World\n\n" + analysisBody + "\n")

// spellingOf runs the spelling lane, failing on a refusal.
func spellingOf(t *testing.T, content string) []SpellingFinding {
	t.Helper()
	findings, err := SpellingFindings(content, "buffer.md")
	if err != nil {
		t.Fatalf("SpellingFindings: %v", err)
	}
	return findings
}

// wordsOf returns the words a spelling run reported.
func wordsOf(findings []SpellingFinding) []string {
	words := []string{}
	for _, finding := range findings {
		words = append(words, finding.Word)
	}
	return words
}

// lintsOf runs the lint lane, failing on a refusal.
func lintsOf(t *testing.T, project, rel, content string) []LintFinding {
	t.Helper()
	entry := entryOf(t, "proj", project)
	findings, err := LintFindings(entry, rel, content, nil, effects.Unbound())
	if err != nil {
		t.Fatalf("LintFindings: %v", err)
	}
	return findings
}

// codesOf returns the codes a lint run reported.
func codesOf(findings []LintFinding) []string {
	codes := []string{}
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	return codes
}

// hasCode reports whether a lint run reported one code.
func hasCode(findings []LintFinding, code string) bool {
	return contains(codesOf(findings), code)
}

// analysisProject writes the project the analysis fixtures name.
func analysisProject(t *testing.T) string {
	t.Helper()
	return makeProject(t, map[string]string{postHelloName: cleanPost})
}

// -- spelling ---------------------------------------------------------------

func TestSpellingOffsets(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("a misspelling is reported at the offset of its word", func(t *testing.T) {
		content := analysisPost("# Hello World\n\nThis is teh post content.\n")
		finding := findWord(t, spellingOf(t, content), "teh")
		if got := runeSlice(content, finding.From, finding.To); got != "teh" {
			t.Errorf("the offsets hold %q", got)
		}
	})

	t.Run("the offset accounts for the frontmatter above it", func(t *testing.T) {
		content := analysisPost("# Hello World\n\nThis is teh post content.\n")
		findings := spellingOf(t, content)
		if len(findings) == 0 {
			t.Fatal("the buffer carries a misspelling")
		}
		finding := findings[0]
		// The offset is into the WHOLE buffer, frontmatter included -- which
		// is the text the editor holds.
		if finding.From <= strings.Index(content, "+++\n") {
			t.Errorf("from = %d, want an offset past the frontmatter fence", finding.From)
		}
		want := strings.Count(runeSlice(content, 0, finding.From), "\n") + 1
		if finding.Line != want {
			t.Errorf("line = %d, want %d", finding.Line, want)
		}
	})

	t.Run("every misspelling maps to its own word", func(t *testing.T) {
		content := analysisPost(
			"# Hello World\n\nteh recieve seperate words are all wrong.\n")
		findings := spellingOf(t, content)
		for _, want := range []string{"teh", "recieve", "seperate"} {
			if !contains(wordsOf(findings), want) {
				t.Errorf("%q was not reported: %v", want, wordsOf(findings))
			}
		}
		for _, finding := range findings {
			if got := runeSlice(content, finding.From, finding.To); got != finding.Word {
				t.Errorf("the offsets of %q hold %q", finding.Word, got)
			}
		}
	})

	t.Run("offsets survive a tab indented line", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n- item\n- teh other item\n")
		finding := findWord(t, spellingOf(t, content), "teh")
		if got := runeSlice(content, finding.From, finding.To); got != "teh" {
			t.Errorf("the offsets hold %q", got)
		}
	})

	t.Run("offsets survive non-ascii text above the word", func(t *testing.T) {
		content := analysisPost(
			"# Hello World\n\nA naïve — dash — line.\n\nteh end.\n")
		finding := findWord(t, spellingOf(t, content), "teh")
		if got := runeSlice(content, finding.From, finding.To); got != "teh" {
			t.Errorf("the offsets hold %q", got)
		}
	})

	t.Run("a clean buffer reports nothing", func(t *testing.T) {
		if findings := spellingOf(t, cleanPost); len(findings) != 0 {
			t.Errorf("findings = %v", wordsOf(findings))
		}
	})

	t.Run("the finding carries the engine's own message", func(t *testing.T) {
		content := analysisPost("# Hello World\n\nThis is teh post.\n")
		findings := spellingOf(t, content)
		if len(findings) == 0 {
			t.Fatal("the buffer carries a misspelling")
		}
		if !strings.HasPrefix(findings[0].Message, "Unrecognized word 'teh'") {
			t.Errorf("message = %q", findings[0].Message)
		}
	})

	t.Run("the finding carries both coordinate systems", func(t *testing.T) {
		content := analysisPost("# Hello World\n\nThis is teh post.\n")
		finding := findWord(t, spellingOf(t, content), "teh")
		if finding.Line < 1 || finding.Column < 1 {
			t.Errorf("line/column = %d/%d, want 1-based coordinates",
				finding.Line, finding.Column)
		}
		if finding.To-finding.From != len([]rune(finding.Word)) {
			t.Errorf("the span is %d characters for a %d-character word",
				finding.To-finding.From, len([]rune(finding.Word)))
		}
	})
}

func TestSpellingIsTheSharedEngine(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("a word inside a code span is masked", func(t *testing.T) {
		// The engine's masks, not a second set written for the editor.
		content := analysisPost("# Hello World\n\nThe `teh` identifier is fine.\n")
		if words := wordsOf(spellingOf(t, content)); len(words) != 0 {
			t.Errorf("words = %v, want none", words)
		}
	})

	t.Run("a fenced code block is not prose", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n```\nteh recieve\n```\n")
		if words := wordsOf(spellingOf(t, content)); len(words) != 0 {
			t.Errorf("words = %v, want none", words)
		}
	})

	t.Run("a link target is masked and its text is not", func(t *testing.T) {
		content := analysisPost(
			"# Hello World\n\nSee [teh page](../../alpha/guide/).\n")
		if words := wordsOf(spellingOf(t, content)); strings.Join(words, ",") != "teh" {
			t.Errorf("words = %v, want only teh", words)
		}
	})

	t.Run("the machine-local accept list is consulted", func(t *testing.T) {
		content := analysisPost("# Hello World\n\nThe frobnitz editor runs here.\n")
		if words := wordsOf(spellingOf(t, content)); strings.Join(words, ",") != "frobnitz" {
			t.Errorf("words = %v, want only frobnitz", words)
		}

		accept := spelling.AcceptListPath()
		if err := os.MkdirAll(filepath.Dir(accept), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(accept, []byte("frobnitz\n"), 0o644); err != nil {
			t.Fatalf("writing the accept list: %v", err)
		}
		if words := wordsOf(spellingOf(t, content)); len(words) != 0 {
			t.Errorf("words = %v, want none once the word is accepted", words)
		}
	})
}

// -- lints ------------------------------------------------------------------

func TestLintMarks(t *testing.T) {
	hygiene.Isolate(t)
	project := analysisProject(t)

	t.Run("a clean post reports nothing", func(t *testing.T) {
		if findings := lintsOf(t, project, postHelloName, cleanPost); len(findings) != 0 {
			t.Errorf("findings = %v", codesOf(findings))
		}
	})

	t.Run("a second h1 is reported under its registered code", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n# Second Title\n\nBody.\n")
		if !hasCode(lintsOf(t, project, postHelloName, content), "SEO001") {
			t.Error("the second heading was not reported")
		}
	})

	t.Run("the severity comes from the registry", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n# Second Title\n\nBody.\n")
		want, err := lints.LintSeverity("SEO001")
		if err != nil {
			t.Fatalf("LintSeverity: %v", err)
		}
		for _, finding := range lintsOf(t, project, postHelloName, content) {
			if finding.Code == "SEO001" && finding.Severity != want {
				t.Errorf("severity = %q, want %q", finding.Severity, want)
			}
		}
	})

	t.Run("a line number is a line of the buffer", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n## Two\n\n#### Four\n")
		lines := strings.Split(content, "\n")
		found := false
		for _, finding := range lintsOf(t, project, postHelloName, content) {
			if finding.Code != "SEO002" {
				continue
			}
			found = true
			if finding.Line == nil {
				t.Fatal("the heading-gap finding carries no line")
			}
			if !strings.HasPrefix(lines[*finding.Line-1], "#### Four") {
				t.Errorf("line %d reads %q", *finding.Line, lines[*finding.Line-1])
			}
		}
		if !found {
			t.Error("the heading gap was not reported")
		}
	})

	t.Run("a draft buffer is judged too", func(t *testing.T) {
		// The build skips a draft; the editor is where a draft is written.
		content := analysisPostAs("Hello World", "hello-world", true,
			"# Hello World\n\n# Second Title\n\nBody.\n")
		if !hasCode(lintsOf(t, project, postHelloName, content), "SEO001") {
			t.Error("the draft was not judged")
		}
	})

	t.Run("the buffer is judged, not the saved file", func(t *testing.T) {
		// The saved post is clean; only the unsaved buffer has the defect.
		content := analysisPost("# Hello World\n\n# Second Title\n\nBody.\n")
		if !hasCode(lintsOf(t, project, postHelloName, content), "SEO001") {
			t.Error("the buffer's own defect was not reported")
		}
		if findings := lintsOf(t, project, postHelloName, cleanPost); len(findings) != 0 {
			t.Errorf("the saved post reports %v", codesOf(findings))
		}
	})

	t.Run("spelling is not reported twice", func(t *testing.T) {
		// One misspelling is one finding, in the lane that has its columns.
		content := analysisPost("# Hello World\n\nThis is teh post content.\n")
		if hasCode(lintsOf(t, project, postHelloName, content), "SPELL001") {
			t.Error("the lint lane restated the spelling finding")
		}
		if words := wordsOf(spellingOf(t, content)); strings.Join(words, ",") != "teh" {
			t.Errorf("words = %v, want only teh", words)
		}
	})
}

func TestTheSliceIsTheUniverseALinkIsJudgedAgainst(t *testing.T) {
	// XREF001 resolves a link against the page's own directory. A post's own
	// directory is the posts directory, so the pages a post's .md link can
	// name are the other posts -- which is the slice the rules run over,
	// saved posts and the unsaved buffer alike.
	hygiene.Isolate(t)

	// twoPostProject writes a project carrying the clean post and a sibling.
	twoPostProject := func(t *testing.T) string {
		t.Helper()
		sibling := analysisPostAs("Sibling", "sibling", false,
			"# Sibling\n\n"+analysisBody+"\n")
		return makeProject(t, map[string]string{
			postHelloName: cleanPost, "sibling.md": sibling,
		})
	}

	t.Run("a link to a sibling post resolves", func(t *testing.T) {
		project := twoPostProject(t)
		content := analysisPost("# Hello World\n\n" + analysisBody +
			"\n\nSee [it](sibling.md).\n")
		if hasCode(lintsOf(t, project, postHelloName, content), "XREF001") {
			t.Error("a link to a saved sibling was called unknown")
		}
	})

	t.Run("a link to a post that does not exist is reported", func(t *testing.T) {
		project := twoPostProject(t)
		content := analysisPost("# Hello World\n\n" + analysisBody +
			"\n\nSee [it](ghost.md).\n")
		if !hasCode(lintsOf(t, project, postHelloName, content), "XREF001") {
			t.Error("a link to nothing was not reported")
		}
	})

	t.Run("a site-level cross link is not an md link at all", func(t *testing.T) {
		// The addresses the completion inserts are directory URLs, not files.
		project := twoPostProject(t)
		content := analysisPost("# Hello World\n\n" + analysisBody +
			"\n\nSee [the guide](../../alpha/guide/).\n")
		if hasCode(lintsOf(t, project, postHelloName, content), "XREF001") {
			t.Error("a site-level address was judged as a file link")
		}
	})
}

func TestABufferThatIsNotAValidPost(t *testing.T) {
	hygiene.Isolate(t)
	project := analysisProject(t)

	t.Run("a missing date is the post check's own code", func(t *testing.T) {
		broken := "+++\ntitle = \"No Date\"\nslug = \"no-date\"\ndirectives = false\n+++\nBody\n"
		codes := codesOf(lintsOf(t, project, postHelloName, broken))
		if strings.Join(codes, ",") != "POST001" {
			t.Errorf("codes = %v, want POST001", codes)
		}
	})

	t.Run("a missing title is reported rather than raised", func(t *testing.T) {
		broken := "+++\ndate = 2024-01-15\nslug = \"no-title\"\ndirectives = false\n+++\nB\n"
		codes := codesOf(lintsOf(t, project, postHelloName, broken))
		if strings.Join(codes, ",") != "POST002" {
			t.Errorf("codes = %v, want POST002", codes)
		}
	})

	t.Run("spelling still answers for a buffer that is not a post", func(t *testing.T) {
		// The lanes fail independently, which is why they are two lanes.
		broken := "+++\ntitle = \"No Date\"\n+++\n\nThis is teh body.\n"
		if words := wordsOf(spellingOf(t, broken)); strings.Join(words, ",") != "teh" {
			t.Errorf("words = %v, want only teh", words)
		}
	})
}

// -- both lanes, and the tree -----------------------------------------------

func TestAnalyzeBuffer(t *testing.T) {
	hygiene.Isolate(t)
	project := analysisProject(t)
	entry := entryOf(t, "proj", project)

	t.Run("both lanes come back", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n# Two\n\nThis is teh body.\n")
		findings, err := AnalyzeBuffer(entry, postHelloName, content, nil, effects.Unbound())
		if err != nil {
			t.Fatalf("AnalyzeBuffer: %v", err)
		}
		if words := wordsOf(findings.Spelling); strings.Join(words, ",") != "teh" {
			t.Errorf("words = %v, want only teh", words)
		}
		if !hasCode(findings.Lints, "SEO001") {
			t.Errorf("lints = %v", codesOf(findings.Lints))
		}
	})

	t.Run("analysing a buffer writes nothing", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n# Two\n\nThis is teh body.\n")
		before := treeFingerprint(t, project)
		if _, err := AnalyzeBuffer(entry, postHelloName, content, nil, effects.Unbound()); err != nil {
			t.Fatalf("AnalyzeBuffer: %v", err)
		}
		if after := treeFingerprint(t, project); after != before {
			t.Error("analysing the buffer mutated the working tree")
		}
	})

	t.Run("a remote entry is refused", func(t *testing.T) {
		remoteEntry, err := writeRegistry(t, remote(t, "afar")).Get("afar")
		if err != nil {
			t.Fatalf("Get(afar): %v", err)
		}
		_, analyzeErr := AnalyzeBuffer(remoteEntry, postHelloName, cleanPost, nil, effects.Unbound())
		if editorError := wantEditorError(t, analyzeErr); editorError.Status != 501 {
			t.Errorf("status = %d, want 501", editorError.Status)
		}
	})
}

// -- the wire ---------------------------------------------------------------

func TestTheAnalysisEndpoint(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	project := analysisProject(t)
	reg := writeRegistry(t, local("proj", project))
	port := serveEditor(t, newState(t, reg, nil))

	t.Run("it answers both lanes", func(t *testing.T) {
		content := analysisPost("# Hello World\n\n# Two\n\nThis is teh body.\n")
		status, body := requestJSON(t, port, "POST",
			"/api/repos/proj/analysis?path="+postHelloName, content)
		if status != 200 {
			t.Fatalf("status = %d, want 200: %v", status, body)
		}
		if body["repo"] != "proj" || body["path"] != postHelloName {
			t.Errorf("the answer reads %v / %v", body["repo"], body["path"])
		}
		if words := jsonWords(t, body); strings.Join(words, ",") != "teh" {
			t.Errorf("words = %v, want only teh", words)
		}
		if !contains(jsonCodes(t, body), "SEO001") {
			t.Errorf("lints = %v", jsonCodes(t, body))
		}
	})

	t.Run("it writes nothing", func(t *testing.T) {
		content := analysisPost("# Hello World\n\nThis is teh body.\n")
		before := treeFingerprint(t, project)
		if status, _ := request(t, port, "POST",
			"/api/repos/proj/analysis?path="+postHelloName, content); status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		if after := treeFingerprint(t, project); after != before {
			t.Error("the analysis endpoint mutated the working tree")
		}
	})

	t.Run("a buffer the renderer would refuse still gets findings", func(t *testing.T) {
		// The reason analysis is a sibling of the preview, not a passenger.
		broken := "+++\ntitle = \"No Date\"\n+++\n\nThis is teh body.\n"
		status, body := requestJSON(t, port, "POST",
			"/api/repos/proj/analysis?path="+postHelloName, broken)
		if status != 200 {
			t.Fatalf("status = %d, want 200: %v", status, body)
		}
		if words := jsonWords(t, body); strings.Join(words, ",") != "teh" {
			t.Errorf("words = %v, want only teh", words)
		}
		if codes := jsonCodes(t, body); strings.Join(codes, ",") != "POST001" {
			t.Errorf("codes = %v, want POST001", codes)
		}

		previewStatus, _ := request(t, port, "POST",
			"/api/repos/proj/preview?path="+postHelloName, broken)
		if previewStatus != 400 {
			t.Errorf("the preview answered %d, want 400", previewStatus)
		}
	})

	t.Run("a path outside the posts directory is refused", func(t *testing.T) {
		status, _ := request(t, port, "POST",
			"/api/repos/proj/analysis?path=..%2Fescape.md", cleanPost)
		if status != 400 {
			t.Errorf("status = %d, want 400", status)
		}
	})
}

// findWord returns the one finding for a word, failing when there is not one.
func findWord(t *testing.T, findings []SpellingFinding, word string) SpellingFinding {
	t.Helper()
	var found []SpellingFinding
	for _, finding := range findings {
		if finding.Word == word {
			found = append(found, finding)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d findings for %q, want one: %v", len(found), word, wordsOf(findings))
	}
	return found[0]
}

// runeSlice returns the characters of a string between two character offsets.
func runeSlice(text string, from, to int) string {
	return sliceRunes([]rune(text), from, to)
}

// jsonWords returns the words an analysis body reported.
func jsonWords(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, _ := body["spelling"].([]any)
	words := []string{}
	for _, item := range raw {
		finding, _ := item.(map[string]any)
		words = append(words, finding["word"].(string))
	}
	return words
}

// jsonCodes returns the codes an analysis body reported.
func jsonCodes(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, _ := body["lints"].([]any)
	codes := []string{}
	for _, item := range raw {
		finding, _ := item.(map[string]any)
		codes = append(codes, finding["code"].(string))
	}
	return codes
}
