package dart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// writeTree writes a file tree under a fresh temporary directory and answers
// its root. Every fixture in this file is written this way, so no test reads
// anything it did not write.
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

// The sample project the symbol tests read, one file carrying every kind of
// declaration Dart writes plus the two generated files that must be refused.
var sampleProject = map[string]string{
	"pubspec.yaml": "name: my_package\nversion: 1.0.0\n",
	"lib/my_package.dart": `/// The main library for my_package.
library my_package;

abstract class Animal {
  String get name;
  void speak();
}

class Dog extends Animal {
  @override
  String get name => 'Dog';
  @override
  void speak() => print('Woof!');
}

sealed class Shape {}
class Circle extends Shape {}

base class Base {}
interface class Interface {}
final class Final {}
mixin class MixinClass {}
abstract base class AbstractBase {}
abstract interface class AbstractInterface {}
abstract mixin class AbstractMixin {}
base mixin class BaseMixinClass {}

mixin Serializable {
  Map<String, dynamic> toJson();
}

base mixin BaseMixin {}

enum Color { red, green, blue }

extension type Meters(double value) {}

typedef StringCallback = void Function(String);

void greet(String name) {
  print('Hello, \$name');
}

Future<List<int>> fetchNumbers() async {
  return [1, 2, 3];
}

const maxRetries = 3;
final defaultName = 'World';
var counter = 0;

String _privateHelper() => '';
int _privateVar = 0;
class _PrivateClass {}
`,
	"lib/my_package.g.dart":       "// GENERATED CODE - DO NOT MODIFY BY HAND\nclass GeneratedClass {}\n",
	"lib/my_package.freezed.dart": "// GENERATED CODE - DO NOT MODIFY BY HAND\nclass FreezedClass {}\n",
}

func TestNameAndFileExtensions(t *testing.T) {
	t.Parallel()

	extractor := newExtractor()
	if got := extractor.Name(); got != "dart" {
		t.Errorf("Name() = %q, want dart", got)
	}
	exts := extractor.FileExtensions()
	if len(exts) != 1 || exts[0] != ".dart" {
		t.Errorf("FileExtensions() = %v, want [.dart]", exts)
	}
}

func TestRegisteredFactory(t *testing.T) {
	t.Parallel()

	extractor, ok, err := extractors.Lookup("dart")
	if err != nil || !ok {
		t.Fatalf("Lookup(dart) = %v, %v, %v", extractor, ok, err)
	}
	if extractor.Name() != "dart" {
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
		{"pubspec present", map[string]string{"pubspec.yaml": "name: x\n"}, true},
		{"empty directory", map[string]string{}, false},
		{"only dart files", map[string]string{"lib.dart": "class A {}\n"}, false},
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

	tests := []struct {
		name      string
		files     map[string]string
		file      string
		want      []string
		wantNot   []string
		wantEmpty bool
	}{
		{
			name:  "every declaration kind",
			files: sampleProject,
			file:  "lib/my_package.dart",
			want: []string{
				"Animal", "Dog", "Shape", "Circle",
				"Base", "Interface", "Final", "MixinClass",
				"AbstractBase", "AbstractInterface", "AbstractMixin", "BaseMixinClass",
				"Serializable", "BaseMixin", "Color", "Meters", "StringCallback",
				"greet", "fetchNumbers", "maxRetries", "defaultName", "counter",
			},
			wantNot: []string{"_privateHelper", "_privateVar", "_PrivateClass"},
		},
		{
			name:      "generated file",
			files:     sampleProject,
			file:      "lib/my_package.g.dart",
			wantEmpty: true,
		},
		{
			name:      "freezed generated file",
			files:     sampleProject,
			file:      "lib/my_package.freezed.dart",
			wantEmpty: true,
		},
		{
			name: "class members are not top level",
			files: map[string]string{"test.dart": `class MyClass {
  void memberMethod() {}
  static void staticMethod() {}
  final int memberVar = 0;
}

void topLevelFunc() {}
`},
			file:    "test.dart",
			want:    []string{"MyClass", "topLevelFunc"},
			wantNot: []string{"memberMethod", "staticMethod", "memberVar"},
		},
		{
			name: "block comment contents are not declarations",
			files: map[string]string{"test.dart": `/* This is a block comment
class CommentedClass {}
*/

class RealClass {}
`},
			file:    "test.dart",
			want:    []string{"RealClass"},
			wantNot: []string{"CommentedClass"},
		},
		{
			name:      "empty file",
			files:     map[string]string{"empty.dart": ""},
			file:      "empty.dart",
			wantEmpty: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			symbols, err := newExtractor().PublicSymbols(filepath.Join(root, test.file))
			if err != nil {
				t.Fatalf("PublicSymbols: %v", err)
			}
			if test.wantEmpty && len(symbols) != 0 {
				t.Fatalf("PublicSymbols = %v, want none", symbols)
			}
			for _, want := range test.want {
				if !contains(symbols, want) {
					t.Errorf("PublicSymbols = %v, missing %q", symbols, want)
				}
			}
			for _, unwanted := range test.wantNot {
				if contains(symbols, unwanted) {
					t.Errorf("PublicSymbols = %v, should not carry %q", symbols, unwanted)
				}
			}
		})
	}
}

func TestPublicSymbolsMissingFile(t *testing.T) {
	t.Parallel()

	symbols, err := newExtractor().PublicSymbols(filepath.Join(t.TempDir(), "nonexistent.dart"))
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
		{name: "dart file", pathArg: "lib/my_package.dart", wantSuffix: "my_package.dart"},
		{name: "directory of dart files", pathArg: "lib", wantSuffix: "lib"},
		{
			name: "through a source path", pathArg: "my_package.dart",
			sourcePaths: []string{"lib/"}, wantSuffix: "my_package.dart",
		},
		{name: "implicit extension", pathArg: "lib/my_package", wantSuffix: "my_package.dart"},
		{name: "not found", pathArg: "nonexistent.dart", wantEmpty: true},
		{
			name: "package import", pathArg: "package:my_package/my_package.dart",
			wantSuffix: filepath.Join("lib", "my_package.dart"),
		},
		{name: "package import, wrong package", pathArg: "package:wrong_name/my_package.dart", wantEmpty: true},
		{name: "package import, missing file", pathArg: "package:my_package/nonexistent.dart", wantEmpty: true},
		{
			name: "package import into a subdirectory", pathArg: "package:my_package/src/helper.dart",
			wantSuffix: "helper.dart",
		},
	}

	files := map[string]string{"lib/src/helper.dart": "class Helper {}\n"}
	for name, content := range sampleProject {
		files[name] = content
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, files)
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

// refFixture is the file every whole-output test reads, carrying a library doc,
// a class with a heading in its doc, a documented function, an enum, a typedef
// and two top-level values.
var refFixture = map[string]string{
	"pubspec.yaml": "name: test\n",
	"lib.dart": `/// The main library.
/// Provides utilities for testing.
library my_lib;

/// A foo class.
///
/// # Usage
/// See [Bar] and [docs](https://x).
class Foo {}

/// Makes a thing.
///
/// Args:
///     name: the name
///     count (int): how many
///
/// Returns:
///     the thing
String make(String name, int count) => name;

enum Color { red, green }

typedef Cb = void Function();

const maxRetries = 3;
var counter = 0;
`,
}

func TestRefRendersWholeLibrary(t *testing.T) {
	t.Parallel()

	root := writeTree(t, refFixture)
	got := extract(t, root, "ref", map[string]string{"path": "lib.dart"})

	want := "## `lib.dart`\n\nThe main library. Provides utilities for testing.\n\n" +
		"A foo class.\n\n### Usage\nSee `Bar` and [docs](https://x).\n\n" +
		"### `Foo`\n\n```dart\nclass Foo\n```\n\n" +
		"A foo class.\n\n#### Usage\nSee `Bar` and [docs](https://x).\n\n" +
		"### `Color`\n\n```dart\nenum Color\n```\n\n" +
		"### `Cb`\n\n```dart\ntypedef Cb = void Function()\n```\n\n" +
		"### `maxRetries`\n\n```dart\nconst maxRetries = 3\n```\n\n" +
		"### `counter`\n\n```dart\nvar counter = 0\n```\n\n" +
		"### `make`\n\n```dart\nString make(String name, int count) => name\n```\n\n" +
		"Makes a thing.\n\n**Args:**\n\n- `name`: the name\n- `count (int)`: how many\n\n" +
		"**Returns:**\n\n- the thing"

	if got != want {
		t.Errorf("ref output\n got: %q\nwant: %q", got, want)
	}
}

func TestRefWithTarget(t *testing.T) {
	t.Parallel()

	root := writeTree(t, refFixture)
	got := extract(t, root, "ref", map[string]string{"path": "lib.dart", "target": "make"})

	want := "### `make`\n\n```dart\nString make(String name, int count) => name\n```\n\n" +
		"Makes a thing.\n\n**Args:**\n\n- `name`: the name\n- `count (int)`: how many\n\n" +
		"**Returns:**\n\n- the thing"

	if got != want {
		t.Errorf("ref target output\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "Foo") {
		t.Errorf("ref with a target rendered another symbol: %q", got)
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
			attrs: map[string]string{"path": "nonexistent.dart"},
			want:  "> *[selfdoc: 'nonexistent.dart' not found]*",
		},
		{
			name:  "generated file",
			files: map[string]string{"lib/model.g.dart": "class A {}\n"},
			attrs: map[string]string{"path": "lib/model.g.dart"},
			want:  "> *[selfdoc: 'lib/model.g.dart' is a generated file]*",
		},
		{
			name:  "target not found",
			files: map[string]string{"core.dart": "void foo() {}\n"},
			attrs: map[string]string{"path": "core.dart", "target": "nonexistent"},
			want:  "> *[selfdoc: symbol 'nonexistent' not found in 'core.dart']*",
		},
		{
			name:  "directory with no dart files",
			files: map[string]string{"empty/readme.txt": "x\n"},
			attrs: map[string]string{"path": "empty"},
			want:  "> *[selfdoc: 'empty' not found]*",
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

func TestRefDirectory(t *testing.T) {
	t.Parallel()

	root := writeTree(t, sampleProject)
	got := extract(t, root, "ref", map[string]string{"path": "lib"})

	if !strings.HasPrefix(got, "## `lib`\n") {
		t.Errorf("ref on a directory = %q, want it headed by the directory", got)
	}
	for _, want := range []string{"### `Animal`", "### `greet`", "### `Color`"} {
		if !strings.Contains(got, want) {
			t.Errorf("ref on a directory is missing %q", want)
		}
	}
	for _, unwanted := range []string{"GeneratedClass", "FreezedClass"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("ref on a directory read a generated file: %q", unwanted)
		}
	}
}

func TestRefDocComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		want    []string
		wantNot []string
	}{
		{
			name: "fenced example in a class doc",
			source: `/// A widget that displays a greeting.
///
/// Use this widget in your app:
/// ` + "```dart" + `
/// Greeting('Hello')
/// ` + "```" + `
class Greeting {}
`,
			want: []string{"A widget that displays a greeting", "### `Greeting`"},
		},
		{
			name:   "cross references become code spans",
			source: "/// Returns a [Widget] based on [BuildContext].\nclass MyWidget {}\n",
			want:   []string{"`Widget`", "`BuildContext`"},
		},
		{
			name:   "doc across a blank line still belongs to the declaration",
			source: "/// This doc comment is above a blank line.\n\nclass Spaced {}\n",
			want:   []string{"This doc comment is above a blank line"},
		},
		{
			name:   "macro tags pass through",
			source: "/// {@macro my_widget}\n/// This uses a macro reference.\nclass MacroWidget {}\n",
			want:   []string{"{@macro my_widget}"},
		},
		{
			name: "markdown links are left alone",
			source: "/// See [documentation](https://example.com) for details.\n" +
				"/// Also references [Widget] type.\nclass LinkedDoc {}\n",
			want: []string{"[documentation](https://example.com)", "`Widget`"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				"pubspec.yaml": "name: test\n",
				"lib.dart":     test.source,
			})
			got := extract(t, root, "ref", map[string]string{"path": "lib.dart"})
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("ref = %q, missing %q", got, want)
				}
			}
			for _, unwanted := range test.wantNot {
				if strings.Contains(got, unwanted) {
					t.Errorf("ref = %q, should not carry %q", got, unwanted)
				}
			}
		})
	}
}

func TestProseDesc(t *testing.T) {
	t.Parallel()

	root := writeTree(t, refFixture)
	got := extract(t, root, "prose-desc", map[string]string{"path": "lib.dart"})

	want := "The main library. Provides utilities for testing.\n\n" +
		"A foo class.\n\n## Usage\nSee `Bar` and [docs](https://x)."
	if got != want {
		t.Errorf("prose-desc\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "### `Foo`") {
		t.Errorf("prose-desc rendered a declaration: %q", got)
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
			name: "file carries no library doc",
			files: map[string]string{
				"pubspec.yaml": "name: test\n",
				"nodoc.dart":   "class Foo {}\n",
			},
			attrs: map[string]string{"path": "nodoc.dart"},
			want:  "> *[selfdoc: no library doc comment found in 'nodoc.dart']*",
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

// schemaFixture is the models file the schema tests read.
var schemaFixture = map[string]string{
	"pubspec.yaml": "name: test\n",
	"models.dart": `/// A user model.
class User {
  /// The user's unique identifier.
  final String id;
  final String name;
  int age = 0;
  String? email;
  late final Map<String, int> counts;

  User(this.id, this.name);
}

class Point {
  final double x; // the x coordinate
  final double y;
  Point(this.x, this.y);
}
`,
}

func TestTableSchemaTarget(t *testing.T) {
	t.Parallel()

	root := writeTree(t, schemaFixture)
	got := extract(t, root, "table-schema", map[string]string{"path": "models.dart", "target": "User"})

	want := "| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `id` | `String` |  | The user's unique identifier. |\n" +
		"| `name` | `String` |  |  |\n" +
		"| `age` | `int` | `0` |  |\n" +
		"| `email` | `String?` |  |  |\n" +
		"| `counts` | `Map<String, int>` |  |  |"

	if got != want {
		t.Errorf("table-schema\n got: %q\nwant: %q", got, want)
	}
}

func TestTableSchemaEveryClass(t *testing.T) {
	t.Parallel()

	root := writeTree(t, schemaFixture)
	got := extract(t, root, "table-schema", map[string]string{"path": "models.dart"})

	want := "### `User`\n\nA user model.\n\n" +
		"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `id` | `String` |  | The user's unique identifier. |\n" +
		"| `name` | `String` |  |  |\n" +
		"| `age` | `int` | `0` |  |\n" +
		"| `email` | `String?` |  |  |\n" +
		"| `counts` | `Map<String, int>` |  |  |\n" +
		"### `Point`\n\n" +
		"| Field | Type | Default | Description |\n| --- | --- | --- | --- |\n" +
		"| `x` | `double` |  | the x coordinate |\n" +
		"| `y` | `double` |  |  |"

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
			attrs: map[string]string{"path": "models.dart"},
			want:  "> *[selfdoc: file 'models.dart' not found]*",
		},
		{
			name: "class not found",
			files: map[string]string{
				"pubspec.yaml": "name: test\n",
				"models.dart":  "class Foo {\n  final String name;\n  Foo(this.name);\n}\n",
			},
			attrs: map[string]string{"path": "models.dart", "target": "Bar"},
			want:  "> *[selfdoc: class 'Bar' not found in 'models.dart']*",
		},
		{
			name: "no class has fields",
			files: map[string]string{
				"pubspec.yaml": "name: test\n",
				"models.dart":  "class Foo {}\n",
			},
			attrs: map[string]string{"path": "models.dart", "target": "Foo"},
			want:  "> *[selfdoc: no classes with fields found in 'models.dart']*",
		},
		{
			name: "a json path is a config file",
			files: map[string]string{
				"pubspec.yaml": "name: t\n",
				"conf.json":    `{"a": 1, "b": "two"}`,
			},
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

func TestTableSchemaSkipsPrivateFields(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"pubspec.yaml": "name: test\n",
		"models.dart": `class Config {
  final String host;
  final int _port;
  String? _cache;

  Config(this.host);
}
`,
	})
	got := extract(t, root, "table-schema", map[string]string{"path": "models.dart", "target": "Config"})

	if !strings.Contains(got, "`host`") {
		t.Errorf("table-schema = %q, missing the public field", got)
	}
	for _, unwanted := range []string{"_port", "_cache"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("table-schema = %q, rendered the private field %q", got, unwanted)
		}
	}
}

func TestUnknownDirective(t *testing.T) {
	t.Parallel()

	got := extract(t, t.TempDir(), "code-help", map[string]string{"path": "test.dart"})
	want := "> *[selfdoc: unknown directive 'code-help' for dart extractor]*"
	if got != want {
		t.Errorf("unknown directive = %q, want %q", got, want)
	}
}

func TestPartFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		file    string
		want    []string
		wantNot []string
	}{
		{
			name: "part declarations belong to the library",
			files: map[string]string{
				"lib/mylib.dart": `/// My library.
library mylib;

part 'src/models.dart';
part 'src/utils.dart';

class LibraryClass {}
`,
				"lib/src/models.dart": "part of '../mylib.dart';\n\nclass User {}\nclass Product {}\n",
				"lib/src/utils.dart": "part of '../mylib.dart';\n\n" +
					"String formatName(String name) => name.trim();\nconst defaultTimeout = 30;\n",
			},
			file: "lib/mylib.dart",
			want: []string{"LibraryClass", "User", "Product", "formatName", "defaultTimeout"},
		},
		{
			name: "generated parts are refused",
			files: map[string]string{
				"lib/model.dart":         "library model;\n\npart 'model.g.dart';\npart 'model.freezed.dart';\n\nclass Model {}\n",
				"lib/model.g.dart":       "part of 'model.dart';\nclass GeneratedModel {}\n",
				"lib/model.freezed.dart": "part of 'model.dart';\nclass FreezedModel {}\n",
			},
			file:    "lib/model.dart",
			want:    []string{"Model"},
			wantNot: []string{"GeneratedModel", "FreezedModel"},
		},
		{
			name: "a missing part is skipped",
			files: map[string]string{
				"lib/mylib.dart": "library mylib;\n\npart 'missing.dart';\n\nclass Present {}\n",
			},
			file: "lib/mylib.dart",
			want: []string{"Present"},
		},
		{
			name: "private part declarations stay private",
			files: map[string]string{
				"lib/mylib.dart": "library mylib;\npart 'impl.dart';\nclass Public {}\n",
				"lib/impl.dart":  "part of 'mylib.dart';\nclass _Private {}\nclass AlsoPublic {}\n",
			},
			file:    "lib/mylib.dart",
			want:    []string{"Public", "AlsoPublic"},
			wantNot: []string{"_Private"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			symbols, err := newExtractor().PublicSymbols(filepath.Join(root, test.file))
			if err != nil {
				t.Fatalf("PublicSymbols: %v", err)
			}
			for _, want := range test.want {
				if !contains(symbols, want) {
					t.Errorf("PublicSymbols = %v, missing %q", symbols, want)
				}
			}
			for _, unwanted := range test.wantNot {
				if contains(symbols, unwanted) {
					t.Errorf("PublicSymbols = %v, should not carry %q", symbols, unwanted)
				}
			}
		})
	}
}

func TestPartDeclarationsInRef(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"pubspec.yaml": "name: test\n",
		"lib/mylib.dart": `/// My library docs.
library mylib;

part 'part_a.dart';

class MainClass {}
`,
		"lib/part_a.dart": "part of 'mylib.dart';\n\n/// A utility class from part file.\nclass PartClass {}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "lib/mylib.dart"})

	for _, want := range []string{"### `MainClass`", "### `PartClass`", "A utility class from part file"} {
		if !strings.Contains(got, want) {
			t.Errorf("ref = %q, missing %q", got, want)
		}
	}
}

func TestExportFollowing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		file    string
		want    []string
		wantNot []string
	}{
		{
			name: "a barrel file exports what it names",
			files: map[string]string{
				"pubspec.yaml":        "name: test\n",
				"lib/test.dart":       "export 'src/models.dart';\nexport 'src/utils.dart';\n",
				"lib/src/models.dart": "class User {}\nclass Product {}\n",
				"lib/src/utils.dart": "String formatName(String name) => name.trim();\n" +
					"const apiVersion = '1.0';\n",
			},
			file: "lib/test.dart",
			want: []string{"User", "Product", "formatName", "apiVersion"},
		},
		{
			name: "show keeps only what it lists",
			files: map[string]string{
				"pubspec.yaml":        "name: test\n",
				"lib/barrel.dart":     "export 'src/models.dart' show User;\n",
				"lib/src/models.dart": "class User {}\nclass Product {}\nclass Order {}\n",
			},
			file:    "lib/barrel.dart",
			want:    []string{"User"},
			wantNot: []string{"Product", "Order"},
		},
		{
			name: "show keeps every name it lists",
			files: map[string]string{
				"pubspec.yaml":     "name: test\n",
				"lib/barrel.dart":  "export 'src/all.dart' show Alpha, Beta;\n",
				"lib/src/all.dart": "class Alpha {}\nclass Beta {}\nclass Gamma {}\nclass Delta {}\n",
			},
			file:    "lib/barrel.dart",
			want:    []string{"Alpha", "Beta"},
			wantNot: []string{"Gamma", "Delta"},
		},
		{
			name: "hide drops what it lists",
			files: map[string]string{
				"pubspec.yaml":        "name: test\n",
				"lib/barrel.dart":     "export 'src/models.dart' hide Product;\n",
				"lib/src/models.dart": "class User {}\nclass Product {}\nclass Order {}\n",
			},
			file:    "lib/barrel.dart",
			want:    []string{"User", "Order"},
			wantNot: []string{"Product"},
		},
		{
			name: "exports are followed transitively",
			files: map[string]string{
				"pubspec.yaml":        "name: test\n",
				"lib/test.dart":       "export 'src/layer1.dart';\n",
				"lib/src/layer1.dart": "export 'layer2.dart';\nclass FromLayer1 {}\n",
				"lib/src/layer2.dart": "class FromLayer2 {}\n",
			},
			file: "lib/test.dart",
			want: []string{"FromLayer1", "FromLayer2"},
		},
		{
			name: "a circular export terminates",
			files: map[string]string{
				"pubspec.yaml": "name: test\n",
				"lib/a.dart":   "export 'b.dart';\nclass FromA {}\n",
				"lib/b.dart":   "export 'a.dart';\nclass FromB {}\n",
			},
			file: "lib/a.dart",
			want: []string{"FromA", "FromB"},
		},
		{
			name: "a conditional export contributes both arms",
			files: map[string]string{
				"pubspec.yaml":        "name: test\n",
				"lib/test.dart":       "export 'src/stub.dart' if (dart.library.io) 'src/native.dart';\n",
				"lib/src/stub.dart":   "class StubImpl {}\n",
				"lib/src/native.dart": "class NativeImpl {}\n",
			},
			file: "lib/test.dart",
			want: []string{"StubImpl", "NativeImpl"},
		},
		{
			name: "a missing export target is skipped",
			files: map[string]string{
				"pubspec.yaml":  "name: test\n",
				"lib/test.dart": "export 'nonexistent.dart';\nclass Local {}\n",
			},
			file: "lib/test.dart",
			want: []string{"Local"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, test.files)
			symbols, err := newExtractor().PublicSymbols(filepath.Join(root, test.file))
			if err != nil {
				t.Fatalf("PublicSymbols: %v", err)
			}
			for _, want := range test.want {
				if !contains(symbols, want) {
					t.Errorf("PublicSymbols = %v, missing %q", symbols, want)
				}
			}
			for _, unwanted := range test.wantNot {
				if contains(symbols, unwanted) {
					t.Errorf("PublicSymbols = %v, should not carry %q", symbols, unwanted)
				}
			}
		})
	}
}

func TestExportedDeclarationsInRef(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"pubspec.yaml":        "name: test\n",
		"lib/test.dart":       "/// Package API.\nexport 'src/widget.dart';\n",
		"lib/src/widget.dart": "/// A custom widget.\nclass MyWidget {}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "lib/test.dart"})

	for _, want := range []string{"### `MyWidget`", "A custom widget"} {
		if !strings.Contains(got, want) {
			t.Errorf("ref = %q, missing %q", got, want)
		}
	}
}

func TestLocalDeclarationShadowsExport(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"pubspec.yaml": "name: test\n",
		"lib/test.dart": `/// The main API.
export 'src/models.dart';

/// Local override of User.
class User {}
`,
		"lib/src/models.dart": "/// Exported User.\nclass User {}\nclass Product {}\n",
	})
	got := extract(t, root, "ref", map[string]string{"path": "lib/test.dart"})

	for _, want := range []string{"### `User`", "### `Product`", "Local override"} {
		if !strings.Contains(got, want) {
			t.Errorf("ref = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "Exported User") {
		t.Errorf("ref = %q, the re-exported doc should have been shadowed", got)
	}
}

func TestEndToEndLibraryWithPartsAndExports(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"pubspec.yaml": "name: marketplace\nversion: 1.0.0\n",
		"lib/marketplace.dart": `/// The marketplace library.
///
/// Provides models and utilities for the marketplace.
library marketplace;

export 'src/models.dart';
export 'src/cart.dart' show Cart;
`,
		"lib/src/models.dart": `/// Models for the marketplace.
library;

part 'models_impl.dart';

/// A product in the marketplace.
class Product {
  final String name;
  final double price;

  Product(this.name, this.price);
}
`,
		"lib/src/models_impl.dart": `part of 'models.dart';

/// A category for organizing products.
class Category {
  final String label;

  Category(this.label);
}
`,
		"lib/src/cart.dart": `/// A shopping cart.
class Cart {
  final List<dynamic> items;

  Cart() : items = [];
}

/// Internal cart item -- hidden by show combinator.
class CartItem {
  final String productId;
  final int quantity;

  CartItem(this.productId, this.quantity);
}
`,
	})

	symbols, err := newExtractor().PublicSymbols(filepath.Join(root, "lib", "marketplace.dart"))
	if err != nil {
		t.Fatalf("PublicSymbols: %v", err)
	}
	for _, want := range []string{"Product", "Category", "Cart"} {
		if !contains(symbols, want) {
			t.Errorf("PublicSymbols = %v, missing %q", symbols, want)
		}
	}
	if contains(symbols, "CartItem") {
		t.Errorf("PublicSymbols = %v, the show combinator should have hidden CartItem", symbols)
	}

	got := extract(t, root, "ref", map[string]string{"path": "lib/marketplace.dart"})
	for _, want := range []string{
		"The marketplace library",
		"### `Product`", "### `Category`", "### `Cart`",
		"A product in the marketplace", "A category for organizing", "A shopping cart",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ref = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "### `CartItem`") {
		t.Errorf("ref = %q, the show combinator should have hidden CartItem", got)
	}
}

func TestModuleDocstring(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"lib.dart": "/// The main library for my_package.\n" +
			"/// Provides utilities for parsing.\nlibrary my_package;\n",
	})
	got, err := newExtractor().ModuleDocstring(filepath.Join(root, "lib.dart"))
	if err != nil {
		t.Fatalf("ModuleDocstring: %v", err)
	}
	want := "The main library for my_package.\nProvides utilities for parsing."
	if got != want {
		t.Errorf("ModuleDocstring = %q, want %q", got, want)
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
			source: "/// Greets the [name] with a [greeting].\n" +
				"/// Returns the formatted message.\n" +
				"String greet(String name, String greeting) {\n  return 'x';\n}\n",
			symbol: "greet",
			params: []wantParam{
				{"name", "String", true},
				{"greeting", "String", true},
			},
			returnType:       "String",
			returnDocumented: true,
		},
		{
			name: "some parameters undocumented",
			source: "/// Sends a message to [recipient].\n" +
				"void sendMessage(String recipient, String body, int priority) {\n}\n",
			symbol: "sendMessage",
			params: []wantParam{
				{"recipient", "String", true},
				{"body", "String", false},
				{"priority", "int", false},
			},
			returnType: "void",
		},
		{
			name: "named parameters",
			source: "/// Fetches items for [userId] with [limit].\n" +
				"Future<List<String>> fetchItems(\n" +
				"  String userId,\n" +
				"  {required int limit,\n" +
				"   bool includeDeleted = false}\n" +
				") async {\n  return [];\n}\n",
			symbol: "fetchItems",
			params: []wantParam{
				{"userId", "String", true},
				{"limit", "int", true},
				{"includeDeleted", "bool", false},
			},
			returnType: "Future<List<String>>",
		},
		{
			name: "optional positional parameters",
			source: "/// Logs [message] with optional [level].\n" +
				"void log(String message, [int level = 0, String? tag]) {\n}\n",
			symbol: "log",
			params: []wantParam{
				{"message", "String", true},
				{"level", "int", true},
				{"tag", "String?", false},
			},
			returnType: "void",
		},
		{
			name:             "return value documented",
			source:           "/// Computes the sum.\n/// Returns the total.\nint sum(int a, int b) {\n  return a + b;\n}\n",
			symbol:           "sum",
			params:           []wantParam{{"a", "int", false}, {"b", "int", false}},
			returnType:       "int",
			returnDocumented: true,
		},
		{
			name:       "no parameters",
			source:     "/// Prints hello.\nvoid sayHello() {\n  print('hello');\n}\n",
			symbol:     "sayHello",
			returnType: "void",
		},
		{
			name:    "unknown symbol",
			source:  "void hello() {}\n",
			symbol:  "nonexistent",
			wantNil: true,
		},
		{
			name: "dotted name selects a class method",
			source: "class UserRepository {\n" +
				"  /// Finds a user by their [id].\n" +
				"  /// Returns the user or null if not found.\n" +
				"  Future<User?> findById(int id) async {\n    return null;\n  }\n\n" +
				"  void deleteAll() {}\n}\n",
			symbol:           "UserRepository.findById",
			params:           []wantParam{{"id", "int", true}},
			returnType:       "Future<User?>",
			returnDocumented: true,
		},
		{
			name: "dotted name selects an abstract class method",
			source: "abstract class AuthService {\n" +
				"  /// Authenticates a [user] with [password].\n" +
				"  Future<bool> login(String user, String password);\n}\n",
			symbol: "AuthService.login",
			params: []wantParam{
				{"user", "String", true},
				{"password", "String", true},
			},
			returnType: "Future<bool>",
		},
		{
			name: "dotted name selects a mixin method",
			source: "mixin Loggable {\n" +
				"  void log(String message) {\n    print(message);\n  }\n}\n",
			symbol:     "Loggable.log",
			params:     []wantParam{{"message", "String", false}},
			returnType: "void",
		},
		{
			name:    "dotted name with an unknown type",
			source:  "class Foo {\n  void bar() {}\n}\n",
			symbol:  "NonExistent.bar",
			wantNil: true,
		},
		{
			name:    "dotted name with an unknown member",
			source:  "class Foo {\n  void bar() {}\n}\n",
			symbol:  "Foo.nonexistent",
			wantNil: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"lib.dart": test.source})
			got, err := newExtractor().SymbolDetails(filepath.Join(root, "lib.dart"), test.symbol)
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
