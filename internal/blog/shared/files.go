package shared

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/robots"
	"github.com/stricttools/selfdoc/internal/util"
)

// SitemapPath is the address of the sitemap the site-wide robots.txt names.
//
// The sitemap is written at the site root by the assembly's shared-file
// generator and this is the same file, so the two cannot name different
// documents.
const SitemapPath = "sitemap.xml"

// LLMSPath is where the composed, site-wide llms.txt is served from.
const LLMSPath = "llms.txt"

// DefaultBlogPath is the navigation document's link to the site-wide blog.
const DefaultBlogPath = "/" + PostsSegment + "/"

// DefaultFeedTitle titles the aggregated feed when the caller names none.
const DefaultFeedTitle = "Documentation"

// GenerateNavJSON produces the navigation document for every project.
//
// The home project is not one of them: it is the site root every nav already
// points back to, not an entry in the project set.
//
// blogPath is the URL path of the blog link; pass [DefaultBlogPath] for the
// site's own blog. The document is written in declaration order rather than
// sorted, with two-space indentation, so the bytes are the ones Python's
// json.dumps(nav, indent=2) produced.
func GenerateNavJSON(manifests []map[string]any, blogPath string, homeSlug string) string {
	var out strings.Builder
	out.WriteString("{\n")
	listed := sortedByName(manifests, homeSlug)
	if len(listed) == 0 {
		out.WriteString("  \"projects\": [],\n")
	} else {
		out.WriteString("  \"projects\": [\n")
		for i, manifest := range listed {
			out.WriteString("    {\n")
			fields := [][2]string{
				{"name", util.PythonStrOrEmpty(manifest["name"])},
				{"slug", util.PythonStrOrEmpty(manifest["slug"])},
				{"version", util.PythonStrOrEmpty(manifest["version"])},
			}
			for k, field := range fields {
				out.WriteString("      \"" + field[0] + "\": ")
				out.WriteString(util.PythonJSONString(field[1]))
				if k < len(fields)-1 {
					out.WriteString(",")
				}
				out.WriteString("\n")
			}
			out.WriteString("    }")
			if i < len(listed)-1 {
				out.WriteString(",")
			}
			out.WriteString("\n")
		}
		out.WriteString("  ],\n")
	}
	out.WriteString("  \"blog\": " + util.PythonJSONString(blogPath) + "\n")
	out.WriteString("}")
	return out.String()
}

// GenerateUnifiedFeed produces an Atom XML feed aggregating the posts of every
// project.
//
// docsBase is the base URL of the documentation site. feedTitle titles the
// feed; empty means [DefaultFeedTitle].
//
// Entries are ordered newest first, and the feed-level <updated> carries the
// most recent post's date -- or, for a site with no posts at all, today's,
// because an Atom feed without an <updated> is not a feed.
func GenerateUnifiedFeed(manifests []map[string]any, docsBase, feedTitle string) (string, error) {
	if feedTitle == "" {
		feedTitle = DefaultFeedTitle
	}
	merged, err := MergeProjectPosts(manifests)
	if err != nil {
		return "", err
	}
	entries := make([]build.FeedEntry, 0, len(merged))
	for _, post := range merged {
		postURL := docsBase + "/" + PostTarget(post.Slug) + "/"
		entries = append(entries, build.MakeFeedEntry(post.Title, postURL, post.Date, ""))
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Date > entries[j].Date
	})

	mostRecent := time.Now().Format("2006-01-02")
	if len(entries) > 0 {
		mostRecent = entries[0].Date
	}

	rendered := make([]string, 0, len(entries))
	for _, entry := range entries {
		rendered = append(rendered, entry.XML)
	}
	entryXML := strings.Join(rendered, "\n")
	if entryXML != "" {
		entryXML += "\n"
	}

	return "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n" +
		"<feed xmlns=\"http://www.w3.org/2005/Atom\">\n" +
		"  <title>" + EscapeHTML(feedTitle) + "</title>\n" +
		"  <link href=\"" + docsBase + "/feed.xml\" rel=\"self\"/>\n" +
		"  <link href=\"" + docsBase + "/\"/>\n" +
		"  <id>" + docsBase + "/</id>\n" +
		"  <updated>" + mostRecent + "T00:00:00Z</updated>\n" +
		entryXML +
		"</feed>\n", nil
}

// GenerateSitemap produces a sitemap XML listing every page and post of every
// project.
//
// docsBase is the absolute base URL of the documentation site. It is required
// and absolute: the sitemap protocol has no relative <loc>, and a crawler
// reading "/alpha/guide/" where an absolute URL belongs drops the entry. An
// empty or root-relative base is an error rather than a sitemap that silently
// indexes nothing.
//
// homeSlug is the roster's home project, whose pages are addressed from the
// site root rather than from a project segment.
func GenerateSitemap(manifests []map[string]any, docsBase string, homeSlug string) (string, error) {
	if docsBase == "" ||
		!(strings.HasPrefix(docsBase, "http://") || strings.HasPrefix(docsBase, "https://")) {
		return "", fmt.Errorf(
			"generate_sitemap needs an absolute base URL, got %s. Every "+
				"<loc> is an absolute URL -- the sitemap protocol has no "+
				"relative form -- so a root-relative or empty base produces "+
				"entries every crawler discards.",
			util.PythonRepr(docsBase),
		)
	}
	urls := make([]string, 0)
	for _, manifest := range manifests {
		manifestSlug := util.PythonStrOrEmpty(manifest["slug"])
		isHome := homeSlug != "" && manifestSlug == homeSlug
		for _, page := range dictList(manifest, "pages") {
			url := docsBase + "/" +
				PageTarget(manifestSlug, util.PythonStrOrEmpty(page["path"]), isHome)
			if !strings.HasSuffix(url, "/") {
				url += "/"
			}
			urls = append(urls, url)
		}
	}
	merged, err := MergeProjectPosts(manifests)
	if err != nil {
		return "", err
	}
	for _, post := range merged {
		urls = append(urls, docsBase+"/"+PostTarget(post.Slug)+"/")
	}
	sort.Strings(urls)

	parts := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
	}
	for _, url := range urls {
		parts = append(parts, "  <url><loc>"+EscapeHTML(url)+"</loc></url>")
	}
	parts = append(parts, "</urlset>")
	return strings.Join(parts, "\n") + "\n", nil
}

// GenerateRobotsTxt produces the assembly's robots.txt, naming the site-wide
// sitemap.
//
// Each constituent project's own build writes a robots.txt at its own output
// root, which ends up buried at "<slug>/robots.txt" where no crawler reads it.
// The one that is served is this one, and it carries the same crawler policy
// -- robots.Agents, read from the build that writes the per-project ones, so
// the site cannot allow a crawler its projects disallow or the other way
// round.
func GenerateRobotsTxt(canonicalBase string) string {
	return robots.RenderRobotsTxt(
		strings.TrimRight(canonicalBase, "/") + "/" + SitemapPath,
	)
}

// MissingDescriptionMarker stands in for a project whose manifest states no
// one-line description.
//
// An entry that stopped after the link read as a project with nothing to say
// about itself, and the gap was invisible to the one person who could fill it:
// whoever maintains that project's selfdoc.json. The marker makes it something
// a reader of the published file sees.
const MissingDescriptionMarker = "(no description)"

// GenerateLLMSTxt produces the assembly's llms.txt, composed by reference.
//
// Every constituent project's build writes its own llms.txt listing its own
// pages, and the graft keeps it at "<slug>/llms.txt". The site-wide file links
// to each of those rather than restating them: an inlined copy would be a
// second, staler rendering of a document the project already publishes, and it
// would go out of date on every deploy that is not this one.
//
// The home project is left out for the same reason it is left out of the
// listing: it is the site root the file is served from, not one of the
// projects it points at.
//
// Every entry carries the first line of its project's manifest description.
// A project whose manifest states none gets [MissingDescriptionMarker] rather
// than a bare link, so the gap is visible in the published file instead of
// reading as an entry that simply had nothing to add.
func GenerateLLMSTxt(manifests []map[string]any, canonicalBase string, homeSlug string) string {
	base := strings.TrimRight(canonicalBase, "/")
	listed := sortedByName(manifests, homeSlug)

	lines := []string{
		"# Documentation",
		"",
		"> Every project's documentation is published here. Each entry below " +
			"links to that project's own llms.txt, which lists its pages.",
		"",
		"## Projects",
		"",
	}
	if len(listed) == 0 {
		lines = append(lines, "- No projects are published yet.")
	}
	for _, manifest := range listed {
		name := util.PythonStrOrEmpty(manifest["name"])
		if name == "" {
			name = util.PythonStrOrEmpty(manifest["slug"])
		}
		slug := util.PythonStrOrEmpty(manifest["slug"])
		summary := ""
		description := util.PythonSplitLines(
			util.PythonStrip(util.PythonStrOrEmpty(manifest["description"])),
		)
		if len(description) > 0 {
			summary = description[0]
		}
		if summary == "" {
			summary = MissingDescriptionMarker
		}
		lines = append(lines,
			"- ["+name+"]("+base+"/"+slug+"/"+LLMSPath+"): "+summary)
	}

	lines = append(lines,
		"",
		"## Blog",
		"",
		"- [Blog]("+base+"/"+PostsSegment+"/): posts from every project, "+
			"newest first.",
		"",
	)
	return strings.Join(lines, "\n")
}
