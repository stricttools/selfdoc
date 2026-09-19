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
		"%s selfdoc keeps every directory it owns under %s/ now, so %s becomes %s.\n\n%s",
		e.Detail, Root, e.Found, e.Replacement, MigrationProcedure())
}

// PreFlipRelease is the last selfdoc release that reads the layout this one
// replaced. Step 3 of [MigrationProcedure] builds the site with it, because
// only a release that can still read the repository as it stands today can
// publish the URL set the move is then held to.
const PreFlipRelease = "v0.41.0"

// MigrationProcedure is the whole ordered move onto this layout, as a refusal
// prints it.
//
// The move has a chain of prerequisites, and each one used to be discovered
// only by running into the next refusal: the move needs the URL set the site
// publishes today, that set comes from a build made with the last pre-flip
// release, that build refuses a selfdoc.json with no "versions" or "locales"
// array, and it refuses every document still carrying the retired "---"
// frontmatter block -- including the generated pages the converter leaves alone
// unless it is told to take them too. The chain is printed in full, in order,
// by the first refusal a repository meets.
func MigrationProcedure() string {
	return strings.Join([]string{
		"Move this repository onto the layout, in this order:",
		"",
		"  1. Declare \"versions\" and \"locales\" in selfdoc.json if they are",
		"     absent -- the build in step 3 refuses a config carrying neither:",
		"       \"versions\": [{\"version\": \"0.1.0\"}]",
		"       \"locales\": [{\"code\": \"en\", \"label\": \"English\", \"default\": true}]",
		"",
		"  2. Convert every document still on the retired \"---\" frontmatter,",
		"     the generated pages included -- the build in step 3 refuses each",
		"     one, and the converter leaves a generated page alone unless it is",
		"     told to take it:",
		"       " + scripts.Fetch(scripts.ConvertFrontmatter),
		"       " + scripts.Run(scripts.ConvertFrontmatter, "--include-generated", "--dry-run"),
		"       " + scripts.Run(scripts.ConvertFrontmatter, "--include-generated", "--apply"),
		"",
		"  3. Build the site once with the last release that reads the old",
		"     layout, which writes the sitemap the move holds its own result to:",
		"       go run github.com/smm-h/selfdoc@" + PreFlipRelease + " build",
		"",
		"  4. Create " + Root + "/. It is the repository's own grant of permission:",
		"     the move writes each directory's " + ManifestFileName + " inside it, and",
		"     never the directory itself:",
		"       mkdir " + Root,
		"",
		"  5. Fetch the move script and run it:",
		"       " + scripts.Fetch(scripts.Move),
		"       " + scripts.Run(scripts.Move, "--dry-run"),
		"       " + scripts.Run(scripts.Move, "--apply"),
	}, "\n")
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
