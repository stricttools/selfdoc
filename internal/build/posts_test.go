package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// The posts the fixtures publish. Each is a full Markdown source, frontmatter
// fences included, as it would be saved under the posts directory.
const (
	postHello = "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"tags = [\"release\"]\ndraft = false\ndirectives = false\n+++\nThis is the post content.\n"
	postSecond = "+++\ntitle = \"Second Post\"\ndate = 2024-01-20\nslug = \"second-post\"\n" +
		"tags = []\ndraft = false\ndirectives = false\n+++\nSecond post body.\n"
	postDraft = "+++\ntitle = \"Draft Post\"\ndate = 2024-01-16\nslug = \"draft-post\"\n" +
		"tags = []\ndraft = true\ndirectives = false\n+++\nDraft content here.\n"
	postWithDirective = "+++\ntitle = \"Directive Post\"\ndate = 2024-02-01\n" +
		"slug = \"directive-post\"\ntags = []\ndraft = false\ndirectives = true\n+++\n" +
		"# Directive Post\n\n:-: ref path=\"mymod\"\n"
)

// postFiles maps a posts-directory filename to the project-relative path a
// fixture writes it at.
func postFiles(postsByName map[string]string) map[string]string {
	files := map[string]string{}
	for name, content := range postsByName {
		files[".stricttools/posts/"+name] = content
	}
	return files
}

func TestInjectPostsIntoDocs(t *testing.T) {
	hygiene.Isolate(t)

	tests := []struct {
		name          string
		postsByName   map[string]string
		includeDrafts bool
		wantFiles     []string
		absentFiles   []string
	}{
		{
			name: "a project with no posts directory injects nothing",
		},
		{
			name:        "a post and its listing are injected",
			postsByName: map[string]string{"hello.md": postHello},
			wantFiles:   []string{"blog/hello-world.md", "blog.md"},
		},
		{
			name: "a draft is left out unless asked for",
			postsByName: map[string]string{
				"hello.md": postHello, "draft.md": postDraft,
			},
			wantFiles:   []string{"blog/hello-world.md", "blog.md"},
			absentFiles: []string{"blog/draft-post.md"},
		},
		{
			name: "a draft is injected when asked for",
			postsByName: map[string]string{
				"hello.md": postHello, "draft.md": postDraft,
			},
			includeDrafts: true,
			wantFiles: []string{
				"blog/hello-world.md", "blog/draft-post.md", "blog.md",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
			for relPath, content := range postFiles(test.postsByName) {
				testproject.WriteText(t, filepath.Join(dir, filepath.FromSlash(relPath)), content)
			}
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatalf("loading the fixture config: %v", err)
			}
			docsDir := filepath.Join(dir, ".stricttools", "docs")
			injected, err := InjectPostsIntoDocs(dir, cfg, docsDir, test.includeDrafts, effects.Unbound())
			if err != nil {
				t.Fatalf("InjectPostsIntoDocs: %v", err)
			}
			if len(injected) != len(test.wantFiles) {
				t.Errorf("the injection wrote %v, want %v", injected, test.wantFiles)
			}
			for _, relPath := range test.wantFiles {
				path := filepath.Join(docsDir, filepath.FromSlash(relPath))
				if !isFile(path) {
					t.Errorf("%s was not injected", relPath)
				}
				if !contains(injected, path) {
					t.Errorf("%s is not among the paths the injection reported", relPath)
				}
			}
			for _, relPath := range test.absentFiles {
				if isFile(filepath.Join(docsDir, filepath.FromSlash(relPath))) {
					t.Errorf("%s was injected", relPath)
				}
			}
			if len(test.wantFiles) > 0 {
				content := readFile(t, filepath.Join(docsDir, "blog", "hello-world.md"))
				assertCarries(t, "the injected post", content,
					"Hello World", "This is the post content.")
			}
		})
	}
}

func TestCleanupInjectedPosts(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("the files and the emptied directory go", func(t *testing.T) {
		docsDir := filepath.Join(t.TempDir(), ".stricttools", "docs")
		postsSubdir := filepath.Join(docsDir, "blog")
		testproject.MkdirAll(t, postsSubdir)
		fileA := filepath.Join(postsSubdir, "a.md")
		fileB := filepath.Join(postsSubdir, "b.md")
		for _, path := range []string{fileA, fileB} {
			testproject.WriteText(t, path, "placeholder")
		}
		if err := CleanupInjectedPosts([]string{fileA, fileB}, docsDir, effects.Unbound()); err != nil {
			t.Fatalf("CleanupInjectedPosts: %v", err)
		}
		if isFile(fileA) || isFile(fileB) {
			t.Error("an injected file was left behind")
		}
		if isDir(postsSubdir) {
			t.Error("the emptied blog directory was left behind")
		}
	})

	t.Run("a directory that still holds a file stays", func(t *testing.T) {
		docsDir := filepath.Join(t.TempDir(), ".stricttools", "docs")
		postsSubdir := filepath.Join(docsDir, "blog")
		testproject.MkdirAll(t, postsSubdir)
		injected := filepath.Join(postsSubdir, "injected.md")
		other := filepath.Join(postsSubdir, "other.md")
		for _, path := range []string{injected, other} {
			testproject.WriteText(t, path, "placeholder")
		}
		if err := CleanupInjectedPosts([]string{injected}, docsDir, effects.Unbound()); err != nil {
			t.Fatalf("CleanupInjectedPosts: %v", err)
		}
		if isFile(injected) {
			t.Error("the injected file was left behind")
		}
		if !isFile(other) {
			t.Error("a file the injection did not write was deleted")
		}
		if !isDir(postsSubdir) {
			t.Error("a directory that still holds a file was removed")
		}
	})
}

// mustRenderListing renders the post listing page or fails the test.
func mustRenderListing(t *testing.T, published []posts.Post) string {
	t.Helper()
	listing, err := RenderPostListing(published)
	if err != nil {
		t.Fatalf("RenderPostListing: %v", err)
	}
	return listing
}

func TestRenderPostListing(t *testing.T) {
	t.Parallel()

	t.Run("one post renders with its date, title and address", func(t *testing.T) {
		listing := mustRenderListing(t, []posts.Post{
			{Date: "2024-06-15", Title: "Hello World", Slug: "hello-world"},
		})
		// The listing is emitted at blog/, so a post is a sibling of it.
		assertCarries(t, "the listing", listing,
			"**2024-06-15**", "[Hello World]", "(hello-world/)")
	})

	t.Run("several posts keep the order they came in", func(t *testing.T) {
		listing := mustRenderListing(t, []posts.Post{
			{Date: "2024-06-16", Title: "Second Post", Slug: "second-post"},
			{Date: "2024-06-15", Title: "First Post", Slug: "first-post"},
		})
		var items []string
		for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
			if strings.HasPrefix(line, "- ") {
				items = append(items, line)
			}
		}
		if len(items) != 2 {
			t.Fatalf("the listing carries %d items, want 2:\n%s", len(items), listing)
		}
		if !strings.Contains(items[0], "Second Post") || !strings.Contains(items[1], "First Post") {
			t.Errorf("the items are not in the order they were given:\n%s", listing)
		}
	})

	t.Run("no posts is a page that says so", func(t *testing.T) {
		assertCarries(t, "the listing", mustRenderListing(t, nil), "No posts yet.")
	})

	t.Run("the listing declares itself unversioned", func(t *testing.T) {
		listing := mustRenderListing(t, []posts.Post{
			{Date: "2024-06-15", Title: "Hello", Slug: "hello"},
		})
		if !strings.HasPrefix(listing, "+++\n") {
			t.Fatalf("the listing does not open with a frontmatter fence:\n%s", listing)
		}
		parts := strings.SplitN(listing, "+++", 3)
		assertCarries(t, "the listing's frontmatter", parts[1],
			"versioned = false", "type = \"post-listing\"")
	})
}

func TestBuildWithPosts(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a post is emitted at the site level", func(t *testing.T) {
		built := buildFixture(t, fixture{Files: postFiles(map[string]string{"hello.md": postHello})})
		if !built.reported("blog/hello-world/index.html") {
			t.Fatal("the post page was not reported written")
		}
		assertCarries(t, "blog/hello-world/index.html",
			built.page(t, "blog/hello-world/index.html"), "Hello World")
	})

	t.Run("a draft is left out by default and included when asked for", func(t *testing.T) {
		files := postFiles(map[string]string{"hello.md": postHello, "draft.md": postDraft})
		built := buildFixture(t, fixture{Files: files})
		if built.reported("blog/draft-post/index.html") {
			t.Error("a draft was published")
		}
		if !built.reported("blog/hello-world/index.html") {
			t.Error("the published post is missing")
		}

		withDrafts := buildFixture(t, fixture{
			Files: files,
			Build: func(o *Options) { o.IncludeDrafts = true },
		})
		if !withDrafts.reported("blog/draft-post/index.html") {
			t.Error("a draft was left out of a build that asked for drafts")
		}
	})

	t.Run("the injected pages are cleaned out of the docs tree", func(t *testing.T) {
		built := buildFixture(t, fixture{Files: postFiles(map[string]string{"hello.md": postHello})})
		if isDir(filepath.Join(built.dir, ".stricttools", "docs", "blog")) {
			t.Error("the injected blog directory was left in the docs tree")
		}
		if isFile(filepath.Join(built.dir, ".stricttools", "docs", "blog.md")) {
			t.Error("the injected listing page was left in the docs tree")
		}
	})

	t.Run("the listing page is emitted at the posts prefix", func(t *testing.T) {
		built := buildFixture(t, fixture{Files: postFiles(map[string]string{
			"alpha.md": "+++\ntitle = \"Alpha Post\"\ndate = 2024-06-10\nslug = \"alpha-post\"\n" +
				"directives = false\ntags = []\ndraft = false\n+++\n# Alpha Post\n\nAlpha content.\n",
			"beta.md": "+++\ntitle = \"Beta Post\"\ndate = 2024-06-11\nslug = \"beta-post\"\n" +
				"directives = false\ntags = []\ndraft = false\n+++\n# Beta Post\n\nBeta content.\n",
		})})
		if !built.reported("blog/index.html") {
			t.Fatal("the listing page was not reported written")
		}
		listing := built.page(t, "blog/index.html")
		assertCarries(t, "blog/index.html", listing,
			"Alpha Post", "Beta Post", "alpha-post", "beta-post")
	})
}

func TestBuildTargetPosts(t *testing.T) {
	hygiene.Isolate(t)

	postsOnly := func(o *Options) { o.Target = "posts" }

	t.Run("the post pages are built and nothing else is", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: postFiles(map[string]string{"hello.md": postHello}),
			Build: postsOnly,
		})
		if !built.reported("blog/hello-world/index.html") {
			t.Fatal("the post page was not reported written")
		}
		assertCarries(t, "blog/hello-world/index.html",
			built.page(t, "blog/hello-world/index.html"), "Hello World")
		for _, outputKey := range []string{
			"sitemap.xml", "feed.xml", "style.css", "index.html", "search-index.json",
		} {
			if built.exists(outputKey) {
				t.Errorf("%s was written by a posts-only build", outputKey)
			}
		}
	})

	t.Run("a project with no posts writes nothing", func(t *testing.T) {
		built := buildFixture(t, fixture{Build: postsOnly})
		if len(built.written) != 0 {
			t.Errorf("a posts-only build of a project with no posts wrote %v", built.written)
		}
	})

	t.Run("a post's directives are still resolved", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: postFiles(map[string]string{"directive.md": postWithDirective}),
			Build: postsOnly,
		})
		if !built.reported("blog/directive-post/index.html") {
			t.Fatal("the post page was not reported written")
		}
		// Whether the directive resolved to content or to an error
		// message, the raw marker never reaches the page.
		assertLacks(t, "blog/directive-post/index.html",
			built.page(t, "blog/directive-post/index.html"), ":-:")
	})

	t.Run("every post is built", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: postFiles(map[string]string{"hello.md": postHello, "second.md": postSecond}),
			Build: postsOnly,
		})
		for _, outputKey := range []string{
			"blog/hello-world/index.html", "blog/second-post/index.html",
		} {
			if !built.reported(outputKey) {
				t.Errorf("%s was not reported written", outputKey)
			}
		}
	})

	t.Run("the injected pages are cleaned up", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: postFiles(map[string]string{"hello.md": postHello}),
			Build: postsOnly,
		})
		blogDir := filepath.Join(built.dir, ".stricttools", "docs", "blog")
		if isDir(blogDir) {
			entries, err := os.ReadDir(blogDir)
			if err != nil {
				t.Fatalf("reading the injected directory: %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("the injected directory still holds %d files", len(entries))
			}
		}
	})

	t.Run("the post manifest records every post", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: postFiles(map[string]string{"hello.md": postHello, "second.md": postSecond}),
			Build: postsOnly,
		})
		raw := readFile(t, filepath.Join(built.dir, ".stricttools", "docs-state", "post-manifest.json"))
		var manifest struct {
			SchemaVersion int              `json:"schema_version"`
			Pages         []map[string]any `json:"pages"`
			Posts         []map[string]any `json:"posts"`
		}
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
			t.Fatalf("decoding the post manifest: %v", err)
		}
		if manifest.SchemaVersion != 1 {
			t.Errorf("the manifest declares schema version %d, want 1", manifest.SchemaVersion)
		}
		if len(manifest.Pages) != 0 {
			t.Errorf("a posts-only manifest records %d pages, want none", len(manifest.Pages))
		}
		if len(manifest.Posts) != 2 {
			t.Fatalf("the manifest records %d posts, want 2", len(manifest.Posts))
		}
		slugs := map[string]bool{}
		for _, post := range manifest.Posts {
			for _, field := range []string{"path", "title", "date", "slug", "tags"} {
				if _, carried := post[field]; !carried {
					t.Errorf("a recorded post carries no %q", field)
				}
			}
			slugs[post["slug"].(string)] = true
		}
		for _, slug := range []string{"hello-world", "second-post"} {
			if !slugs[slug] {
				t.Errorf("the manifest does not record %q", slug)
			}
		}
	})
}

func TestCheckPostSlugUniqueness(t *testing.T) {
	t.Parallel()

	t.Run("distinct slugs are accepted", func(t *testing.T) {
		if err := CheckPostSlugUniqueness([]SlugClaim{
			{Slug: "a", Source: "one"}, {Slug: "b", Source: "two"},
		}); err != nil {
			t.Errorf("distinct slugs were refused: %v", err)
		}
	})

	t.Run("a repeat names both claimants", func(t *testing.T) {
		err := CheckPostSlugUniqueness([]SlugClaim{
			{Slug: "a", Source: "one"}, {Slug: "a", Source: "two"},
		})
		if err == nil {
			t.Fatal("two posts claiming one address were accepted")
		}
		assertCarries(t, "the refusal", err.Error(), "'a'", "'one'", "'two'")
	})
}
