package chrome

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
)

const canonicalBase = "https://docs.example.com"

var (
	stylesheetPattern = regexp.MustCompile(`(?i)<link\b[^>]*\brel="stylesheet"[^>]*>`)
	anyHrefPattern    = regexp.MustCompile(`href="([^"]*)"`)
)

// -- fixtures -----------------------------------------------------------------

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

func loadedManifest(slug, name, version, theme string) map[string]any {
	loaded := map[string]any{
		"schema_version": 1,
		"name":           name,
		"slug":           slug,
		"version":        version,
		"description":    name + " docs",
		"language":       "python",
		"base_url":       canonicalBase + "/" + slug,
		"pages":          []any{map[string]any{"path": "index.md", "title": "Home"}},
		"posts":          []any{},
		"last_gen":       "2024-01-01T00:00:00+00:00",
	}
	if theme != "" {
		loaded["theme"] = theme
	}
	return loaded
}

// builtPage is a page shaped the way a project's build shapes one.
//
// Three references to the same stylesheet -- the preload, the async stylesheet
// and the <noscript> fallback -- because that is what the page wrapper writes
// and all three have to be re-pointed together.
func builtPage(cssHref, canonical string) string {
	return "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n" +
		"<title>A page</title>\n" +
		`<link rel="canonical" href="` + canonical + "\">\n" +
		`<link rel="preload" href="` + cssHref + "\" as=\"style\">\n" +
		`<link rel="stylesheet" href="` + cssHref + `" media="print" ` +
		"onload=\"this.media='all'\">" +
		`<noscript><link rel="stylesheet" href="` + cssHref + `"></noscript>` + "\n" +
		"</head>\n<body>\n<p>body</p>\n</body>\n</html>\n"
}

// tree writes a site with one project subtree, as a graft leaves one, and
// returns its root.
func tree(t *testing.T) string {
	t.Helper()
	site := filepath.Join(t.TempDir(), "site")
	write(t, filepath.Join(site, "alpha", "index.html"),
		builtPage("style.css", canonicalBase+"/alpha/"))
	write(t, filepath.Join(site, "alpha", "guide", "index.html"),
		builtPage("../style.css", canonicalBase+"/alpha/guide/"))
	write(t, filepath.Join(site, "alpha", "style.css"), "/* alpha's own copy */")
	write(t, filepath.Join(site, "blog", "hello", "index.html"),
		builtPage("../../style.css", canonicalBase+"/blog/hello/"))
	return site
}

// repointTree runs the pass a deploy runs: write the asset set for the themes
// the roster declares, then re-point every emitted page at it.
func repointTree(t *testing.T, site string, manifests []map[string]any, homeSlug string) (map[string]string, []string) {
	t.Helper()
	bySlug, homeTheme := Themes(manifests, homeSlug, "")
	themeNames := make([]string, 0, len(bySlug)+1)
	for _, theme := range bySlug {
		themeNames = append(themeNames, theme)
	}
	themeNames = append(themeNames, homeTheme)
	handle := effects.Unbound()
	assets, err := WriteAssets(site, themeNames, handle)
	if err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	pages, err := EmittedPages(site)
	if err != nil {
		t.Fatalf("EmittedPages: %v", err)
	}
	changed, err := RepointPages(site, pages, bySlug, assets, homeTheme, handle)
	if err != nil {
		t.Fatalf("RepointPages: %v", err)
	}
	return assets, changed
}

// stylesheets is every rel=stylesheet href on a page, in document order.
func stylesheets(pageHTML string) []string {
	found := make([]string, 0)
	for _, tag := range stylesheetPattern.FindAllString(pageHTML, -1) {
		if match := anyHrefPattern.FindStringSubmatch(tag); match != nil {
			found = append(found, match[1])
		}
	}
	return found
}

// resolve is the site-relative file a document-relative ref on a page names.
func resolve(pageRel, ref string) string {
	return filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(pageRel), ref)))
}

func entryNames(t *testing.T, chromeDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(chromeDir)
	if err != nil {
		t.Fatalf("reading %s: %v", chromeDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// -- the composed stylesheet --------------------------------------------------

// TestSharedPageCSSMatchesTheReference pins the assembly's own rules against
// the bytes recorded from the Python by
// scripts/record_blog_chrome_reference.py.
func TestSharedPageCSSMatchesTheReference(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile(filepath.Join("testdata", "shared_page.css"))
	if err != nil {
		t.Fatalf("reading the reference: %v", err)
	}
	if sharedPageCSS != string(want) {
		t.Errorf("the shared-page rules do not match the reference\n--- got ---\n%s\n--- want ---\n%s",
			sharedPageCSS, want)
	}
}

// TestCSSCarriesTheThemeAndTheAssemblysOwnRules: theme rules and the
// assembly's shared-page rules, in one file.
func TestCSSCarriesTheThemeAndTheAssemblysOwnRules(t *testing.T) {
	t.Parallel()
	css, err := CSS(DefaultTheme)
	if err != nil {
		t.Fatalf("CSS: %v", err)
	}
	for _, want := range []string{
		"#tm-topbar", ".blog-entry", ".shared-page", ".sibling-projects",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the composed stylesheet does not carry %q", want)
		}
	}
	// Minified: the comment banners the pieces carry are gone.
	if strings.Contains(css, "assembly shared pages") {
		t.Error("the composed stylesheet was not minified")
	}
}

// TestCSSStylesTheSiblingBlock pins the selectors the assembly's sibling block
// is painted by.
//
// The block is written by the build on every assembled page and is styled
// nowhere else: no theme carries its class, and a project's own "style.css"
// never sees it. Without these rules it renders as a bare heading over a
// bulleted list.
func TestCSSStylesTheSiblingBlock(t *testing.T) {
	t.Parallel()
	for _, theme := range []string{"minimal", "clean", "tinymoon"} {
		css, err := CSS(theme)
		if err != nil {
			t.Fatalf("CSS(%q): %v", theme, err)
		}
		for _, want := range []string{
			// The section, separated from the article above it by
			// the theme's own border colour.
			".sibling-projects{",
			"border-top:1px solid var(--border)",
			// Its heading, kept a heading element and painted as
			// secondary text.
			".sibling-projects > h2{",
			// The list: a responsive grid carrying no markers.
			".sibling-projects ul{",
			"list-style:none",
			"grid-template-columns:repeat(auto-fill,minmax(",
			// The one-line description beside each name.
			".sibling-projects li span{",
			"color:var(--text-secondary)",
		} {
			if !strings.Contains(css, want) {
				t.Errorf("the %s stylesheet does not carry %q", theme, want)
			}
		}
	}
}

// TestSiblingBlockCarriesNoColourOfItsOwn: the block's rules name theme
// tokens, so the light and dark palettes paint it without a second set of
// rules and a new theme needs no change here.
func TestSiblingBlockCarriesNoColourOfItsOwn(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"#", "rgb(", "hsl("} {
		if strings.Contains(siblingCSS, literal) {
			t.Errorf("the sibling-block rules name a literal colour (%q); "+
				"they have to read the theme's tokens", literal)
		}
	}
}

// TestCSSIsDeterministic: identical content has to produce an identical name,
// so the composition cannot depend on map order or a clock.
func TestCSSIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, theme := range []string{"minimal", "clean", "tinymoon"} {
		first, err := CSS(theme)
		if err != nil {
			t.Fatalf("CSS(%q): %v", theme, err)
		}
		second, err := CSS(theme)
		if err != nil {
			t.Fatalf("CSS(%q): %v", theme, err)
		}
		if first != second {
			t.Errorf("CSS(%q) is not deterministic", theme)
		}
	}
}

// TestCSSRefusesAnUnknownTheme: a theme the toolchain does not ship is an
// error, never an empty stylesheet.
func TestCSSRefusesAnUnknownTheme(t *testing.T) {
	t.Parallel()
	if _, err := CSS("no-such-theme"); err == nil {
		t.Error("CSS accepted a theme the toolchain does not ship")
	}
}

// -- AssetRel -----------------------------------------------------------------

// TestTheAssetNameIsContentHashed: the hash is the only name that is as stable
// as the bytes it addresses.
func TestTheAssetNameIsContentHashed(t *testing.T) {
	t.Parallel()
	css, err := CSS(DefaultTheme)
	if err != nil {
		t.Fatalf("CSS: %v", err)
	}
	first, err := AssetRel(DefaultTheme, css)
	if err != nil {
		t.Fatalf("AssetRel: %v", err)
	}
	again, err := AssetRel(DefaultTheme, css)
	if err != nil {
		t.Fatalf("AssetRel: %v", err)
	}
	if first != again {
		t.Errorf("AssetRel is not deterministic: %q then %q", first, again)
	}
	changed, err := AssetRel(DefaultTheme, css+"\n.x{}")
	if err != nil {
		t.Fatalf("AssetRel: %v", err)
	}
	if changed == first {
		t.Errorf("changed content kept the name %q", first)
	}
	if !strings.HasPrefix(first, Dir+"/"+DefaultTheme+"-") ||
		!strings.HasSuffix(first, ".css") {
		t.Errorf("a plain theme's asset is not one named file: %q", first)
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(first, Dir+"/"+DefaultTheme+"-"), ".css")
	if len(digest) != hashLength {
		t.Errorf("digest %q is %d characters, want %d", digest, len(digest), hashLength)
	}
}

// TestAFrameworkThemesAssetIsADirectory: the framework's @font-face rules and
// the page's module imports are addressed relative to the sheet.
func TestAFrameworkThemesAssetIsADirectory(t *testing.T) {
	t.Parallel()
	css, err := CSS("tinymoon")
	if err != nil {
		t.Fatalf("CSS: %v", err)
	}
	rel, err := AssetRel("tinymoon", css)
	if err != nil {
		t.Fatalf("AssetRel: %v", err)
	}
	if !strings.HasPrefix(rel, Dir+"/tinymoon-") || !strings.HasSuffix(rel, "/css/style.css") {
		t.Errorf("a framework theme's asset is not a payload directory: %q", rel)
	}
	// The digest covers the module payload as well as the stylesheet, so
	// changing only a module has to rename the directory. The modules cannot
	// be edited here, so the assertion is that the two digests differ: the
	// framework digest is computed over more material than the sheet alone.
	plain, err := AssetRel(DefaultTheme, css)
	if err != nil {
		t.Fatalf("AssetRel: %v", err)
	}
	frameworkDigest := strings.Split(strings.TrimPrefix(rel, Dir+"/tinymoon-"), "/")[0]
	plainDigest := strings.TrimSuffix(strings.TrimPrefix(plain, Dir+"/"+DefaultTheme+"-"), ".css")
	if frameworkDigest == plainDigest {
		t.Error("the framework digest covers only the stylesheet")
	}
}

// -- theme keying -------------------------------------------------------------

// TestAManifestWithoutAThemeUsesTheBuildsOwnDefault: every manifest published
// so far names none.
func TestAManifestWithoutAThemeUsesTheBuildsOwnDefault(t *testing.T) {
	t.Parallel()
	if got := ManifestTheme(loadedManifest("alpha", "Alpha", "1.0.0", "")); got != DefaultTheme {
		t.Errorf("ManifestTheme = %q, want %q", got, DefaultTheme)
	}
	if DefaultTheme != "minimal" {
		t.Errorf("DefaultTheme = %q, want the build's own default %q", DefaultTheme, "minimal")
	}
}

func TestAManifestThemeIsHonoured(t *testing.T) {
	t.Parallel()
	if got := ManifestTheme(loadedManifest("alpha", "Alpha", "1.0.0", "clean")); got != "clean" {
		t.Errorf("ManifestTheme = %q, want %q", got, "clean")
	}
}

func TestThemesAreKeyedBySlugWithTheHomeThemeNamed(t *testing.T) {
	t.Parallel()
	bySlug, homeTheme := Themes([]map[string]any{
		loadedManifest("home", "Home", "0.1.0", "clean"),
		loadedManifest("alpha", "Alpha", "1.0.0", ""),
	}, "home", "")
	want := map[string]string{"home": "clean", "alpha": DefaultTheme}
	if len(bySlug) != len(want) {
		t.Fatalf("bySlug = %#v, want %#v", bySlug, want)
	}
	for slug, theme := range want {
		if bySlug[slug] != theme {
			t.Errorf("bySlug[%q] = %q, want %q", slug, bySlug[slug], theme)
		}
	}
	if homeTheme != "clean" {
		t.Errorf("homeTheme = %q, want %q", homeTheme, "clean")
	}
}

// TestAnOverrideMakesEveryProjectDeclareOneTheme: the preview builds every
// checkout under one theme, so the asset a page references has to be that
// theme's whatever the manifest says.
func TestAnOverrideMakesEveryProjectDeclareOneTheme(t *testing.T) {
	t.Parallel()
	bySlug, homeTheme := Themes([]map[string]any{
		loadedManifest("home", "Home", "0.1.0", "clean"),
		loadedManifest("alpha", "Alpha", "1.0.0", ""),
	}, "home", "tinymoon")
	for slug, theme := range bySlug {
		if theme != "tinymoon" {
			t.Errorf("bySlug[%q] = %q, want the override %q", slug, theme, "tinymoon")
		}
	}
	if homeTheme != "tinymoon" {
		t.Errorf("homeTheme = %q, want the override %q", homeTheme, "tinymoon")
	}
}

// TestARosterWithNoHomeProjectTakesTheBuildsDefault: there is nothing to read
// the site's own theme off.
func TestARosterWithNoHomeProjectTakesTheBuildsDefault(t *testing.T) {
	t.Parallel()
	_, homeTheme := Themes([]map[string]any{
		loadedManifest("alpha", "Alpha", "1.0.0", "clean"),
	}, "", "")
	if homeTheme != DefaultTheme {
		t.Errorf("homeTheme = %q, want %q", homeTheme, DefaultTheme)
	}
}

// TestAManifestWithNoSlugIsNotKeyed: a slug is what a subtree is addressed by,
// and a manifest without one names no subtree.
func TestAManifestWithNoSlugIsNotKeyed(t *testing.T) {
	t.Parallel()
	bySlug, _ := Themes([]map[string]any{loadedManifest("", "Nameless", "1.0.0", "clean")}, "", "")
	if len(bySlug) != 0 {
		t.Errorf("bySlug = %#v, want nothing keyed", bySlug)
	}
}

func TestAPageInASubtreeTakesThatProjectsTheme(t *testing.T) {
	t.Parallel()
	bySlug := map[string]string{"alpha": "clean", "home": "minimal"}
	if got := PageTheme("alpha/guide/index.html", bySlug, "minimal"); got != "clean" {
		t.Errorf("PageTheme = %q, want %q", got, "clean")
	}
}

// TestASiteLevelPageTakesTheHomeTheme: those addresses belong to the site
// rather than to one project.
func TestASiteLevelPageTakesTheHomeTheme(t *testing.T) {
	t.Parallel()
	bySlug := map[string]string{"alpha": "clean"}
	for _, pageRel := range []string{
		"404.html", "blog/index.html", "blog/hello/index.html",
		"projects/index.html", "cv/index.html",
	} {
		if got := PageTheme(pageRel, bySlug, "minimal"); got != "minimal" {
			t.Errorf("PageTheme(%q) = %q, want %q", pageRel, got, "minimal")
		}
	}
}

// -- the reference shape ------------------------------------------------------

func TestTheHopBackToTheSiteRoot(t *testing.T) {
	t.Parallel()
	cases := []struct{ pageRel, want string }{
		{"404.html", ""},
		{"blog/index.html", "../"},
		{"alpha/guide/index.html", "../../"},
		{"a/b/c/d/index.html", "../../../../"},
	}
	for _, tc := range cases {
		if got := SiteRootPrefix(tc.pageRel); got != tc.want {
			t.Errorf("SiteRootPrefix(%q) = %q, want %q", tc.pageRel, got, tc.want)
		}
	}
}

// TestTheReferenceIsRelativeNeverOriginAbsolute: the tree has to resolve under
// any mount point.
func TestTheReferenceIsRelativeNeverOriginAbsolute(t *testing.T) {
	t.Parallel()
	href := Href("alpha/guide/index.html", Dir+"/minimal-a.css")
	if strings.HasPrefix(href, "/") {
		t.Errorf("the reference is origin-absolute: %q", href)
	}
	if want := "../../" + Dir + "/minimal-a.css"; href != want {
		t.Errorf("Href = %q, want %q", href, want)
	}
}

func TestWhichReferencesThePassOwns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ref        string
		recognised bool
	}{
		{"style.css", true},
		{"../style.css", true},
		{"../" + Dir + "/minimal-abc.css", true},
		{Dir + "/minimal-abc.css", true},
		{"custom.css", false},
		{"pagefind/pagefind-ui.css", false},
		{"/style.css", false},
		{"//cdn.example.com/style.css", false},
		{"https://cdn.example.com/style.css", false},
		{"http://cdn.example.com/style.css", false},
		{"#top", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsReference(tc.ref); got != tc.recognised {
			t.Errorf("IsReference(%q) = %v, want %v", tc.ref, got, tc.recognised)
		}
	}
}

func TestRepointingLeavesAPageWithNoChromeReferenceAlone(t *testing.T) {
	t.Parallel()
	pageHTML := "<html><head><title>x</title></head><body></body></html>"
	if got := RepointPage(pageHTML, "a/index.html", Dir+"/m-a.css"); got != pageHTML {
		t.Errorf("RepointPage rewrote a page with no chrome reference:\n%s", got)
	}
}

// TestRepointPageMovesAllThreeReferencesTogether: the preload, the async
// stylesheet and the <noscript> fallback name one file.
func TestRepointPageMovesAllThreeReferencesTogether(t *testing.T) {
	t.Parallel()
	pageHTML := builtPage("../style.css", canonicalBase+"/alpha/guide/")
	rel := Dir + "/minimal-abcdef012345.css"
	got := RepointPage(pageHTML, "alpha/guide/index.html", rel)
	href := Href("alpha/guide/index.html", rel)
	if count := strings.Count(got, `href="`+href+`"`); count != 3 {
		t.Errorf("the page names the asset %d times, want 3:\n%s", count, got)
	}
	// The canonical is an href too, and is not the chrome.
	if !strings.Contains(got, `href="`+canonicalBase+`/alpha/guide/"`) {
		t.Errorf("the canonical was rewritten:\n%s", got)
	}
}

// -- WriteAssets --------------------------------------------------------------

// TestTheAssetSetCoversEveryDeclaredTheme writes one asset per distinct theme.
func TestTheAssetSetCoversEveryDeclaredTheme(t *testing.T) {
	t.Parallel()
	site := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatalf("creating the site: %v", err)
	}
	assets, err := WriteAssets(site, []string{"minimal", "clean", "minimal"}, effects.Unbound())
	if err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	if len(assets) != 2 || assets["minimal"] == "" || assets["clean"] == "" {
		t.Fatalf("assets = %#v, want one entry per distinct theme", assets)
	}
	for theme, rel := range assets {
		path := filepath.Join(site, filepath.Join(strings.Split(rel, "/")...))
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("theme %q: %v", theme, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("theme %q: %s is a directory", theme, rel)
		}
		want, err := CSS(theme)
		if err != nil {
			t.Fatalf("CSS(%q): %v", theme, err)
		}
		if got := read(t, path); got != want {
			t.Errorf("theme %q: the written asset is not the composed stylesheet", theme)
		}
	}
}

// TestWriteAssetsWritesAFrameworkPayloadBesideItsSheet: the sheet and the
// modules address each other by relative URL, so the layout is the
// framework's.
func TestWriteAssetsWritesAFrameworkPayloadBesideItsSheet(t *testing.T) {
	t.Parallel()
	site := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatalf("creating the site: %v", err)
	}
	assets, err := WriteAssets(site, []string{"tinymoon"}, effects.Unbound())
	if err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	rel := assets["tinymoon"]
	payloadRoot := filepath.Dir(filepath.Dir(filepath.Join(site, filepath.Join(strings.Split(rel, "/")...))))
	for _, name := range []string{
		filepath.Join("js", "palette.js"),
		filepath.Join("fonts", "space-grotesk-latin.woff2"),
	} {
		if _, err := os.Stat(filepath.Join(payloadRoot, name)); err != nil {
			t.Errorf("the framework payload is missing %s: %v", name, err)
		}
	}
	if names := entryNames(t, filepath.Join(site, Dir)); len(names) != 1 {
		t.Errorf("entries under %s = %#v, want one payload directory", Dir, names)
	}
}

// TestWriteAssetsPrunesAStaleEntry: an asset whose content changed took a new
// name, and the old name is a file no page references.
func TestWriteAssetsPrunesAStaleEntry(t *testing.T) {
	t.Parallel()
	site := filepath.Join(t.TempDir(), "site")
	write(t, filepath.Join(site, Dir, "minimal-deadbeef0000.css"), "/* stale */")
	write(t, filepath.Join(site, Dir, "tinymoon-deadbeef0000", "css", "style.css"), "/* stale */")
	assets, err := WriteAssets(site, []string{DefaultTheme}, effects.Unbound())
	if err != nil {
		t.Fatalf("WriteAssets: %v", err)
	}
	names := entryNames(t, filepath.Join(site, Dir))
	want := []string{entryName(assets[DefaultTheme])}
	if len(names) != 1 || names[0] != want[0] {
		t.Errorf("entries = %#v, want %#v", names, want)
	}
}

// -- EmittedPages -------------------------------------------------------------

func TestEmittedPages(t *testing.T) {
	t.Parallel()
	site := tree(t)
	write(t, filepath.Join(site, "404.html"), "<html></html>")
	write(t, filepath.Join(site, "alpha", "notes.txt"), "not a page")
	got, err := EmittedPages(site)
	if err != nil {
		t.Fatalf("EmittedPages: %v", err)
	}
	want := []string{
		"404.html", "alpha/guide/index.html", "alpha/index.html",
		"blog/hello/index.html",
	}
	if len(got) != len(want) {
		t.Fatalf("EmittedPages = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEmittedPagesOfATreeThatDoesNotExist: the deploy asks before it knows
// whether any project has grafted yet.
func TestEmittedPagesOfATreeThatDoesNotExist(t *testing.T) {
	t.Parallel()
	got, err := EmittedPages(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("EmittedPages: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("EmittedPages = %#v, want nothing", got)
	}
}

// -- the whole pass over a grafted tree ---------------------------------------

// TestEveryGraftedPageIsRepointedAtTheSiteAsset: a project's pages stop naming
// their own subtree copy, and the file they now name exists in the tree.
func TestEveryGraftedPageIsRepointedAtTheSiteAsset(t *testing.T) {
	t.Parallel()
	site := tree(t)
	manifests := []map[string]any{loadedManifest("alpha", "Alpha", "1.0.0", "")}
	assets, changed := repointTree(t, site, manifests, "")
	rel := assets[DefaultTheme]

	wantChanged := []string{"alpha/guide/index.html", "alpha/index.html", "blog/hello/index.html"}
	if len(changed) != len(wantChanged) {
		t.Fatalf("changed = %#v, want %#v", changed, wantChanged)
	}
	for i := range wantChanged {
		if changed[i] != wantChanged[i] {
			t.Errorf("changed %d = %q, want %q", i, changed[i], wantChanged[i])
		}
	}

	for _, pageRel := range wantChanged {
		pageHTML := read(t, filepath.Join(site, filepath.Join(strings.Split(pageRel, "/")...)))
		if !strings.Contains(pageHTML, Href(pageRel, rel)) {
			t.Errorf("%s does not name the site asset:\n%s", pageRel, pageHTML)
		}
		refs := stylesheets(pageHTML)
		if len(refs) == 0 {
			t.Errorf("%s names no stylesheet", pageRel)
		}
		for _, ref := range refs {
			if ref == "style.css" || strings.HasSuffix(ref, "/style.css") {
				t.Errorf("%s still names its subtree copy %q", pageRel, ref)
			}
			target := resolve(pageRel, ref)
			if _, err := os.Stat(filepath.Join(site, filepath.Join(strings.Split(target, "/")...))); err != nil {
				t.Errorf("%s names %q, which resolves to site/%s and no such file was written",
					pageRel, ref, target)
			}
		}
	}
}

// TestAProjectsOwnStyleCSSIsLeftInPlace: the subtree copy stays, so nothing is
// broken mid-flight while the site migrates.
func TestAProjectsOwnStyleCSSIsLeftInPlace(t *testing.T) {
	t.Parallel()
	site := tree(t)
	repointTree(t, site, []map[string]any{loadedManifest("alpha", "Alpha", "1.0.0", "")}, "")
	if _, err := os.Stat(filepath.Join(site, "alpha", "style.css")); err != nil {
		t.Errorf("the project's own stylesheet was deleted: %v", err)
	}
}

// TestCustomCSSIsNeverRepointed: "custom.css" is the project's content, not
// the chrome the assembly owns.
func TestCustomCSSIsNeverRepointed(t *testing.T) {
	t.Parallel()
	site := tree(t)
	write(t, filepath.Join(site, "alpha", "extra.html"), strings.Replace(
		builtPage("style.css", canonicalBase+"/alpha/extra/"),
		"</head>", `<link rel="stylesheet" href="custom.css">`+"\n</head>", 1))
	repointTree(t, site, []map[string]any{loadedManifest("alpha", "Alpha", "1.0.0", "")}, "")
	pageHTML := read(t, filepath.Join(site, "alpha", "extra.html"))
	if !strings.Contains(pageHTML, `href="custom.css"`) {
		t.Errorf("the project's custom stylesheet was re-pointed:\n%s", pageHTML)
	}
}

// TestASecondPassRepointsPagesAtAChangedAsset: a toolchain upgrade reaches the
// whole site without republishing it, and the stale asset is pruned rather
// than accumulated.
func TestASecondPassRepointsPagesAtAChangedAsset(t *testing.T) {
	t.Parallel()
	site := tree(t)
	manifests := []map[string]any{loadedManifest("alpha", "Alpha", "1.0.0", "")}
	repointTree(t, site, manifests, "")
	first := entryNames(t, filepath.Join(site, Dir))

	// The second deploy's toolchain composes a different stylesheet. The
	// composition itself is not replaceable here, so the changed asset is
	// written directly and the pages are re-pointed at it -- which is the
	// sequence WriteAssets and RepointPages perform.
	handle := effects.Unbound()
	css, err := CSS(DefaultTheme)
	if err != nil {
		t.Fatalf("CSS: %v", err)
	}
	upgraded := css + ".added{}"
	rel, err := AssetRel(DefaultTheme, upgraded)
	if err != nil {
		t.Fatalf("AssetRel: %v", err)
	}
	path := filepath.Join(site, filepath.Join(strings.Split(rel, "/")...))
	if err := handle.MkdirAll(filepath.Dir(path)); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := handle.AtomicWrite(path, []byte(upgraded), effects.ModeDefault); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	if err := handle.Remove(filepath.Join(site, Dir, first[0])); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	pages, err := EmittedPages(site)
	if err != nil {
		t.Fatalf("EmittedPages: %v", err)
	}
	bySlug, homeTheme := Themes(manifests, "", "")
	if _, err := RepointPages(site, pages, bySlug,
		map[string]string{DefaultTheme: rel}, homeTheme, handle); err != nil {
		t.Fatalf("RepointPages: %v", err)
	}

	second := entryNames(t, filepath.Join(site, Dir))
	if len(second) != 1 {
		t.Fatalf("entries = %#v, want one asset", second)
	}
	if second[0] == first[0] {
		t.Errorf("the changed stylesheet kept the name %q", first[0])
	}
	pageHTML := read(t, filepath.Join(site, "alpha", "index.html"))
	if !strings.Contains(pageHTML, second[0]) {
		t.Errorf("the page does not name the new asset:\n%s", pageHTML)
	}
	if strings.Contains(pageHTML, first[0]) {
		t.Errorf("the page still names the pruned asset:\n%s", pageHTML)
	}
}

// TestRepointPagesRefusesAPageWhoseThemeWasNotEmitted: a page left pointing at
// a file the graft no longer serves is the outcome this refusal prevents.
func TestRepointPagesRefusesAPageWhoseThemeWasNotEmitted(t *testing.T) {
	t.Parallel()
	site := tree(t)
	_, err := RepointPages(site, []string{"alpha/index.html"},
		map[string]string{"alpha": "clean"},
		map[string]string{DefaultTheme: Dir + "/minimal-abc.css"},
		DefaultTheme, effects.Unbound())
	if err == nil {
		t.Fatal("RepointPages accepted a page whose theme has no asset")
	}
	for _, want := range []string{"site/alpha/index.html", `"clean"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}
