package html

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stricttools/selfdoc/internal/tokenizer"
)

// The reference set was produced by running the Python implementation this
// package replaces over the inputs it carries, so a Go answer that differs
// from a recorded one is a port defect rather than a judgement call. It is
// committed rather than regenerated, so it keeps standing as the record of
// that behavior once the Python is gone.
//
// Code blocks with a language are deliberately absent from the Markdown
// cases: their token classes come from chroma here and came from Pygments
// there, which is the one accepted divergence. The bare fence, the diff
// fence and the annotation cases are present, because none of them reaches
// a highlighter.
const referencePath = "testdata/python_reference.json"

type headingAnchorRef struct {
	Index       int    `json:"index"`
	Level       int    `json:"level"`
	Text        string `json:"text"`
	Anchor      string `json:"anchor"`
	IsPageTitle bool   `json:"is_page_title"`
}

type declaredTermRef struct {
	Term       string `json:"term"`
	Anchor     string `json:"anchor"`
	Definition string `json:"definition"`
}

type reference struct {
	MdInputs    map[string]string `json:"md_inputs"`
	Md          map[string]string `json:"md"`
	MdStepsOff  map[string]string `json:"md_steps_off"`
	MdAPIInput  string            `json:"md_api_input"`
	MdAPIOn     string            `json:"md_api_on"`
	MdAPIOff    string            `json:"md_api_off"`
	MdAPIInput2 string            `json:"md_api_input2"`
	MdAPIOn2    string            `json:"md_api_on2"`
	MdAPIInput3 string            `json:"md_api_input3"`
	MdAPIOn3    string            `json:"md_api_on3"`
	MinifyInput map[string]string `json:"minify_js_inputs"`
	Minify      map[string]string `json:"minify_js"`
	Slugify     map[string]string `json:"slugify"`
	FirstSent   map[string]string `json:"first_sentence"`
	Paths       struct {
		MdToHTML  map[string]string `json:"md_to_html"`
		HTMLToURL map[string]string `json:"html_to_url"`
		HTMLToMd  map[string]string `json:"html_to_md"`
	} `json:"paths"`
	Rewrite []struct {
		MdPath string `json:"md_path"`
		Body   string `json:"body"`
		Legacy bool   `json:"legacy"`
		Want   string `json:"want"`
	} `json:"rewrite"`
	ParseTable []struct {
		Lines []string `json:"lines"`
		Want  string   `json:"want"`
	} `json:"parse_table"`
	HeadingAnchorsInputs map[string]string             `json:"heading_anchors_inputs"`
	HeadingAnchors       map[string][]headingAnchorRef `json:"heading_anchors"`
	HeadingAnchorsTitled []headingAnchorRef            `json:"heading_anchors_titled"`
	PageTitleAnchor      map[string]string             `json:"page_title_anchor"`
	TermAnchor           map[string]string             `json:"term_anchor"`
	CollectTermsBody     string                        `json:"collect_terms_body"`
	CollectTerms         []declaredTermRef             `json:"collect_terms"`
	CrossPage            struct {
		Body string `json:"body"`
		Want string `json:"want"`
	} `json:"cross_page"`
	LinkSites struct {
		Body string `json:"body"`
		Want string `json:"want"`
	} `json:"link_sites"`
}

func loadReference(t *testing.T) reference {
	t.Helper()
	data, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("reading %s: %v", referencePath, err)
	}
	var ref reference
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatalf("decoding %s: %v", referencePath, err)
	}
	return ref
}

func TestMdToHTMLMatchesReference(t *testing.T) {
	ref := loadReference(t)
	if len(ref.MdInputs) == 0 {
		t.Fatal("the reference set carries no Markdown cases")
	}
	for name, in := range ref.MdInputs {
		want, ok := ref.Md[name]
		if !ok {
			t.Fatalf("%s: input with no recorded output", name)
		}
		if got := MdToHTML(in, nil, nil); got != want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestMdToHTMLHeuristicOptOutsMatchReference(t *testing.T) {
	ref := loadReference(t)
	off := map[string]any{"auto_steps": false}
	for name, want := range ref.MdStepsOff {
		if got := MdToHTML(ref.MdInputs[name], off, nil); got != want {
			t.Errorf("auto_steps false on %s:\n got %q\nwant %q", name, got, want)
		}
	}
	if got := MdToHTML(ref.MdAPIInput, map[string]any{"auto_api": true}, nil); got != ref.MdAPIOn {
		t.Errorf("auto_api true:\n got %q\nwant %q", got, ref.MdAPIOn)
	}
	if got := MdToHTML(ref.MdAPIInput, nil, nil); got != ref.MdAPIOff {
		t.Errorf("auto_api default:\n got %q\nwant %q", got, ref.MdAPIOff)
	}
	on := map[string]any{"auto_api": true}
	if got := MdToHTML(ref.MdAPIInput2, on, nil); got != ref.MdAPIOn2 {
		t.Errorf("auto_api on a single entry:\n got %q\nwant %q", got, ref.MdAPIOn2)
	}
	if got := MdToHTML(ref.MdAPIInput3, on, nil); got != ref.MdAPIOn3 {
		t.Errorf("auto_api on a long code block:\n got %q\nwant %q", got, ref.MdAPIOn3)
	}
}

func TestMinifyJSMatchesReference(t *testing.T) {
	ref := loadReference(t)
	if len(ref.MinifyInput) == 0 {
		t.Fatal("the reference set carries no JavaScript cases")
	}
	for name, in := range ref.MinifyInput {
		if got := MinifyJS(in); got != ref.Minify[name] {
			t.Errorf("%s:\n got %q\nwant %q", name, got, ref.Minify[name])
		}
	}
}

func TestSlugifyMatchesReference(t *testing.T) {
	ref := loadReference(t)
	for in, want := range ref.Slugify {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstSentenceMatchesReference(t *testing.T) {
	ref := loadReference(t)
	for in, want := range ref.FirstSent {
		if got := firstSentence(in); got != want {
			t.Errorf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPathHelpersMatchReference(t *testing.T) {
	ref := loadReference(t)
	for in, want := range ref.Paths.MdToHTML {
		if got := MdToHTMLPath(in); got != want {
			t.Errorf("MdToHTMLPath(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range ref.Paths.HTMLToURL {
		if got := HTMLPathToURL(in); got != want {
			t.Errorf("HTMLPathToURL(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range ref.Paths.HTMLToMd {
		if got := HTMLToMdPath(in); got != want {
			t.Errorf("HTMLToMdPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteInternalLinksMatchesReference(t *testing.T) {
	ref := loadReference(t)
	if len(ref.Rewrite) == 0 {
		t.Fatal("the reference set carries no link-rewrite cases")
	}
	for _, c := range ref.Rewrite {
		got := RewriteInternalLinks(c.Body, c.MdPath, c.Legacy)
		if got != c.Want {
			t.Errorf("on %s (legacy=%v):\n got %q\nwant %q", c.MdPath, c.Legacy, got, c.Want)
		}
	}
}

func TestParseTableMatchesReference(t *testing.T) {
	ref := loadReference(t)
	for _, c := range ref.ParseTable {
		if got := ParseTable(c.Lines); got != c.Want {
			t.Errorf("ParseTable(%q):\n got %q\nwant %q", c.Lines, got, c.Want)
		}
	}
}

func TestAssignHeadingAnchorsMatchesReference(t *testing.T) {
	ref := loadReference(t)
	for name, in := range ref.HeadingAnchorsInputs {
		assertAnchors(t, name, AssignHeadingAnchors(tokenizer.Tokenize(in), nil), ref.HeadingAnchors[name])
	}
	title := "From Frontmatter"
	assertAnchors(t, "dupes with a frontmatter title",
		AssignHeadingAnchors(tokenizer.Tokenize(ref.HeadingAnchorsInputs["dupes"]), &title),
		ref.HeadingAnchorsTitled)
}

func assertAnchors(t *testing.T, name string, got []HeadingAnchor, want []headingAnchorRef) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: got %d anchors, want %d", name, len(got), len(want))
		return
	}
	for i := range got {
		w := want[i]
		if got[i] != (HeadingAnchor{w.Index, w.Level, w.Text, w.Anchor, w.IsPageTitle}) {
			t.Errorf("%s anchor %d: got %+v, want %+v", name, i, got[i], w)
		}
	}
}

func TestAnchorHelpersMatchReference(t *testing.T) {
	ref := loadReference(t)
	for in, want := range ref.PageTitleAnchor {
		if got := PageTitleAnchor(in); got != want {
			t.Errorf("PageTitleAnchor(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range ref.TermAnchor {
		if got := TermAnchor(in); got != want {
			t.Errorf("TermAnchor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollectDeclaredTermsMatchesReference(t *testing.T) {
	ref := loadReference(t)
	got := CollectDeclaredTerms(ref.CollectTermsBody)
	if len(got) != len(ref.CollectTerms) {
		t.Fatalf("got %d terms, want %d: %+v", len(got), len(ref.CollectTerms), got)
	}
	for i, w := range ref.CollectTerms {
		if got[i] != (DeclaredTerm{w.Term, w.Anchor, w.Definition}) {
			t.Errorf("term %d: got %+v, want %+v", i, got[i], w)
		}
	}
}

func TestApplyCrossPageTermsMatchesReference(t *testing.T) {
	ref := loadReference(t)
	terms := NewSiteTerms()
	terms.Add("widget", "other/index.html", "term-widget", "A widget is a thing.")
	got := ApplyCrossPageTerms(ref.CrossPage.Body, terms, "self/index.html", "../", "../../")
	if got != ref.CrossPage.Want {
		t.Errorf("\n got %q\nwant %q", got, ref.CrossPage.Want)
	}
}

func TestLinkDefinitionSitesMatchesReference(t *testing.T) {
	ref := loadReference(t)
	terms := NewSiteTerms()
	entry := terms.Add("widget", "self/index.html", "term-widget", "A widget is a thing. More.")
	entry.GlossaryAnchor = "term-widget"
	got := LinkDefinitionSites(ref.LinkSites.Body, terms, "self/index.html", "../glossary/")
	if got != ref.LinkSites.Want {
		t.Errorf("\n got %q\nwant %q", got, ref.LinkSites.Want)
	}
}
