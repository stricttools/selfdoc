package check

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/manifest"
)

// checkManifestFreshness checks the manifest's pages and posts against the
// files on disk (STALE002).
//
// A project with no manifest has nothing to disagree with, and neither does a
// manifest that cannot be read.
func checkManifestFreshness(config map[string]any, dirPath string) ([]lints.LintResult, error) {
	manifestPath := layout.Path(dirPath, layout.ManifestRel)
	if !isFile(manifestPath) {
		return nil, nil
	}

	record, err := manifest.Load(manifestPath)
	if err != nil || record == nil {
		// A manifest this reader cannot claim to understand is not a
		// freshness finding: the tolerant reader's own refusal is the
		// answer, and the Python's loader answered absence the same way.
		return nil, nil
	}

	docsDir := filepath.Join(dirPath, configString(config, "docs", layout.DocsDefault))
	generatedDir := layout.Path(dirPath, layout.GeneratedPagesRel)
	postsDir := filepath.Join(dirPath, postsDirRel(config))

	// Pages on disk, from both docs roots -- a page is a page wherever it is
	// authored -- excluding the underscore-prefixed templates.
	diskPages := map[string]bool{}
	for _, root := range []string{docsDir, generatedDir} {
		if !isDir(root) {
			continue
		}
		if err := walkMarkdown(root, func(relPath, fileName string) {
			if !strings.HasPrefix(fileName, "_") {
				diskPages[relPath] = true
			}
		}); err != nil {
			return nil, err
		}
	}

	diskPosts := map[string]bool{}
	if isDir(postsDir) {
		if err := walkMarkdown(postsDir, func(relPath, _ string) {
			diskPosts[relPath] = true
		}); err != nil {
			return nil, err
		}
	}

	manifestPages := map[string]bool{}
	for _, page := range record.Pages {
		manifestPages[page.Path] = true
	}
	manifestPosts := map[string]bool{}
	for _, post := range record.Posts {
		manifestPosts[post.Path] = true
	}

	var results []lints.LintResult

	for _, path := range sortedDifference(diskPages, manifestPages) {
		results = append(results, lints.MustLintResult(
			path, nil, "STALE002",
			"page exists on disk but not in manifest (run 'selfdoc gen' to update)",
		))
	}
	for _, path := range sortedDifference(manifestPages, diskPages) {
		results = append(results, lints.MustLintResult(
			path, nil, "STALE002",
			fmt.Sprintf("manifest lists page '%s' but file not found on disk", path),
		))
	}
	for _, path := range sortedDifference(diskPosts, manifestPosts) {
		results = append(results, lints.MustLintResult(
			path, nil, "STALE002",
			"post exists on disk but not in manifest (run 'selfdoc gen' to update)",
		))
	}
	for _, path := range sortedDifference(manifestPosts, diskPosts) {
		results = append(results, lints.MustLintResult(
			path, nil, "STALE002",
			fmt.Sprintf("manifest lists post '%s' but file not found on disk", path),
		))
	}

	return results, nil
}

// walkMarkdown calls visit for every .md file under root, with the file's path
// relative to root and its base name.
func walkMarkdown(root string, visit func(relPath, fileName string)) error {
	return filepath.WalkDir(root, func(walked string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		relPath, relErr := filepath.Rel(root, walked)
		if relErr != nil {
			return relErr
		}
		visit(relPath, entry.Name())
		return nil
	})
}

// sortedDifference returns the members of left that right does not carry, in
// sorted order.
func sortedDifference(left, right map[string]bool) []string {
	var only []string
	for item := range left {
		if !right[item] {
			only = append(only, item)
		}
	}
	sort.Strings(only)
	return only
}
