package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// directiveDataFiles returns the documents the content rendered onto one page
// could have come out of.
//
// The documents in the docs tree, plus any existing file a directive on the
// page names in its "path" attribute. A path that names a module or a
// directory (ref, list-tree) is not a document and contributes nothing. Every
// entry is absolute, so a document reached both ways is held once and never
// reported twice.
func directiveDataFiles(
	pageDirectives []ResolvedDirective,
	docsDocuments []string,
	projectRoot string,
) []string {
	files := make([]string, len(docsDocuments))
	copy(files, docsDocuments)
	for _, resolved := range pageDirectives {
		declared := resolved.Attrs["path"]
		if declared == "" {
			continue
		}
		full, err := filepath.Abs(filepath.Join(projectRoot, declared))
		if err != nil {
			continue
		}
		if !containsString(files, full) && isFile(full) {
			files = append(files, full)
		}
	}
	return files
}

// containsString reports whether haystack carries needle.
func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// wordLocation is one place a word occupies in a data document.
type wordLocation struct {
	// File is the document's path relative to the project root, with
	// forward slashes -- how every other diagnostic names a file.
	File string
	// Line is the 1-based line the word sits on.
	Line int
	// Column is the 1-based character column the word starts at.
	Column int
}

// locateWord returns every place word occupies in dataFiles.
//
// Whole-word matches only, so "ok" inside "token" is not one. The word
// boundary is the Python's own: a match is rejected when the character before
// or after it is a letter, which is what its (?<![^\W\d_]) lookarounds
// expressed -- digits and underscores do not extend a word here, so "ok" in
// "ok_2" IS a match.
func locateWord(word string, dataFiles []string, projectRoot string) ([]wordLocation, error) {
	var hits []wordLocation
	target := []rune(word)
	for _, full := range dataFiles {
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(projectRoot, full)
		if err != nil {
			return nil, err
		}
		relative = filepath.ToSlash(relative)
		for index, line := range strings.Split(string(raw), "\n") {
			for _, column := range wholeWordOffsets([]rune(line), target) {
				hits = append(hits, wordLocation{
					File:   relative,
					Line:   index + 1,
					Column: column + 1,
				})
			}
		}
	}
	return hits, nil
}

// wholeWordOffsets returns the character offsets in line where target occurs
// with a letter on neither side.
func wholeWordOffsets(line, target []rune) []int {
	if len(target) == 0 || len(target) > len(line) {
		return nil
	}
	var offsets []int
	for start := 0; start+len(target) <= len(line); start++ {
		matched := true
		for offset, char := range target {
			if line[start+offset] != char {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if start > 0 && isWordLetter(line[start-1]) {
			continue
		}
		after := start + len(target)
		if after < len(line) && isWordLetter(line[after]) {
			continue
		}
		offsets = append(offsets, start)
	}
	return offsets
}

// isWordLetter reports whether a character extends a word for the purposes of
// [locateWord]'s boundary -- a letter, in any script.
//
// It is Python's [^\W\d_]: a word character that is neither a digit nor an
// underscore, which is a letter. A digit and an underscore therefore do NOT
// extend a word, so "ok" inside "ok_2" is a whole-word match and "ok" inside
// "token" is not.
func isWordLetter(char rune) bool {
	return unicode.IsLetter(char)
}

// spellRenderedDirectives runs SPELL001 over prose a directive rendered out of
// an authored document.
//
// A page's own prose is scanned from the raw body, where every reported column
// is a real column in the file. Prose a directive rendered has no position in
// that file at all -- a marker stands in for it -- so the resolved body is
// scanned instead and each finding is reported against the document it was
// written in, at the word's own line and column there.
//
// A word the resolved body carries but no authored document holds came out of
// source code: a module name, a symbol, a type. Identifiers are not prose, and
// the file to fix would be code rather than a document, so those are not this
// check's findings.
func spellRenderedDirectives(
	relPath, bodyContent, resolved string,
	rawMisspellings []spelling.Misspelling,
	pageDirectives []ResolvedDirective,
	docsDocuments []string,
	projectRoot string,
	words, accepted spelling.Vocab,
	vocab *vocabulary.Vocabulary,
) ([]lints.LintResult, error) {
	if resolved == "" || resolved == bodyContent {
		return nil, nil
	}
	already := map[string]bool{}
	for _, miss := range rawMisspellings {
		already[miss.Word] = true
	}
	dataFiles := directiveDataFiles(pageDirectives, docsDocuments, projectRoot)
	if len(dataFiles) == 0 {
		return nil, nil
	}

	var results []lints.LintResult
	seen := map[string]bool{}
	misspellings := spelling.CheckText(resolved, relPath, words, accepted, 0, true)
	for _, miss := range misspellings {
		if already[miss.Word] || seen[miss.Word] {
			continue
		}
		seen[miss.Word] = true
		locations, err := locateWord(miss.Word, dataFiles, projectRoot)
		if err != nil {
			return nil, err
		}
		for _, location := range locations {
			suffix := ""
			if len(miss.Suggestions) > 0 {
				suffix = "; did you mean " + strings.Join(miss.Suggestions, ", ") + "?"
			}
			results = append(results, lints.MustLintResult(
				location.File, lineOf(location.Line), "SPELL001",
				fmt.Sprintf(
					"Unrecognized word '%s' (col %d)%s -- rendered into %s.%s",
					miss.Word, location.Column, suffix, relPath,
					spellRemedy(miss.Word, vocab),
				),
			))
		}
	}
	return results, nil
}

// spellRemedy is the sentence a SPELL001 message closes with: how to resolve
// the word when it is genuine. A word pending review is resolved by the review
// commands; any other by accepting it.
func spellRemedy(word string, vocab *vocabulary.Vocabulary) string {
	if pending, isPending := vocab.PendingWord(word); isPending {
		return fmt.Sprintf(
			" It is pending review in %s, proposed as %q: approve it with 'selfdoc vocabulary approve %s', approve it with a corrected meaning with 'selfdoc vocabulary approve %s --meaning <text>', or drop the proposal with 'selfdoc vocabulary drop %s' and fix the spelling.",
			vocabulary.Where(layout.ReviewRel, pending.Line), pending.Meaning,
			pending.Word, pending.Word, pending.Word)
	}
	return fmt.Sprintf(
		" If it is a genuine term, accept it into %s with 'selfdoc vocabulary accept %s --meaning <text>'.",
		layout.TermsRel, word)
}

// rejectedTermLints reports every rejected term on the prose lines of one
// page body (VOCAB004). fmOffset turns a body line into a file line.
func rejectedTermLints(
	relPath, body string, fmOffset int, matchers []vocabulary.Matcher,
) []lints.LintResult {
	if len(matchers) == 0 {
		return nil
	}
	var results []lints.LintResult
	for _, line := range spelling.ProseLines(body) {
		for _, matcher := range matchers {
			for _, match := range matcher.Find(line.Masked) {
				// Masking keeps byte offsets and blanks only what is not
				// prose, so the matched text is the page's own.
				rejected := matcher.Rejected
				message := fmt.Sprintf(
					"'%s' (col %d) is rejected as a %s (%s): %s. Rewrite the passage without it.",
					match.Text, match.Offset+1, rejected.Kind,
					vocabulary.Where(rejected.Source, rejected.Line), rejected.Reason)
				if rejected.Source != vocabulary.BaselineSource {
					message += fmt.Sprintf(
						" If the project no longer rejects it, remove the rejection with 'selfdoc vocabulary remove %s'.",
						rejected.Pattern)
				}
				results = append(results, lints.MustLintResult(
					relPath, lineOf(line.Number+fmOffset), "VOCAB004", message,
				))
			}
		}
	}
	return results
}

// vocabularyLints are the lints about the project's vocabulary files rather
// than about one page: unused, duplicated, disagreeing and unsorted entries
// (VOCAB001, VOCAB002, VOCAB003, VOCAB005). pages are every page the run
// checks, whose raw and resolved text decide whether an accepted word is used.
func vocabularyLints(vocab *vocabulary.Vocabulary, pages map[string]docs.Doc) []lints.LintResult {
	texts := make([]string, 0, 2*len(pages))
	for _, relPath := range sortedKeys(pages) {
		texts = append(texts, pages[relPath].Raw, pages[relPath].Resolved)
	}
	var results []lints.LintResult
	for _, group := range []struct {
		code     string
		findings []vocabulary.Finding
	}{
		{"VOCAB001", vocabulary.UnusedAccepted(vocab.Project, texts)},
		{"VOCAB002", vocabulary.DuplicateEntries(vocab)},
		{"VOCAB003", vocabulary.CoveredAccepted(vocab)},
		{"VOCAB005", vocabulary.UnsortedEntries(vocab.Project, vocab.Pending)},
	} {
		for _, finding := range group.findings {
			var line *int
			if finding.Line > 0 {
				line = lineOf(finding.Line)
			}
			results = append(results, lints.MustLintResult(
				finding.File, line, group.code, finding.Message,
			))
		}
	}
	return results
}
