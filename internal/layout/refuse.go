package layout

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/scripts"
)

// DeprecatedRoot is the directory selfdoc kept a repository's state in three
// layouts ago. Its presence is what a refusal recognizes: nothing reads it.
const DeprecatedRoot = ".selfdoc"

// PreviousRoot is the visible directory the layout before this one kept every
// function directory in, each under the name it carries under [Root] today.
// Nothing reads it: `selfdoc layout migrate` moves a repository off it, and
// every other command refuses a repository still on it.
const PreviousRoot = "stricttools"

// EarlierRoot is the hidden directory the layout two before this one kept
// every function directory in, each under its bare function name, generated
// ones included. Like [PreviousRoot], nothing reads it but the move.
const EarlierRoot = ".stricttools"

// MigrateCommand is the command that moves a repository off a previous root.
const MigrateCommand = "selfdoc layout migrate"

// PreviousLayout is one root a repository may still keep selfdoc's
// directories in, and how the directories were named there.
type PreviousLayout struct {
	// Root is the directory, relative to the repository root.
	Root string
	// DottedGenerated reports whether a generated directory's name started
	// with a dot under this root, as it does under [Root].
	DottedGenerated bool
}

// PreviousLayouts are the roots `selfdoc layout migrate` moves a repository
// off, newest first.
var PreviousLayouts = []PreviousLayout{
	{Root: PreviousRoot, DottedGenerated: true},
	{Root: EarlierRoot, DottedGenerated: false},
}

// EntryName is the name a claimed directory carried under this root.
func (p PreviousLayout) EntryName(dir Directory) string {
	if p.DottedGenerated {
		return dir.DiskName()
	}
	return dir.Name
}

// Lookup returns the claimed directory an entry under this root is. Under a
// root that dotted generated directories, an entry whose dot disagrees with
// its side is still that directory, misnamed; the move names it correctly.
func (p PreviousLayout) Lookup(entry string) (Directory, bool) {
	if p.DottedGenerated {
		return LookupFunction(entry)
	}
	return Lookup(entry)
}

// UnmigratedError is a repository refused for still keeping selfdoc's
// directories under a previous root.
type UnmigratedError struct {
	// Root is the previous root the directories are under.
	Root string
	// Found are the directories under Root whose manifest names selfdoc, as
	// paths relative to the repository root.
	Found []string
}

func (e *UnmigratedError) Error() string {
	return fmt.Sprintf(
		"This repository keeps selfdoc's directories under %s/ (%s), a layout before this one, which selfdoc no longer reads. selfdoc keeps them under %s/ now, and a generated directory's name starts with a dot there. Run '%s', which moves them, rewrites the paths selfdoc.json and the generated root files name, and commits the move; '%s --dry-run' prints the plan first.",
		e.Root, strings.Join(e.Found, ", "), Root, MigrateCommand, MigrateCommand)
}

// PreviousSelfdocEntries returns the directories under a previous root whose
// manifest names selfdoc, by the name each carries there, sorted. A directory
// with no manifest, or one naming another tool, is not selfdoc's to move and
// is not returned.
func PreviousSelfdocEntries(baseDir, previousRoot string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(baseDir, previousRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(baseDir, previousRoot, entry.Name(), ManifestFileName)
		manifest, readErr := readManifestFile(manifestPath)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		if manifest.Owner == Owner {
			found = append(found, entry.Name())
		}
	}
	sort.Strings(found)
	return found, nil
}

// CurrentSelfdocEntries returns the entries under [Root] that are one of the
// directories selfdoc claims, by the name each carries on disk, sorted. An
// entry whose dot disagrees with its side still counts: it is the claimed
// directory, misnamed.
func CurrentSelfdocEntries(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(Path(baseDir, Root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []string
	for _, entry := range entries {
		if _, claimed := LookupFunction(entry.Name()); claimed && entry.IsDir() {
			found = append(found, entry.Name())
		}
	}
	sort.Strings(found)
	return found, nil
}

// RefuseUnmigrated returns an [UnmigratedError] when a repository still keeps
// selfdoc's directories under a previous root and none under [Root]: the
// repository `selfdoc layout migrate` is for.
//
// A repository holding selfdoc's directories under [Root] and a previous root
// is part-way through a move, which only the migrate command judges; every
// other command reads the current layout alone.
func RefuseUnmigrated(baseDir string) error {
	current, err := CurrentSelfdocEntries(baseDir)
	if err != nil {
		return err
	}
	if len(current) > 0 {
		return nil
	}
	for _, previousLayout := range PreviousLayouts {
		previous, err := PreviousSelfdocEntries(baseDir, previousLayout.Root)
		if err != nil {
			return err
		}
		if len(previous) == 0 {
			continue
		}
		found := make([]string, 0, len(previous))
		for _, name := range previous {
			found = append(found, previousLayout.Root+"/"+name)
		}
		return &UnmigratedError{Root: previousLayout.Root, Found: found}
	}
	return nil
}

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
		"  5. Fetch the move script and run it. With --apply it commits the",
		"     moves, then the rewritten paths, then runs 'selfdoc layout migrate',",
		"     which converts the moved manifests to the schema that records the",
		"     project's vocabulary and commits that, and last builds the site to",
		"     hold it to the URL set of step 3. The selfdoc it runs must be one",
		"     whose 'selfdoc layout migrate' converts manifests:",
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
	if err := RefuseUnmigrated(baseDir); err != nil {
		return err
	}
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
		if previousRoot, under := underPreviousRoot(declaredKey.value); under {
			return fmt.Errorf(
				"selfdoc.json declares %q: %q, which is under %s/, a layout before this one. Declare %q: %q instead.",
				declaredKey.key, declaredKey.value, previousRoot,
				declaredKey.key, MigratedPath(declaredKey.value))
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

// underPreviousRoot returns the one of the [PreviousLayouts] roots a declared
// path names something inside, reporting whether there is one.
func underPreviousRoot(declaredPath string) (string, bool) {
	clean := path.Clean(strings.TrimRight(filepath.ToSlash(declaredPath), "/"))
	for _, previousLayout := range PreviousLayouts {
		if clean == previousLayout.Root || strings.HasPrefix(clean, previousLayout.Root+"/") {
			return previousLayout.Root, true
		}
	}
	return "", false
}

// MigratedPath maps a path under one of the [PreviousLayouts] roots onto this
// layout: a directory selfdoc claims moves under [Root] with the name its side
// calls for, and the rest of the path is kept, trailing slash included. A path
// that is not inside one of selfdoc's directories under a previous root is
// returned unchanged.
func MigratedPath(previous string) string {
	slashed := filepath.ToSlash(previous)
	for _, previousLayout := range PreviousLayouts {
		rest, found := strings.CutPrefix(slashed, previousLayout.Root+"/")
		if !found {
			continue
		}
		head, tail, hasTail := strings.Cut(rest, "/")
		dir, claimed := previousLayout.Lookup(head)
		if !claimed {
			return previous
		}
		if hasTail {
			return dir.Rel() + "/" + tail
		}
		return dir.Rel()
	}
	return previous
}
