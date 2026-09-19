package listing

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/themes"
)

// listingTOML is the two-category declaration the rendering tests read: one
// project the site serves and one it only links to.
const listingTOML = `[[category]]
name = "Frameworks"

  [[category.project]]
  slug = "alpha"
  blurb = "Does the alpha thing."

[[category]]
name = "Elsewhere"

  [[category.project]]
  slug = "outside"
  name = "Outside"
  blurb = "Lives somewhere else."
  url = "https://example.org/outside"
`

// manifest builds a deployed project's manifest in the shape the assembly
// hands to the renderer.
func manifest(slug, name, version string) map[string]any {
	return map[string]any{"slug": slug, "name": name, "version": version}
}

// mustParse parses a declaration the test states is well formed.
func mustParse(t *testing.T, text string) Listing {
	t.Helper()
	listing, err := Parse(text, SourceFile)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return listing
}

func TestParseReadsCategoriesInDeclaredOrder(t *testing.T) {
	listing := mustParse(t, listingTOML)

	var names []string
	for _, category := range listing.Categories {
		names = append(names, category.Name)
	}
	if strings.Join(names, ",") != "Frameworks,Elsewhere" {
		t.Errorf("categories = %v, want [Frameworks Elsewhere]", names)
	}
	if got := strings.Join(listing.Slugs(), ","); got != "alpha,outside" {
		t.Errorf("slugs = %q, want \"alpha,outside\"", got)
	}
	if !listing.Categories[1].Projects[0].External() {
		t.Error("the entry declaring a url is not reported external")
	}
	if got := len(listing.Entries()); got != 2 {
		t.Errorf("Entries() length = %d, want 2", got)
	}
}

// refusalCases pairs a malformed declaration with the whole message the
// Python reader this package replaces produced for it, captured verbatim.
// Only the invalid-TOML case is matched by prefix: the parser's own complaint
// is the decoder's text, and the two decoders word it differently.
func TestParseRefusals(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   string
		prefix bool
	}{
		{
			name: "an unknown key on a listed project",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\nnote = \"typo\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 declares unknown key(s) 'note'. A listed project carries slug, blurb, url, name, repo.",
		},
		{
			name: "an unknown top-level key",
			text: "projects = []\n",
			want: ".stricttools/docs/projects.toml declares unknown top-level key(s) 'projects'. The listing holds nothing but [[category]] blocks.",
		},
		{
			name: "no category at all",
			text: "",
			want: ".stricttools/docs/projects.toml declares no [[category]] block. The listing is the site's curated project index and there is no empty default.",
		},
		{
			name: "a category with an unknown key",
			text: "[[category]]\nname = \"A\"\ntitle = \"A\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 declares unknown key(s) 'title'. A [[category]] block carries name, project.",
		},
		{
			name: "a category with no name",
			text: "[[category]]\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 is missing a non-empty 'name'.",
		},
		{
			name: "a repeated category name",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\n[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"b\"\nblurb = \"b\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #2 repeats the category name 'A', which an earlier block already declares.",
		},
		{
			name: "an empty category",
			text: "[[category]]\nname = \"A\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A') declares no [[category.project]] block. An empty category would render as a heading over nothing.",
		},
		{
			name: "a project with no slug",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nblurb = \"b\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 is missing a non-empty 'slug'.",
		},
		{
			name: "a project with no blurb",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"a\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 (a) is missing a non-empty 'blurb'. The listing is curated prose, not a directory dump.",
		},
		{
			name: "an external entry with no name",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\nurl = \"https://x/\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 (a) declares a url, so it is a project this site does not serve and has no manifest to take a display name from. Declare 'name'.",
		},
		{
			name: "a served entry declaring a name",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\nname = \"A Thing\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 (a) declares a name but no url. A project this site serves takes its name from its manifest, so declaring one here would be a second source for it.",
		},
		{
			name: "a repository repeating the url",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"out\"\nname = \"Out\"\nblurb = \"b\"\nurl = \"https://github.com/someone/out\"\nrepo = \"https://github.com/someone/out\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 (out) declares the same address as 'url' and 'repo', so the card would print two links to one place. An entry whose only address is its repository needs 'url' alone.",
		},
		{
			name: "a duplicate slug across categories",
			text: "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\n[[category]]\nname = \"B\"\n[[category.project]]\nslug = \"a\"\nblurb = \"b\"\n",
			want: ".stricttools/docs/projects.toml: [[category]] #2 ('B'): [[category.project]] #1 repeats the slug 'a', already listed under 'A'. One project, one card.",
		},
		{
			name: "a category that is not a table",
			text: "category = [\"A\"]\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 is not a table.",
		},
		{
			name: "a project that is not a table",
			text: "[[category]]\nname = \"A\"\nproject = [\"a\"]\n",
			want: ".stricttools/docs/projects.toml: [[category]] #1 ('A'): [[category.project]] #1 is not a table.",
		},
		{
			name:   "a document that is not TOML",
			text:   "[[category]\nname = ",
			want:   ".stricttools/docs/projects.toml is not valid TOML: ",
			prefix: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Parse(testCase.text, SourceFile)
			if err == nil {
				t.Fatalf("Parse(%q) returned no error", testCase.text)
			}
			var listingError *Error
			if !errors.As(err, &listingError) {
				t.Fatalf("error type = %T, want *listing.Error", err)
			}
			if testCase.prefix {
				if !strings.HasPrefix(err.Error(), testCase.want) {
					t.Errorf("error = %q, want a message starting %q", err, testCase.want)
				}
				return
			}
			if err.Error() != testCase.want {
				t.Errorf("error = %q, want %q", err, testCase.want)
			}
		})
	}
}

func TestParseNamesTheSourceItWasGiven(t *testing.T) {
	_, err := Parse("nope = 1\n", "elsewhere/projects.toml")
	if err == nil || !strings.HasPrefix(err.Error(), "elsewhere/projects.toml declares") {
		t.Fatalf("error = %v, want it to name elsewhere/projects.toml", err)
	}
}

// renderOne renders a one-project listing against one manifest.
func renderOne(t *testing.T, text string, manifests []map[string]any) string {
	t.Helper()
	html, err := RenderHTML(mustParse(t, text), manifests, "", "home", "")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	return html
}

func TestTheCardsAreAGrid(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"Developer tools\"\n"+
			"[[category.project]]\nslug = \"alpha\"\nblurb = \"Does the alpha thing.\"\n"+
			"repo = \"https://github.com/someone/alpha\"\n"+
			"[[category.project]]\nslug = \"out\"\nname = \"Outside\"\nblurb = \"Elsewhere.\"\n"+
			"url = \"https://example.org/outside\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "1.0.0")},
	)

	for _, want := range []string{
		`class="card-grid project-grid"`,
		`class="card project-card"`,
		`class="card-title-row"`,
		`class="card-title"`,
		`<span class="badge badge-neutral version-badge">v1.0.0</span>`,
		`class="badge badge-neutral external-badge"`,
		`class="card-badges"`,
		`class="project-repo"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the fragment does not carry %q\n%s", want, html)
		}
	}

	// A bare word "Repository" reads as text; the arrow reads as a link.
	arrow := regexp.MustCompile(`class="project-repo"[^>]*>Repository\s*<span aria-hidden="true">`)
	if !arrow.MatchString(html) {
		t.Errorf("the repository link does not name its destination\n%s", html)
	}
}

func TestAMonorepoVersionIsLabelledRatherThanNumbered(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "0.0.0")},
	)
	if !strings.Contains(html, `<span class="badge badge-neutral version-badge">monorepo</span>`) {
		t.Errorf("0.0.0 is not rendered as the monorepo label\n%s", html)
	}
}

func TestAManifestWithNoVersionCarriesNoBadge(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "")},
	)
	if strings.Contains(html, "card-badges") {
		t.Errorf("a versionless card carries a badge row\n%s", html)
	}
}

func TestACardAddressesAServedProjectThroughTheSiteHop(t *testing.T) {
	html, err := RenderHTML(
		mustParse(t, "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n"),
		[]map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		"../", "home", "",
	)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(html, `href="../alpha/"`) {
		t.Errorf("the card does not address the project through the hop\n%s", html)
	}
}

func TestAHeadingIsRenderedWhenTheSurfaceAsksForOne(t *testing.T) {
	listing := mustParse(t, "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n")
	manifests := []map[string]any{manifest("alpha", "Alpha", "1.0.0")}

	withHeading, err := RenderHTML(listing, manifests, "", "home", "Projects & more")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(withHeading, "<h1>Projects &amp; more</h1>") {
		t.Errorf("the heading is not rendered escaped\n%s", withHeading)
	}

	withoutHeading, err := RenderHTML(listing, manifests, "", "home", "")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if strings.Contains(withoutHeading, "<h1>") {
		t.Errorf("a fragment with no heading rendered one\n%s", withoutHeading)
	}
}

func TestAnApostropheInCuratedProseIsEscaped(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"It's alpha.\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "1.0.0")},
	)
	if !strings.Contains(html, "It&#x27;s alpha.") {
		t.Errorf("the apostrophe is not escaped as the standard library escapes it\n%s", html)
	}
}

func TestALoadedSlugWithNoManifestIsAHardError(t *testing.T) {
	_, err := RenderHTML(mustParse(t, listingTOML), nil, "", "home", "")
	if err == nil {
		t.Fatal("a listing naming an unserved project rendered")
	}
	for _, want := range []string{"alpha", "has no manifest for", "Served projects: (none)."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not carry %q", err, want)
		}
	}
}

func TestTheServedProjectsAreNamedInTheRefusal(t *testing.T) {
	err := CheckAgainst(
		mustParse(t, "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"gone\"\nblurb = \"b\"\n"),
		[]map[string]any{manifest("beta", "Beta", "1.0.0"), manifest("alpha", "Alpha", "1.0.0")},
		"home", SourceFile,
	)
	want := ".stricttools/docs/projects.toml lists gone, which the assembly has no manifest " +
		"for, so the listing would print a card for a project this site does " +
		"not serve. Either the project has never deployed, or the entry names " +
		"an external project and is missing its 'url' and 'name'. Served " +
		"projects: alpha, beta."
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestTheHomeProjectMayNotListItself(t *testing.T) {
	_, err := RenderHTML(
		mustParse(t, "[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"home\"\nblurb = \"me\"\n"),
		[]map[string]any{manifest("home", "Home", "0.1.0")},
		"", "home", "",
	)
	want := ".stricttools/docs/projects.toml lists 'home', which is the home project -- the " +
		"page the listing appears on. The home project is left out of the " +
		"listing it renders."
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestARosterProjectTheListingOmitsIsLegal(t *testing.T) {
	// Curation is selection: an unlisted project simply has no card.
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "1.0.0"), manifest("beta", "Beta", "2.0.0")},
	)
	if !strings.Contains(html, "Alpha") || strings.Contains(html, "Beta") {
		t.Errorf("the omitted project is not omitted\n%s", html)
	}
}

func TestACardWithNoDeclaredRepositoryLinksOnlyItsDocs(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "1.0.0")},
	)
	if strings.Contains(html, "project-repo") {
		t.Errorf("a card with no repository rendered a repository link\n%s", html)
	}
}

func TestAnExternalEntryMayAlsoDeclareARepository(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"out\"\nname = \"Out\"\nblurb = \"b\"\n"+
			"url = \"https://out.example/\"\nrepo = \"https://github.com/someone/out\"\n",
		[]map[string]any{manifest("alpha", "Alpha", "1.0.0")},
	)
	for _, want := range []string{`href="https://out.example/"`, `href="https://github.com/someone/out"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the fragment does not carry %q\n%s", want, html)
		}
	}
}

func TestAnExternalEntryLinksOutAndCarriesNoVersion(t *testing.T) {
	html := renderOne(t, listingTOML, []map[string]any{manifest("alpha", "Alpha", "1.0.0")})
	if !strings.Contains(html, `href="https://example.org/outside"`) || !strings.Contains(html, "Outside") {
		t.Errorf("the external card does not link out\n%s", html)
	}
	if strings.Contains(html, "version-badge\">v</span>") {
		t.Errorf("the external card carries an empty version badge\n%s", html)
	}
}

func TestTheSidecarRoundTrip(t *testing.T) {
	// The deploy writes the listing beside the manifests and reads it back.
	listing := mustParse(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n"+
			"repo = \"https://github.com/someone/alpha\"\n",
	)
	restored, err := ParseSidecar(RenderSidecar(listing, "home"), "home-listing.json")
	if err != nil {
		t.Fatalf("ParseSidecar: %v", err)
	}
	if got := restored.Categories[0].Projects[0].Repo; got != "https://github.com/someone/alpha" {
		t.Errorf("repo = %q, want the declared repository", got)
	}
	if got := restored.Categories[0].Name; got != "A" {
		t.Errorf("category name = %q, want \"A\"", got)
	}
}

func TestTheSidecarIsWrittenInDeclarationOrder(t *testing.T) {
	listing := mustParse(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n",
	)
	want := `{
  "format_version": 1,
  "slug": "home",
  "categories": [
    {
      "name": "A",
      "projects": [
        {
          "slug": "alpha",
          "blurb": "b",
          "url": "",
          "name": "",
          "repo": ""
        }
      ]
    }
  ]
}
`
	if got := RenderSidecar(listing, "home"); got != want {
		t.Errorf("RenderSidecar =\n%s\nwant\n%s", got, want)
	}
}

func TestTheSidecarEscapesEverythingOutsidePrintableASCII(t *testing.T) {
	listing := Listing{Categories: []Category{{
		Name:     "Fällt",
		Projects: []Project{{Slug: "a", Blurb: "an em dash — here"}},
	}}}
	rendered := RenderSidecar(listing, "home")
	for _, want := range []string{`"F\u00e4llt"`, `an em dash \u2014 here`} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the sidecar does not carry %q\n%s", want, rendered)
		}
	}
}

func TestAnEmptyListingStillRendersAWholeSidecar(t *testing.T) {
	want := "{\n  \"format_version\": 1,\n  \"slug\": \"home\",\n  \"categories\": []\n}\n"
	if got := RenderSidecar(Listing{}, "home"); got != want {
		t.Errorf("RenderSidecar = %q, want %q", got, want)
	}
}

func TestSidecarRefusals(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   string
		prefix bool
	}{
		{
			name:   "a document that is not JSON",
			text:   "not json at all",
			want:   "home-listing.json is not valid JSON: ",
			prefix: true,
		},
		{
			name: "a document that is not an object",
			text: "[]",
			want: "home-listing.json must contain a JSON object.",
		},
		{
			name: "no format version at all",
			text: `{"categories": []}`,
			want: "home-listing.json declares format_version None; this selfdoc reads 1. Re-deploy the home project to rewrite the sidecar.",
		},
		{
			name: "another format version",
			text: `{"format_version": 2, "categories": []}`,
			want: "home-listing.json declares format_version 2; this selfdoc reads 1. Re-deploy the home project to rewrite the sidecar.",
		},
		{
			name: "a format version that is a string",
			text: `{"format_version": "1"}`,
			want: "home-listing.json declares format_version '1'; this selfdoc reads 1. Re-deploy the home project to rewrite the sidecar.",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseSidecar(testCase.text, "home-listing.json")
			if err == nil {
				t.Fatalf("ParseSidecar(%q) returned no error", testCase.text)
			}
			if testCase.prefix {
				if !strings.HasPrefix(err.Error(), testCase.want) {
					t.Errorf("error = %q, want a message starting %q", err, testCase.want)
				}
				return
			}
			if err.Error() != testCase.want {
				t.Errorf("error = %q, want %q", err, testCase.want)
			}
		})
	}
}

func TestASidecarWrittenAsAFloatIsStillTheFormatThisBuildReads(t *testing.T) {
	listing, err := ParseSidecar(`{"format_version": 1.0, "categories": []}`, "home-listing.json")
	if err != nil {
		t.Fatalf("ParseSidecar: %v", err)
	}
	if len(listing.Categories) != 0 {
		t.Errorf("categories = %v, want none", listing.Categories)
	}
}

func TestEveryThemeStylesTheCards(t *testing.T) {
	// A class no theme styles is the defect that produced these assertions.
	grid := regexp.MustCompile(`\.project-grid\s*\{([^}]*)\}`)
	for _, name := range themes.List() {
		t.Run(name, func(t *testing.T) {
			css, err := themes.CSS(name)
			if err != nil {
				t.Fatalf("themes.CSS(%q): %v", name, err)
			}
			bodies := grid.FindAllStringSubmatch(css, -1)
			if len(bodies) == 0 {
				t.Fatal("no .project-grid rule")
			}
			var joined strings.Builder
			for _, body := range bodies {
				joined.WriteString(body[1])
				joined.WriteString("\n")
			}
			rules := joined.String()
			if !strings.Contains(rules, "grid-template-columns") && !strings.Contains(rules, "grid-auto-rows") {
				t.Error("the grid is not a grid")
			}
			// One card per row is the regression; the columns are responsive.
			if !strings.Contains(rules, "repeat(auto-fill") && !strings.Contains(rules, "grid-auto-rows: auto") {
				t.Error("the grid holds only one column")
			}
			if !regexp.MustCompile(`\.project-card[\s,{]`).MatchString(css) {
				t.Error("no .project-card rule")
			}
			if !regexp.MustCompile(`\.project-repo[\s,:{]`).MatchString(css) {
				t.Error("no .project-repo rule")
			}
		})
	}
}

// referenceTOML is a declaration exercising every branch of the card markup at
// once: an escaped category name, a served project with a repository and an
// apostrophe in its prose, a workspace whose version is the monorepo label,
// and an external entry with angle brackets in its blurb.
const referenceTOML = `[[category]]
name = "Frameworks & more"

  [[category.project]]
  slug = "alpha"
  blurb = "It's alpha."
  repo = "https://github.com/someone/alpha"

  [[category.project]]
  slug = "mono"
  blurb = "A workspace."

[[category]]
name = "Elsewhere"

  [[category.project]]
  slug = "outside"
  name = "Outside"
  blurb = "Lives <somewhere> else."
  url = "https://example.org/outside"
  repo = "https://github.com/someone/outside"
`

// referenceHTML is the fragment the Python renderer this package replaces
// produced for referenceTOML, captured byte for byte.
const referenceHTML = `<section class="project-list">
  <h1>Projects &amp; more</h1>
  <section class="project-category">
    <h2>Frameworks &amp; more</h2>
    <div class="card-grid project-grid">
      <article class="card project-card">
        <div class="card-title-row">
          <h3 class="card-title"><a href="../alpha/">Alpha</a></h3>
        </div>
        <div class="card-badges"><span class="badge badge-neutral version-badge">v1.0.0</span></div>
        <p class="project-blurb">It&#x27;s alpha.</p>
        <a class="project-repo" href="https://github.com/someone/alpha">Repository<span aria-hidden="true">&#8599;</span></a>
      </article>
      <article class="card project-card">
        <div class="card-title-row">
          <h3 class="card-title"><a href="../mono/">Mono</a></h3>
        </div>
        <div class="card-badges"><span class="badge badge-neutral version-badge">monorepo</span></div>
        <p class="project-blurb">A workspace.</p>
      </article>
    </div>
  </section>
  <section class="project-category">
    <h2>Elsewhere</h2>
    <div class="card-grid project-grid">
      <article class="card project-card">
        <div class="card-title-row">
          <h3 class="card-title"><a href="https://example.org/outside">Outside</a></h3>
        </div>
        <div class="card-badges"><span class="badge badge-neutral external-badge">external</span></div>
        <p class="project-blurb">Lives &lt;somewhere&gt; else.</p>
        <a class="project-repo" href="https://github.com/someone/outside">Repository<span aria-hidden="true">&#8599;</span></a>
      </article>
    </div>
  </section>
</section>`

// referenceSidecar is the sidecar the Python renderer produced for the same
// declaration, captured byte for byte.
const referenceSidecar = `{
  "format_version": 1,
  "slug": "home",
  "categories": [
    {
      "name": "Frameworks & more",
      "projects": [
        {
          "slug": "alpha",
          "blurb": "It's alpha.",
          "url": "",
          "name": "",
          "repo": "https://github.com/someone/alpha"
        },
        {
          "slug": "mono",
          "blurb": "A workspace.",
          "url": "",
          "name": "",
          "repo": ""
        }
      ]
    },
    {
      "name": "Elsewhere",
      "projects": [
        {
          "slug": "outside",
          "blurb": "Lives <somewhere> else.",
          "url": "https://example.org/outside",
          "name": "Outside",
          "repo": "https://github.com/someone/outside"
        }
      ]
    }
  ]
}
`

func TestTheFragmentIsTheOneTheSurfacesAlreadyRender(t *testing.T) {
	html, err := RenderHTML(
		mustParse(t, referenceTOML),
		[]map[string]any{
			manifest("alpha", "Alpha", "1.0.0"),
			manifest("mono", "Mono", "0.0.0"),
		},
		"../", "home", "Projects & more",
	)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if html != referenceHTML {
		t.Errorf("RenderHTML =\n%s\nwant\n%s", html, referenceHTML)
	}
}

func TestTheSidecarIsTheOneTheDeployAlreadyWrites(t *testing.T) {
	if got := RenderSidecar(mustParse(t, referenceTOML), "home"); got != referenceSidecar {
		t.Errorf("RenderSidecar =\n%s\nwant\n%s", got, referenceSidecar)
	}
}

// The literal an unversioned project deploys under is not a version to show.
func TestAnUnversionedProjectCarriesNoVersionBadge(t *testing.T) {
	html := renderOne(t,
		"[[category]]\nname = \"A\"\n[[category.project]]\nslug = \"alpha\"\nblurb = \"b\"\n",
		[]map[string]any{manifest("alpha", "Alpha", config.UnversionedVersion)},
	)
	if strings.Contains(html, "version-badge") {
		t.Errorf("an unversioned card carries a version badge\n%s", html)
	}
}
