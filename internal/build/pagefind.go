package build

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/page"
)

// pagefindTimeout bounds one indexing run.
const pagefindTimeout = 120 * time.Second

// RunPagefind runs Pagefind over the built site to generate its search index.
//
// Two invocations are tried in order -- the Python module ("python3 -m
// pagefind") and then the standalone binary ("pagefind") -- because the
// project installs whichever suits it. An installation that exists and fails
// is an error naming its stderr; no installation at all is an error naming how
// to install one.
//
// An interpreter that does not carry the module counts as no installation and
// the next candidate is tried. The Python this replaces ran the module under
// its OWN interpreter (sys.executable), so the module was either importable or
// the whole program was, and the case could not arise; a Go binary asks
// whatever python3 is on PATH, and refusing at "No module named pagefind"
// instead of trying the standalone binary would refuse a machine that has
// Pagefind installed.
func RunPagefind(outputDir string, h *effects.Handle) error {
	candidates := [][]string{
		{"python3", "-m", "pagefind", "--site", outputDir},
		{"pagefind", "--site", outputDir},
	}
	for _, argv := range candidates {
		result, err := h.Run(argv,
			effects.CaptureOutput(),
			effects.Timeout(pagefindTimeout),
			effects.Resource("pagefind:"+outputDir))
		if err != nil {
			if errors.Is(err, effects.ErrTimeout) {
				return errors.New("Pagefind indexing timed out after 120 seconds.")
			}
			if isExecutableMissing(err) {
				continue
			}
			return err
		}
		if result.Unsettled {
			return nil
		}
		if result.ExitCode == 0 {
			return nil
		}
		if strings.Contains(result.StderrString(), "No module named pagefind") {
			continue
		}
		// The command was found and failed -- report what it said.
		return fmt.Errorf("Pagefind indexing failed (exit %d):\n%s",
			result.ExitCode, result.StderrString())
	}
	return errors.New(
		"Pagefind is not installed. Install with: pip install 'pagefind[bin]' " +
			"or npm install -g pagefind")
}

// isExecutableMissing reports whether err says the command does not exist,
// which is the condition the next candidate invocation is tried on.
func isExecutableMissing(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}

// PruneUnreferencedPagefindWidget deletes Pagefind's own search widget when no
// page in the tree loads it.
//
// The indexer writes its widget beside the index whether or not anything
// references it, and a framework theme draws its own search surface over
// Pagefind's query API instead. What decides is therefore not the theme but
// the pages: a tree where nothing names the widget's bundle carries it as dead
// weight, and its stylesheets paint a design language those pages do not
// share. Reading the answer off the emitted HTML is also what makes this right
// for an assembly, where one tree can carry several themes and one page still
// loading the widget keeps it for everyone.
//
// The index itself and pagefind.js -- the query API a palette calls -- are
// never touched.
//
// It returns the paths removed, in the order [page.PagefindUIAssets] declares
// them.
func PruneUnreferencedPagefindWidget(siteDir string, h *effects.Handle) ([]string, error) {
	widgetDir := filepath.Join(siteDir, "pagefind")
	if info, err := os.Stat(widgetDir); err != nil || !info.IsDir() {
		return nil, nil
	}
	absWidget, err := filepath.Abs(widgetDir)
	if err != nil {
		return nil, err
	}

	referenced := false
	walkErr := filepath.WalkDir(siteDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			absDir, absErr := filepath.Abs(path)
			if absErr != nil {
				return absErr
			}
			if absDir == absWidget || strings.HasPrefix(absDir, absWidget+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".html") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(content), page.PagefindWidgetBundle) {
			referenced = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if referenced {
		return nil, nil
	}

	var removed []string
	for _, name := range page.PagefindUIAssets {
		path := filepath.Join(widgetDir, name)
		if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
			continue
		}
		if err := h.Remove(path); err != nil {
			return removed, err
		}
		removed = append(removed, path)
	}
	return removed, nil
}
