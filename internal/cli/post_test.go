package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// today is the date every scaffolded post carries.
func today() string { return time.Now().Format("2006-01-02") }

// postProject creates a minimal project, with the given config overrides.
func postProject(t *testing.T, overrides map[string]any) string {
	t.Helper()
	dir := testproject.Dir(t)
	config := map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"author":        testproject.Author(),
		"search_engine": "pagefind",
		"version":       "1.0.0",
		"versions":      []any{map[string]any{"version": "1.0.0"}},
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
	}
	for key, value := range overrides {
		config[key] = value
	}
	testproject.WriteJSON(t, filepath.Join(dir, "selfdoc.json"), config)
	writeText(t, filepath.Join(dir, "src", "__init__.py"), "")
	return dir
}

// writePost writes a post file with the given frontmatter lines.
func writePost(t *testing.T, postsDir, filename string, lines []string, body string) {
	t.Helper()
	declared := false
	for _, line := range lines {
		if strings.HasPrefix(line, "directives ") {
			declared = true
		}
	}
	if !declared {
		lines = append(append([]string{}, lines...), "directives = false")
	}
	writeText(t, filepath.Join(postsDir, filename),
		"+++\n"+strings.Join(lines, "\n")+"\n+++\n"+body)
}

// -- post new ---------------------------------------------------------------

func TestPostNewCreatesTheFile(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)

	result := run(t, dir, "blog", "post", "new", "--title", "My First Post")
	if result.ExitCode != 0 {
		t.Fatalf("post new failed: %s", result.Stderr)
	}
	expected := filepath.Join(dir, "stricttools", "posts", today()+"-my-first-post.md")
	if !exists(expected) {
		t.Fatalf("no post at %s", expected)
	}
	if !strings.Contains(result.Stdout, filepath.Join("stricttools", "posts", today()+"-my-first-post.md")) {
		t.Errorf("the created path is not reported:\n%s", result.Stdout)
	}
}

func TestPostNewUsesTheConfiguredPostsDir(t *testing.T) {
	isolate(t)
	// A declared posts directory is honoured, and it lives inside the posts
	// function directory: a posts path outside the tool-state directory is
	// the layout selfdoc used before, and is refused as that.
	dir := postProject(t, map[string]any{
		"posts": map[string]any{"dir": "stricttools/posts/articles/"},
	})

	if result := run(t, dir, "blog", "post", "new", "--title", "Custom Dir"); result.ExitCode != 0 {
		t.Fatalf("post new failed: %s", result.Stderr)
	}
	if !exists(filepath.Join(dir, "stricttools", "posts", "articles", today()+"-custom-dir.md")) {
		t.Error("the post did not land in the configured directory")
	}
}

func TestPostNewFrontmatter(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)

	if result := run(t, dir, "blog", "post", "new", "--title", "Frontmatter Check"); result.ExitCode != 0 {
		t.Fatalf("post new failed: %s", result.Stderr)
	}
	content := readText(t, filepath.Join(dir, "stricttools", "posts", today()+"-frontmatter-check.md"))

	if !strings.HasPrefix(content, "+++\n") {
		t.Fatalf("no frontmatter:\n%s", content)
	}
	for _, want := range []string{
		"title = \"Frontmatter Check\"\n",
		"date = " + today() + "\n",
		"slug = \"frontmatter-check\"\n",
		"tags = []\n",
		"draft = true\n",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("frontmatter does not carry %q:\n%s", want, content)
		}
	}
	if !strings.HasSuffix(content, "+++\n\n") {
		t.Errorf("the scaffold does not end with the closing fence and a blank line:\n%q", content)
	}
	// The scaffold emits no 'project' key: a post's owning project is the
	// repository it lives in, and the assembly learns it from the manifest.
	if strings.Contains(content, "project =") {
		t.Errorf("the scaffold declares a project key:\n%s", content)
	}
}

func TestPostNewRefusesAnExistingFile(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	writeText(t, filepath.Join(dir, "stricttools", "posts", today()+"-duplicate.md"), "existing")

	result := run(t, dir, "blog", "post", "new", "--title", "Duplicate")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "already exists") {
		t.Errorf("the refusal is not the file's: %s", result.Stderr)
	}
}

func TestPostNewRefusesAnEmptyTitle(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	if result := run(t, dir, "blog", "post", "new", "--title", ""); result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
}

func TestPostNewRefusesAnOmittedTitle(t *testing.T) {
	// --title declares presence required, so absence never reaches the
	// handler: the framework refuses it at parse time.
	isolate(t)
	dir := postProject(t, nil)
	result := run(t, dir, "blog", "post", "new")
	if result.ExitCode == 0 {
		t.Fatal("post new ran with no title")
	}
	if !strings.Contains(result.Stderr, "--title") {
		t.Errorf("the refusal does not name the flag: %s", result.Stderr)
	}
}

func TestPostNewRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "blog", "post", "new", "--title", "Orphan Post")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No selfdoc.json") {
		t.Errorf("the refusal is not the missing config's: %s", result.Stderr)
	}
}

// A granted posts directory holds nothing but the manifest that grants it
// until the first post is written into it.
func TestPostNewWritesIntoTheGrantedPostsDirectory(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	postsDir := filepath.Join(dir, "stricttools", "posts")
	entries, err := os.ReadDir(postsDir)
	if err != nil {
		t.Fatalf("reading the granted posts directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != layout.ManifestFileName {
		t.Fatalf("the fixture's posts directory holds %v, want its manifest alone", entries)
	}
	if result := run(t, dir, "blog", "post", "new", "--title", "Dir Creation Test"); result.ExitCode != 0 {
		t.Fatalf("post new failed: %s", result.Stderr)
	}
	if !exists(filepath.Join(postsDir, today()+"-dir-creation-test.md")) {
		t.Error("the post was not written into the posts directory")
	}
}

// -- post list --------------------------------------------------------------

func TestPostListRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "blog", "post", "list")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No selfdoc.json") {
		t.Errorf("the refusal is not the missing config's: %s", result.Stderr)
	}
}

func TestPostListSaysSoWhenThereAreNone(t *testing.T) {
	isolate(t)
	dir := postProject(t, map[string]any{"posts": map[string]any{"dir": "stricttools/posts/"}})
	result := run(t, dir, "blog", "post", "list")
	if !strings.Contains(result.Stdout, "No posts found") {
		t.Errorf("an empty posts directory is not reported:\n%s", result.Stdout)
	}
}

func TestPostListReportsEveryPost(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	postsDir := filepath.Join(dir, "stricttools", "posts")
	writePost(t, postsDir, "a.md", []string{"title = \"First Post\"", "date = 2025-01-15"}, "")
	writePost(t, postsDir, "b.md", []string{"title = \"Second Post\"", "date = 2025-03-20"}, "")

	result := run(t, dir, "blog", "post", "list")
	for _, want := range []string{"First Post", "Second Post", "2 post(s) found"} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("the listing does not carry %q:\n%s", want, result.Stdout)
		}
	}
}

func TestPostListMarksDrafts(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	postsDir := filepath.Join(dir, "stricttools", "posts")
	writePost(t, postsDir, "a.md",
		[]string{"title = \"Draft Post\"", "date = 2025-01-15", "draft = true"}, "")

	result := run(t, dir, "blog", "post", "list")
	if !strings.Contains(result.Stdout, "[DRAFT]") {
		t.Errorf("a draft is not marked:\n%s", result.Stdout)
	}
}

func TestPostListLeavesPublishedPostsUnmarked(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	postsDir := filepath.Join(dir, "stricttools", "posts")
	writePost(t, postsDir, "a.md", []string{"title = \"Published Post\"", "date = 2025-01-15"}, "")

	result := run(t, dir, "blog", "post", "list")
	if strings.Contains(result.Stdout, "[DRAFT]") {
		t.Errorf("a published post is marked as a draft:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Published Post") {
		t.Errorf("the post is missing:\n%s", result.Stdout)
	}
}

func TestPostListIsNewestFirst(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	postsDir := filepath.Join(dir, "stricttools", "posts")
	writePost(t, postsDir, "old.md", []string{"title = \"Old\"", "date = 2024-06-01"}, "")
	writePost(t, postsDir, "new.md", []string{"title = \"New\"", "date = 2025-07-01"}, "")
	writePost(t, postsDir, "mid.md", []string{"title = \"Mid\"", "date = 2025-01-01"}, "")

	result := run(t, dir, "blog", "post", "list")
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line != "" && !strings.Contains(line, "post(s)") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("expected three listing lines, got %v", lines)
	}
	if !strings.Contains(lines[0], "2025-07-01") {
		t.Errorf("the newest post is not first: %q", lines[0])
	}
	if !strings.Contains(lines[2], "2024-06-01") {
		t.Errorf("the oldest post is not last: %q", lines[2])
	}
}

func TestPostListShowsTheSlug(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	writePost(t, filepath.Join(dir, "stricttools", "posts"), "a.md",
		[]string{"title = \"My Great Post\"", "date = 2025-01-15", "slug = \"custom-slug\""}, "")

	result := run(t, dir, "blog", "post", "list")
	if !strings.Contains(result.Stdout, "(custom-slug)") {
		t.Errorf("the slug is not shown:\n%s", result.Stdout)
	}
}

// -- post generate ----------------------------------------------------------

// writeManifest creates a manifest with optional existing posts.
func writeManifest(t *testing.T, dir, version string, posts []any) {
	t.Helper()
	if posts == nil {
		posts = []any{}
	}
	testproject.WriteJSON(t, filepath.Join(dir, "stricttools", ".docs-state", "manifest.json"), map[string]any{
		"schema_version": 2,
		"name":           "test",
		"slug":           "test",
		"version":        version,
		"description":    "",
		"language":       "python",
		"base_url":       "https://example.com",
		"author":         testproject.Author(),
		"pages":          []any{},
		"posts":          posts,
		"last_gen":       "",
	})
}

func TestPostGenerateWritesEveryDeclaredField(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	writeManifest(t, dir, "1.0.0", nil)
	writeText(t, filepath.Join(dir, "changelog.md"), "- Fixed a bug\n- Added a feature\n")
	writeText(t, filepath.Join(dir, "body.md"), "This is a great release!\n")

	result := run(t, dir, "blog", "post", "generate", "--from-release",
		"--version", "2.0.0", "--prev-version", "1.0.0",
		"--bump-type", "major", "--description", "Major release",
		"--context", "Big changes",
		"--changelog-file", filepath.Join(dir, "changelog.md"),
		"--body-file", filepath.Join(dir, "body.md"),
		"--project-name", "MyProject",
		"--release-url", "https://github.com/org/repo/releases/tag/v2.0.0",
		"--registry-url", "https://pypi.org/project/myproject/2.0.0/",
		"--registry-url", "https://npmjs.com/package/myproject")
	if result.ExitCode != 0 {
		t.Fatalf("post generate failed: %s", result.Stderr)
	}

	content := readText(t, filepath.Join(dir, "stricttools", "posts", today()+"-release-v2.0.0.md"))
	for _, want := range []string{
		"title = \"MyProject v2.0.0\"\n",
		"date = " + today() + "\n",
		"slug = \"release-v2.0.0\"\n",
		"draft = false\n",
		"version = \"2.0.0\"\n",
		"prev_version = \"1.0.0\"\n",
		"bump_type = \"major\"\n",
		"release_url = \"https://github.com/org/repo/releases/tag/v2.0.0\"\n",
		"registry_urls = [\"https://pypi.org/project/myproject/2.0.0/\", \"https://npmjs.com/package/myproject\"]\n",
		"tags = [\"release\", \"v2.0.0\"]\n",
		"This is a great release!",
		"## Changelog",
		"- Fixed a bug",
		"- Added a feature",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("the post does not carry %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "project =") {
		t.Errorf("the post declares a project key:\n%s", content)
	}
}

func TestPostGenerateMinimalPost(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)

	if result := run(t, dir, "blog", "post", "generate", "--from-release", "--version", "1.2.3"); result.ExitCode != 0 {
		t.Fatalf("post generate failed: %s", result.Stderr)
	}
	content := readText(t, filepath.Join(dir, "stricttools", "posts", today()+"-release-v1.2.3.md"))
	if !strings.Contains(content, "title = \"Release v1.2.3\"\n") {
		t.Errorf("the default title is wrong:\n%s", content)
	}
	for _, absent := range []string{"prev_version = \"", "bump_type = \"", "release_url = \"", "registry_urls = []"} {
		if strings.Contains(content, absent) {
			t.Errorf("an unstated field was written: %q\n%s", absent, content)
		}
	}
	if !strings.Contains(content, "Version 1.2.3 has been released.") {
		t.Errorf("the default body is missing:\n%s", content)
	}
}

func TestPostGenerateBodyWithoutChangelog(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	writeText(t, filepath.Join(dir, "body.md"), "Custom release notes here.\n")

	if result := run(t, dir, "blog", "post", "generate", "--from-release",
		"--version", "0.5.0", "--body-file", filepath.Join(dir, "body.md")); result.ExitCode != 0 {
		t.Fatalf("post generate failed: %s", result.Stderr)
	}
	content := readText(t, filepath.Join(dir, "stricttools", "posts", today()+"-release-v0.5.0.md"))
	if !strings.Contains(content, "Custom release notes here.") {
		t.Errorf("the body is missing:\n%s", content)
	}
	if strings.Contains(content, "## Changelog") {
		t.Errorf("a changelog section appeared with no changelog:\n%s", content)
	}
}

func TestPostGenerateChangelogWithoutBody(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	writeText(t, filepath.Join(dir, "changes.md"), "- Bug fix #42\n- Performance improvement\n")

	if result := run(t, dir, "blog", "post", "generate", "--from-release",
		"--version", "3.1.0", "--changelog-file", filepath.Join(dir, "changes.md")); result.ExitCode != 0 {
		t.Fatalf("post generate failed: %s", result.Stderr)
	}
	content := readText(t, filepath.Join(dir, "stricttools", "posts", today()+"-release-v3.1.0.md"))
	body := content[strings.LastIndex(content, "\n+++\n")+len("\n+++\n"):]
	if !strings.HasPrefix(strings.TrimSpace(body), "## Changelog") {
		t.Errorf("the body does not start with the changelog heading:\n%s", body)
	}
	for _, want := range []string{"- Bug fix #42", "- Performance improvement"} {
		if !strings.Contains(body, want) {
			t.Errorf("the changelog is missing %q:\n%s", want, body)
		}
	}
}

func TestPostGenerateUpdatesTheManifest(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	existing := map[string]any{
		"path": "2025-01-01-old-post.md", "title": "Old Post",
		"date": "2025-01-01", "slug": "old-post", "tags": []any{"misc"},
	}
	writeManifest(t, dir, "1.0.0", []any{existing})

	if result := run(t, dir, "blog", "post", "generate", "--from-release", "--version", "1.1.0"); result.ExitCode != 0 {
		t.Fatalf("post generate failed: %s", result.Stderr)
	}

	document := readJSON(t, filepath.Join(dir, "stricttools", ".docs-state", "manifest.json"))
	if document["version"] != "1.1.0" {
		t.Errorf("the manifest version is %v, want 1.1.0", document["version"])
	}
	posts := document["posts"].([]any)
	if len(posts) != 2 {
		t.Fatalf("expected the old post plus the new one, got %v", posts)
	}
	if !sameJSON(t, posts[0], existing) {
		t.Errorf("the existing post was rewritten: %v", posts[0])
	}
	added := posts[1].(map[string]any)
	if added["path"] != today()+"-release-v1.1.0.md" ||
		added["title"] != "Release v1.1.0" ||
		added["date"] != today() ||
		added["slug"] != "release-v1.1.0" {
		t.Errorf("the appended entry is %v", added)
	}
	tags := added["tags"].([]any)
	if len(tags) != 2 || tags[0] != "release" || tags[1] != "v1.1.0" {
		t.Errorf("the appended tags are %v", tags)
	}
}

func TestPostGeneratePatchPreservesTheManifestsOwnFields(t *testing.T) {
	// The document is patched rather than regenerated, so a field this
	// command has no opinion about is written back exactly as it stood.
	isolate(t)
	dir := postProject(t, nil)
	writeManifest(t, dir, "1.0.0", nil)
	before := readJSON(t, filepath.Join(dir, "stricttools", ".docs-state", "manifest.json"))

	if result := run(t, dir, "blog", "post", "generate", "--from-release", "--version", "1.1.0"); result.ExitCode != 0 {
		t.Fatalf("post generate failed: %s", result.Stderr)
	}
	after := readJSON(t, filepath.Join(dir, "stricttools", ".docs-state", "manifest.json"))

	for key := range before {
		if key == "version" || key == "posts" {
			continue
		}
		if !sameJSON(t, before[key], after[key]) {
			t.Errorf("field %q changed: %v -> %v", key, before[key], after[key])
		}
	}
}

func TestPostGenerateDryRunRecordsTheWrites(t *testing.T) {
	// The command's own --dry-run is gone: the name is reserved, and the
	// framework flag is what puts the run in preview mode. The preview is the
	// would-do log, which names every path the real run would write.
	isolate(t)
	dir := postProject(t, nil)
	writeManifest(t, dir, "1.0.0", nil)

	result := run(t, dir, "blog", "post", "generate", "--from-release",
		"--version", "1.1.0", "--prev-version", "1.0.0",
		"--bump-type", "minor", "--project-name", "DryTest", "--dry-run")
	if result.ExitCode != 0 {
		t.Fatalf("the preview failed: %s", result.Stderr)
	}

	filename := today() + "-release-v1.1.0.md"
	if exists(filepath.Join(dir, "stricttools", "posts", filename)) {
		t.Error("the preview wrote the post")
	}
	document := readJSON(t, filepath.Join(dir, "stricttools", ".docs-state", "manifest.json"))
	if document["version"] != "1.0.0" {
		t.Errorf("the preview changed the manifest version: %v", document["version"])
	}
	if len(document["posts"].([]any)) != 0 {
		t.Errorf("the preview changed the manifest's posts: %v", document["posts"])
	}
	for _, want := range []string{"DRY RUN", filename, "manifest.json"} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("the would-do log does not name %q:\n%s", want, result.Stdout)
		}
	}
}

func TestPostGenerateRefusesTheModeItDoesNotHave(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	result := run(t, dir, "blog", "post", "generate", "--no-from-release", "--version", "1.0.0")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "--from-release") {
		t.Errorf("the refusal does not name the flag: %s", result.Stderr)
	}
}

func TestPostGenerateRefusesAnEmptyVersion(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	if result := run(t, dir, "blog", "post", "generate", "--from-release", "--version", ""); result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
}

func TestPostGenerateRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "blog", "post", "generate", "--from-release", "--version", "1.0.0")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
}

// sameJSON compares two decoded JSON values.
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("encoding %v: %v", a, err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("encoding %v: %v", b, err)
	}
	return string(left) == string(right)
}
