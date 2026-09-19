package sitedirectives

import (
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/util"
)

// RenderBlogHighlights returns the limit most recent posts across every
// project.
//
// siteHop is the rendering page's hop back to the site root; every post link
// is written against it rather than against the site's base URL, so the
// highlights lead into the tree the reader is on.
func RenderBlogHighlights(manifests []map[string]any, siteHop string, limit int) (string, error) {
	posts, err := shared.MergeProjectPosts(manifests)
	if err != nil {
		return "", err
	}
	sort.SliceStable(posts, func(i, j int) bool {
		if posts[i].Date != posts[j].Date {
			return posts[i].Date > posts[j].Date
		}
		return posts[i].Slug > posts[j].Slug
	})
	parts := []string{`<section class="blog-highlights">`}
	if len(posts) == 0 {
		parts = append(parts, "  <p>No posts yet.</p>")
	}
	if len(posts) > limit {
		posts = posts[:limit]
	}
	for _, post := range posts {
		href := siteHop + shared.PostTarget(post.Slug) + "/"
		parts = append(parts, `  <article class="blog-entry">`)
		parts = append(parts, "    <time>"+shared.EscapeHTML(post.Date)+"</time>")
		parts = append(parts, `    <span class="project-name">`+
			shared.EscapeHTML(post.ProjectName)+"</span>")
		parts = append(parts, `    <a href="`+shared.EscapeHTML(href)+`">`+
			shared.EscapeHTML(post.Title)+"</a>")
		parts = append(parts, "  </article>")
	}
	parts = append(parts, `  <p><a href="`+shared.EscapeHTML(siteHop)+
		`blog/">All posts</a></p>`)
	parts = append(parts, "</section>")
	return strings.Join(parts, "\n"), nil
}

// ResolveProjectsCards returns the curated project cards.
//
// The directive takes no attributes at all: what the listing holds is declared
// in the home project's docs/projects.toml, so a marker that states anything
// is stating it in the wrong place. A context with no listing is refused too,
// naming that file -- the alternative is a front page publishing a listing
// nobody curated.
func ResolveProjectsCards(attrs map[string]string, context SiteContext) (string, error) {
	if unknown := sortedKeys(attrs); len(unknown) > 0 {
		return "", errorf(
			"directive 'projects-cards' takes no attributes, got %s. The "+
				"listing's content is declared in the home project's "+
				"docs/projects.toml, not on the marker.",
			strings.Join(unknown, ", "),
		)
	}
	if context.Listing == nil {
		return "", errorf(
			"directive 'projects-cards' renders the curated project listing, " +
				"which the home project declares in docs/projects.toml. This " +
				"project declares none.",
		)
	}
	rendered, err := listing.RenderHTML(
		*context.Listing, context.Manifests, context.SiteHop,
		context.HomeSlug, "",
	)
	if err != nil {
		return "", err
	}
	return rendered, nil
}

// ResolveBlogHighlights returns the newest posts across every project.
//
// limit is required and has no default: how many recent posts the front page
// shows is an editorial decision, and a number invented here would be one
// nobody made.
func ResolveBlogHighlights(attrs map[string]string, context SiteContext) (string, error) {
	unknown := make([]string, 0, len(attrs))
	for _, key := range sortedKeys(attrs) {
		if key != "limit" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return "", errorf(
			"directive 'blog-highlights' declares unknown attribute(s) %s. "+
				"It takes 'limit'.",
			strings.Join(unknown, ", "),
		)
	}
	raw := attrs["limit"]
	if raw == "" {
		return "", errorf(
			"directive 'blog-highlights' requires limit=\"N\": how many " +
				"recent posts the front page shows is an editorial decision " +
				"with no default.",
		)
	}
	limit, valid := util.ParsePythonInt(raw)
	if !valid {
		return "", errorf(
			"directive 'blog-highlights': limit must be a whole number, got %s.",
			util.PythonRepr(raw),
		)
	}
	if limit < 1 {
		return "", errorf(
			"directive 'blog-highlights': limit must be at least 1, got %d.",
			limit,
		)
	}
	return RenderBlogHighlights(context.Manifests, context.SiteHop, int(limit))
}

// RenderDirectiveBody returns the HTML a site-level directive's region holds.
//
// It reports an error for an unknown name, a missing required attribute, or a
// context that cannot answer the directive (no curated listing).
func RenderDirectiveBody(name string, attrs map[string]string, context SiteContext) (string, error) {
	switch name {
	case "projects-cards":
		return ResolveProjectsCards(attrs, context)
	case "blog-highlights":
		return ResolveBlogHighlights(attrs, context)
	}
	return "", errorf(
		"unknown site-level directive %s; selfdoc resolves %s.",
		util.PythonRepr(name), strings.Join(SiteDirectives, ", "),
	)
}

// ResolveForBuild resolves a site-level directive during a build of the home
// project.
//
// This is what [Directives] binds, and the refusal below is what a
// registration with no context renders: the manifests are what a version badge
// and a post highlight are read from, and no project's own repository holds
// them, so a build that never received them cannot resolve the directive and
// says which command supplies them.
func ResolveForBuild(name string, attrs map[string]string, context *SiteContext) (string, error) {
	if context == nil {
		return "", errorf(
			"directive '%s' is site-level: it renders from the assembled "+
				"site's manifests, which no single project's build can see on "+
				"its own. Build the home project with `selfdoc build "+
				"--target home --site-manifests <dir>`, which supplies them.",
			name,
		)
	}
	return RenderRegion(name, attrs, *context)
}

// Directives returns the resolver registration for the site-level directives,
// each bound to context.
//
// A build reaches this package through the returned functions and through
// nothing else: the context is captured here rather than passed through the
// config document, which stays a document. A nil context registers the names
// with the refusal [ResolveForBuild] renders, which is what a caller that has
// no assembly data gets instead of a silently empty region.
func Directives(context *SiteContext) map[string]resolver.BuiltinDirective {
	registered := make(map[string]resolver.BuiltinDirective, len(SiteDirectives))
	for _, name := range SiteDirectives {
		directive := name
		registered[directive] = func(attrs map[string]string, body []string) (string, error) {
			return ResolveForBuild(directive, attrs, context)
		}
	}
	return registered
}

// sortedKeys is a map's keys in sorted order, which is what every diagnostic
// here names them in.
func sortedKeys(attrs map[string]string) []string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// LazyDirectives returns the site-level directive registration bound to a
// context resolved on first use.
//
// A caller that would have to reach the network to answer these directives
// registers through here rather than through [Directives]: the read happens
// when a page actually carries one of the markers, and a project whose pages
// carry none pays nothing. resolve is called at most once, and its answer --
// value or error -- is what every marker on every page of that run sees.
//
// A resolve that fails names the directive it failed to answer, because the
// reader of the diagnostic is looking at a page that carries a marker, not at
// a command that fetched something.
func LazyDirectives(
	resolve func() (*SiteContext, error),
) map[string]resolver.BuiltinDirective {
	var (
		once     bool
		resolved *SiteContext
		failure  error
	)
	answer := func() (*SiteContext, error) {
		if !once {
			once = true
			resolved, failure = resolve()
		}
		return resolved, failure
	}
	registered := make(map[string]resolver.BuiltinDirective, len(SiteDirectives))
	for _, name := range SiteDirectives {
		directive := name
		registered[directive] = func(attrs map[string]string, body []string) (string, error) {
			context, err := answer()
			if err != nil {
				return "", errorf(
					"directive '%s' is site-level and could not be "+
						"resolved: %s",
					directive, err,
				)
			}
			return ResolveForBuild(directive, attrs, context)
		}
	}
	return registered
}
