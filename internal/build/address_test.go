package build

import (
	"html"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/stricttest/go/hygiene"
)

// addressPages is a docs tree with depth plus one page that opts out of
// versioning: it is built once per locale at <locale>/about/, one level
// shallower than every versioned page, and every versioned page links to it
// from the sidebar.
var addressPages = map[string]string{
	"index.md":                "# Fixture\n\nRoot page.\n",
	"guide.md":                "+++\ntitle = \"Guide\"\n+++\n\n# Guide\n\nOne level down.\n",
	"reference/api.md":        "+++\ntitle = \"API\"\n+++\n\n# API\n\nTwo levels down.\n",
	"reference/deep/notes.md": "+++\ntitle = \"Notes\"\n+++\n\n# Notes\n\nThree levels down.\n",
	"about.md": "+++\ntitle = \"About\"\nversioned = false\n+++\n\n" +
		"# About\n\nThe same page under every version.\n",
}

// The two mounts the address fixtures are built at: a site that owns its
// origin root, and one an assembly serves under a slug.
var (
	originRootConfig = map[string]any{"base_url": "https://example.com"}
	slugPrefixConfig = map[string]any{
		"base_url": "https://docs.example.com/fixture",
		"topology": map[string]any{
			"docs_base": "https://docs.example.com", "slug": "fixture",
		},
	}
)

// makeAddressFixture writes a two-version, multi-level project and returns
// its root. With more than one locale the pages are written per locale under
// docs/<code>/, which is the layout a localized project uses.
func makeAddressFixture(t *testing.T, configExtra map[string]any, localeCodes []string) string {
	t.Helper()
	versions := []string{"0.1.0", "0.2.0"}
	localeEntries := make([]any, 0, len(localeCodes))
	for index, code := range localeCodes {
		localeEntries = append(localeEntries, map[string]any{
			"code": code, "label": strings.ToUpper(code), "default": index == 0,
		})
	}
	config := map[string]any{
		"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/",
		"version":  versions[len(versions)-1],
		"versions": []any{map[string]any{"version": "0.1.0"}, map[string]any{"version": "0.2.0"}},
		"locales":  localeEntries,
	}
	for key, value := range configExtra {
		config[key] = value
	}
	dir := testproject.Make(t, config)

	localized := len(localeCodes) > 1
	var docRoots []string
	for _, code := range localeCodes {
		if localized {
			docRoots = append(docRoots, filepath.Join(dir, ".stricttools", "docs", code))
			continue
		}
		docRoots = append(docRoots, filepath.Join(dir, ".stricttools", "docs"))
	}
	if localized {
		// The single-locale fixture's own index.md would otherwise sit
		// beside the locale directories and be built as a page of its own.
		if err := os.Remove(filepath.Join(dir, ".stricttools", "docs", "index.md")); err != nil {
			t.Fatalf("clearing the unlocalized index page: %v", err)
		}
	}
	for _, docRoot := range docRoots {
		for relPath, content := range addressPages {
			testproject.WriteText(t, filepath.Join(docRoot, filepath.FromSlash(relPath)), content)
		}
	}

	testproject.Git(t, dir, "init")
	testproject.Git(t, dir, "add", ".")
	testproject.Git(t, dir, "commit", "-m", "initial")
	for _, version := range versions {
		for _, docRoot := range docRoots {
			testproject.WriteText(t, filepath.Join(docRoot, "index.md"),
				"# Fixture\n\nRoot page for "+version+".\n")
		}
		testproject.Git(t, dir, "add", ".stricttools")
		testproject.Git(t, dir, "commit", "-m", "docs "+version)
		testproject.Git(t, dir, "tag", "v"+version)
	}
	return dir
}

// buildAddressFixture builds an address fixture and returns the site.
func buildAddressFixture(t *testing.T, configExtra map[string]any, localeCodes []string) site {
	t.Helper()
	dir := makeAddressFixture(t, configExtra, localeCodes)
	written, err := Build(Options{DirPath: dir, Stdout: &discard{}}, effects.Unbound())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return site{dir: dir, output: filepath.Join(dir, ".stricttools", "docs-cache", "build"), written: written}
}

// emittedFiles lists every file the build wrote, as slash paths relative to
// the output root.
//
// With includeIndex false the pagefind/ tree is dropped, because its file
// names are content digests: two builds of the same pages at different base
// URLs index the same content under different names, which says nothing about
// the page tree.
func emittedFiles(t *testing.T, outputDir string, includeIndex bool) map[string]bool {
	t.Helper()
	emitted := map[string]bool{}
	err := filepath.WalkDir(outputDir, func(pathname string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if !includeIndex && entry.Name() == "pagefind" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".gz") || strings.HasSuffix(entry.Name(), ".br") {
			return nil
		}
		rel, relErr := filepath.Rel(outputDir, pathname)
		if relErr != nil {
			return relErr
		}
		emitted[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking the output: %v", err)
	}
	return emitted
}

// referenceAttrs are the attributes whose value is a single URL reference.
var referenceAttrs = []string{"href", "src"}

// skipSchemes are the reference forms that name nothing in this tree.
var skipSchemes = []string{"http://", "https://", "//", "mailto:", "data:", "javascript:"}

// resolveReference resolves ref against the page at pageRel and returns an
// output-relative path, reporting false for a reference that names no file.
func resolveReference(pageRel, ref string) (string, bool) {
	ref, _, _ = strings.Cut(ref, "#")
	ref, _, _ = strings.Cut(ref, "?")
	if ref == "" {
		return "", false
	}
	target := path.Clean(path.Join(path.Dir(pageRel), ref))
	if strings.HasSuffix(ref, "/") || target == "." {
		target = path.Clean(path.Join(target, "index.html"))
	}
	return target, true
}

// assertEveryReferenceResolves checks that every local reference in every
// emitted page names a file the build wrote.
//
// baseURL opts in to the absolute references too -- the share control's
// addresses, which are handed to a reader to open and so must name a page this
// build wrote just as much as an href does.
func assertEveryReferenceResolves(t *testing.T, outputDir, baseURL string) {
	t.Helper()
	emitted := emittedFiles(t, outputDir, true)
	var pages []string
	for rel := range emitted {
		if strings.HasSuffix(rel, ".html") {
			pages = append(pages, rel)
		}
	}
	sort.Strings(pages)
	if len(pages) == 0 {
		t.Fatal("the build emitted no HTML")
	}

	checked := 0
	for _, pageRel := range pages {
		pageHTML := readFile(t, filepath.Join(outputDir, filepath.FromSlash(pageRel)))
		for _, attr := range referenceAttrs {
			pattern := regexp.MustCompile(`\b` + attr + `="([^"]*)"`)
			for _, match := range pattern.FindAllStringSubmatch(pageHTML, -1) {
				raw := html.UnescapeString(match[1])
				if raw == "" || strings.HasPrefix(raw, "#") || hasAnyPrefix(raw, skipSchemes) {
					continue
				}
				if strings.HasPrefix(raw, "/") {
					t.Errorf(`%s: %s="%s" is origin-absolute; the site must `+
						`resolve under any mount point`, pageRel, attr, raw)
					continue
				}
				target, ok := resolveReference(pageRel, raw)
				if !ok {
					continue
				}
				checked++
				if strings.HasPrefix(target, "..") {
					t.Errorf(`%s: %s="%s" escapes the output root`, pageRel, attr, raw)
					continue
				}
				if !emitted[target] {
					t.Errorf(`%s: %s="%s" -> %s (not emitted)`, pageRel, attr, raw, target)
				}
			}
		}
		if baseURL != "" {
			prefix := strings.TrimRight(baseURL, "/") + "/"
			for _, match := range regexp.MustCompile(
				`data-share-url="([^"]*)"`).FindAllStringSubmatch(pageHTML, -1) {
				raw := html.UnescapeString(match[1])
				if !strings.HasPrefix(raw, prefix) {
					continue
				}
				target := strings.TrimPrefix(raw, prefix)
				if target == "" || strings.HasSuffix(target, "/") {
					target = path.Join(target, "index.html")
				}
				checked++
				if !emitted[path.Clean(target)] {
					t.Errorf("%s: the share address %q names %s, which was not emitted",
						pageRel, raw, path.Clean(target))
				}
			}
		}
	}
	if checked == 0 {
		t.Error("the walk checked no reference at all")
	}
}

// hasAnyPrefix reports whether s starts with any of the prefixes.
func hasAnyPrefix(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestEveryEmittedReferenceResolves(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	cases := []struct {
		name   string
		config map[string]any
	}{
		{name: "at an origin root", config: originRootConfig},
		{name: "under an assembly slug", config: slugPrefixConfig},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			built := buildAddressFixture(t, test.config, []string{"en"})
			assertEveryReferenceResolves(t, built.output,
				util.PythonStrOrEmpty(test.config["base_url"]))
		})
	}

	t.Run("across two locales", func(t *testing.T) {
		built := buildAddressFixture(t, originRootConfig, []string{"en", "fa"})
		assertEveryReferenceResolves(t, built.output, "https://example.com")
	})
}

func TestMultiLocaleWithAnUnversionedPageBuildsEveryPage(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// The page partition is per locale: a project whose docs live under
	// docs/<locale>/ still has to produce every versioned page in every
	// locale, plus the unversioned page once per locale.
	built := buildAddressFixture(t, originRootConfig, []string{"en", "fa"})
	emitted := emittedFiles(t, built.output, false)

	pages := []string{
		"index.html", "guide/index.html",
		"reference/api/index.html", "reference/deep/notes/index.html",
	}
	for _, locale := range []string{"en", "fa"} {
		// The current version at the stable mount, the superseded one
		// under the archive prefix beside it.
		for _, mount := range []string{locale, locale + "/v/0.1.0"} {
			for _, outputKey := range pages {
				if !emitted[mount+"/"+outputKey] {
					t.Errorf("%s/%s was not emitted", mount, outputKey)
				}
			}
		}
		for outputKey := range emitted {
			if strings.HasPrefix(outputKey, locale+"/v/0.2.0") {
				t.Errorf("the current version got an archive copy at %s", outputKey)
			}
		}
		// The unversioned page is built once per locale, at the stable
		// mount, and never inside an archive.
		if !emitted[locale+"/about/index.html"] {
			t.Errorf("%s/about/index.html was not emitted", locale)
		}
		if emitted[locale+"/v/0.1.0/about/index.html"] {
			t.Errorf("the unversioned page was emitted inside %s's archive", locale)
		}
	}
}

func TestABuildThatProducesNoContentPagesIsAnError(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// The page partition drives every content build. When it hands back
	// paths no build can match -- the shape a locale-blind partition
	// produced -- the site used to be written with assets and redirect
	// stubs and no pages at all.
	dir := makeAddressFixture(t, originRootConfig, []string{"en", "fa"})
	// A filter naming paths that carry the locale segment: no per-locale
	// build can match them, so every filtered build yields nothing.
	blind := map[string]Partition{}
	for _, locale := range []string{"en", "fa"} {
		blind[locale] = Partition{
			Versioned:              map[string]bool{"en/index.md": true},
			Unversioned:            map[string]bool{"en/about.md": true},
			Site:                   map[string]bool{},
			UnversionedMarkdown:    map[string]string{"en/about.md": "# About"},
			UnversionedFrontmatter: nil,
		}
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	_, err = buildBody(bodyInputs{
		dirPath:           dir,
		config:            cfg,
		locales:           configList(cfg, "locales"),
		versions:          configList(cfg, "versions"),
		defaultLocaleCode: "en",
		latestVersion:     "0.2.0",
		buildVersions:     configList(cfg, "versions"),
		buildLocales:      configList(cfg, "locales"),
		outputDir:         filepath.Join(dir, ".stricttools", "docs-cache", "build"),
		docsDirName:       ".stricttools/docs",
		partitions:        blind,
		sitePages:         map[string]bool{},
		stdout:            &discard{},
	}, effects.Unbound())
	if err == nil {
		t.Fatal("a build whose filters matched nothing wrote a site anyway")
	}
	assertCarries(t, "the refusal", err.Error(), "no content pages")
}

func TestTheOutputTreeIsMountIndependentButForTheNotFoundPage(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// One file depends on where the site is mounted, and only one:
	// 404.html. A hosting provider answers an unmatched address from the
	// root of what it serves, so the not-found page belongs to the served
	// root -- which a mounted project is not. Everything else is addressed
	// document-relative and is identical under every mount.
	origin := buildAddressFixture(t, originRootConfig, []string{"en"})
	slug := buildAddressFixture(t, slugPrefixConfig, []string{"en"})

	originFiles := emittedFiles(t, origin.output, false)
	slugFiles := emittedFiles(t, slug.output, false)

	onlyOrigin := difference(originFiles, slugFiles)
	onlySlug := difference(slugFiles, originFiles)
	if strings.Join(onlyOrigin, ",") != "404.html" {
		t.Errorf("the origin-root build additionally wrote %v, want only 404.html", onlyOrigin)
	}
	if len(onlySlug) != 0 {
		t.Errorf("the mounted build wrote %v, which the origin-root build did not", onlySlug)
	}
}

// difference lists the keys of a that b does not hold, sorted.
func difference(a, b map[string]bool) []string {
	var only []string
	for key := range a {
		if !b[key] {
			only = append(only, key)
		}
	}
	sort.Strings(only)
	return only
}

func TestTheSearchIndexAddressesPagesThatExist(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildAddressFixture(t, originRootConfig, []string{"en"})
	fragments := indexFragments(t, built.output)
	if len(fragments) == 0 {
		t.Fatal("the build indexed no page")
	}
	emitted := emittedFiles(t, built.output, true)
	for _, fragment := range fragments {
		pathname, _, _ := strings.Cut(fragment.URL, "#")
		pathname = strings.TrimPrefix(pathname, "/")
		if !strings.HasSuffix(pathname, ".html") {
			pathname = path.Join(pathname, "index.html")
		}
		target := path.Clean(pathname)
		if !emitted[target] {
			t.Errorf("the indexed page %q names %s, which was not emitted", fragment.URL, target)
		}
	}
}
