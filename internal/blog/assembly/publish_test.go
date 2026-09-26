package assembly

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
)

// buildTree writes a local documentation build, as a build leaves it, and
// returns its output root.
func buildTree(t *testing.T, pages ...string) string {
	t.Helper()
	if len(pages) == 0 {
		pages = []string{"index.html", "guide/index.html"}
	}
	output := filepath.Join(t.TempDir(), "stricttools", ".docs-cache", "build")
	for _, page := range pages {
		path := filepath.Join(output, filepath.Join(strings.Split(page, "/")...))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("<html>"+page+"</html>"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return output
}

// writeBuildFile adds one file to an existing build tree.
func writeBuildFile(t *testing.T, output, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(output, filepath.Join(strings.Split(rel, "/")...))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// publishFixture is a fake assembly declaring alpha, beta and home, holding
// whatever else the test names.
func publishFixture(t *testing.T, extra map[string][]byte) *fakeGH {
	t.Helper()
	return remoteFixture(t, extra)
}

// publishAlpha runs a documentation publish for alpha.
func publishAlpha(t *testing.T, output string, manifestPath string) (*PublishSummary, error) {
	t.Helper()
	return PublishProjectDocs(PublishOptions{
		Repo:         testRepo,
		Slug:         "alpha",
		OutputDir:    output,
		Version:      "1.0.0",
		ManifestPath: manifestPath,
	}, effects.Unbound())
}

// -- the publish -------------------------------------------------------------

func TestADocumentationPageReachesTheAssembly(t *testing.T) {
	gh := publishFixture(t, nil)
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := gh.Text("site/alpha/index.html"); got != "<html>index.html</html>" {
		t.Fatalf("the assembly holds %q", got)
	}
}

func TestABinaryAssetSurvivesTheTrip(t *testing.T) {
	gh := publishFixture(t, nil)
	output := buildTree(t)
	writeBuildFile(t, output, "logo.png", pngBytes)
	if _, err := publishAlpha(t, output, ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	stored, ok := gh.Content("site/alpha/logo.png")
	if !ok || !reflect.DeepEqual(stored, pngBytes) {
		t.Fatalf("the assembly holds %v", stored)
	}
}

func TestTheManifestTravelsWithThePages(t *testing.T) {
	gh := publishFixture(t, nil)
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifest,
		[]byte(`{"slug": "alpha", "version": "1.0.0"}`), 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	if _, err := publishAlpha(t, buildTree(t), manifest); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(gh.Text("manifests/alpha.json")), &document); err != nil {
		t.Fatalf("the pushed manifest is not JSON: %v", err)
	}
	if document["slug"] != "alpha" {
		t.Fatalf("the pushed manifest is %v", document)
	}
}

func TestThePublishedFileRecordNamesThisPublisher(t *testing.T) {
	gh := publishFixture(t, nil)
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	record, err := site.ParseFilesManifest(
		gh.Text("manifests/alpha-files.json"), "manifests/alpha-files.json")
	if err != nil {
		t.Fatalf("the pushed record does not parse: %v", err)
	}
	want := []string{"alpha/guide/index.html", "alpha/index.html"}
	if !reflect.DeepEqual(record["docs"], want) {
		t.Fatalf("the record claims %v for docs, want %v", record["docs"], want)
	}
}

func TestTheMembershipRecordIsRefreshed(t *testing.T) {
	gh := publishFixture(t, map[string][]byte{
		site.ProjectsPath: []byte(
			`{"alpha": {"repo": "owner/alpha", "ref": "v0.9.0", "version": "0.9.0"}}`),
	})
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	membership := decodedMembership(t, gh)
	alpha, _ := membership["alpha"].(map[string]any)
	if alpha["version"] != "1.0.0" {
		t.Fatalf("the record claims version %v", alpha["version"])
	}
	// A documentation publish has no tag, so it invents none and drops none.
	if alpha["ref"] != "v0.9.0" {
		t.Fatalf("the record claims ref %v, want the last released one", alpha["ref"])
	}
	if alpha["repo"] != "owner/alpha" {
		t.Fatalf("the record claims repo %v", alpha["repo"])
	}
}

func TestANeverReleasedProjectRecordsNoRef(t *testing.T) {
	gh := publishFixture(t, nil)
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	alpha, _ := decodedMembership(t, gh)["alpha"].(map[string]any)
	if _, present := alpha["ref"]; present {
		t.Fatalf("a never-released project recorded ref %v", alpha["ref"])
	}
}

func TestOtherProjectsKeepTheirMembershipRecords(t *testing.T) {
	gh := publishFixture(t, map[string][]byte{
		site.ProjectsPath: []byte(
			`{"beta": {"repo": "owner/beta", "ref": "v2.0.0", "version": "2.0.0"}}`),
	})
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	beta, _ := decodedMembership(t, gh)["beta"].(map[string]any)
	if beta["version"] != "2.0.0" {
		t.Fatalf("the other project's record now reads %v", beta)
	}
}

// decodedMembership is the membership record the assembly holds.
func decodedMembership(t *testing.T, gh *fakeGH) map[string]any {
	t.Helper()
	var membership map[string]any
	if err := json.Unmarshal([]byte(gh.Text(site.ProjectsPath)), &membership); err != nil {
		t.Fatalf("the membership record is not JSON: %v", err)
	}
	return membership
}

func TestEverythingTravelsInOneCommit(t *testing.T) {
	gh := publishFixture(t, nil)
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 1 {
		t.Fatalf("the publish made %d commit(s), want 1", got)
	}
}

func TestAnUnchangedPageUploadsNothing(t *testing.T) {
	gh := publishFixture(t, map[string][]byte{
		"site/alpha/index.html": []byte("<html>index.html</html>"),
	})
	if _, err := publishAlpha(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	for _, blob := range ghUploadedBlobs(t, gh) {
		if string(blob) == "<html>index.html</html>" {
			t.Fatal("an unchanged page was uploaded anyway")
		}
	}
}

// -- deletions ---------------------------------------------------------------

func TestAPageRemovedLocallyDisappearsRemotely(t *testing.T) {
	// The second publish no longer builds guide/, so guide/ goes.
	gh := publishFixture(t, map[string][]byte{
		"site/alpha/guide/index.html": []byte("<html>guide/index.html</html>"),
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"docs": {"alpha/index.html", "alpha/guide/index.html"},
		}),
	})
	summary, err := publishAlpha(t, buildTree(t, "index.html"), "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !reflect.DeepEqual(summary.Deleted, []string{"site/alpha/guide/index.html"}) {
		t.Fatalf("deleted = %v", summary.Deleted)
	}
	if _, present := gh.Content("site/alpha/guide/index.html"); present {
		t.Error("the removed page is still on the assembly")
	}
}

func TestADeletionDropsOutOfThePublishedFileRecord(t *testing.T) {
	gh := publishFixture(t, map[string][]byte{
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"docs": {"alpha/index.html", "alpha/guide/index.html"},
		}),
	})
	if _, err := publishAlpha(t, buildTree(t, "index.html"), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	record, err := site.ParseFilesManifest(
		gh.Text("manifests/alpha-files.json"), "manifests/alpha-files.json")
	if err != nil {
		t.Fatalf("the pushed record does not parse: %v", err)
	}
	if !reflect.DeepEqual(record["docs"], []string{"alpha/index.html"}) {
		t.Fatalf("the record still claims %v", record["docs"])
	}
}

func TestADocumentationPublishNeverDeletesAReleasePage(t *testing.T) {
	// It prunes what it published, never what somebody else did.
	gh := publishFixture(t, map[string][]byte{
		"site/alpha/reference/index.html": []byte("released"),
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"release": {"alpha/reference/index.html"},
			"docs":    {},
		}),
	})
	summary, err := publishAlpha(t, buildTree(t), "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(summary.Deleted) != 0 {
		t.Fatalf("deleted %v", summary.Deleted)
	}
	if _, present := gh.Content("site/alpha/reference/index.html"); !present {
		t.Error("the release's page was deleted")
	}
}

func TestADocumentationPublishNeverDeletesAPost(t *testing.T) {
	gh := publishFixture(t, map[string][]byte{
		"site/blog/hello/index.html": []byte("a post"),
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"posts": {"blog/hello/index.html"},
			"docs":  {"blog/hello/index.html"},
		}),
	})
	summary, err := publishAlpha(t, buildTree(t), "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(summary.Deleted) != 0 {
		t.Fatalf("deleted %v", summary.Deleted)
	}
	if _, present := gh.Content("site/blog/hello/index.html"); !present {
		t.Error("the post was deleted")
	}
}

func TestTheFirstPublishDeletesNothing(t *testing.T) {
	publishFixture(t, map[string][]byte{
		"site/alpha/legacy.html": []byte("who wrote this"),
	})
	summary, err := publishAlpha(t, buildTree(t), "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(summary.Deleted) != 0 {
		t.Fatalf("deleted %v", summary.Deleted)
	}
}

// -- membership is declared, never created -----------------------------------

func TestPublishingIntoAnUndeclaredSlugIsAHardError(t *testing.T) {
	publishFixture(t, nil)
	_, err := PublishProjectDocs(PublishOptions{
		Repo: testRepo, Slug: "gamma", OutputDir: buildTree(t), Version: "1.0.0",
	}, effects.Unbound())
	if err == nil {
		t.Fatal("an undeclared slug was published into")
	}
	if !strings.Contains(err.Error(), "not declared in "+site.RosterPath) {
		t.Errorf("err = %q, want it to name the declaration", err)
	}
	if !strings.Contains(err.Error(), "alpha, beta, home") {
		t.Errorf("err = %q, want it to list the declared projects", err)
	}
}

func TestPublishingWithoutARosterIsAHardError(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{})
	if _, err := publishAlpha(t, buildTree(t), ""); err == nil {
		t.Fatal("a publish ran against an assembly with no roster")
	}
}

func TestACorruptPublishedFileRecordIsAHardError(t *testing.T) {
	publishFixture(t, map[string][]byte{
		"manifests/alpha-files.json": []byte("{not json"),
	})
	_, err := publishAlpha(t, buildTree(t), "")
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err = %v, want the unreadable-record refusal", err)
	}
}

func TestACorruptMembershipRecordIsAHardError(t *testing.T) {
	publishFixture(t, map[string][]byte{
		site.ProjectsPath: []byte("[]"),
	})
	_, err := publishAlpha(t, buildTree(t), "")
	if err == nil || !strings.Contains(err.Error(), "must contain a JSON object") {
		t.Fatalf("err = %v, want the wrong-shape refusal", err)
	}
}

// -- the site-level blog is one namespace ------------------------------------

func TestAPublishRefusesToOverwriteAnotherProjectsPost(t *testing.T) {
	publishFixture(t, map[string][]byte{
		"manifests/beta-files.json": filesRecord(t, "beta", map[string][]string{
			"posts": {"blog/hello/index.html"},
		}),
	})
	output := buildTree(t, "index.html", "blog/hello/index.html")
	_, err := publishAlpha(t, output, "")
	if err == nil {
		t.Fatal("a publish overwrote another project's post")
	}
	for _, want := range []string{"blog/hello/index.html", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestAPublishCarriesItsOwnPostsToTheSiteLevelBlog(t *testing.T) {
	gh := publishFixture(t, nil)
	output := buildTree(t, "index.html", "blog/hello/index.html")
	summary, err := publishAlpha(t, output, "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !slices.Contains(summary.Published, "blog/hello/index.html") {
		t.Fatalf("published = %v", summary.Published)
	}
	if _, present := gh.Content("site/blog/hello/index.html"); !present {
		t.Error("the post did not reach the site-level blog")
	}
}

// -- a failed read never becomes an empty record -----------------------------

func TestAFailedClaimsReadAbortsTheDocsPublish(t *testing.T) {
	// The defect itself: a transient failure used to erase the claims.
	gh := publishFixture(t, map[string][]byte{
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"release": {"alpha/index.html", "alpha/api/index.html"},
		}),
	})
	gh.Fail("/contents/manifests/alpha-files.json", 1, rateLimit)
	if _, err := publishAlpha(t, buildTree(t), ""); !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Fatalf("the aborted publish made %d commit(s)", got)
	}
	if got := len(ghUploadedBlobs(t, gh)); got != 0 {
		t.Fatalf("the aborted publish uploaded %d blob(s)", got)
	}
	// The release owner's claims are what the remote still holds.
	record, err := site.ParseFilesManifest(
		gh.Text("manifests/alpha-files.json"), "manifests/alpha-files.json")
	if err != nil {
		t.Fatalf("the record no longer parses: %v", err)
	}
	want := []string{"alpha/api/index.html", "alpha/index.html"}
	if !reflect.DeepEqual(record["release"], want) {
		t.Fatalf("the record now claims %v for release, want %v", record["release"], want)
	}
}

func TestAFailedMembershipReadAbortsTheDocsPublish(t *testing.T) {
	// A rewritten membership record naming only this project destroys the rest.
	gh := publishFixture(t, nil)
	gh.Fail("/contents/"+site.ProjectsPath, 1, rateLimit)
	if _, err := publishAlpha(t, buildTree(t), ""); !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Fatalf("the aborted publish made %d commit(s)", got)
	}
}

func TestAGenuine404IsStillAFirstPublish(t *testing.T) {
	// Nothing regressed: an assembly holding no record for alpha publishes.
	gh := publishFixture(t, nil)
	summary, err := publishAlpha(t, buildTree(t, "index.html"), "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(summary.Deleted) != 0 {
		t.Fatalf("a first publish deleted %v", summary.Deleted)
	}
	record, err := site.ParseFilesManifest(
		gh.Text("manifests/alpha-files.json"), "manifests/alpha-files.json")
	if err != nil {
		t.Fatalf("the pushed record does not parse: %v", err)
	}
	if !reflect.DeepEqual(record["docs"], []string{"alpha/index.html"}) {
		t.Fatalf("the record claims %v", record["docs"])
	}
}

// -- the home project --------------------------------------------------------

// publishHome runs a documentation publish for the roster's home project.
func publishHome(t *testing.T, output, sourceDir string) (*PublishSummary, error) {
	t.Helper()
	return PublishProjectDocs(PublishOptions{
		Repo:      testRepo,
		Slug:      "home",
		OutputDir: output,
		Version:   "1.0.0",
		Home:      true,
		SourceDir: sourceDir,
	}, effects.Unbound())
}

// The home project's content root IS the site root. Publishing it under its
// own slug would leave the site root serving whatever was there before, with
// the front page addressable only at /home/.
func TestPublishingTheHomeProjectAddressesTheSiteRoot(t *testing.T) {
	gh := publishFixture(t, nil)
	if _, err := publishHome(t, buildTree(t), ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := gh.Text("site/index.html"); got != "<html>index.html</html>" {
		t.Fatalf("the site root holds %q", got)
	}
	if _, taken := gh.Content("site/home/index.html"); taken {
		t.Fatal("the home project was published under its own slug as well")
	}
}

// The site root is shared with the assembly's own generated pages, so an
// address the assembly owns is refused rather than overwritten.
func TestPublishingTheHomeProjectRefusesAReservedAddress(t *testing.T) {
	publishFixture(t, nil)
	output := buildTree(t)
	writeBuildFile(t, output, "projects/index.html", []byte("<html>mine</html>"))
	_, err := publishHome(t, output, "")
	if err == nil {
		t.Fatal("a reserved address was published")
	}
	if !strings.Contains(err.Error(), "projects/index.html") {
		t.Errorf("the refusal does not name the address: %v", err)
	}
}

// Both renderings of the curated listing are produced on every deploy,
// including deploys the home project has nothing to do with, so its listing
// travels with its documentation.
func TestPublishingTheHomeProjectCarriesItsCuratedListing(t *testing.T) {
	gh := publishFixture(t, nil)
	source := t.TempDir()
	writeBuildFile(t, filepath.Join(source, "stricttools", "docs"), "projects.toml",
		[]byte("[[category]]\nname = \"Frameworks\"\n"+
			"[[category.project]]\nslug = \"alpha\"\nblurb = \"Does it.\"\n"))
	if _, err := publishHome(t, buildTree(t), source); err != nil {
		t.Fatalf("publish: %v", err)
	}
	sidecar := gh.Text("manifests/home-listing.json")
	if !strings.Contains(sidecar, "Does it.") {
		t.Fatalf("the assembly holds no curated listing for the home project: %q", sidecar)
	}
}
