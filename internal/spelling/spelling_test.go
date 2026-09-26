// The spelling engine and its vendored word list.
//
// One engine serves the check command (SPELL001) and the spell-corpus
// command, so everything asserted here holds for both. The tests are grouped
// by the things that can independently be wrong: what the word list contains
// and whether it ships legally, how an accepted vocabulary is consulted, and
// what the scanner does and does not treat as a word.
package spelling

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// words returns the unrecognized words found in text, as plain strings.
func words(t *testing.T, text string, vocab Vocab, accepted Vocab) []string {
	t.Helper()
	found := CheckText(text, "p.md", vocab, accepted, 0, false)
	out := make([]string, 0, len(found))
	for _, miss := range found {
		out = append(out, miss.Word)
	}
	return out
}

// check runs CheckText with suggestions on.
func check(t *testing.T, text string, vocab Vocab, accepted Vocab) []Misspelling {
	t.Helper()
	return CheckText(text, "p.md", vocab, accepted, 0, true)
}

// -- The vendored word list -------------------------------------------------

func TestWordlistIsMidSized(t *testing.T) {
	// Big enough not to flag ordinary English, small enough to mean something.
	vocab := LoadWordlist()
	if len(vocab) <= 100_000 || len(vocab) >= 300_000 {
		t.Fatalf("word list holds %d words, want between 100000 and 300000", len(vocab))
	}
}

func TestOrdinaryWordsAreKnown(t *testing.T) {
	// Ordinary English, in US and British spelling alike, is accepted.
	vocab := LoadWordlist()
	for _, word := range []string{
		"the", "documentation", "subdirectory", "configure", "release",
		"colour", "behaviour", "analyse", "analyze", "runtime", "metadata",
	} {
		if !vocab.Has(word) {
			t.Errorf("%q is not in the vendored word list", word)
		}
	}
}

func TestCommonMisspellingsAreUnknown(t *testing.T) {
	// The list is an acceptance oracle, not a scrape: typos are absent.
	vocab := LoadWordlist()
	for _, word := range []string{
		"teh", "recieve", "seperate", "occured", "definately", "neccessary",
		"adress", "existance", "reponse", "paramter",
	} {
		if vocab.Has(word) {
			t.Errorf("%q is in the vendored word list", word)
		}
	}
}

func TestSourceRecordMatchesTheVendoredWords(t *testing.T) {
	// SOURCE.json's digest and count describe the words.txt actually shipped.
	record, err := WordlistSource()
	if err != nil {
		t.Fatalf("WordlistSource: %v", err)
	}
	blob := WordlistBytes()
	lines := 0
	for _, line := range strings.Split(string(blob), "\n") {
		if line != "" {
			lines++
		}
	}
	count, ok := record["word_count"].(float64)
	if !ok {
		t.Fatalf("word_count is %T, want a number", record["word_count"])
	}
	if int(count) != lines {
		t.Errorf("word_count is %d, but words.txt holds %d words", int(count), lines)
	}
	sum := sha256.Sum256(blob)
	if got := hex.EncodeToString(sum[:]); record["words_sha256"] != got {
		t.Errorf("words_sha256 is %v, but words.txt digests to %s", record["words_sha256"], got)
	}
}

func TestSourceRecordNamesItsRetrievalMethod(t *testing.T) {
	// The vendoring is reproducible: URL, parameters and script are recorded.
	record, err := WordlistSource()
	if err != nil {
		t.Fatalf("WordlistSource: %v", err)
	}
	url, _ := record["generator_url"].(string)
	if !strings.HasSuffix(url, "app.aspell.net/create") {
		t.Errorf("generator_url is %q", url)
	}
	params, _ := record["generator_params"].([]any)
	found := false
	for _, entry := range params {
		pair, _ := entry.([]any)
		if len(pair) == 2 && pair[0] == "max_size" && pair[1] == "70" {
			found = true
		}
	}
	if !found {
		t.Errorf("generator_params does not carry [max_size, 70]: %v", params)
	}
	if record["regenerate_with"] != "python scripts/regen_wordlist.py" {
		t.Errorf("regenerate_with is %v", record["regenerate_with"])
	}
	if revision, _ := record["esdb_git_revision"].(string); revision == "" {
		t.Error("esdb_git_revision is empty")
	}
}

func TestUpstreamCopyrightShipsWithTheWords(t *testing.T) {
	// Redistribution is conditioned on the notice travelling with the data.
	notice := WordlistCopyright()
	for _, phrase := range []string{
		"Kevin Atkinson", "Permission to use, copy, modify, distribute",
	} {
		if !strings.Contains(notice, phrase) {
			t.Errorf("the copyright notice does not carry %q", phrase)
		}
	}
}

func TestSubSourceNoticeIsIncludedVerbatim(t *testing.T) {
	// UKACD's own terms require its notice be displayed and included verbatim.
	notice := WordlistCopyright()
	for _, phrase := range []string{
		"UK Advanced Cryptics Dictionary", "J Ross Beresford",
		"prominently displayed",
	} {
		if !strings.Contains(notice, phrase) {
			t.Errorf("the copyright notice does not carry %q", phrase)
		}
	}
}

// -- An accepted vocabulary -------------------------------------------------

func TestAcceptedTermIsAcceptedInAnyStandardCasing(t *testing.T) {
	// An accepted term is accepted everywhere, however the sentence cases it.
	vocab := LoadWordlist()
	accepted := Vocab{"selfdoc": {}}
	text := "selfdoc and Selfdoc and SELFDOC.\n"
	if got := words(t, text, vocab, accepted); len(got) != 0 {
		t.Errorf("reported %v", got)
	}
}

// -- The renderer vocabulary ------------------------------------------------

func TestRendererVocabularyIsAcceptedWithNothingAccepted(t *testing.T) {
	// A word selfdoc's own renderers emit is accepted by selfdoc's own check.
	//
	// A consumer project's check has to pass when the project has accepted
	// nothing. Fixed renderer vocabulary is therefore carried by the engine
	// itself.
	vocab := LoadWordlist()
	for _, text := range []string{
		"- Clearable: the property that a run can empty.\n",
		"| Name | Short | Type | Presence | Env | Description |\n",
		"| a flag | | str | required | | what it is for |\n",
		"This app reads a config file; one is selected on the command line.\n",
		"Its env var is not prefixed with the app's env prefix.\n",
		"These flags are owned by the strictcli framework, not by the app.\n",
	} {
		if got := words(t, text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%q reported %v", text, got)
		}
	}
}

func TestRendererVocabularyIsNotAlreadyGeneralEnglish(t *testing.T) {
	// The set earns its keep: these are not words the vendored list holds.
	vocab := LoadWordlist()
	for _, word := range []string{"clearable", "strictcli"} {
		if vocab.Has(word) {
			t.Errorf("%q is already in the vendored list", word)
		}
	}
}

func TestTheRendererVocabularyDoesNotAcceptAMisspelling(t *testing.T) {
	// It is a small closed set, not a hole in the check.
	got := words(t, "- Clearable: teh property.\n", LoadWordlist(), Vocab{})
	if len(got) != 1 || got[0] != "teh" {
		t.Errorf("reported %v", got)
	}
}

// -- What counts as a word --------------------------------------------------

func TestNonProseIsNotScanned(t *testing.T) {
	vocab := LoadWordlist()
	tests := []struct {
		name string
		text string
	}{
		// Structure comes from the tokenizer, so code is excluded by
		// construction.
		{"fenced code block", "```python\nteh = recieve\n```\n"},
		// A ":::" directive block is its own token and carries no prose.
		{"directive block", ":::note teh\nrecieve\n:::\n"},
		// Backtick spans are blanked before the line is scanned.
		{"inline code span", "Prose with `teh recieve` inside it.\n"},
		// An inline ":-:" marker and its attributes are syntax, not prose.
		{"inline directive marker", "Before :-: ref target=\"teh\" after.\n"},
	}
	for _, test := range tests {
		if got := words(t, test.text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%s: reported %v", test.name, got)
		}
	}
}

func TestHTMLTagsWithAttributesAreNotScanned(t *testing.T) {
	// A tag is markup whether or not it carries attributes.
	//
	// The mask used to require a tag with no whitespace in it, so the moment
	// an attribute appeared the whole tag was scanned as prose and the element
	// name and attribute values were reported as misspellings.
	vocab := LoadWordlist()
	for _, text := range []string{
		`<img src="diagram.png" alt="A recieve diagram">` + "\n",
		`<a href="/x" title="teh title">visible text</a>` + "\n",
		`<br class="teh" />` + "\n",
		"<div>\n",
		"</div>\n",
		"<!-- teh recieve -->\n",
	} {
		if got := words(t, text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%q reported %v", text, got)
		}
	}
}

func TestProseAroundAnAttributeBearingTagIsStillScanned(t *testing.T) {
	// Masking the tag must not blank the sentence it sits in.
	text := `Before <img src="x.png" alt="ok"> recieve after.` + "\n"
	found := check(t, text, LoadWordlist(), Vocab{})
	if len(found) != 1 || found[0].Word != "recieve" {
		t.Fatalf("reported %v", found)
	}
	assertColumnPointsAt(t, text, found[0], "recieve")
}

func TestHTMLEntitiesAreNotScanned(t *testing.T) {
	// An entity reference is markup: its name is not a word.
	//
	// The mask covers the named form and both numeric forms, because all three
	// are the same construct spelled three ways.
	vocab := LoadWordlist()
	for _, text := range []string{
		// The entity that started this: four of them indent a table cell in
		// every CLI reference page a scoped flag appears on, and "nbsp" was
		// read as an English word four times per indent level.
		"Indented&nbsp;&nbsp;&nbsp;&nbsp;beyond the cell edge.\n",
		"A&nbsp;non-breaking space.\n",
		"Ampersand&amp;entity and an em&mdash;dash.\n",
		"A numeric&#160;entity.\n",
		"A hexadecimal&#x27;entity.\n",
	} {
		if got := words(t, text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%q reported %v", text, got)
		}
	}
}

func TestProseAroundAnEntityIsStillScanned(t *testing.T) {
	// Blanking the entity must not blank the sentence, or move its columns.
	text := "Before&nbsp;recieve after.\n"
	found := check(t, text, LoadWordlist(), Vocab{})
	if len(found) != 1 || found[0].Word != "recieve" {
		t.Fatalf("reported %v", found)
	}
	assertColumnPointsAt(t, text, found[0], "recieve")
}

func TestURLsAreNotScanned(t *testing.T) {
	// A URL is an address, not prose; its path segments are not words.
	vocab := LoadWordlist()
	for _, text := range []string{
		"See https://example.com/seperate for more.\n",
		"Mail <someone@example.com> about it.\n",
		"A [link](https://example.com/recieve) here.\n",
		"Write to mailto:teh@example.com now.\n",
	} {
		if got := words(t, text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%q reported %v", text, got)
		}
	}
}

func TestIdentifierShapedTokensAreSkipped(t *testing.T) {
	// A machine token is skipped whole -- its letters are not English.
	vocab := LoadWordlist()
	for _, chunk := range []string{
		"max_size", "os.path", "std::vector", "v0.36.0", "utf8",
		"docs/check-guide.md", "~/Projects", "user@host",
	} {
		text := fmt.Sprintf("Prose about %s here.\n", chunk)
		if got := words(t, text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%q reported %v", chunk, got)
		}
	}
}

func TestCamelCaseIsTreatedAsAnIdentifier(t *testing.T) {
	// An unbackticked symbol name is not a spelling mistake to report.
	got := words(t, "The LintResult and parseFrontmatter values.\n", LoadWordlist(), Vocab{})
	if len(got) != 0 {
		t.Errorf("reported %v", got)
	}
}

func TestAnAcronymPrefixedIdentifierIsAnIdentifierToo(t *testing.T) {
	// The same rule, on a name whose case change is between two capitals.
	//
	// "TextResponse" was already skipped because a lowercase letter sits
	// against a capital. "JSONResponse" has no such pair -- the boundary
	// between the acronym and the word is capital-against-capital -- so it was
	// read as one long English word and reported as a misspelling, while its
	// ordinary-word twin passed.
	vocab := LoadWordlist()
	for _, name := range []string{
		"JSONResponse", "HTMLResponse", "XMLHttpRequest", "IOError", "URLBuilder",
	} {
		text := fmt.Sprintf("The %s value.\n", name)
		if got := words(t, text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%q reported %v", name, got)
		}
	}
}

func TestACapitalizedMisspellingIsStillReported(t *testing.T) {
	// The acronym rule needs two capitals in a row, so a normal word starting
	// a sentence is untouched by it.
	vocab := LoadWordlist()
	for _, word := range []string{"adress", "occured", "teh"} {
		capitalized := strings.ToUpper(word[:1]) + word[1:]
		got := words(t, capitalized+" is wrong.\n", vocab, Vocab{})
		if len(got) != 1 || got[0] != capitalized {
			t.Errorf("%q reported %v", capitalized, got)
		}
	}
}

func TestEveryProseTokenTypeIsScanned(t *testing.T) {
	// Headings, table cells, lists, blockquotes and definition lists carry
	// prose and are checked; headings and table cells used to escape every
	// prose rule.
	vocab := LoadWordlist()
	tests := []struct {
		name string
		text string
		want string
	}{
		{"heading", "## A heading with teh typo\n", "teh"},
		{
			"table cell",
			"| Column | Notes |\n| --- | --- |\n| a cell | with adress |\n",
			"adress",
		},
		{"unordered list", "- item with occured\n", "occured"},
		{"ordered list", "1. item with occured\n", "occured"},
		{"blockquote", "> quoted with occured\n", "occured"},
		{"definition list", "Term\n: definition with occured\n", "occured"},
	}
	for _, test := range tests {
		got := words(t, test.text, vocab, Vocab{})
		if len(got) != 1 || got[0] != test.want {
			t.Errorf("%s: reported %v, want [%s]", test.name, got, test.want)
		}
	}
}

// TestASuperscriptIsPartOfTheWordItFollows pins the word class against
// Python's `[^\W\d_]`, which is every word character that is neither a
// decimal digit nor the underscore -- letters, but also the letterlike and
// other numerals (Nl and No). A superscript two is No, so "m²" is one word
// and is reported unknown; a class of letters alone would find the word "m"
// instead, and a lone letter is accepted as an enumeration marker.
func TestASuperscriptIsPartOfTheWordItFollows(t *testing.T) {
	got := words(t, "The m² area here.\n", LoadWordlist(), Vocab{})
	if len(got) != 1 || got[0] != "m²" {
		t.Errorf("reported %v, want [m²]", got)
	}
}

func TestHyphenatedCompoundsAreCheckedPartByPart(t *testing.T) {
	// Each part is checked honestly; only the bad part is reported.
	got := words(t, "A well-knwon compound.\n", LoadWordlist(), Vocab{})
	if len(got) != 1 || got[0] != "knwon" {
		t.Errorf("reported %v", got)
	}
}

func TestOrdinaryProseIsAccepted(t *testing.T) {
	vocab := LoadWordlist()
	tests := []struct {
		name string
		text string
	}{
		// Possessives are handled rather than flagged as unknown forms.
		{"possessives", "The tokenizer's output and the parsers' outputs.\n"},
		// Lowercase matching covers a capitalized ordinary word.
		{"capitalized sentence start", "Documentation is generated.\n"},
		// An all-caps heading matches the ordinary lowercase entry.
		{"all-caps heading", "# GETTING STARTED\n"},
		// A lone letter is an enumeration marker or an initial.
		{"single letters", "a b c point x of y\n"},
	}
	for _, test := range tests {
		if got := words(t, test.text, vocab, Vocab{}); len(got) != 0 {
			t.Errorf("%s: reported %v", test.name, got)
		}
	}
}

// -- Location and suggestions -----------------------------------------------

// assertColumnPointsAt checks that the misspelling's 1-based character column
// addresses word in its own line of text.
func assertColumnPointsAt(t *testing.T, text string, miss Misspelling, word string) {
	t.Helper()
	line := []rune(strings.Split(text, "\n")[miss.Line-1])
	if miss.Column < 1 || miss.Column > len(line) {
		t.Fatalf("column %d is outside the line", miss.Column)
	}
	if got := string(line[miss.Column-1:]); !strings.HasPrefix(got, word) {
		t.Errorf("column %d points at %q, want %q", miss.Column, got, word)
	}
}

func TestLineAndColumnPointAtTheWord(t *testing.T) {
	// A diagnostic names a position a reader can open the file at.
	text := "First line.\n\nSecond has recieve in it.\n"
	found := check(t, text, LoadWordlist(), Vocab{})
	if len(found) != 1 {
		t.Fatalf("reported %v", found)
	}
	if found[0].Line != 3 {
		t.Errorf("line is %d, want 3", found[0].Line)
	}
	assertColumnPointsAt(t, text, found[0], "recieve")
}

func TestColumnsSurviveAMaskedCodeSpan(t *testing.T) {
	// Masking keeps the line's length, so a later column is still true.
	text := "Use `--flag --other` then recieve it.\n"
	found := check(t, text, LoadWordlist(), Vocab{})
	if len(found) != 1 {
		t.Fatalf("reported %v", found)
	}
	assertColumnPointsAt(t, text, found[0], "recieve")
}

func TestLineOffsetAccountsForFrontmatter(t *testing.T) {
	// The body is scanned, but reported lines are the file's own.
	found := CheckText("Body with recieve.\n", "p.md", LoadWordlist(), Vocab{}, 4, true)
	if len(found) != 1 || found[0].Line != 5 {
		t.Fatalf("reported %v", found)
	}
}

func TestFileIsReportedVerbatim(t *testing.T) {
	// Diagnostics name the path the caller gave, unchanged.
	found := CheckText("recieve\n", "posts/2026-01-01-x.md", LoadWordlist(), Vocab{}, 0, true)
	if len(found) != 1 || found[0].File != "posts/2026-01-01-x.md" {
		t.Fatalf("reported %v", found)
	}
}

func TestSuggestionIsOfferedForAOneEditTypo(t *testing.T) {
	// Edit distance one is cheap and right often enough to be worth printing.
	got := SuggestionsFor("occured", LoadWordlist(), 3)
	found := false
	for _, suggestion := range got {
		if suggestion == "occurred" {
			found = true
		}
	}
	if !found {
		t.Errorf("suggestions for 'occured' are %v", got)
	}
}

func TestNoSuggestionRatherThanAWrongOne(t *testing.T) {
	// A word with no near neighbour gets no suggestion at all.
	if got := SuggestionsFor("zzzqqqxxvv", LoadWordlist(), 3); len(got) != 0 {
		t.Errorf("suggestions are %v", got)
	}
}

func TestSuggestionsAreCappedAtTheLimit(t *testing.T) {
	// The cap is the caller's, and it is honoured.
	if got := SuggestionsFor("cat", LoadWordlist(), 3); len(got) != 3 {
		t.Errorf("suggestions for 'cat' are %v, want three", got)
	}
}

func TestALongWordGetsNoSuggestions(t *testing.T) {
	// Over sixteen characters the candidate set stops being worth generating.
	if got := SuggestionsFor("documentationalizing", LoadWordlist(), 3); got != nil {
		t.Errorf("suggestions are %v", got)
	}
}

func TestDescribeNamesWordColumnAndSuggestions(t *testing.T) {
	// The lint message carries everything the engine located.
	found := check(t, "recieve\n", LoadWordlist(), Vocab{})
	if len(found) != 1 {
		t.Fatalf("reported %v", found)
	}
	message := found[0].Describe()
	for _, phrase := range []string{"recieve", "col 1", "receive"} {
		if !strings.Contains(message, phrase) {
			t.Errorf("message %q does not carry %q", message, phrase)
		}
	}
}

func TestDescribeOmitsTheSuggestionClauseWithNoSuggestions(t *testing.T) {
	miss := Misspelling{File: "p.md", Line: 1, Column: 4, Word: "zzqqxx"}
	if got := miss.Describe(); got != "Unrecognized word 'zzqqxx' (col 4)" {
		t.Errorf("message is %q", got)
	}
}

func TestIterUnknownWordsReportsZeroBasedColumns(t *testing.T) {
	// The line-level primitive returns raw offsets; CheckText adds the 1.
	got := IterUnknownWords("recieve", LoadWordlist())
	if len(got) != 1 || got[0].Column != 0 || got[0].Word != "recieve" {
		t.Errorf("reported %v", got)
	}
}

func TestColumnsAreCharacterOffsetsNotByteOffsets(t *testing.T) {
	// A reader opens the file at a character column, so a multi-byte character
	// earlier in the line must not push the reported column along.
	text := "Café — recieve.\n"
	found := check(t, text, LoadWordlist(), Vocab{})
	if len(found) != 1 || found[0].Word != "recieve" {
		t.Fatalf("reported %v", found)
	}
	assertColumnPointsAt(t, text, found[0], "recieve")
}

// TestScannerMatchesTheReferenceEngine holds the Go scanner against outputs
// taken from the Python engine this package replaces, on the lines where the
// two could most easily disagree: Unicode whitespace as a word separator,
// reference-style links, code spans, the word-boundary masks, the block-marker
// lookahead, curly apostrophes, hyphenated compounds, entity references,
// prose comparisons that look like tags, and inline directive attributes.
func TestScannerMatchesTheReferenceEngine(t *testing.T) {
	vocab := LoadWordlist()
	tests := []struct {
		line string
		want []UnknownWord
	}{
		// A no-break space separates words, so the identifier is skipped as
		// a machine token and the typo after it is still read as prose. Go's
		// own `\S` is ASCII-only and would have glued the two into one chunk,
		// whose underscore would then have skipped the typo too.
		{"Set max_size recieve now.", []UnknownWord{{13, "recieve"}}},
		{
			"A [ref][label] and [dest]: https://x.example",
			[]UnknownWord{{20, "dest"}},
		},
		{"See file.md and `teh` and teh.", []UnknownWord{{26, "teh"}}},
		{"Ends with a URL https://x/y", nil},
		// A scheme glued to a preceding word character is not a URL, and the
		// retry the leading word-boundary forces must not then report the
		// remainder either.
		{"Xhttp://example.com is odd.", nil},
		{"Ωhttp://example.com is odd.", nil},
		{"mailto:a@b.c and xmailto:a@b.c", nil},
		{":>: closes it", nil},
		// The block-marker mask needs whitespace or the end of the line after
		// the marker, so this one is prose carrying a stray marker.
		{":>:notspaced teh", []UnknownWord{{3, "notspaced"}, {13, "teh"}}},
		{"The café recieve line", []UnknownWord{{9, "recieve"}}},
		{"Don’t worry aboout it", []UnknownWord{{0, "Don’t"}, {12, "aboout"}}},
		{"a-b-c and teh-adress", []UnknownWord{{10, "teh"}, {14, "adress"}}},
		{"&nbsp;&#160;&#x27;&bogus", nil},
		{
			`Use <b>bold</b> and <x y="teh"> plus x < y and z > 0 recieve`,
			[]UnknownWord{{53, "recieve"}},
		},
		{`:-: ref target="teh" other="adress" teh`, []UnknownWord{{36, "teh"}}},
	}
	for _, test := range tests {
		got := IterUnknownWords(test.line, vocab)
		if len(got) != len(test.want) {
			t.Errorf("%q: reported %v, want %v", test.line, got, test.want)
			continue
		}
		for i := range got {
			if got[i] != test.want[i] {
				t.Errorf("%q: reported %v, want %v", test.line, got, test.want)
				break
			}
		}
	}
}
