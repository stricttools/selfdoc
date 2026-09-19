package build

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// site is a built fixture project: where it is, where its output root is, what
// the build reported writing, and what it printed.
type site struct {
	dir     string
	output  string
	written map[string]bool
	stdout  string
}

// fixture describes one project to build: the docs pages it carries, the
// config keys it overrides, and the build options.
type fixture struct {
	// Docs maps a docs-relative path to the page's Markdown source. An
	// entry for "index.md" replaces the default one.
	Docs map[string]string
	// Files maps a project-relative path to a file written outside the docs
	// tree -- a CHANGELOG.md, a post.
	Files map[string]string
	// Config overrides the default fixture config.
	Config map[string]any
	// Build is applied to the build options before the build runs.
	Build func(*Options)
}

// buildFixture writes a fixture project, builds it, and returns the site.
//
// The project is a fresh temporary directory every time, so a test that
// rebuilds gets a clean tree rather than another test's leftovers.
func buildFixture(t *testing.T, f fixture) site {
	t.Helper()
	built, err := tryBuildFixture(t, f)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return built
}

// tryBuildFixture writes a fixture project and builds it, returning whatever
// the build answered -- which is what a refusal test reads.
func tryBuildFixture(t *testing.T, f fixture) (site, error) {
	t.Helper()
	overrides := map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"}
	for key, value := range f.Config {
		overrides[key] = value
	}
	dir := testproject.Make(t, overrides)
	for relPath, content := range f.Docs {
		testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", filepath.FromSlash(relPath)), content)
	}
	for relPath, content := range f.Files {
		testproject.WriteText(t, filepath.Join(dir, filepath.FromSlash(relPath)), content)
	}

	var out bytes.Buffer
	opts := Options{DirPath: dir, Stdout: &out}
	if f.Build != nil {
		f.Build(&opts)
	}
	written, err := Build(opts, effects.Unbound())
	return site{
		dir:     dir,
		output:  filepath.Join(dir, ".stricttools", "docs-cache", "build"),
		written: written,
		stdout:  out.String(),
	}, err
}

// page reads one built page, given its output key.
func (s site) page(t *testing.T, outputKey string) string {
	t.Helper()
	return readFile(t, filepath.Join(s.output, filepath.FromSlash(outputKey)))
}

// exists reports whether the build wrote a file at an output-relative path.
func (s site) exists(outputKey string) bool {
	_, err := os.Stat(filepath.Join(s.output, filepath.FromSlash(outputKey)))
	return err == nil
}

// reported reports whether the build named an output-relative path among the
// files it wrote.
func (s site) reported(outputKey string) bool {
	return s.written[filepath.Join(s.output, filepath.FromSlash(outputKey))]
}

// assertCarries fails the test when a built page does not carry each wanted
// string.
func assertCarries(t *testing.T, what, content string, wanted ...string) {
	t.Helper()
	for _, want := range wanted {
		if !strings.Contains(content, want) {
			t.Errorf("%s does not carry %q", what, want)
		}
	}
}

// assertLacks fails the test when a built page carries any unwanted string.
func assertLacks(t *testing.T, what, content string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(content, bad) {
			t.Errorf("%s carries %q", what, bad)
		}
	}
}

func TestBuildProducesTheSite(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{
		Docs: map[string]string{
			"index.md": "# Test Project\n\nWelcome.\n",
			"guide.md": "# Guide\n\nA guide page.\n",
			"logo.png": string(makePNG(64, 32)),
		},
		Config: map[string]any{"description": "A fixture project."},
	})

	t.Run("the pages are at the stable mount", func(t *testing.T) {
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index,
			"<!DOCTYPE html>",
			`<h1 id="test-project"><a class="heading-link" href="#test-project" `+
				`aria-label="Link to section: Test Project">#</a>Test Project</h1>`,
			"Welcome.")
		if !built.reported("index.html") {
			t.Error("the build did not report writing index.html")
		}
		if !built.exists("guide/index.html") {
			t.Error("a root-level page is not emitted at its own directory index")
		}
	})

	t.Run("the home page is the root index, with no stub over it", func(t *testing.T) {
		// A single-locale site mounts its current version at the output
		// root, so the home page IS the root index: a stub would overwrite
		// the page.
		assertCarries(t, "index.html", built.page(t, "index.html"), "Welcome.")
		assertLacks(t, "index.html", built.page(t, "index.html"), "http-equiv=\"refresh\"")
	})

	t.Run("the site-level files are all written", func(t *testing.T) {
		for _, outputKey := range []string{
			"style.css", "og-index.png", "og-guide.png", "llms.txt",
			"llms-full.txt", "404.html", "favicon.svg", "sitemap.xml",
			"feed.xml", "robots.txt", "_redirects",
		} {
			if !built.exists(outputKey) {
				t.Errorf("%s was not written", outputKey)
			}
		}
		if !built.exists("pagefind") {
			t.Error("the build did not index its own output")
		}
	})

	t.Run("the write count is the site's file count", func(t *testing.T) {
		// Two pages, one copied asset, the stylesheet, two social cards,
		// the two llms files, the 404 page, the favicon, robots.txt, the
		// sitemap, the feed and the Cloudflare redirects file. No root
		// stub: the home page is itself the root index. The pagefind/ tree
		// is written by the indexer, outside the recorded-write
		// bookkeeping.
		if len(built.written) != 14 {
			var paths []string
			for path := range built.written {
				paths = append(paths, path)
			}
			t.Errorf("the build reported %d files, want 14:\n%s",
				len(built.written), strings.Join(paths, "\n"))
		}
	})

	t.Run("a social card is a real PNG", func(t *testing.T) {
		card := built.page(t, "og-index.png")
		if !strings.HasPrefix(card, "\x89PNG\r\n\x1a\n") {
			t.Error("og-index.png does not open with the PNG signature")
		}
	})

	t.Run("a non-Markdown asset is copied into the mount", func(t *testing.T) {
		if !built.exists("logo.png") {
			t.Error("docs/logo.png was not copied into the output")
		}
		if !built.reported("logo.png") {
			t.Error("the build did not report copying docs/logo.png")
		}
	})

	t.Run("the sidebar links the sibling page", func(t *testing.T) {
		assertCarries(t, "index.html", built.page(t, "index.html"),
			`href="guide/"`,
			`<nav id="tm-nav" aria-label="Site navigation">`)
	})

	t.Run("the document is the chrome the theme states", func(t *testing.T) {
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index,
			`<html lang="en">`,
			`<article data-pagefind-body class="doc-body">`,
			"</article>",
			`<footer class="site-footer">`)
	})

	t.Run("nothing loads a highlighter from a CDN", func(t *testing.T) {
		assertLacks(t, "index.html", built.page(t, "index.html"),
			"highlight.js", "hljs", "cdnjs.cloudflare.com")
	})

	t.Run("the pages are minified", func(t *testing.T) {
		index := built.page(t, "index.html")
		if strings.Contains(index, "<!--") {
			t.Error("index.html still carries an HTML comment")
		}
	})

	t.Run("the stylesheet is minified and carries the highlight rules", func(t *testing.T) {
		css := built.page(t, "style.css")
		if strings.Contains(css, "/* ") {
			t.Error("style.css still carries a CSS comment")
		}
		if !strings.Contains(css, html.PygmentsVarPrefix) {
			t.Errorf("style.css carries no syntax-highlighting rules")
		}
	})

	t.Run("the head inlines the critical stylesheet and loads the rest async", func(t *testing.T) {
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index,
			"<style>",
			`rel="preload"`,
			"<noscript>")
	})

	t.Run("llms-full.txt names every page", func(t *testing.T) {
		full := built.page(t, "llms-full.txt")
		assertCarries(t, "llms-full.txt", full,
			"## Test Project", "## Guide",
			"<!-- path: index.md -->", "<!-- path: guide.md -->")
	})

	t.Run("the feed is the project's", func(t *testing.T) {
		feed := built.page(t, "feed.xml")
		assertCarries(t, "feed.xml", feed,
			`<link href="https://example.com/feed.xml" rel="self"/>`,
			"<subtitle>A fixture project.</subtitle>")
	})

	t.Run("every page links the feed", func(t *testing.T) {
		assertCarries(t, "index.html", built.page(t, "index.html"),
			`type="application/atom+xml"`)
	})

	t.Run("the compressed companions are written", func(t *testing.T) {
		for _, companion := range []string{"index.html.gz", "index.html.br", "style.css.gz"} {
			if !built.exists(companion) {
				t.Errorf("%s was not written", companion)
			}
		}
		if built.exists("logo.png.gz") {
			t.Error("an already-compressed file got a gzip companion")
		}
	})

	t.Run("the build says what it did", func(t *testing.T) {
		assertCarries(t, "the build's output", built.stdout,
			"Pre-compressed", "(gzip + brotli)", "OG cards: basic")
	})
}

func TestBuildSEOAndStructuredData(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{
		Docs: map[string]string{
			"index.md": "# Test Project\n\nWelcome.\n",
			"guide.md": "+++\ndescription = \"How to use it.\"\n+++\n\n# Guide\n\n" +
				"Some prose.\n\n```python\nprint(1)\n```\n",
			"api/reference.md": "# Reference\n\nThe API surface.\n",
		},
		Config: map[string]any{
			"repo":   "https://github.com/test/repo",
			"author": map[string]any{"name": "Jane Doe", "url": "https://jane.dev"},
		},
	})

	index := built.page(t, "index.html")
	guide := built.page(t, "guide/index.html")
	nested := built.page(t, "api/reference/index.html")

	t.Run("the home page declares the site", func(t *testing.T) {
		assertCarries(t, "index.html", index, `"WebSite"`, "https://example.com/")
		assertLacks(t, "index.html", index, `"SearchAction"`, "search_term_string", "potentialAction")
	})

	t.Run("only the home page declares the site", func(t *testing.T) {
		assertLacks(t, "guide/index.html", guide, `"WebSite"`, `"SearchAction"`)
	})

	t.Run("the declared author is the page's Person", func(t *testing.T) {
		assertCarries(t, "index.html", index, `"Person"`, `"Jane Doe"`, `"https://jane.dev"`)
	})

	t.Run("a page with code declares its source", func(t *testing.T) {
		assertCarries(t, "guide/index.html", guide,
			`"SoftwareSourceCode"`, "https://github.com/test/repo")
	})

	t.Run("the Open Graph block names the page's own address", func(t *testing.T) {
		assertCarries(t, "index.html", index,
			`<meta property="og:url" content="https://example.com/">`,
			`<meta property="og:image" content="https://example.com/og-index.png">`,
			`<meta property="og:type" content="website">`,
			`<meta name="twitter:card" content="summary_large_image">`)
		assertCarries(t, "guide/index.html", guide,
			`<meta property="og:url" content="https://example.com/guide/">`,
			`<meta property="og:type" content="article">`,
			`<meta property="og:description" content="How to use it.">`)
	})

	t.Run("the social titles are the document title", func(t *testing.T) {
		// A standalone build's index page is titled with its written title
		// alone, and the social titles carry exactly what the head's title
		// element carries.
		assertCarries(t, "index.html", index,
			`<title>Test Project</title>`,
			`<meta name="twitter:title" content="Test Project">`,
			`<meta property="og:title" content="Test Project">`)
		// The project name is the checkout's directory name, which a
		// temporary fixture cannot spell, so the inner page is matched.
		for _, pattern := range []string{
			`<title>Guide - [^<]+</title>`,
			`<meta name="twitter:title" content="Guide - [^"]+">`,
			`<meta property="og:title" content="Guide - [^"]+">`,
		} {
			if !regexp.MustCompile(pattern).MatchString(guide) {
				t.Errorf("guide/index.html carries nothing matching %s",
					pattern)
			}
		}
	})

	t.Run("a nested page declares its breadcrumbs", func(t *testing.T) {
		assertCarries(t, "api/reference/index.html", nested, `"BreadcrumbList"`)
	})

	t.Run("the 404 page has no social block", func(t *testing.T) {
		notFound := built.page(t, "404.html")
		assertLacks(t, "404.html", notFound, `property="og:`, `name="twitter:`)
		assertCarries(t, "404.html", notFound,
			`<nav id="tm-nav" aria-label="Site navigation">`,
			`href="guide/"`)
	})

	t.Run("a description with no frontmatter comes from the prose", func(t *testing.T) {
		assertCarries(t, "api/reference/index.html", nested,
			`<meta name="description" content="The API surface.">`)
	})
}

func TestBuildPageDates(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{
		Docs: map[string]string{
			"updated-only.md": "+++\nupdated = 2026-05-01\n+++\n# Updated Only\n\nContent.\n",
			"date-only.md":    "+++\ndate = 2026-01-15\n+++\n# Date Only\n\nContent.\n",
			"both.md":         "+++\ndate = 2026-01-15\nupdated = 2026-05-01\n+++\n# Both\n\nContent.\n",
			"neither.md":      "# Neither\n\nContent.\n",
		},
	})

	tests := []struct {
		name      string
		outputKey string
		contains  []string
	}{
		{
			name:      "an updated date is the modification date",
			outputKey: "updated-only/index.html",
			contains: []string{
				"Last updated", `<time datetime="2026-05-01">`, "May 1, 2026",
				`"dateModified": "2026-05-01"`,
			},
		},
		{
			name:      "a declared date is both dates",
			outputKey: "date-only/index.html",
			contains: []string{
				`"datePublished": "2026-01-15"`, `"dateModified": "2026-01-15"`,
			},
		},
		{
			name:      "an updated date wins over the declared one",
			outputKey: "both/index.html",
			contains: []string{
				`"datePublished": "2026-01-15"`, `"dateModified": "2026-05-01"`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCarries(t, test.outputKey, built.page(t, test.outputKey), test.contains...)
		})
	}

	t.Run("a page with no dates is dated from its file", func(t *testing.T) {
		page := built.page(t, "neither/index.html")
		if !regexp.MustCompile(`"dateModified": "\d{4}-\d{2}-\d{2}"`).MatchString(page) {
			t.Error("a page with no declared date carries no ISO modification date")
		}
	})

	t.Run("the sitemap carries the modification dates", func(t *testing.T) {
		assertCarries(t, "sitemap.xml", built.page(t, "sitemap.xml"),
			"<lastmod>2026-05-01</lastmod>", "<lastmod>2026-01-15</lastmod>")
	})
}

func TestBuildRefusals(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("a directory that is not a project", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := Build(Options{DirPath: dir, Stdout: &bytes.Buffer{}}, effects.Unbound()); err == nil {
			t.Fatal("a directory with no selfdoc.json built anyway")
		} else if !strings.Contains(err.Error(), "No selfdoc.json found") {
			t.Errorf("the refusal is %q, want one naming the missing config", err)
		}
	})

	t.Run("a project with no docs directory", func(t *testing.T) {
		dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
		if err := os.RemoveAll(filepath.Join(dir, ".stricttools", "docs")); err != nil {
			t.Fatalf("removing the docs tree: %v", err)
		}
		_, err := Build(Options{DirPath: dir, Stdout: &bytes.Buffer{}}, effects.Unbound())
		if err == nil {
			t.Fatal("a project with no docs directory built anyway")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("the refusal is %q, want one naming the missing directory", err)
		}
	})

	t.Run("a config with no versions", func(t *testing.T) {
		assertConfigRefusal(t, "versions", map[string]any{"versions": nil})
	})

	t.Run("a config with no locales", func(t *testing.T) {
		assertConfigRefusal(t, "locales", map[string]any{"locales": nil})
	})

	t.Run("the versions guidance names no retired key", func(t *testing.T) {
		err := configRefusal(t, map[string]any{"versions": nil})
		if strings.Contains(err.Error(), "indexed") {
			t.Errorf("the guidance offers the retired 'indexed' key: %q", err)
		}
	})

	t.Run("a config with no author", func(t *testing.T) {
		dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
		raw := testproject.DefaultConfig(map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
		delete(raw, "author")
		testproject.WriteJSON(t, filepath.Join(dir, "selfdoc.json"), raw)
		_, err := Build(Options{DirPath: dir, Stdout: &bytes.Buffer{}}, effects.Unbound())
		if err == nil {
			t.Fatal("a project with no declared author built anyway")
		}
		if !strings.Contains(err.Error(), "author") {
			t.Errorf("the refusal is %q, want one naming the missing author", err)
		}
	})

	t.Run("a version the config does not declare", func(t *testing.T) {
		_, err := tryBuildFixture(t, fixture{
			Build: func(o *Options) { o.VersionFilter = "9.9.9" },
		})
		if err == nil {
			t.Fatal("a filter naming an undeclared version built anyway")
		}
		assertCarries(t, "the refusal", err.Error(), "9.9.9", "Available: 1.0.0")
	})

	t.Run("a locale the config does not declare", func(t *testing.T) {
		_, err := tryBuildFixture(t, fixture{
			Build: func(o *Options) { o.LocaleFilter = "fr" },
		})
		if err == nil {
			t.Fatal("a filter naming an undeclared locale built anyway")
		}
		assertCarries(t, "the refusal", err.Error(), "fr", "Available: en")
	})

	t.Run("a declared changelog that is not there", func(t *testing.T) {
		_, err := tryBuildFixture(t, fixture{
			Config: map[string]any{"changelog": "NOTES.md"},
		})
		if err == nil {
			t.Fatal("a project naming a changelog that is not there built anyway")
		}
		assertCarries(t, "the refusal", err.Error(), "NOTES.md", "CHANGELOG.md")
	})
}

// configRefusal builds a fixture whose config carries the overrides and
// returns the error it refused with.
func configRefusal(t *testing.T, overrides map[string]any) error {
	t.Helper()
	dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
	raw := testproject.DefaultConfig(map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
	for key, value := range overrides {
		if value == nil {
			delete(raw, key)
			continue
		}
		raw[key] = value
	}
	testproject.WriteJSON(t, filepath.Join(dir, "selfdoc.json"), raw)
	loaded, err := config.ValidateConfig(map[string]any(raw))
	if err != nil {
		t.Fatalf("the fixture config does not load: %v", err)
	}
	// The key is removed from the resolved config too: the build's own
	// check is what this exercises, not the loader's.
	for key, value := range overrides {
		if value == nil {
			loaded[key] = nil
		}
	}
	_, buildErr := Build(Options{DirPath: dir, Config: loaded, Stdout: &bytes.Buffer{}}, effects.Unbound())
	if buildErr == nil {
		t.Fatal("the build accepted a config it should have refused")
	}
	return buildErr
}

// assertConfigRefusal checks that a missing config key is refused as a config
// error naming the key.
func assertConfigRefusal(t *testing.T, key string, overrides map[string]any) {
	t.Helper()
	err := configRefusal(t, overrides)
	var configErr *config.ConfigError
	if !errorsAs(err, &configErr) {
		t.Fatalf("the refusal is a %T, want a *config.ConfigError: %v", err, err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Errorf("the refusal is %q, want one naming %q", err, key)
	}
}

// errorsAs is errors.As, wrapped so the test file needs no import for one use.
func errorsAs(err error, target **config.ConfigError) bool {
	for err != nil {
		if typed, ok := err.(*config.ConfigError); ok {
			*target = typed
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
