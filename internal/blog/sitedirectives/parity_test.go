package sitedirectives

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/listing"
)

// The reference files are the Python's real output, written by
// scripts/record_sitedirectives_reference.py. A region is left in a published
// page and re-rendered by later deploys, so a byte that moves here changes
// pages the deploy never rebuilt.

// reference reads a recorded reference file.
func reference(t *testing.T, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading reference %s: %v", name, err)
	}
	return string(text)
}

// referenceListing is the listing every recorded cards case was rendered
// from: one served project and one external entry with a repository.
func referenceListing(t *testing.T) *listing.Listing {
	t.Helper()
	parsed, err := listing.Parse(
		"[[category]]\nname = \"Frameworks\"\n\n"+
			"  [[category.project]]\n  slug = \"alpha\"\n"+
			"  blurb = \"Does the alpha thing.\"\n\n"+
			"[[category]]\nname = \"Elsewhere\"\n\n"+
			"  [[category.project]]\n  slug = \"outside\"\n"+
			"  name = \"Outside\"\n  blurb = \"Lives somewhere else.\"\n"+
			"  url = \"https://example.org/outside\"\n"+
			"  repo = \"https://github.com/someone/outside\"\n",
		"docs/projects.toml",
	)
	if err != nil {
		t.Fatalf("parsing the reference listing: %v", err)
	}
	return &parsed
}

// referenceCardsContext is the context every recorded cards case was rendered
// against, at the given hop.
func referenceCardsContext(t *testing.T, siteHop string) SiteContext {
	t.Helper()
	return SiteContext{
		Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
		SiteHop:   siteHop,
		Listing:   referenceListing(t),
		HomeSlug:  "home",
	}
}

func TestRenderedRegionsMatchTheRecordedBytes(t *testing.T) {
	cards := referenceCardsContext(t, "")
	cases := []struct {
		file    string
		name    string
		attrs   map[string]string
		context SiteContext
	}{
		{
			file:  "region_blog_highlights_escapes.html",
			name:  "blog-highlights",
			attrs: map[string]string{"limit": "1"},
			context: SiteContext{
				Manifests: []map[string]any{manifest(
					"alpha", "Alpha & Co", "1.0.0",
					post("hello", `A "quoted" <title>`, "2024-06-01"),
				)},
				SiteHop: "../",
			},
		},
		{
			file:  "region_blog_highlights_limit.html",
			name:  "blog-highlights",
			attrs: map[string]string{"limit": "3"},
			context: SiteContext{
				Manifests: []map[string]any{
					manifest("alpha", "Alpha", "1.0.0",
						post("older", "Older", "2024-01-01"),
						post("newest", "Newest", "2024-09-01"),
						post("oldest", "Oldest", "2023-01-01")),
					manifest("beta", "Beta", "2.0.0",
						post("middle", "Middle", "2024-05-01")),
				},
			},
		},
		{
			file:  "region_blog_highlights_empty.html",
			name:  "blog-highlights",
			attrs: map[string]string{"limit": "3"},
			context: SiteContext{
				Manifests: []map[string]any{manifest("alpha", "Alpha", "1.0.0")},
			},
		},
		{
			file:    "region_projects_cards.html",
			name:    "projects-cards",
			context: cards,
		},
		{
			file:    "region_projects_cards_hop.html",
			name:    "projects-cards",
			context: referenceCardsContext(t, "../"),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.file, func(t *testing.T) {
			rendered, err := RenderRegion(
				testCase.name, testCase.attrs, testCase.context,
			)
			if err != nil {
				t.Fatalf("RenderRegion(%q): %v", testCase.name, err)
			}
			if want := reference(t, testCase.file); rendered != want {
				t.Errorf("rendered:\n%s\n\nrecorded:\n%s", rendered, want)
			}
		})
	}
}

func TestRefreshedPagesMatchTheRecordedBytes(t *testing.T) {
	cards := referenceCardsContext(t, "")
	highlights, err := RenderRegion(
		"blog-highlights", map[string]string{"limit": "2"}, cards,
	)
	if err != nil {
		t.Fatalf("rendering the page's highlights: %v", err)
	}
	projects, err := RenderRegion("projects-cards", nil, cards)
	if err != nil {
		t.Fatalf("rendering the page's cards: %v", err)
	}
	cases := []struct {
		file string
		page string
	}{
		{
			file: "refreshed_paragraph.html",
			page: "<p>\n  " +
				`<selfdoc-region data-directive="projects-cards">old` +
				"</selfdoc-region>\n</p>",
		},
		{
			file: "refreshed_page.html",
			page: "<h1>Me</h1>\n<p>Prose the author wrote.</p>\n" +
				projects + "\n" + highlights + "\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.file, func(t *testing.T) {
			refreshed, err := RefreshRegions(testCase.page, cards, "")
			if err != nil {
				t.Fatalf("RefreshRegions: %v", err)
			}
			if want := reference(t, testCase.file); refreshed != want {
				t.Errorf("refreshed:\n%s\n\nrecorded:\n%s", refreshed, want)
			}
		})
	}
}
