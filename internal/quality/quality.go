// Package quality scores a project's documentation: a maturity tier (0-5) and
// a content grade (A-F).
//
// Two independent axes describe a project's documentation:
//
//   - the tier (0-5, see [Tiers]) is a ladder of selfdoc feature adoption --
//     markdown exists, selfdoc.json exists, root files are generated from
//     templates, directives connect docs to source, custom directives or blog
//     posts are in use. Each rung requires every rung below it, so the tier is
//     the first unmet requirement minus one, and [NextSteps] names the action
//     that reaches the next rung.
//   - the content grade (A-F, see [ContentGrade]) measures volume:
//     documentation lines divided by non-test source lines.
//
// Source lines come from the external dirstat binary (a hard requirement, see
// [CheckDirstat]); documentation lines, test lines, and directive usage are
// counted by walking the tree here. Git submodules are excluded from every
// count, so a project is scored on the code it actually owns.
//
// [Run] is the entry point behind `selfdoc quality`.
package quality

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
)

// scanTimeout is how long one dirstat scan may run. A scan that hangs is a
// report that never arrives, and source LOC has no second source to fall back
// on. It is a var only so the suite can shorten it instead of waiting a real
// minute; the refusal it produces names 60s, which is the value that ships.
var scanTimeout = 60 * time.Second

// CodeExtensions is every file extension this package counts as code. Markup,
// data and lockfiles are deliberately absent: a project is graded on
// documentation against source, and a lockfile is neither.
var CodeExtensions = map[string]bool{
	"py": true, "go": true, "rs": true, "c": true, "cpp": true, "h": true,
	"hpp": true, "java": true, "rb": true, "kt": true,
	"js": true, "ts": true, "jsx": true, "tsx": true, "mjs": true, "cjs": true,
	"html": true, "css": true, "scss": true, "sass": true, "less": true,
	"svelte": true, "vue": true,
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"makefile": true, "cmake": true, "dockerfile": true,
	"sql": true, "swift": true,
	"hcl": true, "tf": true, "nix": true, "proto": true, "graphql": true,
	"gd": true, "gdshader": true,
}

// SkipDirs is every directory name the tree walks step over: version control,
// dependency trees, build output and tool caches.
var SkipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "__pycache__": true,
	"vendor": true, "dist": true, "build": true, ".next": true, "out": true,
	".cache": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	".ruff_cache": true,
}

// SkipMarkdownFiles is every Markdown filename that is not documentation. A
// generated changelog is a record of releases, not prose somebody wrote.
var SkipMarkdownFiles = map[string]bool{"CHANGELOG.md": true}

// Tier is one rung of the maturity ladder: its short label and the
// requirement it states.
type Tier struct {
	// Name is the rung's label, as the headline renders it.
	Name string
	// Requirement is what the rung asks for, as the completed and to-do
	// listings render it.
	Requirement string
}

// Tiers is the maturity ladder, indexed by tier number.
var Tiers = [6]Tier{
	{"None", "No markdown documentation"},
	{"Basic", "Has markdown documentation"},
	{"Selfdoc", "selfdoc.json configured"},
	{"Templates", "Auto-generated root files (README/CLAUDE)"},
	{"Directives", "Directives connect docs to source code"},
	{"Advanced", "Custom directives or blog posts"},
}

// NextSteps names the concrete action that reaches the next rung, indexed by
// the tier the project is on. There is no entry for tier 5, where nothing is
// left to do.
var NextSteps = [5]string{
	"Create a README.md with project description and usage",
	"Run `selfdoc init` to create selfdoc.json",
	"Add docs/_README.md to root_files in selfdoc.json",
	"Use :-: directives in docs/ to connect docs to source code",
	"Define custom directives or configure blog posts",
}

// DirstatError reports that dirstat could not be run, or answered in a shape
// this package cannot read.
//
// It is the Go counterpart of the Python surface's DirstatError, and the one
// error type a caller needs to recognize with errors.As to print one refusal
// line instead of an unexpected internal failure.
type DirstatError struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *DirstatError) Error() string { return e.Message }

// DirstatMissingError reports that the dirstat binary is not installed.
//
// It is a distinct type because it is the one condition [CheckDirstat]
// answers, and the message is the two lines the command prints to stderr
// before exiting non-zero. The Python function it replaces terminated the
// process from inside a library; here the library reports and the CLI decides.
type DirstatMissingError struct{}

// Error returns the refusal and the install command, one per line.
func (e *DirstatMissingError) Error() string {
	return "error: dirstat is not installed\n" +
		"install: go install github.com/smm-h/dirstat/cmd/dirstat@v0"
}

// CheckDirstat returns a [DirstatMissingError] unless the dirstat binary is
// runnable.
//
// Source-line counting has no in-tree fallback, so a missing dirstat would
// silently report every project as 0 source LOC. This probes it once up front
// (`dirstat scan --help`). Only absence is a refusal: a non-zero exit from the
// probe itself is ignored, since it still proves the binary exists.
func CheckDirstat(h *effects.Handle) error {
	_, err := h.Run(
		[]string{"dirstat", "scan", "--help"},
		effects.CaptureOutput(),
		effects.Read(),
	)
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return &DirstatMissingError{}
	}
	return err
}

// gitmodulesPathRE matches a path declaration in a .gitmodules file.
var gitmodulesPathRE = regexp.MustCompile(`(?m)^[ \t]*path[ \t]*=[ \t]*(.+)$`)

// SubmodulePaths returns the path entries declared in the project's
// .gitmodules.
//
// The paths are returned as written (repository-relative, e.g.
// "vendor/theme") and are used by every counter here to keep submodule content
// out of a project's own totals. The result is empty when there is no
// .gitmodules file or it cannot be read.
func SubmodulePaths(projectPath string) []string {
	text, err := os.ReadFile(filepath.Join(projectPath, ".gitmodules"))
	if err != nil {
		return nil
	}
	matches := gitmodulesPathRE.FindAllStringSubmatch(string(text), -1)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, strings.TrimRight(match[1], "\r"))
	}
	return paths
}

// scanGroups runs `dirstat scan` over target and returns its format groups.
//
// dirstat's machine output is the strictcli envelope: stdout carries one
// document whose payload member is the scan document. Every way this can go
// wrong is a [DirstatError]. Nothing here degrades to a zero, because source
// LOC has no second source: a swallowed failure would not make the report
// incomplete, it would make it wrong -- 0 source LOC grades every project an A
// on an infinite doc ratio.
func scanGroups(target string, h *effects.Handle) ([]any, error) {
	argv := []string{
		"dirstat", "scan", target,
		"--json",
		"--type", "text",
		"--stats", "count",
		"--stats", "total-loc",
	}
	result, err := h.Run(
		argv,
		effects.CaptureOutput(),
		effects.Timeout(scanTimeout),
		effects.Read(),
	)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, &DirstatError{Message: fmt.Sprintf("dirstat is not installed: %s", err)}
		}
		if errors.Is(err, effects.ErrTimeout) {
			return nil, &DirstatError{Message: fmt.Sprintf(
				"dirstat scan of %s timed out after 60s", target,
			)}
		}
		return nil, err
	}

	if result.ExitCode != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = "no error output"
		}
		return nil, &DirstatError{Message: fmt.Sprintf(
			"dirstat scan of %s exited %d: %s", target, result.ExitCode, detail,
		)}
	}

	var envelope any
	if err := json.Unmarshal(result.Stdout, &envelope); err != nil {
		return nil, &DirstatError{Message: fmt.Sprintf(
			"dirstat scan of %s produced output that is not valid JSON: %s",
			target, err,
		)}
	}
	table, ok := envelope.(map[string]any)
	if !ok {
		return nil, &DirstatError{Message: fmt.Sprintf(
			"dirstat scan of %s did not answer with an envelope carrying a payload",
			target,
		)}
	}
	payload, declared := table["payload"]
	if !declared {
		return nil, &DirstatError{Message: fmt.Sprintf(
			"dirstat scan of %s did not answer with an envelope carrying a payload",
			target,
		)}
	}
	document, ok := payload.(map[string]any)
	if !ok {
		return nil, &DirstatError{Message: fmt.Sprintf(
			"dirstat scan of %s answered with an empty payload", target,
		)}
	}
	groups, ok := document["groups"].([]any)
	if !ok {
		return nil, &DirstatError{Message: fmt.Sprintf(
			"dirstat scan of %s answered with no groups array", target,
		)}
	}
	return groups, nil
}

// codeTotals sums LOC and file counts over the groups this package calls code.
func codeTotals(groups []any) (int, int) {
	codeLOC, codeFiles := 0, 0
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		format, _ := group["format"].(string)
		if !CodeExtensions[strings.ToLower(format)] {
			continue
		}
		codeLOC += asInt(group["total_loc"])
		codeFiles += asInt(group["count"])
	}
	return codeLOC, codeFiles
}

// asInt reads a decoded JSON number as an int, answering zero for an absent,
// null or non-numeric member -- the Python idiom `group.get(key) or 0`.
func asInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

// CodeLOC returns the project's code LOC and file count, submodules excluded.
//
// It runs `dirstat scan` over projectPath and keeps only the file-format
// groups whose extension appears in [CodeExtensions] -- markup, data and
// lockfiles are therefore not code. Because dirstat scans the whole tree, each
// path in submodulePaths is scanned separately and subtracted; overlapping
// entries (a submodule nested inside another submodule) are subtracted once
// each, so the totals can go negative.
//
// It returns a [DirstatError] when any scan fails, times out (60s per scan),
// or answers in a shape this package cannot read -- the submodule scans
// included, since a subtraction that silently did not happen inflates the
// total it was there to correct.
func CodeLOC(projectPath string, submodulePaths []string, h *effects.Handle) (int, int, error) {
	groups, err := scanGroups(projectPath, h)
	if err != nil {
		return 0, 0, err
	}
	codeLOC, codeFiles := codeTotals(groups)

	for _, relative := range submodulePaths {
		subDir := filepath.Join(projectPath, filepath.FromSlash(relative))
		info, statErr := os.Stat(subDir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		subGroups, err := scanGroups(subDir, h)
		if err != nil {
			return 0, 0, err
		}
		subLOC, subFiles := codeTotals(subGroups)
		codeLOC -= subLOC
		codeFiles -= subFiles
	}

	return codeLOC, codeFiles, nil
}
