package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
)

// lintCase is one lint-rule scenario: a set of pages, optional config
// overrides, and what the rules must and must not say about them.
type lintCase struct {
	// name identifies the subtest.
	name string
	// page is the content of "page.md", the case's only page when pages is
	// empty.
	page string
	// pages are extra pages keyed by their path under the docs directory.
	pages map[string]string
	// config overrides merged over the fixture's config document.
	config map[string]any
	// want are the codes the rules must emit.
	want []string
	// absent are the codes the rules must not emit.
	absent []string
	// resolved are the resolved directives the case hands to the rules.
	resolved []ResolvedDirective
	// assert makes any further assertion on the diagnostics.
	assert func(t *testing.T, fixture lintFixture, results []lints.LintResult)
}

// runLintCases runs a table of lint scenarios.
func runLintCases(t *testing.T, cases []lintCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := lintProject(t)
			for key, value := range testCase.config {
				fixture.Config[key] = value
			}
			if testCase.page != "" {
				write(t, filepath.Join(fixture.DocsDir, "page.md"), testCase.page)
			}
			for relPath, content := range testCase.pages {
				write(t, filepath.Join(fixture.DocsDir, relPath), content)
			}
			results := runLintsOn(t, fixture, testCase.resolved)
			for _, code := range testCase.want {
				if !hasCode(results, code) {
					t.Errorf("%s missing; got %v", code, codes(results))
				}
			}
			for _, code := range testCase.absent {
				if hasCode(results, code) {
					t.Errorf("%s fired but should not have; got %v", code, codes(results))
				}
			}
			if testCase.assert != nil {
				testCase.assert(t, fixture, results)
			}
		})
	}
}

// onlyMessage returns the single diagnostic of a code, failing when there is
// not exactly one.
func onlyMessage(t *testing.T, results []lints.LintResult, code string) lints.LintResult {
	t.Helper()
	matching := withCode(results, code)
	if len(matching) != 1 {
		t.Fatalf("%s fired %d times, want 1: %v", code, len(matching), codes(results))
	}
	return matching[0]
}

// repeatWords builds a paragraph of count words.
func repeatWords(word string, count int) string {
	parts := make([]string, count)
	for index := range parts {
		parts[index] = word
	}
	return strings.Join(parts, " ")
}

func TestSEOStructureRules(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "SEO001 two H1 headings",
			page: "+++\ndescription = \"test\"\n+++\n" +
				"# First Title\n\nSome text.\n\n# Second Title\n\nMore text.\n",
			want: []string{"SEO001"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "SEO001")
				if diagnostic.Severity() != "error" {
					t.Errorf("severity = %q, want error", diagnostic.Severity())
				}
				if !strings.Contains(diagnostic.Message(), "2 found") {
					t.Errorf("message = %q", diagnostic.Message())
				}
			},
		},
		{
			name:   "SEO001 single H1 is silent",
			page:   "+++\ndescription = \"test\"\n+++\n# Only Title\n\nSome text.\n",
			absent: []string{"SEO001"},
		},
		{
			name: "SEO001 does not count a directive as a heading",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				":::cli app.commands\n:::\n\nText.\n",
			absent: []string{"SEO001"},
		},
		{
			name: "SEO002 heading level gap",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\n" +
				"Text.\n\n#### Deep\n\nMore.\n",
			want: []string{"SEO002"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "SEO002")
				if !strings.Contains(diagnostic.Message(), "H2 to H4") {
					t.Errorf("message = %q", diagnostic.Message())
				}
				if diagnostic.Line() == nil {
					t.Error("a heading-gap diagnostic carries no line")
				}
			},
		},
		{
			name: "SEO002 no gap is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\n" +
				"Text.\n\n### Sub\n\nMore.\n",
			absent: []string{"SEO002"},
		},
		{
			name: "SEO003 empty alt text",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"Some text.\n\n![](image.png)\n",
			want: []string{"SEO003"},
		},
		{
			name: "SEO003 alt text present is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"Some text.\n\n![A descriptive caption](image.png)\n",
			absent: []string{"SEO003"},
		},
		{
			name: "SEO013 no title source at all",
			page: "+++\ndescription = \"test\"\n+++\nJust a paragraph with no heading.\n",
			want: []string{"SEO013"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if onlyMessage(t, results, "SEO013").Severity() != "error" {
					t.Error("SEO013 is not error-severity")
				}
			},
		},
		{
			name:   "SEO013 an H1 is a title source",
			page:   "+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n",
			absent: []string{"SEO013"},
		},
		{
			name:   "SEO013 a frontmatter title is a title source",
			page:   "+++\ntitle = \"A Title\"\ndescription = \"test\"\n+++\nText with no heading.\n",
			absent: []string{"SEO013"},
		},
		{
			name: "SEO011 an H2 followed by an H2",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## First\n\n## Second\n\nText.\n",
			want: []string{"SEO011"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "SEO011")
				if !strings.Contains(diagnostic.Message(), "H2 heading has no content") {
					t.Errorf("message = %q", diagnostic.Message())
				}
			},
		},
		{
			name: "SEO011 an H3 followed by an H2",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Top\n\nText.\n\n" +
				"### Sub\n\n## Next\n\nMore.\n",
			want: []string{"SEO011"},
		},
		{
			name: "SEO011 content between headings is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## First\n\nText.\n\n" +
				"## Second\n\nMore.\n",
			absent: []string{"SEO011"},
		},
		{
			name:   "SEO011 an H2 followed by an H3 is silent",
			page:   "+++\ndescription = \"test\"\n+++\n# Title\n\n## Top\n\n### Sub\n\nText.\n",
			absent: []string{"SEO011"},
		},
	})
}

func TestSEOTitleAndDescriptionRules(t *testing.T) {
	longTitle := "A Very Long Documentation Page Title That Exceeds All Limits"
	runLintCases(t, []lintCase{
		{
			name: "SEO004 frontmatter title too long",
			page: "+++\ntitle = \"" + longTitle + "\"\ndescription = \"test\"\n+++\n" +
				"# Heading\n\nText.\n",
			want: []string{"SEO004"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(onlyMessage(t, results, "SEO004").Message(), "chars") {
					t.Error("SEO004 does not report the length")
				}
			},
		},
		{
			name:   "SEO004 short title is silent",
			page:   "+++\ntitle = \"Short\"\ndescription = \"test\"\n+++\n# Heading\n\nText.\n",
			absent: []string{"SEO004"},
		},
		{
			name:   "SEO004 a long H1 with no frontmatter title still fires",
			page:   "+++\ndescription = \"test\"\n+++\n# " + longTitle + "\n\nText.\n",
			want:   []string{"SEO004"},
			absent: []string{"SEO013"},
		},
		{
			name: "SEO006 missing description",
			page: "# Title\n\nText.\n",
			want: []string{"SEO006"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if onlyMessage(t, results, "SEO006").Severity() != "error" {
					t.Error("SEO006 is not error-severity")
				}
			},
		},
		{
			name:   "SEO006 a description is silent",
			page:   "+++\ndescription = \"A description of the page.\"\n+++\n# Title\n\nText.\n",
			absent: []string{"SEO006"},
		},
		{
			name:   "SEO009 a short description",
			page:   "+++\ndescription = \"Too short.\"\n+++\n# Title\n\nText.\n",
			want:   []string{"SEO009"},
			absent: []string{"SEO010"},
		},
		{
			name: "SEO009 a description in the band is silent",
			page: "+++\ndescription = \"" + repeatWords("word", 30) +
				"\"\n+++\n# Title\n\nText.\n",
			absent: []string{"SEO009", "SEO010"},
		},
		{
			name: "SEO010 a description over the ceiling",
			page: "+++\ndescription = \"" + strings.Repeat("x", 200) +
				"\"\n+++\n# Title\n\nText.\n",
			want: []string{"SEO010"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(onlyMessage(t, results, "SEO010").Message(), "200") {
					t.Error("SEO010 does not report the length")
				}
			},
		},
		{
			name: "SEO009 reads the whole first sentence, not the first line",
			page: "# Title\n\n" +
				"This first sentence is deliberately soft-wrapped across two\n" +
				"physical lines so that it comfortably exceeds one hundred and twenty characters in total length.\n\n" +
				"More text.\n",
			absent: []string{"SEO009"},
			want:   []string{"SEO006"},
		},
		{
			name: "SEO009 a short auto-extracted sentence fires",
			page: "# Title\n\nShort sentence.\n\nMore text.\n",
			want: []string{"SEO009", "SEO006"},
		},
		{
			name:   "SEO009 no description and no paragraph is silent",
			page:   "# Title\n\n## Section\n\n" + repeatWords("word", 50) + "\n",
			absent: []string{"SEO009"},
		},
		{
			name: "SEO009 a description just under the floor",
			page: "+++\ndescription = \"" + strings.Repeat("x", 109) +
				"\"\n+++\n# Title\n\nText.\n",
			want: []string{"SEO009"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				message := onlyMessage(t, results, "SEO009").Message()
				if !strings.Contains(message, "110") ||
					!strings.Contains(message, "160") {
					t.Errorf("SEO009 does not name the band: %q", message)
				}
			},
		},
		{
			name: "SEO009 a description between the old floor and the new one is silent",
			page: "+++\ndescription = \"" + strings.Repeat("x", 115) +
				"\"\n+++\n# Title\n\nText.\n",
			absent: []string{"SEO009", "SEO010"},
		},
		{
			name: "SEO010 a description between the old ceiling and the new one is silent",
			page: "+++\ndescription = \"" + strings.Repeat("x", 158) +
				"\"\n+++\n# Title\n\nText.\n",
			absent: []string{"SEO009", "SEO010"},
		},
		{
			name: "SEO010 a description just over the ceiling",
			page: "+++\ndescription = \"" + strings.Repeat("x", 161) +
				"\"\n+++\n# Title\n\nText.\n",
			want: []string{"SEO010"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				message := onlyMessage(t, results, "SEO010").Message()
				if !strings.Contains(message, "160") {
					t.Errorf("SEO010 does not name the ceiling: %q", message)
				}
			},
		},
	})
}

func TestSEO007ParagraphBand(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "a short paragraph after a heading",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\n" +
				"This is short.\n\nMore content here.\n",
			want: []string{"SEO007"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "SEO007")
				if diagnostic.Severity() != "warning" {
					t.Errorf("severity = %q, want warning", diagnostic.Severity())
				}
				for _, fragment := range []string{"3 words", "Section", "30-80"} {
					if !strings.Contains(diagnostic.Message(), fragment) {
						t.Errorf("message %q does not carry %q",
							diagnostic.Message(), fragment)
					}
				}
				if strings.Contains(diagnostic.Message(), "40-60") {
					t.Error("the message quotes a band the rule does not enforce")
				}
			},
		},
		{
			name: "a paragraph inside the band is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\n" +
				repeatWords("word", 50) + "\n\nMore content.\n",
			absent: []string{"SEO007"},
		},
		{
			name: "a directive straight after the heading suppresses it",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## release\n\n" +
				":::cli app.commands.release\n:::\n",
			absent: []string{"SEO007"},
		},
		{
			name: "a directive after a short paragraph suppresses it",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## release\n\n" +
				"Orchestrate a release: bump version, validate changelog.\n\n" +
				":::cli app.commands.release\n:::\n",
			absent: []string{"SEO007"},
		},
		{
			name: "without a directive it still fires",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\n" +
				"Short intro text only.\n\nSome other paragraph.\n",
			want: []string{"SEO007"},
		},
		{
			name: "a generated CLI page gets no exemption",
			pages: map[string]string{
				"cli-build.md": "+++\ndescription = \"test\"\ngenerated = true\n+++\n" +
					"# build\n\n## Flags\n\nShort text.\n\nMore.\n",
			},
			want: []string{"SEO007"},
		},
		{
			name: "the directive exemption holds on a generated page",
			pages: map[string]string{
				"cli-build.md": "+++\ndescription = \"test\"\ngenerated = true\n+++\n" +
					"# build\n\n## Flags\n\n:::cli app.commands.build\n:::\n",
			},
			absent: []string{"SEO007"},
		},
	})
}

func TestSEO008StatisticsDensity(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "a long page with no numbers",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 250) + "\n",
			want: []string{"SEO008"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "SEO008")
				if !strings.Contains(diagnostic.Message(), "250 words") {
					t.Errorf("message = %q", diagnostic.Message())
				}
			},
		},
		{
			name: "a long page with numbers is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 250) + " 42 87% 12ms\n",
			absent: []string{"SEO008"},
		},
		{
			name: "a short page is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 50) + "\n",
			absent: []string{"SEO008"},
		},
		{
			name: "1000 words with too few numbers",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 1000) + " 42\n",
			want: []string{"SEO008"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "SEO008").Message(), "at least 5",
				) {
					t.Errorf("message = %q",
						onlyMessage(t, results, "SEO008").Message())
				}
			},
		},
		{
			name: "1000 words with enough numbers is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 1000) + " 1 2 3 4 5\n",
			absent: []string{"SEO008"},
		},
		{
			name: "version strings and years are not statistics",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 250) + " v0.36.0 0.36.0 2026 (1999)\n",
			want: []string{"SEO008"},
		},
		{
			name: "genuine quantities still count",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 250) + " 3.5 **42** 87%\n",
			absent: []string{"SEO008"},
		},
	})
}

func TestCountsAsStatistic(t *testing.T) {
	rejected := []string{
		"word", "the", "v2", "v1.5", "v0.36.0", "0.36.0", "1.0.0-alpha.1",
		"2.11.3+build.7", "(0.36.0)", "2026", "1999.", "**2026**", "V3.0.0",
		"", "-", "...",
	}
	accepted := []string{
		"42", "3.5", "87%", "12ms", "**42**", "1899", "2100", "0",
		"2026-08-11", "1,024", "x2",
	}
	for _, token := range rejected {
		if CountsAsStatistic(token) {
			t.Errorf("CountsAsStatistic(%q) = true, want false", token)
		}
	}
	for _, token := range accepted {
		if !CountsAsStatistic(token) {
			t.Errorf("CountsAsStatistic(%q) = false, want true", token)
		}
	}
}

func TestSEOAltAndAnchorRules(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name:   "SEO014 a medium word as alt text",
			page:   "+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n\n![screenshot](a.png)\n",
			want:   []string{"SEO014"},
			absent: []string{"SEO003"},
		},
		{
			name: "SEO014 a filename as alt text",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n\n![diagram.png](a.png)\n",
			want: []string{"SEO014"},
		},
		{
			name: "SEO014 a single character as alt text",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n\n![x](a.png)\n",
			want: []string{"SEO014"},
		},
		{
			name: "SEO014 descriptive alt text is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n\n" +
				"![The build pipeline, from templates to output](a.png)\n",
			absent: []string{"SEO014"},
		},
		{
			name: "SEO015 generic anchor text",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"For details, [click here](https://example.com).\n",
			want: []string{"SEO015"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "SEO015").Message(), "click here",
				) {
					t.Error("SEO015 does not quote the anchor text")
				}
			},
		},
		{
			name: "SEO015 descriptive anchor text is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"See [the release workflow](https://example.com).\n",
			absent: []string{"SEO015"},
		},
		{
			name: "SEO015 inside a code block is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n[click here](https://example.com)\n```\n",
			absent: []string{"SEO015"},
		},
	})
}

func TestXREF001InternalLinks(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "a link to a page that does not exist",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"See [the guide](guide.md).\n",
			want: []string{"XREF001"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "XREF001").Message(), "guide.md",
				) {
					t.Error("XREF001 does not name the target")
				}
			},
		},
		{
			name: "a link to a page that exists is silent",
			pages: map[string]string{
				"page.md":  "+++\ndescription = \"test\"\n+++\n# Title\n\nSee [the guide](guide.md).\n",
				"guide.md": "+++\ndescription = \"test\"\n+++\n# Guide\n\nText.\n",
			},
			absent: []string{"XREF001"},
		},
		{
			name: "an external link is ignored",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"See [upstream](https://example.com/doc.md).\n",
			absent: []string{"XREF001"},
		},
		{
			name: "a relative link resolves against the page's own directory",
			pages: map[string]string{
				"sub/page.md": "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
					"See [the guide](../guide.md).\n",
				"guide.md": "+++\ndescription = \"test\"\n+++\n# Guide\n\nText.\n",
			},
			absent: []string{"XREF001"},
		},
		{
			name: "an anchor fragment is stripped before resolution",
			pages: map[string]string{
				"page.md":  "+++\ndescription = \"test\"\n+++\n# Title\n\n[Guide](guide.md#usage)\n",
				"guide.md": "+++\ndescription = \"test\"\n+++\n# Guide\n\nText.\n",
			},
			absent: []string{"XREF001"},
		},
	})
}

func TestDescriptionQualityRules(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "DQ001 the description restates the title",
			page: "+++\ntitle = \"Config Module\"\ndescription = \"config module\"\n+++\n" +
				"# Config Module\n\nText.\n",
			want: []string{"DQ001"},
		},
		{
			name: "DQ001 a real description is silent",
			page: "+++\ntitle = \"Config\"\ndescription = \"Loads and validates a " +
				"project's settings document, resolving every path.\"\n+++\n# Config\n\nText.\n",
			absent: []string{"DQ001"},
		},
		{
			name: "DQ001 a kind suffix is stripped before comparing",
			pages: map[string]string{
				"config.md": "+++\ndescription = \"Config module\"\n+++\n# Config\n\nSome content.\n",
			},
			want: []string{"DQ001"},
		},
		{
			name: "DQ001 the description restates the filename when no title exists",
			pages: map[string]string{
				"load_config.md": "+++\ndescription = \"Load config\"\n+++\n# load_config\n\nSome content.\n",
			},
			want: []string{"DQ001"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if onlyMessage(t, results, "DQ001").Severity() != "warning" {
					t.Error("DQ001 is not warning-severity")
				}
			},
		},
		{
			name: "DQ002 a description under twenty characters",
			page: "+++\ndescription = \"Too short\"\n+++\n# Title\n\nText.\n",
			want: []string{"DQ002"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "DQ002").Message(), "minimum 20",
				) {
					t.Error("DQ002 does not state the minimum")
				}
			},
		},
		{
			name: "DQ002 an adequate description is silent",
			page: "+++\ndescription = \"This description is long enough to say something.\"\n+++\n" +
				"# Title\n\nText.\n",
			absent: []string{"DQ002"},
		},
		{
			name:   "DQ002 no description at all is SEO006, not DQ002",
			page:   "# Title\n\nText.\n",
			want:   []string{"SEO006"},
			absent: []string{"DQ002"},
		},
		{
			name: "DQ003 a ref page with a short description",
			page: "+++\ndescription = \"Short description ok\"\n+++\n# Title\n\n" +
				":-: ref path=\"mylib\"\n",
			want: []string{"DQ003"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "DQ003").Message(), "minimum 30",
				) {
					t.Error("DQ003 does not state the minimum")
				}
			},
		},
		{
			name: "DQ003 a ref page with a substantive description is silent",
			page: "+++\ndescription = \"Every public function of the library, with its " +
				"signature and its documentation.\"\n+++\n# Title\n\n" +
				":-: ref path=\"mylib\"\n",
			absent: []string{"DQ003"},
		},
		{
			name:   "DQ003 a page with no ref directive is silent",
			page:   "+++\ndescription = \"Short description ok\"\n+++\n# Title\n\nText.\n",
			absent: []string{"DQ003"},
		},
	})
}

func TestEXAMPLE001SyntaxTier(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "valid Python is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\ndef greet(name):\n    return name\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "invalid Python is reported",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\ndef greet(name)\n    return name\n    return name\n```\n",
			want: []string{"EXAMPLE001"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "EXAMPLE001")
				want := "Python syntax error in code block: invalid syntax"
				if diagnostic.Message() != want {
					t.Errorf("message = %q, want %q", diagnostic.Message(), want)
				}
				if diagnostic.Line() == nil {
					t.Error("EXAMPLE001 carries no line")
				}
			},
		},
		{
			name: "a snippet under three lines is skipped",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\ndef greet(\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "a block carrying a directive marker is skipped",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\n:-: ref path=\"x\"\ndef greet(\nreturn\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "an indented fragment is exempt",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\n    return value\n    value += 1\n    print(value)\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "an unexpected indent is exempt",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\nvalue = 1\n    value += 1\n    print(value)\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "a dedent matching no outer level is exempt",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\ndef f():\n        a = 1\n    b = 2\n    return a\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "a block that was never indented is exempt",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python\nif ready:\nrun()\nstop()\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "valid JSON is silent",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```json\n{\"a\": 1}\n```\n",
			absent: []string{"EXAMPLE001"},
		},
		{
			name: "invalid JSON is reported",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```json\n{\"a\": 1,}\n```\n",
			want: []string{"EXAMPLE001"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "EXAMPLE001").Message(), "JSON syntax error",
				) {
					t.Error("EXAMPLE001 does not name JSON")
				}
			},
		},
		{
			name: "a JSON block carrying a directive marker is skipped",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```json\n{:-: var key=\"x\"}\n```\n",
			absent: []string{"EXAMPLE001"},
		},
	})
}

func TestEXAMPLE002And003SemanticTier(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "a validate marker with no examples config at all",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python validate\nx = 1\ny = 2\nz = 3\n```\n",
			want: []string{"EXAMPLE003"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "EXAMPLE003")
				if !strings.Contains(diagnostic.Message(), `"examples"`) {
					t.Errorf("message = %q, want it to name the config key",
						diagnostic.Message())
				}
			},
		},
		{
			name:   "a validate marker for an unconfigured language",
			config: map[string]any{"examples": map[string]any{"go": "go vet {file}"}},
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python validate\nx = 1\ny = 2\nz = 3\n```\n",
			want: []string{"EXAMPLE003"},
		},
		{
			name:   "a failing validator is EXAMPLE002",
			config: map[string]any{"examples": map[string]any{"text": "false {file}"}},
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```text validate\nnot a program\n```\n",
			want:   []string{"EXAMPLE002"},
			absent: []string{"EXAMPLE003"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "EXAMPLE002").Message(), "exit 1",
				) {
					t.Errorf("message = %q",
						onlyMessage(t, results, "EXAMPLE002").Message())
				}
			},
		},
		{
			name:   "a passing validator is silent",
			config: map[string]any{"examples": map[string]any{"text": "true {file}"}},
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```text validate\nanything\n```\n",
			absent: []string{"EXAMPLE002", "EXAMPLE003"},
		},
		{
			name:   "a missing validator binary is EXAMPLE002",
			config: map[string]any{"examples": map[string]any{"text": "selfdoc-no-such-binary {file}"}},
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```text validate\nanything\n```\n",
			want: []string{"EXAMPLE002"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if !strings.Contains(
					onlyMessage(t, results, "EXAMPLE002").Message(), "could not be run",
				) {
					t.Errorf("message = %q",
						onlyMessage(t, results, "EXAMPLE002").Message())
				}
			},
		},
		{
			name:   "an unmarked block is never executed",
			config: map[string]any{"examples": map[string]any{"text": "false {file}"}},
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```text\nnot a program\n```\n",
			absent: []string{"EXAMPLE002", "EXAMPLE003"},
		},
	})
}

func TestExampleValidationLeavesNoScratchFiles(t *testing.T) {
	fixture := lintProject(t)
	fixture.Config["examples"] = map[string]any{"text": "true {file}"}
	write(t, filepath.Join(fixture.DocsDir, "page.md"),
		"+++\ndescription = \"test\"\n+++\n# Title\n\n```text validate\nanything\n```\n")

	runLintsOn(t, fixture, nil)

	matches, err := filepath.Glob(filepath.Join(fixture.Root, "*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, match := range matches {
		if strings.Contains(filepath.Base(match), "selfdoc-example-") {
			t.Errorf("a scratch directory survived the run: %s", match)
		}
	}
}

func TestEXAMPLE003FallsBackToTheSyntaxTier(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "an unconfigured validate marker still gets its syntax verdict",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```python validate\ndef greet(name)\n    return name\n    return name\n```\n",
			want: []string{"EXAMPLE003", "EXAMPLE001"},
		},
	})
}

func TestSEO012Contrast(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name:   "the default theme passes",
			page:   "+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n",
			absent: []string{"SEO012"},
		},
	})
}

// TestSEO012CustomCSS writes the stylesheet the override cases need, which the
// table cannot do before the fixture exists.
func TestSEO012CustomCSS(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		css     string
		wantHit bool
		file    string
	}{
		{
			name:    "a low-contrast link override is reported",
			css:     ":root {\n  --link: #eeeeee;\n}\n",
			wantHit: true,
			file:    "custom.css",
		},
		{
			name:    "a high-contrast link override is silent",
			css:     ":root {\n  --link: #003366;\n}\n",
			wantHit: false,
		},
		{
			name:    "a dark-mode override is measured against the dark palette",
			css:     "[data-theme=\"dark\"] {\n  --link: #111111;\n}\n",
			wantHit: true,
			file:    "custom.css",
		},
		{
			name:    "no custom.css leaves only the theme's own verdict",
			css:     "",
			wantHit: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := lintProject(t)
			write(t, filepath.Join(fixture.DocsDir, "page.md"),
				"+++\ndescription = \"test\"\n+++\n# Title\n\nText.\n")
			if testCase.css != "" {
				write(t, filepath.Join(fixture.DocsDir, "custom.css"), testCase.css)
			}
			results := runLintsOn(t, fixture, nil)
			hit := hasCode(results, "SEO012")
			if hit != testCase.wantHit {
				t.Fatalf("SEO012 fired = %v, want %v: %v",
					hit, testCase.wantHit, messagesOf(withCode(results, "SEO012")))
			}
			if testCase.wantHit && withCode(results, "SEO012")[0].File() != testCase.file {
				t.Errorf("file = %q, want %q",
					withCode(results, "SEO012")[0].File(), testCase.file)
			}
		})
	}
}

// messagesOf renders a diagnostic list for a failure message.
func messagesOf(diagnostics []lints.LintResult) []string {
	rendered := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		rendered = append(rendered, fmt.Sprintf("%s %s", diagnostic.Code(), diagnostic.Message()))
	}
	return rendered
}

func TestContrastArithmetic(t *testing.T) {
	white := rgb{255, 255, 255}
	black := rgb{0, 0, 0}
	if ratio := contrastRatio(black, white); ratio < 20.9 || ratio > 21.1 {
		t.Errorf("black on white = %f, want 21", ratio)
	}
	if ratio := contrastRatio(white, white); ratio < 0.99 || ratio > 1.01 {
		t.Errorf("white on white = %f, want 1", ratio)
	}
	if color, ok := parseHexColor("#ff8800"); !ok || color != (rgb{255, 136, 0}) {
		t.Errorf("parseHexColor(\"#ff8800\") = %v, %v", color, ok)
	}
	if color, ok := parseHexColor("  1A2b3C  "); !ok || color != (rgb{26, 43, 60}) {
		t.Errorf("parseHexColor of an unprefixed value = %v, %v", color, ok)
	}
	for _, invalid := range []string{"#fff", "", "#gggggg", "#12345"} {
		if _, ok := parseHexColor(invalid); ok {
			t.Errorf("parseHexColor(%q) was accepted", invalid)
		}
	}
}

func TestSpellingRuleFires(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "a misspelled word in prose",
			page: "+++\ndescription = \"A page about the thing it describes here.\"\n+++\n" +
				"# Title\n\nThis sentance is wrong.\n",
			want: []string{"SPELL001"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				diagnostic := onlyMessage(t, results, "SPELL001")
				if !strings.Contains(diagnostic.Message(), "sentance") {
					t.Errorf("message = %q", diagnostic.Message())
				}
				if diagnostic.Severity() != "error" {
					t.Errorf("severity = %q, want error", diagnostic.Severity())
				}
			},
		},
		{
			name: "a word inside a code span is not prose",
			page: "+++\ndescription = \"A page about the thing it describes here.\"\n+++\n" +
				"# Title\n\nThe `sentance` identifier is code.\n",
			absent: []string{"SPELL001"},
		},
		{
			name: "a fenced block is not prose",
			page: "+++\ndescription = \"A page about the thing it describes here.\"\n+++\n" +
				"# Title\n\n```\nsentance\n```\n",
			absent: []string{"SPELL001"},
		},
	})
}

func TestCleanPageHasNoLints(t *testing.T) {
	fixture := lintProject(t)
	description := "A clean page demonstrating proper formatting and metadata " +
		"usage for SEO best practices and documentation quality standards"
	paragraph := repeatWords("word", 50)
	write(t, filepath.Join(fixture.DocsDir, "page.md"),
		"+++\ntitle = \"Clean\"\ndescription = \""+description+"\"\n+++\n"+
			"# Clean\n\n## Section\n\n"+paragraph+"\n\n### Subsection\n\n"+
			paragraph+"\n\n![diagram](diagram.png)\n")

	results := runLintsOn(t, fixture, nil)

	if len(results) != 0 {
		t.Errorf("a clean page produced %v", messagesOf(results))
	}
}

// --- SEO004 measures the title the page renders ---

// namedLintProject creates a lint fixture whose project root -- and therefore
// the project name every rendered title carries -- is the given name.
func namedLintProject(t *testing.T, name string) lintFixture {
	t.Helper()
	isolate(t)
	root := filepath.Join(t.TempDir(), name)
	docsDir := filepath.Join(root, ".stricttools", "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	return lintFixture{Root: root, DocsDir: docsDir, Config: pythonProjectConfig()}
}

// seo004On writes one page into a fixture and returns what the rules said.
func seo004On(t *testing.T, fixture lintFixture, title string) []lints.LintResult {
	t.Helper()
	write(t, filepath.Join(fixture.DocsDir, "page.md"),
		"+++\ntitle = \""+title+"\"\ndescription = \"A description of the page, at a "+
			"length the description rules have nothing to say about.\"\n+++\n"+
			"# Heading\n\nText.\n")
	return runLintsOn(t, fixture, nil)
}

func TestSEO004WarnsAtSixtyOneRenderedCharacters(t *testing.T) {
	fixture := namedLintProject(t, "project")
	// The rendered title of an ordinary page is "<title> - <project name>".
	title := strings.Repeat("a", 61-len(" - ")-len("project"))
	results := seo004On(t, fixture, title)
	if !hasCode(results, "SEO004") {
		t.Fatalf("SEO004 missing for a 61-character title; got %v", codes(results))
	}
	if message := onlyMessage(t, results, "SEO004").Message(); !strings.Contains(
		message, "61 chars") {
		t.Errorf("message = %q, want the rendered length", message)
	}
}

func TestSEO004IsSilentAtSixtyRenderedCharacters(t *testing.T) {
	fixture := namedLintProject(t, "project")
	title := strings.Repeat("a", 60-len(" - ")-len("project"))
	if results := seo004On(t, fixture, title); hasCode(results, "SEO004") {
		t.Fatalf("SEO004 fired for a 60-character title: %q",
			onlyMessage(t, results, "SEO004").Message())
	}
}

func TestSEO004MeasuresTheDerivedTitleOfTheProjectIndexPage(t *testing.T) {
	// A page titled with the project name renders "<project name> - <thing>"
	// -- the description's first clause, cut to the same cap -- rather than
	// the project name twice. Measuring the name twice reports a length no
	// page ever carries, and no edit to the page can clear it.
	name := strings.Repeat("n", 31)
	fixture := namedLintProject(t, name)
	fixture.Config["description"] =
		"A documentation engine that reads source code and writes pages."
	if results := seo004On(t, fixture, name); hasCode(results, "SEO004") {
		t.Fatalf("SEO004 measured a title the page does not render: %q",
			onlyMessage(t, results, "SEO004").Message())
	}
}
