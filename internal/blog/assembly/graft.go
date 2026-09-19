package assembly

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// GraftOptions is what one [ApplyProjectFiles] takes.
type GraftOptions struct {
	// AssemblyDir is the assembly repository checkout being updated.
	AssemblyDir string
	// SourceDir is the cloned source project whose build is being grafted.
	SourceDir string
	// Slug is the project the graft publishes for.
	Slug string
	// Scope is the dispatch's scope, one of [site.IntegrateScopes].
	Scope string
	// Home routes the documentation to the site root instead of to the
	// project's own subtree, which is the whole of what being the home
	// project changes about a graft.
	Home bool
	// Stderr is where the one advisory a graft prints goes -- the posts-scope
	// build that produced no posts. Nil writes to the process's standard
	// error.
	Stderr io.Writer
}

// stderr is the writer the graft's advisory goes to.
func (o GraftOptions) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

// ApplyProjectFiles grafts a built project into the assembly tree and returns
// the paths it changed.
//
// The graft prunes rather than wipes: what the build produces is what the build
// owns, and only paths this publisher produced *before* and does not produce
// now are removed. Everything else -- a post or a documentation page published
// between releases -- is somebody else's and remains. The published-file record
// at "manifests/<slug>-files.json" is what makes that distinction possible.
//
// The build's output lands in two places, by the rule [site.SplitBuildOutput]
// states: the project's documentation under "site/<slug>/", its posts at the
// site level under "site/blog/".
//
// [GraftOptions.Home] routes the documentation to the site root instead. Its
// output is checked against the addresses the assembly owns first, and its
// curated listing is copied in beside the manifests.
func ApplyProjectFiles(opts GraftOptions, h *effects.Handle) ([]string, error) {
	buildRoot := layout.Path(opts.SourceDir, layout.OutputRel)
	siteDir := filepath.Join(opts.AssemblyDir, "site")
	siteSlugDir := filepath.Join(siteDir, opts.Slug)
	manifestsDir := filepath.Join(opts.AssemblyDir, "manifests")
	if err := h.MkdirAll(manifestsDir); err != nil {
		return nil, err
	}
	var touched []string

	allOutputs, err := site.BuildOutputPaths(buildRoot, true)
	if err != nil {
		return nil, err
	}

	var owner string
	var outputs []string
	var srcManifest, destManifest string
	if opts.Scope == "posts" {
		// Posts-only: this publisher's whole world is the build's post output,
		// so anything the clone happens to carry outside blog/ is not its
		// business and is not grafted.
		owner = "posts"
		for _, rel := range allOutputs {
			if strings.SplitN(rel, "/", 2)[0] == shared.PostsSegment {
				outputs = append(outputs, rel)
			}
		}
		srcManifest = layout.Path(opts.SourceDir, layout.PostManifestRel)
		destManifest = filepath.Join(manifestsDir, opts.Slug+"-posts.json")
	} else {
		owner = "release"
		outputs = allOutputs
		srcManifest = layout.Path(opts.SourceDir, layout.ManifestRel)
		destManifest = filepath.Join(manifestsDir, opts.Slug+".json")
	}

	produced := site.SplitBuildOutput(outputs, opts.Slug, opts.Home)
	if opts.Home {
		var docs []string
		for _, rel := range produced {
			if strings.SplitN(rel, "/", 2)[0] != shared.PostsSegment {
				docs = append(docs, rel)
			}
		}
		if err := site.CheckHomeCollisions(docs, opts.Slug); err != nil {
			return nil, err
		}
	}

	if opts.Scope == "posts" && len(produced) == 0 {
		// A build that produced no posts is not an instruction to unpublish
		// the ones already on the site: the pruning publisher would remove
		// every path it claimed, and a posts build emits nothing when the
		// source's posts directory is empty or absent for any reason at all.
		fmt.Fprintf(opts.stderr(),
			"posts scope for %s: the build produced no post pages, so there "+
				"is nothing to publish. Nothing was written and nothing was "+
				"removed -- posts already published stay. To unpublish a "+
				"post, delete it at the source and run a full release, which "+
				"republishes this project's whole post set.\n",
			util.PythonRepr(opts.Slug),
		)
		return touched, nil
	}

	recordPath := site.FilesManifestPath(manifestsDir, opts.Slug)
	owners, err := site.LoadFilesManifest(recordPath)
	if err != nil {
		return nil, err
	}
	producedPaths := make([]string, 0, len(produced))
	for _, rel := range produced {
		producedPaths = append(producedPaths, rel)
	}
	removed, owners, err := site.PrunePlan(owners, owner, producedPaths)
	if err != nil {
		return nil, err
	}

	// The site-level blog is one namespace shared by every project, so the
	// write itself is checked, not only the merge that reads the manifests
	// afterwards: a post path another project's record claims is refused
	// before anything is copied over it.
	claims, err := site.ForeignPostClaims(manifestsDir, opts.Slug)
	if err != nil {
		return nil, err
	}
	if err := site.RefuseForeignPostOverwrite(opts.Slug, producedPaths, claims); err != nil {
		return nil, err
	}

	if err := site.GraftSubtree(siteDir, buildRoot, produced, removed, h); err != nil {
		return nil, err
	}
	// An artifact already in the tree from an older deploy is removed on
	// sight: the assembly serves one set of headers, redirects and worker for
	// the whole site, and a project's own copies fight them wherever they sit.
	// The home project has no subtree of its own to sweep -- its output was
	// filtered on the way in, by site.SplitBuildOutput.
	if !opts.Home {
		if _, err := site.PruneDeployArtifacts(siteSlugDir, h); err != nil {
			return nil, err
		}
		if _, err := site.PruneEmptyDirs(siteSlugDir, h); err != nil {
			return nil, err
		}
	}
	if _, err := site.PruneEmptyDirs(filepath.Join(siteDir, shared.PostsSegment), h); err != nil {
		return nil, err
	}
	if opts.Home {
		touched = append(touched, siteDir)
	} else {
		touched = append(touched, siteSlugDir)
	}
	for _, rel := range produced {
		if strings.SplitN(rel, "/", 2)[0] == shared.PostsSegment {
			touched = append(touched, filepath.Join(siteDir, shared.PostsSegment))
			break
		}
	}

	record, err := site.RenderFilesManifest(opts.Slug, owners)
	if err != nil {
		return nil, err
	}
	if err := h.Write(recordPath, []byte(record), effects.ModeDefault); err != nil {
		return nil, err
	}
	touched = append(touched, recordPath)

	if isFile(srcManifest) {
		if err := h.CopyFile(srcManifest, destManifest); err != nil {
			return nil, err
		}
		touched = append(touched, destManifest)
	}

	if opts.Home && opts.Scope != "posts" {
		sidecar, err := CopyHomeListing(opts.AssemblyDir, opts.SourceDir, opts.Slug, h)
		if err != nil {
			return nil, err
		}
		if sidecar != "" {
			touched = append(touched, sidecar)
		}
	}

	if owner == "release" {
		overlay, err := FoldPostsIntoOverlay(manifestsDir, opts.Slug, srcManifest, h)
		if err != nil {
			return nil, err
		}
		if overlay != "" {
			touched = append(touched, overlay)
		}
	}
	return touched, nil
}

// CopyHomeListing copies the home project's curated listing into the assembly.
//
// The listing is authored in the home project ("docs/projects.toml") because it
// is content, and it is copied here because both renderings of it -- the front
// page's cards and the generated "/projects/" page -- are produced on every
// deploy, including deploys the home project has nothing to do with.
//
// A home project that declares no listing is a real state and leaves no
// sidecar; a malformed one is a hard error naming the file, reported here
// rather than at the far end where the document is no longer in reach.
//
// It returns the sidecar's path, or "" when there was nothing to copy.
func CopyHomeListing(assemblyDir, sourceDir, slug string, h *effects.Handle) (string, error) {
	rel, content, err := site.HomeListingSidecar(sourceDir, slug)
	if err != nil || rel == "" {
		return "", err
	}
	path := filepath.Join(assemblyDir, filepath.Join(strings.Split(rel, "/")...))
	if err := h.Write(path, content, effects.ModeDefault); err != nil {
		return "", err
	}
	return path, nil
}

// FoldPostsIntoOverlay adds a full build's posts to slug's post overlay and
// returns the overlay's path.
//
// The overlay is the assembly's one authority on a project's posts, so a full
// build cannot simply ignore it -- an overlay written before the release would
// keep the release's own posts off the site. It used to delete the overlay
// outright for that reason, which threw away every post published between
// releases along with the staleness.
//
// Folding is the version that keeps both: the build's posts go in, the
// overlay's posts that the build does not carry stay, and the file remains the
// complete list "post publish" overwrites wholesale. It returns "" when there
// is no overlay to fold into.
//
// The rewritten overlay's keys come out sorted. The Python rewrote the document
// in its own key order, which a decoded JSON document no longer carries; the
// keys and their values are the same either way.
func FoldPostsIntoOverlay(
	manifestsDir, slug, buildManifest string,
	h *effects.Handle,
) (string, error) {
	overlayPath := filepath.Join(manifestsDir, slug+"-posts.json")
	if !isFile(overlayPath) || !isFile(buildManifest) {
		return "", nil
	}
	overlay, err := readJSONObject(overlayPath)
	if err != nil {
		return "", err
	}
	built, err := readJSONObject(buildManifest)
	if err != nil {
		return "", err
	}
	basePosts, _ := built["posts"].([]any)
	overlayPosts, _ := overlay["posts"].([]any)
	overlay["posts"] = site.MergePostLists(basePosts, overlayPosts)
	encoded, err := util.PythonJSONIndent2(overlay)
	if err != nil {
		return "", err
	}
	if err := h.Write(overlayPath, append(encoded, '\n'), effects.ModeDefault); err != nil {
		return "", err
	}
	return overlayPath, nil
}

// readJSONObject decodes one JSON object from a file.
func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, errorf("%s is not valid JSON: %s", path, err)
	}
	return document, nil
}

// isFile reports whether path is a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
