package golang

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/smm-h/stricttest/go/hygiene"
)

// Every expectation in this file is a recorded observation: the strings were
// printed by the Python implementation itself, over the same fixture, by
// scripts/probe_go_extractor.py.

const commitGo = "// Package commit implements the two-phase commit pipeline.\n" +
	"// Phase A is parallel-safe, Phase B is serialized.\n" +
	"package commit\n" +
	"\n" +
	"import \"context\"\n" +
	"\n" +
	"// Exit codes for commit-specific errors.\n" +
	"const (\n" +
	"\tExitCASExhausted  = 7\n" +
	"\tExitWriteTree     = 9\n" +
	")\n" +
	"\n" +
	"// DefaultTimeout is the default lock timeout in seconds.\n" +
	"const DefaultTimeout = 30\n" +
	"\n" +
	"// CommitError carries a structured exit code alongside the error message.\n" +
	"type CommitError struct {\n" +
	"\tCode    int\n" +
	"\tMessage string\n" +
	"}\n" +
	"\n" +
	"// Error implements the error interface.\n" +
	"func (e *CommitError) Error() string { return e.Message }\n" +
	"\n" +
	"// Pipeline orchestrates the full commit flow.\n" +
	"type Pipeline struct {\n" +
	"\tSafegitDir string\n" +
	"\tConfig     Config\n" +
	"}\n" +
	"\n" +
	"// Execute runs the full two-phase commit pipeline.\n" +
	"// On CAS miss it retries up to MaxAttempts times.\n" +
	"func (p *Pipeline) Execute(ctx context.Context, req Request) (*Result, error) {\n" +
	"\treturn nil, nil\n" +
	"}\n" +
	"\n" +
	"// unexportedHelper is private and should be skipped.\n" +
	"func unexportedHelper() {}\n" +
	"\n" +
	"// NewPipeline creates a new Pipeline with defaults.\n" +
	"func NewPipeline(dir string) *Pipeline {\n" +
	"\treturn &Pipeline{SafegitDir: dir}\n" +
	"}\n"

const typesGo = "package commit\n" +
	"\n" +
	"// Request holds all inputs for a single commit operation.\n" +
	"type Request struct {\n" +
	"\tMessage string\n" +
	"\tFiles   []string\n" +
	"}\n" +
	"\n" +
	"// Result is the JSON-serializable output of a successful commit.\n" +
	"type Result struct {\n" +
	"\tSHA      string `json:\"sha\"`\n" +
	"\tRef      string `json:\"ref\"`\n" +
	"\tParent   string `json:\"parent\"`\n" +
	"\tAttempts int    `json:\"attempts\"`\n" +
	"}\n"

const commitTestGo = "package commit\n" +
	"\n" +
	"import \"testing\"\n" +
	"\n" +
	"// TestNewPipeline verifies default construction.\n" +
	"func TestNewPipeline(t *testing.T) {\n" +
	"\tp := NewPipeline(\"/tmp/test\")\n" +
	"\tif p.SafegitDir != \"/tmp/test\" {\n" +
	"\t\tt.Errorf(\"got %q, want /tmp/test\", p.SafegitDir)\n" +
	"\t}\n" +
	"}\n" +
	"\n" +
	"func TestExecute(t *testing.T) {\n" +
	"\t// placeholder\n" +
	"}\n"

const mainGo = "package main\n" +
	"\n" +
	"import (\n" +
	"\t\"flag\"\n" +
	"\t\"fmt\"\n" +
	")\n" +
	"\n" +
	"func usageText() string {\n" +
	"\treturn `Usage: myapp <command> [options]\n" +
	"\n" +
	"Commands:\n" +
	"  run     Run the processor\n" +
	"  check   Check configuration\n" +
	"\n" +
	"Global flags:\n" +
	"  --verbose   Verbose output\n" +
	"  --quiet     Suppress output\n" +
	"`\n" +
	"}\n" +
	"\n" +
	"var verbose bool\n" +
	"var outputFile string\n" +
	"\n" +
	"func main() {\n" +
	"\tflag.BoolVar(&verbose, \"verbose\", false, \"Enable verbose output\")\n" +
	"\tflag.StringVar(&outputFile, \"output\", \"out.txt\", \"Output file path\")\n" +
	"\tflag.Parse()\n" +
	"\tfmt.Println(\"hello\")\n" +
	"}\n"

const commandsGo = "package main\n" +
	"\n" +
	"func registerCommands(app *App) {\n" +
	"\tapp.Command(\"run\", \"Run the processor\")\n" +
	"\tapp.Command(\"check\", \"Check configuration\")\n" +
	"}\n"

const modelsGo = "package models\n" +
	"\n" +
	"// Config holds application configuration.\n" +
	"type Config struct {\n" +
	"\tHost    string `json:\"host\" yaml:\"host\"` // Server hostname\n" +
	"\tPort    int    `json:\"port\"`             // Listen port\n" +
	"\tDebug   bool   `json:\"debug\"`\n" +
	"\tinternal string // unexported, should appear but lowercase\n" +
	"}\n" +
	"\n" +
	"// Entry represents a log entry.\n" +
	"type Entry struct {\n" +
	"\tLevel   string `json:\"level\"`\n" +
	"\tMessage string `json:\"message\"` // Log message text\n" +
	"}\n"

const serverGo = "package server\n" +
	"\n" +
	"// Merge combines two values with a separator.\n" +
	"// The a and b values are concatenated using sep.\n" +
	"func Merge(a, b int, sep string) string {\n" +
	"    return \"\"\n" +
	"}\n" +
	"\n" +
	"// Handle processes an HTTP request.\n" +
	"// Returns the response status code.\n" +
	"func (s *Server) Handle(req *http.Request) (int, error) {\n" +
	"    return 200, nil\n" +
	"}\n" +
	"\n" +
	"// Printf formats and prints.\n" +
	"func Printf(format string, args ...interface{}) {\n" +
	"}\n" +
	"\n" +
	"// NoDoc has no documentation.\n" +
	"func NoDoc(x int) {\n" +
	"}\n"

// fixture writes the sample Go project every test in this file reads and
// returns its base directory.
func fixture(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	files := map[string]string{
		"internal/commit/commit.go":      commitGo,
		"internal/commit/types.go":       typesGo,
		"internal/commit/commit_test.go": commitTestGo,
		"cmd/myapp/main.go":              mainGo,
		"cmd/myapp/commands.go":          commandsGo,
		"internal/models/models.go":      modelsGo,
		"pkg/server.go":                  serverGo,
		"mypkg/doc.go":                   "// Package mypkg provides utilities.\npackage mypkg\n",
		"mypkg/main.go":                  "package mypkg\n\nfunc Hello() string { return \"hello\" }\n",
		"testsonly/only_test.go":         "package testsonly\n",
		"config.json":                    `{"host":"localhost","port":3000,"debug":false}`,
		"app.toml":                       "[server]\nhost = \"localhost\"\nport = 8080\n\n[logging]\nlevel = \"info\"\n",
	}
	for name, content := range files {
		full := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func newExtractor() extractors.Extractor { return New() }

// writeInto writes one file into an existing directory, which is what a case
// needs when a package is more than one file.
func writeInto(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRefRendersTheWholePackage is the strongest parity check in this file: one
// package across two files, whose doc comment, const block, single const,
// types, function and methods all reach the page in the grouped order, compared
// byte for byte.
func TestRefRendersTheWholePackage(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)

	got, err := newExtractor().Extract("ref", map[string]string{"path": "internal/commit"}, nil, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	want := "## `internal/commit`\n\n" +
		"Package commit implements the two-phase commit pipeline. Phase A is parallel-safe, Phase B is serialized.\n\n" +
		"### `ExitCASExhausted`\n\n```go\nconst ExitCASExhausted  = 7\n```\n\n" +
		"Exit codes for commit-specific errors.\n\n" +
		"### `ExitWriteTree`\n\n```go\nconst ExitWriteTree     = 9\n```\n\n" +
		"### `DefaultTimeout`\n\n```go\nconst DefaultTimeout = 30\n```\n\n" +
		"DefaultTimeout is the default lock timeout in seconds.\n\n" +
		"### `CommitError`\n\n```go\ntype CommitError struct\n```\n\n" +
		"CommitError carries a structured exit code alongside the error message.\n\n" +
		"### `Pipeline`\n\n```go\ntype Pipeline struct\n```\n\n" +
		"Pipeline orchestrates the full commit flow.\n\n" +
		"### `Request`\n\n```go\ntype Request struct\n```\n\n" +
		"Request holds all inputs for a single commit operation.\n\n" +
		"### `Result`\n\n```go\ntype Result struct\n```\n\n" +
		"Result is the JSON-serializable output of a successful commit.\n\n" +
		"### `NewPipeline`\n\n```go\nfunc NewPipeline(dir string) *Pipeline\n```\n\n" +
		"NewPipeline creates a new Pipeline with defaults.\n\n" +
		"### `CommitError.Error`\n\n```go\nfunc (e *CommitError) Error() string { return e.Message }\n```\n\n" +
		"Error implements the error interface.\n\n" +
		"### `Pipeline.Execute`\n\n```go\nfunc (p *Pipeline) Execute(ctx context.Context, req Request) (*Result, error)\n```\n\n" +
		"Execute runs the full two-phase commit pipeline. On CAS miss it retries up to MaxAttempts times."
	if got != want {
		t.Fatalf("ref =\n%s\n\nwant\n%s", got, want)
	}
	if strings.Contains(got, "unexportedHelper") {
		t.Error("an unexported function reached the page")
	}
	if strings.Contains(got, "TestNewPipeline") || strings.Contains(got, "TestExecute") {
		t.Error("a test file's declarations reached the page")
	}
}

func TestExactRenderings(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	tests := []struct {
		name      string
		directive string
		attrs     map[string]string
		want      string
	}{
		{
			name:      "ref with a target renders one declaration",
			directive: "ref",
			attrs:     map[string]string{"path": "internal/commit", "target": "NewPipeline"},
			want: "### `NewPipeline`\n\n```go\nfunc NewPipeline(dir string) *Pipeline\n```\n\n" +
				"NewPipeline creates a new Pipeline with defaults.",
		},
		{
			name:      "ref with a dotted target renders one method",
			directive: "ref",
			attrs:     map[string]string{"path": "internal/commit", "target": "Pipeline.Execute"},
			want: "### `Pipeline.Execute`\n\n" +
				"```go\nfunc (p *Pipeline) Execute(ctx context.Context, req Request) (*Result, error)\n```\n\n" +
				"Execute runs the full two-phase commit pipeline. On CAS miss it retries up to MaxAttempts times.",
		},
		{
			name:      "prose-desc renders the package doc alone",
			directive: "prose-desc",
			attrs:     map[string]string{"path": "internal/commit"},
			want:      "Package commit implements the two-phase commit pipeline. Phase A is parallel-safe, Phase B is serialized.",
		},
		{
			name:      "table-schema renders one struct's fields",
			directive: "table-schema",
			attrs:     map[string]string{"path": "internal/models/models.go", "target": "Config"},
			want: "| Field | Type | Tag | Description |\n| --- | --- | --- | --- |\n" +
				"| `Host` | `string` | `json:\"host\" yaml:\"host\"` | Server hostname |\n" +
				"| `Port` | `int` | `json:\"port\"` | Listen port |\n" +
				"| `Debug` | `bool` | `json:\"debug\"` |  |\n" +
				"| `internal` | `string` |  | unexported, should appear but lowercase |",
		},
		{
			name:      "table-schema with no target renders every struct",
			directive: "table-schema",
			attrs:     map[string]string{"path": "internal/models/models.go"},
			want: "### `Config`\n\nConfig holds application configuration.\n\n" +
				"| Field | Type | Tag | Description |\n| --- | --- | --- | --- |\n" +
				"| `Host` | `string` | `json:\"host\" yaml:\"host\"` | Server hostname |\n" +
				"| `Port` | `int` | `json:\"port\"` | Listen port |\n" +
				"| `Debug` | `bool` | `json:\"debug\"` |  |\n" +
				"| `internal` | `string` |  | unexported, should appear but lowercase |\n" +
				"### `Entry`\n\nEntry represents a log entry.\n\n" +
				"| Field | Type | Tag | Description |\n| --- | --- | --- | --- |\n" +
				"| `Level` | `string` | `json:\"level\"` |  |\n" +
				"| `Message` | `string` | `json:\"message\"` | Log message text |",
		},
		{
			name:      "table-schema reads json tags",
			directive: "table-schema",
			attrs:     map[string]string{"path": "internal/commit/types.go", "target": "Result"},
			want: "| Field | Type | Tag | Description |\n| --- | --- | --- | --- |\n" +
				"| `SHA` | `string` | `json:\"sha\"` |  |\n" +
				"| `Ref` | `string` | `json:\"ref\"` |  |\n" +
				"| `Parent` | `string` | `json:\"parent\"` |  |\n" +
				"| `Attempts` | `int` | `json:\"attempts\"` |  |",
		},
		{
			name:      "code-test with no target fences the whole file",
			directive: "code-test",
			attrs:     map[string]string{"path": "internal/commit/commit_test.go"},
			want:      "```go\n" + strings.TrimRight(commitTestGo, "\n") + "\n```",
		},
		{
			name:      "code-test with a target includes the doc comment",
			directive: "code-test",
			attrs:     map[string]string{"path": "internal/commit/commit_test.go", "target": "TestNewPipeline"},
			want: "```go\n// TestNewPipeline verifies default construction.\n" +
				"func TestNewPipeline(t *testing.T) {\n" +
				"\tp := NewPipeline(\"/tmp/test\")\n" +
				"\tif p.SafegitDir != \"/tmp/test\" {\n" +
				"\t\tt.Errorf(\"got %q, want /tmp/test\", p.SafegitDir)\n" +
				"\t}\n}\n```",
		},
		{
			name:      "code-help reads a usage function and stdlib flags",
			directive: "code-help",
			attrs:     map[string]string{"path": "cmd/myapp/main.go"},
			want: "**`usageText()`:**\n\n```\nUsage: myapp <command> [options]\n\n" +
				"Commands:\n  run     Run the processor\n  check   Check configuration\n\n" +
				"Global flags:\n  --verbose   Verbose output\n  --quiet     Suppress output\n```\n\n" +
				"**Flags:**\n\n| Flag | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
				"| `verbose` | `bool` | `false` | Enable verbose output |\n" +
				"| `output` | `string` | `out.txt` | Output file path |",
		},
		{
			name:      "code-help reads strictcli commands",
			directive: "code-help",
			attrs:     map[string]string{"path": "cmd/myapp/commands.go"},
			want: "\n**Commands:**\n\n| Command | Description |\n| --- | --- |\n" +
				"| `run` | Run the processor |\n| `check` | Check configuration |",
		},
		{
			name:      "table-config tabulates a JSON document",
			directive: "table-config",
			attrs:     map[string]string{"path": "config.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `host` | string | `\"localhost\"` |\n| `port` | integer | `3000` |\n" +
				"| `debug` | boolean | `false` |",
		},
		{
			name:      "table-schema on a JSON path drops the target instead of appending it",
			directive: "table-schema",
			attrs:     map[string]string{"path": "config.json", "target": "SomeType"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `host` | string | `\"localhost\"` |\n| `port` | integer | `3000` |\n" +
				"| `debug` | boolean | `false` |",
		},
		{
			name:      "table-schema on a JSON path honors exclude",
			directive: "table-schema",
			attrs:     map[string]string{"path": "config.json", "exclude": "debug"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `host` | string | `\"localhost\"` |\n| `port` | integer | `3000` |",
		},
		{
			name:      "table-config honors exclude on a TOML section",
			directive: "table-config",
			attrs:     map[string]string{"path": "app.toml", "exclude": "logging"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `server.host` | string | `\"localhost\"` |\n| `server.port` | integer | `8080` |",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract(tt.directive, tt.attrs, nil, nil, base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("%s =\n%q\nwant\n%q", tt.directive, got, tt.want)
			}
		})
	}
}

func TestErrorMarkers(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	tests := []struct {
		name      string
		directive string
		attrs     map[string]string
		want      string
	}{
		{"ref with no path", "ref", map[string]string{}, "> *[selfdoc: :::module requires a package path argument]*"},
		{"ref with a missing package", "ref", map[string]string{"path": "nonexistent/pkg"}, "> *[selfdoc: package 'nonexistent/pkg' not found]*"},
		{"ref with a missing target", "ref", map[string]string{"path": "internal/commit", "target": "NonExistent"}, "> *[selfdoc: symbol 'NonExistent' not found in 'internal/commit']*"},
		{"ref on a test-only package", "ref", map[string]string{"path": "testsonly"}, "> *[selfdoc: no .go files in 'testsonly']*"},
		{"code-test with no path", "code-test", map[string]string{}, "> *[selfdoc: :::test requires a file path argument]*"},
		{"code-test with a missing file", "code-test", map[string]string{"path": "nonexistent_test.go"}, "> *[selfdoc: test file 'nonexistent_test.go' not found]*"},
		{"code-test with a missing target", "code-test", map[string]string{"path": "internal/commit/commit_test.go", "target": "NonExistentTest"}, "> *[selfdoc: 'NonExistentTest' not found in 'internal/commit/commit_test.go']*"},
		{"table-schema with no path", "table-schema", map[string]string{}, "> *[selfdoc: :::schema requires a file path argument]*"},
		{"table-schema with a missing file", "table-schema", map[string]string{"path": "nonexistent.go", "target": "SomeType"}, "> *[selfdoc: file 'nonexistent.go' not found]*"},
		{"table-schema with a missing struct", "table-schema", map[string]string{"path": "internal/models/models.go", "target": "NonExistent"}, "> *[selfdoc: struct 'NonExistent' not found in 'internal/models/models.go']*"},
		{"table-schema with an exclude key the document lacks", "table-schema", map[string]string{"path": "config.json", "exclude": "nonexistent"}, "> *[selfdoc: exclude key 'nonexistent' not found in 'config.json']*"},
		{"code-help with no path", "code-help", map[string]string{}, "> *[selfdoc: :::cli requires a file path argument]*"},
		{"code-help with a missing file", "code-help", map[string]string{"path": "nonexistent.go"}, "> *[selfdoc: file 'nonexistent.go' not found]*"},
		{"table-config with no path", "table-config", map[string]string{}, "> *[selfdoc: table-config requires a file path argument]*"},
		{"table-config with a missing file", "table-config", map[string]string{"path": "missing.json"}, "> *[selfdoc: config file 'missing.json' not found]*"},
		{"prose-desc with no path", "prose-desc", map[string]string{}, "> *[selfdoc: :::prose-desc requires a package path argument]*"},
		{"prose-desc with no package doc", "prose-desc", map[string]string{"path": "internal/models"}, "> *[selfdoc: no package doc comment found in 'internal/models']*"},
		{"an unknown directive", "unknown", map[string]string{"path": "arg"}, "> *[selfdoc: unknown directive 'unknown' for go extractor]*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract(tt.directive, tt.attrs, nil, nil, base)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.directive, got, tt.want)
			}
		})
	}
}

func TestNameDetectAndExtensions(t *testing.T) {
	hygiene.Isolate(t)
	extractor := newExtractor()

	if got := extractor.Name(); got != "go" {
		t.Errorf("Name = %q, want go", got)
	}
	if got := extractor.FileExtensions(); !reflect.DeepEqual(got, []string{".go"}) {
		t.Errorf("FileExtensions = %#v", got)
	}
	if extractor.Detect(t.TempDir()) {
		t.Error("an empty directory was detected as a Go project")
	}
	withGoMod := t.TempDir()
	if err := os.WriteFile(filepath.Join(withGoMod, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !extractor.Detect(withGoMod) {
		t.Error("go.mod was not detected")
	}
}

// TestResolvePathReturnsADirectory pins the property that sets the Go extractor
// apart: its unit of documentation is the package, so it resolves to a
// directory where the others resolve to a file.
func TestResolvePathReturnsADirectory(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	got := extractor.ResolvePath("internal/commit", nil, base)
	if !strings.HasSuffix(got, "commit") {
		t.Fatalf("ResolvePath = %q, want a path ending in commit", got)
	}
	if !extractors.IsDir(got) {
		t.Fatalf("ResolvePath = %q, which is not a directory", got)
	}
	if got := extractor.ResolvePath("nope", nil, base); got != "" {
		t.Fatalf("ResolvePath = %q, want empty", got)
	}
	// A directory with no .go file in it does not resolve.
	empty := filepath.Join(base, "emptydir")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := extractor.ResolvePath("emptydir", nil, base); got != "" {
		t.Fatalf("ResolvePath(emptydir) = %q, want empty", got)
	}
}

func TestModuleDocstring(t *testing.T) {
	hygiene.Isolate(t)
	base := fixture(t)
	extractor := newExtractor()

	t.Run("from doc.go in a package directory", func(t *testing.T) {
		got, err := extractor.ModuleDocstring(filepath.Join(base, "mypkg"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "Package mypkg provides utilities."; got != want {
			t.Fatalf("ModuleDocstring = %q, want %q", got, want)
		}
	})

	t.Run("wrapped prose is joined into one sentence", func(t *testing.T) {
		got, err := extractor.ModuleDocstring(filepath.Join(base, "internal/commit"))
		if err != nil {
			t.Fatal(err)
		}
		want := "Package commit implements the two-phase commit pipeline. " +
			"Phase A is parallel-safe, Phase B is serialized."
		if got != want {
			t.Fatalf("ModuleDocstring = %q, want %q", got, want)
		}
	})

	t.Run("a single file reads its own directory", func(t *testing.T) {
		got, err := extractor.ModuleDocstring(filepath.Join(base, "mypkg/main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "Package mypkg provides utilities."; got != want {
			t.Fatalf("ModuleDocstring = %q, want %q", got, want)
		}
	})

	t.Run("a path that is neither a file nor a directory", func(t *testing.T) {
		got, err := extractor.ModuleDocstring(filepath.Join(base, "nope"))
		if err != nil || got != "" {
			t.Fatalf("ModuleDocstring = (%q, %v), want empty", got, err)
		}
	})

	// A comment separated from the package clause by a blank line is not a
	// doc comment -- go doc ignores it -- and the commonest such comment is
	// the generator's "DO NOT EDIT" line, which describes the tool rather
	// than the package.
	t.Run("a generated-file banner is not the package documentation", func(t *testing.T) {
		dir := t.TempDir()
		writeInto(t, dir, "generated.go",
			"// Code generated by stringer. DO NOT EDIT.\n"+
				"\n"+
				"package widget\n"+
				"\n"+
				"func Generated() {}\n")

		got, err := extractor.ModuleDocstring(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Fatalf("ModuleDocstring = %q, want empty", got)
		}
	})

	t.Run("the adjacent comment wins over a generated banner", func(t *testing.T) {
		dir := t.TempDir()
		writeInto(t, dir, "aaa_generated.go",
			"// Code generated by stringer. DO NOT EDIT.\n"+
				"\n"+
				"package widget\n")
		writeInto(t, dir, "doc.go",
			"// Package widget draws the widgets.\n"+
				"package widget\n")

		got, err := extractor.ModuleDocstring(dir)
		if err != nil {
			t.Fatal(err)
		}
		if want := "Package widget draws the widgets."; got != want {
			t.Fatalf("ModuleDocstring = %q, want %q", got, want)
		}
	})
}

// TestEmbeddedCodeBlockIsLeftVerbatim pins that an indented preformatted block
// inside a Go doc comment is not collapsed into the prose around it.
func TestEmbeddedCodeBlockIsLeftVerbatim(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	source := "// Package meta collects rich metadata about each deletion: environment\n" +
		"// variables, git repository context, parent process information, and\n" +
		"// arbitrary user-supplied key-value pairs.\n" +
		"//\n" +
		"// Example usage:\n" +
		"//\n" +
		"//\tm := meta.Collect()\n" +
		"//\tfmt.Println(m.GitBranch)\n" +
		"package meta\n" +
		"\n" +
		"func Collect() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "meta.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := newExtractor().ModuleDocstring(filepath.Join(dir, "meta.go"))
	if err != nil {
		t.Fatal(err)
	}

	wantSentence := "Package meta collects rich metadata about each deletion: environment " +
		"variables, git repository context, parent process information, and " +
		"arbitrary user-supplied key-value pairs."
	if !strings.HasPrefix(doc, wantSentence) {
		t.Errorf("the wrapped first sentence was not joined:\n%q", doc)
	}
	if !strings.Contains(doc, "\tm := meta.Collect()") ||
		!strings.Contains(doc, "\tfmt.Println(m.GitBranch)") {
		t.Errorf("the indented example block was not kept verbatim:\n%q", doc)
	}
	if strings.Contains(doc, "m := meta.Collect() fmt.Println") {
		t.Error("the indented example block was collapsed into the prose")
	}
}
