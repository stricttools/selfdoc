package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// newExtractor builds the extractor under test.
func newExtractor() extractors.Extractor {
	return New()
}

// writeTree writes files into dir, creating the directories each one needs.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// coreTS is the reference module every ref and schema test reads: a
// module-level JSDoc block and one of each exported declaration form.
const coreTS = `/**
 * Core module for the widget library.
 *
 * Provides essential utilities for widget management.
 */

import type { Widget } from './types.js';

/**
 * Create a new widget with the given name and options.
 *
 * @param name - The widget name
 * @param options - Configuration options
 * @returns The created widget instance
 */
export function createWidget(name: string, options?: WidgetOptions): Widget {
  return { name, ...options };
}

/**
 * A processor that transforms widgets in bulk.
 */
export class WidgetProcessor {
  process(items: Widget[]): Widget[] {
    return items;
  }
}

/**
 * Configuration options for widget creation.
 */
export interface WidgetOptions {
  /** Widget width in pixels */
  width: number;
  /** Widget height in pixels */
  height: number;
  /** Whether the widget is visible */
  visible?: boolean;
  /** CSS class name for styling */
  className: string;
}

/**
 * Unique identifier for widgets.
 */
export type WidgetId = string | number;

export const DEFAULT_SIZE = 100;
`

// utilsJS is the reference JavaScript module: the same shape as coreTS, in a
// file whose extension selects the javascript code fence.
const utilsJS = `/**
 * Utility functions for data processing.
 *
 * Handles parsing, formatting, and validation.
 */

const path = require('path');

/**
 * Parse a configuration string into an object.
 *
 * @param raw - The raw config string
 * @param strict - Whether to enforce strict parsing
 * @returns Parsed configuration object
 */
export function parseConfig(raw, strict = false) {
  return JSON.parse(raw);
}
`

// widgetTestTS is the reference test file: two describe blocks, one of which
// nests two it blocks and the other a test block.
const widgetTestTS = `import { createWidget } from '../src/core';

describe("createWidget", () => {
  it("should create a widget with name", () => {
    const widget = createWidget("test");
    expect(widget.name).toBe("test");
  });

  it("should accept options", () => {
    const widget = createWidget("test", { width: 100 });
    expect(widget.width).toBe(100);
  });
});

describe("WidgetProcessor", () => {
  test("processes empty array", () => {
    const processor = new WidgetProcessor();
    expect(processor.process([])).toEqual([]);
  });
});
`

// TestExtractRendersReferenceOutput pins the Markdown every directive emits,
// byte for byte, against the output the Python extractor this ports produces
// for the same input.
func TestExtractRendersReferenceOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		directive   string
		attrs       map[string]string
		sourcePaths []string
		want        string
	}{
		{
			name:        "ref renders the module doc and every export",
			files:       map[string]string{"src/core.ts": coreTS},
			directive:   "ref",
			attrs:       map[string]string{"path": "core.ts"},
			sourcePaths: []string{"src/"},
			want: "## `core`\n\nCore module for the widget library.\n\nProvides essential utilities for widget management." +
				"\n\n### `createWidget`\n\n```typescript\nexport function createWidget(name: string, options?: WidgetOptions): Widget\n```" +
				"\n\nCreate a new widget with the given name and options.\n\n**Parameters:**\n- `name` -- - The widget name\n- `options` -- - Configuration options" +
				"\n\n**Returns:** The created widget instance" +
				"\n\n### `WidgetProcessor`\n\n```typescript\nexport class WidgetProcessor\n```\n\nA processor that transforms widgets in bulk." +
				"\n\n### `WidgetOptions`\n\n```typescript\nexport interface WidgetOptions\n```\n\nConfiguration options for widget creation." +
				"\n\n### `WidgetId`\n\n```typescript\nexport type WidgetId = string | number\n```\n\nUnique identifier for widgets." +
				"\n\n### `DEFAULT_SIZE`\n\n```typescript\nexport const DEFAULT_SIZE = 100\n```",
		},
		{
			name: "ref with a target renders one export, with its JSDoc verbatim",
			files: map[string]string{"mod.ts": "/** Adds two numbers. */\n" +
				"export function add(a: number, b: number): number { return a + b; }\n\n" +
				"/** Subtracts two numbers. */\n" +
				"export function subtract(a: number, b: number): number { return a - b; }\n"},
			directive: "ref",
			attrs:     map[string]string{"path": "mod.ts", "target": "subtract"},
			want:      "### `subtract`\n\n```typescript\nexport function subtract(a: number, b: number): number\n```\n\n Subtracts two numbers. ",
		},
		{
			name:        "ref on a JavaScript file declares the javascript fence",
			files:       map[string]string{"lib/utils.js": utilsJS},
			directive:   "ref",
			attrs:       map[string]string{"path": "utils.js"},
			sourcePaths: []string{"lib/"},
			want: "## `utils`\n\nUtility functions for data processing.\n\nHandles parsing, formatting, and validation." +
				"\n\n### `parseConfig`\n\n```javascript\nexport function parseConfig(raw, strict = false)\n```" +
				"\n\nParse a configuration string into an object.\n\n**Parameters:**\n- `raw` -- - The raw config string\n- `strict` -- - Whether to enforce strict parsing" +
				"\n\n**Returns:** Parsed configuration object",
		},
		{
			name:      "ref renders a default-exported function",
			files:     map[string]string{"src/main.ts": "/**\n * Main entry point.\n *\n * @param args - CLI arguments\n */\nexport default function main(args: string[]): void {\n  console.log(args);\n}\n"},
			directive: "ref", attrs: map[string]string{"path": "main.ts"}, sourcePaths: []string{"src/"},
			want: "## `main`\n\n### `main`\n\n```typescript\nexport default function main(args: string[]): void\n```" +
				"\n\nMain entry point.\n\n**Parameters:**\n- `args` -- - CLI arguments",
		},
		{
			name:      "ref renders a re-export as the statement that declares it",
			files:     map[string]string{"src/index.ts": "export { Foo } from './foo';\nexport { Bar } from './bar';\n"},
			directive: "ref", attrs: map[string]string{"path": "index.ts"}, sourcePaths: []string{"src/"},
			want: "## `index`\n\n### `Foo`\n\n```typescript\nexport { Foo } from './foo'\n```" +
				"\n\n### `Bar`\n\n```typescript\nexport { Bar } from './bar'\n```",
		},
		{
			name:      "ref renders a re-export from its local declaration",
			files:     map[string]string{"src/mod.ts": "/**\n * A widget class.\n */\nclass Foo {\n  run() {}\n}\n\nexport { Foo }\n"},
			directive: "ref", attrs: map[string]string{"path": "mod.ts"}, sourcePaths: []string{"src/"},
			want: "## `mod`\n\nA widget class.\n\n### `Foo`\n\n```typescript\nclass Foo\n```\n\nA widget class.",
		},
		{
			name:      "ref names an aliased re-export by its alias",
			files:     map[string]string{"src/aliases.ts": "export { Foo as Bar } from './foo';\n"},
			directive: "ref", attrs: map[string]string{"path": "aliases.ts"}, sourcePaths: []string{"src/"},
			want: "## `aliases`\n\n### `Bar`\n\n```typescript\nexport { Foo as Bar } from './foo'\n```",
		},
		{
			name:      "code-test renders one describe block with everything nested in it",
			files:     map[string]string{"tests/widget.test.ts": widgetTestTS},
			directive: "code-test", attrs: map[string]string{"path": "tests/widget.test.ts", "target": "createWidget"},
			sourcePaths: []string{"src/"},
			want:        "```typescript\ndescribe(\"createWidget\", () => {\n  it(\"should create a widget with name\", () => {\n    const widget = createWidget(\"test\");\n    expect(widget.name).toBe(\"test\");\n  });\n\n  it(\"should accept options\", () => {\n    const widget = createWidget(\"test\", { width: 100 });\n    expect(widget.width).toBe(100);\n  });\n});\n```",
		},
		{
			name:      "code-test renders a nested test block on its own",
			files:     map[string]string{"tests/widget.test.ts": widgetTestTS},
			directive: "code-test", attrs: map[string]string{"path": "tests/widget.test.ts", "target": "processes empty array"},
			sourcePaths: []string{"src/"},
			want:        "```typescript\ntest(\"processes empty array\", () => {\n    const processor = new WidgetProcessor();\n    expect(processor.process([])).toEqual([]);\n  });\n```",
		},
		{
			name:      "table-schema renders an interface's fields",
			files:     map[string]string{"src/core.ts": coreTS},
			directive: "table-schema", attrs: map[string]string{"path": "core.ts", "target": "WidgetOptions"},
			sourcePaths: []string{"src/"},
			want: "| Field | Type | Description |\n| --- | --- | --- |\n" +
				"| `width` | `number` | Widget width in pixels |\n" +
				"| `height` | `number` | Widget height in pixels |\n" +
				"| `visible` | `boolean (optional)` | Whether the widget is visible |\n" +
				"| `className` | `string` | CSS class name for styling |",
		},
		{
			name:      "table-schema renders an object type alias's fields",
			files:     map[string]string{"src/types.ts": "/** Point in 2D space. */\nexport type Point = {\n  /** X coordinate */\n  x: number;\n  /** Y coordinate */\n  y: number;\n};\n"},
			directive: "table-schema", attrs: map[string]string{"path": "types.ts", "target": "Point"},
			sourcePaths: []string{"src/"},
			want: "| Field | Type | Description |\n| --- | --- | --- |\n" +
				"| `x` | `number` | X coordinate |\n| `y` | `number` | Y coordinate |",
		},
		{
			// The inline comment keeps its "//" marker, which is what the
			// Python emits: the description is the comment text as written.
			name:      "table-schema takes a field's description from its inline comment",
			files:     map[string]string{"src/config.ts": "export interface AppConfig {\n  host: string; // Server hostname\n  port: number; // Server port\n  debug: boolean; // Enable debug mode\n}\n"},
			directive: "table-schema", attrs: map[string]string{"path": "config.ts", "target": "AppConfig"},
			sourcePaths: []string{"src/"},
			want: "| Field | Type | Description |\n| --- | --- | --- |\n" +
				"| `host` | `string` | // Server hostname |\n" +
				"| `port` | `number` | // Server port |\n" +
				"| `debug` | `boolean` | // Enable debug mode |",
		},
		{
			name:      "table-schema renders a JSON document as a key table",
			files:     map[string]string{"schema.json": `{"name": "widgets", "version": "1.0.0", "debug": true}`},
			directive: "table-schema", attrs: map[string]string{"path": "schema.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `name` | string | `\"widgets\"` |\n| `version` | string | `\"1.0.0\"` |\n| `debug` | boolean | `true` |",
		},
		{
			name:      "table-schema drops the excluded keys",
			files:     map[string]string{"schema.json": `{"name": "widgets", "version": "1.0.0", "count": 5}`},
			directive: "table-schema", attrs: map[string]string{"path": "schema.json", "exclude": "version, count"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n| `name` | string | `\"widgets\"` |",
		},
		{
			name:      "table-schema refuses an exclude key the document does not carry",
			files:     map[string]string{"schema.json": `{"name": "widgets", "version": "1.0.0"}`},
			directive: "table-schema", attrs: map[string]string{"path": "schema.json", "exclude": "nonexistent"},
			want: "> *[selfdoc: exclude key 'nonexistent' not found in 'schema.json']*",
		},
		{
			name:      "code-help renders the module doc and every help constant",
			files:     map[string]string{"src/cli.ts": "/**\n * CLI entry point for the widget tool.\n */\n\nconst USAGE = `Usage: widget [options] <command>\n\nCommands:\n    create    Create a new widget\n    list      List all widgets\n`;\n"},
			directive: "code-help", attrs: map[string]string{"path": "cli.ts"}, sourcePaths: []string{"src/"},
			want: "CLI entry point for the widget tool.\n\n```\nUsage: widget [options] <command>\n\nCommands:\n    create    Create a new widget\n    list      List all widgets\n```",
		},
		{
			name:      "code-help falls back to the module doc alone",
			files:     map[string]string{"lib/utils.js": "/**\n * Utility functions for data processing.\n *\n * Handles parsing, formatting, and validation.\n */\n\nconst path = require('path');\n"},
			directive: "code-help", attrs: map[string]string{"path": "utils.js"}, sourcePaths: []string{"lib/"},
			want: "Utility functions for data processing.\n\nHandles parsing, formatting, and validation.",
		},
		{
			name:      "table-config renders a JSON file",
			files:     map[string]string{"config.json": `{"host": "localhost", "port": 3000, "ssl": false}`},
			directive: "table-config", attrs: map[string]string{"path": "config.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `host` | string | `\"localhost\"` |\n| `port` | integer | `3000` |\n| `ssl` | boolean | `false` |",
		},
		{
			name:      "table-config strips JSONC comments before parsing",
			files:     map[string]string{"tsconfig.jsonc": "{\n  // Compiler options\n  \"target\": \"ES2020\",\n  \"module\": \"commonjs\",\n  /* Multi-line\n     comment */\n  \"strict\": true\n}\n"},
			directive: "table-config", attrs: map[string]string{"path": "tsconfig.jsonc"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `target` | string | `\"ES2020\"` |\n| `module` | string | `\"commonjs\"` |\n| `strict` | boolean | `true` |",
		},
		{
			name:      "table-config drops an excluded key from a JSONC file",
			files:     map[string]string{"settings.jsonc": "{\n  // Editor settings\n  \"theme\": \"dark\",\n  \"fontSize\": 14,\n  \"autoSave\": true\n}\n"},
			directive: "table-config", attrs: map[string]string{"path": "settings.jsonc", "exclude": "fontSize"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `theme` | string | `\"dark\"` |\n| `autoSave` | boolean | `true` |",
		},
		{
			name:      "table-config flattens a TOML file and drops an excluded table",
			files:     map[string]string{"config.toml": "[server]\nhost = \"localhost\"\nport = 3000\n\n[logging]\nlevel = \"info\"\n"},
			directive: "table-config", attrs: map[string]string{"path": "config.toml", "exclude": "logging"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `server.host` | string | `\"localhost\"` |\n| `server.port` | integer | `3000` |",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, test.files)
			got, err := newExtractor().Extract(
				test.directive, test.attrs, nil, test.sourcePaths, dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("Extract() =\n%q\nwant\n%q", got, test.want)
			}
		})
	}
}

// TestExtractErrorMarkers pins the marker a directive leaves in place of
// content it could not resolve. Every one of them degrades one region of one
// page rather than failing the build, so the message is what a reader sees.
func TestExtractErrorMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string
		directive string
		attrs     map[string]string
		want      string
	}{
		{
			name: "ref with no path", directive: "ref", attrs: map[string]string{},
			want: "> *[selfdoc: :::module requires a file path argument]*",
		},
		{
			name: "ref on a missing file", directive: "ref", attrs: map[string]string{"path": "nonexistent.ts"},
			want: "> *[selfdoc: module 'nonexistent.ts' not found]*",
		},
		{
			name:      "ref with an unknown target",
			files:     map[string]string{"mod.ts": "export function foo(): void {}\n"},
			directive: "ref", attrs: map[string]string{"path": "mod.ts", "target": "nonexistent"},
			want: "> *[selfdoc: symbol 'nonexistent' not found in 'mod.ts']*",
		},
		{
			name: "code-test with no path", directive: "code-test", attrs: map[string]string{},
			want: "> *[selfdoc: :::test requires a file path argument]*",
		},
		{
			name: "code-test on a missing file", directive: "code-test",
			attrs: map[string]string{"path": "tests/nonexistent.test.ts"},
			want:  "> *[selfdoc: test file 'tests/nonexistent.test.ts' not found]*",
		},
		{
			name:      "code-test with an unknown block",
			files:     map[string]string{"tests/widget.test.ts": widgetTestTS},
			directive: "code-test",
			attrs:     map[string]string{"path": "tests/widget.test.ts", "target": "nonexistentTest"},
			want:      "> *[selfdoc: 'nonexistentTest' not found in 'tests/widget.test.ts']*",
		},
		{
			name: "table-schema with no path", directive: "table-schema", attrs: map[string]string{},
			want: "> *[selfdoc: :::schema requires an argument]*",
		},
		{
			name:      "table-schema with an unknown type",
			files:     map[string]string{"src/core.ts": coreTS},
			directive: "table-schema", attrs: map[string]string{"path": "src/core.ts", "target": "NoSuchType"},
			want: "> *[selfdoc: type 'NoSuchType' not found in 'src/core.ts']*",
		},
		{
			name:      "table-schema with no target on a file declaring no types",
			files:     map[string]string{"bare.ts": "export const x = 1;\n"},
			directive: "table-schema", attrs: map[string]string{"path": "bare.ts"},
			want: "> *[selfdoc: no interfaces or types found in 'bare.ts']*",
		},
		{
			name: "table-schema on a missing JSON file", directive: "table-schema",
			attrs: map[string]string{"path": "missing.json"},
			want:  "> *[selfdoc: JSON file 'missing.json' not found]*",
		},
		{
			name: "code-help with no path", directive: "code-help", attrs: map[string]string{},
			want: "> *[selfdoc: :::cli requires a file path argument]*",
		},
		{
			name: "code-help on a missing file", directive: "code-help",
			attrs: map[string]string{"path": "nonexistent.ts"},
			want:  "> *[selfdoc: module 'nonexistent.ts' not found]*",
		},
		{
			name:      "code-help on a file with no documentation",
			files:     map[string]string{"bare.ts": "export const x = 1;\n"},
			directive: "code-help", attrs: map[string]string{"path": "bare.ts"},
			want: "> *[selfdoc: no CLI documentation found in 'bare.ts']*",
		},
		{
			name: "table-config on a missing file", directive: "table-config",
			attrs: map[string]string{"path": "missing.json"},
			want:  "> *[selfdoc: config file 'missing.json' not found]*",
		},
		{
			name: "table-config on a missing JSONC file", directive: "table-config",
			attrs: map[string]string{"path": "missing.jsonc"},
			want:  "> *[selfdoc: config file 'missing.jsonc' not found]*",
		},
		{
			name: "prose-desc with no path", directive: "prose-desc", attrs: map[string]string{},
			want: "> *[selfdoc: :::prose-desc requires a file path argument]*",
		},
		{
			name:      "prose-desc on a file with no module JSDoc",
			files:     map[string]string{"bare.ts": "export const x = 1;\n"},
			directive: "prose-desc", attrs: map[string]string{"path": "bare.ts"},
			want: "> *[selfdoc: no module-level JSDoc found in 'bare.ts']*",
		},
		{
			name: "an unknown directive names the language", directive: "unknown",
			attrs: map[string]string{"path": "arg"},
			want:  "> *[selfdoc: unknown directive 'unknown' for typescript extractor]*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, test.files)
			got, err := newExtractor().Extract(test.directive, test.attrs, nil, nil, dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("Extract() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestProseDescRendersTheModuleDoc pins the directive that emits a module's
// own documentation without the declaration list.
func TestProseDescRendersTheModuleDoc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"src/core.ts": coreTS})

	got, err := newExtractor().Extract(
		"prose-desc", map[string]string{"path": "core.ts"}, nil, []string{"src/"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "Core module for the widget library.\n\nProvides essential utilities for widget management."
	if got != want {
		t.Errorf("prose-desc = %q, want %q", got, want)
	}
}

// TestCodeTestRendersTheWholeFile pins the no-target form, which renders the
// file rather than looking for a block with no name.
func TestCodeTestRendersTheWholeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"tests/widget.test.ts": widgetTestTS})

	got, err := newExtractor().Extract(
		"code-test", map[string]string{"path": "tests/widget.test.ts"}, nil, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "```typescript\n" + strings.TrimRight(widgetTestTS, "\n") + "\n```"
	if got != want {
		t.Errorf("code-test = %q, want %q", got, want)
	}
}

// TestCodeTestOnJavaScriptDeclaresTheJavascriptFence pins the fence language,
// which follows the file's extension rather than the directive.
func TestCodeTestOnJavaScriptDeclaresTheJavascriptFence(t *testing.T) {
	t.Parallel()

	source := "describe(\"formatNumber\", () => {\n  it(\"formats integers\", () => {\n    expect(formatNumber(1000)).toBe(\"1,000\");\n  });\n});\n"
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"tests/util.test.js": source})

	got, err := newExtractor().Extract(
		"code-test", map[string]string{"path": "tests/util.test.js"}, nil, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "```javascript\n") {
		t.Errorf("code-test = %q, want a javascript fence", got)
	}
}

// TestDetect pins language detection, which is by tsconfig.json and nothing
// else.
func TestDetect(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	extractor := newExtractor()
	if extractor.Detect(dir) {
		t.Error("Detect() on an empty directory = true")
	}
	writeTree(t, dir, map[string]string{"tsconfig.json": "{}\n"})
	if !extractor.Detect(dir) {
		t.Error("Detect() with a tsconfig.json = false")
	}
}

// TestFileExtensions pins the list the build walks a source tree with, which
// is deliberately shorter than the list path resolution recognizes.
func TestFileExtensions(t *testing.T) {
	t.Parallel()

	got := newExtractor().FileExtensions()
	want := []string{".ts", ".tsx", ".js", ".jsx"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FileExtensions() = %v, want %v", got, want)
	}
}

// TestResolvePath pins path resolution: the path as written, under a declared
// source path, with an extension appended, and as a directory with an index
// file.
func TestResolvePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"src/core.ts":      "export const a = 1;\n",
		"src/ui/index.tsx": "export const b = 2;\n",
		"src/legacy.js":    "export const c = 3;\n",
	})
	extractor := newExtractor()

	tests := []struct {
		name        string
		arg         string
		sourcePaths []string
		wantSuffix  string
	}{
		{name: "the path as written", arg: "src/core.ts", wantSuffix: "src/core.ts"},
		{name: "under a source path", arg: "core.ts", sourcePaths: []string{"src/"}, wantSuffix: "src/core.ts"},
		{name: "with the extension appended", arg: "core", sourcePaths: []string{"src/"}, wantSuffix: "src/core.ts"},
		{name: "a directory's index file", arg: "ui", sourcePaths: []string{"src/"}, wantSuffix: "src/ui/index.tsx"},
		{name: "a JavaScript file", arg: "legacy", sourcePaths: []string{"src/"}, wantSuffix: "src/legacy.js"},
		{name: "nothing resolves", arg: "nonexistent", sourcePaths: []string{"src/"}, wantSuffix: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractor.ResolvePath(test.arg, test.sourcePaths, dir)
			if test.wantSuffix == "" {
				if got != "" {
					t.Errorf("ResolvePath(%q) = %q, want the empty string", test.arg, got)
				}
				return
			}
			if !strings.HasSuffix(got, test.wantSuffix) {
				t.Errorf("ResolvePath(%q) = %q, want a path ending in %q", test.arg, got, test.wantSuffix)
			}
		})
	}
}

// TestPublicSymbols pins which names a file is reported to export, and their
// order, which is source order.
func TestPublicSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "one of each declaration form",
			source: "export function createWidget(): void {}\n" +
				"function internal(): void {}\n" +
				"export class Processor {}\n" +
				"export const MAX_SIZE = 100;\n" +
				"export interface Options {}\n" +
				"export type Id = string;\n" +
				"export default function main(): void {}\n",
			want: []string{"createWidget", "Processor", "MAX_SIZE", "Options", "Id", "main"},
		},
		{
			name:   "a re-export list, by exported name",
			source: "export { Foo, Bar as Baz } from './module';\n",
			want:   []string{"Foo", "Baz"},
		},
		{
			name:   "the module's own reference fixture",
			source: coreTS,
			want:   []string{"createWidget", "WidgetProcessor", "WidgetOptions", "WidgetId", "DEFAULT_SIZE"},
		},
		{
			name: "a commented-out export is not an export",
			source: "// export function commented(): void {}\n" +
				"/* export class Blocked {} */\n" +
				"/*\nexport class Spanning {}\n*/\n" +
				"export const real = 1;\n",
			want: []string{"real"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"mod.ts": test.source})
			got, err := newExtractor().PublicSymbols(filepath.Join(dir, "mod.ts"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("PublicSymbols() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestPublicSymbolsOnAMissingFile pins that an unreadable file reports no
// symbols rather than an error: that is a property of the file, not a broken
// toolchain.
func TestPublicSymbolsOnAMissingFile(t *testing.T) {
	t.Parallel()

	got, err := newExtractor().PublicSymbols(filepath.Join(t.TempDir(), "nonexistent.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("PublicSymbols() = %v, want none", got)
	}
}

// TestModuleDocstring pins the module-doc heuristic: which first block comment
// in a file documents the module rather than the declaration below it.
func TestModuleDocstring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a block above an import",
			source: "/**\n * Module description here.\n */\nimport { foo } from \"bar\";\n",
			want:   "Module description here.",
		},
		{
			name: "a block carrying @param documents the function",
			source: "/**\n * Process the data.\n *\n * @param data - The input data\n * @returns The result\n */\n" +
				"export function process(data: string): string {\n  return data;\n}\n",
			want: "",
		},
		{
			name:   "a blank line separates the block from the export",
			source: "/**\n * This is the module description.\n */\n\nexport function doStuff(): void {}\n",
			want:   "This is the module description.",
		},
		{
			name:   "a block touching the export documents the export",
			source: "/**\n * A widget class.\n */\nexport class Widget {}\n",
			want:   "",
		},
		{
			name:   "an @module tag claims the module whatever follows",
			source: "/**\n * The utilities module.\n *\n * @module utils\n */\nexport function helper(): void {}\n",
			want:   "The utilities module.",
		},
		{
			name:   "a file with no block comment",
			source: "export const x = 1;\n",
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"mod.ts": test.source})
			got, err := newExtractor().ModuleDocstring(filepath.Join(dir, "mod.ts"))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("ModuleDocstring() = %q, want %q", got, test.want)
			}
		})
	}
}

// funcsTS is the reference file for symbol details: documented and
// undocumented functions, a non-exported one, defaults, and a rest parameter.
const funcsTS = `/**
 * Create a widget.
 *
 * @param name - The widget name
 * @param options - Configuration options
 * @returns The created widget
 */
export function createWidget(name: string, options?: WidgetOptions): Widget {
  return { name };
}

export function noDoc(x: number, y: number): boolean {
  return x > y;
}

function internalHelper(data: string): void {
  console.log(data);
}

export async function fetchData(url: string, retries: number = 3): Promise<Response> {
  return fetch(url);
}

export function withRest(first: string, ...rest: any[]): void {
  console.log(first, ...rest);
}
`

// routerTS is the reference file for a dotted symbol: a class with a
// documented public method and an undocumented private one.
const routerTS = `/**
 * HTTP router for handling requests.
 */
export class Router {
  /**
   * Handle an incoming request.
   *
   * @param req - The incoming request
   * @returns The response
   */
  handle(req: Request): Response {
    return new Response();
  }

  private log(msg: string): void {
    console.log(msg);
  }
}
`

// TestSymbolDetails pins what the quality measurement reads out of a
// declaration: its parameters with their types, whether the documentation
// covers each, and the declared return type.
func TestSymbolDetails(t *testing.T) {
	t.Parallel()

	str := func(s string) *string { return &s }

	tests := []struct {
		name   string
		source string
		symbol string
		want   *extractors.SymbolDetails
	}{
		{
			name: "a documented function", source: funcsTS, symbol: "createWidget",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "name", Type: str("string"), Documented: true},
					{Name: "options", Type: str("WidgetOptions"), Documented: true},
				},
				ReturnType: str("Widget"), ReturnDocumented: true,
			},
		},
		{
			name: "an undocumented function", source: funcsTS, symbol: "noDoc",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "x", Type: str("number")},
					{Name: "y", Type: str("number")},
				},
				ReturnType: str("boolean"),
			},
		},
		{
			name:   "a generic return type, and a default value dropped from a parameter",
			source: funcsTS, symbol: "fetchData",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "url", Type: str("string")},
					{Name: "retries", Type: str("number")},
				},
				ReturnType: str("Promise<Response>"),
			},
		},
		{
			name: "a rest parameter keeps its prefix", source: funcsTS, symbol: "withRest",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "first", Type: str("string")},
					{Name: "...rest", Type: str("any[]")},
				},
				ReturnType: str("void"),
			},
		},
		{
			name: "a non-exported function is still found", source: funcsTS, symbol: "internalHelper",
			want: &extractors.SymbolDetails{
				Params:     []extractors.SymbolParam{{Name: "data", Type: str("string")}},
				ReturnType: str("void"),
			},
		},
		{
			name: "a symbol the file does not declare", source: funcsTS, symbol: "nonExistent",
			want: nil,
		},
		{
			name: "a dotted name resolves a class method", source: routerTS, symbol: "Router.handle",
			want: &extractors.SymbolDetails{
				Params:     []extractors.SymbolParam{{Name: "req", Type: str("Request"), Documented: true}},
				ReturnType: str("Response"), ReturnDocumented: true,
			},
		},
		{
			name:   "a dotted name whose member is not there",
			source: "export class Service {\n  start(): void {}\n}\n",
			symbol: "Service.stop", want: nil,
		},
		{
			name:   "a dotted name whose type is not there",
			source: "export class Service {\n  start(): void {}\n}\n",
			symbol: "NoSuch.start", want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"mod.ts": test.source})
			got, err := newExtractor().SymbolDetails(filepath.Join(dir, "mod.ts"), test.symbol)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("SymbolDetails(%q) = %s, want %s",
					test.symbol, formatDetails(got), formatDetails(test.want))
			}
		})
	}
}

// formatDetails renders symbol details for a failure message, dereferencing
// the pointers a %#v would print as addresses.
func formatDetails(details *extractors.SymbolDetails) string {
	if details == nil {
		return "nil"
	}
	var b strings.Builder
	b.WriteString("params[")
	for i, p := range details.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name + ":" + deref(p.Type))
		if p.Documented {
			b.WriteString(" (documented)")
		}
	}
	b.WriteString("] return=" + deref(details.ReturnType))
	if details.ReturnDocumented {
		b.WriteString(" (documented)")
	}
	return b.String()
}

func deref(s *string) string {
	if s == nil {
		return "<none>"
	}
	return *s
}

// TestParseJSDocText pins the parser the Svelte extractor shares, including
// the distinction between a bare @returns tag and no tag at all.
func TestParseJSDocText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		raw             string
		wantDescription string
		wantParams      []JSDocParam
		wantReturns     *string
		wantTags        []JSDocTag
	}{
		{
			name:            "a description, parameters and a return",
			raw:             "\n * Greet someone.\n *\n * @param {string} name The person's name\n * @returns {string} The greeting\n ",
			wantDescription: "Greet someone.",
			wantParams:      []JSDocParam{{Name: "name", Description: "The person's name"}},
			wantReturns:     func() *string { s := "The greeting"; return &s }(),
		},
		{
			name:            "a bare returns tag is not the absence of one",
			raw:             "\n * Something.\n * @returns\n ",
			wantDescription: "Something.",
			wantReturns:     func() *string { s := ""; return &s }(),
		},
		{
			name:            "an unrecognized tag is kept as a tag",
			raw:             "\n * The utilities module.\n *\n * @module utils\n ",
			wantDescription: "The utilities module.",
			wantTags:        []JSDocTag{{Tag: "module", Description: "utils"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := ParseJSDocText(test.raw)
			if got.Description != test.wantDescription {
				t.Errorf("Description = %q, want %q", got.Description, test.wantDescription)
			}
			if !reflect.DeepEqual(got.Params, test.wantParams) {
				t.Errorf("Params = %v, want %v", got.Params, test.wantParams)
			}
			if !reflect.DeepEqual(got.Returns, test.wantReturns) {
				t.Errorf("Returns = %v, want %v", deref(got.Returns), deref(test.wantReturns))
			}
			if !reflect.DeepEqual(got.Tags, test.wantTags) {
				t.Errorf("Tags = %v, want %v", got.Tags, test.wantTags)
			}
		})
	}
}

// TestRegistered pins that importing this package is what makes the language
// available, and that the registry hands back this extractor for it.
func TestRegistered(t *testing.T) {
	t.Parallel()

	extractor, ok, err := extractors.Lookup("typescript")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Lookup(\"typescript\") reported no registered factory")
	}
	if extractor.Name() != "typescript" {
		t.Errorf("Name() = %q, want %q", extractor.Name(), "typescript")
	}
}
