package unified

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

func TestBuildUnifiedWritesTheSiteLevelDocuments(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, twoProjects, nil)

	// The documents that describe the site rather than a page are written
	// once, at the output root, from the docs-site's own default-locale
	// pass -- one set for the whole unified site.
	for _, name := range []string{
		"404.html", "favicon.svg", "feed.xml", "llms.txt", "llms-full.txt",
		"og-index.png", "robots.txt", "sitemap.xml",
	} {
		requireFile(t, filepath.Join(output, name))
	}
}

func TestBuildUnifiedSitemapListsEveryMountsPages(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, twoProjects, nil)

	// The sitemap is built from the HTML the whole run wrote, so it covers
	// the constituents' mounts and the docs-site's own, not just the pass
	// its metadata came from.
	sitemap := readFile(t, filepath.Join(output, "sitemap.xml"))
	for _, want := range []string{
		"https://example.com/core/",
		"https://example.com/cli/",
		"https://example.com/common/",
		"https://example.com/common/projects/",
	} {
		if !strings.Contains(sitemap, "<loc>"+want+"</loc>") {
			t.Errorf("the sitemap is missing %q:\n%s", want, sitemap)
		}
	}
}

func TestBuildUnifiedWipesTheOutputDirectoryFirst(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	docsSite, output := buildUnifiedFixture(t, oneProject, nil)
	stale := filepath.Join(output, "stale", "page.html")
	testproject.WriteText(t, stale, "<html>gone</html>")

	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	if isFile(stale) {
		t.Error("a file from an earlier build is still in the output tree")
	}
	requireFile(t, filepath.Join(output, "core", "index.html"))
}
