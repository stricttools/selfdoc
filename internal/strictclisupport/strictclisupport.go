// Package strictclisupport is first-class support for strictcli-based
// projects.
//
// It reads .strictcli/schema.json -- the document `<app> --dump-schema`
// writes -- for the CLI's structure (the app, its commands, flags, arguments
// and groups) and renders that structure as Markdown documentation pages.
//
// # Version 2 only
//
// The reader accepts schema_version 2 and nothing else. v2 is not a superset
// of v1: it deletes the `type`, `repeatable` and arg-level `required` keys the
// v1 reader read, and adds constructs (selectors, constraints, update
// declarations) v1 had no encoding for. A schema of any other version is a
// hard error naming the regeneration command, because the alternative --
// reading a v1 document with v2 rules -- is a page that silently labels every
// flag `str` and every positional argument required. That page shipped once
// already, which is why there is no fallback here.
package strictclisupport

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/excludes"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// SupportedSchemaVersion is the one dumped-schema format this reader
// understands. There is no v1 path and no negotiated fallback.
const SupportedSchemaVersion = 2

// SchemaError is returned when a schema document is present but cannot be
// read on v2's terms: a wrong or absent schema_version, an absent project_id,
// or a project_id naming a different project.
//
// It is the counterpart of the Python surface's ValueError, and a distinct
// type so the command layer renders it as the refusal it is rather than as an
// unexpected internal failure.
type SchemaError struct {
	// Message is the diagnostic, which names the schema path and the
	// regeneration command where one applies.
	Message string
}

// Error returns the diagnostic.
func (e *SchemaError) Error() string { return e.Message }

// SchemaDiscoveryError is returned when .strictcli/schema.json discovery finds
// nothing or finds more than one candidate.
//
// It is a hard error: a page that asked for a CLI table cannot be rendered
// from a schema nobody named, and picking one of several silently would
// document a different app than the author meant.
type SchemaDiscoveryError struct {
	// Message is the diagnostic, which lists the candidates and names the
	// schema-dir attribute that disambiguates them.
	Message string
}

// Error returns the diagnostic.
func (e *SchemaDiscoveryError) Error() string { return e.Message }

// schemaDiscoveryExcludes are the directory names never traversed when
// discovering schemas. Hidden directories (a leading ".") are pruned
// separately -- .strictcli is not traversed into but is still detected as a
// schema location under each visited directory.
var schemaDiscoveryExcludes = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "_build": true,
	"target": true, "vendor": true, "venv": true, "__pycache__": true,
}

// schemaRelPath is where a project's dumped schema sits, relative to the
// directory that holds it.
const schemaRelPath = ".strictcli/schema.json"

// UsesStrictcli reports whether the project at baseDir has a
// .strictcli/schema.json file.
//
// sourcePaths is accepted for call-site symmetry and is not read: detection is
// the presence of the schema document and nothing else.
func UsesStrictcli(sourcePaths []string, baseDir string) bool {
	info, err := os.Stat(filepath.Join(baseDir, schemaRelPath))
	return err == nil && info.Mode().IsRegular()
}

// DiscoverSchemaDirs discovers the directories under baseDir that hold a
// .strictcli/schema.json.
//
// It walks baseDir, pruning vendored and build directories and every hidden
// directory, and records each visited directory that holds a schema as a path
// relative to baseDir -- the project root being ".". That relative path is the
// value a schema-dir attribute takes.
//
// The result is sorted. A walk error is not reported: an unreadable directory
// holds no schema anyone can name, which is the same answer as a directory
// that holds none.
func DiscoverSchemaDirs(baseDir string) []string {
	root := baseDir
	if abs, err := filepath.Abs(baseDir); err == nil {
		root = abs
	}
	var found []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			name := entry.Name()
			if schemaDiscoveryExcludes[name] || strings.HasPrefix(name, ".") ||
				excludes.IsScratchDir(root, path) {
				return fs.SkipDir
			}
		}
		info, statErr := os.Stat(filepath.Join(path, schemaRelPath))
		if statErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		found = append(found, rel)
		return nil
	})
	sort.Strings(found)
	return found
}

// Structure is a CLI's structure as the renderers read it: the app's own
// facts, its behavioral-completeness declarations, and its commands and
// groups in declaration order.
//
// The per-command and per-group entries stay decoded objects rather than
// becoming typed records, for two reasons the Python model had as well: every
// key a schema carries reaches the renderers untouched, whether this version
// of selfdoc knows it or not, and the staleness measurement hashes a command's
// whole entry, so anything dropped here would silently change what a hash
// covers.
type Structure struct {
	// AppName, AppVersion and AppHelp are the app's own facts.
	AppName    string
	AppVersion string
	AppHelp    string
	// GlobalFlags are the app-level flag declarations, each an object.
	GlobalFlags []any
	// Infra is the app's infrastructure declaration (roots, handshake
	// variables, connection variables), nil when it declares none.
	Infra *Object
	// Deprecated maps a retired top-level name to what replaces it, nil
	// when the app retires nothing.
	Deprecated *Object
	// Config reports whether the app reads a configuration file.
	Config bool
	// ConfigFormat, ConfigPath, ConfigConflictMode and EnvPrefix are the
	// app's behavioral-completeness declarations. Each is emitted by
	// strictcli only when the app departs from the framework's own
	// behavior, so a present value is the whole reason to document it.
	ConfigFormat       string
	ConfigPath         any
	ConfigConflictMode string
	EnvPrefix          string
	// Commands and Groups are the top-level commands and command groups,
	// in the order the schema declares them.
	Commands []*Object
	Groups   []*Object
}

// ReadSchemaJSON reads baseDir's .strictcli/schema.json and translates it into
// the structure the renderers read.
//
// It returns a nil structure and a nil error when the document does not exist
// -- "this project has no dumped schema" is an answer, not a failure. Every
// per-entry field the renderers read (value_schema, presence, default,
// choices, elect_by, nullable, negatable, unique, prefixed, variadic, hidden,
// deprecated, passthrough, flag_sets, constraints, update_of, write_mode) and
// every effects-regime per-command field (effect, consequential, grants,
// dry_run_supported, dry_run_unsupported_reason) is carried through untouched.
//
// strictcli omits the app-level global_flags, infra and deprecated keys when
// they are empty and some emitters write an explicit null; both normalize to
// empty containers, so a renderer can truth-test them.
//
// A document declaring a schema_version other than SupportedSchemaVersion,
// carrying no project_id, or naming a different project than the manifest, is
// a SchemaError.
func ReadSchemaJSON(baseDir string) (*Structure, error) {
	// The path is joined the way the Python joined it, because it is quoted
	// in every error below: a project read as "." names
	// "./.strictcli/schema.json", which is the string a reader greps for.
	schemaPath := util.PathJoin(baseDir, schemaRelPath)
	info, err := os.Stat(schemaPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, err
	}
	decoded, err := extractors.DecodeJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", schemaPath, err)
	}
	schema := asObject(decoded)
	if schema == nil {
		return nil, fmt.Errorf("%s: schema document is not a JSON object", schemaPath)
	}

	// The version is read before anything else: every key below is read on
	// v2's terms, and reading a v1 document with them produces a page that
	// is wrong without being empty.
	version := get(schema, "schema_version")
	if declared, ok := version.(int64); !ok || declared != SupportedSchemaVersion {
		appName := "<app>"
		if name := getString(schema, "name"); name != "" {
			appName = name
		}
		return nil, &SchemaError{Message: fmt.Sprintf(
			"Schema at %s declares schema_version %s; this selfdoc reads "+
				"schema_version %d only. Regenerate it with a strictcli "+
				">= 0.41.0: %s --dump-schema",
			schemaPath, pyRepr(version), SupportedSchemaVersion, appName,
		)}
	}

	projectID, hasProjectID := lookup(schema, "project_id")
	if !hasProjectID || projectID == nil {
		appName := "<app>"
		if name := getString(schema, "name"); name != "" {
			appName = name
		}
		return nil, &SchemaError{Message: fmt.Sprintf(
			"Schema missing project_id field. Regenerate with: %s --dump-schema",
			appName,
		)}
	}
	expected := util.ReadProjectField(baseDir, "name")
	declaredID, isString := projectID.(string)
	if expected != util.UnknownField && (!isString || declaredID != expected) {
		return nil, &SchemaError{Message: fmt.Sprintf(
			"Schema project_id '%s' does not match project name '%s'. "+
				"Wrong schema file?", pyStr(projectID), expected,
		)}
	}

	result := &Structure{
		AppName:            getString(schema, "name"),
		AppVersion:         getString(schema, "version"),
		AppHelp:            getString(schema, "help"),
		GlobalFlags:        getList(schema, "global_flags"),
		Infra:              getObject(schema, "infra"),
		Deprecated:         getObject(schema, "deprecated"),
		Config:             getTruthy(schema, "config"),
		ConfigFormat:       getString(schema, "config_format"),
		ConfigPath:         get(schema, "config_path"),
		ConfigConflictMode: getString(schema, "config_conflict_mode"),
		EnvPrefix:          getString(schema, "env_prefix"),
	}

	commands := getObject(schema, "commands")
	for _, name := range keysOf(commands) {
		result.Commands = append(
			result.Commands, translateCommand(name, getObject(commands, name)),
		)
	}
	groups := getObject(schema, "groups")
	for _, name := range keysOf(groups) {
		result.Groups = append(
			result.Groups, translateGroup(name, getObject(groups, name)),
		)
	}

	return result, nil
}

// CommandName is the name a command or group entry declares.
//
// The entries of a Structure are decoded objects rather than typed records --
// see the type's own documentation for why -- so these accessors are how a
// consumer reads the fields every consumer reads, without each one restating
// how a missing or wrongly-typed field degrades.
func CommandName(entry *Object) string { return getString(entry, "name") }

// CommandHelp is the help text a command or group entry declares, "" when it
// declares none.
func CommandHelp(entry *Object) string { return getString(entry, "help") }

// CommandFlags are a command's flag declarations, in declaration order. Each
// element is a decoded object, which is what IterFlagTokens and IterFlagHelp
// read.
func CommandFlags(entry *Object) []any { return getList(entry, "flags") }

// CommandArgs are a command's positional-argument declarations, in declaration
// order.
func CommandArgs(entry *Object) []any { return getList(entry, "args") }

// GroupCommands are a group's subcommands, in declaration order.
func GroupCommands(group *Object) []*Object {
	raw := getList(group, "commands")
	commands := make([]*Object, 0, len(raw))
	for _, entry := range raw {
		commands = append(commands, asObject(entry))
	}
	return commands
}

// Field is the string value of one declared field of any schema entry -- a
// flag's env var, an argument's help -- and "" when the entry omits it or
// declares it as something other than a string.
func Field(entry *Object, key string) string { return getString(entry, key) }

// keysOf are an object's keys in declaration order, and none for a nil object.
func keysOf(o *Object) []string {
	if o == nil {
		return nil
	}
	return o.Keys()
}

// translateCommand translates one schema command entry into the internal form:
// the entry as declared, with its own name filled in when the entry omits it.
func translateCommand(name string, cmd *Object) *Object {
	translated := copyObject(cmd)
	if _, ok := translated.Get("name"); !ok {
		translated.Set("name", name)
	}
	return translated
}

// translateGroup translates one schema group entry into the internal form: its
// name and help, its commands as a list, and the extra fields a group can
// carry.
func translateGroup(name string, grp *Object) *Object {
	translated := extractors.NewJSONObject()
	groupName := name
	if declared := getString(grp, "name"); declared != "" {
		groupName = declared
	}
	translated.Set("name", groupName)
	translated.Set("help", getString(grp, "help"))

	commandsObj := getObject(grp, "commands")
	commands := []any{}
	for _, cname := range keysOf(commandsObj) {
		commands = append(commands, translateCommand(cname, getObject(commandsObj, cname)))
	}
	translated.Set("commands", commands)

	for _, key := range []string{"deprecated", "groups"} {
		if v, ok := lookup(grp, key); ok {
			translated.Set(key, v)
		}
	}
	return translated
}

// NoSchemaError is returned by ExtractCLIStructure when the project has no
// dumped schema at all -- the one condition ReadSchemaJSON answers in band
// instead.
type NoSchemaError struct {
	// BaseDir is the directory that was searched.
	BaseDir string
}

// Error names the directory and the command that writes the missing document.
func (e *NoSchemaError) Error() string {
	return fmt.Sprintf(
		"No .strictcli/schema.json found in %s. Run '<app> --dump-schema' to generate it.",
		util.PythonRepr(e.BaseDir),
	)
}

// ExtractCLIStructure reads the CLI structure from baseDir's dumped schema.
//
// It is ReadSchemaJSON with the absent document turned into a NoSchemaError,
// for the call sites that have already established the project uses strictcli.
// sourcePaths is accepted for call-site symmetry and is not read.
func ExtractCLIStructure(sourcePaths []string, baseDir string) (*Structure, error) {
	result, err := ReadSchemaJSON(baseDir)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &NoSchemaError{BaseDir: baseDir}
	}
	return result, nil
}
