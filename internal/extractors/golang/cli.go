package golang

import (
	"os"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

var (
	// usageConstantRE matches a backtick-quoted string assigned to a name with
	// "usage" or "help" in it, with or without a const or var keyword.
	usageConstantRE = regexp.MustCompile(
		"(?s)(?:const|var)?\\s*(" + extractors.PyWordClass + "*(?:[Uu]sage|[Hh]elp)" + extractors.PyWordClass + "*)" +
			"\\s*(?:=|string\\s*=)\\s*" +
			"`((?:[^`]|\\\\`)*)`",
	)

	// usageFuncRE matches a function with "usage" or "help" in its name whose
	// body immediately returns a backtick-quoted string.
	usageFuncRE = regexp.MustCompile(
		"(?s)func\\s+(" + extractors.PyWordClass + "*(?:[Uu]sage|[Hh]elp)" + extractors.PyWordClass + "*)\\s*\\([^)]*\\)\\s*string\\s*\\{" +
			"\\s*return\\s*`((?:[^`]|\\\\`)*)`",
	)

	// stdlibFlagVarRE matches flag.StringVar(&v, "name", "default", "description").
	stdlibFlagVarRE = regexp.MustCompile(
		`flag\.(` + identifier + `)Var\s*\(\s*` +
			`&` + identifier + `\s*,\s*` +
			`"([^"]*?)"\s*,\s*` + // flag name
			`([^,]+?)\s*,\s*` + // default value
			`"([^"]*?)"\s*\)`, // description
	)

	// stdlibFlagRE matches flag.String("name", "default", "description").
	stdlibFlagRE = regexp.MustCompile(
		`flag\.(` + identifier + `)\s*\(\s*` +
			`"([^"]*?)"\s*,\s*` + // flag name
			`([^,]+?)\s*,\s*` + // default value
			`"([^"]*?)"\s*\)`, // description
	)

	// strictcliFlagRE matches <var>.BoolFlag("name", "description") and its
	// siblings.
	strictcliFlagRE = regexp.MustCompile(
		identifier + `\.(Bool|String|Int|Float|Duration)Flag\s*\(\s*` +
			`"([^"]*?)"\s*,\s*` +
			`"([^"]*?)"\s*\)`,
	)

	// strictcliCommandRE matches <var>.Command("name", "description", ...).
	strictcliCommandRE = regexp.MustCompile(
		identifier + `\.Command\s*\(\s*` +
			`"([^"]*?)"\s*,\s*` +
			`"([^"]*?)"\s*[,)]`,
	)
)

// nonFlagCalls are the flag package's calls that are not flag definitions, so
// a three-argument match on one of them is not a flag.
var nonFlagCalls = map[string]bool{
	"Parse":         true,
	"Visit":         true,
	"VisitAll":      true,
	"Set":           true,
	"Lookup":        true,
	"PrintDefaults": true,
}

// cliFlag is one command-line flag the scanner found.
type cliFlag struct {
	Name        string
	Type        string
	Default     string
	Description string
}

// cliCommand is one subcommand the scanner found.
type cliCommand struct {
	Name        string
	Description string
}

// funcNamePattern matches the declaration line of a function with a given
// name, with or without a receiver.
func funcNamePattern(funcName string) *regexp.Regexp {
	return regexp.MustCompile(`^func\s+(?:\(.*?\)\s+)?` + regexp.QuoteMeta(funcName) + `\s*\(`)
}

// handleCLI resolves a code-help directive: a program's usage text, its flags
// and its subcommands, read out of Go source.
//
// path may name a package directory -- in which case every non-test file in it
// is scanned as one body of source -- or a single file.
func (e *Extractor) handleCLI(
	path string,
	_ *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::cli requires a file path argument"), nil
	}

	var source string

	if packageDir := resolvePackageDir(path, sourcePaths, baseDir); packageDir != "" {
		names, err := nonTestGoFiles(packageDir)
		if err != nil {
			return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
		}
		var sources []string
		for _, name := range names {
			data, readErr := os.ReadFile(util.PathJoin(packageDir, name))
			if readErr != nil {
				continue
			}
			sources = append(sources, string(data))
		}
		source = strings.Join(sources, "\n")
	} else {
		fullPath := resolveFilePath(path, sourcePaths, baseDir)
		if fullPath == "" {
			return extractors.FormatError("file '" + path + "' not found"), nil
		}
		read, err := extractors.ReadSource(fullPath)
		if err != nil {
			return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
		}
		source = read
	}

	var parts []string

	for _, constant := range extractUsageConstants(source) {
		parts = append(parts,
			"**"+extractors.SymbolSpan(constant.Name)+":**",
			"",
			"```\n"+pyStrip(constant.Value)+"\n```",
		)
	}

	flags := extractFlagCalls(source)
	flags = append(flags, extractStrictcliFlags(source)...)

	if len(flags) > 0 {
		parts = append(parts, "", "**Flags:**", "")
		rows := make([][]string, 0, len(flags))
		for _, flag := range flags {
			rows = append(rows, []string{
				extractors.SymbolSpan(flag.Name),
				optionalSpan(flag.Type),
				optionalSpan(flag.Default),
				flag.Description,
			})
		}
		table, err := extractors.RenderTable(
			[]string{"Flag", "Type", "Default", "Description"}, rows)
		if err != nil {
			return "", err
		}
		parts = append(parts, table)
	}

	commands := extractStrictcliCommands(source)
	if len(commands) > 0 {
		parts = append(parts, "", "**Commands:**", "")
		rows := make([][]string, 0, len(commands))
		for _, command := range commands {
			rows = append(rows, []string{extractors.SymbolSpan(command.Name), command.Description})
		}
		table, err := extractors.RenderTable([]string{"Command", "Description"}, rows)
		if err != nil {
			return "", err
		}
		parts = append(parts, table)
	}

	if len(parts) == 0 {
		return extractors.FormatError("no CLI documentation found in '" + path + "'"), nil
	}

	return strings.Join(parts, "\n"), nil
}

// optionalSpan renders a value as a code span, or as nothing when it is empty.
func optionalSpan(value string) string {
	if value == "" {
		return ""
	}
	return extractors.SymbolSpan(value)
}

// usageConstant is one usage or help text the scanner found, with the name it
// was declared under.
type usageConstant struct {
	Name  string
	Value string
}

// extractUsageConstants finds the usage and help text a program declares:
// backtick-quoted strings assigned to a usage- or help-named constant or
// variable, and functions of such a name that return one.
func extractUsageConstants(source string) []usageConstant {
	var results []usageConstant

	for _, m := range usageConstantRE.FindAllStringSubmatch(source, -1) {
		results = append(results, usageConstant{Name: m[1], Value: m[2]})
	}

	for _, m := range usageFuncRE.FindAllStringSubmatch(source, -1) {
		results = append(results, usageConstant{Name: m[1] + "()", Value: m[2]})
	}

	return results
}

// extractFlagCalls finds the stdlib flag package's flag definitions.
func extractFlagCalls(source string) []cliFlag {
	var flags []cliFlag

	for _, m := range stdlibFlagVarRE.FindAllStringSubmatch(source, -1) {
		flags = append(flags, cliFlag{
			Name:        m[2],
			Type:        strings.ToLower(m[1]),
			Default:     strings.Trim(pyStrip(m[3]), `"`),
			Description: m[4],
		})
	}

	for _, m := range stdlibFlagRE.FindAllStringSubmatch(source, -1) {
		callName := m[1]
		// The XxxVar forms are already covered above, and the flag package's
		// non-definition calls are not flags at all.
		if strings.HasSuffix(callName, "Var") || nonFlagCalls[callName] {
			continue
		}
		flags = append(flags, cliFlag{
			Name:        m[2],
			Type:        strings.ToLower(callName),
			Default:     strings.Trim(pyStrip(m[3]), `"`),
			Description: m[4],
		})
	}

	return flags
}

// extractStrictcliFlags finds strictcli's flag declarations, which carry no
// default at the call site.
func extractStrictcliFlags(source string) []cliFlag {
	var flags []cliFlag
	for _, m := range strictcliFlagRE.FindAllStringSubmatch(source, -1) {
		flags = append(flags, cliFlag{
			Name:        m[2],
			Type:        strings.ToLower(m[1]),
			Description: m[3],
		})
	}
	return flags
}

// extractStrictcliCommands finds strictcli's command declarations.
func extractStrictcliCommands(source string) []cliCommand {
	var commands []cliCommand
	for _, m := range strictcliCommandRE.FindAllStringSubmatch(source, -1) {
		commands = append(commands, cliCommand{Name: m[1], Description: m[2]})
	}
	return commands
}
