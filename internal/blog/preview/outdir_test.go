package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The output-directory refusal has both halves: a path git ignores is
// accepted, the same path un-ignored is refused. Both run against a real
// repository, because the question is answered by asking git.

// absolute is path as the refusal computes it, so a comparison against what
// the refusal returns is not a comparison of two spellings of one directory.
func absolute(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs(%q): %v", path, err)
	}
	return resolved
}

// initRepo creates an empty git repository and returns its path.
func initRepo(t *testing.T, path string) string {
	t.Helper()
	testproject.MkdirAll(t, path)
	testproject.Git(t, path, "init")
	return path
}

func TestTheOutputDirectory(t *testing.T) {
	hygiene.Isolate(t)
	handle := effects.Unbound()

	t.Run("a directory outside every repository is accepted", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "preview")
		if got := EnclosingWorktree(out); got != "" {
			t.Fatalf("EnclosingWorktree = %q, want the empty string", got)
		}
		reason, err := OutDirRefusal(out, handle)
		if err != nil {
			t.Fatalf("OutDirRefusal: %v", err)
		}
		if reason != "" {
			t.Errorf("OutDirRefusal = %q, want the empty string", reason)
		}
		if err := RefuseUnsafeOutDir(out, handle); err != nil {
			t.Errorf("RefuseUnsafeOutDir: %v", err)
		}
	})

	t.Run("an untracked path inside a checkout is refused", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"))
		out := filepath.Join(repo, "preview")
		if got := EnclosingWorktree(out); got != absolute(t, repo) {
			t.Fatalf("EnclosingWorktree = %q, want %q", got, absolute(t, repo))
		}
		reason, err := OutDirRefusal(out, handle)
		if err != nil {
			t.Fatalf("OutDirRefusal: %v", err)
		}
		if !strings.Contains(reason, "does not ignore it") {
			t.Errorf("the refusal does not say git ignores nothing: %q", reason)
		}
		if !strings.Contains(reason, "preview") {
			t.Errorf("the refusal does not name the directory: %q", reason)
		}
		err = RefuseUnsafeOutDir(out, handle)
		if err == nil || !strings.Contains(err.Error(), "does not ignore it") {
			t.Errorf("RefuseUnsafeOutDir = %v, want the refusal", err)
		}
	})

	t.Run("the same path gitignored is accepted", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"))
		out := filepath.Join(repo, "preview")
		reason, err := OutDirRefusal(out, handle)
		if err != nil {
			t.Fatalf("OutDirRefusal: %v", err)
		}
		if reason == "" {
			t.Fatal("the un-ignored path was accepted")
		}
		testproject.WriteText(t, filepath.Join(repo, ".gitignore"), "preview/\n")
		reason, err = OutDirRefusal(out, handle)
		if err != nil {
			t.Fatalf("OutDirRefusal: %v", err)
		}
		if reason != "" {
			t.Errorf("the gitignored path was refused: %q", reason)
		}
		if err := RefuseUnsafeOutDir(out, handle); err != nil {
			t.Errorf("RefuseUnsafeOutDir: %v", err)
		}
	})

	t.Run("the root of a checkout is refused by name", func(t *testing.T) {
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"))
		reason, err := OutDirRefusal(repo, handle)
		if err != nil {
			t.Fatalf("OutDirRefusal: %v", err)
		}
		if !strings.Contains(reason, "is the root of the git working tree") {
			t.Errorf("the refusal does not name the root: %q", reason)
		}
	})

	t.Run("a linked worktree is found through its .git file", func(t *testing.T) {
		// ".git" is tested for existence rather than for being a directory,
		// which is the whole of what makes a linked worktree findable.
		root := t.TempDir()
		linked := filepath.Join(root, "linked")
		testproject.MkdirAll(t, linked)
		testproject.WriteText(t, filepath.Join(linked, ".git"), "gitdir: /elsewhere/.git/worktrees/linked\n")
		if got := EnclosingWorktree(filepath.Join(linked, "preview")); got != absolute(t, linked) {
			t.Errorf("EnclosingWorktree = %q, want %q", got, absolute(t, linked))
		}
	})

	t.Run("the pipeline refuses before it writes anything", func(t *testing.T) {
		testproject.RequirePagefind(t)
		repo := initRepo(t, filepath.Join(t.TempDir(), "repo"))
		out := filepath.Join(repo, "preview")
		home := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		_, err := PreviewAssembly(home, nil, out, canonicalBase, false, "", handle)
		if err == nil || !strings.Contains(err.Error(), "does not ignore it") {
			t.Fatalf("PreviewAssembly = %v, want the refusal", err)
		}
		if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
			t.Errorf("%s exists; the refusal wrote something", out)
		}
	})
}
