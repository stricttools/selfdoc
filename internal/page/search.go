package page

import (
	"fmt"
	"strings"

	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/util"
)

// PagefindFacetKeys are the facets the corpus carries, in the order they are
// emitted. Every one is a Pagefind filter, so every one is selectable in the
// search UI; "tags" is last because it is the only multi-valued key.
//
// The slice is package state a caller must not write to.
var PagefindFacetKeys = []string{
	"version", "locale", "group", "type", "target", "project", "tags",
}

// PagefindWidgetBundle is the widget bundle a page names when it loads
// Pagefind's own search UI. A tree in which no page names it is a tree the
// widget can be pruned from.
const PagefindWidgetBundle = "pagefind/pagefind-ui.js"

// PagefindUIAssets is every file the Pagefind indexer writes that belongs to
// its own search WIDGET rather than to the index or the query API.
//
// A framework theme draws its own search surface, so these are neither loaded
// nor kept: they would be a payload every deploy carries and no page
// references, and their stylesheets paint rounded corners the framework does
// not allow.
//
// The slice is package state a caller must not write to.
var PagefindUIAssets = []string{
	"pagefind-ui.css",
	"pagefind-ui.js",
	"pagefind-modular-ui.css",
	"pagefind-modular-ui.js",
	"pagefind-component-ui.css",
	"pagefind-component-ui.js",
	"pagefind-highlight.js",
}

// PagefindHeadTags returns the head tags that load the Pagefind UI bundle.
//
// The bundle is what the indexer itself wrote into "pagefind/" at the output
// root, never a CDN copy: a built site answers its own searches with no
// network at all.
//
// assetPrefix is the hop from this page back to the output root, as the
// addressing authority computed it.
func PagefindHeadTags(assetPrefix string) string {
	return `<link href="` + assetPrefix + `pagefind/pagefind-ui.css" rel="stylesheet">` + "\n" +
		`<script src="` + assetPrefix + `pagefind/pagefind-ui.js"></script>` + "\n"
}

// ModuleSpecifier returns path as a specifier a browser will resolve against
// the page.
//
// A module specifier that begins with neither "." nor "/" is a BARE
// specifier, which a browser refuses outright unless an import map defines it
// -- import "js/palette.js" raises "Failed to resolve module specifier". A
// page at the output root is where the hop is empty and the path becomes
// bare, which is to say the front page and every project's landing page. The
// Pagefind bundle path made the same mistake in the same place, which is why
// this is a function rather than a remembered "./".
func ModuleSpecifier(path string) string {
	if strings.HasPrefix(path, ".") || strings.HasPrefix(path, "/") {
		return path
	}
	return "./" + path
}

// ThemeModulesPrefix returns the hop from a page to the framework payload's
// module directory.
//
// Both places a framework theme's stylesheet is written -- a standalone
// build's "css/style.css" and the assembly's
// "_chrome/<theme>-<digest>/css/style.css" -- end in the same relative tail,
// because both are themes.FrameworkCSSRel. Stripping that tail off the
// address the page already carries yields the payload root, and the modules
// sit beside the stylesheet's directory inside it. Deriving it beats
// threading a second address through every page renderer: the two can then
// never disagree about where the payload is.
//
// A stylesheet address that is not a framework theme's is an error, because
// there is no payload to address from it.
func ThemeModulesPrefix(cssHref string) (string, error) {
	if !strings.HasSuffix(cssHref, themes.FrameworkCSSRel) {
		return "", fmt.Errorf(
			"%q is not a framework theme's stylesheet address; a framework "+
				"payload is only addressable from one that ends in %q",
			cssHref, themes.FrameworkCSSRel)
	}
	return cssHref[:len(cssHref)-len(themes.FrameworkCSSRel)] +
		themes.ModulesDir + "/", nil
}

// PaletteSearchScript returns the module script that gives a framework theme
// its search surface.
//
// The framework's command palette replaces Pagefind's shipped widget: the
// widget's stylesheet and bundle are not loaded at all, and the palette
// queries the index through Pagefind's own search() API instead. What the
// reader gets is the framework's own overlay -- keyboard-driven, painted by
// the sheets already on the page -- rather than a second design language
// bolted onto the corner of the site.
//
// The palette ranks what a source returns by subsequence-matching the query
// against each item's label, so a source that pre-filters (as a full-text
// index does) has to return labels the query still matches. That is why the
// label carries the matched excerpt after the page title rather than the
// title alone.
//
// assetPrefix reaches the index; cssHref locates the framework payload the
// modules are served from.
func PaletteSearchScript(assetPrefix, cssHref string) (string, error) {
	modulesPrefix, err := ThemeModulesPrefix(cssHref)
	if err != nil {
		return "", err
	}
	palette := ModuleSpecifier(modulesPrefix + "palette.js")
	indexURL := ModuleSpecifier(assetPrefix + "pagefind/pagefind.js")
	return `<script type="module">` + "\n" +
		`import { openPalette, registerPaletteSource } from "` + palette + `";` + "\n" +
		`let index = null;` + "\n" +
		`async function search(query) {` + "\n" +
		`  if (!query) return [];` + "\n" +
		`  if (!index) {` + "\n" +
		`    index = await import("` + indexURL + `");` + "\n" +
		`    await index.init();` + "\n" +
		`  }` + "\n" +
		`  const found = await index.search(query);` + "\n" +
		`  const top = await Promise.all(` + "\n" +
		`    found.results.slice(0, 8).map((r) => r.data())` + "\n" +
		`  );` + "\n" +
		`  return top.map((d) => {` + "\n" +
		`    const excerpt = String(d.excerpt || "").replace(/<[^>]*>/g, "");` + "\n" +
		`    return {` + "\n" +
		`      label: d.meta && d.meta.title` + "\n" +
		`        ? d.meta.title + " \u2014 " + excerpt` + "\n" +
		`        : excerpt,` + "\n" +
		`      hint: d.url,` + "\n" +
		`      run: () => { window.location.href = d.url; },` + "\n" +
		`    };` + "\n" +
		`  });` + "\n" +
		`}` + "\n" +
		`registerPaletteSource(search);` + "\n" +
		`function open() { openPalette(); }` + "\n" +
		`document.addEventListener("keydown", (e) => {` + "\n" +
		`  if ((e.metaKey || e.ctrlKey) && e.key === "k") {` + "\n" +
		`    e.preventDefault();` + "\n" +
		`    open();` + "\n" +
		`  }` + "\n" +
		`});` + "\n" +
		`for (const t of document.querySelectorAll(` + "\n" +
		`  ".search-trigger, .search-bar-trigger"` + "\n" +
		`)) t.addEventListener("click", open);` + "\n" +
		`</script>`, nil
}

// PagefindFacets are the facet values one page emits for the search index.
type PagefindFacets struct {
	// Version is the version the page was built from.
	Version string
	// Locale is the locale the page was built for.
	Locale string
	// Group is the navigation group the page belongs to.
	Group string
	// PageType is the page's type facet, as DerivePageType decided it.
	PageType string
	// Target is the deploy target the build is producing.
	Target string
	// Project is the project the page belongs to.
	Project string
	// Tags are the page's frontmatter tags, the one multi-valued facet.
	Tags []string
}

// PagefindFacetsHTML returns the hidden facet elements Pagefind reads its
// filters from.
//
// Pagefind takes one data-pagefind-filter per element, so each facet value is
// its own empty element. That is also the shape multi-valued tags need, and
// it means no value is ever escaped into a comma-separated list where a comma
// inside a tag or a nav group name would split it in two.
//
// Empty values are omitted: an empty filter value is a filter group the UI
// offers and nothing matches.
//
// The elements must sit inside the data-pagefind-body region, which is the
// article -- a filter outside the indexed body is not read.
func PagefindFacetsHTML(facets PagefindFacets) string {
	values := map[string]string{
		"version": facets.Version,
		"locale":  facets.Locale,
		"group":   facets.Group,
		"type":    facets.PageType,
		"target":  facets.Target,
		"project": facets.Project,
	}
	var b strings.Builder
	for _, key := range PagefindFacetKeys {
		if key == "tags" {
			continue
		}
		if value := values[key]; value != "" {
			b.WriteString(`<span class="pagefind-facet" ` +
				`data-pagefind-filter="` + key + `:` + html.EscapeHTML(value) + `"></span>`)
		}
	}
	for _, tag := range facets.Tags {
		if tag != "" {
			b.WriteString(`<span class="pagefind-facet" ` +
				`data-pagefind-filter="tags:` + html.EscapeHTML(tag) + `"></span>`)
		}
	}
	return b.String()
}

// DerivePageType returns the "type" facet for a page.
//
// Explicit frontmatter wins; otherwise the type is read off what the page IS
// -- a generated reference page in an API or CLI nav group, a changelog, a
// glossary, or an ordinary guide. Distinct from the frontmatter-only page
// type the layout and the structured data use: every page has a facet type,
// while only a page that declares one gets special layout.
func DerivePageType(mdPath string, pageMeta util.Frontmatter, navGroup string) string {
	if declared := fmString(pageMeta, "type"); declared != "" {
		return declared
	}
	baseName := pythonLower(strings.ReplaceAll(mdPath, ".md", ""))
	generated := fmBoolTrue(pageMeta, "generated")
	if generated && strings.Contains(navGroup, "API") {
		return "api"
	}
	if generated && strings.Contains(navGroup, "CLI") {
		return "cli"
	}
	if strings.Contains(baseName, "changelog") {
		return "changelog"
	}
	if strings.Contains(baseName, "glossary") {
		return "glossary"
	}
	return "guide"
}

// PagefindMetaHTML returns the hidden elements carrying Pagefind result
// metadata.
//
// Metadata is what a result SHOWS, as opposed to what it filters by. One
// element per key for the same reason the facets get one each: an element
// carries a single data-pagefind-meta attribute, and the comma-separated form
// would split a value that contains a comma.
func PagefindMetaHTML(project, pageType, date string) string {
	pairs := [][2]string{
		{"project", project}, {"type", pageType}, {"date", date},
	}
	var b strings.Builder
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		b.WriteString(`<span class="pagefind-facet" ` +
			`data-pagefind-meta="` + pair[0] + `:` + html.EscapeHTML(pair[1]) + `"></span>`)
	}
	return b.String()
}

// PagefindInitScript returns the inline script that initializes the Pagefind
// UI and wires the Cmd+K shortcut.
//
// NO bundlePath is passed, on purpose. The UI derives its own from
// document.currentScript.src at load time, which yields the ROOT-ABSOLUTE
// path of the directory the bundle was loaded from -- "<assetPrefix>pagefind/"
// resolved against the page. That is the correct answer at every depth and
// under every mount, and it is the one thing a build-time string cannot be.
//
// A build-time value was passed here for a long time, computed as
// assetPrefix + "pagefind/", and it was wrong twice over. The UI loads the
// index with a dynamic import(), whose relative specifiers resolve against
// the MODULE's URL -- "<assetPrefix>pagefind/" -- and not against the page.
// So a page at the output root sent "pagefind/", a bare specifier a browser
// refuses outright, and every site's front page and every project's landing
// page returned no search results at all. A page more than one level inside
// its mount sent one "../" too many and fetched another project's index, or a
// 404. The only depths that worked were the ones where the two mistakes
// cancelled.
//
// assetPrefix stays in the signature because the caller has it and the head
// tags beside this one still need it.
func PagefindInitScript(assetPrefix string) string {
	return `<script>` + "\n" +
		`document.addEventListener("DOMContentLoaded", function() {` + "\n" +
		`  new PagefindUI({ element: "#pagefind-container", showSubResults: true, showImages: false });` + "\n" +
		`  var dialog = document.getElementById("search-dialog");` + "\n" +
		`  function openSearch() {` + "\n" +
		`    if (dialog.open) return;` + "\n" +
		`    dialog.showModal();` + "\n" +
		`    var input = dialog.querySelector(".pagefind-ui__search-input");` + "\n" +
		`    if (input) input.focus();` + "\n" +
		`  }` + "\n" +
		`  var triggers = document.querySelectorAll(".search-trigger, .search-bar-trigger");` + "\n" +
		`  for (var i = 0; i < triggers.length; i++) {` + "\n" +
		`    triggers[i].addEventListener("click", openSearch);` + "\n" +
		`  }` + "\n" +
		`  document.addEventListener("keydown", function(e) {` + "\n" +
		`    if ((e.metaKey || e.ctrlKey) && e.key === "k") {` + "\n" +
		`      e.preventDefault();` + "\n" +
		`      if (dialog.open) { dialog.close(); } else {` + "\n" +
		`        dialog.showModal();` + "\n" +
		`        var input = dialog.querySelector(".pagefind-ui__search-input");` + "\n" +
		`        if (input) input.focus();` + "\n" +
		`      }` + "\n" +
		`    }` + "\n" +
		`    if (e.key === "Escape" && dialog.open) { dialog.close(); }` + "\n" +
		`  });` + "\n" +
		`  var closeBtn = dialog.querySelector(".search-close");` + "\n" +
		`  if (closeBtn) closeBtn.addEventListener("click", function() { dialog.close(); });` + "\n" +
		`  dialog.addEventListener("click", function(e) {` + "\n" +
		`    if (e.target === dialog) dialog.close();` + "\n" +
		`  });` + "\n" +
		`});` + "\n" +
		`</script>`
}

// PagefindDialogHTML returns the search dialog the Pagefind UI mounts into.
//
// The dialog itself is chrome: the input, the results list and the filter
// controls are all rendered by the Pagefind UI inside "#pagefind-container".
func PagefindDialogHTML() string {
	return `<dialog class="search-dialog" id="search-dialog" aria-label="Search documentation">` + "\n" +
		`<div class="search-inner">` + "\n" +
		`<div class="search-header">` + "\n" +
		`<span class="search-header-title">Search</span>` + "\n" +
		`<button class="search-close" aria-label="Close search" type="button">X</button>` + "\n" +
		`</div>` + "\n" +
		`<div id="pagefind-container"></div>` + "\n" +
		`</div>` + "\n" +
		`</dialog>`
}
