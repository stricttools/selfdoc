package assembly

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/testisolation/go/hygiene"
)

const sharedCanonicalBase = "https://docs.example.com"

// sharedManifest is one project's manifest, in the shape the assembly holds.
func sharedManifest(slug, name, description string, pages, posts []any, theme string) map[string]any {
	if len(pages) == 0 {
		pages = []any{map[string]any{"path": "index.md", "title": "Home"}}
	}
	manifest := map[string]any{
		"schema_version": 2,
		"name":           name,
		"slug":           slug,
		"version":        "1.0.0",
		"description":    description,
		"language":       "python",
		"base_url":       sharedCanonicalBase + "/" + slug,
		"author":         map[string]any{"name": "Test Author", "url": "https://author.example"},
		"pages":          pages,
		"posts":          posts,
		"last_gen":       "2024-01-01T00:00:00+00:00",
	}
	if theme != "" {
		manifest["theme"] = theme
	}
	return manifest
}

// sharedTree is an assembly checkout with manifests, a site tree and whatever
// files the test writes into it.
type sharedTree struct {
	t       *testing.T
	Root    string
	Site    string
	Manifs  string
	Written []string
}

// newSharedTree lays out an empty assembly checkout.
func newSharedTree(t *testing.T) *sharedTree {
	t.Helper()
	hygiene.Isolate(t)
	root := t.TempDir()
	tree := &sharedTree{
		t:      t,
		Root:   root,
		Site:   filepath.Join(root, "site"),
		Manifs: filepath.Join(root, "manifests"),
	}
	for _, dir := range []string{tree.Site, tree.Manifs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("making %s: %v", dir, err)
		}
	}
	return tree
}

// Manifest writes one project's manifest into the checkout.
func (s *sharedTree) Manifest(manifest map[string]any) {
	s.t.Helper()
	slug, _ := manifest["slug"].(string)
	s.WriteJSON(filepath.Join(s.Manifs, slug+".json"), manifest)
}

// Listing writes the home project's curated listing sidecar, which a declared
// home carries and shared generation refuses without.
func (s *sharedTree) Listing(homeSlug string, projects []any) {
	s.t.Helper()
	s.WriteJSON(site.ListingSidecarPath(s.Manifs, homeSlug), map[string]any{
		"format_version": 1,
		"slug":           homeSlug,
		"categories": []any{map[string]any{
			"name": "Projects", "projects": projects,
		}},
	})
}

// WriteJSON writes a JSON document.
func (s *sharedTree) WriteJSON(path string, value any) {
	s.t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		s.t.Fatalf("encoding %s: %v", path, err)
	}
	s.Write(path, string(encoded))
}

// Write writes a file, making its parents.
func (s *sharedTree) Write(path, content string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.t.Fatalf("writing %s: %v", path, err)
	}
}

// Page writes a site-relative page.
func (s *sharedTree) Page(rel, content string) {
	s.t.Helper()
	s.Write(filepath.Join(s.Site, filepath.Join(strings.Split(rel, "/")...)), content)
}

// Generate runs the shared-file generator over the checkout.
func (s *sharedTree) Generate(homeSlug string) []string {
	s.t.Helper()
	written, err := GenerateSharedFiles(SharedFilesOptions{
		SiteDir:       s.Site,
		ManifestsDir:  s.Manifs,
		CanonicalBase: sharedCanonicalBase,
		DocsBase:      sharedCanonicalBase,
		HomeSlug:      homeSlug,
	}, effects.Unbound())
	if err != nil {
		s.t.Fatalf("generating the shared files: %v", err)
	}
	s.Written = written
	return written
}

// Read is the text of a site-relative file.
func (s *sharedTree) Read(rel string) string {
	s.t.Helper()
	path := filepath.Join(s.Site, filepath.Join(strings.Split(rel, "/")...))
	data, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

// SiteRels is every file under the site tree, site-relative and sorted.
func (s *sharedTree) SiteRels() []string {
	s.t.Helper()
	rels, err := site.BuildOutputPaths(s.Site, false)
	if err != nil {
		s.t.Fatalf("walking the site tree: %v", err)
	}
	return rels
}

// builtPage is a page shaped the way a project's build shapes one: three
// references to the same stylesheet, because that is what the page wrapper
// writes and all three have to be re-pointed together.
func builtPage(cssHref, canonical string) string {
	return "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n" +
		"<title>A page</title>\n" +
		`<link rel="canonical" href="` + canonical + `">` + "\n" +
		`<link rel="preload" href="` + cssHref + `" as="style">` + "\n" +
		`<link rel="stylesheet" href="` + cssHref + `" media="print" onload="this.media='all'">` +
		`<noscript><link rel="stylesheet" href="` + cssHref + `"></noscript>` + "\n" +
		"</head>\n<body>\n<p>body</p>\n</body>\n</html>\n"
}

// threeProjectTree is the assembly the file-set assertions are made over:
// alpha with a page and a post, beta, and home with the curated listing.
func threeProjectTree(t *testing.T) *sharedTree {
	t.Helper()
	tree := newSharedTree(t)
	tree.Manifest(sharedManifest("alpha", "Alpha", "Does the alpha thing.",
		[]any{
			map[string]any{"path": "index.md", "title": "Home"},
			map[string]any{"path": "guide.md", "title": "Guide"},
		},
		[]any{map[string]any{
			"slug": "hello", "title": "Hello", "date": "2024-06-01",
			"path": "blog/hello.md", "tags": []any{},
		}}, ""))
	tree.Manifest(sharedManifest("beta", "Beta", "Does the beta thing.", nil, nil, ""))
	tree.Manifest(sharedManifest("home", "Home", "The front page.", nil, nil, ""))
	tree.Listing("home", []any{
		map[string]any{"slug": "alpha", "blurb": "Does the alpha thing."},
		map[string]any{"slug": "beta", "blurb": "Does the beta thing."},
	})
	return tree
}

// -- the file set ------------------------------------------------------------

func TestGenerateWritesTheSitesOwnFiles(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	for _, rel := range []string{
		"projects/index.html", "blog/index.html", "nav.json", "feed.xml",
		"sitemap.xml", "robots.txt", "llms.txt", "404.html", "_headers",
	} {
		if _, err := os.Stat(filepath.Join(tree.Site,
			filepath.Join(strings.Split(rel, "/")...))); err != nil {
			t.Errorf("%s was not written: %v", rel, err)
		}
	}
}

func TestGenerateReportsEveryPathItWrote(t *testing.T) {
	tree := threeProjectTree(t)
	// A grafted page both sweeps change: its stylesheet is re-pointed at the
	// site-level asset, and its absolute link to this site is re-expressed
	// relative to the page. It is one written file either way.
	tree.Page("alpha/style.css", "/* alpha's own copy */")
	tree.Page("alpha/index.html", strings.Replace(
		builtPage("style.css", sharedCanonicalBase+"/alpha/"),
		"<p>body</p>",
		`<p><a href="`+sharedCanonicalBase+`/blog/">Blog</a></p>`, 1))
	written := tree.Generate("home")
	for _, rel := range []string{
		"projects/index.html", "blog/index.html", "nav.json", "feed.xml",
		"sitemap.xml", "robots.txt", "llms.txt", "404.html", "_headers",
	} {
		path := filepath.Join(tree.Site, filepath.Join(strings.Split(rel, "/")...))
		if !slices.Contains(written, path) {
			t.Errorf("%s was written but not reported", rel)
		}
	}
	seen := make(map[string]bool, len(written))
	for _, path := range written {
		if seen[path] {
			t.Errorf("%s is reported more than once", path)
		}
		seen[path] = true
	}
}

func TestGenerateWritesTheChromeAssetBeforeThePagesThatNameIt(t *testing.T) {
	// The site-level chrome comes first in the reported order, because every
	// page written after it names it.
	tree := threeProjectTree(t)
	written := tree.Generate("home")
	if len(written) == 0 {
		t.Fatal("nothing was written")
	}
	if !strings.Contains(written[0], chrome.Dir) {
		t.Fatalf("the first file written is %q, want a chrome asset", written[0])
	}
}

func TestGenerateRefusesWithoutItsRequiredInputs(t *testing.T) {
	tree := threeProjectTree(t)
	for _, test := range []struct {
		name  string
		opts  SharedFilesOptions
		field string
	}{
		{"no site dir", SharedFilesOptions{
			ManifestsDir: tree.Manifs, CanonicalBase: sharedCanonicalBase,
		}, "SiteDir"},
		{"no manifests dir", SharedFilesOptions{
			SiteDir: tree.Site, CanonicalBase: sharedCanonicalBase,
		}, "ManifestsDir"},
		{"no canonical base", SharedFilesOptions{
			SiteDir: tree.Site, ManifestsDir: tree.Manifs,
		}, "CanonicalBase"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := GenerateSharedFiles(test.opts, effects.Unbound())
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("err = %v, want it to name %s", err, test.field)
			}
		})
	}
}

// -- the machine-readable files ----------------------------------------------

func TestRobotsNamesTheSitemapThatExists(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	robots := tree.Read("robots.txt")
	want := "Sitemap: " + sharedCanonicalBase + "/sitemap.xml"
	if !strings.Contains(robots, want) {
		t.Fatalf("robots.txt does not carry %q:\n%s", want, robots)
	}
	if _, err := os.Stat(filepath.Join(tree.Site, "sitemap.xml")); err != nil {
		t.Fatalf("robots.txt names a sitemap that is not there: %v", err)
	}
}

func TestLLMSTxtLinksEachProjectsOwnFileWithoutInliningIt(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	llms := tree.Read("llms.txt")
	for _, slug := range []string{"alpha", "beta"} {
		if !strings.Contains(llms, sharedCanonicalBase+"/"+slug+"/llms.txt") {
			t.Errorf("llms.txt does not link %s's own file:\n%s", slug, llms)
		}
	}
	// The home project is the site root the file is served from, not one of
	// the projects it points at.
	if strings.Contains(llms, "/home/llms.txt") {
		t.Error("llms.txt points at the home project's own file")
	}
	if strings.Contains(llms, "Guide") {
		t.Error("llms.txt inlines a project's page list")
	}
}

func TestEverySitemapLocIsAbsoluteUnderTheCanonicalBase(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	locs := regexp.MustCompile(`<loc>([^<]+)</loc>`).
		FindAllStringSubmatch(tree.Read("sitemap.xml"), -1)
	if len(locs) == 0 {
		t.Fatal("the sitemap lists nothing")
	}
	for _, loc := range locs {
		if !strings.HasPrefix(loc[1], sharedCanonicalBase+"/") {
			t.Errorf("<loc>%s</loc> is not under the canonical base", loc[1])
		}
	}
}

func TestTheSitemapStaysAbsoluteWhenDocsBaseIsRelative(t *testing.T) {
	// Only the feed reads the docs base; the sitemap protocol has no relative
	// <loc>, so it is generated from the canonical base whatever that says.
	tree := threeProjectTree(t)
	written, err := GenerateSharedFiles(SharedFilesOptions{
		SiteDir:       tree.Site,
		ManifestsDir:  tree.Manifs,
		CanonicalBase: sharedCanonicalBase,
		DocsBase:      "",
		HomeSlug:      "home",
	}, effects.Unbound())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("nothing was written")
	}
	for _, loc := range regexp.MustCompile(`<loc>([^<]+)</loc>`).
		FindAllStringSubmatch(tree.Read("sitemap.xml"), -1) {
		if !strings.HasPrefix(loc[1], sharedCanonicalBase+"/") {
			t.Errorf("<loc>%s</loc> is not absolute", loc[1])
		}
	}
}

func TestTheHeadersFileCarriesTheSitesOneSecurityPolicy(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	headers := tree.Read("_headers")
	for _, want := range []string{
		"X-Frame-Options: DENY",
		"X-Content-Type-Options: nosniff",
		"Referrer-Policy: strict-origin-when-cross-origin",
	} {
		if !strings.Contains(headers, want) {
			t.Errorf("_headers does not carry %q", want)
		}
	}
}

func TestTheRoot404IsNotTheFrontPage(t *testing.T) {
	// On the hosting provider the file at that name answers every address
	// matching no asset, with a 404 status, so its body has to differ from the
	// front page's -- otherwise an address that does not exist renders the
	// home page and reads as one that does.
	tree := threeProjectTree(t)
	tree.Generate("home")
	notFound := tree.Read("404.html")
	if notFound == tree.Read("projects/index.html") {
		t.Fatal("the 404 page is the project listing")
	}
	// It declares no canonical: an error page is not content and has no
	// address of its own.
	if strings.Contains(notFound, `rel="canonical"`) {
		t.Error("the 404 page declares a canonical")
	}
}

// -- the retired redirect worker ---------------------------------------------

// TestNoWorkerIsEmitted holds the ruling that host redirects belong to the
// zone rather than to a generated Pages worker, and that a historical path
// shape answers with the site's 404 rather than a redirect.
func TestNoWorkerIsEmitted(t *testing.T) {
	tree := threeProjectTree(t)
	written := tree.Generate("home")
	path := filepath.Join(tree.Site, "_worker.js")
	if _, err := os.Stat(path); err == nil {
		t.Error("the shared-files pass wrote a _worker.js")
	}
	if slices.Contains(written, path) {
		t.Error("the shared-files pass reported a _worker.js")
	}
}

// TestAStaleWorkerFromAnEarlierDeployIsDeleted covers the tree an assembly
// that deployed before this ruling still carries: the file sits at the site
// root, nothing rewrites it, and the routing check refuses it.
func TestAStaleWorkerFromAnEarlierDeployIsDeleted(t *testing.T) {
	tree := threeProjectTree(t)
	path := filepath.Join(tree.Site, "_worker.js")
	if err := os.MkdirAll(tree.Site, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("export default {}\n"), 0o644); err != nil {
		t.Fatalf("writing the stale worker: %v", err)
	}
	tree.Generate("home")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the stale worker is still in the tree")
	}
}

// -- the site-level chrome ---------------------------------------------------

// stylesheetRE and hrefRE read a page's stylesheet references.
var (
	stylesheetRE = regexp.MustCompile(`(?i)<link\b[^>]*\brel="stylesheet"[^>]*>`)
	hrefRE       = regexp.MustCompile(`href="([^"]*)"`)
)

// stylesheets is every rel=stylesheet href on a page, in document order.
func stylesheets(pageHTML string) []string {
	var refs []string
	for _, tag := range stylesheetRE.FindAllString(pageHTML, -1) {
		if match := hrefRE.FindStringSubmatch(tag); match != nil {
			refs = append(refs, match[1])
		}
	}
	return refs
}

// resolveRef is the site-relative file a document-relative reference on a page
// names.
func resolveRef(pageRel, ref string) string {
	return filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(pageRel), ref)))
}

// chromeTree is one project subtree with pages that reference their own
// stylesheet, plus the manifests beside it.
func chromeTree(t *testing.T) *sharedTree {
	t.Helper()
	tree := newSharedTree(t)
	tree.Manifest(sharedManifest("alpha", "Alpha", "Alpha docs",
		[]any{
			map[string]any{"path": "index.md", "title": "Home"},
			map[string]any{"path": "guide.md", "title": "Guide"},
		},
		[]any{map[string]any{"slug": "hello", "title": "Hello", "date": "2024-06-01"}},
		""))
	tree.Page("alpha/index.html", builtPage("style.css", sharedCanonicalBase+"/alpha/"))
	tree.Page("alpha/guide/index.html",
		builtPage("../style.css", sharedCanonicalBase+"/alpha/guide/"))
	tree.Page("alpha/style.css", "/* alpha's own copy */")
	tree.Page("blog/hello/index.html",
		builtPage("../../style.css", sharedCanonicalBase+"/blog/hello/"))
	return tree
}

func TestGenerateWritesOneChromeAsset(t *testing.T) {
	tree := chromeTree(t)
	tree.Generate("")
	var assets []string
	for _, rel := range tree.SiteRels() {
		if strings.HasPrefix(rel, chrome.Dir+"/") {
			assets = append(assets, rel)
		}
	}
	if len(assets) != 1 {
		t.Fatalf("the deploy wrote %d chrome asset(s): %v", len(assets), assets)
	}
}

func TestEveryGeneratedSharedPageNamesAStylesheetThatExists(t *testing.T) {
	tree := chromeTree(t)
	tree.Generate("")
	rels := tree.SiteRels()
	for _, pageRel := range []string{
		"projects/index.html", "blog/index.html", "404.html",
	} {
		var refs []string
		for _, ref := range stylesheets(tree.Read(pageRel)) {
			if chrome.IsReference(ref) {
				refs = append(refs, ref)
			}
		}
		if len(refs) == 0 {
			t.Errorf("%s names no page-chrome stylesheet", pageRel)
			continue
		}
		for _, ref := range refs {
			target := resolveRef(pageRel, ref)
			if !slices.Contains(rels, target) {
				t.Errorf("%s names %q, which resolves to %q and no such file was written",
					pageRel, ref, target)
			}
		}
	}
}

func TestEveryGraftedPageIsRepointedAtTheSiteAsset(t *testing.T) {
	tree := chromeTree(t)
	tree.Generate("")
	rels := tree.SiteRels()
	for _, pageRel := range []string{
		"alpha/index.html", "alpha/guide/index.html", "blog/hello/index.html",
	} {
		var refs []string
		for _, ref := range stylesheets(tree.Read(pageRel)) {
			if chrome.IsReference(ref) {
				refs = append(refs, ref)
			}
		}
		if len(refs) == 0 {
			t.Errorf("%s lost its stylesheet", pageRel)
			continue
		}
		for _, ref := range refs {
			target := resolveRef(pageRel, ref)
			if !strings.HasPrefix(target, chrome.Dir+"/") {
				t.Errorf("%s still names its own copy at %q", pageRel, target)
			}
			if !slices.Contains(rels, target) {
				t.Errorf("%s names %q, which is not in the tree", pageRel, target)
			}
		}
	}
}

func TestAllThreeReferencesOnAPageMoveTogether(t *testing.T) {
	tree := chromeTree(t)
	tree.Generate("")
	page := tree.Read("alpha/index.html")
	if strings.Contains(page, `href="style.css"`) {
		t.Fatalf("a reference was left behind:\n%s", page)
	}
	if got := strings.Count(page, chrome.Dir+"/"); got != 3 {
		t.Fatalf("the page names the site asset %d time(s), want 3:\n%s", got, page)
	}
}

func TestAProjectsOwnStylesheetIsLeftInPlace(t *testing.T) {
	// The file is in the project's published-file record; deleting it out from
	// under that record is the prune's business, not this pass's.
	tree := chromeTree(t)
	tree.Generate("")
	if !slices.Contains(tree.SiteRels(), "alpha/style.css") {
		t.Fatal("the project's own stylesheet was deleted")
	}
}

func TestTheChromeDirectoryIsAReservedSiteDirectory(t *testing.T) {
	// No project may claim it as a slug.
	if !slices.Contains(site.SiteReservedDirs, chrome.Dir) {
		t.Fatalf("%q is not reserved: %v", chrome.Dir, site.SiteReservedDirs)
	}
}

func TestTheAssetSetCoversEveryDeclaredTheme(t *testing.T) {
	tree := newSharedTree(t)
	tree.Manifest(sharedManifest("alpha", "Alpha", "Alpha docs", nil, nil, "minimal"))
	tree.Manifest(sharedManifest("beta", "Beta", "Beta docs", nil, nil, "clean"))
	tree.Generate("")
	var assets []string
	for _, rel := range tree.SiteRels() {
		if strings.HasPrefix(rel, chrome.Dir+"/") && strings.HasSuffix(rel, ".css") {
			assets = append(assets, rel)
		}
	}
	if len(assets) != 2 {
		t.Fatalf("two declared themes produced %d asset(s): %v", len(assets), assets)
	}
}

// -- the home project's regions ----------------------------------------------

func TestTheHomeProjectsRegionsAreRefreshedOnEveryDeploy(t *testing.T) {
	// The front page's curated cards carry each project's live version, and a
	// version changes when that project releases, not when the home project
	// does.
	tree := threeProjectTree(t)
	tree.WriteJSON(site.FilesManifestPath(tree.Manifs, "home"), map[string]any{
		"schema_version": site.FilesRecordVersion,
		"slug":           "home",
		"owners":         map[string]any{"release": []any{"index.html"}},
	})
	tree.Page("index.html",
		"<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<title>Front page</title>\n"+
			`<link rel="canonical" href="`+sharedCanonicalBase+`/">`+"\n"+
			`<link rel="stylesheet" href="style.css">`+"\n"+
			"</head>\n<body>\n"+
			`<selfdoc-region data-directive="projects-cards">`+
			"<p>stale</p>"+
			"</selfdoc-region>\n</body>\n</html>\n")

	written := tree.Generate("home")
	front := tree.Read("index.html")
	if strings.Contains(front, "<p>stale</p>") {
		t.Fatalf("the region was not re-rendered:\n%s", front)
	}
	if !strings.Contains(front, "alpha") {
		t.Fatalf("the re-rendered region does not name the projects:\n%s", front)
	}
	path := filepath.Join(tree.Site, "index.html")
	if !slices.Contains(written, path) {
		t.Fatalf("the refreshed page was not reported: %v", written)
	}
}

func TestWithoutAHomeProjectNoRegionIsRefreshed(t *testing.T) {
	tree := chromeTree(t)
	refreshed, err := RefreshHomePages(tree.Site, tree.Manifs, nil, "", nil, effects.Unbound())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(refreshed) != 0 {
		t.Fatalf("a tree with no home project refreshed %v", refreshed)
	}
}

// -- what the listing says ---------------------------------------------------

func TestTheGeneratedListingCarriesEveryProjectButTheHomeOne(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	listingPage := tree.Read("projects/index.html")
	for _, slug := range []string{"alpha", "beta"} {
		if !strings.Contains(listingPage, slug) {
			t.Errorf("the listing does not carry %s", slug)
		}
	}
	// The front page does not list itself.
	if strings.Contains(listingPage, ">Home<") {
		t.Error("the listing carries the home project")
	}
}

func TestARetiredProjectLosesItsListingRows(t *testing.T) {
	// What the shared-only dispatch regenerates after the deletion commit.
	tree := threeProjectTree(t)
	tree.Generate("home")
	if !strings.Contains(tree.Read("projects/index.html"), "beta") {
		t.Fatal("the listing did not carry the project in the first place")
	}

	// Retirement removes the project's manifests; the next shared generation
	// is what takes it out of the listing, the nav and the sitemap.
	for _, name := range []string{"beta.json"} {
		if err := os.Remove(filepath.Join(tree.Manifs, name)); err != nil {
			t.Fatalf("removing %s: %v", name, err)
		}
	}
	tree.Listing("home", []any{
		map[string]any{"slug": "alpha", "blurb": "Does the alpha thing."},
	})
	tree.Generate("home")

	if strings.Contains(tree.Read("projects/index.html"), "beta") {
		t.Error("the retired project is still in the listing")
	}
	if strings.Contains(tree.Read("nav.json"), "beta") {
		t.Error("the retired project is still in the navigation document")
	}
	if strings.Contains(tree.Read("sitemap.xml"), "/beta/") {
		t.Error("the retired project is still in the sitemap")
	}
}

// -- the site-level blog -----------------------------------------------------

func TestTheBlogIndexLinksEveryPostAtTheSiteLevel(t *testing.T) {
	tree := newSharedTree(t)
	tree.Manifest(sharedManifest("alpha", "Alpha", "Alpha docs", nil,
		[]any{map[string]any{
			"slug": "hello", "title": "Hello", "date": "2024-06-01",
		}}, ""))
	tree.Manifest(sharedManifest("beta", "Beta", "Beta docs", nil,
		[]any{map[string]any{
			"slug": "world", "title": "World", "date": "2024-06-02",
		}}, ""))
	tree.Generate("")
	index := tree.Read("blog/index.html")
	for _, slug := range []string{"hello", "world"} {
		if !strings.Contains(index, "../blog/"+slug+"/") {
			t.Errorf("the blog index does not link %s at the site level:\n%s", slug, index)
		}
	}
	// The blog index names the project each post came from.
	for _, name := range []string{"Alpha", "Beta"} {
		if !strings.Contains(index, name) {
			t.Errorf("the blog index does not name %s", name)
		}
	}
}

func TestTwoProjectsClaimingOnePostSlugIsRefused(t *testing.T) {
	// Posts share one slug namespace across the whole assembled site.
	tree := newSharedTree(t)
	for _, slug := range []string{"alpha", "beta"} {
		tree.Manifest(sharedManifest(slug, strings.ToUpper(slug[:1])+slug[1:],
			slug+" docs", nil,
			[]any{map[string]any{
				"slug": "hello", "title": "Hello", "date": "2024-06-01",
			}}, ""))
	}
	_, err := GenerateSharedFiles(SharedFilesOptions{
		SiteDir:       tree.Site,
		ManifestsDir:  tree.Manifs,
		CanonicalBase: sharedCanonicalBase,
		DocsBase:      sharedCanonicalBase,
	}, effects.Unbound())
	if err == nil {
		t.Fatal("two projects claimed one post slug and the site was generated")
	}
	for _, want := range []string{"alpha", "beta", "hello"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

// -- the navigation document -------------------------------------------------

func TestNavIsTheProjectsWithoutTheHomeOne(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	var nav map[string]any
	if err := json.Unmarshal([]byte(tree.Read("nav.json")), &nav); err != nil {
		t.Fatalf("nav.json is not JSON: %v", err)
	}
	projects, ok := nav["projects"].([]any)
	if !ok {
		t.Fatalf("nav.json carries no project list: %v", nav)
	}
	var slugs []string
	for _, entry := range projects {
		record, _ := entry.(map[string]any)
		slug, _ := record["slug"].(string)
		slugs = append(slugs, slug)
	}
	if !reflect.DeepEqual(slugs, []string{"alpha", "beta"}) {
		t.Fatalf("nav lists %v", slugs)
	}
}

// -- what a search engine reads off the shared pages -------------------------

// metaDescriptionOf is the content of a page's meta description, "" when it
// carries none.
func metaDescriptionOf(t *testing.T, html string) string {
	t.Helper()
	match := regexp.MustCompile(
		`<meta name="description" content="([^"]*)">`,
	).FindStringSubmatch(html)
	if match == nil {
		return ""
	}
	return match[1]
}

func TestSharedPagesCarryAMetaDescriptionInTheRenderedWindow(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	for _, test := range []struct{ rel, mustName string }{
		{"projects/index.html", "Alpha"},
		{"blog/index.html", "blog"},
	} {
		t.Run(test.rel, func(t *testing.T) {
			description := metaDescriptionOf(t, tree.Read(test.rel))
			if description == "" {
				t.Fatalf("%s carries no meta description", test.rel)
			}
			if n := len([]rune(description)); n < 110 || n > 160 {
				t.Errorf("%s description is %d chars, want 110-160: %q",
					test.rel, n, description)
			}
			if !strings.Contains(description, test.mustName) {
				t.Errorf("%s description does not name %q: %q",
					test.rel, test.mustName, description)
			}
			if strings.HasSuffix(description, ",") ||
				strings.HasSuffix(description, ", .") ||
				strings.Contains(description, " .") {
				t.Errorf("%s description ends badly: %q", test.rel, description)
			}
		})
	}
}

func TestTheProjectsDescriptionNamesTheProjectsItLists(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	description := metaDescriptionOf(t, tree.Read("projects/index.html"))
	for _, name := range []string{"Alpha", "Beta"} {
		if !strings.Contains(description, name) {
			t.Errorf("the projects description does not name %q: %q",
				name, description)
		}
	}
	if strings.Contains(description, "Home") == false {
		t.Errorf("the projects description does not name the site: %q", description)
	}
}

func TestTheBlogDescriptionCountsNoPosts(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	description := metaDescriptionOf(t, tree.Read("blog/index.html"))
	if strings.ContainsAny(description, "0123456789") {
		t.Errorf("the blog description carries a number, so it goes stale "+
			"with every post: %q", description)
	}
}

// ldDocsOf is every JSON-LD document a page carries, decoded.
func ldDocsOf(t *testing.T, html string) []map[string]any {
	t.Helper()
	matches := regexp.MustCompile(
		`(?s)<script type="application/ld\+json">\n(.*?)\n</script>`,
	).FindAllStringSubmatch(html, -1)
	docs := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		var doc map[string]any
		if err := json.Unmarshal([]byte(match[1]), &doc); err != nil {
			t.Fatalf("decoding JSON-LD %q: %v", match[1], err)
		}
		docs = append(docs, doc)
	}
	return docs
}

func TestSharedPagesCarryACollectionPageWithItsBreadcrumbs(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	for _, test := range []struct{ rel, name, url string }{
		{"projects/index.html", "Projects", sharedCanonicalBase + "/projects/"},
		{"blog/index.html", "Blog", sharedCanonicalBase + "/blog/"},
	} {
		t.Run(test.rel, func(t *testing.T) {
			docs := ldDocsOf(t, tree.Read(test.rel))
			if len(docs) != 1 {
				t.Fatalf("%s carries %d JSON-LD documents, want 1",
					test.rel, len(docs))
			}
			doc := docs[0]
			if doc["@type"] != "CollectionPage" {
				t.Errorf("%s @type = %v, want CollectionPage", test.rel, doc["@type"])
			}
			if doc["url"] != test.url {
				t.Errorf("%s url = %v, want %q", test.rel, doc["url"], test.url)
			}
			if doc["description"] != metaDescriptionOf(t, tree.Read(test.rel)) {
				t.Errorf("%s JSON-LD description differs from the meta one", test.rel)
			}
			crumb, ok := doc["breadcrumb"].(map[string]any)
			if !ok {
				t.Fatalf("%s carries no breadcrumb: %v", test.rel, doc["breadcrumb"])
			}
			if crumb["@type"] != "BreadcrumbList" {
				t.Errorf("%s breadcrumb @type = %v, want BreadcrumbList",
					test.rel, crumb["@type"])
			}
			items, ok := crumb["itemListElement"].([]any)
			if !ok || len(items) != 2 {
				t.Fatalf("%s breadcrumb has %v, want two entries",
					test.rel, crumb["itemListElement"])
			}
			home, _ := items[0].(map[string]any)
			leaf, _ := items[1].(map[string]any)
			if home["name"] != "Home" || home["item"] != sharedCanonicalBase+"/" {
				t.Errorf("%s first crumb = %v, want Home at the canonical base",
					test.rel, home)
			}
			if leaf["name"] != test.name || leaf["item"] != test.url {
				t.Errorf("%s second crumb = %v, want %q at %q",
					test.rel, leaf, test.name, test.url)
			}
		})
	}
}

func TestTheNotFoundPageIsNotIndexedAndCarriesNoStructuredData(t *testing.T) {
	tree := threeProjectTree(t)
	tree.Generate("home")
	notFound := tree.Read("404.html")
	if want := `<meta name="robots" content="noindex">`; !strings.Contains(notFound, want) {
		t.Errorf("404.html does not carry %q:\n%s", want, notFound)
	}
	if docs := ldDocsOf(t, notFound); len(docs) != 0 {
		t.Errorf("404.html carries %d JSON-LD documents, want none", len(docs))
	}
	if description := metaDescriptionOf(t, notFound); description != "" {
		t.Errorf("404.html carries a meta description: %q", description)
	}
}
