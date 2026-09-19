package docs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/smm-h/stricttest/go/hygiene"
)

func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// makeConfig is the minimal loaded-config shape this package reads, with the
// given keys merged over it.
func makeConfig(overrides map[string]any) map[string]any {
	config := map[string]any{
		"source":      []any{},
		"docs":        ".stricttools/docs/",
		"output":      ".stricttools/docs-cache/build/",
		"description": "A described project.",
		"directives":  map[string]any{},
	}
	for key, value := range overrides {
		config[key] = value
	}
	return config
}

func resolveAll(t *testing.T, config map[string]any, docsDir, baseDir string, overlay map[string]string) map[string]Doc {
	t.Helper()
	all, err := ResolveAll(config, docsDir, baseDir, overlay, effects.Unbound())
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	return all
}

func keysOf(all map[string]Doc) []string {
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// -- ResolveMarkdown --------------------------------------------------------

// noopResolver answers every directive with a fixed string, so the parse and
// the line accounting can be tested without a project behind them.
func noopResolver(name string, attrs map[string]string, body []string) (string, error) {
	return "[" + name + "]", nil
}

func TestResolveMarkdownParsesAndCountsFrontmatter(t *testing.T) {
	isolate(t)
	cases := []struct {
		name             string
		content          string
		wantFrontmatter  map[string]any
		wantBody         string
		wantFrontmatterL int
	}{
		{
			name:             "no frontmatter",
			content:          "# Title\n\nBody.\n",
			wantFrontmatter:  map[string]any{},
			wantBody:         "# Title\n\nBody.\n",
			wantFrontmatterL: 0,
		},
		{
			name:             "fence plus one key",
			content:          "+++\ntitle = \"T\"\n+++\n# H\n",
			wantFrontmatter:  map[string]any{"title": "T"},
			wantBody:         "# H\n",
			wantFrontmatterL: 3,
		},
		{
			name:             "blank lines after the fence are consumed",
			content:          "+++\ntitle = \"T\"\n+++\n\n\n# H\n",
			wantFrontmatter:  map[string]any{"title": "T"},
			wantBody:         "# H\n",
			wantFrontmatterL: 5,
		},
		{
			name:             "empty frontmatter block",
			content:          "+++\n+++\nBody\n",
			wantFrontmatter:  map[string]any{},
			wantBody:         "Body\n",
			wantFrontmatterL: 2,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doc, err := ResolveMarkdown(testCase.content, "page.md", noopResolver, nil)
			if err != nil {
				t.Fatalf("ResolveMarkdown: %v", err)
			}
			if !reflect.DeepEqual(map[string]any(doc.Frontmatter), testCase.wantFrontmatter) {
				t.Errorf("frontmatter = %#v, want %#v", doc.Frontmatter, testCase.wantFrontmatter)
			}
			if doc.Raw != testCase.wantBody {
				t.Errorf("raw = %q, want %q", doc.Raw, testCase.wantBody)
			}
			if doc.Resolved != testCase.wantBody {
				t.Errorf("resolved = %q, want %q", doc.Resolved, testCase.wantBody)
			}
			if doc.FrontmatterLines != testCase.wantFrontmatterL {
				t.Errorf("frontmatterLines = %d, want %d", doc.FrontmatterLines, testCase.wantFrontmatterL)
			}
		})
	}
}

func TestResolveMarkdownResolvesDirectivesAndKeepsTheRawBody(t *testing.T) {
	isolate(t)
	content := "+++\ntitle = \"T\"\n+++\nBefore.\n\n:-: ref path=\"x\"\n"
	doc, err := ResolveMarkdown(content, "page.md", noopResolver, nil)
	if err != nil {
		t.Fatalf("ResolveMarkdown: %v", err)
	}
	if !strings.Contains(doc.Resolved, "[ref]") {
		t.Errorf("resolved = %q, want the resolver's answer in it", doc.Resolved)
	}
	if !strings.Contains(doc.Raw, ":-: ref") {
		t.Errorf("raw = %q, want the unresolved marker in it", doc.Raw)
	}
}

func TestResolveMarkdownRefusesAnUnknownDirectiveName(t *testing.T) {
	isolate(t)
	validNames := directives.NameSet{"ref": {}}
	_, err := ResolveMarkdown(":-: nope\n", "page.md", noopResolver, validNames)
	if err == nil {
		t.Fatal("want an error for a name the valid set does not carry")
	}
}

// -- ValidNames -------------------------------------------------------------

func TestValidNamesCarriesTheBuiltinsAndTheDeclaredCustomNames(t *testing.T) {
	isolate(t)
	config := makeConfig(map[string]any{
		"directives": map[string]any{"my-api": "scripts/api.py"},
	})
	valid, err := ValidNames(config)
	if err != nil {
		t.Fatalf("ValidNames: %v", err)
	}
	for _, name := range []string{"ref", "var", "table-commands", "my-api"} {
		if _, ok := valid[name]; !ok {
			t.Errorf("valid names missing %q", name)
		}
	}
	if _, ok := valid["nope"]; ok {
		t.Error("valid names carry an undeclared name")
	}
}

func TestValidNamesRefusesAnUngrammaticalCustomName(t *testing.T) {
	isolate(t)
	config := makeConfig(map[string]any{
		"directives": map[string]any{"1bad": "scripts/api.py"},
	})
	_, err := ValidNames(config)
	if err == nil {
		t.Fatal("want an error for a name that does not match the grammar")
	}
	if !strings.Contains(err.Error(), "1bad") {
		t.Errorf("error = %q, want the offending name in it", err)
	}
}

func TestValidNamesAcceptsAConfigWithNoDirectivesKey(t *testing.T) {
	isolate(t)
	valid, err := ValidNames(map[string]any{})
	if err != nil {
		t.Fatalf("ValidNames: %v", err)
	}
	if _, ok := valid["ref"]; !ok {
		t.Error("valid names missing the built-ins")
	}
}

// -- ResolveAll -------------------------------------------------------------

func TestResolveAllKeysPagesByTheirDocsRelativePath(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# Home\n")
	write(t, filepath.Join(base, ".stricttools", "docs", "api", "reference.md"), "# Reference\n")

	all := resolveAll(t, makeConfig(nil), "", base, nil)

	want := []string{"api/reference.md", "index.md"}
	if got := keysOf(all); !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

func TestResolveAllSkipsUnderscoreTemplatesAndNonMarkdown(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# Home\n")
	write(t, filepath.Join(base, ".stricttools", "docs", "_README.md"), "# Root template\n")
	write(t, filepath.Join(base, ".stricttools", "docs", "_partials", "_nav.md"), "# Nav\n")
	write(t, filepath.Join(base, ".stricttools", "docs", "style.css"), "body{}\n")

	all := resolveAll(t, makeConfig(nil), "", base, nil)

	if got := keysOf(all); !reflect.DeepEqual(got, []string{"index.md"}) {
		t.Errorf("keys = %v, want [index.md]", got)
	}
}

func TestResolveAllSkipsTheOutputDirectory(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# Home\n")
	// A previous build's artifacts, which the walk must not feed back in.
	write(t, filepath.Join(base, ".stricttools", "docs-cache", "build", "leftover.md"), "# Stale\n")
	write(t, filepath.Join(base, ".stricttools", "docs-cache", "build", "deep", "leftover.md"), "# Stale\n")

	config := makeConfig(map[string]any{"output": ".stricttools/docs-cache/build/"})
	all := resolveAll(t, config, "", base, nil)

	if got := keysOf(all); !reflect.DeepEqual(got, []string{"index.md"}) {
		t.Errorf("keys = %v, want [index.md]", got)
	}
}

// TestResolveAllSkipsAnOutputDirectoryWithoutTheUnderscore pins that the
// output directory is excluded because it IS the output directory, not
// because build output conventionally hides behind an underscore.
func TestResolveAllSkipsAnOutputDirectoryWithoutTheUnderscore(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# Home\n")
	write(t, filepath.Join(base, ".stricttools", "docs", "site", "leftover.md"), "# Stale\n")

	config := makeConfig(map[string]any{"output": ".stricttools/docs/site/"})
	all := resolveAll(t, config, "", base, nil)

	if got := keysOf(all); !reflect.DeepEqual(got, []string{"index.md"}) {
		t.Errorf("keys = %v, want [index.md]", got)
	}
}

func TestResolveAllReadsAnExplicitDocsDirectory(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# Home\n")
	write(t, filepath.Join(base, "other", "page.md"), "# Other\n")

	all := resolveAll(t, makeConfig(nil), filepath.Join(base, "other"), base, nil)

	if got := keysOf(all); !reflect.DeepEqual(got, []string{"page.md"}) {
		t.Errorf("keys = %v, want [page.md]", got)
	}
}

func TestResolveAllOnAMissingDocsDirectoryIsEmpty(t *testing.T) {
	isolate(t)
	base := t.TempDir()

	all := resolveAll(t, makeConfig(nil), "", base, nil)

	if len(all) != 0 {
		t.Errorf("keys = %v, want none", keysOf(all))
	}
}

func TestResolveAllOverlayAddsAndReplaces(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# On disk\n")

	overlay := map[string]string{
		"index.md":      "# From the overlay\n",
		"blog/hello.md": "+++\ntitle = \"Hello\"\n+++\nHi.\n",
	}
	all := resolveAll(t, makeConfig(nil), "", base, overlay)

	want := []string{"blog/hello.md", "index.md"}
	if got := keysOf(all); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if all["index.md"].Resolved != "# From the overlay\n" {
		t.Errorf("index.md = %q, want the overlay's content", all["index.md"].Resolved)
	}
	if all["blog/hello.md"].Frontmatter["title"] != "Hello" {
		t.Errorf("overlay frontmatter = %#v, want title Hello", all["blog/hello.md"].Frontmatter)
	}
}

func TestResolveAllResolvesDirectivesAgainstTheProject(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\n+++\n"+`Project: :-: var key="project.description"`+"\n")

	all := resolveAll(t, makeConfig(nil), "", base, nil)

	doc := all["index.md"]
	if !strings.Contains(doc.Resolved, "A described project.") {
		t.Errorf("resolved = %q, want the config's description in it", doc.Resolved)
	}
	if !strings.Contains(doc.Raw, ":-: var") {
		t.Errorf("raw = %q, want the unresolved marker in it", doc.Raw)
	}
	if doc.FrontmatterLines != 3 {
		t.Errorf("frontmatterLines = %d, want 3", doc.FrontmatterLines)
	}
}

func TestResolveAllRefusesAnUngrammaticalCustomDirectiveName(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, ".stricttools", "docs", "index.md"), "# Home\n")

	config := makeConfig(map[string]any{
		"directives": map[string]any{"-bad": "scripts/api.py"},
	})
	if _, err := ResolveAll(config, "", base, nil, effects.Unbound()); err == nil {
		t.Fatal("want an error for a name that does not match the grammar")
	}
}

// -- Conversions ------------------------------------------------------------

func TestConversionsNarrowToTheConsumersOwnShapes(t *testing.T) {
	isolate(t)
	doc := Doc{
		Frontmatter:      map[string]any{"title": "T"},
		Resolved:         "resolved body",
		Raw:              "raw body",
		FrontmatterLines: 3,
	}
	all := map[string]Doc{"index.md": doc}

	manifestDocs := ManifestDocs(all)
	if manifestDocs["index.md"].Resolved != "resolved body" {
		t.Errorf("manifest resolved = %q", manifestDocs["index.md"].Resolved)
	}
	if manifestDocs["index.md"].Raw != "raw body" {
		t.Errorf("manifest raw = %q", manifestDocs["index.md"].Raw)
	}
	if manifestDocs["index.md"].Frontmatter["title"] != "T" {
		t.Errorf("manifest frontmatter = %#v", manifestDocs["index.md"].Frontmatter)
	}

	stalenessDocs := StalenessDocs(all)
	if stalenessDocs["index.md"].Raw != "raw body" {
		t.Errorf("staleness raw = %q", stalenessDocs["index.md"].Raw)
	}
	if stalenessDocs["index.md"].Frontmatter["title"] != "T" {
		t.Errorf("staleness frontmatter = %#v", stalenessDocs["index.md"].Frontmatter)
	}
}

// TestResolveAllMergesTheTwoDocsRoots pins the merge the layout's two docs
// roots need: a page keeps the address it had whichever root it is authored
// in, and the walk reads both.
func TestResolveAllMergesTheTwoDocsRoots(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, layout.DocsRel, "index.md"), "# Home\n")
	write(t, filepath.Join(base, layout.GeneratedPagesRel, "api.md"),
		"# API\n"+layout.GeneratedMarkerPrefix+", do not edit -->\n")
	write(t, filepath.Join(base, layout.GeneratedPagesRel, "internal", "deep.md"), "# Deep\n")

	all := resolveAll(t, makeConfig(nil), "", base, nil)

	if got := keysOf(all); !reflect.DeepEqual(got, []string{"api.md", "index.md", "internal/deep.md"}) {
		t.Errorf("keys = %v, want the pages of both roots", got)
	}
}

// TestResolveAllRefusesTwoPagesAtOneAddress pins the collision refusal: the
// two roots share one URL namespace, so a generated page and a handwritten one
// cannot both claim an address.
func TestResolveAllRefusesTwoPagesAtOneAddress(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	handwritten := filepath.Join(base, layout.DocsRel, "guide.md")
	generated := filepath.Join(base, layout.GeneratedPagesRel, "guide.md")
	write(t, handwritten, "# Guide\n")
	write(t, generated, "# Guide\n"+layout.GeneratedMarkerPrefix+", do not edit -->\n")

	_, err := ResolveAll(makeConfig(nil), "", base, nil, effects.Unbound())
	if err == nil {
		t.Fatal("two pages claiming one address were accepted")
	}
	var collision *CollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("err = %T (%v), want a *CollisionError", err, err)
	}
	for _, want := range []string{"guide.md", handwritten, generated} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestFindPageLooksInBothRoots pins the lookup every caller that reads a page
// off disk goes through: a page is found at its docs-relative address whether
// it is authored in the handwritten root or written into the generated one,
// and a page in neither is reported absent.
func TestFindPageLooksInBothRoots(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	handwritten := filepath.Join(base, layout.DocsRel, "guide.md")
	generated := filepath.Join(base, layout.GeneratedPagesRel, "cli-run.md")
	write(t, handwritten, "# Guide\n")
	write(t, generated, "# Run\n")
	if err := os.MkdirAll(filepath.Join(base, layout.DocsRel, "section"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	config := makeConfig(nil)

	if got, ok := FindPage(config, "", base, "guide.md"); !ok || got != handwritten {
		t.Errorf("FindPage(guide.md) = %q, %v; want %q, true", got, ok, handwritten)
	}
	if got, ok := FindPage(config, "", base, "cli-run.md"); !ok || got != generated {
		t.Errorf("FindPage(cli-run.md) = %q, %v; want %q, true", got, ok, generated)
	}
	if got, ok := FindPage(config, "", base, "absent.md"); ok {
		t.Errorf("FindPage(absent.md) = %q, true; want absent", got)
	}
	if got, ok := FindPage(config, "", base, "section"); ok {
		t.Errorf("FindPage on a directory = %q, true; want absent", got)
	}
}

// TestRootsNamesTheHandwrittenRootFirst pins the order [ResolveAll] walks, and
// that an explicit handwritten root overrides the config's declaration.
func TestRootsNamesTheHandwrittenRootFirst(t *testing.T) {
	base := t.TempDir()
	want := []string{
		filepath.Join(base, layout.DocsRel),
		filepath.Join(base, layout.GeneratedPagesRel),
	}
	if got := Roots(makeConfig(nil), "", base); !reflect.DeepEqual(got, want) {
		t.Errorf("Roots = %v, want %v", got, want)
	}

	explicit := filepath.Join(base, "elsewhere")
	want[0] = explicit
	if got := Roots(makeConfig(nil), explicit, base); !reflect.DeepEqual(got, want) {
		t.Errorf("Roots with an explicit docs dir = %v, want %v", got, want)
	}
}
