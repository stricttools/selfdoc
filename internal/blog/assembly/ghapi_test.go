package assembly

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
)

// pngBytes is a tiny real PNG. The bytes are not valid UTF-8 anywhere in the
// middle, so anything that round-trips them through text is guaranteed to
// fail.
var pngBytes = []byte{
	0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
	0x00, 0x00, 0x00, '\r', 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, '\n', 'I', 'D', 'A', 'T', 'x', 0x9c, 'c', 0x00, 0x01,
	0x00, 0x00, 0x05, 0x00, 0x01, '\r', '\n', '-', 0xb4,
	0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 'B', '`', 0x82,
}

// ghCallsMatching returns every recorded call whose joined argv contains the
// marker.
func ghCallsMatching(t *testing.T, gh *fakeGH, marker string) []ghCallRecord {
	t.Helper()
	var found []ghCallRecord
	for _, call := range gh.Calls() {
		if strings.Contains(call.Joined(), marker) {
			found = append(found, call)
		}
	}
	return found
}

// ghUploadedBlobs returns the decoded content of every blob the push uploaded,
// in upload order.
func ghUploadedBlobs(t *testing.T, gh *fakeGH) [][]byte {
	t.Helper()
	var uploaded [][]byte
	for _, call := range ghCallsMatching(t, gh, "/git/blobs") {
		var payload struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
			t.Fatalf("decoding a blob payload: %v", err)
		}
		if payload.Encoding != "base64" {
			t.Fatalf("a blob travelled as %q, not base64", payload.Encoding)
		}
		data, err := base64.StdEncoding.DecodeString(payload.Content)
		if err != nil {
			t.Fatalf("decoding a blob's content: %v", err)
		}
		uploaded = append(uploaded, data)
	}
	return uploaded
}

// treeEntryRequest is one entry of a recorded tree-creation request.
type treeEntryRequest struct {
	Path string  `json:"path"`
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	SHA  *string `json:"sha"`
}

// ghTreePayloads returns every tree-creation request the push made.
func ghTreePayloads(t *testing.T, gh *fakeGH) []struct {
	BaseTree string
	Tree     []treeEntryRequest
} {
	t.Helper()
	var payloads []struct {
		BaseTree string
		Tree     []treeEntryRequest
	}
	for _, call := range gh.Calls() {
		joined := call.Joined()
		if !strings.Contains(joined, "/git/trees") || !strings.Contains(joined, "POST") {
			continue
		}
		var payload struct {
			BaseTree string             `json:"base_tree"`
			Tree     []treeEntryRequest `json:"tree"`
		}
		if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
			t.Fatalf("decoding a tree payload: %v", err)
		}
		payloads = append(payloads, struct {
			BaseTree string
			Tree     []treeEntryRequest
		}{payload.BaseTree, payload.Tree})
	}
	return payloads
}

// ghCommitCalls returns every commit-creation request the push made.
func ghCommitCalls(t *testing.T, gh *fakeGH) []ghCallRecord {
	t.Helper()
	var found []ghCallRecord
	for _, call := range gh.Calls() {
		joined := call.Joined()
		if strings.Contains(joined, "/git/commits") && strings.Contains(joined, "--method") {
			found = append(found, call)
		}
	}
	return found
}

// -- the API call sequence ---------------------------------------------------

func TestPushFilesSuccessfulSequence(t *testing.T) {
	gh := newFakeGH(t)
	gh.Script(
		ghResponse{Stdout: "abc123\n"},
		ghResponse{Stdout: "tree456\n"},
		ghResponse{Stdout: `{"sha":"tree456","truncated":false,"tree":[]}`},
		ghResponse{Stdout: "blob_a\n"},
		ghResponse{Stdout: "blob_b\n"},
		ghResponse{Stdout: "newtree\n"},
		ghResponse{Stdout: "newcommit\n"},
		ghResponse{Stdout: "newcommit\n"},
	)
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo", map[string][]byte{
		"dir/a.txt": []byte("content a"),
		"dir/b.txt": []byte("content b"),
	}, "test commit", "main", nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if result.SHA != "newcommit" || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	calls := gh.Calls()
	if len(calls) != 8 {
		t.Fatalf("expected 8 API calls, got %d: %v", len(calls), calls)
	}
	for index, want := range []string{
		"/repos/owner/repo/git/ref/heads/main",
		"/repos/owner/repo/git/commits/abc123",
		"/repos/owner/repo/git/trees/tree456",
		"blobs",
		"blobs",
		"trees",
		"commits",
		"refs",
	} {
		if !strings.Contains(calls[index].Joined(), want) {
			t.Errorf("call %d = %q, want it to name %q", index, calls[index].Joined(), want)
		}
	}
	for _, index := range []int{3, 4, 5, 6, 7} {
		if !strings.Contains(calls[index].Joined(), "--method") {
			t.Errorf("call %d does not declare its method: %q", index, calls[index].Joined())
		}
	}
}

func TestPushFilesBlobErrorIsNamed(t *testing.T) {
	gh := newFakeGH(t)
	gh.Script(
		ghResponse{Stdout: "abc123\n"},
		ghResponse{Stdout: "tree456\n"},
		ghResponse{Stdout: `{"truncated":false,"tree":[]}`},
		ghResponse{Code: 1, Stderr: "Not Found"},
	)
	_, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"f.txt": []byte("x")}, "msg", "main", nil)
	if err == nil || !strings.Contains(err.Error(), "create blob") {
		t.Fatalf("err = %v, want it to name the blob step", err)
	}
}

func TestPushFilesTreeErrorIsNamed(t *testing.T) {
	gh := newFakeGH(t)
	gh.Script(
		ghResponse{Stdout: "abc123\n"},
		ghResponse{Stdout: "tree456\n"},
		ghResponse{Stdout: `{"truncated":false,"tree":[]}`},
		ghResponse{Stdout: "blob_a\n"},
		ghResponse{Code: 1, Stderr: "Server Error"},
	)
	_, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"f.txt": []byte("x")}, "msg", "main", nil)
	if err == nil || !strings.Contains(err.Error(), "create tree") {
		t.Fatalf("err = %v, want it to name the tree step", err)
	}
}

func TestPushFilesWithNothingToPushRefuses(t *testing.T) {
	newFakeGH(t)
	_, err := PushFilesToRepo(effects.Unbound(), "owner/repo", nil, "msg", "main", nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v, want the empty refusal", err)
	}
}

func TestPushFilesTreePayloadCarriesPathsAndSHAs(t *testing.T) {
	gh := newFakeGH(t)
	_, err := PushFilesToRepo(effects.Unbound(), "owner/repo", map[string][]byte{
		"site/index.html": []byte("<html/>"),
		"site/style.css":  []byte("body{}"),
	}, "deploy", "main", nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	payloads := ghTreePayloads(t, gh)
	if len(payloads) != 1 {
		t.Fatalf("expected one tree request, got %d", len(payloads))
	}
	if payloads[0].BaseTree != "basetree" {
		t.Errorf("base_tree = %q", payloads[0].BaseTree)
	}
	paths := map[string]bool{}
	for _, entry := range payloads[0].Tree {
		paths[entry.Path] = true
		if entry.Mode != "100644" || entry.Type != "blob" {
			t.Errorf("entry %q = mode %q type %q", entry.Path, entry.Mode, entry.Type)
		}
		if entry.SHA == nil || *entry.SHA == "" {
			t.Errorf("entry %q carries no blob sha", entry.Path)
		}
	}
	if !paths["site/index.html"] || !paths["site/style.css"] || len(paths) != 2 {
		t.Errorf("tree paths = %v", paths)
	}
}

// -- binary content ----------------------------------------------------------

func TestBytesContentRoundTripsByteIdentically(t *testing.T) {
	gh := newFakeGH(t)
	if _, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"site/logo.png": pngBytes}, "add image", "main", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	uploaded := ghUploadedBlobs(t, gh)
	if len(uploaded) != 1 || !reflect.DeepEqual(uploaded[0], pngBytes) {
		t.Fatalf("uploaded = %v", uploaded)
	}
	stored, ok := gh.Content("site/logo.png")
	if !ok || !reflect.DeepEqual(stored, pngBytes) {
		t.Fatalf("the remote holds %v", stored)
	}
}

func TestBytesAndTextTravelInOneCommit(t *testing.T) {
	gh := newFakeGH(t)
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo", map[string][]byte{
		"site/logo.png":   pngBytes,
		"site/index.html": []byte("<html/>"),
	}, "mixed", "main", nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !result.Changed {
		t.Error("the push reported no change")
	}
	if got := len(ghUploadedBlobs(t, gh)); got != 2 {
		t.Errorf("uploaded %d blob(s), want 2", got)
	}
	if got := len(ghCommitCalls(t, gh)); got != 1 {
		t.Errorf("created %d commit(s), want 1", got)
	}
}

// -- unchanged files ---------------------------------------------------------

func TestUnchangedFileUploadsNoBlob(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"site/index.html": []byte("<html/>")})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"site/index.html": []byte("<html/>")}, "noop", "main", nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(ghUploadedBlobs(t, gh)) != 0 {
		t.Error("an unchanged file uploaded a blob")
	}
	if result.Changed || result.SHA != "headsha" {
		t.Fatalf("result = %+v", result)
	}
}

func TestUnchangedPushCreatesNoCommit(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("same")})
	if _, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"a.txt": []byte("same")}, "noop", "main", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Errorf("created %d commit(s), want none", got)
	}
	if got := len(ghTreePayloads(t, gh)); got != 0 {
		t.Errorf("created %d tree(s), want none", got)
	}
}

func TestUnchangedBinaryFileUploadsNoBlob(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"site/logo.png": pngBytes})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"site/logo.png": pngBytes}, "noop", "main", nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(ghUploadedBlobs(t, gh)) != 0 || result.Changed {
		t.Fatalf("result = %+v, uploads = %d", result, len(ghUploadedBlobs(t, gh)))
	}
}

func TestOnlyTheChangedFileOfAPairUploads(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("same"), "b.txt": []byte("old")})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo", map[string][]byte{
		"a.txt": []byte("same"), "b.txt": []byte("new"),
	}, "one change", "main", nil)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	uploaded := ghUploadedBlobs(t, gh)
	if len(uploaded) != 1 || string(uploaded[0]) != "new" {
		t.Fatalf("uploaded = %q", uploaded)
	}
	payloads := ghTreePayloads(t, gh)
	if len(payloads) != 1 || len(payloads[0].Tree) != 1 ||
		payloads[0].Tree[0].Path != "b.txt" {
		t.Fatalf("tree = %+v", payloads)
	}
	if !reflect.DeepEqual(result.Uploaded, []string{"b.txt"}) {
		t.Errorf("result.Uploaded = %v", result.Uploaded)
	}
}

func TestTruncatedRemoteTreeIsAHardError(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("x")})
	gh.Truncate()
	_, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"a.txt": []byte("y")}, "msg", "main", nil)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want the truncated refusal", err)
	}
}

// -- deletion ----------------------------------------------------------------

func TestDeletedPathDisappearsInOneCommit(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{
		"site/old.html":  []byte("gone soon"),
		"site/keep.html": []byte("stay"),
	})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo", nil,
		"remove", "main", []string{"site/old.html"})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	payloads := ghTreePayloads(t, gh)
	if len(payloads) != 1 || len(payloads[0].Tree) != 1 {
		t.Fatalf("tree = %+v", payloads)
	}
	entry := payloads[0].Tree[0]
	if entry.Path != "site/old.html" || entry.Mode != "100644" ||
		entry.Type != "blob" || entry.SHA != nil {
		t.Fatalf("entry = %+v", entry)
	}
	if got := len(ghCommitCalls(t, gh)); got != 1 {
		t.Errorf("created %d commit(s), want 1", got)
	}
	if !reflect.DeepEqual(result.Deleted, []string{"site/old.html"}) || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	if _, present := gh.Content("site/old.html"); present {
		t.Error("the deleted path is still on the remote")
	}
	if _, present := gh.Content("site/keep.html"); !present {
		t.Error("the untouched path went with it")
	}
}

func TestDeletionAndUploadShareOneCommit(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{
		"site/old.html":   []byte("gone"),
		"site/index.html": []byte("old"),
	})
	if _, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"site/index.html": []byte("new")},
		"replace", "main", []string{"site/old.html"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	payloads := ghTreePayloads(t, gh)
	if len(payloads) != 1 {
		t.Fatalf("expected one tree request, got %d", len(payloads))
	}
	entries := map[string]*string{}
	for _, entry := range payloads[0].Tree {
		entries[entry.Path] = entry.SHA
	}
	if entries["site/old.html"] != nil {
		t.Error("the deletion did not carry a null sha")
	}
	if entries["site/index.html"] == nil || *entries["site/index.html"] != "newblob1" {
		t.Errorf("the upload's sha = %v", entries["site/index.html"])
	}
	if got := len(ghCommitCalls(t, gh)); got != 1 {
		t.Errorf("created %d commit(s), want 1", got)
	}
}

func TestDeletingAnAbsentPathIsNotACommit(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("x")})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo", nil,
		"msg", "main", []string{"never/here.txt"})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if result.Changed {
		t.Error("deleting an absent path reported a change")
	}
	if got := len(ghCommitCalls(t, gh)); got != 0 {
		t.Errorf("created %d commit(s), want none", got)
	}
}

func TestDeletionOnlyPushIsAccepted(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("x")})
	result, err := PushFilesToRepo(effects.Unbound(), "owner/repo", nil,
		"msg", "main", []string{"a.txt"})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !result.Changed {
		t.Error("a deletion-only push reported no change")
	}
}

// -- listing the branch ------------------------------------------------------

func TestListRemotePathsIsSorted(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{
		"site/b.html": []byte("b"),
		"site/a.html": []byte("a"),
		"roster.toml": []byte("x"),
	})
	paths, err := ListRemotePaths(effects.Unbound(), "owner/repo", "main")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"roster.toml", "site/a.html", "site/b.html"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestListRemotePathsRefusesATruncatedTree(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("x")})
	gh.Truncate()
	if _, err := ListRemotePaths(effects.Unbound(), "owner/repo", "main"); err == nil ||
		!strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want the truncated refusal", err)
	}
}

func TestListRemotePathsPinsTheTreeMethodToGET(t *testing.T) {
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"a.txt": []byte("x")})
	if _, err := ListRemotePaths(effects.Unbound(), "owner/repo", "main"); err != nil {
		t.Fatalf("list: %v", err)
	}
	tree := ghCallsMatching(t, gh, "/git/trees/")
	if len(tree) != 1 {
		t.Fatalf("expected one tree read, got %d", len(tree))
	}
	if !strings.Contains(tree[0].Joined(), "--method GET") {
		t.Fatalf("the tree read did not pin GET: %q", tree[0].Joined())
	}
}

func TestTheTreeListingAsksForTheWholeTree(t *testing.T) {
	// A non-recursive listing returns only the top level, silently -- and an
	// unchanged-file check made against half a tree re-uploads the other half.
	gh := newFakeGH(t)
	gh.Blobs(map[string][]byte{"site/a/b/c.html": []byte("deep")})
	if _, err := remoteBlobSHAs(effects.Unbound(), "owner/repo", "basetree"); err != nil {
		t.Fatalf("list: %v", err)
	}
	calls := gh.Calls()
	if len(calls) != 1 {
		t.Fatalf("the listing made %d call(s)", len(calls))
	}
	if !strings.Contains(calls[0].Joined(), "recursive=1") {
		t.Fatalf("the listing is not recursive: %q", calls[0].Joined())
	}
}

func TestTheTreeListingReturnsBlobsOnly(t *testing.T) {
	// The response carries a tree entry per directory as well; only the blobs
	// are files a push can compare against.
	gh := newFakeGH(t)
	gh.Script(ghResponse{Stdout: `{"sha": "deadbeef", "truncated": false, ` +
		`"tree": [{"path": "site/index.html", "type": "blob", "sha": "aaa"}, ` +
		`{"path": "site", "type": "tree", "sha": "bbb"}]}`})
	shas, err := remoteBlobSHAs(effects.Unbound(), "owner/repo", "deadbeef")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !reflect.DeepEqual(shas, map[string]string{"site/index.html": "aaa"}) {
		t.Fatalf("the listing returned %v", shas)
	}
}

func TestAnUnparseableTreeResponseIsNamed(t *testing.T) {
	gh := newFakeGH(t)
	gh.Script(ghResponse{Stdout: "{not json"})
	_, err := remoteBlobSHAs(effects.Unbound(), "owner/repo", "deadbeef")
	if err == nil || !strings.Contains(err.Error(), "unparseable response") {
		t.Fatalf("err = %v, want the unparseable-response refusal", err)
	}
}

func TestGHAPIReportsTheStepAndWhatGHSaid(t *testing.T) {
	gh := newFakeGH(t)
	gh.Script(ghResponse{Code: 1, Stderr: "gh: API rate limit exceeded (HTTP 403)\n"})
	_, err := ghAPI(effects.Unbound(), ghCall{
		Args: []string{"/repos/owner/repo/git/blobs"},
		Step: "create blob for x",
	})
	if err == nil {
		t.Fatal("a non-zero gh exit was not an error")
	}
	want := "create blob for x: gh: API rate limit exceeded (HTTP 403)"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

// TestAPushWithNoBranchIsRefused pins that an unnamed branch is refused here
// rather than sent. An empty branch renders "/repos/<repo>/git/ref/heads/",
// which GitHub answers 404 to, so the caller's mistake arrives as a missing
// repository instead of a missing argument.
func TestAPushWithNoBranchIsRefused(t *testing.T) {
	newFakeGH(t)
	_, err := PushFilesToRepo(effects.Unbound(), "owner/repo",
		map[string][]byte{"a.txt": []byte("x")}, "msg", "", nil)
	if err == nil {
		t.Fatal("a push with no branch was accepted")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Errorf("the refusal does not name the branch: %v", err)
	}
}

// TestAListWithNoBranchIsRefused pins the same refusal on the read side.
func TestAListWithNoBranchIsRefused(t *testing.T) {
	newFakeGH(t)
	_, err := ListRemotePaths(effects.Unbound(), "owner/repo", "")
	if err == nil {
		t.Fatal("a listing with no branch was accepted")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Errorf("the refusal does not name the branch: %v", err)
	}
}
