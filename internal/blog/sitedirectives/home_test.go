package sitedirectives

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// homeProject is a home project whose front page carries both site-level
// directives and whose docs declare a curated listing.
func homeProject(t *testing.T) string {
	t.Helper()
	project := testproject.Make(t, map[string]any{
		"name":     "Home",
		"base_url": canonicalBase,
		"topology": map[string]any{"slug": "home"},
	})
	testproject.WriteText(t, filepath.Join(project, ".stricttools", "docs", "projects.toml"),
		"[[category]]\nname = \"Frameworks\"\n"+
			"[[category.project]]\nslug = \"alpha\"\n"+
			"blurb = \"Does the alpha thing.\"\n")
	testproject.WriteText(t, filepath.Join(project, ".stricttools", "docs", "index.md"),
		"+++\ntitle = \"Front page\"\n+++\n\n"+
			"# Me\n\nProse the author wrote.\n\n"+
			":-: projects-cards\n\n"+
			`:-: blog-highlights limit="3"`+"\n")
	return project
}

// assemblyManifests writes an assembly manifests directory holding one
// project's manifest and returns its path.
func assemblyManifests(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "manifests")
	testproject.MkdirAll(t, dir)
	encoded, err := json.Marshal(manifest(
		"alpha", "Alpha", "1.0.0", post("hello", "Hello", "2024-06-01"),
	))
	if err != nil {
		t.Fatalf("encoding the fixture manifest: %v", err)
	}
	write(t, filepath.Join(dir, "alpha.json"), string(encoded))
	return dir
}

func TestAHomeBuildWithoutTheManifestsRefuses(t *testing.T) {
	hygiene.Isolate(t)
	_, err := BuildHomeProject(homeProject(t), "", "", false, handle())
	if err == nil {
		t.Fatal("a home build with no manifests directory must be refused")
	}
	wants(t, err.Error(), "--site-manifests is required",
		"projects-cards, blog-highlights", "manifests/ directory")
}

func TestAHomeBuildWhoseManifestsAreNotADirectoryRefuses(t *testing.T) {
	hygiene.Isolate(t)
	project := homeProject(t)
	notADirectory := filepath.Join(project, "selfdoc.json")
	_, err := BuildHomeProject(project, notADirectory, "", false, handle())
	if err == nil {
		t.Fatal("a home build pointed at a file must be refused")
	}
	wants(t, err.Error(), "which is not a directory",
		"assembly checkout's manifests/ directory")
}

func TestAHomeBuildResolvesTheDirectivesIntoRegions(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	project := homeProject(t)

	written, err := BuildHomeProject(
		project, assemblyManifests(t), "", false, handle(),
	)
	if err != nil {
		t.Fatalf("the home build: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("the home build wrote nothing")
	}

	page := read(t, filepath.Join(project, ".stricttools", "docs-cache", "build", "index.html"))
	wants(t, page, "Prose the author wrote.")
	// The card's version badge is the alpha manifest's, which only the
	// assembly holds: it is the whole reason this build needs them.
	wants(t, regionBody(t, page, "projects-cards"),
		"Does the alpha thing.", "v1.0.0")
	wants(t, regionBody(t, page, "blog-highlights"), "Hello")
	if names := RegionNames(page); !reflect.DeepEqual(
		names, []string{"projects-cards", "blog-highlights"},
	) {
		t.Fatalf("the regions the page carries: %v", names)
	}
	for _, region := range FindRegions(page) {
		if region.Body == "" {
			t.Errorf("the %q region is empty", region.Name)
		}
	}
	if unclosed := FindUnclosedRegions(page); len(unclosed) != 0 {
		t.Fatalf("unclosed regions: %v", unclosed)
	}
}

func TestAHomeBuildsRegionsAreRefreshableFromTheOutput(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	project := homeProject(t)
	manifests := assemblyManifests(t)

	if _, err := BuildHomeProject(project, manifests, "", false, handle()); err != nil {
		t.Fatalf("the home build: %v", err)
	}

	// The deploy-time pass is the same code on the same output: a rebuild
	// of the regions from the same manifests changes nothing, which is what
	// makes a later deploy's refresh a no-op until a version moves.
	output := filepath.Join(project, ".stricttools", "docs-cache", "build")
	context := SiteContext{
		Manifests: []map[string]any{manifest(
			"alpha", "Alpha", "1.0.0", post("hello", "Hello", "2024-06-01"),
		)},
		Listing:  curated(t, "alpha"),
		HomeSlug: "home",
	}
	changed, err := RefreshOutputRegions(output, context, handle())
	if err != nil {
		t.Fatalf("RefreshOutputRegions: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("the deploy-time pass rewrote %v", changed)
	}

	// A project that released since the home project's build: the badge on
	// the front page moves without the home project rebuilding.
	context.Manifests = []map[string]any{manifest(
		"alpha", "Alpha", "2.0.0", post("hello", "Hello", "2024-06-01"),
	)}
	changed, err = RefreshOutputRegions(output, context, handle())
	if err != nil {
		t.Fatalf("RefreshOutputRegions after a release: %v", err)
	}
	if !reflect.DeepEqual(changed, []string{"index.html"}) {
		t.Fatalf("changed pages: %v", changed)
	}
	cards := regionBody(t, read(t, filepath.Join(output, "index.html")),
		"projects-cards")
	wants(t, cards, "v2.0.0")
	rejects(t, cards, "v1.0.0")
}

// regionBody is the body of the named region on a page, failing the test when
// the page carries no region for it.
func regionBody(t *testing.T, page, name string) string {
	t.Helper()
	for _, region := range FindRegions(page) {
		if region.Name == name {
			return region.Body
		}
	}
	t.Fatalf("the page carries no %q region", name)
	return ""
}

func TestAHomeProjectWithNoListingRefusesItsCards(t *testing.T) {
	hygiene.Isolate(t)
	project := homeProject(t)
	if err := removeFile(filepath.Join(project, ".stricttools", "docs", "projects.toml")); err != nil {
		t.Fatalf("removing the fixture listing: %v", err)
	}
	_, err := BuildHomeProject(project, assemblyManifests(t), "", false, handle())
	if err == nil {
		t.Fatal("a home project with no listing must refuse its cards")
	}
	wants(t, err.Error(), "docs/projects.toml", "This project declares none.")
}

func TestTheHomeListingPathFollowsTheDocsKey(t *testing.T) {
	cases := map[string]string{
		"":                   filepath.Join("/p", ".stricttools", "docs", "projects.toml"),
		".stricttools/docs/": filepath.Join("/p", ".stricttools", "docs", "projects.toml"),
		"documents":          filepath.Join("/p", "documents", "projects.toml"),
		"site/docs///":       filepath.Join("/p", "site", "docs", "projects.toml"),
	}
	for declared, want := range cases {
		cfg := map[string]any{}
		if declared != "" {
			cfg["docs"] = declared
		}
		if got := HomeListingPath("/p", cfg); got != want {
			t.Errorf("docs=%q gives %q, wanted %q", declared, got, want)
		}
	}
}
