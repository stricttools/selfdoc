package assembly

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
)

// retireRosterText declares the three projects the retirement tests work over.
var retireRosterText = site.RenderRoster([]site.RosterEntry{
	{Slug: "keeper", Repo: "owner/keeper"},
	{Slug: "goner", Repo: "owner/goner"},
	{Slug: "home", Repo: "owner/home"},
}, "home")

// retireMembership is the derived record the retirement rewrites.
const retireMembership = `{
  "keeper": {
    "repo": "owner/keeper",
    "ref": "v1.0.0",
    "version": "1.0.0"
  },
  "goner": {
    "repo": "owner/goner",
    "ref": "v0.4.0",
    "version": "0.4.0"
  }
}
`

// projectManifest is one project's manifest, enough of one for the assembly to
// read.
func projectManifest(slug, name string) []byte {
	document := map[string]any{
		"schema_version": 2,
		"name":           name,
		"slug":           slug,
		"version":        "1.0.0",
		"description":    name + " docs",
		"language":       "python",
		"base_url":       "https://docs.example.com/" + slug,
		"author":         map[string]any{"name": "Test Author", "url": "https://author.example"},
		"pages":          []any{map[string]any{"path": "index.md", "title": "Home"}},
		"posts": []any{map[string]any{
			"slug": "hello", "title": name + " says hello", "date": "2024-06-01",
		}},
		"last_gen": "2024-01-01T00:00:00+00:00",
	}
	encoded, _ := json.Marshal(document)
	return encoded
}

// retireBlobs is the assembly the retirement tests start from.
func retireBlobs(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		site.RosterPath:               []byte(retireRosterText),
		site.ProjectsPath:             []byte(retireMembership),
		"site/keeper/index.html":      []byte("<html>keeper</html>"),
		"site/goner/index.html":       []byte("<html>goner</html>"),
		"site/goner/guide/index.html": []byte("<html>goner guide</html>"),
		// A post is site-level, so it is outside the subtree retirement
		// removes: what says it was goner's is goner's published-file record.
		"site/blog/goner-hello/index.html": []byte("<html>goner post</html>"),
		"manifests/keeper.json":            projectManifest("keeper", "Keeper"),
		"manifests/goner.json":             projectManifest("goner", "Goner"),
		"manifests/goner-posts.json":       projectManifest("goner", "Goner"),
		"manifests/goner-revisions.json":   []byte("{}"),
		"manifests/goner-files.json": filesRecord(t, "goner", map[string][]string{
			"release": {
				"goner/index.html", "goner/guide/index.html",
				"blog/goner-hello/index.html",
			},
		}),
	}
}

// retireFixture is the fake assembly a retirement runs against.
func retireFixture(t *testing.T) *fakeGH {
	t.Helper()
	gh := newFakeGH(t)
	gh.Blobs(retireBlobs(t))
	return gh
}

func TestRetirementDeletesTheProjectsSection(t *testing.T) {
	gh := retireFixture(t)
	summary, err := RetireProject(effects.Unbound(), testRepo, "goner", "main")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	for _, want := range []string{
		"site/goner/index.html", "site/goner/guide/index.html",
	} {
		if !slices.Contains(summary.Deleted, want) {
			t.Errorf("%s was not deleted; deleted %v", want, summary.Deleted)
		}
		if _, present := gh.Content(want); present {
			t.Errorf("%s is still on the remote", want)
		}
	}
}

func TestRetirementDeletesEveryManifestKind(t *testing.T) {
	retireFixture(t)
	summary, err := RetireProject(effects.Unbound(), testRepo, "goner", "main")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	var manifests []string
	for _, path := range summary.Deleted {
		if strings.HasPrefix(path, "manifests/") {
			manifests = append(manifests, path)
		}
	}
	want := []string{
		"manifests/goner-files.json", "manifests/goner-posts.json",
		"manifests/goner-revisions.json", "manifests/goner.json",
	}
	slices.Sort(manifests)
	if !reflect.DeepEqual(manifests, want) {
		t.Fatalf("deleted %v, want %v", manifests, want)
	}
}

func TestRetirementTakesTheProjectsPostsOffTheBlog(t *testing.T) {
	// A retired project's posts are not in its subtree; they still go.
	retireFixture(t)
	summary, err := RetireProject(effects.Unbound(), testRepo, "goner", "main")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if !slices.Contains(summary.Deleted, "site/blog/goner-hello/index.html") {
		t.Fatalf("the post stayed on the blog; deleted %v", summary.Deleted)
	}
}

func TestRetirementLeavesTheOtherProjectAlone(t *testing.T) {
	gh := retireFixture(t)
	summary, err := RetireProject(effects.Unbound(), testRepo, "goner", "main")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	for _, path := range summary.Deleted {
		if strings.Contains(path, "keeper") {
			t.Errorf("the retirement deleted %s", path)
		}
	}
	if _, present := gh.Content("site/keeper/index.html"); !present {
		t.Error("the other project's page went with the retirement")
	}
}

func TestRetirementRemovesTheRosterBlock(t *testing.T) {
	gh := retireFixture(t)
	if _, err := RetireProject(effects.Unbound(), testRepo, "goner", "main"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	roster, err := site.ParseRoster(gh.Text(site.RosterPath), site.RosterPath)
	if err != nil {
		t.Fatalf("the rewritten roster does not parse: %v", err)
	}
	if got := roster.Slugs(); !reflect.DeepEqual(got, []string{"home", "keeper"}) {
		t.Fatalf("the roster still declares %v", got)
	}
	if roster.Home != "home" {
		t.Fatalf("the rewritten roster names %q home", roster.Home)
	}
}

func TestRetirementRemovesTheMembershipRecord(t *testing.T) {
	gh := retireFixture(t)
	if _, err := RetireProject(effects.Unbound(), testRepo, "goner", "main"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	var membership map[string]any
	if err := json.Unmarshal([]byte(gh.Text(site.ProjectsPath)), &membership); err != nil {
		t.Fatalf("the rewritten record is not JSON: %v", err)
	}
	if len(membership) != 1 {
		t.Fatalf("the record still holds %v", membership)
	}
	if _, present := membership["keeper"]; !present {
		t.Fatalf("the record lost the project that stayed: %v", membership)
	}
}

func TestTheWholeRetirementIsOneCommit(t *testing.T) {
	gh := retireFixture(t)
	if _, err := RetireProject(effects.Unbound(), testRepo, "goner", "main"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 1 {
		t.Fatalf("the retirement made %d commit(s), want 1", got)
	}
}

func TestRetirementReportsWhatRemains(t *testing.T) {
	retireFixture(t)
	summary, err := RetireProject(effects.Unbound(), testRepo, "goner", "main")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if !reflect.DeepEqual(summary.Remaining, []string{"home", "keeper"}) {
		t.Fatalf("remaining = %v", summary.Remaining)
	}
	if !summary.Push.Changed {
		t.Error("the retirement reported no change")
	}
	if summary.Slug != "goner" {
		t.Errorf("summary names %q", summary.Slug)
	}
}

func TestRetiringAnUndeclaredProjectIsAHardError(t *testing.T) {
	retireFixture(t)
	_, err := RetireProject(effects.Unbound(), testRepo, "never-existed", "main")
	if err == nil {
		t.Fatal("an undeclared project was retired")
	}
	if !strings.Contains(err.Error(), "nothing to retire") {
		t.Errorf("err = %q, want it to say there is nothing to retire", err)
	}
	if !strings.Contains(err.Error(), "goner, home, keeper") {
		t.Errorf("err = %q, want it to name the declared projects", err)
	}
}

func TestRetiringTheHomeProjectIsAHardError(t *testing.T) {
	retireFixture(t)
	_, err := RetireProject(effects.Unbound(), testRepo, "home", "main")
	if err == nil {
		t.Fatal("the site's front page was retired")
	}
	for _, want := range []string{"home project", "front page"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestRetiringWithoutARosterIsAHardError(t *testing.T) {
	gh := newFakeGH(t)
	blobs := retireBlobs(t)
	delete(blobs, site.RosterPath)
	gh.Blobs(blobs)
	if _, err := RetireProject(effects.Unbound(), testRepo, "goner", "main"); err == nil {
		t.Fatal("a retirement ran against an assembly with no roster")
	}
}

func TestAFailedClaimsReadAbortsARetirement(t *testing.T) {
	// Retiring on a failed read left the project's posts on the blog forever.
	gh := retireFixture(t)
	gh.Fail("/contents/manifests/goner-files.json", 1, rateLimit)
	if _, err := RetireProject(effects.Unbound(), testRepo, "goner", "main"); !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Fatalf("the aborted retirement still made %d commit(s)", got)
	}
}

func TestAFailedMembershipReadAbortsARetirement(t *testing.T) {
	gh := retireFixture(t)
	gh.Fail("/contents/"+site.ProjectsPath, 1, rateLimit)
	if _, err := RetireProject(effects.Unbound(), testRepo, "goner", "main"); !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Fatalf("the aborted retirement still made %d commit(s)", got)
	}
}

func TestARetirementWithNoRecordAtAllStillWorks(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{
		site.RosterPath:         []byte(retireRosterText),
		"site/goner/index.html": []byte("<html>doc</html>"),
	})
	summary, err := RetireProject(effects.Unbound(), testRepo, "goner", "main")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if !reflect.DeepEqual(summary.Deleted, []string{"site/goner/index.html"}) {
		t.Fatalf("deleted = %v", summary.Deleted)
	}
	if !reflect.DeepEqual(summary.Remaining, []string{"home", "keeper"}) {
		t.Fatalf("remaining = %v", summary.Remaining)
	}
}
