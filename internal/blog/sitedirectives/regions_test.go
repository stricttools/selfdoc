package sitedirectives

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/effects"
)

// canonicalBase is the assembly site's base URL in every fixture manifest.
const canonicalBase = "https://docs.example.com"

// handle is the effects handle every write in these tests runs under:
// unbound, so the writes execute directly.
func handle() *effects.Handle { return effects.Unbound() }

// manifest is one project manifest as the assembly holds it.
func manifest(slug, name, version string, posts ...map[string]any) map[string]any {
	entries := make([]any, 0, len(posts))
	for _, post := range posts {
		entries = append(entries, post)
	}
	return map[string]any{
		"schema_version": 2,
		"name":           name,
		"slug":           slug,
		"version":        version,
		"description":    name + " docs",
		"language":       "python",
		"base_url":       canonicalBase + "/" + slug,
		"author": map[string]any{
			"name": "Test Author", "url": "https://author.example",
		},
		"pages":    []any{map[string]any{"path": "index.md", "title": "Home"}},
		"posts":    entries,
		"last_gen": "2024-01-01T00:00:00+00:00",
	}
}

// post is one post entry of a manifest.
func post(slug, title, date string) map[string]any {
	return map[string]any{
		"slug": slug, "title": title, "date": date,
		"path": "blog/" + slug + ".md", "tags": []any{},
	}
}

// curated is a one-category listing naming each given slug.
func curated(t *testing.T, slugs ...string) *listing.Listing {
	t.Helper()
	text := "[[category]]\nname = \"Frameworks\"\n"
	for _, slug := range slugs {
		text += "[[category.project]]\nslug = \"" + slug +
			"\"\nblurb = \"Does the " + slug + " thing.\"\n"
	}
	parsed, err := listing.Parse(text, "docs/projects.toml")
	if err != nil {
		t.Fatalf("parsing the fixture listing: %v", err)
	}
	return &parsed
}

// mustRender renders a region, failing the test on a refusal.
func mustRender(
	t *testing.T, name string, attrs map[string]string, context SiteContext,
) string {
	t.Helper()
	rendered, err := RenderRegion(name, attrs, context)
	if err != nil {
		t.Fatalf("RenderRegion(%q): %v", name, err)
	}
	return rendered
}

// mustRefresh refreshes a page's regions, failing the test on a refusal.
func mustRefresh(t *testing.T, pageHTML string, context SiteContext) string {
	t.Helper()
	refreshed, err := RefreshRegions(pageHTML, context, "")
	if err != nil {
		t.Fatalf("RefreshRegions: %v", err)
	}
	return refreshed
}

func wants(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, want := range substrings {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func rejects(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, unwanted := range substrings {
		if strings.Contains(content, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, content)
		}
	}
}

// -- the region wrapper ----------------------------------------------------

func TestARegionSurvivesARerenderWithItsAttributes(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{
			manifest("alpha", "Alpha", "1.0.0", post("hello", "Hello", "2024-06-01")),
		},
	}
	once := mustRender(t, "blog-highlights", map[string]string{"limit": "1"}, context)
	if names := RegionNames(once); !reflect.DeepEqual(names, []string{"blog-highlights"}) {
		t.Fatalf("region names of a fresh region: %v", names)
	}
	twice := mustRefresh(t, once, context)
	if names := RegionNames(twice); !reflect.DeepEqual(names, []string{"blog-highlights"}) {
		t.Fatalf("region names after a refresh: %v", names)
	}
	wants(t, twice, "Hello")
}

func TestARefreshIsIdempotent(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{
			manifest("alpha", "Alpha", "1.0.0", post("hello", "Hello", "2024-06-01")),
		},
	}
	once := mustRender(t, "blog-highlights", map[string]string{"limit": "2"}, context)
	twice := mustRefresh(t, once, context)
	if twice != once {
		t.Fatalf("a refresh changed a region it had just rendered:\n%s\n%s", once, twice)
	}
	if thrice := mustRefresh(t, twice, context); thrice != once {
		t.Fatalf("the second refresh changed it:\n%s", thrice)
	}
}

func TestAParagraphWrapperAroundARegionIsAbsorbed(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	wrapped := `<p><selfdoc-region data-directive="projects-cards">old` +
		"</selfdoc-region></p>"
	refreshed := mustRefresh(t, wrapped, context)
	if strings.HasPrefix(refreshed, "<p>") {
		t.Errorf("the paragraph wrapper stayed:\n%s", refreshed)
	}
	if !strings.HasSuffix(refreshed, "</selfdoc-region>") {
		t.Errorf("the closing paragraph stayed:\n%s", refreshed)
	}
	wants(t, refreshed, "Does the alpha thing.")
}

func TestAParagraphWrapperWithWhitespaceIsAbsorbed(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	wrapped := "<p>\n  " + `<selfdoc-region data-directive="projects-cards">` +
		"old</selfdoc-region>\n</p>"
	refreshed := mustRefresh(t, wrapped, context)
	if !strings.HasPrefix(refreshed, "<"+RegionTag+" ") {
		t.Errorf("the opening paragraph and its whitespace stayed:\n%s", refreshed)
	}
	if !strings.HasSuffix(refreshed, "</"+RegionTag+">") {
		t.Errorf("the closing paragraph and its whitespace stayed:\n%s", refreshed)
	}
}

func TestAPageWithNoRegionIsReturnedUnchanged(t *testing.T) {
	page := "<html><body><p>nothing to do</p></body></html>"
	refreshed := mustRefresh(t, page, SiteContext{})
	if refreshed != page {
		t.Fatalf("a page with no region was rewritten:\n%s", refreshed)
	}
}

func TestEveryRegionOnAPageIsRefreshed(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{
			manifest("alpha", "Alpha", "1.0.0", post("hello", "Hello", "2024-06-01")),
		},
		Listing:  curated(t, "alpha"),
		HomeSlug: "home",
	}
	page := "<h1>Me</h1>\n" +
		mustRender(t, "projects-cards", nil, context) +
		"\n<p>Prose the author wrote.</p>\n" +
		mustRender(t, "blog-highlights", map[string]string{"limit": "1"}, context)
	refreshed := mustRefresh(t, page, context)
	if names := RegionNames(refreshed); !reflect.DeepEqual(
		names, []string{"projects-cards", "blog-highlights"},
	) {
		t.Fatalf("region names: %v", names)
	}
	wants(t, refreshed, "Prose the author wrote.", "Does the alpha thing.", "Hello")
}

func TestARegionCarriesItsAttributesAsDataAttributes(t *testing.T) {
	context := SiteContext{Manifests: []map[string]any{}}
	rendered := mustRender(t, "blog-highlights", map[string]string{"limit": "3"}, context)
	wants(t, rendered,
		`<selfdoc-region data-directive="blog-highlights" data-arg-limit="3">`)
	regions := FindRegions(rendered)
	if len(regions) != 1 {
		t.Fatalf("found %d regions", len(regions))
	}
	if !reflect.DeepEqual(regions[0].Attrs, map[string]string{"limit": "3"}) {
		t.Fatalf("parsed attributes: %v", regions[0].Attrs)
	}
}

func TestARegionsAttributesAreEscapedAndReadBack(t *testing.T) {
	// A region's attributes are HTML, so a value with a quote in it has to
	// come back out of the page as it went in -- the re-render uses them.
	rendered := `<selfdoc-region data-directive="blog-highlights" ` +
		`data-arg-limit="&quot;3&quot;">body</selfdoc-region>`
	regions := FindRegions(rendered)
	if len(regions) != 1 {
		t.Fatalf("found %d regions", len(regions))
	}
	if regions[0].Attrs["limit"] != `"3"` {
		t.Fatalf("unescaped attribute: %q", regions[0].Attrs["limit"])
	}
}

func TestARegionBodyIsReportedVerbatim(t *testing.T) {
	// The verifier reads the body to notice a region that holds nothing.
	page := `<selfdoc-region data-directive="projects-cards">` +
		"\n  \n</selfdoc-region>"
	regions := FindRegions(page)
	if len(regions) != 1 {
		t.Fatalf("found %d regions", len(regions))
	}
	if strings.TrimSpace(regions[0].Body) != "" {
		t.Fatalf("body %q is not empty", regions[0].Body)
	}
	if regions[0].Name != "projects-cards" {
		t.Fatalf("name %q", regions[0].Name)
	}
}

func TestARegionWithNoDirectiveAttributeNamesNothing(t *testing.T) {
	page := "<selfdoc-region>body</selfdoc-region>"
	if names := RegionNames(page); !reflect.DeepEqual(names, []string{""}) {
		t.Fatalf("region names: %v", names)
	}
}

func TestARegionNeverAbsorbsAnotherRegionsBody(t *testing.T) {
	// The body is non-greedy, so two regions on one page are two regions.
	page := `<selfdoc-region data-directive="projects-cards">a</selfdoc-region>` +
		`<selfdoc-region data-directive="blog-highlights">b</selfdoc-region>`
	if names := RegionNames(page); !reflect.DeepEqual(
		names, []string{"projects-cards", "blog-highlights"},
	) {
		t.Fatalf("region names: %v", names)
	}
}

// -- unclosed regions ------------------------------------------------------

func TestARegionThatNeverClosesIsAHardError(t *testing.T) {
	_, err := RefreshRegions(
		`<selfdoc-region data-directive="projects-cards">`, SiteContext{}, "page",
	)
	if err == nil {
		t.Fatal("an unclosed region must be refused")
	}
	wants(t, err.Error(), "open and never close", "page: ", "'projects-cards'")
}

func TestUnclosedRegionsAreTheTrailingOpenings(t *testing.T) {
	page := `<selfdoc-region data-directive="projects-cards">a</selfdoc-region>` +
		`<selfdoc-region data-directive="blog-highlights">`
	if names := FindUnclosedRegions(page); !reflect.DeepEqual(
		names, []string{"blog-highlights"},
	) {
		t.Fatalf("unclosed: %v", names)
	}
}

func TestAPageWhoseRegionsAllCloseHasNoUnclosedOnes(t *testing.T) {
	page := `<selfdoc-region data-directive="projects-cards">a</selfdoc-region>`
	if names := FindUnclosedRegions(page); len(names) != 0 {
		t.Fatalf("unclosed: %v", names)
	}
}

func TestAnUnclosedRegionIsNamedOnceAndSorted(t *testing.T) {
	page := `<selfdoc-region data-directive="projects-cards">` +
		`<selfdoc-region data-directive="blog-highlights">` +
		`<selfdoc-region data-directive="blog-highlights">`
	if names := FindUnclosedRegions(page); !reflect.DeepEqual(
		names, []string{"blog-highlights", "projects-cards"},
	) {
		t.Fatalf("unclosed: %v", names)
	}
}

// -- the page-context hop --------------------------------------------------

func TestThePageContextHopCountsTheDirectoriesAPageSitsIn(t *testing.T) {
	context := SiteContext{Manifests: []map[string]any{}, SiteHop: "nonsense/"}
	for page, hop := range map[string]string{
		"index.html":         "",
		"cv/index.html":      "../",
		"a/b/index.html":     "../../",
		"a/b/c/d/index.html": "../../../../",
	} {
		if got := PageContext(context, page).SiteHop; got != hop {
			t.Errorf("the hop of %q is %q, wanted %q", page, got, hop)
		}
	}
}

func TestThePageContextKeepsEverythingElse(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	addressed := PageContext(context, "cv/index.html")
	if addressed.HomeSlug != "home" || addressed.Listing != context.Listing ||
		len(addressed.Manifests) != 1 {
		t.Fatalf("the page context dropped something: %+v", addressed)
	}
	if context.SiteHop != "" {
		t.Fatalf("the original context was mutated: %q", context.SiteHop)
	}
}

func TestARegionLinksThroughThePagesOwnHop(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	front := mustRender(t, "projects-cards", nil, PageContext(context, "index.html"))
	wants(t, front, `href="alpha/"`)
	inner := mustRender(t, "projects-cards", nil, PageContext(context, "cv/index.html"))
	wants(t, inner, `href="../alpha/"`)
}

// -- the output pass -------------------------------------------------------

func TestTheOutputPassRefreshesEveryPageAgainstItsOwnHop(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	output := t.TempDir()
	region := mustRender(t, "projects-cards", nil, context)
	write(t, output+"/index.html", "<body>"+region+"</body>")
	write(t, output+"/cv/index.html", "<body>"+region+"</body>")
	write(t, output+"/style.css", "body{}")

	changed, err := RefreshOutputRegions(output, context, handle())
	if err != nil {
		t.Fatalf("RefreshOutputRegions: %v", err)
	}
	if !reflect.DeepEqual(changed, []string{"cv/index.html"}) {
		t.Fatalf("changed pages: %v", changed)
	}
	wants(t, read(t, output+"/index.html"), `href="alpha/"`)
	wants(t, read(t, output+"/cv/index.html"), `href="../alpha/"`)
}

func TestTheOutputPassNamesThePageAnUnclosedRegionIsOn(t *testing.T) {
	output := t.TempDir()
	write(t, output+"/cv/index.html",
		`<selfdoc-region data-directive="projects-cards">`)
	_, err := RefreshOutputRegions(output, SiteContext{}, handle())
	if err == nil {
		t.Fatal("an unclosed region in the output must be refused")
	}
	wants(t, err.Error(), "cv/index.html: ", "open and never close")
}

func TestTheOutputPassIsIdempotent(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	output := t.TempDir()
	write(t, output+"/cv/index.html",
		"<body>"+mustRender(t, "projects-cards", nil, context)+"</body>")
	if _, err := RefreshOutputRegions(output, context, handle()); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	once := read(t, output+"/cv/index.html")
	changed, err := RefreshOutputRegions(output, context, handle())
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("the second pass rewrote %v", changed)
	}
	if read(t, output+"/cv/index.html") != once {
		t.Fatal("the second pass changed the page")
	}
}
