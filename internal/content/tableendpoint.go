package content

import (
	"os"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/tables"
	"github.com/stricttools/selfdoc/internal/util"
)

// pathItemNonMethods are the keys of an OpenAPI path item that are not
// operations.
var pathItemNonMethods = map[string]bool{
	"summary": true, "description": true, "servers": true, "parameters": true,
}

// ResolveTableEndpoint renders REST API endpoint documentation from an OpenAPI
// 3.x JSON specification.
func ResolveTableEndpoint(attrs map[string]string, baseDir string) (string, error) {
	path := attrs["path"]
	if path == "" {
		return marker("table-endpoint requires a path attribute"), nil
	}

	fullPath := util.ResolveDirectivePath(baseDir, path)
	if info, err := os.Stat(fullPath); err != nil || !info.Mode().IsRegular() {
		return marker("file '%s' not found", path), nil
	}

	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return marker("cannot read '%s': %s", path, err), nil
	}
	decoded, err := extractors.DecodeJSON(raw)
	if err != nil {
		return marker("invalid JSON in '%s': %s", path, err), nil
	}
	spec := asJSONObject(decoded)
	pathsObj := jsonObject(spec, "paths")
	if jsonLen(pathsObj) == 0 {
		return marker("no paths found in '%s'", path), nil
	}

	endpointFilter := attrs["endpoint"]
	methodFilter := strings.ToLower(attrs["method"])

	type operation struct {
		endpoint string
		method   string
		body     *object
	}
	var operations []operation
	endpointPaths := append([]string(nil), jsonKeys(pathsObj)...)
	sort.Strings(endpointPaths)
	for _, endpointPath := range endpointPaths {
		if endpointFilter != "" && !strings.HasPrefix(endpointPath, endpointFilter) {
			continue
		}
		pathItem := jsonObject(pathsObj, endpointPath)
		if pathItem == nil {
			continue
		}
		methods := append([]string(nil), jsonKeys(pathItem)...)
		sort.Strings(methods)
		for _, method := range methods {
			if strings.HasPrefix(method, "x-") || pathItemNonMethods[method] {
				continue
			}
			if methodFilter != "" && strings.ToLower(method) != methodFilter {
				continue
			}
			if body := jsonObject(pathItem, method); body != nil {
				operations = append(operations, operation{
					endpoint: endpointPath,
					method:   strings.ToUpper(method),
					body:     body,
				})
			}
		}
	}

	if len(operations) == 0 {
		return marker("no matching endpoints in '%s'", path), nil
	}

	sections := make([]string, 0, len(operations))
	for _, op := range operations {
		section, err := renderEndpoint(op.endpoint, op.method, op.body, spec)
		if err != nil {
			return "", err
		}
		sections = append(sections, section)
	}
	return strings.Join(sections, "\n\n"), nil
}

// resolveRef resolves a JSON $ref pointer within the specification, or nil
// when it names nothing.
func resolveRef(ref string, spec *object) *object {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var current any = spec
	for _, part := range strings.Split(ref[2:], "/") {
		// JSON pointer escaping.
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		obj := asJSONObject(current)
		if obj == nil || !jsonHas(obj, part) {
			return nil
		}
		current = jsonGet(obj, part)
	}
	return asJSONObject(current)
}

// resolveSchema follows a schema's $ref when it declares one.
func resolveSchema(schema *object, spec *object) *object {
	if jsonHas(schema, "$ref") {
		if resolved := resolveRef(jsonString(schema, "$ref"), spec); resolved != nil {
			return resolved
		}
	}
	return schema
}

// extractType renders a human-readable type string for a JSON Schema object.
func extractType(schema *object, spec *object) string {
	schema = resolveSchema(schema, spec)

	if jsonHas(schema, "allOf") {
		// A composed schema is an object whatever it composes.
		return "object"
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if !jsonHas(schema, key) {
			continue
		}
		alternatives := jsonList(schema, key)
		parts := make([]string, 0, len(alternatives))
		for _, raw := range alternatives {
			sub := resolveSchema(asJSONObject(raw), spec)
			parts = append(parts, extractType(sub, spec))
		}
		return strings.Join(parts, " | ")
	}

	schemaType := "object"
	if jsonHas(schema, "type") {
		// A declared type is normally a string; a document that declares
		// something else gets that value rendered rather than a guess.
		if declared, isString := jsonGet(schema, "type").(string); isString {
			schemaType = declared
		} else {
			schemaType = util.PythonStr(jsonGet(schema, "type"))
		}
	}
	if schemaType == "array" {
		return "array[" + extractType(jsonObject(schema, "items"), spec) + "]"
	}
	return schemaType
}

// property is one documented field of a schema.
type property struct {
	name        string
	kind        string
	required    bool
	description string
}

// extractProperties lists a schema's properties in name order, merging the
// members of an allOf composition.
func extractProperties(schema *object, spec *object) []property {
	schema = resolveSchema(schema, spec)
	var properties []property

	requiredSet := map[string]bool{}
	for _, name := range jsonList(schema, "required") {
		if declared, isString := name.(string); isString {
			requiredSet[declared] = true
		}
	}

	if jsonHas(schema, "allOf") {
		merged := map[string]*object{}
		for _, raw := range jsonList(schema, "allOf") {
			sub := resolveSchema(asJSONObject(raw), spec)
			for _, name := range jsonList(sub, "required") {
				if declared, isString := name.(string); isString {
					requiredSet[declared] = true
				}
			}
			props := jsonObject(sub, "properties")
			for _, name := range jsonKeys(props) {
				merged[name] = asJSONObject(jsonGet(props, name))
			}
		}
		names := make([]string, 0, len(merged))
		for name := range merged {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			propSchema := resolveSchema(merged[name], spec)
			properties = append(properties, property{
				name:        name,
				kind:        extractType(propSchema, spec),
				required:    requiredSet[name],
				description: jsonString(propSchema, "description"),
			})
		}
		return properties
	}

	props := jsonObject(schema, "properties")
	names := append([]string(nil), jsonKeys(props)...)
	sort.Strings(names)
	for _, name := range names {
		propSchema := resolveSchema(asJSONObject(jsonGet(props, name)), spec)
		properties = append(properties, property{
			name:        name,
			kind:        extractType(propSchema, spec),
			required:    requiredSet[name],
			description: jsonString(propSchema, "description"),
		})
	}
	return properties
}

// preferredMedia is an operation content object's application/json entry, or
// -- when it declares none -- the first media type the document lists.
func preferredMedia(content *object) *object {
	if media := jsonObject(content, "application/json"); jsonLen(media) > 0 {
		return media
	}
	for _, mediaType := range jsonKeys(content) {
		return asJSONObject(jsonGet(content, mediaType))
	}
	return nil
}

// renderEndpoint renders a single endpoint operation as Markdown.
func renderEndpoint(endpointPath, method string, op *object, spec *object) (string, error) {
	lines := []string{"### `" + method + " " + endpointPath + "`"}

	description := jsonString(op, "description")
	if description == "" {
		description = jsonString(op, "summary")
	}
	if description != "" {
		lines = append(lines, "", description)
	}

	var pathParams, queryParams []*object
	for _, raw := range jsonList(op, "parameters") {
		param := asJSONObject(raw)
		if jsonHas(param, "$ref") {
			resolved := resolveRef(jsonString(param, "$ref"), spec)
			if resolved == nil {
				continue
			}
			param = resolved
		}
		switch jsonString(param, "in") {
		case "path":
			pathParams = append(pathParams, param)
		case "query":
			queryParams = append(queryParams, param)
		}
	}

	for _, section := range []struct {
		heading string
		params  []*object
	}{
		{"**Path Parameters**", pathParams},
		{"**Query Parameters**", queryParams},
	} {
		if len(section.params) == 0 {
			continue
		}
		rows := make([][]string, 0, len(section.params))
		for _, param := range section.params {
			kind := "string"
			if schema := jsonObject(param, "schema"); jsonLen(schema) > 0 {
				kind = extractType(schema, spec)
			}
			required := "no"
			if truthy(jsonGet(param, "required")) {
				required = "yes"
			}
			rows = append(rows, []string{
				"`" + jsonString(param, "name") + "`", kind, required,
				jsonString(param, "description"),
			})
		}
		table, err := tables.RenderMarkdownTable(
			[]string{"Name", "Type", "Required", "Description"}, rows, nil, false,
		)
		if err != nil {
			return "", err
		}
		lines = append(lines, "", section.heading, "", table)
	}

	requestBody := jsonObject(op, "requestBody")
	if jsonLen(requestBody) > 0 {
		if jsonHas(requestBody, "$ref") {
			if resolved := resolveRef(jsonString(requestBody, "$ref"), spec); resolved != nil {
				requestBody = resolved
			}
		}
		if media := preferredMedia(jsonObject(requestBody, "content")); media != nil {
			bodySchema := resolveSchema(jsonObject(media, "schema"), spec)
			if props := extractProperties(bodySchema, spec); len(props) > 0 {
				rows := make([][]string, 0, len(props))
				for _, prop := range props {
					required := "no"
					if prop.required {
						required = "yes"
					}
					rows = append(rows, []string{
						"`" + prop.name + "`", prop.kind, required, prop.description,
					})
				}
				table, err := tables.RenderMarkdownTable(
					[]string{"Field", "Type", "Required", "Description"},
					rows, nil, false,
				)
				if err != nil {
					return "", err
				}
				lines = append(lines, "", "**Request Body**", "", table)
			}
		}
	}

	responses := jsonObject(op, "responses")
	statusCodes := append([]string(nil), jsonKeys(responses)...)
	sort.Strings(statusCodes)
	for _, statusCode := range statusCodes {
		response := asJSONObject(jsonGet(responses, statusCode))
		if jsonHas(response, "$ref") {
			resolved := resolveRef(jsonString(response, "$ref"), spec)
			if resolved == nil {
				continue
			}
			response = resolved
		}
		media := preferredMedia(jsonObject(response, "content"))
		if media == nil {
			continue
		}
		responseSchema := resolveSchema(jsonObject(media, "schema"), spec)
		props := extractProperties(responseSchema, spec)
		if len(props) == 0 {
			continue
		}
		rows := make([][]string, 0, len(props))
		for _, prop := range props {
			rows = append(rows, []string{
				"`" + prop.name + "`", prop.kind, prop.description,
			})
		}
		table, err := tables.RenderMarkdownTable(
			[]string{"Field", "Type", "Description"}, rows, nil, false,
		)
		if err != nil {
			return "", err
		}
		lines = append(lines, "", "**Response "+statusCode+"**", "", table)
	}

	return strings.Join(lines, "\n"), nil
}
