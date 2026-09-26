package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// `selfdoc build --target`.
//
// One binary carries every build, so the four targets are dispatched here
// rather than split across two commands that refused each other's work.

func TestBuildTargetSiteIsTheAbsentDefault(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	bare := run(t, dir, "build", "--no-auto-commit")
	stated := run(t, dir, "build", "--target", "site", "--no-auto-commit")
	if bare.ExitCode != stated.ExitCode {
		t.Errorf("the bare build exits %d and --target site exits %d",
			bare.ExitCode, stated.ExitCode)
	}
	if !strings.Contains(stated.Stdout, "Built") {
		t.Errorf("--target site did not build:\n%s", stated.Stdout)
	}
}

func TestBuildTargetPosts(t *testing.T) {
	isolate(t)
	dir := postProject(t, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
	})
	writeText(t, filepath.Join(dir, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \"The landing page of a project whose posts are built alone.\"\n+++\n\n# Home\n")
	writePost(t, filepath.Join(dir, "stricttools", "posts"), "hello.md",
		[]string{"title = \"Hello World\"", "date = 2024-01-15", "slug = \"hello-world\"",
			"tags = [\"release\"]", "draft = false"},
		"This is the post content.\n")

	result := run(t, dir, "build", "--target", "posts", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("posts build failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Built ") || !strings.Contains(result.Stdout, "stricttools/.docs-cache/build/") {
		t.Errorf("the posts build summary is not the declared one:\n%s", result.Stdout)
	}
	if !exists(filepath.Join(dir, "stricttools", ".docs-cache", "build", "blog", "hello-world", "index.html")) {
		t.Error("the post page was not written")
	}
}

func TestBuildTargetPostsWithDrafts(t *testing.T) {
	isolate(t)
	dir := postProject(t, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
	})
	writeText(t, filepath.Join(dir, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \"The landing page of a project whose posts are built alone.\"\n+++\n\n# Home\n")
	postsDir := filepath.Join(dir, "stricttools", "posts")
	writePost(t, postsDir, "hello.md",
		[]string{"title = \"Hello World\"", "date = 2024-01-15", "slug = \"hello-world\"", "draft = false"},
		"Published.\n")
	writePost(t, postsDir, "draft.md",
		[]string{"title = \"Draft Post\"", "date = 2024-01-16", "slug = \"draft-post\"", "draft = true"},
		"Draft content here.\n")

	draftPage := filepath.Join(dir, "stricttools", ".docs-cache", "build", "blog", "draft-post", "index.html")

	if result := run(t, dir, "build", "--target", "posts", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("posts build failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if exists(draftPage) {
		t.Error("a draft was published without --drafts")
	}

	if result := run(t, dir, "build", "--target", "posts", "--drafts", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("posts build with drafts failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if !exists(draftPage) {
		t.Fatal("--drafts did not publish the draft")
	}
	if !strings.Contains(readText(t, draftPage), "Draft Post") {
		t.Error("the draft page does not carry its title")
	}
}

func TestBuildTargetUnified(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := testproject.MakeUnified(t, []testproject.UnifiedProject{
		{Name: "alpha"}, {Name: "beta"},
	}, nil)

	result := run(t, dir, "build", "--target", "unified", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("unified build failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "(unified)") {
		t.Errorf("the unified summary is not the declared one:\n%s", result.Stdout)
	}
}

func TestBuildRefusesAnUnknownTarget(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	result := run(t, dir, "build", "--target", "invalid", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "unknown build target 'invalid'") {
		t.Errorf("the refusal does not name the target: %s", result.Stderr)
	}
	for _, target := range []string{"site", "posts", "unified", "home"} {
		if !strings.Contains(result.Stderr, "'"+target+"'") {
			t.Errorf("the refusal does not list %q: %s", target, result.Stderr)
		}
	}
}

func TestBuildTargetHomeRequiresTheAssemblysManifests(t *testing.T) {
	// The home project's front page renders every project's live version, and
	// only the assembly's manifests carry those.
	isolate(t)
	dir := postProject(t, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
	})
	result := run(t, dir, "build", "--target", "home", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stderr, "Error:") {
		t.Errorf("no refusal printed: %s", result.Stderr)
	}
}
