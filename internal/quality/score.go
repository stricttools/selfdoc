package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
)

// Result is one project's score: the shape both the text report and the
// machine payload carry.
type Result struct {
	// Project is the project directory's own name.
	Project string
	// Path is the directory that was scored.
	Path string
	// Tier is the maturity tier 0-5.
	Tier int
	// TierName is the tier's label, from [Tiers].
	TierName string
	// CodeLOC is dirstat's code total, submodules subtracted.
	CodeLOC int
	// TestLOC is the line count of the project's test code.
	TestLOC int
	// SourceLOC is CodeLOC minus TestLOC, floored at zero.
	SourceLOC int
	// DocLOC and DocFiles are the Markdown line and file counts.
	DocLOC   int
	DocFiles int
	// DocRatio is DocLOC over SourceLOC rounded to four decimals, or nil when
	// there was no source to divide by.
	DocRatio *float64
	// ContentGrade is the grade A-F, or "-" for a project with no source.
	ContentGrade string
	// Selfdoc is the project's selfdoc adoption.
	Selfdoc Adoption
	// NextStep is the action that reaches the next tier. Empty at tier 5,
	// where nothing is left to do, and the payload then carries null.
	NextStep string
}

// Payload returns the result as the QUALITY machine payload: the same members
// under the same names, with the selfdoc block carrying only has_selfdoc when
// the project has no selfdoc.json.
func (r Result) Payload() map[string]any {
	adoption := map[string]any{"has_selfdoc": r.Selfdoc.HasSelfdoc}
	if r.Selfdoc.HasSelfdoc {
		adoption["auto_readme"] = r.Selfdoc.AutoREADME
		adoption["auto_claude"] = r.Selfdoc.AutoCLAUDE
		adoption["custom_directives"] = r.Selfdoc.CustomDirectives
		adoption["has_posts"] = r.Selfdoc.HasPosts
		adoption["directive_count"] = r.Selfdoc.DirectiveCount
	}

	var ratio any
	if r.DocRatio != nil {
		ratio = *r.DocRatio
	}
	var nextStep any
	if r.NextStep != "" {
		nextStep = r.NextStep
	}

	return map[string]any{
		"project":       r.Project,
		"path":          r.Path,
		"tier":          r.Tier,
		"tier_name":     r.TierName,
		"code_loc":      r.CodeLOC,
		"test_loc":      r.TestLOC,
		"source_loc":    r.SourceLOC,
		"doc_loc":       r.DocLOC,
		"doc_files":     r.DocFiles,
		"doc_ratio":     ratio,
		"content_grade": r.ContentGrade,
		"selfdoc":       adoption,
		"next_step":     nextStep,
	}
}

// ScoreProject scores one project directory.
//
// It runs every counter in this package against projectPath and combines them.
// SourceLOC is the dirstat code total minus test LOC, floored at zero;
// DocRatio is doc LOC over SourceLOC rounded to four decimals, or nil when
// there is no source to divide by.
func ScoreProject(projectPath string, h *effects.Handle) (Result, error) {
	projectPath = filepath.Clean(projectPath)
	submodulePaths := SubmodulePaths(projectPath)
	codeLOC, _, err := CodeLOC(projectPath, submodulePaths, h)
	if err != nil {
		return Result{}, err
	}
	testLOC := TestLOC(projectPath, submodulePaths)
	info := SelfdocInfo(projectPath)

	var rootFileTemplates []string
	if content, err := os.ReadFile(filepath.Join(projectPath, "selfdoc.json")); err == nil {
		var config map[string]any
		if err := json.Unmarshal(content, &config); err == nil {
			rootFileTemplates = stringList(config["root_files"])
		}
	}

	docLOC, docFiles := MarkdownLOC(projectPath, submodulePaths, rootFileTemplates)
	tier := ComputeTier(docLOC, info)
	sourceLOC := codeLOC - testLOC
	if sourceLOC < 0 {
		sourceLOC = 0
	}
	var ratio *float64
	if sourceLOC > 0 {
		rounded := round4(float64(docLOC) / float64(sourceLOC))
		ratio = &rounded
	}
	nextStep := ""
	if tier < len(NextSteps) {
		nextStep = NextSteps[tier]
	}

	return Result{
		Project:      baseName(projectPath),
		Path:         projectPath,
		Tier:         tier,
		TierName:     Tiers[tier].Name,
		CodeLOC:      codeLOC,
		TestLOC:      testLOC,
		SourceLOC:    sourceLOC,
		DocLOC:       docLOC,
		DocFiles:     docFiles,
		DocRatio:     ratio,
		ContentGrade: ContentGrade(ratio),
		Selfdoc:      info,
		NextStep:     nextStep,
	}, nil
}

// Run runs `selfdoc quality`: score the project rooted at dir.
//
// It verifies dirstat is installed first ([CheckDirstat]), then scores dir as
// an absolute path -- the Python function this replaces always scored the
// process's own working directory, so the project's reported name is the
// directory's name rather than the empty name a relative "." would carry.
//
// It returns a [DirstatError] when the scan the score rests on could not be
// performed or understood. Scoring itself never fails: quality is a report,
// not a blocker, so a low tier or a failing grade is an answer, not an error.
func Run(dir string, h *effects.Handle) (Result, error) {
	if err := CheckDirstat(h); err != nil {
		return Result{}, err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, err
	}
	return ScoreProject(absolute, h)
}

// FormatSingleText renders a [ScoreProject] result as the human-readable
// report.
//
// The report is a headline (project, tier, tier name), a one-line metrics
// summary (source LOC, test LOC when non-zero, doc LOC with the ratio as a
// percentage, file count, grade), the selfdoc adoption block or a "not
// configured" line, the tiers already completed, and the tiers still to do --
// where the immediate next tier is stated as the concrete action from
// [NextSteps] rather than as a requirement.
//
// The returned report carries no trailing newline.
func FormatSingleText(result Result) string {
	var lines []string
	tier := result.Tier
	lines = append(lines, fmt.Sprintf(
		"%s -- Tier %d / 5 (%s)", result.Project, tier, result.TierName,
	))
	lines = append(lines, "")

	ratio := "n/a"
	if result.DocRatio != nil {
		ratio = fmt.Sprintf("%.1f%%", *result.DocRatio*100)
	}
	parts := []string{fmt.Sprintf("%s source LOC", comma(result.SourceLOC))}
	if result.TestLOC > 0 {
		parts = append(parts, fmt.Sprintf("%s test LOC", comma(result.TestLOC)))
	}
	parts = append(parts, fmt.Sprintf("%s doc LOC (%s)", comma(result.DocLOC), ratio))
	parts = append(parts, fmt.Sprintf("%d files", result.DocFiles))
	parts = append(parts, fmt.Sprintf("Grade: %s", result.ContentGrade))
	lines = append(lines, strings.Join(parts, " | "))

	lines = append(lines, "")
	if result.Selfdoc.HasSelfdoc {
		lines = append(lines, "Selfdoc:")
		lines = append(lines, "  Auto-generated README    "+yesNo(result.Selfdoc.AutoREADME))
		lines = append(lines, "  Auto-generated CLAUDE    "+yesNo(result.Selfdoc.AutoCLAUDE))
		lines = append(lines, "  Custom directives        "+countOrDash(result.Selfdoc.CustomDirectives))
		lines = append(lines, "  Blog posts               "+yesNo(result.Selfdoc.HasPosts))
		lines = append(lines, "  Directive uses           "+countOrDash(result.Selfdoc.DirectiveCount))
	} else {
		lines = append(lines, "Selfdoc: not configured")
	}

	if tier > 0 {
		lines = append(lines, "")
		lines = append(lines, "Completed:")
		for rung := 1; rung <= tier; rung++ {
			lines = append(lines, fmt.Sprintf("  Tier %d -- %s", rung, Tiers[rung].Requirement))
		}
	}

	if tier < 5 {
		lines = append(lines, "")
		lines = append(lines, "To do:")
		for rung := tier + 1; rung <= 5; rung++ {
			if rung == tier+1 {
				lines = append(lines, fmt.Sprintf("  Tier %d -- %s", rung, NextSteps[tier]))
				continue
			}
			lines = append(lines, fmt.Sprintf("  Tier %d -- %s", rung, Tiers[rung].Requirement))
		}
	} else {
		lines = append(lines, "")
		lines = append(lines, "All tiers complete.")
	}

	return strings.Join(lines, "\n")
}

// yesNo renders a flag the way the adoption block spells it.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// countOrDash renders a count, or a dash when there is nothing to count --
// the Python idiom `value or '-'`, which reads a zero as absence.
func countOrDash(value int) string {
	if value == 0 {
		return "-"
	}
	return strconv.Itoa(value)
}

// comma renders n with a thousands separator, the way Python's "{:,}" format
// spec does.
func comma(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var out strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return sign + out.String()
}

// round4 rounds a ratio to four decimals the way Python's round(value, 4)
// does, including its half-to-even answer on an exact tie.
func round4(value float64) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(value, 'f', 4, 64), 64)
	if err != nil {
		return value
	}
	return rounded
}

// baseName is the project directory's own name, as Python's Path.name reads
// it: the last component, and the empty string for a path that has none (the
// filesystem root, or a bare ".").
func baseName(path string) string {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}
