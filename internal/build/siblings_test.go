package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// threeSiblings is the roster an assembled build is handed, out of name order
// so the rendering's own ordering is what the assertions read.
func threeSiblings() []SiblingProject {
	return []SiblingProject{
		{Slug: "gamma", Name: "Gamma", Description: "Does the gamma thing."},
		{Slug: "alpha", Name: "Alpha", Description: "Does the alpha thing."},
		{Slug: "beta", Name: "Beta", Description: ""},
	}
}

// buildWithSiblings builds the fixture project and returns its index page.
func buildWithSiblings(t *testing.T, siblings []SiblingProject) string {
	t.Helper()
	hygiene.Isolate(t)
	dir := testproject.Make(t, map[string]any{"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/"})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	empty := ""
	opts := NewSingleOptions()
	opts.DirPath = dir
	opts.Config = cfg
	opts.MountLocale = &empty
	opts.MountVersion = &empty
	opts.VersionOverride = &empty
	opts.WriteBaselines = false
	opts.Siblings = siblings
	result, err := BuildSingle(opts, effects.Unbound())
	if err != nil {
		t.Fatalf("BuildSingle: %v", err)
	}
	page, ok := result.HTMLFiles["index.html"]
	if !ok {
		t.Fatalf("the build wrote no index.html; keys: %v", sortedKeys(result.HTMLFiles))
	}
	return page
}

// TestAStandaloneBuildEmitsNoSiblingBlock: a project deployed on its own has
// no siblings, and nothing may invent them.
func TestAStandaloneBuildEmitsNoSiblingBlock(t *testing.T) {
	page := buildWithSiblings(t, nil)
	if strings.Contains(page, SiblingsHeading) {
		t.Errorf("a build handed no siblings emitted the block:\n%s", page)
	}
	if strings.Contains(page, "sibling-projects") {
		t.Errorf("a build handed no siblings emitted the section element")
	}
}

func TestTheSiblingBlockNamesEveryOtherProjectInNameOrder(t *testing.T) {
	page := buildWithSiblings(t, threeSiblings())
	if !strings.Contains(page, SiblingsHeading) {
		t.Fatalf("the built page carries no sibling block:\n%s", page)
	}
	positions := make([]int, 0, 3)
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		at := strings.Index(page, ">"+name+"</a>")
		if at < 0 {
			t.Fatalf("the sibling block does not name %q", name)
		}
		positions = append(positions, at)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] < positions[i-1] {
			t.Errorf("the siblings are not in name order: %v", positions)
		}
	}
	if !strings.Contains(page, "Does the alpha thing.") {
		t.Error("the sibling block drops the one-line description")
	}
}

func TestTheSiblingBlockLinksDocumentRelatively(t *testing.T) {
	page := buildWithSiblings(t, threeSiblings())
	// The page is the project's own index, which the assembly serves at
	// "<slug>/index.html", so a sibling is one hop out and then in.
	if want := `href="../alpha/"`; !strings.Contains(page, want) {
		t.Errorf("the sibling block does not carry %s", want)
	}
	if strings.Contains(page, `href="/alpha/"`) {
		t.Error("the sibling block writes an origin-absolute link")
	}
}

func TestTheSiblingBlockSitsOutsideTheIndexedBodyAndBeforeTheFooter(t *testing.T) {
	page := buildWithSiblings(t, threeSiblings())
	block := strings.Index(page, SiblingsHeading)
	footer := strings.Index(page, `<footer class="site-footer">`)
	article := strings.Index(page, "</article>")
	if block < 0 || footer < 0 || article < 0 {
		t.Fatalf("block=%d footer=%d article=%d", block, footer, article)
	}
	if block > footer {
		t.Error("the sibling block is emitted after the footer")
	}
	if block < article {
		t.Error("the sibling block is inside the indexed body, so search " +
			"returns it once per page on the site")
	}
	if !strings.Contains(page, "data-pagefind-ignore") {
		t.Error("the sibling block is not declared ignorable to the indexer")
	}
}

// TestTheSiblingBlockPassesTheResolutionRule: the block's links climb out of
// the project's output root, which is the assembled site the project is one
// subtree of. That is what the mounted form of the rule allows, and none of
// them may be absolute against the site's base.
func TestTheSiblingBlockPassesTheResolutionRule(t *testing.T) {
	hygiene.Isolate(t)
	dir := testproject.Make(t, map[string]any{
		"docs":     "stricttools/docs/",
		"output":   "stricttools/.docs-cache/build/",
		"base_url": "https://docs.example.com/selfdoc",
	})
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	empty := ""
	opts := NewSingleOptions()
	opts.DirPath = dir
	opts.Config = cfg
	opts.MountLocale = &empty
	opts.MountVersion = &empty
	opts.VersionOverride = &empty
	opts.WriteBaselines = false
	opts.Siblings = threeSiblings()
	result, err := BuildSingle(opts, effects.Unbound())
	if err != nil {
		t.Fatalf("BuildSingle: %v", err)
	}

	outputDir := t.TempDir()
	for key, pageHTML := range result.HTMLFiles {
		path := filepath.Join(outputDir, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(pageHTML), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	diagnostics, err := resolution.CheckOutputResolution(
		outputDir, "https://docs.example.com/selfdoc", "selfdoc/", nil,
	)
	if err != nil {
		t.Fatalf("CheckOutputResolution: %v", err)
	}
	for _, lint := range diagnostics {
		for _, slug := range []string{"alpha", "beta", "gamma"} {
			if strings.Contains(lint.Message(), slug+"/") {
				t.Errorf("the sibling block fails the resolution rule: %s %s",
					lint.File(), lint.Message())
			}
		}
	}
}

func TestTheSiblingBlockHopsFromThePagesOwnDepth(t *testing.T) {
	for _, test := range []struct{ outputKey, want string }{
		{"index.html", "../"},
		{"guide/index.html", "../../"},
		{"guide/deep/index.html", "../../../"},
		// A post is grafted out of the project's subtree to the site root,
		// so it reaches the site root from its own depth alone.
		{"blog/hello/index.html", "../../"},
	} {
		if got := siteRootHop(test.outputKey); got != test.want {
			t.Errorf("siteRootHop(%q) = %q, want %q", test.outputKey, got, test.want)
		}
	}
}

func TestSiblingsFromManifestsDropsTheHomeProjectAndTheProjectItself(t *testing.T) {
	manifests := []map[string]any{
		{"slug": "home", "name": "Home", "description": "The front page."},
		{"slug": "alpha", "name": "Alpha", "description": "Does the alpha thing."},
		{"slug": "beta", "name": "Beta", "description": "Does the beta thing."},
		{"name": "Nameless", "description": "No slug, no address."},
	}
	got := SiblingsFromManifests(manifests, "home", "alpha")
	if len(got) != 1 || got[0].Slug != "beta" {
		t.Fatalf("SiblingsFromManifests = %+v, want just beta", got)
	}
	if got[0].Description != "Does the beta thing." {
		t.Errorf("the sibling carries %q, want the manifest's description",
			got[0].Description)
	}
	// The home project's own build lists every other project: it is both the
	// home slug and the project the pages belong to.
	home := SiblingsFromManifests(manifests, "home", "home")
	if len(home) != 2 {
		t.Errorf("the home project's siblings = %+v, want alpha and beta", home)
	}
}

// siblingsFooterPage is a page shaped the way a built page is, with the block
// where the build writes it when one is given.
func siblingsFooterPage(block string) string {
	page := "<html><body><article><p>body</p></article>\n"
	if block != "" {
		page += block + "\n"
	}
	return page + `<footer class="site-footer"><p>footer</p></footer></body></html>`
}

func TestWithRefreshedSiblingsReplacesTheBlockAPageAlreadyCarries(t *testing.T) {
	// What the assembly's refresh pass rests on: the block is found and
	// replaced in place, so a page keeps one block whatever it arrived with.
	stale := renderSiblings(threeSiblings(), "../")
	page := siblingsFooterPage(stale)
	refreshed := WithRefreshedSiblings(page, "../", []SiblingProject{
		{Slug: "alpha", Name: "Alpha", Description: "Does the alpha thing."},
	})
	if strings.Count(refreshed, SiblingsBlockStart) != 1 {
		t.Fatalf("the page carries %d block(s):\n%s",
			strings.Count(refreshed, SiblingsBlockStart), refreshed)
	}
	for _, gone := range []string{"gamma/", "Gamma", "beta/", "Beta"} {
		if strings.Contains(refreshed, gone) {
			t.Errorf("the replaced block still names %q:\n%s", gone, refreshed)
		}
	}
	if !strings.Contains(refreshed, `href="../alpha/"`) {
		t.Errorf("the replaced block does not name the current sibling:\n%s", refreshed)
	}
	if want := siblingsFooterPage(renderSiblings([]SiblingProject{
		{Slug: "alpha", Name: "Alpha", Description: "Does the alpha thing."},
	}, "../")); refreshed != want {
		t.Errorf("the page reads\n%s\nwant\n%s", refreshed, want)
	}
}

func TestWithRefreshedSiblingsIsIdempotent(t *testing.T) {
	siblings := threeSiblings()
	once := WithRefreshedSiblings(siblingsFooterPage(""), "../", siblings)
	twice := WithRefreshedSiblings(once, "../", siblings)
	if once != twice {
		t.Errorf("a second refresh moved the page:\n%s\nwas\n%s", twice, once)
	}
}

func TestWithRefreshedSiblingsRemovesTheBlockWhenNothingIsLeftToList(t *testing.T) {
	page := siblingsFooterPage(renderSiblings(threeSiblings(), "../"))
	stripped := WithRefreshedSiblings(page, "../", nil)
	if want := siblingsFooterPage(""); stripped != want {
		t.Errorf("the stripped page reads\n%s\nwant\n%s", stripped, want)
	}
}

func TestWithRefreshedSiblingsLeavesAFooterlessFragmentAlone(t *testing.T) {
	// The block ends a page's main column. A fragment with no footer is not a
	// page, and nothing is written onto it.
	fragment := "<p>not a page</p>"
	if got := WithRefreshedSiblings(fragment, "../", threeSiblings()); got != fragment {
		t.Errorf("a footerless fragment was written onto: %s", got)
	}
}

func TestSiblingsBlockRangeFindsTheBlockAndNothingElse(t *testing.T) {
	block := renderSiblings(threeSiblings(), "../")
	page := "<section class=\"hero\"><p>hero</p></section>\n" + siblingsFooterPage(block)
	start, end, carries := SiblingsBlockRange(page)
	if !carries {
		t.Fatalf("the block was not found in:\n%s", page)
	}
	if got := page[start:end]; got != block {
		t.Errorf("the range covers\n%s\nwant\n%s", got, block)
	}
	if _, _, carries := SiblingsBlockRange(siblingsFooterPage("")); carries {
		t.Error("a page with no block was reported as carrying one")
	}
	// An opened block that is never closed cannot be spliced, and a truncated
	// page is not one to write a second block onto either.
	if _, _, carries := SiblingsBlockRange(SiblingsBlockStart + "<ul>"); carries {
		t.Error("an unclosed block was reported as carrying a range")
	}
}

func TestTheRenderedBlockCloseIsTheFirstOneAfterItsOpen(t *testing.T) {
	// The end marker is found by scanning forward from the start, which only
	// answers the block's own close because the rendering nests no section.
	block := renderSiblings(threeSiblings(), "../")
	if got := strings.Count(block, SiblingsBlockEnd); got != 1 {
		t.Errorf("the block carries %d closing tag(s), so the end marker is "+
			"no longer the first one after the start:\n%s", got, block)
	}
	if got := strings.Count(block, "<section"); got != 1 {
		t.Errorf("the block nests a section:\n%s", block)
	}
}
