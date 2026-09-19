package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// twoLocales is the locale pair the localized fixtures declare.
func twoLocales() []map[string]any {
	return []map[string]any{
		{"code": "en", "label": "English", "default": true},
		{"code": "fa", "label": "Persian"},
	}
}

// buildLocalized writes a per-locale project, builds it, and returns the site.
func buildLocalized(t *testing.T, locales []map[string]any, apply func(*Options)) (site, error) {
	t.Helper()
	dir := testproject.MakeLocalized(t, locales, map[string]any{
		"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/",
	})
	opts := Options{DirPath: dir, Stdout: &discard{}}
	if apply != nil {
		apply(&opts)
	}
	written, err := Build(opts, effects.Unbound())
	return site{
		dir:     dir,
		output:  filepath.Join(dir, ".stricttools", "docs-cache", "build"),
		written: written,
	}, err
}

func TestBuildMultipleLocales(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built, err := buildLocalized(t, twoLocales(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	t.Run("each locale gets its own mount", func(t *testing.T) {
		// Two locales keep the locale segment; the current version drops
		// the version segment.
		for _, outputKey := range []string{"en/index.html", "fa/index.html"} {
			if !built.exists(outputKey) {
				t.Errorf("%s was not written", outputKey)
			}
		}
	})

	t.Run("each locale carries its own docs directory's content", func(t *testing.T) {
		assertCarries(t, "en/index.html", built.page(t, "en/index.html"), "English")
		assertCarries(t, "fa/index.html", built.page(t, "fa/index.html"), "Persian")
	})

	t.Run("every page declares the other locales", func(t *testing.T) {
		english := built.page(t, "en/index.html")
		assertCarries(t, "en/index.html", english, `hreflang="en"`, `hreflang="fa"`)
		xDefault := regexp.MustCompile(
			`hreflang="x-default" href="([^"]+)"`).FindStringSubmatch(english)
		if xDefault == nil {
			t.Fatal("no x-default alternate is declared")
		}
		if !strings.Contains(xDefault[1], "/en/") {
			t.Errorf("x-default points at %q, want the default locale's mount", xDefault[1])
		}
	})

	t.Run("the sitemaps are per locale, under one index", func(t *testing.T) {
		for _, outputKey := range []string{"en/sitemap.xml", "fa/sitemap.xml", "sitemap-index.xml"} {
			if !built.exists(outputKey) {
				t.Errorf("%s was not written", outputKey)
			}
		}
		assertCarries(t, "sitemap-index.xml", built.page(t, "sitemap-index.xml"),
			"en/sitemap.xml", "fa/sitemap.xml")
		assertCarries(t, "robots.txt", built.page(t, "robots.txt"), "sitemap-index.xml")
	})

	t.Run("the locale picker offers both locales", func(t *testing.T) {
		assertCarries(t, "en/index.html", built.page(t, "en/index.html"),
			"locale-picker", "English", "Persian")
	})

	t.Run("the root index redirects into the default locale's mount", func(t *testing.T) {
		root := built.page(t, "index.html")
		assertCarries(t, "index.html", root, `content="0;url=en/"`)
		assertCarries(t, "_redirects", built.page(t, "_redirects"), "/ /en/ 302\n")
	})
}

func TestBuildSingleLocale(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{})

	t.Run("a project with no locale subdirectory builds from docs/", func(t *testing.T) {
		if !built.exists("index.html") {
			t.Error("the page is not the root index")
		}
	})

	t.Run("a single locale declares no alternates", func(t *testing.T) {
		assertLacks(t, "index.html", built.page(t, "index.html"), "hreflang")
	})

	t.Run("a single locale gets one sitemap and no index", func(t *testing.T) {
		if built.exists("sitemap-index.xml") {
			t.Error("a single-locale project got a sitemap index")
		}
		robots := built.page(t, "robots.txt")
		assertCarries(t, "robots.txt", robots, "sitemap.xml")
		assertLacks(t, "robots.txt", robots, "sitemap-index.xml")
	})

	t.Run("a single-locale site writes no root redirect rule", func(t *testing.T) {
		// The current version mounts at the output root, so there is
		// nothing to redirect to.
		redirects := built.page(t, "_redirects")
		if redirects != "" {
			t.Errorf("_redirects is %q, want it empty", redirects)
		}
	})
}

func TestBuildLocaleFilter(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built, err := buildLocalized(t, twoLocales(), func(o *Options) { o.LocaleFilter = "en" })
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !built.exists("en/index.html") {
		t.Error("the named locale was not built")
	}
	if built.exists("fa") {
		t.Error("a locale the filter excluded was built anyway")
	}
}

func TestBuildMissingLocaleDirectory(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	dir := testproject.MakeLocalized(t, twoLocales(), map[string]any{
		"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/",
	})
	if err := os.RemoveAll(filepath.Join(dir, ".stricttools", "docs", "fa")); err != nil {
		t.Fatalf("removing the locale's docs tree: %v", err)
	}
	_, err := Build(Options{DirPath: dir, Stdout: &discard{}}, effects.Unbound())
	if err == nil {
		t.Fatal("a project declaring a locale it has no directory for built anyway")
	}
	assertCarries(t, "the refusal", err.Error(), "Locale directory", "fa")
}
