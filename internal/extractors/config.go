package extractors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/tables"
	"github.com/stricttools/selfdoc/internal/util"
)

// RenderTable renders a Markdown table in the one form the extractors emit:
// no per-column alignment and no padding. It exists so the nine extractors
// share a single call into the table renderer.
func RenderTable(headers []string, rows [][]string) (string, error) {
	return tables.RenderMarkdownTable(headers, rows, nil, false)
}

// JSONObject is a decoded JSON or TOML table that remembers the order its keys
// appeared in.
//
// Order is not a nicety here: every config table selfdoc renders lists its rows
// in document order, which is what a reader comparing the page against the file
// expects. Go's map type has no order, so the decoders in this file build this
// instead.
type JSONObject struct {
	keys   []string
	values map[string]any
}

// NewJSONObject builds an empty object.
func NewJSONObject() *JSONObject {
	return &JSONObject{values: map[string]any{}}
}

// Keys lists the object's keys in the order they appeared.
//
// A nil object has no keys, which is what a decoder that answered "no object
// here" means. Has, Get, Keys and Len all read a nil receiver as the empty
// object, so a caller that took an object out of a document does not have to
// know whether the document carried one.
func (o *JSONObject) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// Len is the number of keys. A nil object has none.
func (o *JSONObject) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Has reports whether the object carries key. A nil object carries nothing.
func (o *JSONObject) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.values[key]
	return ok
}

// Get reports the value at key. A nil object reports nothing.
func (o *JSONObject) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

// Set records key's value, appending the key when it is new and leaving its
// position alone when it is not -- the behavior of assigning into a Python
// dict, which is what the decoders this replaces did.
func (o *JSONObject) Set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// bigInt is an integer literal too large for int64, kept verbatim so it renders
// as Python's arbitrary-precision int would.
type bigInt string

// DecodeJSON decodes JSON text into the value model this package renders:
// nil, bool, int64, float64, string, []any and *JSONObject.
//
// Object key order is kept, and an integer literal stays an integer rather than
// becoming a float, because the config tables print those as different types.
func DecodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("extra data after top-level value")
	}
	return value, nil
}

func decodeJSONValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeJSONFrom(dec, token)
}

func decodeJSONFrom(dec *json.Decoder, token json.Token) (any, error) {
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewJSONObject()
			for {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				if delim, ok := keyToken.(json.Delim); ok && delim == '}' {
					return obj, nil
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				value, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, value)
			}
		case '[':
			items := []any{}
			for {
				itemToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				if delim, ok := itemToken.(json.Delim); ok && delim == ']' {
					return items, nil
				}
				item, err := decodeJSONFrom(dec, itemToken)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
		default:
			return nil, errors.New("unexpected delimiter " + string(rune(t)))
		}
	case json.Number:
		return decodeJSONNumber(t), nil
	default:
		return token, nil
	}
}

// decodeJSONNumber splits a JSON number literal into an integer or a float the
// way Python's json module does: a literal with a fraction or an exponent is a
// float, anything else is an integer.
func decodeJSONNumber(n json.Number) any {
	literal := n.String()
	if !strings.ContainsAny(literal, ".eE") {
		if i, err := strconv.ParseInt(literal, 10, 64); err == nil {
			return i
		}
		return bigInt(literal)
	}
	f, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return bigInt(literal)
	}
	return f
}

// JSONTypeName names a decoded value's type as the config tables print it.
func JSONTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case int64, int, bigInt:
		return "integer"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case *JSONObject:
		return "object"
	case time.Time:
		return "datetime"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// JSONValueRepr renders a decoded value compactly enough for a table cell:
// a scalar verbatim, a long string truncated, and a collection as its size.
func JSONValueRepr(value any) string {
	switch v := value.(type) {
	case nil:
		return "`null`"
	case bool:
		if v {
			return "`true`"
		}
		return "`false`"
	case string:
		runes := []rune(v)
		if len(runes) > 40 {
			return "`\"" + string(runes[:37]) + "...\"`"
		}
		return "`\"" + v + "\"`"
	case int64:
		return "`" + strconv.FormatInt(v, 10) + "`"
	case int:
		return "`" + strconv.Itoa(v) + "`"
	case bigInt:
		return "`" + string(v) + "`"
	case float64:
		return "`" + util.PythonFloatRepr(v) + "`"
	case []any:
		if len(v) == 0 {
			return "`[]`"
		}
		return "`[...] (" + strconv.Itoa(len(v)) + " items)`"
	case *JSONObject:
		if v.Len() == 0 {
			return "`{}`"
		}
		return "`{...} (" + strconv.Itoa(v.Len()) + " keys)`"
	default:
		return "`" + fmt.Sprintf("%v", v) + "`"
	}
}

// RenderJSONIndent2 renders a decoded value the way Python's
// json.dumps(value, indent=2) does, byte for byte: two-space indentation, ": "
// between a key and its value, "{}" and "[]" for the empty collections, and
// every non-ASCII character escaped.
//
// Keys are NOT sorted -- the call site this serves prints a JSON document whose
// top level is not an object, and reordering what the file said would misreport
// it.
func RenderJSONIndent2(value any) string {
	var b strings.Builder
	renderJSONIndented(&b, value, 0)
	return b.String()
}

func renderJSONIndented(b *strings.Builder, value any, depth int) {
	pad := strings.Repeat("  ", depth+1)
	closePad := strings.Repeat("  ", depth)

	switch v := value.(type) {
	case *JSONObject:
		if v.Len() == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, key := range v.Keys() {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			b.WriteString(util.PythonJSONString(key))
			b.WriteString(": ")
			item, _ := v.Get(key)
			renderJSONIndented(b, item, depth+1)
		}
		b.WriteString("\n")
		b.WriteString(closePad)
		b.WriteString("}")
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, item := range v {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			renderJSONIndented(b, item, depth+1)
		}
		b.WriteString("\n")
		b.WriteString(closePad)
		b.WriteString("]")
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		b.WriteString(util.PythonJSONString(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case int:
		b.WriteString(strconv.Itoa(v))
	case bigInt:
		b.WriteString(string(v))
	case float64:
		b.WriteString(util.PythonFloatRepr(v))
	default:
		b.WriteString(util.PythonJSONString(fmt.Sprintf("%v", value)))
	}
}

// ConfigTableFromJSON renders decoded JSON as the Key/Type/Value table.
//
// A document whose top level is not an object has no keys to tabulate, so it is
// rendered as a fenced, indented JSON block instead.
func ConfigTableFromJSON(value any, displayPath string, excludeKeys []string) (string, error) {
	obj, ok := value.(*JSONObject)
	if !ok {
		return "```json\n" + RenderJSONIndent2(value) + "\n```", nil
	}

	filtered, errMarkdown := ApplyExcludeKeys(obj, excludeKeys, displayPath)
	if errMarkdown != "" {
		return errMarkdown, nil
	}

	rows := make([][]string, 0, filtered.Len())
	for _, key := range filtered.Keys() {
		value, _ := filtered.Get(key)
		rows = append(rows, []string{"`" + key + "`", JSONTypeName(value), JSONValueRepr(value)})
	}

	return RenderTable([]string{"Key", "Type", "Value"}, rows)
}

// ConfigTableFromJSONText parses JSON text and renders it as the Key/Type/Value
// table, or the error marker when it does not parse.
func ConfigTableFromJSONText(text []byte, displayPath string, excludeKeys []string) (string, error) {
	value, err := DecodeJSON(text)
	if err != nil {
		return FormatError("cannot parse '" + displayPath + "': " + err.Error()), nil
	}
	return ConfigTableFromJSON(value, displayPath, excludeKeys)
}

// ConfigFromJSON reads a JSON config file and renders it as the Key/Type/Value
// table.
func ConfigFromJSON(fullPath, displayPath string, excludeKeys []string) (string, error) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return FormatError("cannot parse '" + displayPath + "': " + err.Error()), nil
	}
	return ConfigTableFromJSONText(data, displayPath, excludeKeys)
}

// ConfigFromTOML reads a TOML config file and renders its leaf keys as the
// Key/Type/Value table, with nested tables flattened into dotted keys.
func ConfigFromTOML(fullPath, displayPath string, excludeKeys []string) (string, error) {
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return FormatError("cannot parse '" + displayPath + "': " + err.Error()), nil
	}
	decoded, keys, err := util.DecodeTOMLOrdered(raw)
	if err != nil {
		return FormatError("cannot parse '" + displayPath + "': " + err.Error()), nil
	}

	data := orderedFromTOML(decoded, keys)

	filtered, errMarkdown := ApplyExcludeKeys(data, excludeKeys, displayPath)
	if errMarkdown != "" {
		return errMarkdown, nil
	}

	var rows [][]string
	flattenTOML(filtered, "", &rows)

	return RenderTable([]string{"Key", "Type", "Value"}, rows)
}

// orderedFromTOML rebuilds a decoded TOML document with its document order
// restored, driven by the key list the decoder recorded.
//
// The decoder hands back Go maps, which have no order, and the rendered table's
// row order is document order. The decoder's key list is in document order, so
// replaying it over the decoded values reconstructs the tree the Python
// tomllib-backed renderer walked.
func orderedFromTOML(decoded map[string]any, keys [][]string) *JSONObject {
	root := NewJSONObject()

	for _, key := range keys {
		// Resolve the key's value out of the decoded maps.
		var current any = decoded
		reachable := true
		for _, segment := range key {
			m, ok := current.(map[string]any)
			if !ok {
				reachable = false
				break
			}
			current, ok = m[segment]
			if !ok {
				reachable = false
				break
			}
		}
		if !reachable {
			continue
		}

		// Walk (creating as needed) the ordered nodes above the key, stopping
		// if anything above it is not a table.
		node := root
		for _, segment := range key[:len(key)-1] {
			existing, ok := node.Get(segment)
			if !ok {
				child := NewJSONObject()
				node.Set(segment, child)
				node = child
				continue
			}
			child, ok := existing.(*JSONObject)
			if !ok {
				node = nil
				break
			}
			node = child
		}
		if node == nil {
			continue
		}

		leaf := key[len(key)-1]
		if _, ok := current.(map[string]any); ok {
			if _, exists := node.Get(leaf); !exists {
				node.Set(leaf, NewJSONObject())
			}
			continue
		}
		node.Set(leaf, convertTOMLValue(current))
	}

	return root
}

// convertTOMLValue maps a decoded TOML value into this package's value model.
func convertTOMLValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		obj := NewJSONObject()
		for key, item := range v {
			obj.Set(key, convertTOMLValue(item))
		}
		return obj
	case []any:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, convertTOMLValue(item))
		}
		return items
	case []map[string]any:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, convertTOMLValue(item))
		}
		return items
	case int:
		return int64(v)
	default:
		return value
	}
}

// flattenTOML walks an ordered table depth-first, emitting one row per leaf
// key with its dotted path.
func flattenTOML(data *JSONObject, prefix string, rows *[][]string) {
	for _, key := range data.Keys() {
		value, _ := data.Get(key)
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		if table, ok := value.(*JSONObject); ok {
			flattenTOML(table, fullKey, rows)
			continue
		}
		*rows = append(*rows, []string{
			"`" + fullKey + "`",
			JSONTypeName(value),
			JSONValueRepr(value),
		})
	}
}

// HandleTableConfig is the shared table-config handler. It reads a JSON or TOML
// config file and renders it as a key/type/value table; any other extension is
// shown verbatim in a fenced block, since there is nothing to tabulate.
//
// A language that recognizes a further config format maps table-config to its
// own handler and delegates here for everything it does not handle -- what the
// TypeScript extractor does for JSONC.
func HandleTableConfig(
	path string,
	_ *string,
	_ []string,
	_ []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path == "" {
		return FormatError("table-config requires a file path argument"), nil
	}

	fullPath := util.ResolveDirectivePath(baseDir, path)
	if !IsFile(fullPath) {
		return FormatError("config file '" + path + "' not found"), nil
	}

	_, ext := splitExt(path)
	ext = strings.ToLower(ext)
	excludeKeys := ExcludeKeysFromAttrs(attrs)

	switch ext {
	case ".json":
		return ConfigFromJSON(fullPath, path, excludeKeys)
	case ".toml":
		return ConfigFromTOML(fullPath, path, excludeKeys)
	default:
		content, err := ReadSource(fullPath)
		if err != nil {
			return FormatError("cannot read '" + path + "': " + err.Error()), nil
		}
		return "```\n" + pyRStrip(content) + "\n```", nil
	}
}

// splitExt splits path into its stem and its extension, reproducing Python's
// posixpath.splitext: the extension starts at the last dot of the last path
// element, and a leading run of dots belongs to the stem, so a dotfile has no
// extension.
func splitExt(path string) (string, string) {
	sepIndex := strings.LastIndex(path, "/")
	dotIndex := strings.LastIndex(path, ".")
	if dotIndex > sepIndex {
		filenameIndex := sepIndex + 1
		for filenameIndex < dotIndex {
			if path[filenameIndex] != '.' {
				return path[:dotIndex], path[dotIndex:]
			}
			filenameIndex++
		}
	}
	return path, ""
}

// IsFile reports whether path is an existing regular file, the question
// Python's os.path.isfile answers -- which every extractor asks of a candidate
// it is resolving.
func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// IsDir reports whether path is an existing directory, the question Python's
// os.path.isdir answers.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
