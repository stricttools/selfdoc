package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// The posts the fixtures publish, each a full Markdown source as it would be
// saved under the posts directory.
const (
	postHelloName = "hello.md"
	postHello     = "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"tags = [\"release\"]\ndraft = false\ndirectives = false\n+++\n" +
		"# Hello World\n\nThis is the post content.\n\n" +
		"## Setup\n\nFirst.\n\n## Setup\n\nSecond.\n"
	postSecondName = "second.md"
	postSecond     = "+++\ntitle = \"Second Post\"\ndate = 2024-02-01\nslug = \"second-post\"\n" +
		"tags = []\ndraft = false\ndirectives = false\n+++\nSecond post body.\n"
	postDraftName = "draft.md"
	postDraft     = "+++\ntitle = \"Draft Post\"\ndate = 2024-03-01\nslug = \"draft-post\"\n" +
		"tags = []\ndraft = true\ndirectives = false\n+++\nUnfinished.\n"
)

// makeProject writes a minimal project carrying the named posts and returns
// its root.
func makeProject(t *testing.T, posts map[string]string) string {
	t.Helper()
	dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
	testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"),
		"# Test Project\n\nWelcome.\n")
	for name, content := range posts {
		testproject.WriteText(t, filepath.Join(dir, ".stricttools", "posts", name), content)
	}
	return dir
}

// treeEntry is one file's identity for the fingerprint: its path, its size,
// its modification time and the digest of its bytes.
type treeEntry struct {
	rel     string
	size    int64
	modTime int64
	digest  string
}

// fingerprint digests every file under root.
//
// The modification time is included on purpose: an atomic rewrite with
// identical bytes is still a write, and this notices it.
func fingerprint(t *testing.T, root string) (string, []treeEntry) {
	t.Helper()
	var entries []treeEntry
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(content)
		entries = append(entries, treeEntry{
			rel:     filepath.ToSlash(rel),
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
			digest:  hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	digest := sha256.New()
	for _, entry := range entries {
		fmt.Fprintf(digest, "%s|%d|%d|%s\n", entry.rel, entry.size, entry.modTime, entry.digest)
	}
	return hex.EncodeToString(digest.Sum(nil)), entries
}

// assertUnchanged fails the test when the tree moved, naming what differs.
func assertUnchanged(t *testing.T, root, before string, beforeEntries []treeEntry) {
	t.Helper()
	after, afterEntries := fingerprint(t, root)
	if after == before {
		return
	}
	seen := map[treeEntry]bool{}
	for _, entry := range beforeEntries {
		seen[entry] = true
	}
	var differ []string
	for _, entry := range afterEntries {
		if !seen[entry] {
			differ = append(differ, entry.rel)
		}
	}
	t.Errorf("the render mutated the working tree: %v", differ)
}

// builtPost reads the file a posts-target build wrote for one slug.
func builtPost(t *testing.T, dir, slug string) string {
	t.Helper()
	path := filepath.Join(dir, ".stricttools", "docs-cache", "build", "blog", slug, "index.html")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

func TestRenderedPostMatchesTheBuiltPage(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	dir := makeProject(t, map[string]string{
		postHelloName: postHello, postSecondName: postSecond,
	})
	runPostsBuild(t, dir, false)

	t.Run("the rendered buffer is the built file", func(t *testing.T) {
		rendered, err := Post(PostOptions{
			DirPath: dir, SourcePath: postHelloName, Content: postHello,
		}, effects.Unbound())
		if err != nil {
			t.Fatalf("Post: %v", err)
		}
		if rendered != builtPost(t, dir, "hello-world") {
			t.Error("the rendered page differs from the one the build wrote")
		}
	})

	t.Run("every post renders the same way", func(t *testing.T) {
		for name, slug := range map[string]string{
			postHelloName: "hello-world", postSecondName: "second-post",
		} {
			source := postHello
			if name == postSecondName {
				source = postSecond
			}
			rendered, err := Post(PostOptions{
				DirPath: dir, SourcePath: name, Content: source,
			}, effects.Unbound())
			if err != nil {
				t.Fatalf("Post(%s): %v", name, err)
			}
			if rendered != builtPost(t, dir, slug) {
				t.Errorf("the rendered %s differs from the one the build wrote", name)
			}
		}
	})

	t.Run("duplicate heading anchors are deduplicated", func(t *testing.T) {
		rendered, err := Post(PostOptions{
			DirPath: dir, SourcePath: postHelloName, Content: postHello,
		}, effects.Unbound())
		if err != nil {
			t.Fatalf("Post: %v", err)
		}
		for _, want := range []string{`id="setup"`, `id="setup-1"`} {
			if !strings.Contains(rendered, want) {
				t.Errorf("the rendered page does not carry %q", want)
			}
		}
	})
}

func TestRenderWritesNothing(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("after a build, the tree does not move", func(t *testing.T) {
		dir := makeProject(t, map[string]string{
			postHelloName: postHello, postSecondName: postSecond,
		})
		runPostsBuild(t, dir, false)

		before, beforeEntries := fingerprint(t, dir)
		if _, err := Post(PostOptions{
			DirPath: dir, SourcePath: postHelloName, Content: postHello,
		}, effects.Unbound()); err != nil {
			t.Fatalf("Post: %v", err)
		}
		assertUnchanged(t, dir, before, beforeEntries)
	})

	t.Run("with no prior build, not even the baselines are created", func(t *testing.T) {
		dir := makeProject(t, map[string]string{postHelloName: postHello})

		before, beforeEntries := fingerprint(t, dir)
		if _, err := Post(PostOptions{
			DirPath: dir, SourcePath: postHelloName, Content: postHello,
		}, effects.Unbound()); err != nil {
			t.Fatalf("Post: %v", err)
		}
		assertUnchanged(t, dir, before, beforeEntries)
		for _, absent := range []string{
			filepath.Join(dir, ".stricttools", "docs", "blog"),
			filepath.Join(dir, ".stricttools", "docs-state", "hashes"),
		} {
			if info, err := os.Stat(absent); err == nil && info.IsDir() {
				t.Errorf("%s was created", absent)
			}
		}
	})

	t.Run("the edited buffer is not written back", func(t *testing.T) {
		dir := makeProject(t, map[string]string{postHelloName: postHello})
		edited := strings.Replace(postHello,
			"This is the post content.", "Edited in the buffer only.", 1)

		before, beforeEntries := fingerprint(t, dir)
		rendered, err := Post(PostOptions{
			DirPath: dir, SourcePath: postHelloName, Content: edited,
		}, effects.Unbound())
		if err != nil {
			t.Fatalf("Post: %v", err)
		}
		if !strings.Contains(rendered, "Edited in the buffer only.") {
			t.Error("the rendered page does not carry the edited text")
		}
		assertUnchanged(t, dir, before, beforeEntries)
	})
}

func TestBuildSingleWriteContract(t *testing.T) {
	hygiene.Isolate(t)

	// The single-pass build writes the staleness baselines, and nothing
	// else. Both directions are pinned here so the claim stays honest.
	t.Run("the baselines are advanced by default", func(t *testing.T) {
		dir := makeProject(t, nil)
		hashes := filepath.Join(dir, ".stricttools", "docs-state", "hashes")
		if info, err := os.Stat(hashes); err == nil && info.IsDir() {
			t.Fatal("the fixture already carries a hash store")
		}
		empty := ""
		opts := build.NewSingleOptions()
		opts.DirPath = dir
		opts.MountLocale = &empty
		opts.MountVersion = &empty
		opts.VersionOverride = &empty
		if _, err := build.BuildSingle(opts, effects.Unbound()); err != nil {
			t.Fatalf("BuildSingle: %v", err)
		}
		if _, err := os.Stat(filepath.Join(hashes, "hashes.json")); err != nil {
			t.Errorf("the hash store was not written: %v", err)
		}
	})

	t.Run("with baseline writing off, nothing is written at all", func(t *testing.T) {
		dir := makeProject(t, nil)
		before, beforeEntries := fingerprint(t, dir)
		empty := ""
		opts := build.NewSingleOptions()
		opts.DirPath = dir
		opts.MountLocale = &empty
		opts.MountVersion = &empty
		opts.VersionOverride = &empty
		opts.WriteBaselines = false
		if _, err := build.BuildSingle(opts, effects.Unbound()); err != nil {
			t.Fatalf("BuildSingle: %v", err)
		}
		assertUnchanged(t, dir, before, beforeEntries)
	})
}

func TestRenderUnsavedAndDraftPosts(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a post with no file behind it renders", func(t *testing.T) {
		dir := makeProject(t, map[string]string{postHelloName: postHello})
		source := "+++\ntitle = \"Brand New\"\ndate = 2024-04-01\nslug = \"brand-new\"\n" +
			"tags = []\ndraft = false\ndirectives = false\n+++\nNever saved.\n"

		before, beforeEntries := fingerprint(t, dir)
		rendered, err := Post(PostOptions{
			DirPath: dir, SourcePath: "brand-new.md", Content: source,
		}, effects.Unbound())
		if err != nil {
			t.Fatalf("Post: %v", err)
		}
		if !strings.Contains(rendered, "Never saved.") {
			t.Error("the rendered page does not carry the unsaved post's body")
		}
		assertUnchanged(t, dir, before, beforeEntries)
	})

	t.Run("a draft is refused unless asked for", func(t *testing.T) {
		dir := makeProject(t, map[string]string{postDraftName: postDraft})
		_, err := Post(PostOptions{
			DirPath: dir, SourcePath: postDraftName, Content: postDraft,
		}, effects.Unbound())
		if err == nil {
			t.Fatal("a draft rendered without being asked for")
		}
		if !strings.Contains(err.Error(), "draft") {
			t.Errorf("the refusal is %q, want one naming the draft", err)
		}
	})

	t.Run("a draft renders when asked for", func(t *testing.T) {
		dir := makeProject(t, map[string]string{postDraftName: postDraft})
		rendered, err := Post(PostOptions{
			DirPath: dir, SourcePath: postDraftName, Content: postDraft,
			IncludeDrafts: true,
		}, effects.Unbound())
		if err != nil {
			t.Fatalf("Post: %v", err)
		}
		if !strings.Contains(rendered, "Unfinished.") {
			t.Error("the rendered draft does not carry its body")
		}
	})

	t.Run("a draft render matches a drafts build", func(t *testing.T) {
		dir := makeProject(t, map[string]string{postDraftName: postDraft})
		runPostsBuild(t, dir, true)
		rendered, err := Post(PostOptions{
			DirPath: dir, SourcePath: postDraftName, Content: postDraft,
			IncludeDrafts: true,
		}, effects.Unbound())
		if err != nil {
			t.Fatalf("Post: %v", err)
		}
		if rendered != builtPost(t, dir, "draft-post") {
			t.Error("the rendered draft differs from the one the build wrote")
		}
	})
}

func TestRenderRefusesADirectoryThatIsNotAProject(t *testing.T) {
	hygiene.Isolate(t)

	_, err := Post(PostOptions{
		DirPath: t.TempDir(), SourcePath: postHelloName, Content: postHello,
	}, effects.Unbound())
	if err == nil {
		t.Fatal("a directory with no selfdoc.json rendered anyway")
	}
	if !strings.Contains(err.Error(), "No selfdoc.json found") {
		t.Errorf("the refusal is %q, want one naming the missing config", err)
	}
}

// runPostsBuild builds only the post pages of a project.
func runPostsBuild(t *testing.T, dir string, includeDrafts bool) {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	if _, err := build.Build(build.Options{
		DirPath:       dir,
		Config:        cfg,
		Target:        "posts",
		IncludeDrafts: includeDrafts,
		Stdout:        &discard{},
	}, effects.Unbound()); err != nil {
		t.Fatalf("the posts-only build: %v", err)
	}
}

// discard swallows a build's progress lines.
type discard struct{}

// Write reports every byte written and keeps none.
func (discard) Write(p []byte) (int, error) { return len(p), nil }
