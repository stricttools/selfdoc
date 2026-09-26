package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerPublishDocs(group *strictcli.Group) {
	group.Command("publish-docs",
		"Publish this project's documentation to the assembly without a release. Builds the docs locally, pushes the built site, its manifest and its membership record into the assembly repo via the Git Data API -- deleting the pages this project published before and no longer produces -- then dispatches a shared-only workflow to regenerate cross-project elements. Refuses before pushing anything when the vocabulary this project's manifest records and another project's on the assembly disagree about a word -- one's rejected pattern covering a word the other, or selfdoc's built-in baseline, accepts -- naming both projects, the word, the pattern and the fix.",
		c.cmdPublishDocs,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Consequential for the same reason `blog post publish` is:
		// locally-authored content becomes publicly readable at the moment
		// this runs, with no tag and no release standing between the working
		// tree and the live site. It also deletes: a page this project
		// published before and no longer builds disappears for readers in the
		// same commit.
		strictcli.WithConsequential(),
		strictcli.WithGrants(assemblyDispatchGrant),
	)
}

func (c *cli) cmdPublishDocs(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)
	dir := c.dir()

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	repo := configString(cfg, "assembly", "repo")
	if repo == "" {
		return c.failf("Error: assembly.repo not configured in selfdoc.json.")
	}
	slug := configString(cfg, "topology", "slug")
	if slug == "" {
		return c.failf("Error: topology.slug not configured in selfdoc.json.")
	}

	version := assembly.PublishVersion(dir, cfg)

	// Whether this project is the assembly's home project decides both how it
	// is built and where its pages land, so the roster is read before either.
	// A publish reads it again inside [assembly.PublishProjectDocs], for the
	// membership it may not create and the posts it may not overwrite; this
	// read is the one the build needs.
	roster, err := assembly.LoadRemoteRoster(handle, repo)
	if err != nil {
		return c.fail(err)
	}
	home := slug == roster.Home

	// The assembly's manifests are read off the repository, since this
	// publisher never clones it. The build reads them, and so does the publish:
	// the other projects' vocabularies this project's is checked against.
	manifests, err := assembly.FetchRemoteManifests(handle, repo, "")
	if err != nil {
		return c.fail(err)
	}
	peers, err := assembly.PublishedVocabularies(manifests, slug)
	if err != nil {
		return c.fail(err)
	}

	// The same build the deploy runs on a cloned checkout, run here on the
	// working tree.
	if err := assembly.BuildForPublish(assembly.PublishBuildOptions{
		SourceDir: dir, Config: cfg, Slug: slug, Home: home,
		HomeSlug: roster.Home, Manifests: manifests,
	}, handle); err != nil {
		return c.fail(err)
	}

	outputRel := strings.TrimRight(outputDirOf(cfg), "/")
	outputDir := filepath.Join(dir, outputRel)
	if info, err := os.Stat(outputDir); err != nil || !info.IsDir() {
		return c.failf("Error: the build produced no output at %s; there is "+
			"nothing to publish.", outputDir)
	}

	summary, err := assembly.PublishProjectDocs(assembly.PublishOptions{
		Repo:         repo,
		Slug:         slug,
		OutputDir:    outputDir,
		Version:      version,
		ManifestPath: layout.Path(dir, layout.ManifestRel),
		Home:         home,
		SourceDir:    dir,
		Peers:        peers,
	}, handle)
	if err != nil {
		return c.fail(err)
	}

	// Dispatch a shared-only rebuild so the listing, feed, sitemap and search
	// index take account of what just changed.
	if outcome, ok := c.dispatchSharedRebuild(handle, repo); !ok {
		return outcome
	}

	removal := ""
	if len(summary.Deleted) > 0 {
		removal = ", removing " + itoa(len(summary.Deleted)) + " page(s) it no longer builds"
	}
	c.printf("Published %d documentation file(s) for %s to %s%s. Shared elements will regenerate.\n",
		len(summary.Published), slug, repo, removal)
	return strictcli.Exit(0)
}
