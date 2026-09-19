package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/smm-h/selfdoc/internal/scripts"
)

// DeprecatedRoot is the directory selfdoc kept a repository's state in before
// this layout. Its presence is what a refusal recognizes: nothing reads it.
const DeprecatedRoot = ".selfdoc"

// OldLayoutError is a repository refused for still being laid out the way
// selfdoc used to lay one out.
//
// There is no migrator and no dual reading: a repository is moved once, by
// hand, with the script [MoveScript] names, and until it is, every command
// that reads project state refuses it.
type OldLayoutError struct {
	// Found is what was found, as the repository spells it.
	Found string
	// Replacement is where that thing lives now.
	Replacement string
	// Detail says what was found and why it is refused.
	Detail string
}

func (e *OldLayoutError) Error() string {
	return fmt.Sprintf(
		"%s selfdoc keeps every directory it owns under %s/ now, so %s becomes %s. "+
			"Move this repository by fetching the move script and running it: "+
			"'%s', then '%s', then '%s'.",
		e.Detail, Root, e.Found, e.Replacement,
		scripts.Fetch(scripts.Move),
		scripts.Run(scripts.Move, "--dry-run"),
		scripts.Run(scripts.Move, "--apply"))
}

// RefuseOldLayout returns an [OldLayoutError] when a repository has not been
// moved to this layout yet.
//
// It is called from the config loader, so every command that reads project
// state refuses before it reads anything. The three declared paths are the
// config keys that name a layout directory; an empty one is taken as undeclared
// and therefore already the default, which is inside [Root].
func RefuseOldLayout(baseDir, docsDeclared, outputDeclared, postsDeclared string) error {
	if _, err := os.Stat(filepath.Join(baseDir, DeprecatedRoot)); err == nil {
		return &OldLayoutError{
			Found:       DeprecatedRoot + "/",
			Replacement: DocsStateRel + "/ and " + PostsRel + "/",
			Detail:      fmt.Sprintf("This repository still has a %s/ directory, which selfdoc no longer reads.", DeprecatedRoot),
		}
	}
	for _, declaredKey := range []struct{ key, value, replacement string }{
		{"docs", docsDeclared, DocsDefault},
		{"output", outputDeclared, OutputDefault},
		{"posts.dir", postsDeclared, PostsDefault},
	} {
		if strings.TrimSpace(declaredKey.value) == "" || UnderRoot(declaredKey.value) {
			continue
		}
		return &OldLayoutError{
			Found:       fmt.Sprintf("the %q key's %q", declaredKey.key, declaredKey.value),
			Replacement: fmt.Sprintf("%q", declaredKey.replacement),
			Detail: fmt.Sprintf(
				"selfdoc.json declares %q: %q, which is outside %s/.",
				declaredKey.key, declaredKey.value, Root),
		}
	}
	return nil
}
