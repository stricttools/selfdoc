package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// jsonPair is one member of an ordered JSON object.
type jsonPair struct {
	Key   string
	Value any
}

// jsonObject is a JSON object whose members keep the order they were written
// in. Go maps carry no order and the scaffolded selfdoc.json is a document a
// person reads, so the order the Python wrote it in is carried rather than
// recovered.
type jsonObject []jsonPair

// encodeJSON renders value the way Python's json.dumps(value, indent=2) does:
// two-space indent, ", " dropped to "," before a newline, ensure_ascii
// escaping, and an empty object or array on one line.
func encodeJSON(value any, indent int) string {
	pad := strings.Repeat("  ", indent)
	inner := strings.Repeat("  ", indent+1)
	switch typed := value.(type) {
	case jsonObject:
		if len(typed) == 0 {
			return "{}"
		}
		parts := make([]string, 0, len(typed))
		for _, pair := range typed {
			parts = append(parts, inner+util.PythonJSONString(pair.Key)+": "+encodeJSON(pair.Value, indent+1))
		}
		return "{\n" + strings.Join(parts, ",\n") + "\n" + pad + "}"
	case []any:
		if len(typed) == 0 {
			return "[]"
		}
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, inner+encodeJSON(item, indent+1))
		}
		return "[\n" + strings.Join(parts, ",\n") + "\n" + pad + "]"
	case string:
		return util.PythonJSONString(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case json.Number:
		// The literal the document carried, written back byte for byte.
		return typed.String()
	case nil:
		return "null"
	default:
		panic(fmt.Sprintf("encodeJSON: unsupported value %T", value))
	}
}

// decodeOrderedJSON parses data into the value shapes above, keeping every
// object's key order.
//
// It exists so a document this package only PATCHES -- the project manifest a
// release post appends itself to -- is written back in the order it was read
// in, without this package having to know what keys that document carries.
// Python's json.load/json.dumps pair preserved the order for free; Go's maps
// do not, and restating the manifest's own key order here would be a second
// copy of it.
func decodeOrderedJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeOrderedValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the JSON document")
	}
	return value, nil
}

func decodeOrderedValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	return decodeOrderedFrom(decoder, token)
}

func decodeOrderedFrom(decoder *json.Decoder, token json.Token) (any, error) {
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			object := jsonObject{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				value, err := decodeOrderedValue(decoder)
				if err != nil {
					return nil, err
				}
				object = append(object, jsonPair{key, value})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return object, nil
		case '[':
			items := []any{}
			for decoder.More() {
				value, err := decodeOrderedValue(decoder)
				if err != nil {
					return nil, err
				}
				items = append(items, value)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return items, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", typed)
	default:
		return token, nil
	}
}

// get returns the value under key, reporting whether the object carries it.
func (o jsonObject) get(key string) (any, bool) {
	for _, pair := range o {
		if pair.Key == key {
			return pair.Value, true
		}
	}
	return nil, false
}

// set replaces the value under key in place, or appends the member when the
// object does not already carry it.
func (o jsonObject) set(key string, value any) jsonObject {
	for index := range o {
		if o[index].Key == key {
			o[index].Value = value
			return o
		}
	}
	return append(o, jsonPair{key, value})
}
