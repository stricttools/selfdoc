// Tests for the revisions sidecar: content-hash determinism, the document's
// bytes, and the append-on-change recording.
package revisions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// -- Content hash determinism -----------------------------------------------

func TestSameBodySameHash(t *testing.T) {
	// Identical body text produces the same hash.
	body := "Hello world\n\nThis is a post."
	if ComputePostContentHash(body) != ComputePostContentHash(body) {
		t.Error("the same body hashed two different ways")
	}
}

func TestDifferentBodyDifferentHash(t *testing.T) {
	// Different body text produces different hashes.
	if ComputePostContentHash("Version 1 content") == ComputePostContentHash("Version 2 content") {
		t.Error("two different bodies hashed the same")
	}
}

func TestWhitespaceNormalization(t *testing.T) {
	// Trailing whitespace and extra blank lines do not affect the hash.
	first := "Hello world\n\nParagraph two.\n"
	second := "Hello world  \n\n\n\nParagraph two.  \n\n"
	if ComputePostContentHash(first) != ComputePostContentHash(second) {
		t.Error("whitespace-only differences changed the hash")
	}
}

func TestLeadingTrailingWhitespaceIgnored(t *testing.T) {
	// Leading and trailing whitespace on the whole body is stripped.
	if ComputePostContentHash("Content here") != ComputePostContentHash("\n\n  Content here  \n\n") {
		t.Error("surrounding whitespace changed the hash")
	}
}

func TestHashIsSHA256Hex(t *testing.T) {
	// The hash is a 64-character hex string.
	hash := ComputePostContentHash("test")
	if len(hash) != 64 {
		t.Fatalf("hash is %d characters: %q", len(hash), hash)
	}
	if strings.Trim(hash, "0123456789abcdef") != "" {
		t.Errorf("hash is not hex: %q", hash)
	}
}

func TestHashIsContextIndependent(t *testing.T) {
	// The hash depends on the body text alone, not on base URLs or themes --
	// which the surface enforces by taking nothing else.
	body := "## Section\n\nSome content here."
	if ComputePostContentHash(body) != ComputePostContentHash(body) {
		t.Error("the same body hashed two different ways")
	}
}

func TestNormalizationSplitsOnEveryPythonLineBreak(t *testing.T) {
	// The hash is computed over the split, so the line terminators Python's
	// splitlines() recognized must still end a line here.
	if ComputePostContentHash("a\fb") != ComputePostContentHash("a\nb") {
		t.Error("a form feed did not end a line")
	}
	if ComputePostContentHash("a\r\nb") != ComputePostContentHash("a\nb") {
		t.Error("a CRLF did not count as one line break")
	}
}

// -- Load and save ----------------------------------------------------------

func TestLoadNonexistent(t *testing.T) {
	// Loading from a directory without revisions.json returns empty.
	hygiene.Isolate(t)
	document, err := LoadRevisions(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRevisions: %v", err)
	}
	if len(document.Posts) != 0 {
		t.Errorf("read %v", document.Posts)
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	// Data saved with SaveRevisions can be loaded back.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	document := &Document{Posts: []PostRevisions{{
		Slug: "my-slug",
		Revisions: []Revision{{
			ContentHash: "abc123",
			Timestamp:   "2026-07-05T12:00:00Z",
			Summary:     "Initial",
		}},
	}}}
	if _, err := SaveRevisions(effects.Unbound(), document, directory); err != nil {
		t.Fatalf("SaveRevisions: %v", err)
	}
	loaded, err := LoadRevisions(directory)
	if err != nil {
		t.Fatalf("LoadRevisions: %v", err)
	}
	if len(loaded.Posts) != 1 || loaded.Posts[0].Slug != "my-slug" {
		t.Fatalf("read %v", loaded.Posts)
	}
	if len(loaded.Posts[0].Revisions) != 1 || loaded.Posts[0].Revisions[0] != document.Posts[0].Revisions[0] {
		t.Errorf("read %v", loaded.Posts[0].Revisions)
	}
}

func TestSaveCreatesSelfdocDir(t *testing.T) {
	// SaveRevisions creates .selfdoc/ if it does not exist.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	path, err := SaveRevisions(effects.Unbound(), &Document{}, directory)
	if err != nil {
		t.Fatalf("SaveRevisions: %v", err)
	}
	if want := filepath.Join(directory, ".stricttools", "docs-state", "revisions.json"); path != want {
		t.Errorf("wrote %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the sidecar was not written: %v", err)
	}
}

func TestLoadPreservesDocumentOrder(t *testing.T) {
	// The sidecar is rewritten whole, so a rewrite must not reorder the posts.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	if err := os.MkdirAll(filepath.Join(directory, ".stricttools", "docs-state"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "posts": {
    "zebra": {"revisions": []},
    "alpha": {"revisions": []}
  }
}
`
	path := filepath.Join(directory, ".stricttools", "docs-state", "revisions.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	document, err := LoadRevisions(directory)
	if err != nil {
		t.Fatalf("LoadRevisions: %v", err)
	}
	if len(document.Posts) != 2 || document.Posts[0].Slug != "zebra" || document.Posts[1].Slug != "alpha" {
		t.Errorf("read %v, want zebra then alpha", document.Posts)
	}
}

func TestTheDocumentsBytesAreThePythonsBytes(t *testing.T) {
	// The sidecar is written with indent=2 and NO key sorting, so a revision
	// reads content_hash, timestamp, summary. Sorting would move the summary
	// into the middle and rewrite every existing sidecar.
	document := &Document{Posts: []PostRevisions{
		{Slug: "my-slug", Revisions: []Revision{
			{ContentHash: "abc123", Timestamp: "2026-07-05T12:00:00Z", Summary: "Initial"},
			{ContentHash: "def456", Timestamp: "2026-07-06T12:00:00Z"},
		}},
		{Slug: "other"},
	}}
	want := "{\n  \"posts\": {\n    \"my-slug\": {\n      \"revisions\": [\n" +
		"        {\n          \"content_hash\": \"abc123\",\n" +
		"          \"timestamp\": \"2026-07-05T12:00:00Z\",\n" +
		"          \"summary\": \"Initial\"\n        },\n" +
		"        {\n          \"content_hash\": \"def456\",\n" +
		"          \"timestamp\": \"2026-07-06T12:00:00Z\"\n        }\n" +
		"      ]\n    },\n    \"other\": {\n      \"revisions\": []\n    }\n  }\n}\n"
	if got := string(render(document)); got != want {
		t.Errorf("rendered\n%q\nwant\n%q", got, want)
	}
}

func TestAnEmptyDocumentRendersAsAnEmptyPostsObject(t *testing.T) {
	if got := string(render(&Document{})); got != "{\n  \"posts\": {}\n}\n" {
		t.Errorf("rendered %q", got)
	}
}

func TestNonASCIIIsEscapedAsThePythonEscapedIt(t *testing.T) {
	// The Python encoder ran under ensure_ascii, so a non-ASCII summary or
	// slug crosses the file as \uXXXX rather than as UTF-8 bytes.
	document := &Document{Posts: []PostRevisions{{
		Slug: "unicode-ü",
		Revisions: []Revision{{
			ContentHash: "h", Timestamp: "t", Summary: `a "quoted" café`,
		}},
	}}}
	want := "{\n  \"posts\": {\n    \"unicode-\\u00fc\": {\n      \"revisions\": [\n" +
		"        {\n          \"content_hash\": \"h\",\n" +
		"          \"timestamp\": \"t\",\n" +
		"          \"summary\": \"a \\\"quoted\\\" caf\\u00e9\"\n        }\n" +
		"      ]\n    }\n  }\n}\n"
	if got := string(render(document)); got != want {
		t.Errorf("rendered\n%q\nwant\n%q", got, want)
	}
}

// -- Record revision --------------------------------------------------------

func TestFirstRevisionRecorded(t *testing.T) {
	// The first call creates a revision entry.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	changed, err := RecordRevision(effects.Unbound(), directory, "my-post", "Hello world", "Initial publish")
	if err != nil {
		t.Fatalf("RecordRevision: %v", err)
	}
	if !changed {
		t.Error("the first recording reported no change")
	}
	revisions, err := GetPostRevisions(directory, "my-post")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("recorded %v", revisions)
	}
	if revisions[0].Summary != "Initial publish" {
		t.Errorf("summary is %q", revisions[0].Summary)
	}
	if revisions[0].ContentHash == "" || revisions[0].Timestamp == "" {
		t.Errorf("entry is %+v", revisions[0])
	}
}

func TestSameBodyNoNewRevision(t *testing.T) {
	// Publishing with an unchanged body does NOT add a revision.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	if _, err := RecordRevision(effects.Unbound(), directory, "my-post", "Hello world", ""); err != nil {
		t.Fatal(err)
	}
	changed, err := RecordRevision(effects.Unbound(), directory, "my-post", "Hello world", "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("an unchanged body recorded a revision")
	}
	revisions, err := GetPostRevisions(directory, "my-post")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Errorf("recorded %v", revisions)
	}
}

func TestChangedBodyAddsRevision(t *testing.T) {
	// Publishing with a changed body adds a new revision.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	if _, err := RecordRevision(effects.Unbound(), directory, "my-post", "Version 1", ""); err != nil {
		t.Fatal(err)
	}
	changed, err := RecordRevision(effects.Unbound(), directory, "my-post", "Version 2", "Updated examples")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("a changed body recorded nothing")
	}
	revisions, err := GetPostRevisions(directory, "my-post")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[1].Summary != "Updated examples" {
		t.Errorf("recorded %v", revisions)
	}
}

func TestMultiplePostsIndependent(t *testing.T) {
	// Revisions for different slugs are tracked independently.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	for _, entry := range []struct{ slug, body string }{
		{"post-a", "Content A"},
		{"post-b", "Content B"},
	} {
		if _, err := RecordRevision(effects.Unbound(), directory, entry.slug, entry.body, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, slug := range []string{"post-a", "post-b"} {
		revisions, err := GetPostRevisions(directory, slug)
		if err != nil {
			t.Fatal(err)
		}
		if len(revisions) != 1 {
			t.Errorf("%s recorded %v", slug, revisions)
		}
	}
}

func TestWhitespaceOnlyChangeNoRevision(t *testing.T) {
	// Whitespace-only changes do not create a new revision.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	if _, err := RecordRevision(effects.Unbound(), directory, "slug", "Content here", ""); err != nil {
		t.Fatal(err)
	}
	changed, err := RecordRevision(effects.Unbound(), directory, "slug", "  Content here  \n\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("a whitespace-only edit recorded a revision")
	}
	revisions, err := GetPostRevisions(directory, "slug")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Errorf("recorded %v", revisions)
	}
}

func TestSummaryOptional(t *testing.T) {
	// With no summary, the key is left out of the written document.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	if _, err := RecordRevision(effects.Unbound(), directory, "slug", "Body text", ""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, ".stricttools", "docs-state", "revisions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "summary") {
		t.Errorf("the document carries a summary key:\n%s", raw)
	}
}

func TestRecordedTimestampIsTheISOSpelling(t *testing.T) {
	// The timestamp is what Python's datetime.isoformat() wrote: a UTC
	// instant with an explicit offset, which a reader can parse.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	if _, err := RecordRevision(effects.Unbound(), directory, "slug", "Body", ""); err != nil {
		t.Fatal(err)
	}
	revisions, err := GetPostRevisions(directory, "slug")
	if err != nil {
		t.Fatal(err)
	}
	stamp := revisions[0].Timestamp
	if !strings.HasSuffix(stamp, "+00:00") || !strings.Contains(stamp, "T") {
		t.Errorf("timestamp is %q", stamp)
	}
}

// -- Get helpers ------------------------------------------------------------

func TestGetRevisionsNonexistentSlug(t *testing.T) {
	// A slug with no revisions has none.
	hygiene.Isolate(t)
	revisions, err := GetPostRevisions(t.TempDir(), "no-such-slug")
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 0 {
		t.Errorf("read %v", revisions)
	}
}

func TestGetLastUpdatedWithRevisions(t *testing.T) {
	// The most recent revision's timestamp is the answer.
	hygiene.Isolate(t)
	directory := testproject.Dir(t)
	for _, body := range []string{"V1", "V2"} {
		if _, err := RecordRevision(effects.Unbound(), directory, "slug", body, ""); err != nil {
			t.Fatal(err)
		}
	}
	stamp, ok, err := GetLastUpdated(directory, "slug")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a post with revisions reported none")
	}
	revisions, err := GetPostRevisions(directory, "slug")
	if err != nil {
		t.Fatal(err)
	}
	if stamp != revisions[len(revisions)-1].Timestamp {
		t.Errorf("last updated is %q, but the last revision is %q", stamp, revisions[len(revisions)-1].Timestamp)
	}
}

func TestGetLastUpdatedNoRevisions(t *testing.T) {
	// A post with no revision has no last-updated time.
	hygiene.Isolate(t)
	_, ok, err := GetLastUpdated(t.TempDir(), "no-slug")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a post with no revisions reported a last-updated time")
	}
}

// TestHashesMatchTheReferenceEngine holds the normalization against hashes
// taken from the Python this package replaces, on the bodies where the two
// could most easily disagree: paragraph breaks, surrounding whitespace, the
// line terminators Go's own splitter does not recognize, non-ASCII text, and
// the C0 information separator Go's unicode.IsSpace does not trim.
func TestHashesMatchTheReferenceEngine(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{
			"Hello world\n\nThis is a post.",
			"2fe5b1bd60623ebc956d8494801ed3389f43fc9e76bf3735db4ac5813f3066a7",
		},
		{
			"  Content here  \n\n",
			"eef219430e798ceb95ff75701aebe8d677423911cc36faa178ff9a043171b97e",
		},
		{
			"a\fb",
			"7e18f737311b2dc3b2f269dd78396b0351f14fb66efa879f768cb23181883c78",
		},
		{
			"a\r\nb",
			"7e18f737311b2dc3b2f269dd78396b0351f14fb66efa879f768cb23181883c78",
		},
		{
			"## Section\n\nSome content here.",
			"0529671168c21c65c8666569652b6ca6e41ee5987ae40e905c30044d7fb825f2",
		},
		{
			"Café\n\n\n\n  ünïcode  ",
			"c98675471db843b51478a744a075f26e2097037adf38188ec241d80eaa34078d",
		},
		{
			"x\x1fy",
			"adbbc83fffe5c8ca4ebdb1c5071be05fb28ca885d36672e14a4a84f70c266eea",
		},
	}
	for _, test := range tests {
		if got := ComputePostContentHash(test.body); got != test.want {
			t.Errorf("%q hashed to %s, want %s", test.body, got, test.want)
		}
	}
}
