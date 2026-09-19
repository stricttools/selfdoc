package assembly

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
)

// gitTimeout bounds every git invocation the integrate loop makes.
const gitTimeout = 300 * time.Second

// indexTimeout bounds the search index build over the whole assembled site,
// which is every project's pages at once and therefore the longest-running
// subprocess a deploy launches.
const indexTimeout = 900 * time.Second

// stepOptions is one [runStep] invocation, as its caller declares it.
type stepOptions struct {
	// Cwd is the working directory of the child process. Empty inherits.
	Cwd string
	// Step names the operation in the failure message.
	Step string
	// Timeout bounds the run.
	Timeout time.Duration
	// Resource is the opaque token naming what this run produces.
	Resource string
	// Grant names the grant on the running command that authorizes the run.
	Grant string
	// Check makes a non-zero exit a hard error. The caller that wants to read
	// the exit code itself -- the commit and the push, whose non-zero exits
	// mean "nothing to commit" and "retry" -- leaves it false.
	Check bool
	// CaptureOutput captures the streams, which is also what puts the child's
	// stderr into the failure message.
	CaptureOutput bool
}

// runStep runs argv and reports a non-zero exit as an error when the caller
// declared [stepOptions.Check].
//
// A nil result with a nil error means the run was recorded rather than
// performed, which is the previewing handle's answer and the one thing every
// caller has to branch on: there is no exit code to read and no output to
// parse, because nothing ran.
func runStep(h *effects.Handle, argv []string, opts stepOptions) (*effects.Result, error) {
	options := []effects.Option{effects.Timeout(opts.Timeout)}
	if opts.CaptureOutput {
		options = append(options, effects.CaptureOutput())
	}
	if opts.Cwd != "" {
		options = append(options, effects.Cwd(opts.Cwd))
	}
	if opts.Resource != "" {
		options = append(options, effects.Resource(opts.Resource))
	}
	if opts.Grant != "" {
		options = append(options, effects.Grant(opts.Grant))
	}
	result, err := h.Run(argv, options...)
	if err != nil {
		return nil, err
	}
	if result.Unsettled {
		return nil, nil
	}
	if opts.Check && result.ExitCode != 0 {
		detail := ""
		if opts.CaptureOutput {
			detail = strings.TrimSpace(string(result.Stderr))
		}
		if detail != "" {
			return nil, errorf("%s failed (exit %d): %s", opts.Step, result.ExitCode, detail)
		}
		return nil, errorf("%s failed (exit %d)", opts.Step, result.ExitCode)
	}
	return &result, nil
}

// BuildOptions is what one [BuildSourceProject] takes.
type BuildOptions struct {
	// SourceDir is the cloned source project's checkout.
	SourceDir string
	// Scope is the dispatch's scope, one of [site.IntegrateScopes].
	Scope string
	// Home marks the project the roster names home, which is built through
	// the one build that can resolve a site-level directive.
	Home bool
	// ManifestsDir is the assembly's manifests directory, which only the home
	// build reads -- it is where any project's live version comes from.
	ManifestsDir string
	// Theme overrides whatever theme the checkout's own selfdoc.json
	// declares, for this build only. Empty leaves every project on its
	// configured theme, which is what a deploy always does.
	Theme string
	// Siblings are the other projects on the assembled site this build's
	// output is grafted into, which every built page ends by linking. It is
	// stated by the caller rather than read from anywhere here: a standalone
	// build has no assembled site around it, states none, and emits no
	// section at all.
	Siblings []build.SiblingProject
	// SiteName is the name of the assembled site this build's output is
	// grafted into, which every built page's document title ends with. It is
	// stated by the caller for the same reason Siblings is: a standalone
	// build has no site around it and states none.
	SiteName string
}

// BuildSourceProject builds the cloned source project.
//
// The home project builds through the home target, which is the one build that
// can resolve a site-level directive: its front page renders the curated
// listing with every project's live version, and the manifests those versions
// come from are the assembly's, not its own.
//
// The build runs in this process rather than as a subprocess. The Python shelled
// out to two other commands because the former blog package never imported the
// docs generator; one binary carries both, so the build is a call. Nothing
// about which build runs changed:
// a posts-scope dispatch builds the posts alone, the home project builds with
// the assembly's manifests in scope, and every other project builds its newest
// declared version.
func BuildSourceProject(opts BuildOptions, h *effects.Handle) error {
	switch {
	case opts.Scope == "posts":
		_, err := build.Build(build.Options{
			DirPath:  opts.SourceDir,
			Target:   "posts",
			Theme:    opts.Theme,
			Siblings: opts.Siblings,
			SiteName: opts.SiteName,
		}, h)
		return err
	case opts.Home:
		manifests, err := filepath.Abs(opts.ManifestsDir)
		if err != nil {
			return err
		}
		_, err = sitedirectives.BuildHomeProject(
			opts.SourceDir, manifests, opts.Theme, false, h,
		)
		return err
	default:
		latest, err := site.DetectLatestVersion(opts.SourceDir)
		if err != nil {
			return err
		}
		_, err = build.Build(build.Options{
			DirPath:       opts.SourceDir,
			VersionFilter: latest,
			Theme:         opts.Theme,
			Siblings:      opts.Siblings,
			SiteName:      opts.SiteName,
		}, h)
		return err
	}
}

// IndexSite builds the Pagefind search index over the assembled site.
//
// The indexer also writes its own search widget beside the index. An assembly
// whose pages all draw their own search surface -- every framework-theme page
// does -- references none of it, and the unwanted payload is pruned right after
// indexing. One page still loading the widget keeps it for the whole tree.
//
// Two invocations are tried in order, the Python distribution's module entry
// point and the standalone binary, because the two ways pagefind is installed
// put it in two different places. A candidate that is not installed is skipped;
// a candidate that runs and fails is the answer, and is reported.
func IndexSite(siteDir string, h *effects.Handle) error {
	candidates := [][]string{
		{"python3", "-m", "pagefind", "--site", siteDir},
		{"pagefind", "--site", siteDir},
	}
	indexed := false
	for _, argv := range candidates {
		result, err := runStep(h, argv, stepOptions{
			Step:          "pagefind index",
			Timeout:       indexTimeout,
			Resource:      "search-index:" + siteDir,
			CaptureOutput: true,
		})
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if result == nil {
			// Recorded, not performed: there is no index to prune around.
			return nil
		}
		if result.ExitCode == 0 {
			indexed = true
			break
		}
		if strings.Contains(result.StderrString(), "No module named pagefind") {
			continue
		}
		return errorf(
			"pagefind index failed (exit %d): %s",
			result.ExitCode, strings.TrimSpace(result.StderrString()),
		)
	}
	if !indexed {
		return errorf(
			"pagefind is not installed, so the assembled site cannot be " +
				"indexed and its search would answer nothing. Install it " +
				"with 'pip install pagefind[bin]' or 'npm install -g " +
				"pagefind'.",
		)
	}
	_, err := build.PruneUnreferencedPagefindWidget(siteDir, h)
	return err
}

// SiblingsFor is the sibling list one project's build is handed: every other
// project on the assembled site, minus the home project.
//
// It reads the assembly's own manifests directory and roster, which is where
// the site's membership is recorded. A checkout that holds neither yields no
// siblings, and a build handed none emits no section.
func SiblingsFor(manifestsDir, assemblyDir, selfSlug string) ([]build.SiblingProject, error) {
	manifests, err := site.LoadAssemblyManifests(manifestsDir)
	if err != nil {
		return nil, err
	}
	roster, err := site.LoadRoster(assemblyDir)
	if err != nil {
		return nil, err
	}
	return build.SiblingsFromManifests(manifests, roster.Home, selfSlug), nil
}

// SiteName is the name the assembled site goes by, for the builds whose pages
// end their titles with it: the home project's manifest name.
//
// A tree that declares no home project has no front page and therefore no name
// of its own, and its pages read as a standalone deploy's.
func SiteName(manifests []map[string]any, homeSlug string) string {
	if homeSlug == "" {
		return ""
	}
	return shared.SiteName(manifests, homeSlug)
}

// SiteNameFor is [SiteName] read off the assembly's own manifests directory
// and roster, which is where the site's membership and its home project are
// recorded.
func SiteNameFor(manifestsDir, assemblyDir string) (string, error) {
	roster, err := site.LoadRoster(assemblyDir)
	if err != nil {
		return "", err
	}
	if roster.Home == "" {
		return "", nil
	}
	manifests, err := site.LoadAssemblyManifests(manifestsDir)
	if err != nil {
		return "", err
	}
	return SiteName(manifests, roster.Home), nil
}
