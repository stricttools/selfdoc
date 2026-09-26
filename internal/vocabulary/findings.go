package vocabulary

import (
	"fmt"

	"github.com/stricttools/selfdoc/internal/layout"
)

// Finding is one defect in a vocabulary file: where it sits and what to do
// about it.
type Finding struct {
	// File is the file the entry sits in, relative to the project root.
	File string
	// Line is the entry's 1-based line.
	Line int
	// Message states the defect and names the remedy.
	Message string
}

// UnsortedEntries reports the first entry of each array that sorts before the
// entry above it. Entries are kept sorted by word (pattern for a rejected
// term), case-folded.
func UnsortedEntries(project List, pending []Pending) []Finding {
	var findings []Finding
	check := func(file, array string, keys []string, lines []int) {
		for index := 1; index < len(keys); index++ {
			if Less(keys[index], keys[index-1]) {
				findings = append(findings, Finding{
					File: file, Line: lines[index],
					Message: fmt.Sprintf(
						"The [[%s]] entry %q sorts before %q, the entry above it: %s keeps each array sorted by word, case-folded. Move the entry up to its place.",
						array, keys[index], keys[index-1], file),
				})
				return
			}
		}
	}
	var keys []string
	var lines []int
	for _, entry := range project.Accepted {
		keys, lines = append(keys, entry.Word), append(lines, entry.Line)
	}
	check(project.Source, acceptedArray, keys, lines)
	keys, lines = nil, nil
	for _, entry := range project.Rejected {
		keys, lines = append(keys, entry.Pattern), append(lines, entry.Line)
	}
	check(project.Source, rejectedArray, keys, lines)
	keys, lines = nil, nil
	for _, entry := range pending {
		keys, lines = append(keys, entry.Word), append(lines, entry.Line)
	}
	check(layout.ReviewRel, pendingArray, keys, lines)
	return findings
}

// DuplicateEntries reports every project entry that repeats an entry above it
// or in the baseline: an accepted spelling (word or alias) accepted twice, or a
// pattern rejected twice.
func DuplicateEntries(v *Vocabulary) []Finding {
	var findings []Finding
	seenAccepted := map[string]Accepted{}
	for _, entry := range v.Baseline.Accepted {
		for _, written := range entry.Spellings() {
			seenAccepted[Fold(written)] = entry
		}
	}
	for _, entry := range v.Project.Accepted {
		reported := false
		for _, written := range entry.Spellings() {
			first, seen := seenAccepted[Fold(written)]
			if seen && !reported {
				findings = append(findings, Finding{
					File: v.Project.Source, Line: entry.Line,
					Message: fmt.Sprintf(
						"%q is accepted twice: here and at %s. Remove the project's entries with 'selfdoc vocabulary remove %s', then accept it once with 'selfdoc vocabulary accept %s --meaning <text>' if the project still needs its own.",
						written, Where(first.Source, first.Line), entry.Word, entry.Word),
				})
				reported = true
			}
			if !seen {
				seenAccepted[Fold(written)] = entry
			}
		}
	}
	seenRejected := map[string]Rejected{}
	for _, entry := range v.Baseline.Rejected {
		seenRejected[foldSpaces(entry.Pattern)] = entry
	}
	for _, entry := range v.Project.Rejected {
		if first, seen := seenRejected[foldSpaces(entry.Pattern)]; seen {
			findings = append(findings, Finding{
				File: v.Project.Source, Line: entry.Line,
				Message: fmt.Sprintf(
					"%q is rejected twice: here and at %s. Remove the project's entries with 'selfdoc vocabulary remove %s', then reject it once with 'selfdoc vocabulary reject %s --kind %s --reason <text>' if the project still needs its own.",
					entry.Pattern, Where(first.Source, first.Line), entry.Pattern, entry.Pattern, entry.Kind),
			})
			continue
		}
		seenRejected[foldSpaces(entry.Pattern)] = entry
	}
	return findings
}

// CoveredAccepted reports every accepted spelling a rejected suffix or prefix
// covers: a word the two lists disagree about, one naming the whole word and
// the other a part of it. Only entries the project declares are reported,
// since only those are the project's to change.
func CoveredAccepted(v *Vocabulary) []Finding {
	var findings []Finding
	for _, rejected := range v.AllRejected() {
		if rejected.Kind != KindSuffix && rejected.Kind != KindPrefix {
			continue
		}
		matcher := NewMatcher(rejected)
		for _, accepted := range v.AllAccepted() {
			if accepted.Source == BaselineSource && rejected.Source == BaselineSource {
				continue
			}
			for _, written := range accepted.Spellings() {
				if !matcher.Covers(written) {
					continue
				}
				file, line := accepted.Source, accepted.Line
				if accepted.Source == BaselineSource {
					file, line = rejected.Source, rejected.Line
				}
				findings = append(findings, Finding{
					File: file, Line: line,
					Message: fmt.Sprintf(
						"%q is accepted (%s) and the %s %q rejects it (%s): %s. Remove the entry that is wrong: 'selfdoc vocabulary remove %s' removes the accepted word, 'selfdoc vocabulary remove %s' removes the rejection.",
						written, Where(accepted.Source, accepted.Line), rejected.Kind, rejected.Pattern,
						Where(rejected.Source, rejected.Line), rejected.Reason, accepted.Word, rejected.Pattern),
				})
				break
			}
		}
	}
	return findings
}

// UnusedAccepted reports every word the project accepts that no page uses: a
// definition nothing needs. texts are the pages' text, every page once; a word
// is used when it or one of its aliases occurs in any of them,
// case-insensitively, as a whole word.
func UnusedAccepted(project List, texts []string) []Finding {
	var findings []Finding
	for _, entry := range project.Accepted {
		if usedAnywhere(entry, texts) {
			continue
		}
		findings = append(findings, Finding{
			File: project.Source, Line: entry.Line,
			Message: fmt.Sprintf(
				"%q is accepted, and no page uses it or any of its aliases. Remove it with 'selfdoc vocabulary remove %s'.",
				entry.Word, entry.Word),
		})
	}
	return findings
}

// usedAnywhere reports whether any text mentions any spelling of an entry.
func usedAnywhere(entry Accepted, texts []string) bool {
	for _, written := range entry.Spellings() {
		for _, text := range texts {
			if Uses(text, written) {
				return true
			}
		}
	}
	return false
}
