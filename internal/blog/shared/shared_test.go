package shared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/robots"
)

const canonicalBase = "https://docs.example.com"

// reference reads a file recorded from the Python this package ports, written
// by scripts/record_blog_shared_reference.py.
func reference(t *testing.T, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading reference %s: %v", name, err)
	}
	return string(text)
}

func manifest(name, slug, version, description string, pages, posts []any) map[string]any {
	if pages == nil {
		pages = []any{}
	}
	if posts == nil {
		posts = []any{}
	}
	return map[string]any{
		"schema_version": 1,
		"name":           name,
		"slug":           slug,
		"version":        version,
		"description":    description,
		"language":       "python",
		"base_url":       "https://example.com/" + slug,
		"author": map[string]any{
			"name": "Test Author", "url": "https://author.example",
		},
		"pages":    pages,
		"posts":    posts,
		"last_gen": "2024-01-01T00:00:00+00:00",
	}
}

func pageEntry(path, title string) any {
	return map[string]any{"path": path, "title": title}
}

func postEntry(slug, title, date string) any {
	return map[string]any{
		"slug": slug, "title": title, "date": date,
		"path": "blog/" + slug + ".md", "tags": []any{},
	}
}

// roster is the fixture the recorder script used, so every reference file in
// testdata/ is this roster's rendering.
func roster() []map[string]any {
	return []map[string]any{
		manifest("Alpha", "alpha", "1.0.0", "Does the alpha thing.",
			[]any{pageEntry("index.md", "Home"), pageEntry("guide.md", "Guide"),
				pageEntry("api/reference.md", "API")},
			[]any{postEntry("hello", "Hello", "2024-06-01")}),
		manifest("Zebra", "zebra", "0.0.0", "Does the zebra thing.",
			[]any{pageEntry("index.md", "Home")},
			[]any{postEntry("later", "Later", "2025-02-03")}),
		manifest("Home", "home", "0.1.0", "The front page.\nSecond line.",
			[]any{pageEntry("index.md", "Home"), pageEntry("cv.md", "CV")}, nil),
	}
}

func mixedCase() []map[string]any {
	return []map[string]any{
		manifest("charlie", "charlie", "1.0.0", "C.", nil, nil),
		manifest("Alpha", "alpha", "2.0.0", "A.", nil, nil),
		manifest("bravo", "bravo", "3.0.0", "B.", nil, nil),
	}
}

func escapes() []map[string]any {
	return []map[string]any{
		manifest("<script>alert(1)</script>", "xss", "1.0.0",
			"It's & \"quoted\" <b>", nil, nil),
	}
}

// curatedListing is the sidecar the recorder parsed, so the curated rendering
// asserted below is the same document's.
func curatedListing(t *testing.T) *listing.Listing {
	t.Helper()
	sidecar := `{
	  "format_version": 1,
	  "slug": "home",
	  "categories": [{
	    "name": "Frameworks",
	    "projects": [
	      {"slug": "alpha", "blurb": "Does the alpha thing.",
	       "url": "", "name": "", "repo": ""},
	      {"slug": "zebra", "blurb": "Does the zebra thing.",
	       "url": "", "name": "", "repo": ""}
	    ]
	  }]
	}`
	parsed, err := listing.ParseSidecar(sidecar, "home-listing.json")
	if err != nil {
		t.Fatalf("parsing the curated listing: %v", err)
	}
	return &parsed
}

// -- EscapeHTML ---------------------------------------------------------------

// TestEscapeHTML pins the apostrophe spelling. Go's own html.EscapeString
// writes "&#39;" and util.EscapeHTML leaves the character alone; the Python
// this package ports wrote "&#x27;", and a project blurb carrying an
// apostrophe is the ordinary case.
func TestEscapeHTML(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a & b", "a &amp; b"},
		{"<b>", "&lt;b&gt;"},
		{`"q"`, "&quot;q&quot;"},
		{"it's", "it&#x27;s"},
		{`&<>"'`, "&amp;&lt;&gt;&quot;&#x27;"},
	}
	for _, tc := range cases {
		if got := EscapeHTML(tc.in); got != tc.want {
			t.Errorf("EscapeHTML(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// -- the address authority ----------------------------------------------------

func TestPagePathToURLSegment(t *testing.T) {
	t.Parallel()
	cases := []struct{ path, want string }{
		{"index.md", ""},
		{"guide.md", "guide/"},
		{"api/index.md", "api/"},
		{"api/reference.md", "api/reference/"},
		{"deep/nested/index.md", "deep/nested/"},
		// No .md extension: nothing to strip, everything else the same.
		{"guide", "guide/"},
		{"index", ""},
		{"api/index", "api/"},
		// A name merely containing "index" is not special-cased.
		{"reindex.md", "reindex/"},
		{"index-page.md", "index-page/"},
	}
	for _, tc := range cases {
		if got := pagePathToURLSegment(tc.path); got != tc.want {
			t.Errorf("pagePathToURLSegment(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestPageTargetRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct{ slug, path, target, output string }{
		{"alpha", "api/reference.md", "alpha/api/reference/", "alpha/api/reference/index.html"},
		{"alpha", "index.md", "alpha/", "alpha/index.html"},
		{"alpha", "guide.md", "alpha/guide/", "alpha/guide/index.html"},
	}
	for _, tc := range cases {
		target := PageTarget(tc.slug, tc.path, false)
		if target != tc.target {
			t.Errorf("PageTarget(%q, %q) = %q, want %q", tc.slug, tc.path, target, tc.target)
		}
		if got := TargetOutputPath(target); got != tc.output {
			t.Errorf("TargetOutputPath(%q) = %q, want %q", target, got, tc.output)
		}
		if got := OutputPathTarget(tc.output); got != target {
			t.Errorf("OutputPathTarget(%q) = %q, want %q", tc.output, got, target)
		}
	}
}

// TestHomePageTargetDropsTheProjectSegment pins the home project's addresses:
// its content root is the site root.
func TestHomePageTargetDropsTheProjectSegment(t *testing.T) {
	t.Parallel()
	if got := PageTarget("home", "index.md", true); got != "" {
		t.Errorf("home index target = %q, want %q", got, "")
	}
	if got := PageTarget("home", "cv.md", true); got != "cv/" {
		t.Errorf("home cv target = %q, want %q", got, "cv/")
	}
	if got := TargetOutputPath(""); got != "index.html" {
		t.Errorf("TargetOutputPath(\"\") = %q, want %q", got, "index.html")
	}
	if got := OutputPathTarget("index.html"); got != "" {
		t.Errorf("OutputPathTarget(%q) = %q, want %q", "index.html", got, "")
	}
}

func TestPostTargetRoundTrip(t *testing.T) {
	t.Parallel()
	target := PostTarget("hello")
	if target != "blog/hello" {
		t.Fatalf("PostTarget = %q, want %q", target, "blog/hello")
	}
	if got := TargetOutputPath(target); got != "blog/hello/index.html" {
		t.Errorf("TargetOutputPath = %q, want %q", got, "blog/hello/index.html")
	}
	if got := OutputPathTarget("blog/hello/index.html"); got != target {
		t.Errorf("OutputPathTarget = %q, want %q", got, target)
	}
	// Two projects address the same post identically: the blog is the site's.
	if strings.HasPrefix(target, "alpha/") {
		t.Errorf("a post address carries a project segment: %q", target)
	}
}

// TestTheBlogIndexIsAddressedAsAPage keeps the listing's trailing slash: only
// a two-segment blog path is a post.
func TestTheBlogIndexIsAddressedAsAPage(t *testing.T) {
	t.Parallel()
	if got := OutputPathTarget("blog/index.html"); got != "blog/" {
		t.Errorf("OutputPathTarget(blog/index.html) = %q, want %q", got, "blog/")
	}
}

// TestOutputPathTargetAcceptsWindowsSeparators covers the backslash form a
// walk on Windows would hand it.
func TestOutputPathTargetAcceptsWindowsSeparators(t *testing.T) {
	t.Parallel()
	if got := OutputPathTarget(`alpha\guide\index.html`); got != "alpha/guide/" {
		t.Errorf("OutputPathTarget = %q, want %q", got, "alpha/guide/")
	}
}

// -- WrapSharedPage -----------------------------------------------------------

func TestWrapSharedPage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                                         string
		title, body, canonical, cssURL, searchPrefix string
		file                                         string
	}{
		{
			name:  "pagefind at the root",
			title: "Projects", body: `<section class="content"><h1>Welcome</h1></section>`,
			cssURL: "_chrome/minimal-abcdef012345.css", searchPrefix: "",
			file: "wrap_pagefind_root.html",
		},
		{
			name:  "pagefind one level in, with a canonical",
			title: "Blog", body: "<p>x</p>", canonical: canonicalBase + "/blog/",
			cssURL: "../_chrome/minimal-abcdef012345.css", searchPrefix: "../",
			file: "wrap_pagefind_nested.html",
		},
		{
			name:  "every interpolated string is escaped",
			title: `It's <b>&</b> "q"`, body: "<p>x</p>",
			canonical: `https://x/"><script>`, cssURL: `_chrome/x'".css`,
			searchPrefix: "", file: "wrap_escapes.html",
		},
		{
			name:  "a framework stylesheet selects the palette",
			title: "Framework", body: "<p>x</p>",
			cssURL:       "../_chrome/tinymoon-abcdef012345/css/style.css",
			searchPrefix: "../", file: "wrap_framework.html",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := WrapSharedPage(SharedPage{
				Title: tc.title, BodyHTML: tc.body,
				CanonicalURL: tc.canonical, CSSURL: tc.cssURL,
				SearchPrefix: tc.searchPrefix,
			})
			if err != nil {
				t.Fatalf("WrapSharedPage: %v", err)
			}
			if want := reference(t, tc.file); got != want {
				t.Errorf("WrapSharedPage does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestWrapSharedPageRefusesAnEmptyStylesheet pins the refusal: the parameter
// was optional and unused, and every shared page shipped bare.
func TestWrapSharedPageRefusesAnEmptyStylesheet(t *testing.T) {
	t.Parallel()
	_, err := WrapSharedPage(SharedPage{Title: "Plain", BodyHTML: "<p>text</p>"})
	if err == nil {
		t.Fatal("WrapSharedPage accepted an empty css_url")
	}
	if !strings.Contains(err.Error(), "css_url") {
		t.Errorf("refusal does not name css_url: %v", err)
	}
}

// TestWrapSharedPageLinksExactlyTwoStylesheets: its own chrome, and the
// Pagefind UI's.
func TestWrapSharedPageLinksExactlyTwoStylesheets(t *testing.T) {
	t.Parallel()
	got, err := WrapSharedPage(SharedPage{
		Title: "Plain", BodyHTML: "<p>text</p>", CSSURL: "_chrome/main.css",
	})
	if err != nil {
		t.Fatalf("WrapSharedPage: %v", err)
	}
	var links []string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "<link") {
			links = append(links, line)
		}
	}
	want := []string{
		`    <link rel="stylesheet" href="_chrome/main.css">`,
		`<link href="pagefind/pagefind-ui.css" rel="stylesheet">`,
	}
	if len(links) != len(want) {
		t.Fatalf("links = %#v, want %#v", links, want)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("link %d = %q, want %q", i, links[i], want[i])
		}
	}
}

// TestWrapSharedPageCarriesTheSearchDialog: a shared page answers Cmd/Ctrl+K
// like every documentation page.
func TestWrapSharedPageCarriesTheSearchDialog(t *testing.T) {
	t.Parallel()
	got, err := WrapSharedPage(SharedPage{
		Title: "Plain", BodyHTML: "<p>text</p>",
		CSSURL: "../_chrome/x.css", SearchPrefix: "../",
	})
	if err != nil {
		t.Fatalf("WrapSharedPage: %v", err)
	}
	for _, want := range []string{
		`id="pagefind-container"`,
		`<script src="../pagefind/pagefind-ui.js"></script>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the wrapped page does not carry %q", want)
		}
	}
	if strings.Contains(got, "bundlePath") {
		t.Error("the wrapped page carries a bundlePath the build does not write")
	}
}

// TestWrapSharedPageOmitsTheCanonicalWhenEmpty: the 404 page's state.
func TestWrapSharedPageOmitsTheCanonicalWhenEmpty(t *testing.T) {
	t.Parallel()
	got, err := WrapSharedPage(SharedPage{
		Title: "Blog", BodyHTML: "<p>x</p>",
		CSSURL: "../_chrome/x.css", SearchPrefix: "../",
	})
	if err != nil {
		t.Fatalf("WrapSharedPage: %v", err)
	}
	if strings.Contains(got, `rel="canonical"`) {
		t.Error("a page given no canonical URL declares one")
	}
}

// TestWrapSharedPageEscapesTheCanonicalURL keeps a hostile canonical out of
// the head's markup.
func TestWrapSharedPageEscapesTheCanonicalURL(t *testing.T) {
	t.Parallel()
	got, err := WrapSharedPage(SharedPage{
		Title: "Blog", BodyHTML: "<p>x</p>",
		CanonicalURL: `https://x/"><script>`,
		CSSURL:       "../_chrome/x.css", SearchPrefix: "../",
	})
	if err != nil {
		t.Fatalf("WrapSharedPage: %v", err)
	}
	head := strings.SplitN(got, "<body>", 2)[0]
	if strings.Contains(head, "<script>") {
		t.Errorf("the canonical URL was not escaped:\n%s", head)
	}
}

// -- GenerateHomepage ---------------------------------------------------------

func TestGenerateHomepage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		manifests []map[string]any
		siteHop   string
		homeSlug  string
		curated   bool
		file      string
	}{
		{"name ordered", roster(), "../", "", false, "homepage_name_ordered.html"},
		{"at the site root", mixedCase(), "", "", false, "homepage_root_hop.html"},
		{"escaped", escapes(), "../", "", false, "homepage_escapes.html"},
		{"empty roster", nil, "../", "", false, "homepage_empty.html"},
		{"curated, home excluded", roster(), "../", "home", true, "homepage_home_excluded.html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var curated *listing.Listing
			if tc.curated {
				curated = curatedListing(t)
			}
			got, err := GenerateHomepage(tc.manifests, tc.siteHop, tc.homeSlug, curated)
			if err != nil {
				t.Fatalf("GenerateHomepage: %v", err)
			}
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateHomepage does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestGenerateHomepageRefusesADeclaredHomeWithNoListing: the uncurated
// rendering answers one state, and a declared home is not it.
func TestGenerateHomepageRefusesADeclaredHomeWithNoListing(t *testing.T) {
	t.Parallel()
	_, err := GenerateHomepage(
		[]map[string]any{manifest("Alpha", "alpha", "1.0.0", "", nil, nil)},
		"../", "home", nil,
	)
	if err == nil {
		t.Fatal("GenerateHomepage rendered a declared home with no listing")
	}
	if !strings.Contains(err.Error(), "curated listing") {
		t.Errorf("refusal does not name the curated listing: %v", err)
	}
}

// TestAnAssemblyWithNoHomeProjectNeedsNoListing: the one state the
// name-ordered rendering answers.
func TestAnAssemblyWithNoHomeProjectNeedsNoListing(t *testing.T) {
	t.Parallel()
	got, err := GenerateHomepage(
		[]map[string]any{manifest("Alpha", "alpha", "1.0.0", "", nil, nil)},
		"../", "", nil,
	)
	if err != nil {
		t.Fatalf("GenerateHomepage: %v", err)
	}
	if !strings.Contains(got, "Alpha") {
		t.Errorf("the listing omits the only project it has:\n%s", got)
	}
}

// TestHomepageNeverWritesAnAbsoluteOrRootedLink -- see LINK001.
func TestHomepageNeverWritesAnAbsoluteOrRootedLink(t *testing.T) {
	t.Parallel()
	for _, hop := range []string{"", "../", "../../"} {
		got, err := GenerateHomepage(roster(), hop, "", nil)
		if err != nil {
			t.Fatalf("GenerateHomepage: %v", err)
		}
		if !strings.Contains(got, `href="`+hop+`alpha/"`) {
			t.Errorf("hop %q: the card does not address alpha through it:\n%s", hop, got)
		}
		if strings.Contains(got, `href="/`) || strings.Contains(got, `href="http`) {
			t.Errorf("hop %q: the listing writes an address no mount resolves:\n%s", hop, got)
		}
	}
}

// -- GenerateBlogIndex --------------------------------------------------------

func TestGenerateBlogIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		manifests []map[string]any
		siteHop   string
		file      string
	}{
		{"one level in, newest first", roster(), "../", "blog_index.html"},
		{"at the site root", roster(), "", "blog_index_root.html"},
		{"no posts at all", mixedCase(), "../", "blog_index_empty.html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateBlogIndex(tc.manifests, tc.siteHop)
			if err != nil {
				t.Fatalf("GenerateBlogIndex: %v", err)
			}
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateBlogIndex does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestBlogIndexNeverWritesAnAbsoluteOrRootedLink -- see LINK001. An
// origin-absolute address names nothing when the site is served from a
// subdirectory, and an absolute URL resolves on the deployed host alone.
func TestBlogIndexNeverWritesAnAbsoluteOrRootedLink(t *testing.T) {
	t.Parallel()
	manifests := []map[string]any{
		manifest("rlsbl", "rlsbl", "1.0.0", "", nil,
			[]any{postEntry("first", "Post", "2024-01-01")}),
	}
	for _, hop := range []string{"", "../", "../../"} {
		got, err := GenerateBlogIndex(manifests, hop)
		if err != nil {
			t.Fatalf("GenerateBlogIndex: %v", err)
		}
		if !strings.Contains(got, `href="`+hop+`blog/first/"`) {
			t.Errorf("hop %q: the entry does not address the post through it:\n%s", hop, got)
		}
		if strings.Contains(got, `href="/`) || strings.Contains(got, `href="http`) {
			t.Errorf("hop %q: the index writes an address no mount resolves:\n%s", hop, got)
		}
	}
}

// -- GenerateNavJSON ----------------------------------------------------------

func TestGenerateNavJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		manifests []map[string]any
		blogPath  string
		homeSlug  string
		file      string
	}{
		{"every project", roster(), DefaultBlogPath, "", "nav.json"},
		{"home excluded, custom blog path", roster(), "/articles/", "home", "nav_home_excluded.json"},
		{"empty roster", nil, DefaultBlogPath, "", "nav_empty.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GenerateNavJSON(tc.manifests, tc.blogPath, tc.homeSlug)
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateNavJSON does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
			var decoded struct {
				Projects []struct {
					Name    string `json:"name"`
					Slug    string `json:"slug"`
					Version string `json:"version"`
				} `json:"projects"`
				Blog string `json:"blog"`
			}
			if err := json.Unmarshal([]byte(got), &decoded); err != nil {
				t.Fatalf("the navigation document is not valid JSON: %v", err)
			}
			if decoded.Blog != tc.blogPath {
				t.Errorf("blog = %q, want %q", decoded.Blog, tc.blogPath)
			}
		})
	}
}

// TestNavJSONIsOrderedByLowercasedName pins the ordering the picker reads.
func TestNavJSONIsOrderedByLowercasedName(t *testing.T) {
	t.Parallel()
	got := GenerateNavJSON(mixedCase(), DefaultBlogPath, "")
	var decoded struct {
		Projects []struct{ Name string } `json:"projects"`
	}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("the navigation document is not valid JSON: %v", err)
	}
	want := []string{"Alpha", "bravo", "charlie"}
	if len(decoded.Projects) != len(want) {
		t.Fatalf("projects = %#v, want %#v", decoded.Projects, want)
	}
	for i := range want {
		if decoded.Projects[i].Name != want[i] {
			t.Errorf("project %d = %q, want %q", i, decoded.Projects[i].Name, want[i])
		}
	}
}

// -- GenerateUnifiedFeed ------------------------------------------------------

func TestGenerateUnifiedFeed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		manifests []map[string]any
		title     string
		file      string
	}{
		{"default title", roster(), "", "feed.xml"},
		{"declared title, escaped", roster(), "My <Custom> Feed", "feed_titled.xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateUnifiedFeed(tc.manifests, canonicalBase, tc.title)
			if err != nil {
				t.Fatalf("GenerateUnifiedFeed: %v", err)
			}
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateUnifiedFeed does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestFeedWithNoPostsStillCarriesAnUpdated: an Atom feed with no <updated> is
// not a feed, so a site with no posts dates itself today.
func TestFeedWithNoPostsStillCarriesAnUpdated(t *testing.T) {
	t.Parallel()
	got, err := GenerateUnifiedFeed(mixedCase(), canonicalBase, "")
	if err != nil {
		t.Fatalf("GenerateUnifiedFeed: %v", err)
	}
	for _, want := range []string{"<feed ", "</feed>", "<updated>", "</updated>"} {
		if !strings.Contains(got, want) {
			t.Errorf("the feed does not carry %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<entry>") {
		t.Errorf("a site with no posts produced an entry:\n%s", got)
	}
}

// TestFeedEntriesAreInterleavedByDateAcrossProjects: the feed is the site's,
// not a concatenation of per-project ones.
func TestFeedEntriesAreInterleavedByDateAcrossProjects(t *testing.T) {
	t.Parallel()
	manifests := []map[string]any{
		manifest("Alpha", "alpha", "1.0.0", "", nil, []any{
			postEntry("a-early", "A-Early", "2024-01-10"),
			postEntry("a-late", "A-Late", "2024-07-20"),
		}),
		manifest("Beta", "beta", "1.0.0", "", nil, []any{
			postEntry("b-mid", "B-Mid", "2024-04-15"),
		}),
	}
	got, err := GenerateUnifiedFeed(manifests, canonicalBase, "")
	if err != nil {
		t.Fatalf("GenerateUnifiedFeed: %v", err)
	}
	late := strings.Index(got, "A-Late")
	mid := strings.Index(got, "B-Mid")
	early := strings.Index(got, "A-Early")
	if !(late < mid && mid < early) {
		t.Errorf("entries are not newest first (late=%d mid=%d early=%d):\n%s",
			late, mid, early, got)
	}
}

// -- GenerateSitemap ----------------------------------------------------------

func TestGenerateSitemap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		homeSlug string
		file     string
	}{
		{"every project under its slug", "", "sitemap.xml"},
		{"the home project addressed from the site root", "home", "sitemap_home.xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateSitemap(roster(), canonicalBase, tc.homeSlug)
			if err != nil {
				t.Fatalf("GenerateSitemap: %v", err)
			}
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateSitemap does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestSitemapRefusesARelativeBase: a root-relative base produces entries every
// crawler discards.
func TestSitemapRefusesARelativeBase(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"", "/docs", "docs.example.com", "ftp://x"} {
		if _, err := GenerateSitemap(roster(), base, ""); err == nil {
			t.Errorf("GenerateSitemap accepted the base %q", base)
		} else if !strings.Contains(err.Error(), "absolute base URL") {
			t.Errorf("base %q: refusal does not name the absolute base URL: %v", base, err)
		}
	}
}

// -- GenerateRobotsTxt --------------------------------------------------------

func TestGenerateRobotsTxt(t *testing.T) {
	t.Parallel()
	cases := []struct{ base, file string }{
		{canonicalBase, "robots.txt"},
		{canonicalBase + "/", "robots_trailing_slash.txt"},
	}
	for _, tc := range cases {
		got := GenerateRobotsTxt(tc.base)
		if want := reference(t, tc.file); got != want {
			t.Errorf("GenerateRobotsTxt(%q) does not match %s\n--- got ---\n%s\n--- want ---\n%s",
				tc.base, tc.file, got, want)
		}
	}
}

// TestRobotsNamesTheRootSitemapAbsolutely and every crawler the policy names.
func TestRobotsNamesTheRootSitemapAbsolutely(t *testing.T) {
	t.Parallel()
	got := GenerateRobotsTxt(canonicalBase)
	if want := "Sitemap: " + canonicalBase + "/" + SitemapPath; !strings.Contains(got, want) {
		t.Errorf("robots.txt does not carry %q:\n%s", want, got)
	}
	for _, agent := range robots.Agents {
		if want := "User-agent: " + agent + "\nAllow: /"; !strings.Contains(got, want) {
			t.Errorf("robots.txt does not allow %q:\n%s", agent, got)
		}
	}
}

// TestRobotsReferencesAgreeWithTheDeclaredPolicy keeps the recorded files from
// drifting away from the policy silently.
//
// The two testdata files are recordings: the byte-for-byte test above compares
// the generator's output against them, and a crawler added to the policy makes
// that comparison fail, which is the point. What it cannot say is which side
// moved. This one reads the agent lines back out of each recording and names
// the declaration they have to agree with, so re-recording a file with an
// agent the policy never gained fails here instead of passing quietly.
func TestRobotsReferencesAgreeWithTheDeclaredPolicy(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"robots.txt", "robots_trailing_slash.txt"} {
		var recorded []string
		for _, line := range strings.Split(reference(t, file), "\n") {
			if agent, found := strings.CutPrefix(line, "User-agent: "); found {
				recorded = append(recorded, strings.TrimSpace(agent))
			}
		}
		if len(recorded) != len(robots.Agents) {
			t.Errorf("%s names %d agents, the policy declares %d: %q vs %q",
				file, len(recorded), len(robots.Agents), recorded, robots.Agents)
			continue
		}
		for i, agent := range robots.Agents {
			if recorded[i] != agent {
				t.Errorf("%s names %q where the policy declares %q",
					file, recorded[i], agent)
			}
		}
	}
}

// -- GenerateLLMSTxt ----------------------------------------------------------

func TestGenerateLLMSTxt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		manifests []map[string]any
		base      string
		homeSlug  string
		file      string
	}{
		{"every project", roster(), canonicalBase, "", "llms.txt"},
		{"home excluded, trailing slash trimmed", roster(), canonicalBase + "/", "home", "llms_home.txt"},
		{"nothing published yet", nil, canonicalBase, "", "llms_empty.txt"},
		{
			"a manifest with no description", []map[string]any{
				manifest("Alpha", "alpha", "1.0.0", "Does the alpha thing.", nil, nil),
				manifest("Mute", "mute", "1.0.0", "", nil, nil),
				manifest("Blank", "blank", "1.0.0", "   \n  ", nil, nil),
			},
			canonicalBase, "", "llms_no_description.txt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GenerateLLMSTxt(tc.manifests, tc.base, tc.homeSlug)
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateLLMSTxt does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestLLMSTxtComposesByReference: an inlined page list would be a second,
// staler rendering of a document the project already publishes.
func TestLLMSTxtComposesByReference(t *testing.T) {
	t.Parallel()
	got := GenerateLLMSTxt(roster(), canonicalBase, "home")
	for _, want := range []string{
		"[Alpha](" + canonicalBase + "/alpha/llms.txt)",
		"[Zebra](" + canonicalBase + "/zebra/llms.txt)",
		"[Blog](" + canonicalBase + "/blog/)",
		"Does the alpha thing.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("llms.txt does not carry %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"/home/llms.txt",
		canonicalBase + "/alpha/guide/",
		"Guide",
		// Only the first line of a description is the summary.
		"Second line.",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("llms.txt carries %q, which it composes by reference:\n%s", unwanted, got)
		}
	}
}

// TestLLMSTxtMarksAMissingDescription: an entry that silently stops after the
// link reads like a project with nothing to say about itself, and the gap is
// invisible to whoever could fill it. The marker makes it a thing somebody
// sees.
func TestLLMSTxtMarksAMissingDescription(t *testing.T) {
	t.Parallel()
	got := GenerateLLMSTxt([]map[string]any{
		manifest("Mute", "mute", "1.0.0", "", nil, nil),
	}, canonicalBase, "")
	want := "- [Mute](" + canonicalBase + "/mute/" + LLMSPath + "): " +
		MissingDescriptionMarker
	if !strings.Contains(got, want) {
		t.Errorf("llms.txt does not carry %q:\n%s", want, got)
	}
}

// -- MergeProjectPosts --------------------------------------------------------

func TestMergeProjectPostsCollectsEveryProject(t *testing.T) {
	t.Parallel()
	merged, err := MergeProjectPosts([]map[string]any{
		manifest("Alpha", "alpha", "1.0.0", "", nil, []any{postEntry("one", "Post", "2024-06-01")}),
		manifest("Beta", "beta", "2.0.0", "", nil, []any{postEntry("two", "Post", "2024-06-01")}),
	})
	if err != nil {
		t.Fatalf("MergeProjectPosts: %v", err)
	}
	want := []MergedPost{
		{Date: "2024-06-01", Title: "Post", Slug: "one", ProjectName: "Alpha", ManifestSlug: "alpha"},
		{Date: "2024-06-01", Title: "Post", Slug: "two", ProjectName: "Beta", ManifestSlug: "beta"},
	}
	if len(merged) != len(want) {
		t.Fatalf("merged = %#v, want %#v", merged, want)
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Errorf("post %d = %#v, want %#v", i, merged[i], want[i])
		}
	}
}

// TestMergeProjectPostsRefusesASlugCollision over every surface that merges.
// Posts share one slug namespace across the whole assembled site, and the
// manifests arrive from separate deploys, so nothing else ever sees them
// together.
func TestMergeProjectPostsRefusesASlugCollision(t *testing.T) {
	t.Parallel()
	collide := func() []map[string]any {
		return []map[string]any{
			manifest("Alpha", "alpha", "1.0.0", "", nil, []any{postEntry("hello", "Post", "2024-06-01")}),
			manifest("Beta", "beta", "2.0.0", "", nil, []any{postEntry("hello", "Post", "2024-06-01")}),
		}
	}
	surfaces := map[string]func([]map[string]any) error{
		"MergeProjectPosts": func(m []map[string]any) error {
			_, err := MergeProjectPosts(m)
			return err
		},
		"GenerateBlogIndex": func(m []map[string]any) error {
			_, err := GenerateBlogIndex(m, "../")
			return err
		},
		"GenerateUnifiedFeed": func(m []map[string]any) error {
			_, err := GenerateUnifiedFeed(m, canonicalBase, "")
			return err
		},
		"GenerateSitemap": func(m []map[string]any) error {
			_, err := GenerateSitemap(m, canonicalBase, "")
			return err
		},
	}
	for name, call := range surfaces {
		err := call(collide())
		if err == nil {
			t.Errorf("%s accepted two projects claiming one post address", name)
			continue
		}
		for _, want := range []string{"hello", "alpha", "beta"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: refusal does not name %q: %v", name, want, err)
			}
		}
	}
}

// TestOneProjectRepeatingASlugIsStillACollision: the namespace is the site's,
// so a repeat inside one manifest is the same overwrite.
func TestOneProjectRepeatingASlugIsStillACollision(t *testing.T) {
	t.Parallel()
	_, err := MergeProjectPosts([]map[string]any{
		manifest("Alpha", "alpha", "1.0.0", "", nil, []any{
			postEntry("hello", "Post", "2024-06-01"),
			postEntry("hello", "Post", "2024-06-02"),
		}),
	})
	if err == nil {
		t.Fatal("MergeProjectPosts accepted a repeated slug inside one manifest")
	}
	if !strings.Contains(err.Error(), "hello") {
		t.Errorf("refusal does not name the slug: %v", err)
	}
}

// -- ValidateCrossProjectLinks ------------------------------------------------

func TestValidateCrossProjectLinks(t *testing.T) {
	t.Parallel()
	manifests := []map[string]any{
		manifest("MyProj", "myproj", "1.0.0", "",
			[]any{pageEntry("guide.md", "Guide"), pageEntry("api/reference.md", "API"),
				pageEntry("index.md", "Home"), pageEntry("api/index.md", "API Index")},
			[]any{map[string]any{
				"path": "posts/update.md", "slug": "big-update",
				"title": "Big Update", "date": "2024-06-01",
			}}),
	}
	cases := []struct {
		name     string
		registry map[string][]string
		want     []string
	}{
		{
			name: "every target resolves, in both spellings",
			registry: map[string][]string{"index.md": {
				"guide.md", "blog/big-update", "posts/update.md",
				"myproj/guide/", "myproj/api/reference/", "myproj/",
				"myproj/api/",
			}},
			want: nil,
		},
		{
			name:     "an empty registry finds nothing",
			registry: map[string][]string{},
			want:     nil,
		},
		{
			name:     "a broken target is one finding naming both ends",
			registry: map[string][]string{"index.md": {"guide.md", "nonexistent.md"}},
			want: []string{
				"Broken link in 'index.md': target 'nonexistent.md' not found in any project",
			},
		},
		{
			name: "findings are ordered by source page",
			registry: map[string][]string{
				"z.md": {"gone.md"},
				"a.md": {"missing.md"},
			},
			want: []string{
				"Broken link in 'a.md': target 'missing.md' not found in any project",
				"Broken link in 'z.md': target 'gone.md' not found in any project",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateCrossProjectLinks(manifests, tc.registry)
			if len(got) != len(tc.want) {
				t.Fatalf("findings = %#v, want %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("finding %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// -- GenerateNotFoundPage -----------------------------------------------------

func TestGenerateNotFoundPage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cssURL, siteHop, file string
	}{
		{"at the site root", "_chrome/minimal-abcdef012345.css", "", "not_found.html"},
		{"one level in", "../_chrome/minimal-abcdef012345.css", "../", "not_found_hopped.html"},
		{"a hostile hop is escaped", "_chrome/x.css", `"><script>`, "not_found_escaped_hop.html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateNotFoundPage(tc.cssURL, tc.siteHop)
			if err != nil {
				t.Fatalf("GenerateNotFoundPage: %v", err)
			}
			if want := reference(t, tc.file); got != want {
				t.Errorf("GenerateNotFoundPage does not match %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.file, got, want)
			}
		})
	}
}

// TestTheRootNotFoundPageIsARealNotFoundPage: a soft 404 -- the home page
// under an unknown address -- is the defect this asserts against.
func TestTheRootNotFoundPageIsARealNotFoundPage(t *testing.T) {
	t.Parallel()
	notFound, err := GenerateNotFoundPage("_chrome/x.css", "")
	if err != nil {
		t.Fatalf("GenerateNotFoundPage: %v", err)
	}
	if !strings.HasPrefix(notFound, "<!DOCTYPE html>") {
		t.Error("the 404 page is a fragment rather than a whole document")
	}
	for _, want := range []string{
		"<title>Page not found</title>",
		"<h1>Page not found</h1>",
		`href="./"`,
		`href="projects/"`,
		`href="blog/"`,
	} {
		if !strings.Contains(notFound, want) {
			t.Errorf("the 404 page does not carry %q", want)
		}
	}
	// An error page has no address of its own to call canonical, and names
	// no host: it answers unmatched addresses on whatever host serves the
	// tree.
	if strings.Contains(notFound, `rel="canonical"`) {
		t.Error("the 404 page declares a canonical")
	}
	if strings.Contains(notFound, canonicalBase) {
		t.Error("the 404 page names a deployed host")
	}
	if strings.Contains(notFound, "<h1>Projects</h1>") {
		t.Error("the 404 page renders the project listing")
	}

	projects, err := GenerateHomepage(roster(), "../", "", nil)
	if err != nil {
		t.Fatalf("GenerateHomepage: %v", err)
	}
	if strings.Contains(projects, "Page not found") {
		t.Error("the project listing renders the not-found body")
	}
}

// TestTheRootNotFoundPageEscapesItsHop keeps a hostile hop out of the three
// links it writes.
func TestTheRootNotFoundPageEscapesItsHop(t *testing.T) {
	t.Parallel()
	got, err := GenerateNotFoundPage("_chrome/x.css", `"><script>`)
	if err != nil {
		t.Fatalf("GenerateNotFoundPage: %v", err)
	}
	for _, want := range []string{
		`href="&quot;&gt;&lt;script&gt;projects/"`,
		`href="&quot;&gt;&lt;script&gt;blog/"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the 404 page does not carry %q:\n%s", want, got)
		}
	}
}
