package page

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/identity"
	"github.com/stricttools/selfdoc/internal/util"
)

// jsonDumps encodes v the way Python's json.dumps(v) does with every default
// in place, byte for byte: ", " between items, ": " between a key and its
// value, non-ASCII escaped as \uXXXX, and object properties in the order the
// emitter built them rather than sorted.
//
// The order is the reason this exists beside util.PythonJSON, which sorts
// keys and writes no whitespace because it encodes hash inputs. A JSON-LD
// document is read by people as well as crawlers and its shape is part of
// this build's output, so an object is an identity.Entity -- an ordered
// property list -- and never a Go map.
//
// The value shapes it accepts are the ones the structured-data emitters
// build: an entity, a list, a string, a bool, an integer, a float, and nil.
// Anything else is an error rather than a guess at a spelling.
func jsonDumps(v any) (string, error) {
	var b strings.Builder
	if err := encodeJSON(&b, v); err != nil {
		return "", err
	}
	return b.String(), nil
}

func encodeJSON(b *strings.Builder, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case identity.Entity:
		b.WriteByte('{')
		for i, prop := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(util.PythonJSONString(prop.Key))
			b.WriteString(": ")
			if err := encodeJSON(b, prop.Value); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := encodeJSON(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(util.PythonJSONString(item))
		}
		b.WriteByte(']')
	case string:
		b.WriteString(util.PythonJSONString(t))
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		b.WriteString(util.PythonFloatRepr(t))
	default:
		return fmt.Errorf(
			"page: %T cannot be emitted as JSON-LD; a structured-data "+
				"value is an entity, a list, a string, a bool, a number "+
				"or nil", v)
	}
	return nil
}

// ldScript wraps an encoded JSON-LD document in the script element every
// emitter here writes it in, newline-delimited so the document starts and
// ends on its own line.
func ldScript(doc string) string {
	return "\n<script type=\"application/ld+json\">\n" + doc + "\n</script>"
}

// entity builds an ordered JSON-LD entity from alternating keys and values,
// which is how every emitter below states a small object inline.
func entity(pairs ...any) identity.Entity {
	e := make(identity.Entity, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic("page: a JSON-LD property name must be a string")
		}
		e = append(e, identity.Property{Key: key, Value: pairs[i+1]})
	}
	return e
}
