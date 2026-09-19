package listing

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// Load returns the listing declared in the TOML document at path.
func Load(path string) (Listing, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return Listing{}, err
	}
	return Parse(string(text), path)
}

// RenderSidecar returns the JSON the assembly keeps beside the manifests.
//
// The document is written in declaration order rather than sorted, and every
// character outside printable ASCII is escaped, so the bytes are the ones
// Python's json.dumps(document, indent=2) produced.
func RenderSidecar(listing Listing, slug string) string {
	var out strings.Builder
	out.WriteString("{\n")
	out.WriteString("  \"format_version\": 1,\n")
	out.WriteString("  \"slug\": " + util.PythonJSONString(slug) + ",\n")
	if len(listing.Categories) == 0 {
		out.WriteString("  \"categories\": []\n}\n")
		return out.String()
	}
	out.WriteString("  \"categories\": [\n")
	for i, category := range listing.Categories {
		out.WriteString("    {\n")
		out.WriteString("      \"name\": " + util.PythonJSONString(category.Name) + ",\n")
		if len(category.Projects) == 0 {
			out.WriteString("      \"projects\": []\n")
		} else {
			out.WriteString("      \"projects\": [\n")
			for j, project := range category.Projects {
				out.WriteString("        {\n")
				fields := [][2]string{
					{"slug", project.Slug},
					{"blurb", project.Blurb},
					{"url", project.URL},
					{"name", project.Name},
					{"repo", project.Repo},
				}
				for k, field := range fields {
					out.WriteString("          \"" + field[0] + "\": ")
					out.WriteString(util.PythonJSONString(field[1]))
					if k < len(fields)-1 {
						out.WriteString(",")
					}
					out.WriteString("\n")
				}
				out.WriteString("        }")
				if j < len(category.Projects)-1 {
					out.WriteString(",")
				}
				out.WriteString("\n")
			}
			out.WriteString("      ]\n")
		}
		out.WriteString("    }")
		if i < len(listing.Categories)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("  ]\n}\n")
	return out.String()
}

// ParseSidecar returns the listing a sidecar document holds.
//
// The sidecar is written by the deploy, never by hand, so the only thing
// checked here is that it is the format this build understands -- a wrong or
// absent format_version is a hard error, never a guess.
func ParseSidecar(text string, source string) (Listing, error) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return Listing{}, errorf("%s is not valid JSON: %s", source, err)
	}
	data, ok := document.(map[string]any)
	if !ok {
		return Listing{}, errorf("%s must contain a JSON object.", source)
	}
	if !isFormatVersion(data["format_version"]) {
		return Listing{}, errorf(
			"%s declares format_version %s; this selfdoc reads %d. Re-deploy "+
				"the home project to rewrite the sidecar.",
			source, util.PythonRepr(jsonValue(data["format_version"])), FormatVersion,
		)
	}
	rawCategories, _ := data["categories"].([]any)
	categories := make([]Category, 0, len(rawCategories))
	for _, rawCategory := range rawCategories {
		category, ok := rawCategory.(map[string]any)
		if !ok {
			continue
		}
		rawProjects, _ := category["projects"].([]any)
		projects := make([]Project, 0, len(rawProjects))
		for _, rawProject := range rawProjects {
			project, ok := rawProject.(map[string]any)
			if !ok {
				continue
			}
			projects = append(projects, Project{
				Slug:  util.PythonStrOrEmpty(jsonValue(project["slug"])),
				Blurb: util.PythonStrOrEmpty(jsonValue(project["blurb"])),
				URL:   util.PythonStrOrEmpty(jsonValue(project["url"])),
				Name:  util.PythonStrOrEmpty(jsonValue(project["name"])),
				Repo:  util.PythonStrOrEmpty(jsonValue(project["repo"])),
			})
		}
		categories = append(categories, Category{
			Name:     util.PythonStrOrEmpty(jsonValue(category["name"])),
			Projects: projects,
		})
	}
	return Listing{Categories: categories}, nil
}

// LoadSidecar returns the listing the sidecar document at path holds.
func LoadSidecar(path string) (Listing, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return Listing{}, err
	}
	return ParseSidecar(string(text), path)
}

// isFormatVersion reports whether value is the format version this build
// reads. A JSON number equal to one answers yes whether it was spelled 1 or
// 1.0, and so does the boolean true, because Python's equality -- which the
// refusal this replaces was written against -- holds True == 1.
func isFormatVersion(value any) bool {
	switch typed := value.(type) {
	case json.Number:
		if asInt, err := typed.Int64(); err == nil {
			return asInt == FormatVersion
		}
		asFloat, err := typed.Float64()
		return err == nil && asFloat == float64(FormatVersion)
	case bool:
		return typed && FormatVersion == 1
	default:
		return false
	}
}

// jsonValue narrows a decoded JSON value to the types pyStr and pyRepr read,
// turning a deferred number into the int64 or float64 Python's json module
// would have produced.
func jsonValue(value any) any {
	number, ok := value.(json.Number)
	if !ok {
		return value
	}
	if asInt, err := number.Int64(); err == nil {
		return asInt
	}
	if asFloat, err := number.Float64(); err == nil {
		return asFloat
	}
	return number.String()
}
