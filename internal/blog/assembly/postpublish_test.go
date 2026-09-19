package assembly

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
)

// The post publish has no engine function of its own: it is the command
// composing the pieces in this package with the model's own. What this suite
// asserts is that the pieces compose into the publish the command performs --
// the split, the cross-project refusal, the record staging, the two manifest
// sidecars, the commit, the shared-elements dispatch that follows it, and the
// second push that archives the resolved Markdown.
//
// stagePostPublish is that composition, in the order the command runs it.
func stagePostPublish(
	t *testing.T,
	repo, slug, outputDir string,
	sidecars map[string][]byte,
) (map[string][]byte, []string, error) {
	t.Helper()
	handle := effects.Unbound()

	buildRels, err := site.BuildOutputPaths(outputDir, true)
	if err != nil {
		return nil, nil, err
	}
	files := map[string][]byte{}
	var produced []string
	for buildRel, siteRel := range site.SplitBuildOutput(buildRels, slug, false) {
		content, err := os.ReadFile(filepath.Join(outputDir,
			filepath.Join(strings.Split(buildRel, "/")...)))
		if err != nil {
			return nil, nil, err
		}
		files["site/"+siteRel] = content
		produced = append(produced, siteRel)
	}
	slices.Sort(produced)
	for name, content := range sidecars {
		files["manifests/"+slug+name] = content
	}

	roster, err := LoadRemoteRoster(handle, repo)
	if err != nil {
		return nil, nil, err
	}
	claims, err := RemotePostClaims(handle, repo, slug, roster.Slugs())
	if err != nil {
		return nil, nil, err
	}
	if err := site.RefuseForeignPostOverwrite(slug, produced, claims); err != nil {
		return nil, nil, err
	}
	deletePaths, err := site.StagePublishedRecord(
		RemoteTextFetcher(handle), repo, slug, "posts", produced, files,
	)
	if err != nil {
		return nil, nil, err
	}
	return files, deletePaths, nil
}

// postBuild writes what a posts-only build leaves behind: the post pages plus
// the listing page the project's own standalone site serves.
func postBuild(t *testing.T, slugs ...string) string {
	t.Helper()
	pages := []string{"blog/index.html"}
	for _, slug := range slugs {
		pages = append(pages, "blog/"+slug+"/index.html")
	}
	return buildTree(t, pages...)
}

func TestAPostPublishStagesThePostsAndBothManifestSidecars(t *testing.T) {
	gh := publishFixture(t, nil)
	files, deletePaths, err := stagePostPublish(t, testRepo, "alpha",
		postBuild(t, "hello"), map[string][]byte{
			"-posts.json":     []byte(`{"slug": "alpha", "posts": []}`),
			"-revisions.json": []byte(`{"alpha": {}}`),
		})
	if err != nil {
		t.Fatalf("staging: %v", err)
	}
	if len(deletePaths) != 0 {
		t.Fatalf("a first publish deletes %v", deletePaths)
	}
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	want := []string{
		"manifests/alpha-files.json",
		"manifests/alpha-posts.json",
		"manifests/alpha-revisions.json",
		"site/blog/hello/index.html",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("the publish stages %v, want %v", paths, want)
	}

	// The project's own blog listing is never staged: the assembled site's
	// blog index lists every project's posts and is written by the
	// shared-elements rebuild the publish dispatches afterwards.
	if _, present := files["site/blog/index.html"]; present {
		t.Error("the project's standalone blog listing was staged")
	}
	if _, present := files["site/alpha/blog/hello/index.html"]; present {
		t.Error("a post was staged under the project slug")
	}

	record, err := site.ParseFilesManifest(
		string(files["manifests/alpha-files.json"]), "manifests/alpha-files.json")
	if err != nil {
		t.Fatalf("the staged record does not parse: %v", err)
	}
	if !reflect.DeepEqual(record["posts"], []string{"blog/hello/index.html"}) {
		t.Fatalf("the record claims %v for posts", record["posts"])
	}

	// Everything reaches the branch in one commit.
	result, err := PushFilesToRepo(effects.Unbound(), testRepo, files,
		"posts: alpha", DefaultBranch, deletePaths)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !result.Changed {
		t.Error("the publish reported no change")
	}
	if got := len(ghCommitCalls(t, gh)); got != 1 {
		t.Fatalf("the publish made %d commit(s), want 1", got)
	}
	if got := gh.Text("site/blog/hello/index.html"); got == "" {
		t.Error("the post did not reach the assembly")
	}
}

func TestAPostPublishPrunesThePostsItNoLongerPublishes(t *testing.T) {
	publishFixture(t, map[string][]byte{
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"posts": {"blog/hello/index.html", "blog/gone/index.html"},
		}),
	})
	files, deletePaths, err := stagePostPublish(t, testRepo, "alpha",
		postBuild(t, "hello"), nil)
	if err != nil {
		t.Fatalf("staging: %v", err)
	}
	if !reflect.DeepEqual(deletePaths, []string{"site/blog/gone/index.html"}) {
		t.Fatalf("the publish deletes %v", deletePaths)
	}
	record, err := site.ParseFilesManifest(
		string(files["manifests/alpha-files.json"]), "manifests/alpha-files.json")
	if err != nil {
		t.Fatalf("the staged record does not parse: %v", err)
	}
	if !reflect.DeepEqual(record["posts"], []string{"blog/hello/index.html"}) {
		t.Fatalf("the record still claims %v", record["posts"])
	}
}

func TestAPostPublishRefusesAnotherProjectsPostSlug(t *testing.T) {
	publishFixture(t, map[string][]byte{
		"manifests/beta-files.json": filesRecord(t, "beta", map[string][]string{
			"posts": {"blog/hello/index.html"},
		}),
	})
	_, _, err := stagePostPublish(t, testRepo, "alpha", postBuild(t, "hello"), nil)
	if err == nil {
		t.Fatal("a post publish overwrote another project's post")
	}
	for _, want := range []string{"blog/hello/index.html", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestAPostPublishLeavesADocumentationPageAlone(t *testing.T) {
	// A publisher prunes only what it published before and does not publish
	// now; a documentation page belongs to another owner.
	publishFixture(t, map[string][]byte{
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"docs":  {"alpha/index.html"},
			"posts": {"blog/hello/index.html"},
		}),
	})
	_, deletePaths, err := stagePostPublish(t, testRepo, "alpha",
		postBuild(t, "hello"), nil)
	if err != nil {
		t.Fatalf("staging: %v", err)
	}
	if len(deletePaths) != 0 {
		t.Fatalf("the post publish deletes %v", deletePaths)
	}
}

func TestAFailedClaimsReadAbortsThePostPublish(t *testing.T) {
	gh := publishFixture(t, nil)
	gh.Fail("/contents/manifests/alpha-files.json", 1, rateLimit)
	if _, _, err := stagePostPublish(t, testRepo, "alpha",
		postBuild(t, "hello"), nil); !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Fatalf("the aborted publish made %d commit(s)", got)
	}
}

func TestTheSharedElementsRebuildFollowsAPostPublish(t *testing.T) {
	// The publish writes through the Git Data API, which no workflow ran, so
	// the listing, the feed, the sitemap and the search index are stale until
	// a deploy regenerates them.
	dispatch := SharedOnlyDispatch(testRepo)
	if dispatch.Endpoint != "/repos/"+testRepo+"/dispatches" {
		t.Fatalf("endpoint = %q", dispatch.Endpoint)
	}
	client := clientPayload(t, dispatch)
	if client["scope"] != SharedOnlyScope {
		t.Fatalf("the dispatch carries %v", client)
	}
	if _, present := client["slug"]; present {
		t.Error("a shared-elements dispatch names a project")
	}
}

func TestTheSecondRepoTakesTheResolvedMarkdown(t *testing.T) {
	// A project may archive its posts' resolved Markdown in a second
	// repository; that is an ordinary push through the same path.
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/posts", map[string][]byte{
		"alpha/hello.md": []byte("# Hello\n\nResolved body.\n"),
	}, "posts: alpha", DefaultBranch, nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !result.Changed {
		t.Error("the archive push reported no change")
	}
	if got := gh.Text("alpha/hello.md"); got != "# Hello\n\nResolved body.\n" {
		t.Fatalf("the archive holds %q", got)
	}
}
