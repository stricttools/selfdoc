package cli

import (
	"sort"
	"strings"
	"testing"
)

// The command tree, pinned. One binary carries what used to be two CLIs, so
// the groups and commands below are the whole surface a consumer addresses.

func TestTheCommandTreeIsTheDeclaredOne(t *testing.T) {
	registered := make([]string, 0, len(commandEffects))
	for path := range walk(t) {
		registered = append(registered, path)
	}
	sort.Strings(registered)

	expected := []string{
		"assembly.generate-shared", "assembly.init", "assembly.integrate",
		"assembly.preview", "assembly.push", "assembly.rebuild",
		"assembly.redirects", "assembly.retire", "assembly.status",
		"assembly.sync-workflow", "assembly.verify",
		"baseline.accept",
		"blog.editor.list-repos", "blog.editor.serve",
		"blog.post.generate", "blog.post.list", "blog.post.new",
		"blog.post.publish", "blog.publish-docs",
		"build", "check", "deploy",
		"gen", "gen-data", "init",
		"layout.dump", "layout.migrate", "layout.validate",
		"quality", "serve", "spell-corpus",
		"vocabulary.accept", "vocabulary.approve", "vocabulary.drop",
		"vocabulary.reject", "vocabulary.remove",
	}
	if strings.Join(registered, " ") != strings.Join(expected, " ") {
		t.Errorf("the command tree is\n  %v\nwant\n  %v", registered, expected)
	}
}

func TestTheGroupsCarryTheirHelp(t *testing.T) {
	schema := New(Options{}).DumpSchemaDict()
	for path, want := range map[string]string{
		"baseline":    "Manage the content and description hash baselines that drive staleness (STALE001) and source-drift (DRIFT001) detection during selfdoc check",
		"assembly":    "Manage the unified multi-project documentation assembly and deployment",
		"blog":        "Blog posts, the authoring app, and publishing this project's documentation to the unified site",
		"blog.post":   "Manage blog posts and chronological content for the documentation site",
		"blog.editor": "Run and inspect the local authoring app for blog posts",
	} {
		group := schemaGroup(schema, path)
		if group == nil {
			t.Errorf("group %q is not registered", path)
			continue
		}
		if group["help"] != want {
			t.Errorf("group %q help is %q", path, group["help"])
		}
	}
}

// schemaGroup walks a dumped schema to the group at a dotted path, nil when
// no such group is registered.
func schemaGroup(schema map[string]any, path string) map[string]any {
	current := schema
	for _, segment := range strings.Split(path, ".") {
		groups, ok := current["groups"].(map[string]any)
		if !ok {
			return nil
		}
		current, ok = groups[segment].(map[string]any)
		if !ok {
			return nil
		}
	}
	return current
}

// The three groups the blog group absorbed are gone from the top level: a
// consumer that still types `selfdoc post new` is told the command does not
// exist rather than reaching a surviving duplicate.
func TestTheAbsorbedGroupsAreGoneFromTheTopLevel(t *testing.T) {
	schema := New(Options{}).DumpSchemaDict()
	groups, _ := schema["groups"].(map[string]any)
	commands, _ := schema["commands"].(map[string]any)
	for _, name := range []string{"post", "docs", "editor"} {
		if _, ok := groups[name]; ok {
			t.Errorf("%q is still a top-level group", name)
		}
		if _, ok := commands[name]; ok {
			t.Errorf("%q is still a top-level command", name)
		}
	}

	result := run(t, t.TempDir(), "post", "new", "--title", "Nope")
	if result.ExitCode == 0 {
		t.Errorf("`post new` still runs: %s", result.Stdout)
	}
}

func TestEveryGroupAndCommandAnswersHelp(t *testing.T) {
	dir := t.TempDir()
	for _, argv := range [][]string{
		{"--help"},
		{"baseline", "--help"}, {"baseline", "accept", "--help"},
		{"blog", "--help"}, {"blog", "post", "--help"},
		{"blog", "post", "new", "--help"},
		{"blog", "publish-docs", "--help"},
		{"assembly", "--help"},
		{"assembly", "init", "--help"}, {"assembly", "push", "--help"},
		{"assembly", "status", "--help"}, {"assembly", "rebuild", "--help"},
		{"assembly", "retire", "--help"}, {"assembly", "preview", "--help"},
		{"blog", "editor", "--help"}, {"blog", "editor", "serve", "--help"},
		{"blog", "editor", "list-repos", "--help"},
		{"build", "--help"}, {"check", "--help"},
	} {
		result := run(t, dir, argv...)
		if result.ExitCode != 0 {
			t.Errorf("%v exited %d: %s", argv, result.ExitCode, result.Stderr)
		}
	}
}

func TestServeHelpSurvivesDryRunOnTheSameLine(t *testing.T) {
	// --help always beats a dry-run refusal.
	if result := run(t, t.TempDir(), "blog", "editor", "serve", "--dry-run", "--help"); result.ExitCode != 0 {
		t.Errorf("help refused: %d %s", result.ExitCode, result.Stderr)
	}
}
