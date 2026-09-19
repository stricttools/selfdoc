package page

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/html"
)

// The theme toggle's three glyphs, one per colour-scheme state.
const (
	sunIcon = `<svg class="icon-sun" width="18" height="18" viewBox="0 0 24 24" ` +
		`fill="none" stroke="currentColor" stroke-width="2" ` +
		`stroke-linecap="round" stroke-linejoin="round">` +
		`<circle cx="12" cy="12" r="5"/>` +
		`<line x1="12" y1="1" x2="12" y2="3"/>` +
		`<line x1="12" y1="21" x2="12" y2="23"/>` +
		`<line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/>` +
		`<line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/>` +
		`<line x1="1" y1="12" x2="3" y2="12"/>` +
		`<line x1="21" y1="12" x2="23" y2="12"/>` +
		`<line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/>` +
		`<line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>` +
		`</svg>`
	moonIcon = `<svg class="icon-moon" width="18" height="18" viewBox="0 0 24 24" ` +
		`fill="none" stroke="currentColor" stroke-width="2" ` +
		`stroke-linecap="round" stroke-linejoin="round">` +
		`<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>` +
		`</svg>`
	autoIcon = `<svg class="icon-auto" width="18" height="18" viewBox="0 0 24 24" ` +
		`fill="none" stroke="currentColor" stroke-width="2" ` +
		`stroke-linecap="round" stroke-linejoin="round">` +
		`<circle cx="12" cy="12" r="10"/>` +
		`<path d="M12 2a10 10 0 0 1 0 20z" fill="currentColor"/>` +
		`</svg>`
)

// renderPageFooter assembles the page footer from the edit link, the date,
// the feedback widget and the previous/next navigation.
func renderPageFooter(
	editLinkHTML, dateDisplayHTML, feedbackHTML, pageNavHTML string,
) string {
	var footerParts []string
	var metaParts []string
	if editLinkHTML != "" {
		metaParts = append(metaParts, editLinkHTML)
	}
	if dateDisplayHTML != "" {
		metaParts = append(metaParts, "Last updated "+dateDisplayHTML)
	}
	if len(metaParts) > 0 {
		var spans strings.Builder
		for _, part := range metaParts {
			spans.WriteString("<span>" + part + "</span>")
		}
		footerParts = append(footerParts,
			`<div class="page-meta">`+spans.String()+`</div>`)
	}
	footerParts = append(footerParts, feedbackHTML)
	if pageNavHTML != "" {
		footerParts = append(footerParts, pageNavHTML)
	}
	return `<footer class="page-footer">` +
		strings.Join(footerParts, "") +
		`</footer>`
}

// renderSidebar builds the framework's sidebar: the logo block and the nav.
//
// homeHrefValue is where the site-name link points -- the home page as seen
// from the rendering page, which is not always this mount's own index. The
// version badge sits under the wordmark, in the slot the framework's sheet
// dresses as the tagline.
func renderSidebar(projectName, versionBadge, homeHrefValue, navHTML string) string {
	badgeHTML := ""
	if versionBadge != "" {
		badgeHTML = `<div class="tagline">` + versionBadge + `</div>`
	}
	return `<aside id="tm-sidebar">` + "\n" +
		`<div id="tm-logo">` + "\n" +
		`<a class="wordmark project-name" href="` + homeHrefValue + `">` +
		html.EscapeHTML(projectName) + `</a>` + "\n" +
		badgeHTML + "\n" +
		`</div>` + "\n" +
		`<nav id="tm-nav" aria-label="Site navigation">` + "\n" +
		navHTML + "\n" +
		`</nav>` + "\n" +
		`</aside>`
}

// renderTopbar builds the framework's topbar: the hamburger, the page title
// and the action row.
//
// The project name is not here -- it is the sidebar's wordmark, which is
// where the framework's shell puts it. The two pickers are rendered by
// renderVersionPicker and renderLocalePicker, which is where their links are
// decided.
//
// One deliberate departure from the framework's shell fixture: the page-title
// element is a span, not the fixture's h1. The framework's shell is the whole
// of an application's chrome and its content region carries no heading of its
// own, so the topbar is the page's h1. A selfdoc page is a document: the
// article already opens with a real h1 carrying the page's anchor, and
// repeating it in the chrome would give every page two top-level headings and
// announce its title twice. The id, the class surface and the position in the
// bar are the framework's; only the element differs, and the sheet selects it
// by id either way.
func renderTopbar(
	pageTitle, searchTriggerHTML, versionPickerHTML, localePickerHTML string,
) string {
	return `<header id="tm-topbar">` + "\n" +
		`<button class="tm-hamburger" type="button"` +
		` aria-label="Toggle navigation" aria-expanded="false">` + "\n" +
		html.MenuIcon + "\n" +
		`</button>` + "\n" +
		`<span id="tm-page-title">` + html.EscapeHTML(pageTitle) + `</span>` + "\n" +
		`<span id="tm-page-sub"></span>` + "\n" +
		`<div id="tm-topbar-actions">` + "\n" +
		versionPickerHTML +
		localePickerHTML +
		`<button class="theme-toggle" type="button" aria-label="Toggle theme">` + "\n" +
		sunIcon + moonIcon + autoIcon + "\n" +
		`</button>` + "\n" +
		searchTriggerHTML +
		`</div>` + "\n" +
		`</header>`
}
