package content

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stricttools/selfdoc/internal/strictclisupport"
)

// writeSchema writes a minimal dumped schema under dir.
func writeSchema(t *testing.T, dir string) {
	t.Helper()
	write(t, filepath.Join(dir, ".strictcli", "schema.json"), `{
	  "schema_version": 2,
	  "project_id": "test-app",
	  "name": "testcli",
	  "version": "1.0.0",
	  "help": "A test CLI",
	  "commands": {"hello": {"help": "Say hello", "flags": [], "args": []}},
	  "groups": {
	    "config": {"help": "configuration",
	      "commands": {"show": {"help": "Show the config", "flags": [], "args": []}}}
	  }
	}`)
}

// commands renders the table-commands directive, failing on any error.
func commands(t *testing.T, base string, attrs map[string]string) string {
	t.Helper()
	rendered, err := ResolveTableCommands(attrs, map[string]any{}, base)
	if err != nil {
		t.Fatalf("ResolveTableCommands: %v", err)
	}
	return rendered
}

// discoveryError asserts that err is the discovery refusal and returns its
// message.
func discoveryError(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a schema-discovery error")
	}
	var refusal *strictclisupport.SchemaDiscoveryError
	if !errors.As(err, &refusal) {
		t.Fatalf("err is %T, want *strictclisupport.SchemaDiscoveryError", err)
	}
	return refusal.Message
}

func TestTableCommands(t *testing.T) {
	isolate(t)

	t.Run("the command table", func(t *testing.T) {
		base := t.TempDir()
		writeSchema(t, base)
		rendered := commands(t, base, nil)
		wants(t, rendered,
			"| Command | Description |",
			"| `hello` | Say hello |",
			"| **config** | configuration |",
			"| `config show` | Show the config |",
		)
	})

	t.Run("a schema in a subdirectory is discovered", func(t *testing.T) {
		base := t.TempDir()
		writeSchema(t, filepath.Join(base, "myapp"))
		wants(t, commands(t, base, nil), "`hello`")
	})

	t.Run("schema-dir pins the choice", func(t *testing.T) {
		// A second schema elsewhere would make discovery ambiguous; the
		// explicit override decides regardless.
		base := t.TempDir()
		writeSchema(t, filepath.Join(base, "myapp"))
		writeSchema(t, filepath.Join(base, "other"))
		wants(t, commands(t, base, map[string]string{"schema-dir": "myapp"}), "`hello`")
	})

	t.Run("an absent schema is a hard error", func(t *testing.T) {
		_, err := ResolveTableCommands(nil, map[string]any{}, t.TempDir())
		wants(t, discoveryError(t, err), "no .strictcli/schema.json", "schema-dir")
	})

	t.Run("ambiguous discovery is a hard error listing the candidates", func(t *testing.T) {
		base := t.TempDir()
		writeSchema(t, filepath.Join(base, "alpha"))
		writeSchema(t, filepath.Join(base, "beta"))
		message := discoveryError(t, func() error {
			_, err := ResolveTableCommands(nil, map[string]any{}, base)
			return err
		}())
		wants(t, message, "multiple", "schema-dir", "alpha", "beta")
	})

	t.Run("a schema-dir naming nothing is a hard error", func(t *testing.T) {
		_, err := ResolveTableCommands(
			map[string]string{"schema-dir": "nope"}, map[string]any{}, t.TempDir(),
		)
		wants(t, discoveryError(t, err), "schema-dir")
	})

	t.Run("a schema with no commands at all", func(t *testing.T) {
		base := t.TempDir()
		write(t, filepath.Join(base, ".strictcli", "schema.json"), `{
		  "schema_version": 2, "project_id": "x", "name": "x",
		  "commands": {}, "groups": {}}`)
		wants(t, commands(t, base, nil), "no commands found in '.'")
	})

	t.Run("a malformed schema is the reader's error", func(t *testing.T) {
		base := t.TempDir()
		write(t, filepath.Join(base, ".strictcli", "schema.json"), `{
		  "schema_version": 1, "project_id": "x", "name": "old",
		  "commands": {}, "groups": {}}`)
		_, err := ResolveTableCommands(nil, map[string]any{}, base)
		if err == nil {
			t.Fatal("a v1 schema must be refused")
		}
		wants(t, err.Error(), "schema_version")
	})

	t.Run("without config", func(t *testing.T) {
		base := t.TempDir()
		writeSchema(t, base)
		rendered, ok := resolve(t, "table-commands", nil, nil, base, nil)
		if !ok {
			t.Fatal("table-commands is a content directive")
		}
		wants(t, rendered, "requires project config")
	})

	t.Run("through the dispatcher", func(t *testing.T) {
		base := t.TempDir()
		writeSchema(t, base)
		rendered, ok, err := ResolveContent(
			"table-commands", map[string]string{"schema-dir": "."}, nil,
			base, map[string]any{},
		)
		if err != nil || !ok {
			t.Fatalf("ResolveContent: ok=%v err=%v", ok, err)
		}
		wants(t, rendered, "`hello`")
	})
}
