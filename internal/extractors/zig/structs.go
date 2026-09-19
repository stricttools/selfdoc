package zig

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// structType is one public struct declaration with its fields.
type structType struct {
	Name   string
	Doc    string
	Fields []structField
}

// structField is one field of a struct.
type structField struct {
	Name string
	Type string
	// Default is the field's default value as written, empty when it has
	// none.
	Default string
	// Comment is the field's description: its trailing inline comment, or the
	// /// block above it when it has no inline comment.
	Comment string
}

var (
	// structDecl matches a public struct declaration's opening line.
	structDecl = regexp.MustCompile(
		`^pub` + pySpace + `+const` + pySpace + `+(` + pyWord + `+)` + pySpace + `*=` +
			pySpace + `*struct` + pySpace + `*\{`)

	// structFieldPattern matches a struct field: its name and type, an
	// optional default, an optional trailing comma and an optional inline
	// comment.
	structFieldPattern = regexp.MustCompile(
		`^(` + pyWord + `+)` + pySpace + `*:` + pySpace + `*` +
			`([^=,/]+?)` +
			`(?:` + pySpace + `*=` + pySpace + `*([^,/]+?))?` +
			pySpace + `*,?` + pySpace + `*` +
			`(?://` + pySpace + `*(.*))?` +
			pySpace + `*$`)
)

// extractStructs lists the public struct declarations in a Zig source file
// with their fields.
//
// Only the fields at the struct's own top level are collected: a nested
// struct's fields and a method's locals sit at a deeper brace depth and are
// skipped.
func extractStructs(source string) []structType {
	lines := strings.Split(source, "\n")
	var structs []structType

	for i := 0; i < len(lines); i++ {
		stripped := pyStrip(lines[i])

		match := structDecl.FindStringSubmatch(stripped)
		if match == nil {
			continue
		}

		structName := match[1]
		doc := collectDocCommentAbove(lines, i)

		var fields []structField
		braceDepth := 1
		for j := i + 1; j < len(lines) && braceDepth > 0; j++ {
			fieldLine := pyStrip(lines[j])
			for _, ch := range fieldLine {
				switch ch {
				case '{':
					braceDepth++
				case '}':
					braceDepth--
				}
			}

			if braceDepth == 1 {
				if field, ok := parseStructField(fieldLine, lines, j); ok {
					fields = append(fields, field)
				}
			}
		}

		structs = append(structs, structType{Name: structName, Doc: doc, Fields: fields})
	}

	return structs
}

// parseStructField parses one line of a struct body as a field. It reports
// false for a line that is not a field: a blank line, a comment, a method, or
// anything else the pattern does not describe.
func parseStructField(fieldLine string, lines []string, lineIdx int) (structField, bool) {
	if fieldLine == "" ||
		strings.HasPrefix(fieldLine, "//") ||
		strings.HasPrefix(fieldLine, "pub ") ||
		strings.HasPrefix(fieldLine, "fn ") {
		return structField{}, false
	}

	match := structFieldPattern.FindStringSubmatch(fieldLine)
	if match == nil {
		return structField{}, false
	}

	inlineComment := pyStrip(match[4])
	description := inlineComment
	if description == "" {
		description = collectDocCommentAbove(lines, lineIdx)
	}

	return structField{
		Name:    match[1],
		Type:    pyStrip(match[2]),
		Default: pyStrip(match[3]),
		Comment: description,
	}, true
}

// formatStructTable renders a struct's fields as the Field/Type/Default/
// Description table.
func formatStructTable(s structType) (string, error) {
	rows := make([][]string, 0, len(s.Fields))
	for _, field := range s.Fields {
		defaultDisplay := ""
		if field.Default != "" {
			defaultDisplay = "`" + field.Default + "`"
		}
		rows = append(rows, []string{
			"`" + field.Name + "`",
			"`" + field.Type + "`",
			defaultDisplay,
			field.Comment,
		})
	}
	return extractors.RenderTable(
		[]string{"Field", "Type", "Default", "Description"}, rows)
}
