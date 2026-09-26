package check

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// strictcliProject writes a project whose CLI is declared by a dumped schema,
// plus the docs pages the case provides.
func strictcliProject(
	t *testing.T, schema map[string]any, pages map[string]string,
) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "src/", "language": "python"},
	))
	write(t, filepath.Join(root, "src", "__init__.py"), `"""Module."""`+"\n")

	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("encode schema: %v", err)
	}
	write(t, filepath.Join(root, ".strictcli", "schema.json"), string(encoded))

	write(t, filepath.Join(root, "stricttools", "docs", ".keep"), "")
	// The CLI reference pages go where gen writes them: the generated docs
	// root, not the handwritten one.
	for relPath, content := range pages {
		write(t, filepath.Join(root, "stricttools", ".docs-state", "pages", relPath), content)
	}
	return root
}

// cliSchema is a dumped schema declaring the given commands and groups.
func cliSchema(commands, groups map[string]any) map[string]any {
	if commands == nil {
		commands = map[string]any{}
	}
	if groups == nil {
		groups = map[string]any{}
	}
	return map[string]any{
		"schema_version": 2,
		"name":           "myapp",
		"project_id":     "unknown",
		"version":        "1.0.0",
		"help":           "My app",
		"commands":       commands,
		"groups":         groups,
	}
}

// cliFlag is one flag declaration of a dumped schema.
func cliFlag(name, help string) map[string]any {
	return map[string]any{
		"name":         name,
		"help":         help,
		"value_schema": map[string]any{"type": "string"},
		"presence":     "optional",
	}
}

// cliIndexPage is a CLI index page with a description long enough to satisfy
// the description rules.
const cliIndexPage = "+++\ndescription = \"CLI index page for the application\"\n+++\n" +
	"# CLI\n\nOverview.\n"

func TestCLI001(t *testing.T) {

	t.Run("a missing page for a command", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name": "run", "help": "Run something",
					"flags": []any{}, "args": []any{},
				},
			}, nil),
			map[string]string{"cli-index.md": cliIndexPage},
		)
		result := checkFixture(t, root)
		matching := withCode(result.Lints, "CLI001")
		if len(matching) != 1 {
			t.Fatalf("CLI001 count = %d, want 1: %v", len(matching), messagesOf(matching))
		}
		if !strings.Contains(matching[0].Message(), "missing CLI page") ||
			!strings.Contains(matching[0].Message(), "run") {
			t.Errorf("message = %q", matching[0].Message())
		}
		if matching[0].File() != "cli-run.md" {
			t.Errorf("file = %q, want cli-run.md", matching[0].File())
		}
	})

	t.Run("a flag the page does not document", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name": "run", "help": "Run something",
					"flags": []any{
						cliFlag("verbose", "Enable verbose output"),
						cliFlag("output", "Output file"),
					},
					"args": []any{},
				},
			}, nil),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-run.md": "+++\ndescription = \"Reference for the myapp run command " +
					"with usage details\"\n+++\n# myapp run\n\n## Flags\n\n" +
					"| Name | Description |\n|------|-------------|\n" +
					"| `--verbose` | Enable verbose output |\n",
			},
		)
		result := checkFixture(t, root)
		matching := withCode(result.Lints, "CLI001")
		if len(matching) != 1 {
			t.Fatalf("CLI001 count = %d, want 1: %v", len(matching), messagesOf(matching))
		}
		if !strings.Contains(matching[0].Message(), "--output") ||
			!strings.Contains(matching[0].Message(), "not documented") {
			t.Errorf("message = %q", matching[0].Message())
		}
	})

	t.Run("a complete reference is silent", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name": "run",
					"help": "Run something useful with the project's own sources",
					"flags": []any{
						cliFlag("verbose", "Enable verbose output for the whole run"),
					},
					"args": []any{},
				},
			}, nil),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-run.md": "+++\ndescription = \"Reference for the myapp run command " +
					"with usage details\"\n+++\n# myapp run\n\n## Flags\n\n" +
					"| Name | Description |\n|------|-------------|\n" +
					"| `--verbose` | Enable verbose output |\n",
			},
		)
		result := checkFixture(t, root)
		if hasCode(result.Lints, "CLI001") {
			t.Errorf("CLI001 fired: %v", messagesOf(withCode(result.Lints, "CLI001")))
		}
	})

	t.Run("a project with no dumped schema is silent", func(t *testing.T) {
		root := pythonProject(t)
		write(t, filepath.Join(root, "stricttools", "docs", "guide.md"),
			"+++\ndescription = \"A guide covering everything the project does for a "+
				"reader.\"\n+++\n# Guide\n\nText.\n")
		result := checkFixture(t, root)
		if hasCode(result.Lints, "CLI001") || hasCode(result.Lints, "CLI002") {
			t.Errorf("a CLI rule fired for a project with no schema: %v",
				codes(result.Lints))
		}
	})
}

func TestCLI002HelpLength(t *testing.T) {

	t.Run("a command with short help", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name": "run", "help": "Run it",
					"flags": []any{}, "args": []any{},
				},
			}, nil),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-run.md": "+++\ndescription = \"Reference for the myapp run command " +
					"with usage details\"\n+++\n# myapp run\n\nText.\n",
			},
		)
		result := checkFixture(t, root)
		matching := withCode(result.Lints, "CLI002")
		if len(matching) != 1 {
			t.Fatalf("CLI002 count = %d, want 1: %v", len(matching), messagesOf(matching))
		}
		for _, fragment := range []string{"command 'run'", "minimum 50"} {
			if !strings.Contains(matching[0].Message(), fragment) {
				t.Errorf("message %q does not carry %q", matching[0].Message(), fragment)
			}
		}
		if matching[0].File() != "cli-run.md" {
			t.Errorf("file = %q, want cli-run.md", matching[0].File())
		}
	})

	t.Run("a command with adequate help is silent", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name":  "run",
					"help":  "Run the project's own build pipeline end to end, writing output",
					"flags": []any{}, "args": []any{},
				},
			}, nil),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-run.md": "+++\ndescription = \"Reference for the myapp run command " +
					"with usage details\"\n+++\n# myapp run\n\nText.\n",
			},
		)
		result := checkFixture(t, root)
		if hasCode(result.Lints, "CLI002") {
			t.Errorf("CLI002 fired: %v", messagesOf(withCode(result.Lints, "CLI002")))
		}
	})

	t.Run("a flag with short help", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name":  "run",
					"help":  "Run the project's own build pipeline end to end, writing output",
					"flags": []any{cliFlag("verbose", "Be loud")},
					"args":  []any{},
				},
			}, nil),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-run.md": "+++\ndescription = \"Reference for the myapp run command " +
					"with usage details\"\n+++\n# myapp run\n\n`--verbose` is a flag.\n",
			},
		)
		result := checkFixture(t, root)
		matching := withCode(result.Lints, "CLI002")
		if len(matching) != 1 {
			t.Fatalf("CLI002 count = %d, want 1: %v", len(matching), messagesOf(matching))
		}
		if !strings.Contains(matching[0].Message(), "flag '--verbose'") {
			t.Errorf("message = %q", matching[0].Message())
		}
	})

	t.Run("a group and its subcommand are both measured", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(nil, map[string]any{
				"release": map[string]any{
					"name": "release", "help": "Ship it",
					"commands": map[string]any{
						"run": map[string]any{
							"name": "run", "help": "Do it",
							"flags": []any{}, "args": []any{},
						},
					},
				},
			}),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-release.md": "+++\ndescription = \"Reference for the myapp release " +
					"group with usage details\"\n+++\n# myapp release\n\nText.\n",
			},
		)
		result := checkFixture(t, root)
		matching := withCode(result.Lints, "CLI002")
		if len(matching) != 2 {
			t.Fatalf("CLI002 count = %d, want 2: %v", len(matching), messagesOf(matching))
		}
		joined := strings.Join(messagesOf(matching), "\n")
		if !strings.Contains(joined, "group 'release'") {
			t.Errorf("the group's own help was not measured: %q", joined)
		}
		if !strings.Contains(joined, "command 'release run'") {
			t.Errorf("the subcommand's help was not measured: %q", joined)
		}
	})

	t.Run("an argument's help is measured", func(t *testing.T) {
		root := strictcliProject(t,
			cliSchema(map[string]any{
				"run": map[string]any{
					"name":  "run",
					"help":  "Run the project's own build pipeline end to end, writing output",
					"flags": []any{},
					"args":  []any{map[string]any{"name": "target", "help": "What"}},
				},
			}, nil),
			map[string]string{
				"cli-index.md": cliIndexPage,
				"cli-run.md": "+++\ndescription = \"Reference for the myapp run command " +
					"with usage details\"\n+++\n# myapp run\n\nText.\n",
			},
		)
		result := checkFixture(t, root)
		matching := withCode(result.Lints, "CLI002")
		if len(matching) != 1 {
			t.Fatalf("CLI002 count = %d, want 1: %v", len(matching), messagesOf(matching))
		}
		if !strings.Contains(matching[0].Message(), "arg 'target'") {
			t.Errorf("message = %q", matching[0].Message())
		}
	})
}

// TestCLIPagesResolveAcrossBothDocsRoots pins that CLI001 looks for a CLI
// reference page in both roots a project's pages come from: the generated root
// gen writes them into, and the handwritten root a project may author one in
// instead.
func TestCLIPagesResolveAcrossBothDocsRoots(t *testing.T) {
	schema := cliSchema(map[string]any{
		"run": map[string]any{
			"name":  "run",
			"help":  "Run the project's own build pipeline end to end, writing output",
			"flags": []any{}, "args": []any{},
		},
	}, nil)
	const runPage = "+++\ndescription = \"Reference for the myapp run command " +
		"with usage details\"\n+++\n# myapp run\n\nText.\n"

	t.Run("a page only in the generated root", func(t *testing.T) {
		root := strictcliProject(t, schema, map[string]string{
			"cli-index.md": cliIndexPage,
			"cli-run.md":   runPage,
		})
		generated := filepath.Join(root, "stricttools", ".docs-state", "pages", "cli-run.md")
		if !isFile(generated) {
			t.Fatalf("fixture did not write %s", generated)
		}
		if isFile(filepath.Join(root, "stricttools", "docs", "cli-run.md")) {
			t.Fatal("fixture wrote the page into the handwritten root too")
		}
		result := checkFixture(t, root)
		if hasCode(result.Lints, "CLI001") {
			t.Errorf("CLI001 fired for a page in the generated root: %v",
				messagesOf(withCode(result.Lints, "CLI001")))
		}
	})

	t.Run("a page only in the handwritten root", func(t *testing.T) {
		root := strictcliProject(t, schema, map[string]string{"cli-index.md": cliIndexPage})
		write(t, filepath.Join(root, "stricttools", "docs", "cli-run.md"), runPage)
		result := checkFixture(t, root)
		if hasCode(result.Lints, "CLI001") {
			t.Errorf("CLI001 fired for a page in the handwritten root: %v",
				messagesOf(withCode(result.Lints, "CLI001")))
		}
	})
}
