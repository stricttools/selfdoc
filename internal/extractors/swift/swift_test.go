package swift

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

// parserSource is the sample file the symbol, ref and details tests read: a
// module doc, a public class with documented members, an open class, an enum, a
// protocol, a typealias, two properties, an actor, and two members whose
// visibility keeps them off the page.
const parserSource = `/// Parser module: handles source code parsing and AST construction.
/// Supports incremental parsing and error recovery.

import Foundation

/// A source code parser with incremental support.
///
/// Use ` + "``Parser``" + ` to parse source files into an AST.
/// - Note: Thread-safe for read operations only.
public class Parser {
    /// The source text being parsed.
    public var sourceText: String = ""

    /// Whether to enable error recovery.
    public let errorRecovery: Bool = true

    /// Parse the given source text into an AST.
    ///
    /// - Parameter source: The source code to parse.
    /// - Returns: The parsed AST root node.
    /// - Throws: ` + "`ParseError`" + ` if the source is malformed.
    public func parse(source: String) -> ASTNode {
        return ASTNode()
    }

    /// Configure the parser with options.
    ///
    /// - Parameters:
    ///   - mode: The parsing mode to use.
    ///   - flags: Additional compiler flags.
    /// - Returns: The configured parser instance.
    public static func configure(mode: ParseMode, flags: [String]) -> Parser {
        return Parser()
    }

    private func internalHelper() {}
    func defaultAccessHelper() {}
}

/// An open base class for AST visitors.
open class ASTVisitor {
    /// Visit a node in the AST.
    open func visit(node: ASTNode) {}
}

/// All possible token types.
public enum TokenType {
    case identifier
}

/// The parser protocol that all parsers conform to.
public protocol Parseable {
    func parse() -> ASTNode
}

/// A type alias for parse results.
public typealias ParseResult = Result<ASTNode, ParseError>

/// Maximum parse depth allowed.
public let maxParseDepth: Int = 256

/// Current parser version.
public static var parserVersion: String = "1.0"

public actor ParseCoordinator {
    public func coordinate() async {}
}
`

// parserProject is parserSource in a Swift package.
func parserProject() map[string]string {
	return map[string]string{
		"Package.swift":        "// swift-tools-version: 5.9\n",
		"Sources/Parser.swift": parserSource,
		"Sources/Config.swift": configSource,
		"Sources/Blank.swift":  blankLineSource,
	}
}

// configSource is the struct file the schema tests read.
const configSource = `/// Configuration for the parser system.
public struct ParserConfig {
    /// The maximum number of tokens to buffer.
    var bufferSize: Int = 1024
    /// Whether to enable strict mode.
    var strictMode: Bool = false
    /// The output format for results.
    var outputFormat: String = "json"
    let version: Int = 1
}

public struct NetworkConfig: Codable {
    var host: String = "localhost"
    var port: Int = 8080
    var timeout: Double = 30.0
}
`

// blankLineSource is the file that shows a doc comment separated from its
// declaration documents nothing.
const blankLineSource = `/// This comment is for the function below.
public func attached() {}

/// This comment is NOT for the function below.

public func detached() {}
`

func TestNameAndFileExtensions(t *testing.T) {
	t.Parallel()

	extractor := newExtractor()
	if got := extractor.Name(); got != "swift" {
		t.Errorf("Name() = %q, want swift", got)
	}
	exts := extractor.FileExtensions()
	if len(exts) != 1 || exts[0] != ".swift" {
		t.Errorf("FileExtensions() = %v, want [.swift]", exts)
	}
}

func TestRegisteredFactory(t *testing.T) {
	t.Parallel()

	extractor, ok, err := extractors.Lookup("swift")
	if err != nil || !ok {
		t.Fatalf("Lookup(swift) = %v, %v, %v", extractor, ok, err)
	}
	if extractor.Name() != "swift" {
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
		{"package manifest", map[string]string{"Package.swift": "// swift-tools-version: 5.9\n"}, true},
		{"no manifest", map[string]string{"main.swift": "print(\"x\")\n"}, false},
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
	symbols, err := newExtractor().PublicSymbols(filepath.Join(root, "Sources", "Parser.swift"))
	if err != nil {
		t.Fatalf("PublicSymbols: %v", err)
	}

	// The scanner reads declarations line by line, so a public member of a
	// public type is listed beside the type itself.
	want := []string{
		"Parser", "sourceText", "errorRecovery", "parse", "configure",
		"ASTVisitor", "visit", "TokenType", "Parseable", "ParseResult",
		"maxParseDepth", "parserVersion", "ParseCoordinator", "coordinate",
	}
	if len(symbols) != len(want) {
		t.Fatalf("PublicSymbols = %v, want %v", symbols, want)
	}
	for i, name := range want {
		if symbols[i] != name {
			t.Errorf("symbol %d = %q, want %q", i, symbols[i], name)
		}
	}

	// A private member and one with Swift's default visibility are not public.
	for _, unwanted := range []string{"internalHelper", "defaultAccessHelper"} {
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
			name:   "static and class methods",
			source: "public static func staticMethod() {}\npublic class func classMethod() {}\n",
			want:   []string{"staticMethod", "classMethod"},
		},
		{
			name:   "a trailing comment is not part of the declaration",
			source: "public let timeout: Int = 30 // seconds\n",
			want:   []string{"timeout"},
		},
		{
			name:   "a comment line declares nothing",
			source: "// public func commented() {}\n",
			want:   nil,
		},
		{
			name:   "empty file",
			source: "",
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"Main.swift": test.source})
			symbols, err := newExtractor().PublicSymbols(filepath.Join(root, "Main.swift"))
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

	symbols, err := newExtractor().PublicSymbols(filepath.Join(t.TempDir(), "nonexistent.swift"))
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
		{name: "swift file", pathArg: "Sources/Parser.swift", wantSuffix: "Parser.swift"},
		{name: "directory of swift files", pathArg: "Sources", wantSuffix: "Sources"},
		{
			name: "through a source path", pathArg: "Parser.swift",
			sourcePaths: []string{"Sources/"}, wantSuffix: "Parser.swift",
		},
		{name: "implicit extension", pathArg: "Sources/Parser", wantSuffix: "Parser.swift"},
		{name: "not found", pathArg: "nonexistent.swift", wantEmpty: true},
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
	got := extract(t, root, "ref", map[string]string{"path": "Sources/Parser.swift"})

	want := "## `Sources/Parser.swift`\n\n" +
		"Parser module: handles source code parsing and AST construction. " +
		"Supports incremental parsing and error recovery.\n\n" +
		"### `Parser`\n\n```swift\npublic class Parser\n```\n\n" +
		"A source code parser with incremental support.\n\n" +
		"Use `Parser` to parse source files into an AST.\n" +
		"**Note:** Thread-safe for read operations only.\n\n" +
		"### `ASTVisitor`\n\n```swift\nopen class ASTVisitor\n```\n\n" +
		"An open base class for AST visitors.\n\n" +
		"### `TokenType`\n\n```swift\npublic enum TokenType\n```\n\n" +
		"All possible token types.\n\n" +
		"### `Parseable`\n\n```swift\npublic protocol Parseable\n```\n\n" +
		"The parser protocol that all parsers conform to.\n\n" +
		"### `ParseResult`\n\n```swift\n" +
		"public typealias ParseResult = Result<ASTNode, ParseError>\n```\n\n" +
		"A type alias for parse results.\n\n" +
		"### `ParseCoordinator`\n\n```swift\npublic actor ParseCoordinator\n```\n\n" +
		"### `parse`\n\n```swift\npublic func parse(source: String) -> ASTNode\n```\n\n" +
		"Parse the given source text into an AST.\n\n" +
		"**Parameters:**\n\n- `source`: The source code to parse.\n\n" +
		"**Returns:** The parsed AST root node.\n" +
		"**Throws:** `ParseError` if the source is malformed.\n\n" +
		"### `configure`\n\n```swift\n" +
		"public static func configure(mode: ParseMode, flags: [String]) -> Parser\n```\n\n" +
		"Configure the parser with options.\n\n" +
		"**Parameters:**\n\n- `mode`: The parsing mode to use.\n- `flags`: Additional compiler flags.\n\n" +
		"**Returns:** The configured parser instance.\n\n" +
		"### `visit`\n\n```swift\nopen func visit(node: ASTNode)\n```\n\n" +
		"Visit a node in the AST.\n\n" +
		"### `coordinate`\n\n```swift\npublic func coordinate() async\n```\n\n" +
		"### `sourceText`\n\n```swift\npublic var sourceText: String = \"\"\n```\n\n" +
		"The source text being parsed.\n\n" +
		"### `errorRecovery`\n\n```swift\npublic let errorRecovery: Bool = true\n```\n\n" +
		"Whether to enable error recovery.\n\n" +
		"### `maxParseDepth`\n\n```swift\npublic let maxParseDepth: Int = 256\n```\n\n" +
		"Maximum parse depth allowed.\n\n" +
		"### `parserVersion`\n\n```swift\npublic static var parserVersion: String = \"1.0\"\n```\n\n" +
		"Current parser version."

	if got != want {
		t.Errorf("ref output\n got: %q\nwant: %q", got, want)
	}
	for _, unwanted := range []string{"internalHelper", "defaultAccessHelper"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("ref rendered %q, which is not public", unwanted)
		}
	}
}

func TestRefWithTarget(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"Core.swift": "/// Initializes the parser.\n" +
			"public func initialize() {}\n\n" +
			"/// Parses the input.\n" +
			"public func parse(_ input: String) -> Bool {\n    return true\n}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "Core.swift", "target": "parse"})

	want := "### `parse`\n\n```swift\npublic func parse(_ input: String) -> Bool\n```\n\n" +
		"Parses the input."
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
			attrs: map[string]string{"path": "nonexistent.swift"},
			want:  "> *[selfdoc: 'nonexistent.swift' not found]*",
		},
		{
			name:  "target not found",
			files: map[string]string{"Core.swift": "public func foo() {}\n"},
			attrs: map[string]string{"path": "Core.swift", "target": "nonexistent"},
			want:  "> *[selfdoc: symbol 'nonexistent' not found in 'Core.swift']*",
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
		"Sources/Alpha.swift": "/// The alpha module.\npublic func alpha() {}\n",
		"Sources/Beta.swift":  "public func beta() {}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "Sources"})

	// The module doc of the first file heads the section, and the same comment
	// is also the declaration's own doc.
	want := "## `Sources`\n\nThe alpha module.\n\n" +
		"### `alpha`\n\n```swift\npublic func alpha()\n```\n\nThe alpha module.\n\n" +
		"### `beta`\n\n```swift\npublic func beta()\n```"
	if got != want {
		t.Errorf("ref on a directory\n got: %q\nwant: %q", got, want)
	}
}

func TestNoDocAssociationAcrossBlankLine(t *testing.T) {
	t.Parallel()

	root := writeTree(t, parserProject())
	got := extract(t, root, "ref", map[string]string{"path": "Sources/Blank.swift"})

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
	got := extract(t, root, "prose-desc", map[string]string{"path": "Sources/Parser.swift"})

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
			files: map[string]string{"NoDoc.swift": "import Foundation\n\npublic func foo() {}\n"},
			attrs: map[string]string{"path": "NoDoc.swift"},
			want:  "> *[selfdoc: no module doc comment found in 'NoDoc.swift']*",
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

func TestTableSchemaTarget(t *testing.T) {
	t.Parallel()

	root := writeTree(t, parserProject())
	got := extract(t, root, "table-schema", map[string]string{
		"path": "Sources/Config.swift", "target": "ParserConfig",
	})

	want := "| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `bufferSize` | `Int` | `1024` | The maximum number of tokens to buffer. |\n" +
		"| `strictMode` | `Bool` | `false` | Whether to enable strict mode. |\n" +
		"| `outputFormat` | `String` | `\"json\"` | The output format for results. |\n" +
		"| `version` | `Int` | `1` |  |"
	if got != want {
		t.Errorf("table-schema\n got: %q\nwant: %q", got, want)
	}
}

func TestTableSchemaEveryStruct(t *testing.T) {
	t.Parallel()

	root := writeTree(t, parserProject())
	got := extract(t, root, "table-schema", map[string]string{"path": "Sources/Config.swift"})

	want := "### `ParserConfig`\n\nConfiguration for the parser system.\n\n" +
		"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `bufferSize` | `Int` | `1024` | The maximum number of tokens to buffer. |\n" +
		"| `strictMode` | `Bool` | `false` | Whether to enable strict mode. |\n" +
		"| `outputFormat` | `String` | `\"json\"` | The output format for results. |\n" +
		"| `version` | `Int` | `1` |  |\n" +
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
			attrs: map[string]string{"path": "Config.swift"},
			want:  "> *[selfdoc: file 'Config.swift' not found]*",
		},
		{
			name:  "no public struct in the file",
			files: map[string]string{"Config.swift": "struct Config {\n    var host: String = \"x\"\n}\n"},
			attrs: map[string]string{"path": "Config.swift"},
			want:  "> *[selfdoc: no struct types found in 'Config.swift']*",
		},
		{
			name:  "struct not found",
			files: map[string]string{"Config.swift": configSource},
			attrs: map[string]string{"path": "Config.swift", "target": "Missing"},
			want:  "> *[selfdoc: struct 'Missing' not found in 'Config.swift']*",
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

	got := extract(t, t.TempDir(), "code-help", map[string]string{"path": "test.swift"})
	want := "> *[selfdoc: unknown directive 'code-help' for swift extractor]*"
	if got != want {
		t.Errorf("unknown directive = %q, want %q", got, want)
	}
}

func TestParseDocComment(t *testing.T) {
	t.Parallel()

	input := "A summary.\n" +
		"\n" +
		"Use ``Parser`` here.\n" +
		"- Parameter source: The source code\n" +
		"  spanning lines.\n" +
		"- Parameters:\n" +
		"  - mode: The parsing mode to use.\n" +
		"  - flags: Additional compiler flags.\n" +
		"- Returns: The parsed AST root node.\n" +
		"- Throws: `ParseError` if malformed.\n" +
		"- Note: Thread-safe for read operations only.\n" +
		"- Warning: Do not use in production.\n" +
		"- notAKeyword: stays a list item.\n" +
		"Trailing prose."

	want := "A summary.\n\nUse `Parser` here.\n\n" +
		"**Parameters:**\n\n" +
		"- `source`: The source code spanning lines.\n" +
		"- `mode`: The parsing mode to use.\n" +
		"- `flags`: Additional compiler flags.\n\n" +
		"**Returns:** The parsed AST root node.\n" +
		"**Throws:** `ParseError` if malformed.\n" +
		"**Note:** Thread-safe for read operations only.\n" +
		"**Warning:** Do not use in production.\n" +
		"- notAKeyword: stays a list item.\n" +
		"Trailing prose."

	if got := parseDocComment(input); got != want {
		t.Errorf("parseDocComment\n got: %q\nwant: %q", got, want)
	}
}

func TestParseDocCommentEmpty(t *testing.T) {
	t.Parallel()

	if got := parseDocComment(""); got != "" {
		t.Errorf("parseDocComment(\"\") = %q, want the empty string", got)
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
			name: "the run of comments at the top",
			source: "/// Parser module for source code.\n" +
				"/// Handles incremental parsing.\n\nimport Foundation\n",
			want: "Parser module for source code.\nHandles incremental parsing.",
		},
		{
			name:   "code before any comment",
			source: "import Foundation\n\n/// A type.\npublic struct Foo {}\n",
			want:   "",
		},
		{
			name:   "a regular comment does not end the search",
			source: "// swift-tools-version\n/// The module.\nimport Foundation\n",
			want:   "The module.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"Mod.swift": test.source})
			got, err := newExtractor().ModuleDocstring(filepath.Join(root, "Mod.swift"))
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
		files            map[string]string
		file             string
		symbol           string
		wantNil          bool
		params           []wantParam
		returnType       string
		returnDocumented bool
	}{
		{
			name:   "the individual parameter syntax documents its parameter",
			files:  parserProject(),
			file:   "Sources/Parser.swift",
			symbol: "parse",
			params: []wantParam{
				{"source", "String", true},
			},
			returnType:       "ASTNode",
			returnDocumented: true,
		},
		{
			name:   "the parameters block documents each sub-item",
			files:  parserProject(),
			file:   "Sources/Parser.swift",
			symbol: "configure",
			params: []wantParam{
				{"mode", "ParseMode", true},
				{"flags", "[String]", true},
			},
			returnType:       "Parser",
			returnDocumented: true,
		},
		{
			name:    "unknown symbol",
			files:   parserProject(),
			file:    "Sources/Parser.swift",
			symbol:  "nonexistent",
			wantNil: true,
		},
		{
			name: "a function with no doc comment documents nothing",
			files: map[string]string{"Funcs.swift": "public func process(input: Data, count: Int) -> Bool {\n" +
				"    return true\n}\n"},
			file:   "Funcs.swift",
			symbol: "process",
			params: []wantParam{
				{"input", "Data", false},
				{"count", "Int", false},
			},
			returnType: "Bool",
		},
		{
			name:   "no parameters and no return type",
			files:  map[string]string{"Empty.swift": "public func doSomething() {\n}\n"},
			file:   "Empty.swift",
			symbol: "doSomething",
		},
		{
			name: "an external label leaves the internal name",
			files: map[string]string{"Labels.swift": "/// - Parameter value: The value.\n" +
				"public func set(for value: Int, _ name: String) -> Void {\n}\n"},
			file:   "Labels.swift",
			symbol: "set",
			params: []wantParam{
				{"value", "Int", true},
				{"name", "String", false},
			},
			returnType: "Void",
		},
		{
			name: "dotted name selects a method of a type",
			files: map[string]string{"Router.swift": "/// A network router.\n" +
				"class Router {\n" +
				"    /// Handle an incoming request.\n" +
				"    ///\n" +
				"    /// - Parameter request: The URL request to handle.\n" +
				"    /// - Returns: The response for this request.\n" +
				"    func handle(request: URLRequest) -> Response {\n" +
				"        return Response()\n    }\n\n" +
				"    func other() {}\n}\n"},
			file:             "Router.swift",
			symbol:           "Router.handle",
			params:           []wantParam{{"request", "URLRequest", true}},
			returnType:       "Response",
			returnDocumented: true,
		},
		{
			name:    "dotted name with an unknown member",
			files:   map[string]string{"Router.swift": "class Router {\n    func other() {}\n}\n"},
			file:    "Router.swift",
			symbol:  "Router.handle",
			wantNil: true,
		},
		{
			name:    "dotted name with an unknown type",
			files:   map[string]string{"Router.swift": "public func standalone() {}\n"},
			file:    "Router.swift",
			symbol:  "Missing.handle",
			wantNil: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			got, err := newExtractor().SymbolDetails(filepath.Join(root, test.file), test.symbol)
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
