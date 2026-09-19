package strictclisupport

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/util"
)

// PageKind is which CLI page a description belongs to. Every default
// description and every recognition of one is decided per kind.
type PageKind string

const (
	// KindIndex is the CLI reference index page.
	KindIndex PageKind = "index"
	// KindCommand is a top-level command's page.
	KindCommand PageKind = "command"
	// KindGroup is a command group's page.
	KindGroup PageKind = "group"
)

// minHelpForFirstSentence is how long a help text must be before the default
// description becomes its first sentence rather than the long-form template.
// A short help makes a short description, and a page described by three words
// is worse for a reader (and for a search result) than one described by a
// sentence naming the app and the command.
const minHelpForFirstSentence = 50

// The default per-command and per-group description templates, matched so a
// page still carrying its auto-generated description is recomputed while a
// hand-edited one is preserved.
//
// The templates _renderCommandPage and _renderGroupPage produce when the
// command's or group's help text is shorter than minHelpForFirstSentence:
//
//	"Reference for the {app} {name} command — usage, flags, arguments,
//	 and examples for the {name} subcommand of the {app} CLI."
//	"Reference for the {app} {name} command group — subcommands, flags,
//	 arguments, and usage details for the {name} group in the {app} CLI."
//
// And the index template:
//
//	"Complete CLI reference for {app} — all available commands,
//	 subcommands, flags, arguments, and usage examples with detailed
//	 descriptions."
var (
	defaultCommandDescRE = regexp.MustCompile(
		`^Reference for the \S+ \S+ command — ` +
			`usage, flags, arguments, and examples for the \S+ subcommand ` +
			`of the \S+ CLI\.?$`,
	)
	defaultGroupDescRE = regexp.MustCompile(
		`^Reference for the \S+ \S+ command group — ` +
			`subcommands, flags, arguments, and usage details ` +
			`for the \S+ group in the \S+ CLI\.?$`,
	)
	defaultIndexDescRE = regexp.MustCompile(
		`^Complete CLI reference for \S+ — ` +
			`all available commands, subcommands, flags, arguments, ` +
			`and usage examples with detailed descriptions\.?$`,
	)
)

// normalizeCLIDescription strips surrounding whitespace and one layer of
// matching quotes.
func normalizeCLIDescription(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first := value[0]
		if first == value[len(value)-1] && (first == '\'' || first == '"') {
			value = strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}

// ComputeDefaultCLIDescription returns the machine-default description for a
// CLI page.
//
// When the page's help text is at least minHelpForFirstSentence characters
// long the default is the first sentence of that help; otherwise it is a
// long-form template naming the app and the command. The index default is
// always the fixed long-form template.
//
// An unrecognized kind is an error: the caller knows which page it is writing,
// and inventing a description for a page whose kind is unknown would publish
// one.
func ComputeDefaultCLIDescription(kind PageKind, name, appName, helpText string) (string, error) {
	if kind == KindIndex {
		return fmt.Sprintf(
			"Complete CLI reference for %s — all available commands, "+
				"subcommands, flags, arguments, and usage examples with "+
				"detailed descriptions.", appName,
		), nil
	}
	if len(helpText) >= minHelpForFirstSentence {
		return prose.FirstSentence(helpText), nil
	}
	switch kind {
	case KindCommand:
		return fmt.Sprintf(
			"Reference for the %s %s command — usage, flags, arguments, and "+
				"examples for the %s subcommand of the %s CLI.",
			appName, name, name, appName,
		), nil
	case KindGroup:
		return fmt.Sprintf(
			"Reference for the %s %s command group — subcommands, flags, "+
				"arguments, and usage details for the %s group in the %s CLI.",
			appName, name, name, appName,
		), nil
	}
	return "", fmt.Errorf("unknown CLI page kind: %s", util.PythonRepr(string(kind)))
}

// IsDefaultCLIDescription reports whether value is a machine-generated default
// CLI description.
//
// Every historical machine form is recognized, so machine residue is reseeded
// rather than frozen as if a person had written it:
//
//   - the current first-sentence form (help at least
//     minHelpForFirstSentence characters), or
//   - the long-form default template (a shorter help, or the index), or
//   - the historical help[:155] truncation, with or without a trailing
//     ellipsis, or
//   - any prefix of the raw help at least 100 characters long (a truncated
//     default from any prior cut point).
//
// An empty value counts as a machine default: it is a blank machine
// placeholder the caller should reseed. CLI machine text is derivable from the
// schema, so this is a live recompute -- no static set can cover the truncated
// prefix family.
//
// name is accepted for symmetry with ComputeDefaultCLIDescription and is not
// read: which template a value is compared against is decided by the kind and
// the help text alone.
func IsDefaultCLIDescription(value string, kind PageKind, name, appName, helpText string) (bool, error) {
	value = normalizeCLIDescription(value)
	if value == "" {
		return true, nil
	}

	// The default is determined the way the renderers determine it, so
	// recognition matches what ComputeDefaultCLIDescription emits.
	switch {
	case kind == KindIndex:
		if defaultIndexDescRE.MatchString(value) {
			return true, nil
		}
	case len(helpText) >= minHelpForFirstSentence:
		if value == prose.FirstSentence(helpText) {
			return true, nil
		}
	case kind == KindCommand:
		if defaultCommandDescRE.MatchString(value) {
			return true, nil
		}
	case kind == KindGroup:
		if defaultGroupDescRE.MatchString(value) {
			return true, nil
		}
	default:
		return false, fmt.Errorf("unknown CLI page kind: %s", util.PythonRepr(string(kind)))
	}

	if helpText != "" && (kind == KindCommand || kind == KindGroup) {
		core := strings.TrimSuffix(value, "...")
		firstLine := helpText
		if index := strings.Index(firstLine, "\n"); index >= 0 {
			firstLine = firstLine[:index]
		}
		if core == truncate(firstLine, 155) {
			return true, nil
		}
		if len([]rune(core)) >= 100 && strings.HasPrefix(helpText, core) {
			return true, nil
		}
	}

	return false, nil
}

// truncate is s cut to at most limit characters, counted in code points as
// Python's slicing counts them.
func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// readExistingCLIDescription returns the hand-edited description of the CLI
// page at path, or false when the page's description is the machine's to
// rewrite.
//
// The second result is false -- meaning "reseed with the machine default" --
// when the file does not exist, carries no description key, carries an empty
// one, or still holds a machine-generated default. Any other value is a real
// hand edit and is returned verbatim.
func readExistingCLIDescription(path string, kind PageKind, name, appName, helpText string) (string, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", false, nil
	}
	block, err := util.ReadFrontmatter(string(content), path, util.KindPage)
	if err != nil {
		return "", false, err
	}
	raw, isString := block.Values["description"].(string)
	if !isString {
		return "", false, nil
	}
	value := normalizeCLIDescription(raw)
	if value == "" {
		return "", false, nil
	}
	isDefault, err := IsDefaultCLIDescription(value, kind, name, appName, helpText)
	if err != nil {
		return "", false, err
	}
	if isDefault {
		return "", false, nil
	}
	return value, true, nil
}
