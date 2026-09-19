package editor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/stricttools/selfdoc/internal/util"
)

// What the editor can say about a buffer that was never saved.
//
// Two lanes, both answered from the machinery the check already owns rather
// than from a second opinion written for the editor:
//
//   - spelling -- [github.com/stricttools/selfdoc/internal/spelling], the same
//     engine the check runs (SPELL001) and the corpus sweep runs over the
//     fleet: the same masks, the same vendored word list, the same
//     machine-local accept list. Its coordinates are line and column, because
//     that is what a diagnostic in a terminal needs; the editor's decoration
//     interface takes flat character offsets over the buffer, so the one
//     thing this adds is that mapping.
//   - lints -- [github.com/stricttools/selfdoc/internal/check.LintPostBuffer],
//     which overlays the buffer on the saved post set and runs the project's
//     real lint rules over the result. No rule is restated here, so a mark on
//     screen is a finding the check will report, worded identically.
//
// Two deliberate decisions about what comes back:
//
//   - Drafts are judged. The build excludes a draft because it is not on the
//     site; the editor includes it because a draft is what is being written,
//     and a defect found after publishing is found too late.
//   - SPELL001 is dropped from the lint lane. It is the same engine's
//     finding, and the spelling lane already carries it with the column the
//     editor needs. Reporting it in both lanes would put one misspelling in
//     two places -- an inline mark and a gutter marker -- for no added
//     information.
//
// A buffer that is not a valid post does not lose its diagnostics: the post
// parser's refusal is reported as the POST00x lint the check reports it as,
// through the same mapping
// ([github.com/stricttools/selfdoc/internal/check.PostErrorLint]).

// spellingCode is the lint code the spelling lane owns. Dropped from the lint
// lane so one misspelling is one finding.
const spellingCode = "SPELL001"

// SpellingFinding is one unrecognized word, in both coordinate systems: Line
// and Column (1-based, what a diagnostic reads like) and From / To (half-open
// character offsets over the whole buffer, what the editor's decoration
// interface takes).
type SpellingFinding struct {
	From        int      `json:"from"`
	To          int      `json:"to"`
	Line        int      `json:"line"`
	Column      int      `json:"column"`
	Word        string   `json:"word"`
	Suggestions []string `json:"suggestions"`
	Message     string   `json:"message"`
}

// LintFinding is one lint the project's rules report for a buffer. Line is
// nil for a page-level finding.
type LintFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Line     *int   `json:"line"`
	Message  string `json:"message"`
}

// Analysis is both lanes for one buffer, in the shape the shell renders.
type Analysis struct {
	Spelling []SpellingFinding `json:"spelling"`
	Lints    []LintFinding     `json:"lints"`
}

// lineStarts returns the character offset of the first character of every
// line (line N is index N-1).
func lineStarts(text []rune) []int {
	starts := []int{0}
	for index, char := range text {
		if char == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}

// SpellingFindings returns every unrecognized word in content, as editor
// decoration spans.
//
// content is the buffer, frontmatter included, and file is the name the
// engine puts on each diagnostic.
//
// Offsets are character offsets, not byte offsets: the editor holds the
// buffer as text and paints over character positions, and the engine's
// columns are character columns too.
//
// It is an error when a reported word is not at the offset the mapping
// computes. That is a defect in this mapping or in the engine's columns, and
// painting a mark over the wrong word is worse than saying so.
func SpellingFindings(content, file string) ([]SpellingFinding, error) {
	body, err := util.StripFrontmatter(content, file)
	if err != nil {
		return nil, err
	}
	frontmatterLines := len(strings.Split(content, "\n")) - len(strings.Split(body, "\n"))

	runes := []rune(content)
	starts := lineStarts(runes)
	misspellings, err := spelling.CheckText(body, file, nil, nil, frontmatterLines, true)
	if err != nil {
		return nil, err
	}

	findings := make([]SpellingFinding, 0, len(misspellings))
	for _, miss := range misspellings {
		if miss.Line < 1 || miss.Line > len(starts) {
			return nil, fmt.Errorf(
				"spelling reported line %d in a buffer of %d line(s)",
				miss.Line, len(starts),
			)
		}
		start := starts[miss.Line-1] + miss.Column - 1
		end := start + len([]rune(miss.Word))
		if sliceRunes(runes, start, end) != miss.Word {
			return nil, fmt.Errorf(
				"spelling offset %d..%d holds %s, not %s -- the "+
					"line/column to offset mapping is wrong",
				start, end,
				util.PythonRepr(sliceRunes(runes, start, end)),
				util.PythonRepr(miss.Word),
			)
		}
		suggestions := miss.Suggestions
		if suggestions == nil {
			suggestions = []string{}
		}
		findings = append(findings, SpellingFinding{
			From:        start,
			To:          end,
			Line:        miss.Line,
			Column:      miss.Column,
			Word:        miss.Word,
			Suggestions: append([]string{}, suggestions...),
			Message:     miss.Describe(),
		})
	}
	return findings, nil
}

// sliceRunes returns the characters between start and end, clamped the way
// Python clamps a string slice rather than refusing one.
func sliceRunes(runes []rune, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

// LintFindings returns every lint the project's rules report for this buffer.
//
// entry is the local registry entry the post belongs to, rel is the post's
// path relative to the posts directory, content is the buffer with its
// frontmatter, and a nil cfg loads the project config.
func LintFindings(
	entry registry.Entry, rel, content string,
	cfg config.Config, handle *effects.Handle,
) ([]LintFinding, error) {
	path, err := RequireLocal(entry)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		loaded, configErr := RepoConfig(entry)
		if configErr != nil {
			return nil, configErr
		}
		cfg = loaded
	}
	postsDir := postsDirRel(cfg)

	diagnostics, lintErr := check.LintPostBuffer(path, rel, content, cfg, handle)
	var postError *posts.PostError
	if errors.As(lintErr, &postError) {
		diagnostics = []lints.LintResult{check.PostErrorLint(postError, postsDir)}
	} else if lintErr != nil {
		return nil, lintErr
	}

	findings := make([]LintFinding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code() == spellingCode {
			continue
		}
		findings = append(findings, LintFinding{
			Code:     diagnostic.Code(),
			Severity: diagnostic.Severity(),
			Line:     diagnostic.Line(),
			Message:  diagnostic.Message(),
		})
	}
	return findings, nil
}

// AnalyzeBuffer returns both lanes for one buffer, in the shape the shell
// renders.
func AnalyzeBuffer(
	entry registry.Entry, rel, content string,
	cfg config.Config, handle *effects.Handle,
) (Analysis, error) {
	if _, err := RequireLocal(entry); err != nil {
		return Analysis{}, err
	}
	if cfg == nil {
		loaded, err := RepoConfig(entry)
		if err != nil {
			return Analysis{}, err
		}
		cfg = loaded
	}
	spellingFindings, err := SpellingFindings(content, rel)
	if err != nil {
		return Analysis{}, err
	}
	lints, err := LintFindings(entry, rel, content, cfg, handle)
	if err != nil {
		return Analysis{}, err
	}
	return Analysis{Spelling: spellingFindings, Lints: lints}, nil
}
