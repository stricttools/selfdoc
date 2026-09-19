//go:build e2e

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/blog/preview"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/manifest"
)

// CanonicalBase is the site every fixture page canonicalizes against. It is
// the DEPLOYED base, not the loopback address the preview is served from --
// the pages carry the canonicals they would ship with, exactly as a deploy
// writes them.
const CanonicalBase = "https://docs.example.com"

// AllowedExternal is the one off-site address the fixture CONTENT names, from
// a link in alpha's index page. Everything a reader can click that is not on
// this origin and not in ExternalAllowlist is a page sending readers off the
// site, which is the defect class the navigation test stands for.
const AllowedExternal = "https://example.org/external-reference"

// GeneratorLink is the generator's attribution link, which every page's chrome
// carries.
const GeneratorLink = "https://github.com/smm-h/selfdoc"

const (
	authorName   = "Test Author"
	authorURL    = "https://author.example"
	authorSameAs = "https://github.com/testauthor"
)

// ExternalAllowlist is every off-origin address a fixture page may name, and
// why. Written out rather than pattern-matched: an allowlist that accepts a
// shape accepts the next link of that shape too, and the point is that a new
// external link has to be declared deliberately.
var ExternalAllowlist = map[string]string{
	AllowedExternal: "a link in alpha's page content",
	GeneratorLink:   "the generator's attribution link in the page chrome",
	authorURL:       "the author the fixture declares",
	authorSameAs:    "the author's declared profile, and the CV's",
}

// allowedExternals is the allowlist normalized the way collected hrefs are.
var allowedExternals = func() map[string]bool {
	set := map[string]bool{}
	for url := range ExternalAllowlist {
		set[strings.TrimRight(url, "/")] = true
	}
	return set
}()

// png2x2 is a 2x2 opaque PNG. The CV test asserts the portrait decodes to a
// non-zero natural size, which a zero-byte placeholder would not.
var png2x2 = mustDecodeBase64(
	"iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGP8z4AAT" +
		"AwMDAwMDAwMDAwMAA8mAgHRvGYYAAAAAElFTkSuQmCC")

func mustDecodeBase64(payload string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		panic(err)
	}
	return decoded
}

// author returns the author block every fixture project declares.
func author() map[string]any {
	return map[string]any{
		"name":    authorName,
		"url":     authorURL,
		"same_as": []any{authorSameAs},
	}
}

// fixtureLocales is the one locale every fixture project declares.
func fixtureLocales() []any {
	return []any{map[string]any{"code": "en", "label": "English", "default": true}}
}

// projectConfig returns a project config.
//
// "source" is deliberately absent from the base: a project that declares
// source code is refused an "unversioned": true declaration (code is what gets
// released, so it carries a version), and two of the three fixture checkouts
// are codeless on purpose.
func projectConfig(slug, name string, overrides map[string]any) map[string]any {
	cfg := map[string]any{
		"name":          name,
		"description":   name + ", a fixture project.",
		"docs":          ".stricttools/docs/",
		"output":        ".stricttools/docs-cache/build/",
		"base_url":      CanonicalBase + "/" + slug,
		"search_engine": "pagefind",
		"author":        author(),
		"locales":       fixtureLocales(),
		"topology":      map[string]any{"slug": slug, "docs_base": CanonicalBase},
	}
	for key, value := range overrides {
		cfg[key] = value
	}
	return cfg
}

// -- writing a checkout -------------------------------------------------------

func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func writeBytes(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encoding %s: %v", path, err)
	}
	writeText(t, path, string(payload))
}

// runGit runs git in dir.
//
// No identity is injected: stricttest's isolation floor owns the git identity
// and the throwaway global config for the whole test.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}

// writeSource writes the one source file a versioned project's config points at.
func writeSource(t *testing.T, root, name string) {
	t.Helper()
	writeText(t, filepath.Join(root, "src", "__init__.py"),
		fmt.Sprintf("\"\"\"The %s package.\"\"\"\n\n\ndef greet(who):\n"+
			"    \"\"\"Return a greeting for *who*.\"\"\"\n"+
			"    return f\"hello {who}\"\n", name))
}

// writeManifest writes the checkout's .stricttools/docs-state/manifest.json, the way gen does.
//
// A real checkout carries a committed manifest: "selfdoc gen" writes it during
// a release, and the assembly reads it for the version badge, the project
// listing, the unified feed and the sitemap. A fixture without one assembles
// into a site whose shared pages know about no projects at all, so this runs
// the production generator over the production resolution of the checkout's
// own docs -- not a hand-written manifest, which would be exactly the sort of
// stand-in this suite refuses.
func writeManifest(t *testing.T, root string) {
	t.Helper()
	handle := effects.Unbound()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("loading %s: %v", root, err)
	}
	allDocs, err := docs.ResolveAll(cfg, "", root, nil, handle)
	if err != nil {
		t.Fatalf("resolving the docs of %s: %v", root, err)
	}
	manifestDocs := map[string]manifest.Doc{}
	for path, doc := range allDocs {
		manifestDocs[path] = doc.ManifestDoc()
	}

	postsRel := ".stricttools/posts/"
	if postsConfig, ok := cfg["posts"].(map[string]any); ok {
		if dir, ok := postsConfig["dir"].(string); ok && dir != "" {
			postsRel = dir
		}
	}
	postsDir := filepath.Join(root, postsRel)
	var published []manifest.Post
	if info, err := os.Stat(postsDir); err == nil && info.IsDir() {
		discovered, err := posts.Discover(postsDir, "", handle)
		if err != nil {
			t.Fatalf("discovering the posts of %s: %v", root, err)
		}
		for _, post := range discovered {
			if post.Draft {
				continue
			}
			published = append(published, manifest.Post{
				Path: post.Path, Title: post.Title, Date: post.Date,
				Slug: post.Slug, Tags: post.Tags,
			})
		}
	}

	if _, err := manifest.Generate(
		cfg, root, manifestDocs, published, "manifest.json", handle,
	); err != nil {
		t.Fatalf("generating the manifest of %s: %v", root, err)
	}
}

// -- the table-heavy page ------------------------------------------------------

// Rows long enough that the table's own scrollport really scrolls, and columns
// wide enough that it scrolls sideways too. The sticky-header assertion needs
// both: a thead that stays put while rows move under it.
const (
	tableRows = 40
	tableCols = 9
)

// longTableMarkdown returns a table that overflows its wrapper under every
// theme.
//
// The cells are code spans holding one unbroken identifier each, which is what
// makes the overflow reliable rather than incidental. Prose cells wrap, so
// whether the table is wider than its box comes down to the theme's font
// metrics -- and tinymoon's table font is small enough that a prose table fits
// at every width the sweep visits, leaving the sticky-column and
// header-alignment assertions with nothing to scroll. An unbroken token has a
// min-content width the browser cannot reduce.
func longTableMarkdown() string {
	headers := []string{"Setting"}
	for i := 1; i < tableCols; i++ {
		headers = append(headers, fmt.Sprintf("Column %d", i))
	}
	rules := make([]string, len(headers))
	for i := range rules {
		rules[i] = "+++"
	}
	lines := []string{
		"| " + strings.Join(headers, " | ") + " |",
		"| " + strings.Join(rules, " | ") + " |",
	}
	for row := 1; row <= tableRows; row++ {
		cells := []string{fmt.Sprintf("`setting_%02d`", row)}
		for col := 1; col < tableCols; col++ {
			cells = append(cells,
				fmt.Sprintf("`value_%02d_%d_unbroken_identifier`", row, col))
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

// -- the three checkouts -------------------------------------------------------

// writeHomeCheckout writes the project served at the site root: front page, CV,
// posts, listing.
//
// Its "topology" carries the slug and deliberately NO "docs_base", which is
// what makes it the home project's config rather than another mounted
// project's: a topology with both builds every URL under
// <docs_base>/<slug>/, and the home project's pages are grafted to the site
// root instead. Declaring both is what a real home project does not do -- and
// the production verification says so, which is why the fixture guard asserts
// the assembled tree passes it.
func writeHomeCheckout(t *testing.T, root string) string {
	t.Helper()
	writeJSON(t, filepath.Join(root, "selfdoc.json"), projectConfig("home", "Home", map[string]any{
		"base_url":    CanonicalBase,
		"topology":    map[string]any{"slug": "home"},
		"unversioned": true,
		"posts":       map[string]any{"repo": "testauthor/posts"},
	}))

	writeText(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\n"+
			"title = \"The Fixture Site\"\n"+
			"description = \"Front page of the fixture assembly.\"\n"+
			"date = 2026-02-01\n"+
			"+++\n"+
			"\n"+
			"# The Fixture Site\n"+
			"\n"+
			"This front page exists so the site root is a page a browser can\n"+
			"load, with prose long enough for the search index to have words\n"+
			"to match against.\n"+
			"\n"+
			"## Where to go\n"+
			"\n"+
			"- [The CV](cv/)\n"+
			"- [Every project](projects/)\n"+
			"- [The blog](blog/)\n"+
			"\n"+
			"## A second section\n"+
			"\n"+
			"A second heading so the front page has a table of contents worth\n"+
			"rendering at every viewport width the sweep visits.\n")

	// The CV page is a thin host: the whole body comes from the TOML.
	writeText(t, filepath.Join(root, ".stricttools", "docs", "cv.md"),
		"+++\n"+
			"title = \"CV\"\n"+
			"type = \"cv\"\n"+
			"description = \"Curriculum vitae of the fixture author.\"\n"+
			"date = 2026-02-01\n"+
			"+++\n"+
			"\n"+
			":-: cv path=\"docs/cv.toml\"\n")
	writeText(t, filepath.Join(root, ".stricttools", "docs", "cv.toml"), cvTOML)
	writeBytes(t, filepath.Join(root, ".stricttools", "docs", "assets", "photo.png"), png2x2)

	// The curated listing the front page and /projects/ both render from.
	writeText(t, filepath.Join(root, ".stricttools", "docs", "projects.toml"), listingTOML)

	// Posts are site-level: they land at /blog/<slug>/ under no project slug.
	postsDir := filepath.Join(root, ".stricttools", "posts")
	writeText(t, filepath.Join(postsDir, "first.md"),
		"+++\n"+
			"title = \"The First Post\"\n"+
			"date = 2026-01-10\n"+
			"slug = \"the-first-post\"\n"+
			"tags = [\"notes\"]\n"+
			"draft = false\n"+
			"directives = false\n"+
			"+++\n"+
			"\n"+
			"## A heading inside a post\n"+
			"\n"+
			"A post reads top to bottom and carries no table of contents, at\n"+
			"any width. This heading is here so that a post with headings is\n"+
			"what the viewport sweep looks at.\n"+
			"\n"+
			"## A second heading\n"+
			"\n"+
			"More prose, so the search index has something to return.\n")
	writeText(t, filepath.Join(postsDir, "second.md"),
		"+++\n"+
			"title = \"The Second Post\"\n"+
			"date = 2026-01-20\n"+
			"slug = \"the-second-post\"\n"+
			"tags = [\"notes\"]\n"+
			"draft = false\n"+
			"directives = false\n"+
			"+++\n"+
			"\n"+
			"## Another post\n"+
			"\n"+
			"Two posts, so the blog index lists more than one and the feed has\n"+
			"an order to get right.\n")
	writeManifest(t, root)
	return root
}

// writeAlphaCheckout writes a versioned project: two tagged versions, so
// v/0.1.0/ is an archive.
//
// It also carries the table-heavy page and the page that declares the glossary
// terms, because both belong to a project subtree rather than to the site root.
func writeAlphaCheckout(t *testing.T, root string) string {
	t.Helper()
	writeJSON(t, filepath.Join(root, "selfdoc.json"), projectConfig("alpha", "Alpha", map[string]any{
		"source":  []any{map[string]any{"path": "src/", "language": "python"}},
		"version": "0.2.0",
		"versions": []any{
			map[string]any{"version": "0.1.0"},
			map[string]any{"version": "0.2.0"},
		},
	}))
	writeSource(t, root, "alpha")

	indexMD := "+++\n" +
		"title = \"Alpha\"\n" +
		"description = \"The versioned fixture project.\"\n" +
		"date = 2026-02-01\n" +
		"+++\n" +
		"\n" +
		"# Alpha\n" +
		"\n" +
		"Alpha is the versioned fixture project. It has an archive, a\n" +
		"version picker, and enough prose to be indexed.\n" +
		"\n" +
		"## Reading on\n" +
		"\n" +
		"- [The settings table](tables/)\n" +
		"- [The terms](terms/)\n" +
		"- [An external reference](" + AllowedExternal + ")\n" +
		"\n" +
		"## A second section\n" +
		"\n" +
		"So the page has a table of contents with more than one entry.\n"
	writeText(t, filepath.Join(root, ".stricttools", "docs", "index.md"), indexMD)

	writeText(t, filepath.Join(root, ".stricttools", "docs", "tables.md"),
		"+++\n"+
			"title = \"Settings\"\n"+
			"description = \"A table long enough to scroll under its own header.\"\n"+
			"date = 2026-02-01\n"+
			"+++\n"+
			"\n"+
			"# Settings\n"+
			"\n"+
			"Every setting Alpha reads, in one table. The table is longer than\n"+
			"the box it scrolls inside of, on purpose: the header row stays put\n"+
			"while the rows move under it. A name like `setting_01` in prose is\n"+
			"a code chip; the same name inside a cell is not, and this sentence\n"+
			"is what the sweep reads the difference against.\n"+
			"\n"+
			longTableMarkdown()+"\n"+
			"\n"+
			"## After the table\n"+
			"\n"+
			"Prose after the table, so the page does not end on it.\n"+
			"\n"+
			"## Reading the table\n"+
			"\n"+
			"A second heading, because a table of contents is only built for a\n"+
			"page with two or more of them -- and this page is one of the ones\n"+
			"the sweep asserts a table of contents on.\n")

	// The terms page declares the glossary; the build generates
	// glossary/index.html from every term declared across the project.
	writeText(t, filepath.Join(root, ".stricttools", "docs", "terms.md"),
		"+++\n"+
			"title = \"Terms\"\n"+
			"description = \"The terms Alpha's documentation uses.\"\n"+
			"date = 2026-02-01\n"+
			"+++\n"+
			"\n"+
			"# Terms\n"+
			"\n"+
			"The words below mean something specific in Alpha's documentation.\n"+
			"\n"+
			":<: list-glossary\n"+
			":=:\n"+
			"::: **Archive**: A superseded version of a page, emitted under its "+
			"own version segment\n"+
			"::: **Manifest**: The record a build writes describing every page "+
			"it emitted\n"+
			"::: **Anchor**: The identifier a heading carries so that a link can "+
			"point straight at it\n"+
			":>:\n"+
			"\n"+
			"## Why they are here\n"+
			"\n"+
			"A term exists only because an author declared it.\n"+
			"\n"+
			"## Where they are used\n"+
			"\n"+
			"A second heading, for the same reason the settings page has one:\n"+
			"a page with a single heading is given no table of contents.\n")

	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "alpha 0.1.0")
	runGit(t, root, "tag", "v0.1.0")
	// A visible edit between the two versions, so the archive is not a
	// byte-for-byte copy of the current version.
	writeText(t, filepath.Join(root, ".stricttools", "docs", "index.md"), strings.Replace(
		indexMD,
		"Alpha is the versioned fixture project.",
		"Alpha is the versioned fixture project, revised for 0.2.0.", 1))
	runGit(t, root, "add", ".stricttools/docs/index.md")
	runGit(t, root, "commit", "-m", "alpha 0.2.0")
	runGit(t, root, "tag", "v0.2.0")
	writeManifest(t, root)
	return root
}

// writeBetaCheckout writes an unversioned project: no version badge, no
// picker, no archive.
func writeBetaCheckout(t *testing.T, root string) string {
	t.Helper()
	writeJSON(t, filepath.Join(root, "selfdoc.json"),
		projectConfig("beta", "Beta", map[string]any{"unversioned": true}))
	writeText(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\n"+
			"title = \"Beta\"\n"+
			"description = \"The unversioned fixture project.\"\n"+
			"date = 2026-02-01\n"+
			"+++\n"+
			"\n"+
			"# Beta\n"+
			"\n"+
			"Beta declares no versions at all, so nothing about it should carry\n"+
			"a version badge or a version picker.\n"+
			"\n"+
			"## Reading on\n"+
			"\n"+
			"- [The guide](guide/)\n"+
			"\n"+
			"## A second section\n"+
			"\n"+
			"So this page has a table of contents too.\n")
	writeText(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
		"+++\n"+
			"title = \"Beta Guide\"\n"+
			"description = \"A second page in the unversioned project.\"\n"+
			"date = 2026-02-01\n"+
			"+++\n"+
			"\n"+
			"# Beta Guide\n"+
			"\n"+
			"A second page, so the project has page-to-page navigation.\n"+
			"\n"+
			"## A section\n"+
			"\n"+
			"And a heading, so it has a table of contents.\n")
	writeManifest(t, root)
	return root
}

const cvTOML = `format_version = 1

[identity]
name = "Test Author"
headline = "Fixture Engineer"
location = "Somewhere"
email = "author@example.com"
photo = "../assets/photo.png"
updated = "2026-02-01"
summary = """
Writes fixtures that are built the way the real thing is built.
"""

[[identity.profile]]
label = "GitHub"
url = "https://github.com/testauthor"

[[skills]]
category = "Rendering"
items = ["HTML", "CSS", "Headless browsers"]

[[skills]]
category = "Tooling"
items = ["Python", "Static site generation"]

[[projects]]
name = "The fixture assembly"
notes = ["Built through the real pipeline, never a mock."]
technologies = ["Python"]

[[interests]]
title = "Rendered reality"
body = "Asserting against what a browser actually paints."

[[education]]
degree = "BSc"
years = "2010-2014"
institute = "A University"
location = "Somewhere"

[[experience]]
role = "Engineer"
period = "2014-present"
company = "A Company"
location = "Somewhere"
body = "Kept the pipeline honest."

[[languages]]
name = "English"
level = "Native"

[contact]
body = "Reach the fixture author at author@example.com."
`

const listingTOML = `[[category]]
name = "Projects"

[[category.project]]
slug = "alpha"
blurb = "The versioned fixture project."

[[category.project]]
slug = "beta"
blurb = "The unversioned fixture project."
`

// -- building and serving ------------------------------------------------------

// discardWriter swallows a build's progress lines.
type discardWriter struct{}

// Write reports every byte written and keeps none.
func (discardWriter) Write(payload []byte) (int, error) { return len(payload), nil }

// buildFixtureSite writes the three checkouts under root and assembles them
// under theme.
//
// It returns the preview summary, whose SiteDir is the tree the server below
// serves.
func buildFixtureSite(t *testing.T, root, theme string) *preview.Summary {
	t.Helper()
	src := filepath.Join(root, "src")
	home := writeHomeCheckout(t, filepath.Join(src, "home"))
	alpha := writeAlphaCheckout(t, filepath.Join(src, "alpha"))
	beta := writeBetaCheckout(t, filepath.Join(src, "beta"))

	summary, err := preview.PreviewAssembly(
		home, []string{alpha, beta}, filepath.Join(root, "out"),
		CanonicalBase, true, theme, effects.Unbound(),
	)
	if err != nil {
		t.Fatalf("[%s] assembling the fixture site: %v", theme, err)
	}

	// The preview reports verification failures and serves the tree anyway,
	// which is right for a person looking at a broken site and wrong for a
	// suite: a browser assertion against a tree the production verification
	// rejects is an assertion about something a deploy would refuse to
	// publish. So the fixture takes the report as a precondition. This is not
	// a second implementation -- it is the deploy's own check, and it is what
	// told us the first draft of the home checkout was configured like a
	// mounted project.
	if !summary.Report.OK() {
		t.Fatalf("the %s fixture assembled into a tree the production "+
			"verification rejects, so nothing below would be asserting "+
			"against a publishable site:\n%s", theme, summary.Report.ErrorText())
	}
	return summary
}

// buildStandaloneProject builds the versioned checkout's own standalone site
// and returns its output root.
//
// A project has two published shapes and this is the other one: its own site,
// built by the build with no version filter and deployed by "selfdoc deploy".
// The distinction is not cosmetic -- it is where the archive is. An assembly
// build takes the latest version deliberately, so an assembled subtree carries
// the current version and nothing else; a standalone build emits every
// declared version, the current one at the stable address and each superseded
// one under v/<version>/.
//
// So the superseded notice, its dismissal and the version picker are
// assertions about THIS tree, and the assembled site is separately asserted to
// offer no version picker at all -- a picker there would offer addresses the
// assembled site does not serve.
//
// Runs after the assembly so it is this build's output that stays in the
// checkout, and indexes it, because a real standalone deploy is indexed too.
func buildStandaloneProject(t *testing.T, root, theme string) string {
	t.Helper()
	handle := effects.Unbound()
	checkout := filepath.Join(root, "src", "alpha")
	if _, err := build.Build(build.Options{
		DirPath: checkout,
		Theme:   theme,
		Stdout:  discardWriter{},
	}, handle); err != nil {
		t.Fatalf("[%s] building alpha's standalone site: %v", theme, err)
	}
	out := filepath.Join(checkout, ".stricttools", "docs-cache", "build")
	if err := assembly.IndexSite(out, handle); err != nil {
		t.Fatalf("[%s] indexing alpha's standalone site: %v", theme, err)
	}
	return out
}

// servedSite is a built fixture tree, served on loopback for the length of a
// session.
type servedSite struct {
	siteDir string
	port    int
	theme   string
}

// origin is the address the tree is served from.
func (s *servedSite) origin() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

// url returns path's address on this tree.
func (s *servedSite) url(path string) string {
	return s.origin() + "/" + strings.TrimLeft(path, "/")
}

// serve serves siteDir with the production preview server on a free port.
//
// It returns the served site and the function that shuts the server down. The
// server is the one "selfdoc assembly preview" runs, on an ephemeral port --
// not a stand-in static server, because a directory address serving its index
// and a 404 answering with a 404 status are exactly the wire behaviors the
// browser assertions depend on.
func serve(siteDir, theme string) (*servedSite, func(), error) {
	server, err := preview.MakePreviewServer(siteDir, 0)
	if err != nil {
		return nil, nil, err
	}
	server.Handler().Log = discardWriter{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve()
	}()
	if err := waitUntilServing(server.Port()); err != nil {
		_ = server.Stop()
		return nil, nil, err
	}
	stop := func() {
		_ = server.Stop()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
	return &servedSite{siteDir: siteDir, port: server.Port(), theme: theme}, stop, nil
}

// waitUntilServing blocks until port accepts a connection, or reports why not.
func waitUntilServing(port int) error {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("preview server on port %d never accepted a connection", port)
}

// sortedStrings returns values sorted, leaving the argument alone.
func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
