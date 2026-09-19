package page

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/js"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
)

// pageTypeClassRE is what a declared page type must match to become a class
// on the content region: a bare identifier, so nothing an author writes can
// close the attribute.
var pageTypeClassRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// The search trigger the topbar carries, in the two shapes the "search"
// setting selects. The third setting, "hidden", carries none.
const (
	searchIconTrigger = `<button class="search-trigger" aria-label="Search documentation" data-tooltip="Search (Cmd+K)">` + "\n" +
		`<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>` + "\n" +
		`</button>` + "\n"
	searchBarTrigger = `<button class="search-bar-trigger" aria-label="Search documentation">` + "\n" +
		`<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>` + "\n" +
		`<span class="search-bar-text">Search...</span>` + "\n" +
		`<kbd class="search-bar-kbd">Cmd+K</kbd>` + "\n" +
		`</button>` + "\n"
)

// WrapOptions is everything one page's document is built from.
//
// It mirrors the keyword arguments of the Python function it replaces, with
// two omissions: that function also took the current version and an
// is-latest flag, and read neither.
//
// Three fields are pointers because the empty string is a legal value for
// them and "not stated" is a different answer: a page at its mount root
// really does have an empty hop. Leaving one nil takes the default the field
// documents.
type WrapOptions struct {
	// BodyHTML is the converted Markdown body.
	BodyHTML string
	// NavHTML is the rendered sidebar navigation.
	NavHTML string
	// Title is the page's title.
	Title string
	// ProjectName is the project's name, shown as the sidebar's wordmark.
	ProjectName string
	// SiteName is the name of the assembled site this page is published on,
	// which the document title ends with. Empty is a standalone build: a
	// project deployed on its own has no site above it.
	SiteName string
	// Version is the version shown on the badge, "" for no badge.
	Version string
	// CSSHref is where this page's stylesheet is, relative to the page.
	CSSHref string
	// CustomCSSHref is where the project's own stylesheet is, "" when it
	// has none.
	CustomCSSHref string
	// TOCHTML is the rendered table of contents, "" when the page has too
	// few headings for one.
	TOCHTML string
	// Breadcrumbs is the rendered breadcrumb trail, "" on the index page.
	Breadcrumbs string
	// PrevPage and NextPage are the sidebar neighbours, nil at either end.
	PrevPage *NavItem
	NextPage *NavItem
	// Prefix is the hop to this page's own mount root, where its sibling
	// pages are.
	Prefix string
	// AssetPrefix is the hop to the output root, where the shared assets
	// are.
	AssetPrefix string
	// UnversionedPrefix is the hop to the version-free mount, where the
	// pages marked "versioned: false" were built. Nil means Prefix: on a
	// page whose sidebar holds no unversioned item the two are one answer.
	UnversionedPrefix *string
	// SitePrefix is the hop to the site level, where the posts are. Nil
	// means AssetPrefix, which is the answer for every build that is not
	// mounted under a shared site.
	SitePrefix *string
	// HomeHref is where the site-name link points. Nil means this mount's
	// own index page.
	HomeHref *string
	// Repo is the repository URL the edit links are built from.
	Repo string
	// SourcePath is this page's Markdown source, relative to the project
	// root, which the edit links name.
	SourcePath string
	// BaseURL is the site's base URL.
	BaseURL string
	// URLBuilder builds the absolute URLs the metadata and the share
	// control carry.
	URLBuilder urls.URLBuilder
	// PagePath is this page's HTML path. A page with none -- the 404 -- has
	// no address, and therefore no pickers, no notice and no share control.
	PagePath string
	// Description is the page's frontmatter description.
	Description string
	// Lang is the page's language tag.
	Lang string
	// DatePublished and DateModified are the page's dates, in ISO form.
	DatePublished string
	DateModified  string
	// Author is the declared author block.
	Author map[string]any
	// FeedURL is where the Atom feed is, relative to this page.
	FeedURL string
	// Summary is the frontmatter description shown above the title, "" when
	// the page does not print one.
	Summary string
	// CriticalCSS is the stylesheet fragment inlined in the head.
	CriticalCSS string
	// Schema is the frontmatter "schema" declaration.
	Schema string
	// PageType is the frontmatter-declared page type, which decides the
	// layout, the content region's class and the schema.org type.
	PageType string
	// SchemaTypes overrides the page-type to schema.org-type map.
	SchemaTypes map[string]string
	// PageTags are the page's frontmatter tags.
	PageTags []string
	// GlossaryLinks is the page's "glossary_links" declaration: false is
	// the page's opt-out from automatic term links, and a page declaring
	// nothing carries true. The automatic linker is designed to read it;
	// the field is the declaration's home until that linker arrives.
	GlossaryLinks bool
	// TwitterSite is the site's Twitter handle.
	TwitterSite string
	// Search selects the topbar's search trigger: "icon", "bar" or
	// "hidden". Empty means "icon".
	Search string
	// Feedback is the feedback block, nil when the project configures none.
	Feedback map[string]any
	// Branch is the branch the edit links point at.
	Branch string
	// NavGroup is the navigation group the page belongs to, for the group
	// search facet.
	NavGroup string
	// FacetType is the page's derived type facet.
	FacetType string
	// SiteTerms is the site-wide term table, for cross-page term links.
	SiteTerms *html.SiteTerms
	// PageNumber and TotalPages drive the reading-progress line. Zero means
	// the caller states neither.
	PageNumber int
	TotalPages int
	// ThemeMeta is the theme's metadata. Nil takes the default theme's.
	ThemeMeta *themes.Metadata
	// HasHero suppresses the article's own H1, because the hero carries one.
	HasHero bool
	// DeployTarget is the deploy provider, for the target facet and the
	// security meta.
	DeployTarget string
	// PageNav and PageProgress enable the previous/next links and the
	// reading-progress line.
	PageNav      bool
	PageProgress bool
	// MountLocale, MountProject, MountVersion and MountArchived are the
	// mount coordinates this page was built under.
	MountLocale   string
	MountProject  string
	MountVersion  string
	MountArchived bool
	// AvailableVersions and AvailableLocales are the configured versions and
	// locales, which the two pickers offer.
	AvailableVersions []VersionEntry
	AvailableLocales  []LocaleEntry
	// VersionPages maps a version to the pages it has, so the picker never
	// offers a version that does not hold this page. Nil offers every
	// version.
	VersionPages map[string]map[string]bool
	// CurrentLocale is the locale being built, for the locale picker's
	// selected option and the locale facet.
	CurrentLocale string
}

// DocumentTitleLimit is the longest a document title should be: a search
// engine displays about 50 to 60 characters of one and cuts the rest, and the
// check that measures a page's title holds every page to that number.
const DocumentTitleLimit = 60

// documentTitleSeparator joins the written values a document title is made of.
const documentTitleSeparator = " - "

// DocumentTitleParts are the written values a page's <title> is composed of.
//
// Every one of them is text somebody wrote -- a page's frontmatter title, a
// project's declared name, a site's declared name. Nothing here is cut out of
// another field.
type DocumentTitleParts struct {
	// PageTitle is the page's own written title.
	PageTitle string
	// ProjectName is the name of the project that publishes the page. An
	// index page leaves it out; see [DocumentTitle].
	ProjectName string
	// SiteName is the name of the assembled site that publishes the project.
	// Empty is a standalone build, which has no site above it.
	SiteName string
	// IsIndexPage marks the project's own front page.
	IsIndexPage bool
}

// DocumentTitle is what a page's <title> element carries: the written values
// in [DocumentTitleParts], joined with " - ".
//
// The page's own title comes first, then the project that publishes it, then
// the site that publishes the project. An empty name is left out, and a name
// equal to the one before it is written once, so no title ever renders
// "X - X".
//
// An index page names no project. It IS the project's front page, and its
// written title already says which project a reader arrived at, so only the
// site name follows it -- and on a standalone build, where there is no site
// name, the written title stands alone.
func DocumentTitle(parts DocumentTitleParts) string {
	written := []string{strings.TrimSpace(parts.PageTitle)}
	if !parts.IsIndexPage {
		written = append(written, strings.TrimSpace(parts.ProjectName))
	}
	written = append(written, strings.TrimSpace(parts.SiteName))

	kept := make([]string, 0, len(written))
	for _, value := range written {
		if value == "" {
			continue
		}
		if len(kept) > 0 && kept[len(kept)-1] == value {
			continue
		}
		kept = append(kept, value)
	}
	return strings.Join(kept, documentTitleSeparator)
}

// WrapPage wraps a converted body in the full HTML document.
//
// Prefix reaches this page's own mount root (its sibling pages), AssetPrefix
// reaches the output root (the shared assets), and UnversionedPrefix reaches
// the version-free mount (the pages marked "versioned: false"). All three
// come from the addressing authority. HomeHref addresses the page the site
// calls home, which is not always this mount's index.
func WrapPage(opts WrapOptions) (string, error) {
	if opts.CSSHref == "" {
		opts.CSSHref = themes.DefaultCSSRel
	}
	if opts.Lang == "" {
		opts.Lang = "en"
	}
	if opts.Branch == "" {
		opts.Branch = "main"
	}
	if opts.ThemeMeta == nil {
		themeMeta, err := themes.Meta("minimal")
		if err != nil {
			return "", err
		}
		opts.ThemeMeta = &themeMeta
	}
	unversionedPrefix := opts.Prefix
	if opts.UnversionedPrefix != nil {
		unversionedPrefix = *opts.UnversionedPrefix
	}
	// No caller said where the site level is, so it is wherever the output
	// root is -- which is the answer for every build that is not mounted
	// under a shared site.
	sitePrefix := opts.AssetPrefix
	if opts.SitePrefix != nil {
		sitePrefix = *opts.SitePrefix
	}
	homeHrefValue := opts.Prefix + "index.html"
	if opts.HomeHref != nil {
		homeHrefValue = *opts.HomeHref
	}

	// The page's own address decides every control that names another
	// address: the two pickers, the superseded-version notice and the share
	// control. A page with no path of its own (the 404) has no address and
	// gets none of them.
	versionPickerHTML := ""
	localePickerHTML := ""
	versionNoticeHTML := ""
	shareHTML := ""
	if opts.PagePath != "" {
		addr, err := address.NewPageAddress(opts.PagePath, address.Coordinates{
			Locale:   opts.MountLocale,
			Project:  opts.MountProject,
			Version:  opts.MountVersion,
			Archived: opts.MountArchived,
		})
		if err != nil {
			return "", err
		}
		if versionPickerHTML, err = renderVersionPicker(
			addr, opts.AvailableVersions, opts.VersionPages); err != nil {
			return "", err
		}
		if localePickerHTML, err = renderLocalePicker(
			addr, opts.AvailableLocales, opts.CurrentLocale); err != nil {
			return "", err
		}
		if versionNoticeHTML, err = renderVersionNotice(addr); err != nil {
			return "", err
		}
		shareHTML = renderShareControl(addr, opts.URLBuilder, opts.BaseURL)
	}

	meta := buildPageMeta(opts, unversionedPrefix, sitePrefix)
	bodyHTML := meta.bodyHTML

	seoTags, securityMeta, err := RenderSEOTags(SEOOptions{
		Title:            opts.Title,
		BaseURL:          opts.BaseURL,
		URLBuilder:       opts.URLBuilder,
		PagePath:         opts.PagePath,
		Description:      opts.Description,
		BodyHTML:         bodyHTML,
		Author:           opts.Author,
		ProjectName:      opts.ProjectName,
		SiteName:         opts.SiteName,
		Repo:             opts.Repo,
		DatePublished:    opts.DatePublished,
		DateModified:     opts.DateModified,
		Lang:             opts.Lang,
		Breadcrumbs:      opts.Breadcrumbs,
		Schema:           opts.Schema,
		PageType:         opts.PageType,
		SchemaTypes:      opts.SchemaTypes,
		PageTags:         opts.PageTags,
		TwitterSite:      opts.TwitterSite,
		DeployTarget:     opts.DeployTarget,
		AvailableLocales: opts.AvailableLocales,
		MountLocale:      opts.MountLocale,
		MountProject:     opts.MountProject,
		MountVersion:     opts.MountVersion,
	})
	if err != nil {
		return "", err
	}

	searchTriggerHTML := ""
	switch opts.Search {
	case "", "icon":
		searchTriggerHTML = searchIconTrigger
	case "bar":
		searchTriggerHTML = searchBarTrigger
	}

	topbarHTML := renderTopbar(
		opts.Title, searchTriggerHTML, versionPickerHTML, localePickerHTML)
	sidebarHTML := renderSidebar(
		opts.ProjectName, meta.versionBadge, homeHrefValue, opts.NavHTML)

	// The search surface. A framework theme draws its own -- the
	// framework's command palette over Pagefind's query API -- and loads
	// neither the widget's stylesheet nor its bundle nor the dialog it
	// mounts into. Every other theme gets the widget.
	framework, err := themes.FrameworkOf(opts.ThemeMeta.Name)
	if err != nil {
		return "", err
	}
	var searchHeadHTML, searchDialogHTML, searchScriptHTML string
	if framework != nil {
		if searchScriptHTML, err = PaletteSearchScript(
			opts.AssetPrefix, opts.CSSHref); err != nil {
			return "", err
		}
	} else {
		searchHeadHTML = PagefindHeadTags(opts.AssetPrefix)
		searchDialogHTML = PagefindDialogHTML()
		searchScriptHTML = PagefindInitScript(opts.AssetPrefix)
	}

	headJS := js.Head()
	bodyJS := html.MinifyJS(js.AssembleBody(js.PageHTML{
		Body:   bodyHTML,
		TOC:    opts.TOCHTML,
		Footer: meta.footerHTML,
		Extras: versionNoticeHTML + shareHTML,
		Chrome: topbarHTML,
	}))

	gaHeadScript := ""
	if opts.Feedback != nil {
		if ga, _ := opts.Feedback["ga"].(string); ga != "" {
			gaID := html.EscapeHTML(ga)
			gaJS, loadErr := js.Load("ga")
			if loadErr != nil {
				return "", loadErr
			}
			gaHeadScript = `<script async src="https://www.googletagmanager.com/gtag/js?id=` +
				gaID + `"></script>` + "\n" +
				`<script data-ga-id="` + gaID + `">` + gaJS + `</script>` + "\n"
		}
	}

	// The article is the indexed region; the facet and metadata values sit
	// inside it as their own elements, because one element carries one
	// data-pagefind-filter and one data-pagefind-meta attribute.
	facetLocale := opts.CurrentLocale
	if facetLocale == "" {
		facetLocale = opts.MountLocale
	}
	facetType := opts.FacetType
	if facetType == "" {
		facetType = opts.PageType
	}
	pagefindBlock := PagefindFacetsHTML(PagefindFacets{
		Version:  opts.Version,
		Locale:   facetLocale,
		Group:    opts.NavGroup,
		PageType: facetType,
		Target:   opts.DeployTarget,
		Project:  opts.ProjectName,
		Tags:     opts.PageTags,
	}) + PagefindMetaHTML(opts.ProjectName, facetType, opts.DatePublished)

	// A page that declares a type says so in the markup, so a theme can
	// give that kind of page its own treatment without the content having
	// to carry a wrapper of its own. Only the declared frontmatter type
	// counts here: a derived facet type is a search filter, not a design
	// decision.
	pageTypeClass := ""
	if opts.PageType != "" && pageTypeClassRE.MatchString(opts.PageType) {
		pageTypeClass = " page-" + html.EscapeHTML(opts.PageType)
	}

	criticalCSSBlock := ""
	if opts.CriticalCSS != "" {
		criticalCSSBlock = "<style>" + opts.CriticalCSS + "</style>\n"
	}

	narrowLayout := ""
	if opts.PageType == "post" {
		narrowLayout = " docs-layout--narrow"
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n")
	b.WriteString("<html lang=\"" + opts.Lang + "\">\n")
	b.WriteString("<head>\n")
	b.WriteString("<meta charset=\"UTF-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n")
	b.WriteString("<title>" + html.EscapeHTML(DocumentTitle(DocumentTitleParts{
		PageTitle:   opts.Title,
		ProjectName: opts.ProjectName,
		SiteName:    opts.SiteName,
		IsIndexPage: opts.PagePath == "index.html",
	})) + "</title>" + meta.descriptionTag + "\n")
	b.WriteString("<link rel=\"icon\" type=\"image/svg+xml\" href=\"" +
		opts.AssetPrefix + "favicon.svg\">\n")
	b.WriteString(meta.fontTags)
	b.WriteString(criticalCSSBlock)
	b.WriteString("<link rel=\"preload\" href=\"" + opts.CSSHref + "\" as=\"style\">\n")
	b.WriteString("<link rel=\"stylesheet\" href=\"" + opts.CSSHref +
		"\" media=\"print\" onload=\"this.media='all'\">")
	b.WriteString("<noscript><link rel=\"stylesheet\" href=\"" + opts.CSSHref + "\"></noscript>")
	b.WriteString(meta.customCSSTag + meta.feedTag + seoTags + securityMeta + "\n")
	b.WriteString("<script>" + headJS + "</script>\n")
	b.WriteString(gaHeadScript)
	b.WriteString(searchHeadHTML)
	b.WriteString("</head>\n")
	b.WriteString("<body>\n")
	b.WriteString("<a class=\"tm-skip-link\" href=\"#tm-content\">Skip to content</a>\n")
	b.WriteString("<div class=\"reading-progress\" id=\"reading-progress\"></div>\n")
	b.WriteString("<div id=\"tm-app\">\n")
	b.WriteString(sidebarHTML + "\n")
	b.WriteString("<div id=\"tm-main\">\n")
	b.WriteString(topbarHTML + "\n")
	b.WriteString("<main id=\"tm-content\" class=\"content" + pageTypeClass + "\">\n")
	b.WriteString(versionNoticeHTML + "\n")
	b.WriteString("<div class=\"docs-layout" + narrowLayout + "\">\n")
	b.WriteString("<div class=\"docs-main\">\n")
	b.WriteString("<article data-pagefind-body class=\"doc-body\">\n")
	b.WriteString(pagefindBlock + "\n")
	b.WriteString(meta.breadcrumbsHTML + "\n")
	b.WriteString(meta.mobileTOCHTML + "\n")
	b.WriteString(meta.summaryHTML + "\n")
	b.WriteString(meta.autoH1HTML + "\n")
	b.WriteString(bodyHTML + "\n")
	b.WriteString(shareHTML + "\n")
	b.WriteString(meta.footerHTML + "\n")
	b.WriteString("</article>\n")
	b.WriteString("</div>\n")
	b.WriteString(meta.tocAside + "\n")
	b.WriteString("</div>\n")
	b.WriteString("<footer class=\"site-footer\">\n")
	b.WriteString("<p>Built with <a href=\"https://github.com/smm-h/selfdoc\">selfdoc</a></p>\n")
	b.WriteString(meta.feedFooterHTML + "\n")
	b.WriteString("</footer>\n")
	b.WriteString("</main>\n")
	b.WriteString("</div>\n")
	b.WriteString("<div class=\"tm-sr-only\" aria-live=\"polite\" aria-atomic=\"true\"></div>\n")
	b.WriteString("</div>\n")
	b.WriteString("<script>" + bodyJS + "</script>\n")
	b.WriteString(searchDialogHTML + "\n")
	b.WriteString(searchScriptHTML + "\n")
	b.WriteString("</body>\n")
	b.WriteString("</html>\n")
	return b.String(), nil
}
