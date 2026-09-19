package page

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/tokenizer"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// PageDates are the dates one page states: when it was first published and
// when it was last changed, each in ISO form and each "" when unstated.
type PageDates struct {
	Published string
	Modified  string
}

// Options is everything one locale-and-version pass of the build hands the
// page renderer.
//
// It mirrors the keyword arguments of the Python function it replaces, with
// two omissions: that function also took the current version and an
// is-latest flag, and read neither.
//
// Start from NewOptions rather than from the zero value. Four settings
// default to true or to a non-empty string in the surface this replaces --
// the previous/next links, the reading-progress line, the glossary page and
// the code-icon style -- so a zero-valued Options turns three features off
// and asks for a code-icon style that does not exist.
type Options struct {
	// MarkdownFiles are this pass's pages, in the order they were walked.
	// The order decides which nav group title wins when two pages in one
	// directory declare different ones, and how two groups whose titles
	// sort equal are ordered.
	MarkdownFiles []SourceFile
	// ProjectName is the project's name. Empty becomes "Documentation".
	ProjectName string
	// Version is the version shown on every page's badge.
	Version string
	// HasCustomCSS says whether the project ships a custom.css for pages to
	// link.
	HasCustomCSS bool
	// Repo is the repository URL the edit links are built from.
	Repo string
	// DocsDirName is the handwritten docs directory, which the edit links
	// prepend to a handwritten page's source path. Empty becomes the
	// layout's own docs directory. A page whose frontmatter declares it
	// generated is edited where selfdoc wrote it, not here.
	DocsDirName string
	// BaseURL is the site's base URL.
	BaseURL string
	// URLBuilder builds the absolute URLs the metadata carries, and answers
	// whether this build is mounted under a shared site.
	URLBuilder urls.URLBuilder
	// Frontmatter maps a page's source path to its parsed frontmatter.
	Frontmatter map[string]util.Frontmatter
	// Lang is the language tag every page declares. Empty becomes "en".
	Lang string
	// PageDates maps a page's source path to its dates.
	PageDates map[string]PageDates
	// Author is the declared author block, which every page's structured
	// data names. A build with none is refused rather than given an
	// invented identity.
	Author map[string]any
	// FeedURL is where the Atom feed sits relative to the output root.
	FeedURL string
	// CriticalCSS is the stylesheet fragment inlined in every head.
	CriticalCSS string
	// TwitterSite is the site's Twitter handle.
	TwitterSite string
	// Search selects the topbar's search trigger: "icon", "bar" or
	// "hidden". Empty means "icon".
	Search string
	// Feedback is the feedback block, nil when the project configures none.
	Feedback map[string]any
	// Branch is the branch the edit links point at. Empty becomes "main".
	Branch string
	// Branding is the landing page's branding block. Non-nil is what turns
	// the hero and the feature grid on for index.md.
	Branding map[string]any
	// ConfigDescription is the project-level description the hero prints.
	ConfigDescription string
	// SiteName is the name of the assembled site these pages are published
	// on, which every document title ends with. Empty is a standalone
	// build: a project deployed on its own has no site above it.
	SiteName string
	// AutoDetect is the auto-detection block the Markdown converter reads.
	AutoDetect map[string]any
	// ThemeMeta is the theme's metadata.
	ThemeMeta *themes.Metadata
	// DeployTarget is the deploy provider, for the target facet and the
	// security meta.
	DeployTarget string
	// RunButton and LineNumbers configure code blocks.
	RunButton   bool
	LineNumbers bool
	// PageNav and PageProgress enable the previous/next links and the
	// reading-progress line.
	PageNav      bool
	PageProgress bool
	// CodeIcons is the code-block icon style.
	CodeIcons string
	// Glossary enables the synthesized glossary page.
	Glossary bool
	// MountLocale, MountProject, MountVersion and MountArchived are the
	// mount coordinates this pass builds under.
	MountLocale   string
	MountProject  string
	MountVersion  string
	MountArchived bool
	// AvailableVersions and AvailableLocales are the configured versions and
	// locales the two pickers offer.
	AvailableVersions []VersionEntry
	AvailableLocales  []LocaleEntry
	// VersionPages maps a version to the pages it has, so the version
	// picker never offers a version that does not hold the page being
	// rendered. Nil means the caller cannot tell them apart and every
	// version is offered.
	VersionPages map[string]map[string]bool
	// CurrentLocale is the locale being built.
	CurrentLocale string
	// SchemaTypes overrides the page-type to schema.org-type map.
	SchemaTypes map[string]string
	// UnversionedPages are the persistent pages every version's sidebar
	// shows, and UnversionedFrontmatter their frontmatter. They reach the
	// navigation and nothing else -- this pass does not render them.
	UnversionedPages       []SourceFile
	UnversionedFrontmatter map[string]util.Frontmatter
}

// NewOptions returns the options a build starts from: every setting whose
// absence means something other than the Go zero value, spelled out.
func NewOptions() Options {
	return Options{
		DocsDirName:  layout.DocsDefault,
		Lang:         "en",
		Branch:       "main",
		PageNav:      true,
		PageProgress: true,
		CodeIcons:    "colorful",
		Glossary:     true,
	}
}

// pageState is one page's state between the two passes: everything the first
// pass computed, held until the term table is complete and the document can
// be wrapped.
type pageState struct {
	htmlPath               string
	addr                   address.PageAddress
	assetPrefix            string
	bodyHTML               string
	navHTML                string
	title                  string
	description            string
	frontmatterDescription string
	cssHref                string
	customCSSHref          string
	tocHTML                string
	breadcrumbs            string
	prevPage               *NavItem
	nextPage               *NavItem
	prefix                 string
	unversionedPrefix      string
	sitePrefix             string
	homeHref               string
	sourcePath             string
	datePublished          string
	dateModified           string
	pageFeedURL            string
	schema                 string
	pageType               string
	pageTags               []string
	glossaryLinks          bool
	navGroup               string
	facetType              string
	pageNumber             int
	totalPages             int
	hasHero                bool
}

// GenerateHTML converts one mount's Markdown pages into full HTML documents.
//
// The result is keyed by each page's output key -- the mount-prefixed path
// the file is written at -- so no caller has to staple the mount on
// afterwards.
//
// Two passes, because a page's body depends on what the other pages declared.
// The first converts each page, applies the post-processing, and collects
// every author-declared term into the site-wide table. The glossary page is
// then synthesized from that table, unless the project ships a glossary page
// of its own or has turned the feature off. The second pass links each
// definition site to its glossary entry and wraps every page in the full
// document.
//
// It refuses a page that declares more than one H1 heading, and one that
// declares neither an H1 nor a frontmatter title: a page's title is what
// every address, anchor and metadata field is built from, so there is nothing
// to guess from.
func GenerateHTML(opts Options) (map[string]string, error) {
	if opts.ProjectName == "" {
		opts.ProjectName = "Documentation"
	}

	contentByPath := make(map[string]string, len(opts.MarkdownFiles))
	for _, src := range opts.MarkdownFiles {
		contentByPath[src.MdPath] = src.Content
	}

	navItems := BuildNav(
		opts.MarkdownFiles, opts.Frontmatter,
		opts.UnversionedPages, opts.UnversionedFrontmatter)

	// The flattened tree is the page iteration order, so the previous and
	// next links match the sidebar.
	flatNav := FlattenNav(navItems)

	// A page's nav group is also its "group" search facet: the sidebar has
	// already decided which group the page belongs to, and that decision is
	// the facet.
	pageGroup := map[string]string{}
	for _, navItem := range navItems {
		if !navItem.IsGroup() {
			continue
		}
		for _, subItem := range navItem.Items {
			pageGroup[subItem.MdPath] = navItem.Group
		}
	}

	// Every HTML path this pass emits, for the breadcrumbs' link validation
	// and for deciding where home is.
	allHTMLPaths := map[string]bool{}
	for _, src := range opts.MarkdownFiles {
		allHTMLPaths[html.MdToHTMLPath(src.MdPath)] = true
	}

	var pages []pageState
	siteTerms := html.NewSiteTerms()

	pageIdx := -1
	for _, navItem := range flatNav {
		mdContent, present := contentByPath[navItem.MdPath]
		if !present {
			continue
		}
		pageIdx++
		mdPath := navItem.MdPath
		htmlPath := html.MdToHTMLPath(mdPath)
		pageMetaFM := opts.Frontmatter[mdPath]

		mdTokens := tokenizer.Tokenize(mdContent)
		var h1Lines []int
		for _, tok := range mdTokens {
			if h, ok := tok.(tokenizer.Heading); ok && h.Level == 1 {
				h1Lines = append(h1Lines, h.Start())
			}
		}
		if len(h1Lines) > 1 {
			locations := make([]string, 0, len(h1Lines))
			for _, ln := range h1Lines {
				locations = append(locations, "line "+strconv.Itoa(ln))
			}
			return nil, fmt.Errorf(
				"%s: multiple H1 headings found (%s). Each page must have "+
					"at most one '# ' heading.",
				mdPath, strings.Join(locations, ", "))
		}
		if len(h1Lines) == 0 && fmString(pageMetaFM, "title") == "" {
			return nil, fmt.Errorf(
				"%s: no title source found. Add a '# Heading' line or set "+
					"'title:' in frontmatter.", mdPath)
		}

		// Every relative reference this page emits comes from its address.
		// Two distinct hops: prefix reaches this page's own mount root
		// (where its sibling pages are), assetPrefix reaches the output
		// root (where the shared assets are). They differ by the mount, so
		// deriving one from the other's depth is what broke asset links.
		addr, err := address.NewPageAddress(htmlPath, address.Coordinates{
			Locale:   opts.MountLocale,
			Project:  opts.MountProject,
			Version:  opts.MountVersion,
			Archived: opts.MountArchived,
		})
		if err != nil {
			return nil, err
		}
		// A page at the site level is served from a different root than the
		// project's own pages, so both project hops go through projectHop.
		prefix := projectHop(opts.URLBuilder, addr, addr.ToMountRoot())
		assetPrefix := addr.ToSiteRoot()
		// Pages marked "versioned: false" are built at the version-free
		// mount, one level shallower than this page's own.
		unversionedPrefix := projectHop(
			opts.URLBuilder, addr, addr.ToStableMountRoot())
		// Posts are mountless site citizens -- see siteLevelHop.
		sitePrefix := siteLevelHop(opts.URLBuilder, addr)
		homeHrefValue, err := homeHref(addr, allHTMLPaths, opts.URLBuilder)
		if err != nil {
			return nil, err
		}

		mdConfig := map[string]any{}
		if len(opts.AutoDetect) > 0 {
			mdConfig["auto_detect"] = opts.AutoDetect
		}
		if opts.RunButton {
			mdConfig["run_button"] = true
		}
		if opts.LineNumbers {
			mdConfig["line_numbers"] = true
		}
		if opts.CodeIcons != "colorful" {
			mdConfig["code_icons"] = opts.CodeIcons
		}
		if len(mdConfig) == 0 {
			mdConfig = nil
		}
		bodyHTML := html.MdToHTML(mdContent, pageMetaFM, mdConfig)
		bodyHTML = html.RewriteInternalLinks(bodyHTML, mdPath, opts.MountArchived)
		navHTML := RenderNav(
			navItems, prefix, htmlPath, unversionedPrefix, sitePrefix)

		title := fmString(pageMetaFM, "title")
		if title == "" {
			title = ExtractTitle(mdContent, opts.ProjectName)
		}

		description := fmString(pageMetaFM, "description")

		// Truncation is abolished: a handwritten frontmatter description is
		// a complete linguistic unit and is emitted verbatim in the meta
		// tag. The advisory SEO length lint still pressures authors to keep
		// descriptions concise; only the silent mutation was removed.
		frontmatterDescription := description

		cssHref := assetPrefix + html.ThemeCSSRel(opts.ThemeMeta)
		customCSSHref := ""
		if opts.HasCustomCSS {
			customCSSHref = assetPrefix + "custom.css"
		}

		// The previous and next pages are read off the flattened tree at
		// this page's own position in it.
		var prevPage, nextPage *NavItem
		if pageIdx > 0 {
			neighbour := flatNav[pageIdx-1]
			prevPage = &neighbour
		}
		if pageIdx < len(flatNav)-1 {
			neighbour := flatNav[pageIdx+1]
			nextPage = &neighbour
		}

		breadcrumbs := ""
		if htmlPath != "index.html" {
			breadcrumbs = buildBreadcrumbs(
				htmlPath, title, prefix, allHTMLPaths, homeHrefValue, sitePrefix)
		}

		tocHTML := buildTOC(bodyHTML)

		docsDirName := opts.DocsDirName
		if docsDirName == "" {
			docsDirName = layout.DocsDefault
		}
		if generated, ok := opts.Frontmatter[mdPath]["generated"].(bool); ok && generated {
			// A generated page's file is in the generated root, so that is
			// where an edit link has to point.
			docsDirName = layout.GeneratedPagesRel
		}
		sourcePath := strings.TrimRight(docsDirName, "/") + "/" + mdPath

		dates := opts.PageDates[mdPath]

		pageFeedURL := ""
		if opts.FeedURL != "" {
			// The feed lives at the output root, not the mount root.
			pageFeedURL = assetPrefix + opts.FeedURL
		}

		schema := fmString(pageMetaFM, "schema")
		pageType := fmString(pageMetaFM, "type")

		// The search facet type, which every page has -- see DerivePageType.
		facetType := DerivePageType(mdPath, pageMetaFM, pageGroup[mdPath])

		pageTags := fmStrings(pageMetaFM, "tags")

		// The page's automatic-term-link opt-out. A page that declares
		// nothing is linked, so absence reads as true.
		glossaryLinks := true
		if declared, ok := pageMetaFM["glossary_links"].(bool); ok {
			glossaryLinks = declared
		}

		// The landing page: the hero and the feature grid go above the body
		// of index.md when the project declares branding. The hero replaces
		// the page summary block.
		hasHero := false
		if htmlPath == "index.html" && opts.Branding != nil {
			hasHero = true
			heroHTML := generateHeroHTML(
				opts.Branding, opts.ProjectName, opts.ConfigDescription, navItems)
			featuresHTML := generateFeaturesHTML(opts.Branding, navItems)
			landingPrefix := heroHTML
			if featuresHTML != "" {
				landingPrefix += "\n" + featuresHTML
			}
			bodyHTML = landingPrefix + "\n" + bodyHTML
		}

		// Who prints the frontmatter description above the H1.
		//
		// The block is presentation of the same string the description meta
		// tag carries. On a reference page that is a useful summary of what
		// the page covers, and it stays. On the home page and on a post it
		// is the page's own opening line said a second time inside one
		// viewport, because both of those open with a lead paragraph an
		// author wrote -- the home page's site description and the post's
		// own first line.
		//
		// The rule reads the page's identity, never its prose: a home page
		// is index.md, a post declares "type: post". Comparing the
		// description against the first paragraph would make the layout
		// change with the wording, which is a rendering nobody can predict.
		if htmlPath == "index.html" || pageType == "post" {
			frontmatterDescription = ""
		}

		// Every term the page declared goes into the site-wide table.
		// Each one was written by an author (or rendered from a definition
		// list or the glossary directive) and carries the id the definition
		// pass gave it, so the glossary's link back has a real target. The
		// first page to declare a term owns it.
		for _, term := range html.CollectDeclaredTerms(bodyHTML) {
			siteTerms.Add(term.Term, htmlPath, term.Anchor, term.Definition)
		}

		pages = append(pages, pageState{
			htmlPath:               htmlPath,
			addr:                   addr,
			assetPrefix:            assetPrefix,
			bodyHTML:               bodyHTML,
			navHTML:                navHTML,
			title:                  title,
			description:            description,
			frontmatterDescription: frontmatterDescription,
			cssHref:                cssHref,
			customCSSHref:          customCSSHref,
			tocHTML:                tocHTML,
			breadcrumbs:            breadcrumbs,
			prevPage:               prevPage,
			nextPage:               nextPage,
			prefix:                 prefix,
			unversionedPrefix:      unversionedPrefix,
			sitePrefix:             sitePrefix,
			homeHref:               homeHrefValue,
			sourcePath:             sourcePath,
			datePublished:          dates.Published,
			dateModified:           dates.Modified,
			pageFeedURL:            pageFeedURL,
			schema:                 schema,
			pageType:               pageType,
			pageTags:               pageTags,
			glossaryLinks:          glossaryLinks,
			navGroup:               pageGroup[mdPath],
			facetType:              facetType,
			pageNumber:             pageIdx + 1,
			totalPages:             len(flatNav),
			hasHero:                hasHero,
		})
	}

	existingHTMLPaths := map[string]bool{}
	for _, pd := range pages {
		existingHTMLPaths[pd.htmlPath] = true
	}
	glossaryBuilt := false
	if opts.Glossary && siteTerms.Len() > 0 && !existingHTMLPaths["glossary/index.html"] {
		var err error
		pages, navItems, err = synthesizeGlossary(
			opts, pages, navItems, siteTerms, allHTMLPaths)
		if err != nil {
			return nil, err
		}
		glossaryBuilt = true
	}

	// The definition site itself becomes the way into the glossary: the
	// <dfn> an author wrote turns into a link to its glossary entry,
	// carrying the definition's first sentence as its tooltip. This only
	// happens where the synthesized glossary page exists -- with no such
	// page there is no entry to promise.
	anyGlossaryAnchor := false
	for _, term := range siteTerms.All() {
		if term.GlossaryAnchor != "" {
			anyGlossaryAnchor = true
			break
		}
	}
	if glossaryBuilt && anyGlossaryAnchor {
		for i := range pages {
			if pages[i].htmlPath == "glossary/index.html" {
				continue
			}
			glossaryURL := html.PathHop(
				"glossary/index.html", pages[i].prefix, pages[i].sitePrefix) +
				html.HTMLPathToURL("glossary/index.html")
			pages[i].bodyHTML = html.LinkDefinitionSites(
				pages[i].bodyHTML, siteTerms, pages[i].htmlPath, glossaryURL)
		}
	}

	htmlFiles := make(map[string]string, len(pages))
	for _, pd := range pages {
		unversionedPrefix := pd.unversionedPrefix
		sitePrefix := pd.sitePrefix
		homeHrefValue := pd.homeHref
		fullHTML, err := WrapPage(WrapOptions{
			BodyHTML:          pd.bodyHTML,
			NavHTML:           pd.navHTML,
			Title:             pd.title,
			ProjectName:       opts.ProjectName,
			SiteName:          opts.SiteName,
			Version:           opts.Version,
			CSSHref:           pd.cssHref,
			CustomCSSHref:     pd.customCSSHref,
			TOCHTML:           pd.tocHTML,
			Breadcrumbs:       pd.breadcrumbs,
			PrevPage:          pd.prevPage,
			NextPage:          pd.nextPage,
			Prefix:            pd.prefix,
			UnversionedPrefix: &unversionedPrefix,
			SitePrefix:        &sitePrefix,
			HomeHref:          &homeHrefValue,
			AssetPrefix:       pd.assetPrefix,
			Repo:              opts.Repo,
			SourcePath:        pd.sourcePath,
			BaseURL:           opts.BaseURL,
			URLBuilder:        opts.URLBuilder,
			PagePath:          pd.htmlPath,
			Description:       pd.description,
			Lang:              opts.Lang,
			DatePublished:     pd.datePublished,
			DateModified:      pd.dateModified,
			Author:            opts.Author,
			FeedURL:           pd.pageFeedURL,
			Summary:           pd.frontmatterDescription,
			CriticalCSS:       opts.CriticalCSS,
			Schema:            pd.schema,
			PageType:          pd.pageType,
			SchemaTypes:       opts.SchemaTypes,
			PageTags:          pd.pageTags,
			GlossaryLinks:     pd.glossaryLinks,
			TwitterSite:       opts.TwitterSite,
			Search:            opts.Search,
			Feedback:          opts.Feedback,
			Branch:            opts.Branch,
			NavGroup:          pd.navGroup,
			FacetType:         pd.facetType,
			SiteTerms:         siteTerms,
			PageNumber:        pd.pageNumber,
			TotalPages:        pd.totalPages,
			ThemeMeta:         opts.ThemeMeta,
			HasHero:           pd.hasHero,
			DeployTarget:      opts.DeployTarget,
			PageNav:           opts.PageNav,
			PageProgress:      opts.PageProgress,
			MountLocale:       opts.MountLocale,
			MountProject:      opts.MountProject,
			MountVersion:      opts.MountVersion,
			MountArchived:     opts.MountArchived,
			AvailableVersions: opts.AvailableVersions,
			AvailableLocales:  opts.AvailableLocales,
			VersionPages:      opts.VersionPages,
			CurrentLocale:     opts.CurrentLocale,
		})
		if err != nil {
			return nil, err
		}
		// Keyed by the output key from the address, so no caller has to
		// staple the mount on afterwards.
		htmlFiles[pd.addr.OutputKey] = fullHTML
	}

	return htmlFiles, nil
}

// synthesizeGlossary builds the glossary page from the terms the pages
// declared and appends it to the page list and to the navigation.
//
// Every declared term appears once, in one alphabetically-sorted definition
// list, with a link back to the page that defined it. The glossary page has
// its own id space -- two pages can each own a "term-x", but one glossary
// page cannot -- so an entry whose natural id is taken gets a counter.
//
// Adding the glossary link to the sidebar changes every page's navigation, so
// each already-converted page's navigation is re-rendered here.
func synthesizeGlossary(
	opts Options,
	pages []pageState,
	navItems []NavItem,
	siteTerms *html.SiteTerms,
	allHTMLPaths map[string]bool,
) ([]pageState, []NavItem, error) {
	glossaryAddr, err := address.NewPageAddress(
		"glossary/index.html", address.Coordinates{
			Locale:   opts.MountLocale,
			Project:  opts.MountProject,
			Version:  opts.MountVersion,
			Archived: opts.MountArchived,
		})
	if err != nil {
		return nil, nil, err
	}
	glossarySitePrefix := siteLevelHop(opts.URLBuilder, glossaryAddr)

	sortedTerms := siteTerms.Sorted()
	var dlItems []string
	glossaryAnchors := map[string]bool{}
	var definedTerms []any
	for _, info := range sortedTerms {
		base := html.TermAnchor(info.Term)
		entryAnchor := base
		for counter := 1; glossaryAnchors[entryAnchor]; counter++ {
			entryAnchor = base + "-" + strconv.Itoa(counter)
		}
		glossaryAnchors[entryAnchor] = true
		info.GlossaryAnchor = entryAnchor
		// HTMLPathToURL gives the URL relative to the root the source page
		// is served from, and the glossary page sits a level inside the
		// project's mount, so the hop back is what makes a Source link land
		// on the page that defined the term. A term defined in a post is
		// served from the site level instead, which under a mount is a
		// different root.
		sourceURL := html.PathHop(
			info.Page, glossaryAddr.ToMountRoot(), glossarySitePrefix) +
			html.HTMLPathToURL(info.Page) + "#" + info.Anchor
		dlItems = append(dlItems,
			`<dt id="`+entryAnchor+`"><dfn>`+html.EscapeHTML(info.Term)+`</dfn></dt>`+
				`<dd>`+info.Definition+` `+
				`<a href="`+sourceURL+`">Source</a></dd>`)
		definedTerms = append(definedTerms, entity(
			"@type", "DefinedTerm",
			"name", info.Term,
			"description", strings.TrimSpace(
				htmlTagRE.ReplaceAllString(info.Definition, "")),
		))
	}

	// The page title's H1 is emitted by the wrapper from the title
	// "Glossary", so only the list itself is body content here.
	glossaryBody := "<div class=\"glossary\"><dl>\n" +
		strings.Join(dlItems, "\n") +
		"\n</dl></div>"

	doc, err := jsonDumps(entity(
		"@context", "https://schema.org",
		"@type", "DefinedTermSet",
		"name", opts.ProjectName+" Glossary",
		"hasDefinedTerm", definedTerms,
	))
	if err != nil {
		return nil, nil, err
	}
	// Appended to the body rather than handed to the SEO builder: the
	// wrapper's SEO section reads the body it is given, so a script element
	// standing in the body reaches the document either way.
	glossaryBody += "\n" + "<script type=\"application/ld+json\">\n" + doc +
		"\n</script>"

	// The glossary joins the sidebar as a top-level link at the bottom.
	navItems = append(navItems, NavItem{
		Label:  "Glossary",
		Path:   "glossary/index.html",
		MdPath: "glossary.md",
	})

	glossaryNavHTML := RenderNav(
		navItems, glossaryAddr.ToMountRoot(), "glossary/index.html",
		glossaryAddr.ToStableMountRoot(), glossarySitePrefix)

	for i := range pages {
		pages[i].navHTML = RenderNav(
			navItems, pages[i].prefix, pages[i].htmlPath,
			pages[i].unversionedPrefix, pages[i].sitePrefix)
	}

	glossaryAssetPrefix := glossaryAddr.ToSiteRoot()
	glossaryCustomCSS := ""
	if opts.HasCustomCSS {
		glossaryCustomCSS = glossaryAssetPrefix + "custom.css"
	}
	glossaryFeedURL := ""
	if opts.FeedURL != "" {
		glossaryFeedURL = glossaryAssetPrefix + opts.FeedURL
	}
	glossaryHomeHref, err := homeHref(glossaryAddr, allHTMLPaths, opts.URLBuilder)
	if err != nil {
		return nil, nil, err
	}

	pages = append(pages, pageState{
		htmlPath:      "glossary/index.html",
		addr:          glossaryAddr,
		assetPrefix:   glossaryAssetPrefix,
		bodyHTML:      glossaryBody,
		navHTML:       glossaryNavHTML,
		title:         "Glossary",
		cssHref:       glossaryAssetPrefix + html.ThemeCSSRel(opts.ThemeMeta),
		customCSSHref: glossaryCustomCSS,
		tocHTML:       buildTOC(glossaryBody),
		breadcrumbs: buildBreadcrumbs(
			"glossary/index.html", "Glossary",
			glossaryAddr.ToMountRoot(), allHTMLPaths,
			glossaryHomeHref, glossarySitePrefix),
		prefix:            glossaryAddr.ToMountRoot(),
		unversionedPrefix: glossaryAddr.ToStableMountRoot(),
		sitePrefix:        glossarySitePrefix,
		homeHref:          glossaryHomeHref,
		pageFeedURL:       glossaryFeedURL,
		pageType:          "glossary",
		facetType:         "glossary",
	})

	return pages, navItems, nil
}

// Generate404Page generates the custom 404 page from the standard page
// template.
//
// The 404 sits at the output root, so its assets need no relative hop -- but
// the pages it links to live under a mount, and the mount coordinates say
// which one. Every page it links to is a current one, so the hop is always to
// the stable mount: the sidebar never points a lost reader into an archived
// version.
//
// It is emitted only by a project that serves its own output root. A 404 is a
// hosting-provider convention answered at the root of what is served, and a
// mounted project's output root is a subdirectory of somebody else's site, so
// the caller is the one that decides whether to ask for one.
func Generate404Page(opts NotFoundOptions) (string, error) {
	if opts.ProjectName == "" {
		opts.ProjectName = "Documentation"
	}

	// The hop from the output root, where 404.html sits, into the stable
	// mount, where every current page lives -- version-scoped or not.
	rootAddr, err := address.NewPageAddress("index.html", address.Coordinates{
		Locale:  opts.MountLocale,
		Project: opts.MountProject,
	})
	if err != nil {
		return "", err
	}
	mountPrefix := rootAddr.Stable
	unversionedPrefix := mountPrefix

	// The 404 sits at the output root, which for the project that emits one
	// is the served root -- only an unmounted project emits one at all --
	// so the site level is right here and the empty hop reaches it.
	navHTML := RenderNav(opts.NavItems, mountPrefix, "404.html", unversionedPrefix, "")

	// The search prompt carries the same class as the topbar's bar-shaped
	// trigger, so it is dressed by the theme rather than by an inline
	// style: the inline one hardcoded a radius and two colour literals that
	// no theme could reach, and opened the dialog through an inline handler
	// that only knew about one search implementation.
	searchHTML := "<p>Try searching for what you need:</p>\n" +
		`<button type="button" class="search-bar-trigger" ` +
		`aria-label="Search documentation">` +
		`<span class="search-bar-text">Search documentation</span>` +
		`</button>`

	flatNav := FlattenNav(opts.NavItems)
	popularHTML := ""
	if len(flatNav) > 0 {
		limit := len(flatNav)
		if limit > 5 {
			limit = 5
		}
		links := make([]string, 0, limit)
		for _, item := range flatNav[:limit] {
			itemPrefix := mountPrefix
			if item.Unversioned {
				itemPrefix = unversionedPrefix
			}
			links = append(links,
				`<li><a href="`+itemPrefix+html.HTMLPathToURL(item.Path)+`">`+
					html.EscapeHTML(item.Label)+`</a></li>`)
		}
		popularHTML = "\n<h2>Popular pages</h2>\n<ul>\n" +
			strings.Join(links, "\n") + "\n</ul>"
	}

	// The H1 is emitted by the wrapper from the title.
	bodyHTML := "<p>The page you are looking for does not exist.</p>\n" +
		`<p><a href="` + mountPrefix + `index.html">Go to the homepage</a></p>` + "\n" +
		searchHTML + popularHTML

	emptySitePrefix := ""
	return WrapPage(WrapOptions{
		BodyHTML:          bodyHTML,
		NavHTML:           navHTML,
		Title:             "Page not found",
		ProjectName:       opts.ProjectName,
		Version:           opts.Version,
		CSSHref:           html.ThemeCSSRel(opts.ThemeMeta),
		CustomCSSHref:     customCSSAtRoot(opts.HasCustomCSS),
		Prefix:            mountPrefix,
		UnversionedPrefix: &unversionedPrefix,
		SitePrefix:        &emptySitePrefix,
		AssetPrefix:       "",
		BaseURL:           opts.BaseURL,
		URLBuilder:        opts.URLBuilder,
		Lang:              opts.Lang,
		FeedURL:           opts.FeedURL,
		CriticalCSS:       opts.CriticalCSS,
		ThemeMeta:         opts.ThemeMeta,
		PageNav:           true,
		PageProgress:      true,
	})
}

// NotFoundOptions is what the 404 page is built from.
//
// It mirrors the keyword arguments of the Python function it replaces, with
// one omission: that function also took the repository URL and never passed
// it on, because the 404 has no source file to offer an edit link for.
type NotFoundOptions struct {
	// ProjectName is the project's name. Empty becomes "Documentation".
	ProjectName string
	// Version is the version shown on the badge.
	Version string
	// HasCustomCSS says whether the project ships a custom.css.
	HasCustomCSS bool
	// NavItems is the navigation tree the sidebar and the popular-pages
	// list are built from.
	NavItems []NavItem
	// BaseURL is the site's base URL.
	BaseURL string
	// URLBuilder builds the absolute URLs the metadata carries.
	URLBuilder urls.URLBuilder
	// Lang is the language tag the page declares. Empty becomes "en".
	Lang string
	// FeedURL is where the Atom feed sits.
	FeedURL string
	// CriticalCSS is the stylesheet fragment inlined in the head.
	CriticalCSS string
	// ThemeMeta is the theme's metadata.
	ThemeMeta *themes.Metadata
	// MountLocale and MountProject are the mount coordinates the current
	// pages sit under.
	MountLocale  string
	MountProject string
}

// customCSSAtRoot is where the project's own stylesheet sits as seen from the
// output root, which is where the 404 page is written.
func customCSSAtRoot(hasCustomCSS bool) string {
	if hasCustomCSS {
		return "custom.css"
	}
	return ""
}
