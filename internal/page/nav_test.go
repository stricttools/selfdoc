package page

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/util"
)

// navToDocument renders a navigation tree in the shape the Python built it:
// one object per entry, carrying only the keys that entry really had.
//
// The recorded reference is that structure encoded with sorted keys and an
// indent, so the comparison is over the tree's content rather than over a
// Go type's field names.
func navToDocument(items []NavItem) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{}
		if item.IsGroup() {
			entry["group"] = item.Group
			entry["slug"] = item.Slug
			entry["items"] = navToDocument(item.Items)
		} else {
			entry["label"] = item.Label
			entry["path"] = item.Path
			entry["md_path"] = item.MdPath
		}
		if item.Unversioned {
			entry["unversioned"] = true
		}
		out = append(out, entry)
	}
	return out
}

func navJSON(t *testing.T, items []NavItem) string {
	t.Helper()
	encoded, err := util.PythonJSONIndent2(navToDocument(items))
	if err != nil {
		t.Fatalf("encoding nav: %v", err)
	}
	return string(encoded)
}

// TestBuildNavMatchesReference asserts the navigation tree over the ordering,
// grouping and unversioned-page shapes the Python was recorded on.
func TestBuildNavMatchesReference(t *testing.T) {
	var out []string

	out = append(out, "## plain\n"+navJSON(t, BuildNav(
		[]SourceFile{src("index.md", ""), src("guide.md", "")},
		map[string]util.Frontmatter{
			"index.md": {"title": "Home"},
			"guide.md": {"title": "User Guide"},
		}, nil, nil)))

	out = append(out, "## ordered\n"+navJSON(t, BuildNav(
		[]SourceFile{
			src("index.md", ""), src("b.md", ""), src("a.md", ""), src("c.md", ""),
		},
		map[string]util.Frontmatter{
			"b.md": {"nav_order": int64(1)}, "c.md": {"nav_order": int64(0)},
		}, nil, nil)))

	out = append(out, "## groups\n"+navJSON(t, BuildNav(
		[]SourceFile{
			src("index.md", ""),
			src("guide.md", ""),
			src("api/endpoints.md", ""),
			src("api/auth.md", ""),
			src("user_guide/quickstart.md", ""),
			src("zed-things/x.md", ""),
		},
		map[string]util.Frontmatter{
			"api/auth.md": {"nav_order": int64(1)},
			"api/endpoints.md": {
				"nav_order": int64(2), "nav_group": "API Reference",
			},
		}, nil, nil)))

	out = append(out, "## unversioned\n"+navJSON(t, BuildNav(
		[]SourceFile{src("index.md", ""), src("guide.md", "")},
		map[string]util.Frontmatter{
			"index.md": {"title": "Home"},
			"guide.md": {"title": "User Guide"},
		},
		[]SourceFile{
			src("about.md", ""), src("terms.md", ""),
			src("p1.md", ""), src("p2.md", ""),
		},
		map[string]util.Frontmatter{
			"about.md": {"title": "About Us", "nav_order": int64(2)},
			"terms.md": {"title": "Terms of Service", "nav_order": int64(1)},
			"p1.md":    {"title": "First", "type": "post", "date": "2026-01-01"},
			"p2.md":    {"title": "Second", "type": "post", "date": "2026-03-01"},
		})))

	out = append(out, "## unversioned only posts\n"+navJSON(t, BuildNav(
		[]SourceFile{src("index.md", "")}, nil,
		[]SourceFile{src("about.md", "")}, nil)))

	out = append(out, "## empty unversioned\n"+navJSON(t, BuildNav(
		[]SourceFile{src("index.md", ""), src("guide.md", "")}, nil,
		[]SourceFile{}, nil)))

	out = append(out, "## flatten\n"+navJSON(t, FlattenNav(BuildNav(
		[]SourceFile{
			src("index.md", ""), src("api/a.md", ""),
			src("api/b.md", ""), src("z.md", ""),
		}, nil, nil, nil))))

	assertEqual(t, "build_nav", strings.Join(out, "\n"),
		reference(t, "build_nav"))
}

// TestRenderNavMatchesReference asserts the sidebar markup, including which
// group auto-expands and which of the three hops each item takes.
func TestRenderNavMatchesReference(t *testing.T) {
	nav := BuildNav(
		[]SourceFile{
			src("index.md", ""),
			src("guide.md", ""),
			src("api/auth.md", ""),
			src("api/endpoints.md", ""),
		},
		map[string]util.Frontmatter{"guide.md": {"title": "The <Guide>"}},
		[]SourceFile{src("about.md", ""), src("blog/hello.md", "")},
		map[string]util.Frontmatter{
			"blog/hello.md": {
				"title": "Hello", "type": "post", "date": "2026-01-01",
			},
		},
	)
	var out []string
	for _, current := range []string{
		"index.html", "api/auth/index.html", "blog/hello/index.html", "",
	} {
		out = append(out, "## current="+pyRepr(current)+"\n"+
			RenderNav(nav, "../", current, "../../", "../../../"))
	}
	assertEqual(t, "render_nav", strings.Join(out, "\n"),
		reference(t, "render_nav"))
}

// TestTOCAndBreadcrumbsMatchReference asserts the table of contents, the
// breadcrumb trail, the first-paragraph extraction and the title extraction
// against the Python's output.
func TestTOCAndBreadcrumbsMatchReference(t *testing.T) {
	var out []string

	bodies := []string{
		"<p>No headings.</p>",
		`<h2 id="a"><a class="heading-link" href="#a" aria-label="x">#</a>One</h2>`,
		`<h2 id="a"><a class="heading-link" href="#a" aria-label="x">#</a>One</h2>` + "\n" +
			`<h3 id="b"><a class="heading-link" href="#b" aria-label="x">#</a>Two <code>x</code></h3>` + "\n" +
			`<h2 id="c"><a class="heading-link" href="#c" aria-label="x">#</a>Three &amp; four</h2>`,
		`<h2 id="a">One</h2>` + "\n" + `<h2 id="b">Two</h2>`,
		`<h4 id="a"><a class="heading-link" href="#a">#</a>One</h4>` + "\n" +
			`<h2 id="b"><a class="heading-link" href="#b">#</a>Two</h2>` + "\n" +
			`<h2 id="c"><a class="heading-link" href="#c">#</a>Three</h2>`,
		// A heading with no anchor of its own, standing before two that
		// have one: the pattern this port replaces could walk past the
		// close tag looking for an anchor, so this pins what it really
		// produced.
		`<h2 id="a">plain</h2>` + "\n" +
			`<h3 id="b"><a class="heading-link" href="#b">#</a>Two</h3>` + "\n" +
			`<h3 id="c"><a class="heading-link" href="#c">#</a>Three</h3>`,
		// A heading whose text spans a newline is not a heading the table
		// of contents reads, because the pattern's wildcard never crossed
		// one.
		`<h2 id="a"><a class="heading-link" href="#a">#</a>One` + "\ntwo" + `</h2>` + "\n" +
			`<h2 id="b"><a class="heading-link" href="#b">#</a>Three</h2>` + "\n" +
			`<h2 id="c"><a class="heading-link" href="#c">#</a>Four</h2>`,
		// Markup standing between the opening tag and the anchor.
		`<h2 id="a"><span class="pre">x</span>` +
			`<a class="heading-link" href="#a">#</a>One</h2>` + "\n" +
			`<h2 id="b"><a class="heading-link" href="#b">#</a>Two</h2>`,
		// A repeated heading text, whose ids the anchor authority made
		// unique.
		`<h2 id="setup"><a class="heading-link" href="#setup">#</a>Setup</h2>` + "\n" +
			`<h2 id="setup-1"><a class="heading-link" href="#setup-1">#</a>Setup</h2>`,
	}
	for i, body := range bodies {
		out = append(out, "## toc "+strconv.Itoa(i)+"\n"+buildTOC(body))
	}

	crumbCases := []struct {
		path     string
		title    string
		prefix   string
		existing map[string]bool
		home     string
		// site is the site-level hop; "" here means the Python passed None,
		// which defaults to prefix.
		site       string
		siteStated bool
	}{
		{path: "guide/index.html", title: "Guide", prefix: "",
			existing: map[string]bool{}, home: "index.html"},
		{path: "api/endpoints/index.html", title: "Endpoints", prefix: "../",
			existing: map[string]bool{"api/index.html": true},
			home:     "../index.html"},
		{path: "api/endpoints/index.html", title: "Endpoints", prefix: "../",
			existing: map[string]bool{}, home: "../index.html"},
		{path: "blog/hello/index.html", title: "Hello <post>", prefix: "../",
			existing: map[string]bool{"blog/index.html": true},
			home:     "../index.html", site: "../../", siteStated: true},
	}
	for i, c := range crumbCases {
		site := c.prefix
		if c.siteStated {
			site = c.site
		}
		out = append(out, "## crumbs "+strconv.Itoa(i)+"\n"+buildBreadcrumbs(
			c.path, c.title, c.prefix, c.existing, c.home, site))
	}

	for i, body := range []string{
		"<p>First <b>bold</b> paragraph.</p><p>Second.</p>",
		"no paragraph",
		"<p>Multi\nline\nparagraph.</p>",
	} {
		out = append(out, "## first_paragraph "+strconv.Itoa(i)+"\n"+
			extractFirstParagraph(body))
	}

	for i, c := range []struct{ md, fallback string }{
		{"# Hello World\n\nContent.", "fallback"},
		{"## Only H2\n\nContent.", "fallback"},
		{"# Hello `World`\n\nContent.", "fallback"},
		{"```\n# Not a title\n```\n\n# Real Title\n", "fallback"},
		{"```\n# Not a title\n```\n", "fallback"},
	} {
		out = append(out, "## extract_title "+strconv.Itoa(i)+"\n"+
			ExtractTitle(c.md, c.fallback))
	}

	assertEqual(t, "toc_and_crumbs", strings.Join(out, "\n"),
		reference(t, "toc_and_crumbs"))
}
