package editor

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// encodeJSON encodes v the way Python's json.dumps(v) encodes it with no
// arguments, byte for byte.
//
// The editor's bodies are the Python server's bodies, so the spelling has to
// be the Python encoder's rather than Go's: ", " between items and ": "
// between a key and its value (Go writes neither space), every character
// outside printable ASCII escaped as \uXXXX (Python's ensure_ascii default),
// and "<", ">" and "&" left alone (Go escapes all three, which would rewrite
// every byte of a previewed page).
//
// Key order is the order a struct declares its fields, standing in for the
// insertion order of the Python dict each payload was built as. A map is
// therefore never a payload here: Go maps carry no order, so one is encoded
// with its keys sorted and that is not what any Python payload did.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// jsonMember is one member of an object whose order is stated rather than
// read off a struct.
type jsonMember struct {
	key   string
	value any
}

// jsonObject is an object built member by member, for a payload whose member
// SET depends on its own content -- a link target, where a section carries
// two members a page does not.
type jsonObject []jsonMember

// jsonEncodable is a value that states its own members and their order.
type jsonEncodable interface {
	jsonMembers() jsonObject
}

// encodeValue writes one value.
func encodeValue(buf *bytes.Buffer, v reflect.Value) error {
	if !v.IsValid() {
		buf.WriteString("null")
		return nil
	}
	if v.CanInterface() {
		if stated, ok := v.Interface().(jsonEncodable); ok {
			return encodeStated(buf, stated.jsonMembers())
		}
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			buf.WriteString("null")
			return nil
		}
		return encodeValue(buf, v.Elem())
	case reflect.Bool:
		if v.Bool() {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case reflect.String:
		buf.WriteString(util.PythonJSONString(v.String()))
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		buf.WriteString(strconv.FormatInt(v.Int(), 10))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		buf.WriteString(strconv.FormatUint(v.Uint(), 10))
		return nil
	case reflect.Float32, reflect.Float64:
		buf.WriteString(util.PythonFloatRepr(v.Float()))
		return nil
	case reflect.Slice, reflect.Array:
		return encodeList(buf, v)
	case reflect.Struct:
		return encodeStruct(buf, v)
	case reflect.Map:
		return encodeMap(buf, v)
	default:
		return fmt.Errorf("editor: cannot encode %s as JSON", v.Type())
	}
}

// encodeStated writes an object whose members were stated in order.
func encodeStated(buf *bytes.Buffer, members jsonObject) error {
	buf.WriteByte('{')
	for index, member := range members {
		if index > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(util.PythonJSONString(member.key))
		buf.WriteString(": ")
		if err := encodeValue(buf, reflect.ValueOf(member.value)); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// encodeList writes a list. A nil slice encodes as the empty list, because
// the Python it stands in for built a list either way.
func encodeList(buf *bytes.Buffer, v reflect.Value) error {
	buf.WriteByte('[')
	for index := 0; index < v.Len(); index++ {
		if index > 0 {
			buf.WriteString(", ")
		}
		if err := encodeValue(buf, v.Index(index)); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

// encodeStruct writes a struct as an object, its fields in declaration order
// and named by their json tag.
func encodeStruct(buf *bytes.Buffer, v reflect.Value) error {
	structType := v.Type()
	buf.WriteByte('{')
	written := 0
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		if !field.IsExported() {
			continue
		}
		name := field.Tag.Get("json")
		if comma := strings.Index(name, ","); comma >= 0 {
			// No payload member is conditional -- a struct that needs a
			// conditional member states its own order through
			// jsonEncodable -- but a tag option is still read rather than
			// silently becoming part of the member's name.
			name = name[:comma]
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if written > 0 {
			buf.WriteString(", ")
		}
		written++
		buf.WriteString(util.PythonJSONString(name))
		buf.WriteString(": ")
		if err := encodeValue(buf, v.Field(index)); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// encodeMap writes a map as an object with its keys sorted. No payload is a
// map; this exists so a nested value that is one still encodes rather than
// failing at request time.
func encodeMap(buf *bytes.Buffer, v reflect.Value) error {
	if v.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("editor: cannot encode %s as JSON", v.Type())
	}
	keys := make([]string, 0, v.Len())
	for _, key := range v.MapKeys() {
		keys = append(keys, key.String())
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(util.PythonJSONString(key))
		buf.WriteString(": ")
		value := v.MapIndex(reflect.ValueOf(key).Convert(v.Type().Key()))
		if err := encodeValue(buf, value); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}
