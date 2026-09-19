package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
)

// The declared payload schema of `selfdoc check`, held to three things: it is
// what the command actually declares, its lint-code enum stays exactly the
// shipped registry, and its coverage block states exactly the fields the
// command emits.

// checkSchema is the declaration the check command publishes.
func checkSchema(t *testing.T) map[string]any {
	t.Helper()
	entry, ok := New(Options{}).DumpSchemaDict()["commands"].(map[string]any)["check"].(map[string]any)
	if !ok {
		t.Fatal("the check command is not registered")
	}
	schema, ok := entry["payload_schema"].(map[string]any)
	if !ok {
		t.Fatal("the check command declares no payload schema")
	}
	return schema
}

// property walks a schema object to a named property's own subschema.
func property(t *testing.T, schema map[string]any, path ...string) map[string]any {
	t.Helper()
	current := schema
	for _, step := range path {
		switch step {
		case "items":
			next, ok := current["items"].(map[string]any)
			if !ok {
				t.Fatalf("no items at %v", path)
			}
			current = next
		default:
			properties, ok := current["properties"].(map[string]any)
			if !ok {
				t.Fatalf("no properties at %v", path)
			}
			next, ok := properties[step].(map[string]any)
			if !ok {
				t.Fatalf("no property %q at %v", step, path)
			}
			current = next
		}
	}
	return current
}

// stringEnum reads a schema's enum as a sorted string slice.
func stringEnum(t *testing.T, schema map[string]any) []string {
	t.Helper()
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatalf("the schema carries no enum: %v", schema)
	}
	values := make([]string, 0, len(raw))
	for _, entry := range raw {
		value, ok := entry.(string)
		if !ok {
			t.Fatalf("enum entry %v is not a string", entry)
		}
		values = append(values, value)
	}
	return values
}

func TestTheCheckCommandDeclaresTheSchema(t *testing.T) {
	schema := checkSchema(t)
	if schema["type"] != "object" {
		t.Errorf("the declaration is not an object: %v", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Errorf("the declaration admits unknown members")
	}
}

func TestSchemaLintEnumIsDerivedFromTheRegistry(t *testing.T) {
	// The declaration is a DERIVED surface: the embedded lint registry is the
	// single place a code exists. Registering a code without extending the
	// declaration fails here, and so does an enum entry no longer registered.
	declared := stringEnum(t, property(t, checkSchema(t), "lints", "items", "code"))
	expected := append([]string(nil), lints.Registered().Codes()...)
	sort.Strings(expected)

	if strings.Join(declared, ",") != strings.Join(expected, ",") {
		t.Errorf("the declaration's lint-code enum has drifted from the "+
			"registry.\ndeclared: %v\nregistry: %v", declared, expected)
	}
}

func TestSchemaLintEnumIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, code := range stringEnum(t, property(t, checkSchema(t), "lints", "items", "code")) {
		if seen[code] {
			t.Errorf("duplicate code in the enum: %s", code)
		}
		seen[code] = true
	}
}

func TestSchemaSeverityEnumMatchesTheRegistrySeverities(t *testing.T) {
	declared := stringEnum(t, property(t, checkSchema(t), "lints", "items", "severity"))
	sort.Strings(declared)

	used := map[string]bool{}
	registry := lints.Registered()
	for _, code := range registry.Codes() {
		spec, ok := registry.Spec(code)
		if !ok {
			t.Fatalf("the registry lost %s between listing and lookup", code)
		}
		used[spec.Severity] = true
	}
	severities := make([]string, 0, len(used))
	for severity := range used {
		severities = append(severities, severity)
	}
	sort.Strings(severities)

	if strings.Join(declared, ",") != strings.Join(severities, ",") {
		t.Errorf("declared severity enum %v does not match the severities the "+
			"registry uses (%v)", declared, severities)
	}
}

func TestSchemaCoverageIsNullable(t *testing.T) {
	// Nullability is part of the contract: a project with no source to cover
	// emits null here, so the declaration carries the type list.
	coverage := property(t, checkSchema(t), "coverage")
	types, ok := coverage["type"].([]any)
	if !ok || len(types) != 2 || types[0] != "object" || types[1] != "null" {
		t.Errorf(`coverage type is %v, want ["object","null"]`, coverage["type"])
	}
}

func TestSchemaCoveragePropertiesMatchTheEmittedFields(t *testing.T) {
	// Structural guard against schema rot, sibling of the lint-enum test: a
	// field the command emits but the declaration omits is a hard emission
	// failure, and a field the declaration requires but the command never
	// emits is the same failure from the other side. This names which one it
	// is instead of leaving a validator message to be decoded.
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)

	result := run(t, dir, "check", "--json", "--no-auto-commit")
	payload := payloadOf(t, result)
	coverage, ok := payload["coverage"].(map[string]any)
	if !ok {
		t.Fatal("the fixture project must produce coverage for this test to mean anything")
	}

	emitted := map[string]bool{}
	for key := range coverage {
		emitted[key] = true
	}

	schema := property(t, checkSchema(t), "coverage")
	declared := map[string]bool{}
	for key := range schema["properties"].(map[string]any) {
		declared[key] = true
	}
	required := map[string]bool{}
	for _, raw := range schema["required"].([]any) {
		required[raw.(string)] = true
	}

	for key := range emitted {
		if !declared[key] {
			t.Errorf("coverage field %q is emitted but not declared", key)
		}
		if !required[key] {
			t.Errorf("coverage field %q is emitted but not required", key)
		}
	}
	for key := range declared {
		if !emitted[key] {
			t.Errorf("coverage field %q is declared but never emitted", key)
		}
	}
}

func TestADeviatingPayloadIsRefused(t *testing.T) {
	// Enforcement is real: the framework refuses a payload that departs from
	// its declaration. Without this, every assertion above would only be
	// testing that a validator accepts things.
	finding := probePayload(t, map[string]any{
		"directives": []any{},
		"coverage":   nil,
		"lints":      []any{},
		"exit_code":  0,
		"unexpected": true,
	})
	if finding == "" {
		t.Fatal("the framework accepted a payload the declaration forbids")
	}
}

func TestTheDeclarationAcceptsEveryRegisteredCode(t *testing.T) {
	for _, code := range lints.Registered().Codes() {
		finding := probePayload(t, map[string]any{
			"directives": []any{},
			"coverage":   nil,
			"lints": []any{map[string]any{
				"file":     "docs/index.md",
				"line":     nil,
				"code":     code,
				"message":  "example message",
				"severity": "warning",
			}},
			"exit_code": 0,
		})
		if finding != "" {
			t.Errorf("the declaration refuses the registered code %s: %s", code, finding)
		}
	}
}

// probePayload emits value under the check command's declaration through a
// throwaway application, and returns the framework's complaint (empty when the
// document was accepted).
//
// No hand-rolled mirror of the schema lives here: the framework validates a
// payload against its declaration where it writes the envelope, so the honest
// way to ask "does this document satisfy the contract" is to have it emitted.
func probePayload(t *testing.T, value map[string]any) (finding string) {
	t.Helper()
	// A deviating payload is a panic in the Go implementation: the check runs
	// where the envelope is written, past the point a handler could return an
	// exit code. Recovering here turns the refusal into the string this
	// returns.
	defer func() {
		if recovered := recover(); recovered != nil {
			finding = fmt.Sprint(recovered)
		}
	}()
	app := newProbeApp(payloadschemas.Check(), value)
	result := app.Test([]string{"emit", "--json"})
	if result.ExitCode == 0 {
		return ""
	}
	return result.Stderr + result.Stdout
}

func TestDumpSchemaWritesTheProjectIdentity(t *testing.T) {
	// The Go port derives project_id from the module path in go.mod, which is
	// the module's own identity rather than the binary's name.
	isolate(t)
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, "go.mod"), "module github.com/stricttools/selfdoc\n\ngo 1.26.3\n")

	result := runCLI(t, dir, "--dump-schema")
	if result.ExitCode != 0 {
		t.Fatalf("--dump-schema failed: %s", result.Stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".strictcli", "schema.json"))
	if err != nil {
		t.Fatalf("reading the written schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decoding the written schema: %v", err)
	}
	if schema["schema_version"] != float64(2) {
		t.Errorf("schema_version is %v, want 2", schema["schema_version"])
	}
	if schema["project_id"] != "github.com/stricttools/selfdoc" {
		t.Errorf("project_id is %v, want the module path", schema["project_id"])
	}
	if schema["name"] != "selfdoc" {
		t.Errorf("name is %v, want selfdoc", schema["name"])
	}
}

// commandEntry is one registered command's schema entry.
func commandEntry(t *testing.T, name string) map[string]any {
	t.Helper()
	commands, ok := New(Options{}).DumpSchemaDict()["commands"].(map[string]any)
	if !ok {
		t.Fatal("the application declares no commands")
	}
	entry, ok := commands[name].(map[string]any)
	if !ok {
		t.Fatalf("the %s command is not registered", name)
	}
	return entry
}

// TestCheckHelpSaysThatItWrites asserts that the check command's own help says
// it advances the staleness baselines and commits them.
//
// check is one of the two declared writers of the hash store, and it is
// registered as a mutating command -- but a command called "check" is run as a
// read-only audit, and a help line that only promises to check leaves the
// write to be discovered from a dirty working tree.
func TestCheckHelpSaysThatItWrites(t *testing.T) {
	entry := commandEntry(t, "check")
	if entry["effect"] != "mutating" {
		t.Errorf("check declares effect %v, want mutating", entry["effect"])
	}
	help, ok := entry["help"].(string)
	if !ok {
		t.Fatal("check declares no help")
	}
	for _, want := range []string{"baseline", "commits"} {
		if !strings.Contains(help, want) {
			t.Errorf("the check help does not say %q:\n%s", want, help)
		}
	}
}
