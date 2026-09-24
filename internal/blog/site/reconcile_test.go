package site

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// withoutBeta is the roster the assembly gets once beta leaves it.
func withoutBeta() *Roster {
	return NewRoster(map[string]RosterEntry{
		"alpha": fixtureRoster["alpha"],
		"home":  fixtureRoster["home"],
	}, homeSlug)
}

func TestAnUndeclaredProjectLosesItsSubtree(t *testing.T) {
	root := assemblyTree(t)
	if _, err := ReconcileMembership(root, withoutBeta(), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if exists(filepath.Join(root, "site", "beta")) {
		t.Error("beta's subtree is still there")
	}
	if !exists(filepath.Join(root, "site", "alpha")) {
		t.Error("alpha's subtree went with it")
	}
}

func TestAnUndeclaredProjectLosesEveryManifestKind(t *testing.T) {
	root := assemblyTree(t)
	manifests := filepath.Join(root, "manifests")
	writeJSON(t, filepath.Join(manifests, "beta-posts.json"),
		manifestDoc("beta", "Beta", "2.0.0", nil))
	write(t, filepath.Join(manifests, "beta-revisions.json"), "{}")
	writeJSON(t, filepath.Join(manifests, "beta-files.json"), map[string]any{
		"schema_version": FilesRecordVersion, "slug": "beta", "owners": map[string]any{},
	})
	if _, err := ReconcileMembership(root, withoutBeta(), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	left := dirNames(t, manifests)
	for _, name := range left {
		if strings.HasPrefix(name, "beta") {
			t.Errorf("manifests still carry %s", name)
		}
	}
	if !containsString(left, "alpha.json") {
		t.Errorf("manifests = %v, want alpha.json kept", left)
	}
}

func TestAnUndeclaredProjectLosesItsMembershipRecord(t *testing.T) {
	root := assemblyTree(t)
	if _, err := ReconcileMembership(root, withoutBeta(), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	record := readJSON(t, filepath.Join(root, ProjectsPath))
	slugs := make([]string, 0, len(record))
	for slug := range record {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	if !reflect.DeepEqual(slugs, []string{"alpha", "home"}) {
		t.Errorf("membership = %v, want [alpha home]", slugs)
	}
}

func TestReconciliationReportsWhatItRetired(t *testing.T) {
	root := assemblyTree(t)
	summary, err := ReconcileMembership(root, withoutBeta(), handle())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !reflect.DeepEqual(summary.Retired, []string{"beta"}) {
		t.Errorf("retired = %v, want [beta]", summary.Retired)
	}
	named := false
	for _, path := range summary.Removed {
		if strings.Contains(path, "site") && strings.HasSuffix(path, "beta") {
			named = true
		}
	}
	if !named {
		t.Errorf("removed = %v, want beta's subtree named", summary.Removed)
	}
}

// A retired project's posts are site-level, so the record is what says which
// of them were its, and retirement takes them along.
func TestARetiredProjectLosesItsPosts(t *testing.T) {
	root := assemblyTree(t)
	write(t, filepath.Join(root, "site", "blog", "beta-post", "index.html"),
		page("Beta Post", "blog/beta-post/", "beta post"))
	writeJSON(t, filepath.Join(root, "manifests", "beta-files.json"),
		filesRecord("beta", "posts", "blog/beta-post/index.html"))
	summary, err := ReconcileMembership(root, withoutBeta(), handle())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if exists(filepath.Join(root, "site", "blog", "beta-post", "index.html")) {
		t.Error("the retired project's post is still on the blog")
	}
	if exists(filepath.Join(root, "site", "blog", "beta-post")) {
		t.Error("the emptied post directory is still there")
	}
	if !exists(filepath.Join(root, "site", "blog", "old-post", "index.html")) {
		t.Error("another project's post went with it")
	}
	_ = summary
}

// pagefind keys fragments by hash, so a removed page can linger in it.
func TestReconciliationDropsTheStaleSearchIndex(t *testing.T) {
	root := assemblyTree(t)
	write(t, filepath.Join(root, "site", "pagefind", "pagefind.js"), "// index")
	if _, err := ReconcileMembership(root, withoutBeta(), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if exists(filepath.Join(root, "site", "pagefind")) {
		t.Error("the stale index is still there")
	}
}

func TestReconciliationKeepsTheIndexWhenNothingWasRetired(t *testing.T) {
	root := assemblyTree(t)
	write(t, filepath.Join(root, "site", "pagefind", "pagefind.js"), "// index")
	if _, err := ReconcileMembership(root, NewRoster(fixtureRoster, homeSlug), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !exists(filepath.Join(root, "site", "pagefind")) {
		t.Error("the index was rebuilt for nothing")
	}
}

// The site root IS the home project's content root, so a page of its at
// cv/index.html emits at site/cv/, one level up from where every other
// project's pages sit. A sweep that reads every directory under site/ as a
// project subtree finds cv, declares it undeclared, and deletes the home
// project's own page. Its published-file record is what tells the two apart.
func TestReconciliationNeverMistakesAHomeDirectoryForAProject(t *testing.T) {
	root := assemblyTree(t)
	write(t, filepath.Join(root, "site", "cv", "index.html"), page("CV", "cv/", "home cv"))
	writeJSON(t, filepath.Join(root, "manifests", "home-files.json"),
		filesRecord("home", "release", "index.html", "cv/index.html"))
	summary, err := ReconcileMembership(root, NewRoster(fixtureRoster, homeSlug), handle())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !exists(filepath.Join(root, "site", "cv", "index.html")) {
		t.Error("the home project's own page was swept")
	}
	if containsString(summary.Retired, "cv") {
		t.Errorf("retired = %v, which reads a home directory as a project", summary.Retired)
	}
}

// Protecting the home project's directories protects nothing else.
func TestReconciliationStillRetiresAProjectBesideAHomeDirectory(t *testing.T) {
	root := assemblyTree(t)
	write(t, filepath.Join(root, "site", "cv", "index.html"), page("CV", "cv/", "home cv"))
	writeJSON(t, filepath.Join(root, "manifests", "home-files.json"),
		filesRecord("home", "release", "index.html", "cv/index.html"))
	summary, err := ReconcileMembership(root, withoutBeta(), handle())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !reflect.DeepEqual(summary.Retired, []string{"beta"}) {
		t.Errorf("retired = %v, want [beta]", summary.Retired)
	}
	if exists(filepath.Join(root, "site", "beta")) {
		t.Error("beta's subtree is still there")
	}
	if !exists(filepath.Join(root, "site", "cv", "index.html")) {
		t.Error("the home project's own page was swept")
	}
}

func TestReconciliationNeverMistakesASharedDirectoryForAProject(t *testing.T) {
	root := assemblyTree(t)
	write(t, filepath.Join(root, "site", "projects", "index.html"), "<html>list</html>")
	write(t, filepath.Join(root, "site", ChromeDir, "style.css"), "body{}")
	if _, err := ReconcileMembership(root, NewRoster(fixtureRoster, homeSlug), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "site", "blog", "old-post", "index.html"),
		filepath.Join(root, "site", "projects", "index.html"),
		filepath.Join(root, "site", ChromeDir, "style.css"),
	} {
		if !exists(path) {
			t.Errorf("%s was read as a project subtree and swept", path)
		}
	}
}

// A manifest whose stem matches no declared project at all is stale even when
// no subtree or record named it.
func TestReconciliationRemovesAManifestNoDeclaredProjectOwns(t *testing.T) {
	root := assemblyTree(t)
	writeJSON(t, filepath.Join(root, "manifests", "ghost.json"),
		manifestDoc("ghost", "Ghost", "1.0.0", nil))
	writeJSON(t, filepath.Join(root, "manifests", "ghost-posts.json"),
		manifestDoc("ghost", "Ghost", "1.0.0", nil))
	if _, err := ReconcileMembership(root, NewRoster(fixtureRoster, homeSlug), handle()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, name := range []string{"ghost.json", "ghost-posts.json"} {
		if exists(filepath.Join(root, "manifests", name)) {
			t.Errorf("%s is still there", name)
		}
	}
}

func TestReconciliationIsANoOpWhenEverythingIsDeclared(t *testing.T) {
	root := assemblyTree(t)
	before := dirNames(t, filepath.Join(root, "manifests"))
	sort.Strings(before)
	summary, err := ReconcileMembership(root, NewRoster(fixtureRoster, homeSlug), handle())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(summary.Retired) != 0 {
		t.Errorf("retired = %v, want empty", summary.Retired)
	}
	if len(summary.Removed) != 0 {
		t.Errorf("removed = %v, want empty", summary.Removed)
	}
	after := dirNames(t, filepath.Join(root, "manifests"))
	sort.Strings(after)
	if !reflect.DeepEqual(after, before) {
		t.Errorf("manifests = %v, want %v", after, before)
	}
}

// A record naming only a project's own subtree claims nothing outside it.
func TestClaimedSitePathsNamesOnlyWhatIsOutsideTheSubtree(t *testing.T) {
	root := assemblyTree(t)
	manifests := filepath.Join(root, "manifests")
	writeJSON(t, filepath.Join(manifests, "alpha-files.json"),
		filesRecord("alpha", "release",
			"alpha/index.html", "blog/hello/index.html", "blog/second/index.html"))
	claimed, err := ClaimedSitePaths(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []string{"blog/hello/index.html", "blog/second/index.html"}
	if !reflect.DeepEqual(claimed, want) {
		t.Errorf("claimed = %v, want %v", claimed, want)
	}
}

func TestClaimedSitePathsOfAProjectWithNoRecordIsEmpty(t *testing.T) {
	hygiene.Isolate(t)
	claimed, err := ClaimedSitePaths(t.TempDir(), "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(claimed) != 0 {
		t.Errorf("claimed = %v, want empty", claimed)
	}
}

// -- the cross-project post refusal ------------------------------------------

func TestForeignPostClaimsNamesTheClaimant(t *testing.T) {
	hygiene.Isolate(t)
	manifests := filepath.Join(t.TempDir(), "manifests")
	writeJSON(t, filepath.Join(manifests, "beta-files.json"),
		filesRecord("beta", "posts", "blog/shared/index.html"))
	claims, err := ForeignPostClaims(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := map[string]string{"blog/shared/index.html": "beta"}
	if !reflect.DeepEqual(claims, want) {
		t.Errorf("claims = %v, want %v", claims, want)
	}
}

func TestForeignPostClaimsIgnoresThePublishingProject(t *testing.T) {
	hygiene.Isolate(t)
	manifests := filepath.Join(t.TempDir(), "manifests")
	writeJSON(t, filepath.Join(manifests, "alpha-files.json"),
		filesRecord("alpha", "posts", "blog/mine/index.html"))
	claims, err := ForeignPostClaims(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("claims = %v, want empty", claims)
	}
}

// Documentation paths are inside a project's own subtree, so no other project
// can address them and there is nothing to refuse.
func TestForeignPostClaimsIgnoresDocumentationPaths(t *testing.T) {
	hygiene.Isolate(t)
	manifests := filepath.Join(t.TempDir(), "manifests")
	writeJSON(t, filepath.Join(manifests, "beta-files.json"),
		filesRecord("beta", "release", "beta/index.html"))
	claims, err := ForeignPostClaims(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("claims = %v, want empty", claims)
	}
}

func TestAProjectWithNoRecordYetClaimsNothing(t *testing.T) {
	hygiene.Isolate(t)
	claims, err := ForeignPostClaims(filepath.Join(t.TempDir(), "manifests"), "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("claims = %v, want empty", claims)
	}
}

// The earliest record read wins the claim, so the refusal names one claimant
// per path rather than whichever was read last.
func TestForeignPostClaimsKeepsTheFirstClaimant(t *testing.T) {
	hygiene.Isolate(t)
	manifests := filepath.Join(t.TempDir(), "manifests")
	writeJSON(t, filepath.Join(manifests, "beta-files.json"),
		filesRecord("beta", "posts", "blog/shared/index.html"))
	writeJSON(t, filepath.Join(manifests, "gamma-files.json"),
		filesRecord("gamma", "posts", "blog/shared/index.html"))
	claims, err := ForeignPostClaims(manifests, "alpha")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if claims["blog/shared/index.html"] != "beta" {
		t.Errorf("claims = %v, want beta named", claims)
	}
}

func TestRefuseForeignPostOverwrite(t *testing.T) {
	t.Parallel()
	err := RefuseForeignPostOverwrite(
		"alpha", []string{"alpha/index.html", "blog/x/index.html"},
		map[string]string{"blog/x/index.html": "beta"},
	)
	want := "'alpha' would overwrite 1 post file(s) another project published: " +
		"site/blog/x/index.html is claimed by 'beta'. Posts are emitted at " +
		"'blog/<post-slug>/' with no project segment, so a post slug is unique " +
		"across the whole site. Rename the post's slug in the project that " +
		"claims it later."
	if err == nil || err.Error() != want {
		t.Fatalf("refusal:\n%v\nwant:\n%s", err, want)
	}
}

func TestRefuseForeignPostOverwriteAllowsAPostSlugNobodyClaims(t *testing.T) {
	t.Parallel()
	err := RefuseForeignPostOverwrite(
		"alpha", []string{"blog/mine/index.html"},
		map[string]string{"blog/theirs/index.html": "beta"},
	)
	if err != nil {
		t.Errorf("err = %v, want none", err)
	}
}

func TestRefuseForeignPostOverwriteNamesEveryStolenPath(t *testing.T) {
	t.Parallel()
	err := RefuseForeignPostOverwrite(
		"alpha", []string{"blog/b/index.html", "blog/a/index.html"},
		map[string]string{"blog/a/index.html": "beta", "blog/b/index.html": "gamma"},
	)
	if err == nil {
		t.Fatal("want a refusal, got none")
	}
	want := "'alpha' would overwrite 2 post file(s) another project published: " +
		"site/blog/a/index.html is claimed by 'beta'; site/blog/b/index.html is " +
		"claimed by 'gamma'."
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("refusal:\n%s\nwant the prefix:\n%s", err.Error(), want)
	}
}
