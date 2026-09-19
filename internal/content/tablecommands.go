package content

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/strictclisupport"
	"github.com/stricttools/selfdoc/internal/tables"
)

// ResolveTableCommands produces a Markdown table of the CLI commands a
// strictcli schema declares.
//
// The .strictcli/schema.json is discovered by walking the project root. When
// exactly one schema is found it is used; zero and several are both hard
// errors, and schema-dir="<dir>" selects one explicitly when discovery is
// ambiguous.
func ResolveTableCommands(attrs map[string]string, config map[string]any, baseDir string) (string, error) {
	targetDir := attrs["schema-dir"]
	if targetDir != "" {
		structure, err := strictclisupport.ReadSchemaJSON(filepath.Join(baseDir, targetDir))
		if err != nil {
			return "", err
		}
		if structure == nil {
			return "", &strictclisupport.SchemaDiscoveryError{Message: fmt.Sprintf(
				"table-commands: no .strictcli/schema.json found in "+
					`schema-dir="%s" (relative to project root).`, targetDir,
			)}
		}
		return commandTable(structure, targetDir)
	}

	candidates := strictclisupport.DiscoverSchemaDirs(baseDir)
	if len(candidates) == 0 {
		return "", &strictclisupport.SchemaDiscoveryError{Message: "table-commands: " +
			"no .strictcli/schema.json found under the project root. " +
			"Generate one with '<app> --dump-schema', or select it with " +
			`schema-dir="<dir>".`,
		}
	}
	if len(candidates) > 1 {
		return "", &strictclisupport.SchemaDiscoveryError{Message: fmt.Sprintf(
			"table-commands: multiple .strictcli/schema.json found (%s). "+
				`Disambiguate with schema-dir="<dir>" naming the directory `+
				"that contains the .strictcli/ folder.",
			strings.Join(candidates, ", "),
		)}
	}
	targetDir = candidates[0]
	structure, err := strictclisupport.ReadSchemaJSON(filepath.Join(baseDir, targetDir))
	if err != nil {
		return "", err
	}
	if structure == nil {
		// Discovery already confirmed the file exists.
		return "", &strictclisupport.SchemaDiscoveryError{Message: fmt.Sprintf(
			"table-commands: failed to read .strictcli/schema.json in '%s'.",
			targetDir,
		)}
	}
	return commandTable(structure, targetDir)
}

// commandTable renders one row per command, and for each group a heading row
// followed by its subcommands.
func commandTable(structure *strictclisupport.Structure, targetDir string) (string, error) {
	var rows [][]string
	for _, cmd := range structure.Commands {
		name := strictclisupport.CommandName(cmd)
		rows = append(rows, []string{"`" + name + "`", strictclisupport.CommandHelp(cmd)})
	}
	for _, group := range structure.Groups {
		groupName := strictclisupport.CommandName(group)
		rows = append(rows, []string{
			"**" + groupName + "**", strictclisupport.CommandHelp(group),
		})
		for _, cmd := range strictclisupport.GroupCommands(group) {
			rows = append(rows, []string{
				"`" + groupName + " " + strictclisupport.CommandName(cmd) + "`",
				strictclisupport.CommandHelp(cmd),
			})
		}
	}
	if len(rows) == 0 {
		return marker("no commands found in '%s'", targetDir), nil
	}
	return tables.RenderMarkdownTable(
		[]string{"Command", "Description"}, rows, nil, false,
	)
}
