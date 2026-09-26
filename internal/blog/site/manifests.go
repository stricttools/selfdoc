package site

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/util"
)

// MergePostLists returns the union of a build's post list and the overlay's,
// by slug.
//
// The build wins on a slug both carry -- it just re-rendered that post from
// the tag it was released at. What the overlay contributes is the posts the
// build does not carry at all: the ones published between releases.
//
// This runs when a full build is grafted, not when the assembly is read: the
// overlay stays the one authority on a project's posts, and stays a complete
// list, so republishing after deleting a post still removes it from the site.
func MergePostLists(basePosts, overlayPosts []any) []any {
	merged := make([]any, 0, len(basePosts)+len(overlayPosts))
	merged = append(merged, basePosts...)
	known := map[string]bool{}
	for _, post := range merged {
		table, ok := asTable(post)
		if !ok {
			continue
		}
		known[util.PythonStrOrEmpty(table["slug"])] = true
	}
	for _, post := range overlayPosts {
		table, ok := asTable(post)
		if !ok {
			continue
		}
		slug := util.PythonStrOrEmpty(table["slug"])
		if slug != "" && known[slug] {
			continue
		}
		merged = append(merged, post)
		known[slug] = true
	}
	return merged
}

// LoadAssemblyManifests returns the assembly's per-project manifests with post
// overlays applied.
//
// The *-posts.json files are overlays written by the post publish: they carry
// a complete post list for their slug and replace the base manifest's posts
// array, which is how deleting a post and republishing removes it from the
// site. A full build folds its own posts into the overlay when it is grafted
// (see [MergePostLists]), so replacing here never hides a release's posts
// behind an older overlay. The *-revisions.json and *-files.json sidecars are
// not manifests and are skipped.
//
// The documents come back as decoded JSON rather than as typed manifests: the
// assembly reads fields the typed reader drops and writes the documents back
// out, so a tolerant read that discarded unknown keys would lose them.
// [manifest.Compat] still runs on every one, which is what refuses a document
// in a format this reader does not know.
func LoadAssemblyManifests(manifestsDir string) ([]map[string]any, error) {
	documents := map[string][]byte{}
	info, err := os.Stat(manifestsDir)
	if err == nil && info.IsDir() {
		entries, err := os.ReadDir(manifestsDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if !IsManifestDocument(name) {
				continue
			}
			content, err := os.ReadFile(filepath.Join(manifestsDir, name))
			if err != nil {
				return nil, err
			}
			documents[name] = content
		}
	}
	return ManifestsFromDocuments(documents, manifestsDir)
}

// IsManifestDocument reports whether a file name under manifests/ is one of
// the per-project manifests rather than a sidecar.
//
// The rule is one place because two readers ask it: the directory read, and
// the reader that asks a remote assembly for the same set over the Git Data
// API and has no directory to walk.
func IsManifestDocument(name string) bool {
	return strings.HasSuffix(name, ".json") &&
		!hasAnySuffix(name, ManifestSidecarSuffixes)
}

// ManifestsFromDocuments returns the assembly's per-project manifests, decoded
// from documents keyed by their file name under manifests/, with post overlays
// applied.
//
// This is what [LoadAssemblyManifests] is in terms of, and what a caller that
// read the same documents off a remote assembly calls with the bytes it
// fetched. source names where the documents came from, for the diagnostics.
func ManifestsFromDocuments(
	documents map[string][]byte,
	source string,
) ([]map[string]any, error) {
	baseManifests := make([]map[string]any, 0)
	postOverlays := make([]map[string]any, 0)
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !IsManifestDocument(name) {
			continue
		}
		where := filepath.Join(source, name)
		decoded, err := config.DecodeDocument(documents[name])
		if err != nil {
			return nil, errorf("%s is not valid JSON: %v", where, err)
		}
		data, ok := asTable(decoded)
		if !ok {
			return nil, errorf("%s must contain a JSON object", where)
		}
		if _, err := manifest.Compat(data, where); err != nil {
			var outdated *manifest.OutdatedError
			if errors.As(err, &outdated) {
				return nil, &OutdatedManifestError{Outdated: outdated, Project: manifestProject(name, data)}
			}
			return nil, err
		}
		if strings.HasSuffix(name, "-posts.json") {
			postOverlays = append(postOverlays, data)
		} else {
			baseManifests = append(baseManifests, data)
		}
	}

	if len(postOverlays) > 0 {
		baseBySlug := make(map[string]map[string]any, len(baseManifests))
		for _, base := range baseManifests {
			baseBySlug[util.PythonStrOrEmpty(base["slug"])] = base
		}
		for _, overlay := range postOverlays {
			slug := util.PythonStrOrEmpty(overlay["slug"])
			base, ok := baseBySlug[slug]
			if !ok {
				continue
			}
			posts, present := overlay["posts"]
			if !present {
				posts = []any{}
			}
			base["posts"] = posts
		}
	}

	return baseManifests, nil
}

// OutdatedManifestError is a manifest on the assembly that an older selfdoc
// published: a schema before the current one, with no vocabulary.
//
// The assembly holds no checkout to convert it from, so the fix is on the
// project's side: convert its committed manifest, then publish every project
// again in one pass.
type OutdatedManifestError struct {
	// Outdated is the reader's own refusal, naming the document and what it
	// declared.
	Outdated *manifest.OutdatedError
	// Project is the slug the document belongs to.
	Project string
}

func (e *OutdatedManifestError) Error() string {
	return fmt.Sprintf(
		"%s %s, and this selfdoc reads manifest schema_version %d only: project %s was published by an older selfdoc, and its manifest carries no vocabulary. Convert the project's committed manifest with 'selfdoc layout migrate' in its checkout, as every other project's, then publish them all once with 'selfdoc assembly republish-all --home <home checkout> --repo <checkout> --repo <checkout> ...', which replaces every project's manifest on the site.",
		e.Outdated.Source, e.Outdated.Declares(), manifest.SchemaVersion, util.PythonRepr(e.Project))
}

// manifestProject is the project a manifest document belongs to: the slug it
// declares, else its file name's stem without a kind suffix.
func manifestProject(name string, data map[string]any) string {
	if slug := util.PythonStrOrEmpty(data["slug"]); slug != "" {
		return slug
	}
	stem := strings.TrimSuffix(name, ".json")
	return strings.TrimSuffix(stem, "-posts")
}

// ListingSidecarPath is where the assembly keeps the home project's curated
// listing.
func ListingSidecarPath(manifestsDir, homeSlug string) string {
	return filepath.Join(manifestsDir, homeSlug+listing.SidecarSuffix)
}

// LoadListingFor returns the home project's curated listing.
//
// The listing is authored in the home project as its own projects.toml and
// copied here by that project's deploy. A roster that declares a home declares
// its listing with it, so the sidecar's absence is not a state to render
// around -- it means the step that should have pushed the file did not run,
// and the honest answer is to say which file and which step rather than to
// publish a listing nobody curated at the curated one's address.
//
// Returns nil only when the tree declares no home project at all, which the
// deploy path never does: the roster requires one.
func LoadListingFor(manifestsDir, homeSlug string) (*listing.Listing, error) {
	if homeSlug == "" {
		return nil, nil
	}
	path := ListingSidecarPath(manifestsDir, homeSlug)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errorf(
			"%s does not exist, so the assembly carries no curated project "+
				"listing for its home project %s. The listing is authored in "+
				"that project as %s and copied here by its deploy ('selfdoc "+
				"assembly integrate' with scope 'full' or 'docs'), which is "+
				"what should have written this file. Add %s to %s if it has "+
				"none, then deploy %s once before generating the shared files.",
			path, util.PythonRepr(homeSlug), listing.SourceFile,
			listing.SourceFile, util.PythonRepr(homeSlug), util.PythonRepr(homeSlug),
		)
	}
	loaded, err := listing.LoadSidecar(path)
	if err != nil {
		return nil, err
	}
	return &loaded, nil
}

// HomePagePaths returns every site-relative HTML page the home project
// published.
//
// Read from the published-file record rather than guessed from the tree: the
// home project's pages sit at the site root beside other projects' directories
// and the generated artifacts, so "which files are the home project's" is a
// question only its own record answers.
func HomePagePaths(manifestsDir, homeSlug string) ([]string, error) {
	if homeSlug == "" {
		return []string{}, nil
	}
	record, err := LoadFilesManifest(FilesManifestPath(manifestsDir, homeSlug))
	if err != nil {
		return nil, err
	}
	pages := make([]string, 0)
	for _, paths := range record {
		for _, path := range paths {
			if strings.HasSuffix(path, ".html") &&
				strings.Split(path, "/")[0] != address.PostsPrefix {
				pages = append(pages, path)
			}
		}
	}
	return sortedUnique(pages), nil
}

// HomeOwnedRootNames returns the top-level names under site/ the home project
// published.
//
// The home project's pages are at the site root, so its directories sit beside
// the other projects' subtrees. Membership reconciliation and the roster check
// both walk those directories looking for projects, and without this they
// would read the home project's cv/ as an undeclared project and delete it.
func HomeOwnedRootNames(manifestsDir, homeSlug string) (map[string]bool, error) {
	names := map[string]bool{}
	if homeSlug == "" {
		return names, nil
	}
	record, err := LoadFilesManifest(FilesManifestPath(manifestsDir, homeSlug))
	if err != nil {
		return nil, err
	}
	for _, paths := range record {
		for _, path := range paths {
			if !strings.Contains(path, "/") {
				continue
			}
			head := strings.Split(path, "/")[0]
			if head == address.PostsPrefix {
				continue
			}
			names[head] = true
		}
	}
	return names, nil
}

// LoadProjectsJSON returns the derived membership record at path.
//
// A malformed file is a hard error rather than a fresh empty mapping:
// rewriting it would silently drop every other project's record.
func LoadProjectsJSON(path string) (map[string]any, error) {
	data := map[string]any{}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return data, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if util.PythonStrip(string(content)) == "" {
		return data, nil
	}
	decoded, decodeErr := config.DecodeDocument(content)
	if decodeErr != nil {
		return nil, errorf("%s is not valid JSON: %v", path, decodeErr)
	}
	table, ok := asTable(decoded)
	if !ok {
		return nil, errorf("%s must contain a JSON object", path)
	}
	return table, nil
}

// RenderProjectsJSON returns the JSON text of a derived membership record.
func RenderProjectsJSON(data map[string]any) (string, error) {
	encoded, err := util.PythonJSONIndent2(data)
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

// RecordMembership records what slug just deployed and returns the new
// mapping.
//
// projects.json is derived state: it records what each declared project last
// deployed, and a deploy can only write a record for a slug the roster
// declares. Membership therefore cannot grow as a side effect of a dispatch --
// an undeclared slug is refused, naming the file that would have to declare
// it.
func RecordMembership(
	path string,
	roster map[string]RosterEntry,
	slug, repo, ref, version string,
	handle *effects.Handle,
) (map[string]any, error) {
	entry, ok := roster[slug]
	if !ok {
		slugs := make([]string, 0, len(roster))
		for declaredSlug := range roster {
			slugs = append(slugs, declaredSlug)
		}
		sort.Strings(slugs)
		declared := strings.Join(slugs, ", ")
		if declared == "" {
			declared = "(none)"
		}
		return nil, errorf(
			"%s is not declared in %s, so the assembly will not publish it. "+
				"Membership is declared, never accumulated by a deploy. Add a "+
				"[[project]] block naming slug = %s and its repo. Declared "+
				"projects: %s.",
			util.PythonRepr(slug), RosterPath, util.PythonRepr(slug), declared,
		)
	}
	if repo != "" && entry.Repo != repo {
		return nil, errorf(
			"%s declares %s as %s, but this deploy came from %s. One slug has "+
				"one owning repository; fix the declaration or dispatch under "+
				"the right slug.",
			RosterPath, util.PythonRepr(slug), entry.Repo, repo,
		)
	}
	data, err := LoadProjectsJSON(path)
	if err != nil {
		return nil, err
	}
	data[slug] = map[string]any{"repo": entry.Repo, "ref": ref, "version": version}
	rendered, err := RenderProjectsJSON(data)
	if err != nil {
		return nil, err
	}
	if err := handle.Write(path, []byte(rendered), effects.ModeDefault); err != nil {
		return nil, err
	}
	return data, nil
}

// ManifestFilesFor returns every file under manifestsDir that belongs to slug.
//
// "Belongs" is the base manifest plus every kind sidecar -- the posts overlay,
// the revisions sidecar, the published-file record, and anything added later,
// since the rule is the "<slug>-" prefix rather than a closed list of kinds.
func ManifestFilesFor(manifestsDir, slug string) ([]string, error) {
	info, err := os.Stat(manifestsDir)
	if err != nil || !info.IsDir() {
		return []string{}, nil
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
	found := make([]string, 0)
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		stem := strings.TrimSuffix(name, ".json")
		if stem == slug || strings.HasPrefix(stem, slug+"-") {
			found = append(found, filepath.Join(manifestsDir, name))
		}
	}
	return found, nil
}

// manifestOwner returns the declared slug a manifest file's stem belongs to,
// reporting false when no declared project owns it.
func manifestOwner(stem string, declared map[string]bool) (string, bool) {
	if declared[stem] {
		return stem, true
	}
	if index := strings.LastIndex(stem, "-"); index >= 0 {
		head := stem[:index]
		if declared[head] {
			return head, true
		}
	}
	return "", false
}

// ReconcileSummary names what a reconciliation retired and every path it
// removed.
type ReconcileSummary struct {
	// Retired is every slug the roster no longer declares, sorted.
	Retired []string
	// Removed is every path the reconciliation deleted, in deletion order.
	Removed []string
}

// ReconcileMembership removes every trace of a project the roster no longer
// declares.
//
// A project drops out of the assembly by leaving the roster, and this is what
// leaving costs it: its site subtree, every one of its manifest kinds, its
// derived membership record, and -- because the search index is rebuilt from
// scratch whenever anything went -- its entries in the index.
//
// It takes the whole [Roster] rather than the mapping alone, because which
// project is home decides which directories under site/ are project subtrees
// at all: the home project's pages sit at the site root, so telling its
// directories from a project subtree needs to know which project is home.
func ReconcileMembership(
	assemblyDir string,
	roster *Roster,
	handle *effects.Handle,
) (*ReconcileSummary, error) {
	siteDir := filepath.Join(assemblyDir, "site")
	manifestsDir := filepath.Join(assemblyDir, "manifests")
	projectsJSON := filepath.Join(assemblyDir, ProjectsPath)
	declared := map[string]bool{}
	for _, slug := range roster.Slugs() {
		declared[slug] = true
	}
	// The site root IS the home project's content root, so a page of its at
	// cv/index.html emits at site/cv/ -- one level up from where every other
	// project's pages sit, and indistinguishable from a project subtree by
	// position alone. Its published-file record is what tells them apart.
	homeDirs, err := HomeOwnedRootNames(manifestsDir, roster.Home)
	if err != nil {
		return nil, err
	}

	retired := map[string]bool{}
	removed := make([]string, 0)

	if info, err := os.Stat(siteDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(siteDir)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			path := filepath.Join(siteDir, name)
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				continue
			}
			if declared[name] || containsString(SiteReservedDirs, name) {
				continue
			}
			if homeDirs[name] {
				continue
			}
			retired[name] = true
		}
	}

	membership, err := LoadProjectsJSON(projectsJSON)
	if err != nil {
		return nil, err
	}
	membershipSlugs := make([]string, 0, len(membership))
	for slug := range membership {
		membershipSlugs = append(membershipSlugs, slug)
	}
	sort.Strings(membershipSlugs)
	for _, slug := range membershipSlugs {
		if !declared[slug] {
			retired[slug] = true
		}
	}

	retiredSlugs := make([]string, 0, len(retired))
	for slug := range retired {
		retiredSlugs = append(retiredSlugs, slug)
	}
	sort.Strings(retiredSlugs)

	for _, slug := range retiredSlugs {
		subtree := filepath.Join(siteDir, slug)
		if info, err := os.Stat(subtree); err == nil && info.IsDir() {
			if err := handle.RmTree(subtree); err != nil {
				return nil, err
			}
			removed = append(removed, subtree)
		}
		// A retired project's posts are not in its subtree -- they are
		// site-level, at blog/<post-slug>/ -- so the record is what says which
		// of them were its. Read it before it is deleted below.
		claimed, err := ClaimedSitePaths(manifestsDir, slug)
		if err != nil {
			return nil, err
		}
		for _, path := range claimed {
			target := joinRel(siteDir, path)
			if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
				if err := handle.Remove(target); err != nil {
					return nil, err
				}
				removed = append(removed, target)
			}
		}
		kinds, err := ManifestFilesFor(manifestsDir, slug)
		if err != nil {
			return nil, err
		}
		for _, path := range kinds {
			if err := handle.Remove(path); err != nil {
				return nil, err
			}
			removed = append(removed, path)
		}
	}
	if len(retiredSlugs) > 0 {
		if _, err := PruneEmptyDirs(filepath.Join(siteDir, address.PostsPrefix), handle); err != nil {
			return nil, err
		}
	}

	// A manifest whose stem matches no declared project at all is stale even
	// when no subtree or record named it -- a hand-dropped file, or a kind
	// sidecar left by a slug that was renamed.
	if info, err := os.Stat(manifestsDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(manifestsDir)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		alreadyRemoved := make(map[string]bool, len(removed))
		for _, path := range removed {
			alreadyRemoved[path] = true
		}
		for _, name := range names {
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			path := filepath.Join(manifestsDir, name)
			if alreadyRemoved[path] {
				continue
			}
			if _, ok := manifestOwner(strings.TrimSuffix(name, ".json"), declared); ok {
				continue
			}
			if err := handle.Remove(path); err != nil {
				return nil, err
			}
			removed = append(removed, path)
		}
	}

	dropped := make([]string, 0)
	for _, slug := range membershipSlugs {
		if !declared[slug] {
			dropped = append(dropped, slug)
		}
	}
	if len(dropped) > 0 {
		for _, slug := range dropped {
			delete(membership, slug)
		}
		rendered, err := RenderProjectsJSON(membership)
		if err != nil {
			return nil, err
		}
		if err := handle.Write(projectsJSON, []byte(rendered), effects.ModeDefault); err != nil {
			return nil, err
		}
	}

	// pagefind keys its fragments by content hash, so a page that is gone from
	// the tree can still have a fragment on disk. Rebuilding the index from an
	// empty directory is the only way to be sure a retired project stops
	// answering searches.
	indexDir := filepath.Join(siteDir, "pagefind")
	if info, err := os.Stat(indexDir); len(removed) > 0 && err == nil && info.IsDir() {
		if err := handle.RmTree(indexDir); err != nil {
			return nil, err
		}
		removed = append(removed, indexDir)
	}

	return &ReconcileSummary{Retired: retiredSlugs, Removed: removed}, nil
}
