package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// stringRootFilesProject is a committed repository on the current layout whose
// selfdoc.json still names its root files as plain template strings, with the
// files an earlier selfdoc generated from them committed.
func stringRootFilesProject(t *testing.T) string {
	t.Helper()
	dir := testproject.Make(t, map[string]any{
		"root_files": []any{".strictmetadata/docs/_README.md", claudeTemplate},
		"source":     []any{},
	})
	testproject.WriteText(t, filepath.Join(testproject.DocsDir(dir), "index.md"),
		"+++\ntitle = \"Test Project\"\ndescription = \"The documentation of a test project whose repository files are generated from templates and committed where readers find them.\"\n+++\n\n# Test Project\n\nWelcome to the docs.\n")
	testproject.WriteText(t, filepath.Join(dir, ".strictmetadata", "docs", "_README.md"), "# Readme\n")
	testproject.WriteText(t, filepath.Join(dir, filepath.FromSlash(claudeTemplate)), "# Agent notes\n")
	testproject.WriteText(t, filepath.Join(dir, "README.md"), readmeHeader(".strictmetadata/docs/_README.md")+"\n\n# Readme\n")
	testproject.WriteText(t, filepath.Join(dir, ".claude", "CLAUDE.md"), readmeHeader(claudeTemplate)+"\n\n# Agent notes\n")
	testproject.Git(t, dir, "init", "-q")
	testproject.Git(t, dir, "add", ".")
	testproject.Git(t, dir, "commit", "-q", "-m", "plain-string root files")
	return dir
}

// The whole path: gen refuses a plain-string root_files entry, naming the
// migration; the dry run prints the conversion and changes nothing; the
// migration converts every entry to the outputs it generated before, keeps the
// rest of selfdoc.json byte for byte, and commits; gen then succeeds, and a
// second migration has nothing to do.
func TestPlainStringRootFilesAreConvertedByTheMigrationAndGenClears(t *testing.T) {
	isolate(t)
	dir := stringRootFilesProject(t)
	before := readText(t, filepath.Join(dir, "selfdoc.json"))

	gen := run(t, dir, "gen", "--no-auto-commit")
	if gen.ExitCode == 0 || !strings.Contains(gen.Stderr, "root_files[0] is a plain string") ||
		!strings.Contains(gen.Stderr, "'selfdoc layout migrate'") {
		t.Fatalf("gen exited %d without naming the migration:\n%s", gen.ExitCode, gen.Stderr)
	}

	tree := treeListing(t, dir)
	dry := run(t, dir, "layout", "migrate", "--dry-run")
	if dry.ExitCode != 0 {
		t.Fatalf("the dry run failed:\n%s\n%s", dry.Stdout, dry.Stderr)
	}
	if !strings.Contains(dry.Stdout, `rewrite selfdoc.json: root_files entry ".strictmetadata/docs/_CLAUDE.md" -> outputs [".claude/CLAUDE.md"]`) {
		t.Errorf("the plan does not carry the conversion:\n%s", dry.Stdout)
	}
	if after := treeListing(t, dir); after != tree {
		t.Errorf("the dry run changed the tree:\nbefore:\n%s\nafter:\n%s", tree, after)
	}

	migrate := run(t, dir, "layout", "migrate")
	if migrate.ExitCode != 0 {
		t.Fatalf("the migration failed:\n%s\n%s", migrate.Stdout, migrate.Stderr)
	}
	config := readJSON(t, filepath.Join(dir, "selfdoc.json"))
	files, _ := config["root_files"].([]any)
	if len(files) != 2 {
		t.Fatalf("root_files = %v", config["root_files"])
	}
	for i, want := range []struct{ template, output string }{
		{".strictmetadata/docs/_README.md", "README.md"},
		{claudeTemplate, ".claude/CLAUDE.md"},
	} {
		entry, _ := files[i].(map[string]any)
		outputs, _ := entry["outputs"].([]any)
		if entry["template"] != want.template || len(outputs) != 1 || outputs[0] != want.output {
			t.Errorf("root_files[%d] = %v", i, files[i])
		}
	}
	after := readText(t, filepath.Join(dir, "selfdoc.json"))
	start, end := strings.Index(before, `"root_files"`), strings.Index(after, `"root_files"`)
	if before[:start] != after[:end] {
		t.Errorf("the text before root_files changed:\n%s\n---\n%s", before[:start], after[:end])
	}
	if status := gitOutput(t, dir, "status", "--porcelain", "--", "selfdoc.json"); status != "" {
		t.Errorf("the migration left selfdoc.json uncommitted:\n%s", status)
	}
	if subject := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s")); subject !=
		"selfdoc layout migrate: name every root_files entry's outputs" {
		t.Errorf("the last commit is %q", subject)
	}

	if gen := run(t, dir, "gen", "--no-auto-commit"); gen.ExitCode != 0 {
		t.Fatalf("gen after the migration failed:\n%s\n%s", gen.Stdout, gen.Stderr)
	}
	if result := run(t, dir, "layout", "migrate"); result.ExitCode == 0 ||
		!strings.Contains(result.Stderr, "Nothing to migrate") {
		t.Errorf("a second migration: exit %d\n%s", result.ExitCode, result.Stderr)
	}
}

// A plain-string entry whose basename carries no underscore generated nothing,
// so the migration cannot name its outputs and refuses, naming it.
func TestTheMigrationRefusesAStringEntryWithNoOutputToName(t *testing.T) {
	isolate(t)
	dir := testproject.Make(t, map[string]any{"root_files": []any{".strictmetadata/docs/README.md"}})
	testproject.Git(t, dir, "init", "-q")
	testproject.Git(t, dir, "add", ".")
	testproject.Git(t, dir, "commit", "-q", "-m", "init")
	result := run(t, dir, "layout", "migrate", "--dry-run")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, `".strictmetadata/docs/README.md"`) {
		t.Errorf("exit %d\n%s", result.ExitCode, result.Stderr)
	}
}
