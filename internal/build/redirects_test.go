package build

import (
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// twoLocaleFixture is a project with two locales, which is the case where a
// locale segment exists at all: a single-locale site drops the segment, so the
// root redirect stub only has a target when there is more than one locale.
func twoLocaleFixture(t *testing.T, overrides map[string]any) site {
	t.Helper()
	config := map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
		"locales": []any{
			map[string]any{"code": "en", "label": "English", "default": true},
			map[string]any{"code": "fr", "label": "French"},
		},
	}
	for key, value := range overrides {
		config[key] = value
	}
	dir := testproject.Make(t, config)
	for _, locale := range []string{"en", "fr"} {
		testproject.WriteText(t, filepath.Join(dir, "stricttools", "docs", locale, "index.md"),
			"# Test ("+locale+")\n\nContent.\n")
	}
	written, err := Build(Options{DirPath: dir, Stdout: &discard{}}, effects.Unbound())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return site{dir: dir, output: filepath.Join(dir, "stricttools", ".docs-cache", "build"), written: written}
}

// stubHops lists every same-site hop a redirect stub emits.
//
// A stub states its target four times -- the meta refresh, the scripted
// replace, the anchor href and the anchor text -- and all of them must resolve
// identically, so the assertions read the whole set rather than one of them.
func stubHops(html string) []string {
	seen := map[string]bool{}
	for _, pattern := range []string{
		`content="0;url=([^"]*)"`,
		`window\.location\.replace\("([^"]*)"\)`,
		`<a href="([^"]*)">`,
	} {
		for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(html, -1) {
			seen[match[1]] = true
		}
	}
	hops := make([]string, 0, len(seen))
	for hop := range seen {
		hops = append(hops, hop)
	}
	sort.Strings(hops)
	return hops
}

// stubCanonical is the one canonical URL a stub declares.
func stubCanonical(t *testing.T, html string) string {
	t.Helper()
	found := regexp.MustCompile(`<link rel="canonical" href="([^"]*)">`).FindAllStringSubmatch(html, -1)
	if len(found) != 1 {
		t.Fatalf("the stub declares %d canonical links, want one", len(found))
	}
	return found[0][1]
}

// assertHopsResolveTo checks that every hop a stub emits resolves, against the
// address the stub is served at, to the canonical the stub declares.
func assertHopsResolveTo(t *testing.T, html, servedAt, canonical string) {
	t.Helper()
	base, err := url.Parse(servedAt)
	if err != nil {
		t.Fatalf("parsing %q: %v", servedAt, err)
	}
	hops := stubHops(html)
	if len(hops) == 0 {
		t.Fatal("the stub emitted no hop at all")
	}
	for _, hop := range hops {
		relative, parseErr := url.Parse(hop)
		if parseErr != nil {
			t.Fatalf("parsing the hop %q: %v", hop, parseErr)
		}
		if resolved := base.ResolveReference(relative).String(); resolved != canonical {
			t.Errorf("the hop %q resolves to %q, want the canonical %q", hop, resolved, canonical)
		}
	}
}

func TestBuildRootRedirectStub(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a single-locale site writes no stub over its home page", func(t *testing.T) {
		// The current version mounts at the output root, so the root index
		// IS the home page: a stub there would overwrite the page it
		// redirects to.
		built := buildFixture(t, fixture{})
		index := built.page(t, "index.html")
		assertLacks(t, "index.html", index, `meta http-equiv="refresh"`)
		assertCarries(t, "index.html", index,
			`<link rel="canonical" href="https://example.com/">`)
		if redirects := built.page(t, "_redirects"); strings.Contains(redirects, "302") {
			t.Errorf("_redirects carries a root rule: %q", redirects)
		}
	})

	t.Run("a multi-locale site redirects into the default locale", func(t *testing.T) {
		built := twoLocaleFixture(t, nil)
		index := built.page(t, "index.html")
		// The canonical is absolute: a root-relative one resolves against
		// whatever host served the stub, so every alias of the site would
		// claim to be canonical. The hop is document-relative: a same-site
		// hop that must keep working when the site is served under a path
		// prefix.
		assertCarries(t, "index.html", index,
			`<link rel="canonical" href="https://example.com/en/">`,
			`content="0;url=en/"`)
		// Cloudflare only ever reads the root _redirects, where there is no
		// document to resolve a relative target against.
		assertCarries(t, "_redirects", built.page(t, "_redirects"), "/ /en/ 302\n")
	})

	t.Run("the hop resolves under an assembly's slug prefix", func(t *testing.T) {
		// An assembly serves each project's output under /<slug>/, so a
		// root-relative hop would leave the project entirely and land on
		// whatever the assembly root serves.
		built := twoLocaleFixture(t, map[string]any{
			"topology": map[string]any{
				"slug": "proj", "docs_base": "https://docs.example.com",
			},
		})
		index := built.page(t, "index.html")
		canonical := stubCanonical(t, index)
		if canonical != "https://docs.example.com/proj/en/" {
			t.Fatalf("the stub canonicalizes to %q, want the mounted address", canonical)
		}
		assertHopsResolveTo(t, index, "https://docs.example.com/proj/", canonical)
	})

	t.Run("the hop resolves under a subpath base URL", func(t *testing.T) {
		// GitHub Pages project sites live at /<repo>/; a root-relative hop
		// would leave the repository's site the same way it leaves an
		// assembly slug.
		built := twoLocaleFixture(t, map[string]any{
			"base_url": "https://owner.github.io/repo",
		})
		index := built.page(t, "index.html")
		canonical := stubCanonical(t, index)
		if canonical != "https://owner.github.io/repo/en/" {
			t.Fatalf("the stub canonicalizes to %q, want the subpath address", canonical)
		}
		assertHopsResolveTo(t, index, "https://owner.github.io/repo/", canonical)
	})

	t.Run("the hop resolves at an origin root", func(t *testing.T) {
		built := twoLocaleFixture(t, nil)
		index := built.page(t, "index.html")
		canonical := stubCanonical(t, index)
		if canonical != "https://example.com/en/" {
			t.Fatalf("the stub canonicalizes to %q, want the origin-root address", canonical)
		}
		assertHopsResolveTo(t, index, "https://example.com/", canonical)
	})
}

func TestBuildConfigRedirects(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a declared redirect becomes a stub and a rule", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{
			"redirects": []any{map[string]any{"from": "edit-release", "to": "release/edit"}},
		}})
		if !built.reported("edit-release/index.html") {
			t.Fatal("the redirect stub was not reported written")
		}
		stub := built.page(t, "edit-release/index.html")
		// The hop is document-relative so it survives being served under a
		// path prefix; the canonical names the absolute target.
		assertCarries(t, "edit-release/index.html", stub,
			`content="0;url=../release/edit/"`,
			`<link rel="canonical" href="https://example.com/release/edit/">`)
		assertCarries(t, "_redirects", built.page(t, "_redirects"),
			"/edit-release/ /release/edit/ 301\n")
		assertCarries(t, "the build's output", built.stdout, "Generated 1 redirect(s)")
	})

	t.Run("a single-locale site gets the rule and no root rule", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{
			"redirects": []any{map[string]any{"from": "old-cmd", "to": "new-cmd"}},
		}})
		redirects := built.page(t, "_redirects")
		assertCarries(t, "_redirects", redirects, "/old-cmd/ /new-cmd/ 301\n")
		assertLacks(t, "_redirects", redirects, "302")
	})

	t.Run("a stub's hop resolves under an assembly's slug prefix", func(t *testing.T) {
		// These stubs sit a directory deep, so their hop resolves against
		// /<slug>/<from>/ rather than the site root.
		built := buildFixture(t, fixture{Config: map[string]any{
			"topology": map[string]any{
				"slug": "proj", "docs_base": "https://docs.example.com",
			},
			"redirects": []any{map[string]any{"from": "edit-release", "to": "release/edit"}},
		}})
		stub := built.page(t, "edit-release/index.html")
		canonical := stubCanonical(t, stub)
		if canonical != "https://docs.example.com/proj/release/edit/" {
			t.Fatalf("the stub canonicalizes to %q, want the mounted target", canonical)
		}
		assertHopsResolveTo(t, stub, "https://docs.example.com/proj/edit-release/", canonical)
	})

	t.Run("a page that already exists is not replaced by a stub", func(t *testing.T) {
		built := buildFixture(t, fixture{Config: map[string]any{
			"redirects": []any{map[string]any{"from": "index", "to": "new-index"}},
		}})
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index, "Test Project")
		assertLacks(t, "index.html", index, `meta http-equiv="refresh"`)
	})

	t.Run("a redirect expands across every locale and version", func(t *testing.T) {
		dir := testproject.MakeVersioned(t, []string{"0.9.0", "1.0.0"}, map[string]any{
			"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
			"locales": []any{
				map[string]any{"code": "en", "label": "English", "default": true},
				map[string]any{"code": "fr", "label": "French"},
			},
			"redirects": []any{map[string]any{"from": "old-page", "to": "new-page"}},
		})
		// The tagged fixture writes docs/index.md; the locale directories
		// are what a localized build reads, so they are added and committed
		// under both tags.
		for _, locale := range []string{"en", "fr"} {
			testproject.WriteText(t, filepath.Join(dir, "stricttools", "docs", locale, "index.md"),
				"# Test ("+locale+")\n\nContent.\n")
		}
		testproject.Git(t, dir, "add", "stricttools")
		testproject.Git(t, dir, "commit", "-m", "locale docs")
		testproject.Git(t, dir, "tag", "-f", "v0.9.0")
		testproject.Git(t, dir, "tag", "-f", "v1.0.0")

		written, err := Build(Options{DirPath: dir, Stdout: &discard{}}, effects.Unbound())
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		built := site{dir: dir, output: filepath.Join(dir, "stricttools", ".docs-cache", "build"), written: written}

		// Four combinations, each at the address its version is emitted at:
		// the current version at the stable mount, the superseded one under
		// the archive prefix.
		mounts := map[string]string{
			"en/1.0.0": "en",
			"fr/1.0.0": "fr",
			"en/0.9.0": "en/v/0.9.0",
			"fr/0.9.0": "fr/v/0.9.0",
		}
		redirects := built.page(t, "_redirects")
		for combination, mount := range mounts {
			outputKey := mount + "/old-page/index.html"
			if !built.exists(outputKey) {
				t.Errorf("no redirect stub for %s", combination)
				continue
			}
			assertCarries(t, outputKey, built.page(t, outputKey),
				"https://example.com/"+mount+"/new-page/")
			rule := "/" + mount + "/old-page/ /" + mount + "/new-page/ 301"
			assertCarries(t, "_redirects", redirects, rule)
		}
	})
}
