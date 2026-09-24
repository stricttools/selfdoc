package extractors

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// write puts content at name inside a fresh temporary directory and returns
// both the directory and the file's full path.
func write(t *testing.T, name, content string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, full
}

// TestConfigFromJSON pins the rendered table against the output python3
// produced for the same document.
func TestConfigFromJSON(t *testing.T) {
	hygiene.Isolate(t)
	_, full := write(t, "config.json", `{"name":"test","count":42,"enabled":true,`+
		`"items":[1,2],"nested":{"a":1},"nothing":null,"ratio":1.5,`+
		`"empty_list":[],"empty_obj":{},"long":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}`)

	got, err := ConfigFromJSON(full, "config.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "| Key | Type | Value |\n" +
		"| --- | --- | --- |\n" +
		"| `name` | string | `\"test\"` |\n" +
		"| `count` | integer | `42` |\n" +
		"| `enabled` | boolean | `true` |\n" +
		"| `items` | array | `[...] (2 items)` |\n" +
		"| `nested` | object | `{...} (1 keys)` |\n" +
		"| `nothing` | null | `null` |\n" +
		"| `ratio` | number | `1.5` |\n" +
		"| `empty_list` | array | `[]` |\n" +
		"| `empty_obj` | object | `{}` |\n" +
		"| `long` | string | `\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx...\"` |"
	if got != want {
		t.Fatalf("ConfigFromJSON =\n%s\nwant\n%s", got, want)
	}
}

func TestConfigFromJSONNonObject(t *testing.T) {
	hygiene.Isolate(t)
	_, full := write(t, "list.json", `[1,{"b":2,"a":1}]`)
	got, err := ConfigFromJSON(full, "list.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "```json\n[\n  1,\n  {\n    \"b\": 2,\n    \"a\": 1\n  }\n]\n```"
	if got != want {
		t.Fatalf("ConfigFromJSON =\n%s\nwant\n%s", got, want)
	}
}

func TestConfigFromJSONParseError(t *testing.T) {
	hygiene.Isolate(t)
	_, full := write(t, "bad.json", "{invalid json")
	got, err := ConfigFromJSON(full, "bad.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPrefix(got, "> *[selfdoc: cannot parse 'bad.json': ") {
		t.Fatalf("ConfigFromJSON = %q, want a parse-error marker", got)
	}
}

func TestConfigFromJSONExclude(t *testing.T) {
	hygiene.Isolate(t)
	_, full := write(t, "config.json", `{"name":"test","version":"1.0","count":42}`)

	got, err := ConfigFromJSON(full, "config.json", []string{"version"})
	if err != nil {
		t.Fatal(err)
	}
	want := "| Key | Type | Value |\n" +
		"| --- | --- | --- |\n" +
		"| `name` | string | `\"test\"` |\n" +
		"| `count` | integer | `42` |"
	if got != want {
		t.Fatalf("ConfigFromJSON =\n%s\nwant\n%s", got, want)
	}

	got, err = ConfigFromJSON(full, "config.json", []string{"missing"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "> *[selfdoc: exclude key 'missing' not found in 'config.json']*"; got != want {
		t.Fatalf("ConfigFromJSON = %q, want %q", got, want)
	}
}

// TestConfigFromTOML pins the flattened table against the output python3
// produced for the same document, including its document order.
func TestConfigFromTOML(t *testing.T) {
	hygiene.Isolate(t)
	document := "name = \"test\"\ncount = 42\n\n[server]\nhost = \"localhost\"\nport = 3000\n\n[logging]\nlevel = \"debug\"\n"
	_, full := write(t, "config.toml", document)

	got, err := ConfigFromTOML(full, "config.toml", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "| Key | Type | Value |\n" +
		"| --- | --- | --- |\n" +
		"| `name` | string | `\"test\"` |\n" +
		"| `count` | integer | `42` |\n" +
		"| `server.host` | string | `\"localhost\"` |\n" +
		"| `server.port` | integer | `3000` |\n" +
		"| `logging.level` | string | `\"debug\"` |"
	if got != want {
		t.Fatalf("ConfigFromTOML =\n%s\nwant\n%s", got, want)
	}

	got, err = ConfigFromTOML(full, "config.toml", []string{"logging"})
	if err != nil {
		t.Fatal(err)
	}
	want = "| Key | Type | Value |\n" +
		"| --- | --- | --- |\n" +
		"| `name` | string | `\"test\"` |\n" +
		"| `count` | integer | `42` |\n" +
		"| `server.host` | string | `\"localhost\"` |\n" +
		"| `server.port` | integer | `3000` |"
	if got != want {
		t.Fatalf("ConfigFromTOML with an excluded section =\n%s\nwant\n%s", got, want)
	}

	got, err = ConfigFromTOML(full, "config.toml", []string{"missing"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "> *[selfdoc: exclude key 'missing' not found in 'config.toml']*"; got != want {
		t.Fatalf("ConfigFromTOML = %q, want %q", got, want)
	}
}

func TestConfigFromTOMLParseError(t *testing.T) {
	hygiene.Isolate(t)
	_, full := write(t, "bad.toml", "this is not = = toml\n")
	got, err := ConfigFromTOML(full, "bad.toml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPrefix(got, "> *[selfdoc: cannot parse 'bad.toml': ") {
		t.Fatalf("ConfigFromTOML = %q, want a parse-error marker", got)
	}
}

func TestHandleTableConfig(t *testing.T) {
	hygiene.Isolate(t)

	t.Run("no path", func(t *testing.T) {
		got, err := HandleTableConfig("", nil, nil, nil, ".", map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "> *[selfdoc: table-config requires a file path argument]*"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		got, err := HandleTableConfig("nope.json", nil, nil, nil, dir, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "> *[selfdoc: config file 'nope.json' not found]*"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("an unsupported extension is fenced verbatim", func(t *testing.T) {
		dir, _ := write(t, "app.ini", "[section]\nkey = value\n\n\n")
		got, err := HandleTableConfig("app.ini", nil, nil, nil, dir, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "```\n[section]\nkey = value\n```"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("json is tabulated", func(t *testing.T) {
		dir, _ := write(t, "c.json", `{"a":1}`)
		got, err := HandleTableConfig("c.json", nil, nil, nil, dir, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "| Key | Type | Value |\n| --- | --- | --- |\n| `a` | integer | `1` |"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("toml is tabulated", func(t *testing.T) {
		dir, _ := write(t, "c.toml", "a = 1\n")
		got, err := HandleTableConfig("c.toml", nil, nil, nil, dir, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "| Key | Type | Value |\n| --- | --- | --- |\n| `a` | integer | `1` |"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("the exclude attribute is honored", func(t *testing.T) {
		dir, _ := write(t, "c.json", `{"a":1,"b":2}`)
		got, err := HandleTableConfig("c.json", nil, nil, nil, dir, map[string]string{"exclude": "b"})
		if err != nil {
			t.Fatal(err)
		}
		if want := "| Key | Type | Value |\n| --- | --- | --- |\n| `a` | integer | `1` |"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// TestDecodeJSONKeepsKeyOrder pins the property every config table depends on.
func TestDecodeJSONKeepsKeyOrder(t *testing.T) {
	t.Parallel()
	value, err := DecodeJSON([]byte(`{"z":1,"a":2,"m":3}`))
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := value.(*JSONObject)
	if !ok {
		t.Fatalf("DecodeJSON returned %T, want *JSONObject", value)
	}
	if want := []string{"z", "a", "m"}; !reflect.DeepEqual(obj.Keys(), want) {
		t.Fatalf("keys = %#v, want %#v", obj.Keys(), want)
	}
}

func TestDecodeJSONNumberKinds(t *testing.T) {
	t.Parallel()
	value, err := DecodeJSON([]byte(`{"i":42,"f":1.5,"e":4e2,"neg":-7}`))
	if err != nil {
		t.Fatal(err)
	}
	obj := value.(*JSONObject)
	for _, tt := range []struct {
		key      string
		typeName string
		repr     string
	}{
		{"i", "integer", "`42`"},
		{"f", "number", "`1.5`"},
		{"e", "number", "`400.0`"},
		{"neg", "integer", "`-7`"},
	} {
		item, _ := obj.Get(tt.key)
		if got := JSONTypeName(item); got != tt.typeName {
			t.Errorf("JSONTypeName(%s) = %q, want %q", tt.key, got, tt.typeName)
		}
		if got := JSONValueRepr(item); got != tt.repr {
			t.Errorf("JSONValueRepr(%s) = %q, want %q", tt.key, got, tt.repr)
		}
	}
}

func TestDecodeJSONRejectsTrailingData(t *testing.T) {
	t.Parallel()
	if _, err := DecodeJSON([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("a document with two top-level values was accepted")
	}
}

func TestJSONObjectSetKeepsFirstPosition(t *testing.T) {
	t.Parallel()
	obj := NewJSONObject()
	obj.Set("a", int64(1))
	obj.Set("b", int64(2))
	obj.Set("a", int64(3))
	if want := []string{"a", "b"}; !reflect.DeepEqual(obj.Keys(), want) {
		t.Fatalf("keys = %#v, want %#v", obj.Keys(), want)
	}
	if v, _ := obj.Get("a"); v != int64(3) {
		t.Fatalf("a = %v, want 3", v)
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// TestNilJSONObjectReadsAsEmpty covers the readers on a nil object. A decoder
// that found no object answers nil, and a caller that asks such an object what
// it carries gets "nothing" rather than a panic.
func TestNilJSONObjectReadsAsEmpty(t *testing.T) {
	var obj *JSONObject
	if got := obj.Keys(); got != nil {
		t.Errorf("Keys() = %#v, want nil", got)
	}
	if got := obj.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
	if obj.Has("anything") {
		t.Error("Has() = true, want false")
	}
	if value, ok := obj.Get("anything"); ok || value != nil {
		t.Errorf("Get() = (%#v, %v), want (nil, false)", value, ok)
	}
}
