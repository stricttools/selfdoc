package assembly

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/testisolation/go/hygiene"
)

// homeListingTOML is the home project's curated listing, as its author writes
// it.
const homeListingTOML = `[[category]]
name = "Frameworks"

  [[category.project]]
  slug = "alpha"
  blurb = "Does the alpha thing."
`

// homePage is a page shaped the way a home project's build shapes one: at the
// site root, so its addresses carry no project segment.
func homePage(title, address, body string) string {
	if body == "" {
		body = "<p>" + title + "</p>"
	}
	return "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n" +
		"  <title>" + title + "</title>\n" +
		`  <link rel="canonical" href="` + sharedCanonicalBase + "/" + address + `">` + "\n" +
		`  <link rel="stylesheet" href="style.css">` + "\n" +
		"</head>\n<body>\n" + body + "\n</body>\n</html>\n"
}

// -- the graft, for the one project that is the site root --------------------

// homeSource lays out a home project's checkout with a build output tree.
func homeSource(t *testing.T, pages map[string]string, listingTOML string) string {
	t.Helper()
	source := t.TempDir()
	for rel, content := range pages {
		path := filepath.Join(source, "stricttools", ".docs-cache", "build",
			filepath.Join(strings.Split(rel, "/")...))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	if listingTOML != "" {
		path := filepath.Join(source, filepath.Join(strings.Split(listing.SourceFile, "/")...))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(listingTOML), 0o644); err != nil {
			t.Fatalf("writing the listing: %v", err)
		}
	}
	manifest, err := json.Marshal(integrateManifest("home", "Home", "0.1.0", nil))
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(source, "stricttools", ".docs-state", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, manifest, 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return source
}

func TestTheGraftRefusesACollidingHomeBuild(t *testing.T) {
	// The home project emits at the site root, where the assembly's own
	// generated pages are. A page called projects.md builds to
	// projects/index.html, which is the generated listing's address.
	hygiene.Isolate(t)
	source := homeSource(t, map[string]string{
		"index.html":          homePage("Front", "", ""),
		"projects/index.html": homePage("Projects", "projects/", ""),
	}, "")
	assembly := filepath.Join(t.TempDir(), "assembly")
	_, err := ApplyProjectFiles(GraftOptions{
		AssemblyDir: assembly, SourceDir: source, Slug: "home",
		Scope: "full", Home: true,
	}, effects.Unbound())
	if err == nil {
		t.Fatal("a colliding home build was grafted")
	}
	if !strings.Contains(err.Error(), "addresses the assembly owns") {
		t.Fatalf("err = %q, want the collision refusal", err)
	}
}

func TestTheGraftCopiesTheCuratedListingIn(t *testing.T) {
	hygiene.Isolate(t)
	source := homeSource(t, map[string]string{
		"index.html": homePage("Front", "", ""),
	}, homeListingTOML)
	assembly := filepath.Join(t.TempDir(), "assembly")
	if _, err := ApplyProjectFiles(GraftOptions{
		AssemblyDir: assembly, SourceDir: source, Slug: "home",
		Scope: "full", Home: true,
	}, effects.Unbound()); err != nil {
		t.Fatalf("graft: %v", err)
	}

	sidecar := site.ListingSidecarPath(filepath.Join(assembly, "manifests"), "home")
	curated, err := listing.LoadSidecar(sidecar)
	if err != nil {
		t.Fatalf("reading the sidecar: %v", err)
	}
	if len(curated.Categories) == 0 || curated.Categories[0].Name != "Frameworks" {
		t.Fatalf("the sidecar carries %+v", curated.Categories)
	}
	if _, err := os.Stat(filepath.Join(assembly, "site", "index.html")); err != nil {
		t.Fatalf("the front page did not land at the site root: %v", err)
	}

	// The claims are recorded at the addresses the pages actually landed at,
	// which for the home project means no slug prefix.
	owners, err := site.LoadFilesManifest(
		site.FilesManifestPath(filepath.Join(assembly, "manifests"), "home"))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if !reflect.DeepEqual(owners["release"], []string{"index.html"}) {
		t.Fatalf("the record claims %v", owners["release"])
	}
}

func TestAHomeProjectThatDeclaresNoListingLeavesNoSidecar(t *testing.T) {
	hygiene.Isolate(t)
	source := homeSource(t, map[string]string{
		"index.html": homePage("Front", "", ""),
	}, "")
	assembly := filepath.Join(t.TempDir(), "assembly")
	if _, err := ApplyProjectFiles(GraftOptions{
		AssemblyDir: assembly, SourceDir: source, Slug: "home",
		Scope: "full", Home: true,
	}, effects.Unbound()); err != nil {
		t.Fatalf("graft: %v", err)
	}
	sidecar := site.ListingSidecarPath(filepath.Join(assembly, "manifests"), "home")
	if _, err := os.Stat(sidecar); err == nil {
		t.Fatal("a home project with no listing left a sidecar")
	}
}

func TestAMalformedListingIsARefusalAtTheGraft(t *testing.T) {
	// Raised here rather than at the far end, where the document is no longer
	// in reach.
	hygiene.Isolate(t)
	source := homeSource(t, map[string]string{
		"index.html": homePage("Front", "", ""),
	}, "[[category]]\nname = \"Frameworks\"\nnonsense = 1\n")
	assembly := filepath.Join(t.TempDir(), "assembly")
	_, err := ApplyProjectFiles(GraftOptions{
		AssemblyDir: assembly, SourceDir: source, Slug: "home",
		Scope: "full", Home: true,
	}, effects.Unbound())
	if err == nil {
		t.Fatal("a malformed listing was grafted")
	}
}

// -- the front page's regions on somebody else's deploy ----------------------

// homeAssembly is an assembly whose home project's front page carries a
// resolved region, rendered against a stale manifest.
func homeAssembly(t *testing.T) *sharedTree {
	t.Helper()
	tree := newSharedTree(t)
	tree.Manifest(sharedManifest("alpha", "Alpha", "Does the alpha thing.", nil, nil, ""))
	tree.Manifest(sharedManifest("home", "Home", "The front page.", []any{
		map[string]any{"path": "index.md", "title": "Front page"},
		map[string]any{"path": "cv.md", "title": "CV"},
	}, nil, ""))
	tree.Listing("home", []any{
		map[string]any{"slug": "alpha", "blurb": "Does the alpha thing."},
	})
	tree.WriteJSON(site.FilesManifestPath(tree.Manifs, "home"), map[string]any{
		"schema_version": site.FilesRecordVersion,
		"slug":           "home",
		"owners":         map[string]any{"release": []any{"index.html", "cv/index.html"}},
	})

	tree.Page("alpha/index.html", homePage("Alpha", "alpha/", ""))
	tree.Page("cv/index.html", homePage("CV", "cv/", ""))

	// The front page as the home project's own build left it: authored prose
	// plus a resolved region, rendered when alpha was at 0.9.0.
	stale, err := sitedirectives.RenderRegion("projects-cards", nil, sitedirectives.SiteContext{
		Manifests: []map[string]any{
			sharedManifest("alpha", "Alpha", "Does the alpha thing.", nil, nil, ""),
		},
		SiteHop: "",
		Listing: &listing.Listing{Categories: []listing.Category{{
			Name:     "Frameworks",
			Projects: []listing.Project{{Slug: "alpha", Blurb: "Does the alpha thing."}},
		}}},
		HomeSlug: "home",
	})
	if err != nil {
		t.Fatalf("rendering the stale region: %v", err)
	}
	tree.Page("index.html", homePage("Front page", "",
		"<p>Prose the author wrote.</p>\n"+stale))
	return tree
}

func TestADeployOfAnotherProjectRefreshesTheFrontPage(t *testing.T) {
	// The card's version badge is as current as alpha's last deploy, not as
	// the home project's.
	tree := homeAssembly(t)
	before := tree.Read("index.html")
	if !strings.Contains(before, "1.0.0") {
		t.Fatalf("the fixture's region carries no version at all:\n%s", before)
	}

	// alpha releases: its manifest now names a newer version.
	tree.Manifest(sharedManifest("alpha", "Alpha", "Does the alpha thing.", nil, nil, ""))
	updated := sharedManifest("alpha", "Alpha", "Does the alpha thing.", nil, nil, "")
	updated["version"] = "2.4.0"
	tree.Manifest(updated)

	tree.Generate("home")

	after := tree.Read("index.html")
	if !strings.Contains(after, "2.4.0") {
		t.Errorf("the refreshed region does not carry the new version:\n%s", after)
	}
	if strings.Contains(after, "1.0.0") {
		t.Errorf("the refreshed region still carries the old version:\n%s", after)
	}
	if !strings.Contains(after, "Prose the author wrote.") {
		t.Error("the refresh destroyed the authored prose")
	}
}

func TestTheRefreshIsIdempotent(t *testing.T) {
	tree := homeAssembly(t)
	tree.Generate("home")
	once := tree.Read("index.html")
	tree.Generate("home")
	if tree.Read("index.html") != once {
		t.Fatal("a second deploy rewrote the front page differently")
	}
}

func TestTheHomePagesSitBesideTheGeneratedPages(t *testing.T) {
	tree := homeAssembly(t)
	tree.Generate("home")
	for _, rel := range []string{
		"index.html", "cv/index.html", "projects/index.html", "blog/index.html",
	} {
		if _, err := os.Stat(filepath.Join(tree.Site,
			filepath.Join(strings.Split(rel, "/")...))); err != nil {
			t.Errorf("%s is not there: %v", rel, err)
		}
	}
}

func TestTheSitemapAddressesHomePagesFromTheSiteRoot(t *testing.T) {
	tree := homeAssembly(t)
	tree.Generate("home")
	sitemap := tree.Read("sitemap.xml")
	if !strings.Contains(sitemap, "<loc>"+sharedCanonicalBase+"/cv/</loc>") {
		t.Errorf("the home project's page is not addressed from the site root:\n%s", sitemap)
	}
	if strings.Contains(sitemap, sharedCanonicalBase+"/home/") {
		t.Error("the home project's pages are addressed under its slug")
	}
}

func TestNavLeavesTheHomeProjectOut(t *testing.T) {
	tree := homeAssembly(t)
	tree.Generate("home")
	var nav map[string]any
	if err := json.Unmarshal([]byte(tree.Read("nav.json")), &nav); err != nil {
		t.Fatalf("nav.json is not JSON: %v", err)
	}
	projects, _ := nav["projects"].([]any)
	var slugs []string
	for _, entry := range projects {
		record, _ := entry.(map[string]any)
		slug, _ := record["slug"].(string)
		slugs = append(slugs, slug)
	}
	if !reflect.DeepEqual(slugs, []string{"alpha"}) {
		t.Fatalf("nav lists %v", slugs)
	}
}
