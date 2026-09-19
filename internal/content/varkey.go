package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// VersionOverrideKey is a runtime-only config key carrying an explicit project
// version.
//
// It is never read from or written to selfdoc.json: the callers that own it
// (gen and check, under their --version-override flag) inject it into the
// already-loaded config, which is the object that reaches every directive
// resolver. That single injection point covers root-file generation at gen
// time and site pages at build time.
const VersionOverrideKey = "_version_override"

// ResolveVar interpolates a project metadata value.
func ResolveVar(attrs map[string]string, config map[string]any, baseDir string) (string, error) {
	key := attrs["key"]
	if key == "" {
		return marker("var requires a key attribute"), nil
	}

	switch key {
	case "project.language":
		source, _ := config["source"].([]any)
		if len(source) == 0 {
			// project.language is derived from the source entries; a
			// codeless project has none. Returning "unknown" would
			// silently put a placeholder word in the rendered page.
			return "", fmt.Errorf(
				`Directive :-: var key="project.language" is derived from ` +
					"the source code, but selfdoc.json declares no 'source' " +
					"entries, so there is no language to report. Either " +
					`remove the directive, or declare the code: "source": ` +
					`[{"path": "src/", "language": "python"}]`,
			)
		}
		// Unique languages, in first-appearance order.
		seen := map[string]bool{}
		var languages []string
		for _, raw := range source {
			language := "unknown"
			if entry, isObject := raw.(map[string]any); isObject {
				if declared, isString := entry["language"].(string); isString {
					language = declared
				}
			}
			if seen[language] {
				continue
			}
			seen[language] = true
			languages = append(languages, language)
		}
		if len(languages) == 0 {
			return "unknown", nil
		}
		return strings.Join(languages, ", "), nil

	case "project.description":
		if description, isString := config["description"].(string); isString && description != "" {
			return description, nil
		}
		// Fall through to the project's own manifest.
		return readProjectDescription(baseDir), nil

	case "project.name":
		return util.ReadProjectField(baseDir, "name"), nil

	case "project.version":
		if override := config[VersionOverrideKey]; truthy(override) {
			return util.PythonStr(override), nil
		}
		return util.ReadProjectField(baseDir, "version"), nil

	case "topology.docs_url":
		topology, _ := config["topology"].(map[string]any)
		docsBase, _ := topology["docs_base"].(string)
		slug, _ := topology["slug"].(string)
		if docsBase != "" && slug != "" {
			return docsBase + "/" + slug, nil
		}
		return "", nil

	case "topology.posts_url":
		topology, _ := config["topology"].(map[string]any)
		postsBase, _ := topology["posts_base"].(string)
		return postsBase, nil

	case "topology.slug":
		topology, _ := config["topology"].(map[string]any)
		slug, _ := topology["slug"].(string)
		return slug, nil
	}

	return marker("unknown var key '%s'", key), nil
}

// readProjectDescription reads the project description from pyproject.toml or
// package.json, answering "unknown" when neither states one.
func readProjectDescription(baseDir string) string {
	pyproject := filepath.Join(baseDir, "pyproject.toml")
	if info, err := os.Stat(pyproject); err == nil && info.Mode().IsRegular() {
		raw, err := os.ReadFile(pyproject)
		if err != nil {
			return "unknown"
		}
		document, err := util.DecodeTOML(raw)
		if err != nil {
			return "unknown"
		}
		project, _ := document["project"].(map[string]any)
		description, _ := project["description"].(string)
		if description == "" {
			return "unknown"
		}
		return description
	}

	packageJSON := filepath.Join(baseDir, "package.json")
	if info, err := os.Stat(packageJSON); err == nil && info.Mode().IsRegular() {
		raw, err := os.ReadFile(packageJSON)
		if err != nil {
			return "unknown"
		}
		var document struct {
			Description *string `json:"description"`
		}
		if err := json.Unmarshal(raw, &document); err != nil {
			return "unknown"
		}
		if document.Description == nil {
			return "unknown"
		}
		return *document.Description
	}

	return "unknown"
}

// truthy reproduces Python's truth test for the decoded config values this
// package reads.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int64:
		return typed != 0
	case int:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
