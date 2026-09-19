package assembly

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/stricttest/go/hygiene"
)

// siblingEntry is one line of the block a test expects to read.
type siblingEntry struct {
	slug, name, description string
}

// siblingsSection is the markup a page's block carries, stated here rather
// than rendered, so the pass and the assertions do not agree by construction.
func siblingsSection(siteHop string, projects ...siblingEntry) string {
	lines := []string{
		build.SiblingsBlockStart,
		`<h2 id="sibling-projects-heading">` + build.SiblingsHeading + "</h2>",
		"<ul>",
	}
	for _, project := range projects {
		line := `<li><a href="` + siteHop + project.slug + `/">` + project.name + "</a>"
		if project.description != "" {
			line += " <span>" + project.description + "</span>"
		}
		lines = append(lines, line+"</li>")
	}
	return strings.Join(append(lines, "</ul>", build.SiblingsBlockEnd), "\n")
}

// The projects the fixture's assembly publishes, as the pass reads them off
// the manifests, and the one it retired.
var (
	siblingsAlpha = siblingEntry{"alpha", "Alpha", "Does the alpha thing."}
	siblingsBeta  = siblingEntry{"beta", "Beta", "Does the beta thing."}
	siblingsGamma = siblingEntry{"gamma", "Gamma", "Did the gamma thing."}
)

// siblingsPage is a page shaped the way a built page is: an article, then the
// block when the page carries one, then the footer the block is written in
// front of.
func siblingsPage(marker, block string) string {
	body := "<!DOCTYPE html>\n<html lang=\"en\">\n<body>\n" +
		"<article>\n<p>" + marker + "</p>\n</article>\n"
	if block != "" {
		body += block + "\n"
	}
	return body + "<footer class=\"site-footer\">\n<p>footer</p>\n</footer>\n" +
		"</body>\n</html>\n"
}

// siblingsManifests is the membership the pass reads: a home project and two
// others. The retired project has no manifest, which is what retirement
// leaves behind -- its pages and its manifest are gone, and only the blocks
// on other projects' pages still name it.
func siblingsManifests() []map[string]any {
	return []map[string]any{
		{"slug": "home", "name": "Home", "description": "The front page."},
		{"slug": "alpha", "name": "Alpha", "description": "Does the alpha thing."},
		{"slug": "beta", "name": "Beta", "description": "Does the beta thing."},
	}
}

// siblingsFixture writes the tree and the published-file records behind it,
// and returns the site directory and the manifests directory.
//
// pages are site-relative paths to page content.
func siblingsFixture(t *testing.T, pages map[string]string) (string, string) {
	t.Helper()
	hygiene.Isolate(t)
	root := t.TempDir()
	siteDir := filepath.Join(root, "site")
	manifestsDir := filepath.Join(root, "manifests")
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatalf("making the manifests directory: %v", err)
	}
	for rel, content := range pages {
		path := filepath.Join(siteDir, filepath.Join(strings.Split(rel, "/")...))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	for slug, owned := range map[string][]string{
		"home":  {"index.html"},
		"alpha": {"alpha/index.html", "alpha/guide/index.html", "blog/hello/index.html"},
		"beta":  {"beta/index.html", "beta/legacy/index.html"},
	} {
		record := filesRecord(t, slug, map[string][]string{"release": owned})
		path := filepath.Join(manifestsDir, slug+"-files.json")
		if err := os.WriteFile(path, record, 0o644); err != nil {
			t.Fatalf("writing %s's record: %v", slug, err)
		}
	}
	return siteDir, manifestsDir
}

// siblingsStaleTree is the tree a retirement leaves behind: every page still
// carries the block its own last deploy rendered, and every one of those names
// the retired project.
func siblingsStaleTree() map[string]string {
	return map[string]string{
		// The home project's front page, at the site root.
		"index.html": siblingsPage("home",
			siblingsSection("", siblingsAlpha, siblingsBeta, siblingsGamma)),
		"alpha/index.html": siblingsPage("alpha",
			siblingsSection("../", siblingsBeta, siblingsGamma)),
		"alpha/guide/index.html": siblingsPage("alpha guide",
			siblingsSection("../../", siblingsBeta, siblingsGamma)),
		"beta/index.html": siblingsPage("beta",
			siblingsSection("../", siblingsAlpha, siblingsGamma)),
		// A post: site-level, published by alpha, so it lists everything but
		// alpha.
		"blog/hello/index.html": siblingsPage("a post",
			siblingsSection("../../", siblingsBeta, siblingsGamma)),
		// A page from a build that predates the block.
		"beta/legacy/index.html": siblingsPage("beta legacy", ""),
		// The site's own generated page, which carries no block.
		"projects/index.html": siblingsPage("the project listing", ""),
	}
}

// siblingsPages is the fixture's pages, site-relative and sorted, as the pass
// takes them.
func siblingsPages(pages map[string]string) []string {
	rels := make([]string, 0, len(pages))
	for rel := range pages {
		rels = append(rels, rel)
	}
	slices.Sort(rels)
	return rels
}

// siblingsRead is the text of a site-relative page.
func siblingsRead(t *testing.T, siteDir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(siteDir, filepath.Join(strings.Split(rel, "/")...)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

// refreshSiblings runs the pass over the fixture's whole tree.
func refreshSiblings(t *testing.T, siteDir, manifestsDir string, pages []string) []string {
	t.Helper()
	changed, err := RefreshSiblingBlocks(
		siteDir, manifestsDir, siblingsManifests(), "home", pages, effects.Unbound(),
	)
	if err != nil {
		t.Fatalf("refreshing the sibling blocks: %v", err)
	}
	return changed
}

func TestRefreshSiblingBlocksDropsTheRetiredProject(t *testing.T) {
	// The live failure: a retirement removes one subtree, every other
	// project's pages keep linking it, and the verification that reads the
	// whole tree refuses every following deploy.
	tree := siblingsStaleTree()
	siteDir, manifestsDir := siblingsFixture(t, tree)
	refreshSiblings(t, siteDir, manifestsDir, siblingsPages(tree))
	for rel := range tree {
		page := siblingsRead(t, siteDir, rel)
		if strings.Contains(page, siblingsGamma.slug+"/") {
			t.Errorf("%s still links the retired project:\n%s", rel, page)
		}
		if strings.Contains(page, siblingsGamma.name) {
			t.Errorf("%s still names the retired project", rel)
		}
	}
}

func TestRefreshSiblingBlocksListsTheCurrentMembershipOnEveryPage(t *testing.T) {
	tree := siblingsStaleTree()
	siteDir, manifestsDir := siblingsFixture(t, tree)
	refreshSiblings(t, siteDir, manifestsDir, siblingsPages(tree))
	for _, test := range []struct {
		rel  string
		want string
	}{
		// The home project's front page lists every other project: it is both
		// the home slug and the project the page belongs to.
		{"index.html", siblingsSection("", siblingsAlpha, siblingsBeta)},
		{"alpha/index.html", siblingsSection("../", siblingsBeta)},
		{"alpha/guide/index.html", siblingsSection("../../", siblingsBeta)},
		{"beta/index.html", siblingsSection("../", siblingsAlpha)},
		// The post is alpha's, so it lists beta and not alpha.
		{"blog/hello/index.html", siblingsSection("../../", siblingsBeta)},
	} {
		if page := siblingsRead(t, siteDir, test.rel); !strings.Contains(page, test.want) {
			t.Errorf("%s does not carry\n%s\nit carries\n%s", test.rel, test.want, page)
		}
	}
}

func TestRefreshSiblingBlocksGivesABlocklessPageOne(t *testing.T) {
	// A page published before the block existed converges too, or the
	// verification keeps reading a tree half of which was never swept.
	tree := siblingsStaleTree()
	siteDir, manifestsDir := siblingsFixture(t, tree)
	changed := refreshSiblings(t, siteDir, manifestsDir, siblingsPages(tree))
	if !slices.Contains(changed, "beta/legacy/index.html") {
		t.Fatalf("the blockless page was not reported as changed: %v", changed)
	}
	page := siblingsRead(t, siteDir, "beta/legacy/index.html")
	want := siblingsSection("../../", siblingsAlpha)
	if !strings.Contains(page, want) {
		t.Fatalf("the blockless page did not gain the block:\n%s", page)
	}
	block := strings.Index(page, build.SiblingsBlockStart)
	footer := strings.Index(page, `<footer class="site-footer">`)
	article := strings.Index(page, "</article>")
	if block < article || block > footer {
		t.Errorf("the block was written at the wrong place: article=%d block=%d footer=%d",
			article, block, footer)
	}
}

func TestRefreshSiblingBlocksLeavesTheSitesOwnPagesBlockless(t *testing.T) {
	// The generated listing, the blog index and the 404 page are written
	// fresh by every deploy and carry no sibling block. Giving one to a page
	// whose whole job is to list the projects would be this pass inventing a
	// page element.
	tree := siblingsStaleTree()
	siteDir, manifestsDir := siblingsFixture(t, tree)
	changed := refreshSiblings(t, siteDir, manifestsDir, siblingsPages(tree))
	if slices.Contains(changed, "projects/index.html") {
		t.Errorf("the site's own page was rewritten: %v", changed)
	}
	if got := siblingsRead(t, siteDir, "projects/index.html"); got != tree["projects/index.html"] {
		t.Errorf("the site's own page reads\n%s\nwant\n%s", got, tree["projects/index.html"])
	}
}

func TestRefreshSiblingBlocksLeavesACurrentPageAlone(t *testing.T) {
	// The pass runs over the whole tree on every deploy, so a page that
	// already carries what it should must not be rewritten: a write with the
	// same bytes is a file the deploy claims to have changed and a commit
	// entry nobody can explain.
	tree := siblingsStaleTree()
	tree["beta/index.html"] = siblingsPage("beta", siblingsSection("../", siblingsAlpha))
	siteDir, manifestsDir := siblingsFixture(t, tree)
	pages := siblingsPages(tree)
	path := filepath.Join(siteDir, "beta", "index.html")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	changed := refreshSiblings(t, siteDir, manifestsDir, pages)
	if slices.Contains(changed, "beta/index.html") {
		t.Errorf("a current page was reported as changed: %v", changed)
	}
	if got := siblingsRead(t, siteDir, "beta/index.html"); got != tree["beta/index.html"] {
		t.Errorf("a current page's content moved:\n%s", got)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("a current page was rewritten with the content it already had")
	}
}

func TestRefreshSiblingBlocksChangesNothingOnASecondRun(t *testing.T) {
	tree := siblingsStaleTree()
	siteDir, manifestsDir := siblingsFixture(t, tree)
	pages := siblingsPages(tree)
	refreshSiblings(t, siteDir, manifestsDir, pages)
	first := make(map[string]string, len(pages))
	for _, rel := range pages {
		first[rel] = siblingsRead(t, siteDir, rel)
	}

	changed := refreshSiblings(t, siteDir, manifestsDir, pages)
	if len(changed) != 0 {
		t.Errorf("the second run reports %v, want nothing", changed)
	}
	for _, rel := range pages {
		if got := siblingsRead(t, siteDir, rel); got != first[rel] {
			t.Errorf("%s moved on the second run:\n%s\nwas\n%s", rel, got, first[rel])
		}
	}
}

func TestRefreshSiblingBlocksReportsEveryPageItChanged(t *testing.T) {
	tree := siblingsStaleTree()
	siteDir, manifestsDir := siblingsFixture(t, tree)
	changed := refreshSiblings(t, siteDir, manifestsDir, siblingsPages(tree))
	want := []string{
		"alpha/guide/index.html", "alpha/index.html", "beta/index.html",
		"beta/legacy/index.html", "blog/hello/index.html", "index.html",
	}
	if !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
}

func TestRefreshSiblingBlocksRemovesTheBlockWhenNothingIsLeftToList(t *testing.T) {
	// A site down to its home project has no other tool to point at, and a
	// heading over an empty list is worse than no heading.
	tree := map[string]string{
		"index.html": siblingsPage("home",
			siblingsSection("", siblingsAlpha, siblingsGamma)),
	}
	siteDir, manifestsDir := siblingsFixture(t, tree)
	changed, err := RefreshSiblingBlocks(
		siteDir, manifestsDir,
		[]map[string]any{{"slug": "home", "name": "Home", "description": "The front page."}},
		"home", siblingsPages(tree), effects.Unbound(),
	)
	if err != nil {
		t.Fatalf("refreshing: %v", err)
	}
	if !reflect.DeepEqual(changed, []string{"index.html"}) {
		t.Fatalf("changed = %v, want the front page", changed)
	}
	if got := siblingsRead(t, siteDir, "index.html"); got != siblingsPage("home", "") {
		t.Fatalf("the block was not removed:\n%s", got)
	}
}
