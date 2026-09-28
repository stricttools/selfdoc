package content

import (
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
)

// TestResolveTableLintsRendersTheWholeRegistry pins the directive that keeps
// the check guide's lint table from being a second copy of the registry: every
// registered lint renders, in the registry's own documentation order, with the
// severity and description the registry declares.
func TestResolveTableLintsRendersTheWholeRegistry(t *testing.T) {
	rendered, err := ResolveTableLints()
	if err != nil {
		t.Fatalf("ResolveTableLints: %v", err)
	}
	registry := lints.Registered()

	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	if len(lines) != registry.Len()+2 {
		t.Fatalf("rendered %d lines, want %d rows plus a header and its rule",
			len(lines), registry.Len())
	}
	for _, want := range []string{"Name", "Severity", "What it checks"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the header does not name %q: %s", want, lines[0])
		}
	}

	for index, name := range registry.Names() {
		row := lines[index+2]
		spec, _ := registry.Spec(name)
		for _, want := range []string{name, spec.Severity, spec.Description} {
			if !strings.Contains(row, want) {
				t.Errorf("the row for %s does not carry %q: %s", name, want, row)
			}
		}
	}
}

// TestResolveTableLintsIsDispatched pins that the directive is reachable by
// name, not only as a function.
func TestResolveTableLintsIsDispatched(t *testing.T) {
	rendered, handled, err := ResolveContent("table-lints", nil, nil, "", nil)
	if err != nil {
		t.Fatalf("ResolveContent: %v", err)
	}
	if !handled {
		t.Fatal("table-lints is not dispatched by ResolveContent")
	}
	if !strings.Contains(rendered, "multiple-top-level-headings") {
		t.Errorf("the dispatched directive rendered no registry row:\n%s", rendered)
	}
}
