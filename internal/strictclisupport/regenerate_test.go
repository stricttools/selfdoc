package strictclisupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/strictcli/go/strictcli"
)

// The refusals that name how to regenerate .strictmetadata/.cli-schema/schema.json
// are held to
// their word: each test takes the command a refusal prints, runs it against a
// strictcli application, and asserts the schema it produced is read.

// fixtureAppEnv turns this test binary into fixtureAppName, a small strictcli
// application, so a refusal's command can be run against a real one.
const fixtureAppEnv = "SELFDOC_STRICTCLISUPPORT_FIXTURE_APP"

const fixtureAppName = "fixtureapp"

func TestMain(m *testing.M) {
	if os.Getenv(fixtureAppEnv) == "1" {
		app := strictcli.NewApp(fixtureAppName, "0.1.0", "A strictcli application the refusals' commands run against")
		app.Command("greet", "Print a greeting",
			func(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
				return strictcli.Exit(0)
			},
			strictcli.WithEffect(strictcli.EffectReadOnly),
		)
		app.Run()
		return
	}
	os.Exit(m.Run())
}

// fixtureAppOnPath puts this test binary on PATH under the fixture app's name,
// running as that application.
func fixtureAppOnPath(t *testing.T) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, filepath.Join(bin, fixtureAppName)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fixtureAppEnv, "1")
}

// quotedCommand returns the command a refusal prints between single quotes
// after marker.
func quotedCommand(t *testing.T, message, marker string) string {
	t.Helper()
	_, rest, found := strings.Cut(message, marker+"'")
	if !found {
		t.Fatalf("the refusal names no command after %q:\n%s", marker, message)
	}
	command, _, found := strings.Cut(rest, "'")
	if !found {
		t.Fatalf("the refusal's command is not closed:\n%s", message)
	}
	return command
}

// runIn runs command through the shell in dir, as a person would type it.
func runIn(t *testing.T, dir, command string) {
	t.Helper()
	shell := exec.Command("sh", "-c", command)
	shell.Dir = dir
	if out, err := shell.CombinedOutput(); err != nil {
		t.Fatalf("running %q in %s: %v\n%s", command, dir, err, out)
	}
}

// assertTheFixtureSchemaIsRead asserts ReadSchemaJSON now reads the schema the
// fixture application wrote.
func assertTheFixtureSchemaIsRead(t *testing.T, dir string) {
	t.Helper()
	structure, err := ReadSchemaJSON(dir)
	if err != nil {
		t.Fatalf("the regenerated schema is still refused: %v", err)
	}
	if structure == nil || structure.AppName != fixtureAppName {
		t.Fatalf("the regenerated schema is not the fixture app's: %+v", structure)
	}
}

func TestTheSchemaVersionRefusalsCommandRegeneratesIt(t *testing.T) {
	isolate(t)
	fixtureAppOnPath(t)
	dir := t.TempDir()
	writeSchemaText(t, dir, `{"schema_version": 1, "name": "`+fixtureAppName+`",
	  "project_id": "old", "commands": {}, "groups": {}}`)
	_, err := ReadSchemaJSON(dir)
	if err == nil {
		t.Fatal("a schema_version 1 document was read")
	}
	runIn(t, dir, quotedCommand(t, err.Error(), "running "))
	assertTheFixtureSchemaIsRead(t, dir)
}

func TestTheMissingProjectIDRefusalsCommandRegeneratesIt(t *testing.T) {
	isolate(t)
	fixtureAppOnPath(t)
	dir := t.TempDir()
	writeSchemaText(t, dir, `{"schema_version": 2, "name": "`+fixtureAppName+`",
	  "version": "1.0", "help": "test", "commands": {}, "groups": {}}`)
	_, err := ReadSchemaJSON(dir)
	if err == nil {
		t.Fatal("a schema with no project_id was read")
	}
	runIn(t, dir, quotedCommand(t, err.Error(), "running "))
	assertTheFixtureSchemaIsRead(t, dir)
}

// The refusal for a project with no schema names the command with the
// program's name left to fill in, since nothing names it; filled in, the
// command writes the schema in a project that has no .strictmetadata/ yet.
func TestTheNoSchemaRefusalsCommandWritesTheSchema(t *testing.T) {
	isolate(t)
	fixtureAppOnPath(t)
	dir := t.TempDir()
	_, err := ExtractCLIStructure(nil, dir)
	if err == nil {
		t.Fatal("a project with no schema was read")
	}
	command := quotedCommand(t, err.Error(), "running ")
	if !strings.Contains(command, AppPlaceholder) {
		t.Fatalf("the command names no %s to fill in: %s", AppPlaceholder, command)
	}
	runIn(t, dir, strings.ReplaceAll(command, AppPlaceholder, fixtureAppName))
	assertTheFixtureSchemaIsRead(t, dir)
}

// RegenerateCommand is the command every refusal that names one prints; run
// in a directory with no .strictmetadata/, it writes the schema there.
func TestRegenerateCommandWritesTheSchemaWhereNoneWas(t *testing.T) {
	isolate(t)
	fixtureAppOnPath(t)
	dir := t.TempDir()
	runIn(t, dir, RegenerateCommand(fixtureAppName))
	assertTheFixtureSchemaIsRead(t, dir)
}

// A schema at .strictcli/schema.json, where strictcli schemas were kept before
// .strictmetadata/.cli-schema/, is not read.
func TestASchemaAtTheFormerLocationIsNotRead(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	former := filepath.Join(dir, ".strictcli", "schema.json")
	if err := os.MkdirAll(filepath.Dir(former), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(former, []byte(`{"schema_version": 2, "name": "x", "project_id": "x", "commands": {}, "groups": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if UsesStrictcli(nil, dir) {
		t.Error("a schema at .strictcli/schema.json made the project count as a strictcli project")
	}
	if structure, err := ReadSchemaJSON(dir); structure != nil || err != nil {
		t.Errorf("ReadSchemaJSON read the former location: %+v, %v", structure, err)
	}
	if found := DiscoverSchemaDirs(dir); len(found) != 0 {
		t.Errorf("DiscoverSchemaDirs found the former location: %v", found)
	}
}
