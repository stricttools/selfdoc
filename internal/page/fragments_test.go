package page

import (
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/util"
)

// TestSEOVariantsMatchReference renders the head's SEO block across the
// page-type, schema-type, tag and page-path combinations the Python was
// recorded over, and asserts every byte.
func TestSEOVariantsMatchReference(t *testing.T) {
	type variant struct {
		pageType    string
		schemaTypes map[string]string
		pageTags    []string
		pagePath    string
		description string
		lang        string
		// label is the header line the recorder wrote for the variant,
		// spelled as Python's repr does so the two files line up.
		label string
	}
	variants := []variant{
		{pageType: "guide", pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='guide' schema_types=None page_tags=None page_path='guide/index.html' lang='en'"},
		{pageType: "tutorial", pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='tutorial' schema_types=None page_tags=None page_path='guide/index.html' lang='en'"},
		{pageType: "post", pageTags: []string{"python", "testing", "ci"},
			pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='post' schema_types=None page_tags=['python', 'testing', 'ci'] page_path='guide/index.html' lang='en'"},
		{pageType: "post", pageTags: []string{}, pagePath: "guide/index.html",
			description: "A test page",
			lang:        "en",
			label:       "--- page_type='post' schema_types=None page_tags=[] page_path='guide/index.html' lang='en'"},
		{pageType: "changelog", pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='changelog' schema_types=None page_tags=None page_path='guide/index.html' lang='en'"},
		{pageType: "fieldnote", pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='fieldnote' schema_types=None page_tags=None page_path='guide/index.html' lang='en'"},
		{pageType: "", pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type=None schema_types=None page_tags=None page_path='guide/index.html' lang='en'"},
		{pageType: "guide", schemaTypes: map[string]string{"guide": "HowTo"},
			pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='guide' schema_types={'guide': 'HowTo'} page_tags=None page_path='guide/index.html' lang='en'"},
		{pageType: "news", schemaTypes: map[string]string{"news": "BlogPosting"},
			pageTags: []string{"breaking", "update"},
			pagePath: "guide/index.html", description: "A test page",
			lang:  "en",
			label: "--- page_type='news' schema_types={'news': 'BlogPosting'} page_tags=['breaking', 'update'] page_path='guide/index.html' lang='en'"},
		{pageType: "cv", pagePath: "cv/index.html",
			lang:  "en",
			label: "--- page_type='cv' schema_types=None page_tags=None page_path='cv/index.html' lang='en'"},
		{pageType: "guide", schemaTypes: map[string]string{}, pagePath: "index.html",
			lang:  "en",
			label: "--- page_type='guide' schema_types={} page_tags=None page_path='index.html' lang='en'"},
		{pageType: "guide", pagePath: "api/auth/index.html", description: "Nested page",
			lang:  "en",
			label: "--- page_type='guide' schema_types=None page_tags=None page_path='api/auth/index.html' lang='en'"},
		// An unmapped language tag and an unstated one: the Open Graph
		// locale is derived from the tag rather than looked up, and the
		// empty tag reaches inLanguage as written while the locale falls
		// back.
		{pageType: "guide", pagePath: "guide/index.html", description: "A test page",
			lang:  "pt-BR",
			label: "--- page_type='guide' schema_types=None page_tags=None page_path='guide/index.html' lang='pt-BR'"},
		{pageType: "guide", pagePath: "guide/index.html", description: "A test page",
			lang:  "",
			label: "--- page_type='guide' schema_types=None page_tags=None page_path='guide/index.html' lang=''"},
	}

	var blocks []string
	for _, v := range variants {
		seo, security, err := RenderSEOTags(SEOOptions{
			Title:         "A Page",
			BaseURL:       "https://example.com",
			PagePath:      v.pagePath,
			Description:   v.description,
			BodyHTML:      "<p>Body of the page. Second sentence.</p>",
			Author:        testAuthor(),
			ProjectName:   "mypackage",
			Repo:          "https://github.com/u/r",
			DatePublished: "2024-01-01",
			DateModified:  "2026-01-01",
			Lang:          v.lang,
			Breadcrumbs:   "<nav>crumbs</nav>",
			TwitterSite:   "@mypackage",
			DeployTarget:  "github-pages",
			PageType:      v.pageType,
			SchemaTypes:   v.schemaTypes,
			PageTags:      v.pageTags,
			AvailableLocales: []LocaleEntry{
				{Code: "en", Label: "English", Default: true},
				{Code: "pt-BR", Label: "Portuguese"},
			},
			MountLocale: "en",
		})
		if err != nil {
			t.Fatalf("RenderSEOTags(%s): %v", v.label, err)
		}
		blocks = append(blocks, v.label+"\n"+seo+"\n### security\n"+security)
	}
	assertEqual(t, "seo_variants", strings.Join(blocks, "\n"),
		reference(t, "seo_variants"))
}

// TestSEOWithoutBaseURLMatchesReference covers the page built by hand with no
// base URL at all, where the absent base reaches the emitted document.
func TestSEOWithoutBaseURLMatchesReference(t *testing.T) {
	seo, security, err := RenderSEOTags(SEOOptions{
		Title:    "A Page",
		PagePath: "guide/index.html",
		BodyHTML: `<p>Body.</p><pre><code class="language-python">x</code></pre>`,
		Author:   testAuthor(),

		ProjectName: "mypackage",
		Lang:        "pt",
		PageType:    "guide",
	})
	if err != nil {
		t.Fatalf("RenderSEOTags: %v", err)
	}
	assertEqual(t, "seo_no_base_url", seo+"\n### security\n"+security,
		reference(t, "seo_no_base_url"))
}

// TestSearchFragmentsMatchReference asserts every standalone fragment the
// search surface is assembled from.
func TestSearchFragmentsMatchReference(t *testing.T) {
	var out []string
	out = append(out, "## pagefind_head_tags\n"+PagefindHeadTags("../../"))
	out = append(out, "## pagefind_init_script\n"+PagefindInitScript("../"))
	out = append(out, "## pagefind_dialog_html\n"+PagefindDialogHTML())
	for _, pair := range [][2]string{
		{"", "css/style.css"},
		{"../", "../css/style.css"},
		{"../../", "../../_chrome/tinymoon-abc123/css/style.css"},
	} {
		script, err := PaletteSearchScript(pair[0], pair[1])
		if err != nil {
			t.Fatalf("PaletteSearchScript(%q, %q): %v", pair[0], pair[1], err)
		}
		out = append(out, "## palette_search_script "+
			pyRepr(pair[0])+" "+pyRepr(pair[1])+"\n"+script)
	}
	var prefixes []string
	for _, href := range []string{
		"css/style.css", "../css/style.css",
		"../_chrome/tinymoon-abc123/css/style.css",
	} {
		prefix, err := ThemeModulesPrefix(href)
		if err != nil {
			t.Fatalf("ThemeModulesPrefix(%q): %v", href, err)
		}
		prefixes = append(prefixes, prefix)
	}
	out = append(out, "## theme_modules_prefix\n"+strings.Join(prefixes, "\n"))
	var specifiers []string
	for _, path := range []string{
		"js/palette.js", "./js/palette.js", "../js/palette.js",
		"/js/palette.js", "pagefind/pagefind.js",
	} {
		specifiers = append(specifiers, ModuleSpecifier(path))
	}
	out = append(out, "## module_specifier\n"+strings.Join(specifiers, "\n"))
	out = append(out, "## pagefind_facets_html\n"+PagefindFacetsHTML(PagefindFacets{
		Version: "1.0.0", Locale: "pt-BR", Group: "Guides",
		PageType: "guide", Target: "cloudflare-pages",
		Project: `My "Project" <1>`, Tags: []string{"a,b", "c", ""},
	}))
	out = append(out, "## pagefind_facets_html empty\n"+
		PagefindFacetsHTML(PagefindFacets{Tags: []string{}}))
	out = append(out, "## pagefind_meta_html\n"+
		PagefindMetaHTML(`My "Project" <1>`, "guide", "2024-01-15"))
	out = append(out, "## pagefind_meta_html empty\n"+PagefindMetaHTML("", "", ""))
	assertEqual(t, "fragments", strings.Join(out, "\n"), reference(t, "fragments"))
}

// pyRepr renders a string the way Python's repr does for the reference file's
// own header lines.
func pyRepr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}

// TestThemeModulesPrefixRefusesAPlainStylesheet covers the one error the
// framework payload's address can report: a plain theme has no payload, so
// asking for one is a mistake rather than a value to invent.
func TestThemeModulesPrefixRefusesAPlainStylesheet(t *testing.T) {
	_, err := ThemeModulesPrefix("style.css")
	if err == nil {
		t.Fatal("a plain theme's stylesheet must not yield a module prefix")
	}
	if !strings.Contains(err.Error(), "framework payload") {
		t.Fatalf("the refusal must name the framework payload, got: %v", err)
	}
}

// TestNoModuleSpecifierIsEverBare guards the mistake that returned no search
// results on every front page: a specifier starting with neither "." nor "/"
// is refused by the browser outright, and the output root is where the hop is
// empty.
func TestNoModuleSpecifierIsEverBare(t *testing.T) {
	for _, pair := range [][2]string{
		{"", "css/style.css"},
		{"../", "../css/style.css"},
		{"../../", "../../_chrome/tinymoon-abc123/css/style.css"},
	} {
		script, err := PaletteSearchScript(pair[0], pair[1])
		if err != nil {
			t.Fatalf("PaletteSearchScript(%q, %q): %v", pair[0], pair[1], err)
		}
		specifiers := moduleSpecifiersIn(script)
		if len(specifiers) == 0 {
			t.Fatalf("no module specifiers found in:\n%s", script)
		}
		for _, specifier := range specifiers {
			if !strings.HasPrefix(specifier, "./") &&
				!strings.HasPrefix(specifier, "../") &&
				!strings.HasPrefix(specifier, "/") {
				t.Fatalf("bare module specifier %q at prefix %q",
					specifier, pair[0])
			}
		}
	}
}

// moduleSpecifiersIn returns every specifier the script imports.
func moduleSpecifiersIn(script string) []string {
	var found []string
	for _, marker := range []string{`from "`, `import("`} {
		rest := script
		for {
			idx := strings.Index(rest, marker)
			if idx < 0 {
				break
			}
			rest = rest[idx+len(marker):]
			end := strings.IndexByte(rest, '"')
			if end < 0 {
				break
			}
			found = append(found, rest[:end])
			rest = rest[end:]
		}
	}
	return found
}

// TestDerivePageTypeMatchesReference asserts the type facet over every input
// shape the Python was recorded on.
func TestDerivePageTypeMatchesReference(t *testing.T) {
	rows := []struct {
		mdPath string
		meta   util.Frontmatter
		group  string
		label  string
	}{
		{"intro.md", util.Frontmatter{"type": "tutorial"}, "",
			`'intro.md' {'type': 'tutorial'} '' -> `},
		{"changelog.md", util.Frontmatter{"type": "post"}, "",
			`'changelog.md' {'type': 'post'} '' -> `},
		{"glossary.md", util.Frontmatter{"type": "reference"}, "",
			`'glossary.md' {'type': 'reference'} '' -> `},
		{"api.md", util.Frontmatter{"generated": true, "type": "reference"},
			"API Reference",
			`'api.md' {'generated': True, 'type': 'reference'} 'API Reference' -> `},
		{"cli.md", util.Frontmatter{"generated": true, "type": "tutorial"},
			"CLI Reference",
			`'cli.md' {'generated': True, 'type': 'tutorial'} 'CLI Reference' -> `},
		{"blog.md", util.Frontmatter{"type": "post"}, "",
			`'blog.md' {'type': 'post'} '' -> `},
		{"page.md", util.Frontmatter{"type": "cookbook"}, "",
			`'page.md' {'type': 'cookbook'} '' -> `},
		{"page.md", util.Frontmatter{"title": "Page"}, "",
			`'page.md' {'title': 'Page'} '' -> `},
		{"page.md", util.Frontmatter{}, "", `'page.md' {} '' -> `},
		{"changelog.md", util.Frontmatter{}, "", `'changelog.md' {} '' -> `},
		{"glossary.md", util.Frontmatter{}, "", `'glossary.md' {} '' -> `},
		{"api.md", util.Frontmatter{"generated": true}, "API Reference",
			`'api.md' {'generated': True} 'API Reference' -> `},
		{"cli.md", util.Frontmatter{"generated": true}, "CLI Reference",
			`'cli.md' {'generated': True} 'CLI Reference' -> `},
		{"changelog.md", util.Frontmatter{"type": ""}, "",
			`'changelog.md' {'type': ''} '' -> `},
		{"glossary.md", util.Frontmatter{"type": nil}, "",
			`'glossary.md' {'type': None} '' -> `},
		{"page.md",
			util.Frontmatter{"type": "tutorial", "tags": []string{"python"}}, "",
			`'page.md' {'type': 'tutorial', 'tags': ['python']} '' -> `},
		{"api.md", util.Frontmatter{"generated": true, "type": "reference"}, "",
			`'api.md' {'generated': True, 'type': 'reference'} '' -> `},
		{"api.md", util.Frontmatter{"generated": true}, "Guides",
			`'api.md' {'generated': True} 'Guides' -> `},
		{"api.md", util.Frontmatter{"generated": "yes"}, "API Reference",
			`'api.md' {'generated': 'yes'} 'API Reference' -> `},
		{"docs/CHANGELOG.md", util.Frontmatter{}, "",
			`'docs/CHANGELOG.md' {} '' -> `},
		{"Glossary.md", util.Frontmatter{}, "", `'Glossary.md' {} '' -> `},
	}
	var lines []string
	for _, row := range rows {
		lines = append(lines,
			row.label+DerivePageType(row.mdPath, row.meta, row.group))
	}
	assertEqual(t, "derive_page_type", strings.Join(lines, "\n"),
		reference(t, "derive_page_type"))
}
