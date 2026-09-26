// Package vocabulary is a project's word lists: the words its pages may use
// that the English word list does not carry, the terms its pages may not use,
// and the words proposed for acceptance that nobody has reviewed yet.
//
// # Two layers
//
// The accepted and rejected lists come in two layers of one format. The
// baseline ([BaselineSource]) ships inside the selfdoc binary and applies to
// every project; the project's own list is [layout.TermsRel], committed with
// the project. The spell check reads these two and nothing else: no file
// outside the repository is consulted, so the same committed docs get the same
// verdict on every machine.
//
// A word both accepted and rejected -- in one file, or accepted in one layer
// and rejected in the other -- is a load-time error naming where each entry
// sits, because no verdict about that word could be right.
//
// # The review list
//
// [layout.ReviewRel] holds words someone proposed for acceptance, each with a
// guessed meaning, how sure the guess is, and the lines it came from. A pending
// word is not accepted: the spell check reports it like any unknown word, with
// a remedy naming the commands that resolve it -- `selfdoc vocabulary approve`
// and `selfdoc vocabulary drop`.
//
// # Matching
//
// Every comparison is case-insensitive. An accepted word joins the spell
// check's vocabulary as one word, so an entry spelled with a hyphen or a space
// accepts nothing on its own: the check splits prose on those, and accepts each
// part it cannot find elsewhere only when that part is accepted itself. A
// rejected term matches on word boundaries, as a whole word, a whole phrase, the
// end of a word, or the start of one.
package vocabulary

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

	tomledit "github.com/smm-h/go-toml-edit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/stricttools/selfdoc/internal/vocabulary/reviewschema"
	"github.com/stricttools/selfdoc/internal/vocabulary/termsschema"
	"github.com/stricttools/strictspec/go/strictspec"
)

// BaselineSource is how the embedded baseline is named wherever an entry's
// origin is reported.
const BaselineSource = "selfdoc's built-in baseline"

// The kinds a rejected term matches as.
const (
	KindWord   = "word"
	KindPhrase = "phrase"
	KindSuffix = "suffix"
	KindPrefix = "prefix"
)

// Kinds are the rejected-term kinds, in the order help text lists them.
var Kinds = []string{KindWord, KindPhrase, KindSuffix, KindPrefix}

// EmptyTerms is the content of a vocabulary file with no entries: the format
// gate and nothing else.
const EmptyTerms = "format_version = 1\n"

// EmptyReview is the content of a review list with no pending words.
const EmptyReview = "format_version = 1\n"

//go:embed baseline.toml
var baselineTOML []byte

// Accepted is one word a list accepts.
type Accepted struct {
	// Word is the word as the list spells it.
	Word string
	// Meaning is what the word means.
	Meaning string
	// Aliases are other spellings accepted with it.
	Aliases []string
	// Source is the file the entry sits in: [BaselineSource] or the project's
	// terms file.
	Source string
	// Line is the 1-based line the entry's header sits on.
	Line int
}

// Rejected is one term a list rejects.
type Rejected struct {
	// Pattern is the rejected text.
	Pattern string
	// Kind is how the pattern matches: one of [Kinds].
	Kind string
	// Reason is why the term is rejected.
	Reason string
	// Source is the file the entry sits in.
	Source string
	// Line is the 1-based line the entry's header sits on.
	Line int
}

// Pending is one word awaiting review.
type Pending struct {
	// Word is the word as the pages spell it.
	Word string
	// Meaning is the guessed meaning.
	Meaning string
	// Confidence is how sure the guess is, from 0 to 1.
	Confidence float64
	// Evidence are the doc lines the guess came from.
	Evidence []string
	// Line is the 1-based line the entry's header sits on.
	Line int
}

// List is one vocabulary file: the baseline, or a project's terms file.
type List struct {
	// Source names the file.
	Source string
	// Accepted are the accepted entries, in file order.
	Accepted []Accepted
	// Rejected are the rejected entries, in file order.
	Rejected []Rejected
}

// Vocabulary is everything the spell check and the vocabulary lints read for
// one project.
type Vocabulary struct {
	// Baseline is the list embedded in the binary.
	Baseline List
	// Project is the project's own terms file. A project with no file has an
	// empty list.
	Project List
	// Pending are the review list's entries, in file order.
	Pending []Pending
}

// Fold is the case-insensitive key every comparison in this package uses.
func Fold(text string) string {
	return strings.ToLower(text)
}

// LoadBaseline reads the list embedded in the binary.
func LoadBaseline() (List, error) {
	return parseTerms(baselineTOML, BaselineSource)
}

// Load reads a project's vocabulary: the embedded baseline, the project's
// terms file and its review list, and refuses a word both accepted and
// rejected.
//
// A project with no terms file accepts and rejects nothing of its own, and one
// with no review list has nothing pending: both are genuine absence, read the
// same way on every machine. A file that exists and does not validate is an
// error naming the file and what is wrong with it.
func Load(baseDir string) (*Vocabulary, error) {
	baseline, err := LoadBaseline()
	if err != nil {
		return nil, err
	}
	project, err := loadTermsFile(layout.Path(baseDir, layout.TermsRel), layout.TermsRel)
	if err != nil {
		return nil, err
	}
	pending, err := loadReviewFile(layout.Path(baseDir, layout.ReviewRel), layout.ReviewRel)
	if err != nil {
		return nil, err
	}
	vocab := &Vocabulary{Baseline: baseline, Project: project, Pending: pending}
	if err := vocab.refuseConflicts(); err != nil {
		return nil, err
	}
	return vocab, nil
}

// loadTermsFile reads one terms file. A missing file is an empty list.
func loadTermsFile(path, source string) (List, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return List{Source: source}, nil
	}
	if err != nil {
		return List{}, err
	}
	return parseTerms(raw, source)
}

// parseTerms validates and reads one terms document.
func parseTerms(raw []byte, source string) (List, error) {
	document, diags := termsschema.ValidateBytes(raw, "toml")
	if len(diags) > 0 {
		return List{}, invalid(source, "vocabulary", diags)
	}
	lines, err := entryLines(raw)
	if err != nil {
		return List{}, fmt.Errorf("%s: %w", source, err)
	}
	list := List{Source: source}
	for index, entry := range document.Accepted {
		list.Accepted = append(list.Accepted, Accepted{
			Word: entry.Word, Meaning: entry.Meaning,
			Aliases: append([]string(nil), entry.Aliases...),
			Source:  source, Line: lineAt(lines["accepted"], index),
		})
	}
	for index, entry := range document.Rejected {
		list.Rejected = append(list.Rejected, Rejected{
			Pattern: entry.Pattern, Kind: entry.Kind, Reason: entry.Reason,
			Source: source, Line: lineAt(lines["rejected"], index),
		})
	}
	return list, nil
}

// loadReviewFile reads the review list. A missing file has nothing pending.
func loadReviewFile(path, source string) ([]Pending, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	document, diags := reviewschema.ValidateBytes(raw, "toml")
	if len(diags) > 0 {
		return nil, invalid(source, "review list", diags)
	}
	lines, err := entryLines(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	var pending []Pending
	for index, entry := range document.Pending {
		pending = append(pending, Pending{
			Word: entry.Word, Meaning: entry.Meaning, Confidence: entry.Confidence,
			Evidence: append([]string(nil), entry.Evidence...),
			Line:     lineAt(lines["pending"], index),
		})
	}
	return pending, nil
}

// invalid words a validation failure, naming the file and every diagnostic.
func invalid(source, what string, diags []strictspec.Diagnostic) error {
	var detail strings.Builder
	for _, d := range diags {
		fmt.Fprintf(&detail, "\n  %s: %s [%s]", d.Path, d.Message, d.Code)
	}
	return fmt.Errorf("%s is not a valid %s:%s", source, what, detail.String())
}

// entryLines answers, for every array of tables at the root, the line each of
// its entries' headers sits on, in order.
func entryLines(raw []byte) (map[string][]int, error) {
	document, err := tomledit.Parse(raw)
	if err != nil {
		return nil, err
	}
	lines := map[string][]int{}
	for entry := range document.Root().Entries() {
		records, ok := entry.Records()
		if !ok {
			continue
		}
		for _, record := range records {
			lines[entry.Key()] = append(lines[entry.Key()], record.Span().Start.Line)
		}
	}
	return lines, nil
}

// lineAt is one entry's line, or 0 when the parser answered none.
func lineAt(lines []int, index int) int {
	if index < len(lines) {
		return lines[index]
	}
	return 0
}

// Where names one entry's place: its file and line.
func Where(source string, line int) string {
	if line > 0 && source != BaselineSource {
		return fmt.Sprintf("%s:%d", source, line)
	}
	return source
}

// AllAccepted are the accepted entries of both layers, baseline first.
func (v *Vocabulary) AllAccepted() []Accepted {
	return append(append([]Accepted{}, v.Baseline.Accepted...), v.Project.Accepted...)
}

// AllRejected are the rejected entries of both layers, baseline first.
func (v *Vocabulary) AllRejected() []Rejected {
	return append(append([]Rejected{}, v.Baseline.Rejected...), v.Project.Rejected...)
}

// SpellVocab is the set the spell check accepts on top of the English word
// list: every accepted word and alias of both layers, case-folded.
func (v *Vocabulary) SpellVocab() spelling.Vocab {
	accepted := spelling.Vocab{}
	for _, entry := range v.AllAccepted() {
		for _, written := range entry.Spellings() {
			accepted[Fold(written)] = struct{}{}
		}
	}
	return accepted
}

// Spellings are the word and its aliases.
func (a Accepted) Spellings() []string {
	return append([]string{a.Word}, a.Aliases...)
}

// PendingWord returns the pending entry for word, compared case-insensitively.
func (v *Vocabulary) PendingWord(word string) (Pending, bool) {
	for _, entry := range v.Pending {
		if Fold(entry.Word) == Fold(word) {
			return entry, true
		}
	}
	return Pending{}, false
}

// ConflictError is a word both accepted and rejected.
type ConflictError struct {
	// Accepted is the accepting entry.
	Accepted Accepted
	// Spelling is the accepted spelling the rejection names: the word or one
	// of its aliases.
	Spelling string
	// Rejected is the rejecting entry.
	Rejected Rejected
}

func (e *ConflictError) Error() string {
	target := e.Rejected.Pattern
	if e.Accepted.Source != BaselineSource {
		target = e.Accepted.Word
	}
	return fmt.Sprintf(
		"%q is both accepted (%s) and rejected as a %s (%s), so no verdict about it can be right. Remove the project's entries for it with 'selfdoc vocabulary remove %s', which removes every entry of %s whose word or pattern is %q, then add back the one that is right with 'selfdoc vocabulary accept' or 'selfdoc vocabulary reject'.",
		e.Spelling, Where(e.Accepted.Source, e.Accepted.Line),
		e.Rejected.Kind, Where(e.Rejected.Source, e.Rejected.Line),
		target, layout.TermsRel, target)
}

// refuseConflicts returns a [ConflictError] for the first word both accepted
// and rejected as the same text, in either layer.
//
// Only a rejected word or phrase can equal an accepted spelling. A rejected
// suffix or prefix that covers an accepted word is a lint, not a load error:
// the pattern names part of a word, and the author decides which entry goes.
func (v *Vocabulary) refuseConflicts() error {
	rejectedExact := map[string]Rejected{}
	for _, rejected := range v.AllRejected() {
		if rejected.Kind == KindWord || rejected.Kind == KindPhrase {
			key := foldSpaces(rejected.Pattern)
			if _, seen := rejectedExact[key]; !seen {
				rejectedExact[key] = rejected
			}
		}
	}
	for _, accepted := range v.AllAccepted() {
		for _, written := range accepted.Spellings() {
			if rejected, clash := rejectedExact[foldSpaces(written)]; clash {
				return &ConflictError{Accepted: accepted, Spelling: written, Rejected: rejected}
			}
		}
	}
	return nil
}

// foldSpaces is [Fold] with every run of whitespace written as one space, so a
// phrase compares by its words.
func foldSpaces(text string) string {
	return strings.Join(strings.Fields(Fold(text)), " ")
}

// SortKey is the order the entries of every list are kept in: case-folded,
// then as written, so two spellings of one word still order the same way every
// time.
func SortKey(text string) [2]string {
	return [2]string{Fold(text), text}
}

// Less reports whether a sorts before b under [SortKey].
func Less(a, b string) bool {
	ka, kb := SortKey(a), SortKey(b)
	if ka[0] != kb[0] {
		return ka[0] < kb[0]
	}
	return ka[1] < kb[1]
}
