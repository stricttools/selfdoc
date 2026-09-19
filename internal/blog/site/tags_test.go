package site

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/smm-h/stricttest/go/hygiene"
)

// multiReleasableTags is a repository that releases several packages, newest
// first, as `git for-each-ref --sort=-creatordate` reports it. The sibling's
// tag is the newest one in the repo; the docs target's is not.
var multiReleasableTags = []string{
	"demo@v0.3.1",
	"selfdoc-core@v0.8.1",
	"v0.36.0",
	"demo@v0.3.0",
	"v0.35.0",
}

func TestParseVersionTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag         string
		wantFamily  string
		wantVersion string
		wantOK      bool
	}{
		{tag: "v1.2.3", wantVersion: "1.2.3", wantOK: true},
		{tag: "1.2.3", wantVersion: "1.2.3", wantOK: true},
		{tag: "demo@v0.3.1", wantFamily: "demo@", wantVersion: "0.3.1", wantOK: true},
		{tag: "packages/core/v2.0.0", wantFamily: "packages/core/", wantVersion: "2.0.0", wantOK: true},
		{tag: "v1.0.0-rc.1", wantVersion: "1.0.0-rc.1", wantOK: true},
		{tag: "v1.0.0+build.7", wantVersion: "1.0.0+build.7", wantOK: true},
		{tag: "  v1.2.3  ", wantVersion: "1.2.3", wantOK: true},
		{tag: "nightly"},
		{tag: "v1.2"},
		{tag: ""},
	}
	for _, test := range tests {
		t.Run(test.tag, func(t *testing.T) {
			t.Parallel()
			family, version, ok := ParseVersionTag(test.tag)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v", ok, test.wantOK)
			}
			if family != test.wantFamily || version != test.wantVersion {
				t.Errorf("ParseVersionTag(%q) = %q, %q; want %q, %q",
					test.tag, family, version, test.wantFamily, test.wantVersion)
			}
		})
	}
}

// Guards the premise of the bug resolution exists to fix: newest-by-date is
// the wrong answer in a repository that releases more than one thing.
func TestTheNewestTagByDateBelongsToASibling(t *testing.T) {
	t.Parallel()
	if multiReleasableTags[0] != "demo@v0.3.1" {
		t.Fatalf("the fixture no longer puts a sibling's tag first")
	}
}

func TestResolveProjectTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tags    []string
		version string
		want    string
	}{
		{
			name:    "the target project's tag, not the newest",
			tags:    multiReleasableTags,
			version: "0.36.0",
			want:    "v0.36.0",
		},
		{
			name:    "a prefixed family member",
			tags:    multiReleasableTags,
			version: "0.3.1",
			want:    "demo@v0.3.1",
		},
		{
			name:    "an older version of the right family",
			tags:    multiReleasableTags,
			version: "0.35.0",
			want:    "v0.35.0",
		},
		{
			name:    "non-version tags are ignored",
			tags:    []string{"nightly", "latest", "v2.0.0"},
			version: "2.0.0",
			want:    "v2.0.0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveProjectTag(test.tags, test.version)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != test.want {
				t.Errorf("ResolveProjectTag = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveProjectTagRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tags    []string
		version string
		want    string
	}{
		{
			name:    "without a version",
			tags:    multiReleasableTags,
			version: "",
			want: "cannot resolve a release tag without a version; set " +
				"'version' in selfdoc.json or run this from a project with a " +
				"detectable version",
		},
		{
			name:    "two families at the same version",
			tags:    []string{"alpha@v1.0.0", "beta@v1.0.0"},
			version: "1.0.0",
			want: "ambiguous release tag for version 1.0.0: alpha@v1.0.0, " +
				"beta@v1.0.0. Two tag families carry the same version, so the " +
				"dispatch cannot tell which one is this project's.",
		},
		{
			name:    "an untagged version is not a fallback",
			tags:    multiReleasableTags,
			version: "0.37.0",
			want: "no git tag names version 0.37.0. Tag families in this repo: " +
				"'', 'demo@', 'selfdoc-core@'. Release this project before " +
				"dispatching an assembly rebuild -- the assembly builds the " +
				"tag, so an untagged version would publish the wrong docs.",
		},
		{
			name:    "an untagged version in a repository with no version tags",
			tags:    []string{"nightly"},
			version: "0.37.0",
			want: "no git tag names version 0.37.0. Release this project before " +
				"dispatching an assembly rebuild -- the assembly builds the " +
				"tag, so an untagged version would publish the wrong docs.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ResolveProjectTag(test.tags, test.version)
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if err.Error() != test.want {
				t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), test.want)
			}
		})
	}
}

// multiReleasableRepo is a repository with two tag families where the
// sibling's tag is newest.
func multiReleasableRepo(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "README.md"), "x\n")
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	// The docs target is tagged first; the sibling package is tagged after, so
	// it owns the newest creation date.
	runGit(t, repo, "tag", "v0.36.0")
	write(t, filepath.Join(repo, "README.md"), "y\n")
	runGit(t, repo, "commit", "-am", "more")
	runGit(t, repo, "tag", "demo@v0.3.1")
	return repo
}

func TestListRepoTagsIsNewestFirst(t *testing.T) {
	repo := multiReleasableRepo(t)
	tags, err := ListRepoTags(repo, handle())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tags) == 0 || tags[0] != "demo@v0.3.1" {
		t.Errorf("tags = %v, want the sibling's tag first", tags)
	}
	got := sortedStrings(tags)
	if !reflect.DeepEqual(got, []string{"demo@v0.3.1", "v0.36.0"}) {
		t.Errorf("tags = %v", got)
	}
}

func TestARealRepoResolvesTheDocsTargetsTag(t *testing.T) {
	repo := multiReleasableRepo(t)
	tags, err := ListRepoTags(repo, handle())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got, err := ResolveProjectTag(tags, "0.36.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "v0.36.0" {
		t.Errorf("ResolveProjectTag = %q, want v0.36.0", got)
	}
}

func TestListRepoTagsOfARepoWithNoTagsIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "README.md"), "x\n")
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	tags, err := ListRepoTags(repo, handle())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("tags = %v, want empty", tags)
	}
}

// -- the version the build produces ------------------------------------------

func TestBuildTargetVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{
			name: "no versions at all is one implicit version",
			cfg:  map[string]any{},
			want: "",
		},
		{
			name: "an empty versions array is one implicit version",
			cfg:  map[string]any{"versions": []any{}},
			want: "",
		},
		{
			name: "the last entry of versions",
			cfg: map[string]any{"versions": []any{
				map[string]any{"version": "1.0.0"},
				map[string]any{"version": "1.1.0"},
			}},
			want: "1.1.0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := BuildTargetVersion(test.cfg, "")
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if got != test.want {
				t.Errorf("BuildTargetVersion = %q, want %q", got, test.want)
			}
		})
	}
}

// The build takes the last entry, so a blank newest entry would silently
// publish the docs unversioned, at the wrong address.
func TestABlankNewestVersionEntryIsAHardError(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{"versions": []any{
		map[string]any{"version": "1.0"}, map[string]any{},
	}}
	_, err := BuildTargetVersion(cfg, "src/dir")
	want := "the newest 'versions' entry in selfdoc.json for the project at " +
		"src/dir declares no version, so there is nothing to build. The build " +
		"takes the last entry of 'versions'; give it a 'version' string."
	if err == nil || err.Error() != want {
		t.Fatalf("refusal:\n%v\nwant:\n%s", err, want)
	}
	_, err = BuildTargetVersion(cfg, "")
	want = "the newest 'versions' entry in selfdoc.json declares no version, " +
		"so there is nothing to build. The build takes the last entry of " +
		"'versions'; give it a 'version' string."
	if err == nil || err.Error() != want {
		t.Fatalf("refusal:\n%v\nwant:\n%s", err, want)
	}
}

func TestDetectLatestVersionReadsTheLastDeclaredVersion(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "selfdoc.json"),
		`{"versions": [{"version": "0.9.0"}, {"version": "1.0.0"}]}`)
	got, err := DetectLatestVersion(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if got != "1.0.0" {
		t.Errorf("DetectLatestVersion = %q, want 1.0.0", got)
	}
}

// A project with no selfdoc.json at all builds unversioned.
func TestDetectLatestVersionIsEmptyWithoutAConfig(t *testing.T) {
	hygiene.Isolate(t)
	got, err := DetectLatestVersion(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if got != "" {
		t.Errorf("DetectLatestVersion = %q, want empty", got)
	}
}

func TestDetectLatestVersionErrorsOnAVersionlessMultiVersionProject(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "selfdoc.json"),
		`{"versions": [{"version": "0.9.0"}, {}]}`)
	_, err := DetectLatestVersion(dir)
	if err == nil || !strings.Contains(err.Error(), "declares no version") {
		t.Fatalf("err = %v, want the blank-newest-entry refusal", err)
	}
}

// -- the version a dispatch may carry ----------------------------------------

func TestCheckVersionIsDeclaredPasses(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{"versions": []any{
		map[string]any{"version": "1.0.0"}, map[string]any{"version": "1.1.0"},
	}}
	if err := CheckVersionIsDeclared(cfg, "1.1.0"); err != nil {
		t.Errorf("err = %v, want none", err)
	}
}

func TestCheckVersionIsDeclaredRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     map[string]any
		version string
		want    string
	}{
		{
			name:    "no declared versions at all",
			cfg:     map[string]any{"versions": []any{}},
			version: "1.0.0",
			want: "selfdoc.json declares no versions; the assembly has nothing " +
				"to build. Add the released version to 'versions'.",
		},
		{
			name: "an undeclared version",
			cfg: map[string]any{"versions": []any{
				map[string]any{"version": "0.1.0"},
			}},
			version: "1.1.0",
			want: "version 1.1.0 is not the version the assembly would build. " +
				"selfdoc.json's newest declared version is 0.1.0 (declared: " +
				"0.1.0), and the build takes the newest one -- so this dispatch " +
				"would publish 0.1.0's docs recorded under the name 1.1.0. Make " +
				"1.1.0 the last entry of 'versions'.",
		},
		{
			// Membership was never the question: the build takes the last
			// entry. Dispatching 0.9.0 against [0.9.0, 1.4.2] passed the old
			// membership test and then published 1.4.2's docs recorded under
			// the name 0.9.0.
			name: "a declared but not newest version",
			cfg: map[string]any{"versions": []any{
				map[string]any{"version": "0.9.0"}, map[string]any{"version": "1.4.2"},
			}},
			version: "0.9.0",
			want: "version 0.9.0 is not the version the assembly would build. " +
				"selfdoc.json's newest declared version is 1.4.2 (declared: " +
				"0.9.0, 1.4.2), and the build takes the newest one -- so this " +
				"dispatch would publish 1.4.2's docs recorded under the name " +
				"0.9.0. Make 0.9.0 the last entry of 'versions'.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := CheckVersionIsDeclared(test.cfg, test.version)
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if err.Error() != test.want {
				t.Errorf("refusal:\n%s\nwant:\n%s", err.Error(), test.want)
			}
		})
	}
}

// The build target cannot be read, so nothing may be dispatched.
func TestANewestEntryWithNoVersionIsAHardError(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{"versions": []any{
		map[string]any{"version": "1.0"}, map[string]any{},
	}}
	if err := CheckVersionIsDeclared(cfg, "1.0"); err == nil {
		t.Fatal("want a refusal, got none")
	}
}

// One definition of "the version the build produces", used by both.
func TestTheCheckAgreesWithWhatTheBuildWouldProduce(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "selfdoc.json"),
		`{"versions": [{"version": "1.0"}, {"version": "2.0"}]}`)
	built, err := DetectLatestVersion(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	cfg := map[string]any{"versions": []any{
		map[string]any{"version": "1.0"}, map[string]any{"version": "2.0"},
	}}
	if err := CheckVersionIsDeclared(cfg, built); err != nil {
		t.Errorf("err = %v, want none", err)
	}
	if err := CheckVersionIsDeclared(cfg, "1.0"); err == nil {
		t.Error("want a refusal for the stale dispatch, got none")
	}
}

// An unversioned project is dispatched under the literal, and the versions
// array a loaded config carries for it -- one anonymous entry -- is not the
// thing to check it against.
func TestCheckVersionIsDeclaredAcceptsAnUnversionedProject(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"unversioned": true,
		"versions":    []any{map[string]any{"version": ""}},
	}
	if err := CheckVersionIsDeclared(cfg, config.UnversionedVersion); err != nil {
		t.Errorf("err = %v, want none", err)
	}
}

// A version string dispatched for a project that declares it has none is a
// version nobody released.
func TestCheckVersionIsDeclaredRefusesAVersionForAnUnversionedProject(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{"unversioned": true}
	err := CheckVersionIsDeclared(cfg, "1.0.0")
	if err == nil {
		t.Fatal("want a refusal, got none")
	}
	if !strings.Contains(err.Error(), "unversioned") {
		t.Errorf("the refusal does not name the declaration: %s", err)
	}
}
