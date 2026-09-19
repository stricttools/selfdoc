package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gen"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/staleness"
	"github.com/smm-h/strictcli/go/strictcli"
)

// defaultPostsDir is where a project keeps its posts when it declares nothing.
const defaultPostsDir = layout.PostsDefault

// postsDirOf is the posts directory the config declares, relative to the
// project root.
func postsDirOf(cfg map[string]any) string {
	if dir := configString(cfg, "posts", "dir"); dir != "" {
		return dir
	}
	return defaultPostsDir
}

func (c *cli) registerGen() {
	c.app.Command("gen", "Auto-generate documentation pages from project structure",
		c.cmdGen,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.BoolFlag("auto-commit", "Automatically commit generated documentation pages and root files to git. Omitted, it commits; pass --no-auto-commit to leave them uncommitted", strictcli.Optional()),
			strictcli.StringFlag("version-override", "Project version to stamp into version-bearing generated content instead of the version currently recorded in the project manifest (VERSION, pyproject.toml or package.json). Release orchestrators pass the about-to-be-released version here so generated root files are not one release behind (generation runs before the version bump is committed)", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdGen(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	versionOverride := optString(kwargs, "version_override")
	handle := effects.FromContext(ctx)
	dir := c.dir()

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}
	// gen resolves every page to hash it, and a root-file template resolves
	// like any other page, so the home project's site-level markers have to be
	// registered here too -- otherwise gen refuses the whole project.
	cfg = c.withSiteDirectives(cfg, handle)

	// A codeless project declares no 'source', which is the declaration that
	// there are no API or CLI reference pages to derive. Say so and go
	// straight to the root-file templates, which need no source code.
	var genResult gen.GenResult
	if hasSource(cfg) {
		var err error
		genResult, err = gen.GenerateDocs(cfg, dir, versionOverride, handle)
		if err != nil {
			return c.fail(err)
		}
	} else {
		c.println("No 'source' entries in selfdoc.json -- skipping API and CLI " +
			"reference pages (codeless project). Root file templates still " +
			"generate.")
	}

	rootGenerated, err := gen.GenerateRootFiles(cfg, dir, versionOverride, handle)
	if err != nil {
		return c.fail(err)
	}

	if len(rootGenerated) > 0 {
		c.printf("Generated %d root file(s):\n", len(rootGenerated))
		for _, path := range rootGenerated {
			c.printf("  %s\n", path)
		}
	}

	// A generated page is committed where gen wrote it, which is the
	// generated pages directory rather than the handwritten docs root.
	generatedRel := layout.GeneratedPagesRel
	var commitFiles []string

	if len(genResult.Written) > 0 {
		c.printf("Generated %d doc file(s):\n", len(genResult.Written))
		for _, path := range genResult.Written {
			c.printf("  %s\n", path)
			commitFiles = append(commitFiles, filepath.Join(generatedRel, path))
		}
	}

	if len(genResult.Deleted) > 0 {
		c.printf("Deleted %d stale doc file(s):\n", len(genResult.Deleted))
		for _, path := range genResult.Deleted {
			c.printf("  %s\n", path)
			commitFiles = append(commitFiles, filepath.Join(generatedRel, path))
		}
	}

	commitFiles = append(commitFiles, rootGenerated...)

	// Update content/description hashes so that a subsequent 'check' does not
	// report freshly-generated pages as stale.
	allDocs, err := docs.ResolveAll(cfg, "", dir, nil, handle)
	if err != nil {
		return c.fail(err)
	}
	// gen regenerates content and description together, so no page is left
	// stale here -- the skeleton exemption is unnecessary, and the empty set
	// is passed explicitly because there is no default.
	hashDocs := docs.StalenessDocs(allDocs)
	if locales, ok := cfg["locales"].([]any); ok && len(locales) > 0 {
		if first, ok := locales[0].(map[string]any); ok {
			if code, ok := first["code"].(string); ok {
				prefixed := make(map[string]staleness.Doc, len(hashDocs))
				for rel, doc := range hashDocs {
					prefixed[code+"/"+rel] = doc
				}
				hashDocs = prefixed
			}
		}
	}
	if _, _, err := staleness.UpdateHashes(
		hashDocs, dir, false, nil, nil, map[string]bool{}, handle,
	); err != nil {
		return c.fail(err)
	}
	commitFiles = append(commitFiles, hashStorePath)

	// Discover posts for the manifest. A project with no posts directory has
	// none, which is an answer rather than a failure.
	var discovered []posts.Post
	postsDir := filepath.Join(dir, strings.TrimRight(postsDirOf(cfg), "/"))
	if info, err := os.Stat(postsDir); err == nil && info.IsDir() {
		discovered, err = posts.Discover(postsDir, dir, handle)
		if err != nil {
			return c.fail(err)
		}
	}
	published := make([]posts.Post, 0, len(discovered))
	for _, post := range discovered {
		if !post.Draft {
			published = append(published, post)
		}
	}

	if _, err := manifest.Generate(cfg, dir, docs.ManifestDocs(allDocs),
		posts.ManifestPosts(published), manifest.DefaultOutputName, handle); err != nil {
		return c.fail(err)
	}
	commitFiles = append(commitFiles, layout.ManifestRel)

	if len(commitFiles) > 0 && autoCommit {
		if _, _, err := gitcommit.AutoCommit(
			commitFiles, "selfdoc gen: update generated docs", dir, handle,
		); err != nil {
			return c.fail(err)
		}
	}

	if len(genResult.Written) == 0 && len(rootGenerated) == 0 {
		c.println("No files generated.")
	}
	return strictcli.Exit(0)
}

// hasSource reports whether the config declares any source entry.
func hasSource(cfg map[string]any) bool {
	entries, ok := cfg["source"].([]any)
	return ok && len(entries) > 0
}

// docsDirOf is the docs directory the config declares.
func docsDirOf(cfg map[string]any) string {
	if dir, ok := cfg["docs"].(string); ok && dir != "" {
		return dir
	}
	return layout.DocsDefault
}
