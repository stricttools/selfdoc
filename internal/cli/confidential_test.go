package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/strictspec/go/confidential"
)

// The confidential-term rules at selfdoc's command boundary: every public
// output -- a deploy, a post publish, a documentation publish, an assembly
// push -- is refused while what it would publish carries a hit of the
// confidential-term list nothing resolves, or when no releasable may publish
// it; and no command keeps a machine-local name index.

// writeTermList writes a confidential-term list naming terms, encrypted to a
// fresh identity, where the application under test looks for it: under the
// throwaway home directory the isolation floor sets.
func writeTermList(t *testing.T, terms ...string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("format_version = 1\n")
	for _, term := range terms {
		b.WriteString("\n[[terms]]\nterm = \"" + term + "\"\nadded = 2026-10-01\nreason = \"a private name\"\nscope = \"everywhere\"\n")
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	loc := confidential.LocationIn(home)
	writeText(t, loc.List, buf.String())
	writeText(t, loc.Identity, id.String()+"\n")
}

// widgetName is the widget's releasable-name identity.
const widgetName = "\n[[identities]]\nsubject = \"widget\"\nfacet = \"releasable-name\"\nvalue = \"widget\"\nregistry = \"\"\ntag_patterns = [\"v*\"]\nfrom = 2020-01-01\nreason = \"its name\"\n"

// confidentialGitRepository makes dir a git repository with an origin remote
// and a lifecycle-and-license record in which the widget has been proprietary
// since 2020, which makes the repository confidential.
func confidentialGitRepository(t *testing.T, dir string) {
	t.Helper()
	testproject.Git(t, dir, "init")
	testproject.Git(t, dir, "remote", "add", "origin", "https://git.invalid/example/gadget-works.git")
	writeText(t, filepath.Join(dir, ".strictmetadata", "lifecycle-and-license", "lifecycle-and-license.toml"),
		"format_version = 1\n\n[[licenses]]\nsubject = \"widget\"\nlicense = \"proprietary\"\nfrom = 2020-01-01\nreason = \"the widget is proprietary\"\n"+
			widgetName)
}

// No command keeps a machine-local name index: a mutating command in a
// confidential repository writes nothing under the user's configuration
// directory.
func TestAMutatingCommandWritesNoNameIndex(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	confidentialGitRepository(t, dir)
	if result := run(t, dir, "gen-data", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen-data exited %d: %s", result.ExitCode, result.Stderr)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config, "strictspec")); !os.IsNotExist(err) {
		t.Fatalf("a command wrote under %s: %v", filepath.Join(config, "strictspec"), err)
	}
}

func TestDeployRefusesAPageNamingAConfidentialTerm(t *testing.T) {
	tools := newFakeTools(t, "wrangler")
	tools.Reply(toolReply{Match: "", Code: 0})
	writeTermList(t, "portal")
	// No git repository and no record: a repository without a record is
	// public, and its pages are scanned like any other's.
	dir := deployProject(t, map[string]any{"provider": "cloudflare-pages", "project": "docs"})
	page := filepath.Join(dir, ".strictmetadata", ".docs-cache", "build", "guide", "index.html")
	writeText(t, page, "<html>\n<p>The Portal is coming.</p>\n</html>\n")

	result := run(t, dir, "deploy", "--approve-consequential")
	if result.ExitCode == 0 {
		t.Fatal("a deploy of a page naming a confidential term was accepted")
	}
	if !strings.Contains(result.Stderr, "guide/index.html, line 2, column 8: <p>The >>Portal<< is coming.</p>") || !strings.Contains(result.Stderr, "rlsbl confidential judge-false-positive") {
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

// openAPublicLicensePeriod performs the fix the proprietary-output refusal
// names: the widget's proprietary period ends, and a public one opens.
func openAPublicLicensePeriod(t *testing.T, dir string) {
	t.Helper()
	writeText(t, filepath.Join(dir, ".strictmetadata", "lifecycle-and-license", "lifecycle-and-license.toml"),
		"format_version = 1\n\n[[licenses]]\nsubject = \"widget\"\nlicense = \"proprietary\"\nfrom = 2020-01-01\nuntil = 2021-01-01\nreason = \"the widget was proprietary\"\n\n"+
			"[[licenses]]\nsubject = \"widget\"\nlicense = \"MIT\"\nfrom = 2021-01-01\nreason = \"the widget is open source\"\n"+widgetName)
}

func TestDeployOfARepositoryWithEveryLicenseProprietaryIsRefused(t *testing.T) {
	tools := newFakeTools(t, "wrangler")
	tools.Reply(toolReply{Match: "", Code: 0})
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

	// The refusal's fix: open a public license period, and the deploy runs.
	openAPublicLicensePeriod(t, dir)
	if result := run(t, dir, "deploy", "--approve-consequential"); result.ExitCode != 0 {
		t.Fatalf("the deploy was refused after a public license period opened: %s", result.Stderr)
	}
	if calls := tools.Matching("wrangler pages deploy"); len(calls) != 1 {
		t.Errorf("the deploy reached wrangler %d time(s), want 1", len(calls))
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

	// The refusal's fix: open a public license period, and the publish goes
	// on to the assembly.
	openAPublicLicensePeriod(t, dir)
	writePost(t, filepath.Join(dir, ".strictmetadata", "posts"), "a.md", []string{"title = \"Launch\"", "date = 2025-01-15"}, "\nSomething is coming.\n")
	result = run(t, dir, "blog", "post", "publish", "--approve-consequential")
	if strings.Contains(result.Stderr, "proprietary-refuses-public-output") {
		t.Fatalf("the publish was refused after a public license period opened:\n%s", result.Stderr)
	}
	if calls := tools.Calls(); len(calls) == 0 {
		t.Errorf("the publish did not go on to the assembly after a public license period opened:\n%s", result.Stderr)
	}
}

func TestPublishDocsRefusesAPageNamingAConfidentialTerm(t *testing.T) {
	tools := newFakeTools(t, "gh", "pagefind")
	dir := homeSiteProject(t, nil)
	writeHomeFrontPage(t, dir, "Prose about the portal.")
	servePush(t, tools, "home")
	writeTermList(t, "portal")

	result := run(t, dir, "blog", "publish-docs")
	if result.ExitCode == 0 {
		t.Fatal("a documentation publish of a page naming a confidential term was accepted")
	}
	if !strings.Contains(result.Stderr, "documentation publish") || !strings.Contains(result.Stderr, ">>portal<<") {
		t.Errorf("the refusal does not name the output and the term:\n%s", result.Stderr)
	}
	if calls := tools.Matching("POST /repos/owner/assembly/git/blobs"); len(calls) != 0 {
		t.Errorf("the refused publish pushed %d blob(s)", len(calls))
	}
}

func TestPostPublishRefusesAPostNamingAConfidentialTermBeforeWritingAnything(t *testing.T) {
	tools := newFakeTools(t, "gh")
	writeTermList(t, "portal")
	// No git repository and no record: a repository without a record is
	// public, and its posts are scanned like any other's.
	dir := postProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "widget"},
	})
	postsDir := filepath.Join(dir, ".strictmetadata", "posts")
	writePost(t, postsDir, "a.md", []string{"title = \"Launch\"", "date = 2025-01-15"}, "\nThe Portal is coming.\n")

	result := run(t, dir, "blog", "post", "publish", "--approve-consequential")
	if result.ExitCode == 0 {
		t.Fatal("a post publish of a post naming a confidential term was accepted")
	}
	if !strings.Contains(result.Stderr, "post publish") || !strings.Contains(result.Stderr, "a.md, line 7, column 5: The >>Portal<< is coming.") {
		t.Errorf("the refusal does not name the post, line, column, and term:\n%s", result.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(layout.RevisionsRel))); !os.IsNotExist(err) {
		t.Errorf("the refused publish wrote the post's revision into %s (stat: %v)", layout.RevisionsRel, err)
	}
	if calls := tools.Calls(); len(calls) != 0 {
		t.Errorf("the refused publish reached the assembly: %v", calls)
	}

	// The refusal's fix: remove the term, and the publish goes on to the
	// assembly.
	writePost(t, postsDir, "a.md", []string{"title = \"Launch\"", "date = 2025-01-15"}, "\nSomething is coming.\n")
	result = run(t, dir, "blog", "post", "publish", "--approve-consequential")
	if strings.Contains(result.Stderr, "confidential-term hits nothing resolves") {
		t.Fatalf("the publish was refused after the term was removed:\n%s", result.Stderr)
	}
	if calls := tools.Calls(); len(calls) == 0 {
		t.Errorf("the publish did not go on to the assembly after the term was removed:\n%s", result.Stderr)
	}
}

// `assembly push` has the assembly publish the documentation built from a
// ref, so every tracked file at that ref is scanned before the dispatch.
func TestAssemblyPushRefusesARefNamingAConfidentialTerm(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly", "pages_project": "site"},
		"topology": map[string]any{"slug": "myproject", "docs_base": "https://docs.example.com"},
	})
	writeTermList(t, "portal")
	writeText(t, filepath.Join(dir, "notes.md"), "Prose about the Portal.\n")
	testproject.Git(t, dir, "init")
	testproject.Git(t, dir, "add", "selfdoc.json", "notes.md")
	testproject.Git(t, dir, "commit", "-m", "initial")
	testproject.Git(t, dir, "tag", "v1.0.0")
	tools.Reply(
		toolReply{Match: "repo view", Stdout: "owner/source-repo\n"},
		toolReply{Match: "", Stdout: ""},
	)

	result := run(t, dir, "assembly", "push")
	if result.ExitCode == 0 {
		t.Fatal("a push of a ref naming a confidential term was dispatched")
	}
	if !strings.Contains(result.Stderr, "notes.md, line 1, column 17: Prose about the >>Portal<<.") {
		t.Errorf("the refusal does not name the file, line, and term:\n%s", result.Stderr)
	}
	if dispatches := tools.Matching("/dispatches"); len(dispatches) != 0 {
		t.Fatalf("the refused push dispatched: %v", dispatches)
	}

	// The refusal's fix: remove the term, commit, and push the ref that
	// carries the removal.
	writeText(t, filepath.Join(dir, "notes.md"), "Prose about the server.\n")
	testproject.Git(t, dir, "commit", "-am", "remove the term")
	testproject.Git(t, dir, "tag", "-f", "v1.0.0")
	if result := run(t, dir, "assembly", "push"); result.ExitCode != 0 {
		t.Fatalf("the push was refused after the term was removed: %s", result.Stderr)
	}
	if dispatches := tools.Matching("/dispatches"); len(dispatches) != 1 {
		t.Fatalf("expected one dispatch, got %d", len(dispatches))
	}
}

// `assembly republish-all` publishes every checkout's build, so each build is
// scanned before anything is published, in a dry run too.
func TestRepublishAllRefusesABuildNamingAConfidentialTerm(t *testing.T) {
	s := newRepublishSite(t, testVersion, nil)
	writeTermList(t, "moonbeam")
	page := filepath.Join(testproject.DocsDir(s.alpha), "index.md")
	writeText(t, page, "# Alpha\n\nThe moonbeam work.\n")
	commitAll(t, s.alpha)

	result := s.republish("--dry-run")
	if result.ExitCode == 0 {
		t.Fatal("a republish whose build names a confidential term was accepted")
	}
	if !strings.Contains(result.Stderr, "republish of alpha") || !strings.Contains(result.Stderr, "moonbeam") {
		t.Errorf("the refusal does not name the output and term:\n%s", result.Stderr)
	}

	// The refusal's fix: remove the term, and the republish goes through.
	writeText(t, page, "# Alpha\n\nThe server work.\n")
	commitAll(t, s.alpha)
	if result := s.republish("--dry-run"); result.ExitCode != 0 {
		t.Fatalf("the republish was refused after the term was removed: %s", result.Stderr)
	}
}
