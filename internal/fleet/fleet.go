// Package fleet enumerates the selfdoc projects that live beside this one.
//
// Two tools need the same answer to "which sibling directories are selfdoc
// projects, and can their configs be loaded?": the corpus-wide spelling run
// and the one-off lint-impact measurement. The enumeration is here, in one
// package, so both callers share one hardened implementation.
//
// Hardened means every sibling is REPORTED, never fatal. A directory whose
// selfdoc.json is missing, unreadable or rejected by the config schema comes
// back as a [FleetProject] carrying the reason, and the caller decides what
// to say about it. A broken neighbour must not be able to stop a corpus run
// over the rest of the fleet.
//
// Everything here is read-only with respect to the projects it enumerates.
// The one write is a sanitized COPY of a config, placed in a caller-supplied
// scratch directory, never in the project.
package fleet

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// RetiredVersionKeys are the keys the config schema has retired but fleet
// configs still carry. A project whose only load failure is one of these is
// loaded from a sanitized copy, and the result says so, because a retired key
// is stale scaffolding rather than a broken project.
var RetiredVersionKeys = []string{"indexed"}

// FleetProject is one sibling directory carrying a selfdoc.json.
//
// Config is nil exactly when Error is set: the project was found but could
// not be loaded, and why is the Error string.
type FleetProject struct {
	Name      string
	Path      string
	Config    config.Config
	Sanitized bool
	Error     string
}

// Loaded reports whether the config was read successfully.
func (p FleetProject) Loaded() bool { return p.Config != nil }

// ProjectDirs returns every immediate subdirectory of root holding a
// selfdoc.json, sorted by name.
//
// Dot-directories are skipped, so archives and caches under ".archive/" are
// not walked. A root that does not exist yields nothing rather than failing:
// "no siblings" is a real answer.
func ProjectDirs(root string) []string {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		candidate := filepath.Join(root, name, "selfdoc.json")
		if stat, err := os.Stat(candidate); err == nil && stat.Mode().IsRegular() {
			dirs = append(dirs, filepath.Join(root, name))
		}
	}
	return dirs
}

// LoadProjectConfig loads projectDir's config, retrying once without the
// retired schema keys.
//
// scratchDir is where a sanitized copy may be written; it is never the
// project itself. The returned boolean is true when the config only loaded
// after retired keys were dropped from that copy.
//
// An invalid config whose failure retired keys do not explain comes back as
// the original diagnosis. Callers that enumerate the fleet should use
// [DiscoverFleet], which reports such a failure instead of returning it.
//
// The copy is written directly rather than through the effects handle, and
// the reason is the one the Python recorded when it marked the same two
// writes exempt: the file exists only to be read back by the loader on the
// next line, it lands in the caller's scratch directory rather than in any
// project, and nothing outside this function ever sees it. Recording it as
// an effect would make a preview answer "unloadable" for a project that
// loads, which is a different answer from the one a real run gives.
func LoadProjectConfig(projectDir, scratchDir string) (config.Config, bool, error) {
	loaded, err := config.Load(projectDir)
	if err == nil {
		return loaded, false, nil
	}
	var configErr *config.ConfigError
	if !errors.As(err, &configErr) {
		return nil, false, err
	}

	data, readErr := os.ReadFile(filepath.Join(projectDir, "selfdoc.json"))
	if readErr != nil {
		return nil, false, readErr
	}
	raw, decodeErr := config.DecodeDocument(data)
	if decodeErr != nil {
		return nil, false, decodeErr
	}
	document, _ := raw.(map[string]any)
	dropped := false
	if versions, ok := document["versions"].([]any); ok {
		for _, entry := range versions {
			version, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range RetiredVersionKeys {
				if _, present := version[key]; present {
					delete(version, key)
					dropped = true
				}
			}
		}
	}
	if !dropped {
		// Nothing retired to blame -- report the original diagnosis.
		return nil, false, err
	}

	shadow := filepath.Join(scratchDir, filepath.Base(strings.TrimRight(projectDir, "/")))
	// effects: exempt -- the caller's own scratch directory, never the project
	// being read.
	if err := os.MkdirAll(shadow, 0o755); err != nil {
		return nil, false, err
	}
	encoded, err := util.PythonJSON(document)
	if err != nil {
		return nil, false, err
	}
	// effects: exempt -- a scratch copy of a config, written and read back to
	// load it, and never read by anything else.
	if err := os.WriteFile(filepath.Join(shadow, "selfdoc.json"), encoded, 0o644); err != nil {
		return nil, false, err
	}
	sanitized, err := config.Load(shadow)
	if err != nil {
		return nil, false, err
	}
	return sanitized, true, nil
}

// DiscoverFleet enumerates and loads every selfdoc project directly under
// root.
//
// It never reports a broken project as a failure: a config that cannot be
// read comes back with a nil Config and an Error naming the reason. The
// scratch directory used for sanitized config copies is created and removed
// here, so nothing survives the call. The only returned error is a scratch
// directory that could not be created, which would make every sanitized
// retry impossible.
func DiscoverFleet(root string) ([]FleetProject, error) {
	// effects: exempt -- this call's own scratch directory, outside every
	// project being read, created and removed before returning.
	scratchDir, err := os.MkdirTemp("", "selfdoc-fleet-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratchDir)

	var found []FleetProject
	for _, path := range ProjectDirs(root) {
		name := filepath.Base(path)
		loaded, sanitized, err := LoadProjectConfig(path, scratchDir)
		if err != nil {
			found = append(found, FleetProject{
				Name: name, Path: path,
				Error: errorLabel(err) + ": " + err.Error(),
			})
			continue
		}
		if loaded == nil {
			found = append(found, FleetProject{
				Name: name, Path: path,
				Error: "selfdoc.json disappeared while loading",
			})
			continue
		}
		found = append(found, FleetProject{
			Name: name, Path: path, Config: loaded, Sanitized: sanitized,
		})
	}
	return found, nil
}

// errorLabel names the kind of failure, the way the Python reported
// type(exc).__name__ ahead of the message. Go has no exception class to
// read, so the mapping is explicit and covers the failures this package can
// actually produce.
func errorLabel(err error) string {
	var configErr *config.ConfigError
	if errors.As(err, &configErr) {
		return "ConfigError"
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return "JSONDecodeError"
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "JSONDecodeError"
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return "OSError"
	}
	return "Error"
}

// DocBody is one docs-tree page in the shape the lint rules consume: its
// parsed frontmatter, the resolved-directive text (deliberately empty here),
// the raw body, and how many source lines the frontmatter occupied.
type DocBody struct {
	Frontmatter      util.Frontmatter
	Resolved         string
	Body             string
	FrontmatterLines int
}

// LoadDocsBodies reads a docs tree into the shape the lint rules consume,
// keyed by each page's path relative to docsDir.
//
// Frontmatter is parsed; directives are NOT resolved, because resolution runs
// a project's extractors over its source and a corpus pass must stay
// read-only and cheap over projects it does not own. The resolved slot is
// therefore the empty string.
//
// The result is empty when docsDir is not a directory. Underscore-prefixed
// templates are not pages, and neither is anything under a _build directory.
func LoadDocsBodies(docsDir string) (map[string]DocBody, error) {
	bodies := map[string]DocBody{}
	info, err := os.Stat(docsDir)
	if err != nil || !info.IsDir() {
		return bodies, nil
	}
	walkErr := filepath.WalkDir(docsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if hasBuildComponent(filepath.Dir(path)) {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(docsDir, path)
		if err != nil {
			return err
		}
		text := string(content)
		block, readErr := util.ReadFrontmatter(text, relPath, util.KindPage)
		if readErr != nil {
			return readErr
		}
		// The frontmatter's height is the difference in line counts, which
		// is what the lint rules map a body line back to its source line
		// with. It is computed here rather than taken from the reader
		// because it must stay the number the Python reported.
		frontmatterLines := strings.Count(text, "\n") - strings.Count(block.Body, "\n")
		bodies[relPath] = DocBody{
			Frontmatter:      block.Values,
			Resolved:         "",
			Body:             block.Body,
			FrontmatterLines: frontmatterLines,
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return bodies, nil
}

// hasBuildComponent reports whether any path component is "_build", which is
// how build output under a docs tree is left out without pruning the walk.
func hasBuildComponent(dir string) bool {
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		if part == "_build" {
			return true
		}
	}
	return false
}
