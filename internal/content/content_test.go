package content

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/catalog"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/robots"
	"github.com/stricttools/testisolation/go/hygiene"

	// The language packages register their extractors, which is what links
	// a language into a binary. A build links them through internal/cli; the
	// suite does it here.
	_ "github.com/stricttools/selfdoc/internal/extractors/golang"
	_ "github.com/stricttools/selfdoc/internal/extractors/python"
	_ "github.com/stricttools/selfdoc/internal/extractors/typescript"
)

func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// requirePython3 skips the test when no interpreter is on PATH. The Python
// extractor reads every tree through python3, so a listing of Python modules
// cannot be exercised without one.
func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH: the Python extractor reads every tree through it")
	}
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

// resolve runs the dispatcher, failing the test on an unexpected error.
func resolve(
	t *testing.T, name string, attrs map[string]string, body []string,
	baseDir string, config map[string]any,
) (string, bool) {
	t.Helper()
	rendered, ok, err := ResolveContent(
		name, attrs, body, baseDir, config,
	)
	if err != nil {
		t.Fatalf("ResolveContent(%q): %v", name, err)
	}
	return rendered, ok
}

func wants(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, want := range substrings {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func rejects(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, unwanted := range substrings {
		if strings.Contains(content, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, content)
		}
	}
}

// -- The directive set ------------------------------------------------------

func TestContentDirectivesHasEveryEntry(t *testing.T) {
	want := []string{
		"callout-danger", "callout-important", "callout-note", "callout-tip",
		"callout-warning", "cv", "list-crawlers", "list-glossary",
		"list-modules", "list-tree", "table-commands",
		"table-config-schema",
		"table-dep", "table-directives", "table-endpoint", "table-lints", "var",
	}
	got := make([]string, 0, len(ContentDirectives))
	for name := range ContentDirectives {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ContentDirectives = %v, want %v", got, want)
	}
}

func TestUnknownDirectiveIsNotContent(t *testing.T) {
	isolate(t)
	for _, name := range []string{"ref", "code-test", "nonexistent", "list-features"} {
		if _, ok := resolve(t, name, nil, []string{"body"}, t.TempDir(), nil); ok {
			t.Errorf("%q must not resolve as a content directive", name)
		}
	}
}

// -- Callouts ---------------------------------------------------------------

func TestCallouts(t *testing.T) {
	isolate(t)
	cases := []struct {
		name  string
		title string
		body  string
	}{
		{"callout-note", "Note", "This is a note."},
		{"callout-warning", "Warning", "Be careful."},
		{"callout-tip", "Tip", "A helpful tip."},
		{"callout-danger", "Danger", "Dangerous operation."},
		{"callout-important", "Important", "Read this first."},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rendered, ok := resolve(
				t, testCase.name, nil, []string{testCase.body}, t.TempDir(), nil,
			)
			if !ok {
				t.Fatal("a callout is a content directive")
			}
			wants(t, rendered,
				`<div class="callout `+testCase.name+`">`,
				`<p class="callout-title">`+testCase.title+`</p>`,
				"<p>"+testCase.body+"</p>",
				"</div>",
			)
		})
	}
}

func TestCalloutWithNoBodyHasNoParagraph(t *testing.T) {
	isolate(t)
	rendered, _ := resolve(t, "callout-note", nil, nil, t.TempDir(), nil)
	want := "<div class=\"callout callout-note\">\n" +
		"<p class=\"callout-title\">Note</p>\n</div>"
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func TestCalloutJoinsItsBodyWithNewlines(t *testing.T) {
	isolate(t)
	rendered, _ := resolve(t, "callout-warning", nil,
		[]string{"Line one.", "Line two.", "Line three."}, t.TempDir(), nil)
	wants(t, rendered, "<p>Line one.\nLine two.\nLine three.</p>")
}

// -- The glossary -----------------------------------------------------------

func TestGlossary(t *testing.T) {
	isolate(t)

	t.Run("terms and definitions", func(t *testing.T) {
		rendered, ok := resolve(t, "list-glossary", nil, []string{
			"**Term1**: Definition one",
			"**Term2**: Definition two",
		}, t.TempDir(), nil)
		if !ok {
			t.Fatal("list-glossary is a content directive")
		}
		wants(t, rendered,
			`<div class="glossary">`,
			"<dt><dfn>Term1</dfn></dt>", "<dd>Definition one</dd>",
			"<dt><dfn>Term2</dfn></dt>", "<dd>Definition two</dd>",
		)
	})

	t.Run("an empty body is an empty glossary", func(t *testing.T) {
		rendered, _ := resolve(t, "list-glossary", nil, nil, t.TempDir(), nil)
		if rendered != `<div class="glossary"><dl></dl></div>` {
			t.Fatalf("rendered = %q", rendered)
		}
	})

	t.Run("a term with no definition", func(t *testing.T) {
		rendered := ResolveGlossary([]string{"**Solo**"})
		wants(t, rendered, "<dt><dfn>Solo</dfn></dt>", "<dd></dd>")
	})

	t.Run("blank lines are skipped", func(t *testing.T) {
		rendered := ResolveGlossary([]string{"**A**: Alpha", "", "**B**: Beta"})
		wants(t, rendered, "<dt><dfn>A</dfn></dt>", "<dt><dfn>B</dfn></dt>")
	})

	t.Run("the term markers are stripped", func(t *testing.T) {
		rendered := ResolveGlossary([]string{"**X**: cross"})
		wants(t, rendered, "<dt><dfn>X</dfn></dt>", "<dd>cross</dd>")
	})
}

// -- list-tree --------------------------------------------------------------

// sampleTree writes the directory structure the tree tests read and returns
// the directory that holds it.
func sampleTree(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	project := filepath.Join(base, "myproject")
	write(t, filepath.Join(project, "README.md"), "# My Project\n")
	write(t, filepath.Join(project, "setup.py"), "# setup\n")
	write(t, filepath.Join(project, "src", "__init__.py"), "")
	write(t, filepath.Join(project, "src", "main.py"), "# main\n")
	write(t, filepath.Join(project, "src", "utils.py"), "# utils\n")
	write(t, filepath.Join(project, "src", "sub", "helper.py"), "# helper\n")
	write(t, filepath.Join(project, "src", "__pycache__", "main.cpython-311.pyc"), "binary")
	write(t, filepath.Join(project, ".git", "config"), "git config")
	return base
}

func TestListTree(t *testing.T) {
	isolate(t)

	t.Run("produces the tree structure", func(t *testing.T) {
		base := sampleTree(t)
		rendered, ok := resolve(t, "list-tree",
			map[string]string{"path": "myproject"}, nil, base, nil)
		if !ok {
			t.Fatal("list-tree is a content directive")
		}
		wants(t, rendered, "```", "myproject/", "README.md", "setup.py",
			"src/", "main.py", "utils.py", "sub/", "helper.py")
		if !strings.Contains(rendered, "├── ") && !strings.Contains(rendered, "└── ") {
			t.Error("the tree must be drawn with its connectors")
		}
	})

	t.Run("machine state is excluded", func(t *testing.T) {
		base := sampleTree(t)
		rendered := ResolveListTree(map[string]string{"path": "myproject"}, base)
		rejects(t, rendered, "__pycache__", ".git", ".pyc")
	})

	t.Run("the depth limit", func(t *testing.T) {
		base := sampleTree(t)
		rendered := ResolveListTree(
			map[string]string{"path": "myproject", "depth": "1"}, base)
		wants(t, rendered, "README.md", "src/")
		rejects(t, rendered, "main.py")
	})

	t.Run("a missing directory", func(t *testing.T) {
		rendered := ResolveListTree(map[string]string{"path": "nonexistent"}, t.TempDir())
		wants(t, rendered, "not found")
	})

	t.Run("a missing path attribute", func(t *testing.T) {
		rendered := ResolveListTree(nil, t.TempDir())
		wants(t, rendered, "requires a path")
	})
}

// -- table-dep --------------------------------------------------------------

const samplePyproject = `[project]
name = "myproject"
version = "1.0.0"
dependencies = [
    "requests>=2.28.0",
    "click>=8.0,<9.0",
    "pydantic",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.0",
    "black>=23.0",
]
docs = [
    "sphinx>=5.0",
]
`

func TestTableDep(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "pyproject.toml"), samplePyproject)

	t.Run("the dependency table", func(t *testing.T) {
		rendered, ok := resolve(t, "table-dep",
			map[string]string{"path": "pyproject.toml"}, nil, base, nil)
		if !ok {
			t.Fatal("table-dep is a content directive")
		}
		wants(t, rendered,
			"| Package | Version Constraint |", "| --- | --- |",
			"`requests`", ">=2.28.0", "`click`", ">=8.0,<9.0",
			"| `pydantic` | * |",
		)
	})

	t.Run("the optional groups, in document order", func(t *testing.T) {
		rendered := ResolveTableDep(map[string]string{"path": "pyproject.toml"}, base)
		wants(t, rendered, "[dev]", "`pytest`", "`black`", "[docs]", "`sphinx`")
		if strings.Index(rendered, "[dev]") > strings.Index(rendered, "[docs]") {
			t.Error("the groups must render in the order the document declares them")
		}
	})

	t.Run("a missing file", func(t *testing.T) {
		rendered := ResolveTableDep(map[string]string{"path": "nonexistent.toml"}, base)
		wants(t, rendered, "not found")
	})

	t.Run("a missing path attribute", func(t *testing.T) {
		wants(t, ResolveTableDep(nil, base), "requires a path")
	})

	t.Run("a malformed document", func(t *testing.T) {
		write(t, filepath.Join(base, "broken.toml"), "[project\nname = ")
		rendered := ResolveTableDep(map[string]string{"path": "broken.toml"}, base)
		wants(t, rendered, "cannot parse 'broken.toml'")
	})

	t.Run("a document with no dependencies", func(t *testing.T) {
		write(t, filepath.Join(base, "bare.toml"), "[project]\nname = \"x\"\n")
		rendered := ResolveTableDep(map[string]string{"path": "bare.toml"}, base)
		wants(t, rendered, "no dependencies found in 'bare.toml'")
	})
}

func TestParseDepSpecifier(t *testing.T) {
	cases := []struct {
		spec       string
		name       string
		constraint string
	}{
		{"requests>=2.0", "requests", ">=2.0"},
		{"flask", "flask", "*"},
		{"black[jupyter]>=23.0,<24.0", "black[jupyter]", ">=23.0,<24.0"},
		{"uvicorn >= 0.20", "uvicorn", ">= 0.20"},
		{"httpx>=0.24; python_version < '3.11'", "httpx", ">=0.24"},
		{"", "", "*"},
	}
	for _, testCase := range cases {
		name, constraint := parseDepSpecifier(testCase.spec)
		if name != testCase.name || constraint != testCase.constraint {
			t.Errorf("parseDepSpecifier(%q) = (%q, %q), want (%q, %q)",
				testCase.spec, name, constraint, testCase.name, testCase.constraint)
		}
	}
}

// -- table-directives and table-config-schema ------------------------------

func TestTableDirectives(t *testing.T) {
	isolate(t)
	rendered, ok := resolve(t, "table-directives", nil, nil, t.TempDir(), nil)
	if !ok {
		t.Fatal("table-directives is a content directive")
	}
	wants(t, rendered, "| Directive | Description |",
		"`ref`", "`table-schema`", "`callout-note`", "`list-modules`", "`var`")

	var names []string
	for _, line := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		names = append(names, strings.Split(line, "`")[1])
	}
	if len(names) == 0 {
		t.Fatal("the table rendered no rows")
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("the directives must render in name order: %v", names)
	}
}

func TestTableConfigSchema(t *testing.T) {
	isolate(t)
	rendered, ok := resolve(t, "table-config-schema", nil, nil, t.TempDir(), nil)
	if !ok {
		t.Fatal("table-config-schema is a content directive")
	}
	wants(t, rendered, "| Field | Required | Description |", "`source`", "`base_url`")

	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "`base_url`") {
			// base_url is required; source is not, because a codeless
			// project has none.
			wants(t, line, "| yes |")
		}
		if strings.Contains(line, "`docs`") {
			wants(t, line, "| no |")
		}
	}
}

// -- list-crawlers ----------------------------------------------------------

// TestListCrawlersRendersTheDeclaredPolicy: the directive is the docs' reading
// of package robots, so what it renders is that package's list, in its order,
// and nothing typed out beside it.
func TestListCrawlersRendersTheDeclaredPolicy(t *testing.T) {
	isolate(t)
	rendered, ok := resolve(t, "list-crawlers", nil, nil, t.TempDir(), nil)
	if !ok {
		t.Fatal("list-crawlers is a content directive")
	}
	var want []string
	for _, agent := range robots.Agents {
		want = append(want, "- `"+agent+"`")
	}
	if got := strings.Join(want, "\n"); rendered != got {
		t.Errorf("list-crawlers rendered\n%s\nwant\n%s", rendered, got)
	}
}

// TestListCrawlersNamesEveryAgentOnItsOwnLine: a reader has to be able to read
// one agent per bullet, which is also what keeps the rendering greppable
// against the generated robots.txt.
func TestListCrawlersNamesEveryAgentOnItsOwnLine(t *testing.T) {
	isolate(t)
	rendered, _ := resolve(t, "list-crawlers", nil, nil, t.TempDir(), nil)
	lines := strings.Split(rendered, "\n")
	if len(lines) != len(robots.Agents) {
		t.Fatalf("list-crawlers rendered %d lines for %d agents:\n%s",
			len(lines), len(robots.Agents), rendered)
	}
	for i, agent := range robots.Agents {
		if !strings.Contains(lines[i], agent) {
			t.Errorf("line %d (%q) does not name %q", i, lines[i], agent)
		}
	}
}

func TestTableConfigSchemaHidesInternalFields(t *testing.T) {
	isolate(t)
	rendered, err := ResolveTableConfigSchema()
	if err != nil {
		t.Fatalf("ResolveTableConfigSchema: %v", err)
	}
	// An internal field is a runtime key, not something an author writes.
	for _, spec := range configSchemaInternalNames() {
		rejects(t, rendered, "`"+spec+"`")
	}
	// Every other field is in the table, twitter among them: it is a
	// declared key, not a value merged in behind the reader's back.
	wants(t, rendered, "`twitter`")
}

// -- var --------------------------------------------------------------------

func TestVar(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	write(t, filepath.Join(base, "pyproject.toml"),
		"[project]\nname = \"demo\"\nversion = \"1.2.3\"\n"+
			"description = \"A demo project.\"\n")
	config := map[string]any{
		"source":      []any{map[string]any{"path": "demo/", "language": "python"}},
		"description": "Code-aware static site generator.",
	}

	cases := []struct {
		key  string
		want string
	}{
		{"project.language", "python"},
		{"project.description", "Code-aware static site generator."},
		{"project.name", "demo"},
		{"project.version", "1.2.3"},
	}
	for _, testCase := range cases {
		t.Run(testCase.key, func(t *testing.T) {
			rendered, ok := resolve(t, "var",
				map[string]string{"key": testCase.key}, nil, base, config)
			if !ok {
				t.Fatal("var is a content directive")
			}
			if rendered != testCase.want {
				t.Fatalf("var %s = %q, want %q", testCase.key, rendered, testCase.want)
			}
		})
	}

	t.Run("the description falls back to the manifest", func(t *testing.T) {
		bare := map[string]any{
			"source": []any{map[string]any{"path": "demo/", "language": "python"}},
		}
		rendered, _ := resolve(t, "var",
			map[string]string{"key": "project.description"}, nil, base, bare)
		if rendered != "A demo project." {
			t.Fatalf("rendered = %q", rendered)
		}
	})

	t.Run("an unknown key", func(t *testing.T) {
		rendered, _ := resolve(t, "var",
			map[string]string{"key": "nonexistent.field"}, nil, base, config)
		wants(t, rendered, "unknown var key")
	})

	t.Run("a missing key attribute", func(t *testing.T) {
		rendered, _ := resolve(t, "var", nil, nil, base, config)
		wants(t, rendered, "requires a key attribute")
	})

	t.Run("no config at all", func(t *testing.T) {
		rendered, _ := resolve(t, "var",
			map[string]string{"key": "project.name"}, nil, base, nil)
		wants(t, rendered, "requires project config")
	})

	t.Run("the version override wins", func(t *testing.T) {
		overridden := map[string]any{
			"source":           config["source"],
			VersionOverrideKey: "9.9.9",
		}
		rendered, _ := resolve(t, "var",
			map[string]string{"key": "project.version"}, nil, base, overridden)
		if rendered != "9.9.9" {
			t.Fatalf("rendered = %q", rendered)
		}
	})
}

func TestVarProjectLanguage(t *testing.T) {
	isolate(t)
	base := t.TempDir()

	t.Run("one language", func(t *testing.T) {
		config := map[string]any{"source": []any{
			map[string]any{"path": "src/", "language": "python"},
		}}
		rendered, err := ResolveVar(map[string]string{"key": "project.language"}, config, base)
		if err != nil || rendered != "python" {
			t.Fatalf("rendered = %q, err = %v", rendered, err)
		}
	})

	t.Run("several languages, in first-appearance order", func(t *testing.T) {
		config := map[string]any{"source": []any{
			map[string]any{"path": "src/", "language": "python"},
			map[string]any{"path": "cmd/", "language": "go"},
		}}
		rendered, err := ResolveVar(map[string]string{"key": "project.language"}, config, base)
		if err != nil || rendered != "python, go" {
			t.Fatalf("rendered = %q, err = %v", rendered, err)
		}
	})

	t.Run("duplicates are reported once", func(t *testing.T) {
		config := map[string]any{"source": []any{
			map[string]any{"path": "a/", "language": "python"},
			map[string]any{"path": "b/", "language": "python"},
		}}
		rendered, err := ResolveVar(map[string]string{"key": "project.language"}, config, base)
		if err != nil || rendered != "python" {
			t.Fatalf("rendered = %q, err = %v", rendered, err)
		}
	})

	t.Run("a codeless project is a hard error", func(t *testing.T) {
		// project.language is source-derived, so a placeholder word would
		// be a silent lie on the page.
		for _, config := range []map[string]any{{}, {"source": []any{}}} {
			_, err := ResolveVar(map[string]string{"key": "project.language"}, config, base)
			if err == nil {
				t.Fatal("a codeless project must be refused")
			}
			wants(t, err.Error(), "'source'", ":-: var")
			rejects(t, err.Error(), ":::")
		}
	})
}

func TestVarTopology(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	config := map[string]any{"topology": map[string]any{
		"docs_base":  "https://docs.example",
		"posts_base": "https://posts.example",
		"slug":       "alpha",
	}}
	cases := []struct{ key, want string }{
		{"topology.docs_url", "https://docs.example/alpha"},
		{"topology.posts_url", "https://posts.example"},
		{"topology.slug", "alpha"},
	}
	for _, testCase := range cases {
		rendered, err := ResolveVar(map[string]string{"key": testCase.key}, config, base)
		if err != nil || rendered != testCase.want {
			t.Errorf("var %s = %q (err %v), want %q",
				testCase.key, rendered, err, testCase.want)
		}
	}

	// With no topology declared, each key answers empty rather than
	// inventing an address.
	for _, key := range []string{"topology.docs_url", "topology.posts_url", "topology.slug"} {
		rendered, err := ResolveVar(map[string]string{"key": key}, map[string]any{}, base)
		if err != nil || rendered != "" {
			t.Errorf("var %s = %q (err %v), want \"\"", key, rendered, err)
		}
	}
}

// configSchemaInternalNames are the names of the config fields marked
// internal, which the rendered table must not carry.
func configSchemaInternalNames() []string {
	var names []string
	for _, spec := range config.Schema {
		if spec.Internal {
			names = append(names, spec.Name)
		}
	}
	return names
}

func TestTableDirectivesCarriesOnlyTheCoreCatalogue(t *testing.T) {
	// A declared-but-unimplemented directive is not something a page may
	// use, so the reference table does not advertise it.
	isolate(t)
	rendered, err := ResolveTableDirectives()
	if err != nil {
		t.Fatalf("ResolveTableDirectives: %v", err)
	}
	for _, name := range catalog.FutureDirectiveNames() {
		rejects(t, rendered, "| `"+name+"` |")
	}
}
