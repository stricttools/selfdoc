package check

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/lints"
)

// SerializeCheckResult builds the machine payload `selfdoc check --json`
// carries.
//
// This is the single definition of the machine-readable check contract: the
// command layer and its tests both call it, so the declared payload schema has
// one producer to stay in step with -- and the framework validates this
// document against that declaration where it writes the envelope.
//
// exitCode is the code the run will terminate with, from
// [CheckResultExitCode]. The diagnostics must already be filtered through the
// project's suppression list.
func SerializeCheckResult(result *CheckResult, exitCode int) map[string]any {
	directiveDocuments := make([]any, 0, len(result.DirectiveResults))
	for _, directiveResult := range result.DirectiveResults {
		directiveDocuments = append(directiveDocuments, map[string]any{
			"file":      directiveResult.File,
			"line":      directiveResult.Line,
			"directive": directiveResult.Directive,
			"status":    directiveResult.Outcome,
			"error":     directiveResult.Error,
		})
	}

	lintDocuments := make([]any, 0, len(result.Lints))
	for _, lint := range result.Lints {
		var line any
		if lint.Line() != nil {
			line = *lint.Line()
		}
		lintDocuments = append(lintDocuments, map[string]any{
			"file":     lint.File(),
			"line":     line,
			"code":     lint.Code(),
			"message":  lint.Message(),
			"severity": lint.Severity(),
		})
	}

	output := map[string]any{
		"directives": directiveDocuments,
		"coverage":   nil,
		"lints":      lintDocuments,
		"exit_code":  exitCode,
	}
	if result.Coverage != nil {
		coverage := result.Coverage
		output["coverage"] = map[string]any{
			"total_public":         coverage.Total,
			"referenced":           coverage.ReferencedCount,
			"documented":           coverage.DocumentedCount,
			"referenced_symbols":   stringsOrEmpty(coverage.ReferencedSymbols),
			"documented_symbols":   stringsOrEmpty(coverage.DocumentedSymbols),
			"unreferenced_symbols": stringsOrEmpty(coverage.UnreferencedSymbols),
		}
	}
	return output
}

// stringsOrEmpty renders a symbol list as the payload carries it: an array,
// never null, because a project with nothing in a tier has an empty list
// rather than a missing one.
func stringsOrEmpty(values []string) []any {
	documents := make([]any, 0, len(values))
	for _, value := range values {
		documents = append(documents, value)
	}
	return documents
}

// FilterLints returns the diagnostics whose code is not suppressed.
//
// It is the adapter over lints.FilterLints for a caller holding a code set as
// a map, which is what the suppression parser produces.
func FilterLints(diagnostics []lints.LintResult, ignoreCodes map[string]struct{}) []lints.LintResult {
	return lints.FilterLints(diagnostics, ignoreCodes)
}

// colorize wraps text in an ANSI escape when colour is on.
func colorize(text, code string, color bool) string {
	if color {
		return "\033[" + code + "m" + text + "\033[0m"
	}
	return text
}

// PrintResults writes a check result to out in the human-readable form.
//
// color decides whether the report carries ANSI escapes. It is a parameter
// rather than a decision made here, because whether the destination is a
// terminal is the caller's knowledge -- the Python decided it once at import
// time from sys.stdout, which made the report untestable and wrong for any
// writer that was not that stream.
func PrintResults(out io.Writer, result *CheckResult, color bool) {
	if len(result.DirectiveResults) == 0 {
		fmt.Fprintln(out, colorize(
			"No directives found in documentation templates.", "1", color,
		))
	} else {
		fmt.Fprintln(out, colorize("Directives", "1", color))
		for _, directiveResult := range result.DirectiveResults {
			statusString := colorize("OK", "32", color)
			if directiveResult.Error != "" {
				statusString = colorize(
					"FAILED: "+directiveResult.Error, "31", color,
				)
			}
			fmt.Fprintf(out, "  %s:%d  %s  %s\n",
				directiveResult.File, directiveResult.Line,
				directiveResult.Directive, statusString,
			)
		}

		okCount, failCount := 0, 0
		for _, directiveResult := range result.DirectiveResults {
			switch directiveResult.Outcome {
			case StatusOK:
				okCount++
			case StatusFailed:
				failCount++
			}
		}
		total := len(result.DirectiveResults)
		fmt.Fprintf(out, "\n%d directive(s): %d OK, %d FAILED\n",
			total, okCount, failCount,
		)

		if result.Coverage != nil {
			printCoverage(out, result.Coverage, color)
		}
	}

	// Every diagnostic is always shown.
	if len(result.Lints) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, colorize("Lints", "1", color))
		for _, lint := range result.Lints {
			linePart := ""
			if lint.Line() != nil {
				linePart = fmt.Sprintf(":%d", *lint.Line())
			}
			severityString := lint.Severity()
			switch severityString {
			case "error":
				severityString = colorize(severityString, "31", color)
			case "warning":
				severityString = colorize(severityString, "33", color)
			}
			codeString := colorize("["+lint.Code()+"]", "36", color)
			fmt.Fprintf(out, "  %s: %s %s%s - %s\n",
				severityString, codeString, lint.File(), linePart, lint.Message(),
			)
		}
	} else {
		fmt.Fprintln(out, "No lints.")
	}
}

// printCoverage writes the coverage section of the report.
func printCoverage(out io.Writer, coverage *CoverageStats, color bool) {
	if coverage.Total == 0 {
		fmt.Fprintln(out, "Coverage: no public symbols found in source files")
		return
	}
	docPct := coverage.DocumentedCount * 100 / coverage.Total
	refPct := coverage.ReferencedCount * 100 / coverage.Total
	fmt.Fprintf(out, "Coverage: %d/%d symbols documented (%d%%)\n",
		coverage.DocumentedCount, coverage.Total, docPct,
	)
	if coverage.ReferencedCount != coverage.DocumentedCount {
		fmt.Fprintf(out, "          %d/%d symbols referenced (%d%%)\n",
			coverage.ReferencedCount, coverage.Total, refPct,
		)
	}
	if len(coverage.UnreferencedSymbols) > 0 && refPct < 100 {
		fmt.Fprintln(out, colorize("Unreferenced symbols:", "1", color))
		printSymbolsByFile(out, coverage.UnreferencedSymbols)
	}
	if docPct < 100 {
		documented := map[string]bool{}
		for _, symbol := range coverage.DocumentedSymbols {
			documented[symbol] = true
		}
		var skeletonOnly []string
		for _, symbol := range coverage.ReferencedSymbols {
			if !documented[symbol] {
				skeletonOnly = append(skeletonOnly, symbol)
			}
		}
		if len(skeletonOnly) > 0 {
			printSkeletonOnly(out, coverage, skeletonOnly, color)
		}
	}
}

// printSkeletonOnly writes the skeleton-only section: the pages whose
// descriptions are the reason those symbols do not count as documented, what
// makes a page one, and the edit that fixes it.
//
// The section names the PAGE first, because that is the cause. A symbol is
// skeleton-only when the only page naming it is generated and still carries the
// description selfdoc emitted -- its frontmatter declares `seeded = true` --
// and nothing about the symbol's own documentation is involved. A section
// listing symbols alone reads as a list of under-documented code and sends the
// reader to rewrite doc comments that are already complete.
func printSkeletonOnly(out io.Writer, coverage *CoverageStats, skeletonOnly []string, color bool) {
	fmt.Fprintln(out, colorize("Skeleton-only symbols:", "1", color))
	fmt.Fprintln(out, "  Each is named only on a generated page whose frontmatter still declares")
	fmt.Fprintln(out, "  seeded = true, so that page's description is the one selfdoc emitted and")
	fmt.Fprintln(out, "  nobody has rewritten. The symbols' own doc comments are not the cause and")
	fmt.Fprintln(out, "  rewriting them changes nothing here: edit each page's frontmatter")
	fmt.Fprintln(out, "  description instead.")
	if pages := skeletonPagesOf(coverage, skeletonOnly); len(pages) > 0 {
		fmt.Fprintln(out, "  Pages whose description to edit:")
		for _, page := range pages {
			fmt.Fprintf(out, "    %s\n", page)
		}
	}
	fmt.Fprintln(out, "  Symbols they leave undocumented:")
	printSymbolsByFile(out, skeletonOnly)
}

// skeletonPagesOf is the sorted set of pages accounting for skeletonOnly.
func skeletonPagesOf(coverage *CoverageStats, skeletonOnly []string) []string {
	seen := map[string]bool{}
	var pages []string
	for _, symbol := range skeletonOnly {
		page := coverage.SkeletonPagesBySymbol[symbol]
		if page == "" || seen[page] {
			continue
		}
		seen[page] = true
		pages = append(pages, page)
	}
	sort.Strings(pages)
	return pages
}

// printSymbolsByFile writes a symbol list grouped by the file each symbol came
// from, files in sorted order.
func printSymbolsByFile(out io.Writer, qualifiedSymbols []string) {
	byFile := map[string][]string{}
	var order []string
	for _, qualified := range qualifiedSymbols {
		filePath, symbol := qualified, qualified
		if index := strings.LastIndex(qualified, ":"); index >= 0 {
			filePath, symbol = qualified[:index], qualified[index+1:]
		}
		if _, present := byFile[filePath]; !present {
			order = append(order, filePath)
		}
		byFile[filePath] = append(byFile[filePath], symbol)
	}
	sort.Strings(order)
	for _, filePath := range order {
		fmt.Fprintf(out, "  %s: %s\n", filePath, strings.Join(byFile[filePath], ", "))
	}
}
