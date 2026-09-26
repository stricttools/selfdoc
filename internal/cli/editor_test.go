package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/editor"
	"github.com/stricttools/selfdoc/internal/blog/editor/assets"
)

// The editor's command surface.
//
// `serve` is not exercised end to end here -- it blocks until interrupted, and
// the wire behaviour it exposes has its own package. What is asserted is the
// registration (the flags that exist, the ones with no default, the effect
// classification) and the refusals a bad registry or a missing asset tree
// produce before anything binds a port.

// registryFile writes a registry TOML and returns its path.
func registryFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.toml")
	writeText(t, path, body)
	return path
}

func TestTheEditorGroupCarriesBothCommands(t *testing.T) {
	commands := walk(t)
	for _, path := range []string{"blog.editor.serve", "blog.editor.list-repos"} {
		if _, ok := commands[path]; !ok {
			t.Errorf("%q is not registered", path)
		}
	}
	if commands["blog.editor.list-repos"]["effect"] != "read_only" {
		t.Errorf("list-repos is classified %v", commands["blog.editor.list-repos"]["effect"])
	}
	serve := commands["blog.editor.serve"]
	if serve["effect"] != "mutating" {
		t.Errorf("serve is classified %v", serve["effect"])
	}
	if _, declared := serve["consequential"]; declared {
		t.Error("serve declares itself consequential")
	}
}

func TestEditorServeDeclaresThatItCannotBePreviewed(t *testing.T) {
	// An interactive server has no set of effects to record at launch.
	serve := walk(t)["blog.editor.serve"]
	if serve["dry_run_supported"] != false {
		t.Error("serve does not refuse a preview")
	}
	reason, _ := serve["dry_run_unsupported_reason"].(string)
	if !strings.Contains(reason, "at the keyboard") {
		t.Errorf("the refusal reason is %q", reason)
	}
}

func TestEditorServeRefusesAPreviewAtParseTime(t *testing.T) {
	// The refusal comes before anything binds a port or reads a registry.
	// What must never happen is a preview that binds a port and then performs
	// real saves: they run on request threads that carry none of the dispatch
	// context, so a recorded run would execute them for real.
	isolate(t)
	path := registryFile(t, "")
	result := run(t, t.TempDir(), "blog", "editor", "serve",
		"--port", "0", "--registry", path, "--dry-run")
	if result.ExitCode == 0 {
		t.Fatal("the preview was accepted")
	}
	if !strings.Contains(result.Stderr+result.Stdout, "at the keyboard") {
		t.Errorf("the refusal is not the declared one: %s", result.Stderr)
	}
}

func TestEditorServeRefusesAPreviewBeforeItsRequiredFlags(t *testing.T) {
	// Parse-time means it does not need a valid invocation to refuse.
	isolate(t)
	result := run(t, t.TempDir(), "blog", "editor", "serve", "--dry-run")
	if result.ExitCode == 0 {
		t.Fatal("the preview was accepted")
	}
	if !strings.Contains(result.Stderr+result.Stdout, "at the keyboard") {
		t.Errorf("the refusal is not the declared one: %s", result.Stderr)
	}
}

func TestEditorServePortIsDeclaredRequired(t *testing.T) {
	// Presence is declared, never derived -- the port is stated.
	flags := flagsOf(t, "blog.editor.serve")
	port, ok := flags["port"]
	if !ok {
		t.Fatal("serve declares no --port")
	}
	if port["presence"] != "required" {
		t.Errorf("--port presence is %v", port["presence"])
	}
	schema, _ := port["value_schema"].(map[string]any)
	if schema["type"] != "integer" {
		t.Errorf("--port type is %v", schema["type"])
	}
}

func TestEditorServeRefusesWithoutAPort(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "blog", "editor", "serve")
	if result.ExitCode == 0 {
		t.Fatal("serve ran with no port")
	}
	if !strings.Contains(result.Stderr+result.Stdout, "port") {
		t.Errorf("the refusal does not name the flag: %s", result.Stderr)
	}
}

func TestEditorListReposListsAHandWrittenFile(t *testing.T) {
	isolate(t)
	tree := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	path := registryFile(t, "\n"+
		"[[repo]]\nname = \"proj\"\nkind = \"local\"\npath = \""+tree+"\"\n\n"+
		"[[repo]]\nname = \"afar\"\nkind = \"remote\"\nrepo = \"smm-h/afar\"\n"+
		"ref = \"main\"\ncache = \""+cache+"\"\nrender = false\n")

	result := run(t, t.TempDir(), "blog", "editor", "list-repos", "--registry", path)
	if result.ExitCode != 0 {
		t.Fatalf("list-repos failed: %s", result.Stderr)
	}
	for _, want := range []string{"proj", tree, "smm-h/afar@main", "not served yet"} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("the listing does not carry %q:\n%s", want, result.Stdout)
		}
	}
}

func TestEditorListReposSaysSoWhenTheRegistryIsEmpty(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "blog", "editor", "list-repos", "--registry", registryFile(t, ""))
	if result.ExitCode != 0 {
		t.Fatalf("list-repos failed: %s", result.Stderr)
	}
	if !strings.Contains(result.Stdout, "No repositories") {
		t.Errorf("an empty registry is not reported:\n%s", result.Stdout)
	}
}

func TestEditorListReposNamesAMalformedEntry(t *testing.T) {
	isolate(t)
	path := registryFile(t, "\n[[repo]]\nname = \"broken\"\nkind = \"local\"\n")
	result := run(t, t.TempDir(), "blog", "editor", "list-repos", "--registry", path)
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	for _, want := range []string{"broken", "path"} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("the refusal does not name %q: %s", want, result.Stderr)
		}
	}
}

func TestEditorListReposNamesAMissingRegistry(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "nope.toml")
	result := run(t, t.TempDir(), "blog", "editor", "list-repos", "--registry", missing)
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "nope.toml") {
		t.Errorf("the refusal does not name the path: %s", result.Stderr)
	}
}

func TestEditorServeStopsOnAMalformedRegistry(t *testing.T) {
	isolate(t)
	path := registryFile(t, "port = 1\n")
	result := run(t, t.TempDir(), "blog", "editor", "serve", "--port", "0", "--registry", path)
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "port") {
		t.Errorf("the refusal does not name the offending key: %s", result.Stderr)
	}
}

func TestEditorServeStopsOnMissingTinymoonAssets(t *testing.T) {
	isolate(t)
	nowhere := filepath.Join(t.TempDir(), "nowhere")
	result := run(t, t.TempDir(), "blog", "editor", "serve",
		"--port", "0", "--registry", registryFile(t, ""),
		"--tinymoon-assets", nowhere)
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "nowhere") {
		t.Errorf("the refusal does not name the path: %s", result.Stderr)
	}
}

func TestEditorServeStopsOnAnAssetTreeWithoutTheEditorTier(t *testing.T) {
	isolate(t)
	tree := filepath.Join(t.TempDir(), "assets")
	for _, rel := range assets.TinymoonRequired {
		if rel == "js/editor.js" {
			continue
		}
		writeText(t, filepath.Join(tree, filepath.FromSlash(rel)), "/* stub */\n")
	}

	result := run(t, t.TempDir(), "blog", "editor", "serve",
		"--port", "0", "--registry", registryFile(t, ""),
		"--tinymoon-assets", tree)
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "js/editor.js") {
		t.Errorf("the refusal does not name the missing file: %s", result.Stderr)
	}
}

// -- the publish surface the editor is handed ------------------------------

func TestThePublisherProjectsTheCommandsOwnDeclaration(t *testing.T) {
	// The consent dialog renders what the command really declares, read off
	// the schema rather than copied into the editor.
	descriptor := (&publisher{}).Descriptor()
	if descriptor.Effect != "mutating" {
		t.Errorf("the projected effect is %q", descriptor.Effect)
	}
	if !descriptor.Consequential {
		t.Error("the projected declaration is not consequential")
	}
	if descriptor.Help == "" {
		t.Error("the projected declaration carries no help")
	}
	if len(descriptor.Grants) != 1 || descriptor.Grants[0].Name != "assembly-dispatch" {
		t.Errorf("the projected grants are %v", descriptor.Grants)
	}
	if descriptor.Grants[0].Kind != "proc_mutate" {
		t.Errorf("the projected grant kind is %q", descriptor.Grants[0].Kind)
	}
}

func TestThePublisherPlansFromTheProjectsOwnPosts(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	postsDir := filepath.Join(dir, "stricttools", "posts")
	writePost(t, postsDir, "live.md",
		[]string{"title = \"Live\"", "date = 2025-01-15", "slug = \"live\"", "draft = false"}, "Body.\n")
	writePost(t, postsDir, "held.md",
		[]string{"title = \"Held\"", "date = 2025-02-15", "slug = \"held\"", "draft = true"}, "Body.\n")

	plan, err := (&publisher{}).Plan(dir)
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if len(plan.Publishing) != 1 || plan.Publishing[0].Slug != "live" {
		t.Errorf("publishing is %v", plan.Publishing)
	}
	if len(plan.Withheld) != 1 || plan.Withheld[0].Slug != "held" {
		t.Errorf("withheld is %v", plan.Withheld)
	}
}

func TestThePublisherRefusesAnUnconsentedPublish(t *testing.T) {
	// The publish command is consequential, so an in-process call with no
	// consent is refused by the framework and forwarded verbatim.
	isolate(t)
	dir := postProject(t, nil)
	var out strings.Builder
	code, err := (&publisher{}).Publish(dir, false, &out)
	if err == nil {
		t.Fatal("an unconsented publish was accepted")
	}
	var refusal *editor.Refusal
	if !asError(err, &refusal) {
		t.Fatalf("the error is not the refusal shape the editor answers: %T %v", err, err)
	}
	if code != 1 {
		t.Errorf("the refused call reports exit %d", code)
	}
	if !strings.Contains(refusal.Message, "consequential") {
		t.Errorf("the forwarded message is %q", refusal.Message)
	}
}

func TestThePublisherWritesEverythingTheInvocationEmits(t *testing.T) {
	// The invocation is handed one writer and nothing redirects a
	// process-wide stream, so the editor gets the command's own output.
	isolate(t)
	// A project with nothing to publish: the command reports that and stops
	// before it reaches the network.
	dir := postProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "myproject"},
	})
	var out strings.Builder
	code, err := (&publisher{}).Publish(dir, true, &out)
	if err != nil {
		t.Fatalf("publishing: %v", err)
	}
	if code != 0 {
		t.Fatalf("the publish exited %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "No non-draft posts to publish.") {
		t.Errorf("the invocation's output did not reach the writer: %q", out.String())
	}
}

// flagsOf returns a command's declared flags, keyed by name.
func flagsOf(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	entry, ok := walk(t)[path]
	if !ok {
		t.Fatalf("command %q is not registered", path)
	}
	flags := map[string]map[string]any{}
	raw, _ := entry["flags"].([]any)
	for _, item := range raw {
		flag, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := flag["name"].(string)
		flags[name] = flag
	}
	return flags
}
