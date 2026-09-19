package strictclisupport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/smm-h/stricttest/go/hygiene"
)

// isolate binds the environment isolation floor. Nothing here calls
// t.Parallel: hygiene mutates process-wide variables.
func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeSchemaText writes text as dir's dumped schema and returns its path.
func writeSchemaText(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, ".strictcli", "schema.json")
	writeFile(t, path, text)
	return path
}

// writePyproject writes a minimal pyproject.toml declaring name.
func writePyproject(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "pyproject.toml"),
		"[project]\nname = \""+name+"\"\nversion = \"0.1.0\"\n")
}

// mustReadSchema reads dir's schema, failing the test on any error.
func mustReadSchema(t *testing.T, dir string) *Structure {
	t.Helper()
	structure, err := ReadSchemaJSON(dir)
	if err != nil {
		t.Fatalf("ReadSchemaJSON: %v", err)
	}
	if structure == nil {
		t.Fatal("ReadSchemaJSON returned no structure")
	}
	return structure
}

// structureFromJSON builds a Structure from the literal form the Python tests
// wrote their fixtures in: an object keyed by app_name, commands and groups.
func structureFromJSON(t *testing.T, text string) *Structure {
	t.Helper()
	decoded, err := extractors.DecodeJSON([]byte(text))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	obj := asObject(decoded)
	if obj == nil {
		t.Fatal("fixture is not a JSON object")
	}
	structure := &Structure{
		AppName:            getString(obj, "app_name"),
		AppVersion:         getString(obj, "app_version"),
		AppHelp:            getString(obj, "app_help"),
		GlobalFlags:        getList(obj, "global_flags"),
		Infra:              getObject(obj, "infra"),
		Deprecated:         getObject(obj, "deprecated"),
		Config:             getTruthy(obj, "config"),
		ConfigFormat:       getString(obj, "config_format"),
		ConfigPath:         get(obj, "config_path"),
		ConfigConflictMode: getString(obj, "config_conflict_mode"),
		EnvPrefix:          getString(obj, "env_prefix"),
	}
	for _, raw := range getList(obj, "commands") {
		structure.Commands = append(structure.Commands, asObject(raw))
	}
	for _, raw := range getList(obj, "groups") {
		structure.Groups = append(structure.Groups, asObject(raw))
	}
	return structure
}

// mustGenerate generates the pages for structure into dir/docs and returns the
// docs directory.
func mustGenerate(t *testing.T, structure *Structure, dir string) string {
	t.Helper()
	docsDir := filepath.Join(dir, ".stricttools", "docs")
	if _, err := GenerateCLIPages(structure, docsDir, effects.Unbound()); err != nil {
		t.Fatalf("GenerateCLIPages: %v", err)
	}
	return docsDir
}

// genFromSchema writes text as dir's schema, reads it back and generates the
// pages, returning the docs directory.
func genFromSchema(t *testing.T, dir, text string) string {
	t.Helper()
	writeSchemaText(t, dir, text)
	return mustGenerate(t, mustReadSchema(t, dir), dir)
}

func readPage(t *testing.T, docsDir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(docsDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func wants(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, want := range substrings {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func rejects(t *testing.T, content string, substrings ...string) {
	t.Helper()
	for _, unwanted := range substrings {
		if strings.Contains(content, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, content)
		}
	}
}

// -- Fixtures ---------------------------------------------------------------

// realisticSchema is a dumped schema for a strictcli app exercising the
// app-level sections, the effects regime and a group.
const realisticSchema = `{
  "schema_version": 2,
  "name": "testapp",
  "project_id": "testapp",
  "version": "1.0",
  "help": "A test app",
  "env_prefix": null,
  "config": false,
  "global_flags": [
    {"name": "json", "value_schema": {"type": "boolean"},
     "help": "emit machine-readable JSON", "short": null,
     "presence": "default", "default": false, "env": null,
     "negatable": true}
  ],
  "infra": {
    "roots": [{"env_var": "TESTAPP_HOME", "default": "~/.testapp"}],
    "handshakes": [
      {"env_var": "TESTAPP_SESSION_ID",
       "help": "session identifier set by the invoking agent"}
    ],
    "connections": [
      {"env_var": "TESTAPP_DB_URL", "help": "database connection URL"}
    ]
  },
  "commands": {
    "deploy": {
      "name": "deploy",
      "help": "deploy stuff",
      "effect": "mutating",
      "consequential": true,
      "dry_run_supported": false,
      "dry_run_unsupported_reason": "the remote decides what a deploy does, so a preview cannot honestly show the result",
      "grants": [
        {"kind": "proc_mutate", "name": "push",
         "reason": "publishing the build is what this command is for"}
      ],
      "flags": [
        {"name": "target", "value_schema": {"type": "string"},
         "help": "deploy target", "short": null, "presence": "default",
         "default": "prod", "env": null, "negatable": null, "hidden": false},
        {"name": "dry-run", "value_schema": {"type": "boolean"},
         "help": "dry run mode", "short": "n", "presence": "default",
         "default": false, "env": null, "negatable": true, "hidden": false}
      ],
      "args": [],
      "passthrough": false
    }
  },
  "groups": {
    "config": {
      "name": "config",
      "help": "configuration",
      "commands": {
        "show": {
          "name": "show",
          "help": "show config",
          "effect": "read_only",
          "flags": [
            {"name": "format",
             "value_schema": {"type": "string", "enum": ["text", "json"]},
             "help": "output format", "short": null, "presence": "default",
             "default": "text", "env": null,
             "choices": [
               {"value": "text", "help": "human-readable output"},
               {"value": "json"}
             ],
             "negatable": null, "hidden": false}
          ],
          "args": [],
          "passthrough": false
        }
      },
      "deprecated": {},
      "groups": {}
    }
  },
  "deprecated": {"ship": "use 'deploy' instead"}
}`

// basicStructure is the hand-built structure the page-generation tests render.
const basicStructure = `{
  "app_name": "testapp",
  "app_version": "1.0",
  "app_help": "A test app",
  "commands": [
    {"name": "deploy", "help": "deploy stuff",
     "flags": [
       {"name": "target", "value_schema": {"type": "string"},
        "help": "deploy target", "short": null, "presence": "default",
        "default": "prod", "env": null},
       {"name": "dry-run", "value_schema": {"type": "boolean"},
        "help": "dry run mode", "short": "n", "presence": "optional",
        "env": null}
     ],
     "args": []}
  ],
  "groups": [
    {"name": "config", "help": "configuration",
     "commands": [
       {"name": "show", "help": "show config",
        "flags": [
          {"name": "format", "value_schema": {"type": "string"},
           "help": "output format", "short": null, "presence": "default",
           "default": "text", "env": null}
        ],
        "args": []}
     ]}
  ]
}`

// preEffectsStructure is a CLI structure from a schema that predates the
// effects regime: no effect key anywhere, so the badge lines and the
// reserved-quartet section must both stay off.
const preEffectsStructure = `{
  "app_name": "oldapp",
  "app_version": "1.0",
  "app_help": "An app built before the effects regime",
  "commands": [{"name": "deploy", "help": "deploy stuff", "flags": [], "args": []}],
  "groups": [
    {"name": "config", "help": "configuration",
     "commands": [{"name": "show", "help": "show config", "flags": [], "args": []}]}
  ]
}`

// -- Schema discovery -------------------------------------------------------

const minimalSchema = `{"schema_version": 2, "project_id": "x", "name": "x",
  "commands": {}, "groups": {}}`

func TestDiscoverSchemaDirs(t *testing.T) {
	isolate(t)
	cases := []struct {
		name  string
		dirs  []string
		wants []string
	}{
		{"none found", nil, nil},
		{"root schema", []string{""}, []string{"."}},
		{"subdir schema", []string{"app"}, []string{"app"}},
		{
			"multiple schemas sorted",
			[]string{"", "beta", "alpha"},
			[]string{".", "alpha", "beta"},
		},
		{
			"excludes vendored and hidden dirs",
			[]string{"node_modules/pkg", "dist", ".venv/lib", "real"},
			[]string{"real"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			base := t.TempDir()
			for _, sub := range testCase.dirs {
				writeSchemaText(t, filepath.Join(base, sub), minimalSchema)
			}
			got := DiscoverSchemaDirs(base)
			if strings.Join(got, ",") != strings.Join(testCase.wants, ",") {
				t.Fatalf("DiscoverSchemaDirs = %v, want %v", got, testCase.wants)
			}
		})
	}
}

// -- Detection --------------------------------------------------------------

func TestUsesStrictcli(t *testing.T) {
	isolate(t)

	t.Run("schema json exists", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, realisticSchema)
		if !UsesStrictcli([]string{"src/"}, dir) {
			t.Fatal("UsesStrictcli = false, want true")
		}
	})

	t.Run("no schema json", func(t *testing.T) {
		if UsesStrictcli([]string{"src/"}, t.TempDir()) {
			t.Fatal("UsesStrictcli = true, want false")
		}
	})

	t.Run("empty source paths", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, realisticSchema)
		if !UsesStrictcli(nil, dir) {
			t.Fatal("source paths are not read, so the schema still decides")
		}
	})

	t.Run("nonexistent base dir", func(t *testing.T) {
		if UsesStrictcli([]string{"src/"}, "/nonexistent/path") {
			t.Fatal("UsesStrictcli = true, want false")
		}
	})
}

// -- Schema reader ----------------------------------------------------------

func TestReadSchemaJSONReturnsNilWhenMissing(t *testing.T) {
	isolate(t)
	structure, err := ReadSchemaJSON(t.TempDir())
	if err != nil {
		t.Fatalf("ReadSchemaJSON: %v", err)
	}
	if structure != nil {
		t.Fatal("a project with no schema reads as no structure, not an error")
	}
}

func TestReadSchemaJSONTranslation(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	writeSchemaText(t, dir, realisticSchema)
	structure := mustReadSchema(t, dir)

	if structure.AppName != "testapp" {
		t.Errorf("AppName = %q", structure.AppName)
	}
	if structure.AppVersion != "1.0" {
		t.Errorf("AppVersion = %q", structure.AppVersion)
	}
	if structure.AppHelp != "A test app" {
		t.Errorf("AppHelp = %q", structure.AppHelp)
	}

	if len(structure.Commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(structure.Commands))
	}
	deploy := structure.Commands[0]
	if getString(deploy, "name") != "deploy" {
		t.Errorf("command name = %q", getString(deploy, "name"))
	}
	if getString(deploy, "help") != "deploy stuff" {
		t.Errorf("command help = %q", getString(deploy, "help"))
	}
	if truthy(get(deploy, "passthrough")) {
		t.Error("passthrough false must survive the translation as false")
	}

	if len(structure.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(structure.Groups))
	}
	group := structure.Groups[0]
	if getString(group, "name") != "config" {
		t.Errorf("group name = %q", getString(group, "name"))
	}
	if getString(group, "help") != "configuration" {
		t.Errorf("group help = %q", getString(group, "help"))
	}
	subcommands := getList(group, "commands")
	if len(subcommands) != 1 {
		t.Fatalf("got %d group commands, want 1", len(subcommands))
	}
	show := asObject(subcommands[0])
	if getString(show, "name") != "show" || getString(show, "help") != "show config" {
		t.Errorf("group command = %v", PlainValue(show))
	}

	flags := getList(deploy, "flags")
	if len(flags) != 2 {
		t.Fatalf("got %d flags, want 2", len(flags))
	}
	target := findFlag(t, flags, "target")
	if getString(getObject(target, "value_schema"), "type") != "string" {
		t.Error("target's value_schema is not carried through")
	}
	if getString(target, "presence") != "default" {
		t.Errorf("target presence = %q", getString(target, "presence"))
	}
	if getString(target, "default") != "prod" {
		t.Errorf("target default = %v", get(target, "default"))
	}
	if get(target, "short") != nil {
		t.Errorf("target short = %v, want null", get(target, "short"))
	}
	dryRun := findFlag(t, flags, "dry-run")
	if getString(getObject(dryRun, "value_schema"), "type") != "boolean" {
		t.Error("dry-run's value_schema is not carried through")
	}
	if getString(dryRun, "short") != "n" {
		t.Errorf("dry-run short = %v", get(dryRun, "short"))
	}
	if get(dryRun, "negatable") != true {
		t.Errorf("dry-run negatable = %v", get(dryRun, "negatable"))
	}
	if get(dryRun, "hidden") != false {
		t.Errorf("dry-run hidden = %v", get(dryRun, "hidden"))
	}

	// v2 splits choices in two: the values as an enum inside the fragment,
	// the value-plus-help records beside it.
	format := getList(show, "flags")[0]
	enum := getList(getObject(asObject(format), "value_schema"), "enum")
	if len(enum) != 2 || enum[0] != "text" || enum[1] != "json" {
		t.Errorf("format enum = %v", enum)
	}
	records := getList(asObject(format), "choices")
	if len(records) != 2 {
		t.Fatalf("got %d choice records, want 2", len(records))
	}
	if getString(asObject(records[0]), "value") != "text" ||
		getString(asObject(records[0]), "help") != "human-readable output" {
		t.Errorf("first choice record = %v", PlainValue(records[0]))
	}
	if getString(asObject(records[1]), "value") != "json" {
		t.Errorf("second choice record = %v", PlainValue(records[1]))
	}
}

func findFlag(t *testing.T, flags []any, name string) *Object {
	t.Helper()
	for _, raw := range flags {
		flag := asObject(raw)
		if getString(flag, "name") == name {
			return flag
		}
	}
	t.Fatalf("no flag named %q", name)
	return nil
}

func TestReadSchemaJSONMalformedJSON(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	writeSchemaText(t, dir, "not valid json{{{")
	if _, err := ReadSchemaJSON(dir); err == nil {
		t.Fatal("a malformed schema document must be an error")
	}
}

func TestReadSchemaJSONEmptySchema(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	writeSchemaText(t, dir, `{"schema_version": 2, "name": "empty",
	  "project_id": "empty", "version": "0.1", "help": ""}`)
	structure := mustReadSchema(t, dir)
	if structure.AppName != "empty" {
		t.Errorf("AppName = %q", structure.AppName)
	}
	if len(structure.Commands) != 0 || len(structure.Groups) != 0 {
		t.Error("a schema with no commands and no groups declares none")
	}
}

// -- project_id validation --------------------------------------------------

func TestProjectIDValidation(t *testing.T) {
	isolate(t)

	t.Run("missing project_id", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "testapp",
		  "version": "1.0", "help": "test", "commands": {}, "groups": {}}`)
		_, err := ReadSchemaJSON(dir)
		if err == nil || !strings.Contains(err.Error(), "Schema missing project_id field") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing project_id names the app", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "myapp",
		  "version": "1.0", "help": "test", "commands": {}, "groups": {}}`)
		_, err := ReadSchemaJSON(dir)
		if err == nil || !strings.Contains(err.Error(), "myapp --dump-schema") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("mismatched project_id", func(t *testing.T) {
		dir := t.TempDir()
		writePyproject(t, dir, "testapp")
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "testapp",
		  "project_id": "wrong", "version": "1.0", "help": "test",
		  "commands": {}, "groups": {}}`)
		_, err := ReadSchemaJSON(dir)
		if err == nil || !strings.Contains(err.Error(), "does not match project name") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("matching project_id", func(t *testing.T) {
		dir := t.TempDir()
		writePyproject(t, dir, "testapp")
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "testapp",
		  "project_id": "testapp", "version": "1.0", "help": "test",
		  "commands": {}, "groups": {}}`)
		if mustReadSchema(t, dir).AppName != "testapp" {
			t.Fatal("a matching project_id reads normally")
		}
	})

	t.Run("polyglot go repo resolves its name from go.mod", func(t *testing.T) {
		// The selfdoc.json source language picks the manifest; an
		// incidental package.json (a browser-test harness at the repo
		// root) must not win the lookup chain and break the check.
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/owner/proj\n")
		writeFile(t, filepath.Join(dir, "package.json"),
			`{"name": "proj-webui-tests", "private": true}`+"\n")
		writeFile(t, filepath.Join(dir, "selfdoc.json"),
			`{"version": "0.3.0", "source": [{"path": ".", "language": "go"}]}`+"\n")
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "proj",
		  "project_id": "github.com/owner/proj", "version": "1.0",
		  "help": "test", "commands": {}, "groups": {}}`)
		if mustReadSchema(t, dir).AppName != "proj" {
			t.Fatal("the go.mod identity must answer the project_id check")
		}
	})

	t.Run("unknown project name skips the check", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "testapp",
		  "project_id": "anything", "version": "1.0", "help": "test",
		  "commands": {}, "groups": {}}`)
		if mustReadSchema(t, dir).AppName != "testapp" {
			t.Fatal("with no manifest to compare against, any project_id is accepted")
		}
	})
}

// -- Schema version ---------------------------------------------------------

func TestSchemaVersionRefusals(t *testing.T) {
	isolate(t)
	cases := []struct {
		name string
		text string
	}{
		{"v1 is refused", `{"schema_version": 1, "name": "old",
		  "project_id": "old", "commands": {}, "groups": {}}`},
		{"a null version is refused", `{"schema_version": null, "name": "old",
		  "project_id": "old", "commands": {}, "groups": {}}`},
		{"an absent version is refused", `{"name": "old",
		  "project_id": "old", "commands": {}, "groups": {}}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSchemaText(t, dir, testCase.text)
			_, err := ReadSchemaJSON(dir)
			if err == nil || !strings.Contains(err.Error(), "schema_version") {
				t.Fatalf("err = %v", err)
			}
			var schemaErr *SchemaError
			if !asSchemaError(err, &schemaErr) {
				t.Fatalf("err is %T, want *SchemaError", err)
			}
			if !strings.Contains(err.Error(), "old --dump-schema") {
				t.Errorf("the refusal must name the regeneration command: %v", err)
			}
		})
	}
}

func asSchemaError(err error, target **SchemaError) bool {
	schemaErr, ok := err.(*SchemaError)
	if ok {
		*target = schemaErr
	}
	return ok
}

// -- App-level fields -------------------------------------------------------

func TestReadSchemaJSONAppLevelFields(t *testing.T) {
	isolate(t)

	t.Run("global flags, infra and deprecated pass through", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, realisticSchema)
		structure := mustReadSchema(t, dir)

		if len(structure.GlobalFlags) != 1 {
			t.Fatalf("got %d global flags, want 1", len(structure.GlobalFlags))
		}
		flag := asObject(structure.GlobalFlags[0])
		if getString(flag, "name") != "json" {
			t.Errorf("global flag name = %q", getString(flag, "name"))
		}
		if getString(flag, "help") != "emit machine-readable JSON" {
			t.Errorf("global flag help = %q", getString(flag, "help"))
		}

		roots := getList(structure.Infra, "roots")
		if getString(asObject(roots[0]), "env_var") != "TESTAPP_HOME" {
			t.Error("infra roots are not carried through")
		}
		handshakes := getList(structure.Infra, "handshakes")
		if getString(asObject(handshakes[0]), "env_var") != "TESTAPP_SESSION_ID" {
			t.Error("infra handshakes are not carried through")
		}
		connections := getList(structure.Infra, "connections")
		if getString(asObject(connections[0]), "env_var") != "TESTAPP_DB_URL" {
			t.Error("infra connections are not carried through")
		}

		if getString(structure.Deprecated, "ship") != "use 'deploy' instead" {
			t.Error("the deprecated map is not carried through")
		}
	})

	t.Run("absent app-level fields normalize to empty", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "bare",
		  "project_id": "bare", "version": "1.0", "help": "",
		  "commands": {}, "groups": {}}`)
		structure := mustReadSchema(t, dir)
		if len(structure.GlobalFlags) != 0 {
			t.Error("absent global_flags must read as none")
		}
		if truthy(structure.Infra) || truthy(structure.Deprecated) {
			t.Error("absent infra and deprecated must read as empty")
		}
	})

	t.Run("explicit nulls normalize to empty", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, `{"schema_version": 2, "name": "bare",
		  "project_id": "bare", "version": "1.0", "help": "",
		  "commands": {}, "groups": {},
		  "global_flags": null, "infra": null, "deprecated": null}`)
		structure := mustReadSchema(t, dir)
		if len(structure.GlobalFlags) != 0 {
			t.Error("a null global_flags must read as none")
		}
		if truthy(structure.Infra) || truthy(structure.Deprecated) {
			t.Error("a null infra and deprecated must read as empty")
		}
	})

	t.Run("per-command effects fields pass through", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, realisticSchema)
		structure := mustReadSchema(t, dir)
		deploy := structure.Commands[0]
		if getString(deploy, "effect") != "mutating" {
			t.Errorf("effect = %q", getString(deploy, "effect"))
		}
		if get(deploy, "consequential") != true {
			t.Errorf("consequential = %v", get(deploy, "consequential"))
		}
		if get(deploy, "dry_run_supported") != false {
			t.Errorf("dry_run_supported = %v", get(deploy, "dry_run_supported"))
		}
		grants := getList(deploy, "grants")
		if getString(asObject(grants[0]), "kind") != "proc_mutate" {
			t.Error("grants are not carried through")
		}
		show := asObject(getList(structure.Groups[0], "commands")[0])
		if getString(show, "effect") != "read_only" {
			t.Errorf("group command effect = %q", getString(show, "effect"))
		}
	})
}

// -- ExtractCLIStructure ----------------------------------------------------

func TestExtractCLIStructure(t *testing.T) {
	isolate(t)

	t.Run("wraps the reader", func(t *testing.T) {
		dir := t.TempDir()
		writeSchemaText(t, dir, realisticSchema)
		structure, err := ExtractCLIStructure([]string{"src/"}, dir)
		if err != nil {
			t.Fatalf("ExtractCLIStructure: %v", err)
		}
		if structure.AppName != "testapp" || structure.AppVersion != "1.0" ||
			structure.AppHelp != "A test app" {
			t.Errorf("structure = %+v", structure)
		}
		if len(structure.Commands) != 1 || len(structure.Groups) != 1 {
			t.Errorf("got %d commands and %d groups, want 1 and 1",
				len(structure.Commands), len(structure.Groups))
		}
		if len(getList(structure.Commands[0], "flags")) != 2 {
			t.Error("the command's flags are not carried through")
		}
		show := asObject(getList(structure.Groups[0], "commands")[0])
		if getString(show, "name") != "show" {
			t.Errorf("group command = %v", PlainValue(show))
		}
		if getString(getList(show, "flags")[0].(*Object), "default") != "text" {
			t.Error("the group command's flag default is not carried through")
		}
	})

	t.Run("no schema is an error", func(t *testing.T) {
		_, err := ExtractCLIStructure([]string{"src/"}, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "schema.json") {
			t.Fatalf("err = %v", err)
		}
		if _, ok := err.(*NoSchemaError); !ok {
			t.Fatalf("err is %T, want *NoSchemaError", err)
		}
	})
}

// -- Page generation --------------------------------------------------------

func TestGenerateCLIPages(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	structure := structureFromJSON(t, basicStructure)
	docsDir := filepath.Join(dir, ".stricttools", "docs")
	pages, err := GenerateCLIPages(structure, docsDir, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateCLIPages: %v", err)
	}

	t.Run("returns the filenames", func(t *testing.T) {
		joined := strings.Join(pages, ",")
		for _, want := range []string{"cli-index.md", "cli-deploy.md", "cli-config.md"} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing %q in %v", want, pages)
			}
		}
	})

	t.Run("creates the files read-only", func(t *testing.T) {
		for _, name := range pages {
			info, err := os.Stat(filepath.Join(docsDir, name))
			if err != nil {
				t.Fatalf("stat %s: %v", name, err)
			}
			if info.Mode().Perm()&0o400 == 0 {
				t.Errorf("%s is not readable by its owner", name)
			}
			if info.Mode().Perm()&0o200 != 0 {
				t.Errorf("%s is writable by its owner", name)
			}
		}
	})

	t.Run("index frontmatter", func(t *testing.T) {
		content := readPage(t, docsDir, "cli-index.md")
		if !strings.HasPrefix(content, "+++\n") {
			t.Error("the page must open with its frontmatter fence")
		}
		wants(t, content,
			"generated = true", "title = \"", `nav_group = "CLI Reference"`,
			"nav_order = 91",
		)
	})

	t.Run("command page frontmatter and marker", func(t *testing.T) {
		content := readPage(t, docsDir, "cli-deploy.md")
		wants(t, content,
			`nav_group = "CLI Reference"`, "nav_order =",
			"<!-- generated by selfdoc gen (strictcli), do not edit -->",
		)
	})

	t.Run("group page frontmatter", func(t *testing.T) {
		content := readPage(t, docsDir, "cli-config.md")
		wants(t, content, `nav_group = "CLI Reference"`, "nav_order =")
	})

	t.Run("flag table", func(t *testing.T) {
		content := readPage(t, docsDir, "cli-deploy.md")
		wants(t, content,
			"| Name | Short | Type | Presence | Env | Description |",
			"`--target`", "`--dry-run`", "`-n`", "deploy target",
		)
	})

	t.Run("group page carries its subcommands", func(t *testing.T) {
		content := readPage(t, docsDir, "cli-config.md")
		wants(t, content, "config show", "show config", "`--format`")
	})

	t.Run("the index links its siblings one level up", func(t *testing.T) {
		// Pages are emitted at <stem>/index.html, so the index page is
		// itself inside a directory and a bare cli-deploy/ would resolve
		// inside it. The hop back up is what makes the link land.
		content := readPage(t, docsDir, "cli-index.md")
		wants(t, content, "(../cli-deploy/)", "(../cli-config/)")
		rejects(t, content, ".html)")
	})
}

func TestGenerateCLIPagesOverwritesExisting(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	docsDir := mustGenerate(t, structureFromJSON(t, basicStructure), dir)

	updated := structureFromJSON(t, basicStructure)
	updated.Commands[0].Set("help", "new help text")
	if _, err := GenerateCLIPages(updated, docsDir, effects.Unbound()); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	wants(t, readPage(t, docsDir, "cli-deploy.md"), "new help text")
}

func TestExpectedCLIPageFilenames(t *testing.T) {
	isolate(t)

	if got := ExpectedCLIPageFilenames(nil); len(got) != 0 {
		t.Errorf("ExpectedCLIPageFilenames(nil) = %v, want none", got)
	}

	structure := structureFromJSON(t, `{
	  "commands": [{"name": "deploy"}, {"name": "ping"}],
	  "groups": [{"name": "config"}]
	}`)
	got := strings.Join(ExpectedCLIPageFilenames(structure), ",")
	for _, want := range []string{
		"cli-index.md", "cli-deploy.md", "cli-ping.md", "cli-config.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

// -- Argument tables --------------------------------------------------------

func TestCommandPageArgumentsTable(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	docsDir := mustGenerate(t, structureFromJSON(t, `{
	  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
	  "commands": [
	    {"name": "deploy", "help": "deploy stuff", "flags": [],
	     "args": [
	       {"name": "target", "presence": "required", "help": "deploy target"},
	       {"name": "extra", "presence": "optional", "help": "optional extra arg"}
	     ]}
	  ],
	  "groups": []
	}`), dir)
	content := readPage(t, docsDir, "cli-deploy.md")
	wants(t, content,
		"## Arguments",
		"| Name | Type | Presence | Description |",
		"| --- | --- | --- | --- |",
		"| `target` |  | required | deploy target |",
		"| `extra` |  | optional | optional extra arg |",
	)
}

func TestCommandPageArgumentPresenceIsNeverGuessed(t *testing.T) {
	// v2 emits presence on every arg entry, so this shape cannot come from
	// strictcli. The rule the test pins is that the page states nothing
	// rather than inventing "required", which is what the deleted
	// ar.get("required", True) read did.
	isolate(t)
	dir := t.TempDir()
	docsDir := mustGenerate(t, structureFromJSON(t, `{
	  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
	  "commands": [
	    {"name": "run", "help": "run something", "flags": [],
	     "args": [{"name": "script", "help": "script to run"}]}
	  ],
	  "groups": []
	}`), dir)
	wants(t, readPage(t, docsDir, "cli-run.md"), "| `script` |  |  | script to run |")
}

func TestGroupPageArgumentsTable(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	docsDir := mustGenerate(t, structureFromJSON(t, `{
	  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
	  "commands": [],
	  "groups": [
	    {"name": "config", "help": "configuration",
	     "commands": [
	       {"name": "set", "help": "set a config value", "flags": [],
	        "args": [
	          {"name": "key", "presence": "required", "help": "config key"},
	          {"name": "value", "presence": "required", "help": "config value"}
	        ]},
	       {"name": "get", "help": "get a config value", "flags": [],
	        "args": [
	          {"name": "key", "presence": "required", "help": "config key to read"},
	          {"name": "fallback", "presence": "optional", "help": "default if missing"}
	        ]}
	     ]}
	  ]
	}`), dir)
	content := readPage(t, docsDir, "cli-config.md")
	wants(t, content,
		"### Arguments",
		"| Name | Type | Presence | Description |",
		"| --- | --- | --- | --- |",
		"| `key` |  | required | config key |",
		"| `value` |  | required | config value |",
		"| `fallback` |  | optional | default if missing |",
		"## config set", "## config get",
		"| `key` |  | required | config key to read |",
	)
}

// -- Description preservation -----------------------------------------------

// descriptionStructure carries one command and one group whose help is long
// enough for the first-sentence default, and one of each whose help is short
// enough for the long-form template.
const descriptionStructure = `{
  "app_name": "testapp",
  "app_version": "1.0",
  "app_help": "A test app",
  "commands": [
    {"name": "deploy",
     "help": "Deploy the application to one or more configured remote environments with health checks.",
     "flags": [], "args": []},
    {"name": "ping", "help": "ping a host", "flags": [], "args": []}
  ],
  "groups": [
    {"name": "config",
     "help": "Manage configuration files for the application across multiple environments.",
     "commands": []},
    {"name": "log", "help": "show logs", "commands": []}
  ]
}`

// rewriteDescription replaces the description line in a generated page's
// frontmatter, handling the read-only permissions selfdoc sets on it.
func rewriteDescription(t *testing.T, path, description string) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(content), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "description = \"") {
			lines[index] = `description = "` + description + `"`
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDescriptionPreservation(t *testing.T) {
	isolate(t)

	cases := []struct {
		name        string
		page        string
		description string
	}{
		{"command", "cli-deploy.md", "Handwritten deployment description with full sentence ending."},
		{"group", "cli-config.md", "Handwritten config group description with explicit purpose."},
		{"index", "cli-index.md", "Handwritten CLI index landing description for testapp."},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			docsDir := mustGenerate(t, structureFromJSON(t, descriptionStructure), dir)
			path := filepath.Join(docsDir, testCase.page)
			rewriteDescription(t, path, testCase.description)

			if _, err := GenerateCLIPages(
				structureFromJSON(t, descriptionStructure), docsDir, effects.Unbound(),
			); err != nil {
				t.Fatalf("regenerate: %v", err)
			}
			wants(t, readPage(t, docsDir, testCase.page),
				`description = "`+testCase.description+`"`)
		})
	}

	t.Run("a handwritten description survives repeated regeneration", func(t *testing.T) {
		dir := t.TempDir()
		docsDir := mustGenerate(t, structureFromJSON(t, descriptionStructure), dir)
		custom := "Persistent handwritten deploy description spanning runs."
		rewriteDescription(t, filepath.Join(docsDir, "cli-deploy.md"), custom)
		for range 3 {
			if _, err := GenerateCLIPages(
				structureFromJSON(t, descriptionStructure), docsDir, effects.Unbound(),
			); err != nil {
				t.Fatalf("regenerate: %v", err)
			}
		}
		wants(t, readPage(t, docsDir, "cli-deploy.md"), `description = "`+custom+`"`)
	})

	t.Run("a default description is recomputed", func(t *testing.T) {
		dir := t.TempDir()
		docsDir := mustGenerate(t, structureFromJSON(t, descriptionStructure), dir)

		// Change the command's help and regenerate without touching the
		// page's description. Because the existing description is still
		// the machine's, it must be overwritten from the new help.
		updated := structureFromJSON(t, descriptionStructure)
		updated.Commands[0].Set("help", "Updated deploy help text long enough "+
			"to trigger truncation of the description so the regeneration is "+
			"observable.")
		if _, err := GenerateCLIPages(updated, docsDir, effects.Unbound()); err != nil {
			t.Fatalf("regenerate: %v", err)
		}
		wants(t, readPage(t, docsDir, "cli-deploy.md"), "Updated deploy help text")
	})

	t.Run("a grown help reseeds to the complete first sentence", func(t *testing.T) {
		// A stale machine default must not be mistaken for handwritten.
		// When the help gains a second sentence the first sentence is
		// unchanged, so the reseeded description is that complete first
		// sentence -- no truncation, no trailing ellipsis.
		dir := t.TempDir()
		docsDir := mustGenerate(t, structureFromJSON(t, descriptionStructure), dir)

		updated := structureFromJSON(t, descriptionStructure)
		grown := getString(updated.Commands[0], "help") +
			" Now with extra detail about the deploy lifecycle and rollback " +
			"behavior on failure."
		updated.Commands[0].Set("help", grown)
		if _, err := GenerateCLIPages(updated, docsDir, effects.Unbound()); err != nil {
			t.Fatalf("regenerate: %v", err)
		}

		content := readPage(t, docsDir, "cli-deploy.md")
		wants(t, content, `description = "`+prose.FirstSentence(grown)+`"`)
		for _, line := range strings.Split(content, "\n") {
			if !strings.HasPrefix(line, "description = \"") {
				continue
			}
			rejects(t, line, "...", "Now with extra detail")
			break
		}
	})

	t.Run("a shipped 155-character truncation is reseeded", func(t *testing.T) {
		// A shipped help[:155] mid-sentence truncation is machine-owned
		// and reseeded to the full first sentence, not frozen as if a
		// person had written it.
		dir := t.TempDir()
		longHelp := "Deploy the application to every one of the configured " +
			"remote environments, running health checks and automatic " +
			"rollback on failure, then emit a detailed report describing " +
			"the whole run."
		if len(longHelp) <= 155 {
			t.Fatalf("the fixture help must exceed 155 characters, got %d", len(longHelp))
		}
		structure := structureFromJSON(t, descriptionStructure)
		structure.Commands[0].Set("help", longHelp)
		docsDir := mustGenerate(t, structure, dir)

		deployPath := filepath.Join(docsDir, "cli-deploy.md")
		rewriteDescription(t, deployPath, longHelp[:155])

		regenerated := structureFromJSON(t, descriptionStructure)
		regenerated.Commands[0].Set("help", longHelp)
		if _, err := GenerateCLIPages(regenerated, docsDir, effects.Unbound()); err != nil {
			t.Fatalf("regenerate: %v", err)
		}

		content := readPage(t, docsDir, "cli-deploy.md")
		wants(t, content, `description = "`+prose.FirstSentence(longHelp)+`"`)
		rejects(t, content, `description = "`+longHelp[:155]+`"`)
	})
}

// -- Effects metadata -------------------------------------------------------

func TestEffectBadges(t *testing.T) {
	isolate(t)

	t.Run("a mutating consequential command carries its badge", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), realisticSchema)
		content := readPage(t, docsDir, "cli-deploy.md")
		wants(t, content,
			"**Effect:** mutating", "**consequential**",
			"`--approve-consequential`",
			"**Dry run:** not supported",
			"the remote decides what a deploy does",
		)
	})

	t.Run("a read_only group command carries its badge", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), realisticSchema)
		content := readPage(t, docsDir, "cli-config.md")
		wants(t, content, "**Effect:** read_only")
		// A read_only command is never consequential, and an absent
		// dry_run_supported is the normal case that prints nothing.
		rejects(t, content, "**consequential**", "**Dry run:**")
	})

	t.Run("a pre-effects schema carries no badge", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, preEffectsStructure), t.TempDir())
		rejects(t, readPage(t, docsDir, "cli-deploy.md"), "**Effect:**")
		rejects(t, readPage(t, docsDir, "cli-config.md"), "**Effect:**")
	})
}

func TestGrantsTable(t *testing.T) {
	isolate(t)

	t.Run("a command's grants render under an H2", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), realisticSchema)
		content := readPage(t, docsDir, "cli-deploy.md")
		wants(t, content,
			"## Grants", "| Kind | Name | Reason |",
			"| proc_mutate | `push` |",
			"publishing the build is what this command is for",
		)
	})

	t.Run("no grants, no table", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, preEffectsStructure), t.TempDir())
		rejects(t, readPage(t, docsDir, "cli-deploy.md"), "Grants")
	})

	t.Run("a subcommand's grants render under an H3", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, `{
		  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
		  "commands": [],
		  "groups": [
		    {"name": "release", "help": "release management",
		     "commands": [
		       {"name": "run", "help": "run a release", "effect": "mutating",
		        "consequential": true,
		        "grants": [{"kind": "net_mutate", "name": "publish",
		                    "reason": "a release publishes to a registry"}],
		        "flags": [], "args": []}
		     ]}
		  ]
		}`), t.TempDir())
		content := readPage(t, docsDir, "cli-release.md")
		wants(t, content,
			"### Grants", "| Kind | Name | Reason |",
			"| net_mutate | `publish` |",
		)
	})
}

func TestGroupDeprecatedSection(t *testing.T) {
	isolate(t)

	t.Run("a group's own deprecated map renders", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, `{
		  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
		  "commands": [],
		  "groups": [
		    {"name": "config", "help": "configuration",
		     "deprecated": {"dump": "use 'config show' instead"},
		     "commands": [
		       {"name": "show", "help": "show config", "effect": "read_only",
		        "flags": [], "args": []}
		     ]}
		  ]
		}`), t.TempDir())
		content := readPage(t, docsDir, "cli-config.md")
		wants(t, content, "## Deprecated", "`dump`", "use 'config show' instead")
	})

	t.Run("no deprecated map, no section", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, preEffectsStructure), t.TempDir())
		rejects(t, readPage(t, docsDir, "cli-config.md"), "Deprecated")
	})
}

func TestReservedQuartetSection(t *testing.T) {
	isolate(t)

	t.Run("present for a schema with effects", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), realisticSchema)
		content := readPage(t, docsDir, "cli-index.md")
		wants(t, content,
			"## Framework flags", "`--dry-run`", "`--approve-consequential`",
			"`--quiet`", "`--verbose`",
		)
	})

	t.Run("absent for a pre-effects schema", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, preEffectsStructure), t.TempDir())
		content := readPage(t, docsDir, "cli-index.md")
		rejects(t, content, "## Framework flags", "--approve-consequential")
	})

	t.Run("keyed on the schema, not the top level", func(t *testing.T) {
		// An app whose only effect declarations live inside a group still
		// gets the section.
		docsDir := mustGenerate(t, structureFromJSON(t, `{
		  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
		  "commands": [],
		  "groups": [
		    {"name": "config", "help": "configuration",
		     "commands": [
		       {"name": "show", "help": "show config", "effect": "read_only",
		        "flags": [], "args": []}
		     ]}
		  ]
		}`), t.TempDir())
		wants(t, readPage(t, docsDir, "cli-index.md"), "## Framework flags")
	})
}

func TestIndexAppLevelSections(t *testing.T) {
	isolate(t)

	t.Run("global flags, infrastructure and deprecated", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), realisticSchema)
		content := readPage(t, docsDir, "cli-index.md")
		wants(t, content,
			"## Global flags",
			"| Name | Short | Type | Presence | Env | Description |",
			"`--json`", "emit machine-readable JSON",
			"## Infrastructure",
			"`TESTAPP_HOME`", "~/.testapp",
			"`TESTAPP_SESSION_ID`", "session identifier set by the invoking agent",
			"`TESTAPP_DB_URL`",
			"## Deprecated", "`ship`", "use 'deploy' instead",
		)
	})

	t.Run("sections absent when empty", func(t *testing.T) {
		docsDir := mustGenerate(t, structureFromJSON(t, preEffectsStructure), t.TempDir())
		content := readPage(t, docsDir, "cli-index.md")
		rejects(t, content, "## Global flags", "## Infrastructure", "## Deprecated")
	})

	t.Run("a structured default renders as JSON", func(t *testing.T) {
		// strictcli's relative_to_root marker is a container, and a page
		// showing Python syntax for it would be publishing the wrong
		// language to a reader of a CLI reference.
		docsDir := mustGenerate(t, structureFromJSON(t, `{
		  "app_name": "testapp", "app_version": "1.0", "app_help": "A test app",
		  "commands": [], "groups": [],
		  "global_flags": [
		    {"name": "archive-dir", "value_schema": {"type": "string"},
		     "help": "path to the archive directory", "presence": "default",
		     "default": {"relative_to_root": {"env_var": "T_HOME"}}}
		  ]
		}`), t.TempDir())
		content := readPage(t, docsDir, "cli-index.md")
		wants(t, content, "`{\"relative_to_root\": {\"env_var\": \"T_HOME\"}}`")
		rejects(t, content, "'relative_to_root'")
	})
}

// -- v2 constructs ----------------------------------------------------------

// v2Schema wraps one command entry as a whole schema document.
func v2Schema(command string) string {
	return `{"schema_version": 2, "name": "app", "project_id": "app",
	  "version": "1.0", "help": "an app",
	  "commands": {"run": ` + command + `}, "groups": {}}`
}

// v2Command is a minimal v2 command entry carrying flags.
func v2Command(flags string) string {
	return `{"name": "run",
	  "help": "run the thing that this command runs, at length",
	  "effect": "mutating", "flags": [` + flags + `], "args": []}`
}

func TestValueSchemaTypeWords(t *testing.T) {
	isolate(t)
	cases := []struct {
		fragment string
		word     string
	}{
		{`{"type": "string"}`, "str"},
		{`{"type": "boolean"}`, "bool"},
		{`{"type": "integer"}`, "int"},
		{`{"type": "number"}`, "float"},
		{`{"type": "array", "items": {"type": "string"}}`, "list[str]"},
		{`{"type": "array", "items": {"type": "integer"}}`, "list[int]"},
		{
			`{"type": "object", "additionalProperties": {"type": "string"}}`,
			"dict[str, str]",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.word, func(t *testing.T) {
			docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(
				`{"name": "value", "help": "a value of some declared shape",
				  "value_schema": `+testCase.fragment+`, "presence": "optional"}`,
			)))
			wants(t, readPage(t, docsDir, "cli-run.md"), "| "+testCase.word+" |")
		})
	}
}

func TestNoFlagIsLabelledStrByDefault(t *testing.T) {
	// The v1 reader's fl.get("type", "str") labelled everything str.
	isolate(t)
	docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(
		`{"name": "count", "help": "how many times to run the thing",
		  "value_schema": {"type": "integer"}, "presence": "required"}`,
	)))
	content := readPage(t, docsDir, "cli-run.md")
	wants(t, content, "| int |")
	rejects(t, content, "| str |")
}

func TestPresenceColumn(t *testing.T) {
	isolate(t)

	t.Run("required, optional and default", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(
			`{"name": "a", "help": "the required one, described at length",
			  "value_schema": {"type": "string"}, "presence": "required"},
			 {"name": "b", "help": "the optional one, described at length",
			  "value_schema": {"type": "string"}, "presence": "optional"},
			 {"name": "c", "help": "the defaulted one, described at length",
			  "value_schema": {"type": "integer"}, "presence": "default",
			  "default": 3}`,
		)))
		wants(t, readPage(t, docsDir, "cli-run.md"),
			"| Name | Short | Type | Presence | Env | Description |",
			"| `--a` |  | str | required |",
			"| `--b` |  | str | optional |",
			"| `--c` |  | int | default: `3` |",
		)
	})

	t.Run("an empty declared default is rendered", func(t *testing.T) {
		// [], "", 0 and false are declarations, not absences.
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(
			`{"name": "tags", "help": "the empty list default, at some length",
			  "value_schema": {"type": "array", "items": {"type": "string"}},
			  "presence": "default", "default": []},
			 {"name": "quiet", "help": "the false default, described at length",
			  "value_schema": {"type": "boolean"}, "presence": "default",
			  "default": false}`,
		)))
		wants(t, readPage(t, docsDir, "cli-run.md"),
			"default: `[]`", "default: `false`")
	})
}

func TestChoicesRecords(t *testing.T) {
	isolate(t)
	docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(
		`{"name": "format", "help": "how the result should be rendered",
		  "value_schema": {"type": "string", "enum": ["text", "json"]},
		  "presence": "optional",
		  "choices": [{"value": "text", "help": "an indented text tree"},
		              {"value": "json"}]}`,
	)))
	wants(t, readPage(t, docsDir, "cli-run.md"),
		"Values: `text` (an indented text tree), `json`.")
}

// memberSpelledCommand declares a member-flags selector with a payload-carrying
// choice and a choice with no scope at all.
const memberSpelledFlags = `{
  "name": "mode",
  "help": "which mode to run, of the two declared below here",
  "presence": "required",
  "elect_by": "member-flags",
  "choices": [
    {"name": "pattern",
     "help": "match mode: rewrite every occurrence found",
     "flags": [
       {"name": "value", "help": "the regex to match with",
        "value_schema": {"type": "string"}, "presence": "required"},
       {"name": "replace",
        "help": "literal text to substitute for each match",
        "value_schema": {"type": "string"}, "presence": "optional"}
     ]},
    {"name": "everything",
     "help": "whole-tree mode: no pattern is consulted at all"}
  ]
}`

func TestSelectorRendering(t *testing.T) {
	isolate(t)

	t.Run("a member-spelled selector is not a flag token", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(memberSpelledFlags)))
		content := readPage(t, docsDir, "cli-run.md")
		rejects(t, content, "`--mode`")
		wants(t, content,
			"| `mode` |",
			"`--pattern`", "`--everything`",
			"Elects `mode` = `pattern`.",
			indent+"`--pattern`",
			"`--replace`", "Only with `--pattern`.",
		)
		// The reserved `value` entry is the member's own payload, so it
		// is not a token of its own.
		rejects(t, content, "`--value`")
		wants(t, content, "Its value: the regex to match with")
	})

	t.Run("a token-spelled selector keeps its dashes", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(`{
		  "name": "via",
		  "help": "the delivery channel this notification travels on",
		  "presence": "required",
		  "elect_by": "selector-token",
		  "choices": [
		    {"name": "email", "help": "deliver it as an email message",
		     "flags": [{"name": "recipient", "help": "destination address",
		                "value_schema": {"type": "string"},
		                "presence": "required"}]},
		    {"name": "webhook", "help": "post it to a URL somewhere"}
		  ]
		}`)))
		wants(t, readPage(t, docsDir, "cli-run.md"),
			"`--via <choice>`", "Only with `--via email`.")
	})
}

func TestConstraintRendering(t *testing.T) {
	isolate(t)

	t.Run("at least one", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(`{
		  "name": "run",
		  "help": "run the thing that this command runs, at length",
		  "effect": "mutating", "args": [],
		  "flags": [
		    {"name": "commits", "help": "the commits to select entries by",
		     "value_schema": {"type": "string"}, "presence": "optional"},
		    {"name": "id", "help": "the entry id to select an entry by",
		     "value_schema": {"type": "string"}, "presence": "optional"}
		  ],
		  "constraints": [
		    {"type": "at_least_one", "name": "entry-selection",
		     "members": [
		       {"kind": "flag", "name": "commits", "when": "present"},
		       {"kind": "flag", "name": "id", "when": "present"}
		     ]}
		  ]
		}`))
		wants(t, readPage(t, docsDir, "cli-run.md"),
			"## Constraints", "`entry-selection`",
			"At least one of `--commits` (when supplied), `--id` (when supplied).",
		)
	})

	t.Run("requires and implies", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(`{
		  "name": "run",
		  "help": "run the thing that this command runs, at length",
		  "effect": "mutating", "args": [], "flags": [],
		  "constraints": [
		    {"type": "requires", "name": "needs-base",
		     "flag": "diff", "depends_on": "base"},
		    {"type": "implies", "name": "forces-json",
		     "flag": "machine", "implies": "format", "value": "json"}
		  ]
		}`))
		wants(t, readPage(t, docsDir, "cli-run.md"),
			"`--diff` requires `--base`.",
			"`--machine` implies `--format` = `json`.",
		)
	})
}

// updateCommand declares an update with one clearable property.
func updateCommand(writeMode string) string {
	return `{
	  "name": "update-record",
	  "help": "run the thing that this command runs, at length",
	  "effect": "mutating",
	  "update_of": {"resource": "dns-record", "identity": ["zone"],
	                "properties": ["content", "ttl"]},
	  "write_mode": "` + writeMode + `",
	  "args": [],
	  "flags": [
	    {"name": "zone", "help": "the zone the record belongs to",
	     "value_schema": {"type": "string"}, "presence": "required"},
	    {"name": "content", "help": "the record's content value",
	     "value_schema": {"type": "string"}, "presence": "optional"},
	    {"name": "ttl", "help": "the record's time to live, seconds",
	     "value_schema": {"type": "integer"}, "presence": "optional",
	     "nullable": true}
	  ]
	}`
}

func TestUpdateRendering(t *testing.T) {
	isolate(t)

	t.Run("resource, mode and roles", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), `{"schema_version": 2,
		  "name": "app", "project_id": "app", "version": "1.0",
		  "help": "an app", "groups": {},
		  "commands": {"update-record": `+updateCommand("sparse")+`}}`)
		content := readPage(t, docsDir, "cli-update-record.md")
		wants(t, content,
			"**Updates:** `dns-record` (write mode: sparse)",
			"- Identified by: `--zone`",
			"- Writes: `--content`, `--ttl` -- at least one of them is required.",
			"- A property that is not supplied is left unchanged.",
			// A nullable property publishes the minted --unset flag.
			"`--ttl`, `--unset-ttl`",
			"- Clearable: `--unset-ttl`.",
		)
	})

	t.Run("full_replace says the other thing", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), `{"schema_version": 2,
		  "name": "app", "project_id": "app", "version": "1.0",
		  "help": "an app", "groups": {},
		  "commands": {"update-record": `+updateCommand("full_replace")+`}}`)
		wants(t, readPage(t, docsDir, "cli-update-record.md"), "re-sent as read")
	})
}

func TestNegationSpelling(t *testing.T) {
	// A negatable bool publishes --no-x, which gets no entry of its own.
	isolate(t)
	docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command(
		`{"name": "cache", "help": "whether to consult the on-disk cache",
		  "value_schema": {"type": "boolean"}, "presence": "optional"},
		 {"name": "force-delete",
		  "help": "delete without consulting anything at all",
		  "value_schema": {"type": "boolean"}, "presence": "optional",
		  "negatable": false}`,
	)))
	content := readPage(t, docsDir, "cli-run.md")
	wants(t, content, "`--cache`, `--no-cache`")
	rejects(t, content, "`--no-force-delete`")
}

func TestFlagSetsRendering(t *testing.T) {
	// A command's flag-set grouping is otherwise invisible on the page.
	isolate(t)
	docsDir := genFromSchema(t, t.TempDir(), v2Schema(`{
	  "name": "run", "help": "run the thing that this command runs, at length",
	  "effect": "mutating", "args": [],
	  "flags": [
	    {"name": "push-timeout", "help": "seconds to allow per push",
	     "value_schema": {"type": "integer"}, "presence": "optional"},
	    {"name": "ci-timeout", "help": "seconds to allow for the CI",
	     "value_schema": {"type": "integer"}, "presence": "optional"}
	  ],
	  "flag_sets": [{"name": "timeouts", "flags": ["push-timeout", "ci-timeout"]}]
	}`))
	wants(t, readPage(t, docsDir, "cli-run.md"),
		"Flag sets:", "- `timeouts` -- `--push-timeout`, `--ci-timeout`")
}

func TestAppConfigSection(t *testing.T) {
	isolate(t)

	t.Run("the config keys render", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), `{"schema_version": 2,
		  "name": "app", "project_id": "app", "version": "1.0",
		  "help": "an app", "groups": {},
		  "commands": {"run": `+v2Command("")+`},
		  "config": true, "config_format": "toml",
		  "config_conflict_mode": "config-wins", "env_prefix": "APP_"}`)
		wants(t, readPage(t, docsDir, "cli-index.md"),
			"## Configuration", "`toml`", "`config-wins`", "`APP_`")
	})

	t.Run("absent when the app declares nothing", func(t *testing.T) {
		docsDir := genFromSchema(t, t.TempDir(), v2Schema(v2Command("")))
		rejects(t, readPage(t, docsDir, "cli-index.md"), "## Configuration")
	})
}

// -- The flag-token walk ----------------------------------------------------

func TestIterFlagTokens(t *testing.T) {
	isolate(t)

	t.Run("a member selector yields its choices, not itself", func(t *testing.T) {
		flags := mustDecodeList(t, `[`+memberSpelledFlags+`]`)
		got := strings.Join(IterFlagTokens(flags), ",")
		if got != "--pattern,--replace,--everything" {
			t.Fatalf("IterFlagTokens = %s", got)
		}
	})

	t.Run("a token selector yields its own name", func(t *testing.T) {
		flags := mustDecodeList(t, `[{
		  "name": "via", "help": "h", "presence": "required",
		  "elect_by": "selector-token",
		  "choices": [{"name": "email", "help": "h", "flags": [
		    {"name": "recipient", "help": "h",
		     "value_schema": {"type": "string"}, "presence": "required"}
		  ]}]
		}]`)
		got := strings.Join(IterFlagTokens(flags), ",")
		if got != "--via,--recipient" {
			t.Fatalf("IterFlagTokens = %s", got)
		}
	})
}

func TestIterFlagHelp(t *testing.T) {
	isolate(t)
	flags := mustDecodeList(t, `[`+memberSpelledFlags+`]`)
	var labels []string
	for _, entry := range IterFlagHelp(flags) {
		labels = append(labels, entry.Label)
	}
	got := strings.Join(labels, ",")
	want := "mode,--pattern,--pattern <value>,--replace,--everything"
	if got != want {
		t.Fatalf("IterFlagHelp labels = %s, want %s", got, want)
	}
	for _, entry := range IterFlagHelp(flags) {
		if entry.Label == "--pattern <value>" && entry.Help != "the regex to match with" {
			t.Errorf("the payload's help is not reported: %q", entry.Help)
		}
	}
}

func mustDecodeList(t *testing.T, text string) []any {
	t.Helper()
	decoded, err := extractors.DecodeJSON([]byte(text))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := decoded.([]any)
	if !ok {
		t.Fatal("fixture is not a JSON array")
	}
	return items
}
