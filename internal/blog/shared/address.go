package shared

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
)

// PostsSegment is the site-level directory every post is served from:
// "blog/<post-slug>/", at the assembly root and never under a project slug.
//
// It is the build's own posts prefix because the two cannot be allowed to
// disagree: what a project's build emits under "blog/" is what the assembly
// serves at "blog/", moved across unchanged.
const PostsSegment = address.PostsPrefix

// pagePathToURLSegment converts a manifest page path to a URL segment:
// "guide.md" becomes "guide/", "index.md" becomes "", "api/index.md" becomes
// "api/" and "api/reference.md" becomes "api/reference/".
func pagePathToURLSegment(path string) string {
	path = strings.TrimSuffix(path, ".md")
	if path == "index" {
		return ""
	}
	if strings.HasSuffix(path, "/index") {
		return strings.TrimSuffix(path, "/index") + "/"
	}
	return path + "/"
}

// PageTarget is the site-relative address of a project's page
// ("alpha/guide/").
//
// This and [PostTarget] are the one place that decides where a manifest entry
// lives on the assembled site. The blog index, the sitemap, the feed, the
// cross-project link check and the deploy-time verifier all address pages
// through them, so they cannot disagree about where a page is.
//
// home marks the roster's home project: its content root is the site root, so
// its pages carry no project segment at all -- "cv.md" is at "cv/" and its
// "index.md" is the site's front page.
func PageTarget(projectSlug, pagePath string, home bool) string {
	if home {
		return pagePathToURLSegment(pagePath)
	}
	return projectSlug + "/" + pagePathToURLSegment(pagePath)
}

// PostTarget is the site-relative address of a post ("blog/hello").
//
// A post has no project segment: the blog is the site's, one slug namespace
// shared by every project, and "blog/<post-slug>/" is where the build emits a
// post and where the assembly serves it. Which project wrote it is metadata
// the blog index prints, not part of its address.
func PostTarget(postSlug string) string {
	return PostsSegment + "/" + postSlug
}

// TargetOutputPath is the emitted file a site-relative target names.
//
// Both address forms land on the same file: a directory index. The trailing
// slash a page target carries and the one a post target does not are a
// spelling difference, not two addresses.
func TargetOutputPath(target string) string {
	path := strings.Trim(target, "/")
	if path == "" {
		return "index.html"
	}
	return path + "/index.html"
}

// OutputPathTarget is the address form an emitted file has, as a link target.
//
// The inverse of [TargetOutputPath], in the spelling
// [ValidateCrossProjectLinks] recognises: a post keeps no trailing slash,
// everything else gets one.
func OutputPathTarget(relOutput string) string {
	path := strings.ReplaceAll(relOutput, `\`, "/")
	switch {
	case strings.HasSuffix(path, "/index.html"):
		path = strings.TrimSuffix(path, "/index.html")
	case path == "index.html":
		path = ""
	}
	segments := strings.Split(path, "/")
	if len(segments) == 2 && segments[0] == PostsSegment {
		return path
	}
	if path == "" {
		return ""
	}
	return path + "/"
}
