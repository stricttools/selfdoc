package content

import (
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/tables"
)

// ResolveTableLints produces a Markdown table of every lint code selfdoc can
// emit, in the registry's documentation order.
//
// A lint's severity and its one-line description are declared once, in the
// embedded lint registry, and a page that repeats them in prose is a second
// copy: the two go out of step the first time a code is added, renamed or
// changes severity, and nothing fails when they do. This directive makes the
// page read the registry instead -- the same move list-crawlers makes for the
// crawler policy.
func ResolveTableLints() (string, error) {
	registry := lints.Registered()
	codes := registry.Codes()
	rows := make([][]string, 0, len(codes))
	for _, code := range codes {
		spec, _ := registry.Spec(code)
		rows = append(rows, []string{code, spec.Severity, spec.Description})
	}
	return tables.RenderMarkdownTable(
		[]string{"Code", "Severity", "What it checks"}, rows, nil, false,
	)
}
