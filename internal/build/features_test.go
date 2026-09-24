package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

func TestBuildNavigation(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a subdirectory becomes a group", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"guides/intro.md": "# Introduction\n\nIntro content.\n",
			"guides/setup.md": "# Setup\n\nSetup content.\n",
		}})
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index,
			`class="tm-tree-details"`,
			`class="tm-tree-label"`,
			`class="tm-tree-group"`,
			"Guides",
			`href="guides/intro/"`,
			`href="guides/setup/"`)
	})

	t.Run("root-level pages are flat", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"faq.md": "# FAQ\n\nFrequently asked questions.\n",
		}})
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index, `href="faq/"`)
		assertLacks(t, "index.html", index, `class="tm-tree-details"`)
	})

	t.Run("nav_group overrides the directory's title", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"api/config.md": "+++\nnav_group = \"Custom Title\"\n+++\n# Configuration\n\nConfig.\n",
		}})
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index, "Custom Title")
		assertLacks(t, "index.html", index, ">Api<")
	})

	t.Run("nav_order orders a group's pages", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"guides/intro.md": "+++\ntitle = \"Introduction\"\nnav_order = 2\n+++\n# Introduction\n\nIntro.\n",
			"guides/setup.md": "+++\ntitle = \"Setup\"\nnav_order = 1\n+++\n# Setup\n\nSetup.\n",
		}})
		index := built.page(t, "index.html")
		if strings.Index(index, "guides/setup/") > strings.Index(index, "guides/intro/") {
			t.Error("the page declaring nav_order 1 does not come first")
		}
	})

	t.Run("the group holding the page auto-expands", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"guides/intro.md": "# Introduction\n\nIntro content.\n",
		}})
		intro := built.page(t, "guides/intro/index.html")
		assertCarries(t, "guides/intro/index.html", intro,
			`<details class="tm-tree-details" open>`)
		index := built.page(t, "index.html")
		assertLacks(t, "index.html", index, `<details class="tm-tree-details" open>`)
		assertCarries(t, "index.html", index, `<details class="tm-tree-details">`)
	})

	t.Run("groups are ordered by title", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"api/ref.md":      "# API Reference\n\nRef.\n",
			"guides/start.md": "# Getting Started\n\nStart.\n",
		}})
		index := built.page(t, "index.html")
		if strings.Index(index, ">Api<") > strings.Index(index, ">Guides<") {
			t.Error("the groups are not ordered by their titles")
		}
	})
}

func TestBuildSearchTrigger(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	tests := []struct {
		name     string
		search   any
		contains []string
		absent   []string
	}{
		{
			name:     "no declaration renders the icon",
			search:   nil,
			contains: []string{`class="search-trigger"`},
			absent:   []string{`class="search-bar-trigger"`},
		},
		{
			name:     "icon renders the icon",
			search:   "icon",
			contains: []string{`class="search-trigger"`},
			absent:   []string{`class="search-bar-trigger"`},
		},
		{
			name:   "bar renders the bar",
			search: "bar",
			contains: []string{
				`class="search-bar-trigger"`, `class="search-bar-text"`, `class="search-bar-kbd"`,
			},
			absent: []string{`class="search-trigger"`},
		},
		{
			name:   "hidden renders neither",
			search: "hidden",
			absent: []string{`class="search-trigger"`, `class="search-bar-trigger"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrides := map[string]any{}
			if test.search != nil {
				overrides["search"] = test.search
			}
			built := buildFixture(t, fixture{Config: overrides})
			index := built.page(t, "index.html")
			assertCarries(t, "index.html", index, test.contains...)
			assertLacks(t, "index.html", index, test.absent...)
			// The dialog and the wiring are there whatever the trigger is:
			// the keyboard shortcut works in every mode.
			assertCarries(t, "index.html", index, `id="search-dialog"`, "PagefindUI")
		})
	}
}

func TestBuildChangelogPage(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a root CHANGELOG.md becomes the changelog page", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## 1.0.0\n\n- Initial release\n",
			},
		})
		changelog := built.page(t, "changelog/index.html")
		assertCarries(t, "changelog/index.html", changelog,
			"<!DOCTYPE html>", "Initial release", "Changelog")
		// The injected page declares feed: false.
		assertLacks(t, "feed.xml", built.page(t, "feed.xml"), "changelog/")
	})

	t.Run("a lowercase changelog.md is found too", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Files: map[string]string{
				"changelog.md": "# Changelog\n\n## 0.1.0\n\n- First beta\n",
			},
		})
		assertCarries(t, "changelog/index.html",
			built.page(t, "changelog/index.html"), "First beta")
	})

	t.Run("a declared changelog wins over the root one", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Config: map[string]any{"changelog": "packages/thing/CHANGELOG.md"},
			Files: map[string]string{
				"CHANGELOG.md": "# thing\n\n## 1.0.0\n\n- Root roll-up\n\n" +
					"# other\n\n## 2.0.0\n\n- A sibling release\n",
				"packages/thing/CHANGELOG.md": "# Changelog\n\n## 1.0.0\n\n- This site's own release\n",
			},
		})
		changelog := built.page(t, "changelog/index.html")
		assertCarries(t, "changelog/index.html", changelog, "own release")
		assertLacks(t, "changelog/index.html", changelog, "A sibling release", "Root roll-up")
	})

	t.Run("no changelog anywhere is no error and no page", func(t *testing.T) {
		built := buildFixture(t, fixture{})
		if built.exists("changelog/index.html") {
			t.Error("a project with no changelog got a changelog page")
		}
	})
}

func TestBuildGlossary(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	const declareResolver = "# Page A\n\n" +
		":<: list-glossary\n:=:\n" +
		"::: **Resolver**: A component that resolves directives\n" +
		":>:\n"

	t.Run("a term declared on one page is linked on another", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"page_a.md": declareResolver,
			"page_b.md": "# Page B\n\nThe Resolver handles all incoming requests.\n",
		}})
		assertCarries(t, "page_b/index.html", built.page(t, "page_b/index.html"),
			`page_a/#term-resolver" class="term-link"`)
	})

	t.Run("a term is not linked on the page that declares it", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"page_a.md": declareResolver + "\nThe Resolver is very useful.\n",
		}})
		assertLacks(t, "page_a/index.html", built.page(t, "page_a/index.html"), "term-link")
	})

	t.Run("a term inside code is not linked", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"page_a.md": "# Page A\n\n:<: list-glossary\n:=:\n" +
				"::: **Widget**: A UI component\n:>:\n",
			"page_b.md": "# Page B\n\nUse `Widget` in your code.\n",
		}})
		assertLacks(t, "page_b/index.html", built.page(t, "page_b/index.html"), "term-link")
	})

	t.Run("the glossary page is synthesized from every term", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "# Home\n\n:<: list-glossary\n:=:\n" +
				"::: **Zebra**: A striped animal\n:>:\n",
			"guide.md": "# Guide\n\n:<: list-glossary\n:=:\n" +
				"::: **Alpha**: The first letter\n:>:\n",
		}})
		glossary := built.page(t, "glossary/index.html")
		assertCarries(t, "glossary/index.html", glossary, "Alpha", "Zebra")
		if strings.Index(glossary, "Alpha") > strings.Index(glossary, "Zebra") {
			t.Error("the glossary's terms are not in alphabetical order")
		}
	})

	t.Run("the glossary joins the sidebar", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "# Home\n\n:<: list-glossary\n:=:\n" +
				"::: **API**: Application Programming Interface\n:>:\n",
		}})
		assertCarries(t, "index.html", built.page(t, "index.html"),
			`href="glossary/"`, "Glossary")
	})

	t.Run("glossary: false suppresses the page", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Config: map[string]any{"glossary": false},
			Docs: map[string]string{
				"index.md": "# Home\n\n:<: list-glossary\n:=:\n" +
					"::: **API**: Application Programming Interface\n:>:\n",
			},
		})
		if built.exists("glossary/index.html") {
			t.Error("a project turning the glossary off got a glossary page")
		}
	})
}

func TestBuildThemeOverride(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// A declaration only the framework theme makes, so its presence in a
	// build's stylesheet is that theme's fingerprint.
	const frameworkMarker = "--grain-opacity"

	t.Run("the config decides with no override", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{"theme": "minimal"}})
		assertLacks(t, "style.css", built.page(t, "style.css"), frameworkMarker)
	})

	t.Run("the override reaches the rendered stylesheet", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Config: map[string]any{"theme": "minimal"},
			Build:  func(o *Options) { o.Theme = "tinymoon" },
		})
		assertCarries(t, "css/style.css", built.page(t, "css/style.css"), frameworkMarker)
	})

	t.Run("the override is not written back to the config", func(t *testing.T) {
		built := buildFixture(t, fixture{
			Config: map[string]any{"theme": "minimal"},
			Build:  func(o *Options) { o.Theme = "tinymoon" },
		})
		raw, err := os.ReadFile(filepath.Join(built.dir, "selfdoc.json"))
		if err != nil {
			t.Fatalf("reading the config back: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decoding the config: %v", err)
		}
		if decoded["theme"] != "minimal" {
			t.Errorf("the config now declares theme %v, want the one it was written with", decoded["theme"])
		}
	})

	t.Run("an unknown theme is refused and writes nothing", func(t *testing.T) {
		built, err := tryBuildFixture(t, fixture{
			Config: map[string]any{"theme": "minimal"},
			Build:  func(o *Options) { o.Theme = "nosuchtheme" },
		})
		if err == nil {
			t.Fatal("a build naming a theme the registry does not carry succeeded")
		}
		assertCarries(t, "the refusal", err.Error(), "nosuchtheme", "tinymoon")
		if built.exists("style.css") {
			t.Error("the refused build left a stylesheet behind")
		}
	})
}

func TestBuildDeployTargets(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a Cloudflare Pages project gets a _headers file", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{
			"deploy": map[string]any{"provider": "cloudflare-pages", "project": "fixture"},
		}})
		if !built.exists("_headers") {
			t.Error("_headers was not written for a Cloudflare Pages deploy")
		}
	})

	t.Run("a GitHub Pages project gets none", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{
			"deploy": map[string]any{"provider": "github-pages"},
		}})
		if built.exists("_headers") {
			t.Error("_headers was written for a provider that does not read it")
		}
	})

	t.Run("no deploy block gets none", func(t *testing.T) {
		built := buildFixture(t, fixture{})
		if built.exists("_headers") {
			t.Error("_headers was written for a project that declares no deploy")
		}
	})

	t.Run("a GitHub Pages project declares the security meta", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{
			"deploy": map[string]any{"provider": "github-pages"},
		}})
		assertCarries(t, "index.html", built.page(t, "index.html"),
			"Content-Security-Policy")
	})

	t.Run("a Cloudflare Pages project does not", func(t *testing.T) {
		// The headers file states the policy there, so a meta element
		// restating it would be a second authority.
		built := buildFixture(t, fixture{Config: map[string]any{
			"deploy": map[string]any{"provider": "cloudflare-pages", "project": "fixture"},
		}})
		assertLacks(t, "index.html", built.page(t, "index.html"),
			`<meta http-equiv="Content-Security-Policy"`)
	})
}

func TestBuildIsReproducible(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
	testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", "guide.md"),
		"+++\ndate = 2024-01-15\n+++\n# Guide\n\nA guide page.\n")

	first := buildTwice(t, dir)
	second := buildTwice(t, dir)
	for outputKey, content := range first {
		if second[outputKey] != content {
			t.Errorf("%s differs between two builds of the same tree", outputKey)
		}
	}
	if len(first) != len(second) {
		t.Errorf("the two builds wrote %d and %d files", len(first), len(second))
	}
}

// buildTwice builds a project and returns every text file the output carries,
// keyed by its output-relative path. The Pagefind index is excluded: the
// indexer names its own files by content digest and writes a build timestamp.
func buildTwice(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := Build(Options{DirPath: dir, Stdout: &discard{}}, effects.Unbound()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	output := filepath.Join(dir, ".stricttools", "docs-cache", "build")
	files := map[string]string{}
	err := filepath.Walk(output, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "pagefind" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(output, path)
		if relErr != nil {
			return relErr
		}
		switch filepath.Ext(path) {
		case ".html", ".css", ".txt", ".xml", ".svg":
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			files[filepath.ToSlash(rel)] = string(content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the output: %v", err)
	}
	return files
}

// discard swallows a build's progress lines.
type discard struct{}

// Write reports every byte written and keeps none.
func (discard) Write(p []byte) (int, error) { return len(p), nil }
