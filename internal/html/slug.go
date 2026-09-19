package html

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/stricttools/selfdoc/internal/util"
)

var (
	tagRE = regexp.MustCompile(`<[^>]+>`)
	// Everything a slug may not carry. Python's `\w` is Unicode-aware when
	// it matches a str, so a CJK or Cyrillic heading keeps its characters
	// and only punctuation is dropped.
	nonSlugRE  = regexp.MustCompile(`[^\p{L}\p{N}_-]`)
	dashRunRE  = regexp.MustCompile(`-+`)
	slugPartRE = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slugify converts heading text to a URL-friendly slug for deep linking.
//
// HTML tags are stripped first, then the text is NFKD-normalized so an
// accented character decomposes, its combining marks are dropped,
// the result is lower-cased, spaces become hyphens, everything that is
// neither a letter, a digit, an underscore nor a hyphen is removed, runs of
// hyphens collapse to one, and the edges are trimmed of hyphens.
//
// Dropping only the combining marks is what preserves CJK and Cyrillic:
// those characters are letters, so they stay, while "Déploiement" and
// "Deploiement" slug the same way.
func Slugify(text string) string {
	text = tagRE.ReplaceAllString(text, "")
	text = norm.NFKD.String(text)
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.ToLower(b.String())
	s = strings.ReplaceAll(s, " ", "-")
	s = nonSlugRE.ReplaceAllString(s, "")
	s = dashRunRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// EscapeHTML escapes the HTML special characters "&", "<", ">" and the
// double quote, and deliberately not the apostrophe.
//
// It is [util.EscapeHTML], re-exported because every emitter in this
// package and in the page chrome above it escapes through one name.
func EscapeHTML(text string) string { return util.EscapeHTML(text) }
