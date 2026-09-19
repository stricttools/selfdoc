package strictclisupport

import (
	"sort"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/util"
)

// Object is one decoded JSON object of a dumped schema, remembering the order
// its keys appeared in.
//
// The order is not a nicety: the index page lists commands and groups in the
// order the schema declares them, so a model that lost the order would
// reshuffle every generated page from one run to the next.
type Object = extractors.JSONObject

// asObject is v as an object, or nil when v is anything else. A schema entry
// that is not an object is read as an absent one rather than aborting the
// page, which is what the Python's `.get()` chains did.
func asObject(v any) *Object {
	obj, _ := v.(*Object)
	return obj
}

// get is the value at key, or nil when o is nil or carries no such key.
func get(o *Object, key string) any {
	if o == nil {
		return nil
	}
	v, _ := o.Get(key)
	return v
}

// lookup is the value at key plus whether the object carries it at all.
//
// The distinction is read by the declarations whose absence means something
// different from their false value: `negatable` absent publishes `--no-x`,
// `negatable: false` does not, and `dry_run_supported` absent is the normal
// case that prints nothing.
func lookup(o *Object, key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	return o.Get(key)
}

// getString is the string at key, or "" when the key is absent or its value is
// not a string.
func getString(o *Object, key string) string {
	s, _ := get(o, key).(string)
	return s
}

// getList is the list at key, or nil when the key is absent or its value is
// not a list.
func getList(o *Object, key string) []any {
	items, _ := get(o, key).([]any)
	return items
}

// getObject is the object at key, or nil when the key is absent or its value
// is not an object.
func getObject(o *Object, key string) *Object {
	return asObject(get(o, key))
}

// getTruthy reports whether the value at key is truthy by Python's rules --
// the test every `if entry.get("x")` in the Python renderer performed.
func getTruthy(o *Object, key string) bool {
	return truthy(get(o, key))
}

// isFalse reports whether the value at key is the boolean false, which is a
// different declaration from an absent key.
func isFalse(o *Object, key string) bool {
	v, ok := lookup(o, key)
	if !ok {
		return false
	}
	b, isBool := v.(bool)
	return isBool && !b
}

// truthy reproduces Python's truth test for a decoded JSON value: null, false,
// zero, the empty string and an empty collection are false, everything else is
// true.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case int64:
		return t != 0
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case *Object:
		return t != nil && t.Len() > 0
	default:
		return true
	}
}

// sortedKeys are an object's keys in code-point order, which is the order the
// Python renderer's `sorted(...)` loops walked the deprecated maps in.
func sortedKeys(o *Object) []string {
	if o == nil {
		return nil
	}
	keys := append([]string(nil), o.Keys()...)
	sort.Strings(keys)
	return keys
}

// copyObject is a shallow copy of o, preserving key order. It stands in for
// Python's dict(obj).
func copyObject(o *Object) *Object {
	out := extractors.NewJSONObject()
	if o == nil {
		return out
	}
	for _, key := range o.Keys() {
		v, _ := o.Get(key)
		out.Set(key, v)
	}
	return out
}

// pyStr renders a decoded value the way Python's str() -- and therefore an
// f-string interpolation -- renders it. Every cell of every generated table
// that interpolates a schema value goes through here, so a declared integer
// default prints "3" rather than Go's own spelling of an int64.
//
// A container is answered by pyRepr, because this package's object type
// remembers its key order and [util.PythonRepr] cannot see it.
func pyStr(v any) string {
	switch v.(type) {
	case []any, *Object:
		return pyRepr(v)
	default:
		return util.PythonStr(v)
	}
}

// pyRepr renders a decoded value the way Python's repr() renders it.
//
// Only a container reaches it: every scalar interpolation is answered by
// pyStr, and the one place a container is interpolated -- a structured default
// -- renders as JSON instead. It exists so a schema that declares a container
// where the renderer expects a scalar prints something a reader recognizes as
// the container it is.
//
// An object's keys keep the order the document declared them, which is why
// this is not [util.PythonRepr]: that one sorts a Go map's keys, having no
// order to preserve.
func pyRepr(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, pyRepr(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Object:
		if t == nil {
			return "None"
		}
		parts := make([]string, 0, t.Len())
		for _, key := range t.Keys() {
			value, _ := t.Get(key)
			parts = append(parts, util.PythonRepr(key)+": "+pyRepr(value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return util.PythonRepr(v)
	}
}

// pyJSONDumps renders a decoded value the way Python's json.dumps(value) does
// with its own defaults: ", " between items, ": " between a key and its value,
// every non-ASCII character escaped, and keys left in the order the document
// declared them.
//
// That spelling is what a structured default renders as, and the Python's
// choice of json.dumps over a repr is deliberate: a page showing
// `{'relative_to_root': ...}` would be publishing Python syntax to a reader of
// a CLI reference.
func pyJSONDumps(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return util.PythonJSONString(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return util.PythonFloatRepr(t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, pyJSONDumps(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Object:
		if t == nil {
			return "null"
		}
		parts := make([]string, 0, t.Len())
		for _, key := range t.Keys() {
			value, _ := t.Get(key)
			parts = append(parts, util.PythonJSONString(key)+": "+pyJSONDumps(value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return util.PythonJSONString(pyStr(v))
	}
}

// PlainValue converts a decoded schema value into the plain Go value model --
// map[string]any, []any and scalars.
//
// It is what the schema-hash input needs: the hash is canonical JSON with
// sorted keys, so the ordered model this package reads a schema with carries
// nothing the hash uses, and a reflect-based encoder cannot walk it.
func PlainValue(v any) any {
	switch t := v.(type) {
	case *Object:
		if t == nil {
			return nil
		}
		out := make(map[string]any, t.Len())
		for _, key := range t.Keys() {
			value, _ := t.Get(key)
			out[key] = PlainValue(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, PlainValue(item))
		}
		return out
	default:
		return v
	}
}
