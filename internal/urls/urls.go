// Package urls builds absolute URLs from relative paths, decoupling URL
// generation from a hardcoded base_url and supporting locale-prefixed and
// versioned paths.
//
// Project identification uses two identifiers, and they are not
// interchangeable:
//
//   - slug: the machine identifier (URL-safe, lowercase, hyphens) used in
//     URLs, directory names, cross-references, frontmatter and manifest keys.
//   - name: the human-readable display name (which may carry spaces, capitals
//     and special characters) used in UI, homepages and documentation.
package urls

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
)

// URLBuilder builds absolute URLs from relative paths.
//
// Two implementations exist: [SimpleURLBuilder] for a project that is its own
// site, and [TopologyURLBuilder] for one a shared site mounts under a slug.
type URLBuilder interface {
	// PageURL returns the absolute URL for a page path, so "guide/" becomes
	// "https://example.com/guide/".
	PageURL(path string) string
	// AssetURL returns the absolute URL for an asset path, e.g.
	// "og-index.png".
	AssetURL(path string) string
	// FeedURL returns the absolute URL for the Atom feed.
	FeedURL() string
	// Base returns the base URL string, with no trailing slash.
	Base() string
	// Mounted reports whether this project is served under a shared site's
	// mount.
	//
	// A mounted project's own output root is not the served root: the site
	// serves it under its slug, and serves the site-level pages (posts)
	// from the site root instead. The two roots are different directories,
	// so a reference crossing between them has to climb out of one and back
	// into the other. Every surface that writes such a reference asks here
	// rather than sniffing config.
	Mounted() bool
	// MountPrefix returns the path segments the site serves this project
	// under: "" for a project that is its own site, "<slug>/" for one the
	// site mounts.
	//
	// This is what turns a hop to the PROJECT's output root into a hop to
	// the SITE's root, and back: a reference crossing the mount boundary is
	// still document-relative, which is what lets the same tree resolve on
	// production, on a local preview and on a mirror. An absolute URL there
	// resolves on one host only.
	MountPrefix() string
	// SiteRoot returns the URL of the served site's root, with a trailing
	// slash.
	//
	// For a standalone project that is its own base; for a mounted one it is
	// the shared site's base, above this project's slug. It is absolute, so
	// it belongs to metadata -- a visible link crosses the mount with
	// MountPrefix instead.
	SiteRoot() string
}

// SimpleURLBuilder joins a base URL with paths.
//
// It strips trailing slashes from the base URL and handles path joining, so
// base plus "/" plus path never produces a double slash.
type SimpleURLBuilder struct {
	baseURL string
}

// NewSimpleURLBuilder returns a builder for a project served at baseURL, whose
// trailing slashes are stripped once, here.
func NewSimpleURLBuilder(baseURL string) *SimpleURLBuilder {
	return &SimpleURLBuilder{baseURL: strings.TrimRight(baseURL, "/")}
}

// PageURL returns the absolute URL for a page path.
func (b *SimpleURLBuilder) PageURL(path string) string {
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return b.baseURL + "/"
	}
	return b.baseURL + "/" + path
}

// AssetURL returns the absolute URL for an asset path.
func (b *SimpleURLBuilder) AssetURL(path string) string {
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return b.baseURL + "/"
	}
	return b.baseURL + "/" + path
}

// FeedURL returns the absolute URL for the Atom feed.
func (b *SimpleURLBuilder) FeedURL() string {
	return b.baseURL + "/feed.xml"
}

// Base returns the base URL string, with no trailing slash.
func (b *SimpleURLBuilder) Base() string {
	return b.baseURL
}

// Mounted reports false: a standalone project's output root is what is served.
func (b *SimpleURLBuilder) Mounted() bool {
	return false
}

// MountPrefix returns "": with no mount, the project's output root already is
// the site root.
func (b *SimpleURLBuilder) MountPrefix() string {
	return ""
}

// SiteRoot returns the served root, which for a standalone project is its own
// base.
func (b *SimpleURLBuilder) SiteRoot() string {
	return b.baseURL + "/"
}

// TopologyURLBuilder builds URLs for a topology-aware multi-project
// deployment, incorporating the project slug under a shared docs base.
//
// With a docs base of "https://docs.smmh.dev" and a slug of "selfdoc",
// PageURL("guide/") returns "https://docs.smmh.dev/selfdoc/guide/".
//
// Site-level pages are the exception, and the reason this type rather than its
// callers decides: a post is a citizen of the site, not of the project that
// wrote it. The site serves every project's posts from one shared "blog/" at
// the site root, so PageURL("blog/hello/") returns
// "https://docs.smmh.dev/blog/hello/" with no slug segment. Assets keep the
// slug -- a post's OG card, stylesheet and search index are the project's own
// files and stay in the project's subtree.
type TopologyURLBuilder struct {
	docsBase string
	slug     string
}

// NewTopologyURLBuilder returns a builder for a project the site at docsBase
// serves under slug.
func NewTopologyURLBuilder(docsBase, slug string) *TopologyURLBuilder {
	return &TopologyURLBuilder{
		docsBase: strings.TrimRight(docsBase, "/"),
		slug:     slug,
	}
}

// PageURL returns the absolute URL for a page path, under this project's slug
// unless the path is site-level -- see the type's own documentation.
func (b *TopologyURLBuilder) PageURL(path string) string {
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return b.docsBase + "/" + b.slug + "/"
	}
	if address.IsSiteLevel(path) {
		return b.docsBase + "/" + path
	}
	return b.docsBase + "/" + b.slug + "/" + path
}

// AssetURL returns the absolute URL for an asset path under this project's
// slug.
func (b *TopologyURLBuilder) AssetURL(path string) string {
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return b.docsBase + "/" + b.slug + "/"
	}
	return b.docsBase + "/" + b.slug + "/" + path
}

// FeedURL returns the absolute URL for the Atom feed.
func (b *TopologyURLBuilder) FeedURL() string {
	return b.docsBase + "/" + b.slug + "/feed.xml"
}

// Base returns the base URL string -- the docs base plus the slug, with no
// trailing slash.
func (b *TopologyURLBuilder) Base() string {
	return b.docsBase + "/" + b.slug
}

// Mounted reports true: the site serves a topology project under its slug.
func (b *TopologyURLBuilder) Mounted() bool {
	return true
}

// MountPrefix returns the slug segment the site serves this project's output
// under.
func (b *TopologyURLBuilder) MountPrefix() string {
	return b.slug + "/"
}

// SiteRoot returns the shared site's root, above this project's slug.
func (b *TopologyURLBuilder) SiteRoot() string {
	return b.docsBase + "/"
}

// CrossProjectURL builds a URL to another project's content: the docs base,
// the project's slug, and the path.
//
// Every project the site serves is mounted under its own slug, so the slug is
// the whole address and there is nothing to declare per project. A slug the
// site does not serve is not an address this function can tell apart from one
// it does -- the assembly's verification is what refuses a link naming a
// project the roster does not carry.
func (b *TopologyURLBuilder) CrossProjectURL(projectSlug, path string) string {
	base := b.docsBase + "/" + projectSlug
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return base + "/"
	}
	return base + "/" + path
}
