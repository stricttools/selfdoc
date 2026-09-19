package check

import (
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/staleness"
)

// DirectiveResult is the result of validating a single directive.
type DirectiveResult struct {
	// File is the directive's page, relative to the docs directory, with
	// the version or project prefix a multi-version or unified run adds.
	File string
	// Line is the directive's 1-based line in its source file,
	// frontmatter included.
	Line int
	// Directive is the directive as the report shows it: its name
	// followed by its attributes.
	Directive string
	// Outcome is "OK" or "FAILED". [DirectiveResult.Status] reports it,
	// which is what lints.DirectiveOutcome asks of this type -- a field of
	// that name and a method of that name cannot coexist.
	Outcome string
	// Error is the resolution failure's message, empty when the directive
	// resolved.
	Error string
}

// Status reports the directive's outcome, satisfying lints.DirectiveOutcome.
func (d DirectiveResult) Status() string { return d.Outcome }

// StatusOK is the Outcome of a directive that resolved.
const StatusOK = "OK"

// StatusFailed is the Outcome of a directive that did not resolve.
const StatusFailed = "FAILED"

// ResolvedDirective is a successfully resolved directive with its output.
type ResolvedDirective struct {
	// Name is the directive name -- ref, table-schema, code-test and the
	// rest.
	Name string
	// Attrs are the directive's attributes.
	Attrs map[string]string
	// Content is the Markdown the directive resolved to.
	Content string
	// File is the page the directive sits on, relative to the docs
	// directory.
	File string
	// SourceEntry is the declared source path that answered the directive,
	// nil when none did.
	SourceEntry *extractors.SourceEntry
}

// StalenessDirective narrows the record to what the drift measurement reads.
func (r ResolvedDirective) StalenessDirective() staleness.PageDirective {
	return staleness.PageDirective{PathArg: r.Attrs["path"], SourceEntry: r.SourceEntry}
}

// CoverageStats is the coverage of a project's public symbols by its
// directives.
//
// Two tiers are counted. A symbol is "referenced" when any directive's
// resolved output names it, and "documented" when it does so on a page that is
// not a bare generated skeleton -- a page whose description a person wrote, or
// customized.
//
// The count fields are spelled Total, ReferencedCount and DocumentedCount
// rather than after the tiers, because [CoverageStats.TotalPublic] and
// [CoverageStats.Documented] are the methods lints.Coverage asks for and a
// field cannot share a method's name.
type CoverageStats struct {
	// Total is how many public symbols the project's sources export.
	Total int
	// ReferencedCount is how many of them any directive names.
	ReferencedCount int
	// DocumentedCount is how many of them a non-skeleton page names.
	DocumentedCount int
	// ReferencedSymbols are the "<file>:<symbol>" identifiers any
	// directive names.
	ReferencedSymbols []string
	// DocumentedSymbols are the identifiers a non-skeleton page names.
	DocumentedSymbols []string
	// UnreferencedSymbols are the identifiers no directive names.
	UnreferencedSymbols []string
	// SkeletonPagesBySymbol maps a referenced identifier to the generated,
	// still-seeded page that named it. It is what lets the report say
	// WHICH page's description is the reason a symbol counts as referenced
	// and not documented, because that is a property of the page and not
	// of the symbol's own documentation.
	SkeletonPagesBySymbol map[string]string
}

// TotalPublic reports how many public symbols there are, satisfying
// lints.Coverage.
func (c *CoverageStats) TotalPublic() int { return c.Total }

// Documented reports how many public symbols are documented, satisfying
// lints.Coverage.
func (c *CoverageStats) Documented() int { return c.DocumentedCount }

// CheckResult is the whole verdict of one [CheckDocs] run.
type CheckResult struct {
	// DirectiveResults is one entry per directive the run validated, in
	// page order.
	DirectiveResults []DirectiveResult
	// Coverage is the symbol coverage measurement, nil for a project that
	// declares no source.
	Coverage *CoverageStats
	// Lints are the diagnostics the run produced, in rule order.
	Lints []lints.LintResult
}

// directiveOutcomes is result.DirectiveResults as the verdict rules read it.
func directiveOutcomes(results []DirectiveResult) []lints.DirectiveOutcome {
	if results == nil {
		return nil
	}
	outcomes := make([]lints.DirectiveOutcome, 0, len(results))
	for _, result := range results {
		outcomes = append(outcomes, result)
	}
	return outcomes
}

// coverageOf is result.Coverage as the verdict rules read it.
//
// A nil *CoverageStats has to become a nil interface rather than a non-nil
// interface holding a nil pointer, which is what lints.CheckExitCode refuses.
func coverageOf(coverage *CoverageStats) lints.Coverage {
	if coverage == nil {
		return nil
	}
	return coverage
}

// CheckResultExitCode computes the process exit code for a whole CheckResult.
//
// It is the adapter over lints.CheckExitCode -- the one definition of the
// verdict rules -- for a caller holding a full result. A reduced entry point
// (the post-build lint pass, the posts-only check) calls that function
// directly with just its diagnostics.
//
// config is read for "coverage_threshold"; pass nil when no configuration is
// in play.
func CheckResultExitCode(result *CheckResult, config map[string]any) int {
	return lints.CheckExitCode(
		result.Lints,
		directiveOutcomes(result.DirectiveResults),
		coverageOf(result.Coverage),
		config,
	)
}

// CoverageBelowThreshold reports whether a run's documented coverage is under
// the project's configured threshold.
//
// It is the adapter over lints.CoverageBelowThreshold for a caller holding a
// full result, which is what the command layer needs to decide whether to
// print the below-threshold note beside the report.
func CoverageBelowThreshold(result *CheckResult, config map[string]any) bool {
	return lints.CoverageBelowThreshold(coverageOf(result.Coverage), config)
}
