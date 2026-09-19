package zig

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

// The reference sources. Between them they carry every declaration form this
// extractor reads: module documentation, public constants, variables and
// functions with and without doc comments, private declarations that must not
// be reported, documented struct fields, and test blocks.
const (
	audioZig = `//! Audio system: 16-channel mixing, sound cache, music playback.
//! Self-contained SDL3 audio module.

const std = @import("std");

pub const NUM_CHANNELS = 16;
pub const MUSIC_CHANNEL = 0;

const AudioChannel = struct {
    stream: ?*anyopaque = null,
    is_playing: bool = false,
};

var g_audio_device: u32 = 0;

/// Open the audio device and create streams for all channels.
/// Call once after SDL_Init.
pub fn init() void {}

/// Destroy all streams, close the audio device, free music data.
pub fn deinit() void {}

/// Add a pre-synthesized PCM buffer to the sound cache.
/// Returns true on success, false if the cache is full.
pub fn cacheSoundEntry(name: []const u8, data: []f32) bool {
    _ = name;
    _ = data;
    return false;
}

pub fn lookupSoundCache(name: []const u8) ?[]f32 {
    _ = name;
    return null;
}

pub var sound_cache_count: usize = 0;

fn privateHelper() void {}
`

	typesZig = `//! Type definitions for the persistence layer.

const std = @import("std");

/// Queue entry for the persistence ring buffer.
pub const QueueEntry = struct {
    /// Type of database operation.
    entry_type: u8 = 0,
    session_id_len: usize = 0,
    player_id_len: usize = 0,
    /// JSON payload for the operation.
    payload_len: usize = 0,
    sequence_num: u64 = 0,
};

/// Configuration for the writer thread.
pub const WriterConfig = struct {
    conninfo: []const u8,
    batch_size: u32 = 64,
    backoff_ms: u64 = 100,
    max_backoff_ms: u64 = 30000,
};
`

	mathZig = `const std = @import("std");

pub fn add(a: u32, b: u32) u32 {
    return a + b;
}

pub fn multiply(a: u32, b: u32) u32 {
    return a * b;
}

test "add works" {
    try std.testing.expectEqual(@as(u32, 5), add(2, 3));
}

test "multiply works" {
    try std.testing.expectEqual(@as(u32, 6), multiply(2, 3));
}
`

	configZig = `const std = @import("std");

pub const Config = struct {
    width: u32 = 800,
    height: u32 = 600,

    /// Initialize config with default values.
    /// Returns a new Config instance.
    pub fn init(self: *Config, allocator: std.mem.Allocator) !void {
        _ = self;
        _ = allocator;
    }

    pub fn deinit(self: *Config) void {
        _ = self;
    }
};
`
)

// referenceSources is the fixture tree most tests read.
var referenceSources = map[string]string{
	"src/audio.zig": audioZig,
	"src/types.zig": typesZig,
	"src/math.zig":  mathZig,
	"build.zig":     "const std = @import(\"std\");\n",
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
			// The declarations are grouped by kind -- constants, then
			// variables, then functions -- rather than rendered in source
			// order, and the private ones are absent.
			name:      "ref renders the module doc and every public declaration, grouped by kind",
			directive: "ref", attrs: map[string]string{"path": "src/audio.zig"},
			want: "## `src/audio.zig`\n\nAudio system: 16-channel mixing, sound cache, music playback. Self-contained SDL3 audio module." +
				"\n\n### `NUM_CHANNELS`\n\n```zig\npub const NUM_CHANNELS = 16\n```" +
				"\n\n### `MUSIC_CHANNEL`\n\n```zig\npub const MUSIC_CHANNEL = 0\n```" +
				"\n\n### `sound_cache_count`\n\n```zig\npub var sound_cache_count: usize = 0\n```" +
				"\n\n### `init`\n\n```zig\npub fn init() void\n```" +
				"\n\nOpen the audio device and create streams for all channels. Call once after SDL_Init." +
				"\n\n### `deinit`\n\n```zig\npub fn deinit() void\n```" +
				"\n\nDestroy all streams, close the audio device, free music data." +
				"\n\n### `cacheSoundEntry`\n\n```zig\npub fn cacheSoundEntry(name: []const u8, data: []f32) bool\n```" +
				"\n\nAdd a pre-synthesized PCM buffer to the sound cache. Returns true on success, false if the cache is full." +
				"\n\n### `lookupSoundCache`\n\n```zig\npub fn lookupSoundCache(name: []const u8) ?[]f32\n```",
		},
		{
			name:      "ref with a target renders one declaration",
			files:     map[string]string{"core.zig": "/// Initializes the system.\npub fn init() void {}\n\n/// Shuts down the system.\npub fn deinit() void {}\n"},
			directive: "ref", attrs: map[string]string{"path": "core.zig", "target": "deinit"},
			want: "### `deinit`\n\n```zig\npub fn deinit() void\n```\n\nShuts down the system.",
		},
		{
			// A container's methods are declarations of the file, so they
			// render beside it rather than inside it.
			name:      "ref renders a container type and the functions declared in it",
			files:     map[string]string{"config.zig": configZig},
			directive: "ref", attrs: map[string]string{"path": "config.zig"},
			want: "## `config.zig`\n\n### `Config`\n\n```zig\npub const Config = struct\n```" +
				"\n\n### `init`\n\n```zig\npub fn init(self: *Config, allocator: std.mem.Allocator) !void\n```" +
				"\n\nInitialize config with default values. Returns a new Config instance." +
				"\n\n### `deinit`\n\n```zig\npub fn deinit(self: *Config) void\n```",
		},
		{
			// A directory is documented as one unit: the module doc of the
			// first file that has one, then every file's declarations in
			// filename order.
			name: "ref on a directory reads every file in it",
			files: map[string]string{
				"src/alpha.zig": "//! The first module.\n\npub const A = 1;\n",
				"src/beta.zig":  "//! The second module.\n\n/// Does the thing.\npub fn doThing(x: u32) void {\n    _ = x;\n}\n",
			},
			directive: "ref", attrs: map[string]string{"path": "src"},
			want: "## `src`\n\nThe first module.\n\n### `A`\n\n```zig\npub const A = 1\n```" +
				"\n\n### `doThing`\n\n```zig\npub fn doThing(x: u32) void\n```\n\nDoes the thing.",
		},
		{
			name:      "prose-desc renders the module doc alone",
			directive: "prose-desc", attrs: map[string]string{"path": "src/audio.zig"},
			want: "Audio system: 16-channel mixing, sound cache, music playback. Self-contained SDL3 audio module.",
		},
		{
			name: "prose-desc on a directory takes the first module doc it finds",
			files: map[string]string{
				"src/alpha.zig": "//! The first module.\n\npub const A = 1;\n",
				"src/beta.zig":  "//! The second module.\n",
			},
			directive: "prose-desc", attrs: map[string]string{"path": "src"},
			want: "The first module.",
		},
		{
			name:      "table-schema renders one struct's fields",
			directive: "table-schema", attrs: map[string]string{"path": "src/types.zig", "target": "QueueEntry"},
			want: "| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
				"| `entry_type` | `u8` | `0` | Type of database operation. |\n" +
				"| `session_id_len` | `usize` | `0` |  |\n" +
				"| `player_id_len` | `usize` | `0` |  |\n" +
				"| `payload_len` | `usize` | `0` | JSON payload for the operation. |\n" +
				"| `sequence_num` | `u64` | `0` |  |",
		},
		{
			name:      "table-schema with no target renders every struct in the file",
			directive: "table-schema", attrs: map[string]string{"path": "src/types.zig"},
			want: "### `QueueEntry`\n\nQueue entry for the persistence ring buffer.\n\n" +
				"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
				"| `entry_type` | `u8` | `0` | Type of database operation. |\n" +
				"| `session_id_len` | `usize` | `0` |  |\n" +
				"| `player_id_len` | `usize` | `0` |  |\n" +
				"| `payload_len` | `usize` | `0` | JSON payload for the operation. |\n" +
				"| `sequence_num` | `u64` | `0` |  |\n" +
				"### `WriterConfig`\n\nConfiguration for the writer thread.\n\n" +
				"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
				"| `conninfo` | `[]const u8` |  |  |\n" +
				"| `batch_size` | `u32` | `64` |  |\n" +
				"| `backoff_ms` | `u64` | `100` |  |\n" +
				"| `max_backoff_ms` | `u64` | `30000` |  |",
		},
		{
			// Only the struct's own fields are collected: the methods below
			// them sit at a deeper brace depth.
			name:      "table-schema skips the methods declared in a struct",
			files:     map[string]string{"config.zig": configZig},
			directive: "table-schema", attrs: map[string]string{"path": "config.zig", "target": "Config"},
			want: "| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
				"| `width` | `u32` | `800` |  |\n| `height` | `u32` | `600` |  |",
		},
		{
			name:      "code-test renders every test block, each in its own fence",
			directive: "code-test", attrs: map[string]string{"path": "src/math.zig"},
			want: "```zig\ntest \"add works\" {\n    try std.testing.expectEqual(@as(u32, 5), add(2, 3));\n}\n```" +
				"\n\n```zig\ntest \"multiply works\" {\n    try std.testing.expectEqual(@as(u32, 6), multiply(2, 3));\n}\n```",
		},
		{
			name:      "code-test with a target renders one block",
			directive: "code-test", attrs: map[string]string{"path": "src/math.zig", "target": "add works"},
			want: "```zig\ntest \"add works\" {\n    try std.testing.expectEqual(@as(u32, 5), add(2, 3));\n}\n```",
		},
		{
			// A Zig test is named by a string literal, so a target written
			// with the quotes selects the same block as one written without.
			name:      "code-test drops the quotes a target may carry",
			directive: "code-test", attrs: map[string]string{"path": "src/math.zig", "target": `"add works"`},
			want: "```zig\ntest \"add works\" {\n    try std.testing.expectEqual(@as(u32, 5), add(2, 3));\n}\n```",
		},
		{
			name:      "table-config renders a JSON file through the shared handler",
			files:     map[string]string{"config.json": `{"host": "localhost", "port": 3000}`},
			directive: "table-config", attrs: map[string]string{"path": "config.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `host` | string | `\"localhost\"` |\n| `port` | integer | `3000` |",
		},
		{
			name:      "table-schema delegates a TOML path to the config handler",
			files:     map[string]string{"config.toml": "host = \"localhost\"\n"},
			directive: "table-schema", attrs: map[string]string{"path": "config.toml"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n| `host` | string | `\"localhost\"` |",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := test.files
			if files == nil {
				files = referenceSources
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
			want: "> *[selfdoc: :::ref requires a file path argument]*",
		},
		{
			name: "ref on a missing file", directive: "ref",
			attrs: map[string]string{"path": "nonexistent.zig"},
			want:  "> *[selfdoc: 'nonexistent.zig' not found]*",
		},
		{
			name: "ref with an unknown target", directive: "ref",
			attrs: map[string]string{"path": "src/audio.zig", "target": "nonexistent"},
			want:  "> *[selfdoc: symbol 'nonexistent' not found in 'src/audio.zig']*",
		},
		{
			name: "prose-desc with no path", directive: "prose-desc", attrs: map[string]string{"path": ""},
			want: "> *[selfdoc: :::prose-desc requires a file path argument]*",
		},
		{
			name: "prose-desc on a file with no module doc", directive: "prose-desc",
			attrs: map[string]string{"path": "src/math.zig"},
			want:  "> *[selfdoc: no module doc comment found in 'src/math.zig']*",
		},
		{
			name: "prose-desc on a missing file", directive: "prose-desc",
			attrs: map[string]string{"path": "nonexistent.zig"},
			want:  "> *[selfdoc: 'nonexistent.zig' not found]*",
		},
		{
			name: "table-schema with no path", directive: "table-schema", attrs: map[string]string{"path": ""},
			want: "> *[selfdoc: :::table-schema requires a file path argument]*",
		},
		{
			name: "table-schema on a file with no structs", directive: "table-schema",
			attrs: map[string]string{"path": "src/math.zig"},
			want:  "> *[selfdoc: no struct types found in 'src/math.zig']*",
		},
		{
			name: "table-schema with an unknown struct", directive: "table-schema",
			attrs: map[string]string{"path": "src/types.zig", "target": "NonExistent"},
			want:  "> *[selfdoc: struct 'NonExistent' not found in 'src/types.zig']*",
		},
		{
			name: "table-schema on a missing file", directive: "table-schema",
			attrs: map[string]string{"path": "nonexistent.zig"},
			want:  "> *[selfdoc: file 'nonexistent.zig' not found]*",
		},
		{
			name: "code-test with no path", directive: "code-test", attrs: map[string]string{"path": ""},
			want: "> *[selfdoc: :::code-test requires a file path argument]*",
		},
		{
			name: "code-test on a file with no test blocks", directive: "code-test",
			attrs: map[string]string{"path": "src/audio.zig"},
			want:  "> *[selfdoc: no test blocks found in 'src/audio.zig']*",
		},
		{
			name: "code-test with an unknown block", directive: "code-test",
			attrs: map[string]string{"path": "src/math.zig", "target": "nonexistent test"},
			want:  "> *[selfdoc: test 'nonexistent test' not found in 'src/math.zig']*",
		},
		{
			name: "code-test on a missing file", directive: "code-test",
			attrs: map[string]string{"path": "nonexistent.zig"},
			want:  "> *[selfdoc: test file 'nonexistent.zig' not found]*",
		},
		{
			// code-help is a TypeScript directive, not a Zig one: the marker
			// names both the directive and the language, because the same name
			// is valid elsewhere.
			name: "a directive this language does not serve", directive: "code-help",
			attrs: map[string]string{"path": "src/audio.zig"},
			want:  "> *[selfdoc: unknown directive 'code-help' for zig extractor]*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, referenceSources)
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

// TestPublicSymbols pins which declarations are reported as public API, across
// every form a Zig declaration takes.
func TestPublicSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a function, a constant and a variable",
			source: "const std = @import(\"std\");\n\npub fn init() void {}\npub const MAX_SIZE = 100;\npub var counter: u32 = 0;\n",
			want:   []string{"init", "MAX_SIZE", "counter"},
		},
		{
			name:   "an extern function",
			source: "pub extern fn SDL_Init(flags: u32) c_int;\n",
			want:   []string{"SDL_Init"},
		},
		{
			name:   "an exported function",
			source: "pub export fn gameInit() void {}\n",
			want:   []string{"gameInit"},
		},
		{
			name:   "an inline function",
			source: "pub inline fn fastAdd(a: u32, b: u32) u32 {\n    return a + b;\n}\n",
			want:   []string{"fastAdd"},
		},
		{
			name:   "a struct type",
			source: "pub const Config = struct {\n    timeout: u32 = 30,\n    retries: u8 = 3,\n};\n",
			want:   []string{"Config"},
		},
		{
			name:   "an enum type",
			source: "pub const Color = enum {\n    red,\n    green,\n    blue,\n};\n",
			want:   []string{"Color"},
		},
		{
			name:   "a tagged union",
			source: "pub const Token = union(enum) {\n    number: f64,\n    string: []const u8,\n    eof,\n};\n",
			want:   []string{"Token"},
		},
		{
			name:   "an error set",
			source: "pub const FileError = error {\n    NotFound,\n    AccessDenied,\n};\n",
			want:   []string{"FileError"},
		},
		{
			name:   "a tagged enum",
			source: "pub const EntryType = enum(u8) {\n    session_create,\n    action,\n    snapshot,\n};\n",
			want:   []string{"EntryType"},
		},
		{
			name: "the private declarations are not public API",
			source: "const private_const = 42;\nvar private_var: u32 = 0;\nfn privateFunc() void {}\n" +
				"pub fn publicFunc() void {}\npub const PUBLIC_CONST = 100;\n",
			want: []string{"publicFunc", "PUBLIC_CONST"},
		},
		{
			name:   "a test block is not a declaration",
			source: "pub fn add(a: u32, b: u32) u32 {\n    return a + b;\n}\n\ntest \"add works\" {\n    try std.testing.expectEqual(@as(u32, 3), add(1, 2));\n}\n",
			want:   []string{"add"},
		},
		{
			name:   "the reference module",
			source: audioZig,
			want: []string{"NUM_CHANNELS", "MUSIC_CHANNEL", "init", "deinit",
				"cacheSoundEntry", "lookupSoundCache", "sound_cache_count"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"mod.zig": test.source})
			got, err := newExtractor().PublicSymbols(filepath.Join(dir, "mod.zig"))
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

	got, err := newExtractor().PublicSymbols(filepath.Join(t.TempDir(), "nonexistent.zig"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("PublicSymbols() = %v, want none", got)
	}
}

// TestModuleDocstring pins the module documentation, which keeps its line
// breaks: the joining of soft-wrapped prose happens where the text is
// rendered, not where it is read.
func TestModuleDocstring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a run of module doc lines",
			source: "//! Audio system for mixing.\n//! Supports 16 channels.\n\nconst std = @import(\"std\");\n",
			want:   "Audio system for mixing.\nSupports 16 channels.",
		},
		{
			name:   "a file with no module doc",
			source: mathZig,
			want:   "",
		},
		{
			name:   "a module doc after code is not a module doc",
			source: "const std = @import(\"std\");\n//! Not the module doc.\n",
			want:   "",
		},
		{
			name:   "a plain comment before the module doc is allowed",
			source: "// SPDX-License-Identifier: MIT\n//! The module doc.\n",
			want:   "The module doc.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"mod.zig": test.source})
			got, err := newExtractor().ModuleDocstring(filepath.Join(dir, "mod.zig"))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("ModuleDocstring() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSymbolDetails pins what the quality measurement reads out of a function
// declaration.
//
// Zig has no doc-comment tags, so a parameter counts as documented when the
// comment names it and the return value counts as documented when the comment
// uses the word "return".
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
			name: "a partly documented function",
			source: "/// Initialize the audio system with the given config.\n" +
				"/// The sample_rate controls audio quality.\n" +
				"/// Returns an error if the device cannot be opened.\n" +
				"pub fn init(sample_rate: u32, channels: u8) !void {\n    // ...\n}\n",
			symbol: "init",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "sample_rate", Type: str("u32"), Documented: true},
					{Name: "channels", Type: str("u8")},
				},
				ReturnType: str("!void"), ReturnDocumented: true,
			},
		},
		{
			name: "the comptime keyword is dropped from a parameter",
			source: "/// Generic container type.\n/// The type T determines element storage.\n" +
				"pub fn Container(comptime T: type, allocator: std.mem.Allocator) !*Self {\n    // ...\n}\n",
			symbol: "Container",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "T", Type: str("type"), Documented: true},
					{Name: "allocator", Type: str("std.mem.Allocator")},
				},
				ReturnType: str("!*Self"),
			},
		},
		{
			name: "an error union return type",
			source: "/// Read bytes from the stream.\n/// Returns the number of bytes actually read.\n" +
				"pub fn read(buffer: []u8, flags: u32) anyerror!usize {\n    return 0;\n}\n",
			symbol: "read",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "buffer", Type: str("[]u8")},
					{Name: "flags", Type: str("u32")},
				},
				ReturnType: str("anyerror!usize"), ReturnDocumented: true,
			},
		},
		{
			name:   "an undocumented function",
			source: "pub fn noDoc(x: u32, y: u32) u32 {\n    return x + y;\n}\n",
			symbol: "noDoc",
			want: &extractors.SymbolDetails{
				Params: []extractors.SymbolParam{
					{Name: "x", Type: str("u32")},
					{Name: "y", Type: str("u32")},
				},
				ReturnType: str("u32"),
			},
		},
		{
			name:   "an anytype parameter",
			source: "pub fn print(value: anytype) void {\n    _ = value;\n}\n",
			symbol: "print",
			want: &extractors.SymbolDetails{
				Params:     []extractors.SymbolParam{{Name: "value", Type: str("anytype")}},
				ReturnType: str("void"),
			},
		},
		{
			name:   "a symbol the file does not declare",
			source: "pub fn exists(x: u32) u32 {\n    return x;\n}\n",
			symbol: "nonexistent", want: nil,
		},
		{
			// The receiver is the type itself rather than something a caller
			// passes, so it is not reported as a parameter.
			name:   "a dotted name resolves a struct method, without its receiver",
			source: configZig, symbol: "Config.init",
			want: &extractors.SymbolDetails{
				Params:     []extractors.SymbolParam{{Name: "allocator", Type: str("std.mem.Allocator")}},
				ReturnType: str("!void"), ReturnDocumented: true,
			},
		},
		{
			name: "a dotted name resolves an enum method",
			source: "pub const State = enum(u8) {\n    idle,\n    running,\n    stopped,\n\n" +
				"    pub fn isActive(self: State) bool {\n        return self == .running;\n    }\n};\n",
			symbol: "State.isActive",
			want: &extractors.SymbolDetails{
				Params:     []extractors.SymbolParam{},
				ReturnType: str("bool"),
			},
		},
		{
			name:   "a dotted name whose member is not there",
			source: "pub const Config = struct {\n    pub fn init(self: *Config) void {\n        _ = self;\n    }\n};\n",
			symbol: "Config.nonexistent", want: nil,
		},
		{
			name:   "a dotted name whose type is not there",
			source: "pub const Config = struct {\n    pub fn init(self: *Config) void {\n        _ = self;\n    }\n};\n",
			symbol: "Other.init", want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"mod.zig": test.source})
			got, err := newExtractor().SymbolDetails(filepath.Join(dir, "mod.zig"), test.symbol)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("SymbolDetails(%q) = %+v, want %+v", test.symbol, got, test.want)
			}
		})
	}
}

// TestExtractAllTestBlocks pins which test blocks a file is reported to carry,
// and their order.
func TestExtractAllTestBlocks(t *testing.T) {
	t.Parallel()

	got := extractAllTestBlocks(mathZig)
	var names []string
	for _, test := range got {
		names = append(names, test.Name)
	}
	want := []string{"add works", "multiply works"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("extractAllTestBlocks() named %v, want %v", names, want)
	}
}

// TestDetect pins language detection, which is by the build script or its
// dependency manifest.
func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{name: "a build script", files: map[string]string{"build.zig": "const std = @import(\"std\");\n"}, want: true},
		{name: "a dependency manifest", files: map[string]string{"build.zig.zon": ".{}\n"}, want: true},
		{name: "neither", want: false},
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
	if !reflect.DeepEqual(got, []string{".zig"}) {
		t.Errorf("FileExtensions() = %v, want [.zig]", got)
	}
}

// TestResolvePath pins path resolution, including the directory form and the
// implicit extension.
func TestResolvePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, referenceSources)
	extractor := newExtractor()

	tests := []struct {
		name        string
		arg         string
		sourcePaths []string
		wantSuffix  string
	}{
		{name: "the path as written", arg: "src/audio.zig", wantSuffix: "src/audio.zig"},
		{name: "a directory of Zig source", arg: "src", wantSuffix: "src"},
		{name: "under a source path", arg: "audio.zig", sourcePaths: []string{"src/"}, wantSuffix: "src/audio.zig"},
		{name: "with the extension appended", arg: "src/audio", wantSuffix: "src/audio.zig"},
		{name: "nothing resolves", arg: "nonexistent.zig", wantSuffix: ""},
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

// TestResolvePathIgnoresADirectoryWithNoZigSource pins that a same-named
// directory beside the file the author meant does not shadow it.
func TestResolvePathIgnoresADirectoryWithNoZigSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"thing/notes.md": "no source here\n",
		"thing.zig":      "pub const A = 1;\n",
	})

	got := newExtractor().ResolvePath("thing", nil, dir)
	if !strings.HasSuffix(got, "thing.zig") {
		t.Errorf("ResolvePath(\"thing\") = %q, want the .zig file", got)
	}
}

// TestRegistered pins that importing this package is what makes the language
// available.
func TestRegistered(t *testing.T) {
	t.Parallel()

	extractor, ok, err := extractors.Lookup("zig")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Lookup(\"zig\") reported no registered factory")
	}
	if extractor.Name() != "zig" {
		t.Errorf("Name() = %q, want %q", extractor.Name(), "zig")
	}
}
