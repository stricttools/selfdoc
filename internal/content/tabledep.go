package content

import (
	"os"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/tables"
	"github.com/stricttools/selfdoc/internal/util"
)

// depSpecifierRE splits a PEP 508 dependency specifier at the end of its name:
// everything a distribution name may carry, then the rest.
var depSpecifierRE = regexp.MustCompile(`^([A-Za-z0-9_\-.\[\]]+)\s*(.*)`)

// pyprojectDeps is the part of a pyproject.toml this directive reads.
type pyprojectDeps struct {
	Dependencies         []string
	OptionalDependencies map[string][]string
}

// readPyprojectDeps narrows a decoded pyproject.toml to the two dependency
// declarations this directive renders.
//
// Anything that is not a list of strings is not a dependency declaration and
// is passed over: the directive renders what a document states, and a table
// whose [project] says something else has no dependencies to show.
func readPyprojectDeps(document map[string]any) pyprojectDeps {
	deps := pyprojectDeps{OptionalDependencies: map[string][]string{}}
	project, _ := document["project"].(map[string]any)
	deps.Dependencies, _ = dependencyList(project["dependencies"])
	groups, _ := project["optional-dependencies"].(map[string]any)
	for name, value := range groups {
		if specifiers, ok := dependencyList(value); ok {
			deps.OptionalDependencies[name] = specifiers
		}
	}
	return deps
}

// dependencyList narrows a decoded value to the list of specifier strings it
// states, dropping every element that is not one. The second result reports
// whether the value was a list at all, which is what tells a declared but
// empty group apart from a key declaring something else entirely.
func dependencyList(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	specifiers := make([]string, 0, len(items))
	for _, item := range items {
		if specifier, ok := item.(string); ok {
			specifiers = append(specifiers, specifier)
		}
	}
	return specifiers, true
}

// ResolveTableDep parses a pyproject.toml and produces a Markdown dependency
// table: the project's own dependencies, then one labelled block per
// optional-dependency group, in the order the document declares them.
func ResolveTableDep(attrs map[string]string, baseDir string) string {
	path := attrs["path"]
	if path == "" {
		return marker("table-dep requires a path attribute")
	}

	fullPath := util.ResolveDirectivePath(baseDir, path)
	info, err := os.Stat(fullPath)
	if err != nil || !info.Mode().IsRegular() {
		return marker("file '%s' not found", path)
	}

	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return marker("cannot parse '%s': %s", path, err)
	}
	decoded, keys, err := util.DecodeTOMLOrdered(raw)
	if err != nil {
		return marker("cannot parse '%s': %s", path, err)
	}
	document := readPyprojectDeps(decoded)

	var rows [][]string
	for _, spec := range document.Dependencies {
		name, constraint := parseDepSpecifier(spec)
		rows = append(rows, []string{"`" + name + "`", constraint})
	}

	for _, group := range optionalGroupOrder(keys, document) {
		rows = append(rows, []string{"**[" + group + "]**", ""})
		for _, spec := range document.OptionalDependencies[group] {
			name, constraint := parseDepSpecifier(spec)
			rows = append(rows, []string{"`" + name + "`", constraint})
		}
	}

	if len(rows) == 0 {
		return marker("no dependencies found in '%s'", path)
	}

	rendered, err := tables.RenderMarkdownTable(
		[]string{"Package", "Version Constraint"}, rows, nil, false,
	)
	if err != nil {
		return marker("cannot render '%s': %s", path, err)
	}
	return rendered
}

// optionalGroupOrder lists the optional-dependency group names in the order
// the document declares them.
//
// A Go map has no order, so the order comes from the decoder's own record of
// the keys it saw. Reordering a project's extras would misreport the document
// the page claims to show.
func optionalGroupOrder(keys [][]string, document pyprojectDeps) []string {
	groups := make([]string, 0, len(document.OptionalDependencies))
	seen := map[string]bool{}
	for _, parts := range keys {
		if len(parts) != 3 || parts[0] != "project" ||
			parts[1] != "optional-dependencies" {
			continue
		}
		name := parts[2]
		if seen[name] {
			continue
		}
		if _, declared := document.OptionalDependencies[name]; !declared {
			continue
		}
		seen[name] = true
		groups = append(groups, name)
	}
	return groups
}

// parseDepSpecifier splits a PEP 508 dependency specifier into its package and
// its version constraint:
//
//	"requests>=2.0"              -> ("requests", ">=2.0")
//	"flask"                      -> ("flask", "*")
//	"black[jupyter]>=23.0,<24.0" -> ("black[jupyter]", ">=23.0,<24.0")
//
// An environment marker is dropped: it says when the dependency applies, not
// which version.
func parseDepSpecifier(spec string) (string, string) {
	match := depSpecifierRE.FindStringSubmatch(spec)
	if match == nil {
		return strings.TrimSpace(spec), "*"
	}
	name := strings.TrimSpace(match[1])
	constraint := strings.TrimSpace(match[2])
	if index := strings.Index(constraint, ";"); index >= 0 {
		constraint = strings.TrimSpace(constraint[:index])
	}
	if constraint == "" {
		return name, "*"
	}
	return name, constraint
}
