package kotlin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// writeTree writes a file tree under a fresh temporary directory and answers
// its root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

// newExtractor builds the extractor under test.
func newExtractor() extractors.Extractor { return New() }

// extract resolves one directive against a fixture root.
func extract(t *testing.T, root, directive string, attrs map[string]string, sourcePaths ...string) string {
	t.Helper()
	out, err := newExtractor().Extract(directive, attrs, nil, sourcePaths, root)
	if err != nil {
		t.Fatalf("extract %s: %v", directive, err)
	}
	return out
}

// parserSource is the sample file the symbol and ref tests read: a module doc,
// every kind of type Kotlin declares, a documented function, properties, a
// typealias, and one declaration of each restricted visibility.
const parserSource = `/**
 * Parser module: handles source code parsing and AST construction.
 * Supports incremental parsing and error recovery.
 */

package com.example.parser

import java.io.File

/**
 * A source code parser with incremental support.
 *
 * Use [Parser] to parse source files into an AST.
 * @param source The source code to parse.
 * @return The parsed AST root node.
 * @constructor Creates a new parser instance.
 */
class Parser(val source: String) {
    fun parse(): ASTNode {
        return ASTNode()
    }
}

/** An AST node. */
data class ASTNode(val type: String = "root", val children: List<ASTNode> = emptyList())

sealed class ParseResult {
    data class Success(val node: ASTNode) : ParseResult()
}

object ParserFactory {
    fun create(): Parser = Parser("")
}

data object EmptyNode

interface Parseable {
    fun parse(): ASTNode
}

enum class TokenType {
    IDENTIFIER
}

/**
 * Parse the given source text into an AST.
 *
 * @param source The source code to parse.
 * @param mode The parsing mode.
 * @return The parsed AST root node.
 * @throws ParseException if the source is malformed.
 */
fun parseSource(source: String, mode: String = "default"): ASTNode {
    return ASTNode()
}

val maxParseDepth: Int = 256

var parserVersion: String = "1.0"

typealias ParseCallback = (ASTNode) -> Unit

private class InternalHelper {
    fun help() {}
}

internal fun internalSetup() {}

protected val protectedValue: Int = 42

@PublishedApi
internal fun publishedApiHelper(): String = "public"
`

// parserProject is parserSource in a Gradle project.
func parserProject() map[string]string {
	return map[string]string{
		"build.gradle.kts": "plugins { kotlin(\"jvm\") }\n",
		"src/Parser.kt":    parserSource,
	}
}

func TestNameAndFileExtensions(t *testing.T) {
	t.Parallel()

	extractor := newExtractor()
	if got := extractor.Name(); got != "kotlin" {
		t.Errorf("Name() = %q, want kotlin", got)
	}
	exts := extractor.FileExtensions()
	if len(exts) != 1 || exts[0] != ".kt" {
		t.Errorf("FileExtensions() = %v, want [.kt]", exts)
	}
}

func TestRegisteredFactory(t *testing.T) {
	t.Parallel()

	extractor, ok, err := extractors.Lookup("kotlin")
	if err != nil || !ok {
		t.Fatalf("Lookup(kotlin) = %v, %v, %v", extractor, ok, err)
	}
	if extractor.Name() != "kotlin" {
		t.Errorf("registered extractor named %q", extractor.Name())
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"kotlin build script", map[string]string{"build.gradle.kts": "plugins {}\n"}, true},
		{"groovy build script", map[string]string{"build.gradle": "apply plugin: 'kotlin'\n"}, true},
		{"no build script", map[string]string{"Main.kt": "fun main() {}\n"}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			if got := newExtractor().Detect(root); got != test.want {
				t.Errorf("Detect() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPublicSymbols(t *testing.T) {
	t.Parallel()

	root := writeTree(t, parserProject())
	symbols, err := newExtractor().PublicSymbols(filepath.Join(root, "src", "Parser.kt"))
	if err != nil {
		t.Fatalf("PublicSymbols: %v", err)
	}

	// The scanner reads declarations line by line, so a member of a type is
	// listed beside the type itself.
	want := []string{
		"Parser", "parse", "ASTNode", "ParseResult", "Success", "ParserFactory",
		"create", "EmptyNode", "Parseable", "TokenType", "parseSource",
		"maxParseDepth", "parserVersion", "ParseCallback", "help", "publishedApiHelper",
	}
	if len(symbols) != len(want) {
		t.Fatalf("PublicSymbols = %v, want %v", symbols, want)
	}
	for i, name := range want {
		if symbols[i] != name {
			t.Errorf("symbol %d = %q, want %q", i, symbols[i], name)
		}
	}

	// The restricted declarations themselves are off the list; @PublishedApi
	// internal is on it.
	for _, unwanted := range []string{"InternalHelper", "internalSetup", "protectedValue"} {
		if contains(symbols, unwanted) {
			t.Errorf("PublicSymbols = %v, should not carry %q", symbols, unwanted)
		}
	}
}

func TestPublicSymbolsEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a trailing comment is not part of the declaration",
			source: "val timeout: Int = 30 // seconds\n",
			want:   []string{"timeout"},
		},
		{
			name:   "an annotation between the doc and its declaration keeps it published",
			source: "@PublishedApi\n/** Helper. */\ninternal fun helper() {}\n",
			want:   []string{"helper"},
		},
		{
			name:   "an unmarked internal declaration stays off the list",
			source: "internal fun helper() {}\n",
			want:   nil,
		},
		{
			name:   "modifiers before the keyword are consumed",
			source: "suspend inline fun <T> await(block: () -> T): T = block()\n",
			want:   []string{"await"},
		},
		{
			name:   "empty file",
			source: "",
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"Main.kt": test.source})
			symbols, err := newExtractor().PublicSymbols(filepath.Join(root, "Main.kt"))
			if err != nil {
				t.Fatalf("PublicSymbols: %v", err)
			}
			if len(symbols) != len(test.want) {
				t.Fatalf("PublicSymbols = %v, want %v", symbols, test.want)
			}
			for i, name := range test.want {
				if symbols[i] != name {
					t.Errorf("symbol %d = %q, want %q", i, symbols[i], name)
				}
			}
		})
	}
}

func TestPublicSymbolsMissingFile(t *testing.T) {
	t.Parallel()

	symbols, err := newExtractor().PublicSymbols(filepath.Join(t.TempDir(), "nonexistent.kt"))
	if err != nil {
		t.Fatalf("PublicSymbols: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("PublicSymbols = %v, want none", symbols)
	}
}

func TestResolvePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pathArg     string
		sourcePaths []string
		wantSuffix  string
		wantEmpty   bool
	}{
		{name: "kotlin file", pathArg: "src/Parser.kt", wantSuffix: "Parser.kt"},
		{name: "directory of kotlin files", pathArg: "src", wantSuffix: "src"},
		{
			name: "through a source path", pathArg: "Parser.kt",
			sourcePaths: []string{"src/"}, wantSuffix: "Parser.kt",
		},
		{name: "implicit extension", pathArg: "src/Parser", wantSuffix: "Parser.kt"},
		{name: "not found", pathArg: "nonexistent.kt", wantEmpty: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, parserProject())
			got := newExtractor().ResolvePath(test.pathArg, test.sourcePaths, root)
			if test.wantEmpty {
				if got != "" {
					t.Fatalf("ResolvePath = %q, want the empty string", got)
				}
				return
			}
			if !strings.HasSuffix(got, test.wantSuffix) {
				t.Errorf("ResolvePath = %q, want a path ending in %q", got, test.wantSuffix)
			}
		})
	}
}

func TestRefRendersWholeFile(t *testing.T) {
	t.Parallel()

	root := writeTree(t, parserProject())
	got := extract(t, root, "ref", map[string]string{"path": "src/Parser.kt"})

	want := "## `src/Parser.kt`\n\n" +
		"Parser module: handles source code parsing and AST construction. " +
		"Supports incremental parsing and error recovery.\n\n" +
		"### `Parser`\n\n```kotlin\nclass Parser(val source: String)\n```\n\n" +
		"A source code parser with incremental support.\n\n" +
		"Use `Parser` to parse source files into an AST.\n\n" +
		"**Parameters:**\n\n- `source`: The source code to parse.\n\n" +
		"**Returns:** The parsed AST root node.\n" +
		"**Constructor:** Creates a new parser instance.\n\n\n" +
		"### `ASTNode`\n\n```kotlin\n" +
		"data class ASTNode(val type: String = \"root\", val children: List<ASTNode> = emptyList())\n" +
		"```\n\nAn AST node.\n\n" +
		"### `ParseResult`\n\n```kotlin\nsealed class ParseResult\n```\n\n" +
		"### `Success`\n\n```kotlin\ndata class Success(val node: ASTNode) : ParseResult()\n```\n\n" +
		"### `ParserFactory`\n\n```kotlin\nobject ParserFactory\n```\n\n" +
		"### `EmptyNode`\n\n```kotlin\ndata object EmptyNode\n```\n\n" +
		"### `Parseable`\n\n```kotlin\ninterface Parseable\n```\n\n" +
		"### `TokenType`\n\n```kotlin\nenum class TokenType\n```\n\n" +
		"### `ParseCallback`\n\n```kotlin\ntypealias ParseCallback = (ASTNode) -> Unit\n```\n\n" +
		"### `parse`\n\n```kotlin\nfun parse(): ASTNode\n```\n\n" +
		"### `create`\n\n```kotlin\nfun create(): Parser = Parser(\"\")\n```\n\n" +
		"### `parseSource`\n\n```kotlin\n" +
		"fun parseSource(source: String, mode: String = \"default\"): ASTNode\n```\n\n" +
		"Parse the given source text into an AST.\n\n" +
		"**Parameters:**\n\n- `source`: The source code to parse.\n- `mode`: The parsing mode.\n\n" +
		"**Returns:** The parsed AST root node.\n" +
		"**Throws:** `ParseException` if the source is malformed.\n\n\n" +
		"### `help`\n\n```kotlin\nfun help()\n```\n\n" +
		"### `publishedApiHelper`\n\n```kotlin\ninternal fun publishedApiHelper(): String = \"public\"\n```\n\n" +
		"### `maxParseDepth`\n\n```kotlin\nval maxParseDepth: Int = 256\n```\n\n" +
		"### `parserVersion`\n\n```kotlin\nvar parserVersion: String = \"1.0\"\n```"

	if got != want {
		t.Errorf("ref output\n got: %q\nwant: %q", got, want)
	}
	for _, unwanted := range []string{"InternalHelper", "internalSetup", "protectedValue"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("ref rendered the restricted declaration %q", unwanted)
		}
	}
}

func TestRefWithTarget(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"Parser.kt": "/**\n * Parses input strings.\n */\n" +
			"fun parse(input: String): Boolean = true\n\n" +
			"/**\n * Formats output.\n */\n" +
			"fun format(data: Any): String = data.toString()\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "Parser.kt", "target": "format"})

	// The doc block's own trailing blank line is part of what the KDoc
	// scanner read, so the rendered doc ends with a newline.
	want := "### `format`\n\n```kotlin\nfun format(data: Any): String = data.toString()\n```\n\n" +
		"Formats output.\n"
	if got != want {
		t.Errorf("ref target\n got: %q\nwant: %q", got, want)
	}
}

func TestRefErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		attrs map[string]string
		want  string
	}{
		{
			name:  "no path",
			attrs: map[string]string{"path": ""},
			want:  "> *[selfdoc: :::ref requires a file path argument]*",
		},
		{
			name:  "missing file",
			attrs: map[string]string{"path": "nonexistent.kt"},
			want:  "> *[selfdoc: 'nonexistent.kt' not found]*",
		},
		{
			name:  "target not found",
			files: map[string]string{"Parser.kt": "fun foo() {}\n"},
			attrs: map[string]string{"path": "Parser.kt", "target": "nonexistent"},
			want:  "> *[selfdoc: symbol 'nonexistent' not found in 'Parser.kt']*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			if got := extract(t, root, "ref", test.attrs); got != test.want {
				t.Errorf("ref = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRefOnDirectory(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"src/Alpha.kt": "/** The alpha module. */\nfun alpha() {}\n",
		"src/Beta.kt":  "fun beta() {}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "src"})

	// The module doc of the first file heads the section, and the same
	// one-line comment is also the declaration's own doc.
	want := "## `src`\n\nThe alpha module.\n\n" +
		"### `alpha`\n\n```kotlin\nfun alpha()\n```\n\nThe alpha module.\n\n" +
		"### `beta`\n\n```kotlin\nfun beta()\n```"
	if got != want {
		t.Errorf("ref on a directory\n got: %q\nwant: %q", got, want)
	}
}

func TestNoDocAssociationAcrossBlankLine(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"BlankLine.kt": "/** This comment is for the function below. */\n" +
			"fun attached() {}\n\n" +
			"/** This comment is NOT for the function below. */\n\n" +
			"fun detached() {}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "BlankLine.kt"})

	if !strings.Contains(got, "This comment is for the function below") {
		t.Errorf("ref = %q, the attached doc is missing", got)
	}
	if strings.Contains(got, "This comment is NOT for the function below") {
		t.Errorf("ref = %q, a doc across a blank line was associated", got)
	}
}

func TestProseDesc(t *testing.T) {
	t.Parallel()

	root := writeTree(t, parserProject())
	got := extract(t, root, "prose-desc", map[string]string{"path": "src/Parser.kt"})

	want := "Parser module: handles source code parsing and AST construction. " +
		"Supports incremental parsing and error recovery."
	if got != want {
		t.Errorf("prose-desc = %q, want %q", got, want)
	}
	for _, unwanted := range []string{"### `Parser`", "A source code parser with incremental support"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("prose-desc = %q, should not carry %q", got, unwanted)
		}
	}
}

func TestProseDescErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		attrs map[string]string
		want  string
	}{
		{
			name:  "no path",
			attrs: map[string]string{"path": ""},
			want:  "> *[selfdoc: :::prose-desc requires a file path argument]*",
		},
		{
			name:  "file carries no module doc",
			files: map[string]string{"NoDoc.kt": "package x\n\nfun foo() {}\n"},
			attrs: map[string]string{"path": "NoDoc.kt"},
			want:  "> *[selfdoc: no module doc comment found in 'NoDoc.kt']*",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			if got := extract(t, root, "prose-desc", test.attrs); got != test.want {
				t.Errorf("prose-desc = %q, want %q", got, test.want)
			}
		})
	}
}

// configSource is the data-class file the schema tests read.
const configSource = `/**
 * Configuration for the parser system.
 *
 * @property bufferSize The maximum number of tokens to buffer.
 * @property strictMode Whether to enable strict mode.
 * @property outputFormat The output format for results.
 */
data class ParserConfig(
    val bufferSize: Int = 1024,
    val strictMode: Boolean = false,
    val outputFormat: String = "json"
)

data class NetworkConfig(
    val host: String = "localhost",
    val port: Int = 8080,
    val timeout: Double = 30.0
)
`

func TestTableSchemaTarget(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{"Config.kt": configSource})
	got := extract(t, root, "table-schema", map[string]string{
		"path": "Config.kt", "target": "ParserConfig",
	})

	want := "| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `bufferSize` | `Int` | `1024` | The maximum number of tokens to buffer. |\n" +
		"| `strictMode` | `Boolean` | `false` | Whether to enable strict mode. |\n" +
		"| `outputFormat` | `String` | `\"json\"` | The output format for results. |"
	if got != want {
		t.Errorf("table-schema\n got: %q\nwant: %q", got, want)
	}
}

func TestTableSchemaEveryDataClass(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{"Config.kt": configSource})
	got := extract(t, root, "table-schema", map[string]string{"path": "Config.kt"})

	want := "### `ParserConfig`\n\nConfiguration for the parser system.\n\n" +
		"**Properties:**\n\n" +
		"- `bufferSize`: The maximum number of tokens to buffer.\n" +
		"- `strictMode`: Whether to enable strict mode.\n" +
		"- `outputFormat`: The output format for results.\n\n\n\n" +
		"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `bufferSize` | `Int` | `1024` | The maximum number of tokens to buffer. |\n" +
		"| `strictMode` | `Boolean` | `false` | Whether to enable strict mode. |\n" +
		"| `outputFormat` | `String` | `\"json\"` | The output format for results. |\n" +
		"### `NetworkConfig`\n\n" +
		"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `host` | `String` | `\"localhost\"` |  |\n" +
		"| `port` | `Int` | `8080` |  |\n" +
		"| `timeout` | `Double` | `30.0` |  |"
	if got != want {
		t.Errorf("table-schema\n got: %q\nwant: %q", got, want)
	}
}

func TestTableSchemaErrorsAndDelegation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		attrs map[string]string
		want  string
	}{
		{
			name:  "no path",
			attrs: map[string]string{"path": ""},
			want:  "> *[selfdoc: :::table-schema requires a file path argument]*",
		},
		{
			name:  "missing file",
			attrs: map[string]string{"path": "Config.kt"},
			want:  "> *[selfdoc: file 'Config.kt' not found]*",
		},
		{
			name:  "no data class in the file",
			files: map[string]string{"Config.kt": "class Config(val host: String)\n"},
			attrs: map[string]string{"path": "Config.kt"},
			want:  "> *[selfdoc: no data class types found in 'Config.kt']*",
		},
		{
			name:  "data class not found",
			files: map[string]string{"Config.kt": configSource},
			attrs: map[string]string{"path": "Config.kt", "target": "Missing"},
			want:  "> *[selfdoc: data class 'Missing' not found in 'Config.kt']*",
		},
		{
			name:  "a json path is a config file",
			files: map[string]string{"conf.json": `{"a": 1, "b": "two"}`},
			attrs: map[string]string{"path": "conf.json"},
			want: "| Key | Type | Value |\n| --- | --- | --- |\n" +
				"| `a` | integer | `1` |\n| `b` | string | `\"two\"` |",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			if got := extract(t, root, "table-schema", test.attrs); got != test.want {
				t.Errorf("table-schema = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUnknownDirective(t *testing.T) {
	t.Parallel()

	got := extract(t, t.TempDir(), "code-help", map[string]string{"path": "test.kt"})
	want := "> *[selfdoc: unknown directive 'code-help' for kotlin extractor]*"
	if got != want {
		t.Errorf("unknown directive = %q, want %q", got, want)
	}
}

func TestParseKDocTags(t *testing.T) {
	t.Parallel()

	input := "A summary line.\n" +
		"\n" +
		"Use [Parser] and see [the parser][Parser].\n" +
		"@param source The source code\n" +
		"   spanning lines.\n" +
		"@param[mode] The mode\n" +
		"@property name The name of the entity.\n" +
		"@return The parsed AST\n" +
		"@throws ParseException if malformed.\n" +
		"@exception IOException\n" +
		"@constructor Creates a new parser instance.\n" +
		"@receiver The string to parse.\n" +
		"@sample com.example.ParserTest.testParse\n" +
		"@see Parser\n" +
		"@author Jane Doe\n" +
		"@since 1.0\n" +
		"@suppress\n" +
		"Trailing prose."

	want := "A summary line.\n\n" +
		"Use `Parser` and see the parser (`Parser`).\n\n" +
		"**Parameters:**\n\n" +
		"- `source`: The source code spanning lines.\n" +
		"- `mode`: The mode\n\n" +
		"**Properties:**\n\n" +
		"- `name`: The name of the entity.\n\n" +
		"**Returns:** The parsed AST\n" +
		"**Throws:** `ParseException` if malformed.\n" +
		"**Throws:** `IOException`\n" +
		"**Constructor:** Creates a new parser instance.\n" +
		"**Receiver:** The string to parse.\n" +
		"**Sample:** `com.example.ParserTest.testParse`\n" +
		"**See:** `Parser`\n" +
		"**Author:** Jane Doe\n" +
		"**Since:** 1.0\n" +
		"Trailing prose."

	if got := parseKDoc(input); got != want {
		t.Errorf("parseKDoc\n got: %q\nwant: %q", got, want)
	}
}

func TestParseKDocEmpty(t *testing.T) {
	t.Parallel()

	if got := parseKDoc(""); got != "" {
		t.Errorf("parseKDoc(\"\") = %q, want the empty string", got)
	}
}

func TestModuleDocstring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "multi-line block",
			source: "/**\n * Parser module for Kotlin sources.\n" +
				" * Handles incremental parsing.\n */\n\npackage com.example\n",
			want: "Parser module for Kotlin sources.\nHandles incremental parsing.",
		},
		{
			name:   "one-line block",
			source: "/** Parser module. */\npackage com.example\n",
			want:   "Parser module.",
		},
		{
			name:   "no module doc",
			source: "package com.example\n\n/** A type. */\nclass Foo\n",
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"Mod.kt": test.source})
			got, err := newExtractor().ModuleDocstring(filepath.Join(root, "Mod.kt"))
			if err != nil {
				t.Fatalf("ModuleDocstring: %v", err)
			}
			if got != test.want {
				t.Errorf("ModuleDocstring = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSymbolDetails(t *testing.T) {
	t.Parallel()

	type wantParam struct {
		name       string
		paramType  string
		documented bool
	}

	tests := []struct {
		name             string
		source           string
		symbol           string
		wantNil          bool
		params           []wantParam
		returnType       string
		returnDocumented bool
	}{
		{
			name: "every parameter documented",
			source: "/**\n * Calculate the sum.\n * @param x First number\n" +
				" * @param y Second number\n * @return The sum\n */\n" +
				"fun calculate(x: Int, y: Double): Double {\n    return x + y\n}\n",
			symbol: "calculate",
			params: []wantParam{
				{"x", "Int", true},
				{"y", "Double", true},
			},
			returnType:       "Double",
			returnDocumented: true,
		},
		{
			name: "some parameters undocumented",
			source: "/**\n * Process items.\n * @param items The list to process\n */\n" +
				"fun process(items: List<String>, verbose: Boolean = false): Int {\n" +
				"    return items.size\n}\n",
			symbol: "process",
			params: []wantParam{
				{"items", "List<String>", true},
				{"verbose", "Boolean", false},
			},
			returnType: "Int",
		},
		{
			name: "a data class answers with its primary constructor",
			source: "/**\n * A user record.\n * @property name The user's name\n" +
				" * @property age The user's age\n */\n" +
				"data class User(val name: String, val age: Int, val email: String)\n",
			symbol: "User",
			params: []wantParam{
				{"name", "String", true},
				{"age", "Int", true},
				{"email", "String", false},
			},
			returnDocumented: true,
		},
		{
			name:       "no parameters",
			source:     "/** Get the current timestamp. */\nfun now(): Long = System.currentTimeMillis()\n",
			symbol:     "now",
			returnType: "Long",
		},
		{
			name:    "unknown symbol",
			source:  "fun something(): Unit {}\n",
			symbol:  "nonexistent",
			wantNil: true,
		},
		{
			name: "dotted name selects a class method",
			source: "class UserService {\n    /**\n     * Find a user by ID.\n" +
				"     * @param id The user ID\n" +
				"     * @return The user, or null if not found\n     */\n" +
				"    fun findUser(id: Int): User? {\n        return null\n    }\n}\n",
			symbol:           "UserService.findUser",
			params:           []wantParam{{"id", "Int", true}},
			returnType:       "User?",
			returnDocumented: true,
		},
		{
			name: "dotted name selects a data class method",
			source: "data class Repo(val name: String) {\n" +
				"    fun fullName(org: String): String {\n        return \"x\"\n    }\n}\n",
			symbol:     "Repo.fullName",
			params:     []wantParam{{"org", "String", false}},
			returnType: "String",
		},
		{
			name: "dotted name selects an object method",
			source: "object Factory {\n    fun create(name: String): Item {\n" +
				"        return Item(name)\n    }\n}\n",
			symbol:     "Factory.create",
			params:     []wantParam{{"name", "String", false}},
			returnType: "Item",
		},
		{
			name:       "dotted name selects an interface method",
			source:     "interface Service {\n    fun execute(command: String): Boolean\n}\n",
			symbol:     "Service.execute",
			params:     []wantParam{{"command", "String", false}},
			returnType: "Boolean",
		},
		{
			name: "dotted name selects a sealed class method",
			source: "sealed class Result {\n    fun describe(): String {\n" +
				"        return \"result\"\n    }\n}\n",
			symbol:     "Result.describe",
			returnType: "String",
		},
		{
			name:    "dotted name with an unknown member",
			source:  "class Empty {\n}\n",
			symbol:  "Empty.missing",
			wantNil: true,
		},
		{
			name:    "dotted name with an unknown type",
			source:  "fun standalone(): Unit {}\n",
			symbol:  "Missing.method",
			wantNil: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"Main.kt": test.source})
			got, err := newExtractor().SymbolDetails(filepath.Join(root, "Main.kt"), test.symbol)
			if err != nil {
				t.Fatalf("SymbolDetails: %v", err)
			}
			if test.wantNil {
				if got != nil {
					t.Fatalf("SymbolDetails = %+v, want nothing", got)
				}
				return
			}
			if got == nil {
				t.Fatal("SymbolDetails = nothing, want details")
			}
			if len(got.Params) != len(test.params) {
				t.Fatalf("SymbolDetails params = %+v, want %d", got.Params, len(test.params))
			}
			for i, want := range test.params {
				param := got.Params[i]
				if param.Name != want.name {
					t.Errorf("param %d name = %q, want %q", i, param.Name, want.name)
				}
				if want.paramType == "" {
					if param.Type != nil {
						t.Errorf("param %d type = %q, want none", i, *param.Type)
					}
				} else if param.Type == nil || *param.Type != want.paramType {
					t.Errorf("param %d type = %v, want %q", i, param.Type, want.paramType)
				}
				if param.Documented != want.documented {
					t.Errorf("param %d documented = %v, want %v", i, param.Documented, want.documented)
				}
			}
			if test.returnType == "" {
				if got.ReturnType != nil {
					t.Errorf("return type = %q, want none", *got.ReturnType)
				}
			} else if got.ReturnType == nil || *got.ReturnType != test.returnType {
				t.Errorf("return type = %v, want %q", got.ReturnType, test.returnType)
			}
			if got.ReturnDocumented != test.returnDocumented {
				t.Errorf("return documented = %v, want %v", got.ReturnDocumented, test.returnDocumented)
			}
		})
	}
}

// contains reports whether values carries want.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
