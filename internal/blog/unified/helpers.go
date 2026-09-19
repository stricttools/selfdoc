package unified

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/stricttools/selfdoc/internal/config"
)

// unifiedProjects is the constituent project entries a unified config
// declares, in the declared order, dropping anything that is not an object.
func unifiedProjects(unifiedConfig map[string]any) []map[string]any {
	raw, _ := unifiedConfig["projects"].([]any)
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// configExcludePatterns is the exclude patterns a unified config declares,
// dropping anything that is not a string.
func configExcludePatterns(unifiedConfig map[string]any) []string {
	raw, _ := unifiedConfig["exclude"].([]any)
	patterns := make([]string, 0, len(raw))
	for _, item := range raw {
		if pattern, ok := item.(string); ok {
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

// configList reads a list-of-objects config key, dropping any element that is
// not an object.
func configList(cfg config.Config, key string) []map[string]any {
	raw, _ := cfg[key].([]any)
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// configString reads a string-valued config key, answering "" for a key that
// is absent or carries something else.
func configString(cfg config.Config, key string) string {
	value, _ := cfg[key].(string)
	return value
}

// isFile reports whether path names an existing regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// absPath is path made absolute against the process's working directory,
// answering the cleaned path when that cannot be resolved -- Python's
// os.path.abspath, which never fails either.
func absPath(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}

// sortedKeys returns a map's keys in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writtenSet is the set of paths a build wrote, remembering the order they
// were first written in.
//
// The order is read: the auxiliary files take the site's HTML paths from it,
// and the sitemap lists them in that order. A Go map has none, so the order is
// kept beside the set rather than recovered from it.
type writtenSet struct {
	paths map[string]bool
	order []string
}

// newWrittenSet returns an empty set.
func newWrittenSet() *writtenSet {
	return &writtenSet{paths: map[string]bool{}}
}

// add records a written path, keeping the position of its first write.
func (w *writtenSet) add(path string) {
	if w.paths[path] {
		return
	}
	w.paths[path] = true
	w.order = append(w.order, path)
}

// merge records every path of another write result, in that result's own
// sorted order so a preview names them the same way on every run.
func (w *writtenSet) merge(other map[string]bool) {
	for _, path := range sortedKeys(other) {
		w.add(path)
	}
}
