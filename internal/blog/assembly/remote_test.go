package assembly

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
)

const (
	testRepo = "owner/assembly"
	// rateLimit is what gh says when the API refused the read. It carries an
	// HTTP status, and it is not 404, which is the whole distinction under
	// test here.
	rateLimit = "gh: API rate limit exceeded for user ID 1234. (HTTP 403)\n"
)

// testRosterText is the declaration every remote test reads off the fake
// assembly.
var testRosterText = site.RenderRoster([]site.RosterEntry{
	{Slug: "alpha", Repo: "owner/alpha"},
	{Slug: "beta", Repo: "owner/beta"},
	{Slug: "home", Repo: "owner/home"},
}, "home")

// remoteFixture is a fake assembly repository holding the roster plus whatever
// else the test names.
func remoteFixture(t *testing.T, extra map[string][]byte) *fakeGH {
	t.Helper()
	gh := newFakeGH(t)
	blobs := map[string][]byte{site.RosterPath: []byte(testRosterText)}
	for path, content := range extra {
		blobs[path] = content
	}
	gh.Blobs(blobs)
	return gh
}

// isRemoteReadError reports whether err is the distinct failed-read type,
// which is what every caller has to be able to tell from an absent file.
func isRemoteReadError(err error) bool {
	var read *RemoteReadError
	return errors.As(err, &read)
}

// -- the classification itself -----------------------------------------------

func TestAnExplicit404IsAbsence(t *testing.T) {
	remoteFixture(t, nil)
	text, err := FetchRemoteText(
		effects.Unbound(), testRepo, "manifests/alpha-files.json", true,
		"read alpha's claims",
	)
	if err != nil {
		t.Fatalf("an absent file was an error: %v", err)
	}
	if text != "" {
		t.Fatalf("an absent file read as %q", text)
	}
}

func TestARateLimitIsNotAbsence(t *testing.T) {
	gh := remoteFixture(t, nil)
	gh.Fail("/contents/manifests/alpha-files.json", 1, rateLimit)
	_, err := FetchRemoteText(
		effects.Unbound(), testRepo, "manifests/alpha-files.json", true,
		"read alpha's claims",
	)
	if !isRemoteReadError(err) {
		t.Fatalf("err = %v (%T), want a RemoteReadError", err, err)
	}
	for _, want := range []string{"read alpha's claims", "rate limit exceeded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestAnUnclassifiableFailureIsNotAbsence(t *testing.T) {
	// No HTTP status at all -- a DNS failure, gh missing -- is still failure.
	gh := remoteFixture(t, nil)
	gh.Fail("/contents/"+site.ProjectsPath, 1,
		"dial tcp: lookup api.github.com: no such host\n")
	_, err := FetchRemoteText(
		effects.Unbound(), testRepo, site.ProjectsPath, true,
		"read the membership record",
	)
	if !isRemoteReadError(err) || !strings.Contains(err.Error(), "no such host") {
		t.Fatalf("err = %v, want the unclassifiable-failure refusal", err)
	}
}

func TestA5xxIsNotAbsence(t *testing.T) {
	gh := remoteFixture(t, nil)
	gh.Fail("/contents/"+site.ProjectsPath, 1, "gh: Server Error (HTTP 502)\n")
	_, err := FetchRemoteText(
		effects.Unbound(), testRepo, site.ProjectsPath, true,
		"read the membership record",
	)
	if !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
}

func TestA404WithoutMissingOKIsStillAnError(t *testing.T) {
	remoteFixture(t, nil)
	_, err := FetchRemoteText(
		effects.Unbound(), testRepo, "manifests/nope.json", false,
		"read a required file",
	)
	if !isRemoteReadError(err) || !strings.Contains(err.Error(), "read a required file") {
		t.Fatalf("err = %v, want the absent-file refusal naming the operation", err)
	}
}

func TestAFailureWithNoOutputAtAllIsStillNamed(t *testing.T) {
	gh := remoteFixture(t, nil)
	gh.Fail("/contents/"+site.ProjectsPath, 7, "")
	_, err := FetchRemoteText(
		effects.Unbound(), testRepo, site.ProjectsPath, true, "read the record",
	)
	if !isRemoteReadError(err) ||
		!strings.Contains(err.Error(), "gh api exited 7 with no output") {
		t.Fatalf("err = %v, want the silent-failure refusal", err)
	}
}

func TestASuccessfulReadDecodes(t *testing.T) {
	remoteFixture(t, map[string][]byte{
		site.ProjectsPath: []byte(`{"alpha": {}}`),
	})
	text, err := FetchRemoteText(
		effects.Unbound(), testRepo, site.ProjectsPath, true,
		"read the membership record",
	)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(text), &document); err != nil {
		t.Fatalf("the decoded text is not JSON: %v (%q)", err, text)
	}
	if len(document) != 1 {
		t.Fatalf("decoded %v", document)
	}
}

func TestAnOperationlessReadStillNamesThePathAndRepo(t *testing.T) {
	remoteFixture(t, nil)
	_, err := FetchRemoteText(effects.Unbound(), testRepo, "manifests/nope.json", false, "")
	if err == nil {
		t.Fatal("an absent required file was accepted")
	}
	for _, want := range []string{"manifests/nope.json", testRepo} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

// -- the roster loader -------------------------------------------------------

func TestLoadRemoteRosterReadsTheDeclaration(t *testing.T) {
	remoteFixture(t, nil)
	roster, err := LoadRemoteRoster(effects.Unbound(), testRepo)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := roster.Slugs(); !reflect.DeepEqual(got, []string{"alpha", "beta", "home"}) {
		t.Fatalf("slugs = %v", got)
	}
	if roster.Home != "home" {
		t.Fatalf("home = %q", roster.Home)
	}
}

func TestAFailedRosterReadIsNotAMissingRoster(t *testing.T) {
	gh := remoteFixture(t, nil)
	gh.Fail("/contents/"+site.RosterPath, 1, rateLimit)
	_, err := LoadRemoteRoster(effects.Unbound(), testRepo)
	if !isRemoteReadError(err) || !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
}

func TestAnAbsentRosterIsItsOwnRefusal(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{})
	_, err := LoadRemoteRoster(effects.Unbound(), testRepo)
	if err == nil {
		t.Fatal("an assembly with no roster loaded one")
	}
	if isRemoteReadError(err) {
		t.Fatalf("an absent roster was reported as a failed read: %v", err)
	}
	if !strings.Contains(err.Error(), testRepo+":"+site.RosterPath) {
		t.Fatalf("err = %q, want it to name the file that has to exist", err)
	}
}

// -- the remote claim reader -------------------------------------------------

// filesRecord renders a published-file record for a slug.
func filesRecord(t *testing.T, slug string, owners map[string][]string) []byte {
	t.Helper()
	text, err := site.RenderFilesManifest(slug, owners)
	if err != nil {
		t.Fatalf("rendering %s's record: %v", slug, err)
	}
	return []byte(text)
}

func TestRemotePostClaimsMapsEachPostToItsClaimant(t *testing.T) {
	remoteFixture(t, map[string][]byte{
		"manifests/beta-files.json": filesRecord(t, "beta", map[string][]string{
			"posts":   {"blog/hello/index.html"},
			"release": {"beta/index.html"},
		}),
		"manifests/home-files.json": filesRecord(t, "home", map[string][]string{
			"release": {"blog/world/index.html", "index.html"},
		}),
	})
	claims, err := RemotePostClaims(
		effects.Unbound(), testRepo, "alpha", []string{"alpha", "beta", "home"},
	)
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	want := map[string]string{
		"blog/hello/index.html": "beta",
		"blog/world/index.html": "home",
	}
	if !reflect.DeepEqual(claims, want) {
		t.Fatalf("claims = %v, want %v", claims, want)
	}
}

func TestRemotePostClaimsSkipsTheAskersOwnRecord(t *testing.T) {
	remoteFixture(t, map[string][]byte{
		"manifests/alpha-files.json": filesRecord(t, "alpha", map[string][]string{
			"posts": {"blog/mine/index.html"},
		}),
	})
	claims, err := RemotePostClaims(
		effects.Unbound(), testRepo, "alpha", []string{"alpha", "beta"},
	)
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("a project's own posts were read as somebody else's: %v", claims)
	}
}

func TestRemotePostClaimsIgnoresPathsOutsideTheBlog(t *testing.T) {
	remoteFixture(t, map[string][]byte{
		"manifests/beta-files.json": filesRecord(t, "beta", map[string][]string{
			"release": {"beta/index.html", "beta/guide/index.html"},
		}),
	})
	claims, err := RemotePostClaims(
		effects.Unbound(), testRepo, "alpha", []string{"alpha", "beta"},
	)
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("documentation paths were read as post claims: %v", claims)
	}
}

func TestAProjectWithNoRecordClaimsNothing(t *testing.T) {
	remoteFixture(t, nil)
	claims, err := RemotePostClaims(
		effects.Unbound(), testRepo, "alpha", []string{"alpha", "beta", "home"},
	)
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("claims = %v, want none", claims)
	}
}

func TestAFailedClaimsReadIsNeverAnEmptyClaimSet(t *testing.T) {
	gh := remoteFixture(t, map[string][]byte{
		"manifests/beta-files.json": filesRecord(t, "beta", map[string][]string{
			"posts": {"blog/hello/index.html"},
		}),
	})
	gh.Fail("/contents/manifests/beta-files.json", 1, rateLimit)
	_, err := RemotePostClaims(
		effects.Unbound(), testRepo, "alpha", []string{"alpha", "beta"},
	)
	if !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
}

func TestRemoteTextFetcherReadsAnAbsentFileAsEmpty(t *testing.T) {
	// It is the reader site.StagePublishedRecord takes, and that model reads
	// an absent record as the real first-publish state.
	remoteFixture(t, nil)
	text, err := RemoteTextFetcher(effects.Unbound())(
		testRepo, "manifests/alpha-files.json", "stage alpha's record",
	)
	if err != nil || text != "" {
		t.Fatalf("text = %q, err = %v", text, err)
	}
}

func TestRemoteTextFetcherPropagatesAFailedRead(t *testing.T) {
	gh := remoteFixture(t, nil)
	gh.Fail("/contents/manifests/alpha-files.json", 1, rateLimit)
	_, err := RemoteTextFetcher(effects.Unbound())(
		testRepo, "manifests/alpha-files.json", "stage alpha's record",
	)
	if !isRemoteReadError(err) {
		t.Fatalf("err = %v, want a RemoteReadError", err)
	}
}
