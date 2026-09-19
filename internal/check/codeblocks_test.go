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
			name: "SEO001 does not see an H1 inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Real Title\n\n" +
				"```markdown\n# Example Title\n```\n",
			absent: []string{"SEO001"},
		},
		{
			name: "SEO001 does not see several H1s inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Real Title\n\n" +
				"```markdown\n# One\n\n# Two\n```\n",
			absent: []string{"SEO001"},
		},
		{
			name: "SEO013 still fires when the only H1 is inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n" +
				"```markdown\n# Example Title\n```\n\nText.\n",
			want: []string{"SEO013"},
		},
		{
			name: "SEO003 does not see an empty alt inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n![](image.png)\n```\n",
			absent: []string{"SEO003"},
		},
		{
			name: "SEO003 still fires outside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n![](inside.png)\n```\n\n![](outside.png)\n",
			want: []string{"SEO003"},
			assert: func(t *testing.T, _ lintFixture, results []lints.LintResult) {
				if len(withCode(results, "SEO003")) != 1 {
					t.Errorf("SEO003 fired %d times, want once",
						len(withCode(results, "SEO003")))
				}
			},
		},
		{
			name: "SEO002 does not see a heading gap inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Section\n\nText.\n\n" +
				"```markdown\n## Two\n\n#### Four\n```\n",
			absent: []string{"SEO002"},
		},
		{
			name: "SEO008 does not count the words inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```\n" + repeatWords("word", 300) + "\n```\n",
			absent: []string{"SEO008"},
		},
		{
			name: "SEO008 counts prose beside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				repeatWords("word", 250) + "\n\n```\ncode here\n```\n",
			want: []string{"SEO008"},
		},
		{
			name: "SEO007 does not see a heading inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n## Section\n\nShort.\n```\n",
			absent: []string{"SEO007"},
		},
		{
			name: "SEO014 does not see meaningless alt inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n![screenshot](a.png)\n```\n",
			absent: []string{"SEO014"},
		},
		{
			name: "SEO011 does not see consecutive headings inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n## Real\n\nText.\n\n" +
				"```markdown\n## One\n## Two\n```\n",
			absent: []string{"SEO011"},
		},
		{
			name: "XREF001 does not see a link inside a fence",
			page: "+++\ndescription = \"test\"\n+++\n# Title\n\n" +
				"```markdown\n[Guide](missing.md)\n```\n",
			absent: []string{"XREF001"},
		},
	})
}
