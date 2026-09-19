package unified

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// makeWorkspace writes a monorepo carrying an rlsbl workspace declaration
// whose projects list is the given TOML fragment, plus a selfdoc.json in each
// named package directory, and returns the docs-site's path.
func makeWorkspace(t *testing.T, projectsTOML string, packages []string) string {
	t.Helper()
	monorepo := filepath.Join(t.TempDir(), "monorepo")
	testproject.WriteText(t,
		filepath.Join(monorepo, ".rlsbl-monorepo", "workspace.toml"), projectsTOML)
	for _, name := range packages {
		testproject.WriteText(t,
			filepath.Join(monorepo, "packages", name, "selfdoc.json"), "{}")
	}
	docsSite := filepath.Join(monorepo, "packages", "docs-site")
	testproject.MkdirAll(t, docsSite)
	return docsSite
}

// unifiedEntries builds a unified config block declaring the given paths, and
// the given exclude patterns.
func unifiedEntries(paths, exclude []string) map[string]any {
	projects := make([]any, 0, len(paths))
	for _, path := range paths {
		projects = append(projects, map[string]any{"path": path})
	}
	block := map[string]any{"projects": projects}
	if exclude != nil {
		patterns := make([]any, 0, len(exclude))
		for _, pattern := range exclude {
			patterns = append(patterns, pattern)
		}
		block["exclude"] = patterns
	}
	return block
}

func TestValidateRlsblWorkspaceWithoutAWorkspaceIsANoOp(t *testing.T) {
	t.Parallel()

	docsSite := filepath.Join(t.TempDir(), "docs-site")
	testproject.MkdirAll(t, docsSite)
	if err := validateRlsblWorkspace(docsSite, unifiedEntries(nil, nil)); err != nil {
		t.Fatalf("validateRlsblWorkspace: %v", err)
	}
}

func TestValidateRlsblWorkspaceAcceptsAFullyDeclaredWorkspace(t *testing.T) {
	t.Parallel()

	docsSite := makeWorkspace(t,
		"projects = [\"packages/core\", \"packages/cli\"]\n", []string{"core", "cli"})
	if err := validateRlsblWorkspace(docsSite,
		unifiedEntries([]string{"../core", "../cli"}, nil)); err != nil {
		t.Fatalf("validateRlsblWorkspace: %v", err)
	}
}

func TestValidateRlsblWorkspaceRefusesAnUndeclaredProject(t *testing.T) {
	t.Parallel()

	docsSite := makeWorkspace(t,
		"projects = [\"packages/core\", \"packages/cli\"]\n", []string{"core", "cli"})
	err := validateRlsblWorkspace(docsSite, unifiedEntries([]string{"../core"}, nil))
	if err == nil {
		t.Fatal("an undeclared workspace project was accepted")
	}
	for _, want := range []string{"cli", "neither in unified.projects"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to say %q", err, want)
		}
	}
}

func TestValidateRlsblWorkspaceAcceptsAnExcludedProject(t *testing.T) {
	t.Parallel()

	docsSite := makeWorkspace(t,
		"projects = [\"packages/core\", \"packages/internal\"]\n", []string{"core", "internal"})
	if err := validateRlsblWorkspace(docsSite,
		unifiedEntries([]string{"../core"}, []string{"internal"})); err != nil {
		t.Fatalf("validateRlsblWorkspace: %v", err)
	}
}

func TestValidateRlsblWorkspaceExcludePatternsAreGlobsCompiledAsExpressions(t *testing.T) {
	t.Parallel()

	// The pattern's "*" becomes ".*" and the match is anchored at the start
	// of the name and nowhere else, so "int*" and the bare prefix
	// "internal" both cover "internal-tools".
	docsSite := makeWorkspace(t,
		"projects = [\"packages/core\", \"packages/internal-tools\"]\n",
		[]string{"core", "internal-tools"})
	declared := []string{"../core"}

	for _, pattern := range []string{"int*", "internal", "*tools"} {
		if err := validateRlsblWorkspace(docsSite,
			unifiedEntries(declared, []string{pattern})); err != nil {
			t.Errorf("pattern %q did not exclude internal-tools: %v", pattern, err)
		}
	}
	if err := validateRlsblWorkspace(docsSite,
		unifiedEntries(declared, []string{"tools"})); err == nil {
		t.Error("an unanchored suffix pattern excluded internal-tools; the match is anchored at the start")
	}
}

func TestValidateRlsblWorkspaceReadsTableEntriesToo(t *testing.T) {
	t.Parallel()

	// A workspace.toml states its members either as bare path strings or as
	// tables carrying a "path" key.
	docsSite := makeWorkspace(t,
		"[[projects]]\npath = \"packages/core\"\n\n[[projects]]\npath = \"packages/cli\"\n",
		[]string{"core", "cli"})
	err := validateRlsblWorkspace(docsSite, unifiedEntries([]string{"../core"}, nil))
	if err == nil || !strings.Contains(err.Error(), "packages/cli") {
		t.Fatalf("err = %v, want it to name packages/cli", err)
	}
}

func TestValidateRlsblWorkspacePassesOverWhatItCannotJudge(t *testing.T) {
	t.Parallel()

	t.Run("a workspace declaring no projects", func(t *testing.T) {
		t.Parallel()
		docsSite := makeWorkspace(t, "projects = []\n", nil)
		if err := validateRlsblWorkspace(docsSite, unifiedEntries(nil, nil)); err != nil {
			t.Fatalf("validateRlsblWorkspace: %v", err)
		}
	})

	t.Run("a member carrying no selfdoc.json", func(t *testing.T) {
		t.Parallel()
		docsSite := makeWorkspace(t,
			"projects = [\"packages/core\", \"packages/plain\"]\n", []string{"core"})
		testproject.MkdirAll(t, filepath.Join(
			filepath.Dir(filepath.Dir(docsSite)), "packages", "plain"))
		if err := validateRlsblWorkspace(docsSite,
			unifiedEntries([]string{"../core"}, nil)); err != nil {
			t.Fatalf("a member with no selfdoc.json was refused: %v", err)
		}
	})

	t.Run("the docs-site itself", func(t *testing.T) {
		t.Parallel()
		docsSite := makeWorkspace(t,
			"projects = [\"packages/core\", \"packages/docs-site\"]\n",
			[]string{"core", "docs-site"})
		if err := validateRlsblWorkspace(docsSite,
			unifiedEntries([]string{"../core"}, nil)); err != nil {
			t.Fatalf("the docs-site was refused as a constituent of itself: %v", err)
		}
	})
}
