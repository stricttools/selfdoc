// Package address is the single addressing authority for built pages.
//
// One function -- [NewPageAddress] -- decides, for a page and the locale,
// project and version it belongs to, all four things the rest of the build
// needs:
//
//   - OutputKey: where the page file lands under the output root
//     ("guide/index.html" for the current version, "v/1.0.0/guide/index.html"
//     for a superseded one).
//   - Stable: the version-free URL path for the page ("guide/"). This is
//     where the CURRENT version of every page lives, and it is what every
//     version of the page declares canonical.
//   - Pinned: the version-pinned URL path ("v/1.0.0/guide/"). A superseded
//     version is emitted there; the current version's pinned address is the
//     address it will occupy once a newer version supersedes it.
//   - Depth: how many directory levels the output key sits below the output
//     root, and from it the two relative hops every page needs --
//     [PageAddress.ToSiteRoot] back to the output root, where the shared
//     assets live, and [PageAddress.ToMountRoot] back to this page's own
//     mount, where its sibling pages live.
//
// # The scheme
//
// The current version of every page lives at a stable, unversioned address:
//
//	<locale>/<project>/<page>/
//
// Superseded versions live beside it under the archive prefix "v":
//
//	<locale>/<project>/v/<version>/<page>/
//
// The locale segment is dropped entirely while a site has one locale --
// [LocaleSegment] is the one place that decides it -- and the project segment
// exists only on a unified site. A single-locale standalone site therefore
// mounts its current version at the output root: "guide/".
//
// "v" is reserved. A top-level page named "v" would collide with the archive
// tree, so [NewPageAddress] refuses it.
//
// # Why its own package rather than urls
//
// The urls package turns a path into an absolute URL against a configured base
// (base_url, or a docs base plus a slug). That is a deployment concern -- it
// answers "what does the world call this page". Addressing answers "where does
// this page sit in the output tree, and how does it reach its neighbours",
// which has to be correct with no base URL at all and identical under every
// mount point. Mixing the two is what produced the depth defect this package
// replaces: a site's own asset links must never depend on where the site is
// served from, so they are always document-relative and always derived here.
package address

import (
	"fmt"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// ArchivePrefix is the URL segment every archived (superseded) version is
// emitted under.
const ArchivePrefix = "v"

// PostsPrefix is the site-level URL segment every post is emitted under:
// "blog/<slug>/". Fixed, and the same in a standalone build and on the unified
// site.
const PostsPrefix = "blog"

// IsSiteLevel reports whether path addresses the site level rather than a
// project mount.
//
// Posts are site citizens: they carry no locale, project or version segment,
// and on an assembled site they are served from the site root at
// "blog/<post-slug>/" while the project that wrote them is served under its
// own slug. Every surface that has to tell the two apart -- the URL builder
// deciding whether to write the slug, the sidebar deciding which hop reaches
// an item -- asks here.
//
// It accepts either form the build speaks: an output path
// ("blog/hello/index.html") or a URL path ("blog/hello/", "blog/").
func IsSiteLevel(path string) bool {
	first := strings.TrimLeft(path, "/")
	if idx := strings.Index(first, "/"); idx >= 0 {
		first = first[:idx]
	}
	switch {
	case strings.HasSuffix(first, ".md"):
		first = strings.TrimSuffix(first, ".md")
	case strings.HasSuffix(first, ".html"):
		first = strings.TrimSuffix(first, ".html")
	}
	return first == PostsPrefix
}

// PageAddress is every address a single built page has.
//
// It is a value type with no pointer fields, so a copy is independent of its
// original: that is what stands in for the frozen dataclass this replaces.
// Nothing in the package mutates one after [NewPageAddress] returns it, and a
// caller should not either -- the addresses are consistent with each other
// only as constructed.
type PageAddress struct {
	// PagePath is the mount-relative HTML path, e.g. "guide/index.html".
	PagePath string
	// Locale is the locale segment of the mount -- "" when the site has one
	// locale, which is when the segment is dropped.
	Locale string
	// Project is the constituent-project segment of the mount, "" on a
	// standalone site.
	Project string
	// Version is the version this page was built from, "" for pages that
	// are not version-scoped. A version does not imply a version segment:
	// the current version has none.
	Version string
	// Archived reports whether this page is a superseded version, emitted
	// under the archive prefix instead of at the stable address.
	Archived bool
	// Mount is the output prefix this page is built under.
	Mount string
	// OutputKey is the path of the page file relative to the output root.
	OutputKey string
	// Stable is the version-free URL path for the page -- where its current
	// version lives, and what every version canonicalizes to.
	Stable string
	// Pinned is the version-pinned URL path for the page. It equals Stable
	// for a page that is not version-scoped.
	Pinned string
	// Depth is the number of directory levels between the page and the
	// output root.
	Depth int
}

// URL is the URL path this page is actually emitted at.
func (a PageAddress) URL() string {
	if a.Archived {
		return a.Pinned
	}
	return a.Stable
}

// StableMount is the version-free mount: where the current version's pages
// sit.
func (a PageAddress) StableMount() string {
	return join(a.Locale, a.Project)
}

// ArchiveMount is the mount superseded copies of this page's version sit
// under.
func (a PageAddress) ArchiveMount() string {
	if a.Version == "" {
		return a.StableMount()
	}
	return join(a.Locale, a.Project, ArchivePrefix, a.Version)
}

// ToSiteRoot is the relative hop from this page's directory to the output
// root.
func (a PageAddress) ToSiteRoot() string {
	return hops(a.Depth)
}

// ToMountRoot is the relative hop from this page's directory to its own mount
// root.
func (a PageAddress) ToMountRoot() string {
	return hops(strings.Count(a.PagePath, "/"))
}

// ToStableMountRoot is the relative hop from this page's directory to the
// version-free mount.
//
// On a page emitted at the stable address this is the same hop as
// [PageAddress.ToMountRoot]. On an archive page it climbs two levels further,
// over "v/<version>/".
func (a PageAddress) ToStableMountRoot() string {
	mountDepth := 0
	for _, p := range strings.Split(a.StableMount(), "/") {
		if p != "" {
			mountDepth++
		}
	}
	return hops(a.Depth - mountDepth)
}

// RootPageLink is the link written on one root-level docs page to another
// root-level page.
//
// Every root-level page except index.md is emitted at "<stem>/index.html", so
// a page writing a link is itself inside a directory and a sibling is one
// level up: "../<stem>/". Writing the bare "<stem>/" -- correct back when
// pages were flat "<stem>.html" files -- now resolves inside the writing
// page's own directory and names nothing.
//
// The generated index pages (the API reference and the CLI reference) are the
// callers: both are always at the docs root, which is what makes the single
// hop the right one.
func RootPageLink(mdFilename string) string {
	stem := strings.TrimSuffix(mdFilename, ".md")
	if stem == "index" {
		return "../"
	}
	return "../" + stem + "/"
}

// LocaleSegment is the locale segment a mount carries for localeCode.
//
// A site with one locale has nothing to disambiguate, so it emits no locale
// segment at all; a multi-locale site emits the code. Every caller that turns
// a configured locale into a mount coordinate goes through here, so the two
// cases can never disagree.
//
// locales is the project's full locales config list. The element type is free
// because only its length is read, which keeps this package independent of the
// config package: pass the configured slice directly.
func LocaleSegment[T any](localeCode string, locales []T) string {
	if len(locales) > 1 {
		return localeCode
	}
	return ""
}

// toURL turns an HTML file path into its directory-index URL path:
// "index.html" -> "", "guide/index.html" -> "guide/", "404.html" ->
// "404.html".
func toURL(htmlPath string) string {
	if htmlPath == "index.html" {
		return ""
	}
	if strings.HasSuffix(htmlPath, "/index.html") {
		return strings.TrimSuffix(htmlPath, "index.html")
	}
	return htmlPath
}

// join joins the non-empty path segments with a single slash.
func join(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "/")
}

// mountURL is the URL path for pageURL under mount, keeping the mount's slash.
//
// Not [join]: an index page's URL segment is empty and the mount still needs
// its trailing slash ("en/", not "en").
func mountURL(mount, pageURL string) string {
	if mount != "" {
		return mount + "/" + pageURL
	}
	return pageURL
}

// hops renders n levels of "../", and the empty string for a non-positive n --
// which is Python's "../" * n.
func hops(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("../", n)
}

// Coordinates are the mount coordinates a page is built under. The zero value
// is a single-locale standalone site's unversioned page, which is what the
// Python's keyword defaults expressed.
type Coordinates struct {
	// Locale is the locale segment for this build, "" on a single-locale
	// site -- see [LocaleSegment].
	Locale string
	// Project is the constituent project slug on a unified site, "" on a
	// standalone site.
	Project string
	// Version is the version this page was built from, "" for pages that
	// are not version-scoped.
	Version string
	// Archived is true when this page is a superseded version, which is
	// emitted under "v/<version>/" instead of at the stable address. It
	// requires a version.
	Archived bool
}

// NewPageAddress maps a page and its mount coordinates to every address it
// has.
//
// pagePath is the mount-relative HTML path, e.g. "guide/index.html". It must
// be relative, non-empty, and must not start with the reserved archive segment
// "v/".
func NewPageAddress(pagePath string, coords Coordinates) (PageAddress, error) {
	if pagePath == "" {
		return PageAddress{}, fmt.Errorf("page_path must be a non-empty relative HTML path")
	}
	if strings.HasPrefix(pagePath, "/") {
		return PageAddress{}, fmt.Errorf(
			"page_path must be relative to the mount root, got %s",
			util.PythonRepr(pagePath),
		)
	}
	firstSegment := pagePath
	if idx := strings.Index(firstSegment, "/"); idx >= 0 {
		firstSegment = firstSegment[:idx]
	}
	if firstSegment == ArchivePrefix {
		return PageAddress{}, fmt.Errorf(
			"page path %s starts with the reserved segment %s/, which is "+
				"where superseded versions are emitted. Rename the page.",
			util.PythonRepr(pagePath), util.PythonRepr(ArchivePrefix),
		)
	}
	if coords.Archived && coords.Version == "" {
		return PageAddress{}, fmt.Errorf(
			"archived=True needs a version: an archive address is "+
				"%s/<version>/<page>/ and there is no version to name",
			ArchivePrefix,
		)
	}

	stableMount := join(coords.Locale, coords.Project)
	archiveMount := stableMount
	if coords.Version != "" {
		archiveMount = join(coords.Locale, coords.Project, ArchivePrefix, coords.Version)
	}
	mount := stableMount
	if coords.Archived {
		mount = archiveMount
	}
	outputKey := join(mount, pagePath)
	pageURL := toURL(pagePath)

	return PageAddress{
		PagePath:  pagePath,
		Locale:    coords.Locale,
		Project:   coords.Project,
		Version:   coords.Version,
		Archived:  coords.Archived,
		Mount:     mount,
		OutputKey: outputKey,
		Stable:    mountURL(stableMount, pageURL),
		Pinned:    mountURL(archiveMount, pageURL),
		Depth:     strings.Count(outputKey, "/"),
	}, nil
}
