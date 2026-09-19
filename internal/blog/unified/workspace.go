package unified

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// workspaceSearchDepth is how many directory levels up from the docs-site the
// search for an rlsbl workspace climbs before giving up.
const workspaceSearchDepth = 10

// validateRlsblWorkspace refuses a unified config that leaves one of the
// surrounding rlsbl workspace's documented projects undeclared.
//
// It climbs from docsSiteDir looking for ".rlsbl-monorepo/workspace.toml".
// A checkout with no such file is not an rlsbl workspace and there is nothing
// to check. When there is one, every workspace project carrying a
// selfdoc.json has to appear in either "unified.projects" or
// "unified.exclude": a project with documentation that no one declared would
// otherwise be silently missing from the site.
//
// The docs-site itself is exempt -- it is the site, not a constituent of it.
func validateRlsblWorkspace(docsSiteDir string, unifiedConfig map[string]any) error {
	workspaceTOML := findWorkspaceTOML(docsSiteDir)
	if workspaceTOML == "" {
		return nil
	}
	monorepoRoot := filepath.Dir(filepath.Dir(workspaceTOML))

	workspace, err := util.DecodeTOMLFile(workspaceTOML)
	if err != nil {
		return err
	}
	workspaceProjects := workspaceProjectList(workspace["projects"])
	if len(workspaceProjects) == 0 {
		return nil
	}

	// The declared paths are resolved against docsSiteDir as written, which
	// is what the comparison below is against.
	knownPaths := map[string]bool{}
	for _, entry := range unifiedProjects(unifiedConfig) {
		knownPaths[normJoin(docsSiteDir, util.PythonStrOrEmpty(entry["path"]))] = true
	}
	patterns := configExcludePatterns(unifiedConfig)

	for _, raw := range workspaceProjects {
		// A workspace.toml states its members either as bare path strings
		// or as tables carrying a "path" key; anything else is not a
		// member declaration and is passed over.
		var wpPath string
		switch value := raw.(type) {
		case string:
			wpPath = value
		case map[string]any:
			wpPath = util.PythonStrOrEmpty(value["path"])
		default:
			continue
		}

		absWP := normJoin(monorepoRoot, wpPath)
		if !isFile(filepath.Join(absWP, "selfdoc.json")) {
			continue
		}
		if absPath(absWP) == absPath(docsSiteDir) {
			continue
		}
		if knownPaths[absWP] {
			continue
		}

		excluded, err := matchesAnyExclude(patterns, pythonBasename(absWP))
		if err != nil {
			return err
		}
		if excluded {
			continue
		}

		return &config.ConfigError{Message: fmt.Sprintf(
			"rlsbl workspace project '%s' has selfdoc.json but is neither in "+
				"unified.projects nor unified.exclude. Add it to one.", wpPath)}
	}
	return nil
}

// workspaceProjectList normalizes a workspace.toml "projects" value into the
// elements it declares.
//
// The two spellings decode differently: "projects = [...]" yields a list of
// values, while a repeated "[[projects]]" table yields a list of tables, which
// the TOML decoder hands over as its own slice type rather than as a slice of
// anything.
func workspaceProjectList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		entries := make([]any, 0, len(typed))
		for _, entry := range typed {
			entries = append(entries, entry)
		}
		return entries
	default:
		return nil
	}
}

// findWorkspaceTOML climbs from docsSiteDir looking for the rlsbl workspace
// declaration, and answers "" when no ancestor within reach carries one.
func findWorkspaceTOML(docsSiteDir string) string {
	current := absPath(docsSiteDir)
	for range workspaceSearchDepth {
		candidate := filepath.Join(current, ".rlsbl-monorepo", "workspace.toml")
		if isFile(candidate) {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
	return ""
}

// matchesAnyExclude reports whether name matches one of the exclude patterns.
//
// A pattern is a glob-shaped string compiled as a regular expression: "*"
// becomes ".*" and the match is anchored at the start of the name and nowhere
// else -- so "internal" excludes "internal-tools" as well. That is what the
// Python surface these patterns were written against did, quirks included,
// and the declared patterns depend on it.
func matchesAnyExclude(patterns []string, name string) (bool, error) {
	for _, pattern := range patterns {
		expression, err := regexp.Compile("^" + strings.ReplaceAll(pattern, "*", ".*"))
		if err != nil {
			return false, &config.ConfigError{Message: fmt.Sprintf(
				"unified.exclude pattern '%s' is not a valid expression: %v", pattern, err)}
		}
		if expression.MatchString(name) {
			return true, nil
		}
	}
	return false, nil
}
