package assembly

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
)

const integrateCanonicalBase = "https://docs.example.com"

// integrateRoster is the declaration the integrate fixture carries. Every
// assembly declares one home project: the one served at the site root. It is
// an ordinary declared project in every other way.
var integrateRoster = []site.RosterEntry{
	{Slug: "home", Repo: "owner/home"},
	{Slug: "alpha", Repo: "owner/alpha"},
	{Slug: "beta", Repo: "owner/beta"},
}

// integratePage is a page shaped the way a real build's pages are shaped.
//
// The deploy verifies the tree it assembled before it pushes any of it, and a
// page with no title or no canonical fails that verification -- so a fixture
// standing in for a built page carries both, as the build's own output does.
// address is the site-relative address the page is emitted at, and marker is
// the body text a test looks for to tell one build's output from another's.
//
// It carries a stylesheet too, naming the project-local "style.css" a build
// writes. The shared generator re-points every page at the site-level chrome
// asset, and a page that arrives with no stylesheet at all is one the deploy
// refuses.
func integratePage(title, address, marker, version string) string {
	versionAttr := ""
	if version != "" {
		versionAttr = ` data-default-version="` + version + `"`
	}
	if marker == "" {
		marker = title
	}
	return "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n" +
		"  <title>" + title + "</title>\n" +
		`  <link rel="canonical" href="` + integrateCanonicalBase + "/" + address + `">` + "\n" +
		`  <link rel="stylesheet" href="style.css">` + "\n" +
		"</head>\n<body>\n" +
		`  <dialog class="search-dialog" data-search-base="./"` + versionAttr + `></dialog>` + "\n" +
		"  <p>" + marker + "</p>\n" +
		"</body>\n</html>\n"
}

// siblingBlock is the section every assembled page ends with, naming the other
// projects the site publishes.
//
// siteHop is the rendering page's hop back to the site root, so each link is
// document-relative, and the markup is the one internal/build writes -- the
// fixture's pages are hand-written, so the block has to be stated here rather
// than rendered.
func siblingBlock(siteHop string, slugs ...string) string {
	lines := []string{
		build.SiblingsBlockStart,
		`<h2 id="sibling-projects-heading">` + build.SiblingsHeading + "</h2>",
		"<ul>",
	}
	for _, slug := range slugs {
		lines = append(lines,
			`<li><a href="`+siteHop+slug+`/">`+slug+"</a></li>")
	}
	return strings.Join(append(lines, "</ul>", build.SiblingsBlockEnd), "\n") + "\n"
}

// integrateManifest is one project's manifest in the assembly.
func integrateManifest(slug, name, version string, posts []any) map[string]any {
	return map[string]any{
		"schema_version": 2,
		"name":           name,
		"slug":           slug,
		"version":        version,
		"description":    name + " docs",
		"language":       "python",
		"base_url":       integrateCanonicalBase + "/" + slug,
		"author":         map[string]any{"name": "Test Author", "url": "https://author.example"},
		"pages":          []any{map[string]any{"path": "index.md", "title": "Home"}},
		"posts":          posts,
		"last_gen":       "2024-01-01T00:00:00+00:00",
	}
}

// assemblyTree is an assembly repository checkout with an origin behind it and
// a cloned, already-built source project inside it.
type assemblyTree struct {
	t *testing.T
	// Root is the checkout the deploy runs in.
	Root string
	// Origin is the bare repository the deploy fetches from and pushes to.
	Origin string
	// git is the real git the fixture itself drives.
	git string
	// bin is the directory at the front of PATH, where the recording git
	// wrapper is written.
	bin string
	// log is where the wrapper records every git invocation.
	log string
	// pushes is where the wrapper counts pushes.
	pushes string
}

// newAssemblyTree lays out a realistic assembly checkout: two project subtrees
// already deployed under site/, their manifests, a membership record, the
// roster, and a cloned source project whose build output is waiting to be
// grafted in. Everything is committed and pushed to an origin, because the
// deploy's first act is to reset the checkout to it.
func newAssemblyTree(t *testing.T) *assemblyTree {
	t.Helper()
	bin := isolate(t)
	testproject.RequirePagefind(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is what the deploy commits and pushes with: %v", err)
	}

	base := t.TempDir()
	tree := &assemblyTree{
		t:      t,
		Root:   filepath.Join(base, "assembly"),
		Origin: filepath.Join(base, "origin.git"),
		git:    realGit,
		bin:    bin,
		log:    filepath.Join(base, "git.log"),
		pushes: filepath.Join(base, "pushes"),
	}

	// Two project subtrees already deployed.
	tree.Write("site/alpha/index.html", integratePage("Alpha", "alpha/", "old alpha", ""))
	tree.Write("site/alpha/guide/index.html",
		integratePage("Alpha Guide", "alpha/guide/", "old guide", ""))
	tree.Write("site/alpha/retired/index.html",
		integratePage("Retired", "alpha/retired/", "gone upstream", ""))
	// A post published between releases, at the site-level address every post
	// has: nobody's build produced it, so no publisher is entitled to prune it
	// and it outlives a full build.
	tree.Write("site/blog/old-post/index.html",
		integratePage("Old", "blog/old-post/", "old post", ""))
	tree.Write("site/beta/index.html", integratePage("Beta", "beta/", "beta", "2.0.0"))
	// The home project's front page, at the site root under no slug. It
	// carries the sibling block every assembled page ends with, which is how
	// a reader arriving at the site reaches a project the curated listing
	// leaves out.
	tree.Write("site/index.html",
		integratePage("Front page", "", "home", "")+siblingBlock("", "alpha", "beta"))

	// Manifests, including a stale posts overlay for alpha.
	tree.WriteJSON("manifests/alpha.json", integrateManifest("alpha", "Alpha", "0.9.0", nil))
	tree.WriteJSON("manifests/alpha-posts.json", integrateManifest(
		"alpha", "Alpha", "0.9.0", []any{map[string]any{
			"slug": "old-post", "title": "Old", "date": "2024-01-01",
		}}))
	tree.WriteJSON("manifests/beta.json", integrateManifest("beta", "Beta", "2.0.0", nil))
	tree.WriteJSON("manifests/home.json", integrateManifest("home", "Home", "0.1.0", nil))
	tree.WriteJSON("manifests/home-files.json", map[string]any{
		"schema_version": site.FilesRecordVersion,
		"slug":           "home",
		"owners":         map[string]any{"release": []any{"index.html"}},
	})
	// A declared home carries its curated listing: its deploy copies it in
	// beside the manifests, and shared generation refuses without it. Curation
	// is selection, so listing only alpha is legal -- and it keeps the fixture
	// usable by the tests that retire beta.
	tree.WriteJSON("manifests/home-listing.json", map[string]any{
		"format_version": 1,
		"slug":           "home",
		"categories": []any{map[string]any{
			"name": "Projects",
			"projects": []any{map[string]any{
				"slug": "alpha", "blurb": "Does the alpha thing.",
				"url": "", "name": "",
			}},
		}},
	})

	tree.WriteJSON(site.ProjectsPath, map[string]any{
		"home":  map[string]any{"repo": "owner/home", "ref": "v0.1.0", "version": "0.1.0"},
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v0.9.0", "version": "0.9.0"},
		"beta":  map[string]any{"repo": "owner/beta", "ref": "v2.0.0", "version": "2.0.0"},
	})
	tree.Write(site.RosterPath, site.RenderRoster(integrateRoster, "home"))

	// What the last release published for alpha, which is what a prune is
	// entitled to remove. Every path is site-relative, and the out-of-band
	// post is deliberately absent.
	tree.Write("manifests/alpha-files.json", string(filesRecord(t, "alpha", map[string][]string{
		"release": {
			"alpha/index.html", "alpha/guide/index.html", "alpha/retired/index.html",
		},
	})))

	tree.Commit()
	tree.writeSourceProject()
	tree.installGitWrapper(0)
	return tree
}

// writeSourceProject lays out the cloned source project the workflow's second
// checkout leaves behind, with a build output tree as a build would produce.
func (a *assemblyTree) writeSourceProject() {
	a.t.Helper()
	a.WriteJSON("source/alpha/selfdoc.json", map[string]any{
		"versions": []any{
			map[string]any{"version": "0.9.0"},
			map[string]any{"version": "1.0.0"},
		},
	})
	a.Write("source/alpha/stricttools/.docs-cache/build/index.html",
		integratePage("Alpha", "alpha/", "new alpha", "1.0.0"))
	a.Write("source/alpha/stricttools/.docs-cache/build/guide/index.html",
		integratePage("Alpha Guide", "alpha/guide/", "new guide", "1.0.0"))
	// The listing page the build renders for the project's own standalone
	// site. It is not grafted: the assembled site's blog index is written by
	// the shared generator and lists every project's posts.
	a.Write("source/alpha/stricttools/.docs-cache/build/blog/index.html",
		integratePage("Alpha Posts", "blog/", "standalone listing", "1.0.0"))
	a.Write("source/alpha/stricttools/.docs-cache/build/blog/hello/index.html",
		integratePage("Hello", "blog/hello/", "hello", "1.0.0"))
	// Per-project deploy artifacts the assembly must not inherit.
	a.Write("source/alpha/stricttools/.docs-cache/build/_headers", "/*\n  X-Frame-Options: DENY\n")
	a.Write("source/alpha/stricttools/.docs-cache/build/_redirects", "/* /index.html 200\n")
	a.Write("source/alpha/stricttools/.docs-cache/build/_worker.js", "export default {}\n")
	a.Write("source/alpha/stricttools/.docs-cache/build/index.html.gz", "gzipped")
	a.Write("source/alpha/stricttools/.docs-cache/build/guide/index.html.br", "brotli")
	// A full build's manifest carries the posts the build rendered.
	post := []any{map[string]any{"slug": "hello", "title": "Hello", "date": "2024-06-01"}}
	a.WriteJSON("source/alpha/stricttools/.docs-state/manifest.json",
		integrateManifest("alpha", "Alpha", "1.0.0", post))
	a.WriteJSON("source/alpha/stricttools/.docs-state/post-manifest.json",
		integrateManifest("alpha", "Alpha", "1.0.0", post))
}

// Path is an absolute path inside the checkout.
func (a *assemblyTree) Path(rel string) string {
	return filepath.Join(a.Root, filepath.Join(strings.Split(rel, "/")...))
}

// Write writes a file inside the checkout, making its parents.
func (a *assemblyTree) Write(rel, content string) {
	a.t.Helper()
	path := a.Path(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		a.t.Fatalf("writing %s: %v", rel, err)
	}
}

// WriteJSON writes a JSON document inside the checkout.
func (a *assemblyTree) WriteJSON(rel string, value any) {
	a.t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		a.t.Fatalf("encoding %s: %v", rel, err)
	}
	a.Write(rel, string(encoded)+"\n")
}

// Read is the text of a file inside the checkout.
func (a *assemblyTree) Read(rel string) string {
	a.t.Helper()
	data, err := os.ReadFile(a.Path(rel))
	if err != nil {
		a.t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

// ReadJSON decodes a JSON document inside the checkout.
func (a *assemblyTree) ReadJSON(rel string) map[string]any {
	a.t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(a.Read(rel)), &document); err != nil {
		a.t.Fatalf("%s is not JSON: %v", rel, err)
	}
	return document
}

// Exists reports whether a path inside the checkout is there.
func (a *assemblyTree) Exists(rel string) bool {
	_, err := os.Stat(a.Path(rel))
	return err == nil
}

// Remove deletes a path inside the checkout.
func (a *assemblyTree) Remove(rel string) {
	a.t.Helper()
	if err := os.RemoveAll(a.Path(rel)); err != nil {
		a.t.Fatalf("removing %s: %v", rel, err)
	}
}

// run drives the real git, outside the recording wrapper.
func (a *assemblyTree) run(dir string, args ...string) {
	a.t.Helper()
	command := exec.Command(a.git, args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		a.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// Commit stages everything in the checkout and publishes it to the origin,
// creating both on the first call.
//
// The deploy resets the checkout to the origin before it writes anything, so
// a fixture change that is not committed is a change the first attempt throws
// away. Every test that edits a tracked file calls this afterwards.
func (a *assemblyTree) Commit() {
	a.t.Helper()
	if _, err := os.Stat(filepath.Join(a.Root, ".git")); err != nil {
		if err := os.MkdirAll(a.Root, 0o755); err != nil {
			a.t.Fatalf("making the checkout: %v", err)
		}
		a.run(filepath.Dir(a.Origin), "init", "--bare", "--initial-branch=main", a.Origin)
		// A bare repository runs "gc --auto" in the background after a push,
		// and that process is still rewriting objects/ when the test's
		// temporary directory is removed. Nothing here is large enough to
		// need packing.
		a.run(a.Origin, "config", "receive.autogc", "false")
		a.run(a.Origin, "config", "gc.auto", "0")
		a.run(a.Root, "init", "--initial-branch=main")
		a.run(a.Root, "config", "gc.auto", "0")
		a.run(a.Root, "remote", "add", "origin", a.Origin)
		if err := os.WriteFile(filepath.Join(a.Root, ".gitignore"),
			[]byte(GitignoreContent()), 0o644); err != nil {
			a.t.Fatalf("writing the .gitignore: %v", err)
		}
	}
	a.run(a.Root, "add", "-A")
	a.run(a.Root, "commit", "--allow-empty", "-m", "fixture")
	a.run(a.Root, "push", "-u", "origin", "main")
}

// installGitWrapper writes a git at the front of PATH that records every
// invocation and rejects the first failures pushes.
//
// Everything else it forwards to the real git, so the deploy's fetch, reset,
// add and commit are the real operations against a real repository; only the
// push's verdict is the test's to choose, which is how a rejected push --
// another deploy having committed first -- is reproduced.
func (a *assemblyTree) installGitWrapper(failures int) {
	a.t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
if [ "$1" = "push" ]; then
  count=0
  [ -f %s ] && count=$(cat %s)
  count=$((count + 1))
  printf '%%s' "$count" > %s
  if [ "$count" -le %d ]; then
    printf 'rejected\n' >&2
    exit 1
  fi
fi
exec %s "$@"
`, shellQuote(a.log), shellQuote(a.pushes), shellQuote(a.pushes),
		shellQuote(a.pushes), failures, shellQuote(a.git))
	if err := os.WriteFile(filepath.Join(a.bin, "git"), []byte(script), 0o755); err != nil {
		a.t.Fatalf("writing the git wrapper: %v", err)
	}
}

// FailPushes makes the next failures pushes be rejected.
func (a *assemblyTree) FailPushes(failures int) {
	a.t.Helper()
	a.installGitWrapper(failures)
}

// GitCalls is every git invocation the deploy made, argv joined by spaces.
func (a *assemblyTree) GitCalls() []string {
	a.t.Helper()
	data, err := os.ReadFile(a.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		a.t.Fatalf("reading the git log: %v", err)
	}
	var calls []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return calls
}

// GitCallsOf is every recorded git invocation starting with prefix.
func (a *assemblyTree) GitCallsOf(prefix string) []string {
	a.t.Helper()
	var found []string
	for _, call := range a.GitCalls() {
		if strings.HasPrefix(call, prefix) {
			found = append(found, call)
		}
	}
	return found
}

// Pushes is how many pushes the deploy attempted.
func (a *assemblyTree) Pushes() int {
	a.t.Helper()
	data, err := os.ReadFile(a.pushes)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		a.t.Fatalf("reading the push counter: %v", err)
	}
	count := 0
	fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count)
	return count
}

// integrateOptions is the run every test starts from.
func (a *assemblyTree) integrateOptions() IntegrateOptions {
	return IntegrateOptions{
		Slug:          "alpha",
		Version:       "1.0.0",
		Ref:           "v1.0.0",
		SourceRepo:    "owner/alpha",
		Scope:         "full",
		CanonicalBase: integrateCanonicalBase,
		AssemblyDir:   a.Root,
		Attempts:      DefaultAttempts,
		RetryDelay:    0,
		SkipBuild:     true,
		Stderr:        &strings.Builder{},
	}
}

// Integrate runs one integration with the fixture's defaults, as modified.
func (a *assemblyTree) Integrate(modify func(*IntegrateOptions)) (*IntegrateSummary, error) {
	a.t.Helper()
	options := a.integrateOptions()
	if modify != nil {
		modify(&options)
	}
	return IntegrateProject(options, effects.Unbound())
}

// MustIntegrate runs one integration and fails the test when it refuses.
func (a *assemblyTree) MustIntegrate(modify func(*IntegrateOptions)) *IntegrateSummary {
	a.t.Helper()
	summary, err := a.Integrate(modify)
	if err != nil {
		a.t.Fatalf("integrate: %v", err)
	}
	return summary
}

// -- the graft ---------------------------------------------------------------

// graft runs the graft alone, outside a deploy.
func (a *assemblyTree) graft(scope string, home bool, stderr *strings.Builder) ([]string, error) {
	a.t.Helper()
	return ApplyProjectFiles(GraftOptions{
		AssemblyDir: a.Root,
		SourceDir:   a.Path("source/alpha"),
		Slug:        "alpha",
		Scope:       scope,
		Home:        home,
		Stderr:      stderr,
	}, effects.Unbound())
}

func TestApplyProjectFilesFullScope(t *testing.T) {
	tree := newAssemblyTree(t)
	if _, err := tree.graft("full", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	if tree.Exists("site/alpha/retired") {
		t.Error("a page the build no longer produces stayed")
	}
	if !tree.Exists("site/blog/hello/index.html") {
		t.Error("the build's post did not reach the site-level blog")
	}
	for _, rel := range []string{"site/alpha/_headers", "site/alpha/index.html.gz"} {
		if tree.Exists(rel) {
			t.Errorf("the assembly inherited %s", rel)
		}
	}
	if got := tree.ReadJSON("manifests/alpha.json")["version"]; got != "1.0.0" {
		t.Errorf("the manifest was not refreshed: version = %v", got)
	}
}

func TestAGraftedPostLandsAtTheSiteLevelAndNowhereElse(t *testing.T) {
	// The whole contract: "blog/<post-slug>/", never under a project slug.
	tree := newAssemblyTree(t)
	if _, err := tree.graft("full", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	if !tree.Exists("site/blog/hello/index.html") {
		t.Error("the post is not at the site level")
	}
	for _, rel := range []string{"site/alpha/blog", "site/alpha/posts"} {
		if tree.Exists(rel) {
			t.Errorf("a post landed under the project slug at %s", rel)
		}
	}
}

func TestTheProjectsOwnBlogListingIsNotGrafted(t *testing.T) {
	// A project's standalone listing would claim the whole site's blog index.
	tree := newAssemblyTree(t)
	if _, err := tree.graft("full", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	if tree.Exists("site/blog/index.html") &&
		strings.Contains(tree.Read("site/blog/index.html"), "standalone listing") {
		t.Fatal("the project's own blog listing claimed the site's blog index")
	}
}

func TestApplyProjectFilesRecordsWhatTheReleasePublished(t *testing.T) {
	tree := newAssemblyTree(t)
	if _, err := tree.graft("full", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	owners, err := site.LoadFilesManifest(tree.Path("manifests/alpha-files.json"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	want := []string{"alpha/guide/index.html", "alpha/index.html", "blog/hello/index.html"}
	got := append([]string(nil), owners["release"]...)
	slices.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the record claims %v, want %v", got, want)
	}
}

func TestApplyProjectFilesKeepsThePostsOverlay(t *testing.T) {
	// The overlay carries posts published between releases; it is merged now.
	tree := newAssemblyTree(t)
	if _, err := tree.graft("full", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	if !tree.Exists("manifests/alpha-posts.json") {
		t.Fatal("the overlay was deleted")
	}
	overlay := tree.ReadJSON("manifests/alpha-posts.json")
	posts, _ := overlay["posts"].([]any)
	var slugs []string
	for _, post := range posts {
		record, _ := post.(map[string]any)
		slug, _ := record["slug"].(string)
		slugs = append(slugs, slug)
	}
	slices.Sort(slugs)
	if !reflect.DeepEqual(slugs, []string{"hello", "old-post"}) {
		t.Fatalf("the folded overlay carries %v", slugs)
	}
}

func TestApplyProjectFilesPostsScopeTouchesOnlyPosts(t *testing.T) {
	tree := newAssemblyTree(t)
	if _, err := tree.graft("posts", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	if !strings.Contains(tree.Read("site/alpha/index.html"), "old alpha") {
		t.Error("the posts scope touched the project's pages")
	}
	if !strings.Contains(tree.Read("site/blog/hello/index.html"), "hello") {
		t.Error("the posts scope did not publish the post")
	}
	overlay := tree.ReadJSON("manifests/alpha-posts.json")
	posts, _ := overlay["posts"].([]any)
	if len(posts) == 0 {
		t.Fatal("the overlay carries no posts")
	}
	first, _ := posts[0].(map[string]any)
	if first["slug"] != "hello" {
		t.Fatalf("the overlay's first post is %v", first["slug"])
	}
}

func TestThePostsScopeClaimsTheSiteLevelPathsItWrote(t *testing.T) {
	tree := newAssemblyTree(t)
	if _, err := tree.graft("posts", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	owners, err := site.LoadFilesManifest(tree.Path("manifests/alpha-files.json"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if !reflect.DeepEqual(owners["posts"], []string{"blog/hello/index.html"}) {
		t.Fatalf("the posts owner claims %v", owners["posts"])
	}
}

func TestAPostsScopePublishWithNoPostsPublishesNothing(t *testing.T) {
	// A build that emitted no posts is not an instruction to unpublish.
	tree := newAssemblyTree(t)
	tree.Remove("source/alpha/stricttools/.docs-cache/build/blog")
	tree.Write("manifests/alpha-files.json", string(filesRecord(t, "alpha",
		map[string][]string{"posts": {"blog/old-post/index.html"}})))

	stderr := &strings.Builder{}
	touched, err := tree.graft("posts", false, stderr)
	if err != nil {
		t.Fatalf("graft: %v", err)
	}
	if len(touched) != 0 {
		t.Fatalf("the graft touched %v", touched)
	}
	if !tree.Exists("site/blog/old-post/index.html") {
		t.Error("the already-published post was unpublished")
	}
	owners, err := site.LoadFilesManifest(tree.Path("manifests/alpha-files.json"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if !reflect.DeepEqual(owners["posts"], []string{"blog/old-post/index.html"}) {
		t.Fatalf("the record now claims %v", owners["posts"])
	}
	if !strings.Contains(stderr.String(), "nothing to publish") {
		t.Fatalf("the advisory was not printed: %q", stderr.String())
	}
}

func TestAGraftRefusesToOverwriteAnotherProjectsPost(t *testing.T) {
	// Two projects, one post slug: the write is refused, naming both.
	tree := newAssemblyTree(t)
	tree.Write("manifests/beta-files.json", string(filesRecord(t, "beta",
		map[string][]string{"release": {"beta/index.html", "blog/hello/index.html"}})))
	_, err := tree.graft("full", false, nil)
	if err == nil {
		t.Fatal("the graft overwrote another project's post")
	}
	if !strings.Contains(err.Error(), "claimed by 'beta'") {
		t.Fatalf("err = %q, want it to name the claimant", err)
	}
	// A refused graft writes nothing.
	if tree.Exists("site/blog/hello") {
		t.Error("the refused graft wrote the post anyway")
	}
	if !strings.Contains(tree.Read("site/alpha/index.html"), "old alpha") {
		t.Error("the refused graft replaced the project's pages")
	}
}

// -- the full integrate run --------------------------------------------------

func TestFullIntegrateGraftsTheBuildAndCommits(t *testing.T) {
	tree := newAssemblyTree(t)
	summary := tree.MustIntegrate(nil)
	if !summary.Committed {
		t.Error("the deploy created no commit")
	}
	if summary.Attempt != 1 {
		t.Errorf("the deploy took %d attempt(s)", summary.Attempt)
	}
	if !strings.Contains(tree.Read("site/alpha/index.html"), "new alpha") {
		t.Error("the build was not grafted")
	}
	if tree.Exists("site/alpha/retired") {
		t.Error("a page the build no longer produces stayed")
	}
}

func TestFullIntegrateLeavesOtherProjectsAlone(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	if !strings.Contains(tree.Read("site/beta/index.html"), "beta") {
		t.Error("the other project's page changed")
	}
	beta, _ := tree.ReadJSON(site.ProjectsPath)["beta"].(map[string]any)
	if beta["version"] != "2.0.0" {
		t.Errorf("the other project's membership record reads %v", beta)
	}
}

func TestFullIntegrateRecordsMembership(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	alpha, _ := tree.ReadJSON(site.ProjectsPath)["alpha"].(map[string]any)
	want := map[string]any{"repo": "owner/alpha", "ref": "v1.0.0", "version": "1.0.0"}
	if !reflect.DeepEqual(alpha, want) {
		t.Fatalf("the record reads %v, want %v", alpha, want)
	}
}

func TestFullIntegrateRegeneratesTheSharedFiles(t *testing.T) {
	tree := newAssemblyTree(t)
	summary := tree.MustIntegrate(nil)
	siteDir := filepath.Join(tree.Root, "site")
	names := map[string]bool{}
	for _, path := range summary.Shared {
		rel, err := filepath.Rel(siteDir, path)
		if err != nil {
			t.Fatalf("%s is not under the site tree: %v", path, err)
		}
		names[filepath.ToSlash(rel)] = true
	}
	assets := 0
	for name := range names {
		if strings.HasPrefix(name, chrome.Dir+"/") {
			assets++
		}
	}
	// One site-level stylesheet, content-hashed, for the one theme in use.
	if assets != 1 {
		t.Errorf("the deploy wrote %d chrome asset(s), want 1", assets)
	}
	// Every page in the tree was re-pointed at it, so each is reported too.
	for _, rel := range []string{
		"alpha/index.html", "alpha/guide/index.html", "beta/index.html",
		"blog/hello/index.html", "blog/old-post/index.html",
	} {
		if !names[rel] {
			t.Errorf("%s was not reported as re-pointed", rel)
		}
	}
	// The listing at its fixed address, the home project's front page
	// re-rendered in place, and the rest of the site-wide artifacts. There is
	// no generated root index.html: the site root is the home project's page.
	for _, rel := range []string{
		"projects/index.html", "index.html", "blog/index.html", "nav.json",
		"feed.xml", "sitemap.xml", "robots.txt", "llms.txt", "404.html",
		"_headers",
	} {
		if !names[rel] {
			t.Errorf("%s was not reported as written", rel)
		}
	}
	// No redirect worker: the assembly emits none.
	if names["_worker.js"] {
		t.Error("the deploy reported a _worker.js")
	}
}

func TestFullIntegrateSharedFilesSeeTheNewManifest(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	nav := tree.ReadJSON("site/nav.json")
	projects, _ := nav["projects"].([]any)
	versions := map[string]any{}
	for _, entry := range projects {
		record, _ := entry.(map[string]any)
		slug, _ := record["slug"].(string)
		versions[slug] = record["version"]
	}
	if versions["alpha"] != "1.0.0" || versions["beta"] != "2.0.0" {
		t.Fatalf("nav records %v", versions)
	}
}

func TestFullIntegrateIndexesTheSite(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	if _, err := os.Stat(tree.Path("site/pagefind/pagefind-entry.json")); err != nil {
		t.Fatalf("the search index was not rebuilt: %v", err)
	}
}

func TestFullIntegrateSyncsWithTheRemoteBeforeWriting(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	calls := tree.GitCalls()
	if len(calls) < 2 {
		t.Fatalf("the deploy ran %d git command(s)", len(calls))
	}
	if !strings.HasPrefix(calls[0], "fetch origin ") {
		t.Errorf("the deploy's first git command is %q", calls[0])
	}
	if !strings.HasPrefix(calls[1], "reset --hard origin/") {
		t.Errorf("the deploy's second git command is %q", calls[1])
	}
}

func TestFullIntegrateStagesAndPushes(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	if len(tree.GitCallsOf("add ")) == 0 {
		t.Error("the deploy tree was not staged")
	}
	if len(tree.GitCallsOf("push ")) == 0 {
		t.Error("the deploy commit was not pushed")
	}
}

func TestFullIntegrateCommitMessageNamesTheRelease(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	commits := tree.GitCallsOf("-c user.name=")
	if len(commits) == 0 {
		t.Fatal("the deploy made no commit")
	}
	if !strings.HasSuffix(commits[0], "deploy: alpha v1.0.0") {
		t.Errorf("the commit message line is %q", commits[0])
	}
	// The identity is explicit, never the machine's.
	if !strings.Contains(commits[0], "user.name="+DefaultGitUserName) {
		t.Errorf("the commit did not name its identity: %q", commits[0])
	}
	if !strings.Contains(commits[0], "user.email=") {
		t.Errorf("the commit declared no address: %q", commits[0])
	}
}

func TestPostsScopeReplacesOnlyThePostsSubtree(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(func(o *IntegrateOptions) { o.Scope = "posts" })
	if !strings.Contains(tree.Read("site/alpha/index.html"), "old alpha") {
		t.Error("the posts scope replaced the project's pages")
	}
	if !tree.Exists("site/blog/hello/index.html") {
		t.Error("the posts scope published no post")
	}
}

func TestSharedOnlyScopeTouchesNoProjectFiles(t *testing.T) {
	tree := newAssemblyTree(t)
	before := tree.Read(site.ProjectsPath)
	summary := tree.MustIntegrate(func(o *IntegrateOptions) {
		o.Scope = SharedOnlyScope
		o.Slug = ""
	})
	if !strings.Contains(tree.Read("site/alpha/index.html"), "old alpha") {
		t.Error("a shared-only deploy replaced a project's pages")
	}
	if tree.Read(site.ProjectsPath) != before {
		t.Error("a shared-only deploy rewrote the membership record")
	}
	if len(summary.Shared) == 0 {
		t.Error("a shared-only deploy regenerated nothing")
	}
	commits := tree.GitCallsOf("-c user.name=")
	if len(commits) == 0 || !strings.HasSuffix(commits[0], "deploy: shared elements") {
		t.Fatalf("the commit message line is %v", commits)
	}
}

func TestEmptyScopeMeansAFullBuild(t *testing.T) {
	tree := newAssemblyTree(t)
	summary := tree.MustIntegrate(func(o *IntegrateOptions) { o.Scope = "" })
	if summary.Scope != "full" {
		t.Fatalf("scope = %q", summary.Scope)
	}
}

func TestUnknownScopeIsAHardError(t *testing.T) {
	tree := newAssemblyTree(t)
	_, err := tree.Integrate(func(o *IntegrateOptions) { o.Scope = "everything" })
	if err == nil || !strings.Contains(err.Error(), "unknown scope") {
		t.Fatalf("err = %v, want the unknown-scope refusal", err)
	}
}

func TestAProjectScopedIntegrateNeedsASlug(t *testing.T) {
	tree := newAssemblyTree(t)
	_, err := tree.Integrate(func(o *IntegrateOptions) { o.Slug = "" })
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("err = %v, want the missing-slug refusal", err)
	}
}

func TestAnUndeclaredSlugCannotBeDeployedInto(t *testing.T) {
	tree := newAssemblyTree(t)
	_, err := tree.Integrate(func(o *IntegrateOptions) { o.Slug = "gamma" })
	if err == nil {
		t.Fatal("a dispatch for an undeclared slug was integrated")
	}
	if !strings.Contains(err.Error(), "not declared in "+site.RosterPath) {
		t.Fatalf("err = %q, want it to name the declaration", err)
	}
}

func TestTheHomeProjectsPageStaysTheSiteRoot(t *testing.T) {
	// A deploy of another project leaves the front page where it is.
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	if !strings.Contains(tree.Read("site/index.html"), "home") {
		t.Error("the front page was replaced")
	}
	if !tree.Exists("site/projects/index.html") {
		t.Error("the generated listing is missing")
	}
}

func TestTheGeneratedListingLeavesTheHomeProjectOut(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.MustIntegrate(nil)
	listing := tree.Read("site/projects/index.html")
	if !strings.Contains(listing, "Alpha") {
		t.Error("the listing does not carry the project")
	}
	if strings.Contains(listing, ">Home<") {
		t.Error("the listing carries the home project")
	}
}

// -- the retry loop ----------------------------------------------------------

func TestARejectedPushIsRetriedAfterAReSync(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.FailPushes(1)
	summary := tree.MustIntegrate(nil)
	if summary.Attempt != 2 {
		t.Errorf("the deploy took %d attempt(s), want 2", summary.Attempt)
	}
	if got := len(tree.GitCallsOf("fetch ")); got != 2 {
		t.Errorf("the deploy fetched %d time(s); each attempt re-syncs first", got)
	}
	if got := tree.Pushes(); got != 2 {
		t.Errorf("the deploy pushed %d time(s), want 2", got)
	}
}

func TestExhaustedAttemptsAreAHardError(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.FailPushes(99)
	_, err := tree.Integrate(nil)
	if err == nil || !strings.Contains(err.Error(), "failed after 3 attempt") {
		t.Fatalf("err = %v, want the exhausted-attempts refusal", err)
	}
	if got := tree.Pushes(); got != 3 {
		t.Errorf("the deploy pushed %d time(s), want 3", got)
	}
}

func TestTheAttemptCountIsTheCallersToState(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.FailPushes(99)
	if _, err := tree.Integrate(func(o *IntegrateOptions) { o.Attempts = 1 }); err == nil {
		t.Fatal("an exhausted deploy reported success")
	}
	if got := tree.Pushes(); got != 1 {
		t.Errorf("the deploy pushed %d time(s), want 1", got)
	}
}

func TestZeroAttemptsIsRejected(t *testing.T) {
	tree := newAssemblyTree(t)
	_, err := tree.Integrate(func(o *IntegrateOptions) { o.Attempts = 0 })
	if err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("err = %v, want the attempt-count refusal", err)
	}
}

// -- verification blocks the deploy ------------------------------------------
//
// The deploy verifies the tree it assembled before it commits or pushes any of
// it. There is no flag that turns this off: a tree that fails is a tree that
// does not ship.

// breakTheBuild puts a page the assembly must not serve into the source build.
func (a *assemblyTree) breakTheBuild() {
	a.t.Helper()
	a.Write("source/alpha/stricttools/.docs-cache/build/index.html",
		"<html><head></head><body>no title, no canonical</body></html>")
}

func TestABrokenTreeFailsTheDeployAndNamesEveryOffender(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.breakTheBuild()
	_, err := tree.Integrate(nil)
	if err == nil {
		t.Fatal("a broken tree was deployed")
	}
	for _, want := range []string{
		"failed verification", "page-metadata", "site/alpha/index.html",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestNothingIsDeployedWhenVerificationFails(t *testing.T) {
	// The whole point: the failure comes before the commit and the push.
	tree := newAssemblyTree(t)
	tree.breakTheBuild()
	if _, err := tree.Integrate(nil); err == nil {
		t.Fatal("a broken tree was deployed")
	}
	if got := tree.GitCallsOf("push "); len(got) != 0 {
		t.Errorf("a failing tree was pushed: %v", got)
	}
	if got := tree.GitCallsOf("-c user.name="); len(got) != 0 {
		t.Errorf("a failing tree was committed: %v", got)
	}
}

func TestAManifestPromisingAPageTheBuildDroppedFailsTheDeploy(t *testing.T) {
	tree := newAssemblyTree(t)
	manifest := integrateManifest("beta", "Beta", "2.0.0", nil)
	manifest["pages"] = []any{map[string]any{"path": "gone.md", "title": "Gone"}}
	tree.WriteJSON("manifests/beta.json", manifest)
	tree.Commit()
	_, err := tree.Integrate(nil)
	if err == nil || !strings.Contains(err.Error(), "manifest-pages-emitted") {
		t.Fatalf("err = %v, want the unemitted-page refusal", err)
	}
}

func TestASoundTreeStillDeploys(t *testing.T) {
	tree := newAssemblyTree(t)
	summary := tree.MustIntegrate(nil)
	if !summary.Committed {
		t.Error("a sound tree was not committed")
	}
	if len(tree.GitCallsOf("push ")) == 0 {
		t.Error("a sound tree was not pushed")
	}
}

func TestADeployAfterARetirementClearsTheRetiredProjectsSiblingLinks(t *testing.T) {
	// The live failure, end to end. A retirement removes one project's
	// subtree and its manifests; every other project's pages keep the sibling
	// block their own last deploy rendered, which still links the address the
	// tree no longer serves. Verification reads the whole tree, so until
	// something rewrites those blocks every deploy is refused -- and no single
	// project's rebuild can reach another project's pages.
	tree := newAssemblyTree(t)
	tree.Write("site/index.html",
		integratePage("Front page", "", "home", "")+
			siblingBlock("", "alpha", "beta", "gamma"))
	tree.Write("site/beta/index.html",
		integratePage("Beta", "beta/", "beta", "2.0.0")+
			siblingBlock("../", "alpha", "gamma"))
	tree.Write("site/blog/old-post/index.html",
		integratePage("Old", "blog/old-post/", "old post", "")+
			siblingBlock("../../", "alpha", "beta", "gamma"))
	tree.Commit()

	summary := tree.MustIntegrate(nil)
	if !summary.Committed {
		t.Fatal("the deploy after a retirement created no commit")
	}
	for _, rel := range []string{
		"site/index.html", "site/beta/index.html", "site/blog/old-post/index.html",
	} {
		page := tree.Read(rel)
		if strings.Contains(page, "gamma/") {
			t.Errorf("%s still links the retired project:\n%s", rel, page)
		}
	}
	// And the blocks say what the membership is now, descriptions and all.
	if want := `<li><a href="../alpha/">Alpha</a> <span>Alpha docs</span></li>`; !strings.Contains(
		tree.Read("site/beta/index.html"), want) {
		t.Errorf("beta's block does not carry %s:\n%s", want, tree.Read("site/beta/index.html"))
	}
	front := tree.Read("site/index.html")
	for _, want := range []string{`<a href="alpha/">Alpha</a>`, `<a href="beta/">Beta</a>`} {
		if !strings.Contains(front, want) {
			t.Errorf("the front page's block does not carry %s:\n%s", want, front)
		}
	}
}

func TestADeployRefreshesTheSiblingBlockOfAProjectThatDidNotDeploy(t *testing.T) {
	// The other half of the same defect: a project joining, or one whose
	// description changed, reaches the pages of projects that did not deploy.
	tree := newAssemblyTree(t)
	tree.Write("site/beta/index.html",
		integratePage("Beta", "beta/", "beta", "2.0.0")+siblingBlock("../"))
	tree.Commit()

	tree.MustIntegrate(nil)
	page := tree.Read("site/beta/index.html")
	if want := `<li><a href="../alpha/">Alpha</a> <span>Alpha docs</span></li>`; !strings.Contains(page, want) {
		t.Errorf("beta's block never learned about alpha; it reads\n%s", page)
	}
}

func TestTheDeployReportsWhichChecksItRan(t *testing.T) {
	tree := newAssemblyTree(t)
	summary := tree.MustIntegrate(nil)
	for _, check := range []string{"roster-agreement", "cross-project-links"} {
		if !slices.Contains(summary.Verified, check) {
			t.Errorf("%s is not in the reported checks: %v", check, summary.Verified)
		}
	}
}

func TestAnUnconfiguredOutboundCheckIsAnnounced(t *testing.T) {
	// A check that could not run says so rather than passing quietly.
	tree := newAssemblyTree(t)
	stderr := &strings.Builder{}
	tree.MustIntegrate(func(o *IntegrateOptions) { o.Stderr = stderr })
	if !strings.Contains(stderr.String(), "outbound-links was NOT checked") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestTheDeployKeepsTheOutboundResultsItProduced(t *testing.T) {
	// The store is the deploy's to write, so the next deploy inherits it.
	tree := newAssemblyTree(t)
	tree.Write(site.OutboundPath,
		"cache_days = 7\n\n[[page]]\npath = \"beta/index.html\"\n")
	tree.Write("site/beta/index.html", integratePage(
		"Beta", "beta/", `<a href="https://live.example.net/">x</a>`, "2.0.0"))
	tree.Commit()

	var asked []string
	tree.MustIntegrate(func(o *IntegrateOptions) {
		o.Fetch = func(url string) (int, string) {
			asked = append(asked, url)
			return 200, ""
		}
	})

	if !reflect.DeepEqual(asked, []string{"https://live.example.net/"}) {
		t.Fatalf("the deploy fetched %v", asked)
	}
	stored := tree.ReadJSON(site.OutboundCachePath)
	entries, _ := stored["entries"].(map[string]any)
	if _, present := entries["https://live.example.net/"]; !present {
		t.Fatalf("the store holds %v", stored)
	}
	staged := false
	for _, call := range tree.GitCallsOf("add ") {
		if strings.Contains(call, site.OutboundCachePath) {
			staged = true
		}
	}
	if !staged {
		t.Errorf("the store was not staged: %v", tree.GitCallsOf("add "))
	}
}
