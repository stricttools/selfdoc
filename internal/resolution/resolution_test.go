package resolution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/smm-h/stricttest/go/hygiene"
)

const base = "https://example.com"

func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// write lays one file down inside the output tree.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// check runs the resolution check, failing the test on an unexpected error.
func check(
	t *testing.T,
	outputDir, baseURL, mountPrefix string,
	exemptElements ...string,
) []lints.LintResult {
	t.Helper()
	results, err := CheckOutputResolution(outputDir, baseURL, mountPrefix, exemptElements)
	if err != nil {
		t.Fatalf("CheckOutputResolution: %v", err)
	}
	return results
}

// codes are the lint codes of results, joined for comparison.
func codes(results []lints.LintResult) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		parts = append(parts, result.Code())
	}
	return strings.Join(parts, ",")
}

// -- The absolute-anchor rule -----------------------------------------------

func TestAnAbsoluteAnchorIntoThisSiteIsReported(t *testing.T) {
	// The whole class, seen by the walker rather than by a spot check.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"),
		`<a href="`+base+`/blog/hello/">Hello</a>`)
	write(t, filepath.Join(out, "blog", "hello", "index.html"), "<p>hi</p>")

	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "absolute") {
		t.Errorf("message = %q", results[0].Message())
	}
	if !strings.Contains(results[0].Message(), base+"/blog/hello/") {
		t.Errorf("the message must name the offending address: %q", results[0].Message())
	}
	if results[0].Severity() != "error" {
		t.Errorf("severity = %q, want error", results[0].Severity())
	}
}

func TestAnAbsoluteAnchorIsReportedEvenThoughThePageExists(t *testing.T) {
	// It is not a dangling reference -- it resolves, on one host only. The
	// file the URL names is right there in the tree, which is why the
	// file-existence half of the check never saw this class.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), `<a href="`+base+`/guide/">G</a>`)
	write(t, filepath.Join(out, "guide", "index.html"), "<p>guide</p>")

	if got := codes(check(t, out, base, "")); got != LintCode {
		t.Fatalf("codes = %q, want %q", got, LintCode)
	}
}

func TestAnAnchorToSomebodyElsesSiteIsLeftAlone(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"),
		`<a href="https://github.com/owner/repo">Repository</a>`)
	if results := check(t, out, base, ""); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

func TestAbsoluteMetadataIsNotAVisibleLink(t *testing.T) {
	// Canonical, share address and feed entry name a place in the world.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"),
		`<link rel="canonical" href="`+base+`/">`+
			`<button data-share-url="`+base+`/">Copy</button>`)
	if results := check(t, out, base, ""); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

func TestARelativeAnchorToASiblingPageIsFine(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), `<a href="guide/">Guide</a>`)
	write(t, filepath.Join(out, "guide", "index.html"), "<p>guide</p>")
	if results := check(t, out, base, ""); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

// -- The mount allowance ----------------------------------------------------

func TestAMountedBuildMayClimbToItsMount(t *testing.T) {
	// A mounted build's pages reach the site level by climbing out of the
	// output root; declaring the mount is what says so.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"), `<a href="../../">Site</a>`)
	if results := check(t, out, base, "alpha/"); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

func TestTheSameOutputReadAsAWholeSiteReportsTheCrossing(t *testing.T) {
	// A build that really is its own site has no mount to climb, so a
	// reference that leaves the output root names nothing.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"), `<a href="../../">Site</a>`)
	results := check(t, out, base, "")
	if len(results) == 0 {
		t.Fatal("a reference leaving the output root must be reported")
	}
	if !strings.Contains(results[0].Message(), "escapes the output root") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestAReferenceClimbingPastTheMountIsStillAnEscape(t *testing.T) {
	// The allowance is the mount, not "relative links are fine".
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<a href="../../../elsewhere/">Out</a>`)
	results := check(t, out, base, "alpha/")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "escapes the output root") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestAMountedBuildsPostIsNotAnsweredHere(t *testing.T) {
	// A post is grafted out of the subtree to the site root, so it
	// addresses its neighbours from an address this directory does not
	// have. The assembly's own pass answers those.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "blog", "hello", "index.html"),
		`<a href="../../guide/">Guide</a>`)
	if results := check(t, out, base, "alpha/"); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

// -- Existence --------------------------------------------------------------

func TestTheCheckPassesOnAGoodTree(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), `<a href="guide/">Guide</a>`+
		`<link rel="canonical" href="`+base+`/">`)
	write(t, filepath.Join(out, "guide", "index.html"),
		`<a href="../">Home</a><link rel="canonical" href="`+base+`/guide/">`+
			`<button data-share-url="`+base+`/guide/">Copy</button>`)
	write(t, filepath.Join(out, "sitemap.xml"),
		"<urlset><url><loc>"+base+"/guide/</loc></url></urlset>")
	write(t, filepath.Join(out, "feed.xml"),
		`<feed><link href="`+base+`/feed.xml"/><link href="`+base+`/guide/"/></feed>`)
	if results := check(t, out, base, ""); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

func TestTheCheckFiresOnABrokenReference(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<a href="../nowhere/">gone</a>`)
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if results[0].File() != "guide/index.html" {
		t.Errorf("file = %q", results[0].File())
	}
	if results[0].Line() != nil {
		t.Errorf("a reference belongs to the file, not to one of its lines")
	}
	if !strings.Contains(results[0].Message(), "nowhere") {
		t.Errorf("message = %q", results[0].Message())
	}
	if !strings.Contains(results[0].Message(), "which this build did not write") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestTheCheckFiresOnAnOriginAbsoluteReference(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), `<img src="/assets/x.png">`)
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "is origin-absolute") {
		t.Errorf("message = %q", results[0].Message())
	}
	if !strings.Contains(results[0].Message(), `src="/assets/x.png"`) {
		t.Errorf("the message must quote the attribute: %q", results[0].Message())
	}
}

func TestTheCheckFiresOnAShareAddressThatWasNotWritten(t *testing.T) {
	// A share address is a reference, so LINK001 owns it too. This is the
	// structural guard behind the share control's shape: a control offering
	// the current version's v/<version>/ address -- which nothing writes
	// until that version is superseded -- is not a judgement call the
	// renderer gets to make quietly.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<button data-share-url="`+base+`/v/0.2.0/guide/">Copy</button>`)
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "share address") {
		t.Errorf("message = %q", results[0].Message())
	}
	if !strings.Contains(results[0].Message(), "v/0.2.0/guide/") {
		t.Errorf("message = %q", results[0].Message())
	}
	if results[0].File() != "guide/index.html" {
		t.Errorf("file = %q", results[0].File())
	}
}

func TestTheCheckFiresOnABrokenCanonical(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"),
		`<link rel="canonical" href="`+base+`/ghost/">`)
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "canonical") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestTheCheckFiresOnABrokenSitemapEntry(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), "<p>home</p>")
	write(t, filepath.Join(out, "sitemap.xml"),
		"<urlset><url><loc>"+base+"/ghost/</loc></url></urlset>")
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "ghost") {
		t.Errorf("message = %q", results[0].Message())
	}
	if !strings.Contains(results[0].Message(), "sitemap entry") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestTheCheckFiresOnASitemapIndexEntryThatWasNotWritten(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), "<p>home</p>")
	write(t, filepath.Join(out, "sitemap.xml"),
		"<sitemapindex><sitemap><loc>"+base+
			"/sitemap-en.xml</loc></sitemap></sitemapindex>")
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "sitemap index entry") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestTheCheckFiresOnABrokenFeedLink(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), "<p>home</p>")
	write(t, filepath.Join(out, "feed.xml"),
		`<feed><link href="`+base+`/feed.xml"/><link href="`+base+`/ghost/"/></feed>`)
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "feed link") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestADirectoryWithNoHTMLYieldsNothing(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "notes.txt"), "not a site")
	if results := check(t, out, base, ""); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

func TestAnAbsentDirectoryYieldsNothing(t *testing.T) {
	isolate(t)
	if results := check(t, filepath.Join(t.TempDir(), "missing"), base, ""); len(results) != 0 {
		t.Fatalf("results = %v", results)
	}
}

func TestCompressedCopiesAreNotEmittedFiles(t *testing.T) {
	// The build writes .gz and .br beside each page; nothing references
	// them, and counting them as emitted would let a reference to a missing
	// page pass because its compressed copy exists.
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"), `<a href="guide/">Guide</a>`)
	write(t, filepath.Join(out, "guide", "index.html.gz"), "compressed")
	write(t, filepath.Join(out, "guide", "index.html.br"), "compressed")
	results := check(t, out, base, "")
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q", codes(results), LintCode)
	}
}

// -- The reference collectors ----------------------------------------------

func TestReferenceTarget(t *testing.T) {
	cases := []struct {
		pageRel   string
		ref       string
		want      string
		addresses bool
	}{
		{"index.html", "guide/", "guide/index.html", true},
		{"index.html", "guide/index.html", "guide/index.html", true},
		{"guide/index.html", "../", "index.html", true},
		{"guide/index.html", "../checks/#detail", "checks/index.html", true},
		{"guide/index.html", "?q=1", "", false},
		{"guide/index.html", "#section", "", false},
		{"guide/index.html", "", "", false},
		{"index.html", "./", "index.html", true},
		{"guide/index.html", "../../out/", "../out/index.html", true},
	}
	for _, testCase := range cases {
		got, addresses := ReferenceTarget(testCase.pageRel, testCase.ref)
		if addresses != testCase.addresses || got != testCase.want {
			t.Errorf("ReferenceTarget(%q, %q) = (%q, %v), want (%q, %v)",
				testCase.pageRel, testCase.ref, got, addresses,
				testCase.want, testCase.addresses)
		}
	}
}

func TestSiteRelativePath(t *testing.T) {
	cases := []struct {
		url  string
		base string
		want string
		ours bool
	}{
		{base, base, "index.html", true},
		{base + "/", base, "index.html", true},
		{base + "/guide/", base, "guide/index.html", true},
		{base + "/guide/index.html", base, "guide/index.html", true},
		{base + "/guide/?q=1", base, "guide/index.html", true},
		{base + "/guide/#x", base, "guide/index.html", true},
		{"https://other.example/guide/", base, "", false},
		{base + "/guide/", "", "", false},
		{base + "/guide/", base + "/", "guide/index.html", true},
	}
	for _, testCase := range cases {
		got, ours := SiteRelativePath(testCase.url, testCase.base)
		if ours != testCase.ours || got != testCase.want {
			t.Errorf("SiteRelativePath(%q, %q) = (%q, %v), want (%q, %v)",
				testCase.url, testCase.base, got, ours,
				testCase.want, testCase.ours)
		}
	}
}

func TestPageReferencesDropsWhatAddressesNothingHere(t *testing.T) {
	page := `<a href="guide/">g</a><img src="x.png">` +
		`<script src="https://cdn.example/s.js"></script>` +
		`<a href="#section">s</a><a href="mailto:a@b.c">m</a>` +
		`<a href="">empty</a><div data-search-base="../"></div>` +
		`<a href="//cdn.example/x">protocol relative</a>` +
		`<a href="a&amp;b/">escaped</a>`
	var got []string
	for _, reference := range PageReferences(page) {
		got = append(got, reference.Attr+"="+reference.Ref)
	}
	want := "href=guide/,src=x.png,data-search-base=../,href=a&b/"
	if strings.Join(got, ",") != want {
		t.Fatalf("PageReferences = %q, want %q", strings.Join(got, ","), want)
	}
}

func TestNavigationReferencesReadsOnlyAnchors(t *testing.T) {
	page := `<a class="x" href="guide/">g</a><img src="x.png">` +
		`<A HREF="other/">upper</A><link rel="stylesheet" href="s.css">`
	got := strings.Join(NavigationReferences(page), ",")
	if got != "guide/,other/" {
		t.Fatalf("NavigationReferences = %q", got)
	}
}

// A structured-data document states where a page sits in the world, in
// absolute URLs, and none of them is a link a reader clicks. The rule that
// refuses an absolute self-link reads anchors and reference attributes, so a
// JSON-LD block passes through it untouched.
func TestStructuredDataURLsAreNotClickableReferences(t *testing.T) {
	page := `<script type="application/ld+json">` +
		`{"@type": "CollectionPage", "url": "https://docs.example.com/projects/", ` +
		`"breadcrumb": {"itemListElement": [{"item": "https://docs.example.com/"}]}}` +
		`</script><a href="../alpha/">Alpha</a>`
	if got := NavigationReferences(page); len(got) != 1 || got[0] != "../alpha/" {
		t.Errorf("NavigationReferences = %q, want just the anchor", got)
	}
	if got := ExternalReferences(page); len(got) != 0 {
		t.Errorf("ExternalReferences = %q, want none", got)
	}
	for _, reference := range PageReferences(page) {
		if strings.Contains(reference.Ref, "docs.example.com") {
			t.Errorf("a structured-data URL was collected as %v", reference)
		}
	}
}

func TestExternalReferencesSkipsOriginOnlyHints(t *testing.T) {
	page := `<link rel="preconnect" href="https://fonts.example">` +
		`<link rel="preload" href="https://cdn.example/f.woff2">` +
		`<a href="https://github.com/owner/repo">r</a>` +
		`<a href="guide/">g</a>`
	got := strings.Join(ExternalReferences(page), ",")
	want := "https://cdn.example/f.woff2,https://github.com/owner/repo"
	if got != want {
		t.Fatalf("ExternalReferences = %q, want %q", got, want)
	}
}

func TestExternalReferencesKeepsALinkToAPreconnectedOrigin(t *testing.T) {
	// The exemption is per-element: a page that both preconnects to an
	// origin and links to it still has the link collected.
	page := `<link rel="preconnect" href="https://fonts.example">` +
		`<a href="https://fonts.example/specimen">s</a>`
	got := strings.Join(ExternalReferences(page), ",")
	if got != "https://fonts.example/specimen" {
		t.Fatalf("ExternalReferences = %q", got)
	}
}

// -- elements this build does not answer for --------------------------------

// A site-level region is written from the assembled site's own data and
// re-rendered by the assembly on every deploy. Its links address the assembled
// site -- other projects' subtrees, the site-level blog -- which the project
// that carries the region never writes. The assembly's own pass over the whole
// tree answers them; this one cannot.
func TestAReferenceInsideAnExemptElementIsNotThisBuildsToResolve(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"),
		"<p>Mine</p>\n"+
			`<selfdoc-region data-directive="projects-cards">`+"\n"+
			`<a href="alpha/">Alpha</a>`+"\n"+
			"</selfdoc-region>\n")

	if results := check(t, out, base, "", "selfdoc-region"); len(results) != 0 {
		t.Fatalf("the exempt element's reference was reported: %v", results)
	}
	if results := check(t, out, base, ""); len(results) != 1 {
		t.Fatalf("without the exemption the reference is this build's: %v", results)
	}
}

// The exemption is per element, not per page: a link the page itself writes is
// still this build's to resolve.
func TestAPagesOwnReferenceSurroundingAnExemptElementIsStillChecked(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "index.html"),
		`<a href="missing/">Mine</a>`+"\n"+
			`<selfdoc-region data-directive="projects-cards">`+"\n"+
			`<a href="alpha/">Alpha</a>`+"\n"+
			"</selfdoc-region>\n")

	results := check(t, out, base, "", "selfdoc-region")
	if len(results) != 1 {
		t.Fatalf("results = %v, want the page's own dangling reference", results)
	}
	if !strings.Contains(results[0].Message(), "missing/") {
		t.Errorf("message = %q", results[0].Message())
	}
}

// -- rewriting the references a reader clicks -------------------------------

// The rewriter and the check have to recognise the same elements, or a tree
// repaired by one still fails the other.
func TestRewriteNavigationReferencesTouchesOnlyClickableReferences(t *testing.T) {
	page := `<link rel="canonical" href="` + base + `/guide/">` +
		`<a class="nav" href="` + base + `/blog/" data-x="1">Posts</a>` +
		`<img src="` + base + `/og.png">` +
		`<button data-share-url="` + base + `/guide/">Share</button>` +
		`<a href="../other/">Other</a>`

	got := RewriteNavigationReferences(page, func(ref string) (string, bool) {
		rest, ours := SiteRelativePath(ref, base)
		if !ours {
			return "", false
		}
		return "../" + strings.TrimSuffix(rest, "index.html"), true
	})

	want := `<link rel="canonical" href="` + base + `/guide/">` +
		`<a class="nav" href="../blog/" data-x="1">Posts</a>` +
		`<img src="` + base + `/og.png">` +
		`<button data-share-url="` + base + `/guide/">Share</button>` +
		`<a href="../other/">Other</a>`
	if got != want {
		t.Fatalf("RewriteNavigationReferences =\n%s\nwant\n%s", got, want)
	}
}

// A page nothing answers for comes back byte-identical, so a caller can
// compare and skip the write.
func TestRewriteNavigationReferencesLeavesAPageItAnswersNothingForAlone(t *testing.T) {
	page := `<a href="../guide/">Guide</a><a href="https://elsewhere.example/">Away</a>`
	if got := RewriteNavigationReferences(page, func(string) (string, bool) {
		return "", false
	}); got != page {
		t.Fatalf("RewriteNavigationReferences = %q, want the page unchanged", got)
	}
}

// -- The stale-build filter -------------------------------------------------

// checkSourced runs the project-aware resolution check, handing it the current
// Markdown of the pages it names.
func checkSourced(
	t *testing.T, outputDir string, sources map[string]string,
) []lints.LintResult {
	t.Helper()
	results, err := CheckProjectOutputResolution(outputDir, base, "", nil, sources)
	if err != nil {
		t.Fatalf("CheckProjectOutputResolution: %v", err)
	}
	return results
}

func TestABrokenLinkTheCurrentSourceWritesIsReported(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<main id="tm-content"><a href="../missing/">Guide</a></main>`)
	results := checkSourced(t, out, map[string]string{
		"guide/index.html": "# Guide\n\nSee the [Guide](missing.md).\n",
	})
	if codes(results) != LintCode {
		t.Fatalf("codes = %q, want %q -- the source still writes the "+
			"reference the built page carries", codes(results), LintCode)
	}
	if !strings.Contains(results[0].Message(), "missing") {
		t.Errorf("message = %q", results[0].Message())
	}
}

func TestAFragmentTheCurrentSourceWritesIsReported(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<main id="tm-content"><a href="../missing/#detail">Guide</a></main>`)
	if codes(checkSourced(t, out, map[string]string{
		"guide/index.html": "# Guide\n\n[Guide](missing.md#detail)\n",
	})) != LintCode {
		t.Fatal("a reference the source writes with a fragment is still checked")
	}
}

func TestALinkOnlyAnOlderRenderingWroteIsNotReported(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<main id="tm-content"><a href="../missing/">Gone</a></main>`)
	if results := checkSourced(t, out, map[string]string{
		"guide/index.html": "# Guide\n\nThe link is gone from the source.\n",
	}); len(results) != 0 {
		t.Fatalf("results = %v -- the built body predates its source", results)
	}
}

func TestAPageWithNoCurrentSourceIsCheckedAsBefore(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<main id="tm-content"><a href="../missing/">Gone</a></main>`)
	if codes(checkSourced(t, out, map[string]string{})) != LintCode {
		t.Fatal("a page this run did not resolve is checked as it always was")
	}
}

func TestAReferenceThatIsNotAPageIsTakenFromTheSourceAsWritten(t *testing.T) {
	isolate(t)
	out := filepath.Join(t.TempDir(), "out")
	write(t, filepath.Join(out, "guide", "index.html"),
		`<main id="tm-content"><img src="../assets/logo.png"></main>`)
	if codes(checkSourced(t, out, map[string]string{
		"guide/index.html": "# Guide\n\n![Logo](../assets/logo.png)\n",
	})) != LintCode {
		t.Fatal("an asset reference the source writes is still checked")
	}
}
