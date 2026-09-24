package preview

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/verify"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The command's whole claim is that what it shows you is what a deploy would
// publish, so these assert against the tree the PRODUCTION functions produce
// -- the same graft, the same shared generation, the same chrome asset, the
// same verification -- rather than against anything the preview does of its
// own.
//
// One preview of a home project and two others, assembled for real, carries
// every assertion below: the tree, the verification of it, and the server over
// it. The build itself is not run -- each checkout's build output directory is
// pre-populated -- so what is asserted is the graft and everything downstream
// of it.

// previewOnce assembles the three-project fixture and returns the summary.
func previewOnce(t *testing.T) *Summary {
	t.Helper()
	root := t.TempDir()
	home := homeCheckout(t, filepath.Join(root, "src", "home"),
		fixturePage{Path: "index.md", Title: "Front page"},
		fixturePage{Path: "cv.md", Title: "CV"},
	)
	alpha := writeCheckout(t, checkoutSpec{
		Root: filepath.Join(root, "src", "alpha"), Slug: "alpha", Name: "Alpha",
		Version: "1.0.0",
		Pages: []fixturePage{
			{Path: "index.md", Title: "Alpha"},
			{Path: "guide.md", Title: "Alpha Guide"},
		},
		Posts: []fixturePost{
			{Slug: "hello", Title: "Hello", Date: "2024-06-01", Path: "blog/hello.md"},
		},
	})
	beta := writeCheckout(t, checkoutSpec{
		Root: filepath.Join(root, "src", "beta"), Slug: "beta", Name: "Beta",
		Version: "2.0.0",
		Pages:   []fixturePage{{Path: "index.md", Title: "Beta"}},
	})
	summary, err := PreviewAssembly(home, []string{alpha, beta},
		filepath.Join(root, "out"), canonicalBase, false, "", effects.Unbound())
	if err != nil {
		t.Fatalf("PreviewAssembly: %v", err)
	}
	return summary
}

func TestThePreviewedAssembly(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	previewed := previewOnce(t)
	inSite := func(parts ...string) string {
		return filepath.Join(append([]string{previewed.SiteDir}, parts...)...)
	}

	// -- the tree the preview builds ----------------------------------------

	t.Run("the home project is served at the site root", func(t *testing.T) {
		for _, rel := range []string{"index.html", "cv/index.html"} {
			if !isFile(inSite(filepath.FromSlash(rel))) {
				t.Errorf("%s is not at the site root", rel)
			}
		}
		// ...and has no subtree of its own.
		if isDir(inSite("home")) {
			t.Error("the home project kept a subtree under its own slug")
		}
	})

	t.Run("every other project gets its own subtree", func(t *testing.T) {
		for _, rel := range []string{
			"alpha/index.html", "alpha/guide/index.html", "beta/index.html",
		} {
			if !isFile(inSite(filepath.FromSlash(rel))) {
				t.Errorf("%s was not grafted", rel)
			}
		}
	})

	t.Run("a post is site level", func(t *testing.T) {
		if !isFile(inSite("blog", "hello", "index.html")) {
			t.Error("the post did not reach the site-level blog")
		}
	})

	t.Run("the shared pages exist", func(t *testing.T) {
		for _, rel := range []string{
			"projects/index.html", "blog/index.html", "nav.json", "feed.xml",
			"sitemap.xml", "robots.txt", "llms.txt", "404.html", "_headers",
		} {
			if !isFile(inSite(filepath.FromSlash(rel))) {
				t.Errorf("the shared generator did not write %s", rel)
			}
		}
	})

	t.Run("the chrome asset is written and every page names it", func(t *testing.T) {
		assets, err := os.ReadDir(inSite(site.ChromeDir))
		if err != nil {
			t.Fatalf("reading the chrome directory: %v", err)
		}
		if len(assets) == 0 {
			t.Fatal("the shared generator wrote no chrome asset")
		}
		page := testproject.ReadText(t, inSite("alpha", "index.html"))
		// The graft delivered a page naming its own style.css; the shared
		// generator re-pointed it at the site-level asset.
		if !strings.Contains(page, "../"+site.ChromeDir+"/") {
			t.Errorf("the page does not name the site-level asset:\n%s", page)
		}
		if strings.Contains(page, `href="style.css"`) {
			t.Errorf("the page still names its own stylesheet:\n%s", page)
		}
	})

	t.Run("the generated pages are styled", func(t *testing.T) {
		for _, rel := range []string{
			"projects/index.html", "blog/index.html", "404.html",
		} {
			page := testproject.ReadText(t, inSite(filepath.FromSlash(rel)))
			if !strings.Contains(page, site.ChromeDir) {
				t.Errorf("%s references no stylesheet", rel)
			}
		}
	})

	t.Run("the standalone deploy artifacts are left behind", func(t *testing.T) {
		// The home project's build wrote a 404.html and _headers for its own
		// hosting; the site's own are the assembly's, not that build's.
		if headers := testproject.ReadText(t, inSite("_headers")); strings.Contains(headers, "X-Test") {
			t.Errorf("the project's own _headers survived:\n%s", headers)
		}
		if isFile(inSite("alpha", "404.html")) {
			t.Error("a project's own 404.html survived the graft")
		}
	})

	t.Run("the roster and membership record are written", func(t *testing.T) {
		roster := testproject.ReadText(t, filepath.Join(previewed.OutDir, site.RosterPath))
		if !strings.Contains(roster, `home = "home"`) {
			t.Errorf("the roster names no home:\n%s", roster)
		}
		for _, slug := range []string{"home", "alpha", "beta"} {
			if !strings.Contains(roster, `slug = "`+slug+`"`) {
				t.Errorf("the roster does not declare %s:\n%s", slug, roster)
			}
		}
		membership := readJSON(t, filepath.Join(previewed.OutDir, site.ProjectsPath))
		recorded := make([]string, 0, len(membership))
		for slug := range membership {
			recorded = append(recorded, slug)
		}
		sort.Strings(recorded)
		if !slices.Equal(recorded, []string{"alpha", "beta", "home"}) {
			t.Errorf("the membership record names %v", recorded)
		}
		alpha, isObject := membership["alpha"].(map[string]any)
		if !isObject {
			t.Fatalf("alpha's record is not an object: %v", membership["alpha"])
		}
		if alpha["version"] != "1.0.0" {
			t.Errorf("alpha's recorded version is %v, want 1.0.0", alpha["version"])
		}
		// The repository field records where the content came from: this
		// machine, since a preview has no dispatch to check an origin against.
		if alpha["repo"] != "local/alpha" {
			t.Errorf("alpha's recorded repo is %v, want local/alpha", alpha["repo"])
		}
		if alpha["ref"] != "local" {
			t.Errorf("alpha's recorded ref is %v, want local", alpha["ref"])
		}
	})

	t.Run("the manifests are copied beside the site", func(t *testing.T) {
		manifests := filepath.Join(previewed.OutDir, "manifests")
		for _, name := range []string{"alpha.json", "home.json"} {
			if !isFile(filepath.Join(manifests, name)) {
				t.Errorf("%s was not copied", name)
			}
		}
		// The home project's curated listing rides along as a sidecar.
		if !isFile(filepath.Join(manifests, "home-listing.json")) {
			t.Error("the curated listing was not copied")
		}
	})

	t.Run("the search index is built", func(t *testing.T) {
		entry := inSite("pagefind", "pagefind-entry.json")
		if !isFile(entry) {
			t.Fatal("the search index was not built")
		}
		if readJSON(t, entry)["languages"] == nil {
			t.Error("the search index entry names no languages")
		}
	})

	t.Run("the summary names what was assembled", func(t *testing.T) {
		if previewed.Home != "home" {
			t.Errorf("Home = %q, want home", previewed.Home)
		}
		if !slices.Equal(previewed.Slugs, []string{"alpha", "beta", "home"}) {
			t.Errorf("Slugs = %v", previewed.Slugs)
		}
		if len(previewed.Shared) == 0 {
			t.Error("the summary names no shared files")
		}
		if previewed.SiteDir != filepath.Join(previewed.OutDir, "site") {
			t.Errorf("SiteDir = %q, want site/ inside the out dir", previewed.SiteDir)
		}
	})

	// -- verification runs against the tree ---------------------------------

	t.Run("the real verification runs", func(t *testing.T) {
		// outbound-links is the one check a preview tree does not configure.
		ran := append([]string(nil), previewed.Report.Ran...)
		sort.Strings(ran)
		want := []string{}
		for _, check := range verify.Checks {
			if check != "outbound-links" {
				want = append(want, check)
			}
		}
		sort.Strings(want)
		if !slices.Equal(ran, want) {
			t.Errorf("the checks that ran are %v, want %v", ran, want)
		}
	})

	t.Run("the assembled tree passes", func(t *testing.T) {
		if !previewed.Report.OK() {
			t.Errorf("verification failed:\n%s", previewed.Report.ErrorText())
		}
	})

	t.Run("the report names the tree and the counts", func(t *testing.T) {
		text := RenderReport(previewed.Report, previewed.OutDir)
		if !strings.Contains(text, previewed.OutDir) {
			t.Errorf("the report does not name the tree:\n%s", text)
		}
		if !strings.Contains(text, "check(s) ran") {
			t.Errorf("the report does not count the checks:\n%s", text)
		}
		if !strings.Contains(text, "Every check that ran passed.") {
			t.Errorf("the report does not say the tree passed:\n%s", text)
		}
	})

	// -- the server over the real tree --------------------------------------

	t.Run("the assembled tree is served on the wire", func(t *testing.T) {
		port := serveTree(t, previewed.SiteDir)
		answer := request(t, port, "GET", "/")
		if answer.Status != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", answer.Status)
		}
		if !contains(answer.Body, "Front page") {
			t.Errorf("the root does not serve the home project's front page:\n%s", answer.Body)
		}
		post := request(t, port, "GET", "/blog/hello/")
		if post.Status != http.StatusOK {
			t.Errorf("GET /blog/hello/ = %d, want 200", post.Status)
		}
		missing := request(t, port, "GET", "/nothing/here/")
		if missing.Status != http.StatusNotFound {
			t.Errorf("GET /nothing/here/ = %d, want 404", missing.Status)
		}
		if string(missing.Body) != testproject.ReadText(t, inSite("404.html")) {
			t.Error("the 404 body is not the assembly's own page")
		}
	})
}
