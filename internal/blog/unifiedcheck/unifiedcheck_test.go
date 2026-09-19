package unifiedcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"

	// The fixtures declare Python sources, so the Python extractor has to
	// be linked in or resolution answers with a stub.
	_ "github.com/stricttools/selfdoc/internal/extractors/python"
)

// isolate binds the test-environment isolation floor: a throwaway HOME, an
// isolated git identity, and no ambient credentials.
func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// requirePython skips the test when python3 is not on this machine, which is
// what the Python extractor needs.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed, so the Python-backed checks cannot run")
	}
}

// postFrontmatter is a well-formed post frontmatter block whose description is
// long enough to keep the description rules out of these assertions.
const postFrontmatter = "+++\n" +
	"title = \"Hello World\"\n" +
	"date = 2024-01-15\n" +
	"slug = \"hello-world\"\n" +
	"draft = false\n" +
	"directives = false\n" +
	"description = \"A post description written at a comfortable length, so " +
	"that the description-length rules stay out of the assertions these " +
	"tests actually make.\"\n" +
	"+++\n"

// filesOf is every file a result attributes something to: the directive
// results' pages and the diagnostics' paths.
func filesOf(result *check.CheckResult) []string {
	files := make([]string, 0, len(result.DirectiveResults)+len(result.Lints))
	for _, directiveResult := range result.DirectiveResults {
		files = append(files, directiveResult.File)
	}
	for _, diagnostic := range result.Lints {
		files = append(files, diagnostic.File())
	}
	return files
}

// containsSubstring reports whether any of files carries substring.
func containsSubstring(files []string, substring string) bool {
	for _, file := range files {
		if strings.Contains(file, substring) {
			return true
		}
	}
	return false
}

// withCode returns the diagnostics carrying code.
func withCode(diagnostics []lints.LintResult, code string) []lints.LintResult {
	var matching []lints.LintResult
	for _, diagnostic := range diagnostics {
		if diagnostic.Code() == code {
			matching = append(matching, diagnostic)
		}
	}
	return matching
}

func TestCheckUnifiedRunsAcrossEveryProject(t *testing.T) {
	isolate(t)
	requirePython(t)
	testproject.RequirePagefind(t)
	docsSite := testproject.MakeUnified(
		t, []testproject.UnifiedProject{{Name: "core", Language: "python"}}, nil,
	)

	siteConfig, err := config.Load(docsSite)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	result, err := CheckUnified(siteConfig, docsSite, true, effects.Unbound())
	if err != nil {
		t.Fatalf("CheckUnified: %v", err)
	}

	// Every attribution carries a project's mount slug: the constituent's
	// own, or the docs-site's "[common]".
	files := filesOf(result)
	if !containsSubstring(files, "[core]") && !containsSubstring(files, "[common]") {
		t.Errorf("no attributed result at all: %v", files)
	}
	for _, file := range files {
		if !strings.HasPrefix(file, "[") {
			t.Errorf("unattributed file %q", file)
		}
	}
}

// TestAPostDefectIsReportedUnderItsProjectsSlug is the unified surface of the
// post-lint slice: a defect in a constituent project's post arrives with the
// project's slug prefixed onto the post's own path.
func TestAPostDefectIsReportedUnderItsProjectsSlug(t *testing.T) {
	isolate(t)
	requirePython(t)
	testproject.RequirePagefind(t)

	packages := filepath.Join(t.TempDir(), "monorepo", "packages")
	post := postFrontmatter + "Intro.\n\n![](/img/x.png)\n"
	core := filepath.Join(packages, "core")
	writePostsProject(t, core, map[string]string{"hello.md": post})

	docsSite := filepath.Join(packages, "docs-site")
	siteDocument := testproject.DefaultConfig(map[string]any{
		"unified": map[string]any{
			"projects": []any{map[string]any{"path": "../core"}},
		},
	})
	testproject.WriteJSON(t, filepath.Join(docsSite, "selfdoc.json"), siteDocument)
	testproject.WriteText(t, filepath.Join(docsSite, "src", "__init__.py"),
		`"""Docs site."""`+"\n")
	testproject.WriteText(t, filepath.Join(docsSite, ".stricttools", "docs", "index.md"),
		"# Docs site\n\nHello.\n")

	result, err := CheckUnified(nil, docsSite, true, effects.Unbound())
	if err != nil {
		t.Fatalf("CheckUnified: %v", err)
	}

	matching := withCode(result.Lints, "SEO003")
	if len(matching) != 1 {
		t.Fatalf("SEO003 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	wantFile := "[core] " + filepath.Join(".stricttools", "posts", "hello.md")
	if matching[0].File() != wantFile {
		t.Errorf("file = %q, want %q", matching[0].File(), wantFile)
	}
	// The prefix is the only thing the unified run adds: the line is the
	// post source file's own line, unchanged.
	if matching[0].Line() == nil || *matching[0].Line() != 11 {
		t.Errorf("line = %v, want 11", matching[0].Line())
	}
	// Relabelling produces a new diagnostic, whose severity must still be
	// the registry's answer for SEO003 rather than a default.
	severity, err := lints.LintSeverity("SEO003")
	if err != nil {
		t.Fatalf("LintSeverity: %v", err)
	}
	if matching[0].Severity() != severity {
		t.Errorf("severity = %q, want %q", matching[0].Severity(), severity)
	}
}

// TestAProjectWithNoConfigIsUNIFIED001 covers a condition the Python suite
// never asserted: a declared project directory that is not a selfdoc project.
func TestAProjectWithNoConfigIsUNIFIED001(t *testing.T) {
	isolate(t)
	requirePython(t)
	testproject.RequirePagefind(t)
	docsSite := testproject.MakeUnified(
		t, []testproject.UnifiedProject{{Name: "core", Language: "python"}}, nil,
	)

	// The directory stays on disk -- a missing one is a ConfigError from
	// the path resolution, a different condition -- but its config goes.
	coreConfig := filepath.Join(filepath.Dir(docsSite), "core", "selfdoc.json")
	if err := os.Remove(coreConfig); err != nil {
		t.Fatalf("remove %s: %v", coreConfig, err)
	}

	result, err := CheckUnified(nil, docsSite, true, effects.Unbound())
	if err != nil {
		t.Fatalf("CheckUnified: %v", err)
	}

	matching := withCode(result.Lints, "UNIFIED001")
	if len(matching) != 1 {
		t.Fatalf("UNIFIED001 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if matching[0].File() != "[core]" {
		t.Errorf("file = %q, want %q", matching[0].File(), "[core]")
	}
	if matching[0].Message() != "No selfdoc.json in project 'core'" {
		t.Errorf("message = %q", matching[0].Message())
	}
	if matching[0].Line() != nil {
		t.Errorf("line = %v, want none", matching[0].Line())
	}
	// The docs-site's own content is still checked: one project the run
	// cannot read does not end the run.
	if !containsSubstring(filesOf(result), "[common]") {
		t.Error("the common pages were not checked")
	}
}

// TestAProjectWhoseCheckRefusesIsUNIFIED002 covers the other condition the
// Python suite never asserted: a constituent whose check cannot run at all.
func TestAProjectWhoseCheckRefusesIsUNIFIED002(t *testing.T) {
	isolate(t)
	requirePython(t)
	testproject.RequirePagefind(t)
	docsSite := testproject.MakeUnified(
		t, []testproject.UnifiedProject{{Name: "core", Language: "python"}}, nil,
	)

	// A project whose declared docs directory is not on disk: the check
	// refuses before it validates anything.
	coreDocs := filepath.Join(filepath.Dir(docsSite), "core", ".stricttools", "docs")
	if err := os.RemoveAll(coreDocs); err != nil {
		t.Fatalf("remove %s: %v", coreDocs, err)
	}

	result, err := CheckUnified(nil, docsSite, true, effects.Unbound())
	if err != nil {
		t.Fatalf("CheckUnified: %v", err)
	}

	matching := withCode(result.Lints, "UNIFIED002")
	if len(matching) != 1 {
		t.Fatalf("UNIFIED002 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if matching[0].File() != "[core]" {
		t.Errorf("file = %q, want %q", matching[0].File(), "[core]")
	}
	// The refusal's own message is carried through, not a restatement.
	if matching[0].Message() != "Docs directory '.stricttools/docs/' not found." {
		t.Errorf("message = %q", matching[0].Message())
	}
	if !containsSubstring(filesOf(result), "[common]") {
		t.Error("the common pages were not checked")
	}
}

func TestNoUnifiedSectionIsRefused(t *testing.T) {
	isolate(t)
	project := testproject.Make(t, nil)

	_, err := CheckUnified(nil, project, true, effects.Unbound())
	if err == nil {
		t.Fatal("a project with no unified section was accepted")
	}
	if err.Error() != "No 'unified' section in selfdoc.json" {
		t.Errorf("error = %q", err.Error())
	}
}

func TestNoConfigAtAllIsRefused(t *testing.T) {
	isolate(t)
	_, err := CheckUnified(nil, t.TempDir(), true, effects.Unbound())
	if err == nil {
		t.Fatal("a directory that is not a project was accepted")
	}
	want := "No selfdoc.json found. Run 'selfdoc init' to initialize."
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestADeclaredProjectPathThatIsNotThereIsAnError(t *testing.T) {
	isolate(t)
	docsSite := filepath.Join(t.TempDir(), "docs-site")
	document := testproject.DefaultConfig(map[string]any{
		"unified": map[string]any{
			"projects": []any{map[string]any{"path": "../nowhere"}},
		},
	})
	testproject.WriteJSON(t, filepath.Join(docsSite, "selfdoc.json"), document)
	testproject.WriteText(t, filepath.Join(docsSite, "src", "__init__.py"),
		`"""Docs site."""`+"\n")
	testproject.WriteText(t, filepath.Join(docsSite, ".stricttools", "docs", "index.md"),
		"# Docs site\n\nHello.\n")

	_, err := CheckUnified(nil, docsSite, true, effects.Unbound())
	if err == nil {
		t.Fatal("a project path that resolves nowhere was accepted")
	}
	if !strings.Contains(err.Error(), "unified project path '../nowhere'") {
		t.Errorf("error = %q", err.Error())
	}
}

// writePostsProject writes a project carrying the given posts in the default
// posts directory, plus one docs page and one source file.
func writePostsProject(t *testing.T, root string, posts map[string]string) {
	t.Helper()
	document := testproject.DefaultConfig(map[string]any{
		"posts": map[string]any{"dir": ".stricttools/posts/"},
	})
	testproject.WriteJSON(t, filepath.Join(root, "selfdoc.json"), document)
	testproject.WriteText(t, filepath.Join(root, "src", "__init__.py"),
		`"""Example package."""`+"\n")
	testproject.WriteText(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \"A home page whose description is long "+
			"enough to keep the description rules quiet in this fixture.\"\n"+
			"+++\n# Test Project\n\nWelcome.\n")
	for name, content := range posts {
		testproject.WriteText(t, filepath.Join(root, ".stricttools", "posts", name), content)
	}
}

// messagesOf is the messages of a diagnostic list, for a failure report.
func messagesOf(diagnostics []lints.LintResult) []string {
	found := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		found = append(found, diagnostic.Code()+": "+diagnostic.Message())
	}
	return found
}
