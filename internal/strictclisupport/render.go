package strictclisupport

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/tables"
	"github.com/stricttools/selfdoc/internal/util"
)

// generatedPageMode is the permission every generated CLI page carries: read
// only, because the page is the machine's and an edit to it is lost on the
// next gen.
const generatedPageMode fs.FileMode = 0o444

// writablePageMode is what a generated page is briefly set to before being
// rewritten, so a filesystem that refuses to replace a read-only file still
// lets the rewrite through.
const writablePageMode fs.FileMode = 0o600

// reservedQuartetRows is the framework-owned reserved quartet. These four
// flags are declared by strictcli itself, never by the app, so they appear
// nowhere in schema.json at any level -- the section is static and keyed on
// the schema carrying `effect` fields, which a post-quartet app always does.
var reservedQuartetRows = [][]string{
	{
		"`--dry-run`",
		"Preview mode: no mutation runs. The framework prints a log of " +
			"every effect the command would have performed.",
	},
	{
		"`--approve-consequential`",
		"Skips the confirmation prompt a consequential command shows " +
			"before it runs.",
	},
	{
		"`--quiet`",
		"Hides informational output. Warnings, errors, structured data " +
			"and the dry-run log are never suppressed.",
	},
	{
		"`--verbose`",
		"Shows debug output. `--quiet` wins when both are passed.",
	},
}

// reservedQuartetIntro introduces the reserved-quartet table.
const reservedQuartetIntro = "These flags are owned by the strictcli framework, not by the app. " +
	"No command may declare a flag with one of these names, and each is " +
	"recognized anywhere on the command line."

// indent is two levels of indentation inside a table cell, for a scoped
// declaration.
const indent = "&nbsp;&nbsp;&nbsp;&nbsp;"

// scalarWords maps JSON Schema type names to the words a reader of a CLI page
// knows. The fragment subset is closed at four keywords (type, items,
// additionalProperties, enum), so this table plus the two container rules in
// typeWord covers every fragment strictcli can emit.
var scalarWords = map[string]string{
	"string":  "str",
	"boolean": "bool",
	"integer": "int",
	"number":  "float",
}

// constraintIntro is the opening clause of each membership constraint.
var constraintIntro = map[string]string{
	"at_least_one": "At least one of",
	"all_or_none":  "All or none of",
}

// whenWords render a constraint member's election condition.
var whenWords = map[string]string{
	"present":   "when supplied",
	"true":      "when true",
	"non_empty": "when non-empty",
}

// ExpectedCLIPageFilenames returns the filenames GenerateCLIPages would write
// for structure.
//
// The stale-file cleanup pass reads it to know which CLI page names are
// current, so the pages a prior run generated are not deleted as stale before
// the new ones have been written.
func ExpectedCLIPageFilenames(structure *Structure) []string {
	if structure == nil {
		return []string{}
	}
	names := []string{"cli-index.md"}
	for _, cmd := range structure.Commands {
		names = append(names, "cli-"+getString(cmd, "name")+".md")
	}
	for _, grp := range structure.Groups {
		names = append(names, "cli-"+getString(grp, "name")+".md")
	}
	return names
}

// GenerateCLIPages generates the Markdown documentation pages for structure
// into docsDir: an index page, one page per top-level command and one page per
// command group.
//
// Every page carries `generated = true` frontmatter and is written read-only
// through handle, atomically. A page whose `description` frontmatter has been
// hand-edited keeps that description; one still carrying a machine default has
// it recomputed and is marked `seeded = true`.
//
// It returns the generated filenames, relative to docsDir.
func GenerateCLIPages(structure *Structure, docsDir string, handle *effects.Handle) ([]string, error) {
	if err := handle.MkdirAll(docsDir); err != nil {
		return nil, err
	}
	var generated []string

	appName := structure.AppName
	appHelp := structure.AppHelp
	appVersion := structure.AppVersion

	defaultIndexDesc, err := ComputeDefaultCLIDescription(KindIndex, "", appName, "")
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(docsDir, "cli-index.md")
	existingIndexDesc, handwritten, err := readExistingCLIDescription(
		indexPath, KindIndex, "", appName, "",
	)
	if err != nil {
		return nil, err
	}
	indexDesc := defaultIndexDesc
	if handwritten {
		indexDesc = existingIndexDesc
	}
	fields := []util.FrontmatterField{
		{Key: "title", Value: appName + " CLI Reference"},
		{Key: "description", Value: indexDesc},
		{Key: "generated", Value: true},
	}
	if !handwritten {
		fields = append(fields, util.FrontmatterField{Key: "seeded", Value: true})
	}
	fields = append(fields,
		util.FrontmatterField{Key: "nav_group", Value: "CLI Reference"},
		util.FrontmatterField{Key: "nav_order", Value: int64(91)},
	)
	frontmatter, err := util.RenderFrontmatter(fields)
	if err != nil {
		return nil, err
	}
	lines := append(strings.Split(strings.TrimRight(frontmatter, "\n"), "\n"),
		"<!-- generated by selfdoc gen (strictcli), do not edit -->",
		"",
		"# "+appName+" CLI Reference",
		"",
	)
	if appHelp != "" {
		lines = append(lines, appHelp, "")
	}
	if appVersion != "" {
		// A var DIRECTIVE, not the literal version: the raw page body is
		// what staleness hashes, so baking the version in here moved the
		// content hash on every release and tripped STALE001 with zero
		// signal. The directive resolves at build time, so the site still
		// shows the current version.
		lines = append(lines, `Version: :-: var key="project.version"`, "")
	}

	if len(structure.Commands) > 0 {
		lines = append(lines, "## Commands", "")
		for _, cmd := range structure.Commands {
			name := getString(cmd, "name")
			href := address.RootPageLink("cli-" + name + ".md")
			lines = append(lines, fmt.Sprintf(
				"- [%s](%s) -- %s", name, href, getString(cmd, "help"),
			))
		}
		lines = append(lines, "")
	}

	if len(structure.Groups) > 0 {
		lines = append(lines, "## Command Groups", "")
		for _, grp := range structure.Groups {
			name := getString(grp, "name")
			href := address.RootPageLink("cli-" + name + ".md")
			lines = append(lines, fmt.Sprintf(
				"- [%s](%s) -- %s", name, href, getString(grp, "help"),
			))
		}
		lines = append(lines, "")
	}

	if len(structure.GlobalFlags) > 0 {
		table, err := flagTable(structure.GlobalFlags)
		if err != nil {
			return nil, err
		}
		lines = append(lines, "## Global flags", "", table, "")
	}

	if schemaHasEffects(structure) {
		quartet, err := tables.RenderMarkdownTable(
			[]string{"Flag", "Effect"}, reservedQuartetRows, nil, false,
		)
		if err != nil {
			return nil, err
		}
		lines = append(lines,
			"## Framework flags", "", reservedQuartetIntro, "", quartet, "",
		)
	}

	configSection, err := configLines(structure)
	if err != nil {
		return nil, err
	}
	lines = append(lines, configSection...)

	if structure.Infra != nil && structure.Infra.Len() > 0 {
		lines = append(lines, "## Infrastructure", "")
		sections := []struct {
			key     string
			heading string
			header  string
		}{
			{"roots", "### Roots", "Default"},
			{"handshakes", "### Handshake variables", "Description"},
			{"connections", "### Connection variables", "Description"},
		}
		for _, section := range sections {
			entries := getList(structure.Infra, section.key)
			if len(entries) == 0 {
				continue
			}
			table, err := envTable(entries, section.header)
			if err != nil {
				return nil, err
			}
			lines = append(lines, section.heading, "", table, "")
		}
	}

	if structure.Deprecated != nil && structure.Deprecated.Len() > 0 {
		lines = append(lines, "## Deprecated", "")
		for _, name := range sortedKeys(structure.Deprecated) {
			value, _ := structure.Deprecated.Get(name)
			lines = append(lines, fmt.Sprintf("- `%s` -- %s", name, pyStr(value)))
		}
		lines = append(lines, "")
	}

	if err := writePage(handle, indexPath, strings.Join(lines, "\n")); err != nil {
		return nil, err
	}
	generated = append(generated, "cli-index.md")

	// Alphabetical ordering across every command and group page.
	allNames := make([]string, 0, len(structure.Commands)+len(structure.Groups))
	for _, cmd := range structure.Commands {
		allNames = append(allNames, getString(cmd, "name"))
	}
	for _, grp := range structure.Groups {
		allNames = append(allNames, getString(grp, "name"))
	}
	sort.Strings(allNames)
	navOrder := make(map[string]int, len(allNames))
	for index, name := range allNames {
		navOrder[name] = index + 1
	}

	for _, cmd := range structure.Commands {
		name := getString(cmd, "name")
		filename := "cli-" + name + ".md"
		outPath := filepath.Join(docsDir, filename)
		content, err := renderCommandPage(cmd, appName, navOrder[name], outPath)
		if err != nil {
			return nil, err
		}
		if err := writePage(handle, outPath, content); err != nil {
			return nil, err
		}
		generated = append(generated, filename)
	}

	for _, grp := range structure.Groups {
		name := getString(grp, "name")
		filename := "cli-" + name + ".md"
		outPath := filepath.Join(docsDir, filename)
		content, err := renderGroupPage(grp, appName, navOrder[name], outPath)
		if err != nil {
			return nil, err
		}
		if err := writePage(handle, outPath, content); err != nil {
			return nil, err
		}
		generated = append(generated, filename)
	}

	return generated, nil
}

// writePage writes a generated page atomically with read-only permissions.
//
// An existing page is made writable first. The atomic rename would replace a
// read-only file regardless, so this is defense on the filesystems where it
// would not, and it is what the rest of the generator does.
func writePage(handle *effects.Handle, path, content string) error {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		if err := handle.Chmod(path, writablePageMode); err != nil {
			return err
		}
	}
	return handle.AtomicWrite(path, []byte(content), generatedPageMode)
}

// schemaHasEffects reports whether any command in structure declares an
// effect.
//
// Classification is mandatory under the effects regime, so a single `effect`
// field anywhere proves the app is built on a strictcli that owns the reserved
// quartet.
func schemaHasEffects(structure *Structure) bool {
	for _, cmd := range structure.Commands {
		if getTruthy(cmd, "effect") {
			return true
		}
	}
	for _, grp := range structure.Groups {
		for _, entry := range getList(grp, "commands") {
			if getTruthy(asObject(entry), "effect") {
				return true
			}
		}
	}
	return false
}

// fmtDefault formats a declared default value for table display.
//
// Every default is rendered, "", 0, false, [] and {} included: under the
// presence declaration a default is emitted when presence is "default", so an
// empty-looking value is a declaration and not an absence. Structured defaults
// -- strictcli's relative_to_root marker and a selector's flat default map --
// render as compact JSON rather than as Python syntax.
func fmtDefault(value any) string {
	switch t := value.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "`true`"
		}
		return "`false`"
	case []any:
		return "`" + pyJSONDumps(t) + "`"
	case *Object:
		return "`" + pyJSONDumps(t) + "`"
	default:
		return "`" + pyStr(value) + "`"
	}
}

// typeWord renders a v2 value_schema fragment as a readable type word.
//
// It returns "" for a missing fragment, which is a selector: its value is a
// variant the closed subset cannot express, and its absence is the declaration
// -- the caller renders it as the construct it is.
func typeWord(valueSchema any) string {
	if !truthy(valueSchema) {
		return ""
	}
	schema := asObject(valueSchema)
	kind := get(schema, "type")
	if kind == "array" {
		return "list[" + itemWord(get(schema, "items")) + "]"
	}
	if kind == "object" {
		return "dict[str, " + itemWord(get(schema, "additionalProperties")) + "]"
	}
	name, isString := kind.(string)
	if !isString {
		return pyStr(kind)
	}
	if word, known := scalarWords[name]; known {
		return word
	}
	return name
}

// itemWord is the type word of a container's element fragment: the reader's
// word for a declared type, the declared type itself when it is not one of the
// four, and "?" when the fragment declares none.
func itemWord(fragment any) string {
	kind := get(asObject(fragment), "type")
	name, isString := kind.(string)
	if !isString {
		if kind == nil {
			return "?"
		}
		return pyStr(kind)
	}
	if word, known := scalarWords[name]; known {
		return word
	}
	return name
}

// presenceCell renders an entry's presence as the one fact it declares.
//
// One of required, optional or "default: <value>", which is what strictcli's
// own --help renders per line. A Default column cannot carry three states, and
// an empty one said nothing about which of the two valueless states a flag was
// in.
func presenceCell(entry *Object) string {
	presence := getString(entry, "presence")
	if presence == "default" {
		return "default: " + fmtDefault(get(entry, "default"))
	}
	if presence == "required" || presence == "optional" {
		return presence
	}
	return ""
}

// choicesNote renders a value flag's choices records as one prose sentence.
//
// v2 splits a choices declaration in two: the values as an enum inside
// value_schema, and the value-plus-help records beside it under choices. The
// records are the half a reader wants, and help is omitted from an entry that
// declares none.
func choicesNote(entry *Object) string {
	records := getList(entry, "choices")
	if len(records) == 0 || getTruthy(entry, "elect_by") {
		return ""
	}
	parts := make([]string, 0, len(records))
	for _, raw := range records {
		record := asObject(raw)
		value := "`" + pyStr(get(record, "value")) + "`"
		if help := getString(record, "help"); help != "" {
			parts = append(parts, value+" ("+help+")")
			continue
		}
		parts = append(parts, value)
	}
	return "Values: " + strings.Join(parts, ", ") + "."
}

// spelledName renders every argv spelling one flag declaration mints.
//
// A negatable bool publishes --no-x and a nullable property publishes
// --unset-x; neither gets an entry of its own in the dump, exactly as
// negatable has never published one, so the reader has to be told they exist.
func spelledName(flag *Object) string {
	name := getString(flag, "name")
	spellings := []string{"`--" + name + "`"}
	isBool := get(getObject(flag, "value_schema"), "type") == "boolean"
	if isBool && !isFalse(flag, "negatable") {
		spellings = append(spellings, "`--no-"+name+"`")
	}
	if getTruthy(flag, "nullable") {
		spellings = append(spellings, "`--unset-"+name+"`")
	}
	return strings.Join(spellings, ", ")
}

// describe composes a flag's description cell: scope note, help, choices and
// the notes its own declarations mint.
func describe(flag *Object, prefix string) string {
	var parts []string
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if help := getString(flag, "help"); help != "" {
		parts = append(parts, help)
	}
	if note := choicesNote(flag); note != "" {
		parts = append(parts, note)
	}
	if getTruthy(flag, "nullable") {
		parts = append(parts, "`--unset-"+getString(flag, "name")+"` clears it.")
	}
	if isFalse(flag, "prefixed") && getTruthy(flag, "env") {
		parts = append(parts, "Its env var is not prefixed with the app's env prefix.")
	}
	return strings.Join(parts, " ")
}

// cell renders a schema value as a table cell: the value as Python would
// stringify it, and "" for every falsy value -- the `x or ""` the Python
// renderer wrote at each of these sites.
func cell(value any) string {
	if !truthy(value) {
		return ""
	}
	return pyStr(value)
}

// fmtShort formats a short flag for table display.
func fmtShort(short any) string {
	if !truthy(short) {
		return ""
	}
	return "`-" + pyStr(short) + "`"
}

// flagRows builds flag-table rows for flags, recursing through selector
// scopes.
//
// A selector is an entry carrying elect_by; the presence of that key is the
// discriminator, and such an entry never carries a value_schema. It is
// rendered as the construct it is rather than as an ordinary row:
//
//   - under member-flags the selector's own name is never typed -- it is only
//     the handler key and the noun errors use -- so its row shows the bare
//     name with no "--" and each choice becomes a row of its own under its own
//     token (`--pattern`), carrying the choice's payload type when it has one;
//   - under selector-token the selector's name IS typed (`--via email`), so
//     its row keeps the "--" and the choices are listed as the values it
//     accepts.
//
// Either way a choice's scope -- the flags that exist only while it is elected
// -- follows indented, to unlimited depth. Each scoped row also names its
// scope in prose, so a row read on its own (or after a reader sorts the table)
// still states the condition it lives under.
func flagRows(flags []any, depth int, scope string) [][]string {
	var rows [][]string
	pad := strings.Repeat(indent, depth)
	for _, raw := range flags {
		flag := asObject(raw)
		if getTruthy(flag, "elect_by") {
			rows = append(rows, selectorRows(flag, depth, scope)...)
			continue
		}
		word := typeWord(get(flag, "value_schema"))
		if getTruthy(flag, "unique") {
			word += " (unique)"
		}
		prefix := ""
		if scope != "" {
			prefix = "Only with " + scope + "."
		}
		rows = append(rows, []string{
			pad + spelledName(flag),
			fmtShort(get(flag, "short")),
			word,
			presenceCell(flag),
			cell(get(flag, "env")),
			describe(flag, prefix),
		})
	}
	return rows
}

// selectorRows are the rows for one selector flag and, indented under it,
// every choice scope.
func selectorRows(selector *Object, depth int, scope string) [][]string {
	var rows [][]string
	pad := strings.Repeat(indent, depth)
	memberSpelled := get(selector, "elect_by") == "member-flags"
	choices := getList(selector, "choices")
	spellings := make([]string, 0, len(choices))
	for _, raw := range choices {
		name := pyStr(get(asObject(raw), "name"))
		if memberSpelled {
			spellings = append(spellings, "`--"+name+"`")
			continue
		}
		spellings = append(spellings, "`"+name+"`")
	}
	names := strings.Join(spellings, ", ")

	selectorName := getString(selector, "name")
	var headName, headDesc string
	if memberSpelled {
		// The name is the handler's key and the errors' noun, never a token.
		headName = pad + "`" + selectorName + "`"
		headDesc = "Selection (not typed as a flag). Elect exactly one of " + names + "."
	} else {
		headName = pad + "`--" + selectorName + " <choice>`"
		headDesc = "Selection. Takes exactly one of " + names + "."
	}
	prefix := ""
	if scope != "" {
		prefix = "Only with " + scope + ". "
	}
	description := prefix + headDesc
	if help := getString(selector, "help"); help != "" {
		description += " " + help
	}
	rows = append(rows, []string{
		headName,
		fmtShort(get(selector, "short")),
		"choice",
		presenceCell(selector),
		cell(get(selector, "env")),
		description,
	})

	for _, raw := range choices {
		choice := asObject(raw)
		choiceName := pyStr(get(choice, "name"))
		scoped := append([]any(nil), getList(choice, "flags")...)
		var payload *Object
		// A member-spelled choice's payload is the first scoped entry,
		// under the reserved name `value`: it is supplied by electing the
		// member, so it belongs on the member's own row rather than under
		// it.
		if memberSpelled && len(scoped) > 0 && getString(asObject(scoped[0]), "name") == "value" {
			payload = asObject(scoped[0])
			scoped = scoped[1:]
		}

		var childScope string
		if memberSpelled {
			token := "`--" + choiceName + "`"
			descParts := []string{fmt.Sprintf(
				"Elects `%s` = `%s`.", selectorName, choiceName,
			)}
			if help := getString(choice, "help"); help != "" {
				descParts = append(descParts, help)
			}
			if payload != nil {
				if help := getString(payload, "help"); help != "" {
					descParts = append(descParts, "Its value: "+help)
				}
			}
			// A member flag declares required, read as *required once this
			// member is elected*; the payload carries the same.
			presence := presenceCell(payload)
			if payload == nil {
				presence = presenceCell(choice)
			}
			if presence == "" {
				presence = "required"
			}
			rows = append(rows, []string{
				strings.Repeat(indent, depth+1) + token,
				"",
				typeWord(get(payload, "value_schema")),
				presence,
				"",
				strings.Join(descParts, " "),
			})
			childScope = token
		} else {
			description := "Value of `--" + selectorName + "`."
			if help := getString(choice, "help"); help != "" {
				description += " " + help
			}
			rows = append(rows, []string{
				strings.Repeat(indent, depth+1) + "`" + choiceName + "`",
				"", "", "", "",
				description,
			})
			childScope = "`--" + selectorName + " " + choiceName + "`"
		}

		rows = append(rows, flagRows(scoped, depth+2, childScope)...)
	}

	return rows
}

// flagTable renders the standard flag table for a list of schema flags.
func flagTable(flags []any) (string, error) {
	return tables.RenderMarkdownTable(
		[]string{"Name", "Short", "Type", "Presence", "Env", "Description"},
		flagRows(flags, 0, ""), nil, false,
	)
}

// argTable renders the standard argument table for a list of schema args.
//
// Presence comes from the declaration, not from an assumption: the v1
// `required` key is deleted, and reading it with a true fallback -- which is
// what this table did -- labelled every positional argument required whether
// it was or not.
func argTable(args []any) (string, error) {
	rows := make([][]string, 0, len(args))
	for _, raw := range args {
		arg := asObject(raw)
		word := typeWord(get(arg, "value_schema"))
		if getTruthy(arg, "variadic") {
			word += " (variadic)"
		}
		description := getString(arg, "help")
		if note := choicesNote(arg); note != "" {
			description = strings.TrimSpace(description + " " + note)
		}
		rows = append(rows, []string{
			"`" + getString(arg, "name") + "`",
			word,
			presenceCell(arg),
			description,
		})
	}
	return tables.RenderMarkdownTable(
		[]string{"Name", "Type", "Presence", "Description"}, rows, nil, false,
	)
}

// IterFlagTokens returns every --token an invocation can actually type, in
// declaration order, recursing through selector scopes.
//
// A member-spelled selector's own name is not among them -- it is never typed
// -- while each of its choices is, and a scoped flag is a token like any
// other. This is what a completeness check must compare a page against;
// comparing against flag NAMES would demand that a page document a token no
// user can write.
func IterFlagTokens(flags []any) []string {
	var tokens []string
	for _, raw := range flags {
		flag := asObject(raw)
		electBy := get(flag, "elect_by")
		if truthy(electBy) {
			if electBy == "selector-token" {
				tokens = append(tokens, "--"+getString(flag, "name"))
			}
			for _, rawChoice := range getList(flag, "choices") {
				choice := asObject(rawChoice)
				scoped := append([]any(nil), getList(choice, "flags")...)
				if electBy == "member-flags" {
					tokens = append(tokens, "--"+pyStr(get(choice, "name")))
					if len(scoped) > 0 && getString(asObject(scoped[0]), "name") == "value" {
						scoped = scoped[1:]
					}
				}
				tokens = append(tokens, IterFlagTokens(scoped)...)
			}
			continue
		}
		tokens = append(tokens, "--"+getString(flag, "name"))
	}
	return tokens
}

// FlagHelp is one declaration a reader sees, as IterFlagHelp reports it.
type FlagHelp struct {
	// Label is how the declaration is written on the page: its token, or a
	// selector's bare name -- there is no "--" to print for a name that is
	// never typed.
	Label string
	// Help is the declaration's help text, "" when it declares none.
	Help string
}

// IterFlagHelp returns the label and help of every declaration a reader sees,
// in declaration order.
//
// It recurses through selector scopes, so a scoped flag's help is measured
// like any other. A selector's own label is its bare name, and each choice is
// labelled by the token that elects it.
func IterFlagHelp(flags []any) []FlagHelp {
	var out []FlagHelp
	for _, raw := range flags {
		flag := asObject(raw)
		electBy := get(flag, "elect_by")
		if truthy(electBy) {
			memberSpelled := electBy == "member-flags"
			out = append(out, FlagHelp{
				Label: getString(flag, "name"),
				Help:  getString(flag, "help"),
			})
			for _, rawChoice := range getList(flag, "choices") {
				choice := asObject(rawChoice)
				name := pyStr(get(choice, "name"))
				label := name
				if memberSpelled {
					label = "--" + name
				}
				out = append(out, FlagHelp{
					Label: label, Help: getString(choice, "help"),
				})
				scoped := append([]any(nil), getList(choice, "flags")...)
				if memberSpelled && len(scoped) > 0 &&
					getString(asObject(scoped[0]), "name") == "value" {
					payload := asObject(scoped[0])
					scoped = scoped[1:]
					out = append(out, FlagHelp{
						Label: "--" + name + " <value>",
						Help:  getString(payload, "help"),
					})
				}
				out = append(out, IterFlagHelp(scoped)...)
			}
			continue
		}
		out = append(out, FlagHelp{
			Label: "--" + getString(flag, "name"),
			Help:  getString(flag, "help"),
		})
	}
	return out
}

// memberText renders one constraint member: its resolved kind, name and
// election.
func memberText(member *Object) string {
	kind := getString(member, "kind")
	name := getString(member, "name")
	var rendered string
	switch kind {
	case "arg":
		rendered = "`" + name + "`"
	case "constraint":
		// A nested member has no election of its own -- the rule it names
		// carries one.
		return "the `" + name + "` rule"
	default:
		rendered = "`--" + name + "`"
	}
	if when, known := whenWords[getString(member, "when")]; known {
		return rendered + " (" + when + ")"
	}
	return rendered
}

// constraintSentence renders one constraint as the rule it states.
func constraintSentence(constraint *Object) string {
	kind := getString(constraint, "type")
	if intro, known := constraintIntro[kind]; known {
		members := getList(constraint, "members")
		parts := make([]string, 0, len(members))
		for _, raw := range members {
			parts = append(parts, memberText(asObject(raw)))
		}
		return intro + " " + strings.Join(parts, ", ") + "."
	}
	switch kind {
	case "requires":
		return fmt.Sprintf(
			"`--%s` requires `--%s`.",
			getString(constraint, "flag"), getString(constraint, "depends_on"),
		)
	case "implies":
		return fmt.Sprintf(
			"`--%s` implies `--%s` = %s.",
			getString(constraint, "flag"), getString(constraint, "implies"),
			fmtDefault(get(constraint, "value")),
		)
	}
	return pyStr(get(constraint, "type"))
}

// constraintsLines returns the constraints table lines for one command, or
// none.
func constraintsLines(cmd *Object, heading string) ([]string, error) {
	constraints := getList(cmd, "constraints")
	if len(constraints) == 0 {
		return nil, nil
	}
	rows := make([][]string, 0, len(constraints))
	for _, raw := range constraints {
		constraint := asObject(raw)
		rows = append(rows, []string{
			"`" + getString(constraint, "name") + "`",
			constraintSentence(constraint),
		})
	}
	table, err := tables.RenderMarkdownTable(
		[]string{"Rule", "What it requires"}, rows, nil, false,
	)
	if err != nil {
		return nil, err
	}
	return []string{
		heading,
		"",
		"The framework enforces these before the command runs.",
		"",
		table,
		"",
	}, nil
}

// flagSetsLines returns the flag-set grouping lines for one command, or none.
//
// A command's flag-set members are merged into its flag list, so without this
// the grouping a project declared is invisible on the page.
func flagSetsLines(cmd *Object) []string {
	sets := getList(cmd, "flag_sets")
	if len(sets) == 0 {
		return nil
	}
	lines := []string{"Flag sets:", ""}
	for _, raw := range sets {
		set := asObject(raw)
		members := getList(set, "flags")
		parts := make([]string, 0, len(members))
		for _, name := range members {
			parts = append(parts, "`--"+pyStr(name)+"`")
		}
		lines = append(lines, fmt.Sprintf(
			"- `%s` -- %s", getString(set, "name"), strings.Join(parts, ", "),
		))
	}
	return append(lines, "")
}

// updateLines returns the update-declaration lines for one command, or none.
//
// update_of and write_mode are emitted together and never alone. The two facts
// a reader most needs are what an unsupplied property means and that at least
// one property is required, so both are stated outright.
func updateLines(cmd *Object) []string {
	update := getObject(cmd, "update_of")
	if !truthy(get(cmd, "update_of")) {
		return nil
	}
	writeMode := getString(cmd, "write_mode")
	argNames := map[string]bool{}
	for _, raw := range getList(cmd, "args") {
		argNames[getString(asObject(raw), "name")] = true
	}
	reference := func(name string) string {
		if argNames[name] {
			return "`" + name + "`"
		}
		return "`--" + name + "`"
	}

	properties := getList(update, "properties")
	nullableNames := map[string]bool{}
	for _, raw := range getList(cmd, "flags") {
		flag := asObject(raw)
		if getTruthy(flag, "nullable") {
			nullableNames[getString(flag, "name")] = true
		}
	}
	// Declaration order is the properties list's, not the flag list's.
	var nullable []string
	for _, raw := range properties {
		name := pyStr(raw)
		if nullableNames[name] {
			nullable = append(nullable, name)
		}
	}

	lines := []string{
		fmt.Sprintf(
			"**Updates:** `%s` (write mode: %s)",
			getString(update, "resource"), writeMode,
		),
		"",
	}
	identity := getList(update, "identity")
	if len(identity) > 0 {
		parts := make([]string, 0, len(identity))
		for _, name := range identity {
			parts = append(parts, reference(pyStr(name)))
		}
		lines = append(lines, "- Identified by: "+strings.Join(parts, ", "))
	}
	parts := make([]string, 0, len(properties))
	for _, name := range properties {
		parts = append(parts, reference(pyStr(name)))
	}
	lines = append(lines, "- Writes: "+strings.Join(parts, ", ")+
		" -- at least one of them is required.")
	if writeMode == "full_replace" {
		lines = append(lines, "- A property that is not supplied is re-sent "+
			"as read, so every property is written.")
	} else {
		lines = append(lines, "- A property that is not supplied is left unchanged.")
	}
	if len(nullable) > 0 {
		cleared := make([]string, 0, len(nullable))
		for _, name := range nullable {
			cleared = append(cleared, "`--unset-"+name+"`")
		}
		lines = append(lines, "- Clearable: "+strings.Join(cleared, ", ")+".")
	}
	return append(lines, "")
}

// configLines returns the app-level configuration lines, or none.
//
// Each of these keys is emitted only when the app departs from the framework's
// own behavior, which is what makes it worth a reader's attention.
func configLines(structure *Structure) ([]string, error) {
	var rows [][]string
	if structure.Config {
		rows = append(rows, []string{
			"Config file",
			"This app reads a config file; `--config <path>` selects one.",
		})
	}
	if structure.ConfigFormat != "" {
		rows = append(rows, []string{"Config format", "`" + structure.ConfigFormat + "`"})
	}
	if truthy(structure.ConfigPath) {
		rows = append(rows, []string{"Config path", fmtDefault(structure.ConfigPath)})
	}
	if structure.ConfigConflictMode != "" {
		rows = append(rows, []string{
			"Config conflict mode",
			"`" + structure.ConfigConflictMode + "` (which source wins when " +
				"both a CLI token and the config file supply a value)",
		})
	}
	if structure.EnvPrefix != "" {
		rows = append(rows, []string{
			"Env prefix",
			fmt.Sprintf(
				"`%s` -- every env-bound flag reads `%s` + its own variable "+
					"name unless it declares otherwise.",
				structure.EnvPrefix, structure.EnvPrefix,
			),
		})
	}
	if len(rows) == 0 {
		return nil, nil
	}
	table, err := tables.RenderMarkdownTable(
		[]string{"Setting", "Value"}, rows, nil, false,
	)
	if err != nil {
		return nil, err
	}
	return []string{"## Configuration", "", table, ""}, nil
}

// commandMetaLines returns the effect badge and dry-run refusal lines for one
// command.
//
// They are empty for a pre-effects schema: `effect` is mandatory under the
// effects regime, so its absence means the app predates the regime and there
// is nothing honest to print.
func commandMetaLines(cmd *Object) []string {
	var lines []string
	if effect := getString(cmd, "effect"); effect != "" {
		badge := "**Effect:** " + effect
		if getTruthy(cmd, "consequential") {
			badge += " · **consequential** (prompts before running; " +
				"`--approve-consequential` skips)"
		}
		lines = append(lines, badge, "")
	}

	// Emitted by strictcli only on a command that refuses --dry-run;
	// absence is the normal case (dry run works) and prints nothing.
	if isFalse(cmd, "dry_run_supported") {
		text := "**Dry run:** not supported"
		if reason := getString(cmd, "dry_run_unsupported_reason"); reason != "" {
			text += " — " + reason
		}
		lines = append(lines, text, "")
	}

	return lines
}

// grantsLines returns the grants table lines for one command, or none.
func grantsLines(cmd *Object, heading string) ([]string, error) {
	grants := getList(cmd, "grants")
	if len(grants) == 0 {
		return nil, nil
	}
	rows := make([][]string, 0, len(grants))
	for _, raw := range grants {
		grant := asObject(raw)
		rows = append(rows, []string{
			getString(grant, "kind"),
			"`" + getString(grant, "name") + "`",
			getString(grant, "reason"),
		})
	}
	table, err := tables.RenderMarkdownTable(
		[]string{"Kind", "Name", "Reason"}, rows, nil, false,
	)
	if err != nil {
		return nil, err
	}
	return []string{heading, "", table, ""}, nil
}

// envTable renders an infra env-var table: env_var plus one other column.
func envTable(entries []any, descriptionHeader string) (string, error) {
	key := "help"
	if descriptionHeader == "Default" {
		key = "default"
	}
	rows := make([][]string, 0, len(entries))
	for _, raw := range entries {
		entry := asObject(raw)
		rows = append(rows, []string{
			"`" + getString(entry, "env_var") + "`",
			cell(get(entry, key)),
		})
	}
	return tables.RenderMarkdownTable(
		[]string{"Env var", descriptionHeader}, rows, nil, false,
	)
}

// renderCommandPage renders the Markdown page for a single command.
//
// When existingPath names a page whose description frontmatter has been
// hand-edited -- neither the current default (the complete first sentence of
// the help) nor a historical truncation nor the long-form default template --
// that description is preserved instead of being recomputed.
func renderCommandPage(cmd *Object, appName string, navOrder int, existingPath string) (string, error) {
	name := getString(cmd, "name")
	help := getString(cmd, "help")
	flags := getList(cmd, "flags")
	args := getList(cmd, "args")

	defaultDesc, err := ComputeDefaultCLIDescription(KindCommand, name, appName, help)
	if err != nil {
		return "", err
	}
	description := defaultDesc
	seeded := true
	if existingPath != "" {
		existing, handwritten, err := readExistingCLIDescription(
			existingPath, KindCommand, name, appName, help,
		)
		if err != nil {
			return "", err
		}
		if handwritten {
			description = existing
			seeded = false
		}
	}

	fields := []util.FrontmatterField{
		{Key: "title", Value: appName + " " + name},
		{Key: "description", Value: description},
		{Key: "generated", Value: true},
	}
	if seeded {
		fields = append(fields, util.FrontmatterField{Key: "seeded", Value: true})
	}
	fields = append(fields,
		util.FrontmatterField{Key: "nav_group", Value: "CLI Reference"},
		util.FrontmatterField{Key: "nav_order", Value: int64(navOrder)},
	)
	frontmatter, err := util.RenderFrontmatter(fields)
	if err != nil {
		return "", err
	}
	lines := append(strings.Split(strings.TrimRight(frontmatter, "\n"), "\n"),
		"<!-- generated by selfdoc gen (strictcli), do not edit -->",
		"",
		"# "+appName+" "+name,
		"",
	)
	if help != "" {
		lines = append(lines, help, "")
	}

	lines = append(lines, commandMetaLines(cmd)...)
	lines = append(lines, updateLines(cmd)...)

	if len(flags) > 0 {
		table, err := flagTable(flags)
		if err != nil {
			return "", err
		}
		lines = append(lines, "## Flags", "", table, "")
		lines = append(lines, flagSetsLines(cmd)...)
	}

	if len(args) > 0 {
		table, err := argTable(args)
		if err != nil {
			return "", err
		}
		lines = append(lines, "## Arguments", "", table, "")
	}

	constraints, err := constraintsLines(cmd, "## Constraints")
	if err != nil {
		return "", err
	}
	lines = append(lines, constraints...)
	grants, err := grantsLines(cmd, "## Grants")
	if err != nil {
		return "", err
	}
	lines = append(lines, grants...)

	return strings.Join(lines, "\n"), nil
}

// renderGroupPage renders the Markdown page for a command group and its
// subcommands.
//
// The group's own description is preserved the same way a command's is; see
// renderCommandPage.
func renderGroupPage(grp *Object, appName string, navOrder int, existingPath string) (string, error) {
	name := getString(grp, "name")
	help := getString(grp, "help")
	subcommands := getList(grp, "commands")

	defaultDesc, err := ComputeDefaultCLIDescription(KindGroup, name, appName, help)
	if err != nil {
		return "", err
	}
	description := defaultDesc
	seeded := true
	if existingPath != "" {
		existing, handwritten, err := readExistingCLIDescription(
			existingPath, KindGroup, name, appName, help,
		)
		if err != nil {
			return "", err
		}
		if handwritten {
			description = existing
			seeded = false
		}
	}

	fields := []util.FrontmatterField{
		{Key: "title", Value: appName + " " + name},
		{Key: "description", Value: description},
		{Key: "generated", Value: true},
	}
	if seeded {
		fields = append(fields, util.FrontmatterField{Key: "seeded", Value: true})
	}
	fields = append(fields,
		util.FrontmatterField{Key: "nav_group", Value: "CLI Reference"},
		util.FrontmatterField{Key: "nav_order", Value: int64(navOrder)},
	)
	frontmatter, err := util.RenderFrontmatter(fields)
	if err != nil {
		return "", err
	}
	lines := append(strings.Split(strings.TrimRight(frontmatter, "\n"), "\n"),
		"<!-- generated by selfdoc gen (strictcli), do not edit -->",
		"",
		"# "+appName+" "+name,
		"",
	)
	if help != "" {
		lines = append(lines, help, "")
	}

	deprecated := getObject(grp, "deprecated")
	if deprecated != nil && deprecated.Len() > 0 {
		lines = append(lines, "## Deprecated", "")
		for _, retired := range sortedKeys(deprecated) {
			value, _ := deprecated.Get(retired)
			lines = append(lines, fmt.Sprintf("- `%s` -- %s", retired, pyStr(value)))
		}
		lines = append(lines, "")
	}

	for _, raw := range subcommands {
		cmd := asObject(raw)
		cmdName := getString(cmd, "name")
		cmdHelp := getString(cmd, "help")
		flags := getList(cmd, "flags")
		args := getList(cmd, "args")

		lines = append(lines, "## "+name+" "+cmdName, "")
		if cmdHelp != "" {
			lines = append(lines, cmdHelp, "")
		}

		lines = append(lines, commandMetaLines(cmd)...)
		lines = append(lines, updateLines(cmd)...)

		if len(flags) > 0 {
			table, err := flagTable(flags)
			if err != nil {
				return "", err
			}
			lines = append(lines, "### Flags", "", table, "")
			lines = append(lines, flagSetsLines(cmd)...)
		}

		if len(args) > 0 {
			table, err := argTable(args)
			if err != nil {
				return "", err
			}
			lines = append(lines, "### Arguments", "", table, "")
		}

		constraints, err := constraintsLines(cmd, "### Constraints")
		if err != nil {
			return "", err
		}
		lines = append(lines, constraints...)
		grants, err := grantsLines(cmd, "### Grants")
		if err != nil {
			return "", err
		}
		lines = append(lines, grants...)
	}

	return strings.Join(lines, "\n"), nil
}
