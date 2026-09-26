package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// buildVersioned writes a git-tagged multi-version project, builds it, and
// returns the site.
func buildVersioned(t *testing.T, versions []string, apply func(*Options)) site {
	t.Helper()
	dir := testproject.MakeVersioned(t, versions, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
	})
	opts := Options{DirPath: dir, Stdout: &discard{}}
	if apply != nil {
		apply(&opts)
	}
	written, err := Build(opts, effects.Unbound())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return site{
		dir:     dir,
		output:  filepath.Join(dir, "stricttools", ".docs-cache", "build"),
		written: written,
	}
}

func TestBuildMultipleVersions(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildVersioned(t, []string{"0.1.0", "0.2.0"}, nil)

	t.Run("the current version is stable and the older one is archived", func(t *testing.T) {
		if !built.exists("index.html") {
			t.Error("the current version is not at the stable address")
		}
		if !built.exists("v/0.1.0/index.html") {
			t.Error("the superseded version is not under the archive prefix")
		}
		if built.exists("v/0.2.0") {
			t.Error("the current version got an archive copy of its own")
		}
	})

	t.Run("the current version says nothing about being superseded", func(t *testing.T) {
		assertLacks(t, "index.html", built.page(t, "index.html"), "version-notice")
	})

	t.Run("an archived page carries a dismissable notice keyed to its version", func(t *testing.T) {
		old := built.page(t, "v/0.1.0/index.html")
		assertCarries(t, "v/0.1.0/index.html", old,
			`class="tm-notice tm-notice-warn"`,
			`data-notice-key="0.1.0"`,
			"tm-notice-dismiss",
			// The link out is document-relative, back to the stable address.
			`href="../../"`)
	})

	t.Run("an archived page carries its tag's content, not the working tree's", func(t *testing.T) {
		assertCarries(t, "v/0.1.0/index.html", built.page(t, "v/0.1.0/index.html"), "0.1.0")
	})

	t.Run("an archived page canonicalizes to the stable address", func(t *testing.T) {
		old := built.page(t, "v/0.1.0/index.html")
		canonical := regexp.MustCompile(`<link rel="canonical" href="([^"]*)">`).FindStringSubmatch(old)
		if canonical == nil {
			t.Fatalf("the archived page declares no canonical link")
		}
		if canonical[1] != "https://example.com/" {
			t.Errorf("the archived page canonicalizes to %q, want the stable address", canonical[1])
		}
		if !strings.Contains(built.page(t, "index.html"), canonical[1]) {
			t.Error("the canonical the archive names is not the current page's own address")
		}
	})

	t.Run("an archive is canonicalized away, never noindexed", func(t *testing.T) {
		assertLacks(t, "v/0.1.0/index.html", built.page(t, "v/0.1.0/index.html"), "noindex")
	})

	t.Run("the sitemap lists stable addresses only", func(t *testing.T) {
		sitemap := built.page(t, "sitemap.xml")
		assertCarries(t, "sitemap.xml", sitemap, "<loc>")
		assertLacks(t, "sitemap.xml", sitemap, "/v/0.1.0", "/0.2.0/")
	})

	t.Run("the root index is the current version itself", func(t *testing.T) {
		root := built.page(t, "index.html")
		assertLacks(t, "index.html", root, `meta http-equiv="refresh"`)
		assertCarries(t, "index.html", root, "0.2.0")
	})

	t.Run("each extracted version has a cache entry, and the cache is ignored", func(t *testing.T) {
		cacheDir := filepath.Join(built.dir, "stricttools", ".docs-cache", "versions")
		if !isDir(cacheDir) {
			t.Fatal("the cache directory was not created")
		}
		if !isDir(filepath.Join(cacheDir, "0.1.0")) {
			t.Error("the superseded version has no cache entry")
		}
		if isDir(filepath.Join(cacheDir, "0.2.0")) {
			t.Error("the current version was extracted instead of read from the working tree")
		}
		// The cache is kept out of the repository by the one derived
		// ignore file inside the tool-state directory, not by an ignore
		// file of its own.
		gitignore := readFile(t, filepath.Join(built.dir, "stricttools", ".gitignore"))
		if !strings.Contains(gitignore, "docs-cache/") {
			t.Errorf("the derived ignore file is %q, want it ignoring the cache directory", gitignore)
		}
	})

	t.Run("the version picker offers every version with its own address", func(t *testing.T) {
		index := built.page(t, "index.html")
		assertCarries(t, "index.html", index, "version-picker", "v0.1.0", "v0.2.0")
		hrefs := map[string]string{}
		for _, match := range regexp.MustCompile(
			`data-value="([^"]*)" data-href="([^"]*)"`).FindAllStringSubmatch(index, -1) {
			hrefs[match[1]] = match[2]
		}
		want := map[string]string{"0.1.0": "v/0.1.0/", "0.2.0": "./"}
		for version, href := range want {
			if hrefs[version] != href {
				t.Errorf("the picker sends %s to %q, want %q", version, hrefs[version], href)
			}
		}
	})

	t.Run("each page's picker selects the version it belongs to", func(t *testing.T) {
		assertCarries(t, "v/0.1.0/index.html", built.page(t, "v/0.1.0/index.html"),
			`aria-selected="true" data-value="0.1.0"`)
		assertCarries(t, "index.html", built.page(t, "index.html"),
			`aria-selected="true" data-value="0.2.0"`)
	})
}

func TestBuildSingleVersion(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildVersioned(t, []string{"1.0.0"}, nil)
	index := built.page(t, "index.html")

	t.Run("the only version there is has not been superseded", func(t *testing.T) {
		assertLacks(t, "index.html", index, "version-notice")
	})

	t.Run("a picker with one option is not offered at all", func(t *testing.T) {
		assertLacks(t, "index.html", index, `<select class="version-picker"`)
	})
}

func TestBuildVersionFilter(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildVersioned(t, []string{"0.1.0", "0.2.0"}, func(o *Options) {
		o.VersionFilter = "0.2.0"
	})
	if !built.exists("index.html") {
		t.Error("the named version is not at the stable address")
	}
	if built.exists("v/0.1.0") {
		t.Error("a version the filter excluded was built anyway")
	}
}

func TestExtractVersionContent(t *testing.T) {
	hygiene.Isolate(t)

	dir := testproject.MakeVersioned(t, []string{"0.1.0"}, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}

	t.Run("the tag's docs are extracted into the cache", func(t *testing.T) {
		cacheDir, err := ExtractVersionContent("0.1.0", cfg, dir, effects.Unbound())
		if err != nil {
			t.Fatalf("ExtractVersionContent: %v", err)
		}
		if !isDir(cacheDir) {
			t.Fatalf("%s is not a directory", cacheDir)
		}
		content := readFile(t, filepath.Join(cacheDir, "stricttools", "docs", "index.md"))
		if !strings.Contains(content, "0.1.0") {
			t.Errorf("the extracted page is %q, want the tag's own content", content)
		}
	})

	t.Run("a tag standing where it stood is not extracted again", func(t *testing.T) {
		cacheDir, err := ExtractVersionContent("0.1.0", cfg, dir, effects.Unbound())
		if err != nil {
			t.Fatalf("ExtractVersionContent: %v", err)
		}
		hashFile := filepath.Join(cacheDir, ".hash")
		first, err := os.Stat(hashFile)
		if err != nil {
			t.Fatalf("stating the sentinel: %v", err)
		}
		again, err := ExtractVersionContent("0.1.0", cfg, dir, effects.Unbound())
		if err != nil {
			t.Fatalf("ExtractVersionContent: %v", err)
		}
		if again != cacheDir {
			t.Errorf("the second call answered %q, want the same cache directory", again)
		}
		second, err := os.Stat(hashFile)
		if err != nil {
			t.Fatalf("stating the sentinel: %v", err)
		}
		if !second.ModTime().Equal(first.ModTime()) {
			t.Error("the sentinel was rewritten for a tag that had not moved")
		}
	})

	t.Run("a version with no tag is an error naming both spellings", func(t *testing.T) {
		_, err := ExtractVersionContent("9.9.9", cfg, dir, effects.Unbound())
		if err == nil {
			t.Fatal("a version with no tag extracted anyway")
		}
		assertCarries(t, "the refusal", err.Error(), "not found", "'v9.9.9'", "'9.9.9'")
	})
}
