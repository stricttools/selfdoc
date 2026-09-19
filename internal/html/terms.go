package html

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/util"
)

// TermAnchor returns the id a definition site carries for term.
//
// Terms live in their own "term-" namespace so a term can never take an id
// a heading already owns -- heading ids come from [AssignHeadingAnchors]
// and are bare slugs.
func TermAnchor(term string) string { return "term-" + Slugify(term) }

// DeclaredTerm is one author-declared term: the term as written, the id its
// definition site carries, and the definition's HTML.
type DeclaredTerm struct {
	// Term is the term as the author wrote it, with any markup stripped.
	Term string
	// Anchor is the id the definition site already carries.
	Anchor string
	// Definition is the definition's HTML: the <dd> of a glossary entry,
	// or the text of the paragraph a standalone <dfn> stands in.
	Definition string
}

// SiteTerm is one term in the site-wide term table: where it is defined,
// under what id, with what definition, and -- once a glossary page has
// been synthesized -- the id of its entry on that page.
type SiteTerm struct {
	// Term is the term as the author wrote it.
	Term string
	// Page is the html_path of the page that defines it.
	Page string
	// Anchor is the id of the definition site on that page.
	Anchor string
	// Definition is the definition's HTML.
	Definition string
	// GlossaryAnchor is the id of this term's entry on the synthesized
	// glossary page, empty until that page has been built.
	GlossaryAnchor string
}

// SiteTerms is the site-wide term table: every term any page declared,
// keyed by the lower-cased term and kept in the order the terms were first
// seen.
//
// The order is part of the contract, not an implementation detail: the
// passes that consume the table walk it in order, and a Go map's iteration
// order would make a built site differ between two runs over the same
// sources. It stands in for the insertion-ordered dict the Python surface
// passed around.
type SiteTerms struct {
	order []*SiteTerm
	index map[string]*SiteTerm
}

// NewSiteTerms returns an empty term table.
func NewSiteTerms() *SiteTerms {
	return &SiteTerms{index: map[string]*SiteTerm{}}
}

// Add records term as defined on page, and returns the table's entry for
// it.
//
// The first page to declare a term owns it: a later declaration of the
// same term, in any casing, is ignored and the existing entry returned.
func (s *SiteTerms) Add(term, page, anchor, definition string) *SiteTerm {
	key := strings.ToLower(term)
	if existing, ok := s.index[key]; ok {
		return existing
	}
	entry := &SiteTerm{Term: term, Page: page, Anchor: anchor, Definition: definition}
	s.index[key] = entry
	s.order = append(s.order, entry)
	return entry
}

// Get returns the entry for a term, matched case-insensitively.
func (s *SiteTerms) Get(term string) (*SiteTerm, bool) {
	if s == nil {
		return nil, false
	}
	entry, ok := s.index[strings.ToLower(term)]
	return entry, ok
}

// All returns every entry, in the order the terms were first seen.
func (s *SiteTerms) All() []*SiteTerm {
	if s == nil {
		return nil
	}
	return s.order
}

// Len returns how many terms the table holds.
func (s *SiteTerms) Len() int {
	if s == nil {
		return 0
	}
	return len(s.order)
}

// Sorted returns every entry ordered by its lower-cased term, which is the
// order the glossary page lists them in.
func (s *SiteTerms) Sorted() []*SiteTerm {
	out := append([]*SiteTerm(nil), s.All()...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Term) < strings.ToLower(out[j].Term)
	})
	return out
}

var (
	glossaryEntryRE = regexp.MustCompile(`(?s)<dt><dfn([^>]*)>(.*?)</dfn></dt>\s*<dd>(.*?)</dd>`)
	paragraphRE     = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	dfnRE           = regexp.MustCompile(`(?s)<dfn([^>]*)>(.*?)</dfn>`)
	idAttrRE        = regexp.MustCompile(`id="([^"]+)"`)
	anyIDAttrRE     = regexp.MustCompile(`\sid="([^"]+)"`)
	pySpaceRunRE    = regexp.MustCompile(pySpaceClass + `+`)
	firstSentenceRE = regexp.MustCompile(`^(.*?[.!?])(?:` + pySpaceClass + `|$)`)
)

// CollectDeclaredTerms returns every author-declared term in bodyHTML, in
// document order.
//
// There is one result per definition site: a <dt><dfn> inside a glossary
// block, whose definition is its <dd>, or a standalone <dfn> in a
// paragraph, whose definition is the paragraph. The anchor is the id the
// definition site already carries, so a caller linking to it has a real
// target.
//
// Nothing here is inferred -- a term appears only where an author wrote a
// <dfn>, a definition list, or the glossary directive.
func CollectDeclaredTerms(bodyHTML string) []DeclaredTerm {
	var terms []DeclaredTerm
	seenAnchors := map[string]bool{}

	if strings.Contains(bodyHTML, `<div class="glossary">`) {
		for _, m := range glossaryEntryRE.FindAllStringSubmatch(bodyHTML, -1) {
			attrs, rawTerm, definition := m[1], m[2], m[3]
			term := util.PythonStrip(tagRE.ReplaceAllString(rawTerm, ""))
			if term == "" {
				continue
			}
			anchor := dfnAnchor(attrs, term)
			seenAnchors[anchor] = true
			terms = append(terms, DeclaredTerm{term, anchor, util.PythonStrip(definition)})
		}
	}

	for _, pm := range paragraphRE.FindAllStringSubmatchIndex(bodyHTML, -1) {
		pContent := bodyHTML[pm[2]:pm[3]]
		dfnMatch := dfnRE.FindStringSubmatch(pContent)
		if dfnMatch == nil {
			continue
		}
		// Skip a <p> inside a glossary block: those terms are already
		// collected above from their <dt>/<dd> pair.
		pStart := pm[0]
		if glossaryOpen := strings.LastIndex(bodyHTML[:pStart], `<div class="glossary">`); glossaryOpen != -1 {
			glossaryClose := strings.Index(bodyHTML[glossaryOpen:], "</div>")
			if glossaryClose == -1 || glossaryOpen+glossaryClose > pStart {
				continue
			}
		}
		attrs, rawTerm := dfnMatch[1], dfnMatch[2]
		term := util.PythonStrip(tagRE.ReplaceAllString(rawTerm, ""))
		if term == "" {
			continue
		}
		anchor := dfnAnchor(attrs, term)
		if seenAnchors[anchor] {
			continue
		}
		seenAnchors[anchor] = true
		terms = append(terms, DeclaredTerm{
			term, anchor, util.PythonStrip(tagRE.ReplaceAllString(pContent, "")),
		})
	}

	return terms
}

// dfnAnchor returns the id on a <dfn>'s attribute string, or the term's
// own.
func dfnAnchor(attrs, term string) string {
	if m := idAttrRE.FindStringSubmatch(attrs); m != nil {
		return m[1]
	}
	return TermAnchor(term)
}

// firstSentence returns the first sentence of text, for a definition
// tooltip. Text with no sentence terminator is returned whole.
func firstSentence(text string) string {
	plain := util.PythonStrip(pySpaceRunRE.ReplaceAllString(tagRE.ReplaceAllString(text, ""), " "))
	if m := firstSentenceRE.FindStringSubmatch(plain); m != nil {
		return m[1]
	}
	return plain
}

// assignDefinitionIDs gives every <dfn> without an id the id of its term.
//
// A <dfn> reaches this pass only because an author wrote one, or wrote a
// definition list or a glossary directive that renders to one. The id is
// "term-<slug>", deduplicated against every id already in the document
// (headings included) and against earlier terms, so a page never emits the
// same id twice.
func assignDefinitionIDs(html string) string {
	if !strings.Contains(html, "<dfn") {
		return html
	}
	used := map[string]bool{}
	for _, m := range anyIDAttrRE.FindAllStringSubmatch(html, -1) {
		used[m[1]] = true
	}
	return replaceAllSubmatchFunc(dfnRE, html, func(whole string, groups []string) string {
		attrs, inner := groups[1], groups[2]
		if strings.Contains(attrs, "id=") {
			return whole
		}
		term := util.PythonStrip(tagRE.ReplaceAllString(inner, ""))
		if term == "" {
			return whole
		}
		base := TermAnchor(term)
		anchor := base
		for counter := 1; used[anchor]; counter++ {
			anchor = base + "-" + strconv.Itoa(counter)
		}
		used[anchor] = true
		return `<dfn id="` + anchor + `"` + attrs + ">" + inner + "</dfn>"
	})
}

// LinkDefinitionSites turns each definition site on currentPage into a
// glossary link.
//
// The <dfn> an author wrote keeps its id and gains a tooltip with the
// definition's first sentence; its text becomes a link to the term's
// glossary entry. Other occurrences of the term on the same page are left
// alone -- [ApplyCrossPageTerms] deliberately links only terms defined
// elsewhere, and a page does not need a forest of links to a term it
// defines itself.
func LinkDefinitionSites(bodyHTML string, siteTerms *SiteTerms, currentPage, glossaryURL string) string {
	for _, info := range siteTerms.All() {
		if info.Page != currentPage {
			continue
		}
		if info.GlossaryAnchor == "" {
			continue
		}
		anchor := info.Anchor
		pattern := regexp.MustCompile(
			`(?s)<dfn id="` + regexp.QuoteMeta(anchor) + `"([^>]*)>(.*?)</dfn>`,
		)
		tooltip := EscapeHTML(firstSentence(info.Definition))
		href := glossaryURL + "#" + info.GlossaryAnchor

		replaced := false
		bodyHTML = replaceAllSubmatchFunc(pattern, bodyHTML, func(whole string, groups []string) string {
			if replaced {
				return whole
			}
			replaced = true
			attrs, inner := groups[1], groups[2]
			if strings.Contains(inner, "term-def-link") {
				return whole
			}
			title := ""
			if tooltip != "" {
				title = ` data-tooltip="` + tooltip + `"`
			}
			return `<dfn id="` + anchor + `"` + title + attrs + ">" +
				`<a class="term-def-link" href="` + href + `">` + inner + "</a>" +
				"</dfn>"
		})
	}
	return bodyHTML
}

// skipTags are the elements whose content a cross-page term link must not
// reach into.
var skipTags = map[string]bool{
	"a": true, "code": true, "pre": true, "dfn": true, "dt": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

var (
	htmlTagSplitRE = regexp.MustCompile(`<[^>]+>`)
	closeTagRE     = regexp.MustCompile(`^</([\p{L}\p{N}_]+)`)
	openTagRE      = regexp.MustCompile(`^<([\p{L}\p{N}_]+)`)
)

// ApplyCrossPageTerms links the first occurrence of each cross-page term in
// bodyHTML.
//
// For every term defined on a DIFFERENT page, the first occurrence in
// bodyHTML that is not inside an <a>, <code>, <pre>, <dfn>, <dt> or
// heading element is wrapped in an <a class="term-link"> pointing at the
// definition page. Only the first match per term is linked, to avoid link
// spam.
//
// A term can be defined on either side of the mount boundary, so the hop
// is chosen per target: prefix reaches the project's own pages and
// sitePrefix the site level, and under a mount those are two different
// roots. A caller with only one root passes it as both.
func ApplyCrossPageTerms(bodyHTML string, siteTerms *SiteTerms, currentPage, prefix, sitePrefix string) string {
	if siteTerms.Len() == 0 {
		return bodyHTML
	}

	// Terms defined on other pages, longest first so a longer multi-word
	// term matches before a shorter substring of it.
	var crossTerms []*SiteTerm
	for _, info := range siteTerms.All() {
		if info.Page != currentPage {
			crossTerms = append(crossTerms, info)
		}
	}
	sort.SliceStable(crossTerms, func(i, j int) bool {
		return len(crossTerms[i].Term) > len(crossTerms[j].Term)
	})

	for _, info := range crossTerms {
		termText := info.Term
		targetPage := info.Page

		// Both currentPage and targetPage are mount-relative html_paths,
		// so the target is reached through the hop the page rendering the
		// link has to whichever root the target belongs to.
		hop := PathHop(targetPage, prefix, sitePrefix)
		href := hop + HTMLPathToURL(targetPage) + "#" + info.Anchor

		// The page title for the tooltip, derived from the target's name.
		targetSlug := strings.TrimSuffix(HTMLToMdPath(targetPage), ".md")
		pageTitle := titleCase(strings.NewReplacer("-", " ", "_", " ").Replace(targetSlug))

		// Walk the HTML as alternating markup and text, tracking how deep
		// inside a skipped element the walk currently is, and replace only
		// in a text segment that is not inside one.
		var resultParts []string
		replaced := false
		segments := splitKeepDelimiters(htmlTagSplitRE, bodyHTML)
		var skipStack []string
		pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(termText))

		for _, segment := range segments {
			if replaced {
				resultParts = append(resultParts, segment)
				continue
			}
			if strings.HasPrefix(segment, "<") {
				if m := closeTagRE.FindStringSubmatch(segment); m != nil {
					tagName := strings.ToLower(m[1])
					if n := len(skipStack); n > 0 && skipStack[n-1] == tagName {
						skipStack = skipStack[:n-1]
					}
				} else if m := openTagRE.FindStringSubmatch(segment); m != nil {
					tagName := strings.ToLower(m[1])
					// A self-closing tag encloses nothing to skip.
					if skipTags[tagName] && !strings.HasSuffix(segment, "/>") {
						skipStack = append(skipStack, tagName)
					}
				}
				resultParts = append(resultParts, segment)
				continue
			}
			if len(skipStack) > 0 {
				resultParts = append(resultParts, segment)
				continue
			}
			// Match case-insensitively but keep the original casing as the
			// link text.
			loc := pattern.FindStringIndex(segment)
			if loc == nil {
				resultParts = append(resultParts, segment)
				continue
			}
			originalText := segment[loc[0]:loc[1]]
			escapedTitle := EscapeHTML("Defined in: " + pageTitle)
			link := `<a href="` + href + `" class="term-link" ` +
				`data-tooltip="` + escapedTitle + `">` + originalText + "</a>"
			resultParts = append(resultParts, segment[:loc[0]]+link+segment[loc[1]:])
			replaced = true
		}

		if replaced {
			bodyHTML = strings.Join(resultParts, "")
		}
	}

	return bodyHTML
}

// titleCase renders s the way Python's str.title() does: the first letter
// of every run of letters upper-cased and every later letter of the run
// lower-cased. Anything that is not a letter ends the run, so "a1b"
// becomes "A1B" -- which is the answer Python gives.
func titleCase(s string) string {
	var b strings.Builder
	prevIsLetter := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			b.WriteRune(r)
			prevIsLetter = false
			continue
		}
		if prevIsLetter {
			b.WriteString(strings.ToLower(string(r)))
		} else {
			b.WriteString(strings.ToUpper(string(r)))
		}
		prevIsLetter = true
	}
	return b.String()
}

// splitKeepDelimiters splits s on re, keeping the matched delimiters as
// their own elements.
//
// Python's re.split does this when the pattern carries a capture group;
// Go's Split drops the delimiters, so the walk that has to see both the
// tags and the text between them needs its own split.
func splitKeepDelimiters(re *regexp.Regexp, s string) []string {
	var out []string
	last := 0
	for _, m := range re.FindAllStringIndex(s, -1) {
		out = append(out, s[last:m[0]], s[m[0]:m[1]])
		last = m[1]
	}
	return append(out, s[last:])
}
