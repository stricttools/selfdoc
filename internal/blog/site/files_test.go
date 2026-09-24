package site

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// -- the produced set --------------------------------------------------------

func TestBuildOutputPathsIsRelativeAndSlashJoined(t *testing.T) {
	build := buildTree(t)
	produced, err := BuildOutputPaths(build, true)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !containsString(produced, "guide/index.html") {
		t.Errorf("produced = %v, want guide/index.html", produced)
	}
}

func TestBuildOutputPathsExcludesDeployArtifacts(t *testing.T) {
	build := buildTree(t)
	produced, err := BuildOutputPaths(build, true)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, unwanted := range []string{"_headers", "index.html.gz", "guide/index.html.br"} {
		if containsString(produced, unwanted) {
			t.Errorf("produced carries %s, which the assembly must not inherit", unwanted)
		}
	}
}

func TestBuildOutputPathsKeepsArtifactsWhenAskedTo(t *testing.T) {
	build := buildTree(t)
	produced, err := BuildOutputPaths(build, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !containsString(produced, "_headers") {
		t.Errorf("produced = %v, want _headers", produced)
	}
}

func TestBuildOutputPathsOfAMissingDirectoryIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	produced, err := BuildOutputPaths(filepath.Join(t.TempDir(), "nope"), true)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(produced) != 0 {
		t.Errorf("produced = %v, want empty", produced)
	}
}

func TestIsDeployArtifact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"_headers", true},
		{"_redirects", true},
		{"_worker.js", true},
		{"404.html", true},
		{"index.html.gz", true},
		{"style.css.br", true},
		{"index.html", false},
		{"headers", false},
	}
	for _, test := range tests {
		if got := IsDeployArtifact(test.name); got != test.want {
			t.Errorf("IsDeployArtifact(%q) = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestPruneDeployArtifactsRemovesThemAndReportsThePaths(t *testing.T) {
	build := buildTree(t)
	removed, err := PruneDeployArtifacts(build, handle())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	want := []string{
		filepath.Join(build, "_headers"),
		filepath.Join(build, "_redirects"),
		filepath.Join(build, "_worker.js"),
		filepath.Join(build, "guide", "index.html.br"),
		filepath.Join(build, "index.html.gz"),
	}
	if !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	for _, path := range want {
		if exists(path) {
			t.Errorf("%s is still there", path)
		}
	}
	if !exists(filepath.Join(build, "index.html")) {
		t.Error("the prune took a page with it")
	}
}

func TestPruneDeployArtifactsOfAMissingDirectoryIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	removed, err := PruneDeployArtifacts(filepath.Join(t.TempDir(), "nope"), handle())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
}

// root itself always survives, even when the subtree ends up empty: the
// project still has a section, it just has no files in it.
func TestPruneEmptyDirsKeepsTheRoot(t *testing.T) {
	hygiene.Isolate(t)
	root := filepath.Join(t.TempDir(), "site")
	write(t, filepath.Join(root, "keep", "index.html"), "x")
	if err := mkdirAll(filepath.Join(root, "gone")); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	removed, err := PruneEmptyDirs(root, handle())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !reflect.DeepEqual(removed, []string{filepath.Join(root, "gone")}) {
		t.Errorf("removed = %v", removed)
	}
	if !exists(root) || !exists(filepath.Join(root, "keep")) {
		t.Error("the prune took the root or an occupied directory")
	}
}

func TestPruneEmptyDirsOfAMissingDirectoryIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	removed, err := PruneEmptyDirs(filepath.Join(t.TempDir(), "nope"), handle())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
}

// The order is every directory path ascending, so a parent is examined before
// its children: a nest of empty directories collapses one level per call, as
// it did in the Python.
func TestPruneEmptyDirsCollapsesOneLevelPerCall(t *testing.T) {
	hygiene.Isolate(t)
	root := filepath.Join(t.TempDir(), "site")
	if err := mkdirAll(filepath.Join(root, "outer", "inner")); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	removed, err := PruneEmptyDirs(root, handle())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !reflect.DeepEqual(removed, []string{filepath.Join(root, "outer", "inner")}) {
		t.Errorf("removed = %v, want only the innermost directory", removed)
	}
	removed, err = PruneEmptyDirs(root, handle())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !reflect.DeepEqual(removed, []string{filepath.Join(root, "outer")}) {
		t.Errorf("removed = %v, want the parent on the second pass", removed)
	}
}

// -- the prune plan ----------------------------------------------------------

func TestAPathTheBuildDroppedIsPruned(t *testing.T) {
	t.Parallel()
	removed, updated, err := PrunePlan(
		map[string][]string{"release": {"a.html", "gone.html"}},
		"release", []string{"a.html"},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !reflect.DeepEqual(removed, []string{"gone.html"}) {
		t.Errorf("removed = %v, want [gone.html]", removed)
	}
	if !reflect.DeepEqual(updated["release"], []string{"a.html"}) {
		t.Errorf("updated[release] = %v", updated["release"])
	}
}

// This is the whole difference from a wipe: unknown means not mine.
func TestAPathNobodyRecordedIsNeverPruned(t *testing.T) {
	t.Parallel()
	removed, _, err := PrunePlan(
		map[string][]string{"release": {"a.html"}}, "release", []string{"a.html"},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
}

func TestAnotherPublishersCurrentPathIsNeverPruned(t *testing.T) {
	t.Parallel()
	removed, _, err := PrunePlan(map[string][]string{
		"release": {"blog/x/index.html"},
		"posts":   {"blog/x/index.html"},
	}, "release", nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
}

func TestThePruningPublisherOnlyTouchesItsOwnRecord(t *testing.T) {
	t.Parallel()
	_, updated, err := PrunePlan(map[string][]string{
		"release": {"a.html"}, "docs": {"b.html"},
	}, "release", []string{"c.html"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !reflect.DeepEqual(updated["docs"], []string{"b.html"}) {
		t.Errorf("updated[docs] = %v", updated["docs"])
	}
	if !reflect.DeepEqual(updated["release"], []string{"c.html"}) {
		t.Errorf("updated[release] = %v", updated["release"])
	}
}

func TestAnUnknownPublisherIsAHardError(t *testing.T) {
	t.Parallel()
	_, _, err := PrunePlan(nil, "somebody", nil)
	want := "unknown publisher 'somebody'; expected one of release, docs, posts"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestEveryPublisherIsADeclaredOne(t *testing.T) {
	t.Parallel()
	if !reflect.DeepEqual(PublishOwners, []string{"release", "docs", "posts"}) {
		t.Errorf("PublishOwners = %v", PublishOwners)
	}
}

// -- the published-file record -----------------------------------------------

func TestFilesManifestPath(t *testing.T) {
	t.Parallel()
	want := filepath.Join("manifests", "alpha-files.json")
	if got := FilesManifestPath("manifests", "alpha"); got != want {
		t.Errorf("FilesManifestPath = %q, want %q", got, want)
	}
}

func TestAnAbsentRecordIsAnEmptyMapping(t *testing.T) {
	hygiene.Isolate(t)
	record, err := LoadFilesManifest(filepath.Join(t.TempDir(), "nothing.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(record) != 0 {
		t.Errorf("record = %v, want empty", record)
	}
}

func TestAnEmptyRecordIsAnEmptyMapping(t *testing.T) {
	t.Parallel()
	record, err := ParseFilesManifest("   \n", "p.json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(record) != 0 {
		t.Errorf("record = %v, want empty", record)
	}
}

// A falsy owners value -- absent, null, an empty object or an empty array
// alike -- is an empty mapping and never reaches the type check, which is what
// the Python's `data.get("owners") or {}` did.
func TestAFalsyOwnersValueIsAnEmptyMapping(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"schema_version": 2}`,
		`{"schema_version": 2, "owners": null}`,
		`{"schema_version": 2, "owners": {}}`,
		`{"schema_version": 2, "owners": []}`,
	} {
		record, err := ParseFilesManifest(raw, "p.json")
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		if len(record) != 0 {
			t.Errorf("parse %s = %v, want empty", raw, record)
		}
	}
}

func TestParseFilesManifestRefusals(t *testing.T) {
	t.Parallel()
	const versionOne = "p.json is a version 1 published-file record; this " +
		"selfdoc writes and reads version 2. Version 1 addressed every path " +
		"from the project's own subtree and put posts at '<slug>/posts/...'; " +
		"version 2 addresses every path from site/ and posts are site-level, " +
		"at 'blog/<post-slug>/...'. The two cannot be told apart by reading " +
		"them, so this one is refused rather than reinterpreted. Either " +
		"rewrite it -- prefix every documentation path with '<slug>/' and " +
		"re-address every post as 'blog/<post-slug>/...' -- or delete it " +
		"together with the stale site/<slug>/posts/ tree it describes, in " +
		"which case the next publish records what it produces and until then " +
		"no publisher is entitled to remove anything."
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "not a JSON object",
			raw:  "[]",
			want: "p.json must contain a JSON object",
		},
		{
			name: "a version 1 record",
			raw:  `{"schema_version": 1}`,
			want: versionOne,
		},
		{
			name: "no version at all",
			raw:  `{"owners": {}}`,
			want: strings.Replace(versionOne, "a version 1 published", "a version None published", 1),
		},
		{
			name: "an unknown publisher",
			raw:  `{"schema_version": 2, "owners": {"whoever": ["a"], "nope": ["b"]}}`,
			want: "p.json records paths under unknown publisher(s) 'nope', " +
				"'whoever'; known publishers are release, docs, posts.",
		},
		{
			name: "paths that are not a list",
			raw:  `{"schema_version": 2, "owners": {"release": "a"}}`,
			want: "p.json: the paths recorded for 'release' must be a list of strings.",
		},
		{
			name: "paths that are not strings",
			raw:  `{"schema_version": 2, "owners": {"release": [1]}}`,
			want: "p.json: the paths recorded for 'release' must be a list of strings.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseFilesManifest(test.raw, "p.json")
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if err.Error() != test.want {
				t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), test.want)
			}
		})
	}
}

func TestACorruptRecordIsAHardError(t *testing.T) {
	t.Parallel()
	_, err := ParseFilesManifest("{not json", "p.json")
	if err == nil || !strings.HasPrefix(err.Error(), "p.json is not valid JSON: ") {
		t.Fatalf("err = %v, want the invalid-JSON refusal", err)
	}
}

func TestRenderFilesManifestIsByteIdenticalToThePython(t *testing.T) {
	t.Parallel()
	got, err := RenderFilesManifest("alpha", map[string][]string{
		"release": {"b.html", "a.html"},
		"posts":   {"blog/x/index.html"},
		"docs":    {},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := reference(t, "files-record.json"); got != want {
		t.Errorf("rendered record:\n%q\nwant:\n%q", got, want)
	}
}

func TestARenderedRecordRoundTrips(t *testing.T) {
	hygiene.Isolate(t)
	path := filepath.Join(t.TempDir(), "alpha-files.json")
	rendered, err := RenderFilesManifest("alpha", map[string][]string{
		"release": {"b.html", "a.html"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	write(t, path, rendered)
	record, err := LoadFilesManifest(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := map[string][]string{"release": {"a.html", "b.html"}}
	if !reflect.DeepEqual(record, want) {
		t.Errorf("record = %v, want %v", record, want)
	}
}

func TestARenderedRecordOmitsPublishersThatPublishedNothing(t *testing.T) {
	t.Parallel()
	rendered, err := RenderFilesManifest("alpha", map[string][]string{
		"release": {"a"}, "docs": {},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(rendered, "docs") {
		t.Errorf("rendered record names a publisher with nothing to claim:\n%s", rendered)
	}
}

// -- the record a remote publisher stages ------------------------------------

func TestStagePublishedRecordPrunesAgainstWhatItProduces(t *testing.T) {
	t.Parallel()
	previous, err := RenderFilesManifest("alpha", map[string][]string{
		"docs":    {"alpha/index.html", "alpha/gone/index.html"},
		"release": {"alpha/kept-by-release.html"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var asked []string
	fetch := func(repo, path, operation string) (string, error) {
		asked = append(asked, repo+" "+path+" | "+operation)
		return previous, nil
	}
	files := map[string][]byte{}
	deletions, err := StagePublishedRecord(
		fetch, "owner/assembly", "alpha", "docs",
		[]string{"alpha/index.html"}, files,
	)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !reflect.DeepEqual(deletions, []string{"site/alpha/gone/index.html"}) {
		t.Errorf("deletions = %v", deletions)
	}
	wantAsked := []string{
		"owner/assembly manifests/alpha-files.json | record what 'docs' " +
			"publishes for 'alpha' on owner/assembly",
	}
	if !reflect.DeepEqual(asked, wantAsked) {
		t.Errorf("asked = %q, want %q", asked, wantAsked)
	}
	staged, ok := files["manifests/alpha-files.json"]
	if !ok {
		t.Fatal("the rewritten record was not staged beside the content")
	}
	record, err := ParseFilesManifest(string(staged), "staged")
	if err != nil {
		t.Fatalf("parse staged: %v", err)
	}
	want := map[string][]string{
		"docs":    {"alpha/index.html"},
		"release": {"alpha/kept-by-release.html"},
	}
	if !reflect.DeepEqual(record, want) {
		t.Errorf("staged record = %v, want %v", record, want)
	}
}

// Absence is the real first-publish state: the record is created naming only
// what this publisher produced.
func TestStagePublishedRecordOnAFirstPublish(t *testing.T) {
	t.Parallel()
	fetch := func(repo, path, operation string) (string, error) { return "", nil }
	files := map[string][]byte{}
	deletions, err := StagePublishedRecord(
		fetch, "owner/assembly", "alpha", "posts",
		[]string{"blog/hello/index.html"}, files,
	)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if len(deletions) != 0 {
		t.Errorf("deletions = %v, want empty", deletions)
	}
	record, err := ParseFilesManifest(string(files["manifests/alpha-files.json"]), "staged")
	if err != nil {
		t.Fatalf("parse staged: %v", err)
	}
	want := map[string][]string{"posts": {"blog/hello/index.html"}}
	if !reflect.DeepEqual(record, want) {
		t.Errorf("staged record = %v, want %v", record, want)
	}
}

// A failed read would prune this owner's entry against a record it never saw
// and drop every other owner's claims from the file it rewrites.
func TestStagePublishedRecordRefusesWhenItCannotRead(t *testing.T) {
	t.Parallel()
	fetch := func(repo, path, operation string) (string, error) {
		return "", errorf("reading %s from %s failed", path, repo)
	}
	files := map[string][]byte{}
	_, err := StagePublishedRecord(
		fetch, "owner/assembly", "alpha", "docs", []string{"alpha/index.html"}, files,
	)
	if err == nil {
		t.Fatal("want the read failure, got none")
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want nothing staged", files)
	}
}

// -- git's own blob hash -----------------------------------------------------

// The local hash is git's, so it is comparable with the API's shas.
func TestGitBlobSHA1MatchesGitsOwnObjectID(t *testing.T) {
	t.Parallel()
	// printf 'hello\n' | git hash-object --stdin
	if got := GitBlobSHA1([]byte("hello\n")); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("GitBlobSHA1 = %q", got)
	}
}

func TestGitBlobSHA1HashesTheHeaderAndTheBytes(t *testing.T) {
	t.Parallel()
	// A four-byte payload with a NUL and a high byte in it, so a hash computed
	// over decoded text rather than bytes would differ. The expected value is
	// git's own: printf '\x89\x50\x00\xff' | git hash-object --stdin
	data := []byte{0x89, 0x50, 0x00, 0xff}
	if got := GitBlobSHA1(data); got != "195f0ed1f7100845c42f98844c782b370775301f" {
		t.Errorf("GitBlobSHA1 = %q", got)
	}
}
