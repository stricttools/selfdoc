package svelte

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

// The reference components. Between them they carry every declaration form
// this extractor reads: a documented component with typed $props() including a
// $bindable() one, a component with both script blocks, a Svelte 3/4 component
// declaring its properties with "export let", and a component with no script
// block at all.
const (
	counterSvelte = `<script lang="ts">
/**
 * A counter component with increment and reset functionality.
 */
let { count = 0, label, value = $bindable(), onchange }: { count: number; label: string; value: number; onchange: () => void } = $props();

export function reset() {
    count = 0;
}

export const defaultCount = 10;
</script>

<button on:click={() => count++}>{label}: {count}</button>
`

	utilsSvelte = `<script module>
export function pauseAll() {
    // pause all instances
}

export const VERSION = '1.0';
</script>

<script lang="ts">
let { name, size = 'medium' } = $props();

export function refresh() {
    // refresh this instance
}
</script>

<div>{name}</div>
`

	legacySvelte = `<script>
export let name = 'world';
export let count;
export let size: string = 'medium';
</script>

<p>Hello {name}!</p>
`

	blankSvelte = "<p>Static content</p>\n"

	emptyScriptSvelte = `<script>
</script>

<p>Content</p>
`

	contextModuleSvelte = `<script context="module">
export const SHARED = 'shared';
</script>

<script>
let { title } = $props();
</script>

<h1>{title}</h1>
`
)

// referenceComponents is the fixture tree most tests read.
var referenceComponents = map[string]string{
	"src/lib/Counter.svelte":       counterSvelte,
	"src/lib/Utils.svelte":         utilsSvelte,
	"src/lib/Legacy.svelte":        legacySvelte,
	"src/lib/Blank.svelte":         blankSvelte,
	"src/lib/EmptyScript.svelte":   emptyScriptSvelte,
	"src/lib/ContextModule.svelte": contextModuleSvelte,
	"svelte.config.js":             "export default {};\n",
}

// TestExtractRendersReferenceOutput pins the Markdown every directive emits,
// byte for byte, against the output the Python extractor this ports produces
// for the same input.
func TestExtractRendersReferenceOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string
		directive string
		attrs     map[string]string
		want      string
	}{
		{
			name:      "ref renders the component doc, the props and the instance exports",
			directive: "ref", attrs: map[string]string{"path": "src/lib/Counter.svelte"},
			want: "## `Counter`\n\nA counter component with increment and reset functionality." +
				"\n\n### Props\n\n| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `count` | `number` | `0` | No |\n| `label` | `string` |  | No |\n" +
				"| `value` | `number` |  | Yes |\n| `onchange` | `() => void` |  | No |" +
				"\n\n### Instance Exports\n\n#### `reset`\n\n```typescript\nexport function reset()\n```" +
				"\n\n#### `defaultCount`\n\n```typescript\nexport const defaultCount = 10\n```",
		},
		{
			name:      "ref renders the module exports in their own section, after the instance ones",
			directive: "ref", attrs: map[string]string{"path": "src/lib/Utils.svelte"},
			want: "## `Utils`\n\n### Props\n\n| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `name` |  |  | No |\n| `size` |  | `'medium'` | No |" +
				"\n\n### Instance Exports\n\n#### `refresh`\n\n```typescript\nexport function refresh()\n```" +
				"\n\n### Module Exports\n\n#### `pauseAll`\n\n```typescript\nexport function pauseAll()\n```" +
				"\n\n#### `VERSION`\n\n```typescript\nexport const VERSION = '1.0'\n```",
		},
		{
			name:      "ref reads the properties a Svelte 3 or 4 component declares",
			directive: "ref", attrs: map[string]string{"path": "src/lib/Legacy.svelte"},
			want: "## `Legacy`\n\n### Props\n\n| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `name` |  | `'world'` | No |\n| `count` |  |  | No |\n| `size` | `string` | `'medium'` | No |",
		},
		{
			name:      "ref with a target renders one property as a one-row table",
			directive: "ref", attrs: map[string]string{"path": "src/lib/Counter.svelte", "target": "value"},
			want: "### `value`\n\n| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `value` | `number` |  | Yes |",
		},
		{
			name:      "ref with a target renders one instance export",
			directive: "ref", attrs: map[string]string{"path": "src/lib/Counter.svelte", "target": "reset"},
			want: "### `reset`\n\n```typescript\nexport function reset()\n```",
		},
		{
			name:      "ref with a target renders one module export",
			directive: "ref", attrs: map[string]string{"path": "src/lib/Utils.svelte", "target": "VERSION"},
			want: "### `VERSION`\n\n```typescript\nexport const VERSION = '1.0'\n```",
		},
		{
			name:      "prose-desc renders the component doc alone",
			directive: "prose-desc", attrs: map[string]string{"path": "src/lib/Counter.svelte"},
			want: "A counter component with increment and reset functionality.",
		},
		{
			name:      "table-schema renders the props table on its own",
			directive: "table-schema", attrs: map[string]string{"path": "src/lib/Counter.svelte"},
			want: "| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `count` | `number` | `0` | No |\n| `label` | `string` |  | No |\n" +
				"| `value` | `number` |  | Yes |\n| `onchange` | `() => void` |  | No |",
		},
		{
			name:      "table-schema reads the legacy property declarations too",
			directive: "table-schema", attrs: map[string]string{"path": "src/lib/Legacy.svelte"},
			want: "| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `name` |  | `'world'` | No |\n| `count` |  |  | No |\n| `size` | `string` | `'medium'` | No |",
		},
		{
			// The inline type's keys keep the optional marker -- "label?" --
			// while the destructured name does not, so an optional property's
			// type is not found and its cell is empty. The Python does the
			// same thing, and a page built from either says so identically.
			name: "ref reports no type for an optionally-declared property",
			files: map[string]string{"Button.svelte": "<script lang=\"ts\">\n" +
				"  let { label = \"Click\", disabled = false }: { label?: string; disabled?: boolean } = $props();\n" +
				"</script>\n<button {disabled}>{label}</button>\n"},
			directive: "ref", attrs: map[string]string{"path": "Button.svelte", "target": "label"},
			want: "### `label`\n\n| Prop | Type | Default | Bindable |\n| --- | --- | --- | --- |\n" +
				"| `label` |  | `\"Click\"` | No |",
		},
		{
			name:      "table-config renders a JSON file through the shared handler",
			files:     map[string]string{"config.json": `{"host": "localhost", "port": 3000}`},
			directive: "table-config", attrs: map[string]string{"path": "config.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `host` | string | `\"localhost\"` |\n| `port` | integer | `3000` |",
		},
		{
			name:      "table-schema delegates a JSON path to the config handler",
			files:     map[string]string{"config.json": `{"host": "localhost"}`},
			directive: "table-schema", attrs: map[string]string{"path": "config.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n| `host` | string | `\"localhost\"` |",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := test.files
			if files == nil {
				files = referenceComponents
			}
			writeTree(t, dir, files)
			got, err := newExtractor().Extract(test.directive, test.attrs, nil, nil, dir)
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
// content it could not resolve.
func TestExtractErrorMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		directive string
		attrs     map[string]string
		want      string
	}{
		{
			name: "ref with no path", directive: "ref", attrs: map[string]string{"path": ""},
			want: "> *[selfdoc: ref requires a file path argument]*",
		},
		{
			name: "ref on a missing component", directive: "ref",
			attrs: map[string]string{"path": "nonexistent.svelte"},
			want:  "> *[selfdoc: component 'nonexistent.svelte' not found]*",
		},
		{
			name: "ref with an unknown target", directive: "ref",
			attrs: map[string]string{"path": "src/lib/Counter.svelte", "target": "nonexistent"},
			want:  "> *[selfdoc: symbol 'nonexistent' not found in 'src/lib/Counter.svelte']*",
		},
		{
			name: "prose-desc with no path", directive: "prose-desc", attrs: map[string]string{"path": ""},
			want: "> *[selfdoc: prose-desc requires a file path argument]*",
		},
		{
			name: "prose-desc on a component with no doc", directive: "prose-desc",
			attrs: map[string]string{"path": "src/lib/Legacy.svelte"},
			want:  "> *[selfdoc: no component-level JSDoc found in 'src/lib/Legacy.svelte']*",
		},
		{
			name: "table-schema with no path", directive: "table-schema", attrs: map[string]string{"path": ""},
			want: "> *[selfdoc: table-schema requires a file path argument]*",
		},
		{
			name: "table-schema on a component with no props", directive: "table-schema",
			attrs: map[string]string{"path": "src/lib/Blank.svelte"},
			want:  "> *[selfdoc: no props found in 'src/lib/Blank.svelte']*",
		},
		{
			name: "table-schema on a missing component", directive: "table-schema",
			attrs: map[string]string{"path": "nonexistent.svelte"},
			want:  "> *[selfdoc: component 'nonexistent.svelte' not found]*",
		},
		{
			// code-help is a TypeScript directive, not a Svelte one: the
			// marker names both the directive and the language, because the
			// same name is valid elsewhere.
			name: "a directive this language does not serve", directive: "code-help",
			attrs: map[string]string{"path": "src/lib/Counter.svelte"},
			want:  "> *[selfdoc: unknown directive 'code-help' for svelte extractor]*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, referenceComponents)
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

// TestExtractScriptBlocks pins which script block is the instance one and
// which is the module one, across every spelling of the module marker.
func TestExtractScriptBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantInstance string
		wantModule   string
	}{
		{
			name:         "a typed instance script",
			source:       "<script lang=\"ts\">\nlet x = 1;\n</script>\n<p>hi</p>",
			wantInstance: "\nlet x = 1;\n",
		},
		{
			name:       "the bare module marker",
			source:     "<script module>\nexport const A = 1;\n</script>\n<p>hi</p>",
			wantModule: "\nexport const A = 1;\n",
		},
		{
			name:         "both blocks, module first",
			source:       utilsSvelte,
			wantInstance: "\nlet { name, size = 'medium' } = $props();\n\nexport function refresh() {\n    // refresh this instance\n}\n",
			wantModule:   "\nexport function pauseAll() {\n    // pause all instances\n}\n\nexport const VERSION = '1.0';\n",
		},
		{
			name:   "no script block at all",
			source: "<p>Just HTML</p>",
		},
		{
			name:         "the context attribute spelling of the module marker",
			source:       contextModuleSvelte,
			wantInstance: "\nlet { title } = $props();\n",
			wantModule:   "\nexport const SHARED = 'shared';\n",
		},
		{
			name:         "a script block with no attributes",
			source:       "<script>\nlet y = 2;\n</script>",
			wantInstance: "\nlet y = 2;\n",
		},
		{
			name:         "a JavaScript-typed instance script",
			source:       "<script lang=\"js\">\nlet z = 3;\n</script>",
			wantInstance: "\nlet z = 3;\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := extractScriptBlocks(test.source)
			if got.Instance != test.wantInstance {
				t.Errorf("Instance = %q, want %q", got.Instance, test.wantInstance)
			}
			if got.Module != test.wantModule {
				t.Errorf("Module = %q, want %q", got.Module, test.wantModule)
			}
		})
	}
}

// TestExtractProps pins what the $props() rune yields, including the
// $bindable() marker and the two forms a type annotation takes.
func TestExtractProps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   []prop
	}{
		{
			name:   "plain destructuring",
			script: "let { name, count } = $props();",
			want:   []prop{{Name: "name"}, {Name: "count"}},
		},
		{
			name:   "default values",
			script: "let { name = 'hello', count = 0 } = $props();",
			want:   []prop{{Name: "name", Default: "'hello'"}, {Name: "count", Default: "0"}},
		},
		{
			name:   "a bindable property with no default",
			script: "let { value = $bindable() } = $props();",
			want:   []prop{{Name: "value", Bindable: true}},
		},
		{
			name:   "a bindable property with a default",
			script: "let { value = $bindable(42) } = $props();",
			want:   []prop{{Name: "value", Default: "42", Bindable: true}},
		},
		{
			name:   "an inline object type gives each property its own type",
			script: "let { name, count }: { name: string; count: number } = $props();",
			want:   []prop{{Name: "name", Type: "string"}, {Name: "count", Type: "number"}},
		},
		{
			name:   "a named interface is reported as every property's type",
			script: "let { name, count }: Props = $props();",
			want:   []prop{{Name: "name", Type: "Props"}, {Name: "count", Type: "Props"}},
		},
		{
			name:   "a rest element",
			script: "let { name, ...rest } = $props();",
			want:   []prop{{Name: "name"}, {Name: "rest", Default: "...rest"}},
		},
		{
			name:   "a script with no $props() call",
			script: "let x = 1;",
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := extractProps(test.script)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("extractProps() = %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestExtractLegacyProps pins the Svelte 3 and 4 declaration form.
func TestExtractLegacyProps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   []prop
	}{
		{name: "a bare declaration", script: "export let name;", want: []prop{{Name: "name"}}},
		{
			name: "a default value", script: "export let name = 'world';",
			want: []prop{{Name: "name", Default: "'world'"}},
		},
		{
			name: "a type and a default", script: "export let size: string = 'medium';",
			want: []prop{{Name: "size", Type: "string", Default: "'medium'"}},
		},
		{
			name: "every declaration in a script block", script: legacySvelte,
			want: []prop{
				{Name: "name", Default: "'world'"},
				{Name: "count"},
				{Name: "size", Type: "string", Default: "'medium'"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := extractLegacyProps(test.script)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("extractLegacyProps() = %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestExtractExports pins the exported functions and constants of a script
// block, and the signature each renders as.
func TestExtractExports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   []export
	}{
		{
			name:   "an exported function",
			script: "export function reset() {\n    count = 0;\n}\n",
			want:   []export{{Name: "reset", Kind: "function", Signature: "export function reset()"}},
		},
		{
			name:   "an exported constant",
			script: "export const defaultCount = 10;",
			want:   []export{{Name: "defaultCount", Kind: "const", Signature: "export const defaultCount = 10"}},
		},
		{
			name:   "a typed exported constant",
			script: "export const VERSION: string = '1.0';",
			want:   []export{{Name: "VERSION", Kind: "const", Signature: "export const VERSION: string = '1.0'"}},
		},
		{
			name:   "a script block exporting nothing",
			script: "let x = 1;",
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := extractExports(test.script)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("extractExports() = %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestPublicSymbols pins what a component is reported to expose, and the order
// it is reported in: the component itself, then its properties, its instance
// exports and its module exports.
func TestPublicSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		want []string
	}{
		{
			name: "every category at once", file: "src/lib/Counter.svelte",
			want: []string{"Counter", "count", "label", "value", "onchange", "reset", "defaultCount"},
		},
		{
			name: "both script blocks", file: "src/lib/Utils.svelte",
			want: []string{"Utils", "name", "size", "refresh", "pauseAll", "VERSION"},
		},
		{
			name: "the legacy declaration form", file: "src/lib/Legacy.svelte",
			want: []string{"Legacy", "name", "count", "size"},
		},
		{
			name: "a component with no script block", file: "src/lib/Blank.svelte",
			want: []string{"Blank"},
		},
		{
			name: "a component whose script block is empty", file: "src/lib/EmptyScript.svelte",
			want: []string{"EmptyScript"},
		},
	}

	dir := t.TempDir()
	writeTree(t, dir, referenceComponents)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := newExtractor().PublicSymbols(filepath.Join(dir, test.file))
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
// symbols rather than an error.
func TestPublicSymbolsOnAMissingFile(t *testing.T) {
	t.Parallel()

	got, err := newExtractor().PublicSymbols(filepath.Join(t.TempDir(), "Nope.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("PublicSymbols() = %v, want none", got)
	}
}

// TestModuleDocstring pins the component's own documentation, which is the
// JSDoc block at the top of the instance script.
func TestModuleDocstring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a block at the top of the instance script",
			source: "<script>\n/**\n * A counter component with reset.\n */\nlet x = 1;\n</script>\n",
			want:   "A counter component with reset.",
		},
		{
			name:   "a component with no doc block",
			source: legacySvelte,
			want:   "",
		},
		{
			name:   "a component with no script block",
			source: blankSvelte,
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"C.svelte": test.source})
			got, err := newExtractor().ModuleDocstring(filepath.Join(dir, "C.svelte"))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("ModuleDocstring() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSymbolDetails pins what the quality measurement reads out of an exported
// function, and that a component name -- which is not a function -- answers
// with nothing.
func TestSymbolDetails(t *testing.T) {
	t.Parallel()

	str := func(s string) *string { return &s }

	greet := "<script>\n/**\n * Greet someone.\n * @param name The person's name\n */\n" +
		"export function greet(name: string, count: number): string {\n" +
		"    return `Hello ${name}! (${count})`;\n}\n</script>\n"

	tests := []struct {
		name   string
		source string
		symbol string
		want   *extractors.SymbolDetails
	}{
		{
			name: "a partly documented exported function", source: greet, symbol: "greet",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "name", Type: str("string"), Documented: true},
					{Name: "count", Type: str("number")},
				},
				ReturnType: str("string"),
			},
		},
		{
			name:   "a function exported by the module script",
			source: utilsSvelte, symbol: "pauseAll",
			want: &extractors.SymbolDetails{Params: []extractors.SymbolParam{}},
		},
		{
			name:   "the component itself is not a function",
			source: "<script lang=\"ts\">\nlet { name, count }: { name: string; count: number } = $props();\n</script>\n<p>{name}</p>\n",
			symbol: "MyComponent", want: nil,
		},
		{
			name:   "a name the component does not export",
			source: "<script>\nexport function hello() {}\n</script>\n",
			symbol: "nonexistent", want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"MyComponent.svelte": test.source})
			got, err := newExtractor().SymbolDetails(
				filepath.Join(dir, "MyComponent.svelte"), test.symbol)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("SymbolDetails(%q) = %+v, want %+v", test.symbol, got, test.want)
			}
		})
	}
}

// TestDetect pins language detection, which is by either spelling of the
// Svelte config file.
func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{name: "a JavaScript config", files: map[string]string{"svelte.config.js": "export default {};\n"}, want: true},
		{name: "a TypeScript config", files: map[string]string{"svelte.config.ts": "export default {};\n"}, want: true},
		{name: "no config at all", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, test.files)
			if got := newExtractor().Detect(dir); got != test.want {
				t.Errorf("Detect() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestFileExtensions pins the one extension this language owns.
func TestFileExtensions(t *testing.T) {
	t.Parallel()

	got := newExtractor().FileExtensions()
	if !reflect.DeepEqual(got, []string{".svelte"}) {
		t.Errorf("FileExtensions() = %v, want [.svelte]", got)
	}
}

// TestResolvePath pins path resolution, including the implicit extension.
func TestResolvePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, referenceComponents)
	extractor := newExtractor()

	tests := []struct {
		name        string
		arg         string
		sourcePaths []string
		wantSuffix  string
	}{
		{name: "the path as written", arg: "src/lib/Counter.svelte", wantSuffix: "Counter.svelte"},
		{name: "under a source path", arg: "Counter.svelte", sourcePaths: []string{"src/lib/"}, wantSuffix: "Counter.svelte"},
		{name: "with the extension appended", arg: "src/lib/Counter", wantSuffix: "Counter.svelte"},
		{name: "nothing resolves", arg: "nonexistent.svelte", wantSuffix: ""},
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

// TestRegisteredBeforeTypeScript pins both that importing this package
// registers the language and that detection tries it before TypeScript: a
// Svelte project also carries TypeScript files and a tsconfig.json, so the
// other order would report every Svelte project as a TypeScript one.
func TestRegisteredBeforeTypeScript(t *testing.T) {
	t.Parallel()

	extractor, ok, err := extractors.Lookup("svelte")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Lookup(\"svelte\") reported no registered factory")
	}
	if extractor.Name() != "svelte" {
		t.Errorf("Name() = %q, want %q", extractor.Name(), "svelte")
	}

	order := extractors.DetectionOrder()
	svelteIdx, typescriptIdx := -1, -1
	for i, name := range order {
		switch name {
		case "svelte":
			svelteIdx = i
		case "typescript":
			typescriptIdx = i
		}
	}
	if svelteIdx < 0 || typescriptIdx < 0 {
		t.Fatalf("detection order %v is missing one of the two languages", order)
	}
	if svelteIdx > typescriptIdx {
		t.Errorf("detection order %v tries typescript before svelte", order)
	}
}
