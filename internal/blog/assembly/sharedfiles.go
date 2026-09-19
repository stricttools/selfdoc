package assembly

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/effects"
)

// retiredWorkerName is the Cloudflare Pages worker the assembly used to
// generate at the site root. Nothing writes one now; the name lives on as the
// path the shared-files pass deletes.
const retiredWorkerName = "_worker.js"

// headersContent is the one set of response headers the whole assembled site
// is served with.
const headersContent = "/*\n" +
	"  X-Frame-Options: DENY\n" +
	"  X-Content-Type-Options: nosniff\n" +
	"  Referrer-Policy: strict-origin-when-cross-origin\n"

// RefreshHomePages re-renders every site-level directive region the home
// project emitted, and returns the pages it looked at.
//
// This is the second of the two moments a site-level directive resolves (the
// first is the home project's own build). It runs on every deploy, including
// deploys of other projects, which is the point: the front page's curated
// cards carry each project's live version, and a version changes when *that*
// project releases, not when the home project does.
//
// Each page is re-rendered against its own hop back to the site root, so a
// region on a page one level down links a project as "../alpha/" and the same
// region on the front page links it as "alpha/".
func RefreshHomePages(
	siteDir, manifestsDir string,
	manifests []map[string]any,
	homeSlug string,
	curated *listing.Listing,
	h *effects.Handle,
) ([]string, error) {
	var written []string
	if homeSlug == "" {
		return written, nil
	}
	context := sitedirectives.SiteContext{
		Manifests: manifests,
		Listing:   curated,
		HomeSlug:  homeSlug,
	}
	rels, err := site.HomePagePaths(manifestsDir, homeSlug)
	if err != nil {
		return nil, err
	}
	for _, rel := range rels {
		path := filepath.Join(siteDir, filepath.Join(strings.Split(rel, "/")...))
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		pageHTML, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		refreshed, err := sitedirectives.RefreshRegions(
			string(pageHTML),
			sitedirectives.PageContext(context, rel),
			"site/"+rel,
		)
		if err != nil {
			return nil, err
		}
		if refreshed != string(pageHTML) {
			if err := h.AtomicWrite(path, []byte(refreshed), effects.ModeDefault); err != nil {
				return nil, err
			}
		}
		written = append(written, path)
	}
	return written, nil
}

// SharedFilesOptions is what one [GenerateSharedFiles] takes.
type SharedFilesOptions struct {
	// SiteDir is the assembled tree's root. Required.
	SiteDir string
	// ManifestsDir is where the per-project manifests are. Required.
	ManifestsDir string
	// CanonicalBase is the absolute canonical base URL of the assembly site.
	// Required.
	CanonicalBase string
	// DocsBase is the base the Atom feed's entries are written against. Only
	// the feed reads it.
	DocsBase string
	// HomeSlug is the roster's home project.
	HomeSlug string
	// Theme names one theme every page's chrome asset is built from,
	// overriding what each project's manifest declares. It is the preview's
	// --theme: the checkouts were all built under that theme, so their pages
	// have to reference its stylesheet. Empty -- which is what every deploy
	// passes -- leaves each project on its own.
	Theme string
}

// GenerateSharedFiles writes the assembly's shared cross-project files and
// returns their paths.
//
// The files are the project listing at "projects/index.html", the blog index at
// "blog/index.html", "nav.json", "feed.xml", "sitemap.xml", "robots.txt",
// "llms.txt", "404.html" and "_headers". Both generated pages sit at fixed,
// generator-owned addresses; the site root belongs to the home project, whose
// own pages are grafted there and are never written by this function.
//
// The pass also owns the site root's retired routing file: an assembly that
// deployed before the redirect worker was dropped still carries a "_worker.js"
// there, nothing rewrites it, and the routing-artifact check refuses it. It is
// deleted here, on the next integration of any project.
//
// "robots.txt", "llms.txt" and "404.html" are the site's, not any project's:
// every constituent build writes its own set at its own output root, where they
// end up buried under "<slug>/" and serve nobody. The per-project "llms.txt"
// files are the exception that stays useful -- the site-wide one links to each
// of them rather than restating them.
//
// [SharedFilesOptions.HomeSlug] is the roster's home project. It is left out of
// the generated listing and out of nav -- the front page does not list itself
// -- and its pages are addressed from the site root. Every site-level directive
// region in its emitted pages is re-rendered here, on every deploy, so a
// version badge on the front page is as current as the last deploy of the
// project it names rather than as the last deploy of the home project.
//
// A missing required input is an error -- the CLI turns those into a usage
// error, the integrate command lets them abort the deploy.
func GenerateSharedFiles(opts SharedFilesOptions, h *effects.Handle) ([]string, error) {
	if opts.SiteDir == "" {
		return nil, errorf("SiteDir is required")
	}
	if opts.ManifestsDir == "" {
		return nil, errorf("ManifestsDir is required")
	}
	if opts.CanonicalBase == "" {
		return nil, errorf(
			"CanonicalBase is required (set topology.docs_base in " +
				"selfdoc.json and regenerate the assembly workflow).",
		)
	}
	canonicalBase := strings.TrimRight(opts.CanonicalBase, "/")
	docsBase := strings.TrimRight(opts.DocsBase, "/")

	manifests, err := site.LoadAssemblyManifests(opts.ManifestsDir)
	if err != nil {
		return nil, err
	}
	curated, err := site.LoadListingFor(opts.ManifestsDir, opts.HomeSlug)
	if err != nil {
		return nil, err
	}

	// Both generated pages sit one level in, so both address the site root by
	// hopping out of their own directory. Nothing here is written against
	// DocsBase: a link a reader clicks has to resolve under any mount, and
	// only the feed and the sitemap below stay absolute.
	homepageFragment, err := shared.GenerateHomepage(
		manifests, "../", opts.HomeSlug, curated,
	)
	if err != nil {
		return nil, err
	}
	blogFragment, err := shared.GenerateBlogIndex(manifests, "../")
	if err != nil {
		return nil, err
	}
	navJSON := shared.GenerateNavJSON(manifests, shared.DefaultBlogPath, opts.HomeSlug)
	feedXML, err := shared.GenerateUnifiedFeed(manifests, docsBase, "")
	if err != nil {
		return nil, err
	}
	// The sitemap takes the canonical base, never DocsBase: every <loc> is an
	// absolute URL by protocol, and DocsBase is allowed to be root-relative
	// for in-page links.
	sitemapXML, err := shared.GenerateSitemap(manifests, canonicalBase, opts.HomeSlug)
	if err != nil {
		return nil, err
	}

	var written []string

	if err := h.MkdirAll(opts.SiteDir); err != nil {
		return nil, err
	}

	// The site-level page chrome, before anything that references it. One
	// asset per theme the roster declares, sourced from the theme files of the
	// toolchain running this deploy; the shared pages below name theirs
	// directly and every grafted page is re-pointed at the end.
	themesBySlug, homeTheme := chrome.Themes(manifests, opts.HomeSlug, opts.Theme)
	themeNames := make([]string, 0, len(themesBySlug)+1)
	for _, slug := range sortedKeys(themesBySlug) {
		themeNames = append(themeNames, themesBySlug[slug])
	}
	themeNames = append(themeNames, homeTheme)
	chromeAssets, err := chrome.WriteAssets(opts.SiteDir, themeNames, h)
	if err != nil {
		return nil, err
	}
	assetRels := make([]string, 0, len(chromeAssets))
	for _, rel := range chromeAssets {
		assetRels = append(assetRels, rel)
	}
	sort.Strings(assetRels)
	for _, rel := range assetRels {
		written = append(written, sitePath(opts.SiteDir, rel))
	}
	homeChrome := chromeAssets[homeTheme]

	projectsDir := filepath.Join(opts.SiteDir, "projects")
	if err := h.MkdirAll(projectsDir); err != nil {
		return nil, err
	}
	projectsPath := filepath.Join(projectsDir, "index.html")
	projectsDescription := shared.ProjectsDescription(manifests, opts.HomeSlug)
	projectsLD, err := shared.CollectionPageJSONLD(
		"Projects", projectsDescription, canonicalBase, "projects",
	)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if err := h.AtomicWrite(projectsPath, []byte(projectsPage), effects.ModeDefault); err != nil {
		return nil, err
	}
	written = append(written, projectsPath)

	refreshed, err := RefreshHomePages(
		opts.SiteDir, opts.ManifestsDir, manifests, opts.HomeSlug, curated, h,
	)
	if err != nil {
		return nil, err
	}
	written = append(written, refreshed...)

	blogDir := filepath.Join(opts.SiteDir, "blog")
	if err := h.MkdirAll(blogDir); err != nil {
		return nil, err
	}
	blogPath := filepath.Join(blogDir, "index.html")
	blogDescription := shared.BlogDescription(manifests, opts.HomeSlug)
	blogLD, err := shared.CollectionPageJSONLD(
		"Blog", blogDescription, canonicalBase, "blog",
	)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if err := h.AtomicWrite(blogPath, []byte(blogPage), effects.ModeDefault); err != nil {
		return nil, err
	}
	written = append(written, blogPath)

	notFound, err := shared.GenerateNotFoundPage(
		chrome.Href("404.html", homeChrome), "",
	)
	if err != nil {
		return nil, err
	}

	// The redirect worker an earlier deploy left at the site root. Host
	// redirects are the zone's now and a historical path shape answers 404,
	// so the file routes nothing; it is removed rather than left for the
	// routing-artifact check to refuse forever.
	if err := h.RemoveIfExists(filepath.Join(opts.SiteDir, retiredWorkerName)); err != nil {
		return nil, err
	}

	for _, file := range []struct {
		name    string
		content string
	}{
		{"nav.json", navJSON},
		{"feed.xml", feedXML},
		{"sitemap.xml", sitemapXML},
		{"robots.txt", shared.GenerateRobotsTxt(canonicalBase)},
		{"llms.txt", shared.GenerateLLMSTxt(manifests, canonicalBase, opts.HomeSlug)},
		{"404.html", notFound},
		{"_headers", headersContent},
	} {
		path := filepath.Join(opts.SiteDir, file.name)
		if err := h.AtomicWrite(path, []byte(file.content), effects.ModeDefault); err != nil {
			return nil, err
		}
		written = append(written, path)
	}

	// Last, once every page this deploy writes exists: aim every stylesheet
	// reference in the tree at the site-level asset. A project's build emits a
	// self-contained style.css beside its own pages -- it has to, a standalone
	// deploy has no assembly behind it -- so a grafted subtree arrives
	// pointing at its own copy and is re-pointed here. Because this runs on
	// every deploy of any project, a toolchain upgrade reaches pages published
	// months ago without republishing them.
	pages, err := chrome.EmittedPages(opts.SiteDir)
	if err != nil {
		return nil, err
	}
	repointed, err := chrome.RepointPages(
		opts.SiteDir, pages, themesBySlug, chromeAssets, homeTheme, h,
	)
	if err != nil {
		return nil, err
	}
	for _, rel := range repointed {
		written = append(written, sitePath(opts.SiteDir, rel))
	}

	// And, over the same pages, the rule that a link a reader clicks stays
	// inside whatever mount the tree is served from. A page that addresses
	// this site by its own base works on production and silently leaves a
	// preview or a mirror; the verification below refuses the whole tree over
	// one, including pages this deploy did not write.
	relativized, err := RelativizeSiteLinks(opts.SiteDir, canonicalBase, pages, h)
	if err != nil {
		return nil, err
	}
	for _, rel := range relativized {
		written = append(written, sitePath(opts.SiteDir, rel))
	}

	// And, over the same pages again, the one element of a page that names
	// the other projects: the sibling block each page ends with. It is
	// rendered at build time from the membership of that day, so every roster
	// change leaves it stale everywhere but in the subtree that just
	// deployed, and a retirement leaves a link to an address the tree no
	// longer serves. Regenerating it here is what carries a membership change
	// to the pages of projects that did not deploy.
	refreshedSiblings, err := RefreshSiblingBlocks(
		opts.SiteDir, opts.ManifestsDir, manifests, opts.HomeSlug, pages, h,
	)
	if err != nil {
		return nil, err
	}
	for _, rel := range refreshedSiblings {
		written = append(written, sitePath(opts.SiteDir, rel))
	}

	// A page more than one sweep changed was written more than once and is
	// one file, so the count the caller prints counts files rather than
	// writes.
	return dedupePaths(written), nil
}

// dedupePaths drops repeated paths, keeping each path's first position.
func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		unique = append(unique, path)
	}
	return unique
}

// sitePath joins a site-relative path onto the tree's root.
func sitePath(siteDir, rel string) string {
	return filepath.Join(siteDir, filepath.Join(strings.Split(rel, "/")...))
}

// sortedKeys returns a mapping's keys in sorted order, so every derived list
// is the same on every run.
func sortedKeys(mapping map[string]string) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
