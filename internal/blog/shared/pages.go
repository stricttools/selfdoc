package shared

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/util"
)

// ErrCSSURLRequired is returned by [WrapSharedPage] when it is handed no
// stylesheet reference.
//
// The parameter was optional and no caller ever passed it, so every shared
// page the assembly published shipped as bare HTML. There is no styling
// without it and nothing to fall back to.
var ErrCSSURLRequired = errors.New(
	"WrapSharedPage needs a css_url: a shared page carries the " +
		"site-level chrome stylesheet like every other page on the " +
		"site, and with none it renders as unstyled HTML. Pass the " +
		"page's reference to the asset chrome.AssetRel names.",
)

// SharedPage is one shared page's head and body: what [WrapSharedPage] needs
// to write a complete document.
type SharedPage struct {
	// Title titles the document.
	Title string
	// BodyHTML is the fragment placed inside the body.
	BodyHTML string
	// Description is the page's meta description. Empty emits no meta
	// description, which is the right answer for a page no engine should
	// index.
	Description string
	// Robots is the meta robots directive ("noindex"). Empty emits none,
	// which leaves the page indexable.
	Robots string
	// JSONLD is one structured-data document, already encoded, written into
	// the head inside an ld+json script. Empty emits no script.
	JSONLD string
	// CanonicalURL is the absolute URL for the page's rel=canonical link --
	// the assembly site is reachable on more than one host, so the shared
	// pages declare which one is canonical, and an empty value emits no
	// canonical link at all.
	CanonicalURL string
	// CSSURL is the page's reference to the site-level chrome stylesheet,
	// relative to the page. It is required: an empty value returns
	// [ErrCSSURLRequired].
	CSSURL string
	// SearchPrefix is the hop from this page back to the site root, where
	// the assembly's one site-wide Pagefind index lives ("" for a page at
	// the root, "../" one level in). A shared page carries the same search
	// as every documentation page, and the hop is a fact about where the
	// page sits.
	SearchPrefix string
}

// WrapSharedPage wraps an HTML fragment in a complete HTML page.
//
// The wrapper reuses the theme's own class surface where the theme has one --
// ".site-footer" is the theme's footer, and the version badge in the project
// listing is the theme's badge. The rest of what these pages render (the
// listing cards, the blog rows, the not-found list) is the assembly's own
// markup, styled by the shared-page rules appended to the site-level chrome
// asset by the chrome package.
//
// The theme's three-column ".layout" is deliberately not reused: it reserves a
// 240px sidebar column and a 200px table-of-contents column, and a shared page
// has neither, so it would render as a centred column with two empty gutters.
// ".shared-page" is the container these pages get instead.
//
// Which search surface the page carries is read off the stylesheet it already
// names: a framework theme's sheet is the only one written at
// themes.FrameworkCSSRel inside its payload directory, and the framework's
// modules sit beside it. Deriving it beats threading the theme name through
// every shared-page caller, and it cannot disagree with the stylesheet the
// page actually loads.
func WrapSharedPage(opts SharedPage) (string, error) {
	if opts.CSSURL == "" {
		return "", ErrCSSURLRequired
	}
	cssLink := "\n    <link rel=\"stylesheet\" href=\"" + EscapeHTML(opts.CSSURL) + "\">"
	var searchHead, searchDialog, searchScript string
	if strings.HasSuffix(opts.CSSURL, themes.FrameworkCSSRel) {
		script, err := page.PaletteSearchScript(opts.SearchPrefix, opts.CSSURL)
		if err != nil {
			return "", err
		}
		searchScript = script
	} else {
		searchHead = page.PagefindHeadTags(opts.SearchPrefix)
		searchDialog = page.PagefindDialogHTML()
		searchScript = page.PagefindInitScript(opts.SearchPrefix)
	}
	canonicalLink := ""
	if opts.CanonicalURL != "" {
		canonicalLink = "\n    <link rel=\"canonical\" href=\"" +
			EscapeHTML(opts.CanonicalURL) + "\">"
	}
	descriptionMeta := ""
	if opts.Description != "" {
		descriptionMeta = "\n    <meta name=\"description\" content=\"" +
			EscapeHTML(opts.Description) + "\">"
	}
	robotsMeta := ""
	if opts.Robots != "" {
		robotsMeta = "\n    <meta name=\"robots\" content=\"" +
			EscapeHTML(opts.Robots) + "\">"
	}
	structuredData := ""
	if opts.JSONLD != "" {
		structuredData = "<script type=\"application/ld+json\">\n" +
			opts.JSONLD + "\n</script>\n"
	}
	return "<!DOCTYPE html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		"    <meta charset=\"utf-8\">\n" +
		"    <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"    <title>" + EscapeHTML(opts.Title) + "</title>" +
		descriptionMeta + robotsMeta + canonicalLink + cssLink + "\n" +
		structuredData +
		searchHead +
		"</head>\n" +
		"<body>\n" +
		"<div class=\"shared-page\">\n" +
		opts.BodyHTML + "\n" +
		"</div>\n" +
		"<footer class=\"site-footer\">\n" +
		"<p>Built with <a href=\"https://github.com/smm-h/selfdoc\">selfdoc</a></p>\n" +
		"</footer>\n" +
		searchDialog + "\n" +
		searchScript + "\n" +
		"</body>\n" +
		"</html>\n", nil
}

// GenerateHomepage produces the project listing fragment the "/projects/" page
// serves.
//
// curated is the home project's curated listing, and it is the source:
// categories, order and blurbs are content the home project authors, and this
// page is one of its two renderings (the front page's cards directive is the
// other, which refuses without it too).
//
// A declared home project declares a listing with it, so a nil curated beside
// a homeSlug is refused rather than rendered around. The name-ordered
// rendering answers one state: a tree that declares no home project at all,
// where no curated listing can exist.
//
// The home project itself is never in either rendering: it is the page the
// listing is reached from, not one of the projects it lists.
//
// siteHop is the hop from the page this fragment is rendered into back to the
// site root ("" at the root, "../" one level in). Every card's link is written
// against it, so the listing resolves under any mount -- production, a local
// preview, a mirror -- rather than only under the deployed base.
func GenerateHomepage(manifests []map[string]any, siteHop string, homeSlug string, curated *listing.Listing) (string, error) {
	if homeSlug != "" && curated == nil {
		return "", fmt.Errorf(
			"the home project %s declares a curated listing, so the "+
				"/projects/ page is rendered from it; there is no listing "+
				"here to render. Load it with "+
				"site.LoadListingFor, which names the file "+
				"and the deploy step when it is missing.",
			util.PythonRepr(homeSlug),
		)
	}
	if curated != nil {
		return listing.RenderHTML(*curated, manifests, siteHop, homeSlug, "Projects")
	}

	parts := []string{`<section class="project-list">`, "  <h1>Projects</h1>"}
	for _, manifest := range sortedByName(manifests, homeSlug) {
		name := EscapeHTML(util.PythonStrOrEmpty(manifest["name"]))
		slug := EscapeHTML(util.PythonStrOrEmpty(manifest["slug"]))
		version := EscapeHTML(util.PythonStrOrEmpty(manifest["version"]))
		description := EscapeHTML(util.PythonStrOrEmpty(manifest["description"]))
		href := siteHop + slug + "/"
		parts = append(parts, `  <article class="project-card">`)
		parts = append(parts, `    <h2><a href="`+href+`">`+name+"</a></h2>")
		if version != "" {
			if version == "0.0.0" {
				parts = append(parts, `    <span class="version-badge">monorepo</span>`)
			} else {
				parts = append(parts, `    <span class="version-badge">v`+version+"</span>")
			}
		}
		if description != "" {
			parts = append(parts, "    <p>"+description+"</p>")
		}
		parts = append(parts, "  </article>")
	}
	parts = append(parts, "</section>")
	return strings.Join(parts, "\n"), nil
}

// GenerateBlogIndex produces an HTML fragment listing every post across
// projects, newest first.
//
// siteHop is the hop from the page this fragment is rendered into back to the
// site root. Each entry links a post relative to it, so the index works on
// every mount rather than on the deployed host alone.
func GenerateBlogIndex(manifests []map[string]any, siteHop string) (string, error) {
	posts, err := MergeProjectPosts(manifests)
	if err != nil {
		return "", err
	}
	if len(posts) == 0 {
		return "<p>No posts yet.</p>", nil
	}
	sort.SliceStable(posts, func(i, j int) bool {
		return posts[i].Date > posts[j].Date
	})

	parts := []string{`<section class="blog-index">`, "  <h1>Blog</h1>"}
	for _, post := range posts {
		href := siteHop + PostTarget(post.Slug) + "/"
		parts = append(parts, `  <article class="blog-entry">`)
		parts = append(parts, "    <time>"+EscapeHTML(post.Date)+"</time>")
		parts = append(parts, `    <span class="project-name">`+
			EscapeHTML(post.ProjectName)+"</span>")
		parts = append(parts, `    <a href="`+href+`">`+
			EscapeHTML(post.Title)+"</a>")
		parts = append(parts, "  </article>")
	}
	parts = append(parts, "</section>")
	return strings.Join(parts, "\n"), nil
}

// GenerateNotFoundPage produces the assembly's root 404 page.
//
// The only one the site has. "404.html" is answered at the root of what the
// provider serves, so a copy inside a project's subtree is never reached; the
// projects stopped emitting one and this page answers every unmatched address
// on the site.
//
// It is served through the hosting provider's "404.html" convention: a request
// matching no asset gets this body with a 404 status. That is why its body has
// to differ from the front page's -- an unknown address that renders the front
// page is a soft 404, and a crawler reads it as a duplicate of the home page
// rather than as a dead link.
//
// It declares no canonical, deliberately. A canonical says "this content lives
// at this address"; an error page is not content and has no address of its own
// -- it is the answer to every address the site does not serve. Naming one
// would hand a crawler a real URL for a page that only ever appears under URLs
// that do not exist.
//
// cssURL is the page's reference to the site-level chrome stylesheet. The 404
// sits at the site root, so it is the one shared page whose hop back to the
// root is empty, and the three links it offers are written against siteHop
// rather than against the deployed base. An absolute one would send every
// reader who hit a dead address on a preview or a mirror to production.
func GenerateNotFoundPage(cssURL, siteHop string) (string, error) {
	hop := EscapeHTML(siteHop)
	home := hop
	if home == "" {
		home = "./"
	}
	body := "<main class=\"not-found\">\n" +
		"  <h1>Page not found</h1>\n" +
		"  <p>There is no page at this address. It may have moved, or the " +
		"link that brought you here may be wrong.</p>\n" +
		"  <p>These three are always here:</p>\n" +
		"  <ul>\n" +
		"    <li><a href=\"" + home + "\">Home</a></li>\n" +
		"    <li><a href=\"" + hop + "projects/\">Projects</a></li>\n" +
		"    <li><a href=\"" + hop + PostsSegment + "/\">Blog</a></li>\n" +
		"  </ul>\n" +
		"  <p>Or search the whole site from any documentation page.</p>\n" +
		"</main>"
	return WrapSharedPage(SharedPage{
		Title:    "Page not found",
		BodyHTML: body,
		// An error page is the answer to every address the site does not
		// serve, so it is the one shared page that asks not to be indexed
		// and carries no structured data: there is no collection here to
		// describe, and no address of its own to describe it at.
		Robots:       "noindex",
		CSSURL:       cssURL,
		SearchPrefix: siteHop,
	})
}

// sortedByName is every manifest except the home project, ordered by
// lowercased display name.
//
// The ordering is stable, so two projects whose names lowercase equal keep the
// order the roster gave them -- which is what Python's sorted() did.
func sortedByName(manifests []map[string]any, homeSlug string) []map[string]any {
	listed := make([]map[string]any, 0, len(manifests))
	for _, manifest := range manifests {
		if util.PythonStrOrEmpty(manifest["slug"]) == homeSlug {
			continue
		}
		listed = append(listed, manifest)
	}
	sort.SliceStable(listed, func(i, j int) bool {
		return strings.ToLower(util.PythonStrOrEmpty(listed[i]["name"])) <
			strings.ToLower(util.PythonStrOrEmpty(listed[j]["name"]))
	})
	return listed
}
