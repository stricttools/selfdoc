package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
)

// checkFixture runs CheckDocs over a fixture project with no version filter
// and no version override.
func checkFixture(t *testing.T, root string) *CheckResult {
	t.Helper()
	result, err := CheckDocs(root, nil, false, "", "", handle())
	if err != nil {
		t.Fatalf("CheckDocs: %v", err)
	}
	return result
}

func TestAllDirectivesOK(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"# API\n\n:-: ref path=\"mylib\"\n")

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 1 {
		t.Fatalf("directive results = %d, want 1: %+v",
			len(result.DirectiveResults), result.DirectiveResults)
	}
	got := result.DirectiveResults[0]
	if got.File != "api.md" {
		t.Errorf("file = %q, want \"api.md\"", got.File)
	}
	if got.Line != 3 {
		t.Errorf("line = %d, want 3", got.Line)
	}
	if got.Outcome != StatusOK {
		t.Errorf("outcome = %q (error %q), want OK", got.Outcome, got.Error)
	}
	if got.Error != "" {
		t.Errorf("error = %q, want empty", got.Error)
	}
	if !strings.Contains(got.Directive, "ref") {
		t.Errorf("directive = %q, want it to name ref", got.Directive)
	}
}

func TestMultipleDirectivesAllOK(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"# API\n\n:-: ref path=\"mylib\"\n\n:-: ref path=\"mylib.utils\"\n")

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 2 {
		t.Fatalf("directive results = %d, want 2", len(result.DirectiveResults))
	}
	for _, got := range result.DirectiveResults {
		if got.Outcome != StatusOK {
			t.Errorf("%s:%d outcome = %q (error %q), want OK",
				got.File, got.Line, got.Outcome, got.Error)
		}
	}
}

func TestFailedDirectiveReported(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"# API\n\n:-: ref path=\"mylib.nonexistent\"\n")

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 1 {
		t.Fatalf("directive results = %d, want 1", len(result.DirectiveResults))
	}
	got := result.DirectiveResults[0]
	if got.Outcome != StatusFailed {
		t.Errorf("outcome = %q, want FAILED", got.Outcome)
	}
	if got.Error == "" {
		t.Error("a failed directive carries no error message")
	}
}

func TestDirectivesAcrossMultipleFiles(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "a.md"), "# A\n\n:-: ref path=\"mylib\"\n")
	write(t, filepath.Join(root, ".stricttools", "docs", "b.md"), "# B\n\n:-: ref path=\"mylib.utils\"\n")

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 2 {
		t.Fatalf("directive results = %d, want 2", len(result.DirectiveResults))
	}
	if result.DirectiveResults[0].File != "a.md" ||
		result.DirectiveResults[1].File != "b.md" {
		t.Errorf("pages = %q, %q; want a.md then b.md (sorted)",
			result.DirectiveResults[0].File, result.DirectiveResults[1].File)
	}
}

func TestCoverageFull(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"),
		"# API\n\n:-: ref path=\"mylib\"\n\n:-: ref path=\"mylib.utils\"\n")

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.Total != 4 {
		t.Errorf("total public = %d, want 4 (greet, farewell, Widget, helper): %v",
			result.Coverage.Total, result.Coverage.ReferencedSymbols)
	}
	if result.Coverage.ReferencedCount != 4 {
		t.Errorf("referenced = %d, want 4; unreferenced %v",
			result.Coverage.ReferencedCount, result.Coverage.UnreferencedSymbols)
	}
	if result.Coverage.DocumentedCount != 4 {
		t.Errorf("documented = %d, want 4", result.Coverage.DocumentedCount)
	}
}

func TestCoveragePartial(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "api.md"), "# API\n\n:-: ref path=\"mylib\"\n")

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.Total != 4 {
		t.Errorf("total public = %d, want 4", result.Coverage.Total)
	}
	if result.Coverage.ReferencedCount != 3 {
		t.Errorf("referenced = %d, want 3; unreferenced %v",
			result.Coverage.ReferencedCount, result.Coverage.UnreferencedSymbols)
	}
}

func TestCoverageNoneDocumented(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"), "# Guide\n\nNo directives here.\n")

	result := checkFixture(t, root)

	if result.Coverage == nil {
		t.Fatal("coverage was not measured")
	}
	if result.Coverage.ReferencedCount != 0 {
		t.Errorf("referenced = %d, want 0", result.Coverage.ReferencedCount)
	}
	if len(result.Coverage.UnreferencedSymbols) != 4 {
		t.Errorf("unreferenced = %v, want four symbols",
			result.Coverage.UnreferencedSymbols)
	}
}

func TestNoDocsDirRaises(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, pythonProjectConfig())

	_, err := CheckDocs(root, nil, false, "", "", handle())
	if err == nil {
		t.Fatal("a project with no docs directory was accepted")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to say the docs directory was not found", err)
	}
}

func TestNoConfigRaises(t *testing.T) {
	isolate(t)
	root := t.TempDir()

	_, err := CheckDocs(root, nil, false, "", "", handle())
	if err == nil {
		t.Fatal("a directory with no selfdoc.json was accepted")
	}
	if !strings.Contains(err.Error(), "No selfdoc.json found") {
		t.Errorf("error = %q, want it to name the missing config", err)
	}
}

func TestRootTemplateDirectivesValidated(t *testing.T) {
	root := pythonProject(t)
	config := pythonProjectConfig()
	config["root_files"] = []any{".stricttools/docs/_README.md"}
	writeConfig(t, root, config)
	write(t, filepath.Join(root, ".stricttools", "docs", "_README.md"),
		"# Project\n\n:-: ref path=\"mylib\"\n")

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 1 {
		t.Fatalf("directive results = %+v, want the root template's one",
			result.DirectiveResults)
	}
	if result.DirectiveResults[0].File != ".stricttools/docs/_README.md" {
		t.Errorf("file = %q, want docs/_README.md", result.DirectiveResults[0].File)
	}
}

func TestRootTemplateMissingFileSkipped(t *testing.T) {
	root := pythonProject(t)
	config := pythonProjectConfig()
	config["root_files"] = []any{"docs/_MISSING.md"}
	writeConfig(t, root, config)

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 0 {
		t.Errorf("directive results = %+v, want none", result.DirectiveResults)
	}
}

func TestRootTemplateWithFrontmatter(t *testing.T) {
	root := pythonProject(t)
	config := pythonProjectConfig()
	config["root_files"] = []any{".stricttools/docs/_README.md"}
	writeConfig(t, root, config)
	write(t, filepath.Join(root, ".stricttools", "docs", "_README.md"),
		"+++\ntitle = \"Readme\"\n+++\n\n# Project\n\n:-: ref path=\"mylib\"\n")

	result := checkFixture(t, root)

	if len(result.DirectiveResults) != 1 {
		t.Fatalf("directive results = %+v", result.DirectiveResults)
	}
	// The frontmatter occupies lines 1-3 plus the blank line after it, so
	// the directive's reported line is its real line in the file.
	if result.DirectiveResults[0].Line != 7 {
		t.Errorf("line = %d, want 7", result.DirectiveResults[0].Line)
	}
}

func TestPrintResultsNoDirectives(t *testing.T) {
	var out bytes.Buffer
	PrintResults(&out, &CheckResult{}, false)
	if !strings.Contains(out.String(), "No directives found in documentation templates.") {
		t.Errorf("output = %q", out.String())
	}
	if !strings.Contains(out.String(), "No lints.") {
		t.Errorf("output = %q, want the no-lints line", out.String())
	}
}

func TestPrintResultsColorOnlyWhenAsked(t *testing.T) {
	result := &CheckResult{
		DirectiveResults: []DirectiveResult{{
			File: "index.md", Line: 1, Directive: `ref path="foo"`, Outcome: StatusOK,
		}},
	}
	var plain bytes.Buffer
	PrintResults(&plain, result, false)
	if strings.Contains(plain.String(), "\033[") {
		t.Errorf("plain output carries escapes: %q", plain.String())
	}
	var colored bytes.Buffer
	PrintResults(&colored, result, true)
	if !strings.Contains(colored.String(), "\033[32mOK\033[0m") {
		t.Errorf("colored output = %q, want a green OK", colored.String())
	}
}

func TestSerializeCheckResultShape(t *testing.T) {
	result := &CheckResult{
		DirectiveResults: []DirectiveResult{{
			File: "index.md", Line: 4, Directive: `ref path="foo"`, Outcome: StatusOK,
		}},
		Coverage: &CoverageStats{
			Total: 3, ReferencedCount: 2, DocumentedCount: 1,
			ReferencedSymbols:   []string{"a.py:one", "a.py:two"},
			DocumentedSymbols:   []string{"a.py:one"},
			UnreferencedSymbols: []string{"a.py:three"},
		},
	}
	payload := SerializeCheckResult(result, 1)

	if payload["exit_code"] != 1 {
		t.Errorf("exit_code = %v, want 1", payload["exit_code"])
	}
	directives, _ := payload["directives"].([]any)
	if len(directives) != 1 {
		t.Fatalf("directives = %v", payload["directives"])
	}
	entry, _ := directives[0].(map[string]any)
	if entry["status"] != StatusOK {
		t.Errorf("status = %v, want OK", entry["status"])
	}
	coverage, _ := payload["coverage"].(map[string]any)
	if coverage["total_public"] != 3 || coverage["documented"] != 1 {
		t.Errorf("coverage = %v", coverage)
	}
	if _, isList := coverage["referenced_symbols"].([]any); !isList {
		t.Errorf("referenced_symbols = %T, want a list", coverage["referenced_symbols"])
	}
}

func TestSerializeCheckResultNoCoverage(t *testing.T) {
	payload := SerializeCheckResult(&CheckResult{}, 0)
	if payload["coverage"] != nil {
		t.Errorf("coverage = %v, want null", payload["coverage"])
	}
}

func TestStrictcliCodeHelpIsAHardError(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".strictcli", "schema.json"), `{
  "schema_version": 2,
  "name": "mylib",
  "project_id": "unknown",
  "version": "1.0.0",
  "help": "A tool.",
  "commands": {},
  "groups": {}
}`)
	write(t, filepath.Join(root, ".stricttools", "docs", "cli.md"),
		"# CLI\n\n:-: code-help path=\"mylib\"\n")

	_, err := CheckDocs(root, nil, false, "", "", handle())
	if err == nil {
		t.Fatal("a strictcli project with a code-help directive was accepted")
	}
	if !strings.Contains(err.Error(), "selfdoc gen") {
		t.Errorf("error = %q, want it to name 'selfdoc gen'", err)
	}
}

func TestCheckResultExitCode(t *testing.T) {
	isolate(t)
	failing := &CheckResult{DirectiveResults: []DirectiveResult{
		{File: "a.md", Line: 1, Directive: "ref", Outcome: StatusFailed, Error: "boom"},
	}}
	if got := CheckResultExitCode(failing, nil); got != 1 {
		t.Errorf("exit code with a failed directive = %d, want 1", got)
	}
	passing := &CheckResult{DirectiveResults: []DirectiveResult{
		{File: "a.md", Line: 1, Directive: "ref", Outcome: StatusOK},
	}}
	if got := CheckResultExitCode(passing, nil); got != 0 {
		t.Errorf("exit code with everything OK = %d, want 0", got)
	}
}

func TestCheckResultExitCodeCoverageThreshold(t *testing.T) {
	isolate(t)
	result := &CheckResult{
		DirectiveResults: []DirectiveResult{
			{File: "a.md", Line: 1, Directive: "ref", Outcome: StatusOK},
		},
		Coverage: &CoverageStats{Total: 4, ReferencedCount: 4, DocumentedCount: 2},
	}
	if got := CheckResultExitCode(result, nil); got != 1 {
		t.Errorf("exit code below the default threshold = %d, want 1", got)
	}
	lowered := map[string]any{"coverage_threshold": 0.5}
	if got := CheckResultExitCode(result, lowered); got != 0 {
		t.Errorf("exit code at a lowered threshold = %d, want 0", got)
	}
}

func TestThemeCSSAnswersEveryShippedTheme(t *testing.T) {
	if _, present := ThemeCSS("minimal"); !present {
		t.Error("the minimal theme's stylesheet was not found")
	}
	if _, present := ThemeCSS("no-such-theme"); present {
		t.Error("an unknown theme answered with a stylesheet")
	}
}

func TestCheckDocsWritesHashStore(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
		"+++\ndescription = \"A guide to the library and everything in it.\"\n+++\n# Guide\n\nText.\n")

	checkFixture(t, root)

	hashesPath := filepath.Join(root, ".stricttools", "docs-state", "hashes", "hashes.json")
	if _, err := os.Stat(hashesPath); err != nil {
		t.Fatalf("the hash store was not written: %v", err)
	}
}

func TestCheckDocsDryRunLeavesHashStoreAlone(t *testing.T) {
	root := pythonProject(t)
	write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
		"+++\ndescription = \"A guide to the library and everything in it.\"\n+++\n# Guide\n\nText.\n")

	if _, err := CheckDocs(root, nil, true, "", "", handle()); err != nil {
		t.Fatalf("CheckDocs: %v", err)
	}

	hashesPath := filepath.Join(root, ".stricttools", "docs-state", "hashes", "hashes.json")
	if _, err := os.Stat(hashesPath); err == nil {
		t.Error("a dry run wrote the hash store")
	}
}

func TestLINK001OverTheBuiltTree(t *testing.T) {

	t.Run("a project with no build output has nothing to check", func(t *testing.T) {
		root := pythonProject(t)
		write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
			"+++\ndescription = \"A guide covering everything the project does for a "+
				"reader.\"\n+++\n# Guide\n\nText.\n")

		result := checkFixture(t, root)

		if hasCode(result.Lints, "LINK001") {
			t.Errorf("LINK001 fired with no built tree: %v",
				messagesOf(withCode(result.Lints, "LINK001")))
		}
	})

	t.Run("an emitted reference naming nothing is reported", func(t *testing.T) {
		root := pythonProject(t)
		write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
			"+++\ndescription = \"A guide covering everything the project does for a "+
				"reader.\"\n+++\n# Guide\n\nText.\n")
		write(t, filepath.Join(root, ".stricttools", "docs-cache", "build", "index.html"),
			`<a href="missing/">Missing</a>`)

		result := checkFixture(t, root)

		if !hasCode(result.Lints, "LINK001") {
			t.Fatalf("LINK001 missing; got %v", codes(result.Lints))
		}
	})

	t.Run("a resolving tree is silent", func(t *testing.T) {
		root := pythonProject(t)
		write(t, filepath.Join(root, ".stricttools", "docs", "guide.md"),
			"+++\ndescription = \"A guide covering everything the project does for a "+
				"reader.\"\n+++\n# Guide\n\nText.\n")
		write(t, filepath.Join(root, ".stricttools", "docs-cache", "build", "index.html"),
			`<a href="guide/">Guide</a>`)
		write(t, filepath.Join(root, ".stricttools", "docs-cache", "build", "guide", "index.html"),
			`<p>hi</p>`)

		result := checkFixture(t, root)

		if hasCode(result.Lints, "LINK001") {
			t.Errorf("LINK001 fired on a resolving tree: %v",
				messagesOf(withCode(result.Lints, "LINK001")))
		}
	})
}

func TestFilterLints(t *testing.T) {
	result := &CheckResult{}
	fixture := lintProject(t)
	write(t, filepath.Join(fixture.DocsDir, "page.md"), "# Title\n\nText.\n")
	result.Lints = runLintsOn(t, fixture, nil)

	if !hasCode(result.Lints, "SEO006") {
		t.Fatalf("the fixture produced no SEO006: %v", codes(result.Lints))
	}
	filtered := FilterLints(result.Lints, map[string]struct{}{"SEO006": {}})
	if hasCode(filtered, "SEO006") {
		t.Error("a suppressed code survived the filter")
	}
	if len(FilterLints(result.Lints, nil)) != len(result.Lints) {
		t.Error("an empty suppression list changed the diagnostics")
	}
}

func TestCoverageBelowThreshold(t *testing.T) {
	isolate(t)
	result := &CheckResult{
		Coverage: &CoverageStats{Total: 4, ReferencedCount: 4, DocumentedCount: 2},
	}
	if !CoverageBelowThreshold(result, nil) {
		t.Error("half coverage passed the default threshold")
	}
	if CoverageBelowThreshold(result, map[string]any{"coverage_threshold": 0.5}) {
		t.Error("half coverage failed a threshold of one half")
	}
	if CoverageBelowThreshold(&CheckResult{}, nil) {
		t.Error("a run that measured no coverage was reported below threshold")
	}
}

// codelessProjectWithDescription writes a project declaring no source whose
// selfdoc.json carries the given project description.
func codelessProjectWithDescription(t *testing.T, description string) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	projectConfig := configForSource()
	if description != "" {
		projectConfig["description"] = description
	}
	writeConfig(t, root, projectConfig)
	write(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\ndescription = \"A home page whose description is long enough to keep "+
			"the description rules quiet in this fixture.\"\n+++\n# Home\n\nWelcome.\n")
	return root
}

// TestTheProjectDescriptionIsNotJudgedAgainstATitleForm holds the ruling that
// a page's title is written text: nothing composes one out of the project
// description, so the description's shape decides nothing and no lint
// measures it against a form.
func TestTheProjectDescriptionIsNotJudgedAgainstATitleForm(t *testing.T) {
	registry, err := lints.Load()
	if err != nil {
		t.Fatalf("lints.Load: %v", err)
	}
	if registry.Has("SEO016") {
		t.Error("the registry still declares a lint on the description's form")
	}
	for _, description := range []string{
		"Builds documentation sites.",
		"",
		"Static site generator that builds documentation from source code.",
	} {
		root := codelessProjectWithDescription(t, description)
		result := checkFixture(t, root)
		for _, lint := range result.Lints {
			if lint.File() == "selfdoc.json" &&
				strings.Contains(lint.Message(), " that ") {
				t.Errorf("description %q drew %s: %q",
					description, lint.Code(), lint.Message())
			}
		}
	}
}
