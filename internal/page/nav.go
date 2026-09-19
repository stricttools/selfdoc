package page

import (
	"regexp"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// SourceFile is one Markdown source and the docs-relative path it was read
// from.
//
// It stands in for one entry of the insertion-ordered dict the Python surface
// passed around: the order of the slice is the order the pages were walked,
// and three navigation decisions read it (see the package documentation).
type SourceFile struct {
	// MdPath is the path relative to the docs directory, e.g. "api/auth.md".
	MdPath string
	// Content is the Markdown source, with directives already resolved.
	Content string
}

// NavItem is one entry of the sidebar navigation tree.
//
// An entry is either a page or a group, and Group is what tells them apart:
// a page carries Label, Path and MdPath, while a group carries Group, Slug
// and Items. Nothing carries both.
type NavItem struct {
	// Label is the text the sidebar shows for a page.
	Label string
	// Path is the page's HTML path, e.g. "api/auth/index.html".
	Path string
	// MdPath is the page's Markdown source path.
	MdPath string
	// Unversioned marks a page built at the version-free mount, which is
	// one level shallower than a versioned page's own mount.
	Unversioned bool
	// Group is the group's title, empty on a page entry.
	Group string
	// Slug is the group title slugified, for the group's element id.
	Slug string
	// Items are the pages the group holds, in sidebar order.
	Items []NavItem

	// navOrder is the "nav_order" frontmatter value, read while sorting a
	// group's items and dropped from the result.
	navOrder float64
	// date is the "date" frontmatter value rendered as Python's str()
	// would, read while sorting the posts group and dropped from the
	// result.
	date string
	// sortTier and sortOrder are the top-level sort key: pages declaring a
	// "nav_order" sort ahead of those that do not.
	sortTier  int
	sortOrder float64
}

// IsGroup reports whether this entry is a group rather than a page.
func (n NavItem) IsGroup() bool { return n.Group != "" }

var navSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

// BuildNav builds the navigation tree from the Markdown file list.
//
// Top-level pages sort by the "nav_order" frontmatter value (lower first) and
// then alphabetically by source path; a page declaring none sorts after every
// page that does. index.md is always first, and its label is always
// "Home" -- a frontmatter title does not reach the sidebar for the home page.
//
// A page in a subdirectory joins a collapsible group named after the first
// path component: the directory name with hyphens and underscores replaced by
// spaces and then title-cased, unless a page in it declares "nav_group",
// which overrides the title (the last such page in the file list wins).
// Within a group, pages sort by "nav_order" (default 0) and then by source
// path. Groups sort by their title, lower-cased, with two equal titles
// keeping the order their first page appeared in.
//
// unversionedPages are the persistent pages a versioned build shows in every
// version's sidebar. They are appended at the end as up to two groups: the
// pages declaring "type: post" as "Posts", newest first by their "date"
// value, and the rest as "General", sorted like any group. Every item in
// both carries Unversioned. Passing none appends neither group.
func BuildNav(
	markdownFiles []SourceFile,
	frontmatter map[string]util.Frontmatter,
	unversionedPages []SourceFile,
	unversionedFrontmatter map[string]util.Frontmatter,
) []NavItem {
	type groupData struct {
		title string
		items []NavItem
	}
	var ungrouped []NavItem
	groups := map[string]*groupData{}
	var groupOrder []string

	for _, src := range markdownFiles {
		mdPath := src.MdPath
		meta := frontmatter[mdPath]
		label := fmString(meta, "title")
		if label == "" {
			label = strings.ReplaceAll(
				strings.ReplaceAll(mdPath, ".md", ""), "/", " / ")
		}
		item := NavItem{
			Label:  label,
			Path:   html.MdToHTMLPath(mdPath),
			MdPath: mdPath,
		}

		parts := strings.Split(mdPath, "/")
		if len(parts) > 1 {
			dirName := parts[0]
			navGroup := fmString(meta, "nav_group")
			groupTitle := navGroup
			if groupTitle == "" {
				groupTitle = util.TitleCase(strings.ReplaceAll(
					strings.ReplaceAll(dirName, "-", " "), "_", " "))
			}
			item.navOrder = fmNumber(meta, "nav_order")

			if _, ok := groups[dirName]; !ok {
				groups[dirName] = &groupData{title: groupTitle}
				groupOrder = append(groupOrder, dirName)
			}
			if navGroup != "" {
				groups[dirName].title = navGroup
			}
			groups[dirName].items = append(groups[dirName].items, item)
			continue
		}

		if order, ok := fmNumberOK(meta, "nav_order"); ok {
			item.sortTier, item.sortOrder = 0, order
		} else {
			item.sortTier, item.sortOrder = 1, 0
		}
		ungrouped = append(ungrouped, item)
	}

	var nav []NavItem
	var indexItem *NavItem
	var rest []NavItem
	for _, item := range ungrouped {
		if item.MdPath == "index.md" {
			copied := item
			indexItem = &copied
		} else {
			rest = append(rest, item)
		}
	}
	if indexItem != nil {
		indexItem.Label = "Home"
		nav = append(nav, *indexItem)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := rest[i], rest[j]
		if a.sortTier != b.sortTier {
			return a.sortTier < b.sortTier
		}
		if a.sortOrder != b.sortOrder {
			return a.sortOrder < b.sortOrder
		}
		return a.MdPath < b.MdPath
	})
	nav = append(nav, rest...)
	for i := range nav {
		nav[i].sortTier, nav[i].sortOrder = 0, 0
	}

	sortedKeys := append([]string(nil), groupOrder...)
	sort.SliceStable(sortedKeys, func(i, j int) bool {
		return strings.ToLower(groups[sortedKeys[i]].title) <
			strings.ToLower(groups[sortedKeys[j]].title)
	})
	for _, dirName := range sortedKeys {
		data := groups[dirName]
		items := data.items
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].navOrder != items[j].navOrder {
				return items[i].navOrder < items[j].navOrder
			}
			return items[i].MdPath < items[j].MdPath
		})
		for i := range items {
			items[i].navOrder = 0
		}
		nav = append(nav, NavItem{
			Group: data.title,
			Slug: strings.Trim(
				navSlugRE.ReplaceAllString(strings.ToLower(data.title), "-"),
				"-"),
			Items: items,
		})
	}

	if len(unversionedPages) == 0 {
		return nav
	}

	paths := make([]string, 0, len(unversionedPages))
	for _, src := range unversionedPages {
		paths = append(paths, src.MdPath)
	}
	sort.Strings(paths)

	var uvItems, postItems []NavItem
	for _, mdPath := range paths {
		meta := unversionedFrontmatter[mdPath]
		label := fmString(meta, "title")
		if label == "" {
			label = strings.ReplaceAll(
				strings.ReplaceAll(mdPath, ".md", ""), "/", " / ")
		}
		item := NavItem{
			Label:       label,
			Path:        html.MdToHTMLPath(mdPath),
			MdPath:      mdPath,
			Unversioned: true,
			navOrder:    fmNumber(meta, "nav_order"),
			date:        pyStr(meta["date"]),
		}
		if fmString(meta, "type") == "post" {
			postItems = append(postItems, item)
		} else {
			uvItems = append(uvItems, item)
		}
	}

	if len(postItems) > 0 {
		sort.SliceStable(postItems, func(i, j int) bool {
			return postItems[i].date > postItems[j].date
		})
		for i := range postItems {
			postItems[i].navOrder, postItems[i].date = 0, ""
		}
		nav = append(nav, NavItem{
			Group:       "Posts",
			Slug:        "posts",
			Items:       postItems,
			Unversioned: true,
		})
	}

	if len(uvItems) > 0 {
		sort.SliceStable(uvItems, func(i, j int) bool {
			if uvItems[i].navOrder != uvItems[j].navOrder {
				return uvItems[i].navOrder < uvItems[j].navOrder
			}
			return uvItems[i].MdPath < uvItems[j].MdPath
		})
		for i := range uvItems {
			uvItems[i].navOrder, uvItems[i].date = 0, ""
		}
		nav = append(nav, NavItem{
			Group:       "General",
			Slug:        "general",
			Items:       uvItems,
			Unversioned: true,
		})
	}

	return nav
}

// FlattenNav flattens grouped navigation items into a simple page list.
//
// Groups are expanded in place, so the previous/next links a page carries
// cross group boundaries in sidebar order.
func FlattenNav(navItems []NavItem) []NavItem {
	var flat []NavItem
	for _, item := range navItems {
		if item.IsGroup() {
			flat = append(flat, item.Items...)
		} else {
			flat = append(flat, item)
		}
	}
	return flat
}

// mounted reports whether a shared site serves this build under a mount.
//
// It is the nil-safe form of the URL builder's own question, because every
// hop below is computed for builds that have no URL builder at all.
func mounted(ub urls.URLBuilder) bool {
	return ub != nil && ub.Mounted()
}

// homeHref returns the document-relative href to the home page, as seen from
// addr's page.
//
// ownPages is the set of HTML paths this build emits under the same mount.
// Two cases, decided by what exists:
//
//   - The mount has its own index.html. Home is that page -- an archive
//     page's home is the same archived version's index, so a reader browsing
//     v0.1.0 stays in v0.1.0.
//   - It does not, which is what a build of only the site-level or
//     "versioned: false" pages looks like. Home is the current version's
//     index at the stable mount, which every build writes.
//
// Both hops go through projectHop, because the home page is one of the
// project's own: from a post under a site mount a bare hop out lands on the
// SITE's front page, which belongs to whichever project the assembly serves
// at the root -- a link that resolves, to the wrong page.
func homeHref(
	addr address.PageAddress, ownPages map[string]bool, ub urls.URLBuilder,
) (string, error) {
	if ownPages["index.html"] {
		return projectHop(ub, addr, addr.ToMountRoot()) + "index.html", nil
	}
	stableHome, err := address.NewPageAddress("index.html", address.Coordinates{
		Locale:  addr.Locale,
		Project: addr.Project,
	})
	if err != nil {
		return "", err
	}
	return projectHop(ub, addr, addr.ToSiteRoot()) +
		stableHome.Stable + "index.html", nil
}

// siteLevelHop returns where a site-level page (a post) is reached from addr.
//
// Posts carry no mount: they are emitted at "blog/<post-slug>/" under the
// output root, and on an assembled site they are served from the SITE root
// while the project that wrote them is served under its slug. Those are two
// different directories, so which hop is correct depends on whether this
// build is mounted, and on which side of the boundary the rendering page
// sits:
//
//   - Not mounted -- the project's output root is the served root, the posts
//     are inside it, and the document-relative hop back to the output root
//     reaches them.
//   - Mounted, from a project page -- the hop back to the output root reaches
//     the project's own subtree, where the assembly does not put posts.
//     Climbing the mount as well reaches the site root, where it does: the
//     graft moves the whole output under the URL builder's mount prefix, and
//     that is how much further out the site root is.
//   - Mounted, from a post -- the post is already served from the site root,
//     so its own hop out is the site hop unchanged.
//
// Relative in every case, deliberately. An absolute URL here resolves on the
// production host and nowhere else: on a preview or a mirror it still works,
// by leaving the tree the reader is looking at.
func siteLevelHop(ub urls.URLBuilder, addr address.PageAddress) string {
	if !mounted(ub) {
		return addr.ToSiteRoot()
	}
	if address.IsSiteLevel(addr.PagePath) {
		return addr.ToSiteRoot()
	}
	return addr.ToSiteRoot() +
		strings.Repeat("../", strings.Count(ub.MountPrefix(), "/"))
}

// projectHop returns where the project's own pages are reached from addr.
//
// The mirror of siteLevelHop, and it exists for the same reason read the
// other way round. A page at the site level -- a post -- is served from the
// site root, so a relative hop out of it reaches the site root and never the
// project's subtree, where the assembly puts the project's documentation.
// Descending back through the mount prefix is what crosses in.
//
// Off the site level, or with no mount at all, the page and the target share
// a served root and relative is the hop that reaches it: the caller passes
// whichever of the page's relative hops the target belongs to (its own mount,
// or the version-free mount).
func projectHop(
	ub urls.URLBuilder, addr address.PageAddress, relative string,
) string {
	if mounted(ub) && address.IsSiteLevel(addr.PagePath) {
		return addr.ToSiteRoot() + ub.MountPrefix()
	}
	return relative
}

// navHop returns the hop that reaches one sidebar item from the page
// rendering it.
func navHop(item NavItem, prefix, unversionedPrefix, sitePrefix string) string {
	if address.IsSiteLevel(item.Path) {
		return sitePrefix
	}
	if item.Unversioned {
		return unversionedPrefix
	}
	return prefix
}

// RenderNav renders the sidebar navigation HTML.
//
// Page entries render as flat anchors. Group entries render inside a native
// details/summary disclosure reusing the framework's tree-row classes, so the
// scripted and unscripted spellings are painted identically; the group
// holding the active page carries "open" so it auto-expands. Hrefs use clean
// directory URLs ("guide/" rather than "guide/index.html").
//
// Three hops, because the sidebar spans three roots: prefix reaches the
// rendering page's own mount, unversionedPrefix reaches the version-free
// mount where every item carrying the unversioned marker was built, and
// sitePrefix reaches the site level, where the posts are. Inside a version
// the first two differ by one level, and addressing an unversioned page with
// the versioned hop names a file no build ever writes.
func RenderNav(
	navItems []NavItem, prefix, currentPath, unversionedPrefix, sitePrefix string,
) string {
	entry := func(item NavItem, extraClass string) string {
		itemPrefix := navHop(item, prefix, unversionedPrefix, sitePrefix)
		href := itemPrefix + html.HTMLPathToURL(item.Path)
		isActive := item.Path == currentPath
		classes := "nav-item" + extraClass
		current := ""
		if isActive {
			classes += " active"
			current = ` aria-current="page"`
		}
		return `<a class="` + classes + `" href="` + href + `"` + current + `>` +
			`<span class="nav-label">` + html.EscapeHTML(item.Label) + `</span>` +
			`</a>`
	}

	var b strings.Builder
	for _, item := range navItems {
		if !item.IsGroup() {
			b.WriteString(entry(item, ""))
			continue
		}
		openAttr := ""
		for _, sub := range item.Items {
			if sub.Path == currentPath {
				openAttr = " open"
				break
			}
		}
		var subItems strings.Builder
		for _, sub := range item.Items {
			subItems.WriteString(entry(sub, " nav-item-nested"))
		}
		b.WriteString(`<details class="tm-tree-details"` + openAttr + `>` +
			`<summary class="tm-tree-row">` +
			`<span class="tm-tree-twist">` + html.ChevronIcon + `</span>` +
			`<span class="tm-tree-label">` +
			html.EscapeHTML(item.Group) + `</span></summary>` +
			`<div class="tm-tree-group">` +
			subItems.String() +
			`</div>` +
			`</details>`)
	}
	return b.String()
}
