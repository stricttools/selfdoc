package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// gitProbeTimeout bounds one tag-resolution probe, and archiveTimeout bounds
// the extraction of a whole tag's content.
const (
	gitProbeTimeout = 10 * time.Second
	archiveTimeout  = 30 * time.Second
)

// ExtractVersionContent extracts a version's docs and source content out of
// its git tag into a cache directory, and returns that directory.
//
// The tag is looked up as "v{version}" and then as "{version}". Its commit is
// resolved through "^{commit}", so an annotated tag is dereferenced to what it
// points at, and recorded in a ".hash" sentinel beside the extracted content:
// a tag still standing at the same commit is not extracted again.
//
// The content is moved by streaming "git archive" into "tar -x", so nothing
// buffers the whole tree. The cache is one of selfdoc's uncommitted
// directories, so the derived ignore file inside the tool-state directory is
// what keeps it out of the repository.
//
// Both docs roots are extracted: the handwritten pages the config names, and
// the generated pages committed beside them, so an archived version's build
// sees the same two roots the working tree's build does.
func ExtractVersionContent(version string, config map[string]any, baseDir string, h *effects.Handle) (string, error) {
	cacheRoot := layout.Path(baseDir, layout.VersionsRel)
	cacheDir := filepath.Join(cacheRoot, version)
	hashFile := filepath.Join(cacheDir, ".hash")

	if err := layout.EnsureDir(h, baseDir, layout.VersionsRel); err != nil {
		return "", err
	}

	tagName := ""
	for _, candidate := range []string{"v" + version, version} {
		result, err := h.Run([]string{"git", "rev-parse", "refs/tags/" + candidate},
			effects.CaptureOutput(), effects.Timeout(gitProbeTimeout),
			effects.Cwd(baseDir), effects.Read())
		if err != nil {
			return "", err
		}
		if result.ExitCode == 0 {
			tagName = candidate
			break
		}
	}
	if tagName == "" {
		return "", fmt.Errorf(
			"Git tag for version '%s' not found. Tried 'v%s' and '%s'.",
			version, version, version)
	}

	result, err := h.Run([]string{"git", "rev-parse", tagName + "^{commit}"},
		effects.CaptureOutput(), effects.Timeout(gitProbeTimeout),
		effects.Cwd(baseDir), effects.Read())
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("Failed to resolve commit for tag '%s': %s",
			tagName, util.PythonStrip(string(result.Stderr)))
	}
	commitHash := util.PythonStrip(string(result.Stdout))

	if cached, readErr := os.ReadFile(hashFile); readErr == nil {
		if util.PythonStrip(string(cached)) == commitHash {
			return cacheDir, nil
		}
	}

	if info, statErr := os.Stat(cacheDir); statErr == nil && info.IsDir() {
		if err := h.RmTree(cacheDir); err != nil {
			return "", err
		}
	}
	if err := h.MkdirAll(cacheDir); err != nil {
		return "", err
	}

	docsPath := strings.TrimRight(configString(config, "docs"), "/")
	archivePaths := []string{docsPath}
	// The generated pages are the second docs root, and a tag that predates
	// them carries none: asking git archive for a path the tag does not
	// hold is an error, so the path is named only when the tag holds it.
	//
	// The ownership manifests travel with them: the extracted checkout is
	// built like any other repository, and creating its output directory
	// needs the manifest that permits it. A manifest already inside an
	// archived path -- the docs directory's own -- is not named twice.
	optionalPaths := []string{layout.GeneratedPagesRel}
	for _, claimed := range layout.Declared() {
		manifestPath := layout.DirectoryManifestRel(claimed.Name)
		if strings.HasPrefix(manifestPath, docsPath+"/") {
			continue
		}
		optionalPaths = append(optionalPaths, manifestPath)
	}
	for _, optional := range optionalPaths {
		inTag, err := pathInTag(tagName, optional, baseDir, h)
		if err != nil {
			return "", err
		}
		if inTag {
			archivePaths = append(archivePaths, optional)
		}
	}
	if declaresSource(config) {
		rawSourcePaths, sourceErr := extractors.SourcePaths(config)
		if sourceErr != nil {
			return "", sourceErr
		}
		for _, sourcePath := range rawSourcePaths {
			archivePaths = append(archivePaths, strings.TrimRight(sourcePath, "/"))
		}
	}

	gitCmd := append([]string{"git", "archive", tagName}, archivePaths...)
	tarCmd := []string{"tar", "-x", "-C", cacheDir}

	outcome, err := h.Pipeline([][]string{gitCmd, tarCmd},
		effects.Cwd(baseDir), effects.Timeout(archiveTimeout))
	if err != nil {
		return "", err
	}
	if outcome.Unsettled {
		// Recorded, not extracted. The sentinel is recorded too, so the
		// preview names every path this cache fill would create.
		if writeErr := h.Write(hashFile, []byte(commitHash+"\n"), effects.ModeDefault); writeErr != nil {
			return "", writeErr
		}
		return cacheDir, nil
	}
	if outcome.ExitCode != 0 {
		return "", fmt.Errorf("tag archive extraction failed for tag '%s': %s",
			tagName, util.PythonStrip(string(outcome.Stderr)))
	}

	if err := h.Write(hashFile, []byte(commitHash+"\n"), effects.ModeDefault); err != nil {
		return "", err
	}
	return cacheDir, nil
}

// pathInTag reports whether a tag's tree carries a path.
func pathInTag(tagName, path, baseDir string, h *effects.Handle) (bool, error) {
	result, err := h.Run(
		[]string{"git", "ls-tree", "--name-only", tagName, "--", path},
		effects.CaptureOutput(), effects.Timeout(gitProbeTimeout),
		effects.Cwd(baseDir), effects.Read())
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0 && strings.TrimSpace(string(result.Stdout)) != "", nil
}

// isFile reports whether path names an existing regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isDir reports whether path names an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// declaresSource reports whether the config declares any source entry.
func declaresSource(config map[string]any) bool {
	entries, ok := config["source"].([]any)
	return ok && len(entries) > 0
}

// configString reads a string-valued config key, answering "" for a key that
// is absent or carries something else.
func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}
