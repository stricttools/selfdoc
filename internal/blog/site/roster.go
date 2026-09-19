package site

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// RosterFields is every key a [[project]] block may carry, all of them
// required. An unknown key is a hard error rather than a silently ignored
// line: a typo in a membership declaration would otherwise retire a project.
var RosterFields = []string{"slug", "repo"}

// RosterTopLevelKeys is every top-level key the roster document may carry.
// "home" names the one project whose content root is the site root; see
// [Roster].
var RosterTopLevelKeys = []string{"home", "project"}

// RosterHeader is the comment block [RenderRoster] writes above the
// declaration.
const RosterHeader = `# The assembly's membership: every project the unified site serves.
#
# This file is the declaration; the deploy reconciles the site to it and can
# never add to it. A project with no [[project]] block here has its subtree,
# its manifests, its membership record and its search-index entries removed at
# the next deploy -- which is what ` + "`selfdoc assembly retire <slug>`" + ` does in
# one operation.
#
# ` + "`home`" + ` names the one declared project that IS the front page: its pages are
# emitted at the site root instead of under site/<slug>/, it is left out of the
# generated project listing, and it is required -- a site needs a front page,
# so there is no default and no more than one.
#
# projects.json next to this file is derived state, rewritten by every deploy.
# Edit this file, never that one.
`

// RosterEntry is one declared member of the assembly.
//
// Repo is part of the declaration rather than derived from a dispatch so that
// a slug has one owning repository on record: a dispatch arriving for a
// declared slug from a different repository is a hard error instead of a
// silent takeover of that slug's section.
type RosterEntry struct {
	Slug string
	Repo string
}

// Roster is the declared membership, plus the one project that is the site
// root.
//
// A roster is read as a mapping of slug -> [RosterEntry] everywhere membership
// is the question, which is most places. Home is the extra fact only the front
// page cares about: one declared slug whose content root emits at the site root
// rather than under site/<slug>/.
//
// The home project is an ordinary project in every other respect -- a real
// repository that dispatches its own deploys. Being home is a flag on it,
// never a separate kind of thing and never the assembly repository itself.
type Roster struct {
	projects map[string]RosterEntry

	// Home is the slug of the declared project served at the site root.
	Home string
}

// NewRoster builds a [Roster] over a copy of projects.
func NewRoster(projects map[string]RosterEntry, home string) *Roster {
	copied := make(map[string]RosterEntry, len(projects))
	for slug, entry := range projects {
		copied[slug] = entry
	}
	return &Roster{projects: copied, Home: home}
}

// Get returns the entry declared for slug, reporting whether the roster
// declares it at all.
func (r *Roster) Get(slug string) (RosterEntry, bool) {
	entry, ok := r.projects[slug]
	return entry, ok
}

// Has reports whether the roster declares slug.
func (r *Roster) Has(slug string) bool {
	_, ok := r.projects[slug]
	return ok
}

// Len is the number of declared projects.
func (r *Roster) Len() int { return len(r.projects) }

// Slugs returns every declared slug, sorted.
func (r *Roster) Slugs() []string {
	slugs := make([]string, 0, len(r.projects))
	for slug := range r.projects {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// Entries returns a copy of the declared mapping, for the call sites that take
// membership alone -- [RecordMembership] among them.
func (r *Roster) Entries() map[string]RosterEntry {
	copied := make(map[string]RosterEntry, len(r.projects))
	for slug, entry := range r.projects {
		copied[slug] = entry
	}
	return copied
}

// String renders the roster the way the Python surface's repr did.
func (r *Roster) String() string {
	slugs := make([]any, 0, len(r.projects))
	for _, slug := range r.Slugs() {
		slugs = append(slugs, slug)
	}
	return fmt.Sprintf(
		"Roster(projects=%s, home=%s)",
		util.PythonRepr(slugs), util.PythonRepr(r.Home),
	)
}

// RenderRoster returns the TOML text for entries.
//
// home is written as the top-level "home" key. An empty home leaves a
// commented placeholder instead: a roster with no home is refused when it is
// read, and a scaffolded file that silently named some project home would be
// choosing the front page on the author's behalf.
func RenderRoster(entries []RosterEntry, home string) string {
	head := `# home = "<slug>"   # required: the project served at the site root` + "\n"
	if home != "" {
		head = fmt.Sprintf("home = %s\n", util.PythonJSONString(home))
	}
	sorted := make([]RosterEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })
	blocks := make([]string, 0, len(sorted))
	for _, entry := range sorted {
		blocks = append(blocks, fmt.Sprintf(
			"[[project]]\nslug = %s\nrepo = %s\n",
			util.PythonJSONString(entry.Slug), util.PythonJSONString(entry.Repo),
		))
	}
	return RosterHeader + "\n" + head + "\n" + strings.Join(blocks, "\n")
}

// ParseRoster returns the [Roster] the roster document text declares, naming
// source in every diagnostic.
//
// Validation is strict in every direction: an unknown top-level table, an
// unknown key on a block, a missing or empty required key, a duplicate slug,
// and a slug that collides with one of the assembly's own directories are each
// a hard error naming the offending declaration. A roster with no [[project]]
// block at all is legal and means an empty assembly.
//
// The "home" key is required and names a declared slug. Both failures are hard
// errors: a missing key because a site needs a front page and no project may
// be picked for the author by default, and a key naming an undeclared slug
// because the front page has to be something the assembly actually serves.
func ParseRoster(text string, source string) (*Roster, error) {
	if source == "" {
		source = RosterPath
	}
	data, err := util.DecodeTOML([]byte(text))
	if err != nil {
		return nil, errorf("%s is not valid TOML: %s", source, err)
	}

	if unknown := unknownKeys(data, RosterTopLevelKeys); len(unknown) > 0 {
		return nil, errorf(
			"%s declares unknown top-level key(s) %s. The roster holds "+
				"nothing but [[project]] blocks and the 'home' key.",
			source, joinReprs(unknown),
		)
	}

	rawValue, present := data["project"]
	raw, ok := asList(rawValue)
	if present && !ok {
		return nil, errorf(
			"%s: 'project' must be a list of [[project]] blocks.", source,
		)
	}

	entries := map[string]RosterEntry{}
	order := make([]string, 0, len(raw))
	for index, itemAny := range raw {
		where := fmt.Sprintf("%s: [[project]] #%d", source, index+1)
		item, ok := asTable(itemAny)
		if !ok {
			return nil, errorf("%s is not a table.", where)
		}
		if unknown := unknownKeys(item, RosterFields); len(unknown) > 0 {
			return nil, errorf(
				"%s declares unknown key(s) %s. A [[project]] block carries "+
					"exactly %s.",
				where, joinReprs(unknown), strings.Join(RosterFields, ", "),
			)
		}
		missing := make([]string, 0, len(RosterFields))
		for _, field := range RosterFields {
			if !pythonTruthy(item[field]) {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			return nil, errorf(
				"%s is missing %s. Every declared project names %s.",
				where, strings.Join(missing, ", "), strings.Join(RosterFields, ", "),
			)
		}
		slug := util.PythonStr(item["slug"])
		if containsString(SiteReservedDirs, slug) {
			return nil, errorf(
				"%s claims the slug %s, which is one of the assembly's own "+
					"directories (%s). Give the project a different slug.",
				where, util.PythonRepr(slug), strings.Join(SiteReservedDirs, ", "),
			)
		}
		if _, seen := entries[slug]; seen {
			return nil, errorf(
				"%s repeats the slug %s, which an earlier block already declares.",
				where, util.PythonRepr(slug),
			)
		}
		entries[slug] = RosterEntry{Slug: slug, Repo: util.PythonStr(item["repo"])}
		order = append(order, slug)
	}

	// Three ways the home declaration goes wrong, three messages: the key is
	// absent, the key is present and says nothing, or it names a slug no block
	// declares. They are different mistakes with different fixes, and one
	// shared message sent an author to add a key already in the file.
	declaredSlugs := make([]string, len(order))
	copy(declaredSlugs, order)
	sort.Strings(declaredSlugs)
	declared := strings.Join(declaredSlugs, ", ")
	if declared == "" {
		declared = "(none)"
	}
	homeValue, present := data["home"]
	if !present || homeValue == nil {
		return nil, errorf(
			"%s carries no top-level 'home' key. Exactly one declared project "+
				"is the site's front page: its pages are emitted at the site "+
				"root instead of under site/<slug>/. There is no default -- add "+
				`home = "<slug>" naming one of the declared projects. `+
				"Declared projects: %s.",
			source, declared,
		)
	}
	home, ok := homeValue.(string)
	if !ok {
		return nil, errorf(
			"%s: home must be a slug string, got %s.",
			source, util.PythonRepr(homeValue),
		)
	}
	if util.PythonStrip(home) == "" {
		return nil, errorf(
			"%s declares an empty home (%s). The key is there, so this is not "+
				"a roster that forgot one -- it is a roster that names no "+
				"project as the site's front page, and a site needs one. Give "+
				"home the slug of a declared project. Declared projects: %s.",
			source, util.PythonRepr(home), declared,
		)
	}
	home = util.PythonStrip(home)
	if _, ok := entries[home]; !ok {
		return nil, errorf(
			"%s names %s as the home project, but no [[project]] block "+
				"declares it. The home project is an ordinary declared project "+
				"that happens to be served at the site root, never a slug the "+
				"roster does not carry. Declared projects: %s.",
			source, util.PythonRepr(home), declared,
		)
	}
	return NewRoster(entries, home), nil
}

// missingRosterError is the refusal a roster file's absence earns, carrying a
// scaffold the author can paste.
func missingRosterError(path string) *Error {
	example := RenderRoster(
		[]RosterEntry{{Slug: "example", Repo: "owner/example"}}, "example",
	)
	return errorf(
		"%s does not exist, so the assembly declares no membership. "+
			"Membership is a declared list the deploy reconciles to, never "+
			"something a deploy accumulates, so there is no empty default. "+
			"Create the file in the assembly repository with one block per "+
			"project the site serves:\n\n%s",
		path, example,
	)
}

// MissingRosterError returns the refusal a missing roster at path earns.
//
// It is exported because the remote roster read raises the same refusal for a
// roster that is not on the assembly repository, and one wording for the two
// is the point.
func MissingRosterError(path string) error { return missingRosterError(path) }

// LoadRoster returns the roster declared in assemblyDir, or an error when it
// is absent.
func LoadRoster(assemblyDir string) (*Roster, error) {
	path := filepath.Join(assemblyDir, RosterPath)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, missingRosterError(path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRoster(string(content), path)
}
