package check

import (
	"bytes"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
)

// pythonReferenceReport is what the Python surface's print_results wrote for
// the result built below, captured from it with colour off.
//
// The report is a user-visible artifact -- it is what a reader sees and what
// scripts scrape -- so the port is held to it byte for byte rather than to a
// description of it.
//
// One section departs from the Python on purpose: the skeleton-only block now
// names the pages whose seeded descriptions are the cause and says the symbols'
// doc comments are not, because the bare symbol list the Python wrote sent
// readers to rewrite doc comments that were already complete.
const pythonReferenceReport = "Directives\n" +
	"  a.md:3  ref path=\"mylib\"  OK\n" +
	"  b.md:7  ref path=\"nope\"  FAILED: boom\n" +
	"\n" +
	"2 directive(s): 1 OK, 1 FAILED\n" +
	"Coverage: 1/4 symbols documented (25%)\n" +
	"          3/4 symbols referenced (75%)\n" +
	"Unreferenced symbols:\n" +
	"  n.py: four\n" +
	"Skeleton-only symbols:\n" +
	"  Each is named only on a generated page whose frontmatter still declares\n" +
	"  seeded = true, so that page's description is the one selfdoc emitted and\n" +
	"  nobody has rewritten. The symbols' own doc comments are not the cause and\n" +
	"  rewriting them changes nothing here: edit each page's frontmatter\n" +
	"  description instead.\n" +
	"  Pages whose description to edit:\n" +
	"    m.md\n" +
	"    n.md\n" +
	"  Symbols they leave undocumented:\n" +
	"  m.py: two\n" +
	"  n.py: three\n" +
	"\n" +
	"Lints\n" +
	"  error: [SEO006] a.md - No 'description' in frontmatter\n" +
	"  warning: [SEO002] b.md:12 - Heading level jumps from H2 to H4 (skips H3)\n"

// referenceResult is the result pythonReferenceReport was captured from.
func referenceResult() *CheckResult {
	line12 := 12
	return &CheckResult{
		DirectiveResults: []DirectiveResult{
			{File: "a.md", Line: 3, Directive: `ref path="mylib"`, Outcome: StatusOK},
			{
				File: "b.md", Line: 7, Directive: `ref path="nope"`,
				Outcome: StatusFailed, Error: "boom",
			},
		},
		Coverage: &CoverageStats{
			Total: 4, ReferencedCount: 3, DocumentedCount: 1,
			ReferencedSymbols:   []string{"m.py:one", "m.py:two", "n.py:three"},
			DocumentedSymbols:   []string{"m.py:one"},
			UnreferencedSymbols: []string{"n.py:four"},
			SkeletonPagesBySymbol: map[string]string{
				"m.py:two":   "m.md",
				"n.py:three": "n.md",
			},
		},
		Lints: []lints.LintResult{
			lints.MustLintResult("a.md", nil, "SEO006",
				"No 'description' in frontmatter"),
			lints.MustLintResult("b.md", &line12, "SEO002",
				"Heading level jumps from H2 to H4 (skips H3)"),
		},
	}
}

func TestPrintResultsMatchesTheReferenceReport(t *testing.T) {
	var out bytes.Buffer
	PrintResults(&out, referenceResult(), false)
	if out.String() != pythonReferenceReport {
		t.Errorf("report =\n%q\nwant\n%q", out.String(), pythonReferenceReport)
	}
}

func TestPrintResultsCoverageWithNoPublicSymbols(t *testing.T) {
	result := &CheckResult{
		DirectiveResults: []DirectiveResult{
			{File: "a.md", Line: 1, Directive: "ref", Outcome: StatusOK},
		},
		Coverage: &CoverageStats{},
	}
	var out bytes.Buffer
	PrintResults(&out, result, false)
	want := "Directives\n" +
		"  a.md:1  ref  OK\n" +
		"\n1 directive(s): 1 OK, 0 FAILED\n" +
		"Coverage: no public symbols found in source files\n" +
		"No lints.\n"
	if out.String() != want {
		t.Errorf("report =\n%q\nwant\n%q", out.String(), want)
	}
}

func TestSerializeCheckResultMatchesTheDeclaredShape(t *testing.T) {
	payload := SerializeCheckResult(referenceResult(), 1)

	directives, _ := payload["directives"].([]any)
	if len(directives) != 2 {
		t.Fatalf("directives = %v", payload["directives"])
	}
	first, _ := directives[0].(map[string]any)
	for key, want := range map[string]any{
		"file": "a.md", "line": 3, "directive": `ref path="mylib"`,
		"status": "OK", "error": "",
	} {
		if first[key] != want {
			t.Errorf("directives[0][%q] = %v, want %v", key, first[key], want)
		}
	}

	diagnostics, _ := payload["lints"].([]any)
	if len(diagnostics) != 2 {
		t.Fatalf("lints = %v", payload["lints"])
	}
	unlined, _ := diagnostics[0].(map[string]any)
	if unlined["line"] != nil {
		t.Errorf("a file-level diagnostic carries line %v, want null", unlined["line"])
	}
	if unlined["severity"] != "error" {
		t.Errorf("severity = %v, want error", unlined["severity"])
	}
	lined, _ := diagnostics[1].(map[string]any)
	if lined["line"] != 12 {
		t.Errorf("line = %v, want 12", lined["line"])
	}

	coverage, _ := payload["coverage"].(map[string]any)
	for key, want := range map[string]any{
		"total_public": 4, "referenced": 3, "documented": 1,
	} {
		if coverage[key] != want {
			t.Errorf("coverage[%q] = %v, want %v", key, coverage[key], want)
		}
	}
	referenced, _ := coverage["referenced_symbols"].([]any)
	if len(referenced) != 3 {
		t.Errorf("referenced_symbols = %v", coverage["referenced_symbols"])
	}
}
