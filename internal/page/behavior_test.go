package page

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/util"
)

// wrapForTest renders one page by hand, the way the Python tests called the
// wrapper directly: a body, a nav fragment, and whatever the case overrides.
func wrapForTest(t *testing.T, mutate func(*WrapOptions)) string {
	t.Helper()
	resetSelectCounter()
	opts := WrapOptions{
		BodyHTML:     "<p>Test content</p>",
		NavHTML:      "<li>Nav</li>",
		Title:        "Test Page",
		ProjectName:  "TestProject",
		Version:      "1.0.0",
		Prefix:       "",
		PageNav:      true,
		PageProgress: true,
	}
	if mutate != nil {
		mutate(&opts)
	}
	rendered, err := WrapPage(opts)
	if err != nil {
		t.Fatalf("WrapPage: %v", err)
	}
	return rendered
}

// --- The title a page's H1 comes from ---

func TestTwoH1HeadingsIsRefused(t *testing.T) {
	opts := baseOptions(src("index.md", "# First\n\nText.\n\n# Second\n\nMore.\n"))
	_, err := GenerateHTML(opts)
	if err == nil {
		t.Fatal("a page with two H1 headings must be refused")
	}
	if !strings.Contains(err.Error(), "multiple H1 headings") {
		t.Fatalf("the refusal must name the defect, got: %v", err)
	}
	if !strings.Contains(err.Error(), "line 1") ||
		!strings.Contains(err.Error(), "line 5") {
		t.Fatalf("the refusal must name every heading's line, got: %v", err)
	}
}

func TestThreeH1HeadingsIsRefused(t *testing.T) {
	opts := baseOptions(src("index.md", "# A\n\n# B\n\n# C\n"))
	_, err := GenerateHTML(opts)
	if err == nil || !strings.Contains(err.Error(), "multiple H1 headings") {
		t.Fatalf("a page with three H1 headings must be refused, got: %v", err)
	}
}

func TestNoTitleSourceIsRefused(t *testing.T) {
	opts := baseOptions(src("index.md", "## Only H2\n\nContent.\n"))
	_, err := GenerateHTML(opts)
	if err == nil || !strings.Contains(err.Error(), "no title source") {
		t.Fatalf("a page with no title source must be refused, got: %v", err)
	}
}

func TestFrontmatterTitleSatisfiesTheTitleRequirement(t *testing.T) {
	opts := baseOptions(src("index.md", "## Section\n\nContent.\n"))
	opts.Frontmatter = map[string]util.Frontmatter{
		"index.md": {"title": "My Title"},
	}
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if _, ok := files["index.html"]; !ok {
		t.Fatal("the page was not emitted")
	}
}

func TestAPageCarriesExactlyOneH1(t *testing.T) {
	opts := baseOptions(src("index.md", "# Original Title\n\n## Section\n\nText.\n"))
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	content := files["index.html"]
	if got := strings.Count(content, "<h1"); got != 1 {
		t.Fatalf("expected one H1, got %d", got)
	}
	if !strings.Contains(content, `<h1 id="original-title">`) {
		t.Fatal("the H1 must carry the page title's own anchor")
	}
	if !strings.Contains(content, `href="#original-title"`) {
		t.Fatal("the H1 must carry a link to itself")
	}
	if !strings.Contains(content, "<h2") {
		t.Fatal("the body's own headings must still be rendered")
	}
}

func TestAHeroPageCarriesOnlyTheHerosH1(t *testing.T) {
	opts := baseOptions(src("index.md", "# Welcome\n\nSome intro text.\n"))
	opts.Branding = map[string]any{"tagline": "A test project"}
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	content := files["index.html"]
	if got := strings.Count(content, "<h1"); got != 1 {
		t.Fatalf("expected one H1, got %d", got)
	}
	if !strings.Contains(content, `<h1 class="hero-title">`) {
		t.Fatal("the hero's own H1 must be the page's only one")
	}
	if strings.Contains(content, `<h1 id="welcome">`) {
		t.Fatal("the article must not add a second H1 beside the hero's")
	}
}

// --- Layout by page type ---

func TestPostPagesUseTheNarrowLayoutAndNoTableOfContents(t *testing.T) {
	toc := `<nav class="docs-toc" aria-label="On this page">` +
		`<a class="docs-toc-item" href="#sec">Section</a></nav>`
	post := wrapForTest(t, func(o *WrapOptions) {
		o.PageType = "post"
		o.TOCHTML = toc
	})
	if !strings.Contains(post, `class="docs-layout docs-layout--narrow"`) {
		t.Fatal("a post must use the narrow layout")
	}
	// One decision, both elements: the desktop aside and the mobile
	// disclosure are the same feature at two widths.
	if strings.Contains(post, `<nav class="docs-toc"`) {
		t.Fatal("a post must carry no table of contents aside")
	}
	if strings.Contains(post, "mobile-toc") {
		t.Fatal("a post must carry no mobile table of contents either")
	}

	guide := wrapForTest(t, func(o *WrapOptions) {
		o.PageType = "guide"
		o.TOCHTML = toc
	})
	if !strings.Contains(guide, `class="docs-layout"`) ||
		strings.Contains(guide, "layout--narrow") {
		t.Fatal("a guide must use the standard layout")
	}
	if !strings.Contains(guide, `<nav class="docs-toc"`) ||
		!strings.Contains(guide, `<details class="mobile-toc">`) {
		t.Fatal("a guide must carry both table-of-contents elements")
	}

	empty := wrapForTest(t, func(o *WrapOptions) { o.PageType = "guide" })
	if strings.Contains(empty, `<nav class="docs-toc"`) {
		t.Fatal("a page with no headings must carry no table of contents")
	}
}

func TestADeclaredTypeBecomesAClassOnTheContentRegion(t *testing.T) {
	declared := wrapForTest(t, func(o *WrapOptions) { o.PageType = "cv" })
	if !strings.Contains(declared, `<main id="tm-content" class="content page-cv"`) {
		t.Fatal("a declared type must reach the content region as a class")
	}
	// A derived facet type is a search filter, not a design decision.
	derived := wrapForTest(t, func(o *WrapOptions) { o.FacetType = "changelog" })
	if !strings.Contains(derived, `<main id="tm-content" class="content"`) ||
		strings.Contains(derived, "page-changelog") {
		t.Fatal("a derived facet type must not become a class")
	}
	// Nothing an author writes may close the attribute.
	unsafe := wrapForTest(t, func(o *WrapOptions) { o.PageType = `a" onload="x` })
	if strings.Contains(unsafe, "page-a") {
		t.Fatal("a type that is not a bare identifier must not become a class")
	}
}

func TestThePageStatesWhenItWasUpdatedOnce(t *testing.T) {
	rendered := wrapForTest(t, func(o *WrapOptions) {
		o.PageType = "post"
		o.DateModified = "2026-06-29"
	})
	if got := strings.Count(rendered, "Last updated"); got != 1 {
		t.Fatalf("expected one \"Last updated\", got %d", got)
	}
	if !strings.Contains(rendered, `<time datetime="2026-06-29">June 29, 2026</time>`) {
		t.Fatal("the footer must carry the date in readable form")
	}
	for _, leftover := range []string{
		"post-read-indicator", "post-last-updated", "post-updated-badge",
		`class="post-meta"`,
	} {
		if strings.Contains(rendered, leftover) {
			t.Fatalf("the deleted %q block reappeared", leftover)
		}
	}
}

func TestADocumentThatStatesItsOwnDateStandsTheFooterDown(t *testing.T) {
	// A CV whose document declares an updated date closes with its own
	// line, so the footer's generic date beside it would be the same fact
	// twice from two sources free to disagree.
	rendered := wrapForTest(t, func(o *WrapOptions) {
		o.BodyHTML = `<p>A CV.</p><p class="cv-updated">Updated June 2026</p>`
		o.DateModified = "2026-06-29"
	})
	if strings.Contains(rendered, "Last updated") {
		t.Fatal("the footer's date must stand down for a document that states its own")
	}
}

// --- The superseded-version notice ---

// archivedPage renders a page as a superseded version, which is where the
// notice belongs.
func archivedPage(t *testing.T, pageType string, archived bool) string {
	t.Helper()
	return wrapForTest(t, func(o *WrapOptions) {
		o.AssetPrefix = "../../../"
		o.PagePath = "guide/index.html"
		o.PageType = pageType
		o.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
		o.MountLocale = "en"
		o.Author = testAuthor()
		// The mount version is what makes the page version-scoped, and
		// whether it is an archive is what decides the notice.
		if archived {
			o.MountVersion = "0.9.0"
		} else {
			o.MountVersion = "1.0.0"
		}
		o.MountArchived = archived
	})
}

func TestAnArchivedPageOfAnyTypeShowsTheSupersededNotice(t *testing.T) {
	for _, pageType := range []string{
		"guide", "tutorial", "api", "cli", "reference", "changelog",
		"glossary", "",
	} {
		rendered := archivedPage(t, pageType, true)
		if !strings.Contains(rendered, `class="tm-notice tm-notice-warn"`) {
			t.Fatalf("page type %q: the notice is missing", pageType)
		}
		if !strings.Contains(rendered, "has been superseded") {
			t.Fatalf("page type %q: the notice says nothing", pageType)
		}
	}
}

func TestTheSupersededNoticeIsKeyedAndDismissable(t *testing.T) {
	rendered := archivedPage(t, "guide", true)
	if !strings.Contains(rendered, `data-notice-key="0.9.0"`) {
		t.Fatal("the dismissal must be keyed to this version")
	}
	if !strings.Contains(rendered, "tm-notice-dismiss") {
		t.Fatal("the notice must carry a dismiss button")
	}
	if !strings.Contains(rendered, "selfdoc-version-notice-") {
		t.Fatal("the script that stores the dismissal must be on the page")
	}
	// From en/v/0.9.0/guide/ out to the output root and back in to
	// en/guide/ -- the same two steps every cross-mount link takes.
	if !strings.Contains(rendered, `href="../../../../en/guide/"`) {
		t.Fatal("the notice must link the current version of this page")
	}
}

func TestTheCurrentVersionAndAnAddresslessPageShowNoNotice(t *testing.T) {
	if strings.Contains(archivedPage(t, "guide", false),
		`class="tm-notice tm-notice-warn"`) {
		t.Fatal("the current version must show no superseded notice")
	}
	// The 404 page has no address of its own, so it has no version.
	addressless := wrapForTest(t, func(o *WrapOptions) {
		o.PageType = "guide"
		o.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
	})
	if strings.Contains(addressless, `class="tm-notice tm-notice-warn"`) {
		t.Fatal("a page with no address must show no notice")
	}
}

// --- The pickers ---

// pickerPage renders a version-scoped page with the given picker
// configuration, the way the Python picker tests did.
func pickerPage(t *testing.T, mutate func(*Options)) map[string]string {
	t.Helper()
	resetSelectCounter()
	opts := baseOptions(src("index.md", "# Test\n\nHello.\n"))
	opts.Version = "1.0.0"
	opts.MountVersion = "1.0.0"
	if mutate != nil {
		mutate(&opts)
	}
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	return files
}

func TestTheVersionPickerOffersEveryVersionThatHasThePage(t *testing.T) {
	files := pickerPage(t, func(o *Options) {
		o.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
	})
	content := files["index.html"]
	if !strings.Contains(content, `<div class="sel version-picker">`) {
		t.Fatal("the version picker is missing")
	}
	if !strings.Contains(content, `role="combobox"`) ||
		!strings.Contains(content, `class="sel-opt"`) {
		t.Fatal("the picker must be the framework's combobox shape")
	}
	// Each option carries the address the build computed for it, and the
	// one being rendered is the selected option.
	if !strings.Contains(content,
		`aria-selected="true" data-value="1.0.0" data-href="./"`) {
		t.Fatal("the current version's option must address the stable page")
	}
	if !strings.Contains(content, `data-value="0.9.0" data-href="v/0.9.0/"`) {
		t.Fatal("an older version's option must address its archive copy")
	}
	if strings.Contains(content, `aria-selected="true" data-value="0.9.0"`) {
		t.Fatal("only the version being rendered may be selected")
	}
}

func TestTheVersionPickerButtonNamesItsOwnListbox(t *testing.T) {
	// A button pointing at an id no element carries reads to assistive
	// technology as a combobox with no options at all.
	files := pickerPage(t, func(o *Options) {
		o.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
	})
	content := files["index.html"]
	controls := regexp.MustCompile(`aria-controls="([^"]+)"`).
		FindStringSubmatch(content)
	if controls == nil {
		t.Fatal("the combobox names no listbox")
	}
	if !strings.Contains(content,
		`<div class="sel-menu" id="`+controls[1]+`" role="listbox">`) {
		t.Fatalf("no listbox carries the id %q the button names", controls[1])
	}
}

func TestNoVersionPickerWhereThereIsNowhereToGo(t *testing.T) {
	// One option is not a choice, so the control is not offered at all.
	single := pickerPage(t, func(o *Options) {
		o.AvailableVersions = []VersionEntry{{"1.0.0"}}
	})["index.html"]
	if strings.Contains(single, "version-picker") {
		t.Fatal("a single version must produce no picker")
	}
	// A page marked "versioned = false" has no version to switch away from.
	unversioned := pickerPage(t, func(o *Options) {
		o.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
		o.MountVersion = ""
	})["index.html"]
	if strings.Contains(unversioned, "version-picker") {
		t.Fatal("a page with no version must produce no picker")
	}
	none := pickerPage(t, nil)["index.html"]
	if strings.Contains(none, "version-picker") {
		t.Fatal("no configured versions must produce no picker")
	}
	// A version that does not hold this page is not offered, because the
	// link would name a file no build wrote.
	filtered := pickerPage(t, func(o *Options) {
		o.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
		o.VersionPages = map[string]map[string]bool{
			"1.0.0": {"index.html": true},
			"0.9.0": {"other.html": true},
		}
	})["index.html"]
	if strings.Contains(filtered, "version-picker") {
		t.Fatal("a version that lacks the page must not be offered")
	}
}

func TestTheLocalePickerAddressesThisPageInEveryLocale(t *testing.T) {
	resetSelectCounter()
	opts := baseOptions(src("index.md", "# Test\n\nHello.\n"))
	opts.Version = "1.0.0"
	opts.MountLocale = "en"
	opts.CurrentLocale = "en"
	opts.AvailableLocales = []LocaleEntry{
		{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
	}
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	content := files["en/index.html"]
	if !strings.Contains(content, `<div class="sel locale-picker">`) {
		t.Fatal("the locale picker is missing")
	}
	if !strings.Contains(content, "English") || !strings.Contains(content, "French") {
		t.Fatal("every configured locale must be offered")
	}
	if !strings.Contains(content, `data-value="fr" data-href="../fr/"`) {
		t.Fatal("each option must address this same page in the other locale")
	}
	if !strings.Contains(content, `aria-selected="true" data-value="en"`) ||
		strings.Contains(content, `aria-selected="true" data-value="fr"`) {
		t.Fatal("only the locale being rendered may be selected")
	}
	// The hidden native select is the framework's to print, never ours:
	// server-emitted markup printing one would be a banned native control.
	if strings.Contains(content, "<select") || strings.Contains(content, "<option") {
		t.Fatal("no picker may print a native form control")
	}
}

func TestNoLocalePickerForASingleLocale(t *testing.T) {
	// One locale means no locale segment and nothing to switch between.
	files := pickerPage(t, func(o *Options) {
		o.AvailableLocales = []LocaleEntry{{Code: "en", Label: "English"}}
	})
	if strings.Contains(files["index.html"], "locale-picker") {
		t.Fatal("a single locale must produce no picker")
	}
	if strings.Contains(pickerPage(t, nil)["index.html"], "locale-picker") {
		t.Fatal("no configured locales must produce no picker")
	}
}

// --- The Pagefind search surface ---

func TestPagefindFacetKeysAreTheSevenFacetsInOrder(t *testing.T) {
	want := []string{
		"version", "locale", "group", "type", "target", "project", "tags",
	}
	if len(PagefindFacetKeys) != len(want) {
		t.Fatalf("expected %d facet keys, got %d",
			len(want), len(PagefindFacetKeys))
	}
	for i, key := range want {
		if PagefindFacetKeys[i] != key {
			t.Fatalf("facet %d is %q, expected %q",
				i, PagefindFacetKeys[i], key)
		}
	}
}

func TestEveryFacetIsEmittedInsideTheIndexedBody(t *testing.T) {
	rendered := wrapForTest(t, func(o *WrapOptions) {
		o.PageType = "guide"
		o.CurrentLocale = "pt-BR"
		o.NavGroup = "Guides"
		o.DeployTarget = "cloudflare-pages"
		o.PageTags = []string{"deploy", "hosting"}
	})
	// A filter outside data-pagefind-body would never be read.
	articleStart := strings.Index(rendered, "<article")
	articleEnd := strings.Index(rendered, "</article>")
	if articleStart < 0 || articleEnd < articleStart {
		t.Fatal("the page has no article element")
	}
	articleTag := rendered[articleStart : strings.Index(
		rendered[articleStart:], ">")+articleStart+1]
	if !strings.Contains(articleTag, "data-pagefind-body") {
		t.Fatal("the article must be the indexed region")
	}
	block := rendered[articleStart:articleEnd]
	for _, filter := range []string{
		`data-pagefind-filter="version:1.0.0"`,
		`data-pagefind-filter="locale:pt-BR"`,
		`data-pagefind-filter="group:Guides"`,
		`data-pagefind-filter="type:guide"`,
		`data-pagefind-filter="target:cloudflare-pages"`,
		`data-pagefind-filter="project:TestProject"`,
		`data-pagefind-filter="tags:deploy"`,
		`data-pagefind-filter="tags:hosting"`,
	} {
		if !strings.Contains(block, filter) {
			t.Fatalf("missing %s inside the indexed body", filter)
		}
	}
	// Two filters on one element would lose one to HTML parsing.
	for _, span := range strings.Split(rendered, "<span")[1:] {
		if strings.Count(span, "data-pagefind-filter=") > 1 {
			t.Fatal("an element carries more than one filter")
		}
		if strings.Count(span, "data-pagefind-meta=") > 1 {
			t.Fatal("an element carries more than one metadata attribute")
		}
	}
}

func TestFacetValuesAreNeverSplitOrEmpty(t *testing.T) {
	// Each tag is its own element, so no value is split on a comma.
	repeated := wrapForTest(t, func(o *WrapOptions) {
		o.PageTags = []string{"a,b", "c"}
	})
	if !strings.Contains(repeated, `data-pagefind-filter="tags:a,b"`) ||
		!strings.Contains(repeated, `data-pagefind-filter="tags:c"`) {
		t.Fatal("a tag containing a comma must stay one value")
	}
	// An empty value would offer a filter group nothing matches.
	empty := wrapForTest(t, func(o *WrapOptions) { o.PageTags = []string{} })
	for _, absent := range []string{
		`data-pagefind-filter="locale:"`,
		`data-pagefind-filter="group:"`,
		`data-pagefind-filter="target:"`,
		`data-pagefind-filter="tags:`,
	} {
		if strings.Contains(empty, absent) {
			t.Fatalf("an empty facet was emitted: %s", absent)
		}
	}
	// The locale facet falls back to the mount's locale.
	mounted := wrapForTest(t, func(o *WrapOptions) { o.MountLocale = "fr" })
	if !strings.Contains(mounted, `data-pagefind-filter="locale:fr"`) {
		t.Fatal("the locale facet must fall back to the mount locale")
	}
}

func TestResultMetadataIsEmittedAndEscaped(t *testing.T) {
	withType := wrapForTest(t, func(o *WrapOptions) { o.PageType = "guide" })
	if !strings.Contains(withType, `data-pagefind-meta="type:guide"`) {
		t.Fatal("the type metadata is missing")
	}
	if !strings.Contains(withType, `data-pagefind-meta="project:TestProject"`) {
		t.Fatal("the project metadata is missing")
	}
	bare := wrapForTest(t, nil)
	if strings.Contains(bare, `data-pagefind-meta="type:`) {
		t.Fatal("a page with no type must emit no type metadata")
	}
	if strings.Contains(bare, `data-pagefind-meta="date:`) {
		t.Fatal("a page with no publication date must emit no date metadata")
	}
	dated := wrapForTest(t, func(o *WrapOptions) {
		o.DatePublished = "2024-01-15"
	})
	if !strings.Contains(dated, `data-pagefind-meta="date:2024-01-15"`) {
		t.Fatal("the date metadata is missing")
	}
	escaped := wrapForTest(t, func(o *WrapOptions) {
		o.ProjectName = `My "Project" <1>`
	})
	if !strings.Contains(escaped,
		`data-pagefind-meta="project:My &quot;Project&quot; &lt;1&gt;"`) {
		t.Fatal("a metadata value must be escaped for the attribute")
	}
}

func TestThePagefindWidgetIsServedFromTheIndexersOwnOutput(t *testing.T) {
	rendered := wrapForTest(t, nil)
	for _, needed := range []string{
		"pagefind/pagefind-ui.css", "pagefind/pagefind-ui.js",
		"pagefind-ui__search-input", `id="pagefind-container"`, "PagefindUI",
		`<dialog class="search-dialog"`, `class="search-close"`,
		"metaKey", "ctrlKey", `"k"`,
	} {
		if !strings.Contains(rendered, needed) {
			t.Fatalf("missing %q", needed)
		}
	}
	for _, absent := range []string{
		"search.js", "cdn.jsdelivr.net", `class="search-input"`,
		`id="search-results"`, "data-search-base", "search-index.json",
		"autofocus",
	} {
		if strings.Contains(rendered, absent) {
			t.Fatalf("the deleted %q reappeared", absent)
		}
	}
	lowered := strings.ToLower(rendered)
	for _, absent := range []string{"fuse", "minisearch"} {
		if strings.Contains(lowered, absent) {
			t.Fatalf("a bundled search library named %q reappeared", absent)
		}
	}
}

func TestNoBundlePathIsEverWrittenIntoThePage(t *testing.T) {
	// The UI reads document.currentScript.src and takes the directory it
	// was loaded from -- a root-absolute path, correct at every depth and
	// under every mount. A build-time value cannot be: the dynamic import()
	// that loads the index resolves relative specifiers against the
	// bundle's URL rather than the page's, so the hop that is right for the
	// script element is wrong for the import.
	for _, prefix := range []string{"", "../", "../../", "../../../../"} {
		rendered := wrapForTest(t, func(o *WrapOptions) {
			o.AssetPrefix = prefix
		})
		if strings.Contains(rendered, "bundlePath") {
			t.Fatalf("asset prefix %q wrote a bundlePath into the page", prefix)
		}
	}
	// The bundle's own assets resolve from the page's own address.
	rendered := wrapForTest(t, func(o *WrapOptions) { o.AssetPrefix = "../../" })
	if !strings.Contains(rendered, "../../pagefind/pagefind-ui.css") ||
		!strings.Contains(rendered, "../../pagefind/pagefind-ui.js") {
		t.Fatal("the widget's assets must be addressed from the page")
	}
}

// --- The body script bundle ---

func TestTheBodyBundleCarriesOnlyTheBlocksThePageNeeds(t *testing.T) {
	plain := baseOptions(src("index.md", "# Hello\n\nJust text, no code.\n"))
	plainFiles, err := GenerateHTML(plain)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	content := plainFiles["index.html"]
	for _, absent := range []string{"copy-btn", "code-tabs"} {
		if strings.Contains(content, absent) {
			t.Fatalf("a page with no code block shipped %q", absent)
		}
	}

	withCode := baseOptions(src("index.md", "# API\n\n"+
		"```python\nprint('hello')\n```\n"+
		"```go\nfmt.Println(\"hello\")\n```\n"))
	withCode.RunButton = true
	codeFiles, err := GenerateHTML(withCode)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	content = codeFiles["index.html"]
	for _, needed := range []string{
		"copy-btn", "code-tabs", "run-btn", "theme-toggle", "hamburger",
		"search-dialog", "ArrowRight", "ArrowLeft",
	} {
		if !strings.Contains(content, needed) {
			t.Fatalf("a page with code blocks is missing %q", needed)
		}
	}
}

// --- The navigation tree's structure ---

func TestTheUnversionedGroupIsAlwaysLast(t *testing.T) {
	nav := BuildNav(
		[]SourceFile{
			src("index.md", ""), src("guide.md", ""),
			src("api/endpoints.md", ""), src("api/auth.md", ""),
			src("tutorials/quickstart.md", ""),
		}, nil,
		[]SourceFile{src("about.md", "")}, nil,
	)
	last := nav[len(nav)-1]
	if last.Group != "General" || !last.Unversioned {
		t.Fatalf("the last entry is %+v, expected the General group", last)
	}
	var groups []string
	for _, item := range nav {
		if item.IsGroup() {
			groups = append(groups, item.Group)
		}
	}
	if len(groups) < 3 || groups[len(groups)-1] != "General" {
		t.Fatalf("the groups are %v, expected General last", groups)
	}
	for _, item := range last.Items {
		if !item.Unversioned {
			t.Fatalf("%q is missing the unversioned marker", item.MdPath)
		}
	}
}

func TestNoUnversionedGroupWithoutUnversionedPages(t *testing.T) {
	for _, unversioned := range [][]SourceFile{nil, {}} {
		nav := BuildNav(
			[]SourceFile{src("index.md", ""), src("guide.md", "")},
			nil, unversioned, nil,
		)
		for _, item := range nav {
			if item.Group == "General" {
				t.Fatal("a General group appeared with no unversioned pages")
			}
		}
	}
}

// --- The glossary page the terms synthesize ---

func TestTheGlossaryPageIsSynthesizedFromTheDeclaredTerms(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# Home\n\nWelcome.\n"),
		src("terms.md", "# Terms\n\nAPI\n: Application Programming Interface\n"),
	)
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	glossary, ok := files["glossary/index.html"]
	if !ok {
		t.Fatal("no glossary page was synthesized")
	}
	if !strings.Contains(glossary, "API") {
		t.Fatal("the glossary must list the declared term")
	}
	if !strings.Contains(glossary, ">Source</a>") {
		t.Fatal("each entry must link back to the page that defined the term")
	}
	if !strings.Contains(files["index.html"], ">Glossary<") {
		t.Fatal("the glossary must join every page's sidebar")
	}
}

func TestNoGlossaryPageWithoutTermsOrWithTheFeatureOff(t *testing.T) {
	bare := baseOptions(src("index.md", "# Home\n\nJust prose.\n"))
	files, err := GenerateHTML(bare)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if _, ok := files["glossary/index.html"]; ok {
		t.Fatal("a site that declares no term must get no glossary page")
	}
	if strings.Contains(files["index.html"], "DefinedTermSet") {
		t.Fatal("a page that declares no term must emit no DefinedTermSet")
	}

	off := baseOptions(
		src("index.md", "# Home\n\nWelcome.\n"),
		src("terms.md", "# Terms\n\nAPI\n: Application Programming Interface\n"),
	)
	off.Glossary = false
	files, err = GenerateHTML(off)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if _, ok := files["glossary/index.html"]; ok {
		t.Fatal("the glossary feature was turned off and a page appeared anyway")
	}
}

func TestAProjectsOwnGlossaryPageIsNotReplaced(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# Home\n\nWelcome.\n"),
		src("glossary.md", "# Glossary\n\nAPI\n: Application Programming Interface\n"),
	)
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	page := files["glossary/index.html"]
	if strings.Contains(page, `<div class="glossary"><dl>`) {
		t.Fatal("the project's own glossary page was replaced by a synthesized one")
	}
}

// --- A missing author is refused rather than invented ---

func TestAPageWithNoDeclaredAuthorIsRefused(t *testing.T) {
	opts := baseOptions(src("index.md", "# Home\n\nWelcome.\n"))
	opts.Author = nil
	_, err := GenerateHTML(opts)
	if err == nil || !strings.Contains(err.Error(), "author") {
		t.Fatalf("a build with no declared author must be refused, got: %v", err)
	}
	opts.Author = map[string]any{"name": "Jane Doe"}
	_, err = GenerateHTML(opts)
	if err == nil || !strings.Contains(err.Error(), "author") {
		t.Fatalf("an author with no URL must be refused, got: %v", err)
	}
}

// --- Internal links address the directories the build emits ---

func TestInternalLinksUseDirectoryAddresses(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# Home\n\nWelcome.\n"),
		src("guide.md", "# Guide\n\nContent.\n"),
	)
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	content := files["index.html"]
	if !strings.Contains(content, `href="guide/"`) {
		t.Fatal("a sibling page must be addressed as a directory")
	}
	for _, stale := range []string{`href="guide.html"`, `href="guide/index.html"`} {
		if strings.Contains(content, stale) {
			t.Fatalf("a stale address %s was emitted", stale)
		}
	}
}

// --- The document title the head carries ---

func TestAnInnerPageTitleIsThePageThenTheProject(t *testing.T) {
	got := DocumentTitle(DocumentTitleParts{
		PageTitle: "Guide", ProjectName: "selfdoc",
	})
	if got != "Guide - selfdoc" {
		t.Fatalf("title = %q, want \"Guide - selfdoc\"", got)
	}
}

func TestAnInnerPageOnTheSiteEndsWithTheSiteName(t *testing.T) {
	got := DocumentTitle(DocumentTitleParts{
		PageTitle: "Guide", ProjectName: "selfdoc", SiteName: "StrictTools",
	})
	if got != "Guide - selfdoc - StrictTools" {
		t.Fatalf("title = %q, want \"Guide - selfdoc - StrictTools\"", got)
	}
}

func TestAProjectIndexPageOnTheSiteNamesNoProject(t *testing.T) {
	got := DocumentTitle(DocumentTitleParts{
		PageTitle: "rlsbl", ProjectName: "rlsbl", SiteName: "StrictTools",
		IsIndexPage: true,
	})
	if got != "rlsbl - StrictTools" {
		t.Fatalf("title = %q, want \"rlsbl - StrictTools\"", got)
	}
}

func TestAStandaloneIndexPageIsTheWrittenTitleAlone(t *testing.T) {
	got := DocumentTitle(DocumentTitleParts{
		PageTitle: "selfdoc", ProjectName: "selfdoc", IsIndexPage: true,
	})
	if got != "selfdoc" {
		t.Fatalf("title = %q, want \"selfdoc\"", got)
	}
}

// TestTheHomeProjectNeverRendersItsNameTwice covers the site's own front page
// and its inner pages: the home project's name IS the site name, so a
// composition that simply appended both would publish it twice.
func TestTheHomeProjectNeverRendersItsNameTwice(t *testing.T) {
	home := DocumentTitle(DocumentTitleParts{
		PageTitle: "StrictTools", ProjectName: "StrictTools",
		SiteName: "StrictTools", IsIndexPage: true,
	})
	if home != "StrictTools" {
		t.Fatalf("the site's home page title = %q, want %q",
			home, "StrictTools")
	}
	inner := DocumentTitle(DocumentTitleParts{
		PageTitle: "About", ProjectName: "StrictTools", SiteName: "StrictTools",
	})
	if inner != "About - StrictTools" {
		t.Fatalf("a home project inner page title = %q, want %q",
			inner, "About - StrictTools")
	}
}

// TestNoDocumentTitleEverRepeatsAName sweeps the compositions a build can
// produce and asserts none of them writes the same name twice in a row.
func TestNoDocumentTitleEverRepeatsAName(t *testing.T) {
	names := []string{"", "alpha", "beta", "StrictTools"}
	for _, pageTitle := range names {
		for _, projectName := range names {
			for _, siteName := range names {
				for _, isIndex := range []bool{false, true} {
					got := DocumentTitle(DocumentTitleParts{
						PageTitle:   pageTitle,
						ProjectName: projectName,
						SiteName:    siteName,
						IsIndexPage: isIndex,
					})
					for _, name := range names[1:] {
						if strings.Contains(got, name+" - "+name) {
							t.Errorf(
								"DocumentTitle(%q, %q, %q, index=%v) = %q "+
									"repeats %q",
								pageTitle, projectName, siteName, isIndex,
								got, name)
						}
					}
				}
			}
		}
	}
}

// TestTheTitleIsNeverComposedFromTheDescription holds the ruling that titles
// are authored text: nothing a project writes in its description reaches the
// title of any page it publishes.
func TestTheTitleIsNeverComposedFromTheDescription(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# TestProject\n\nWelcome.\n"),
		src("guide.md", "# Guide\n\nProse.\n"),
	)
	opts.ConfigDescription =
		"A documentation engine that reads source code."
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	for _, pagePath := range []string{"index.html", "guide/index.html"} {
		if got := titleOf(t, files[pagePath]); strings.Contains(
			got, "documentation engine") {
			t.Errorf("%s title = %q draws on the project description",
				pagePath, got)
		}
	}
}

// TestTheRenderedTitlesOfAStandaloneBuild asserts the two shapes a project
// deployed on its own publishes.
func TestTheRenderedTitlesOfAStandaloneBuild(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# Home\n\nWelcome.\n"),
		src("guide.md", "# Guide\n\nProse.\n"),
	)
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	for pagePath, want := range map[string]string{
		"index.html":       "Home",
		"guide/index.html": "Guide - TestProject",
	} {
		if got := titleOf(t, files[pagePath]); got != want {
			t.Errorf("%s title = %q, want %q", pagePath, got, want)
		}
	}
}

// TestTheRenderedTitlesOfAnAssembledBuild asserts the two shapes a project
// mounted on the unified site publishes.
func TestTheRenderedTitlesOfAnAssembledBuild(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# TestProject\n\nWelcome.\n"),
		src("guide.md", "# Guide\n\nProse.\n"),
	)
	opts.SiteName = "StrictTools"
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	for pagePath, want := range map[string]string{
		"index.html":       "TestProject - StrictTools",
		"guide/index.html": "Guide - TestProject - StrictTools",
	} {
		if got := titleOf(t, files[pagePath]); got != want {
			t.Errorf("%s title = %q, want %q", pagePath, got, want)
		}
	}
}

// titleOf returns what a rendered document's title element carries.
func titleOf(t *testing.T, rendered string) string {
	t.Helper()
	start := strings.Index(rendered, "<title>")
	end := strings.Index(rendered, "</title>")
	if start < 0 || end < 0 {
		t.Fatal("the rendered document carries no title element")
	}
	return rendered[start+len("<title>") : end]
}

// TestTheSocialTitlesAreTheDocumentTitle builds a whole project mounted on the
// site and asserts the Open Graph and Twitter Card titles carry what the
// head's title element carries.
//
// The two social titles used to concatenate the page title and the project
// name themselves, so a page titled with the project name published
// "<name> - <name>" to every crawler that reads them.
func TestTheSocialTitlesAreTheDocumentTitle(t *testing.T) {
	opts := baseOptions(
		src("index.md", "# TestProject\n\nWelcome.\n"),
		src("guide.md", "# Guide\n\nProse.\n"),
	)
	opts.SiteName = "StrictTools"
	opts.BaseURL = "https://example.com"
	files, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	for _, pagePath := range []string{"index.html", "guide/index.html"} {
		rendered, ok := files[pagePath]
		if !ok {
			t.Fatalf("%s was not emitted", pagePath)
		}
		want := titleOf(t, rendered)
		for _, attr := range []string{
			`property="og:title"`, `name="twitter:title"`,
		} {
			got := metaContentOf(t, rendered, attr)
			if got != want {
				t.Errorf("%s: %s = %q, want the document title %q",
					pagePath, attr, got, want)
			}
			if name := opts.ProjectName; strings.Contains(
				got, name+" - "+name) {
				t.Errorf("%s: %s = %q repeats the project name",
					pagePath, attr, got)
			}
		}
	}
}

// metaContentOf returns the content a rendered document's meta element with
// the given attribute carries.
func metaContentOf(t *testing.T, rendered, attr string) string {
	t.Helper()
	at := strings.Index(rendered, "<meta "+attr+" content=\"")
	if at < 0 {
		t.Fatalf("the rendered document carries no <meta %s>", attr)
	}
	rest := rendered[at+len("<meta "+attr+" content=\""):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("the <meta %s> content attribute is unterminated", attr)
	}
	return rest[:end]
}
