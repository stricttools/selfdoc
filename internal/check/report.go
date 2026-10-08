package check

import (
	"fmt"
	"io"
	"sort"
	"strings"
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
// [CheckResultExitCode]. The diagnostics must already carry the repository's
// lint options (lints.Settings.Apply).
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
			"name":     lint.Name(),
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

// PrintResults writes a check result to out in the human-readable form. It
// is plain text: the report goes through strictcli's ctx.Out, whose
// destination may be the --json envelope's output member, so it carries no
// ANSI escapes.
func PrintResults(out io.Writer, result *CheckResult) {
	if len(result.DirectiveResults) == 0 {
		fmt.Fprintln(out, "No directives found in documentation templates.")
	} else {
		fmt.Fprintln(out, "Directives")
		for _, directiveResult := range result.DirectiveResults {
			statusString := "OK"
			if directiveResult.Error != "" {
				statusString = "FAILED: " + directiveResult.Error
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
			printCoverage(out, result.Coverage)
		}
	}

	// Every diagnostic is always shown.
	if len(result.Lints) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Lints")
		for _, lint := range result.Lints {
			linePart := ""
			if lint.Line() != nil {
				linePart = fmt.Sprintf(":%d", *lint.Line())
			}
			fmt.Fprintf(out, "  %s: [%s] %s%s - %s\n",
				lint.Severity(), lint.Name(), lint.File(), linePart, lint.Message(),
			)
		}
	} else {
		fmt.Fprintln(out, "No lints.")
	}
}

// printCoverage writes the coverage section of the report.
func printCoverage(out io.Writer, coverage *CoverageStats) {
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
		fmt.Fprintln(out, "Unreferenced symbols:")
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
			printSkeletonOnly(out, coverage, skeletonOnly)
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
func printSkeletonOnly(out io.Writer, coverage *CoverageStats, skeletonOnly []string) {
	fmt.Fprintln(out, "Skeleton-only symbols:")
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
