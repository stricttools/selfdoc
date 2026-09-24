package site

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// -- where a built file goes -------------------------------------------------

func TestSplitBuildOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		rels  []string
		slug  string
		home  bool
		want  map[string]string
		about string
	}{
		{
			name:  "the home project's pages land at the site root",
			rels:  []string{"index.html", "cv/index.html", "assets/pic.jpg"},
			slug:  "home",
			home:  true,
			about: "the site root IS its content root",
			want: map[string]string{
				"index.html":     "index.html",
				"cv/index.html":  "cv/index.html",
				"assets/pic.jpg": "assets/pic.jpg",
			},
		},
		{
			name: "another project's pages land under its slug",
			rels: []string{"index.html"},
			slug: "alpha",
			want: map[string]string{"index.html": "alpha/index.html"},
		},
		{
			name:  "the home project's posts are still site-level",
			rels:  []string{"blog/hello/index.html", "blog/index.html"},
			slug:  "home",
			home:  true,
			about: "the standalone blog index is never grafted",
			want:  map[string]string{"blog/hello/index.html": "blog/hello/index.html"},
		},
		{
			name:  "another project's posts are site-level too",
			rels:  []string{"blog/hello/index.html", "blog/index.html", "index.html"},
			slug:  "alpha",
			about: "a post carries no project segment",
			want: map[string]string{
				"blog/hello/index.html": "blog/hello/index.html",
				"index.html":            "alpha/index.html",
			},
		},
		{
			name: "the home build drops the artifacts the assembly writes",
			rels: append(append([]string{}, HomeDroppedArtifacts...),
				"index.html", "index.html.gz"),
			slug:  "home",
			home:  true,
			about: "its own build wrote a robots.txt for standalone hosting; the site has one",
			want:  map[string]string{"index.html": "index.html"},
		},
		{
			name: "the home build drops its own search index",
			rels: []string{
				"index.html",
				"pagefind/pagefind-entry.json",
				"pagefind/fragment/en_abc.pf_fragment",
			},
			slug:  "home",
			home:  true,
			about: "the site-wide index is the assembly's",
			want:  map[string]string{"index.html": "index.html"},
		},
		{
			name:  "a home page under the index directory is kept for refusal",
			rels:  []string{"pagefind/index.html", "pagefind/pagefind-entry.json"},
			slug:  "home",
			home:  true,
			about: "a page called pagefind.md claims the site-wide index's address",
			want:  map[string]string{"pagefind/index.html": "pagefind/index.html"},
		},
		{
			name:  "another project keeps its own search index",
			rels:  []string{"index.html", "pagefind/pagefind-entry.json"},
			slug:  "alpha",
			about: "its pages address the index inside their own subtree",
			want: map[string]string{
				"index.html":                   "alpha/index.html",
				"pagefind/pagefind-entry.json": "alpha/pagefind/pagefind-entry.json",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := SplitBuildOutput(test.rels, test.slug, test.home)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("SplitBuildOutput = %v, want %v (%s)", got, test.want, test.about)
			}
		})
	}
}

// -- the addresses the assembly owns -----------------------------------------

func TestEveryReservedDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	found := HomeCollisions([]string{
		"blog/x/index.html", "projects/index.html", "v/1.0.0/index.html",
		"pagefind/pagefind.js", "cv/index.html",
	})
	paths := make([]string, 0, len(found))
	for _, collision := range found {
		paths = append(paths, collision.Path)
	}
	sort.Strings(paths)
	want := []string{
		"blog/x/index.html", "pagefind/pagefind.js", "projects/index.html",
		"v/1.0.0/index.html",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("collisions = %v, want %v", paths, want)
	}
	for _, collision := range found {
		if !strings.Contains(collision.Why, "are reserved") {
			t.Errorf("collision %q says %q, which names no reason", collision.Path, collision.Why)
		}
	}
}

func TestAPageAtANonReservedAddressIsNoCollision(t *testing.T) {
	t.Parallel()
	if found := HomeCollisions([]string{"index.html", "cv/index.html"}); len(found) != 0 {
		t.Errorf("collisions = %v, want none", found)
	}
}

// A page called projects.md would claim the generated listing's address.
func TestAHomePageOnAReservedDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	err := CheckHomeCollisions([]string{"index.html", "projects/index.html"}, "home")
	want := "the home project 'home' emits 1 file(s) at addresses the assembly " +
		"owns: site/projects/index.html -- projects/ is the assembly's own " +
		"directory (blog, projects, v, pagefind are reserved). The home " +
		"project's content root is the site root, so it shares that namespace " +
		"with the generated listing, blog, archives and site-wide artifacts. " +
		"Rename the page."
	if err == nil || err.Error() != want {
		t.Fatalf("refusal:\n%v\nwant:\n%s", err, want)
	}
}

func TestAHomePageUnderTheIndexDirectoryIsStillRefused(t *testing.T) {
	t.Parallel()
	produced := SplitBuildOutput(
		[]string{"pagefind/index.html", "pagefind/pagefind-entry.json"}, "home", true,
	)
	served := make([]string, 0, len(produced))
	for _, siteRel := range produced {
		served = append(served, siteRel)
	}
	err := CheckHomeCollisions(served, "home")
	if err == nil || !strings.Contains(err.Error(), "pagefind/") {
		t.Fatalf("err = %v, want the pagefind collision refused", err)
	}
}

func TestACollisionFreeHomeBuildPasses(t *testing.T) {
	t.Parallel()
	if err := CheckHomeCollisions([]string{"index.html", "cv/index.html"}, "home"); err != nil {
		t.Errorf("err = %v, want none", err)
	}
}

// -- the graft ---------------------------------------------------------------

func TestGraftSubtreeCopiesProducedAndDeletesRemoved(t *testing.T) {
	hygiene.Isolate(t)
	base := t.TempDir()
	src := filepath.Join(base, "build")
	dest := filepath.Join(base, "site")
	write(t, filepath.Join(src, "index.html"), "fresh front")
	write(t, filepath.Join(src, "blog", "hello", "index.html"), "fresh post")
	write(t, filepath.Join(dest, "alpha", "gone", "index.html"), "stale")

	produced := SplitBuildOutput([]string{"index.html", "blog/hello/index.html"}, "alpha", false)
	err := GraftSubtree(dest, src, produced, []string{"alpha/gone/index.html"}, handle())
	if err != nil {
		t.Fatalf("graft: %v", err)
	}
	if !exists(filepath.Join(dest, "alpha", "index.html")) {
		t.Error("the documentation page did not land under the project's slug")
	}
	if !exists(filepath.Join(dest, "blog", "hello", "index.html")) {
		t.Error("the post did not land on the site-level blog")
	}
	if exists(filepath.Join(dest, "alpha", "gone", "index.html")) {
		t.Error("the removed page is still there")
	}
}

// removed is destination-relative, and a path that is not a file is not an
// error: the record may name something a previous deploy already took.
func TestGraftSubtreeIgnoresARemovedPathThatIsNotThere(t *testing.T) {
	hygiene.Isolate(t)
	base := t.TempDir()
	src := filepath.Join(base, "build")
	dest := filepath.Join(base, "site")
	write(t, filepath.Join(src, "index.html"), "front")
	err := GraftSubtree(
		dest, src, map[string]string{"index.html": "alpha/index.html"},
		[]string{"alpha/never-was.html"}, handle(),
	)
	if err != nil {
		t.Fatalf("graft: %v", err)
	}
}

// -- collecting a local build ------------------------------------------------

func TestCollectSiteFilesAddressesTheProjectsSubtree(t *testing.T) {
	hygiene.Isolate(t)
	output := filepath.Join(t.TempDir(), ".stricttools", "docs-cache", "build")
	write(t, filepath.Join(output, "index.html"), "<html>index</html>")
	write(t, filepath.Join(output, "guide", "index.html"), "<html>guide</html>")
	files, err := CollectSiteFiles(output, "alpha", false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []string{"site/alpha/guide/index.html", "site/alpha/index.html"}
	if got := sortedKeys(files); !reflect.DeepEqual(got, want) {
		t.Errorf("collected = %v, want %v", got, want)
	}
}

// A locally built post is site-level, as a deployed one is.
func TestCollectSiteFilesSendsAPostToTheSiteLevelBlog(t *testing.T) {
	hygiene.Isolate(t)
	output := filepath.Join(t.TempDir(), ".stricttools", "docs-cache", "build")
	write(t, filepath.Join(output, "index.html"), "<html>index</html>")
	write(t, filepath.Join(output, "blog", "hello", "index.html"), "<html>hello</html>")
	files, err := CollectSiteFiles(output, "alpha", false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []string{"site/alpha/index.html", "site/blog/hello/index.html"}
	if got := sortedKeys(files); !reflect.DeepEqual(got, want) {
		t.Errorf("collected = %v, want %v", got, want)
	}
}

// The site's blog index is the assembly's, listing every project's posts.
func TestCollectSiteFilesDropsTheProjectsOwnBlogListing(t *testing.T) {
	hygiene.Isolate(t)
	output := filepath.Join(t.TempDir(), ".stricttools", "docs-cache", "build")
	write(t, filepath.Join(output, "blog", "index.html"), "<html>listing</html>")
	write(t, filepath.Join(output, "blog", "hello", "index.html"), "<html>hello</html>")
	files, err := CollectSiteFiles(output, "alpha", false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []string{"site/blog/hello/index.html"}
	if got := sortedKeys(files); !reflect.DeepEqual(got, want) {
		t.Errorf("collected = %v, want %v", got, want)
	}
}

func TestCollectSiteFilesReadsContentAsBytes(t *testing.T) {
	hygiene.Isolate(t)
	output := filepath.Join(t.TempDir(), ".stricttools", "docs-cache", "build")
	write(t, filepath.Join(output, "index.html"), "<html>index</html>")
	// A PNG header: valid bytes that are not valid UTF-8, so a read that
	// decoded them as text would destroy the file.
	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe}
	if err := writeBytes(filepath.Join(output, "logo.png"), pngBytes); err != nil {
		t.Fatalf("write png: %v", err)
	}
	files, err := CollectSiteFiles(output, "alpha", false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !reflect.DeepEqual(files["site/alpha/logo.png"], pngBytes) {
		t.Errorf("collected bytes = %v, want %v", files["site/alpha/logo.png"], pngBytes)
	}
}

func TestCollectSiteFilesAppliesTheDeployArtifactExclusions(t *testing.T) {
	hygiene.Isolate(t)
	output := filepath.Join(t.TempDir(), ".stricttools", "docs-cache", "build")
	write(t, filepath.Join(output, "index.html"), "<html>index</html>")
	write(t, filepath.Join(output, "guide", "index.html"), "<html>guide</html>")
	write(t, filepath.Join(output, "_headers"), "/*\n")
	write(t, filepath.Join(output, "_redirects"), "/* /x 200\n")
	write(t, filepath.Join(output, "index.html.gz"), "z")
	files, err := CollectSiteFiles(output, "alpha", false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := []string{"site/alpha/guide/index.html", "site/alpha/index.html"}
	if got := sortedKeys(files); !reflect.DeepEqual(got, want) {
		t.Errorf("collected = %v, want %v", got, want)
	}
}

// -- what a project owns in the assembly -------------------------------------

func TestProjectPathsOwnsItsSubtreeAndEveryManifestKind(t *testing.T) {
	t.Parallel()
	paths := []string{
		"site/alpha/index.html",
		"site/alpha/guide/index.html",
		"site/beta/index.html",
		"site/blog/hello/index.html",
		"site/index.html",
		"manifests/alpha.json",
		"manifests/alpha-posts.json",
		"manifests/alpha-files.json",
		"manifests/alpha-revisions.json",
		"manifests/alphabet.json",
		"manifests/beta.json",
		RosterPath,
	}
	got := ProjectPaths(paths, "alpha", []string{"blog/hello/index.html"})
	want := []string{
		"manifests/alpha-files.json",
		"manifests/alpha-posts.json",
		"manifests/alpha-revisions.json",
		"manifests/alpha.json",
		"site/alpha/guide/index.html",
		"site/alpha/index.html",
		"site/blog/hello/index.html",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ProjectPaths = %v, want %v", got, want)
	}
}

// Without the claimed paths a retirement leaves the project's posts on the
// blog with nothing left to explain where they came from.
func TestProjectPathsWithoutClaimsLeavesThePostsBehind(t *testing.T) {
	t.Parallel()
	paths := []string{"site/alpha/index.html", "site/blog/hello/index.html"}
	got := ProjectPaths(paths, "alpha", nil)
	if !reflect.DeepEqual(got, []string{"site/alpha/index.html"}) {
		t.Errorf("ProjectPaths = %v", got)
	}
}

// -- the redirect a retired standalone site serves ---------------------------

func TestGenerateRedirectsFile(t *testing.T) {
	t.Parallel()
	tests := []struct{ slug, base, want string }{
		{"selfdoc", "https://docs.smmh.dev", "/* https://docs.smmh.dev/selfdoc/:splat 301\n"},
		{"selfdoc", "https://docs.smmh.dev/", "/* https://docs.smmh.dev/selfdoc/:splat 301\n"},
		{"selfdoc", "https://docs.smmh.dev///", "/* https://docs.smmh.dev/selfdoc/:splat 301\n"},
	}
	for _, test := range tests {
		if got := GenerateRedirectsFile(test.slug, test.base); got != test.want {
			t.Errorf("GenerateRedirectsFile(%q, %q) = %q, want %q",
				test.slug, test.base, got, test.want)
		}
	}
}
