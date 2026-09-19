package html

import (
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/themes"
)

// A term is only ever declared, never inferred from prose: it appears only
// where an author wrote a <dfn>, a definition list, or the glossary
// directive.
func TestCollectDeclaredTermsReadsBothDefinitionSiteShapes(t *testing.T) {
	body := MdToHTML(
		"Widget\n: A widget is a thing. Extra.\n\n"+
			"<p>A <dfn>gadget</dfn> is another thing. More.</p>\n", nil, nil)
	got := CollectDeclaredTerms(body)
	if len(got) != 2 {
		t.Fatalf("got %d terms, want 2: %+v", len(got), got)
	}
	// The glossary block's <dt>/<dd> pair comes first, in document order.
	if got[0].Term != "Widget" || got[0].Anchor != "term-widget" {
		t.Errorf("first term is %+v", got[0])
	}
	if got[1].Term != "gadget" || got[1].Anchor != "term-gadget" {
		t.Errorf("second term is %+v", got[1])
	}
	// The anchor is the id the definition site already carries, so a
	// caller linking to it has a real target.
	for _, term := range got {
		if !strings.Contains(body, `id="`+term.Anchor+`"`) {
			t.Errorf("%q names anchor %q, which is on no element", term.Term, term.Anchor)
		}
	}
}

func TestProseWithNoDfnDeclaresNothing(t *testing.T) {
	if got := CollectDeclaredTerms(MdToHTML("A widget is a thing.\n", nil, nil)); len(got) != 0 {
		t.Errorf("prose alone declared %+v", got)
	}
}

// A page never emits the same id twice, headings included.
func TestDefinitionIDsAreDeduplicatedAgainstEveryIDOnThePage(t *testing.T) {
	html := MdToHTML(
		"## Widget\n\n<p>A <dfn>Widget</dfn> is a thing.</p>\n\n"+
			"<p>And a <dfn>widget</dfn> again.</p>\n", nil, nil)
	ids := idAttrsIn(html)
	if !ids["term-widget"] || !ids["term-widget-1"] {
		t.Errorf("the two definition sites did not take distinct ids; ids were %v", ids)
	}
	if n := strings.Count(html, `id="term-widget"`); n != 1 {
		t.Errorf(`id="term-widget" appears %d times`, n)
	}
}

func TestADfnThatAlreadyCarriesAnIDKeepsIt(t *testing.T) {
	html := MdToHTML(`<p>A <dfn id="mine">widget</dfn> is a thing.</p>`, nil, nil)
	mustContain(t, html, `<dfn id="mine">widget</dfn>`)
	mustNotContain(t, html, "term-widget")
}

func TestSiteTermsKeepsInsertionOrderAndFirstDeclaration(t *testing.T) {
	terms := NewSiteTerms()
	terms.Add("zeta", "a/index.html", "term-zeta", "Zeta.")
	terms.Add("alpha", "b/index.html", "term-alpha", "Alpha.")
	// The first page to declare a term owns it, in any casing.
	terms.Add("Zeta", "c/index.html", "term-zeta", "A later Zeta.")

	all := terms.All()
	if len(all) != 2 {
		t.Fatalf("got %d terms, want 2", len(all))
	}
	if all[0].Term != "zeta" || all[1].Term != "alpha" {
		t.Errorf("insertion order lost: %q then %q", all[0].Term, all[1].Term)
	}
	if all[0].Page != "a/index.html" {
		t.Errorf("a later declaration took the term: %q", all[0].Page)
	}
	// Sorted is the order the glossary page lists them in.
	sorted := terms.Sorted()
	if sorted[0].Term != "alpha" || sorted[1].Term != "zeta" {
		t.Errorf("sorted order is %q then %q", sorted[0].Term, sorted[1].Term)
	}
	if entry, ok := terms.Get("ZETA"); !ok || entry != all[0] {
		t.Error("lookup is not case-insensitive")
	}
	if _, ok := terms.Get("missing"); ok {
		t.Error("an undeclared term was found")
	}
}

func TestANilSiteTermsTableIsEmptyRatherThanAPanic(t *testing.T) {
	var terms *SiteTerms
	if terms.Len() != 0 || terms.All() != nil {
		t.Error("a nil table reported contents")
	}
	if _, ok := terms.Get("x"); ok {
		t.Error("a nil table answered a lookup")
	}
	if got := ApplyCrossPageTerms("<p>x</p>", terms, "a.html", "", ""); got != "<p>x</p>" {
		t.Errorf("a nil table changed the body: %q", got)
	}
}

func TestCrossPageTermsLinkTheFirstProseOccurrenceOnly(t *testing.T) {
	terms := NewSiteTerms()
	terms.Add("widget", "other/index.html", "term-widget", "A widget is a thing.")
	body := "<p>The widget is here. A widget again.</p>"
	got := ApplyCrossPageTerms(body, terms, "self/index.html", "", "")
	if n := strings.Count(got, `class="term-link"`); n != 1 {
		t.Errorf("got %d links, want 1:\n%s", n, got)
	}
	mustContain(t, got, `href="other/#term-widget"`,
		`data-tooltip="Defined in: Other"`)
}

func TestCrossPageTermsNeverReachIntoCodeOrHeadings(t *testing.T) {
	terms := NewSiteTerms()
	terms.Add("widget", "other/index.html", "term-widget", "A widget is a thing.")
	for _, body := range []string{
		`<h2 id="x">widget</h2>`,
		"<p><code>widget</code></p>",
		"<pre>widget</pre>",
		`<p><a href="x">widget</a></p>`,
		"<dl><dt>widget</dt></dl>",
		`<p><dfn id="d">widget</dfn></p>`,
	} {
		if got := ApplyCrossPageTerms(body, terms, "self/index.html", "", ""); got != body {
			t.Errorf("a term was linked inside a skipped element:\n%s", got)
		}
	}
}

func TestATermIsNotLinkedOnThePageThatDefinesIt(t *testing.T) {
	terms := NewSiteTerms()
	terms.Add("widget", "self/index.html", "term-widget", "A widget is a thing.")
	body := "<p>The widget is here.</p>"
	if got := ApplyCrossPageTerms(body, terms, "self/index.html", "", ""); got != body {
		t.Errorf("the defining page linked its own term:\n%s", got)
	}
}

func TestLongerTermsMatchBeforeShorterSubstrings(t *testing.T) {
	terms := NewSiteTerms()
	terms.Add("widget", "a/index.html", "term-widget", "A widget.")
	terms.Add("widget factory", "b/index.html", "term-widget-factory", "A factory.")
	got := ApplyCrossPageTerms("<p>The widget factory runs.</p>", terms, "self/index.html", "", "")
	mustContain(t, got, `href="b/#term-widget-factory"`)
	mustContain(t, got, ">widget factory</a>")
}

func TestADefinitionSiteBecomesAGlossaryLinkWithATooltip(t *testing.T) {
	terms := NewSiteTerms()
	entry := terms.Add("widget", "self/index.html", "term-widget", "A widget is a thing. More.")
	entry.GlossaryAnchor = "term-widget"
	body := `<p>A <dfn id="term-widget">widget</dfn> is a thing. More text.</p>`
	got := LinkDefinitionSites(body, terms, "self/index.html", "../glossary/")
	mustContain(t, got,
		`<a class="term-def-link" href="../glossary/#term-widget">widget</a>`,
		`data-tooltip="A widget is a thing."`)
}

func TestADefinitionSiteWithNoGlossaryEntryIsLeftAlone(t *testing.T) {
	terms := NewSiteTerms()
	terms.Add("widget", "self/index.html", "term-widget", "A widget is a thing.")
	body := `<p>A <dfn id="term-widget">widget</dfn> is a thing.</p>`
	if got := LinkDefinitionSites(body, terms, "self/index.html", "../glossary/"); got != body {
		t.Errorf("a term with no glossary entry was linked:\n%s", got)
	}
}

func TestFirstSentenceKeepsWholeUnits(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"First sentence. Second one.", "First sentence."},
		{"No terminator here", "No terminator here"},
		{"<p>Tagged <b>text</b>. More.</p>", "Tagged text."},
		{"Ends with question? Yes.", "Ends with question?"},
	} {
		if got := firstSentence(c.in); got != c.want {
			t.Errorf("firstSentence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestThemeCSSRelAnswersPlainlyWithoutMetadata(t *testing.T) {
	if got := ThemeCSSRel(nil); got != themes.DefaultCSSRel {
		t.Errorf("got %q, want %q", got, themes.DefaultCSSRel)
	}
	// A framework theme's sheet sits one level in, with fonts/ beside it.
	meta, err := themes.Meta("tinymoon")
	if err != nil {
		t.Fatal(err)
	}
	if got := ThemeCSSRel(&meta); got != themes.FrameworkCSSRel {
		t.Errorf("got %q, want %q", got, themes.FrameworkCSSRel)
	}
}

func TestGetCSSReturnsTheComposedStylesheet(t *testing.T) {
	for _, name := range themes.List() {
		css, err := GetCSS(name)
		if err != nil {
			t.Errorf("GetCSS(%q): %v", name, err)
			continue
		}
		if css == "" {
			t.Errorf("GetCSS(%q) is empty", name)
		}
	}
	if _, err := GetCSS("no-such-theme"); err == nil {
		t.Error("an unknown theme was accepted")
	}
}
