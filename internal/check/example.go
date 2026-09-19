package check

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/tokenizer"
)

// EXAMPLE002/EXAMPLE003 -- opt-in semantic example validation.
//
// EXAMPLE001 parses a fenced block; it cannot tell a program that compiles
// from a program that works. A "validate" token in the fence info string opts
// a block into the semantic tier: selfdoc writes it to a scratch file and
// hands the path to the command configured for that language under the
// "examples" config key. The marker is opt-in because most documentation
// snippets are deliberately partial -- an opt-out polarity would flag them
// all. A marker whose language has no configured command is EXAMPLE003, a hard
// error rather than a silent skip: a marker that validates nothing is
// indistinguishable from a passing one, which is the defect the whole tier
// exists to remove.
//
// No sandbox: the configured validators compile and register, they are not a
// harness for running untrusted payloads, and the snippets are the project's
// own documentation.

// exampleValidateTimeout is how long a configured validator may run before it
// is killed. selfdoc's "external calls must have timeouts" convention -- no
// unbounded wait.
const exampleValidateTimeout = 60 * time.Second

// exampleStderrTailLines is how many trailing output lines of a failing
// validator reach the message.
const exampleStderrTailLines = 5

// exampleSuffixes is the scratch-file suffix per fenced-block language.
//
// Validators dispatch on the extension (a Go toolchain will not look at a file
// that is not "*.go"), so the marked language has to reach disk under a name
// that names it.
var exampleSuffixes = map[string]string{
	"python": ".py", "py": ".py", "python3": ".py",
	"go": ".go", "golang": ".go",
	"ts": ".ts", "typescript": ".ts",
	"js": ".js", "javascript": ".js", "jsx": ".jsx", "tsx": ".tsx",
	"json": ".json",
	"rust": ".rs", "rs": ".rs",
	"sh": ".sh", "bash": ".sh", "shell": ".sh",
	"c": ".c", "cpp": ".cpp", "c++": ".cpp",
	"java": ".java", "kotlin": ".kt", "kt": ".kt",
	"swift": ".swift", "dart": ".dart", "zig": ".zig",
	"ruby": ".rb", "rb": ".rb",
	"sql": ".sql", "toml": ".toml",
	"yaml": ".yaml", "yml": ".yml",
	"svelte": ".svelte",
}

// nonAlphanumeric matches everything stripped out of an unknown fence
// language before it becomes a file suffix.
var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]`)

// exampleSuffix returns the scratch-file suffix for a fenced-block language.
func exampleSuffix(lang string) string {
	if known, present := exampleSuffixes[lang]; present {
		return known
	}
	cleaned := nonAlphanumeric.ReplaceAllString(strings.ToLower(lang), "")
	if cleaned == "" {
		return ".txt"
	}
	return "." + cleaned
}

// exampleOutputTail collapses a failing validator's output into one
// message-sized line.
func exampleOutputTail(result effects.Result) string {
	text := string(result.Stderr)
	if text == "" {
		text = string(result.Stdout)
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("exit status %d, no output", result.ExitCode)
	}
	if len(lines) > exampleStderrTailLines {
		lines = lines[len(lines)-exampleStderrTailLines:]
	}
	return strings.Join(lines, " | ")
}

// validateExampleBlock executes one "validate"-marked block and returns its
// EXAMPLE002 diagnostic, or nil when the block passed.
//
// The block's raw text is written to a scratch file whose suffix names the
// language, "{file}" in commandTemplate is replaced with that path, and the
// result runs through the effects chokepoint. Under a preview the run is
// recorded rather than executed, so there is no verdict to report and the
// block yields no diagnostic.
func validateExampleBlock(
	block tokenizer.CodeBlock,
	relPath, commandTemplate, cwd string,
	handle *effects.Handle,
) (*lints.LintResult, error) {
	argvTemplate, err := SplitShellWords(commandTemplate)
	if err != nil {
		return nil, fmt.Errorf(
			"examples command for %q cannot be split into words (%s): %w",
			block.Lang, commandTemplate, err,
		)
	}

	body := strings.Join(block.Lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}

	// The scratch directory is this call's own: created here, read by the
	// validator, and gone before the call returns. It is exempt from the
	// effects chokepoint for that reason -- nothing outside this function
	// can observe it, in any mode.
	scratch, err := os.MkdirTemp("", "selfdoc-example-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)

	snippet := filepath.Join(scratch, "example"+exampleSuffix(block.Lang))
	if err := os.WriteFile(snippet, []byte(body), 0o644); err != nil {
		return nil, err
	}

	argv := make([]string, 0, len(argvTemplate))
	for _, part := range argvTemplate {
		argv = append(argv, strings.ReplaceAll(part, "{file}", snippet))
	}

	result, err := handle.Run(
		argv,
		effects.Cwd(cwd),
		effects.CaptureOutput(),
		effects.Timeout(exampleValidateTimeout),
	)
	if err != nil {
		message := fmt.Sprintf(
			"example validator could not be run (%s): %s",
			commandTemplate, err,
		)
		if errors.Is(err, effects.ErrTimeout) {
			message = fmt.Sprintf(
				"example validator timed out after %ds: %s",
				int(exampleValidateTimeout/time.Second), commandTemplate,
			)
		}
		lint := lints.MustLintResult(
			relPath, lineOf(block.Start()), "EXAMPLE002", message,
		)
		return &lint, nil
	}
	if result.Unsettled || result.ExitCode == 0 {
		return nil, nil
	}
	lint := lints.MustLintResult(
		relPath, lineOf(block.Start()), "EXAMPLE002",
		fmt.Sprintf(
			"%s example failed validation (exit %d): %s",
			block.Lang, result.ExitCode, exampleOutputTail(result),
		),
	)
	return &lint, nil
}
