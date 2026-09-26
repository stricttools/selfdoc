package assembly

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/verify"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// navLinksPostSlug is the post the project in this file publishes.
const navLinksPostSlug = "why-we-built-it"

// TestAProjectPageLinksToTheSiteBlogDocumentRelatively asserts the two links
// every project page carrying posts writes into the site-level blog -- the
// listing and each post's permalink -- are document-relative in the assembled
// tree.
//
// The project here is configured the way a real constituent of the unified
// site is, which is what the older mounted-graft test does not cover: a
// base_url naming a retired per-project subdomain rather than the mount, a
// declared docs_base and a declared posts_base. Each of those is a place an
// absolute URL could be taken from, and an <a href> built from any of them
// resolves on the deployed host alone -- it leaves a preview or a mirror
// silently, which the file-existence half of the resolution check can never
// see because the page it names really is there.
func TestAProjectPageLinksToTheSiteBlogDocumentRelatively(t *testing.T) {
	tree := newAssemblyTree(t)
	source := buildSiteLikeAlpha(t)

	tree.Remove("source/alpha")
	if err := os.MkdirAll(filepath.Dir(tree.Path("source/alpha")), 0o755); err != nil {
		t.Fatalf("making the source directory: %v", err)
	}
	if err := effects.Unbound().CopyTree(source, tree.Path("source/alpha"), false); err != nil {
		t.Fatalf("installing the build: %v", err)
	}
	for _, name := range []string{"style.css", "favicon.svg"} {
		tree.Write("site/"+name, "/* home */")
	}
	tree.Commit()

	tree.MustIntegrate(nil)

	// The project page sits two levels under the site root, so both links
	// climb out of the project's mount and back down into "blog/".
	page := tree.Read("site/alpha/guide/index.html")
	for _, want := range []string{
		`href="../../blog/"`,
		`href="../../blog/` + navLinksPostSlug + `/"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the project page does not carry %s", want)
		}
	}
	for _, ref := range resolution.NavigationReferences(page) {
		if _, ours := resolution.SiteRelativePath(ref, integrateCanonicalBase); ours {
			t.Errorf("a clickable link is absolute against the site's base: %s", ref)
		}
	}

	// And the same rule over the whole assembled tree, which is the check the
	// deploy refuses on.
	findings, err := resolution.CheckOutputResolution(
		filepath.Join(tree.Root, "site"), integrateCanonicalBase, "", nil,
	)
	if err != nil {
		t.Fatalf("reading the tree's references: %v", err)
	}
	for _, finding := range findings {
		t.Errorf("unresolved reference in %s: %s", finding.File(), finding.Message())
	}
}

// TestIntegrateRelativizesAnOlderDeploysLinksIntoTheSiteBlog asserts a page an
// earlier deploy left in the tree, addressing the site-level blog by the site's
// own base, is re-expressed document-relative by the next deploy of any
// project -- and that the deploy's verification passes over it afterwards.
//
// The assembly is never rebuilt whole: a project's subtree is replaced only
// when that project deploys. A page an older toolchain wrote therefore outlives
// it, and the rule the verification applies to the whole tree would otherwise
// refuse every deploy over pages the dispatch neither wrote nor could fix --
// with no dispatch left that could ever repair them.
func TestIntegrateRelativizesAnOlderDeploysLinksIntoTheSiteBlog(t *testing.T) {
	tree := newAssemblyTree(t)
	stale := strings.Replace(
		integratePage("Beta Guide", "beta/guide/", "beta guide", ""),
		"<p>beta guide</p>",
		`<p><a href="`+integrateCanonicalBase+`/blog/">Posts</a>`+
			`<a href="`+integrateCanonicalBase+`/blog/old-post/">Old</a></p>`,
		1,
	)
	if !strings.Contains(stale, integrateCanonicalBase+"/blog/") {
		t.Fatal("the fixture page does not carry the links this test is about")
	}
	tree.Write("site/beta/guide/index.html", stale)
	tree.Commit()

	tree.MustIntegrate(nil)

	page := tree.Read("site/beta/guide/index.html")
	for _, want := range []string{`href="../../blog/"`, `href="../../blog/old-post/"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page an earlier deploy left does not carry %s:\n%s", want, page)
		}
	}
	if strings.Contains(page, `<a href="`+integrateCanonicalBase) {
		t.Errorf("a clickable link still names the site's own base:\n%s", page)
	}
	// Metadata says where the page lives in the world, and stays absolute.
	wantCanonical := `<link rel="canonical" href="` + integrateCanonicalBase + `/beta/guide/">`
	if !strings.Contains(page, wantCanonical) {
		t.Errorf("the canonical was rewritten:\n%s", page)
	}

	report, err := verify.VerifyAssembly(tree.Root, integrateCanonicalBase,
		func(string) (int, string) { return 200, "" }, 0)
	if err != nil {
		t.Fatalf("verifying the tree: %v", err)
	}
	if !report.OK() {
		t.Fatalf("the tree failed verification:\n%s", report.ErrorText())
	}
}

// buildSiteLikeAlpha builds a project mounted at "/alpha/" and configured the
// way a constituent of the unified site is, and returns its checkout.
func buildSiteLikeAlpha(t *testing.T) string {
	t.Helper()
	source := testproject.Make(t, map[string]any{
		// The retired per-project subdomain a converted project keeps
		// declaring: it is not the address the assembly serves this project
		// at, so a link built from it names another host entirely.
		"base_url": "https://alpha.example.org",
		"topology": map[string]any{
			"docs_base":  integrateCanonicalBase,
			"slug":       "alpha",
			"posts_base": integrateCanonicalBase + "/blog",
		},
	})
	docs := filepath.Join(source, "stricttools", "docs")
	testproject.WriteText(t, filepath.Join(docs, "guide.md"),
		"# Guide\n\nHow to use the thing.\n")
	testproject.WriteText(t, filepath.Join(source, "stricttools", "posts", "why.md"),
		"+++\ntitle = \"Why We Built It\"\ndate = 2024-06-01\n"+
			"slug = \""+navLinksPostSlug+"\"\ntags = []\ndraft = false\ndirectives = false\n"+
			"+++\nThe post body.\n")

	if _, err := build.Build(build.Options{
		DirPath: source,
		Stdout:  &strings.Builder{},
	}, effects.Unbound()); err != nil {
		t.Fatalf("building the mounted project: %v", err)
	}

	manifest := integrateManifest("alpha", "Alpha", "1.0.0", []any{
		map[string]any{
			"slug": navLinksPostSlug, "title": "Why We Built It", "date": "2024-06-01",
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
