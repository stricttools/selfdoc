package site

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/testisolation/go/hygiene"
)

// -- the posts overlay -------------------------------------------------------

func TestMergePostListsKeepsPostsOnlyTheOverlayCarries(t *testing.T) {
	t.Parallel()
	merged := MergePostLists(
		[]any{map[string]any{"slug": "a"}, map[string]any{"slug": "b"}},
		[]any{map[string]any{"slug": "c"}},
	)
	if got := postSlugs(merged); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("merged = %v", got)
	}
}

func TestMergePostListsLetsTheBuildWinOnASharedSlug(t *testing.T) {
	t.Parallel()
	merged := MergePostLists(
		[]any{map[string]any{"slug": "a", "title": "rebuilt"}},
		[]any{map[string]any{"slug": "a", "title": "older"}},
	)
	want := []any{map[string]any{"slug": "a", "title": "rebuilt"}}
	if !reflect.DeepEqual(merged, want) {
		t.Errorf("merged = %v, want %v", merged, want)
	}
}

func TestMergePostListsSkipsEntriesThatAreNotTables(t *testing.T) {
	t.Parallel()
	merged := MergePostLists(
		[]any{map[string]any{"slug": "a"}}, []any{"not a post", map[string]any{"slug": "b"}},
	)
	if got := postSlugs(merged); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("merged = %v", got)
	}
}

// Folding happens on write, so republishing can still remove a post: the
// overlay replaces the base post list when the assembly is read.
func TestTheOverlayReplacesTheBasePostListWhenRead(t *testing.T) {
	root := assemblyTree(t)
	manifests, err := LoadAssemblyManifests(filepath.Join(root, "manifests"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	alpha := manifestBySlug(t, manifests, "alpha")
	posts, _ := alpha["posts"].([]any)
	if got := postSlugs(posts); !reflect.DeepEqual(got, []string{"old-post"}) {
		t.Errorf("alpha posts = %v, want [old-post]", got)
	}
}

// <slug>-files.json lives under manifests/ but is not one.
func TestThePublishedFileRecordIsNotReadAsAManifest(t *testing.T) {
	root := assemblyTree(t)
	manifests, err := LoadAssemblyManifests(filepath.Join(root, "manifests"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	slugs := make([]string, 0, len(manifests))
	for _, doc := range manifests {
		slug, _ := doc["slug"].(string)
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	if !reflect.DeepEqual(slugs, []string{"alpha", "beta", "home"}) {
		t.Errorf("slugs = %v, want [alpha beta home]", slugs)
	}
}

func TestLoadAssemblyManifestsOfAMissingDirectoryIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	manifests, err := LoadAssemblyManifests(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("manifests = %v, want empty", manifests)
	}
}

// A document in a format this reader does not know is refused rather than read
// tolerantly, which is what the shared compatibility reader is for.
func TestLoadAssemblyManifestsRefusesAnUnknownSchemaVersion(t *testing.T) {
	hygiene.Isolate(t)
	dir := filepath.Join(t.TempDir(), "manifests")
	writeJSON(t, filepath.Join(dir, "alpha.json"), map[string]any{
		"schema_version": 99, "slug": "alpha",
	})
	_, err := LoadAssemblyManifests(dir)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("err = %v, want the unsupported-version refusal", err)
	}
}

// -- the home project's curated listing --------------------------------------

func TestListingSidecarPath(t *testing.T) {
	t.Parallel()
	want := filepath.Join("manifests", "home"+listing.SidecarSuffix)
	if got := ListingSidecarPath("manifests", "home"); got != want {
		t.Errorf("ListingSidecarPath = %q, want %q", got, want)
	}
}

func TestLoadListingForReadsTheGraftedSidecar(t *testing.T) {
	root := assemblyTree(t)
	loaded, err := LoadListingFor(filepath.Join(root, "manifests"), "home")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded listing is nil")
	}
	if got := loaded.Slugs(); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("Slugs() = %v, want [alpha]", got)
	}
}

// The sidecar's absence is not a state to render around -- it means the step
// that should have pushed the file did not run.
func TestLoadListingForRefusesWhenTheSidecarIsAbsent(t *testing.T) {
	hygiene.Isolate(t)
	dir := filepath.Join(t.TempDir(), "manifests")
	_, err := LoadListingFor(dir, "home")
	if err == nil {
		t.Fatal("want a refusal, got none")
	}
	want := ListingSidecarPath(dir, "home") + " does not exist, so the assembly " +
		"carries no curated project listing for its home project 'home'. The " +
		"listing is authored in that project as .stricttools/docs/projects.toml and copied " +
		"here by its deploy ('selfdoc assembly integrate' with scope 'full' or " +
		"'docs'), which is what should have written this file. Add " +
		".stricttools/docs/projects.toml to 'home' if it has none, then deploy 'home' once " +
		"before generating the shared files."
	if err.Error() != want {
		t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), want)
	}
}

// nil comes back only when the tree declares no home project at all, which the
// deploy path never does: the roster requires one.
func TestLoadListingForWithNoHomeIsNil(t *testing.T) {
	hygiene.Isolate(t)
	loaded, err := LoadListingFor(t.TempDir(), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded != nil {
		t.Errorf("loaded = %v, want nil", loaded)
	}
}

// -- what the home project owns at the site root -----------------------------

func TestHomePagePathsReadsThePublishedRecord(t *testing.T) {
	root := assemblyTree(t)
	manifests := filepath.Join(root, "manifests")
	writeJSON(t, filepath.Join(manifests, "home-files.json"),
		filesRecord("home", "release",
			"index.html", "cv/index.html", "blog/hello/index.html", "style.css"))
	pages, err := HomePagePaths(manifests, "home")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []string{"cv/index.html", "index.html"}
	if !reflect.DeepEqual(pages, want) {
		t.Errorf("pages = %v, want %v", pages, want)
	}
}

func TestHomePagePathsWithNoHomeIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	pages, err := HomePagePaths(t.TempDir(), "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("pages = %v, want empty", pages)
	}
}

func TestHomeOwnedRootNamesSkipsThePostsSegmentAndRootFiles(t *testing.T) {
	root := assemblyTree(t)
	manifests := filepath.Join(root, "manifests")
	writeJSON(t, filepath.Join(manifests, "home-files.json"),
		filesRecord("home", "release",
			"index.html", "cv/index.html", "art/deep/thing.png", "blog/hello/index.html"))
	names, err := HomeOwnedRootNames(manifests, "home")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := make([]string, 0, len(names))
	for name := range names {
		got = append(got, name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"art", "cv"}) {
		t.Errorf("names = %v, want [art cv]", got)
	}
}

// -- the derived membership record -------------------------------------------

func TestLoadProjectsJSONOfAnAbsentFileIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	data, err := LoadProjectsJSON(filepath.Join(t.TempDir(), ProjectsPath))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("data = %v, want empty", data)
	}
}

func TestLoadProjectsJSONOfAnEmptyFileIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), ProjectsPath)
	write(t, path, "  \n")
	data, err := LoadProjectsJSON(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("data = %v, want empty", data)
	}
}

// A malformed file is a hard error rather than a fresh empty mapping:
// rewriting it would silently drop every other project's record.
func TestLoadProjectsJSONRefusesAMalformedFile(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), ProjectsPath)
	write(t, path, "{not json")
	_, err := LoadProjectsJSON(path)
	if err == nil || !strings.HasPrefix(err.Error(), path+" is not valid JSON: ") {
		t.Fatalf("err = %v, want the invalid-JSON refusal", err)
	}
}

func TestLoadProjectsJSONRefusesANonObject(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), ProjectsPath)
	write(t, path, "[]")
	_, err := LoadProjectsJSON(path)
	if err == nil || err.Error() != path+" must contain a JSON object" {
		t.Fatalf("err = %v, want the non-object refusal", err)
	}
}

func TestRenderProjectsJSONIsByteIdenticalToThePython(t *testing.T) {
	t.Parallel()
	got, err := RenderProjectsJSON(map[string]any{
		"home":  map[string]any{"repo": "owner/home", "ref": "v0.1.0", "version": "0.1.0"},
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v1.0.0", "version": "1.0.0"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := reference(t, "projects-json.json"); got != want {
		t.Errorf("rendered record:\n%q\nwant:\n%q", got, want)
	}
}

func TestRecordMembershipWritesTheDeclaredRepository(t *testing.T) {
	root := assemblyTree(t)
	path := filepath.Join(root, ProjectsPath)
	data, err := RecordMembership(
		path, fixtureRoster, "alpha", "owner/alpha", "v1.0.0", "1.0.0", handle(),
	)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	entry, _ := data["alpha"].(map[string]any)
	want := map[string]any{"repo": "owner/alpha", "ref": "v1.0.0", "version": "1.0.0"}
	if !reflect.DeepEqual(entry, want) {
		t.Errorf("entry = %v, want %v", entry, want)
	}
	onDisk := readJSON(t, path)
	if _, ok := onDisk["beta"]; !ok {
		t.Error("the rewrite dropped another project's record")
	}
}

// Membership is declared, never accumulated by a deploy.
func TestRecordMembershipRefusesAnUndeclaredSlug(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), ProjectsPath)
	_, err := RecordMembership(
		path, map[string]RosterEntry{}, "alpha", "owner/alpha", "v1", "1.0", handle(),
	)
	want := "'alpha' is not declared in roster.toml, so the assembly will not " +
		"publish it. Membership is declared, never accumulated by a deploy. Add " +
		"a [[project]] block naming slug = 'alpha' and its repo. Declared " +
		"projects: (none)."
	if err == nil || err.Error() != want {
		t.Fatalf("refusal:\n%v\nwant:\n%s", err, want)
	}
	if exists(path) {
		t.Error("the refusal wrote a record anyway")
	}
}

// One slug has one owning repository.
func TestRecordMembershipRefusesADispatchFromAnotherRepository(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), ProjectsPath)
	_, err := RecordMembership(
		path, fixtureRoster, "alpha", "owner/other", "v1", "1.0", handle(),
	)
	want := "roster.toml declares 'alpha' as owner/alpha, but this deploy came " +
		"from owner/other. One slug has one owning repository; fix the " +
		"declaration or dispatch under the right slug."
	if err == nil || err.Error() != want {
		t.Fatalf("refusal:\n%v\nwant:\n%s", err, want)
	}
}

// An empty repo is a caller with nothing to compare, not a mismatch.
func TestRecordMembershipAcceptsNoSourceRepository(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), ProjectsPath)
	data, err := RecordMembership(path, fixtureRoster, "alpha", "", "v1", "1.0", handle())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	entry, _ := data["alpha"].(map[string]any)
	if entry["repo"] != "owner/alpha" {
		t.Errorf("entry = %v, want the declared repository", entry)
	}
}

// -- every manifest kind a slug owns -----------------------------------------

func TestManifestFilesForFindsEveryKind(t *testing.T) {
	root := assemblyTree(t)
	manifests := filepath.Join(root, "manifests")
	writeJSON(t, filepath.Join(manifests, "alpha-revisions.json"), map[string]any{})
	found, err := ManifestFilesFor(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []string{
		filepath.Join(manifests, "alpha-files.json"),
		filepath.Join(manifests, "alpha-posts.json"),
		filepath.Join(manifests, "alpha-revisions.json"),
		filepath.Join(manifests, "alpha.json"),
	}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("found = %v, want %v", found, want)
	}
}

// The rule is the "<slug>-" prefix, so a longer slug that merely starts with
// the same letters is not this project's.
func TestManifestFilesForIsPrefixBounded(t *testing.T) {
	root := assemblyTree(t)
	manifests := filepath.Join(root, "manifests")
	writeJSON(t, filepath.Join(manifests, "alphabet.json"),
		manifestDoc("alphabet", "Alphabet", "1.0.0", nil))
	found, err := ManifestFilesFor(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, path := range found {
		if strings.Contains(path, "alphabet") {
			t.Errorf("found = %v, which claims another project's manifest", found)
		}
	}
}

func TestManifestFilesForOfAMissingDirectoryIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	found, err := ManifestFilesFor(filepath.Join(t.TempDir(), "nope"), "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("found = %v, want empty", found)
	}
}

// -- postSlugs and manifestBySlug --------------------------------------------

// postSlugs lists the slug of every post in a decoded post list.
func postSlugs(posts []any) []string {
	slugs := make([]string, 0, len(posts))
	for _, post := range posts {
		table, ok := post.(map[string]any)
		if !ok {
			continue
		}
		slug, _ := table["slug"].(string)
		slugs = append(slugs, slug)
	}
	return slugs
}

// manifestBySlug picks one manifest out of a loaded set.
func manifestBySlug(t *testing.T, manifests []map[string]any, slug string) map[string]any {
	t.Helper()
	for _, doc := range manifests {
		if got, _ := doc["slug"].(string); got == slug {
			return doc
		}
	}
	t.Fatalf("no manifest for %q", slug)
	return nil
}

// A project with no public version still deploys, and what it deploys is
// recorded: the literal every membership reader treats as "no version to
// show", never an empty field that reads as a lost one.
func TestRecordMembershipRecordsAnUnversionedProject(t *testing.T) {
	root := assemblyTree(t)
	path := filepath.Join(root, ProjectsPath)
	data, err := RecordMembership(
		path, fixtureRoster, "alpha", "owner/alpha", "main",
		config.UnversionedVersion, handle(),
	)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	entry, _ := data["alpha"].(map[string]any)
	want := map[string]any{
		"repo": "owner/alpha", "ref": "main", "version": config.UnversionedVersion,
	}
	if !reflect.DeepEqual(entry, want) {
		t.Errorf("entry = %v, want %v", entry, want)
	}
}
