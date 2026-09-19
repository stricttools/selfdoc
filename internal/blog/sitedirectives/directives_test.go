package sitedirectives

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/catalog"
	"github.com/stricttools/selfdoc/internal/directives"
)

// write creates path's parents and writes content to it.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// removeFile deletes a file, which is how a fixture states what a project
// does not declare.
func removeFile(path string) error { return os.Remove(path) }

// read returns a file's contents, failing the test when it cannot be read.
func read(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(text)
}

// -- blog-highlights -------------------------------------------------------

func TestBlogHighlightsHonoursItsLimit(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest(
			"alpha", "Alpha", "1.0.0",
			post("one", "One", "2024-06-01"),
			post("two", "Two", "2024-07-01"),
		)},
	}
	rendered := mustRender(t, "blog-highlights", map[string]string{"limit": "1"}, context)
	wants(t, rendered, "Two")
	rejects(t, rendered, ">One<")
}

func TestBlogHighlightsIsNewestFirstAcrossProjects(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{
			manifest("alpha", "Alpha", "1.0.0",
				post("older", "Older", "2024-01-01"),
				post("newest", "Newest", "2024-09-01")),
			manifest("beta", "Beta", "2.0.0",
				post("middle", "Middle", "2024-05-01")),
		},
	}
	body, err := RenderDirectiveBody(
		"blog-highlights", map[string]string{"limit": "3"}, context,
	)
	if err != nil {
		t.Fatalf("RenderDirectiveBody: %v", err)
	}
	if order := titleOrder(body, "Newest", "Middle", "Older"); !reflect.DeepEqual(
		order, []string{"Newest", "Middle", "Older"},
	) {
		t.Fatalf("the posts came out as %v", order)
	}
	wants(t, body, `<span class="project-name">Alpha</span>`,
		`<span class="project-name">Beta</span>`)
}

func TestBlogHighlightsNamesTheProjectAndLinksThroughTheHop(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{
			manifest("alpha", "Alpha", "1.0.0", post("hello", "Hello", "2024-06-01")),
		},
		SiteHop: "../",
	}
	body, err := RenderDirectiveBody(
		"blog-highlights", map[string]string{"limit": "1"}, context,
	)
	if err != nil {
		t.Fatalf("RenderDirectiveBody: %v", err)
	}
	if body != `<section class="blog-highlights">`+"\n"+
		`  <article class="blog-entry">`+"\n"+
		"    <time>2024-06-01</time>\n"+
		`    <span class="project-name">Alpha</span>`+"\n"+
		`    <a href="../blog/hello/">Hello</a>`+"\n"+
		"  </article>\n"+
		`  <p><a href="../blog/">All posts</a></p>`+"\n"+
		"</section>" {
		t.Fatalf("the rendered highlights are:\n%s", body)
	}
}

func TestBlogHighlightsWithNoPostsSaysSo(t *testing.T) {
	body, err := RenderDirectiveBody(
		"blog-highlights", map[string]string{"limit": "3"},
		SiteContext{Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")}},
	)
	if err != nil {
		t.Fatalf("RenderDirectiveBody: %v", err)
	}
	if body != `<section class="blog-highlights">`+"\n"+
		"  <p>No posts yet.</p>\n"+
		`  <p><a href="blog/">All posts</a></p>`+"\n"+
		"</section>" {
		t.Fatalf("the rendered highlights are:\n%s", body)
	}
}

func TestBlogHighlightsEscapesWhatItInterpolates(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{
			manifest("alpha", "Alpha & Co", "1.0.0",
				post("hello", `A "quoted" <title>`, "2024-06-01")),
		},
	}
	body, err := RenderDirectiveBody(
		"blog-highlights", map[string]string{"limit": "1"}, context,
	)
	if err != nil {
		t.Fatalf("RenderDirectiveBody: %v", err)
	}
	wants(t, body, "Alpha &amp; Co", "A &quot;quoted&quot; &lt;title&gt;")
	rejects(t, body, "<title>")
}

func TestBlogHighlightsRequiresALimit(t *testing.T) {
	for _, attrs := range []map[string]string{nil, {"limit": ""}} {
		_, err := RenderRegion("blog-highlights", attrs, SiteContext{})
		if err == nil {
			t.Fatalf("limit=%v must be refused", attrs)
		}
		wants(t, err.Error(), "requires limit")
	}
}

func TestBlogHighlightsRefusesALimitThatIsNotAWholeNumber(t *testing.T) {
	_, err := RenderRegion(
		"blog-highlights", map[string]string{"limit": "three"}, SiteContext{},
	)
	if err == nil {
		t.Fatal("a non-numeric limit must be refused")
	}
	wants(t, err.Error(), "limit must be a whole number, got 'three'.")
}

func TestBlogHighlightsRefusesALimitBelowOne(t *testing.T) {
	_, err := RenderRegion(
		"blog-highlights", map[string]string{"limit": "0"}, SiteContext{},
	)
	if err == nil {
		t.Fatal("a zero limit must be refused")
	}
	wants(t, err.Error(), "limit must be at least 1, got 0.")
}

func TestBlogHighlightsRefusesAnUnknownAttribute(t *testing.T) {
	_, err := RenderRegion(
		"blog-highlights",
		map[string]string{"limit": "1", "count": "2", "since": "2024"},
		SiteContext{},
	)
	if err == nil {
		t.Fatal("an unknown attribute must be refused")
	}
	wants(t, err.Error(),
		"declares unknown attribute(s) count, since", "It takes 'limit'.")
}

// -- projects-cards --------------------------------------------------------

func TestProjectsCardsRendersTheCuratedListing(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	body, err := RenderDirectiveBody("projects-cards", nil, context)
	if err != nil {
		t.Fatalf("RenderDirectiveBody: %v", err)
	}
	wants(t, body, "Frameworks", "Alpha", "Does the alpha thing.", "v1.0.0",
		`href="alpha/"`)
}

func TestProjectsCardsWithoutAListingIsAHardError(t *testing.T) {
	_, err := RenderRegion("projects-cards", nil, SiteContext{})
	if err == nil {
		t.Fatal("a context with no listing must be refused")
	}
	wants(t, err.Error(), "docs/projects.toml", "This project declares none.")
}

func TestProjectsCardsTakesNoAttributes(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	_, err := RenderRegion(
		"projects-cards", map[string]string{"limit": "2", "category": "a"}, context,
	)
	if err == nil {
		t.Fatal("an attribute on projects-cards must be refused")
	}
	wants(t, err.Error(),
		"takes no attributes, got category, limit", "docs/projects.toml")
}

func TestProjectsCardsCarriesTheListingsOwnRefusal(t *testing.T) {
	// A listed slug with no manifest is the listing's error, and it reaches
	// the page's author rather than being rendered around.
	context := SiteContext{
		Manifests: []map[string]any{},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	_, err := RenderRegion("projects-cards", nil, context)
	if err == nil {
		t.Fatal("a listed slug with no manifest must be refused")
	}
	wants(t, err.Error(), "alpha")
}

// -- the dispatch ----------------------------------------------------------

func TestAnUnknownSiteLevelDirectiveIsAHardError(t *testing.T) {
	_, err := RenderDirectiveBody("recent-releases", nil, SiteContext{})
	if err == nil {
		t.Fatal("an unknown directive must be refused")
	}
	wants(t, err.Error(), "unknown site-level directive 'recent-releases'",
		"projects-cards, blog-highlights")
}

func TestTheRegisteredDirectivesAreTheDeclaredSet(t *testing.T) {
	registered := Directives(nil)
	names := make([]string, 0, len(registered))
	for name := range registered {
		names = append(names, name)
	}
	sort.Strings(names)
	declared := append([]string(nil), SiteDirectives...)
	sort.Strings(declared)
	if !reflect.DeepEqual(names, declared) {
		t.Fatalf("registered %v for the declared %v", names, declared)
	}
}

func TestARegistrationWithNoContextRefusesAndNamesTheCommand(t *testing.T) {
	for name, resolve := range Directives(nil) {
		_, err := resolve(map[string]string{"limit": "1"}, nil)
		if err == nil {
			t.Fatalf("%s resolved with no assembly context", name)
		}
		wants(t, err.Error(), "directive '"+name+"' is site-level",
			"--site-manifests <dir>")
	}
}

func TestARegistrationResolvesAWholeRegion(t *testing.T) {
	context := SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		Listing:   curated(t, "alpha"),
		HomeSlug:  "home",
	}
	rendered, err := Directives(&context)["projects-cards"](nil, nil)
	if err != nil {
		t.Fatalf("the registered projects-cards: %v", err)
	}
	if names := RegionNames(rendered); !reflect.DeepEqual(
		names, []string{"projects-cards"},
	) {
		t.Fatalf("region names: %v", names)
	}
}

func TestAPlainBuildCannotResolveASiteLevelDirective(t *testing.T) {
	// The catalog has no site-level directive, so a page carrying one is
	// refused at the marker by any build that did not register them.
	builtin := catalog.AllBuiltinDirectives()
	for _, name := range SiteDirectives {
		if _, known := builtin[name]; known {
			t.Fatalf("the catalog declares %q", name)
		}
	}
	_, err := directives.ParseDirectives(":-: projects-cards\n", builtin)
	if err == nil {
		t.Fatal("a site-level marker must be refused by a plain build")
	}
	wants(t, err.Error(), "Unknown directive 'projects-cards'")
}

// titleOrder is the given titles in the order they appear in body.
func titleOrder(body string, titles ...string) []string {
	type placed struct {
		title string
		at    int
	}
	found := make([]placed, 0, len(titles))
	for _, title := range titles {
		if at := strings.Index(body, ">"+title+"<"); at >= 0 {
			found = append(found, placed{title, at})
		}
	}
	for index := 1; index < len(found); index++ {
		for back := index; back > 0 && found[back].at < found[back-1].at; back-- {
			found[back], found[back-1] = found[back-1], found[back]
		}
	}
	order := make([]string, 0, len(found))
	for _, entry := range found {
		order = append(order, entry.title)
	}
	return order
}
