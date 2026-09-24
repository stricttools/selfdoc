package unified

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// -- helpers --

// buildUnifiedFixture builds a unified fixture site and returns its output
// root, failing the test when the build does.
func buildUnifiedFixture(
	t *testing.T, projects []testproject.UnifiedProject, overrides map[string]any,
) (docsSiteDir, outputDir string) {
	t.Helper()
	docsSiteDir = testproject.MakeUnified(t, projects, overrides)
	if _, err := BuildUnified(docsSiteDir, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	return docsSiteDir, filepath.Join(docsSiteDir, ".stricttools", "docs-cache", "build")
}

// readFile reads a built file, failing the test when it is missing.
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// requireFile fails the test unless path names an existing regular file.
func requireFile(t *testing.T, path string) {
	t.Helper()
	if !isFile(path) {
		t.Fatalf("%s was not written", path)
	}
}

var (
	metaRefreshPattern = regexp.MustCompile(`content="0;url=([^"]*)"`)
	jsReplacePattern   = regexp.MustCompile(`window\.location\.replace\("([^"]*)"\)`)
	anchorPattern      = regexp.MustCompile(`<a href="([^"]*)">`)
	canonicalPattern   = regexp.MustCompile(`<link rel="canonical" href="([^"]*)">`)
)

// stubHops is every same-site hop a redirect stub emits.
//
// A stub states its target four times -- the meta refresh, the JavaScript
// replace, the anchor href and the anchor text -- and all four have to resolve
// identically, so the assertions run over the whole set rather than over one
// representative.
func stubHops(t *testing.T, document string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, pattern := range []*regexp.Regexp{metaRefreshPattern, jsReplacePattern, anchorPattern} {
		for _, match := range pattern.FindAllStringSubmatch(document, -1) {
			seen[match[1]] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("the stub emitted no hop at all")
	}
	hops := make([]string, 0, len(seen))
	for hop := range seen {
		hops = append(hops, hop)
	}
	sort.Strings(hops)
	return hops
}

// canonicalOf is the single rel=canonical URL a stub declares.
func canonicalOf(t *testing.T, document string) string {
	t.Helper()
	found := canonicalPattern.FindAllStringSubmatch(document, -1)
	if len(found) != 1 {
		t.Fatalf("expected one canonical, got %d", len(found))
	}
	return found[0][1]
}

// resolve joins a hop against the address the document was served from, the
// way a browser does.
func resolve(t *testing.T, servedAt, hop string) string {
	t.Helper()
	base, err := url.Parse(servedAt)
	if err != nil {
		t.Fatalf("parsing %s: %v", servedAt, err)
	}
	target, err := url.Parse(hop)
	if err != nil {
		t.Fatalf("parsing %s: %v", hop, err)
	}
	return base.ResolveReference(target).String()
}

// indexFragment is one indexed page, as Pagefind recorded it.
type indexFragment struct {
	URL     string              `json:"url"`
	Content string              `json:"content"`
	Filters map[string][]string `json:"filters"`
}

// indexFragments reads every fragment the indexer wrote under an output root.
//
// Each fragment file is a short binary prefix followed by the fragment's JSON
// object, gzip-compressed.
func indexFragments(t *testing.T, outputDir string) []indexFragment {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(outputDir, "pagefind", "fragment", "*.pf_fragment"))
	if err != nil {
		t.Fatalf("listing the index fragments: %v", err)
	}
	sort.Strings(paths)
	fragments := make([]indexFragment, 0, len(paths))
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			t.Fatalf("opening %s: %v", path, openErr)
		}
		reader, gzipErr := gzip.NewReader(file)
		if gzipErr != nil {
			file.Close()
			t.Fatalf("%s is not gzip: %v", path, gzipErr)
		}
		raw, readErr := io.ReadAll(reader)
		file.Close()
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		start := strings.Index(string(raw), "{")
		if start < 0 {
			t.Fatalf("%s carries no JSON object", path)
		}
		var fragment indexFragment
		if err := json.Unmarshal(raw[start:], &fragment); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

// twoProjects is the constituent list most of the build tests use.
var twoProjects = []testproject.UnifiedProject{
	{Name: "core", Language: "python"},
	{Name: "cli", Language: "python"},
}

// oneProject is the single-constituent list.
var oneProject = []testproject.UnifiedProject{{Name: "core", Language: "python"}}

// -- the config entry helpers --

func TestProjectSlug(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry map[string]any
		want  string
	}{
		{"an explicit slug wins", map[string]any{"path": "../core", "slug": "my-core"}, "my-core"},
		{"no slug derives from the path", map[string]any{"path": "../core"}, "core"},
		{"a trailing separator is dropped", map[string]any{"path": "../core/"}, "core"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := ProjectSlug(testCase.entry); got != testCase.want {
				t.Errorf("ProjectSlug(%v) = %q, want %q", testCase.entry, got, testCase.want)
			}
		})
	}
}

func TestProjectNavTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry map[string]any
		want  string
	}{
		{"an explicit title wins",
			map[string]any{"path": "../core", "nav_title": "Core Library"}, "Core Library"},
		{"no title title-cases the slug", map[string]any{"path": "../my-lib"}, "My Lib"},
		{"underscores separate words too", map[string]any{"path": "../my_lib"}, "My Lib"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := ProjectNavTitle(testCase.entry); got != testCase.want {
				t.Errorf("ProjectNavTitle(%v) = %q, want %q", testCase.entry, got, testCase.want)
			}
		})
	}
}

func TestResolveProjectPath(t *testing.T) {
	t.Parallel()

	t.Run("a path that names a directory resolves", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		docsSite := filepath.Join(root, "docs-site")
		testproject.MkdirAll(t, docsSite)
		testproject.MkdirAll(t, filepath.Join(root, "core"))

		resolved, err := ResolveProjectPath(map[string]any{"path": "../core"}, docsSite)
		if err != nil {
			t.Fatalf("ResolveProjectPath: %v", err)
		}
		if filepath.Base(resolved) != "core" {
			t.Errorf("resolved = %q, want a path ending in core", resolved)
		}
		if !isDir(resolved) {
			t.Errorf("resolved %q is not a directory", resolved)
		}
	})

	t.Run("a path that names nothing is refused", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		docsSite := filepath.Join(root, "docs-site")
		testproject.MkdirAll(t, docsSite)

		_, err := ResolveProjectPath(map[string]any{"path": "../nonexistent"}, docsSite)
		if err == nil {
			t.Fatal("a missing project path was accepted")
		}
		var configErr *config.ConfigError
		if !asConfigError(err, &configErr) {
			t.Fatalf("error is %T, want *config.ConfigError", err)
		}
		if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("error = %q, want it to say the path does not exist", err)
		}
	})
}

// -- the landing page --

func TestGenerateLandingPage(t *testing.T) {
	t.Parallel()

	cards := []projectCard{{
		Slug:        "core",
		NavTitle:    "Core Library",
		Description: "The core framework",
		Version:     "2.0.0",
		Home:        "core/",
	}}
	body := generateLandingPage(cards, "../../")

	for _, want := range []string{
		"project-grid", "Core Library", "The core framework", "v2.0.0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the landing body is missing %q:\n%s", want, body)
		}
	}
}

func TestGenerateLandingPageCardHopsOutToTheSiteRoot(t *testing.T) {
	t.Parallel()

	// A card addresses another mount, so it climbs to the root first.
	body := generateLandingPage([]projectCard{{
		Slug: "core", NavTitle: "Core", Version: "1.0.0", Home: "core/",
	}}, "../../")
	if !strings.Contains(body, `href="../../core/"`) {
		t.Errorf("the card does not hop out to the site root:\n%s", body)
	}
}

func TestGenerateLandingPageOmitsWhatAProjectDoesNotState(t *testing.T) {
	t.Parallel()

	body := generateLandingPage([]projectCard{{
		Slug: "core", NavTitle: "Core", Home: "core/",
	}}, "")
	if strings.Contains(body, "<p>") {
		t.Errorf("a project with no description still got a paragraph:\n%s", body)
	}
	if strings.Contains(body, "project-version") {
		t.Errorf("a project with no version still got a version badge:\n%s", body)
	}
}

// -- the whole unified build --

func TestBuildUnifiedProducesEveryMount(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, twoProjects, nil)

	if !isDir(output) {
		t.Fatalf("%s is not a directory", output)
	}
	// The docs-site's own content, and every constituent's.
	requireFile(t, filepath.Join(output, "common", "index.html"))
	requireFile(t, filepath.Join(output, "core", "index.html"))
	requireFile(t, filepath.Join(output, "cli", "index.html"))
}

func TestBuildUnifiedIndexesEveryProjectUnderOneIndex(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, twoProjects, nil)
	requireFile(t, filepath.Join(output, "pagefind", "pagefind-entry.json"))

	// One index over the whole site, with every constituent in it. The
	// docs-site's own cross-cutting pages carry the docs-site's name -- the
	// project facet is the project each page belongs to.
	projectNames := map[string]bool{}
	for _, fragment := range indexFragments(t, output) {
		for _, value := range fragment.Filters["project"] {
			projectNames[value] = true
		}
	}
	for _, want := range []string{"core", "cli", "docs-site"} {
		if !projectNames[want] {
			t.Errorf("the project facet is missing %q; it offers %v", want, sortedKeys(projectNames))
		}
	}
}

func TestBuildUnifiedRootRedirectPointsAtTheCommonMount(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, oneProject, nil)

	rootIndex := readFile(t, filepath.Join(output, "index.html"))
	if !strings.Contains(rootIndex, `href="common/"`) {
		t.Errorf("the root stub does not point at the common mount:\n%s", rootIndex)
	}
}

func TestBuildUnifiedRootRedirectCanonicalIsAbsolute(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, oneProject, nil)
	rootIndex := readFile(t, filepath.Join(output, "index.html"))

	if want := `<link rel="canonical" href="https://example.com/common/">`; !strings.Contains(rootIndex, want) {
		t.Errorf("the root stub is missing %q:\n%s", want, rootIndex)
	}
	for _, hop := range stubHops(t, rootIndex) {
		if got := resolve(t, "https://example.com/", hop); got != "https://example.com/common/" {
			t.Errorf("hop %q resolves to %q, want https://example.com/common/", hop, got)
		}
	}
}

func TestBuildUnifiedRootRedirectHopResolvesUnderASubpathBaseURL(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// A unified site served under a path prefix has to hop within that
	// prefix: an assembly serves each project under /<slug>/, and a
	// root-relative hop would leave the subtree and land on whatever the
	// assembly root serves.
	_, output := buildUnifiedFixture(t, oneProject, map[string]any{
		"base_url": "https://docs.example.com/proj",
	})
	rootIndex := readFile(t, filepath.Join(output, "index.html"))

	servedAt := "https://docs.example.com/proj/"
	canonical := canonicalOf(t, rootIndex)
	if canonical != "https://docs.example.com/proj/common/" {
		t.Errorf("canonical = %q, want https://docs.example.com/proj/common/", canonical)
	}
	for _, hop := range stubHops(t, rootIndex) {
		resolved := resolve(t, servedAt, hop)
		if !strings.Contains(resolved, "proj") {
			t.Errorf("hop %q escapes the project subtree: %q", hop, resolved)
		}
		if resolved != canonical {
			t.Errorf("hop %q resolves to %q, want %q", hop, resolved, canonical)
		}
	}
}

func TestBuildUnifiedCloudflareRuleStaysSiteAbsolute(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// _redirects is read at the deployed root, where there is no document
	// to resolve a relative target against, so its target is absolute.
	_, output := buildUnifiedFixture(t, oneProject, nil)

	redirects := readFile(t, filepath.Join(output, "_redirects"))
	if !strings.Contains(redirects, "/ /common/ 302\n") {
		t.Errorf("_redirects = %q, want it to carry \"/ /common/ 302\"", redirects)
	}
}

func TestBuildUnifiedLandingPageListsEveryProject(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, twoProjects, nil)

	landing := readFile(t, filepath.Join(output, "common", "projects", "index.html"))
	for _, want := range []string{"project-grid", "Core", "Cli"} {
		if !strings.Contains(landing, want) {
			t.Errorf("the landing page is missing %q:\n%s", want, landing)
		}
	}
}

func TestBuildUnifiedConstituentCarriesItsOwnContent(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, oneProject, nil)

	coreIndex := readFile(t, filepath.Join(output, "core", "index.html"))
	if !strings.Contains(strings.ToLower(coreIndex), "core") {
		t.Errorf("core's page does not name core:\n%s", coreIndex)
	}
}

func TestBuildUnifiedWritesTheSharedAssetsOnce(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	_, output := buildUnifiedFixture(t, oneProject, nil)

	requireFile(t, filepath.Join(output, "style.css"))
	requireFile(t, filepath.Join(output, "pagefind", "pagefind-entry.json"))
	requireFile(t, filepath.Join(output, "pagefind", "pagefind-ui.js"))

	// The one stylesheet the whole site is painted with carries the landing
	// page's card styles: there is no sheet of the landing page's own.
	stylesheet := readFile(t, filepath.Join(output, "style.css"))
	if !strings.Contains(stylesheet, ".project-grid") {
		t.Error("the shared stylesheet does not carry the project-card styles")
	}
}

func TestBuildUnifiedReportsEveryPathItWrote(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	docsSite := testproject.MakeUnified(t, twoProjects, nil)
	written, err := BuildUnified(docsSite, nil, "", false, effects.Unbound())
	if err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	output := filepath.Join(docsSite, ".stricttools", "docs-cache", "build")
	for _, rel := range []string{
		"index.html", "_redirects", "style.css",
		filepath.Join("common", "index.html"),
		filepath.Join("common", "projects", "index.html"),
		filepath.Join("core", "index.html"),
		filepath.Join("cli", "index.html"),
	} {
		if !written[filepath.Join(output, rel)] {
			t.Errorf("%s was not reported written", rel)
		}
	}
}

// -- refusals --

func TestBuildUnifiedRefusals(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("a directory that is not a project", func(t *testing.T) {
		_, err := BuildUnified(t.TempDir(), nil, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "No selfdoc.json found") {
			t.Fatalf("err = %v, want it to name the missing selfdoc.json", err)
		}
	})

	t.Run("a project with no unified section", func(t *testing.T) {
		dir := testproject.Make(t, nil)
		_, err := BuildUnified(dir, nil, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "No 'unified' section") {
			t.Fatalf("err = %v, want it to name the missing unified section", err)
		}
	})

	t.Run("an unknown theme", func(t *testing.T) {
		docsSite := testproject.MakeUnified(t, oneProject, nil)
		_, err := BuildUnified(docsSite, nil, "nosuchtheme", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "unknown theme 'nosuchtheme'") {
			t.Fatalf("err = %v, want it to name the unknown theme", err)
		}
		if !strings.Contains(err.Error(), "available themes:") {
			t.Errorf("err = %v, want it to list the available themes", err)
		}
	})

	t.Run("a config with no versions array", func(t *testing.T) {
		docsSite := testproject.MakeUnified(t, oneProject, nil)
		cfg := loadFixtureConfig(t, docsSite)
		delete(cfg, "versions")
		cfg["versions"] = nil
		_, err := BuildUnified(docsSite, cfg, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "requires 'versions' array") {
			t.Fatalf("err = %v, want it to require the versions array", err)
		}
	})

	t.Run("a config with no locales array", func(t *testing.T) {
		docsSite := testproject.MakeUnified(t, oneProject, nil)
		cfg := loadFixtureConfig(t, docsSite)
		cfg["locales"] = nil
		_, err := BuildUnified(docsSite, cfg, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "requires 'locales' array") {
			t.Fatalf("err = %v, want it to require the locales array", err)
		}
	})

	t.Run("a constituent path that names nothing", func(t *testing.T) {
		docsSite := testproject.MakeUnified(t, oneProject, map[string]any{
			"unified": map[string]any{
				"projects": []any{map[string]any{"path": "../nowhere"}},
			},
		})
		_, err := BuildUnified(docsSite, nil, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("err = %v, want it to name the missing constituent", err)
		}
	})

	t.Run("a constituent with no selfdoc.json", func(t *testing.T) {
		docsSite := testproject.MakeUnified(t, oneProject, nil)
		if err := os.Remove(filepath.Join(
			filepath.Dir(docsSite), "core", "selfdoc.json")); err != nil {
			t.Fatalf("removing core's config: %v", err)
		}
		_, err := BuildUnified(docsSite, nil, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "No selfdoc.json found in constituent project") {
			t.Fatalf("err = %v, want it to name the constituent with no config", err)
		}
	})

	t.Run("a docs directory that is missing", func(t *testing.T) {
		docsSite := testproject.MakeUnified(t, oneProject, nil)
		if err := os.RemoveAll(filepath.Join(docsSite, ".stricttools", "docs")); err != nil {
			t.Fatalf("removing the docs tree: %v", err)
		}
		_, err := BuildUnified(docsSite, nil, "", false, effects.Unbound())
		if err == nil || !strings.Contains(err.Error(), "Docs directory '.stricttools/docs' not found") {
			t.Fatalf("err = %v, want it to name the missing docs directory", err)
		}
	})
}

// loadFixtureConfig loads a fixture's config so a test can state a variant of
// it without rewriting the file.
func loadFixtureConfig(t *testing.T, dir string) config.Config {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	if cfg == nil {
		t.Fatalf("%s is not a selfdoc project", dir)
	}
	return cfg
}

// asConfigError reports whether err is a *config.ConfigError, assigning it to
// target when it is.
func asConfigError(err error, target **config.ConfigError) bool {
	configErr, ok := err.(*config.ConfigError)
	if ok {
		*target = configErr
	}
	return ok
}

func TestBuildUnifiedEmitsVersionFreePagesOnceAtTheirOwnMount(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// A page declaring "versioned = false" is emitted once, at the mount
	// with no version segment, while its neighbours are emitted per
	// version -- the superseded one under the archive prefix.
	docsSite := testproject.MakeUnified(t, oneProject, map[string]any{
		"version": "2.0.0",
		"versions": []any{
			map[string]any{"version": "1.0.0"},
			map[string]any{"version": "2.0.0"},
		},
	})
	testproject.WriteText(t,
		filepath.Join(projectDirOf(docsSite, "core"), ".stricttools", "docs", "about.md"),
		"+++\nversioned = false\n+++\n\n# About Core\n\nThe same at every version.\n")
	testproject.WriteText(t, filepath.Join(docsSite, ".stricttools", "docs", "policy.md"),
		"+++\nversioned = false\n+++\n\n# Policy\n\nThe site's own persistent page.\n")

	if _, err := BuildUnified(docsSite, nil, "", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	output := filepath.Join(docsSite, ".stricttools", "docs-cache", "build")

	requireFile(t, filepath.Join(output, "core", "about", "index.html"))
	requireFile(t, filepath.Join(output, "common", "policy", "index.html"))
	// The version-free page is emitted once: it never appears under the
	// archive prefix beside the superseded version's own pages.
	if isFile(filepath.Join(output, "core", "v", "1.0.0", "about", "index.html")) {
		t.Error("the version-free page was also emitted under the archive prefix")
	}
	requireFile(t, filepath.Join(output, "core", "v", "1.0.0", "index.html"))
}

func TestBuildUnifiedThemeOverrideDecidesTheSharedStylesheet(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// The theme argument overrides the config's for this build only, and
	// the whole unified site -- every mount -- is painted with it.
	docsSite := testproject.MakeUnified(t, oneProject, map[string]any{"theme": "minimal"})
	if _, err := BuildUnified(docsSite, nil, "clean", false, effects.Unbound()); err != nil {
		t.Fatalf("BuildUnified: %v", err)
	}
	output := filepath.Join(docsSite, ".stricttools", "docs-cache", "build")

	stylesheet := readFile(t, filepath.Join(output, "style.css"))
	if !strings.Contains(stylesheet, ".project-grid") {
		t.Error("the overridden stylesheet does not carry the project-card styles")
	}
	// Nothing is written back to the config: the override lives as long as
	// the call does.
	if got := loadFixtureConfig(t, docsSite)["theme"]; got != "minimal" {
		t.Errorf("the config's theme is now %v, want it left at minimal", got)
	}
}

// TestBuildUnifiedCreatesItsOutputThroughTheLayout pins that the unified
// build's output directory is created the way every other directory under the
// layout root is: through the ownership check that reads the directory's
// manifest. A raw mkdir here would write into a directory another tool owns.
func TestBuildUnifiedCreatesItsOutputThroughTheLayout(t *testing.T) {
	hygiene.Isolate(t)
	docsSite := testproject.MakeUnified(t, oneProject, nil)
	if err := os.Remove(layout.DirectoryManifestPath(docsSite, layout.DocsCacheName)); err != nil {
		t.Fatalf("removing the output directory's manifest: %v", err)
	}
	if err := os.RemoveAll(layout.Path(docsSite, layout.DocsCacheRel)); err != nil {
		t.Fatalf("removing the output tree: %v", err)
	}
	_, err := BuildUnified(docsSite, nil, "", false, effects.Unbound())
	if err == nil {
		t.Fatal("a build wrote into a directory carrying no manifest")
	}
	for _, want := range []string{
		layout.DirectoryManifestRel(layout.DocsCacheName), `owner = "selfdoc"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
	if _, statErr := os.Stat(layout.Path(docsSite, layout.OutputRel)); !os.IsNotExist(statErr) {
		t.Errorf("the output directory was created anyway (stat err = %v)", statErr)
	}
}
