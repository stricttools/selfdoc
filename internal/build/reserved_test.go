package build

import (
	"path/filepath"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

func TestCheckReservedPagePaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		paths    []string
		wantStem string
	}{
		{
			name:     "a page named after the archive prefix",
			paths:    []string{"v.md"},
			wantStem: "'v'",
		},
		{
			name:     "a page under the archive prefix",
			paths:    []string{"v/notes.md"},
			wantStem: "'v'",
		},
		{
			name:     "a page named after the posts prefix",
			paths:    []string{"blog.md"},
			wantStem: "'blog'",
		},
		{
			name:  "ordinary pages, a version-shaped directory included",
			paths: []string{"about.md", "help/faq.md", "1.0.0/guide.md", "versions.md"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := map[string]bool{}
			for _, path := range test.paths {
				set[path] = true
			}
			err := CheckReservedPagePaths(set)
			if test.wantStem == "" {
				if err != nil {
					t.Errorf("ordinary pages were refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("a page on the reserved path %s was accepted", test.wantStem)
			}
			assertCarries(t, "the refusal", err.Error(),
				"reserved top-level path "+test.wantStem)
		})
	}
}

// The authored page an author committed, and the post a build would inject
// beside it.
const (
	authoredBlogPage = "+++\ntitle = \"My Blog\"\n+++\n\n# My Blog\n\nHand-written.\n"
	reservedPost     = "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"directives = false\ntags = []\ndraft = false\n+++\nPost body.\n"
)

func TestBuildRefusesAnAuthoredReservedPage(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	tests := []struct {
		name        string
		docs        map[string]string
		posts       map[string]string
		wantInError string
		// survives names the authored file that must still be on disk
		// afterwards, "" when the case writes none.
		survives string
	}{
		{
			name:        "an authored blog page, with posts to inject over it",
			docs:        map[string]string{"blog.md": authoredBlogPage},
			posts:       map[string]string{"hello.md": reservedPost},
			wantInError: "blog.md",
			survives:    ".stricttools/docs/blog.md",
		},
		{
			name: "an authored blog page, with no posts at all",
			// "blog" is the build's segment whether or not this build
			// injects anything into it, so an authored page there is
			// refused either way -- otherwise adding the first post would
			// silently destroy the page.
			docs:        map[string]string{"blog.md": authoredBlogPage},
			wantInError: "blog.md",
			survives:    ".stricttools/docs/blog.md",
		},
		{
			name: "an authored page under the blog directory",
			docs: map[string]string{
				"blog/notes.md": "# Notes\n\nHand-written.\n",
			},
			posts:       map[string]string{"hello.md": reservedPost},
			wantInError: "blog/notes.md",
			survives:    ".stricttools/docs/blog/notes.md",
		},
		{
			name: "an authored page under the archive prefix",
			docs: map[string]string{
				"v/old.md": "# Old\n\nHand-written.\n",
			},
			wantInError: "v/old.md",
			survives:    ".stricttools/docs/v/old.md",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			built, err := tryBuildFixture(t, fixture{
				Docs:  test.docs,
				Files: postFiles(test.posts),
			})
			if err == nil {
				t.Fatal("a build wrote over a page on a reserved path")
			}
			assertCarries(t, "the refusal", err.Error(), test.wantInError)
			if test.survives != "" {
				path := filepath.Join(built.dir, filepath.FromSlash(test.survives))
				if !isFile(path) {
					t.Errorf("%s was deleted by the build it stopped", test.survives)
				}
			}
		})
	}

	t.Run("a project with posts and no authored reserved page is unaffected", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: postFiles(map[string]string{"hello.md": reservedPost}),
		})
		if !built.reported("blog/hello-world/index.html") {
			t.Error("the post page was not written")
		}
		if isFile(filepath.Join(built.dir, ".stricttools", "docs", "blog.md")) {
			t.Error("the injected listing page was left in the docs tree")
		}
		if isDir(filepath.Join(built.dir, ".stricttools", "docs", "blog")) {
			t.Error("the injected blog directory was left in the docs tree")
		}
	})
}

func TestCheckReservedAuthoredPages(t *testing.T) {
	t.Parallel()

	t.Run("a clean docs tree passes", func(t *testing.T) {
		dir := t.TempDir()
		testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", "index.md"), "# Home\n")
		if err := CheckReservedAuthoredPages(filepath.Join(dir, ".stricttools", "docs"), dir); err != nil {
			t.Errorf("a clean docs tree was refused: %v", err)
		}
	})

	t.Run("the reported path is relative to the base directory", func(t *testing.T) {
		dir := t.TempDir()
		testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", "blog.md"), authoredBlogPage)
		err := CheckReservedAuthoredPages(filepath.Join(dir, ".stricttools", "docs"), dir)
		if err == nil {
			t.Fatal("an authored page on a reserved path was accepted")
		}
		assertCarries(t, "the refusal", err.Error(), "'.stricttools/docs/blog.md'")
	})
}

func TestIsSiteLevelPage(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"blog.md":        true,
		"blog/hello.md":  true,
		"index.md":       false,
		"guide/intro.md": false,
		"blogging.md":    false,
	}
	for mdPath, want := range tests {
		if got := IsSiteLevelPage(mdPath); got != want {
			t.Errorf("IsSiteLevelPage(%q) = %v, want %v", mdPath, got, want)
		}
	}
}
