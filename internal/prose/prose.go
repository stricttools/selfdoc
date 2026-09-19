// Package prose holds the shared unit-pickers that extract complete
// linguistic units from text.
//
// Every summary selfdoc emits (bullet text, frontmatter "description",
// llms.txt and Atom-feed entries, auto-extracted meta descriptions) is a
// complete linguistic unit: a whole first sentence or a whole first paragraph.
// There are no character caps, no ellipses and no synthesized punctuation --
// the text is never truncated mid-word and never gains a period it did not
// already have.
//
// Two granularities: [FirstSentence] returns the first sentence of the first
// paragraph, and [FirstParagraph] returns the whole first paragraph with its
// soft-wrapped lines joined into one line.
//
// [JoinWrappedLines] normalizes source-wrapped prose (Go, JSDoc and KDoc doc
// comments wrap at about 75 columns) by joining soft-wrapped lines within a
// paragraph while leaving code blocks, list items and doctest lines verbatim.
package prose

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/util"
)

// abbreviations are the words whose trailing period must NOT be treated as a
// sentence boundary. The candidate is the whitespace-delimited token ending at
// the period (the token includes the period itself), matched
// case-insensitively.
var abbreviations = map[string]bool{
	"e.g.": true, "i.e.": true, "etc.": true, "vs.": true, "cf.": true,
	"al.": true, "esp.": true, "approx.": true,
	"dr.": true, "mr.": true, "mrs.": true, "ms.": true, "prof.": true,
	"sr.": true, "jr.": true, "st.": true,
	"vol.": true, "no.": true, "nos.": true, "fig.": true, "figs.": true,
	"eq.": true, "ref.": true, "refs.": true,
	"inc.": true, "ltd.": true, "co.": true, "corp.": true, "dept.": true,
	"univ.": true,
}

// findSentenceEnd returns the byte offset just past the first sentence
// terminator in text, and whether one was found.
//
// A terminator is ".", "!" or "?" followed by whitespace or the end of the
// text. Decimals and versions ("3.14", "v1.0") are naturally excluded because
// their internal period is followed by a digit, not whitespace. The words in
// [abbreviations] are guarded so their period does not split the sentence.
func findSentenceEnd(text string) (int, bool) {
	for i, ch := range text {
		if ch != '.' && ch != '!' && ch != '?' {
			continue
		}
		// Must be followed by whitespace or end-of-text.
		if next := i + utf8.RuneLen(ch); next < len(text) {
			r, _ := utf8.DecodeRuneInString(text[next:])
			if !util.IsPythonSpace(r) {
				continue
			}
		}
		if ch == '.' {
			// The whitespace-delimited token ending at this period.
			k := i
			for k > 0 {
				r, size := utf8.DecodeLastRuneInString(text[:k])
				if util.IsPythonSpace(r) {
					break
				}
				k -= size
			}
			token := text[k : i+1]
			if abbreviations[strings.ToLower(token)] {
				continue
			}
		}
		return i + 1, true
	}
	return 0, false
}

// FirstParagraph returns the first paragraph of text as a single joined line.
//
// Leading blank lines are skipped and the paragraph ends at the next blank
// line. Soft-wrapped physical lines within the paragraph are joined with a
// single space, so the result is one complete linguistic unit. No caps, no
// ellipsis, no synthesized punctuation.
func FirstParagraph(text string) string {
	if text == "" {
		return ""
	}
	var para []string
	for _, line := range strings.Split(text, "\n") {
		stripped := util.PythonStrip(line)
		if stripped == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, stripped)
	}
	return strings.Join(para, " ")
}

// FirstSentence returns the first sentence of the first paragraph of text.
//
// The sentence includes its terminating ".", "!" or "?". When the paragraph
// contains no sentence terminator the whole paragraph is returned unchanged --
// a complete unit is always emitted and punctuation is never synthesized.
func FirstSentence(text string) string {
	para := FirstParagraph(text)
	if para == "" {
		return ""
	}
	end, ok := findSentenceEnd(para)
	if !ok {
		return para
	}
	return para[:end]
}

// isListItem reports whether a stripped line begins a Markdown-style list
// item: "-", "*" or "+" followed by a space, or a run of digits followed by
// ". " or ") ".
func isListItem(stripped string) bool {
	switch firstRunes(stripped, 2) {
	case "- ", "* ", "+ ":
		return true
	}
	i := 0
	for i < len(stripped) {
		r, size := utf8.DecodeRuneInString(stripped[i:])
		if !unicode.IsDigit(r) {
			break
		}
		i += size
	}
	if i == 0 {
		return false
	}
	switch firstRunes(stripped[i:], 2) {
	case ". ", ") ":
		return true
	}
	return false
}

// isATXHeading reports whether a stripped line is an ATX heading, "#" through
// "######" followed by whitespace or nothing.
func isATXHeading(stripped string) bool {
	hashes := 0
	for hashes < len(stripped) && stripped[hashes] == '#' {
		hashes++
	}
	if hashes < 1 || hashes > 6 {
		return false
	}
	rest := stripped[hashes:]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return util.IsPythonSpace(r)
}

// JoinWrappedLines joins soft-wrapped physical lines within the paragraphs of
// text.
//
// Doc comments (Go, JSDoc, KDoc) wrap at about 75 columns, so a single
// sentence spans several physical lines. This joins those lines within a
// paragraph so downstream unit-pickers see whole sentences. Preserved
// verbatim:
//
//   - blank-line paragraph breaks
//   - fenced code blocks (``` and ~~~)
//   - indented preformatted blocks (the Go doc convention)
//   - list items ("-", "*", "+", "1.")
//   - doctest lines (">>>" and "...")
//   - ATX headings, which end the paragraph above and start nothing: a
//     heading joined to the sentence under it becomes part of the heading
//
// Idempotent: joining already-joined prose is a no-op.
func JoinWrappedLines(text string) string {
	if text == "" {
		return text
	}
	var out []string
	var para []string
	inFence := false

	flush := func() {
		if len(para) > 0 {
			out = append(out, strings.Join(para, " "))
			para = para[:0]
		}
	}

	for _, line := range strings.Split(text, "\n") {
		stripped := util.PythonStrip(line)

		if strings.HasPrefix(stripped, "```") || strings.HasPrefix(stripped, "~~~") {
			flush()
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}

		if stripped == "" {
			flush()
			out = append(out, "")
			continue
		}

		if isATXHeading(stripped) {
			flush()
			out = append(out, line)
			continue
		}

		indented := false
		if line != "" {
			r, _ := utf8.DecodeRuneInString(line)
			indented = util.IsPythonSpace(r)
		}
		isDoctest := strings.HasPrefix(stripped, ">>>") || strings.HasPrefix(stripped, "...")
		if indented || isListItem(stripped) || isDoctest {
			flush()
			out = append(out, line)
			continue
		}

		para = append(para, stripped)
	}

	flush()
	return strings.Join(out, "\n")
}

// firstRunes returns the first n runes of s, or all of s when it is shorter.
// It is Python's s[:n], which counts code points rather than bytes.
func firstRunes(s string, n int) string {
	i, count := 0, 0
	for i < len(s) && count < n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		count++
	}
	return s[:i]
}
