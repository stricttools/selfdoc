package check

import (
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
)

// A fenced code block is not prose, and no page rule may read one. The rules
// get their structure from the block tokenizer, so the exclusion is
// structural: a code block is a token type the prose rules never visit. These
// cases assert that for every rule that reads a line.
func TestCodeBlocksAreNotProse(t *testing.T) {
	runLintCases(t, []lintCase{
		{
			name: "multiple-top-level-headings does not see an H1 inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Real Title\n\n" +
				"```markdown\n# Example Title\n```\n",
			absent: []string{"multiple-top-level-headings"},
		},
		{
			name: "multiple-top-level-headings does not see several H1s inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Real Title\n\n" +
				"```markdown\n# One\n\n# Two\n```\n",
			absent: []string{"multiple-top-level-headings"},
		},
		{
			name: "missing-page-title still fires when the only H1 is inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n" +
				"```markdown\n# Example Title\n```\n\nText.\n",
			want: []string{"missing-page-title"},
		},
		{
			name: "empty-image-alt-text does not see an empty alt inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n![](image.png)\n```\n",
			absent: []string{"empty-image-alt-text"},
		},
		{
			name: "empty-image-alt-text still fires outside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n![](inside.png)\n```\n\n![](outside.png)\n",
			want: []string{"empty-image-alt-text"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if len(withCode(results, "empty-image-alt-text")) != 1 {
					t.Errorf("empty-image-alt-text fired %d times, want once",
						len(withCode(results, "empty-image-alt-text")))
				}
			},
		},
		{
			name: "skipped-heading-level does not see a heading gap inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\nText.\n\n" +
				"```markdown\n## Two\n\n#### Four\n```\n",
			absent: []string{"skipped-heading-level"},
		},
		{
			name: "low-numeric-data-density does not count the words inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```\n" + repeatWords("word", 300) + "\n```\n",
			absent: []string{"low-numeric-data-density"},
		},
		{
			name: "low-numeric-data-density counts prose beside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 250) + "\n\n```\ncode here\n```\n",
			want: []string{"low-numeric-data-density"},
		},
		{
			name: "first-paragraph-length-out-of-range does not see a heading inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n## Section\n\nShort.\n```\n",
			absent: []string{"first-paragraph-length-out-of-range"},
		},
		{
			name: "meaningless-image-alt-text does not see meaningless alt inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n![screenshot](a.png)\n```\n",
			absent: []string{"meaningless-image-alt-text"},
		},
		{
			name: "empty-heading-section does not see consecutive headings inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Real\n\nText.\n\n" +
				"```markdown\n## One\n## Two\n```\n",
			absent: []string{"empty-heading-section"},
		},
		{
			name: "broken-page-link does not see a link inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n[Guide](missing.md)\n```\n",
			absent: []string{"broken-page-link"},
		},
	})
}
