// Package ownership decides whether a generated page's frontmatter
// description is machine-owned -- a placeholder selfdoc emitted and may
// freely overwrite -- or handwritten, and must never be overwritten.
//
// The guiding principle: descriptions are handwritten; machine text is only
// ever a placeholder. The critical property is the INVERSE -- handwritten
// text must NEVER be classified machine-owned. The other direction is
// acceptable: legacy machine residue occasionally reads as handwritten when
// no seed hash was ever recorded for it, and the next gen reseeds it, since
// a release always runs gen before check.
//
// Ownership is decided per page kind:
//
//   - module pages: the current or the historical instantiated template for
//     that module, or the recorded seed hash.
//   - the generated API index: the current or a legacy index template, or the
//     recorded seed hash.
//   - CLI pages: a live recompute from the dumped schema (which covers the
//     truncated-prefix family no static set can), or the recorded seed hash.
//
// The seed hash is the SHA-256 of the machine-emitted description TEXT,
// recorded per page by gen in the staleness store. An unrecorded seed hash is
// the empty string.
package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/strictclisupport"
	"github.com/stricttools/selfdoc/internal/util"
)

// ModuleDescTemplate is the current auto-generated description of a module
// reference page, with "{module}" standing for the module's name.
//
// The predicate compares a description against this text for equality, so
// every character of it -- the em dash included -- is part of the contract:
// a changed byte turns every page carrying the old text into handwritten
// prose that gen may no longer reseed.
const ModuleDescTemplate = "API reference for the {module} module — " +
	"auto-generated documentation covering public functions, " +
	"classes, and type signatures."

// HistoricalModuleDescTemplate is the module page description a
// pre-current-template selfdoc emitted. It is still recognized, so that
// residue is reseeded rather than frozen as if a person had written it.
const HistoricalModuleDescTemplate = "Documentation for {module}"

// LegacyIndexDescriptions are the machine-seeded generated-index
// descriptions produced by selfdoc versions that recorded no seed hash.
//
// They hardcode "selfdoc" and so are wrong for every consuming project;
// they are treated as machine residue and reseeded.
var LegacyIndexDescriptions = []string{
	"Auto-generated API reference index",
	"Auto-generated API reference index for the selfdoc package — " +
		"browse all public modules with their docstrings and source locations.",
	"Complete auto-generated API reference index — browse all modules, " +
		"classes, and functions with their signatures and docstrings.",
}

// indexDescRe matches the current generated-index description formats:
//
//	API reference index for {project_name} covering {n} module(s)
//	API reference index covering {n} module(s)
//
// The project name and the module count change over time -- that is the whole
// point of staleness -- so the format is matched rather than any one
// instantiation. The digit class is Python's \d, which is every Unicode
// decimal digit.
var indexDescRe = regexp.MustCompile(
	`^API reference index(?: for .+?)? covering [\p{Nd}]+ modules?$`)

// NormalizeDescription strips surrounding whitespace and one layer of
// matching quotes, so a description written as 'x' or "x" in frontmatter
// compares equal to the same text written bare.
func NormalizeDescription(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(pyStr(value))
	if len(text) >= 2 {
		first, last := text[0], text[len(text)-1]
		if first == last && (first == '\'' || first == '"') {
			text = strings.TrimSpace(text[1 : len(text)-1])
		}
	}
	return text
}

// DescriptionSeedHash is the SHA-256 of a normalized machine-emitted
// description string. It is what gen records per page, and what the
// predicates below compare a page's current text against.
func DescriptionSeedHash(value any) string {
	sum := sha256.Sum256([]byte(NormalizeDescription(value)))
	return hex.EncodeToString(sum[:])
}

// matchesSeedHash reports whether value hashes to the recorded seed hash. An
// unrecorded seed hash ("") matches nothing.
func matchesSeedHash(value any, seedHash string) bool {
	return seedHash != "" && DescriptionSeedHash(value) == seedHash
}

// IsMachineOwnedModuleDescription reports whether value is machine-owned for
// a module page documenting moduleName.
//
// An empty moduleName means the page carries no title to instantiate the
// templates with, so only the recorded seed hash can answer.
func IsMachineOwnedModuleDescription(value any, moduleName, seedHash string) bool {
	text := NormalizeDescription(value)
	if text == "" {
		return false
	}
	if matchesSeedHash(text, seedHash) {
		return true
	}
	if moduleName != "" {
		if text == instantiate(ModuleDescTemplate, moduleName) {
			return true
		}
		if text == instantiate(HistoricalModuleDescTemplate, moduleName) {
			return true
		}
	}
	return false
}

// IsMachineOwnedIndexDescription reports whether value is a machine-owned
// description of the generated API index page.
func IsMachineOwnedIndexDescription(value any, seedHash string) bool {
	text := NormalizeDescription(value)
	if text == "" {
		return false
	}
	if matchesSeedHash(text, seedHash) {
		return true
	}
	for _, legacy := range LegacyIndexDescriptions {
		if text == legacy {
			return true
		}
	}
	return indexDescRe.MatchString(text)
}

// IsMachineOwnedCLIDescription reports whether value is a machine-owned
// description of a CLI page.
//
// CLI machine text is derivable from the dumped schema, so beyond the
// recorded seed hash this is a live recompute: no static set can cover the
// family of truncated defaults earlier selfdoc versions wrote.
func IsMachineOwnedCLIDescription(
	value any,
	kind strictclisupport.PageKind,
	name, appName, helpText, seedHash string,
) (bool, error) {
	text := NormalizeDescription(value)
	if matchesSeedHash(text, seedHash) {
		return true, nil
	}
	return strictclisupport.IsDefaultCLIDescription(text, kind, name, appName, helpText)
}

// lookupCLI returns the kind and help text of the CLI page named name, plus
// the app's own name.
//
// The kind is "" when the schema declares no command or group by that name --
// which is the answer for a CLI page whose command has since been removed,
// and for a project with no dumped schema at all.
func lookupCLI(structure *strictclisupport.Structure, name string) (
	kind strictclisupport.PageKind, helpText, appName string,
) {
	if structure == nil {
		return "", "", ""
	}
	for _, command := range structure.Commands {
		if strictclisupport.CommandName(command) == name {
			return strictclisupport.KindCommand,
				strictclisupport.CommandHelp(command), structure.AppName
		}
	}
	for _, group := range structure.Groups {
		if strictclisupport.CommandName(group) == name {
			return strictclisupport.KindGroup,
				strictclisupport.CommandHelp(group), structure.AppName
		}
	}
	return "", "", ""
}

// IsMachineOwned classifies a page's description as machine-owned (true) or
// handwritten (false).
//
// relPath selects the page kind by its filename; frontmatter supplies the
// description, and the title for a module page. seedHash is the page's
// recorded seed hash from the staleness store, "" when none was recorded.
// cliStructure is the parsed dumped schema, needed to look up a CLI command's
// help text; pass nil for a project that is not strictcli-based.
//
// Handwritten text is never classified machine-owned. Only a page declaring
// `generated: true` can be machine-owned at all, so a hand-authored page
// always answers false and keeps its full staleness protection.
func IsMachineOwned(
	relPath string,
	frontmatter util.Frontmatter,
	seedHash string,
	cliStructure *strictclisupport.Structure,
) (bool, error) {
	description, hasDescription := frontmatter["description"]
	generated := isTrue(frontmatter["generated"])

	if !hasDescription {
		// There is no description text to classify. These edge pages --
		// generated and seeded, carrying no description at all -- fall back
		// to the frontmatter's own skeleton signal.
		return generated && isTrue(frontmatter["seeded"]), nil
	}

	if !generated {
		return false, nil
	}

	text := NormalizeDescription(description)
	base := path.Base(relPath)

	if base == "cli-index.md" {
		appName := ""
		if cliStructure != nil {
			appName = cliStructure.AppName
		}
		return IsMachineOwnedCLIDescription(
			text, strictclisupport.KindIndex, "", appName, "", seedHash)
	}

	if strings.HasPrefix(base, "cli-") && strings.HasSuffix(base, ".md") {
		name := strings.TrimSuffix(strings.TrimPrefix(base, "cli-"), ".md")
		kind, helpText, appName := lookupCLI(cliStructure, name)
		if kind != "" {
			return IsMachineOwnedCLIDescription(
				text, kind, name, appName, helpText, seedHash)
		}
		// An unknown CLI page, with no schema entry to recompute against:
		// trust only the recorded seed hash.
		return matchesSeedHash(text, seedHash), nil
	}

	if base == "gen-index.md" {
		return IsMachineOwnedIndexDescription(text, seedHash), nil
	}

	// A module page: the title carries the module's name. A page with no
	// title names no module, so only the recorded seed hash can answer.
	moduleName := ""
	if title := frontmatter["title"]; isTruthy(title) {
		moduleName = pyStr(title)
	}
	return IsMachineOwnedModuleDescription(text, moduleName, seedHash), nil
}

// instantiate fills a description template's "{module}" placeholder.
func instantiate(template, moduleName string) string {
	return strings.ReplaceAll(template, "{module}", moduleName)
}

// isTruthy reports whether a frontmatter value is truthy as Python reads it:
// an absent key, a null, an empty string, a false and a zero are not.
func isTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case bool:
		return typed
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case []string:
		return len(typed) > 0
	default:
		return true
	}
}

// isTrue reports whether a frontmatter value is the boolean true, and not
// merely truthy: the string "true" is a document that wrote a quoted boolean,
// which is not the declaration this predicate reads.
func isTrue(value any) bool {
	declared, ok := value.(bool)
	return ok && declared
}

// pyStr renders a frontmatter value as Python's str() would, so a title or a
// description written as a bare number or boolean is classified on the text
// it renders as.
//
// An absent key is the empty string rather than [util.PythonStr]'s "None":
// the Python read every one of these keys through `.get(key, "")`, so a key
// that is not there carries no text to classify.
func pyStr(value any) string {
	if value == nil {
		return ""
	}
	return util.PythonStr(value)
}
