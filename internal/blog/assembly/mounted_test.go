package assembly

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/verify"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// mountedPostSlug is the post the mounted project publishes.
const mountedPostSlug = "hello-world"

// TestAMountedBuildGraftsWithEveryReferenceStillResolving runs the real build
// of a project declaring a mount, grafts what it produced the way a deploy
// does, and reads the result with the deploy's own verification.
//
// That is the only way the defects this covers could be seen at all.
//
// The first: the build emitted a project page linking "../blog/<post>/" and a
// post canonicalized under the project's slug, both correct in the project's
// own output tree and both naming nothing once the assembly has moved the
// posts to the site level.
//
// The second, which the repair for the first introduced: those references were
// made absolute, against the site's declared base. In the assembled tree they
// resolve -- to production, from wherever the tree is actually being served. A
// preview reads as a preview right up to the first click on a post, which
// silently leaves it. So the assertions are two-sided: every reference resolves
// inside the tree, and no reference a reader clicks names the canonical base at
// all.
func TestAMountedBuildGraftsWithEveryReferenceStillResolving(t *testing.T) {
	tree := newAssemblyTree(t)
	source := buildMountedAlpha(t, tree)

	tree.Remove("source/alpha")
	if err := os.MkdirAll(filepath.Dir(tree.Path("source/alpha")), 0o755); err != nil {
		t.Fatalf("making the source directory: %v", err)
	}
	if err := effects.Unbound().CopyTree(source, tree.Path("source/alpha"), false); err != nil {
		t.Fatalf("installing the build: %v", err)
	}
	// The site root's own assets, which the home project supplies on the live
	// site: its output root IS the site root, so its stylesheet and favicon are
	// the ones a page at the site level asks for.
	for _, name := range []string{"style.css", "favicon.svg"} {
		tree.Write("site/"+name, "/* home */")
	}
	tree.Commit()

	tree.MustIntegrate(nil)

	// The post is served at the site level, under no project slug.
	if !tree.Exists("site/blog/" + mountedPostSlug + "/index.html") {
		t.Fatalf("the post is not at site/blog/%s/", mountedPostSlug)
	}
	if tree.Exists("site/alpha/blog") {
		t.Error("the post landed under the project slug as well")
	}

	// Its canonical is its site-level address.
	post := tree.Read("site/blog/" + mountedPostSlug + "/index.html")
	want := `<link rel="canonical" href="` + integrateCanonicalBase + "/blog/" + mountedPostSlug + `/">`
	if !strings.Contains(post, want) {
		t.Errorf("the post's canonical is not its site-level address:\n%s", post)
	}

	// Every reference in the grafted tree resolves, and none of them leaves it.
	siteDir := filepath.Join(tree.Root, "site")
	findings, err := resolution.CheckOutputResolution(siteDir, integrateCanonicalBase, "", nil)
	if err != nil {
		t.Fatalf("reading the tree's references: %v", err)
	}
	for _, finding := range findings {
		t.Errorf("unresolved reference in %s: %s", finding.File(), finding.Message())
	}

	// The deploy's own verification passes over the tree it assembled.
	report, err := verify.VerifyAssembly(tree.Root, integrateCanonicalBase,
		func(string) (int, string) { return 200, "" }, 0)
	if err != nil {
		t.Fatalf("verifying the grafted tree: %v", err)
	}
	if !report.OK() {
		t.Fatalf("the grafted tree failed verification:\n%s", report.ErrorText())
	}

	// A mounted build emits no 404 of its own: the provider answers an
	// unmatched address from the root of what it serves, so a copy buried in a
	// project's subtree is never reached.
	if tree.Exists("site/alpha/404.html") {
		t.Error("the project's own 404 page reached the assembly")
	}
}

// buildMountedAlpha builds a project mounted at "/alpha/" with one post, and
// returns its checkout.
//
// It is built before the fixture's recording git wrapper matters, because the
// build shells out to git for the post manifest and those calls are not the
// deploy's.
func buildMountedAlpha(t *testing.T, tree *assemblyTree) string {
	t.Helper()
	source := testproject.Make(t, map[string]any{
		"base_url": integrateCanonicalBase + "/alpha",
		"topology": map[string]any{
			"docs_base": integrateCanonicalBase,
			"slug":      "alpha",
		},
	})
	docs := filepath.Join(source, "stricttools", "docs")
	// The guide defines a term of its own and mentions the one the post
	// defines. That makes the build write a cross-page term link in each
	// direction, and under a mount the two cross the boundary between the
	// project's subtree and the site level in opposite directions -- the
	// references this test exists to resolve.
	testproject.WriteText(t, filepath.Join(docs, "guide.md"),
		"# Guide\n\nHow to.\n\n"+
			"## Widget catalog\n\n"+
			"<dfn>Widget catalog</dfn> is a list of every widget this project ships.\n\n"+
			"See the notes on chained revision for the history model.\n")
	testproject.WriteText(t, filepath.Join(source, "stricttools", "posts", "hello.md"),
		"+++\ntitle = \"Hello World\"\ndate = 2024-06-01\n"+
			"slug = \""+mountedPostSlug+"\"\ntags = []\ndraft = false\ndirectives = false\n"+
			"+++\nThe post body.\n\n"+
			"## Chained revision\n\n"+
			"<dfn>Chained revision</dfn> is a recorded edge between two schema states.\n\n"+
			"The widget catalog is described at length in the guide.\n")

	if _, err := build.Build(build.Options{
		DirPath: source,
		Stdout:  &strings.Builder{},
	}, effects.Unbound()); err != nil {
		t.Fatalf("building the mounted project: %v", err)
	}

	// The manifest the deploy copies in beside the assembly's own.
	manifest := integrateManifest("alpha", "Alpha", "1.0.0", []any{
		map[string]any{
			"slug": mountedPostSlug, "title": "Hello World", "date": "2024-06-01",
		},
	})
	manifest["pages"] = []any{
		map[string]any{"path": "index.md", "title": "Home"},
		map[string]any{"path": "guide.md", "title": "Guide"},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	testproject.WriteText(t, filepath.Join(source, "stricttools", ".docs-state", "manifest.json"), string(encoded))
	return source
}

// TestTheGraftedTreeClaimsThePostAtItsSiteLevelAddress asserts the record the
// release writes names the post where the assembly serves it, not where the
// build emitted it.
func TestTheGraftedTreeClaimsThePostAtItsSiteLevelAddress(t *testing.T) {
	tree := newAssemblyTree(t)
	if _, err := tree.graft("full", false, nil); err != nil {
		t.Fatalf("graft: %v", err)
	}
	owners, err := site.LoadFilesManifest(tree.Path("manifests/alpha-files.json"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if !slices.Contains(owners["release"], "blog/hello/index.html") {
		t.Fatalf("the release claims %v", owners["release"])
	}
	for _, claimed := range owners["release"] {
		if strings.HasPrefix(claimed, "alpha/blog/") {
			t.Errorf("the release claims a post under the project slug: %s", claimed)
		}
	}
}
