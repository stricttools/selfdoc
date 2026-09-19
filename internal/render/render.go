// Package render renders a page from content held in memory, writing nothing.
//
// The build pipeline turns a post into a page by writing it into the docs
// tree, building, and deleting it again. That is fine for a build and useless
// for an editor: previewing an unsaved buffer must not touch the working tree
// at all -- no injected files, no staleness baselines, no manifest.
//
// This is that path. It hands the same payloads the injector would have
// written to the build's single-pass entry point as an in-memory overlay, with
// baseline writing off, and returns the page HTML as the build would have
// written it. Same directive resolution, same HTML pass, same addressing --
// the only difference is that nothing is written.
package render

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// PostOptions is what one [Post] render takes.
type PostOptions struct {
	// DirPath is the project root.
	DirPath string
	// SourcePath is the post's path relative to the posts directory, e.g.
	// "hello.md". The file need not exist -- an unsaved new post renders
	// the same way.
	SourcePath string
	// Content is the post's Markdown source, frontmatter included, as it
	// would be saved.
	Content string
	// Config is a pre-loaded config. Nil loads selfdoc.json from DirPath.
	Config config.Config
	// IncludeDrafts renders the post even when its frontmatter marks it a
	// draft. It mirrors the build flag of the same name: with it off, a
	// draft has no page and asking for one is an error.
	IncludeDrafts bool
}

// Post renders one post to its final HTML from an in-memory buffer.
//
// The result is byte-identical to the file a posts-target build writes for the
// same content saved to disk, and the call writes nothing anywhere.
//
// It is an error when no config is found, when the post is a draft and drafts
// were not asked for, and when the rendered page is missing from the build
// result.
func Post(opts PostOptions, h *effects.Handle) (string, error) {
	cfg := opts.Config
	if cfg == nil {
		loaded, err := config.Load(opts.DirPath)
		if err != nil {
			return "", err
		}
		cfg = loaded
	}
	if cfg == nil {
		return "", errors.New(
			"No selfdoc.json found. Run 'selfdoc init' to initialize.")
	}

	edited, err := posts.Parse(opts.Content, opts.SourcePath, "")
	if err != nil {
		return "", err
	}

	if edited.Draft && !opts.IncludeDrafts {
		return "", fmt.Errorf(
			"Post %s is a draft and has no page. Pass include_drafts=True to render it anyway.",
			opts.SourcePath)
	}

	postsConfig, _ := cfg["posts"].(map[string]any)
	postsDirRel := util.PythonStrOrEmpty(postsConfig["dir"])
	if postsDirRel == "" {
		postsDirRel = layout.PostsDefault
	}
	postsDir := filepath.Join(opts.DirPath, postsDirRel)

	// The rest of the posts come from disk: a post page's navigation and
	// listing neighbours are whatever is saved. The edited post replaces
	// its own saved copy in place, so its position in the listing is the
	// saved one until it is saved again.
	discovered, err := posts.Discover(postsDir, opts.DirPath, h)
	if err != nil {
		return "", err
	}
	all := append([]posts.Post(nil), discovered...)
	replaced := false
	for i := range all {
		if all[i].Path == opts.SourcePath {
			all[i] = edited
			replaced = true
			break
		}
	}
	if !replaced {
		all = append(all, edited)
	}

	published := make([]posts.Post, 0, len(all))
	for _, post := range all {
		if opts.IncludeDrafts || !post.Draft {
			published = append(published, post)
		}
	}
	payloads, err := build.PostDocsPayloads(published)
	if err != nil {
		return "", err
	}

	pageFilter := make(map[string]bool, len(payloads))
	for relPath := range payloads {
		pageFilter[relPath] = true
	}

	// A post is site-level, so it is rendered with the arguments every
	// site-level page is built with -- the same ones the full build and the
	// posts-only target use. That shared definition is what makes this
	// byte-identical to what a build would have written.
	singleOpts := build.SiteLevelBuildArgs(cfg, strings.TrimRight(configString(cfg, "docs"), "/"))
	singleOpts.DirPath = opts.DirPath
	singleOpts.PageFilter = pageFilter
	singleOpts.OverlayDocs = payloads
	singleOpts.WriteBaselines = false

	result, err := build.BuildSingle(singleOpts, h)
	if err != nil {
		return "", err
	}

	outputKey := html.MdToHTMLPath(address.PostsPrefix + "/" + edited.Slug + ".md")
	rendered, found := result.HTMLFiles[outputKey]
	if !found {
		return "", fmt.Errorf("Post %s produced no page at %s.",
			opts.SourcePath, util.PythonRepr(outputKey))
	}
	return build.MinifyHTML(rendered), nil
}

// configString reads a string-valued config key, answering "" for a key that
// is absent or carries something else.
func configString(cfg config.Config, key string) string {
	value, _ := cfg[key].(string)
	return value
}
