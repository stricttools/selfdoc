package page

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/cv"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/identity"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/urls"
)

// absentBaseURL is what a missing base URL renders as inside an absolute URL
// this package builds without a URL builder.
//
// The Python surface interpolated its base_url parameter into an f-string
// without checking it, so a caller that passed none published the literal
// "None/guide/" as the page's canonical URL and its og:url. Only a caller
// that states no base URL at all reaches it -- the config's base_url is
// required -- and the pages that do are built by hand in tests, so the
// spelling is reproduced rather than corrected: an output difference here
// would be a difference nothing in the build can produce.
const absentBaseURL = "None"

// baseJoin joins an absolute base URL with a path the way the Python's
// f-string did, including its rendering of an absent base.
func baseJoin(baseURL, path string) string {
	if baseURL == "" {
		return absentBaseURL + "/" + path
	}
	return baseURL + "/" + path
}

var (
	// codeLanguageRE finds the language class a highlighted code block
	// carries, for the SoftwareSourceCode entity's programmingLanguage.
	codeLanguageRE = regexp.MustCompile(`class="language-([\p{L}\p{N}_]+)"`)
	// listItemRE finds each list item of a rendered body, for the ItemList
	// entity. A newline inside an item ends the match, as the pattern it
	// replaces did without the DOTALL flag.
	listItemRE = regexp.MustCompile(`<li>(.*?)</li>`)
	// listItemHrefRE reads the first link out of a list item, which becomes
	// that ListItem's own URL.
	listItemHrefRE = regexp.MustCompile(`<a\s+href="([^"]+)"`)
)

// defaultSchemaTypes maps a page's declared type to the schema.org type its
// structured data states. A type nothing maps is an Article.
var defaultSchemaTypes = map[string]string{
	"guide":     "TechArticle",
	"tutorial":  "TechArticle",
	"post":      "BlogPosting",
	"changelog": "WebPage",
	// A CV page is about a person, and the page's own Person entity is
	// emitted into its body by the cv directive.
	"cv": "ProfilePage",
}

// langToLocale maps a language tag to the Open Graph locale string. A tag
// nothing maps gets the tag plus its own upper-cased form.
var langToLocale = map[string]string{
	"en": "en_US", "es": "es_ES", "fr": "fr_FR", "de": "de_DE",
	"it": "it_IT", "pt": "pt_BR", "ja": "ja_JP", "ko": "ko_KR",
	"zh": "zh_CN", "ru": "ru_RU", "ar": "ar_SA", "fa": "fa_IR",
	"nl": "nl_NL", "pl": "pl_PL", "tr": "tr_TR", "sv": "sv_SE",
}

// SEOOptions is everything the head's SEO block is built from.
//
// It mirrors the keyword arguments of the Python function it replaces, one
// field per argument, with one omission: that function also took the current
// locale and never read it.
type SEOOptions struct {
	// Title is the page's title.
	Title string
	// BaseURL is the site's base URL, "" when the caller states none -- see
	// absentBaseURL for what that publishes.
	BaseURL string
	// PagePath is the page's HTML path. A page with none -- the 404 -- gets
	// no canonical URL, no article entity and no Open Graph block.
	PagePath string
	// Description is the page's frontmatter description.
	Description string
	// BodyHTML is the rendered body, read for the code languages, the
	// declared terms, the list items and the CV's Person payload.
	BodyHTML string
	// Author is the declared author block. The article's author and
	// publisher and the home page's standalone entity all come from it, and
	// a build with none is refused rather than given an invented identity.
	Author map[string]any
	// ProjectName is the project's name.
	ProjectName string
	// SiteName is the name of the assembled site this page is published on,
	// which the social titles carry through [DocumentTitle] exactly as the
	// head's title element does. Empty is a standalone build.
	SiteName string
	// Repo is the repository URL, which the SoftwareSourceCode entity names
	// as the code repository.
	Repo string
	// DatePublished and DateModified are the page's dates, in ISO form.
	DatePublished string
	DateModified  string
	// Lang is the page's language tag.
	Lang string
	// Breadcrumbs is the rendered breadcrumb trail, "" on a page that has
	// none. Its presence is what decides whether a BreadcrumbList is
	// emitted.
	Breadcrumbs string
	// Schema is the frontmatter "schema" declaration, "itemlist" being the
	// one value that emits anything.
	Schema string
	// TwitterSite is the site's Twitter handle.
	TwitterSite string
	// DeployTarget is the deploy provider, which decides the security meta
	// block.
	DeployTarget string
	// AvailableLocales are the configured locales, for the hreflang block.
	AvailableLocales []LocaleEntry
	// MountLocale, MountProject and MountVersion are the mount coordinates
	// the page was built under.
	MountLocale  string
	MountProject string
	MountVersion string
	// URLBuilder builds absolute URLs. With none, one is made from BaseURL.
	URLBuilder urls.URLBuilder
	// PageType is the frontmatter-declared page type, which selects the
	// schema.org type.
	PageType string
	// SchemaTypes overrides the default page-type to schema.org-type map.
	SchemaTypes map[string]string
	// PageTags are the page's frontmatter tags, which become a BlogPosting's
	// keywords.
	PageTags []string
}

// RenderSEOTags builds the head's SEO block: the JSON-LD documents, the Open
// Graph and Twitter Card meta, the canonical link, the hreflang alternates,
// and -- separately -- the security meta a GitHub Pages deploy needs.
//
// The two results are returned apart because the page template writes them in
// that order with nothing between them, and the security block is the one
// part that depends on where the site is hosted rather than on the page.
func RenderSEOTags(opts SEOOptions) (seoTags string, securityMeta string, err error) {
	ub := opts.URLBuilder
	if ub == nil && opts.BaseURL != "" {
		ub = urls.NewSimpleURLBuilder(opts.BaseURL)
	}
	// The social titles carry the document title itself -- the same string
	// the head's title element renders -- so a crawler reading them records
	// what a reader sees in the tab.
	escapedDocumentTitle := html.EscapeHTML(DocumentTitle(DocumentTitleParts{
		PageTitle:   opts.Title,
		ProjectName: opts.ProjectName,
		SiteName:    opts.SiteName,
		IsIndexPage: opts.PagePath == "index.html",
	}))
	escapedProject := html.EscapeHTML(opts.ProjectName)

	// The canonical is the stable address, from every version including the
	// archived ones: one page, one canonical URL, and an archived copy tells
	// a crawler which address supersedes it.
	canonicalURL := ""
	if opts.PagePath != "" {
		addr, addrErr := address.NewPageAddress(opts.PagePath, address.Coordinates{
			Locale:  opts.MountLocale,
			Project: opts.MountProject,
			Version: opts.MountVersion,
		})
		if addrErr != nil {
			return "", "", addrErr
		}
		if ub != nil {
			canonicalURL = ub.PageURL(addr.Stable)
		} else {
			canonicalURL = baseJoin(opts.BaseURL, addr.Stable)
		}
	}

	var b strings.Builder

	if opts.PagePath != "" {
		// One Person across the whole site, from the declared author block.
		// There is no inferred author: a config with no block is refused at
		// load, so reaching here without one is a wiring mistake and says
		// so rather than minting an entity out of a directory name.
		authorObj, authorErr := identity.PersonEntity(opts.Author, false, nil)
		if authorErr != nil {
			return "", "", authorErr
		}

		schemaType := "Article"
		lookup := opts.PageType
		if lookup == "" {
			lookup = "guide"
		}
		if override, ok := opts.SchemaTypes[lookup]; ok {
			schemaType = override
		} else if fallback, ok := defaultSchemaTypes[lookup]; ok {
			schemaType = fallback
		}

		ld := entity(
			"@context", "https://schema.org",
			"@type", schemaType,
			"headline", opts.Title,
			"author", authorObj,
		)
		if canonicalURL != "" {
			ld.Set("url", canonicalURL)
		}
		if opts.Description != "" {
			ld.Set("description", opts.Description)
		}
		if opts.DateModified != "" {
			ld.Set("dateModified", opts.DateModified)
			published := opts.DatePublished
			if published == "" {
				published = opts.DateModified
			}
			ld.Set("datePublished", published)
		}
		// The declared author publishes their own site: one identity,
		// stated in both roles, rather than a second entity invented for
		// the slot.
		ld.Set("publisher", authorObj)
		ld.Set("inLanguage", opts.Lang)
		if schemaType == "BlogPosting" && len(opts.PageTags) > 0 {
			ld.Set("keywords", strings.Join(opts.PageTags, ", "))
		}
		doc, encErr := jsonDumps(ld)
		if encErr != nil {
			return "", "", encErr
		}
		b.WriteString(ldScript(doc))
	}

	if opts.Breadcrumbs != "" && opts.PagePath != "" {
		// Structured data names the address a crawler should keep, which is
		// the stable one -- the same address this page's canonical points
		// at, archived copy or not.
		crumbURL := func(crumbPagePath string) (string, error) {
			stable, addrErr := address.NewPageAddress(
				crumbPagePath, address.Coordinates{
					Locale:  opts.MountLocale,
					Project: opts.MountProject,
				})
			if addrErr != nil {
				return "", addrErr
			}
			if ub != nil {
				return ub.PageURL(stable.Stable), nil
			}
			return baseJoin(opts.BaseURL, stable.Stable), nil
		}

		homeItem, crumbErr := crumbURL("index.html")
		if crumbErr != nil {
			return "", "", crumbErr
		}
		items := []any{entity(
			"@type", "ListItem",
			"position", 1,
			"name", "Home",
			"item", homeItem,
		)}
		logicalPagePath := strings.TrimSuffix(opts.PagePath, "/index.html")
		parts := strings.Split(logicalPagePath, "/")
		for i, dirName := range parts[:len(parts)-1] {
			dirPath := strings.Join(parts[:i+1], "/")
			item, itemErr := crumbURL(dirPath + "/index.html")
			if itemErr != nil {
				return "", "", itemErr
			}
			items = append(items, entity(
				"@type", "ListItem",
				"position", len(items)+1,
				"name", pythonCapitalize(dirName),
				"item", item,
			))
		}
		// The final page entry carries no item URL, per Google's spec.
		items = append(items, entity(
			"@type", "ListItem",
			"position", len(items)+1,
			"name", opts.Title,
		))
		doc, encErr := jsonDumps(entity(
			"@context", "https://schema.org",
			"@type", "BreadcrumbList",
			"itemListElement", items,
		))
		if encErr != nil {
			return "", "", encErr
		}
		b.WriteString(ldScript(doc))
	}

	// The WebSite entity on the home page. It carries no SearchAction: that
	// node advertised a "?q=" URL pattern which serves the same page for
	// every query, so it published a duplicate-content address for each
	// search term and pointed crawlers at it. The site's search is
	// client-side and has no crawlable result URL to advertise.
	if opts.PagePath == "index.html" {
		siteURL := baseJoin(opts.BaseURL, "")
		if ub != nil {
			siteURL = ub.PageURL("")
		}
		doc, encErr := jsonDumps(entity(
			"@context", "https://schema.org",
			"@type", "WebSite",
			"name", opts.ProjectName,
			"url", siteURL,
		))
		if encErr != nil {
			return "", "", encErr
		}
		b.WriteString(ldScript(doc))

		// The standalone entity on the home page: the same declared Person
		// the articles name, emitted once as its own document so a crawler
		// can read the identity without an article around it.
		person, personErr := identity.PersonEntity(opts.Author, true, nil)
		if personErr != nil {
			return "", "", personErr
		}
		personDoc, encErr := jsonDumps(person)
		if encErr != nil {
			return "", "", encErr
		}
		b.WriteString(ldScript(personDoc))
	}

	// A CV page states a Person of its own: the same declared identity, plus
	// what the CV knows about it (the job title, the languages, the
	// schools). The cv directive puts it on the rendered body as an encoded
	// payload, because a directive resolves before the Markdown converter
	// and anything legible would be converted; here it becomes the
	// structured data it always was.
	cvPerson, hasCVPerson, cvErr := cv.ExtractCVPerson(opts.BodyHTML)
	if cvErr != nil {
		return "", "", cvErr
	}
	if hasCVPerson && cvPerson != "" {
		b.WriteString(ldScript(cvPerson))
	}

	if langMatches := codeLanguageRE.FindAllStringSubmatch(opts.BodyHTML, -1); len(langMatches) > 0 {
		var uniqueLangs []string
		seen := map[string]bool{}
		for _, m := range langMatches {
			if !seen[m[1]] {
				seen[m[1]] = true
				uniqueLangs = append(uniqueLangs, m[1])
			}
		}
		var progLang any = uniqueLangs
		if len(uniqueLangs) == 1 {
			progLang = uniqueLangs[0]
		}
		ld := entity(
			"@context", "https://schema.org",
			"@type", "SoftwareSourceCode",
			"name", opts.Title,
			"programmingLanguage", progLang,
		)
		if opts.Repo != "" {
			ld.Set("codeRepository", opts.Repo)
		}
		doc, encErr := jsonDumps(ld)
		if encErr != nil {
			return "", "", encErr
		}
		b.WriteString(ldScript(doc))
	}

	// The DefinedTermSet entity from the page's author-declared terms:
	// definition lists, the glossary directive, and hand-written <dfn>.
	var definedTerms []any
	seenNames := map[string]bool{}
	for _, term := range html.CollectDeclaredTerms(opts.BodyHTML) {
		if seenNames[term.Term] {
			continue
		}
		seenNames[term.Term] = true
		definedTerms = append(definedTerms, entity(
			"@type", "DefinedTerm",
			"name", term.Term,
			"description", term.Definition,
		))
	}
	if len(definedTerms) > 0 {
		doc, encErr := jsonDumps(entity(
			"@context", "https://schema.org",
			"@type", "DefinedTermSet",
			"name", opts.Title+" Glossary",
			"hasDefinedTerm", definedTerms,
		))
		if encErr != nil {
			return "", "", encErr
		}
		b.WriteString(ldScript(doc))
	}

	// ItemList auto-detection: a page that declares no schema and is
	// list-heavy -- more list items than paragraphs, and at least five of
	// them -- is treated as one.
	schema := opts.Schema
	if schema == "" {
		liCount := strings.Count(opts.BodyHTML, "<li>")
		pCount := strings.Count(opts.BodyHTML, "<p>")
		if liCount > pCount && liCount >= 5 {
			schema = "itemlist"
		}
	}

	if schema == "itemlist" {
		liMatches := listItemRE.FindAllStringSubmatch(opts.BodyHTML, -1)
		if len(liMatches) > 0 {
			elements := make([]any, 0, len(liMatches))
			for pos, m := range liMatches {
				liContent := m[1]
				plainText := strings.TrimSpace(
					htmlTagRE.ReplaceAllString(liContent, ""))
				e := entity(
					"@type", "ListItem",
					"position", pos+1,
					"name", plainText,
				)
				if href := listItemHrefRE.FindStringSubmatch(liContent); href != nil {
					e.Set("item", href[1])
				}
				elements = append(elements, e)
			}
			doc, encErr := jsonDumps(entity(
				"@context", "https://schema.org",
				"@type", "ItemList",
				"itemListElement", elements,
			))
			if encErr != nil {
				return "", "", encErr
			}
			b.WriteString(ldScript(doc))
		}
	}

	if opts.PagePath != "" {
		// Fall back to the auto-extracted first sentence when the
		// description is empty -- a complete, naturally bounded unit, and
		// the same text the meta description carries.
		ogDescription := opts.Description
		if ogDescription == "" {
			ogDescription = prose.FirstSentence(extractFirstParagraph(opts.BodyHTML))
		}
		escapedDesc := html.EscapeHTML(ogDescription)
		ogDescTag := ""
		twitterDescTag := ""
		if ogDescription != "" {
			ogDescTag = "\n<meta property=\"og:description\" content=\"" + escapedDesc + "\">"
			twitterDescTag = "\n<meta name=\"twitter:description\" content=\"" + escapedDesc + "\">"
		}

		ogType := "article"
		if opts.PagePath == "index.html" {
			ogType = "website"
		}
		twitterSiteTag := ""
		if opts.TwitterSite != "" {
			twitterSiteTag = "\n<meta name=\"twitter:site\" content=\"" +
				html.EscapeHTML(opts.TwitterSite) + "\">"
		}

		effectiveLang := opts.Lang
		if effectiveLang == "" {
			effectiveLang = "en"
		}
		ogLocale, ok := langToLocale[effectiveLang]
		if !ok {
			ogLocale = effectiveLang + "_" + strings.ToUpper(effectiveLang)
		}

		b.WriteString("\n<meta property=\"og:title\" content=\"" +
			escapedDocumentTitle + "\">" +
			"\n<meta property=\"og:type\" content=\"" + ogType + "\">" +
			"\n<meta property=\"og:site_name\" content=\"" + escapedProject + "\">" +
			"\n<meta property=\"og:locale\" content=\"" + ogLocale + "\">" +
			ogDescTag +
			"\n<meta name=\"twitter:card\" content=\"summary_large_image\">" +
			"\n<meta name=\"twitter:title\" content=\"" +
			escapedDocumentTitle + "\">" +
			twitterDescTag +
			twitterSiteTag)

		slug := strings.ReplaceAll(html.HTMLToMdPath(opts.PagePath), ".md", "")
		// The image's alt text is the description where there is one, and
		// the title otherwise.
		ogImageAlt := opts.Description
		if ogImageAlt == "" {
			ogImageAlt = opts.Title
		}
		ogImageAlt = html.EscapeHTML(ogImageAlt)
		ogImageURL := baseJoin(opts.BaseURL, "og-"+slug+".png")
		if ub != nil {
			ogImageURL = ub.AssetURL("og-" + slug + ".png")
		}
		b.WriteString("\n<meta property=\"og:image\" content=\"" + ogImageURL + "\">" +
			"\n<meta property=\"og:image:type\" content=\"image/png\">" +
			"\n<meta property=\"og:image:width\" content=\"1200\">" +
			"\n<meta property=\"og:image:height\" content=\"630\">" +
			"\n<meta property=\"og:image:alt\" content=\"" + ogImageAlt + "\">" +
			"\n<meta property=\"og:url\" content=\"" + canonicalURL + "\">" +
			"\n<meta name=\"twitter:image\" content=\"" + ogImageURL + "\">")
	}

	if canonicalURL != "" {
		b.WriteString("\n<link rel=\"canonical\" href=\"" + canonicalURL + "\">")
	}

	// The hreflang alternates, only on a site that really has more than one
	// locale -- with one there is no locale segment for them to differ in.
	if len(opts.AvailableLocales) > 1 && opts.PagePath != "" && opts.BaseURL != "" {
		localeHref := func(code string) (string, error) {
			stable, addrErr := address.NewPageAddress(
				opts.PagePath, address.Coordinates{
					Locale:  code,
					Project: opts.MountProject,
				})
			if addrErr != nil {
				return "", addrErr
			}
			if ub != nil {
				return ub.PageURL(stable.Stable), nil
			}
			return baseJoin(opts.BaseURL, stable.Stable), nil
		}

		defaultLocaleCode := ""
		for _, loc := range opts.AvailableLocales {
			if loc.Default {
				defaultLocaleCode = loc.Code
			}
			href, hrefErr := localeHref(loc.Code)
			if hrefErr != nil {
				return "", "", hrefErr
			}
			b.WriteString("\n<link rel=\"alternate\" hreflang=\"" + loc.Code +
				"\" href=\"" + href + "\">")
		}

		// x-default points at the default locale, or at the first one when
		// no locale declares itself the default.
		if defaultLocaleCode == "" {
			defaultLocaleCode = opts.AvailableLocales[0].Code
		}
		href, hrefErr := localeHref(defaultLocaleCode)
		if hrefErr != nil {
			return "", "", hrefErr
		}
		b.WriteString("\n<link rel=\"alternate\" hreflang=\"x-default\" href=\"" +
			href + "\">")
	}

	// Security meta for a GitHub Pages deploy. Cloudflare Pages uses a
	// _headers file instead, and HSTS and Permissions-Policy are not
	// expressible as meta elements at all.
	if opts.DeployTarget == "github-pages" {
		securityMeta = "\n<meta http-equiv=\"X-Content-Type-Options\" content=\"nosniff\">" +
			"\n<meta http-equiv=\"X-Frame-Options\" content=\"DENY\">" +
			"\n<meta http-equiv=\"Content-Security-Policy\" content=\"" +
			"default-src 'self'; " +
			// Only the origins a page really loads from: the build inlines
			// its own JS and CSS and pulls no library from a CDN, so the
			// two script CDNs that used to stand here allowed nothing the
			// site uses and everything an injected script would want. The
			// font origins stay -- the theme's fonts URL loads from them.
			"script-src 'self' 'unsafe-inline'; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
			"font-src 'self' https://fonts.gstatic.com; " +
			"img-src 'self' data: https:; " +
			"connect-src 'self'" +
			"\">"
	}

	return b.String(), securityMeta, nil
}
