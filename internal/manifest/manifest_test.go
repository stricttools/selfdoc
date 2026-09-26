// Tests for the manifest: the slug and title derivations, what a generated
// manifest records, the recorded heading anchors, the idempotency skip, the
// tolerant reader, the read out of git, and the document's recorded bytes.
//
// The bytes in testdata were written by the Python implementation itself,
// through scripts/record_staleness_manifest_bytes.py, over the same input the
// byte-parity test below rebuilds in Go.
package manifest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/testisolation/go/hygiene"
)

// pageWithDupes carries a repeated heading, a deeper one, and a "#" line
// inside a fence -- the three things heading anchors have to get right.
const pageWithDupes = "# Guide\n" +
	"\n" +
	"Intro.\n" +
	"\n" +
	"## Setup\n" +
	"\n" +
	"One.\n" +
	"\n" +
	"## Setup\n" +
	"\n" +
	"Two.\n" +
	"\n" +
	"### Deeper\n" +
	"\n" +
	"```python\n" +
	"# not a heading\n" +
	"```\n"

// baseConfig is the project config the generation tests start from.
func baseConfig() map[string]any {
	return map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"author":        map[string]any{"name": "Test Author", "url": "https://author.example"},
		"search_engine": "pagefind",
		"version":       "1.0.0",
	}
}

// basePages is one page in the shape the docs resolution returns.
func basePages() map[string]Doc {
	return map[string]Doc{
		"index.md": {
			Frontmatter: util.Frontmatter{"title": "Home"},
			Resolved:    "resolved content",
			Raw:         "# Home\nWelcome",
		},
	}
}

// generate runs Generate with the defaults the Python's keyword arguments
// supplied, and fails the test on an error.
func generate(t *testing.T, projectConfig map[string]any, pages map[string]Doc, posts []Post, dirPath string) *Manifest {
	t.Helper()
	manifest, err := Generate(projectConfig, dirPath, pages, posts, DefaultOutputName, effects.Unbound())
	if err != nil {
		t.Fatalf("generating the manifest: %v", err)
	}
	return manifest
}

// readDocument reads the written manifest as a generic document.
func readDocument(t *testing.T, dirPath string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dirPath, "stricttools", ".docs-state", DefaultOutputName))
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parsing the manifest: %v", err)
	}
	return document
}

// -- ToKebab ----------------------------------------------------------------

func TestToKebab(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "spaces become hyphens and the result is lowercased", in: "My Project", want: "my-project"},
		{name: "underscores become hyphens", in: "my_cool_project", want: "my-cool-project"},
		{name: "other non-alphanumerics are dropped", in: "hello@world!v2", want: "helloworldv2"},
		{name: "runs of hyphens collapse", in: "a - - b", want: "a-b"},
		{name: "the ends are trimmed", in: " -hello- ", want: "hello"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ToKebab(test.in); got != test.want {
				t.Errorf("ToKebab(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// -- extractTitle -----------------------------------------------------------

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter util.Frontmatter
		content     string
		want        string
	}{
		{
			name:        "the frontmatter title wins",
			frontmatter: util.Frontmatter{"title": "My Page"},
			content:     "# Heading\nBody",
			want:        "My Page",
		},
		{
			name:        "the first heading is the fallback",
			frontmatter: util.Frontmatter{},
			content:     "# First Heading\nSome text",
			want:        "First Heading",
		},
		{
			name:        "a page with neither has no title",
			frontmatter: util.Frontmatter{},
			content:     "Just some plain text\nNo headings here",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractTitle(test.frontmatter, test.content); got != test.want {
				t.Errorf("extracted %q, want %q", got, test.want)
			}
		})
	}
}

// -- Generate ---------------------------------------------------------------

func TestGenerateRecordsTheProjectsFacts(t *testing.T) {
	base := testproject.Dir(t)
	manifest := generate(t, baseConfig(), basePages(), nil, base)
	if manifest.SchemaVersion != 1 {
		t.Errorf("schema version %d, want 1", manifest.SchemaVersion)
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("version %q, want 1.0.0", manifest.Version)
	}
	if manifest.BaseURL != "https://example.com" {
		t.Errorf("base URL %q", manifest.BaseURL)
	}
	if manifest.Language != "python" {
		t.Errorf("language %q, want python", manifest.Language)
	}
	if manifest.Theme != DefaultTheme {
		t.Errorf("theme %q, want the default %q", manifest.Theme, DefaultTheme)
	}
}

func TestGenerateNameAndSlug(t *testing.T) {
	// A nil wantName/wantSlug means "the directory's own name", which is only
	// known once the case has its temporary directory.
	tests := []struct {
		name        string
		configure   func(map[string]any)
		wantName    string
		wantSlug    string
		fromDirName bool
	}{
		{
			name:      "the configured name and its kebab-cased slug",
			configure: func(c map[string]any) { c["name"] = "My Project" },
			wantName:  "My Project",
			wantSlug:  "my-project",
		},
		{
			name: "the topology slug wins over the derived one",
			configure: func(c map[string]any) {
				c["name"] = "My Project"
				c["topology"] = map[string]any{"slug": "custom-slug"}
			},
			wantName: "My Project",
			wantSlug: "custom-slug",
		},
		{
			name:        "with no configured name, the directory names the project",
			configure:   func(map[string]any) {},
			fromDirName: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := testproject.Dir(t)
			wantName, wantSlug := test.wantName, test.wantSlug
			if test.fromDirName {
				wantName = filepath.Base(base)
				wantSlug = ToKebab(wantName)
			}
			projectConfig := baseConfig()
			test.configure(projectConfig)
			manifest := generate(t, projectConfig, basePages(), nil, base)
			if manifest.Name != wantName {
				t.Errorf("name %q, want %q", manifest.Name, wantName)
			}
			if manifest.Slug != wantSlug {
				t.Errorf("slug %q, want %q", manifest.Slug, wantSlug)
			}
		})
	}
}

func TestGeneratePagesAreSortedAndTyped(t *testing.T) {
	pages := map[string]Doc{
		"guide.md": {
			Frontmatter: util.Frontmatter{"title": "Guide", "type": "tutorial"},
			Resolved:    "resolved",
			Raw:         "# Guide\nText",
		},
		"api.md": {
			Frontmatter: util.Frontmatter{},
			Resolved:    "resolved",
			Raw:         "# API Reference\nStuff",
		},
	}
	manifest := generate(t, baseConfig(), pages, nil, testproject.Dir(t))
	want := []Page{
		{Path: "api.md", Title: "API Reference", Type: "doc", Headings: []Heading{}},
		{Path: "guide.md", Title: "Guide", Type: "tutorial", Headings: []Heading{}},
	}
	if !reflect.DeepEqual(manifest.Pages, want) {
		t.Errorf("recorded %+v, want %+v", manifest.Pages, want)
	}
}

func TestGeneratePosts(t *testing.T) {
	posts := []Post{{
		Path: "blog/first.md", Title: "First Post", Date: "2026-01-01",
		Slug: "first", Tags: []string{"news"},
	}}
	manifest := generate(t, baseConfig(), basePages(), posts, testproject.Dir(t))
	if !reflect.DeepEqual(manifest.Posts, posts) {
		t.Errorf("recorded %+v, want %+v", manifest.Posts, posts)
	}

	none := generate(t, baseConfig(), basePages(), nil, testproject.Dir(t))
	if len(none.Posts) != 0 {
		t.Errorf("recorded %+v for a project with no posts", none.Posts)
	}
}

func TestGenerateWritesTheFile(t *testing.T) {
	base := testproject.Dir(t)
	generate(t, baseConfig(), basePages(), nil, base)
	document := readDocument(t, base)
	if document["schema_version"] != float64(1) {
		t.Errorf("schema_version is %v", document["schema_version"])
	}
	if document["version"] != "1.0.0" {
		t.Errorf("version is %v", document["version"])
	}
	if document["base_url"] != "https://example.com" {
		t.Errorf("base_url is %v", document["base_url"])
	}
	if _, ok := document["pages"].([]any); !ok {
		t.Errorf("pages is %T, want a list", document["pages"])
	}
	if _, ok := document["posts"].([]any); !ok {
		t.Errorf("posts is %T, want a list", document["posts"])
	}
}

// TestGenerateSkipsTheWriteWhenOnlyTheTimestampWouldChange is why a gen over
// untouched content does not dirty the working tree.
func TestGenerateSkipsTheWriteWhenOnlyTheTimestampWouldChange(t *testing.T) {
	base := testproject.Dir(t)
	path := filepath.Join(base, "stricttools", ".docs-state", DefaultOutputName)

	generate(t, baseConfig(), basePages(), nil, base)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the first manifest: %v", err)
	}

	generate(t, baseConfig(), basePages(), nil, base)
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the second manifest: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("the manifest was rewritten:\n%s\nwas\n%s", second, first)
	}
}

func TestGenerateRewritesWhenTheContentChanges(t *testing.T) {
	base := testproject.Dir(t)
	generate(t, baseConfig(), basePages(), nil, base)
	first := readDocument(t, base)

	pages := basePages()
	pages["new.md"] = Doc{
		Frontmatter: util.Frontmatter{"title": "New"},
		Resolved:    "resolved",
		Raw:         "# New\nBody",
	}
	generate(t, baseConfig(), pages, nil, base)
	second := readDocument(t, base)

	if len(second["pages"].([]any)) != len(first["pages"].([]any))+1 {
		t.Errorf("the new page did not reach the file")
	}
	if second["last_gen"] == first["last_gen"] {
		t.Error("the timestamp was not advanced by a real content change")
	}
}

// -- Recorded heading anchors -----------------------------------------------

func TestPagesCarryTheirHeadingAnchors(t *testing.T) {
	base := testproject.Dir(t)
	pages := map[string]Doc{
		"guide.md": {Frontmatter: util.Frontmatter{}, Resolved: pageWithDupes, Raw: pageWithDupes},
	}
	manifest := generate(t, map[string]any{"name": "proj"}, pages, nil, base)
	want := []Heading{
		{Level: 1, Text: "Guide", Anchor: "guide"},
		{Level: 2, Text: "Setup", Anchor: "setup"},
		{Level: 2, Text: "Setup", Anchor: "setup-1"},
		{Level: 3, Text: "Deeper", Anchor: "deeper"},
	}
	if !reflect.DeepEqual(manifest.Pages[0].Headings, want) {
		t.Errorf("recorded %+v, want %+v", manifest.Pages[0].Headings, want)
	}
	for _, heading := range manifest.Pages[0].Headings {
		if heading.Text == "not a heading" {
			t.Error("a \"#\" line inside a fence was recorded as a heading")
		}
	}
	// The schema version does not move: headings are an additive field, which
	// the tolerant reader contract already covers.
	if readDocument(t, base)["schema_version"] != float64(1) {
		t.Error("an additive field moved the schema version")
	}
}

func TestThePageTitleAnchorFollowsTheFrontmatterTitle(t *testing.T) {
	// The first H1 is rendered from the page title, so its anchor is too.
	pages := map[string]Doc{
		"guide.md": {
			Frontmatter: util.Frontmatter{"title": "Getting Started"},
			Resolved:    pageWithDupes,
			Raw:         pageWithDupes,
		},
	}
	manifest := generate(t, map[string]any{"name": "proj"}, pages, nil, testproject.Dir(t))
	want := Heading{Level: 1, Text: "Guide", Anchor: "getting-started"}
	if manifest.Pages[0].Headings[0] != want {
		t.Errorf("recorded %+v, want %+v", manifest.Pages[0].Headings[0], want)
	}
}

func TestAPageWithNoHeadingsRecordsAnEmptyList(t *testing.T) {
	base := testproject.Dir(t)
	pages := map[string]Doc{
		"guide.md": {Frontmatter: util.Frontmatter{}, Resolved: "Just a paragraph.\n", Raw: "Just a paragraph.\n"},
	}
	generate(t, map[string]any{"name": "proj"}, pages, nil, base)
	document := readDocument(t, base)
	headings := document["pages"].([]any)[0].(map[string]any)["headings"]
	if list, ok := headings.([]any); !ok || len(list) != 0 {
		t.Errorf("recorded %v, want an empty list", headings)
	}
}

// -- The document's bytes ---------------------------------------------------

// TestTheManifestsBytesAreThePythonsBytes is the parity check that keeps the
// port from rewriting every committed manifest in the fleet: the key order,
// the two-space indent, the empty-list spelling and the ensure_ascii
// escaping are all the Python's.
func TestTheManifestsBytesAreThePythonsBytes(t *testing.T) {
	recorded, err := os.ReadFile(filepath.Join("testdata", "python_manifest.json"))
	if err != nil {
		t.Fatalf("reading the recorded bytes: %v", err)
	}
	// The recorded document's own timestamp, so the comparison covers every
	// byte including that line.
	var asDecoded map[string]any
	if err := json.Unmarshal(recorded, &asDecoded); err != nil {
		t.Fatalf("parsing the recorded bytes: %v", err)
	}
	lastGen, ok := asDecoded["last_gen"].(string)
	if !ok {
		t.Fatalf("the recorded bytes carry no last_gen")
	}

	manifest := &Manifest{
		SchemaVersion: 1,
		Name:          "Récord Project",
		Slug:          "record-project",
		Version:       "1.2.3",
		Description:   `A project with a "quoted" description — and an em dash.`,
		Language:      "python",
		BaseURL:       "https://example.com",
		Pages: []Page{
			{Path: "api.md", Title: "API Réference", Type: "doc", Headings: []Heading{}},
			{Path: "guide.md", Title: "Guide", Type: "tutorial", Headings: []Heading{
				{Level: 1, Text: "Guide", Anchor: "guide"},
				{Level: 2, Text: "Setup", Anchor: "setup"},
				{Level: 2, Text: "Setup", Anchor: "setup-1"},
				{Level: 3, Text: "Déeper", Anchor: "deeper"},
			}},
		},
		Posts: []Post{
			{
				Path: "posts/hello.md", Title: "Hello, wörld", Date: "2026-01-02",
				Slug: "hello-world", Tags: []string{"news", "release"},
			},
			{
				Path: "posts/bare.md", Title: "Bare", Date: "2026-01-03",
				Slug: "bare", Tags: []string{},
			},
		},
		LastGen: lastGen,
		Theme:   "brutalist",
	}
	if got := string(encode(manifest.document())); got != string(recorded) {
		t.Errorf("encoded\n%s\nwant\n%s", got, recorded)
	}
}

// TestGenerateProducesTheRecordedDocument builds the same manifest through
// Generate, from the same inputs the recorder handed the Python, and compares
// everything but the timestamp.
func TestGenerateProducesTheRecordedDocument(t *testing.T) {
	recorded, err := os.ReadFile(filepath.Join("testdata", "python_manifest.json"))
	if err != nil {
		t.Fatalf("reading the recorded bytes: %v", err)
	}
	guide := "# Guide\n\nIntro.\n\n## Setup\n\nOne.\n\n## Setup\n\nTwo.\n\n### Déeper\n\n```python\n# not a heading\n```\n"
	projectConfig := map[string]any{
		"name":        "Récord Project",
		"topology":    map[string]any{"slug": "record-project"},
		"version":     "1.2.3",
		"description": `A project with a "quoted" description — and an em dash.`,
		"source":      []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":    "https://example.com",
		"theme":       "brutalist",
	}
	pages := map[string]Doc{
		"guide.md": {Frontmatter: util.Frontmatter{"type": "tutorial"}, Resolved: guide, Raw: guide},
		"api.md": {
			Frontmatter: util.Frontmatter{"title": "API Réference"},
			Resolved:    "Just a paragraph.\n",
			Raw:         "Just a paragraph.\n",
		},
	}
	posts := []Post{
		{
			Path: "posts/hello.md", Title: "Hello, wörld", Date: "2026-01-02",
			Slug: "hello-world", Tags: []string{"news", "release"},
		},
		{Path: "posts/bare.md", Title: "Bare", Date: "2026-01-03", Slug: "bare"},
	}
	base := testproject.Dir(t)
	generate(t, projectConfig, pages, posts, base)
	written, err := os.ReadFile(filepath.Join(base, "stricttools", ".docs-state", DefaultOutputName))
	if err != nil {
		t.Fatalf("reading the written manifest: %v", err)
	}
	if got, want := withoutTimestamp(string(written)), withoutTimestamp(string(recorded)); got != want {
		t.Errorf("wrote\n%s\nwant\n%s", got, want)
	}
}

// withoutTimestamp drops the last_gen line, which is the one line two runs
// never agree on.
func withoutTimestamp(document string) string {
	kept := []string{}
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `"last_gen"`) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// -- Compat -----------------------------------------------------------------

func TestCompatIgnoresUnknownKeys(t *testing.T) {
	manifest, err := Compat(map[string]any{
		"schema_version":  int64(1),
		"name":            "compat-test",
		"future_field":    "should be ignored",
		"another_unknown": []any{int64(1), int64(2), int64(3)},
	}, "")
	if err != nil {
		t.Fatalf("reading a document with unknown keys: %v", err)
	}
	if manifest.Name != "compat-test" || manifest.SchemaVersion != 1 {
		t.Errorf("read %+v", manifest)
	}
}

func TestCompatDefaults(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
	}{
		{name: "only a schema version", data: map[string]any{"schema_version": int64(1)}},
		{name: "an empty document", data: map[string]any{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := Compat(test.data, "")
			if err != nil {
				t.Fatalf("reading a minimal document: %v", err)
			}
			if manifest.SchemaVersion != 1 {
				t.Errorf("schema version %d, want the default 1", manifest.SchemaVersion)
			}
			for field, got := range map[string]string{
				"name": manifest.Name, "slug": manifest.Slug, "version": manifest.Version,
				"description": manifest.Description, "language": manifest.Language,
				"base_url": manifest.BaseURL, "last_gen": manifest.LastGen,
				"theme": manifest.Theme,
			} {
				if got != "" {
					t.Errorf("%s defaulted to %q", field, got)
				}
			}
			if len(manifest.Pages) != 0 || len(manifest.Posts) != 0 {
				t.Errorf("pages %v, posts %v", manifest.Pages, manifest.Posts)
			}
		})
	}
}

func TestCompatRefusesAFutureSchema(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "with no source named",
			want: "Unsupported manifest schema_version 2 (max supported: 1)",
		},
		{
			name:   "with a source named",
			source: "git HEAD",
			want:   "Unsupported manifest schema_version 2 in git HEAD (max supported: 1)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Compat(map[string]any{"schema_version": int64(2)}, test.source)
			if err == nil {
				t.Fatal("a future schema version was accepted")
			}
			if err.Error() != test.want {
				t.Errorf("refused with %q, want %q", err, test.want)
			}
		})
	}
}

func TestCompatReadsEveryField(t *testing.T) {
	manifest, err := Compat(map[string]any{
		"schema_version": int64(1),
		"name":           "full",
		"slug":           "full-slug",
		"version":        "2.0.0",
		"description":    "Full test",
		"language":       "go",
		"base_url":       "https://full.test",
		"pages":          []any{map[string]any{"path": "index.md"}},
		"posts":          []any{map[string]any{"slug": "hello"}},
		"last_gen":       "2026-07-05T00:00:00Z",
		"theme":          "dark",
	}, "")
	if err != nil {
		t.Fatalf("reading a full document: %v", err)
	}
	want := &Manifest{
		SchemaVersion: 1, Name: "full", Slug: "full-slug", Version: "2.0.0",
		Description: "Full test", Language: "go", BaseURL: "https://full.test",
		Pages:   []Page{{Path: "index.md", Headings: []Heading{}}},
		Posts:   []Post{{Slug: "hello", Tags: []string{}}},
		LastGen: "2026-07-05T00:00:00Z",
		Theme:   "dark",
	}
	if !reflect.DeepEqual(manifest, want) {
		t.Errorf("read %+v, want %+v", manifest, want)
	}
}

// -- Load -------------------------------------------------------------------

func TestLoad(t *testing.T) {
	base := testproject.Dir(t)
	write := func(name, document string) string {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
			t.Fatalf("writing the manifest: %v", err)
		}
		return path
	}

	t.Run("a full manifest", func(t *testing.T) {
		path := write("full.json", `{
			"schema_version": 1, "name": "test-project", "slug": "test-project",
			"version": "2.0.0", "description": "A test", "language": "python",
			"base_url": "https://test.com",
			"pages": [{"path": "index.md", "title": "Home", "type": "doc"}],
			"posts": [], "last_gen": "2026-01-01T00:00:00+00:00"
		}`)
		manifest, err := Load(path)
		if err != nil {
			t.Fatalf("loading the manifest: %v", err)
		}
		if manifest == nil {
			t.Fatal("loaded nothing from a manifest that exists")
		}
		if manifest.Name != "test-project" || manifest.Version != "2.0.0" ||
			manifest.Language != "python" || len(manifest.Pages) != 1 {
			t.Errorf("loaded %+v", manifest)
		}
	})

	t.Run("an absent file", func(t *testing.T) {
		manifest, err := Load(filepath.Join(base, "nonexistent.json"))
		if err != nil {
			t.Fatalf("loading an absent manifest: %v", err)
		}
		if manifest != nil {
			t.Errorf("loaded %+v from an absent file", manifest)
		}
	})

	t.Run("a document carrying unknown keys", func(t *testing.T) {
		path := write("unknown.json", `{"schema_version": 1, "name": "x",
			"totally_unknown_key": "value", "nested_unknown": {"deep": {"value": 42}}}`)
		manifest, err := Load(path)
		if err != nil {
			t.Fatalf("loading a tolerant read: %v", err)
		}
		if manifest == nil || manifest.Name != "x" {
			t.Errorf("loaded %+v", manifest)
		}
	})

	t.Run("a schema version below the supported one is accepted", func(t *testing.T) {
		path := write("legacy.json", `{"schema_version": 0, "name": "legacy"}`)
		manifest, err := Load(path)
		if err != nil {
			t.Fatalf("loading a v0 manifest: %v", err)
		}
		if manifest == nil || manifest.SchemaVersion != 0 {
			t.Errorf("loaded %+v", manifest)
		}
	})

	t.Run("a future schema version is refused", func(t *testing.T) {
		path := write("future.json", `{"schema_version": 2, "name": "future"}`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("a future schema version was accepted")
		}
		if !strings.Contains(err.Error(), "Unsupported manifest schema_version 2") {
			t.Errorf("refused with %q", err)
		}
	})
}

// -- LoadFromGit ------------------------------------------------------------

func TestLoadFromGit(t *testing.T) {
	requireGit(t)
	hygiene.Isolate(t)
	handle := effects.Unbound()

	t.Run("a directory that is not a repository", func(t *testing.T) {
		manifest, err := LoadFromGit(testproject.Dir(t), handle)
		if err != nil {
			t.Fatalf("reading outside a repository: %v", err)
		}
		if manifest != nil {
			t.Errorf("read %+v from a directory with no repository", manifest)
		}
	})

	t.Run("a repository with no commits", func(t *testing.T) {
		base := testproject.Dir(t)
		run(t, base, "git", "init", "--quiet")
		manifest, err := LoadFromGit(base, handle)
		if err != nil {
			t.Fatalf("reading a repository with no commits: %v", err)
		}
		if manifest != nil {
			t.Errorf("read %+v from a repository with no commits", manifest)
		}
	})

	t.Run("a manifest that was never committed", func(t *testing.T) {
		base := testproject.Dir(t)
		run(t, base, "git", "init", "--quiet")
		if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("hi\n"), 0o644); err != nil {
			t.Fatalf("writing a file to commit: %v", err)
		}
		run(t, base, "git", "add", "README.md")
		run(t, base, "git", "commit", "--quiet", "-m", "first")
		manifest, err := LoadFromGit(base, handle)
		if err != nil {
			t.Fatalf("reading an uncommitted manifest: %v", err)
		}
		if manifest != nil {
			t.Errorf("read %+v for a manifest that was never committed", manifest)
		}
	})

	t.Run("the committed manifest, not the working tree's", func(t *testing.T) {
		base := testproject.Dir(t)
		run(t, base, "git", "init", "--quiet")
		path := filepath.Join(base, "stricttools", ".docs-state", DefaultOutputName)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating .selfdoc: %v", err)
		}
		committed := `{"schema_version": 1, "name": "committed",
			"posts": [{"path": "posts/hello.md", "slug": "hello-world"}]}`
		if err := os.WriteFile(path, []byte(committed), 0o644); err != nil {
			t.Fatalf("writing the manifest: %v", err)
		}
		run(t, base, "git", "add", filepath.Join("stricttools", ".docs-state", DefaultOutputName))
		run(t, base, "git", "commit", "--quiet", "-m", "manifest")

		// gen has since rewritten the working-tree copy with a new slug; the
		// read must report what was published, not what is on disk.
		rewritten := `{"schema_version": 1, "name": "rewritten",
			"posts": [{"path": "posts/hello.md", "slug": "hello-world-renamed"}]}`
		if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
			t.Fatalf("rewriting the manifest: %v", err)
		}

		manifest, err := LoadFromGit(base, handle)
		if err != nil {
			t.Fatalf("reading the committed manifest: %v", err)
		}
		if manifest == nil {
			t.Fatal("read nothing for a committed manifest")
		}
		if manifest.Name != "committed" {
			t.Errorf("read the name %q, want the committed one", manifest.Name)
		}
		if len(manifest.Posts) != 1 || manifest.Posts[0].Slug != "hello-world" {
			t.Errorf("read the posts %+v, want the committed slug", manifest.Posts)
		}
	})
}

// requireGit skips a test that needs a repository when git is not installed.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH: the committed manifest is read through it")
	}
}

// run runs a command in dir and fails the test when it does not succeed.
func run(t *testing.T, dir string, argv ...string) {
	t.Helper()
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", argv, err, output)
	}
}
