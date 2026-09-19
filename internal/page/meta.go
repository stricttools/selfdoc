package page

import (
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/prose"
)

// pageMeta is every fragment the page template interpolates, computed once
// from the page's own state.
type pageMeta struct {
	// bodyHTML is the body after cross-page term linking.
	bodyHTML string
	// versionBadge is the version stamp under the sidebar's wordmark.
	versionBadge string
	// customCSSTag links the project's own stylesheet, when it has one.
	customCSSTag string
	// feedTag links the Atom feed.
	feedTag string
	// descriptionTag is the meta description.
	descriptionTag string
	// breadcrumbsHTML is the breadcrumb trail, wrapped in the content header
	// when the header carries anything else.
	breadcrumbsHTML string
	// footerHTML is the page footer.
	footerHTML string
	// tocAside is the desktop table of contents.
	tocAside string
	// mobileTOCHTML is the same table of contents as a disclosure.
	mobileTOCHTML string
	// summaryHTML is the frontmatter description shown above the title.
	summaryHTML string
	// fontTags load the theme's webfonts.
	fontTags string
	// autoH1HTML is the page title rendered as the article's own heading.
	autoH1HTML string
	// feedFooterHTML is the feed link in the site footer.
	feedFooterHTML string
}

// buildPageMeta computes every fragment the page template needs.
//
// opts must already carry the resolved hops: WrapPage fills the three that
// default to another hop before calling.
func buildPageMeta(opts WrapOptions, unversionedPrefix, sitePrefix string) pageMeta {
	var meta pageMeta

	// Cross-page term linking: the first occurrence of a term defined on
	// another page becomes a link to that definition, before the template
	// wraps the body.
	bodyHTML := opts.BodyHTML
	if opts.SiteTerms != nil && opts.SiteTerms.Len() > 0 && opts.PagePath != "" {
		bodyHTML = html.ApplyCrossPageTerms(
			bodyHTML, opts.SiteTerms, opts.PagePath, opts.Prefix, sitePrefix)
	}
	meta.bodyHTML = bodyHTML

	// The framework's badge family has five sanctioned variants and a
	// version stamp is a neutral one. The version-badge class rides along
	// as the hook the build's own rules and assertions name it by.
	if opts.Version != "" {
		meta.versionBadge = `<span class="badge badge-neutral version-badge">` +
			`v` + html.EscapeHTML(opts.Version) + `</span>`
	}
	if opts.CustomCSSHref != "" {
		meta.customCSSTag = "\n<link rel=\"stylesheet\" href=\"" +
			opts.CustomCSSHref + "\">"
	}
	if opts.FeedURL != "" {
		// No title attribute: a single alternate feed needs no
		// disambiguating label, and the name a reader displays comes from
		// the Atom document's own title rather than from this element.
		meta.feedTag = "\n<link rel=\"alternate\" type=\"application/atom+xml\" " +
			"href=\"" + opts.FeedURL + "\">"
	}

	// The meta description falls back to the auto-extracted first sentence
	// when the frontmatter states none -- a complete unit, and the same text
	// og:description carries.
	metaDescription := opts.Description
	if metaDescription == "" {
		metaDescription = prose.FirstSentence(extractFirstParagraph(bodyHTML))
	}
	if metaDescription != "" {
		meta.descriptionTag = "\n<meta name=\"description\" content=\"" +
			html.EscapeHTML(metaDescription) + "\">"
	}

	meta.breadcrumbsHTML = opts.Breadcrumbs

	editLinkHTML := ""
	editURL := ""
	if opts.Repo != "" && opts.SourcePath != "" {
		editURL = strings.TrimRight(opts.Repo, "/") + "/edit/" + opts.Branch +
			"/" + opts.SourcePath
		editLinkHTML = `<a class="edit-link" href="` + editURL +
			`" target="_blank" rel="noopener">Edit this page on GitHub</a>`
	}

	topEditLinkHTML := ""
	if editURL != "" {
		topEditLinkHTML = `<a class="edit-link edit-link-top" href="` + editURL +
			`" target="_blank" rel="noopener">Edit</a>`
	}

	contentDateHTML := ""
	if opts.DateModified != "" {
		contentDateHTML = `<span class="content-date">Updated ` +
			`<time datetime="` + html.EscapeHTML(opts.DateModified) + `">` +
			html.EscapeHTML(formatDateModified(opts.DateModified)) +
			`</time></span>`
	}

	var headerRightParts []string
	if contentDateHTML != "" {
		headerRightParts = append(headerRightParts, contentDateHTML)
	}
	if topEditLinkHTML != "" {
		headerRightParts = append(headerRightParts, topEditLinkHTML)
	}
	headerRightHTML := strings.Join(headerRightParts, "\n")

	switch {
	case meta.breadcrumbsHTML != "" && headerRightHTML != "":
		meta.breadcrumbsHTML = "<div class=\"content-header\">\n" +
			meta.breadcrumbsHTML + "\n" + headerRightHTML + "\n</div>"
	case headerRightHTML != "":
		meta.breadcrumbsHTML = "<div class=\"content-header\">\n" +
			headerRightHTML + "\n</div>"
	}

	pageNavHTML := ""
	if opts.PageNav && (opts.PrevPage != nil || opts.NextPage != nil) {
		prevLink := ""
		nextLink := ""
		if opts.PrevPage != nil {
			// A neighbour marked unversioned lives at the version-free
			// mount, one level up from this page's own; a site-level one
			// lives outside every mount. Same three roots the sidebar
			// spans, so the same rule decides the hop.
			hop := navHop(*opts.PrevPage, opts.Prefix, unversionedPrefix, sitePrefix)
			prevLink = `<a class="page-nav-prev" href="` +
				hop + html.HTMLPathToURL(opts.PrevPage.Path) + `">` +
				`<span class="page-nav-label">Previous</span>` +
				`&larr; ` + html.EscapeHTML(opts.PrevPage.Label) + `</a>`
		}
		progressHTML := ""
		if opts.PageProgress && opts.PageNumber != 0 && opts.TotalPages > 1 {
			progressHTML = `<span class="page-progress">Page ` +
				strconv.Itoa(opts.PageNumber) + ` of ` +
				strconv.Itoa(opts.TotalPages) + `</span>`
		}
		if opts.NextPage != nil {
			hop := navHop(*opts.NextPage, opts.Prefix, unversionedPrefix, sitePrefix)
			nextLink = `<a class="page-nav-next" href="` +
				hop + html.HTMLPathToURL(opts.NextPage.Path) + `">` +
				`<span class="page-nav-label">Next</span>` +
				html.EscapeHTML(opts.NextPage.Label) + ` &rarr;</a>`
		}
		pageNavHTML = `<nav class="page-nav">` + prevLink + progressHTML +
			nextLink + `</nav>`
	}

	feedbackHTML := ""
	if opts.Feedback != nil {
		dataAttrs := ""
		if webhook, _ := opts.Feedback["webhook"].(string); webhook != "" {
			dataAttrs += ` data-webhook="` + html.EscapeHTML(webhook) + `"`
		}
		if ga, _ := opts.Feedback["ga"].(string); ga != "" {
			dataAttrs += ` data-ga="` + html.EscapeHTML(ga) + `"`
		}
		feedbackHTML = `<div class="feedback"` + dataAttrs + `>` +
			`<span>Was this page helpful?</span>` +
			`<button class="feedback-yes" aria-label="Yes">Yes</button>` +
			`<button class="feedback-no" aria-label="No">No</button>` +
			`</div>`
	}

	// A page that already states its own date states it once: a CV whose
	// document declares an updated date closes with its own line, and the
	// footer's generic "Last updated" beside it is the same fact twice, in
	// two formats, from two sources that are free to disagree. The
	// document's own statement is the authority; the footer's stands down.
	// Read off the body rather than off a page-type flag because the CV's
	// closing line is itself conditional -- a CV that declares no updated
	// date has nothing to defer to, and keeps the footer's date.
	dateDisplayHTML := ""
	if opts.DateModified != "" && !strings.Contains(bodyHTML, `class="cv-updated"`) {
		dateDisplayHTML = `<time datetime="` + html.EscapeHTML(opts.DateModified) +
			`">` + html.EscapeHTML(formatDateModified(opts.DateModified)) + `</time>`
	}

	meta.footerHTML = renderPageFooter(
		editLinkHTML, dateDisplayHTML, feedbackHTML, pageNavHTML)

	// A post reads top to bottom and carries no table of contents. That is
	// one decision, so it is taken once here and applies to both elements:
	// the desktop aside and the mobile disclosure are the same feature at
	// two widths, and suppressing only the aside left a table of contents
	// that appeared below 1280px and nowhere else.
	wantsTOC := opts.TOCHTML != "" && opts.PageType != "post"

	// The docs layout's second column, desktop only. The table of contents
	// is itself the framework's nav element, so it needs no wrapper: an
	// aside around it would be a second landmark naming the same region.
	if wantsTOC {
		meta.tocAside = opts.TOCHTML
		meta.mobileTOCHTML = `<details class="mobile-toc">` +
			`<summary>On this page</summary>` + opts.TOCHTML + `</details>`
	}

	if opts.Summary != "" {
		meta.summaryHTML = "<div class=\"page-summary\">\n  <p>" +
			html.EscapeHTML(opts.Summary) + "</p>\n</div>"
	}

	if opts.ThemeMeta != nil && opts.ThemeMeta.FontsURL != "" {
		fontsURL := opts.ThemeMeta.FontsURL
		preconnect := opts.ThemeMeta.FontsPreconnect
		var preconnectLines strings.Builder
		for _, pcURL := range preconnect {
			// The first preconnect is same-origin; the rest are
			// cross-origin and say so.
			if pcURL == preconnect[0] {
				preconnectLines.WriteString(
					`<link rel="preconnect" href="` + pcURL + `">` + "\n")
			} else {
				preconnectLines.WriteString(
					`<link rel="preconnect" href="` + pcURL + `" crossorigin>` + "\n")
			}
		}
		meta.fontTags = preconnectLines.String() +
			`<link rel="preload" href="` + fontsURL + `" as="style">` + "\n" +
			`<link rel="stylesheet" href="` + fontsURL + `" media="print"` +
			` onload="this.media='all'">` +
			`<noscript><link rel="stylesheet" href="` + fontsURL + `"></noscript>` + "\n"
	}

	// The page title becomes the article's own H1. A hero section already
	// provides one, so a landing page gets none from here.
	if !opts.HasHero {
		// One implementation assigns every anchor on the page, the page
		// title's included -- see html.PageTitleAnchor.
		h1Slug := html.PageTitleAnchor(opts.Title)
		h1Readable := html.EscapeHTML(opts.Title)
		h1Anchor := `<a class="heading-link" href="#` + h1Slug + `"` +
			` aria-label="Link to section: ` + h1Readable + `">#</a>`
		meta.autoH1HTML = `<h1 id="` + h1Slug + `">` + h1Anchor +
			html.EscapeHTML(opts.Title) + `</h1>`
	}

	if opts.FeedURL != "" {
		meta.feedFooterHTML = "\n<p><a class=\"feed-link\" href=\"" +
			html.EscapeHTML(opts.FeedURL) + "\">" +
			`<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">` +
			`<circle cx="6.18" cy="17.82" r="2.18"/>` +
			`<path d="M4 4.44v2.83c7.03 0 12.73 5.7 12.73 12.73h2.83c0-8.59-6.97-15.56-15.56-15.56z` +
			`m0 5.66v2.83c3.9 0 7.07 3.17 7.07 7.07h2.83c0-5.47-4.43-9.9-9.9-9.9z"/>` +
			`</svg>` +
			`Subscribe via RSS</a></p>`
	}

	return meta
}
