package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/util"
)

// defaultPostsDir is where a project keeps its posts when it declares no
// directory of its own.
const defaultPostsDir = layout.PostsDefault

// RenderPostListing renders the Markdown listing page from published post
// metadata, frontmatter included.
//
// publishedPosts must already be ordered newest-first and filtered -- no
// drafts unless drafts were asked for.
func RenderPostListing(publishedPosts []posts.Post) (string, error) {
	frontmatter, err := util.RenderFrontmatter([]util.FrontmatterField{
		{Key: "title", Value: "Posts"},
		{Key: "versioned", Value: false},
		{Key: "type", Value: "post-listing"},
		{Key: "nav_order", Value: int64(95)},
		{Key: "feed", Value: false},
	})
	if err != nil {
		return "", err
	}
	lines := append(strings.Split(strings.TrimRight(frontmatter, "\n"), "\n"),
		"",
		"# Posts",
		"",
	)
	if len(publishedPosts) == 0 {
		lines = append(lines, "No posts yet.")
	} else {
		for _, post := range publishedPosts {
			// The listing is emitted at blog/, so a post is a sibling.
			lines = append(lines, fmt.Sprintf("- **%s** -- [%s](%s/)",
				post.Date, post.Title, post.Slug))
		}
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n"), nil
}

// PostDocsPayloads returns the docs-tree Markdown for a set of published
// posts.
//
// It maps a docs-relative path -- "blog/<slug>.md", plus "blog.md" for the
// listing, which is emitted at "blog/" -- to the Markdown that page is built
// from. Posts are site-level pages: they are built at "blog/<slug>/" under the
// output root, with no locale, project or version segment, and both the full
// build and the posts-only build emit them at that same address. This is the
// one place a post becomes a docs page: [InjectPostsIntoDocs] writes these
// payloads into the docs tree, and the in-memory render path hands the same
// payloads to [BuildSingle] as an overlay -- so both produce the same pages.
//
// publishedPosts must already be filtered (drafts removed unless they were
// asked for) and ordered newest-first.
func PostDocsPayloads(publishedPosts []posts.Post) (map[string]string, error) {
	payloads := map[string]string{}
	for _, post := range publishedPosts {
		fields := make([]util.FrontmatterField, 0, len(post.FrontmatterFields))
		for _, field := range post.FrontmatterFields {
			if field.Value == nil {
				continue
			}
			fields = append(fields, field)
		}
		frontmatter, err := util.RenderFrontmatter(fields)
		if err != nil {
			return nil, fmt.Errorf("post %s: %w", post.Path, err)
		}
		payloads[address.PostsPrefix+"/"+post.Slug+".md"] = frontmatter + post.Content
	}
	if len(publishedPosts) > 0 {
		listing, err := RenderPostListing(publishedPosts)
		if err != nil {
			return nil, err
		}
		payloads[address.PostsPrefix+".md"] = listing
	}
	return payloads, nil
}

// InjectPostsIntoDocs writes the post pages into the docs tree so the normal
// build pipeline discovers them, and returns the absolute paths it wrote, in
// sorted order, for cleanup.
//
// Posts are discovered from the configured posts directory and drafts are left
// out unless includeDrafts is set. Each post is written under
// "<docsDir>/blog/" and the listing at "<docsDir>/blog.md", which is emitted
// at "blog/". The written files are picked up by the docs walk and partitioned
// into the site-level page set, which is built with no mount at all -- so a
// post is at "blog/<slug>/" whichever build produced it.
//
// It refuses first: an authored page on a reserved path is a file this
// function would overwrite and cleanup would then delete, so no build that
// could inject may proceed past one. Checking here rather than at each call
// site is what makes the destruction structurally impossible -- every build
// path reaches the tree through this function.
func InjectPostsIntoDocs(
	dirPath string, cfg config.Config, docsDir string, includeDrafts bool, h *effects.Handle,
) ([]string, error) {
	if err := CheckReservedAuthoredPages(docsDir, dirPath); err != nil {
		return nil, err
	}

	postsDir := postsDirOf(dirPath, cfg)
	if !isDir(postsDir) {
		return nil, nil
	}

	allPosts, err := posts.Discover(postsDir, dirPath, h)
	if err != nil {
		return nil, err
	}
	published := publishedPosts(allPosts, includeDrafts)

	payloads, err := PostDocsPayloads(published)
	if err != nil {
		return nil, err
	}
	relPaths := make([]string, 0, len(payloads))
	for relPath := range payloads {
		relPaths = append(relPaths, relPath)
	}
	sort.Strings(relPaths)

	injected := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		postDocsPath := filepath.Join(docsDir, filepath.FromSlash(relPath))
		if err := h.MkdirAll(filepath.Dir(postDocsPath)); err != nil {
			return injected, err
		}
		if err := h.Write(postDocsPath, []byte(payloads[relPath]), effects.ModeDefault); err != nil {
			return injected, err
		}
		injected = append(injected, postDocsPath)
	}
	return injected, nil
}

// CleanupInjectedPosts removes the post files [InjectPostsIntoDocs] wrote, and
// the "blog/" subdirectory under docsDir when it is left empty.
func CleanupInjectedPosts(injectedFiles []string, docsDir string, h *effects.Handle) error {
	for _, path := range injectedFiles {
		if isFile(path) {
			if err := h.Remove(path); err != nil {
				return err
			}
		}
	}
	postsSubdir := filepath.Join(docsDir, address.PostsPrefix)
	if isDir(postsSubdir) {
		entries, err := os.ReadDir(postsSubdir)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := h.Rmdir(postsSubdir); err != nil {
				return err
			}
		}
	}
	return nil
}

// publishedPosts filters out the drafts unless drafts were asked for.
func publishedPosts(all []posts.Post, includeDrafts bool) []posts.Post {
	published := make([]posts.Post, 0, len(all))
	for _, post := range all {
		if includeDrafts || !post.Draft {
			published = append(published, post)
		}
	}
	return published
}

// postsDirOf is the directory a project keeps its posts in.
func postsDirOf(dirPath string, cfg config.Config) string {
	postsConfig, _ := cfg["posts"].(map[string]any)
	rel := util.PythonStrOrEmpty(postsConfig["dir"])
	if rel == "" {
		rel = defaultPostsDir
	}
	return filepath.Join(dirPath, rel)
}

// DefaultLocaleOf is the locale code a project's site-level pages are written
// in: the declared default if one is marked, else the first declared -- the
// same rule [Build] applies to the versioned tree.
func DefaultLocaleOf(cfg config.Config) string {
	locales := configList(cfg, "locales")
	if len(locales) == 0 {
		return ""
	}
	return util.PythonStrOrEmpty(defaultLocaleEntry(locales)["code"])
}

// SiteLevelBuildArgs returns the [SingleOptions] every site-level page is
// built with.
//
// A post is one page with one address, and three paths produce it: the full
// build's site-level pass, the posts-only target the assembly runs when
// nothing but a post changed, and the in-memory renderer an editor previews
// with. Whichever ran last decides what the site serves, so they cannot
// differ -- and the way to guarantee that is for the arguments to have one
// definition rather than three copies.
//
// Site-level means no mount and no version segment: the page is served from
// the site root, not from the project's subtree and not from an archive
// prefix. It still carries the project's default locale, which is what puts it
// in a locale search filter.
//
// The caller sets DirPath and whichever of PageFilter and OverlayDocs its own
// path needs.
func SiteLevelBuildArgs(cfg config.Config, docsDirName string) SingleOptions {
	siteConfig := make(config.Config, len(cfg))
	for key, value := range cfg {
		siteConfig[key] = value
	}
	siteConfig["docs"] = docsDirName

	localeCode := DefaultLocaleOf(cfg)
	empty := ""

	opts := NewSingleOptions()
	opts.Config = siteConfig
	opts.MountLocale = &empty
	opts.MountVersion = &empty
	opts.VersionOverride = &empty
	opts.LocaleOverride = &localeCode
	opts.AvailableLocales = nil
	opts.CurrentLocale = localeCode
	return opts
}

// BuildPostsOnly builds only the post pages, skipping the versioned docs and
// every site-level file.
//
// It injects the posts into the docs tree, runs them through [BuildSingle]
// with a page filter and no mount, writes the HTML, cleans up, and writes a
// post-manifest. No sitemap, feed, search index or stylesheet is produced.
//
// The mount is empty here and the post pages are site-level in the full build
// too, so a post is at "blog/<slug>/" either way -- the two builds cannot
// disagree about where a post lives. The arguments handed to [BuildSingle] are
// the ones the full build's site-level pass uses, for the same reason: the
// assembly rebuilds posts alone when only a post changed and grafts the result
// beside the full build's output, so any disagreement would make the last
// build to run decide what the site serves.
//
// It returns the paths written.
func BuildPostsOnly(
	dirPath string,
	cfg config.Config,
	outputDir, docsDirName, docsDir string,
	includeDrafts bool,
	siblings []SiblingProject,
	siteName string,
	h *effects.Handle,
) (map[string]bool, error) {
	// The posts are discovered here too, for the manifest: injection calls
	// the same discovery internally, and the manifest records what was
	// discovered rather than what was written.
	postsDir := postsDirOf(dirPath, cfg)
	discovered, err := posts.Discover(postsDir, dirPath, h)
	if err != nil {
		return nil, err
	}
	if !includeDrafts {
		discovered = publishedPosts(discovered, false)
	}

	injectedPaths, err := InjectPostsIntoDocs(dirPath, cfg, docsDir, includeDrafts, h)
	if err != nil {
		return nil, err
	}

	written, buildErr := buildPostsOnlyBody(
		dirPath, cfg, outputDir, docsDirName, discovered, injectedPaths,
		docsDir, siblings, siteName, h)
	if cleanupErr := CleanupInjectedPosts(injectedPaths, docsDir, h); cleanupErr != nil && buildErr == nil {
		return written, cleanupErr
	}
	return written, buildErr
}

// buildPostsOnlyBody is the part of [BuildPostsOnly] the injected files are
// cleaned up after, whether it succeeded or not.
func buildPostsOnlyBody(
	dirPath string,
	cfg config.Config,
	outputDir, docsDirName string,
	discovered []posts.Post,
	injectedPaths []string,
	docsDir string,
	siblings []SiblingProject,
	siteName string,
	h *effects.Handle,
) (map[string]bool, error) {
	if len(injectedPaths) == 0 {
		// A project with no posts still gets a post-manifest, so a
		// consumer reading one does not have to tell "no posts" from "no
		// manifest".
		if _, err := manifest.Generate(
			cfg, dirPath, map[string]manifest.Doc{}, nil, "post-manifest.json", h,
		); err != nil {
			return nil, err
		}
		return map[string]bool{}, nil
	}

	pageFilter := map[string]bool{}
	for _, path := range injectedPaths {
		rel, err := filepath.Rel(docsDir, path)
		if err != nil {
			return nil, err
		}
		pageFilter[filepath.ToSlash(rel)] = true
	}

	opts := SiteLevelBuildArgs(cfg, docsDirName)
	opts.DirPath = dirPath
	opts.PageFilter = pageFilter
	opts.Siblings = siblings
	opts.SiteName = siteName
	result, err := BuildSingle(opts, h)
	if err != nil {
		return nil, err
	}

	written := map[string]bool{}
	for _, relPath := range sortedKeys(result.HTMLFiles) {
		outPath := filepath.Join(outputDir, filepath.FromSlash(relPath))
		if err := h.MkdirAll(filepath.Dir(outPath)); err != nil {
			return written, err
		}
		if err := h.Write(outPath,
			[]byte(MinifyHTML(result.HTMLFiles[relPath])), effects.ModeDefault); err != nil {
			return written, err
		}
		written[outPath] = true
	}

	if _, err := manifest.Generate(
		cfg, dirPath, map[string]manifest.Doc{}, posts.ManifestPosts(discovered),
		"post-manifest.json", h,
	); err != nil {
		return written, err
	}
	return written, nil
}

// PartitionPages partitions a docs tree's pages into the versioned, the
// unversioned and the site-level sets.
//
// It resolves every page once and reads the "versioned" frontmatter key: a
// page without it, or with it set true, is versioned. A page under the posts
// prefix is neither -- it is a site-level page, built once with no mount at
// all, and comes back as its own set.
func PartitionPages(
	cfg config.Config, docsDir, dirPath string, h *effects.Handle,
) (Partition, error) {
	allDocs, err := docs.ResolveAll(cfg, docsDir, dirPath, nil, h)
	if err != nil {
		return Partition{}, err
	}
	partition := Partition{
		Versioned:              map[string]bool{},
		Unversioned:            map[string]bool{},
		Site:                   map[string]bool{},
		UnversionedMarkdown:    map[string]string{},
		UnversionedFrontmatter: map[string]util.Frontmatter{},
	}
	for _, relPath := range sortedDocKeys(allDocs) {
		doc := allDocs[relPath]
		versioned, declared := doc.Frontmatter["versioned"].(bool)
		switch {
		case IsSiteLevelPage(relPath):
			partition.Site[relPath] = true
		case declared && !versioned:
			partition.Unversioned[relPath] = true
			partition.UnversionedMarkdown[relPath] = doc.Resolved
			if len(doc.Frontmatter) > 0 {
				partition.UnversionedFrontmatter[relPath] = doc.Frontmatter
			}
		default:
			partition.Versioned[relPath] = true
		}
	}
	return partition, nil
}

// Partition is one docs tree's pages, split by how the build mounts them.
type Partition struct {
	// Versioned are the pages emitted once per version.
	Versioned map[string]bool
	// Unversioned are the pages declaring "versioned: false", emitted once
	// at the version-free mount.
	Unversioned map[string]bool
	// Site are the site-level pages -- the posts and their listing --
	// emitted once with no mount at all.
	Site map[string]bool
	// UnversionedMarkdown and UnversionedFrontmatter carry the unversioned
	// pages' resolved content and metadata, which every version's sidebar
	// needs.
	UnversionedMarkdown    map[string]string
	UnversionedFrontmatter map[string]util.Frontmatter
}

// VersionedHTMLPaths lists the output keys of the version-scoped pages a docs
// tree would build.
//
// A cheap scan -- walk the tree, read frontmatter, skip the site-level and
// "versioned: false" pages -- because the only question it answers is which
// versions hold a given page. The version picker asks that before offering a
// version, so it never links to a file no build wrote.
func VersionedHTMLPaths(
	buildDir, docsDirName, localeCode string, locales []map[string]any, cfg config.Config,
) (map[string]bool, error) {
	localeDocsDir, err := ResolveLocaleDocsDir(buildDir, docsDirName, localeCode, locales)
	if err != nil {
		// A locale directory this version does not have holds no pages.
		return map[string]bool{}, nil
	}

	paths := map[string]bool{}
	walkErr := filepath.WalkDir(localeDocsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") {
			return nil
		}
		rel, relErr := filepath.Rel(localeDocsDir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if IsSiteLevelPage(relSlash) {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		block, readErr := util.ReadFrontmatter(string(content), relSlash, util.KindPage)
		if readErr != nil {
			return readErr
		}
		if versioned, declared := block.Values["versioned"].(bool); declared && !versioned {
			return nil
		}
		paths[html.MdToHTMLPath(relSlash)] = true
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	// The build injects the project's changelog as a page of its own.
	changelogPath, err := changelogSource(cfg, buildDir, true)
	if err != nil {
		return nil, err
	}
	if changelogPath != "" {
		paths[html.MdToHTMLPath("changelog.md")] = true
	}
	return paths, nil
}

// ResolveLocaleDocsDir resolves the docs directory one locale's pages are read
// from.
//
// A multi-locale project keeps them under "docs/<code>/", which must exist.
// A single-locale project need not: when its locale subdirectory is absent and
// "docs/" itself holds pages, that is the locale's directory.
func ResolveLocaleDocsDir(dirPath, docsDirName, localeCode string, locales []map[string]any) (string, error) {
	localeDocs := filepath.Join(dirPath, docsDirName, localeCode)
	if isDir(localeDocs) {
		return localeDocs, nil
	}
	if len(locales) == 1 {
		plainDocs := filepath.Join(dirPath, docsDirName)
		if isDir(plainDocs) && holdsMarkdown(plainDocs) {
			return plainDocs, nil
		}
	}
	return "", fmt.Errorf(
		"Locale directory '%s/%s/' not found. Create it with .md files for locale '%s'.",
		docsDirName, localeCode, localeCode)
}

// holdsMarkdown reports whether a directory holds at least one .md file of its
// own, subdirectories excluded.
func holdsMarkdown(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			return true
		}
	}
	return false
}

// sortedKeys returns a map's keys in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedDocKeys returns a walk result's page paths in sorted order.
func sortedDocKeys(all map[string]docs.Doc) []string {
	return sortedKeys(all)
}
