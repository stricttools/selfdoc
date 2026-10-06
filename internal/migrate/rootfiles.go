package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/gen"
)

// rootFilesConversion converts every root_files entry of a selfdoc.json that
// names only its template into the object that names its template and its
// outputs, the outputs being the files selfdoc generated from such an entry
// ([gen.ConvertedRootFileOutput]). Only the root_files value is rewritten;
// every other byte of text is kept.
//
// It returns the converted text, one change line per converted entry, and
// every entry as it stands once converted. A text whose entries all name their
// outputs comes back unchanged, with no change lines. A plain-string entry
// whose basename carries no underscore generated nothing, so no output can be
// named for it, and is refused.
func rootFilesConversion(text string) (string, []string, []config.RootFile, error) {
	start, end, found, err := topLevelValueSpan(text, "root_files")
	if err != nil || !found {
		return text, nil, nil, err
	}
	var items []any
	if err := json.Unmarshal([]byte(text[start:end]), &items); err != nil {
		return "", nil, nil, fmt.Errorf("selfdoc.json's root_files is not a list: %w", err)
	}
	var changes []string
	var rootFiles []config.RootFile
	converted := make([]any, 0, len(items))
	for _, item := range items {
		template, isString := item.(string)
		if !isString {
			converted = append(converted, item)
			rootFiles = append(rootFiles, config.RootFiles(map[string]any{"root_files": []any{item}})...)
			continue
		}
		output, named := gen.ConvertedRootFileOutput(template)
		if !named {
			return "", nil, nil, fmt.Errorf(
				"selfdoc.json's root_files entry %s is a template whose basename does not start with '_', "+
					"so it generated nothing and the migration cannot name its outputs. Rename the template "+
					"with a leading '_', or remove the entry, and run this again",
				jsonString(template))
		}
		converted = append(converted, orderedRootFile{Template: template, Outputs: []string{output}})
		rootFiles = append(rootFiles, config.RootFile{Template: template, Outputs: []string{output}})
		changes = append(changes, fmt.Sprintf("root_files entry %s -> outputs [%s]", jsonString(template), jsonString(output)))
	}
	if len(changes) == 0 {
		return text, nil, rootFiles, nil
	}
	indent := lineIndent(text, start)
	unit := indent
	if unit == "" {
		unit = "  "
	}
	var rendered bytes.Buffer
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent(indent, unit)
	if err := encoder.Encode(converted); err != nil {
		return "", nil, nil, err
	}
	return text[:start] + strings.TrimSuffix(rendered.String(), "\n") + text[end:], changes, rootFiles, nil
}

// orderedRootFile renders a converted entry with its template before its
// outputs, the order the documentation writes them in.
type orderedRootFile struct {
	Template string   `json:"template"`
	Outputs  []string `json:"outputs"`
}

// topLevelValueSpan finds the value of one key of a JSON object document: the
// byte offsets of its first and one-past-last characters.
func topLevelValueSpan(text, key string) (int, int, bool, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return 0, 0, false, fmt.Errorf("selfdoc.json is not a JSON object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return 0, 0, false, fmt.Errorf("selfdoc.json is not valid JSON: %w", err)
		}
		name, _ := token.(string)
		before := int(decoder.InputOffset())
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return 0, 0, false, fmt.Errorf("selfdoc.json is not valid JSON: %w", err)
		}
		if name != key {
			continue
		}
		end := int(decoder.InputOffset())
		start := before + strings.Index(text[before:end], string(value[:1]))
		return start, end, true, nil
	}
	return 0, 0, false, nil
}

// lineIndent is the leading whitespace of the line holding offset.
func lineIndent(text string, offset int) string {
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	line := text[lineStart:offset]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
