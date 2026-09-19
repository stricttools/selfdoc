package shared

import (
	"fmt"
	"sort"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/util"
)

// MergedPost is one post as the assembly sees it: the post's own metadata plus
// the project whose manifest carried it.
//
// It replaces the untyped dictionary the Python merge produced. Every field is
// the string form of the manifest's value, so a manifest that declares a date
// as a number still renders.
type MergedPost struct {
	// Date is the post's publication date, in ISO form.
	Date string
	// Title is the post's title, unescaped.
	Title string
	// Slug is the post's site-level address segment.
	Slug string
	// ProjectName is the display name of the project that published it.
	ProjectName string
	// ManifestSlug is the slug of the project that published it.
	ManifestSlug string
}

// dictList reads a manifest key that holds a list of objects, skipping any
// element that is not one.
//
// Manifests arrive decoded from JSON, so a list is []any of map[string]any;
// a key that is absent, null or another type yields nothing, which is what
// Python's "m.get(key) or []" did.
func dictList(manifest map[string]any, key string) []map[string]any {
	raw, ok := manifest[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, element := range raw {
		if entry, ok := element.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

// MergeProjectPosts returns every project's posts as one list, refusing a slug
// collision.
//
// Posts share one slug namespace across the whole assembled site, so two
// projects publishing "hello" would claim the same address and one would
// silently overwrite the other. The unified build refuses that at build time;
// this is the same refusal on the assembly side, where the posts arrive as
// separate manifests written by separate deploys and no single build ever sees
// them together.
//
// The error names both projects that claim the slug.
func MergeProjectPosts(manifests []map[string]any) ([]MergedPost, error) {
	claims := make([]build.SlugClaim, 0)
	for _, manifest := range manifests {
		for _, post := range dictList(manifest, "posts") {
			slug := util.PythonStrOrEmpty(post["slug"])
			if slug == "" {
				continue
			}
			claims = append(claims, build.SlugClaim{
				Slug:   slug,
				Source: util.PythonStrOrEmpty(manifest["slug"]),
			})
		}
	}
	if err := build.CheckPostSlugUniqueness(claims); err != nil {
		return nil, err
	}
	merged := make([]MergedPost, 0)
	for _, manifest := range manifests {
		manifestSlug := util.PythonStrOrEmpty(manifest["slug"])
		for _, post := range dictList(manifest, "posts") {
			merged = append(merged, MergedPost{
				Date:         util.PythonStrOrEmpty(post["date"]),
				Title:        util.PythonStrOrEmpty(post["title"]),
				Slug:         util.PythonStrOrEmpty(post["slug"]),
				ProjectName:  util.PythonStrOrEmpty(manifest["name"]),
				ManifestSlug: manifestSlug,
			})
		}
	}
	return merged, nil
}

// ValidateCrossProjectLinks checks that every cross-project link resolves to a
// known page or post, and returns one error string per broken link.
//
// linkRegistry maps a source page path to the list of targets that page links.
// Both spellings of a target are accepted for every entry: the manifest path
// ("guide.md") and the site address the addressing authority gives it
// ("alpha/guide/", "blog/hello").
//
// The returned slice is empty when every link resolves; it is a list of
// findings rather than an error, because the caller reports all of them at
// once.
func ValidateCrossProjectLinks(manifests []map[string]any, linkRegistry map[string][]string) []string {
	known := map[string]bool{}
	for _, manifest := range manifests {
		manifestSlug := util.PythonStrOrEmpty(manifest["slug"])
		for _, page := range dictList(manifest, "pages") {
			path := util.PythonStrOrEmpty(page["path"])
			known[path] = true
			known[PageTarget(manifestSlug, path, false)] = true
		}
		for _, post := range dictList(manifest, "posts") {
			known[util.PythonStrOrEmpty(post["path"])] = true
			known[PostTarget(util.PythonStrOrEmpty(post["slug"]))] = true
		}
	}

	sources := make([]string, 0, len(linkRegistry))
	for source := range linkRegistry {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	errors := make([]string, 0)
	for _, source := range sources {
		for _, target := range linkRegistry[source] {
			if !known[target] {
				errors = append(errors, fmt.Sprintf(
					"Broken link in '%s': target '%s' not found in any project",
					source, target,
				))
			}
		}
	}
	return errors
}
