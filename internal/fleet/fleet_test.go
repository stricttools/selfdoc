package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/smm-h/stricttest/go/hygiene"
)

// defaultConfig is the minimal valid selfdoc config the Python fixture used:
// required versions and locales, one source entry, a declared author and
// search engine.
func defaultConfig() map[string]any {
	return map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"version":       "1.0.0",
		"versions":      []any{map[string]any{"version": "1.0.0"}},
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
		"search_engine": "pagefind",
		"author":        map[string]any{"name": "Test Author", "url": "https://author.example"},
	}
}

// project creates a sibling project directory under root. A nil cfg writes
// the default config; docs maps a path relative to the project's docs/
// directory to its content.
func project(t *testing.T, root, name string, cfg map[string]any, docs map[string]string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	if cfg == nil {
		cfg = defaultConfig()
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshalling the config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "selfdoc.json"), encoded, 0o644); err != nil {
		t.Fatalf("writing selfdoc.json: %v", err)
	}
	for rel, content := range docs {
		full := filepath.Join(path, ".stricttools", "docs", rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", full, err)
		}
	}
	return path
}

// names renders a ProjectDirs result as the basenames the cases assert on.
func names(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, dir := range dirs {
		out[i] = filepath.Base(dir)
	}
	return out
}

// discover runs DiscoverFleet through an unbound handle -- the library path,
// which executes directly.
func discover(t *testing.T, root string) map[string]FleetProject {
	t.Helper()
	found, err := DiscoverFleet(root)
	if err != nil {
		t.Fatalf("DiscoverFleet: %v", err)
	}
	byName := map[string]FleetProject{}
	for _, entry := range found {
		byName[entry.Name] = entry
	}
	if len(byName) != len(found) {
		t.Fatalf("two projects came back under one name: %#v", found)
	}
	return byName
}

// dispatchPreviewing hands body the handle a real --dry-run dispatch builds,
// so the preview refusal is asserted against the framework rather than a
// stand-in.
func dispatchPreviewing(t *testing.T, body func(h *effects.Handle)) {
	t.Helper()
	app := strictcli.NewApp("fleettest", "0.0.0", "harness for the fleet package")
	app.Command("do", "run the test body",
		func(ctx *strictcli.Context, _ map[string]interface{}) strictcli.Outcome {
			body(effects.FromContext(ctx))
			return strictcli.Exit(0)
		},
		strictcli.WithEffect(strictcli.EffectMutating),
	)
	app.Test([]string{"do", "--dry-run"})
}

// -- ProjectDirs --------------------------------------------------------------

func TestProjectDirs(t *testing.T) {
	t.Run("a sibling is a directory holding a selfdoc.json, sorted by name", func(t *testing.T) {
		root := t.TempDir()
		project(t, root, "beta", nil, nil)
		project(t, root, "alpha", nil, nil)
		if err := os.MkdirAll(filepath.Join(root, "not-a-project"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := names(ProjectDirs(root)); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
			t.Fatalf("ProjectDirs = %#v", got)
		}
	})

	t.Run("archives and caches under a dot-directory are not the fleet", func(t *testing.T) {
		root := t.TempDir()
		project(t, root, ".archive", nil, nil)
		if got := ProjectDirs(root); len(got) != 0 {
			t.Fatalf("ProjectDirs = %#v, want nothing", got)
		}
	})

	t.Run("no siblings is a real answer, not a failure", func(t *testing.T) {
		if got := ProjectDirs(filepath.Join(t.TempDir(), "nowhere")); len(got) != 0 {
			t.Fatalf("ProjectDirs = %#v, want nothing", got)
		}
	})

	t.Run("a file where a project would be is not a project", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ProjectDirs(root); len(got) != 0 {
			t.Fatalf("ProjectDirs = %#v, want nothing", got)
		}
	})
}

// -- DiscoverFleet ------------------------------------------------------------

func TestDiscoverFleetLoadsAHealthyProject(t *testing.T) {
	root := t.TempDir()
	project(t, root, "alpha", nil, nil)
	found := discover(t, root)
	alpha := found["alpha"]
	if !alpha.Loaded() {
		t.Fatalf("alpha did not load: %#v", alpha)
	}
	if alpha.Sanitized {
		t.Fatal("alpha was reported sanitized, but its config carries no retired key")
	}
	if alpha.Error != "" {
		t.Fatalf("alpha carries an error: %s", alpha.Error)
	}
	if alpha.Config["base_url"] != "https://example.com" {
		t.Fatalf("alpha's config = %#v", alpha.Config)
	}
}

func TestDiscoverFleetReportsABrokenConfigWithoutFailing(t *testing.T) {
	root := t.TempDir()
	project(t, root, "healthy", nil, nil)
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "selfdoc.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := discover(t, root)
	if !found["healthy"].Loaded() {
		t.Fatal("a broken neighbour stopped the sweep over the rest of the fleet")
	}
	if found["broken"].Loaded() {
		t.Fatalf("the broken project reported a config: %#v", found["broken"])
	}
	if found["broken"].Error == "" {
		t.Fatal("the broken project carries no reason")
	}
	if !strings.HasPrefix(found["broken"].Error, "JSONDecodeError: ") {
		t.Fatalf("the reason does not name the failure kind: %s", found["broken"].Error)
	}
}

func TestDiscoverFleetReportsASchemaRefusalWithoutFailing(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg["bogus_key"] = true
	project(t, root, "invalid", cfg, nil)

	found := discover(t, root)
	if found["invalid"].Loaded() {
		t.Fatalf("an invalid config loaded: %#v", found["invalid"])
	}
	if !strings.HasPrefix(found["invalid"].Error, "ConfigError: unknown config key") {
		t.Fatalf("the reason does not name the schema refusal: %s", found["invalid"].Error)
	}
}

func TestDiscoverFleetSanitizesRetiredKeys(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg["versions"] = []any{map[string]any{"version": "1.0.0", "indexed": true}}
	project(t, root, "stale", cfg, nil)

	found := discover(t, root)
	stale := found["stale"]
	if !stale.Loaded() {
		t.Fatalf("a retired schema key made the project unloadable: %s", stale.Error)
	}
	if !stale.Sanitized {
		t.Fatal("the project loaded but was not reported sanitized")
	}
}

func TestDiscoverFleetNeverWritesIntoTheProject(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg["versions"] = []any{map[string]any{"version": "1.0.0", "indexed": true}}
	path := project(t, root, "stale", cfg, nil)

	before, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeNames := make([]string, len(before))
	for i, entry := range before {
		beforeNames[i] = entry.Name()
	}

	discover(t, root)

	after, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	afterNames := make([]string, len(after))
	for i, entry := range after {
		afterNames[i] = entry.Name()
	}
	if !reflect.DeepEqual(beforeNames, afterNames) {
		t.Fatalf("the project's contents changed: %#v -> %#v", beforeNames, afterNames)
	}

	data, err := os.ReadFile(filepath.Join(path, "selfdoc.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := config.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	versions := raw.(map[string]any)["versions"].([]any)
	if versions[0].(map[string]any)["indexed"] != true {
		t.Fatalf("the project's own config was rewritten: %#v", versions)
	}
}

// TestDiscoverFleetLeavesNoScratchDirectoryBehind points TMPDIR at a
// directory of this test's own, so the assertion is about this call's scratch
// directory and not about whatever else the machine has in /tmp.
func TestDiscoverFleetLeavesNoScratchDirectoryBehind(t *testing.T) {
	hygiene.Isolate(t)
	root := t.TempDir()
	tmp := t.TempDir()
	// Set after both directories exist, so neither is created inside the
	// directory the assertion below requires to be empty.
	t.Setenv("TMPDIR", tmp)

	cfg := defaultConfig()
	cfg["versions"] = []any{map[string]any{"version": "1.0.0", "indexed": true}}
	project(t, root, "stale", cfg, nil)

	found := discover(t, root)
	if !found["stale"].Sanitized {
		t.Fatalf("the sanitized path did not run, so nothing was written: %#v", found["stale"])
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, entry := range entries {
			names[i] = entry.Name()
		}
		t.Fatalf("a scratch directory survived the call: %#v", names)
	}
}

// -- LoadProjectConfig --------------------------------------------------------

func TestLoadProjectConfigReturnsTheOriginalDiagnosisWhenNoRetiredKeyExplainsIt(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	delete(cfg, "base_url")
	path := project(t, root, "invalid", cfg, nil)

	_, sanitized, err := LoadProjectConfig(path, t.TempDir())
	if err == nil {
		t.Fatal("expected the schema refusal")
	}
	if sanitized {
		t.Fatal("a failed load reported itself sanitized")
	}
	var configErr *config.ConfigError
	if !asConfigErr(err, &configErr) || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("expected the original diagnosis, got %v", err)
	}
}

func asConfigErr(err error, target **config.ConfigError) bool {
	typed, ok := err.(*config.ConfigError)
	if ok {
		*target = typed
	}
	return ok
}

// TestLoadProjectConfigCompletesTheSanitizedRetryUnderAPreview covers the one
// write this package performs outside the effects handle. The copy exists only
// to be read back by the loader, so a preview performs it and answers what a
// real run answers, rather than reporting a loadable project unloadable.
func TestLoadProjectConfigCompletesTheSanitizedRetryUnderAPreview(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg["versions"] = []any{map[string]any{"version": "1.0.0", "indexed": true}}
	path := project(t, root, "stale", cfg, nil)
	scratch := t.TempDir()

	dispatchPreviewing(t, func(h *effects.Handle) {
		if !h.Previewing() {
			t.Fatal("the dispatch did not hand a previewing handle")
		}
		loaded, sanitized, err := LoadProjectConfig(path, scratch)
		if err != nil {
			t.Fatalf("a preview refused the sanitized retry: %v", err)
		}
		if !sanitized || loaded == nil {
			t.Fatalf("sanitized = %v, config = %#v", sanitized, loaded)
		}
	})
	copyPath := filepath.Join(scratch, "stale", "selfdoc.json")
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("a preview did not write the sanitized copy to %s: %v", copyPath, err)
	}
}

func TestLoadProjectConfigWritesTheSanitizedCopyIntoTheScratchDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg["versions"] = []any{map[string]any{"version": "1.0.0", "indexed": true}}
	path := project(t, root, "stale", cfg, nil)
	scratch := t.TempDir()

	loaded, sanitized, err := LoadProjectConfig(path, scratch)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !sanitized || loaded == nil {
		t.Fatalf("sanitized = %v, config = %#v", sanitized, loaded)
	}
	copyPath := filepath.Join(scratch, "stale", "selfdoc.json")
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("the sanitized copy is not at %s: %v", copyPath, err)
	}
	data, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "indexed") {
		t.Fatalf("the copy still carries the retired key: %s", data)
	}
}

// -- LoadDocsBodies -----------------------------------------------------------

func TestLoadDocsBodiesReturnsTheLintSliceShape(t *testing.T) {
	root := t.TempDir()
	path := project(t, root, "alpha", nil, map[string]string{
		"index.md": "+++\ntitle = \"Home\"\n+++\n\nBody text.\n",
	})
	bodies, err := LoadDocsBodies(filepath.Join(path, ".stricttools", "docs"))
	if err != nil {
		t.Fatalf("LoadDocsBodies: %v", err)
	}
	page, ok := bodies["index.md"]
	if !ok {
		t.Fatalf("index.md is missing: %#v", bodies)
	}
	if page.Frontmatter["title"] != "Home" {
		t.Fatalf("frontmatter = %#v", page.Frontmatter)
	}
	if page.Resolved != "" {
		t.Fatalf("directives were resolved: %q", page.Resolved)
	}
	if !strings.Contains(page.Body, "Body text.") {
		t.Fatalf("body = %q", page.Body)
	}
	// Delimiters plus the blank line separating frontmatter from the body.
	if page.FrontmatterLines != 4 {
		t.Fatalf("FrontmatterLines = %d, want 4", page.FrontmatterLines)
	}
}

func TestLoadDocsBodiesSkipsPartialsAndBuildOutput(t *testing.T) {
	root := t.TempDir()
	path := project(t, root, "alpha", nil, map[string]string{
		"index.md":        "# Home\n",
		"_partial.md":     "# Partial\n",
		"_build/stale.md": "# Stale\n",
		"guide/deep.md":   "# Deep\n",
		"notes.txt":       "not a page\n",
	})
	bodies, err := LoadDocsBodies(filepath.Join(path, ".stricttools", "docs"))
	if err != nil {
		t.Fatalf("LoadDocsBodies: %v", err)
	}
	got := make([]string, 0, len(bodies))
	for rel := range bodies {
		got = append(got, rel)
	}
	want := map[string]bool{"index.md": true, filepath.Join("guide", "deep.md"): true}
	if len(got) != len(want) {
		t.Fatalf("pages = %#v, want %#v", got, want)
	}
	for _, rel := range got {
		if !want[rel] {
			t.Fatalf("%q is not a page", rel)
		}
	}
}

func TestLoadDocsBodiesOnAMissingTreeIsEmpty(t *testing.T) {
	bodies, err := LoadDocsBodies(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("LoadDocsBodies: %v", err)
	}
	if len(bodies) != 0 {
		t.Fatalf("bodies = %#v, want none", bodies)
	}
}

// -- FleetProject -------------------------------------------------------------

func TestFleetProjectLoadedTracksTheConfig(t *testing.T) {
	if !(FleetProject{Name: "x", Path: "/x", Config: config.Config{"a": int64(1)}}).Loaded() {
		t.Error("a project carrying a config did not report itself loaded")
	}
	if (FleetProject{Name: "x", Path: "/x", Error: "boom"}).Loaded() {
		t.Error("a project with no config reported itself loaded")
	}
}

func TestRetiredVersionKeys(t *testing.T) {
	if !reflect.DeepEqual(RetiredVersionKeys, []string{"indexed"}) {
		t.Fatalf("RetiredVersionKeys = %#v", RetiredVersionKeys)
	}
}
