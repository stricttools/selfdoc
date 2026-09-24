package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/testisolation/go/hygiene"
)

// What a checkout has to declare before the assembly can serve it, and what
// its build tree has to have been produced under when the preview is not
// rebuilding it.

func TestTheCheckouts(t *testing.T) {
	hygiene.Isolate(t)
	handle := effects.Unbound()

	t.Run("a checkout with no config is refused", func(t *testing.T) {
		empty := filepath.Join(t.TempDir(), "empty")
		testproject.MkdirAll(t, empty)
		_, err := ReadSlug(empty)
		if err == nil || !strings.Contains(err.Error(), "no selfdoc.json") {
			t.Fatalf("ReadSlug = %v, want the refusal", err)
		}
	})

	t.Run("a checkout with no slug is refused", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "noslug")
		testproject.WriteJSON(t, filepath.Join(root, "selfdoc.json"), map[string]any{
			"name":          "No Slug",
			"base_url":      canonicalBase,
			"unversioned":   true,
			"search_engine": "pagefind",
			"author":        map[string]any{"name": "Test Author", "url": "https://author.example"},
			"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
		})
		_, err := ReadSlug(root)
		if err == nil || !strings.Contains(err.Error(), "no topology.slug") {
			t.Fatalf("ReadSlug = %v, want the refusal", err)
		}
	})

	t.Run("a declared slug is read back", func(t *testing.T) {
		root := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		slug, err := ReadSlug(root)
		if err != nil {
			t.Fatalf("ReadSlug: %v", err)
		}
		if slug != "home" {
			t.Errorf("ReadSlug = %q, want home", slug)
		}
	})

	t.Run("two checkouts with the same slug are refused", func(t *testing.T) {
		testproject.RequirePagefind(t)
		home := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		twin := writeCheckout(t, checkoutSpec{
			Root: filepath.Join(t.TempDir(), "twin"), Slug: "home", Name: "Twin",
			Version: "1.0.0", Pages: []fixturePage{{Path: "index.md", Title: "Twin"}},
		})
		_, err := PreviewAssembly(home, []string{twin}, filepath.Join(t.TempDir(), "out"),
			canonicalBase, false, "", handle)
		if err == nil || !strings.Contains(err.Error(), "declare the slug 'home'") {
			t.Fatalf("PreviewAssembly = %v, want the refusal", err)
		}
	})

	t.Run("a canonical base is required", func(t *testing.T) {
		home := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		_, err := PreviewAssembly(home, nil, filepath.Join(t.TempDir(), "out"),
			"", false, "", handle)
		if err == nil || !strings.Contains(err.Error(), "canonical_base is required") {
			t.Fatalf("PreviewAssembly = %v, want the refusal", err)
		}
	})

	t.Run("an unknown theme is refused by name", func(t *testing.T) {
		home := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		_, err := PreviewAssembly(home, nil, filepath.Join(t.TempDir(), "out"),
			canonicalBase, false, "nosuchtheme", handle)
		if err == nil || !strings.Contains(err.Error(), "unknown theme 'nosuchtheme'") {
			t.Fatalf("PreviewAssembly = %v, want the refusal", err)
		}
		if !strings.Contains(err.Error(), "available themes: ") {
			t.Errorf("the refusal does not list the themes: %v", err)
		}
	})
}

func TestTheThemeOfAnAlreadyBuiltTree(t *testing.T) {
	hygiene.Isolate(t)
	handle := effects.Unbound()
	// "minimal" is the theme a build falls back to when a config names none.
	theme := "minimal"

	// A theme reaches a page twice: the build inlines its critical part into
	// the page's own head, and the page then references the site's chrome
	// asset for the rest. Without --build only the second one can come from
	// the named theme, so the first is checked rather than assumed.

	t.Run("a checkout with no build output was not built under any theme", func(t *testing.T) {
		root := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		if err := os.RemoveAll(filepath.Join(root, ".stricttools", "docs-cache", "build")); err != nil {
			t.Fatalf("removing the build tree: %v", err)
		}
		built, err := BuiltUnderTheme(root, theme)
		if err != nil {
			t.Fatalf("BuiltUnderTheme: %v", err)
		}
		if built {
			t.Error("a checkout with no build output claimed the theme")
		}
	})

	t.Run("a stylesheet byte for byte the build's counts", func(t *testing.T) {
		root := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		expected, err := ExpectedStylesheet(theme)
		if err != nil {
			t.Fatalf("ExpectedStylesheet: %v", err)
		}
		rel, err := themes.CSSRel(theme)
		if err != nil {
			t.Fatalf("CSSRel: %v", err)
		}
		css := filepath.Join(root, ".stricttools", "docs-cache", "build", filepath.FromSlash(rel))
		testproject.WriteText(t, css, expected)
		built, err := BuiltUnderTheme(root, theme)
		if err != nil {
			t.Fatalf("BuiltUnderTheme: %v", err)
		}
		if !built {
			t.Error("the recomputed stylesheet did not match the build's")
		}

		// One byte off is a page rendered against another theme.
		testproject.WriteText(t, css, expected+"\n/* drifted */")
		built, err = BuiltUnderTheme(root, theme)
		if err != nil {
			t.Fatalf("BuiltUnderTheme: %v", err)
		}
		if built {
			t.Error("a drifted stylesheet claimed the theme")
		}
	})

	t.Run("no build with a theme refuses every stale checkout by name", func(t *testing.T) {
		home := homeCheckout(t, filepath.Join(t.TempDir(), "home"))
		_, err := PreviewAssembly(home, nil, filepath.Join(t.TempDir(), "out"),
			canonicalBase, false, theme, handle)
		if err == nil {
			t.Fatal("the stale build tree was accepted")
		}
		if !strings.Contains(err.Error(), "was not produced under that theme") {
			t.Errorf("the refusal does not say what is wrong: %v", err)
		}
		if !strings.Contains(err.Error(), "home") {
			t.Errorf("the refusal does not name the checkout: %v", err)
		}
	})
}
