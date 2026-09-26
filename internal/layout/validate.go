package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Problem is one thing wrong with a repository's layout: what is wrong, and
// what to do about it.
type Problem struct {
	// Check names the rule that failed: "ownership", "side", "hidden" or
	// "ignore-file". "hidden" covers both halves of the dot rule: a directory
	// selfdoc owns whose leading dot disagrees with its side, and a dotted
	// entry inside one of its committed directories.
	Check string
	// Message states the defect and names the remedy.
	Message string
}

// Error renders a problem the way the command prints it.
func (p Problem) Error() string { return "[" + p.Check + "] " + p.Message }

// The names of the rules [Validate] holds a repository to.
const (
	CheckOwnership = "ownership"
	CheckSide      = "side"
	CheckHidden    = "hidden"
	CheckIgnore    = "ignore-file"
)

// Validate checks one repository's layout and returns every problem it finds,
// in check order.
//
// The rules: every directory under [Root] carries a [ManifestFileName] naming
// a tool this machine has, and every directory selfdoc claims that exists
// names selfdoc; every directory selfdoc owns carries the leading dot its side
// calls for; every directory selfdoc owns holds only what its side allows;
// nothing inside selfdoc's committed directories starts with a dot; and the
// derived ignore file's selfdoc block is what the declaration says it should
// be. A directory another tool owns is held to the manifest rule only: its
// name and its contents are its owner's to judge.
//
// A missing [Root] is returned as an error rather than a problem: the rest of
// the rules are unanswerable without it. So is a repository still on the
// layout before this one, which [RefuseUnmigrated] names.
func Validate(baseDir string) ([]Problem, error) {
	if err := RefuseUnmigrated(baseDir); err != nil {
		return nil, err
	}
	rootPath := Path(baseDir, Root)
	info, err := os.Stat(rootPath)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"%s/ is missing, so this repository has not granted selfdoc any directory. Run 'selfdoc init', which creates it and writes the %s of each directory selfdoc needs -- %s holding:\n%s",
			Root, ManifestFileName, DirectoryManifestRel(DocsName),
			strings.TrimRight(DirectoryManifestContent(Owner), "\n"))
	}

	owners, problems := ownershipProblems(baseDir)
	problems = append(problems, sideProblems(baseDir, owners)...)
	problems = append(problems, hiddenProblems(baseDir, owners)...)
	problems = append(problems, ignoreProblems(baseDir)...)
	return problems, nil
}

// ownershipProblems reads every directory's manifest under [Root] and reports
// the ones that declare nothing, declare a tool this machine does not have,
// declare another tool for a directory selfdoc claims, or name a directory
// selfdoc owns with a dot its side does not call for.
//
// It returns the owner each entry declares, keyed by the name the entry carries
// on disk, which is what the side and hidden rules read to decide whose
// directory they are looking at.
func ownershipProblems(baseDir string) (map[string]string, []Problem) {
	owners := map[string]string{}
	entries, err := os.ReadDir(Path(baseDir, Root))
	if err != nil {
		return owners, []Problem{{Check: CheckOwnership, Message: err.Error()}}
	}
	var problems []Problem
	for _, entry := range entries {
		name := entry.Name()
		if name == IgnoreFileName {
			continue
		}
		shown := Root + "/" + name
		if !entry.IsDir() {
			problems = append(problems, Problem{
				Check: CheckOwnership,
				Message: fmt.Sprintf(
					"%s is a file, and %s holds directories: one function per directory, one owner per function. Move it into the directory of the function it belongs to.",
					shown, Root),
			})
			continue
		}
		claimedDir, claimed := LookupFunction(name)
		manifest, readErr := readEntryManifest(baseDir, name)
		if errors.Is(readErr, os.ErrNotExist) {
			suggested := Owner
			if !claimed {
				suggested = "<tool>"
			}
			problems = append(problems, Problem{
				Check: CheckOwnership,
				Message: fmt.Sprintf(
					"%s carries no %s, so nothing declares who owns it. Create %s holding this line:\n%s",
					shown, ManifestFileName, entryManifestRel(name),
					strings.TrimRight(DirectoryManifestContent(suggested), "\n")),
			})
			continue
		}
		if readErr != nil {
			problems = append(problems, Problem{Check: CheckOwnership, Message: readErr.Error()})
			continue
		}
		owners[name] = manifest.Owner
		if !KnownOwner(manifest.Owner) {
			problems = append(problems, Problem{
				Check: CheckOwnership,
				Message: fmt.Sprintf(
					"%s declares %q as the owner of %s, and this machine has no such tool. An owner is %q itself, or a name PATH answers with an executable.",
					entryManifestRel(name), manifest.Owner, shown, Owner),
			})
		}
		if !claimed {
			continue
		}
		if manifest.Owner != Owner {
			problems = append(problems, Problem{
				Check: CheckOwnership,
				Message: fmt.Sprintf(
					"%s is a directory selfdoc claims, and %s declares %q as its owner. Write this line instead, or rename the directory to one selfdoc does not claim:\n%s",
					shown, entryManifestRel(name), manifest.Owner,
					strings.TrimRight(DirectoryManifestContent(Owner), "\n")),
			})
			continue
		}
		if name != claimedDir.DiskName() {
			problems = append(problems, Problem{
				Check:   CheckHidden,
				Message: dotMismatch(claimedDir, shown),
			})
		}
	}
	return owners, problems
}

// dotMismatch words the refusal of a directory selfdoc owns whose leading dot
// disagrees with its side, naming the rename that clears it.
func dotMismatch(dir Directory, shown string) string {
	if dir.Side == Generated {
		return fmt.Sprintf(
			"%s is %s, and a generated directory's name starts with a dot. Rename it to %s.",
			shown, dir.Side, dir.Rel())
	}
	return fmt.Sprintf(
		"%s is %s, and only a generated directory's name starts with a dot. Rename it to %s.",
		shown, dir.Side, dir.Rel())
}

// sideProblems reports the files sitting on the wrong side of the authorship
// line in the directories selfdoc owns.
//
// A Markdown file carrying selfdoc's generated-page marker belongs in the
// generated pages directory; one without it belongs in the handwritten docs
// directory. The uncommitted cache is not checked: it holds extracted
// checkouts and built output, which carry whatever the source tree carries.
func sideProblems(baseDir string, owners map[string]string) []Problem {
	var problems []Problem
	for _, dir := range Declared() {
		if owners[dir.DiskName()] != Owner || dir.Commitment == Uncommitted {
			continue
		}
		root := Path(baseDir, dir.Rel())
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(full string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			content, readErr := os.ReadFile(full)
			if readErr != nil {
				return nil
			}
			marked := strings.Contains(string(content), GeneratedMarkerPrefix)
			shown := showPath(baseDir, full)
			switch {
			case dir.Side == Handwritten && marked:
				problems = append(problems, Problem{
					Check: CheckSide,
					Message: fmt.Sprintf(
						"%s carries selfdoc's generated-page marker but sits in %s, which is handwritten. Move it under %s, or delete the marker if a person wrote the page.",
						shown, dir.Rel(), GeneratedPagesRel),
				})
			case dir.Side == Generated && !marked:
				problems = append(problems, Problem{
					Check: CheckSide,
					Message: fmt.Sprintf(
						"%s carries no generated-page marker but sits in %s, which is generated. Move it under %s, where handwritten pages live.",
						shown, dir.Rel(), DocsRel),
				})
			}
			return nil
		})
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].Message < problems[j].Message })
	return problems
}

// hiddenProblems reports the entries inside selfdoc's committed directories
// whose names start with a dot.
//
// The leading dot marks a generated directory at the top level of [Root] and
// means nothing deeper, so nothing below that level carries one. Only the
// committed directories are walked through: an uncommitted one holds extracted
// checkouts, whose dotted entries are the source tree's. A directory another
// tool owns is its owner's to judge.
func hiddenProblems(baseDir string, owners map[string]string) []Problem {
	var problems []Problem
	for _, dir := range Declared() {
		if owners[dir.DiskName()] != Owner || dir.Commitment == Uncommitted {
			continue
		}
		root := Path(baseDir, dir.Rel())
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(full string, walked os.DirEntry, err error) error {
			if err != nil || full == root || !strings.HasPrefix(walked.Name(), ".") {
				return nil
			}
			problems = append(problems, Problem{
				Check: CheckHidden,
				Message: fmt.Sprintf(
					"%s starts with a dot. The leading dot marks a generated directory at the top level of %s/ and means nothing deeper, so nothing inside %s carries one. Rename it.",
					showPath(baseDir, full), Root, dir.Rel()),
			})
			if walked.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})
	}
	return problems
}

// ignoreProblems reports a derived ignore file that does not carry selfdoc's
// block as the declaration renders it, with the content it should hold.
func ignoreProblems(baseDir string) []Problem {
	current, wanted := IgnoreIsCurrent(baseDir)
	if current {
		return nil
	}
	return []Problem{{
		Check: CheckIgnore,
		Message: fmt.Sprintf(
			"%s is not what selfdoc's commitment declaration renders. Run 'selfdoc build', which rewrites it. It should hold:\n%s",
			Root+"/"+IgnoreFileName, strings.TrimRight(wanted, "\n")),
	}}
}

// showPath renders an absolute path the way a diagnostic names it: relative to
// the repository root when it is inside one, in slash form.
func showPath(baseDir, full string) string {
	rel, err := filepath.Rel(baseDir, full)
	if err != nil {
		return filepath.ToSlash(full)
	}
	return filepath.ToSlash(rel)
}
