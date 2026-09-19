package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/config"
)

// -- the clean tree ---------------------------------------------------------

func TestASoundTreePassesEveryAssertion(t *testing.T) {
	report := verifyTree(t, newAssembly(t))
	if !report.OK() {
		t.Fatalf("%s", report.ErrorText())
	}
}

func TestEveryCheckButOutboundRan(t *testing.T) {
	report := verifyTree(t, newAssembly(t))
	want := []string{}
	for _, check := range Checks {
		if check != "outbound-links" {
			want = append(want, check)
		}
	}
	got := append([]string(nil), report.Ran...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ran %v, want %v", got, want)
	}
}

func TestACanonicalBaseIsRequired(t *testing.T) {
	root := newAssembly(t)
	_, err := VerifyAssembly(root, "", nil, 0)
	if err == nil || !strings.Contains(err.Error(), "canonical_base is required") {
		t.Fatalf("err = %v, want one naming canonical_base", err)
	}
}

// -- roster, subtrees and manifests agree in both directions ----------------

func TestAnUndeclaredSubtreeFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "gamma", "index.html"),
		page("Gamma", canonicalBase+"/gamma/", pageOptions{}))
	report := verifyTree(t, root)
	if !checksThatFailed(report)["roster-agreement"] {
		t.Fatal("roster-agreement did not fail")
	}
	requireFailure(t, report, "roster-agreement", "gamma")
}

func TestADeclaredProjectWithNoSubtreeFails(t *testing.T) {
	root := newAssembly(t)
	entries := append(append([]site.RosterEntry(nil), rosterEntries...),
		site.RosterEntry{Slug: "gamma", Repo: "owner/gamma"})
	writeFile(t, filepath.Join(root, "roster.toml"),
		site.RenderRoster(entries, homeSlug))
	report := verifyTree(t, root)
	requireFailure(t, report, "roster-agreement", "gamma", "no site/ subtree")
}

func TestAnOrphanManifestFails(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "manifests", "gamma-posts.json"),
		manifestDoc("gamma", "Gamma", "1.0.0", nil, nil))
	report := verifyTree(t, root)
	requireFailure(t, report, "roster-agreement", "gamma-posts.json")
}

// TestAnOrphanFilesSidecarFails asserts that a sidecar of any kind counts: it
// is not a manifest, but it is a trace.
func TestAnOrphanFilesSidecarFails(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "manifests", "gamma-files.json"), map[string]any{
		"schema_version": 1, "slug": "gamma", "owners": map[string]any{},
	})
	report := verifyTree(t, root)
	requireFailure(t, report, "roster-agreement", "gamma-files.json")
}

func TestADeclaredProjectWithNoManifestFails(t *testing.T) {
	root := newAssembly(t)
	removeFile(t, filepath.Join(root, "manifests", "beta.json"))
	report := verifyTree(t, root)
	requireFailure(t, report, "roster-agreement", "beta", "manifests/<slug>.json")
}

func TestAMembershipRecordForAnUndeclaredProjectFails(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "projects.json"), map[string]any{
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v1", "version": "1.0.0"},
		"beta":  map[string]any{"repo": "owner/beta", "ref": "v2", "version": "2.0.0"},
		"gamma": map[string]any{"repo": "owner/gamma", "ref": "v3", "version": "3.0.0"},
	})
	report := verifyTree(t, root)
	requireFailure(t, report, "roster-agreement", "gamma")
}

// -- a manifest describes the tree it sits next to --------------------------

func TestAManifestNamingAnotherSlugFails(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "manifests", "beta.json"), manifestDoc(
		"elsewhere", "Beta", "2.0.0",
		[]any{map[string]any{"path": "index.md", "title": "Home"}}, nil,
	))
	report := verifyTree(t, root)
	requireFailure(t, report, "manifest-identity", "elsewhere")
}

// TestAManifestVersionThePagesDisagreeWithFails covers the classic stale
// deploy: manifest and tree from different builds.
func TestAManifestVersionThePagesDisagreeWithFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{Version: "1.9.0"}))
	report := verifyTree(t, root)
	requireFailure(t, report, "manifest-identity", "1.9.0", "2.0.0")
}

func TestTheCurrentVersionSittingInTheArchiveFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "alpha", "v", "1.0.0", "index.html"),
		page("Alpha", canonicalBase+"/alpha/", pageOptions{Version: "1.0.0"}))
	report := verifyTree(t, root)
	requireFailure(t, report, "manifest-identity", "v/1.0.0")
}

// -- every page and post a manifest lists was emitted -----------------------

func TestAListedPageThatWasNotEmittedFails(t *testing.T) {
	root := newAssembly(t)
	removeFile(t, filepath.Join(root, "site", "alpha", "guide", "index.html"))
	report := verifyTree(t, root)
	requireFailure(t, report, "manifest-pages-emitted", "guide.md")
}

func TestAListedPostThatWasNotEmittedFails(t *testing.T) {
	root := newAssembly(t)
	removeFile(t, filepath.Join(root, "site", "blog", "hello", "index.html"))
	report := verifyTree(t, root)
	requireFailure(t, report, "manifest-posts-emitted", "hello")
	requireFailure(t, report, "manifest-posts-emitted", "site/blog/hello/index.html")
}

// TestAPostEmittedUnderItsProjectSlugIsNotTheAddressChecked moves the post
// back to the old address: nothing serves it there.
func TestAPostEmittedUnderItsProjectSlugIsNotTheAddressChecked(t *testing.T) {
	root := newAssembly(t)
	if err := os.Rename(
		filepath.Join(root, "site", "blog", "hello"),
		filepath.Join(root, "site", "alpha", "posts"),
	); err != nil {
		t.Fatalf("rename: %v", err)
	}
	report := verifyTree(t, root)
	if len(report.FailuresOf("manifest-posts-emitted")) == 0 {
		t.Fatal("manifest-posts-emitted did not fail")
	}
}

// TestAPostOverlayIsVerifiedToo asserts that the overlay replaces the
// manifest's posts, so it is what is checked.
func TestAPostOverlayIsVerifiedToo(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "manifests", "alpha-posts.json"), manifestDoc(
		"alpha", "Alpha", "1.0.0", nil,
		[]any{map[string]any{
			"slug": "out-of-band", "title": "Out of band",
			"date": "2024-07-01", "path": "blog/out-of-band.md", "tags": []any{},
		}},
	))
	report := verifyTree(t, root)
	requireFailure(t, report, "manifest-posts-emitted", "out-of-band")
}

// -- the shared artifacts ---------------------------------------------------

func TestAMissingSharedArtifactFails(t *testing.T) {
	for _, rel := range []string{
		"index.html", "blog/index.html", "robots.txt", "llms.txt", "404.html",
		"nav.json",
	} {
		t.Run(rel, func(t *testing.T) {
			root := newAssembly(t)
			removeFile(t, filepath.Join(root, "site",
				filepath.Join(strings.Split(rel, "/")...)))
			report := verifyTree(t, root)
			requireFailure(t, report, "shared-artifacts", rel)
		})
	}
}

func TestAnUnparsableSitemapFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "sitemap.xml"), "<urlset><url></urlset>")
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "sitemap.xml", "does not parse")
}

func TestAnUnparsableFeedFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "feed.xml"), "<feed><entry></feed>")
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "feed.xml", "does not parse")
}

func TestAnUnparsableNavJSONFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "nav.json"), "{not json")
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "nav.json", "does not parse")
}

// The search index used to be asserted by existence alone -- any file at all
// under pagefind/ counted as one. pagefind writes its runtime JS whether or
// not it indexed anything, so a directory holding only that passed while the
// site answered no searches. What the runtime actually loads is asserted now.

// TestASearchIndexOfRuntimeFilesOnlyFails covers pagefind's JS being present
// while its index is not: the site answers nothing.
func TestASearchIndexOfRuntimeFilesOnlyFails(t *testing.T) {
	root := newAssembly(t)
	index := filepath.Join(root, "site", "pagefind")
	removeFile(t, filepath.Join(index, "pagefind-entry.json"))
	entries, err := os.ReadDir(filepath.Join(index, "fragment"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		removeFile(t, filepath.Join(index, "fragment", entry.Name()))
	}
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "pagefind-entry.json")
	requireFailure(t, report, "shared-artifacts", "fragment")
}

func TestAnUnparsableSearchIndexEntryFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "pagefind", "pagefind-entry.json"),
		"{not json")
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts",
		"pagefind-entry.json", "does not parse")
}

func TestASearchIndexDeclaringNoLanguageFails(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "site", "pagefind", "pagefind-entry.json"),
		map[string]any{"version": "1.3.0", "languages": map[string]any{}})
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "pagefind-entry.json")
}

func TestASearchIndexThatIndexedNoPagesFails(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "site", "pagefind", "pagefind-entry.json"),
		map[string]any{
			"version": "1.3.0",
			"languages": map[string]any{
				"en": map[string]any{"hash": "en_abc123", "wasm": "en", "page_count": 0},
			},
		})
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "indexed page")
}

// TestASearchIndexWithNoFragmentsFails: a match with no fragment has no page
// record to render a result from.
func TestASearchIndexWithNoFragmentsFails(t *testing.T) {
	root := newAssembly(t)
	fragments := filepath.Join(root, "site", "pagefind", "fragment")
	entries, err := os.ReadDir(fragments)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		removeFile(t, filepath.Join(fragments, entry.Name()))
	}
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "fragment")
}

// TestTheListingLivesAtItsFixedAddress: the generated listing is /projects/,
// and its absence is a failure.
func TestTheListingLivesAtItsFixedAddress(t *testing.T) {
	root := newAssembly(t)
	if report := verifyTree(t, root); !report.OK() {
		t.Fatalf("%s", report.ErrorText())
	}
	removeFile(t, filepath.Join(root, "site", "projects", "index.html"))
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "projects/index.html")
}

// TestTheHomeProjectsFrontPageIsTheSiteRoot: the site root is the home
// project's page, and its absence is a failure.
func TestTheHomeProjectsFrontPageIsTheSiteRoot(t *testing.T) {
	root := newAssembly(t)
	removeFile(t, filepath.Join(root, "site", "index.html"))
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "index.html")
}

// -- the root 404 is a not-found page, not a second front page --------------

// TestA404ThatRepeatsTheFrontPageFails covers a soft 404: an unknown address
// renders the home page and reads as one.
func TestA404ThatRepeatsTheFrontPageFails(t *testing.T) {
	root := newAssembly(t)
	front := readFile(t, filepath.Join(root, "site", "index.html"))
	writeFile(t, filepath.Join(root, "site", "404.html"), front)
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "404.html", "front page")
}

func TestA404WithAnEmptyBodyFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "404.html"),
		page("Page not found", canonicalBase+"/404.html", pageOptions{}))
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "404.html")
}

// TestA404ThatIsADeadEndFails asserts every way back: a 404 with no links out
// strands a reader.
func TestA404ThatIsADeadEndFails(t *testing.T) {
	for _, way := range [][2]string{
		{"/", "./"}, {"/projects/", "projects/"}, {"/blog/", "blog/"},
	} {
		gone, href := way[0], way[1]
		t.Run(gone, func(t *testing.T) {
			root := newAssembly(t)
			path := filepath.Join(root, "site", "404.html")
			html := readFile(t, path)
			writeFile(t, path, strings.ReplaceAll(
				html, `href="`+href+`"`, `href="#"`))
			report := verifyTree(t, root)
			requireFailure(t, report, "shared-artifacts", "404.html", gone)
		})
	}
}

// -- robots.txt names the sitemap -------------------------------------------

func TestRobotsThatNamesNoSitemapFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "robots.txt"),
		"User-agent: *\nAllow: /\n")
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "robots.txt", "Sitemap")
}

func TestRobotsThatNamesARelativeSitemapFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "robots.txt"),
		"User-agent: *\nAllow: /\n\nSitemap: /sitemap.xml\n")
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "robots.txt")
}

// TestRobotsNamingASitemapTheTreeLacksFails: robots.txt points at a file the
// deploy did not write.
func TestRobotsNamingASitemapTheTreeLacksFails(t *testing.T) {
	root := newAssembly(t)
	removeFile(t, filepath.Join(root, "site", "sitemap.xml"))
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts",
		"robots.txt", "the tree does not carry")
}

// -- llms.txt references every declared project -----------------------------

func TestLLMSTxtMissingADeclaredProjectFails(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "llms.txt")
	llms := readFile(t, path)
	writeFile(t, path, strings.ReplaceAll(
		llms, canonicalBase+"/beta/llms.txt", "#"))
	report := verifyTree(t, root)
	requireFailure(t, report, "shared-artifacts", "llms.txt", "beta")
}

func TestLLMSTxtReferencingEveryDeclaredProjectPasses(t *testing.T) {
	report := verifyTree(t, newAssembly(t))
	for _, failure := range report.FailuresOf("shared-artifacts") {
		if strings.Contains(failure.Offender, "llms.txt") {
			t.Fatalf("%s", report.ErrorText())
		}
	}
}

// -- every reference resolves -----------------------------------------------

func TestALinkToAPageThatWasNotWrittenFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "alpha", "index.html"),
		page("Alpha", canonicalBase+"/alpha/", pageOptions{
			Body: `  <a href="missing/">Missing</a>`, Version: "1.0.0",
		}))
	report := verifyTree(t, root)
	requireFailure(t, report, "internal-references", "missing/")
}

func TestASitemapEntryWithNoPageFails(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "sitemap.xml")
	writeFile(t, path, strings.Replace(readFile(t, path), "</urlset>",
		"  <url><loc>"+canonicalBase+"/alpha/ghost/</loc></url>\n</urlset>", 1))
	report := verifyTree(t, root)
	requireFailure(t, report, "sitemap-entries", "ghost")
}

// TestASitemapEntryUnderAForeignBaseFails: a <loc> that is not this site's is
// an entry nothing here can serve.
//
// The resolution pass measures absolute URLs against the canonical base and
// has nothing to say about somebody else's -- which let an entry on a stale or
// wrong host pass verification silently, the one place a sitemap entry is
// checked at all.
func TestASitemapEntryUnderAForeignBaseFails(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "sitemap.xml")
	writeFile(t, path, strings.Replace(readFile(t, path), "</urlset>",
		"  <url><loc>https://old.example.net/alpha/</loc></url>\n</urlset>", 1))
	report := verifyTree(t, root)
	requireFailure(t, report, "sitemap-entries", "https://old.example.net/alpha/")
}

// TestASitemapIndexEntryUnderAForeignBaseFails: the same holds for a sitemap
// index naming a sitemap elsewhere.
func TestASitemapIndexEntryUnderAForeignBaseFails(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "sitemap.xml")
	writeFile(t, path, strings.Replace(readFile(t, path), "</urlset>",
		"  <url><loc>https://old.example.net/sitemap.xml</loc></url>\n</urlset>", 1))
	report := verifyTree(t, root)
	requireFailure(t, report, "sitemap-entries", "https://old.example.net/sitemap.xml")
}

// TestEverySitemapEntryOfACleanTreePasses: the fixture's own sitemap names
// only addresses under the base.
func TestEverySitemapEntryOfACleanTreePasses(t *testing.T) {
	report := verifyTree(t, newAssembly(t))
	if len(report.FailuresOf("sitemap-entries")) != 0 {
		t.Fatalf("%s", report.ErrorText())
	}
}

func TestAFeedLinkWithNoPageFails(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "feed.xml")
	writeFile(t, path, strings.Replace(readFile(t, path), "</feed>",
		`  <entry><link href="`+canonicalBase+`/blog/ghost/"/></entry>`+"\n</feed>", 1))
	report := verifyTree(t, root)
	requireFailure(t, report, "feed-links", "ghost")
}

// -- every page is addressable ----------------------------------------------

func TestAPageWithNoTitleFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		`<html><head><title></title>`+
			`<link rel="canonical" href="`+canonicalBase+`/beta/">`+
			`</head><body>b</body></html>`)
	report := verifyTree(t, root)
	requireFailure(t, report, "page-metadata", "beta/index.html", "no title")
}

func TestAPageWithNoCanonicalFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		"<html><head><title>Beta</title></head><body>b</body></html>")
	report := verifyTree(t, root)
	requireFailure(t, report, "page-metadata", "rel=canonical")
}

// TestTheRoot404NeedsNoCanonical: the one page whose canonical is required to
// be absent.
//
// The shared generator writes it without one, so the clean tree already
// carries a canonical-less page; this says that is the passing shape rather
// than an unnoticed hole.
func TestTheRoot404NeedsNoCanonical(t *testing.T) {
	root := newAssembly(t)
	notFound := readFile(t, filepath.Join(root, "site", "404.html"))
	if strings.Contains(notFound, `rel="canonical"`) {
		t.Fatal("the generated 404 declares a canonical")
	}
	report := verifyTree(t, root)
	for _, failure := range report.FailuresOf("page-metadata") {
		if strings.Contains(failure.Offender, "404.html") {
			t.Fatalf("404 reported: %s", failure)
		}
	}
}

func TestA404DeclaringACanonicalFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "404.html"),
		page("Page not found", canonicalBase+"/404.html", pageOptions{}))
	report := verifyTree(t, root)
	requireFailure(t, report, "page-metadata", "404.html", "canonical")
}

func TestA404WithNoTitleStillFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "404.html"),
		"<html><head><title></title></head><body>gone</body></html>")
	report := verifyTree(t, root)
	requireFailure(t, report, "page-metadata", "404.html", "no title")
}

// TestAProjectSubtree404Fails: a subtree 404 is never served, so it has no
// business in the tree.
func TestAProjectSubtree404Fails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "404.html"),
		page("Page not found", "", pageOptions{}))
	report := verifyTree(t, root)
	requireFailure(t, report, "routing-artifacts", "beta/404.html")
}

func TestACanonicalPointingOffTheSiteFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", "https://elsewhere.example.net/beta/",
			pageOptions{Version: "2.0.0"}))
	report := verifyTree(t, root)
	requireFailure(t, report, "page-metadata", "elsewhere.example.net")
}

// -- every page is styled by the site's own chrome --------------------------

func TestTheCleanTreeCarriesASiteLevelChromeAsset(t *testing.T) {
	root := newAssembly(t)
	entries, err := os.ReadDir(filepath.Join(root, "site", chrome.Dir))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d chrome assets, want 1", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".css") {
		t.Fatalf("chrome asset %q is not a stylesheet", entries[0].Name())
	}
}

// stylesheetLinkRE matches the re-pointed stylesheet link on a page, so a test
// can take it away.
var stylesheetLinkRE = regexp.MustCompile(
	`<link rel="stylesheet" href="[^"]*_chrome[^"]*">\n?`)

// TestAPageWithNoStylesheetFails covers the live defect: the shared pages
// shipped as bare HTML.
func TestAPageWithNoStylesheetFails(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "blog", "index.html")
	pageHTML := readFile(t, path)
	stripped := stylesheetLinkRE.ReplaceAllString(pageHTML, "")
	if stripped == pageHTML {
		t.Fatal("the blog index carried no re-pointed stylesheet to strip")
	}
	writeFile(t, path, stripped)
	report := verifyTree(t, root)
	requireFailure(t, report, "site-chrome", "blog/index.html", "unstyled")
}

// TestAPagePointingAtItsOwnSubtreeCopyFails names a page the next
// presentation fix would not reach.
func TestAPagePointingAtItsOwnSubtreeCopyFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{
			Version: "2.0.0", CSSHref: "style.css",
		}))
	writeFile(t, filepath.Join(root, "site", "beta", "style.css"), "/* local */")
	report := verifyTree(t, root)
	requireFailure(t, report, "site-chrome", "beta/index.html", chrome.Dir)
}

func TestAMissingChromeAssetFails(t *testing.T) {
	root := newAssembly(t)
	clearChrome(t, root)
	report := verifyTree(t, root)
	requireFailure(t, report, "site-chrome", chrome.Dir, "no stylesheet")
}

// TestTheChromeReferenceIsResolvedByTheLinkPass: a dangling stylesheet href is
// the LINK001 pass's failure, not a new one.
func TestTheChromeReferenceIsResolvedByTheLinkPass(t *testing.T) {
	root := newAssembly(t)
	clearChrome(t, root)
	report := verifyTree(t, root)
	requireFailure(t, report, "internal-references", chrome.Dir)
}

// clearChrome deletes every site-level chrome asset.
func clearChrome(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "site", chrome.Dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
}

// -- nothing half-built or per-project leaked in ----------------------------

func TestAnUnresolvedDirectiveMarkerFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{
			Version: "2.0.0",
			Body: "<blockquote><em>[selfdoc: python_ref target=x " +
				"— not yet resolved]</em></blockquote>",
		}))
	report := verifyTree(t, root)
	requireFailure(t, report, "unresolved-directives", "not yet resolved")
}

func TestARawDirectiveMarkerInProseFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{
			Version: "2.0.0",
			Body:    "<p>\n:-: python_ref target=selfdoc.build\n</p>",
		}))
	report := verifyTree(t, root)
	if !checksThatFailed(report)["unresolved-directives"] {
		t.Fatal("unresolved-directives did not fail")
	}
}

// TestADirectiveQuotedInACodeBlockIsNotADefect: the directive documentation
// quotes every marker there is.
func TestADirectiveQuotedInACodeBlockIsNotADefect(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{
			Version: "2.0.0",
			Body: "<pre><code>:-: python_ref target=x\n" +
				":&lt;: cli_ref\n:&gt;:</code></pre>",
		}))
	report := verifyTree(t, root)
	if got := report.FailuresOf("unresolved-directives"); len(got) != 0 {
		t.Fatalf("unresolved-directives reported %v", got)
	}
}

func TestAPerProjectRoutingArtifactFails(t *testing.T) {
	for _, rel := range []string{
		"alpha/_headers", "alpha/_redirects", "alpha/_worker.js",
		"alpha/index.html.gz", "alpha/guide/index.html.br",
	} {
		t.Run(rel, func(t *testing.T) {
			root := newAssembly(t)
			writeFile(t, filepath.Join(root, "site",
				filepath.Join(strings.Split(rel, "/")...)), "leaked")
			report := verifyTree(t, root)
			requireFailure(t, report, "routing-artifacts", rel)
		})
	}
}

func TestTheSitesOwnRoutingFilesAreNotADefect(t *testing.T) {
	root := newAssembly(t)
	for _, name := range []string{"_headers", "404.html"} {
		if _, err := os.Stat(filepath.Join(root, "site", name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
	report := verifyTree(t, root)
	if got := report.FailuresOf("routing-artifacts"); len(got) != 0 {
		t.Fatalf("routing-artifacts reported %v", got)
	}
}

// TestAWorkerAtTheSiteRootFails covers the tree a deploy that predates the
// worker's retirement left behind: the assembly emits none, so one at the
// site root is an artifact nothing routes through and the next integration's
// shared-files pass deletes it.
func TestAWorkerAtTheSiteRootFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "_worker.js"),
		"export default {};\n")
	report := verifyTree(t, root)
	requireFailure(t, report, "routing-artifacts", "_worker.js")
}

// -- cross-project links ----------------------------------------------------

func TestTheLinkRegistryIsBuiltFromTheEmittedPages(t *testing.T) {
	root := newAssembly(t)
	tree, err := ReadTree(root, canonicalBase)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	registry, err := ExtractLinkRegistry(tree)
	if err != nil {
		t.Fatalf("ExtractLinkRegistry: %v", err)
	}
	got := registry["alpha/guide/index.html"]
	if len(got) != 1 || got[0] != "beta/" {
		t.Fatalf("registry[alpha/guide/index.html] = %v, want [beta/]", got)
	}
}

func TestALinkInsideOneProjectIsNotACrossProjectLink(t *testing.T) {
	root := newAssembly(t)
	tree, err := ReadTree(root, canonicalBase)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	registry, err := ExtractLinkRegistry(tree)
	if err != nil {
		t.Fatalf("ExtractLinkRegistry: %v", err)
	}
	// alpha/index.html links to alpha/guide/ -- the same project.
	if _, found := registry["alpha/index.html"]; found {
		t.Fatalf("alpha/index.html is in the registry: %v", registry)
	}
}

func TestACrossProjectLinkToAPageNobodyPublishesFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "ghost", "index.html"),
		page("Ghost", canonicalBase+"/beta/ghost/", pageOptions{Version: "2.0.0"}))
	writeFile(t, filepath.Join(root, "site", "alpha", "guide", "index.html"),
		page("Alpha Guide", canonicalBase+"/alpha/guide/", pageOptions{
			Body: `  <a href="../../beta/ghost/">Ghost</a>`, Version: "1.0.0",
		}))
	report := verifyTree(t, root)
	requireFailure(t, report, "cross-project-links", "beta/ghost/")
}

// TestACrossProjectLinkToASlugTheRosterDoesNotCarryFails covers the link a
// cross-project reference resolves to now that nothing declares a per-project
// base URL: every slug addresses "<docs_base>/<slug>/", including a slug the
// site does not serve, and the assembled tree is where that is caught.
func TestACrossProjectLinkToASlugTheRosterDoesNotCarryFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "alpha", "guide", "index.html"),
		page("Alpha Guide", canonicalBase+"/alpha/guide/", pageOptions{
			Body:    `  <a href="../../undeclared/guide/">Undeclared</a>`,
			Version: "1.0.0",
		}))
	report := verifyTree(t, root)
	if len(report.FailuresOf("internal-references")) == 0 {
		t.Fatal("a link to a slug the roster does not carry was not refused")
	}
	requireFailure(t, report, "internal-references", "undeclared/guide/")
}

// TestALinkToAPublishedPostResolves: a post is addressed at the site level
// from every project's pages.
func TestALinkToAPublishedPostResolves(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{
			Body:    `  <a href="../blog/hello/">Hello</a>`,
			Version: "2.0.0",
			CSSHref: chromeRef(t, "beta/index.html"),
		}))
	report := verifyTree(t, root)
	if got := report.FailuresOf("cross-project-links"); len(got) != 0 {
		t.Fatalf("cross-project-links reported %v", got)
	}
	if got := report.FailuresOf("internal-references"); len(got) != 0 {
		t.Fatalf("internal-references reported %v", got)
	}
}

// TestALinkToAPostUnderAProjectSlugIsADeadLink: the old address is not
// served, since nothing is emitted under <slug>/posts/.
func TestALinkToAPostUnderAProjectSlugIsADeadLink(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{
			Body:    `  <a href="../alpha/posts/hello/">Hello</a>`,
			Version: "2.0.0",
		}))
	report := verifyTree(t, root)
	if len(report.FailuresOf("internal-references")) == 0 {
		t.Fatal("internal-references did not fail")
	}
}

// -- outbound links ---------------------------------------------------------
//
// The one check that leaves the machine. Which pages are worth the requests is
// declared in a committed file; with no file there is no outbound check, which
// the report says out loud rather than passing quietly.

// fetchRecorder stands in for the network. Every URL answers 200 unless told
// otherwise.
type fetchRecorder struct {
	dead  map[string]bool
	asked []string
}

func newFetchRecorder(dead ...string) *fetchRecorder {
	set := map[string]bool{}
	for _, url := range dead {
		set[url] = true
	}
	return &fetchRecorder{dead: set}
}

func (f *fetchRecorder) fetch(url string) (int, string) {
	f.asked = append(f.asked, url)
	if f.dead[url] {
		return 404, "Not Found"
	}
	return 200, ""
}

// outboundDeclaration is the declaration the outbound tests install.
const outboundDeclaration = "cache_days = 7\n\n[[page]]\npath = \"beta/index.html\"\n"

// withOutbound declares an outbound check, and optionally puts an external
// link on a page.
func withOutbound(t *testing.T, root, body, pageBody string) {
	t.Helper()
	writeFile(t, filepath.Join(root, site.OutboundPath), body)
	if pageBody != "" {
		writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
			page("Beta", canonicalBase+"/beta/", pageOptions{
				Body:    pageBody,
				Version: "2.0.0",
				CSSHref: chromeRef(t, "beta/index.html"),
			}))
	}
}

func TestOutboundCheckingIsSkippedLoudlyWhenItIsNotConfigured(t *testing.T) {
	report := verifyTree(t, newAssembly(t))
	for _, check := range report.Ran {
		if check == "outbound-links" {
			t.Fatal("outbound-links ran with no declaration")
		}
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Check != "outbound-links" {
		t.Fatalf("skipped = %v", report.Skipped)
	}
	reason := report.Skipped[0].Reason
	if !strings.Contains(reason, site.OutboundPath) ||
		!strings.Contains(reason, "not configured") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestADeadOutboundLinkFails(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <a href="https://dead.example.net/x">x</a>`)
	fetch := newFetchRecorder("https://dead.example.net/x")
	report := verifyTreeWith(t, root, fetch.fetch, 1000.0)
	requireFailure(t, report, "outbound-links", "dead.example.net", "404")
	if len(fetch.asked) != 1 || fetch.asked[0] != "https://dead.example.net/x" {
		t.Fatalf("asked = %v", fetch.asked)
	}
}

func TestALiveOutboundLinkPasses(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <a href="https://live.example.net/x">x</a>`)
	report := verifyTreeWith(t, root, newFetchRecorder().fetch, 1000.0)
	if !report.OK() {
		t.Fatalf("%s", report.ErrorText())
	}
	found := false
	for _, check := range report.Ran {
		if check == "outbound-links" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ran = %v", report.Ran)
	}
}

// TestASecondRunInsideTheWindowMakesNoRequests is the point of the store: the
// same tree, checked twice, asks once.
func TestASecondRunInsideTheWindowMakesNoRequests(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <a href="https://live.example.net/x">x</a>`)
	first := newFetchRecorder()
	report := verifyTreeWith(t, root, first.fetch, 1000.0)
	if len(first.asked) != 1 || first.asked[0] != "https://live.example.net/x" {
		t.Fatalf("asked = %v", first.asked)
	}
	if report.Requests != 1 {
		t.Fatalf("requests = %d, want 1", report.Requests)
	}

	// The deploy is what persists the store; a second verification reads it.
	persistCache(t, root, report.OutboundCache)
	second := newFetchRecorder()
	later := verifyTreeWith(t, root, second.fetch, 1000.0+6*86400)
	if len(second.asked) != 0 {
		t.Fatalf("asked = %v", second.asked)
	}
	if later.Requests != 0 {
		t.Fatalf("requests = %d, want 0", later.Requests)
	}
	if !later.OK() {
		t.Fatalf("%s", later.ErrorText())
	}
}

func TestAResultOlderThanTheWindowIsFetchedAgain(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <a href="https://live.example.net/x">x</a>`)
	first := newFetchRecorder()
	report := verifyTreeWith(t, root, first.fetch, 1000.0)
	persistCache(t, root, report.OutboundCache)
	second := newFetchRecorder()
	verifyTreeWith(t, root, second.fetch, 1000.0+8*86400)
	if len(second.asked) != 1 || second.asked[0] != "https://live.example.net/x" {
		t.Fatalf("asked = %v", second.asked)
	}
}

func TestACachedFailureStillFailsWithoutARequest(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <a href="https://dead.example.net/x">x</a>`)
	first := newFetchRecorder("https://dead.example.net/x")
	report := verifyTreeWith(t, root, first.fetch, 1000.0)
	persistCache(t, root, report.OutboundCache)
	second := newFetchRecorder()
	later := verifyTreeWith(t, root, second.fetch, 1000.0+3600)
	if len(second.asked) != 0 {
		t.Fatalf("asked = %v", second.asked)
	}
	if len(later.FailuresOf("outbound-links")) == 0 {
		t.Fatal("outbound-links did not fail from the store")
	}
}

// persistCache writes the store the way the deploy commits it.
func persistCache(t *testing.T, root string, entries map[string]any) {
	t.Helper()
	rendered, err := site.RenderOutboundCache(entries)
	if err != nil {
		t.Fatalf("RenderOutboundCache: %v", err)
	}
	writeFile(t, filepath.Join(root, site.OutboundCachePath), rendered)
}

// TestTheSitesOwnAbsoluteURLsAreNotOutbound: nobody's server is asked -- and
// the link is still a defect here.
//
// An absolute URL into this site is not an outbound link, so the outbound
// check ignores it. The reference check does not: a link a reader clicks has
// to be document-relative, or the site resolves on the deployed host alone.
func TestTheSitesOwnAbsoluteURLsAreNotOutbound(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <a href="`+canonicalBase+`/alpha/">Alpha</a>`)
	fetch := newFetchRecorder()
	report := verifyTreeWith(t, root, fetch.fetch, 1000.0)
	if len(fetch.asked) != 0 {
		t.Fatalf("asked = %v", fetch.asked)
	}
	if got := report.FailuresOf("outbound-links"); len(got) != 0 {
		t.Fatalf("outbound-links reported %v", got)
	}
	requireFailure(t, report, "internal-references",
		"beta/index.html", "absolute against the site's own base")
}

// TestAResourceHintIsNotAnOutboundLink: a preconnect names an origin to warm,
// not a page anyone can open.
//
// The page carries both shapes: two resource hints at bare origins, which
// answer 404 to a GET and are not navigation, and one real dead link. Only the
// real one is fetched, and only the real one fails.
func TestAResourceHintIsNotAnOutboundLink(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <link rel="preconnect" href="https://fonts.example.net">`+"\n"+
			`  <link rel="dns-prefetch" href="https://cdn.example.net">`+"\n"+
			`  <a href="https://dead.example.net/x">x</a>`)
	fetch := newFetchRecorder(
		"https://dead.example.net/x",
		"https://fonts.example.net",
		"https://cdn.example.net",
	)
	report := verifyTreeWith(t, root, fetch.fetch, 1000.0)

	if len(fetch.asked) != 1 || fetch.asked[0] != "https://dead.example.net/x" {
		t.Fatalf("asked = %v", fetch.asked)
	}
	messages := messagesOf(report, "outbound-links")
	if len(messages) != 1 {
		t.Fatalf("outbound-links reported %v", messages)
	}
	if !strings.Contains(messages[0], "dead.example.net") {
		t.Fatalf("message = %q", messages[0])
	}
}

// TestAPreloadedResourceIsStillAnOutboundLink: only the origin-only hints are
// exempt, since a preload names a real file.
func TestAPreloadedResourceIsStillAnOutboundLink(t *testing.T) {
	root := newAssembly(t)
	withOutbound(t, root, outboundDeclaration,
		`  <link rel="preload" as="font" href="https://cdn.example.net/f.woff2">`)
	fetch := newFetchRecorder("https://cdn.example.net/f.woff2")
	report := verifyTreeWith(t, root, fetch.fetch, 1000.0)

	if len(fetch.asked) != 1 || fetch.asked[0] != "https://cdn.example.net/f.woff2" {
		t.Fatalf("asked = %v", fetch.asked)
	}
	if len(report.FailuresOf("outbound-links")) == 0 {
		t.Fatal("outbound-links did not fail")
	}
}

func TestADeclaredPageThatWasNotEmittedFails(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, site.OutboundPath),
		"cache_days = 7\n\n[[page]]\npath = \"ghost/index.html\"\n")
	report := verifyTreeWith(t, root, newFetchRecorder().fetch, 1000.0)
	requireFailure(t, report, "outbound-links", "ghost/index.html")
}

// TestOnlyTheDeclaredPagesAreChecked: a link on a page nobody declared costs
// no request.
func TestOnlyTheDeclaredPagesAreChecked(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "alpha", "index.html"),
		page("Alpha", canonicalBase+"/alpha/", pageOptions{
			Version: "1.0.0",
			Body: `  <a href="guide/">Guide</a>` +
				`  <a href="https://never.example.net/">never</a>`,
		}))
	withOutbound(t, root, outboundDeclaration,
		`  <a href="https://live.example.net/x">x</a>`)
	fetch := newFetchRecorder()
	verifyTreeWith(t, root, fetch.fetch, 1000.0)
	if len(fetch.asked) != 1 || fetch.asked[0] != "https://live.example.net/x" {
		t.Fatalf("asked = %v", fetch.asked)
	}
}

func TestACorruptCacheIsAHardError(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, site.OutboundCachePath), "{not json")
	_, err := VerifyAssembly(root, canonicalBase, nil, 0)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err = %v, want one naming invalid JSON", err)
	}
}

func TestAMissingRosterIsAHardError(t *testing.T) {
	root := t.TempDir()
	_, err := VerifyAssembly(root, canonicalBase, nil, 0)
	if err == nil || !strings.Contains(err.Error(), site.RosterPath) {
		t.Fatalf("err = %v, want one naming %s", err, site.RosterPath)
	}
}

// TestATreeWithNothingButARosterReportsRatherThanCrashing covers the shape a
// verification meets before any project has grafted: a roster, and nothing
// else. Every missing artifact is a finding, and no missing directory is a
// read error.
func TestATreeWithNothingButARosterReportsRatherThanCrashing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "roster.toml"),
		site.RenderRoster(rosterEntries, homeSlug))
	report := verifyTree(t, root)
	if report.OK() {
		t.Fatal("an empty tree passed verification")
	}
	for _, check := range []string{
		"roster-agreement", "shared-artifacts", "site-chrome",
	} {
		if len(report.FailuresOf(check)) == 0 {
			t.Errorf("%s reported nothing on an empty tree", check)
		}
	}
}

// -- the home project served at the site root -------------------------------

// TestAHomeSubtreeResidueIsNamed: a site/<home>/ directory is a second, stale
// copy of the front page.
func TestAHomeSubtreeResidueIsNamed(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "home", "index.html"),
		page("Stale front page", canonicalBase+"/home/", pageOptions{}))
	report := verifyTree(t, root)
	requireFailure(t, report, "home-project", "site/home", "residue")
}

func TestAHomeDirectoryThatShadowsAProjectIsNamed(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "manifests", "home-files.json")
	var record map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &record); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	owners := record["owners"].(map[string]any)
	owners["release"] = append(owners["release"].([]any), "alpha/index.html")
	writeJSON(t, path, record)
	report := verifyTree(t, root)
	requireFailure(t, report, "home-project", "alpha")
}

func TestAnEmptyRegionOnAPublishedPageIsNamed(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "index.html"),
		page("Front page", canonicalBase+"/", pageOptions{
			Body: `<selfdoc-region data-directive="projects-cards">` +
				`</selfdoc-region>`,
		}))
	report := verifyTree(t, root)
	requireFailure(t, report, "home-project", "empty site-level region")
}

func TestAnUnclosedRegionOnAPublishedPageIsNamed(t *testing.T) {
	root := newAssembly(t)
	writeFile(t, filepath.Join(root, "site", "index.html"),
		page("Front page", canonicalBase+"/", pageOptions{
			Body: `<selfdoc-region data-directive="projects-cards">`,
		}))
	report := verifyTree(t, root)
	requireFailure(t, report, "home-project", "never close")
}

// TestNavListingTheHomeProjectIsNamed: the front page does not list itself.
func TestNavListingTheHomeProjectIsNamed(t *testing.T) {
	root := newAssembly(t)
	path := filepath.Join(root, "site", "nav.json")
	var nav map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &nav); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nav["projects"] = append(nav["projects"].([]any),
		map[string]any{"slug": homeSlug, "name": "Home"})
	writeJSON(t, path, nav)
	report := verifyTree(t, root)
	requireFailure(t, report, "home-project", "site/nav.json", homeSlug)
}

// -- the report itself ------------------------------------------------------

func TestFailureRendersCheckOffenderAndMessage(t *testing.T) {
	failure := Failure{"site-chrome", "site/beta/index.html", "links nothing."}
	if got := failure.String(); got != "[site-chrome] site/beta/index.html: links nothing." {
		t.Fatalf("String() = %q", got)
	}
}

func TestFailuresAreSortedByCheckThenOffender(t *testing.T) {
	root := newAssembly(t)
	// Two checks, two offenders each, injected out of report order.
	writeFile(t, filepath.Join(root, "site", "zeta", "_headers"), "leaked")
	writeFile(t, filepath.Join(root, "site", "alpha", "_headers"), "leaked")
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		"<html><head></head><body>b</body></html>")
	report := verifyTree(t, root)

	var seen []string
	for _, failure := range report.Failures {
		seen = append(seen, failure.Check+"|"+failure.Offender)
	}
	sorted := append([]string(nil), seen...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := strings.SplitN(sorted[i], "|", 2)
		right := strings.SplitN(sorted[j], "|", 2)
		if checkOrder[left[0]] != checkOrder[right[0]] {
			return checkOrder[left[0]] < checkOrder[right[0]]
		}
		return left[1] < right[1]
	})
	if strings.Join(seen, ",") != strings.Join(sorted, ",") {
		t.Fatalf("failures out of order:\n%v\nwant\n%v", seen, sorted)
	}
}

// A project with no public version deploys under the literal, and its pages
// carry no version attribute at all. The identity check reads that as the
// agreement it is, rather than as a tree built at some other version.
func TestAnUnversionedProjectPassesManifestIdentity(t *testing.T) {
	root := newAssembly(t)
	writeJSON(t, filepath.Join(root, "manifests", "beta.json"), manifestDoc(
		"beta", "Beta", config.UnversionedVersion,
		[]any{map[string]any{"path": "index.md", "title": "Home"}}, nil,
	))
	writeJSON(t, filepath.Join(root, "projects.json"), map[string]any{
		"home":  map[string]any{"repo": "owner/home", "ref": "v0.1.0", "version": "0.1.0"},
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v1.0.0", "version": "1.0.0"},
		"beta": map[string]any{
			"repo": "owner/beta", "ref": "main", "version": config.UnversionedVersion,
		},
	})
	writeFile(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{}))
	report := verifyTree(t, root)
	for _, message := range messagesOf(report, "manifest-identity") {
		if strings.Contains(message, "beta") {
			t.Errorf("the unversioned project fails manifest-identity: %s", message)
		}
	}
}

// -- every project is reachable by following links from an arrival page ----

// TestAProjectNoLinkPathReachesFails: a project no chain of links from the
// site root or the project listing arrives at is published where nobody
// arrives.
func TestAProjectNoLinkPathReachesFails(t *testing.T) {
	root := newAssembly(t)
	front := filepath.Join(root, "site", "index.html")
	writeFile(t, front, strings.Replace(
		readFile(t, front), `<a href="beta/">Beta</a>`, "", 1))
	listing := filepath.Join(root, "site", "projects", "index.html")
	writeFile(t, listing, strings.ReplaceAll(
		readFile(t, listing), `href="../beta/"`, `href="../beta-typo/"`))
	guide := filepath.Join(root, "site", "alpha", "guide", "index.html")
	writeFile(t, guide, strings.Replace(
		readFile(t, guide), `<a href="../../beta/">Beta</a>`, "", 1))
	report := verifyTree(t, root)
	requireFailure(t, report, "project-reachability", "beta", "beta/index.html")
}

// TestAFrontPageMayCurateItsProjects: a project the front page leaves out is
// still reached through the listing.
func TestAFrontPageMayCurateItsProjects(t *testing.T) {
	root := newAssembly(t)
	front := filepath.Join(root, "site", "index.html")
	writeFile(t, front, strings.Replace(
		readFile(t, front), `<a href="alpha/">Alpha</a>`, "", 1))
	report := verifyTree(t, root)
	for _, message := range messagesOf(report, "project-reachability") {
		t.Errorf("a curated front page was refused: %s", message)
	}
}

// TestAProjectReachedThroughAnotherPageIsReachable: neither arrival page
// links beta, but alpha's page does, and alpha is linked, so a reader
// following links still arrives at beta.
func TestAProjectReachedThroughAnotherPageIsReachable(t *testing.T) {
	root := newAssembly(t)
	front := filepath.Join(root, "site", "index.html")
	writeFile(t, front, strings.Replace(
		readFile(t, front), `<a href="beta/">Beta</a>`, "", 1))
	listing := filepath.Join(root, "site", "projects", "index.html")
	writeFile(t, listing, strings.ReplaceAll(
		readFile(t, listing), `href="../beta/"`, `href="../beta-typo/"`))
	alpha := filepath.Join(root, "site", "alpha", "index.html")
	writeFile(t, alpha, strings.Replace(
		readFile(t, alpha), `</article>`, `<a href="../beta/">Beta</a></article>`, 1))
	report := verifyTree(t, root)
	for _, message := range messagesOf(report, "project-reachability") {
		t.Errorf("a project reached through another page was refused: %s", message)
	}
}

// TestTheHomeProjectIsNotAskedToLinkItself: the site root is the home
// project's own page, so it is reached by definition rather than by a link.
func TestTheHomeProjectIsNotAskedToLinkItself(t *testing.T) {
	report := verifyTree(t, newAssembly(t))
	for _, message := range messagesOf(report, "project-reachability") {
		if strings.Contains(message, homeSlug) {
			t.Errorf("the home project is asked to link itself: %s", message)
		}
	}
}
