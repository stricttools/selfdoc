package assembly

import (
	"os"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// PublishSummary is what one [PublishProjectDocs] call did.
type PublishSummary struct {
	// Slug is the project published.
	Slug string
	// Published is every site-relative path the commit carries, sorted.
	Published []string
	// Deleted is every repository path the commit removes.
	Deleted []string
	// Push is the commit that carried the publish.
	Push PushResult
}

// PublishOptions is what one [PublishProjectDocs] takes.
type PublishOptions struct {
	// Repo is the assembly repository.
	Repo string
	// Slug is the project publishing.
	Slug string
	// OutputDir is the local build output being published.
	OutputDir string
	// Version is the version this publish records. Required: a documentation
	// publish has no tag, so the version is the one thing it can say about
	// what it published.
	Version string
	// ManifestPath is the project's own manifest, copied in beside the
	// content. Empty publishes no manifest.
	ManifestPath string
	// Branch is the assembly branch to commit to. Empty means
	// [DefaultBranch].
	Branch string
	// Home routes the documentation to the site root instead of to the
	// project's own subtree, which is the whole of what being the home
	// project changes about a publish. See [site.SplitBuildOutput].
	Home bool
	// SourceDir is the project's checkout, read for the one file only the
	// home project has: its curated listing. Empty publishes none.
	SourceDir string
}

// PublishProjectDocs pushes a locally built documentation site into the
// assembly.
//
// This is the documentation counterpart of publishing a post: a documentation
// change reaches the live site with no tag and no release, through the same Git
// Data API commit that a post takes. What it pushes is the project's subtree,
// its manifest, its published-file record and its derived membership entry;
// what it deletes is every page it published before and does not publish now,
// so a page removed locally disappears remotely.
//
// It cannot create membership: publishing into a slug the roster does not
// declare is a hard error naming the block that would have to exist.
func PublishProjectDocs(opts PublishOptions, h *effects.Handle) (*PublishSummary, error) {
	branch := opts.Branch
	if branch == "" {
		branch = DefaultBranch
	}

	roster, err := LoadRemoteRoster(h, opts.Repo)
	if err != nil {
		return nil, err
	}
	if !roster.Has(opts.Slug) {
		declared := strings.Join(roster.Slugs(), ", ")
		if declared == "" {
			declared = "(none)"
		}
		return nil, errorf(
			"%s is not declared in %s on %s, so there is no section to "+
				"publish into. Membership is declared, never created by a "+
				"publish. Add a [[project]] block naming slug = %s and its "+
				"repo. Declared projects: %s.",
			util.PythonRepr(opts.Slug), site.RosterPath, opts.Repo,
			util.PythonRepr(opts.Slug), declared,
		)
	}

	buildRels, err := site.BuildOutputPaths(opts.OutputDir, true)
	if err != nil {
		return nil, err
	}
	split := site.SplitBuildOutput(buildRels, opts.Slug, opts.Home)
	produced := make([]string, 0, len(split))
	for _, siteRel := range split {
		produced = append(produced, siteRel)
	}
	sort.Strings(produced)

	// The home project emits at the site root, where the assembly's own
	// generated listing, blog and archives live. A page claiming one of those
	// addresses is refused here, as the integrate graft refuses it.
	if opts.Home {
		docs := make([]string, 0, len(produced))
		for _, rel := range produced {
			if strings.Split(rel, "/")[0] != shared.PostsSegment {
				docs = append(docs, rel)
			}
		}
		if err := site.CheckHomeCollisions(docs, opts.Slug); err != nil {
			return nil, err
		}
	}

	// A full documentation build carries the project's posts too, and a post
	// is site-level: it addresses "blog/<post-slug>/" with no project segment,
	// in a namespace every project shares. The write is refused before
	// anything is collected, as the integrate graft refuses it.
	claims, err := RemotePostClaims(h, opts.Repo, opts.Slug, roster.Slugs())
	if err != nil {
		return nil, err
	}
	if err := site.RefuseForeignPostOverwrite(opts.Slug, produced, claims); err != nil {
		return nil, err
	}

	files, err := site.CollectSiteFiles(opts.OutputDir, opts.Slug, opts.Home)
	if err != nil {
		return nil, err
	}

	// Both renderings of the curated listing -- the front page's cards and
	// the generated "/projects/" page -- are produced on every deploy,
	// including deploys the home project has nothing to do with, so the
	// listing travels with its documentation. A home project that declares
	// none leaves no sidecar.
	if opts.Home && opts.SourceDir != "" {
		sidecar, content, err := site.HomeListingSidecar(opts.SourceDir, opts.Slug)
		if err != nil {
			return nil, err
		}
		if sidecar != "" {
			files[sidecar] = content
		}
	}

	deletePaths, err := site.StagePublishedRecord(
		RemoteTextFetcher(h), opts.Repo, opts.Slug, "docs", produced, files,
	)
	if err != nil {
		return nil, err
	}

	if opts.ManifestPath != "" && isFile(opts.ManifestPath) {
		manifest, err := os.ReadFile(opts.ManifestPath)
		if err != nil {
			return nil, err
		}
		files["manifests/"+opts.Slug+".json"] = manifest
	}

	// An assembly with no membership record at all is a real state; a failed
	// read is not. This publish rewrites the whole record, so reading a
	// failure as "empty" would push a file naming only this project and
	// destroy every other project's entry.
	rawMembership, err := FetchRemoteText(
		h, opts.Repo, site.ProjectsPath, true,
		"publish "+util.PythonRepr(opts.Slug)+" documentation to "+opts.Repo,
	)
	if err != nil {
		return nil, err
	}
	membership := map[string]any{}
	if strings.TrimSpace(rawMembership) != "" {
		membership, err = decodeMembership(opts.Repo, rawMembership)
		if err != nil {
			return nil, err
		}
	}
	// A documentation publish has no tag, which is the point of it, so it
	// records the version it built and leaves whatever ref the last release
	// recorded alone. A project that has never been released therefore has no
	// ref here, and "assembly rebuild" says so rather than inventing one.
	previous, _ := membership[opts.Slug].(map[string]any)
	declared, _ := roster.Get(opts.Slug)
	entry := map[string]any{"repo": declared.Repo, "version": opts.Version}
	if ref := membershipField(previous, "ref"); ref != "" {
		entry["ref"] = ref
	}
	membership[opts.Slug] = entry
	rendered, err := site.RenderProjectsJSON(membership)
	if err != nil {
		return nil, err
	}
	files[site.ProjectsPath] = []byte(rendered)

	message := strings.TrimSpace("docs: " + opts.Slug + " " + opts.Version)
	push, err := PushFilesToRepo(h, opts.Repo, files, message, branch, deletePaths)
	if err != nil {
		return nil, err
	}
	return &PublishSummary{
		Slug:      opts.Slug,
		Published: produced,
		Deleted:   deletePaths,
		Push:      push,
	}, nil
}
