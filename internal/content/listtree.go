package content

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// treeExcludes are the names and suffixes a tree listing never shows. They are
// this directive's own list rather than the source-walk exclusions: a tree is a
// picture of a directory, so it shows test files and fixtures, and hides only
// what is machine state.
var treeExcludes = []string{
	"__pycache__", ".pyc", ".git", "node_modules", ".egg-info",
	layout.DocsCacheName, ".tox", ".mypy_cache", ".pytest_cache",
}

// shouldExcludeFromTree reports whether a file or directory name is excluded
// from tree output.
func shouldExcludeFromTree(name string) bool {
	for _, excluded := range treeExcludes {
		if name == excluded || strings.HasSuffix(name, excluded) {
			return true
		}
	}
	return false
}

// ResolveListTree walks a directory and produces a text tree inside a fenced
// code block.
func ResolveListTree(attrs map[string]string, baseDir string) string {
	path := attrs["path"]
	if path == "" {
		return marker("list-tree requires a path attribute")
	}

	maxDepth := -1
	if depth := attrs["depth"]; isDigits(depth) {
		parsed, err := strconv.Atoi(depth)
		if err != nil {
			// A run of digits too long for an int is not a depth anyone
			// declared; Python's arbitrary-precision int would accept it
			// and then never reach it, which is what no limit means.
			maxDepth = -1
		} else {
			maxDepth = parsed
		}
	}

	fullPath := util.ResolveDirectivePath(baseDir, path)
	info, err := os.Stat(fullPath)
	if err != nil || !info.IsDir() {
		return marker("directory '%s' not found", path)
	}

	lines := buildTree(fullPath, "", maxDepth, 0)
	rootName := filepath.Base(strings.TrimRight(fullPath, "/")) + "/"
	treeText := rootName + "\n" + strings.Join(lines, "\n")
	return "```\n" + treeText + "\n```"
}

// isDigits reports whether s is a non-empty run of ASCII digits, the test
// Python's str.isdigit() performs for this attribute.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// buildTree builds the tree lines for one directory, recursing into its
// subdirectories. A maxDepth below zero means no limit.
func buildTree(dirPath, prefix string, maxDepth, currentDepth int) []string {
	if maxDepth >= 0 && currentDepth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !shouldExcludeFromTree(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	var lines []string
	for index, name := range names {
		isLast := index == len(names)-1
		connector := "├── "
		extension := "│   "
		if isLast {
			connector = "└── "
			extension = "    "
		}
		entryPath := filepath.Join(dirPath, name)
		if info, err := os.Stat(entryPath); err == nil && info.IsDir() {
			lines = append(lines, prefix+connector+name+"/")
			lines = append(lines, buildTree(
				entryPath, prefix+extension, maxDepth, currentDepth+1,
			)...)
			continue
		}
		lines = append(lines, prefix+connector+name)
	}

	return lines
}
