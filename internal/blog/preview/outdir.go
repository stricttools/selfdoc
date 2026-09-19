package preview

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// checkIgnoreTimeout is how long the one git query this package makes is
// given.
const checkIgnoreTimeout = 15 * time.Second

// Error is the failure every operation here reports: an output directory a
// preview may not be written to, a checkout that declares no slug, two
// checkouts declaring one slug, a missing canonical base, an unknown theme.
//
// It is the one error type a caller needs to recognize with errors.As to
// render a preview refusal distinctly from an unexpected internal failure.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

// errorf builds an [Error] from a format string.
func errorf(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// EnclosingWorktree returns the git working tree path sits in, or "" when it
// sits in none.
//
// The nearest ancestor carrying ".git" wins, and path itself counts. ".git" is
// tested for existence rather than for being a directory, so a linked worktree
// (where it is a file) is found too.
func EnclosingWorktree(path string) string {
	current, err := filepath.Abs(path)
	if err != nil {
		current = filepath.Clean(path)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(current, ".git")); statErr == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// OutDirRefusal returns why outDir may not be written to, or "" when it may.
//
// A directory outside every git working tree is always fine. Inside one, the
// only acceptable location is a path git ignores: a preview writes thousands
// of generated files, and a checkout that another session shares must not grow
// them as untracked noise.
func OutDirRefusal(outDir string, handle *effects.Handle) (string, error) {
	absolute, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	outDir = absolute
	repo := EnclosingWorktree(outDir)
	if repo == "" {
		return "", nil
	}
	rel, err := filepath.Rel(repo, outDir)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return fmt.Sprintf(
			"%s is the root of the git working tree at %s. A preview writes a "+
				"whole generated site; it cannot be written over a checkout. "+
				"Choose a directory outside any repository, or a gitignored "+
				"path inside one.", outDir, repo), nil
	}
	// The trailing slash is not cosmetic: git decides whether a
	// directory-only pattern like "preview/" applies by looking at the path's
	// type, and a path that does not exist yet -- the normal state of an
	// output directory -- is taken for a file without it. The output directory
	// is a directory by definition, so it is asked about as one.
	ignored, err := handle.Run(
		[]string{"git", "-C", repo, "check-ignore", "-q", "--", rel + "/"},
		effects.CaptureOutput(), effects.Timeout(checkIgnoreTimeout), effects.Read(),
	)
	if err != nil {
		return "", err
	}
	if ignored.ExitCode == 0 {
		return "", nil
	}
	return fmt.Sprintf(
		"%s is inside the git working tree at %s and git does not ignore it. "+
			"A preview writes thousands of generated files, which would appear "+
			"as untracked noise in every `git status` run in that checkout -- "+
			"including other sessions' -- and can be committed by accident. "+
			"Either choose a directory outside any repository, or add %s to "+
			"that repository's .gitignore first.",
		outDir, repo, util.PythonRepr(rel)), nil
}

// RefuseUnsafeOutDir reports an error when outDir is not a place a preview may
// be written.
func RefuseUnsafeOutDir(outDir string, handle *effects.Handle) error {
	reason, err := OutDirRefusal(outDir, handle)
	if err != nil {
		return err
	}
	if reason != "" {
		return &Error{Message: reason}
	}
	return nil
}
