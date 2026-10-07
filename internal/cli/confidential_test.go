package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/strictspec/go/lifecycle/index"
)

// The confidential-name rules at selfdoc's command boundary: every mutating
// command keeps the machine-local index current for its repository and a
// read-only command never writes it, and every public output -- a deploy, a
// post publish, a documentation publish -- is refused when what it would
// publish names a confidential term, or when no releasable may publish it.

// confidentialIndexPath is the index path the application under test uses:
// the isolation floor points the user configuration directory at a throwaway
// one.
func confidentialIndexPath(t *testing.T) string {
	t.Helper()
	path, err := index.DefaultPath()
	if err != nil {
		t.Fatalf("locating the confidential-name index: %v", err)
	}
	return path
}

// writeConfidentialNames writes an index holding one other repository's
// entry with the given names.
func writeConfidentialNames(t *testing.T, names ...string) string {
	t.Helper()
	path := confidentialIndexPath(t)
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + n + `"`
	}
	writeText(t, path, "format_version = 1\n\n[[repositories]]\norigin = \"github.com/example/portal-works\"\nnames = ["+
		strings.Join(quoted, ", ")+"]\n")
	return path
}

// confidentialGitRepository makes dir a git repository with an origin remote
// and a lifecycle-and-license record in which the widget has been proprietary
// since 2020, which makes the repository confidential.
func confidentialGitRepository(t *testing.T, dir string) {
	t.Helper()
	testproject.Git(t, dir, "init")
	testproject.Git(t, dir, "remote", "add", "origin", "https://git.invalid/example/gadget-works.git")
	writeText(t, filepath.Join(dir, ".strictmetadata", "lifecycle-and-license", "lifecycle-and-license.toml"),
		"format_version = 1\n\n[[licenses]]\nsubject = \"widget\"\nlicense = \"proprietary\"\nfrom = 2020-01-01\nreason = \"the widget is proprietary\"\n")
}

func indexExists(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(confidentialIndexPath(t))
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return false
}

func TestReadOnlyCommandLeavesTheIndexUnwrittenAndAMutatingOneWritesIt(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	confidentialGitRepository(t, dir)

	// Whatever a read-only command's verdict, it writes no index.
	run(t, dir, "blog", "post", "list")
	if indexExists(t) {
		t.Fatal("a read-only command wrote the confidential-name index")
	}

	// gen-data is mutating; with nothing configured it does nothing else.
	if result := run(t, dir, "gen-data", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen-data exited %d: %s", result.ExitCode, result.Stderr)
	}
	got := readText(t, confidentialIndexPath(t))
	for _, want := range []string{`origin = "git.invalid/example/gadget-works"`, `"widget"`, `"gadget-works"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the mutating command did not record %s:\n%s", want, got)
		}
	}
}

func TestDryRunOfAMutatingCommandLeavesTheIndexUnwritten(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	confidentialGitRepository(t, dir)

	if result := run(t, dir, "--dry-run", "gen-data", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen-data --dry-run exited %d: %s", result.ExitCode, result.Stderr)
	}
	if indexExists(t) {
		t.Error("a dry run wrote the confidential-name index")
	}
}

func TestConfidentialRepositoryWithoutAnOriginRefusesMutatingCommands(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	confidentialGitRepository(t, dir)
	testproject.Git(t, dir, "remote", "remove", "origin")

	result := run(t, dir, "gen-data", "--no-auto-commit")
	if result.ExitCode == 0 {
		t.Fatal("a confidential repository with no origin ran a mutating command without recording its names")
	}
	if !strings.Contains(result.Stderr, "git remote add origin") {
		t.Errorf("the refusal does not name the fix:\n%s", result.Stderr)
	}

	// The refusal's fix: add the origin remote, and the command runs.
	testproject.Git(t, dir, "remote", "add", "origin", "https://git.invalid/example/gadget-works.git")
	if result := run(t, dir, "gen-data", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen-data still refused after the origin was added: %s", result.Stderr)
	}
}

func TestDeployRefusesAPageNamingAConfidentialTerm(t *testing.T) {
	tools := newFakeTools(t, "wrangler")
	tools.Reply(toolReply{Match: "", Code: 0})
	writeConfidentialNames(t, "portal")
	// No git repository and no record: a repository without a record is
	// public, and its pages are scanned like any other's.
	dir := deployProject(t, map[string]any{"provider": "cloudflare-pages", "project": "docs"})
	page := filepath.Join(dir, ".strictmetadata", ".docs-cache", "build", "guide", "index.html")
	writeText(t, page, "<html>\n<p>The Portal is coming.</p>\n</html>\n")

	result := run(t, dir, "deploy", "--approve-consequential")
	if result.ExitCode == 0 {
		t.Fatal("a deploy of a page naming a confidential term was accepted")
	}
	if !strings.Contains(result.Stderr, "guide/index.html, line 2, column 8: portal") {
		t.Errorf("the refusal does not name the page, line, and term:\n%s", result.Stderr)
	}
	if calls := tools.Matching("wrangler"); len(calls) != 0 {
		t.Fatalf("the refused deploy reached wrangler: %v", calls)
	}

	// The refusal's fix: remove the term and rebuild, and the deploy runs.
	writeText(t, page, "<html>\n<p>Something is coming.</p>\n</html>\n")
	if result := run(t, dir, "deploy", "--approve-consequential"); result.ExitCode != 0 {
		t.Fatalf("the deploy was refused after the term was removed: %s", result.Stderr)
	}
	if calls := tools.Matching("wrangler pages deploy"); len(calls) != 1 {
		t.Errorf("the deploy reached wrangler %d time(s), want 1", len(calls))
	}
}

func TestDeployOfARepositoryWithEveryLicenseProprietaryIsRefused(t *testing.T) {
	tools := newFakeTools(t, "wrangler")
	dir := deployProject(t, map[string]any{"provider": "cloudflare-pages", "project": "docs"})
	confidentialGitRepository(t, dir)

	result := run(t, dir, "deploy", "--approve-consequential")
	if result.ExitCode == 0 {
		t.Fatal("a deploy from a repository with no releasable that may publish was accepted")
	}
	for _, want := range []string{"proprietary-refuses-public-output", "widget"} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, result.Stderr)
		}
	}
	if calls := tools.Matching("wrangler"); len(calls) != 0 {
		t.Fatalf("the refused deploy reached wrangler: %v", calls)
	}
}

func TestPostPublishOfARepositoryWithEveryLicenseProprietaryIsRefused(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := postProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "widget"},
	})
	confidentialGitRepository(t, dir)

	result := run(t, dir, "blog", "post", "publish", "--approve-consequential")
	if result.ExitCode == 0 {
		t.Fatal("a post publish from a repository with no releasable that may publish was accepted")
	}
	if !strings.Contains(result.Stderr, "proprietary-refuses-public-output") {
		t.Errorf("the refusal does not name the rule:\n%s", result.Stderr)
	}
	if calls := tools.Calls(); len(calls) != 0 {
		t.Errorf("the refused publish reached the assembly: %v", calls)
	}
}

func TestPublishDocsRefusesAPageNamingAConfidentialTerm(t *testing.T) {
	tools := newFakeTools(t, "gh", "pagefind")
	dir := homeSiteProject(t, nil)
	writeHomeFrontPage(t, dir, "Prose about the portal.")
	servePush(t, tools, "home")
	writeConfidentialNames(t, "portal")

	result := run(t, dir, "blog", "publish-docs")
	if result.ExitCode == 0 {
		t.Fatal("a documentation publish of a page naming a confidential term was accepted")
	}
	if !strings.Contains(result.Stderr, "documentation publish") || !strings.Contains(result.Stderr, ": portal") {
		t.Errorf("the refusal does not name the output and the term:\n%s", result.Stderr)
	}
	if calls := tools.Matching("POST /repos/owner/assembly/git/blobs"); len(calls) != 0 {
		t.Errorf("the refused publish pushed %d blob(s)", len(calls))
	}
}
