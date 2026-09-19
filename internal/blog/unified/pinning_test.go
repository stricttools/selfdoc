package unified

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

func TestBuildUnifiedPinnedConstituentServesItsOwnArchivedContent(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// A superseded docs-site version that pins a constituent to a version
	// of its own is built from that constituent's tag, not from its working
	// tree -- so the archived docs-site version really serves the
	// constituent content of its day.
	docsSite := testproject.MakeUnified(t, oneProject, map[string]any{
		"version": "2.0.0",
		"versions": []any{
			map[string]any{
				"version":  "1.0.0",
				"projects": map[string]any{"core": "1.0.0"},
			},
			map[string]any{"version": "2.0.0"},
		},
	})

	coreDir := projectDirOf(docsSite, "core")
	testproject.WriteText(t, filepath.Join(coreDir, ".stricttools", "docs", "index.md"),
		"# Core v1\n\nOld core content for version 1.0.0.\n")
	testproject.Git(t, coreDir, "init")
	testproject.Git(t, coreDir, "add", ".")
	testproject.Git(t, coreDir, "commit", "-m", "core v1.0.0")
	testproject.Git(t, coreDir, "tag", "v1.0.0")

	testproject.WriteText(t, filepath.Join(coreDir, ".stricttools", "docs", "index.md"),
		"# Core v2\n\nNew core content for version 2.0.0.\n")
	testproject.Git(t, coreDir, "add", ".stricttools/docs/index.md")
	testproject.Git(t, coreDir, "commit", "-m", "core v2.0.0")
	testproject.Git(t, coreDir, "tag", "v2.0.0")

	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	output := filepath.Join(docsSite, ".stricttools", "docs-cache", "build")

	// The old docs-site version is an archive: core's pinned 1.0.0 content
	// sits under the archive prefix inside core's own mount.
	oldCore := readFile(t, filepath.Join(output, "core", "v", "1.0.0", "index.html"))
	if !strings.Contains(oldCore, "Old core content") && !strings.Contains(oldCore, "Core v1") {
		t.Errorf("the archived core page does not carry the pinned content:\n%s", oldCore)
	}

	// The current docs-site version is the stable address, with core's
	// working-tree content.
	newCore := readFile(t, filepath.Join(output, "core", "index.html"))
	if !strings.Contains(newCore, "New core content") && !strings.Contains(newCore, "Core v2") {
		t.Errorf("the current core page does not carry the current content:\n%s", newCore)
	}
}

func TestBuildUnifiedWithoutAPinningBuildsTheWorkingTreeForEveryVersion(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// A superseded docs-site version that pins nothing still gets an
	// archived mount per constituent -- built from the working tree,
	// because there is no tag to read instead.
	docsSite := testproject.MakeUnified(t, oneProject, map[string]any{
		"version": "2.0.0",
		"versions": []any{
			map[string]any{"version": "1.0.0"},
			map[string]any{"version": "2.0.0"},
		},
	})
	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	output := filepath.Join(docsSite, ".stricttools", "docs-cache", "build")

	requireFile(t, filepath.Join(output, "core", "v", "1.0.0", "index.html"))
	requireFile(t, filepath.Join(output, "core", "index.html"))
	// The docs-site's own pages are only ever built at its current version:
	// the "common" mount carries no archive.
	requireFile(t, filepath.Join(output, "common", "index.html"))
	if isDir(filepath.Join(output, "common", "v")) {
		t.Error("the common mount grew an archive tree")
	}
}
