package strictclisupport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The schema path a message names is spelled the way the caller named the
// project directory. A project is read as ".", and the message the Python
// printed for a stale schema said `./<schema path>`; a reader
// grepping their terminal for that string must find it.
func TestSchemaErrorNamesThePathAsJoined(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".strictmetadata", ".cli-schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	schema := `{"schema_version": 1, "name": "toolname", "project_id": "x"}`
	if err := os.WriteFile(
		filepath.Join(dir, ".strictmetadata", ".cli-schema", "schema.json"), []byte(schema), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	_, err := ReadSchemaJSON(".")
	if err == nil {
		t.Fatal("a schema_version 1 document was accepted")
	}
	want := "Schema at ./.strictmetadata/.cli-schema/schema.json declares schema_version 1"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// A named subdirectory is joined without a leading "./", as posixpath.join
// does.
func TestSchemaErrorNamesANestedPathPlainly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg", ".strictmetadata", ".cli-schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	schema := `{"schema_version": 1, "name": "toolname", "project_id": "x"}`
	if err := os.WriteFile(
		filepath.Join(dir, "pkg", ".strictmetadata", ".cli-schema", "schema.json"), []byte(schema), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	_, err := ReadSchemaJSON("pkg")
	if err == nil {
		t.Fatal("a schema_version 1 document was accepted")
	}
	want := "Schema at pkg/.strictmetadata/.cli-schema/schema.json declares"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}
