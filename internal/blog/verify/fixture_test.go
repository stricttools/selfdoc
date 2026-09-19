package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
)

// The tree every assertion is measured against.
//
// Every test here works the same way: the fixture builds an assembly tree
// that passes verification, one test injects one defect, and the assertion
// that owns that defect is the one that fails. A check with no test that can
// fail it is a check nobody has shown to work, so there is one injected defect
// per asserted property.

const canonicalBase = "https://docs.example.com"

// homeSlug is the declared project served at the site root: no subtree of its
// own, and left out of the listing it renders.
const homeSlug = "home"

// rosterEntries is the declared membership, in declaration order.
var rosterEntries = []site.RosterEntry{
	{Slug: "home", Repo: "owner/home"},
	{Slug: "alpha", Repo: "owner/alpha"},
	{Slug: "beta", Repo: "owner/beta"},
}

// writeFile writes content at path, creating the directories above it.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeJSON writes value at path as JSON.
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	writeFile(t, path, string(encoded))
}

// readFile returns the text at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// removeFile deletes path.
func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

// pageOptions are the parts of a built page a test varies.
type pageOptions struct {
	// Body is the markup between the search dialog and the closing body tag.
	Body string
	// Version is the version badge attribute a build writes, absent when
	// empty.
	Version string
	// CSSHref is the stylesheet reference; empty means the "style.css" a
	// project's own build writes at its own output root.
	CSSHref string
	// NoCSSHref suppresses the stylesheet link entirely.
	NoCSSHref bool
}

// page is a page shaped the way a real build's pages are shaped.
//
// That includes the stylesheet link: a project's build writes one on every
// page, pointing at the "style.css" at its own output root. The shared
// generator re-points it at the site-level asset, so a page that arrives here
// naming its own copy is what the graft really delivers.
func page(title, canonical string, options pageOptions) string {
	versionAttr := ""
	if options.Version != "" {
		versionAttr = fmt.Sprintf(" data-default-version=%q", options.Version)
	}
	cssHref := options.CSSHref
	if cssHref == "" {
		cssHref = "style.css"
	}
	stylesheet := fmt.Sprintf("  <link rel=\"stylesheet\" href=%q>\n", cssHref)
	if options.NoCSSHref {
		stylesheet = ""
	}
	return "<!DOCTYPE html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		fmt.Sprintf("  <title>%s</title>\n", title) +
		fmt.Sprintf("  <link rel=\"canonical\" href=%q>\n", canonical) +
		stylesheet +
		"</head>\n" +
		"<body>\n" +
		fmt.Sprintf("  <dialog class=\"search-dialog\" data-search-base=\"./\"%s></dialog>\n",
			versionAttr) +
		options.Body + "\n" +
		"</body>\n" +
		"</html>\n"
}

// chromeRef is the stylesheet reference a re-pointed page at pageRel carries.
//
// Tests that rewrite a page after the shared generator has run are writing a
// page nothing will re-point, so they write the re-pointed form themselves.
func chromeRef(t *testing.T, pageRel string) string {
	t.Helper()
	css, err := chrome.CSS(chrome.DefaultTheme)
	if err != nil {
		t.Fatalf("chrome.CSS: %v", err)
	}
	assetRel, err := chrome.AssetRel(chrome.DefaultTheme, css)
	if err != nil {
		t.Fatalf("chrome.AssetRel: %v", err)
	}
	return chrome.Href(pageRel, assetRel)
}

// manifestDoc is a project manifest as a deploy writes one.
func manifestDoc(slug, name, version string, pages, posts []any) map[string]any {
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
		"description":    name + " docs",
		"language":       "python",
		"base_url":       canonicalBase + "/" + slug,
		"author": map[string]any{
			"name": "Test Author", "url": "https://author.example",
		},
		"pages":    pages,
		"posts":    posts,
		"last_gen": "2024-01-01T00:00:00+00:00",
	}
}

// newAssembly builds an assembled tree that passes every assertion.
//
// Two declared projects, one of them with a post and a cross-project link, the
// home project at the site root, the shared files as the deploy's own
// generator writes them, and a search index as pagefind leaves one.
func newAssembly(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "assembly")
	siteDir := filepath.Join(root, "site")
	manifestsDir := filepath.Join(root, "manifests")

	writeFile(t, filepath.Join(root, "roster.toml"),
		site.RenderRoster(rosterEntries, homeSlug))
	writeJSON(t, filepath.Join(root, "projects.json"), map[string]any{
		"home":  map[string]any{"repo": "owner/home", "ref": "v0.1.0", "version": "0.1.0"},
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v1.0.0", "version": "1.0.0"},
		"beta":  map[string]any{"repo": "owner/beta", "ref": "v2.0.0", "version": "2.0.0"},
	})

	writeJSON(t, filepath.Join(manifestsDir, "alpha.json"), manifestDoc(
		"alpha", "Alpha", "1.0.0",
		[]any{
			map[string]any{"path": "index.md", "title": "Home"},
			map[string]any{"path": "guide.md", "title": "Guide"},
		},
		[]any{map[string]any{
			"slug": "hello", "title": "Hello", "date": "2024-06-01",
			"path": "blog/hello.md", "tags": []any{},
		}},
	))
	writeJSON(t, filepath.Join(manifestsDir, "beta.json"), manifestDoc(
		"beta", "Beta", "2.0.0",
		[]any{map[string]any{"path": "index.md", "title": "Home"}}, nil,
	))
	writeJSON(t, filepath.Join(manifestsDir, "home.json"), manifestDoc(
		"home", "Home", "0.1.0",
		[]any{
			map[string]any{"path": "index.md", "title": "Front page"},
			map[string]any{"path": "cv.md", "title": "CV"},
		}, nil,
	))
	writeJSON(t, filepath.Join(manifestsDir, "home-files.json"), map[string]any{
		"schema_version": 2, "slug": "home",
		"owners": map[string]any{"release": []any{"index.html", "cv/index.html"}},
	})
	// A declared home project declares its curated listing with it: the deploy
	// copies docs/projects.toml in beside the manifests, and shared generation
	// refuses without it.
	writeJSON(t, filepath.Join(manifestsDir, "home-listing.json"), map[string]any{
		"format_version": 1, "slug": "home",
		"categories": []any{map[string]any{
			"name": "Projects",
			"projects": []any{
				map[string]any{
					"slug": "alpha", "blurb": "Does the alpha thing.",
					"url": "", "name": "",
				},
				map[string]any{
					"slug": "beta", "blurb": "Does the beta thing.",
					"url": "", "name": "",
				},
			},
		}},
	})
	writeJSON(t, filepath.Join(manifestsDir, "alpha-files.json"), map[string]any{
		"schema_version": 2, "slug": "alpha",
		"owners": map[string]any{"release": []any{
			"alpha/index.html", "alpha/guide/index.html", "blog/hello/index.html",
		}},
	})

	writeFile(t, filepath.Join(siteDir, "alpha", "index.html"),
		page("Alpha", canonicalBase+"/alpha/", pageOptions{
			Body: `  <a href="guide/">Guide</a>`, Version: "1.0.0",
		}))
	writeFile(t, filepath.Join(siteDir, "alpha", "guide", "index.html"),
		page("Alpha Guide", canonicalBase+"/alpha/guide/", pageOptions{
			Body: `  <a href="../../beta/">Beta</a>`, Version: "1.0.0",
		}))
	// A post is site-level: blog/<post-slug>/, under no project slug.
	writeFile(t, filepath.Join(siteDir, "blog", "hello", "index.html"),
		page("Hello", canonicalBase+"/blog/hello/", pageOptions{Version: "1.0.0"}))
	writeFile(t, filepath.Join(siteDir, "beta", "index.html"),
		page("Beta", canonicalBase+"/beta/", pageOptions{Version: "2.0.0"}))
	// The home project's pages: at the site root, no slug segment.
	// The front page carries the curated project cards the home project's
	// build renders from the site-level directive, which is what links every
	// project from the address a reader arrives at.
	writeFile(t, filepath.Join(siteDir, "index.html"),
		page("Front page", canonicalBase+"/", pageOptions{
			Body: `  <a href="cv/">CV</a>` + "\n" +
				`  <a href="alpha/">Alpha</a>` + "\n" +
				`  <a href="beta/">Beta</a>`,
		}))
	writeFile(t, filepath.Join(siteDir, "cv", "index.html"),
		page("CV", canonicalBase+"/cv/", pageOptions{}))

	// The search index, as pagefind leaves it: the runtime's own files, the
	// entry the runtime fetches to find its index, and a fragment per indexed
	// page. Only the last two say anything about whether the site can answer a
	// search -- pagefind writes its JS whether or not it indexed a thing.
	writeFile(t, filepath.Join(siteDir, "pagefind", "pagefind.js"), "// runtime")
	writeFile(t, filepath.Join(siteDir, "pagefind", "pagefind-ui.js"), "// ui")
	writeFile(t, filepath.Join(siteDir, "pagefind", "pagefind-ui.css"), "/* ui */")
	writeJSON(t, filepath.Join(siteDir, "pagefind", "pagefind-entry.json"), map[string]any{
		"version": "1.3.0",
		"languages": map[string]any{
			"en": map[string]any{"hash": "en_abc123", "wasm": "en", "page_count": 4},
		},
	})
	writeFile(t, filepath.Join(siteDir, "pagefind", "index", "en_abc123.pf_index"), "index")
	for _, name := range []string{"f1", "f2", "f3", "f4"} {
		writeFile(t, filepath.Join(
			siteDir, "pagefind", "fragment", "en_"+name+".pf_fragment"), "fragment")
	}

	generateSharedFiles(t, siteDir, manifestsDir)
	return root
}

// generateSharedFiles writes the assembly's shared cross-project files, the
// way the deploy's own generator does.
//
// The generator itself belongs to the assembly, which sits above this package;
// this is the same composition over the same generators, so the fixture's tree
// is the tree a deploy produces rather than a hand-written imitation of one.
func generateSharedFiles(t *testing.T, siteDir, manifestsDir string) {
	t.Helper()
	handle := effects.Unbound()

	manifests, err := site.LoadAssemblyManifests(manifestsDir)
	if err != nil {
		t.Fatalf("LoadAssemblyManifests: %v", err)
	}
	curated, err := site.LoadListingFor(manifestsDir, homeSlug)
	if err != nil {
		t.Fatalf("LoadListingFor: %v", err)
	}

	// Both generated pages sit one level in, so both address the site root by
	// hopping out of their own directory.
	homepageFragment, err := shared.GenerateHomepage(manifests, "../", homeSlug, curated)
	if err != nil {
		t.Fatalf("GenerateHomepage: %v", err)
	}
	blogFragment, err := shared.GenerateBlogIndex(manifests, "../")
	if err != nil {
		t.Fatalf("GenerateBlogIndex: %v", err)
	}
	feedXML, err := shared.GenerateUnifiedFeed(manifests, canonicalBase, "")
	if err != nil {
		t.Fatalf("GenerateUnifiedFeed: %v", err)
	}
	// The sitemap takes the canonical base: every <loc> is an absolute URL by
	// protocol.
	sitemapXML, err := shared.GenerateSitemap(manifests, canonicalBase, homeSlug)
	if err != nil {
		t.Fatalf("GenerateSitemap: %v", err)
	}

	// The site-level page chrome, before anything that references it.
	themesBySlug, homeTheme := chrome.Themes(manifests, homeSlug, "")
	themeNames := []string{homeTheme}
	for _, theme := range themesBySlug {
		themeNames = append(themeNames, theme)
	}
	sort.Strings(themeNames)
	assets, err := chrome.WriteAssets(siteDir, themeNames, handle)
	if err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	homeChrome := assets[homeTheme]

	projectsDescription := shared.ProjectsDescription(manifests, homeSlug)
	projectsLD, err := shared.CollectionPageJSONLD(
		"Projects", projectsDescription, canonicalBase, "projects",
	)
	if err != nil {
		t.Fatalf("CollectionPageJSONLD(projects): %v", err)
	}
	projectsPage, err := shared.WrapSharedPage(shared.SharedPage{
		Title:        "Projects",
		BodyHTML:     homepageFragment,
		Description:  projectsDescription,
		JSONLD:       projectsLD,
		CanonicalURL: canonicalBase + "/projects/",
		CSSURL:       chrome.Href("projects/index.html", homeChrome),
		SearchPrefix: "../",
	})
	if err != nil {
		t.Fatalf("WrapSharedPage(projects): %v", err)
	}
	writeFile(t, filepath.Join(siteDir, "projects", "index.html"), projectsPage)

	blogDescription := shared.BlogDescription(manifests, homeSlug)
	blogLD, err := shared.CollectionPageJSONLD(
		"Blog", blogDescription, canonicalBase, "blog",
	)
	if err != nil {
		t.Fatalf("CollectionPageJSONLD(blog): %v", err)
	}
	blogPage, err := shared.WrapSharedPage(shared.SharedPage{
		Title:        "Blog",
		BodyHTML:     blogFragment,
		Description:  blogDescription,
		JSONLD:       blogLD,
		CanonicalURL: canonicalBase + "/blog/",
		CSSURL:       chrome.Href("blog/index.html", homeChrome),
		SearchPrefix: "../",
	})
	if err != nil {
		t.Fatalf("WrapSharedPage(blog): %v", err)
	}
	writeFile(t, filepath.Join(siteDir, "blog", "index.html"), blogPage)

	writeFile(t, filepath.Join(siteDir, "nav.json"),
		shared.GenerateNavJSON(manifests, shared.DefaultBlogPath, homeSlug))
	writeFile(t, filepath.Join(siteDir, "feed.xml"), feedXML)
	writeFile(t, filepath.Join(siteDir, "sitemap.xml"), sitemapXML)
	writeFile(t, filepath.Join(siteDir, "robots.txt"),
		shared.GenerateRobotsTxt(canonicalBase))
	writeFile(t, filepath.Join(siteDir, "llms.txt"),
		shared.GenerateLLMSTxt(manifests, canonicalBase, homeSlug))

	notFound, err := shared.GenerateNotFoundPage(chrome.Href("404.html", homeChrome), "")
	if err != nil {
		t.Fatalf("GenerateNotFoundPage: %v", err)
	}
	writeFile(t, filepath.Join(siteDir, "404.html"), notFound)

	writeFile(t, filepath.Join(siteDir, "_headers"),
		"/*\n  X-Frame-Options: DENY\n  X-Content-Type-Options: nosniff\n"+
			"  Referrer-Policy: strict-origin-when-cross-origin\n")

	// Last, once every page this deploy writes exists: aim every stylesheet
	// reference in the tree at the site-level asset.
	pages, err := chrome.EmittedPages(siteDir)
	if err != nil {
		t.Fatalf("EmittedPages: %v", err)
	}
	if _, err := chrome.RepointPages(
		siteDir, pages, themesBySlug, assets, homeTheme, handle,
	); err != nil {
		t.Fatalf("RepointPages: %v", err)
	}
}

// verifyTree runs a verification over root with the fixture's canonical base
// and no outbound fetching configured.
func verifyTree(t *testing.T, root string) *VerifyReport {
	t.Helper()
	report, err := VerifyAssembly(root, canonicalBase, nil, 0)
	if err != nil {
		t.Fatalf("VerifyAssembly: %v", err)
	}
	return report
}

// verifyTreeWith runs a verification with an injected fetcher and clock.
func verifyTreeWith(t *testing.T, root string, fetch Fetcher, now float64) *VerifyReport {
	t.Helper()
	report, err := VerifyAssembly(root, canonicalBase, fetch, now)
	if err != nil {
		t.Fatalf("VerifyAssembly: %v", err)
	}
	return report
}

// messagesOf renders every failure reported under one check.
func messagesOf(report *VerifyReport, check string) []string {
	var messages []string
	for _, failure := range report.FailuresOf(check) {
		messages = append(messages, failure.String())
	}
	return messages
}

// anyContains reports whether one of the messages holds every needle.
func anyContains(messages []string, needles ...string) bool {
	for _, message := range messages {
		matched := true
		for _, needle := range needles {
			if !strings.Contains(message, needle) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// checksThatFailed is the set of checks with at least one failure.
func checksThatFailed(report *VerifyReport) map[string]bool {
	failed := map[string]bool{}
	for _, failure := range report.Failures {
		failed[failure.Check] = true
	}
	return failed
}

// requireFailure fails the test unless a failure under check holds every
// needle.
func requireFailure(t *testing.T, report *VerifyReport, check string, needles ...string) {
	t.Helper()
	messages := messagesOf(report, check)
	if !anyContains(messages, needles...) {
		t.Fatalf("no %s failure naming %v; reported %v", check, needles, messages)
	}
}
