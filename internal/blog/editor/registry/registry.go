// Package registry reads the authoring app's repository registry: a
// hand-written TOML file.
//
// This is not framework configuration. It is one machine-local list of the
// repositories the editor may open, written by hand by the one person the
// editor serves, and read strictly: every key is known, every required key is
// present, every name is unique and addressable in a URL, and every local path
// really is a directory. Anything else refuses and names the offender.
//
// The strictness is the point. A registry entry that quietly fails to parse is
// a project whose posts silently stop being editable, with no error anywhere
// to explain why -- so there is no shape here that is merely skipped.
//
// # Format
//
// Two kinds of entry, both under [[repo]], both declaring their kind:
//
//	[[repo]]
//	name = "selfdoc"
//	kind = "local"
//	path = "~/Projects/selfdoc"
//
//	[[repo]]
//	name = "afar"
//	kind = "remote"
//	repo = "smm-h/afar"
//	ref = "v1.2.3"
//	cache = "~/.cache/selfdoc/afar"
//	render = true
//
// A remote entry is validated in full but not served yet: the server refuses
// it with "remote entries not yet served" rather than pretending. render is
// required and has no default because directive resolution reads a source
// tree, and whether a remote entry gets one is a decision the file has to
// state.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// DefaultPath returns where the registry lives. Machine-local by design --
// the editor writes working trees on this machine and is not a published,
// shared surface, so its list of repositories belongs beside the other
// machine-local records rather than in any project's committed config.
//
// It is a function rather than a constant because it reads the home directory
// out of the environment, which the Python module it replaces did once at
// import time.
func DefaultPath() string {
	return filepath.Join(expandUser("~"), "Projects", "ark", "selfdoc-registry.toml")
}

// nameRE is the shape of a registry name. A name is a URL path segment
// (/api/repos/<name>/posts) and a nav key. Restricting it here means path
// traversal cannot enter through a registry entry, and a name never has to be
// escaped anywhere downstream.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// localKeys is every key a local entry may carry.
var localKeys = []string{"name", "kind", "path"}

// remoteKeys is every key a remote entry may carry.
var remoteKeys = []string{"name", "kind", "repo", "ref", "cache", "render"}

// Error reports that the registry file is unusable, and the message says
// exactly how.
//
// It is the Go counterpart of the Python surface's RegistryError, and the one
// error type a caller needs to recognize with errors.As to render a registry
// refusal as one line instead of an unexpected internal failure.
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

// Entry is one registry entry: a [LocalRepo] or a [RemoteRepo]. The two are
// separate types rather than one struct with unused members because the server
// serves a local entry and refuses a remote one, and the distinction is the
// thing it branches on.
type Entry interface {
	// Name is the entry's registry name: a URL path segment and a nav key.
	Name() string
	// Kind is "local" or "remote".
	Kind() string
	// entry seals the interface to this package's two implementations.
	entry()
}

// LocalRepo is a working tree on this machine. Edits land in it directly.
type LocalRepo struct {
	name string
	path string
}

// Name is the entry's registry name.
func (r *LocalRepo) Name() string { return r.name }

// Kind is "local".
func (r *LocalRepo) Kind() string { return "local" }

// Path is the absolute path of the working tree.
func (r *LocalRepo) Path() string { return r.path }

func (r *LocalRepo) entry() {}

// RemoteRepo is a repository elsewhere. Validated here, not served yet.
type RemoteRepo struct {
	name   string
	repo   string
	ref    string
	cache  string
	render bool
}

// Name is the entry's registry name.
func (r *RemoteRepo) Name() string { return r.name }

// Kind is "remote".
func (r *RemoteRepo) Kind() string { return "remote" }

// Repo is the repository the entry names, as owner/name.
func (r *RemoteRepo) Repo() string { return r.repo }

// Ref is the git ref the entry pins.
func (r *RemoteRepo) Ref() string { return r.ref }

// Cache is the absolute path of the checkout cache.
func (r *RemoteRepo) Cache() string { return r.cache }

// Render states whether rendering runs against a checkout: directive
// resolution needs a source tree, so an entry that answers no can only ever
// serve prose. It is required in the file because neither answer is safe to
// assume.
func (r *RemoteRepo) Render() bool { return r.render }

func (r *RemoteRepo) entry() {}

// Registry is the parsed registry: ordered entries, addressable by name.
type Registry struct {
	// Entries are the registry's entries, in the order the file declares them.
	Entries []Entry
	// Path is the file the entries were read from.
	Path   string
	byName map[string]Entry
}

// Names returns the entry names, in the order the file declares them.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.Entries))
	for _, entry := range r.Entries {
		names = append(names, entry.Name())
	}
	return names
}

// Get returns the entry called name, or a refusal naming what is on offer.
func (r *Registry) Get(name string) (Entry, error) {
	if entry, ok := r.byName[name]; ok {
		return entry, nil
	}
	known := strings.Join(r.Names(), ", ")
	if known == "" {
		known = "(none)"
	}
	return nil, errorf(
		"No repository named %s in %s. Known repositories: %s.",
		util.PythonRepr(name), r.Path, known,
	)
}

// Len returns how many entries the registry declares.
func (r *Registry) Len() int { return len(r.Entries) }

// RenderList returns the lines `editor list-repos` prints: one per entry --
// a local entry's working tree, or a remote entry's repository, ref and
// whether it declares that rendering runs against a checkout -- then a blank
// line and the count. An empty registry renders as the one line that says so.
func (r *Registry) RenderList() []string {
	if len(r.Entries) == 0 {
		return []string{fmt.Sprintf("No repositories in %s.", r.Path)}
	}
	lines := make([]string, 0, len(r.Entries)+2)
	for _, entry := range r.Entries {
		switch typed := entry.(type) {
		case *LocalRepo:
			lines = append(lines, fmt.Sprintf("%s  local   %s", typed.Name(), typed.Path()))
		case *RemoteRepo:
			rendered := "no-render"
			if typed.Render() {
				rendered = "render"
			}
			lines = append(lines, fmt.Sprintf(
				"%s  remote  %s@%s [%s, not served yet]",
				typed.Name(), typed.Repo(), typed.Ref(), rendered,
			))
		}
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf(
		"%d repository(ies) in %s.", len(r.Entries), r.Path,
	))
	return lines
}

// Load reads and validates the registry at path. An empty path reads
// [DefaultPath].
//
// It returns an [Error] when the file is missing, unparsable, or declares any
// shape this package does not accept. The message names the offender.
func Load(path string) (*Registry, error) {
	if path == "" {
		path = DefaultPath()
	}

	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil, errorf(
			"No editor registry at %s. Create it with one [[repo]] block per "+
				"repository: name, kind, and (for kind = \"local\") path.",
			path,
		)
	}

	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	document, err := util.DecodeTOML(text)
	if err != nil {
		return nil, errorf("%s is not valid TOML: %s", path, err)
	}

	if unknown := unknownKeys(document, []string{"repo"}); len(unknown) > 0 {
		return nil, errorf(
			"%s: unknown top-level key(s) %s. The registry holds [[repo]] "+
				"entries and nothing else.",
			path, strings.Join(unknown, ", "),
		)
	}

	rawEntries, ok := asList(document["repo"])
	if !ok {
		if _, declared := document["repo"]; declared {
			return nil, errorf(
				"%s: 'repo' must be an array of tables ([[repo]]), got %s.",
				path, util.PythonTypeName(document["repo"]),
			)
		}
	}

	entries := make([]Entry, 0, len(rawEntries))
	seen := map[string]int{}
	byName := map[string]Entry{}
	for index, raw := range rawEntries {
		entry, err := parseEntry(raw, index, path)
		if err != nil {
			return nil, err
		}
		if earlier, ok := seen[entry.Name()]; ok {
			return nil, errorf(
				"%s: duplicate repository name %s, declared by entry #%d and "+
					"entry #%d.",
				path, util.PythonRepr(entry.Name()), earlier+1, index+1,
			)
		}
		seen[entry.Name()] = index
		byName[entry.Name()] = entry
		entries = append(entries, entry)
	}

	return &Registry{Entries: entries, Path: path, byName: byName}, nil
}

// parseEntry validates one [[repo]] table into a typed entry.
func parseEntry(raw any, index int, path string) (Entry, error) {
	where := fmt.Sprintf("%s: entry #%d", path, index+1)
	table, ok := raw.(map[string]any)
	if !ok {
		return nil, errorf(
			"%s: each 'repo' element must be a table ([[repo]]), got %s.",
			where, util.PythonTypeName(raw),
		)
	}

	rawName, declared := table["name"]
	if !declared {
		return nil, errorf("%s: 'name' is required.", where)
	}
	name, isString := rawName.(string)
	if !isString || !nameRE.MatchString(name) {
		return nil, errorf(
			"%s: 'name' must be a URL-addressable identifier (letters, "+
				"digits, dot, dash, underscore; no slashes, no spaces), got %s.",
			where, util.PythonRepr(rawName),
		)
	}

	where = fmt.Sprintf("%s: repository %s", path, util.PythonRepr(name))

	rawKind, declared := table["kind"]
	if !declared {
		return nil, errorf(
			"%s: 'kind' is required and has no default. Declare "+
				"kind = \"local\" for a working tree on this machine, or "+
				"kind = \"remote\" for a repository elsewhere.",
			where,
		)
	}
	switch rawKind {
	case "local":
		return parseLocal(table, name, where)
	case "remote":
		return parseRemote(table, name, where)
	}
	return nil, errorf(
		"%s: unknown kind %s. Valid kinds are \"local\" and \"remote\".",
		where, util.PythonRepr(rawKind),
	)
}

// rejectUnknownKeys refuses a table carrying anything the kind does not take.
func rejectUnknownKeys(table map[string]any, allowed []string, where, kind string) error {
	unknown := unknownKeys(table, allowed)
	if len(unknown) == 0 {
		return nil
	}
	sorted := append([]string(nil), allowed...)
	sort.Strings(sorted)
	return errorf(
		"%s: unknown key(s) %s on a %s entry. A %s entry takes: %s.",
		where, strings.Join(unknown, ", "), kind, kind, strings.Join(sorted, ", "),
	)
}

// requireString reads a required non-empty string key.
func requireString(table map[string]any, key, where string) (string, error) {
	value, declared := table[key]
	if !declared {
		return "", errorf("%s: %s is required.", where, util.PythonRepr(key))
	}
	text, isString := value.(string)
	if !isString || strings.TrimSpace(text) == "" {
		return "", errorf(
			"%s: %s must be a non-empty string, got %s.",
			where, util.PythonRepr(key), util.PythonRepr(value),
		)
	}
	return text, nil
}

// parseLocal validates a local entry.
func parseLocal(table map[string]any, name, where string) (Entry, error) {
	if err := rejectUnknownKeys(table, localKeys, where, "local"); err != nil {
		return nil, err
	}
	declared, err := requireString(table, "path", where)
	if err != nil {
		return nil, err
	}
	path, absErr := filepath.Abs(expandUser(declared))
	if absErr != nil {
		path = filepath.Clean(expandUser(declared))
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() {
		return nil, errorf(
			"%s: path %s is not a directory. A local entry names a working "+
				"tree on this machine.",
			where, path,
		)
	}
	return &LocalRepo{name: name, path: path}, nil
}

// parseRemote validates a remote entry.
func parseRemote(table map[string]any, name, where string) (Entry, error) {
	if err := rejectUnknownKeys(table, remoteKeys, where, "remote"); err != nil {
		return nil, err
	}
	repo, err := requireString(table, "repo", where)
	if err != nil {
		return nil, err
	}
	ref, err := requireString(table, "ref", where)
	if err != nil {
		return nil, err
	}
	declaredCache, err := requireString(table, "cache", where)
	if err != nil {
		return nil, err
	}
	cache, absErr := filepath.Abs(expandUser(declaredCache))
	if absErr != nil {
		cache = filepath.Clean(expandUser(declaredCache))
	}

	rawRender, declared := table["render"]
	if !declared {
		return nil, errorf(
			"%s: 'render' is required and has no default. Declare "+
				"render = true if rendering runs against a checkout of this "+
				"repository (directive resolution needs a source tree), or "+
				"render = false if it does not.",
			where,
		)
	}
	render, isBool := rawRender.(bool)
	if !isBool {
		return nil, errorf(
			"%s: 'render' must be true or false, got %s.",
			where, util.PythonRepr(rawRender),
		)
	}

	return &RemoteRepo{
		name:   name,
		repo:   repo,
		ref:    ref,
		cache:  cache,
		render: render,
	}, nil
}
