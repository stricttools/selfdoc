package site

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/testisolation/go/hygiene"
)

// canonicalBase is the assembly site's base URL in every fixture page.
const canonicalBase = "https://docs.example.com"

// homeSlug is the fixture assembly's home project.
const homeSlug = "home"

// fixtureRoster is the membership every fixture assembly declares: one home
// project served at the site root, and two ordinary projects.
var fixtureRoster = map[string]RosterEntry{
	"home":  {Slug: "home", Repo: "owner/home"},
	"alpha": {Slug: "alpha", Repo: "owner/alpha"},
	"beta":  {Slug: "beta", Repo: "owner/beta"},
}

// handle is the effects handle every engine call in these tests runs under:
// unbound, so every write and every subprocess executes directly.
func handle() *effects.Handle { return effects.Unbound() }

// write creates path's parents and writes content to it.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeJSON writes value as a JSON document at path.
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	write(t, path, string(encoded))
}

// readJSON decodes the JSON object at path.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(content, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

// page renders a page shaped the way a real build's pages are shaped: with a
// title, a canonical address and a stylesheet, because the deploy verifies the
// tree it assembled and a page missing any of those is one it refuses.
func page(title, addr, marker string) string {
	if marker == "" {
		marker = title
	}
	return "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n" +
		"  <title>" + title + "</title>\n" +
		"  <link rel=\"canonical\" href=\"" + canonicalBase + "/" + addr + "\">\n" +
		"  <link rel=\"stylesheet\" href=\"style.css\">\n" +
		"</head>\n<body>\n  <p>" + marker + "</p>\n</body>\n</html>\n"
}

// manifestDoc is a project manifest as a deploy writes it.
func manifestDoc(slug, name, version string, posts []any) map[string]any {
	if posts == nil {
		posts = []any{}
	}
	return map[string]any{
		"schema_version": 1,
		"name":           name,
		"slug":           slug,
		"version":        version,
		"description":    name + " docs",
		"language":       "python",
		"base_url":       canonicalBase + "/" + slug,
		"author":         map[string]any{"name": "Test Author", "url": "https://author.example"},
		"pages":          []any{map[string]any{"path": "index.md", "title": "Home"}},
		"posts":          posts,
		"last_gen":       "2024-01-01T00:00:00+00:00",
	}
}

// filesRecord is a published-file record naming one publisher's paths.
func filesRecord(slug, owner string, paths ...string) map[string]any {
	return map[string]any{
		"schema_version": FilesRecordVersion,
		"slug":           slug,
		"owners":         map[string]any{owner: paths},
	}
}

// assemblyTree writes a realistic assembly checkout and returns its root: two
// deployed project subtrees, a post published between releases, the home
// project's front page at the site root, the manifests including a stale posts
// overlay, the derived membership record and the roster that declares all
// three projects.
func assemblyTree(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	root := filepath.Join(t.TempDir(), "assembly")

	write(t, filepath.Join(root, "site", "alpha", "index.html"),
		page("Alpha", "alpha/", "old alpha"))
	write(t, filepath.Join(root, "site", "alpha", "guide", "index.html"),
		page("Alpha Guide", "alpha/guide/", "old guide"))
	write(t, filepath.Join(root, "site", "alpha", "retired", "index.html"),
		page("Retired", "alpha/retired/", "gone upstream"))
	// A post published between releases, at the site-level address every post
	// has: nobody's build produced it, so no publisher is entitled to prune it
	// and it outlives a full build.
	write(t, filepath.Join(root, "site", "blog", "old-post", "index.html"),
		page("Old", "blog/old-post/", "old post"))
	write(t, filepath.Join(root, "site", "beta", "index.html"),
		page("Beta", "beta/", "beta"))
	write(t, filepath.Join(root, "site", "index.html"),
		page("Front page", "", "home"))

	writeJSON(t, filepath.Join(root, "manifests", "alpha.json"),
		manifestDoc("alpha", "Alpha", "0.9.0", nil))
	writeJSON(t, filepath.Join(root, "manifests", "alpha-posts.json"),
		manifestDoc("alpha", "Alpha", "0.9.0", []any{
			map[string]any{"slug": "old-post", "title": "Old", "date": "2024-01-01"},
		}))
	writeJSON(t, filepath.Join(root, "manifests", "beta.json"),
		manifestDoc("beta", "Beta", "2.0.0", nil))
	writeJSON(t, filepath.Join(root, "manifests", "home.json"),
		manifestDoc("home", "Home", "0.1.0", nil))
	writeJSON(t, filepath.Join(root, "manifests", "home-files.json"),
		filesRecord("home", "release", "index.html"))
	// A declared home carries its curated listing: its deploy copies it in
	// beside the manifests, and shared generation refuses without it.
	writeJSON(t, filepath.Join(root, "manifests", "home-listing.json"), map[string]any{
		"format_version": 1,
		"slug":           "home",
		"categories": []any{map[string]any{
			"name": "Projects",
			"projects": []any{map[string]any{
				"slug": "alpha", "blurb": "Does the alpha thing.", "url": "", "name": "",
			}},
		}},
	})

	writeJSON(t, filepath.Join(root, ProjectsPath), map[string]any{
		"home":  map[string]any{"repo": "owner/home", "ref": "v0.1.0", "version": "0.1.0"},
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v0.9.0", "version": "0.9.0"},
		"beta":  map[string]any{"repo": "owner/beta", "ref": "v2.0.0", "version": "2.0.0"},
	})
	write(t, filepath.Join(root, RosterPath), RenderRoster([]RosterEntry{
		fixtureRoster["home"], fixtureRoster["alpha"], fixtureRoster["beta"],
	}, homeSlug))

	// What the last release published for alpha, which is what a prune is
	// entitled to remove. Every path is site-relative, and the out-of-band
	// post is deliberately absent.
	writeJSON(t, filepath.Join(root, "manifests", "alpha-files.json"),
		filesRecord("alpha", "release",
			"alpha/index.html", "alpha/guide/index.html", "alpha/retired/index.html"))

	return root
}

// buildTree writes a build output tree as a selfdoc build would have produced
// it, including the per-project deploy artifacts the assembly must not
// inherit, and returns its root.
func buildTree(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	build := filepath.Join(t.TempDir(), "stricttools", ".docs-cache", "build")
	write(t, filepath.Join(build, "index.html"), page("Alpha", "alpha/", "new alpha"))
	write(t, filepath.Join(build, "guide", "index.html"),
		page("Alpha Guide", "alpha/guide/", "new guide"))
	write(t, filepath.Join(build, "blog", "index.html"),
		page("Alpha Posts", "blog/", "standalone listing"))
	write(t, filepath.Join(build, "blog", "hello", "index.html"),
		page("Hello", "blog/hello/", "hello"))
	write(t, filepath.Join(build, "_headers"), "/*\n  X-Frame-Options: DENY\n")
	write(t, filepath.Join(build, "_redirects"), "/* /index.html 200\n")
	write(t, filepath.Join(build, "_worker.js"), "export default {}\n")
	write(t, filepath.Join(build, "index.html.gz"), "gzipped")
	write(t, filepath.Join(build, "guide", "index.html.br"), "brotli")
	return build
}

// dirNames lists the entry names directly under dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// exists reports whether path is there at all.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runGit runs one git command in dir, failing the test when it does.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// mkdirAll creates dir and its parents.
func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o755) }

// writeBytes writes data at path, creating its parents.
func writeBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// sortedKeys lists a byte-content map's keys in order.
func sortedKeys(files map[string][]byte) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
