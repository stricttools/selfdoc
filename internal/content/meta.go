package content

import (
	"sort"

	"github.com/stricttools/selfdoc/internal/catalog"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/tables"
)

// ResolveTableDirectives produces a Markdown table of every core built-in
// directive, in name order.
func ResolveTableDirectives() (string, error) {
	core := catalog.Core()
	names := append([]string(nil), core.Names()...)
	sort.Strings(names)

	rows := make([][]string, 0, len(names))
	for _, name := range names {
		spec, _ := core.Spec(name)
		rows = append(rows, []string{"`" + name + "`", spec.Description})
	}
	return tables.RenderMarkdownTable(
		[]string{"Directive", "Description"}, rows, nil, false,
	)
}

// ResolveTableConfigSchema produces a Markdown table of the selfdoc.json
// configuration fields, in schema order.
//
// A field marked internal is left out: it is a runtime key rather than
// something an author writes, so documenting it would invite a declaration
// that is not one.
func ResolveTableConfigSchema() (string, error) {
	var rows [][]string
	for _, spec := range config.Schema {
		if spec.Internal {
			continue
		}
		required := "no"
		if spec.Required {
			required = "yes"
		}
		rows = append(rows, []string{
			"`" + spec.Name + "`", required, spec.Description,
		})
	}
	return tables.RenderMarkdownTable(
		[]string{"Field", "Required", "Description"}, rows, nil, false,
	)
}
