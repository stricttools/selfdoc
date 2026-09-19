package preview

import (
	"fmt"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/verify"
)

// RenderReport returns the verification report as the preview prints it.
//
// Loud, and first: a preview that quietly served a broken tree would be the
// very failure the command exists to prevent.
func RenderReport(report *verify.VerifyReport, outDir string) string {
	rule := strings.Repeat("=", 72)
	lines := []string{rule, fmt.Sprintf("verify: %s", outDir)}
	lines = append(lines, fmt.Sprintf("  %d of %d check(s) ran, %d problem(s) found.",
		len(report.Ran), len(verify.Checks), len(report.Failures)))
	for _, skip := range report.Skipped {
		lines = append(lines, fmt.Sprintf("  NOT CHECKED: %s -- %s", skip.Check, skip.Reason))
	}
	if report.OK() {
		lines = append(lines, "  Every check that ran passed.")
	} else {
		lines = append(lines, "")
		lines = append(lines, report.ErrorText())
		lines = append(lines, "")
		lines = append(lines,
			"  The preview serves this tree anyway -- looking at a tree that "+
				"is not right yet is what a preview is for. A deploy would "+
				"refuse it.")
	}
	lines = append(lines, rule)
	return strings.Join(lines, "\n")
}
