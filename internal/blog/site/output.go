package site

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// BuildTargetVersion returns the version a selfdoc build of config would
// produce.
//
// This is the one definition of "the version being built", and both the build
// ([DetectLatestVersion]) and the dispatch check ([CheckVersionIsDeclared])
// read it, so they cannot disagree.
//
// Empty when the project declares no versions at all -- a single implicit
// version, which a selfdoc build handles without a version flag. A declared
// versions array whose newest entry carries no version string is a hard error
// however long the array is: the build takes the last entry, so a blank newest
// entry would silently publish the docs unversioned, at the wrong address.
//
// source is what to name in the error message (a directory, usually).
func BuildTargetVersion(cfg map[string]any, source string) (string, error) {
	versions, _ := asList(cfg["versions"])
	if len(versions) == 0 {
		return "", nil
	}
	newest := versions[len(versions)-1]
	latest := ""
	if table, ok := asTable(newest); ok {
		latest = util.PythonStrOrEmpty(table["version"])
	}
	if latest == "" {
		where := ""
		if source != "" {
			where = fmt.Sprintf(" for the project at %s", source)
		}
		return "", errorf(
			"the newest 'versions' entry in selfdoc.json%s declares no "+
				"version, so there is nothing to build. The build takes the "+
				"last entry of 'versions'; give it a 'version' string.",
			where,
		)
	}
	return latest, nil
}

// DetectLatestVersion returns the newest version declared by a source
// project's config.
//
// A project with no selfdoc.json at all builds unversioned; see
// [BuildTargetVersion] for everything else.
func DetectLatestVersion(sourceDir string) (string, error) {
	cfgPath := filepath.Join(sourceDir, "selfdoc.json")
	info, err := os.Stat(cfgPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", err
	}
	decoded, err := config.DecodeDocument(content)
	if err != nil {
		return "", err
	}
	table, _ := asTable(decoded)
	return BuildTargetVersion(table, sourceDir)
}

// PruneDeployArtifacts deletes per-project deploy artifacts under root and
// returns their paths, sorted.
//
// A project build emits _headers, _redirects and pre-compressed .gz / .br
// copies for its own standalone hosting. Inside the assembly those files would
// fight the site-wide ones the shared generator writes. The set also covers
// _worker.js, which nothing emits any more: a subtree deployed before the
// redirect worker was dropped still carries one, and this is what takes it
// out.
func PruneDeployArtifacts(root string, handle *effects.Handle) ([]string, error) {
	removed := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return fs.SkipAll
			}
			return err
		}
		if entry.IsDir() || !IsDeployArtifact(entry.Name()) {
			return nil
		}
		if err := handle.Remove(path); err != nil {
			return err
		}
		removed = append(removed, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(removed)
	return removed, nil
}

// IsDeployArtifact reports whether a file name is a per-project deploy
// artifact.
func IsDeployArtifact(name string) bool {
	return containsString(DeployArtifactNames, name) ||
		hasAnySuffix(name, DeployArtifactSuffixes)
}

// BuildOutputPaths returns every file under root as a "/"-joined relative
// path, sorted.
//
// This is the "what the build produces" set the prune is driven by, so with
// skipArtifacts it excludes the per-project deploy artifacts: those are
// filtered out on the way in and must not be recorded as though the assembly
// served them. Pass true at every call site that models a graft.
func BuildOutputPaths(root string, skipArtifacts bool) ([]string, error) {
	found := make([]string, 0)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return found, nil
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if skipArtifacts && IsDeployArtifact(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

// PruneEmptyDirs removes empty directories under root and returns the ones
// removed.
//
// root itself always survives, even when the subtree ends up empty: the
// project still has a section, it just has no files in it.
//
// The order is every directory path ascending, so a parent is examined before
// its children and a nest of empty directories collapses one level per call.
// That is what the Python did -- it sorted a bottom-up walk back into path
// order -- and a deploy runs this after every prune, so the nest empties out
// over the deploys that produced it.
func PruneEmptyDirs(root string, handle *effects.Handle) ([]string, error) {
	removed := make([]string, 0)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return removed, nil
	}
	dirs := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if len(entries) > 0 {
			continue
		}
		if err := handle.Rmdir(dir); err != nil {
			return nil, err
		}
		removed = append(removed, dir)
	}
	return removed, nil
}

// HomeReservedDirs are the directory names at the site root the generator
// owns. A home project page emitting into one of them would be overwritten by,
// or would overwrite, the assembly's own listing, blog or archive space.
var HomeReservedDirs = []string{"blog", "projects", "v", "pagefind"}

// HomeDroppedArtifacts are the files at the site root the assembly generates
// for the whole site on every deploy.
//
// A project's own build writes its own copy of each at its own output root,
// for its own standalone hosting; for a project under site/<slug>/ those
// copies are dropped on the way in, and for the home project they would land
// on top of the site-wide ones. They are dropped from the home graft, the same
// treatment [PruneDeployArtifacts] gives every other project's routing files.
// "404.html" reaches this set through [DeployArtifactNames], which drops it for
// every project rather than only for the home one. "index.html" is
// deliberately absent: the home project's front page is what belongs at the
// site root.
//
// "projects.toml" is the curated listing's source document, which the build
// copies through as a static asset. The site serves the two renderings of it,
// not it.
var HomeDroppedArtifacts = append([]string{
	"projects.toml",
	"sitemap.xml",
	"sitemap-index.xml",
	"robots.txt",
	"feed.xml",
	"llms.txt",
	"llms-full.txt",
	"nav.json",
}, DeployArtifactNames...)

// HomeDroppedDirs are the directories at the home project's output root whose
// machine-written contents the assembly writes itself.
//
// The home project's pages are at the site root, so its own Pagefind index
// lands where the site-wide one belongs -- and the site-wide one, written over
// the whole assembled tree, is the index those pages must answer from. Every
// other project keeps its own index inside its subtree, which is what its
// pages address.
//
// Only the indexer's own files are dropped. An .html page under one of these
// directories is content -- a home page called pagefind.md builds to
// pagefind/index.html -- and stays a refused collision rather than
// disappearing quietly.
var HomeDroppedDirs = []string{"pagefind"}

// Collision is one address a home project claimed that the assembly owns, and
// why the assembly owns it.
type Collision struct {
	// Path is the site-relative address the home project emits at.
	Path string
	// Why names the assembly's own directory the address falls inside.
	Why string
}

// HomeCollisions returns one [Collision] per reserved address in siteRels.
//
// The home project emits at the site root, where the assembly's own generated
// pages live. A page called projects.md builds to projects/index.html, which
// is the generated project listing's address; one of the two would silently
// win. Neither does: the collision is refused, at the graft and again at
// verification.
//
// Only the reserved directories can be refused this way, and they are the
// whole rule: a name in [HomeDroppedArtifacts] never reaches a graft to be
// checked, because every selfdoc build writes those for its own standalone
// hosting and the assembly writes the ones the site serves.
func HomeCollisions(siteRels []string) []Collision {
	found := make([]Collision, 0)
	for _, rel := range sortedUnique(siteRels) {
		head := strings.Split(rel, "/")[0]
		if !containsString(HomeReservedDirs, head) {
			continue
		}
		found = append(found, Collision{
			Path: rel,
			Why: fmt.Sprintf(
				"%s/ is the assembly's own directory (%s are reserved)",
				head, strings.Join(HomeReservedDirs, ", "),
			),
		})
	}
	return found
}

// CheckHomeCollisions returns an error when the home project claims an address
// the assembly owns.
func CheckHomeCollisions(siteRels []string, slug string) error {
	found := HomeCollisions(siteRels)
	if len(found) == 0 {
		return nil
	}
	parts := make([]string, 0, len(found))
	for _, collision := range found {
		parts = append(parts, fmt.Sprintf("site/%s -- %s", collision.Path, collision.Why))
	}
	return errorf(
		"the home project %s emits %d file(s) at addresses the assembly owns: "+
			"%s. The home project's content root is the site root, so it "+
			"shares that namespace with the generated listing, blog, archives "+
			"and site-wide artifacts. Rename the page.",
		util.PythonRepr(slug), len(found), strings.Join(parts, "; "),
	)
}

// SplitBuildOutput maps each file a build produced to where the assembly
// serves it.
//
// A project's build output lands in two places, and this is the rule that
// decides which:
//
//   - blog/<post-slug>/... -- two or more segments under blog/ -- is one of the
//     project's posts. Posts are site-level: the file keeps its address
//     exactly, at site/blog/<post-slug>/..., under no project slug.
//   - A file directly under blog/ -- in practice blog/index.html, the listing
//     page the build renders so the project's own standalone site has a blog
//     page -- is not grafted at all. The assembled site's blog index lists
//     every project's posts and is written by the shared generator; a single
//     project's copy would claim the same address and serve one project's posts
//     as the whole site's.
//   - Everything else is the project's documentation, and lands under its own
//     subtree at site/<slug>/...
//
// home is the one project the roster names home. Its documentation is not
// filed under a slug at all: the site root IS its content root, so index.html
// lands at site/index.html and cv/index.html at site/cv/index.html, beside the
// generated blog/ and projects/. Its posts follow the same site-level rule as
// everybody else's, and the site-wide artifacts its own build wrote for
// standalone hosting ([HomeDroppedArtifacts] and [HomeDroppedDirs], plus every
// compressed variant) are left behind -- the assembly writes the ones the site
// serves.
//
// Returns build-relative path -> site-relative path, with the skipped
// standalone blog index simply absent.
func SplitBuildOutput(buildRels []string, slug string, home bool) map[string]string {
	mapping := map[string]string{}
	for _, rel := range buildRels {
		segments := strings.Split(rel, "/")
		switch {
		case segments[0] == address.PostsPrefix:
			if len(segments) < 3 {
				continue
			}
			mapping[rel] = rel
		case home:
			if containsString(HomeDroppedArtifacts, rel) {
				continue
			}
			if containsString(HomeDroppedDirs, segments[0]) && !strings.HasSuffix(rel, ".html") {
				continue
			}
			if hasAnySuffix(rel, DeployArtifactSuffixes) {
				continue
			}
			mapping[rel] = rel
		default:
			mapping[rel] = fmt.Sprintf("%s/%s", slug, rel)
		}
	}
	return mapping
}

// GraftSubtree copies produced out of src into dest and deletes removed from
// it.
//
// produced maps a source-relative path to the destination-relative path it
// lands at, because a build's output no longer reaches one place: its posts go
// to the site-level blog and everything else to the project's own subtree.
// removed is destination-relative, in the same addressing the published-file
// record uses.
func GraftSubtree(
	dest, src string,
	produced map[string]string,
	removed []string,
	handle *effects.Handle,
) error {
	if err := handle.MkdirAll(dest); err != nil {
		return err
	}
	srcRels := make([]string, 0, len(produced))
	for srcRel := range produced {
		srcRels = append(srcRels, srcRel)
	}
	sort.Strings(srcRels)
	for _, srcRel := range srcRels {
		target := joinRel(dest, produced[srcRel])
		parent := filepath.Dir(target)
		if parent == "" {
			parent = dest
		}
		if err := handle.MkdirAll(parent); err != nil {
			return err
		}
		if err := handle.CopyFile(joinRel(src, srcRel), target); err != nil {
			return err
		}
	}
	for _, rel := range sortedStrings(removed) {
		target := joinRel(dest, rel)
		if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
			if err := handle.Remove(target); err != nil {
				return err
			}
		}
	}
	return nil
}

// joinRel joins a "/"-addressed relative path onto a filesystem base.
func joinRel(base, rel string) string {
	return filepath.Join(append([]string{base}, strings.Split(rel, "/")...)...)
}

// ClaimedSitePaths returns the paths slug published outside its own subtree.
//
// Its documentation goes with site/<slug>/ when that directory is removed; its
// posts do not, because they are site-level. Retirement and reconciliation
// read this to take them along, so a project that leaves the assembly does not
// leave its posts behind on the blog.
func ClaimedSitePaths(manifestsDir, slug string) ([]string, error) {
	record, err := LoadFilesManifest(FilesManifestPath(manifestsDir, slug))
	if err != nil {
		return nil, err
	}
	prefix := slug + "/"
	claimed := make([]string, 0)
	for _, paths := range record {
		for _, path := range paths {
			if !strings.HasPrefix(path, prefix) {
				claimed = append(claimed, path)
			}
		}
	}
	return sortedUnique(claimed), nil
}

// ForeignPostClaims maps every site-level post path other projects claim to
// its claimant.
//
// The manifest merge refuses two projects publishing the same post slug, but
// that refusal reads manifests; this one reads the published-file records, so
// the write itself can be refused too. Both are needed: a graft happens before
// the manifests are merged, and it is the graft that would overwrite the other
// project's file.
func ForeignPostClaims(manifestsDir, slug string) (map[string]string, error) {
	claims := map[string]string{}
	info, err := os.Stat(manifestsDir)
	if err != nil || !info.IsDir() {
		return claims, nil
	}
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, "-files.json") {
			continue
		}
		other := strings.TrimSuffix(name, "-files.json")
		if other == slug {
			continue
		}
		record, err := LoadFilesManifest(filepath.Join(manifestsDir, name))
		if err != nil {
			return nil, err
		}
		owners := make([]string, 0, len(record))
		for owner := range record {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		for _, owner := range owners {
			for _, path := range record[owner] {
				if strings.Split(path, "/")[0] != address.PostsPrefix {
					continue
				}
				if _, taken := claims[path]; !taken {
					claims[path] = other
				}
			}
		}
	}
	return claims, nil
}

// RefuseForeignPostOverwrite returns an error if any of produced is a post
// path claims gives to someone else.
//
// One refusal for every publisher: the integrate graft, which reads the
// records out of the assembly clone, and the two Git Data API publishers,
// which read them off the remote. Sharing the wording is the point -- three
// copies of a refusal are three chances for one of them to be quietly weaker
// than the others.
//
// produced is site-relative, in the same addressing the published-file record
// uses, and claims maps such a path to the project that claims it.
func RefuseForeignPostOverwrite(slug string, produced []string, claims map[string]string) error {
	stolen := make([]string, 0)
	for _, destRel := range sortedUnique(produced) {
		if _, ok := claims[destRel]; ok {
			stolen = append(stolen, destRel)
		}
	}
	if len(stolen) == 0 {
		return nil
	}
	parts := make([]string, 0, len(stolen))
	for _, path := range stolen {
		parts = append(parts, fmt.Sprintf(
			"site/%s is claimed by %s", path, util.PythonRepr(claims[path]),
		))
	}
	return errorf(
		"%s would overwrite %d post file(s) another project published: %s. "+
			"Posts are emitted at '%s/<post-slug>/' with no project segment, "+
			"so a post slug is unique across the whole site. Rename the post's "+
			"slug in the project that claims it later.",
		util.PythonRepr(slug), len(stolen), strings.Join(parts, "; "),
		address.PostsPrefix,
	)
}

// ProjectPaths returns every assembly path that belongs to slug.
//
// That is its whole site subtree, plus every one of its manifest kinds -- the
// base manifest, the posts overlay, the revisions sidecar and the
// published-file record -- plus claimed, the site-relative paths its
// published-file record names outside that subtree. Its posts are all of the
// last kind: they sit at the site level under blog/, so removing the subtree
// alone would leave them on the blog with nothing left to explain where they
// came from.
func ProjectPaths(paths []string, slug string, claimed []string) []string {
	sitePrefix := fmt.Sprintf("site/%s/", slug)
	manifestPrefix := fmt.Sprintf("manifests/%s-", slug)
	baseManifest := fmt.Sprintf("manifests/%s.json", slug)
	outside := make(map[string]bool, len(claimed))
	for _, rel := range claimed {
		outside["site/"+rel] = true
	}
	owned := make([]string, 0)
	for _, path := range paths {
		if strings.HasPrefix(path, sitePrefix) ||
			outside[path] ||
			path == baseManifest ||
			(strings.HasPrefix(path, manifestPrefix) && strings.HasSuffix(path, ".json")) {
			owned = append(owned, path)
		}
	}
	return sortedUnique(owned)
}

// CollectSiteFiles returns assembly path -> bytes for every file a local build
// produced.
//
// Content travels as bytes because a documentation site is not all text:
// fonts, favicons and screenshots go through the same commit as the HTML, and
// decoding them as UTF-8 on the way past would destroy them. The same
// per-project deploy artifacts the deploy filters out are filtered here -- see
// [BuildOutputPaths] -- and the output is split the same way a deploy splits
// it, so a locally built post lands on the site-level blog rather than inside
// the project's subtree, and the home project's pages land at the site root.
// See [SplitBuildOutput].
func CollectSiteFiles(outputDir, slug string, home bool) (map[string][]byte, error) {
	rels, err := BuildOutputPaths(outputDir, true)
	if err != nil {
		return nil, err
	}
	produced := SplitBuildOutput(rels, slug, home)
	buildRels := make([]string, 0, len(produced))
	for buildRel := range produced {
		buildRels = append(buildRels, buildRel)
	}
	sort.Strings(buildRels)
	files := make(map[string][]byte, len(buildRels))
	for _, buildRel := range buildRels {
		content, err := os.ReadFile(joinRel(outputDir, buildRel))
		if err != nil {
			return nil, err
		}
		files["site/"+produced[buildRel]] = content
	}
	return files, nil
}

// HomeListingSidecar returns where the home project's curated listing belongs
// in the assembly and the bytes to put there.
//
// The listing is authored in the home project ("docs/projects.toml") because
// it is content, and it is copied into the assembly because both renderings of
// it -- the front page's cards and the generated "/projects/" page -- are
// produced on every deploy, including deploys the home project has nothing to
// do with.
//
// A home project that declares no listing is a real state and returns an empty
// path; a malformed one is a hard error naming the file, reported here rather
// than at the far end where the document is no longer in reach.
func HomeListingSidecar(sourceDir, slug string) (string, []byte, error) {
	source := filepath.Join(sourceDir,
		filepath.Join(strings.Split(listing.SourceFile, "/")...))
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, nil
	}
	curated, err := listing.Load(source)
	if err != nil {
		return "", nil, err
	}
	return "manifests/" + slug + listing.SidecarSuffix,
		[]byte(listing.RenderSidecar(curated, slug)), nil
}
