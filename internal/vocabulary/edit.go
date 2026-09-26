package vocabulary

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	tomledit "github.com/smm-h/go-toml-edit"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
)

// The arrays of tables the two files hold, and the field each is sorted by.
const (
	acceptedArray = "accepted"
	rejectedArray = "rejected"
	pendingArray  = "pending"
)

// Edit is what one editing command changed: the files it wrote, relative to
// the project root, and a one-line account of each change.
type Edit struct {
	// Files are the files written, relative to the project root, in slash
	// form.
	Files []string
	// Changes are the changes made, one line each.
	Changes []string
}

// Accept adds an accepted word to the project's terms file, in sorted
// position.
//
// It refuses a word already accepted (as a word or an alias, in either layer),
// a word a rejected term covers, and a word pending review, which is resolved
// with [Approve] instead.
func Accept(h *effects.Handle, baseDir, word, meaning string) (Edit, error) {
	vocab, err := Load(baseDir)
	if err != nil {
		return Edit{}, err
	}
	if pending, isPending := vocab.PendingWord(word); isPending {
		return Edit{}, fmt.Errorf(
			"%q is pending review (%s): approve it with 'selfdoc vocabulary approve %s', or approve it with a corrected meaning with 'selfdoc vocabulary approve %s --meaning <text>'",
			word, Where(layout.ReviewRel, pending.Line), pending.Word, pending.Word)
	}
	if err := vocab.refuseAccepting(word); err != nil {
		return Edit{}, err
	}
	if err := insertEntry(h, baseDir, layout.TermsRel, EmptyTerms, acceptedArray, word, []field{
		{"word", word}, {"meaning", meaning},
	}); err != nil {
		return Edit{}, err
	}
	return Edit{
		Files:   []string{layout.TermsRel},
		Changes: []string{fmt.Sprintf("accepted %q in %s", word, layout.TermsRel)},
	}, nil
}

// refuseAccepting refuses a word the vocabulary cannot take as accepted.
func (v *Vocabulary) refuseAccepting(word string) error {
	for _, accepted := range v.AllAccepted() {
		for _, written := range accepted.Spellings() {
			if Fold(written) == Fold(word) {
				return fmt.Errorf("%q is already accepted (%s)", word, Where(accepted.Source, accepted.Line))
			}
		}
	}
	for _, rejected := range v.AllRejected() {
		if NewMatcher(rejected).Covers(word) {
			return fmt.Errorf(
				"%q is rejected as a %s %q (%s): %s. Accepting it would make the lists disagree",
				word, rejected.Kind, rejected.Pattern, Where(rejected.Source, rejected.Line), rejected.Reason)
		}
	}
	return nil
}

// Reject adds a rejected term to the project's terms file, in sorted position.
//
// It refuses a pattern already rejected in either layer, and one that covers
// an accepted word.
func Reject(h *effects.Handle, baseDir, pattern, kind, reason string) (Edit, error) {
	vocab, err := Load(baseDir)
	if err != nil {
		return Edit{}, err
	}
	for _, rejected := range vocab.AllRejected() {
		if foldSpaces(rejected.Pattern) == foldSpaces(pattern) {
			return Edit{}, fmt.Errorf("%q is already rejected as a %s (%s)",
				pattern, rejected.Kind, Where(rejected.Source, rejected.Line))
		}
	}
	matcher := NewMatcher(Rejected{Pattern: pattern, Kind: kind})
	// Baseline words are checked first and all at once: removing a project
	// word cannot clear a rejection that also covers a baseline word, so that
	// refusal is the one that names a remedy which works.
	var baselineCovered []string
	for _, accepted := range vocab.Baseline.Accepted {
		for _, written := range accepted.Spellings() {
			if matcher.Covers(written) {
				baselineCovered = append(baselineCovered, strconv.Quote(written))
			}
		}
	}
	if len(baselineCovered) > 0 {
		return Edit{}, fmt.Errorf(
			"rejecting %q as a %s would reject words %s accepts: %s. %s, so narrow the pattern until it covers none of them: %s",
			pattern, kind, BaselineSource, strings.Join(baselineCovered, ", "), baselineIsSelfdocs, narrowingFix)
	}
	for _, accepted := range vocab.Project.Accepted {
		for _, written := range accepted.Spellings() {
			if matcher.Covers(written) {
				return Edit{}, fmt.Errorf(
					"rejecting %q as a %s would reject %q, which is accepted (%s). Remove the accepted entry first with 'selfdoc vocabulary remove %s'",
					pattern, kind, written, Where(accepted.Source, accepted.Line), accepted.Word)
			}
		}
	}
	if err := insertEntry(h, baseDir, layout.TermsRel, EmptyTerms, rejectedArray, pattern, []field{
		{"pattern", pattern}, {"kind", kind}, {"reason", reason},
	}); err != nil {
		return Edit{}, err
	}
	return Edit{
		Files:   []string{layout.TermsRel},
		Changes: []string{fmt.Sprintf("rejected %q as a %s in %s", pattern, kind, layout.TermsRel)},
	}, nil
}

// baselineIsSelfdocs states why a project cannot resolve a disagreement with
// a baseline word by changing the word.
const baselineIsSelfdocs = "A word of " + BaselineSource + " is changed only in selfdoc itself, for every project, never by a project"

// narrowingFix is how a project narrows a rejected pattern: it rejects the
// specific words it meant, one entry each.
const narrowingFix = "reject the specific words meant, one at a time, with 'selfdoc vocabulary reject <word> --kind word --reason <text>'"

// Remove deletes every entry of the project's terms file whose word or
// pattern is word, compared case-insensitively.
//
// It reads the file without the conflict check, because removing an entry is
// how a word both accepted and rejected is resolved. An entry of the baseline
// is not the project's to remove, and is refused by name.
func Remove(h *effects.Handle, baseDir, word string) (Edit, error) {
	path := layout.Path(baseDir, layout.TermsRel)
	project, err := loadTermsFile(path, layout.TermsRel)
	if err != nil {
		return Edit{}, err
	}
	var acceptedIndexes, rejectedIndexes []int
	for index, accepted := range project.Accepted {
		if Fold(accepted.Word) == Fold(word) {
			acceptedIndexes = append(acceptedIndexes, index)
		}
	}
	for index, rejected := range project.Rejected {
		if foldSpaces(rejected.Pattern) == foldSpaces(word) {
			rejectedIndexes = append(rejectedIndexes, index)
		}
	}
	if len(acceptedIndexes)+len(rejectedIndexes) == 0 {
		baseline, baselineErr := LoadBaseline()
		if baselineErr == nil && baselineHolds(baseline, word) {
			return Edit{}, fmt.Errorf("%q is in %s, not in %s, and a project cannot remove it", word, BaselineSource, layout.TermsRel)
		}
		return Edit{}, fmt.Errorf("%s has no entry whose word or pattern is %q", layout.TermsRel, word)
	}
	if err := deleteEntries(h, path, map[string][]int{
		acceptedArray: acceptedIndexes, rejectedArray: rejectedIndexes,
	}); err != nil {
		return Edit{}, err
	}
	var changes []string
	for range acceptedIndexes {
		changes = append(changes, fmt.Sprintf("removed the accepted %q from %s", word, layout.TermsRel))
	}
	for range rejectedIndexes {
		changes = append(changes, fmt.Sprintf("removed the rejected %q from %s", word, layout.TermsRel))
	}
	return Edit{Files: []string{layout.TermsRel}, Changes: changes}, nil
}

// baselineHolds reports whether the baseline carries word as an accepted word
// or a rejected pattern.
func baselineHolds(baseline List, word string) bool {
	for _, accepted := range baseline.Accepted {
		if Fold(accepted.Word) == Fold(word) {
			return true
		}
	}
	for _, rejected := range baseline.Rejected {
		if foldSpaces(rejected.Pattern) == foldSpaces(word) {
			return true
		}
	}
	return false
}

// Approve moves a pending word into the accepted list, with its guessed
// meaning or, when meaning is non-empty, the corrected one.
func Approve(h *effects.Handle, baseDir, word, meaning string) (Edit, error) {
	vocab, err := Load(baseDir)
	if err != nil {
		return Edit{}, err
	}
	index, pending, err := vocab.pendingIndex(word)
	if err != nil {
		return Edit{}, err
	}
	if meaning == "" {
		meaning = pending.Meaning
	}
	if err := vocab.refuseAccepting(pending.Word); err != nil {
		return Edit{}, err
	}
	if err := insertEntry(h, baseDir, layout.TermsRel, EmptyTerms, acceptedArray, pending.Word, []field{
		{"word", pending.Word}, {"meaning", meaning},
	}); err != nil {
		return Edit{}, err
	}
	if err := deleteEntries(h, layout.Path(baseDir, layout.ReviewRel),
		map[string][]int{pendingArray: {index}}); err != nil {
		return Edit{}, err
	}
	return Edit{
		Files: []string{layout.TermsRel, layout.ReviewRel},
		Changes: []string{
			fmt.Sprintf("accepted %q in %s", pending.Word, layout.TermsRel),
			fmt.Sprintf("removed the pending %q from %s", pending.Word, layout.ReviewRel),
		},
	}, nil
}

// Drop deletes a pending word from the review list without accepting it.
func Drop(h *effects.Handle, baseDir, word string) (Edit, error) {
	vocab, err := Load(baseDir)
	if err != nil {
		return Edit{}, err
	}
	index, pending, err := vocab.pendingIndex(word)
	if err != nil {
		return Edit{}, err
	}
	if err := deleteEntries(h, layout.Path(baseDir, layout.ReviewRel),
		map[string][]int{pendingArray: {index}}); err != nil {
		return Edit{}, err
	}
	return Edit{
		Files:   []string{layout.ReviewRel},
		Changes: []string{fmt.Sprintf("removed the pending %q from %s", pending.Word, layout.ReviewRel)},
	}, nil
}

// pendingIndex finds a pending word's position in the review list.
func (v *Vocabulary) pendingIndex(word string) (int, Pending, error) {
	for index, pending := range v.Pending {
		if Fold(pending.Word) == Fold(word) {
			return index, pending, nil
		}
	}
	return 0, Pending{}, fmt.Errorf("%q is not pending in %s", word, layout.ReviewRel)
}

// field is one key of a new entry, in the order it is written.
type field struct {
	key   string
	value string
}

// insertEntry appends one [[array]] entry to a file and moves it to its sorted
// position among the array's entries, preserving every comment and every
// other entry's order. A missing file starts as empty.
func insertEntry(h *effects.Handle, baseDir, rel, empty, array, sortKey string, fields []field) error {
	if err := layout.EnsureDir(h, baseDir, layout.VocabularyRel); err != nil {
		return err
	}
	path := layout.Path(baseDir, rel)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw = []byte(empty)
	} else if err != nil {
		return err
	}
	document, err := tomledit.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	existing := arrayNodes(document, array)
	keys := make([]string, len(existing))
	for index := range existing {
		keys[index] = entryKey(document, array, index)
	}
	if err := document.NewArrayTable(array); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	for _, f := range fields {
		if err := document.SetCreate(array+"[-1]."+f.key, f.value); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	children := document.Children()
	added := len(children) - 1
	// The first existing entry the new one sorts before, or none.
	var before tomledit.Node
	for index, key := range keys {
		if Less(sortKey, key) {
			before = existing[index]
			break
		}
	}
	order := make([]int, 0, len(children))
	for index, child := range children[:added] {
		if before != nil && child == before {
			order = append(order, added)
		}
		order = append(order, index)
	}
	if before == nil {
		order = append(order, added)
	}
	if err := document.PermuteChildren("", order); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return writeKeepingMode(h, path, withBlankLines(document.Bytes()))
}

// entryKey reads the sort field of one existing entry: its word, or its
// pattern. The schema requires one of the two, so an entry answering neither
// sorts first.
func entryKey(document *tomledit.Document, array string, index int) string {
	for _, name := range []string{"word", "pattern"} {
		value, err := document.GetString(array + "[" + strconv.Itoa(index) + "]." + name)
		if err == nil {
			return value
		}
	}
	return ""
}

// arrayNodes are the header nodes of an array of tables, in document order.
func arrayNodes(document *tomledit.Document, array string) []tomledit.Node {
	entry, ok := document.Root().Get(array)
	if !ok {
		return nil
	}
	records, ok := entry.Records()
	if !ok {
		return nil
	}
	nodes := make([]tomledit.Node, 0, len(records))
	for _, record := range records {
		node, _ := record.Node()
		nodes = append(nodes, node)
	}
	return nodes
}

// deleteEntries removes entries of arrays of tables by index and writes the
// file back. Indexes are removed from the highest down, so each one still
// names the entry it named when it was read.
func deleteEntries(h *effects.Handle, path string, indexes map[string][]int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	document, err := tomledit.Parse(raw)
	if err != nil {
		return err
	}
	for array, list := range indexes {
		for i := len(list) - 1; i >= 0; i-- {
			if err := document.Delete(array + "[" + strconv.Itoa(list[i]) + "]"); err != nil {
				return err
			}
		}
	}
	return writeKeepingMode(h, path, withBlankLines(document.Bytes()))
}

// writeKeepingMode writes a vocabulary file atomically with the mode it
// already has, or the ordinary 0644 of a file a person edits when it is new.
func writeKeepingMode(h *effects.Handle, path string, content []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return h.AtomicWrite(path, content, mode)
}

// withBlankLines puts one blank line before every [[header]] written flush
// against what precedes it, and trims the file to one trailing newline. An
// entry written by a command then looks like one written by hand.
func withBlankLines(raw []byte) []byte {
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var out []string
	for index, line := range lines {
		if index > 0 && strings.HasPrefix(line, "[[") && strings.TrimSpace(out[len(out)-1]) != "" &&
			!strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "#") {
			out = append(out, "")
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n") + "\n")
}
