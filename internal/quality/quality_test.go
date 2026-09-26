package quality

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
)

// writeFile puts content at a path relative to root, making the directories it
// needs.
func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("writing %s: %v", relative, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", relative, err)
	}
}

// project writes a tree with Markdown, test code, production code, a
// submodule and a selfdoc.json, and returns its root.
func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "README.md", "one\ntwo\nthree\n")
	writeFile(t, root, "CHANGELOG.md", "generated\nand\nnot\ndocumentation\n")
	writeFile(t, root, "docs/guide.md", "a\nb\n")
	writeFile(t, root, "docs/_README.md", "template\nprose\nthat\nis\ncounted\nat\nthe\nroot\n")
	writeFile(t, root, "docs/_build/stale.md", "output\nnot\nsource\n")
	writeFile(t, root, "todo/plan.md", "planning\nnotes\nare\nnot\ndocs\n")
	writeFile(t, root, "node_modules/dep/readme.md", "somebody\nelse\n")
	writeFile(t, root, "vendor-theme/theme.md", "a\nsubmodule\n")
	writeFile(t, root, ".gitmodules", "[submodule \"theme\"]\n\tpath = vendor-theme\n\turl = https://x/y\n")
	writeFile(t, root, "app.py", "print(1)\n")
	writeFile(t, root, "tests/test_app.py", "one\ntwo\nthree\nfour\n")
	writeFile(t, root, "engine_test.go", "one\ntwo\n")
	writeFile(t, root, "conftest.py", "x\n")
	writeFile(t, root, "web/app.spec.tsx", "a\nb\nc\n")
	writeFile(t, root, "Makefile", "all:\n\techo\n")
	return root
}

func TestMarkdownLOCCountsWhatIsDocumentation(t *testing.T) {
	root := project(t)

	// The template is skipped because its generated output at the root is
	// counted instead: README.md (3) plus docs/guide.md (2).
	// README.md (3), docs/guide.md (2) and docs/_build/stale.md (3): the
	// changelog, the planning notes, the dependency tree and the submodule are
	// all stepped over, and the template is skipped because its generated
	// output at the root is counted instead.
	loc, files := MarkdownLOC(root, SubmodulePaths(root), []string{"docs/_README.md"})
	if loc != 8 || files != 3 {
		t.Errorf("MarkdownLOC = (%d, %d), want (8, 3)", loc, files)
	}
}

func TestMarkdownLOCCountsTheTemplateWhenNothingDeclaresIt(t *testing.T) {
	root := project(t)
	loc, files := MarkdownLOC(root, SubmodulePaths(root), nil)
	if loc != 16 || files != 4 {
		t.Errorf("MarkdownLOC = (%d, %d), want (16, 4)", loc, files)
	}
}

func TestMarkdownLOCCountsTheBuildOutputItFinds(t *testing.T) {
	// The docs walk skips _build; the Markdown walk has never had a reason to,
	// and this pins which of the two does what.
	root := t.TempDir()
	writeFile(t, root, "docs/_build/page.md", "a\nb\n")
	loc, files := MarkdownLOC(root, nil, nil)
	if loc != 2 || files != 1 {
		t.Errorf("MarkdownLOC = (%d, %d), want (2, 1)", loc, files)
	}
}

func TestSubmodulePathsReadsTheDeclarations(t *testing.T) {
	root := project(t)
	paths := SubmodulePaths(root)
	if len(paths) != 1 || paths[0] != "vendor-theme" {
		t.Errorf("SubmodulePaths = %v, want [vendor-theme]", paths)
	}
}

func TestSubmodulePathsIsEmptyWithoutGitmodules(t *testing.T) {
	if paths := SubmodulePaths(t.TempDir()); len(paths) != 0 {
		t.Errorf("SubmodulePaths = %v, want none", paths)
	}
}

func TestTestLOCCountsOnlyTestCode(t *testing.T) {
	root := project(t)

	// tests/test_app.py (4) + engine_test.go (2) + conftest.py (1)
	// + web/app.spec.tsx (3). app.py and Makefile are production source.
	if got := TestLOC(root, SubmodulePaths(root)); got != 10 {
		t.Errorf("TestLOC = %d, want 10", got)
	}
}

func TestTestLOCStepsOverTheSubmodule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitmodules", "\tpath = vendor-theme\n")
	writeFile(t, root, "vendor-theme/tests/test_theme.py", "a\nb\nc\n")
	writeFile(t, root, "tests/test_own.py", "a\n")

	if got := TestLOC(root, SubmodulePaths(root)); got != 1 {
		t.Errorf("TestLOC = %d, want 1 -- the submodule's tests are not ours", got)
	}
}

func TestExtensionOfReadsTheKindThePythonExpressionDid(t *testing.T) {
	cases := map[string]string{
		"app.py":       "py",
		"Makefile":     "makefile",
		"Dockerfile":   "dockerfile",
		"archive.TAR":  "tar",
		"a.tar.gz":     "gz",
		".bashrc":      "bashrc",
		"app.spec.tsx": "tsx",
	}
	for name, want := range cases {
		if got := extensionOf(name); got != want {
			t.Errorf("extensionOf(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSelfdocInfoReadsAdoption(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "selfdoc.json", `{
		"root_files": ["docs/_README.md", "docs/_CLAUDE.md"],
		"directives": {"one": {}, "two": {}},
		"posts": {"dir": "posts"},
		"docs": "documentation"
	}`)
	writeFile(t, root, "documentation/page.md", "text\n:-: version\nmore\n:<: block\n:>:\n")
	writeFile(t, root, "documentation/_build/page.md", ":-: version\n")

	info := SelfdocInfo(root)
	if !info.HasSelfdoc || !info.AutoREADME || !info.AutoCLAUDE || !info.HasPosts {
		t.Errorf("adoption = %+v, want every flag set", info)
	}
	if info.CustomDirectives != 2 {
		t.Errorf("CustomDirectives = %d, want 2", info.CustomDirectives)
	}
	// Three marker lines, and the build output is not one of them.
	if info.DirectiveCount != 3 {
		t.Errorf("DirectiveCount = %d, want 3", info.DirectiveCount)
	}
}

func TestSelfdocInfoWithoutAConfig(t *testing.T) {
	cases := map[string]func(t *testing.T) string{
		"no selfdoc.json at all": func(t *testing.T) string { return t.TempDir() },
		"a selfdoc.json that does not parse": func(t *testing.T) string {
			root := t.TempDir()
			writeFile(t, root, "selfdoc.json", "{not json")
			return root
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if info := SelfdocInfo(build(t)); info.HasSelfdoc {
				t.Errorf("adoption = %+v, want HasSelfdoc false", info)
			}
		})
	}
}

func TestSelfdocInfoDefaultsTheDocsDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "selfdoc.json", `{"root_files": ["stricttools/docs/_README.md"]}`)
	writeFile(t, root, "stricttools/docs/page.md", ":-: version\n")

	info := SelfdocInfo(root)
	if info.DirectiveCount != 1 {
		t.Errorf("DirectiveCount = %d, want 1", info.DirectiveCount)
	}
}

func TestComputeTierClimbsTheLadderInOrder(t *testing.T) {
	cases := []struct {
		name   string
		docLOC int
		info   Adoption
		want   int
	}{
		{"no markdown at all", 0, Adoption{HasSelfdoc: true, AutoREADME: true, DirectiveCount: 9, HasPosts: true}, 0},
		{"markdown only", 10, Adoption{}, 1},
		{"selfdoc.json but no template", 10, Adoption{HasSelfdoc: true}, 2},
		{"a template but no directives", 10, Adoption{HasSelfdoc: true, AutoREADME: true}, 3},
		{"directives but nothing advanced", 10, Adoption{HasSelfdoc: true, AutoREADME: true, DirectiveCount: 1}, 4},
		{"custom directives", 10, Adoption{HasSelfdoc: true, AutoREADME: true, DirectiveCount: 1, CustomDirectives: 1}, 5},
		{"blog posts", 10, Adoption{HasSelfdoc: true, AutoREADME: true, DirectiveCount: 1, HasPosts: true}, 5},
	}
	for _, testCase := range cases {
		if got := ComputeTier(testCase.docLOC, testCase.info); got != testCase.want {
			t.Errorf("%s: ComputeTier = %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestContentGradeCutOffs(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{0.50, "A"}, {0.30, "A"}, {0.2999, "B"}, {0.15, "B"}, {0.1499, "C"},
		{0.05, "C"}, {0.0499, "D"}, {0.01, "D"}, {0.0099, "F"}, {0, "F"},
	}
	for _, testCase := range cases {
		ratio := testCase.ratio
		if got := ContentGrade(&ratio); got != testCase.want {
			t.Errorf("ContentGrade(%v) = %q, want %q", ratio, got, testCase.want)
		}
	}
	if got := ContentGrade(nil); got != "-" {
		t.Errorf("ContentGrade(nil) = %q, want \"-\" -- no source is not failing", got)
	}
}

func TestRound4RoundsHalfToEvenAsPythonDoes(t *testing.T) {
	cases := map[float64]float64{
		1.0 / 3.0:  0.3333,
		0.03125:    0.0312,
		2.0 / 3.0:  0.6667,
		45.0 / 900: 0.05,
	}
	for value, want := range cases {
		if got := round4(value); got != want {
			t.Errorf("round4(%v) = %v, want %v", value, got, want)
		}
	}
}

func TestCommaGroupsThousands(t *testing.T) {
	cases := map[int]string{
		0: "0", 42: "42", 999: "999", 1000: "1,000", 100000: "100,000",
		1234567: "1,234,567", -1234: "-1,234",
	}
	for value, want := range cases {
		if got := comma(value); got != want {
			t.Errorf("comma(%d) = %q, want %q", value, got, want)
		}
	}
}

// scoringDirstat writes a fake dirstat answering one code total for a scan of
// root, and an empty scan for anything else -- a submodule subtraction
// included, so the score under test rests on a known source figure.
func scoringDirstat(t *testing.T, bin, root string, loc, files int) {
	t.Helper()
	outer := envelope(`{"groups":[{"format":"py","count":` +
		strconv.Itoa(files) + `,"total_loc":` + strconv.Itoa(loc) + `}]}`)
	inner := envelope(`{"groups":[]}`)
	fakeDirstat(t, bin, `case "$2" in
`+root+`) cat <<'OUTER'
`+outer+`
OUTER
;;
*) cat <<'INNER'
`+inner+`
INNER
;;
esac`)
}

func TestScoreProjectCombinesEveryCounter(t *testing.T) {
	bin := isolate(t)
	root := project(t)
	writeFile(t, root, "selfdoc.json", `{"root_files": ["docs/_README.md"]}`)
	scoringDirstat(t, bin, root, 1000, 20)

	result, err := ScoreProject(root, effects.Unbound())
	if err != nil {
		t.Fatalf("ScoreProject: %v", err)
	}
	if result.Path != root || result.Project != filepath.Base(root) {
		t.Errorf("project/path = %q/%q, want %q under %q", result.Project, result.Path, filepath.Base(root), root)
	}
	if result.CodeLOC != 1000 {
		t.Errorf("CodeLOC = %d, want 1000", result.CodeLOC)
	}
	if result.TestLOC != 10 {
		t.Errorf("TestLOC = %d, want 10", result.TestLOC)
	}
	if result.SourceLOC != 990 {
		t.Errorf("SourceLOC = %d, want 990", result.SourceLOC)
	}
	if result.DocLOC != 8 || result.DocFiles != 3 {
		t.Errorf("doc = (%d, %d), want (8, 3)", result.DocLOC, result.DocFiles)
	}
	if result.DocRatio == nil || *result.DocRatio != 0.0081 {
		t.Errorf("DocRatio = %v, want 0.0081", result.DocRatio)
	}
	if result.ContentGrade != "F" {
		t.Errorf("ContentGrade = %q, want \"F\"", result.ContentGrade)
	}
	if result.Tier != 3 || result.TierName != "Templates" {
		t.Errorf("tier = %d (%q), want 3 (Templates)", result.Tier, result.TierName)
	}
	if result.NextStep != NextSteps[3] {
		t.Errorf("NextStep = %q, want %q", result.NextStep, NextSteps[3])
	}
}

func TestScoreProjectWithNoSourceHasNoRatio(t *testing.T) {
	bin := isolate(t)
	root := t.TempDir()
	writeFile(t, root, "README.md", "a\n")
	scoringDirstat(t, bin, root, 0, 0)

	result, err := ScoreProject(root, effects.Unbound())
	if err != nil {
		t.Fatalf("ScoreProject: %v", err)
	}
	if result.DocRatio != nil {
		t.Errorf("DocRatio = %v, want none", *result.DocRatio)
	}
	if result.ContentGrade != "-" {
		t.Errorf("ContentGrade = %q, want \"-\"", result.ContentGrade)
	}
	if result.SourceLOC != 0 {
		t.Errorf("SourceLOC = %d, want 0", result.SourceLOC)
	}
}

func TestScoreProjectFloorsSourceAtZero(t *testing.T) {
	// A tree whose tests outweigh dirstat's total (submodules subtracted) must
	// not report negative source.
	bin := isolate(t)
	root := t.TempDir()
	writeFile(t, root, "tests/test_all.py", "a\nb\nc\nd\ne\n")
	scoringDirstat(t, bin, root, 2, 1)

	result, err := ScoreProject(root, effects.Unbound())
	if err != nil {
		t.Fatalf("ScoreProject: %v", err)
	}
	if result.SourceLOC != 0 || result.CodeLOC != 2 || result.TestLOC != 5 {
		t.Errorf("source/code/test = %d/%d/%d, want 0/2/5",
			result.SourceLOC, result.CodeLOC, result.TestLOC)
	}
}

func TestRunRefusesWithoutDirstat(t *testing.T) {
	isolateWithoutTools(t)
	_, err := Run(t.TempDir(), effects.Unbound())
	if err == nil {
		t.Fatal("Run scored a project with no dirstat installed")
	}
	if _, ok := err.(*DirstatMissingError); !ok {
		t.Fatalf("error = %v (%T), want *quality.DirstatMissingError", err, err)
	}
}

func TestRunScoresTheDirectoryAsAnAbsolutePath(t *testing.T) {
	bin := isolate(t)
	root := project(t)
	scoringDirstat(t, bin, root, 500, 5)

	result, err := Run(root, effects.Unbound())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !filepath.IsAbs(result.Path) {
		t.Errorf("Path = %q, want an absolute path", result.Path)
	}
	if result.Project == "" {
		t.Error("Project is empty, want the directory's own name")
	}
}

func TestThePayloadCarriesTheDeclaredMembers(t *testing.T) {
	ratio := 0.25
	result := Result{
		Project: "selfdoc", Path: "/p/selfdoc", Tier: 5, TierName: "Advanced",
		CodeLOC: 10, TestLOC: 2, SourceLOC: 8, DocLOC: 2, DocFiles: 1,
		DocRatio: &ratio, ContentGrade: "B",
		Selfdoc: Adoption{
			HasSelfdoc: true, AutoREADME: true, AutoCLAUDE: false,
			CustomDirectives: 3, HasPosts: true, DirectiveCount: 9,
		},
	}

	payload := result.Payload()
	declared := []string{
		"project", "path", "tier", "tier_name", "code_loc", "test_loc",
		"source_loc", "doc_loc", "doc_files", "doc_ratio", "content_grade",
		"selfdoc", "next_step",
	}
	for _, member := range declared {
		if _, ok := payload[member]; !ok {
			t.Errorf("the payload does not carry %q", member)
		}
	}
	if len(payload) != len(declared) {
		t.Errorf("the payload carries %d members, want %d", len(payload), len(declared))
	}
	if payload["next_step"] != nil {
		t.Errorf("next_step = %v, want null at tier 5", payload["next_step"])
	}
	adoption, ok := payload["selfdoc"].(map[string]any)
	if !ok {
		t.Fatalf("selfdoc = %T, want a nested object", payload["selfdoc"])
	}
	for _, member := range []string{
		"has_selfdoc", "auto_readme", "auto_claude", "custom_directives",
		"has_posts", "directive_count",
	} {
		if _, ok := adoption[member]; !ok {
			t.Errorf("the adoption block does not carry %q", member)
		}
	}
}

func TestThePayloadOfAProjectWithoutSelfdocCarriesOneFlag(t *testing.T) {
	payload := Result{Tier: 0, TierName: "None", NextStep: NextSteps[0]}.Payload()
	adoption, ok := payload["selfdoc"].(map[string]any)
	if !ok {
		t.Fatalf("selfdoc = %T, want a nested object", payload["selfdoc"])
	}
	if len(adoption) != 1 || adoption["has_selfdoc"] != false {
		t.Errorf("adoption = %v, want only has_selfdoc false", adoption)
	}
	if payload["doc_ratio"] != nil {
		t.Errorf("doc_ratio = %v, want null", payload["doc_ratio"])
	}
	if payload["next_step"] != NextSteps[0] {
		t.Errorf("next_step = %v, want the tier 0 action", payload["next_step"])
	}
}

func TestCountersStepOverTheScratchDirectoriesAtTheRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tests/test_own.py", "a\n")
	writeFile(t, root, "experiments/tests/test_probe.py", "a\nb\nc\n")
	writeFile(t, root, "screenshots/notes_test.go", "a\nb\n")
	writeFile(t, root, "guide.md", "one\n")
	writeFile(t, root, "experiments/findings.md", "a\nb\nc\n")
	writeFile(t, root, "screenshots/notes.md", "a\nb\n")
	// A directory of the same name below the root is part of the project.
	writeFile(t, root, "lib/experiments/tests/test_lib.py", "a\n")

	if got := TestLOC(root, SubmodulePaths(root)); got != 2 {
		t.Errorf("TestLOC = %d, want 2 -- scratch directories are not the project's tests", got)
	}
	if lines, files := MarkdownLOC(root, SubmodulePaths(root), nil); lines != 1 || files != 1 {
		t.Errorf("MarkdownLOC = %d lines in %d files, want 1 in 1 -- scratch directories are not documentation", lines, files)
	}
}
