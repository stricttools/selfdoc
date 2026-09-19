package unified

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// ResolveProjectPath resolves a constituent project's absolute path from its
// entry in the unified config.
//
// The entry's "path" is stated relative to the docs-site directory, e.g.
// "../core". A path that does not name a directory is a ConfigError: the whole
// unified build is described by these entries, so a typo here would otherwise
// surface as a missing page somewhere far downstream.
func ResolveProjectPath(projectEntry map[string]any, docsSiteDir string) (string, error) {
	rawPath := util.PythonStrOrEmpty(projectEntry["path"])
	absPath := normJoin(docsSiteDir, rawPath)
	if !isDir(absPath) {
		return "", &config.ConfigError{Message: fmt.Sprintf(
			"unified project path '%s' resolves to '%s' which does not exist",
			rawPath, absPath)}
	}
	return absPath, nil
}

// ProjectSlug is the URL segment a constituent project is mounted under: the
// entry's explicit "slug" when it states one, else the last component of its
// path.
func ProjectSlug(projectEntry map[string]any) string {
	if slug := util.PythonStrOrEmpty(projectEntry["slug"]); slug != "" {
		return slug
	}
	return pythonBasename(strings.TrimRight(util.PythonStrOrEmpty(projectEntry["path"]), "/"))
}

// ProjectNavTitle is the navigation title a constituent project carries: the
// entry's explicit "nav_title" when it states one, else its slug with the
// separators turned into spaces and every word title-cased.
func ProjectNavTitle(projectEntry map[string]any) string {
	if title := util.PythonStrOrEmpty(projectEntry["nav_title"]); title != "" {
		return title
	}
	slug := ProjectSlug(projectEntry)
	return util.TitleCase(strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " "))
}

// isDir reports whether path names an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// normJoin joins a base directory with a path the way Python's
// os.path.normpath(os.path.join(...)) does: an absolute second element
// replaces the first, and the result is cleaned.
func normJoin(base, rel string) string {
	return filepath.Clean(util.PathJoin(base, rel))
}

// pythonBasename is the last component of a slash-separated path, reproducing
// os.path.basename -- which answers "" for a path ending in a separator and
// for the empty path, where path.Base would answer "/" and ".".
func pythonBasename(p string) string {
	if index := strings.LastIndex(p, "/"); index >= 0 {
		return p[index+1:]
	}
	return p
}
