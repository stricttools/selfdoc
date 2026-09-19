// Package scripts declares selfdoc's hand-run maintenance scripts and renders
// the commands that fetch and run one.
//
// # Why a refusal cannot name a repository path
//
// The scripts live in selfdoc's own repository under scripts/, and no release
// artifact carries them: a consumer installs the binary, from the module proxy
// or from a platform archive, and never has selfdoc's checkout. A refusal that
// told such a consumer to run "python3 scripts/<name>" named a path their
// repository does not have, so the remedy could not be executed as printed.
//
// Every refusal therefore renders [Fetch] and [Run] from here: one command that
// downloads the script into the working directory, and one that runs it. The
// two script names and the address they are fetched from are declared once, in
// this package, so a message, a docs page and a test cannot disagree about
// them.
package scripts

import "strings"

// The maintenance scripts, by the filename each one carries.
const (
	// Move is the one-way move of a repository onto selfdoc's .stricttools/
	// layout. Every refusal of the old layout names it.
	Move = "move-to-stricttools-layout.py"
	// ConvertFrontmatter rewrites a document's retired "---" frontmatter
	// block into the TOML block selfdoc reads. Every refusal of a retired
	// block names it.
	ConvertFrontmatter = "convert-frontmatter-to-toml.py"
)

// RawBase is the address the scripts are served from, up to the filename.
//
// It is the repository's default branch rather than a tag: a script is
// maintained after the release that first refused without it, and the person
// running one wants the maintained version.
const RawBase = "https://raw.githubusercontent.com/stricttools/selfdoc/main/scripts/"

// RepoPath is a script's path inside selfdoc's own checkout, which is where it
// is edited and where the suite drives it.
func RepoPath(name string) string {
	return "scripts/" + name
}

// Fetch renders the command that downloads a script into the working
// directory, under its own filename.
func Fetch(name string) string {
	return "curl -fsSL " + RawBase + name + " -o " + name
}

// Run renders the command that runs a fetched script with args.
func Run(name string, args ...string) string {
	command := "python3 " + name
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}
	return command
}
