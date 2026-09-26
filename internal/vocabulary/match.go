package vocabulary

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Match is one place a term occurs in a line of text.
type Match struct {
	// Offset is the 0-based character offset the match starts at.
	Offset int
	// Text is the matched text as written.
	Text string
}

// Matcher finds one rejected term in text.
type Matcher struct {
	// Rejected is the entry the matcher was built from.
	Rejected Rejected
	pattern  *regexp.Regexp
}

// NewMatcher builds the matcher of one rejected term.
func NewMatcher(rejected Rejected) Matcher {
	words := strings.Fields(rejected.Pattern)
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = regexp.QuoteMeta(word)
	}
	// A phrase's words may be separated by any run of whitespace; a single
	// word is the one-element case.
	return Matcher{
		Rejected: rejected,
		pattern:  regexp.MustCompile(`(?i)` + strings.Join(quoted, `\s+`)),
	}
}

// Find returns every place the term occurs in line.
//
// A word or a phrase matches with a word boundary on both sides. A suffix
// matches at the end of a longer word: a word character before it and a
// boundary after. A prefix matches at the start of a longer word: a boundary
// before it and a word character after.
func (m Matcher) Find(line string) []Match {
	var found []Match
	for start := 0; start <= len(line); {
		location := m.pattern.FindStringIndex(line[start:])
		if location == nil {
			break
		}
		from, to := start+location[0], start+location[1]
		if from == to {
			break
		}
		if m.accepts(line, from, to) {
			found = append(found, Match{
				Offset: utf8.RuneCountInString(line[:from]),
				Text:   line[from:to],
			})
			start = to
			continue
		}
		_, width := utf8.DecodeRuneInString(line[from:])
		start = from + width
	}
	return found
}

// accepts applies the kind's boundary rule to one candidate match.
func (m Matcher) accepts(line string, from, to int) bool {
	wordBefore := from > 0 && isWordRune(lastRune(line[:from]))
	wordAfter := to < len(line) && isWordRune(firstRune(line[to:]))
	switch m.Rejected.Kind {
	case KindSuffix:
		return wordBefore && !wordAfter
	case KindPrefix:
		return !wordBefore && wordAfter
	default:
		return !wordBefore && !wordAfter
	}
}

// Covers reports whether the term matches a whole accepted spelling: a word or
// a phrase equal to it, a suffix it ends with, or a prefix it starts with.
func (m Matcher) Covers(spelling string) bool {
	for _, match := range m.Find(spelling) {
		if match.Offset == 0 && len(match.Text) == len(spelling) {
			return true
		}
		if m.Rejected.Kind == KindSuffix && strings.HasSuffix(spelling, match.Text) {
			return true
		}
		if m.Rejected.Kind == KindPrefix && match.Offset == 0 {
			return true
		}
	}
	return false
}

// Uses reports whether text mentions a word: case-insensitively, with a word
// boundary on both sides.
func Uses(text, word string) bool {
	return len(NewMatcher(Rejected{Pattern: word, Kind: KindWord}).Find(text)) > 0
}

// isWordRune reports whether r continues a word: a letter, a digit or an
// underscore.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func lastRune(text string) rune {
	r, _ := utf8.DecodeLastRuneInString(text)
	return r
}

func firstRune(text string) rune {
	r, _ := utf8.DecodeRuneInString(text)
	return r
}
