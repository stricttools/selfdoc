package unified

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// post renders a post's Markdown source, frontmatter fences included, as it
// would be saved under a project's posts directory.
func post(title, date, slug string, draft bool) string {
	drafted := "false"
	if draft {
		drafted = "true"
	}
	return "+++\ntitle = \"" + title + "\"\ndate = " + date + "\nslug = \"" + slug +
		"\"\ntags = []\ndraft = " + drafted + "\ndirectives = false\n+++\n" +
		"Body of " + title + ".\n"
}

// writePost saves a post under a project's default posts directory.
func writePost(t *testing.T, projectDir, name, source string) {
	t.Helper()
	testproject.WriteText(t, filepath.Join(projectDir, "stricttools", "posts", name), source)
}

// projectDirOf is a constituent's directory inside a unified fixture.
func projectDirOf(docsSiteDir, slug string) string {
	return filepath.Join(filepath.Dir(docsSiteDir), slug)
}

// assertNoInjectedPosts fails the test when a docs tree still carries the
// pages post injection writes into it.
func assertNoInjectedPosts(t *testing.T, docsDir string) {
	t.Helper()
	if isFile(filepath.Join(docsDir, "blog.md")) {
		t.Errorf("%s still carries the injected listing page", docsDir)
	}
	entries, err := os.ReadDir(filepath.Join(docsDir, "blog"))
	if err == nil && len(entries) > 0 {
		t.Errorf("%s still carries %d injected post page(s)", docsDir, len(entries))
	}
}

func TestBuildUnifiedEmitsEveryProjectsPostsIntoTheSharedTree(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	docsSite := testproject.MakeUnified(t, twoProjects, nil)
	writePost(t, projectDirOf(docsSite, "core"), "core-post.md",
		post("Core Post", "2024-01-15", "core-post", false))
	writePost(t, projectDirOf(docsSite, "cli"), "cli-post.md",
		post("Cli Post", "2024-02-20", "cli-post", false))
	writePost(t, docsSite, "site-post.md",
		post("Site Post", "2024-03-25", "site-post", false))

	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	output := filepath.Join(docsSite, "stricttools", ".docs-cache", "build")

	// A post has no project segment whichever project wrote it: every one
	// of them is served from the site-level tree at "blog/<slug>/".
	for _, slug := range []string{"core-post", "cli-post", "site-post"} {
		requireFile(t, filepath.Join(output, "blog", slug, "index.html"))
	}
}

func TestBuildUnifiedCleansUpTheInjectedPostsOfEveryProject(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	docsSite := testproject.MakeUnified(t, twoProjects, nil)
	coreDir := projectDirOf(docsSite, "core")
	writePost(t, coreDir, "core-post.md", post("Core Post", "2024-01-15", "core-post", false))
	writePost(t, docsSite, "site-post.md", post("Site Post", "2024-03-25", "site-post", false))

	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}

	// The pages injection wrote are removed from every docs tree it wrote
	// into, so a build never leaves a generated page behind in the source.
	assertNoInjectedPosts(t, filepath.Join(coreDir, "stricttools", "docs"))
	assertNoInjectedPosts(t, filepath.Join(docsSite, "stricttools", "docs"))
}

func TestBuildUnifiedCleansUpTheInjectedPostsWhenTheBuildFails(t *testing.T) {
	hygiene.Isolate(t)

	// Two projects claiming one post slug is refused inside the build body,
	// after every project's posts have already been injected -- which is
	// what makes this the case that proves the cleanup is unconditional.
	docsSite := testproject.MakeUnified(t, twoProjects, nil)
	coreDir := projectDirOf(docsSite, "core")
	cliDir := projectDirOf(docsSite, "cli")
	writePost(t, coreDir, "shared.md", post("Shared", "2024-01-15", "shared", false))
	writePost(t, cliDir, "shared.md", post("Shared", "2024-02-15", "shared", false))

	_, err := BuildUnified(docsSite, nil, "", false, effects.Unbound())
	if err == nil {
		t.Fatal("two projects claiming one post slug was accepted")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Errorf("err = %q, want it to name the repeated slug", err)
	}

	assertNoInjectedPosts(t, filepath.Join(coreDir, "stricttools", "docs"))
	assertNoInjectedPosts(t, filepath.Join(cliDir, "stricttools", "docs"))
	assertNoInjectedPosts(t, filepath.Join(docsSite, "stricttools", "docs"))
}

func TestBuildUnifiedCleansUpTheInjectedPostsWhenSetupFails(t *testing.T) {
	hygiene.Isolate(t)

	// The second constituent's config is unreadable, so the setup loop
	// fails after the first constituent's posts were already injected --
	// before the build body, which the other cleanup path wraps, ever runs.
	docsSite := testproject.MakeUnified(t, twoProjects, nil)
	coreDir := projectDirOf(docsSite, "core")
	cliDir := projectDirOf(docsSite, "cli")
	writePost(t, coreDir, "core-post.md", post("Core Post", "2024-01-15", "core-post", false))
	testproject.WriteText(t, filepath.Join(cliDir, "selfdoc.json"), "{ not json\n")

	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err == nil {
		t.Fatal("an unreadable constituent config was accepted")
	}

	assertNoInjectedPosts(t, filepath.Join(coreDir, "stricttools", "docs"))
}

func TestBuildUnifiedLeavesADocsTreeAloneWhenThereAreNoPosts(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	docsSite, output := buildUnifiedFixture(t, twoProjects, nil)

	assertNoInjectedPosts(t, filepath.Join(projectDirOf(docsSite, "core"), "stricttools", "docs"))
	assertNoInjectedPosts(t, filepath.Join(docsSite, "stricttools", "docs"))
	if isDir(filepath.Join(output, "blog")) {
		t.Error("a site with no posts still got a site-level blog tree")
	}
}

func TestBuildUnifiedDraftsAreWithheldUnlessAskedFor(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	for _, testCase := range []struct {
		name          string
		includeDrafts bool
		wantDraft     bool
	}{
		{"without drafts", false, false},
		{"with drafts", true, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			docsSite := testproject.MakeUnified(t, oneProject, nil)
			coreDir := projectDirOf(docsSite, "core")
			writePost(t, coreDir, "live.md", post("Live", "2024-01-15", "live", false))
			writePost(t, coreDir, "hidden.md", post("Hidden", "2024-01-16", "hidden", true))

			if _, err := BuildUnified(
				docsSite, nil, "", testCase.includeDrafts, effects.Unbound()); err != nil {
				t.Fatalf("BuildUnified: %v", err)
			}
			output := filepath.Join(docsSite, "stricttools", ".docs-cache", "build")
			requireFile(t, filepath.Join(output, "blog", "live", "index.html"))

			draftPage := filepath.Join(output, "blog", "hidden", "index.html")
			if got := isFile(draftPage); got != testCase.wantDraft {
				t.Errorf("the draft page exists = %v, want %v", got, testCase.wantDraft)
			}
		})
	}
}
