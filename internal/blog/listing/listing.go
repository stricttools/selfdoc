// Package listing carries the home project's curated project listing: one
// declared source, two renderings.
//
// The assembled site shows the projects it serves in two places -- the front
// page's cards and the generated /projects/ page -- and both read this one
// document, [SourceFile] in the home project. The listing is content, and
// content is the home project's territory, so it is authored there rather than
// in the assembly repository.
//
// Curation means selection: a roster project the listing leaves out simply
// does not appear, which is legal and deliberate. The reverse is not: a listed
// slug with no manifest at assembly time is a hard error naming it, because
// the listing would otherwise print a card for a project the site cannot
// serve.
package listing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// SourceFile is the file, relative to the home project's root, that declares
// the listing. It sits beside the pages, in the handwritten docs directory: the
// listing is content a person writes.
var SourceFile = layout.DocsRel + "/projects.toml"

// SidecarSuffix is where the assembly keeps the copy the deploy grafted, as a
// manifest sidecar belonging to the home slug.
const SidecarSuffix = "-listing.json"

// FormatVersion is the sidecar format this package writes and reads.
const FormatVersion = 1

// CategoryKeys is every key a [[category]] block may carry.
var CategoryKeys = []string{"name", "project"}

// ProjectKeys is every key a [[category.project]] block may carry.
//
// slug and blurb are required; url marks an entry the assembly does not serve
// (a project with no docs section), and name is required for exactly those --
// an entry the assembly does serve takes its name from its manifest, so
// declaring one here would be a second source for the same fact. repo is the
// project's repository, rendered as a second link on the card beside the one
// the title carries.
var ProjectKeys = []string{"slug", "blurb", "url", "name", "repo"}

// Error is the failure every listing operation reports: an unparsable or
// invalid declaration, a sidecar in another format, and a listing that names a
// project the assembly cannot serve.
//
// It is the Go counterpart of the RuntimeError the Python surface raised, and
// the one error type a caller needs to recognize with errors.As to render a
// listing refusal distinctly from an unexpected internal failure.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

// errorf builds an [Error] from a format string.
func errorf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// Project is one curated entry.
//
// URL empty means the entry names a project the assembly serves: its display
// name and version come from its manifest and its address is its section on
// this site. URL set means an external project with no docs section here,
// which therefore carries its own Name. Repo is the project's repository,
// which a card links to beside its documentation.
type Project struct {
	Slug  string
	Blurb string
	URL   string
	Name  string
	Repo  string
}

// External reports whether the entry names a project this site does not serve.
func (p Project) External() bool { return p.URL != "" }

// Category is one named group of curated entries, in declared order.
type Category struct {
	Name     string
	Projects []Project
}

// Listing is the whole curated listing, in declared order.
type Listing struct {
	Categories []Category
}

// Slugs is every listed slug, in declared order.
func (l Listing) Slugs() []string {
	slugs := make([]string, 0)
	for _, category := range l.Categories {
		for _, project := range category.Projects {
			slugs = append(slugs, project.Slug)
		}
	}
	return slugs
}

// Entry is one listed project together with the category that holds it -- the
// pair [Listing.Entries] walks.
type Entry struct {
	Category Category
	Project  Project
}

// Entries walks every listed project in declared order, each paired with its
// category.
func (l Listing) Entries() []Entry {
	entries := make([]Entry, 0)
	for _, category := range l.Categories {
		for _, project := range category.Projects {
			entries = append(entries, Entry{Category: category, Project: project})
		}
	}
	return entries
}

// Parse returns the [Listing] the document text declares, naming source in
// every diagnostic.
//
// Validation is strict in every direction: an unknown top-level key, an
// unknown key on any block, a missing or empty required key, a category with
// no entries, a duplicate category name and a duplicate slug are each a hard
// error naming the offending declaration.
func Parse(text string, source string) (Listing, error) {
	data, err := util.DecodeTOML([]byte(text))
	if err != nil {
		return Listing{}, errorf("%s is not valid TOML: %s", source, err)
	}

	if unknown := unknownKeys(data, []string{"category"}); len(unknown) > 0 {
		return Listing{}, errorf(
			"%s declares unknown top-level key(s) %s. The listing holds "+
				"nothing but [[category]] blocks.",
			source, joinReprs(unknown),
		)
	}

	rawCategories, ok := asList(data["category"])
	if !ok || len(rawCategories) == 0 {
		return Listing{}, errorf(
			"%s declares no [[category]] block. The listing is the site's "+
				"curated project index and there is no empty default.",
			source,
		)
	}

	categories := make([]Category, 0, len(rawCategories))
	seenCategories := map[string]bool{}
	seenSlugs := map[string]string{}

	for index, rawAny := range rawCategories {
		where := fmt.Sprintf("%s: [[category]] #%d", source, index+1)
		raw, ok := asTable(rawAny)
		if !ok {
			return Listing{}, errorf("%s is not a table.", where)
		}
		if unknown := unknownKeys(raw, CategoryKeys); len(unknown) > 0 {
			return Listing{}, errorf(
				"%s declares unknown key(s) %s. A [[category]] block carries %s.",
				where, joinReprs(unknown), strings.Join(CategoryKeys, ", "),
			)
		}
		name, ok := raw["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return Listing{}, errorf("%s is missing a non-empty 'name'.", where)
		}
		name = strings.TrimSpace(name)
		if seenCategories[name] {
			return Listing{}, errorf(
				"%s repeats the category name %s, which an earlier block "+
					"already declares.",
				where, util.PythonRepr(name),
			)
		}
		seenCategories[name] = true

		rawProjects, ok := asList(raw["project"])
		if !ok || len(rawProjects) == 0 {
			return Listing{}, errorf(
				"%s (%s) declares no [[category.project]] block. An empty "+
					"category would render as a heading over nothing.",
				where, util.PythonRepr(name),
			)
		}

		projects := make([]Project, 0, len(rawProjects))
		for position, itemAny := range rawProjects {
			spot := fmt.Sprintf(
				"%s (%s): [[category.project]] #%d", where, util.PythonRepr(name), position+1,
			)
			item, ok := asTable(itemAny)
			if !ok {
				return Listing{}, errorf("%s is not a table.", spot)
			}
			if unknown := unknownKeys(item, ProjectKeys); len(unknown) > 0 {
				return Listing{}, errorf(
					"%s declares unknown key(s) %s. A listed project carries %s.",
					spot, joinReprs(unknown), strings.Join(ProjectKeys, ", "),
				)
			}
			slug, ok := item["slug"].(string)
			if !ok || strings.TrimSpace(slug) == "" {
				return Listing{}, errorf("%s is missing a non-empty 'slug'.", spot)
			}
			slug = strings.TrimSpace(slug)
			blurb, ok := item["blurb"].(string)
			if !ok || strings.TrimSpace(blurb) == "" {
				return Listing{}, errorf(
					"%s (%s) is missing a non-empty 'blurb'. The listing is "+
						"curated prose, not a directory dump.",
					spot, slug,
				)
			}
			url := strings.TrimSpace(util.PythonStrOrEmpty(item["url"]))
			entryName := strings.TrimSpace(util.PythonStrOrEmpty(item["name"]))
			if url != "" && entryName == "" {
				return Listing{}, errorf(
					"%s (%s) declares a url, so it is a project this site does "+
						"not serve and has no manifest to take a display name "+
						"from. Declare 'name'.",
					spot, slug,
				)
			}
			if entryName != "" && url == "" {
				return Listing{}, errorf(
					"%s (%s) declares a name but no url. A project this site "+
						"serves takes its name from its manifest, so declaring "+
						"one here would be a second source for it.",
					spot, slug,
				)
			}
			repo := strings.TrimSpace(util.PythonStrOrEmpty(item["repo"]))
			if repo != "" && repo == url {
				return Listing{}, errorf(
					"%s (%s) declares the same address as 'url' and 'repo', so "+
						"the card would print two links to one place. An entry "+
						"whose only address is its repository needs 'url' alone.",
					spot, slug,
				)
			}
			if earlier, ok := seenSlugs[slug]; ok {
				return Listing{}, errorf(
					"%s repeats the slug %s, already listed under %s. One "+
						"project, one card.",
					spot, util.PythonRepr(slug), util.PythonRepr(earlier),
				)
			}
			seenSlugs[slug] = name
			projects = append(projects, Project{
				Slug:  slug,
				Blurb: strings.TrimSpace(blurb),
				URL:   url,
				Name:  entryName,
				Repo:  repo,
			})
		}

		categories = append(categories, Category{Name: name, Projects: projects})
	}

	return Listing{Categories: categories}, nil
}

// unknownKeys returns the keys of table that allowed does not name, sorted.
func unknownKeys(table map[string]any, allowed []string) []string {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	unknown := make([]string, 0)
	for key := range table {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// joinReprs renders keys the way Python's ", ".join(repr(k) for k in keys)
// does, because the refusals quote every key they name.
func joinReprs(keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, util.PythonRepr(key))
	}
	return strings.Join(parts, ", ")
}

// asList normalizes a decoded TOML array into a slice of elements. The decoder
// answers an array of tables as []map[string]any and every other array as
// []any, and both spell the same document shape Python's tomllib returns as one
// list of dicts.
func asList(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items, true
	default:
		return nil, false
	}
}

// asTable narrows a decoded value to a TOML table.
func asTable(value any) (map[string]any, bool) {
	table, ok := value.(map[string]any)
	return table, ok
}
