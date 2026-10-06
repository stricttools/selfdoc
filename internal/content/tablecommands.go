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
// The .strictmetadata/.cli-schema/schema.json is discovered by walking the
// project root. When
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
				"table-commands: no %s found in "+
					`schema-dir="%s" (relative to project root). Write it by `+
					"running '%s' in that directory, with %s replaced by the "+
					"program's name.",
				strictclisupport.SchemaRelPath, targetDir,
				strictclisupport.RegenerateCommand(strictclisupport.AppPlaceholder),
				strictclisupport.AppPlaceholder,
			)}
		}
		return commandTable(structure, targetDir)
	}

	candidates := strictclisupport.DiscoverSchemaDirs(baseDir)
	if len(candidates) == 0 {
		return "", &strictclisupport.SchemaDiscoveryError{Message: fmt.Sprintf(
			"table-commands: no %s found under the project root. Write one "+
				"by running '%s' in the directory of the program's module, "+
				"with %s replaced by the program's name, or select one with "+
				`schema-dir="<dir>".`,
			strictclisupport.SchemaRelPath,
			strictclisupport.RegenerateCommand(strictclisupport.AppPlaceholder),
			strictclisupport.AppPlaceholder,
		)}
	}
	if len(candidates) > 1 {
		return "", &strictclisupport.SchemaDiscoveryError{Message: fmt.Sprintf(
			"table-commands: multiple %s found (%s). "+
				`Disambiguate with schema-dir="<dir>" naming the directory `+
				"that contains the .strictmetadata/ folder.",
			strictclisupport.SchemaRelPath, strings.Join(candidates, ", "),
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
			"table-commands: failed to read %s in '%s'.",
			strictclisupport.SchemaRelPath, targetDir,
		)}
	}
	return commandTable(structure, targetDir)
}

// commandTable renders one row per command, and for each group a heading row
// followed by its subcommands and then its nested groups, at any depth. Every
// row names the full command path.
func commandTable(structure *strictclisupport.Structure, targetDir string) (string, error) {
	var rows [][]string
	for _, cmd := range structure.Commands {
		name := strictclisupport.CommandName(cmd)
		rows = append(rows, []string{"`" + name + "`", strictclisupport.CommandHelp(cmd)})
	}
	for _, group := range structure.Groups {
		rows = groupRows(rows, group)
	}
	if len(rows) == 0 {
		return marker("no commands found in '%s'", targetDir), nil
	}
	return tables.RenderMarkdownTable(
		[]string{"Command", "Description"}, rows, nil, false,
	)
}

// groupRows appends a group's heading row and its subcommands, then the same
// for each nested group, with every path prefixed by the enclosing groups.
func groupRows(rows [][]string, group *strictclisupport.Object) [][]string {
	_ = strictclisupport.WalkGroup(group, func(path string, grp *strictclisupport.Object) error {
		rows = append(rows, []string{"**" + path + "**", strictclisupport.CommandHelp(grp)})
		for _, cmd := range strictclisupport.GroupCommands(grp) {
			rows = append(rows, []string{
				"`" + path + " " + strictclisupport.CommandName(cmd) + "`",
				strictclisupport.CommandHelp(cmd),
			})
		}
		return nil
	})
	return rows
}
