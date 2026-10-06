package util

import (
	"fmt"
	"os"
	"strings"
	"time"

	tomledit "github.com/stricttools/go-toml-edit"
)

// DecodeTOML decodes a TOML document into the generic Go value every caller in
// this module validates by hand.
//
// The shapes are the ones the hand-written validations and their pinned
// refusals were written against: a string, an int64, a float64, a bool, a
// time.Time for each of the four date-time flavors, a []any for an array, a
// map[string]any for a table, a []map[string]any for an array of tables, and
// nested maps for a dotted key. A caller reads them with a type assertion and
// refuses anything else by name, so a shape stated here is part of every one
// of those refusals.
func DecodeTOML(data []byte) (map[string]any, error) {
	values, _, err := DecodeTOMLOrdered(data)
	return values, err
}

// DecodeTOMLFile reads path and decodes it through [DecodeTOML].
func DecodeTOMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeTOML(data)
}

// DecodeTOMLOrdered decodes like [DecodeTOML] and additionally answers every
// key the document declares, as its path from the root, in document order.
//
// A Go map has no order, so a renderer whose row order is document order reads
// the order from here instead. The list carries a table header before the keys
// written under it, one entry per array-of-tables element, and the keys of an
// inline table under the key it is bound to -- a sequence a caller replays over
// the decoded values to rebuild the document's own order.
func DecodeTOMLOrdered(data []byte) (map[string]any, [][]string, error) {
	document, err := tomledit.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	values, err := tomlValues(document)
	if err != nil {
		return nil, nil, err
	}
	return values, tomlKeyOrder(document), nil
}

// tomlValues builds the decoded value tree from the parser's read-layer, which
// answers what the document MEANS: a dotted key, a [header] table and an
// inline table all arrive as one record, and an array of tables arrives as the
// records collected under its key however deeply it is nested.
func tomlValues(document *tomledit.Document) (map[string]any, error) {
	return tomlRecord(document.Root())
}

// tomlRecord converts one read-layer record into the map shape the callers read.
func tomlRecord(record *tomledit.Record) (map[string]any, error) {
	table := make(map[string]any, record.Len())
	for entry := range record.Entries() {
		value, err := tomlEntry(entry)
		if err != nil {
			return nil, err
		}
		table[entry.Key()] = value
	}
	return table, nil
}

// tomlEntry converts one record entry: a value through the AST node it was
// written as, a table through its own record, an array of tables through the
// ordered records collected under its key.
func tomlEntry(entry tomledit.Entry) (any, error) {
	switch entry.Kind() {
	case tomledit.EntryValue:
		node, present := entry.Node()
		if !present {
			return nil, fmt.Errorf("key %q carries no value node", entry.Key())
		}
		return tomlValue(node)
	case tomledit.EntryRecord:
		nested, present := entry.Record()
		if !present {
			return nil, fmt.Errorf("key %q carries no table", entry.Key())
		}
		return tomlRecord(nested)
	case tomledit.EntryRecords:
		records, present := entry.Records()
		if !present {
			return nil, fmt.Errorf("key %q carries no array of tables", entry.Key())
		}
		elements := make([]map[string]any, 0, len(records))
		for _, nested := range records {
			element, err := tomlRecord(nested)
			if err != nil {
				return nil, err
			}
			elements = append(elements, element)
		}
		return elements, nil
	default:
		return nil, fmt.Errorf("key %q holds an unsupported TOML entry of kind %s",
			entry.Key(), entry.Kind())
	}
}

// tomlValue converts one value node into the Go value the callers read.
//
// An array and an inline table written inside an array are read off the AST:
// the read-layer folds a table into a record only where a key binds it, so an
// array's own elements arrive as the nodes they were written as.
func tomlValue(node tomledit.Node) (any, error) {
	switch typed := node.(type) {
	case *tomledit.ArrayNode:
		elements := typed.Elements()
		items := make([]any, 0, len(elements))
		for _, element := range elements {
			item, err := tomlValue(element)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case *tomledit.InlineTableNode:
		table := map[string]any{}
		for _, child := range typed.Children() {
			pair, ok := child.(*tomledit.KeyValueNode)
			if !ok {
				continue
			}
			if err := setTOMLPair(table, pair); err != nil {
				return nil, err
			}
		}
		return table, nil
	case tomledit.Scalar:
		return tomlScalar(typed.Value()), nil
	default:
		return nil, fmt.Errorf("unsupported TOML value of kind %s", node.Type())
	}
}

// tomlScalar normalizes a scalar node's payload.
//
// The parser answers a local date, a local time and a local date-time as its
// own three structs; every consumer here -- the Python type names, the
// rendered config tables -- reads a date-time as a time.Time. A local flavor
// carries no zone and is read in the machine's zone, which is the reading a
// value written without one asks for. Every other payload -- a string, an
// int64, a float64, a bool, an offset date-time's time.Time -- is already the
// shape the callers assert on.
func tomlScalar(value any) any {
	switch typed := value.(type) {
	case tomledit.LocalDateTime:
		return time.Date(
			typed.Year, time.Month(typed.Month), typed.Day,
			typed.Hour, typed.Minute, typed.Second, typed.Nanosecond,
			time.Local,
		)
	case tomledit.LocalDate:
		return time.Date(
			typed.Year, time.Month(typed.Month), typed.Day,
			0, 0, 0, 0, time.Local,
		)
	case tomledit.LocalTime:
		return time.Date(
			0, time.January, 1,
			typed.Hour, typed.Minute, typed.Second, typed.Nanosecond,
			time.Local,
		)
	default:
		return value
	}
}

// setTOMLPair binds one key-value pair written inside an inline table, creating
// the intermediate tables a dotted key names.
func setTOMLPair(table map[string]any, pair *tomledit.KeyValueNode) error {
	parts := pair.Key().Parts()
	target, err := descendTOMLTable(table, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	value, err := tomlValue(pair.Val())
	if err != nil {
		return err
	}
	target[parts[len(parts)-1]] = value
	return nil
}

// descendTOMLTable walks path from table, creating a table for a segment that
// names nothing yet.
func descendTOMLTable(table map[string]any, path []string) (map[string]any, error) {
	current := table
	for index, segment := range path {
		switch existing := current[segment].(type) {
		case nil:
			created := map[string]any{}
			current[segment] = created
			current = created
		case map[string]any:
			current = existing
		default:
			return nil, tomlPathError(path[:index+1])
		}
	}
	return current, nil
}

// tomlPathError reports a key whose path runs through something that is not a
// table -- a document the parser accepted but whose keys contradict each other.
func tomlPathError(path []string) error {
	return fmt.Errorf("key %q is declared under a value that is not a table",
		strings.Join(path, "."))
}

// tomlKeyOrder lists every key the document declares, as its path from the
// root, in the order the document writes them.
//
// Document order is a question about how the file was WRITTEN, so it is read
// off the AST rather than the read-layer: the layer folds a dotted key and a
// table header into the same shape, and the order a renderer replays is the
// order of the headers and keys as typed.
func tomlKeyOrder(document *tomledit.Document) [][]string {
	var keys [][]string
	for _, child := range document.Children() {
		switch node := child.(type) {
		case *tomledit.KeyValueNode:
			keys = appendTOMLKeys(keys, nil, node)
		case *tomledit.TableNode:
			keys = append(keys, clonePath(node.KeyPath()))
			keys = appendTOMLTableKeys(keys, node.KeyPath(), node.Children())
		case *tomledit.ArrayTableNode:
			keys = append(keys, clonePath(node.KeyPath()))
			keys = appendTOMLTableKeys(keys, node.KeyPath(), node.Children())
		}
	}
	return keys
}

// appendTOMLTableKeys appends the keys a table body declares, each under the
// path of the header it was written below.
func appendTOMLTableKeys(keys [][]string, prefix []string, children []tomledit.Node) [][]string {
	for _, child := range children {
		if pair, ok := child.(*tomledit.KeyValueNode); ok {
			keys = appendTOMLKeys(keys, prefix, pair)
		}
	}
	return keys
}

// appendTOMLKeys appends one key-value pair's own key, then the keys of the
// inline table it binds, if that is what it binds.
func appendTOMLKeys(keys [][]string, prefix []string, pair *tomledit.KeyValueNode) [][]string {
	path := append(clonePath(prefix), pair.Key().Parts()...)
	keys = append(keys, path)
	if inline, ok := pair.Val().(*tomledit.InlineTableNode); ok {
		for _, child := range inline.Children() {
			if nested, ok := child.(*tomledit.KeyValueNode); ok {
				keys = appendTOMLKeys(keys, path, nested)
			}
		}
	}
	return keys
}

// clonePath copies a key path, so an appended segment never writes into the
// AST's own slice or into a path already recorded.
func clonePath(path []string) []string {
	return append(make([]string, 0, len(path)), path...)
}
