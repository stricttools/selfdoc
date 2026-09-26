package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cvDocument is the authored TOML document a CV page's whole body is rendered
// out of. Its prose is where a misspelling on the rendered page has to be
// reported, because the page itself carries only a directive marker.
const cvDocument = `
format_version = 1

[identity]
name = "Ada Lovelace"
headline = "Analyst"
location = "London, England"
email = "ada@example.org"
summary = "I write notes about engines."

[[skills]]
category = "Languages"
items = ["French"]

[[projects]]
name = "Note G"
notes = ["The first published algorithm"]

[[interests]]
title = "Poetical science"
body = "Imagination is the discovering faculty."

[[education]]
degree = "Private tuition in mathematics"
years = "1833 - 1840"
institute = "University of London"
location = "London, England"

[[experience]]
role = "Translator"
period = "1842"
company = "Scientific Memoirs"
location = "London, England"

[[languages]]
name = "English"
level = "Native"

[contact]
body = "Write to ada@example.org."
`

// cvPage is a page whose whole body is the cv directive.
const cvPage = "+++\ntitle = \"CV\"\ntype = \"cv\"\ndescription = \"" +
	"The curriculum vitae of Ada Lovelace, analyst, with her skills, " +
	"projects, interests, education, work and languages.\"\n+++\n\n" +
	":-: cv path=\"stricttools/docs/cv.toml\"\n"

// spellProject writes a minimal project whose docs tree holds the given pages.
func spellProject(t *testing.T, pages map[string]string) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "src/", "language": "python"},
	))
	write(t, filepath.Join(root, "src", "__init__.py"), `"""Example package."""`+"\n")
	write(t, filepath.Join(root, "stricttools", "docs", ".keep"), "")
	for relPath, content := range pages {
		write(t, filepath.Join(root, "stricttools", "docs", relPath), content)
	}
	return root
}

// cvProject writes a project whose one page is the CV page, rendered out of
// the given document.
func cvProject(t *testing.T, document string) string {
	t.Helper()
	root := spellProject(t, map[string]string{"cv.md": cvPage})
	write(t, filepath.Join(root, "stricttools", "docs", "cv.toml"), document)
	return root
}

func TestAMisspellingOnAPageIsReported(t *testing.T) {
	root := spellProject(t, map[string]string{
		"index.md": "+++\ntitle = \"Home\"\ndescription = \"A page of ordinary prose " +
			"that says something concrete about the project and its " +
			"documentation for the reader.\"\n+++\n\n" +
			"# Home\n\nThis page is spelled corectly.\n",
	})

	result := checkFixture(t, root)

	matching := withCode(result.Lints, "SPELL001")
	if len(matching) != 1 {
		t.Fatalf("SPELL001 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if !strings.Contains(matching[0].Message(), "corectly") {
		t.Errorf("message = %q", matching[0].Message())
	}
	if matching[0].File() != "index.md" {
		t.Errorf("file = %q, want index.md", matching[0].File())
	}
	if matching[0].Line() == nil || *matching[0].Line() != 8 {
		t.Errorf("line = %v, want the page's own line 8", matching[0].Line())
	}
	if CheckResultExitCode(result, nil) != 1 {
		t.Error("a misspelling did not fail the check")
	}
}

func TestCleanProseProducesNoSpellingDiagnostic(t *testing.T) {
	root := spellProject(t, map[string]string{
		"index.md": "+++\ntitle = \"Home\"\ndescription = \"A page of ordinary prose " +
			"that says something concrete about the project and its " +
			"documentation for the reader.\"\n+++\n\n" +
			"# Home\n\nThis page is spelled correctly.\n",
	})

	result := checkFixture(t, root)

	if hasCode(result.Lints, "SPELL001") {
		t.Errorf("SPELL001 fired: %v", messagesOf(withCode(result.Lints, "SPELL001")))
	}
}

func TestAMisspellingInTheCVDocumentIsReported(t *testing.T) {
	document := strings.ReplaceAll(cvDocument,
		"Imagination is the discovering faculty.",
		"Imagination is the discovring faculty.")
	root := cvProject(t, document)

	result := checkFixture(t, root)

	matching := withCode(result.Lints, "SPELL001")
	if len(matching) != 1 {
		t.Fatalf("SPELL001 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if !strings.Contains(matching[0].Message(), "discovring") {
		t.Errorf("message = %q", matching[0].Message())
	}
	// The diagnostic names the document a reader edits, at the line the
	// word is written on, and says which page rendered it.
	if matching[0].File() != "stricttools/docs/cv.toml" {
		t.Errorf("file = %q, want stricttools/docs/cv.toml", matching[0].File())
	}
	raw, err := os.ReadFile(filepath.Join(root, "stricttools", "docs", "cv.toml"))
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	if matching[0].Line() == nil {
		t.Fatal("the diagnostic carries no line")
	}
	if !strings.Contains(lines[*matching[0].Line()-1], "discovring") {
		t.Errorf("line %d of the document is %q",
			*matching[0].Line(), lines[*matching[0].Line()-1])
	}
	if !strings.Contains(matching[0].Message(), "cv.md") {
		t.Errorf("message = %q, want it to name the page it was rendered into",
			matching[0].Message())
	}
}

func TestACleanCVDocumentProducesNoSpellingDiagnostic(t *testing.T) {
	root := cvProject(t, cvDocument)

	result := checkFixture(t, root)

	if hasCode(result.Lints, "SPELL001") {
		t.Errorf("SPELL001 fired: %v", messagesOf(withCode(result.Lints, "SPELL001")))
	}
}

func TestADocumentIsReportedOnceHoweverItWasFound(t *testing.T) {
	// The docs walk finds stricttools/docs/cv.toml, and the directive's own path names
	// the same file: a document reached both ways is held once.
	document := strings.ReplaceAll(cvDocument, "Analyst", "Analsyt")
	root := cvProject(t, document)

	result := checkFixture(t, root)

	matching := withCode(result.Lints, "SPELL001")
	if len(matching) != 1 {
		t.Fatalf("SPELL001 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if CheckResultExitCode(result, nil) != 1 {
		t.Error("a misspelling in the document did not fail the check")
	}
}

func TestACopyInTheBuildOutputIsNotReported(t *testing.T) {
	document := strings.ReplaceAll(cvDocument, "Analyst", "Analsyt")
	root := cvProject(t, document)
	// A generated copy is overwritten by the next build; it is not a source
	// anyone can fix.
	write(t, filepath.Join(root, "stricttools", ".docs-cache", "build", "cv.toml"), document)

	result := checkFixture(t, root)

	var files []string
	for _, diagnostic := range withCode(result.Lints, "SPELL001") {
		files = append(files, diagnostic.File())
	}
	if len(files) != 1 || files[0] != "stricttools/docs/cv.toml" {
		t.Errorf("files = %v, want only stricttools/docs/cv.toml", files)
	}
}

func TestASymbolNameADirectiveExtractedIsNotAMisspelling(t *testing.T) {
	root := spellProject(t, map[string]string{
		"api.md": "+++\ntitle = \"API\"\ndescription = \"The API reference for this " +
			"project's one example package, listing every public symbol it " +
			"exports for callers to use.\"\n+++\n\n# API\n\n" +
			":-: ref path=\"src\"\n",
	})
	write(t, filepath.Join(root, "stricttools", "docs", "notes.toml"),
		"# an authored document that holds none of those names\n")

	result := checkFixture(t, root)

	if hasCode(result.Lints, "SPELL001") {
		t.Errorf("SPELL001 fired for an extracted identifier: %v",
			messagesOf(withCode(result.Lints, "SPELL001")))
	}
}

func TestThePagesOwnProseIsNotReportedTwice(t *testing.T) {
	page := strings.ReplaceAll(cvPage,
		":-: cv path=\"stricttools/docs/cv.toml\"",
		"The page says recieve.\n\n:-: cv path=\"stricttools/docs/cv.toml\"")
	root := spellProject(t, map[string]string{"cv.md": page})
	write(t, filepath.Join(root, "stricttools", "docs", "cv.toml"), cvDocument)

	result := checkFixture(t, root)

	matching := withCode(result.Lints, "SPELL001")
	if len(matching) != 1 {
		t.Fatalf("SPELL001 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if matching[0].File() != "cv.md" {
		t.Errorf("file = %q, want cv.md -- the page's own prose", matching[0].File())
	}
}

func TestWholeWordLocationSemantics(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		line    string
		word    string
		offsets []int
	}{
		{name: "a word inside a longer word is not a match", line: "token", word: "ok"},
		{name: "a bare word matches", line: "ok", word: "ok", offsets: []int{0}},
		{
			name: "a digit does not extend a word",
			line: "ok_2 and ok2", word: "ok", offsets: []int{0, 9},
		},
		{
			name: "several occurrences are all located",
			line: "ok then ok", word: "ok", offsets: []int{0, 8},
		},
		{
			name: "punctuation does not extend a word",
			line: `"ok", ok.`, word: "ok", offsets: []int{1, 6},
		},
		{
			name: "a letter before the word blocks the match",
			line: "xok", word: "ok",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := wholeWordOffsets([]rune(testCase.line), []rune(testCase.word))
			if len(got) != len(testCase.offsets) {
				t.Fatalf("offsets = %v, want %v", got, testCase.offsets)
			}
			for index, want := range testCase.offsets {
				if got[index] != want {
					t.Errorf("offset %d = %d, want %d", index, got[index], want)
				}
			}
		})
	}
}
