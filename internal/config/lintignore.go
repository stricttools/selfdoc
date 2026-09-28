package config

import (
	"fmt"
	"strings"

	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/util"
)

// retiredLintCodes maps each lint code selfdoc used before its lints were
// named to the name the lint carries now. The refusal of a selfdoc.json still
// carrying "lint_ignore" reads it, so a repository moving off that key is told
// the option each of its codes became; nothing else reads it.
var retiredLintCodes = map[string]string{
	"SEO001":     "multiple-top-level-headings",
	"SEO002":     "skipped-heading-level",
	"SEO003":     "empty-image-alt-text",
	"SEO004":     "page-title-too-long",
	"SEO006":     "missing-frontmatter-description",
	"SEO007":     "first-paragraph-length-out-of-range",
	"SEO008":     "low-numeric-data-density",
	"SEO009":     "description-too-short-for-search-snippet",
	"SEO010":     "description-too-long-for-search-snippet",
	"SEO011":     "empty-heading-section",
	"SEO012":     "insufficient-theme-color-contrast",
	"SEO013":     "missing-page-title",
	"SEO014":     "meaningless-image-alt-text",
	"SEO015":     "generic-link-text",
	"STALE001":   "stale-page-description",
	"STALE002":   "manifest-disagrees-with-disk",
	"DRIFT001":   "description-drifted-from-source",
	"DQ001":      "description-restates-name",
	"DQ002":      "description-too-short",
	"DQ003":      "reference-page-description-too-short",
	"XREF001":    "broken-page-link",
	"XREF002":    "directive-path-not-on-disk",
	"PARAM001":   "undocumented-parameter",
	"RETURN001":  "undocumented-return-value",
	"EXAMPLE001": "code-block-syntax-error",
	"EXAMPLE002": "code-block-validation-failed",
	"EXAMPLE003": "code-block-validator-not-configured",
	"CLI001":     "undocumented-cli-command-or-flag",
	"CLI002":     "cli-help-text-too-short",
	"LANG001":    "unsupported-source-language",
	"SEARCH001":  "search-indexer-not-installed",
	"VER001":     "version-tag-not-extractable",
	"VER002":     "version-mismatch-with-project-manifest",
	"VER003":     "version-mismatch-with-versions-array",
	"VER004":     "version-mismatch-in-generated-root-file",
	"SPELL001":   "unknown-word",
	"VOCAB001":   "unused-accepted-word",
	"VOCAB002":   "duplicate-vocabulary-entry",
	"VOCAB003":   "accepted-word-matches-rejected-pattern",
	"VOCAB004":   "rejected-term-in-prose",
	"VOCAB005":   "unsorted-vocabulary-entries",
	"POST001":    "missing-post-date",
	"POST002":    "missing-post-title",
	"POST003":    "malformed-post-date",
	"POST004":    "duplicate-post-slug",
	"POST005":    "changed-published-post-slug",
	"POST006":    "invalid-post-directives-declaration",
	"POST007":    "directive-marker-in-post-without-directives",
	"LINK001":    "broken-emitted-reference",
	"UNIFIED001": "unified-project-without-config",
	"UNIFIED002": "unified-project-check-failed",
}

// lintIgnoreRefusal is the refusal of a selfdoc.json still declaring
// "lint_ignore". A lint is turned down through its option now, an entry in the
// repository's options directory that carries its reason, so the refusal
// prints, for every lint the list names, the command that writes that entry.
func lintIgnoreRefusal(declared any) *ConfigError {
	var commands, unknown []string
	list, _ := declared.([]any)
	for _, item := range list {
		code, isString := item.(string)
		name := code
		if retired, found := retiredLintCodes[code]; found {
			name = retired
		}
		if !isString || !lints.Registered().Has(name) {
			unknown = append(unknown, util.PythonRepr(item))
			continue
		}
		commands = append(commands, fmt.Sprintf(
			"  selfdoc options set %s --current %s --ideal %s --reason <text>",
			lints.OptionID(name), lints.SettingOff, lints.SettingOff))
	}
	var message strings.Builder
	message.WriteString("selfdoc.json declares 'lint_ignore', which selfdoc no longer reads: a lint is turned " +
		"down through its option, an entry in .strictmetadata/options/docs.toml that states its reason. ")
	if len(commands) > 0 {
		message.WriteString("Write the entry for each lint the list names, with <text> saying why this " +
			"repository turns the lint off, then delete 'lint_ignore' from selfdoc.json:\n")
		message.WriteString(strings.Join(commands, "\n"))
	} else {
		message.WriteString("Delete 'lint_ignore' from selfdoc.json.")
	}
	if len(unknown) > 0 {
		fmt.Fprintf(&message, "\n%s names no lint, so it needs no entry.", strings.Join(unknown, ", "))
	}
	return &ConfigError{Message: message.String()}
}
