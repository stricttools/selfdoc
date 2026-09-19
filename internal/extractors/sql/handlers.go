package sql

import (
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/tables"
)

// typeLabel is the one-line shape of a user-defined type: its labels, or its
// fields with their types.
//
// Every name in it was read out of the DDL, so each is set as a code span
// rather than run together into a sentence.
func typeLabel(t userType) string {
	if t.kind == "enum" {
		spans := make([]string, 0, len(t.values))
		for _, value := range t.values {
			spans = append(spans, extractors.SymbolSpan(value))
		}
		return "ENUM: " + strings.Join(spans, ", ")
	}

	spans := make([]string, 0, len(t.fields))
	for _, field := range t.fields {
		spans = append(spans, extractors.SymbolSpan(field.name+" "+field.fieldType))
	}
	return "COMPOSITE: " + strings.Join(spans, ", ")
}

// handleRef lists every object a DDL document creates with its COMMENT ON
// description, grouped by kind: tables, then views, then types, then functions.
func handleRef(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::ref requires a file path argument"), nil
	}

	resolved := resolveSQLPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(resolved)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	tableList := parseCreateTable(source)
	viewList := parseCreateView(source)
	typeList := parseCreateType(source)
	functionList := parseCreateFunction(source)

	if len(tableList) == 0 && len(viewList) == 0 &&
		len(typeList) == 0 && len(functionList) == 0 {
		return extractors.FormatError("no CREATE objects found in '" + path + "'"), nil
	}

	comments := parseComments(source)

	if target != nil && *target != "" {
		return refTarget(*target, path, tableList, viewList, typeList, functionList, comments), nil
	}

	var parts []string
	parts = append(parts, extractors.SymbolHeading(2, filepath.Base(resolved)))

	if len(tableList) > 0 {
		parts = append(parts, "", "### Tables", "")
		for _, t := range tableList {
			parts = append(parts, listEntry(
				t.name, lookupComment(comments, "table", t.name, t.schema), ""))
		}
	}

	if len(viewList) > 0 {
		parts = append(parts, "", "### Views", "")
		for _, v := range viewList {
			parts = append(parts, listEntry(
				v.name, lookupComment(comments, "view", v.name, v.schema), ""))
		}
	}

	if len(typeList) > 0 {
		parts = append(parts, "", "### Types", "")
		for _, t := range typeList {
			parts = append(parts, listEntry(
				t.name, lookupComment(comments, "type", t.name, t.schema), typeLabel(t)))
		}
	}

	if len(functionList) > 0 {
		parts = append(parts, "", "### Functions", "")
		for _, f := range functionList {
			parts = append(parts, listEntry(
				f.name, lookupComment(comments, "function", f.name, f.schema), ""))
		}
	}

	return strings.Join(parts, "\n"), nil
}

// listEntry is one line of a ref listing: the object's name, the shape a type
// carries, and its description, each present only when there is one.
func listEntry(name, description, label string) string {
	entry := "- " + extractors.SymbolSpan(name)
	if label != "" {
		entry += " -- " + label
	}
	if description != "" {
		entry += " -- " + description
	}
	return entry
}

// refTarget renders the one object a ref directive named, searched for among
// the tables, then the views, then the types, then the functions.
func refTarget(
	target, path string,
	tableList []table,
	viewList []view,
	typeList []userType,
	functionList []function,
	comments *commentSet,
) string {
	for _, t := range tableList {
		if t.name == target {
			return headingWithDescription(
				t.name, lookupComment(comments, "table", t.name, t.schema))
		}
	}
	for _, v := range viewList {
		if v.name == target {
			return headingWithDescription(
				v.name, lookupComment(comments, "view", v.name, v.schema))
		}
	}
	for _, t := range typeList {
		if t.name == target {
			parts := []string{extractors.SymbolHeading(3, t.name), "", typeLabel(t)}
			if desc := lookupComment(comments, "type", t.name, t.schema); desc != "" {
				parts = append(parts, "", extractors.DemoteDocHeadings(desc, 3))
			}
			return strings.Join(parts, "\n")
		}
	}
	for _, f := range functionList {
		if f.name == target {
			return headingWithDescription(
				f.name, lookupComment(comments, "function", f.name, f.schema))
		}
	}
	return extractors.FormatError("symbol '" + target + "' not found in '" + path + "'")
}

// headingWithDescription is an object's heading, with its description beneath
// it when the document carries one.
func headingWithDescription(name, description string) string {
	heading := extractors.SymbolHeading(3, name)
	if description == "" {
		return heading
	}
	return heading + "\n\n" + extractors.DemoteDocHeadings(description, 3)
}

// handleProseDesc renders the COMMENT ON TABLE text of the table a directive
// names.
//
// The target is required: a DDL document describes many tables and no single
// thing, so there is no prose to render without being told which table's.
func handleProseDesc(
	path string,
	target *string,
	_ []string,
	sourcePaths []string,
	baseDir string,
	_ map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::prose-desc requires a file path argument"), nil
	}

	if target == nil || *target == "" {
		return extractors.FormatError(
			"prose-desc requires a target table name for SQL files"), nil
	}

	resolved := resolveSQLPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(resolved)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	comments := parseComments(source)

	// A bare target matches a schema-qualified statement too, since the table
	// it names is the same table.
	for _, key := range comments.Keys() {
		if key.objType != "table" {
			continue
		}
		bareName := key.objName
		if dotIdx := strings.LastIndex(bareName, "."); dotIdx >= 0 {
			bareName = bareName[dotIdx+1:]
		}
		if bareName == *target || key.objName == *target {
			return comments.get(key.objType, key.objName), nil
		}
	}

	return extractors.FormatError("no comment found for table '" + *target + "'"), nil
}

// handleTableSchema renders a table's columns as a table. A JSON or TOML path
// is a config file rather than DDL, and is rendered as one.
//
// A document with one table needs no target; one with several renders them all,
// each under its own heading.
func handleTableSchema(
	path string,
	target *string,
	body []string,
	sourcePaths []string,
	baseDir string,
	attrs map[string]string,
) (string, error) {
	if path == "" {
		return extractors.FormatError(":::table-schema requires a file path argument"), nil
	}

	if strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".toml") {
		return extractors.HandleTableConfig(path, nil, body, sourcePaths, baseDir, attrs)
	}

	resolved := resolveSQLPath(path, sourcePaths, baseDir)
	if resolved == "" {
		return extractors.FormatError("'" + path + "' not found"), nil
	}

	source, err := extractors.ReadSource(resolved)
	if err != nil {
		return extractors.FormatError("cannot read '" + path + "': " + err.Error()), nil
	}

	tableList := parseCreateTable(source)
	if len(tableList) == 0 {
		return extractors.FormatError("no tables found in '" + path + "'"), nil
	}

	comments := parseComments(source)

	// A column's description is its COMMENT ON COLUMN.
	for ti := range tableList {
		for ci := range tableList[ti].columns {
			desc := lookupColumnComment(
				comments,
				tableList[ti].name,
				tableList[ti].columns[ci].name,
				tableList[ti].schema,
			)
			if desc != "" {
				tableList[ti].columns[ci].description = desc
			}
		}
	}

	if target != nil && *target != "" {
		for _, t := range tableList {
			if t.name == *target {
				return formatTableSchema(t)
			}
		}
		return extractors.FormatError(
			"table '" + *target + "' not found in '" + path + "'"), nil
	}

	if len(tableList) == 1 {
		return formatTableSchema(tableList[0])
	}

	var results []string
	for _, t := range tableList {
		results = append(results, extractors.SymbolHeading(3, t.name), "")
		rendered, err := formatTableSchema(t)
		if err != nil {
			return "", err
		}
		results = append(results, rendered)
	}
	return strings.Join(results, "\n"), nil
}

// formatTableSchema renders a table's columns as the schema table.
func formatTableSchema(t table) (string, error) {
	rows := make([][]string, 0, len(t.columns))
	for _, col := range t.columns {
		nullableDisplay := ""
		if !col.nullable {
			nullableDisplay = "NOT NULL"
		}
		rows = append(rows, []string{
			"`" + col.name + "`",
			"`" + col.colType + "`",
			nullableDisplay,
			col.defaultValue,
			col.description,
		})
	}
	return tables.RenderMarkdownTable(
		[]string{"Column", "Type", "Nullable", "Default", "Description"}, rows, nil, false,
	)
}
