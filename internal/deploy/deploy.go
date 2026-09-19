// Package deploy publishes a built documentation site.
//
// Two providers are supported, selected by the "provider" key of the deploy
// section of selfdoc.json and dispatched by [Dispatch]:
//
//   - Cloudflare Pages, through the wrangler CLI;
//   - GitHub Pages, by force-pushing the built tree to a gh-pages branch.
package deploy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// Error is the failure every deploy operation reports: a missing wrangler,
// an unusable push target, a git step that failed, a deploy that timed out,
// and a provider command that exited non-zero.
//
// It is the Go counterpart of the Python surface's DeployError, and the one
// error type a caller needs to recognize with errors.As to render "Deploy
// error" diagnostics distinctly from an unexpected internal failure.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

// UnknownProviderError is returned by [Dispatch] for a deploy section naming a
// provider this package does not implement. It is a distinct type rather than
// an [Error] because it reports a malformed configuration rather than a deploy
// that went wrong, and the CLI renders the two differently.
type UnknownProviderError struct {
	// Provider is the unrecognized provider name, as configured.
	Provider string
}

// Error names the unrecognized provider.
func (e *UnknownProviderError) Error() string {
	return fmt.Sprintf("Unknown deploy provider '%s'", e.Provider)
}

// deployTimeout bounds both providers' one network operation: wrangler's
// upload, and the force-push of the gh-pages branch.
const deployTimeout = 120 * time.Second

// Config is the decoded "deploy" section of selfdoc.json.
type Config struct {
	// Provider selects the implementation: "cloudflare-pages" or
	// "github-pages".
	Provider string
	// Project is the Cloudflare Pages project name. It is read only by the
	// cloudflare-pages provider.
	Project string
}

// Dispatch deploys outputDir through the provider cfg names.
//
// version is the version string the deploy's commit message carries. target is
// the GitHub Pages push target and is ignored by the Cloudflare provider; see
// [GitHubPages] for what a target may be and why it is never inferred.
//
// A provider this package does not implement is an [UnknownProviderError].
func Dispatch(cfg Config, outputDir, version, target string, h *effects.Handle) error {
	switch cfg.Provider {
	case "cloudflare-pages":
		return CloudflarePages(outputDir, cfg.Project, version, h)
	case "github-pages":
		return GitHubPages(outputDir, version, target, h)
	default:
		return &UnknownProviderError{Provider: cfg.Provider}
	}
}

// CloudflarePages deploys outputDir to the Cloudflare Pages project
// projectName using the wrangler CLI, with version in the deploy's commit
// message.
//
// wrangler must be installed and authenticated, either through `wrangler
// login` or through the credential variables [ResolveCloudflareEnv] bridges.
//
// Under a handle in preview mode the upload is recorded rather than performed,
// and nothing is reported as deployed.
func CloudflarePages(outputDir, projectName, version string, h *effects.Handle) error {
	ResolveCloudflareEnv()

	if _, err := exec.LookPath("wrangler"); err != nil {
		return &Error{
			"wrangler CLI not found. Install it with: npm install -g wrangler\n" +
				"Then authenticate with: wrangler login",
		}
	}

	argv := []string{
		"wrangler",
		"pages",
		"deploy",
		outputDir,
		"--project-name=" + projectName,
		"--commit-message=v" + version,
	}

	result, err := h.Run(
		argv,
		effects.CaptureOutput(),
		effects.Timeout(deployTimeout),
		effects.Resource("cf-pages:"+projectName),
		effects.Grant("deploy"),
	)
	if err != nil {
		if errors.Is(err, effects.ErrTimeout) {
			return &Error{fmt.Sprintf(
				"Cloudflare Pages deploy timed out after 120s for project '%s'", projectName,
			)}
		}
		return err
	}

	if result.Unsettled {
		// Recorded, not deployed: nothing ran, so there is no exit status to
		// test and nothing to report as deployed.
		return nil
	}

	if result.ExitCode != 0 {
		return &Error{fmt.Sprintf(
			"Cloudflare Pages deploy failed (exit %d):\n%s",
			result.ExitCode, strings.TrimSpace(string(result.Stderr)),
		)}
	}

	fmt.Printf("Deployed docs v%s to Cloudflare Pages project '%s'\n", version, projectName)
	return nil
}

// ResolveCloudflareEnv bridges the CF_-prefixed credential variables to the
// CLOUDFLARE_-prefixed ones wrangler reads.
//
// The canonical names here are CF_ACCOUNT_ID and CF_PAGES_API_TOKEN; wrangler
// expects CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN. An already-set
// CLOUDFLARE_ variable is never overwritten.
//
// It sets them on this process's own environment, which is what the child
// inherits: passing them as an explicit child environment instead would mean
// handing the effects handle the whole inherited environment as overrides, and
// a preview would then render every variable this process holds.
func ResolveCloudflareEnv() {
	bridge := func(target, fallback string) {
		if os.Getenv(target) != "" {
			return
		}
		if value := os.Getenv(fallback); value != "" {
			os.Setenv(target, value)
		}
	}
	bridge("CLOUDFLARE_ACCOUNT_ID", "CF_ACCOUNT_ID")
	bridge("CLOUDFLARE_API_TOKEN", "CF_PAGES_API_TOKEN")
}

// GitHubPages deploys outputDir by force-pushing it to the remote's gh-pages
// branch, with version in the commit message.
//
// The tree is staged in a temporary directory, so the current working tree is
// never touched, and a .nojekyll file is written so GitHub serves the built
// HTML instead of running Jekyll over it.
//
// target is the required push target: either a git remote URL, used verbatim,
// or the path of a repository whose "origin" remote is resolved. It is
// deliberately not derived from the process's working directory -- this
// function FORCE-pushes a gh-pages branch, and a target taken from the working
// directory silently aims that force-push at whatever repository the process
// happens to be sitting in.
//
// Under a handle in preview mode every step is recorded rather than performed,
// and nothing is reported as deployed.
func GitHubPages(outputDir, version, target string, h *effects.Handle) error {
	if target == "" {
		return &Error{
			"selfdoc deploy requires an explicit push target for GitHub " +
				"Pages: pass either a git remote URL or the path to the " +
				"repository whose 'origin' remote should be used. This deploy " +
				"force-pushes the gh-pages branch, so the target is never " +
				"inferred.",
		}
	}

	remote := target
	if !looksLikeRemoteURL(target) {
		info, err := os.Stat(target)
		if err != nil || !info.IsDir() {
			return &Error{fmt.Sprintf(
				"selfdoc deploy target '%s' is neither a git "+
					"remote URL nor an existing directory.", target,
			)}
		}
		result, err := h.Run(
			[]string{"git", "remote", "get-url", "origin"},
			effects.Cwd(target),
			effects.CaptureOutput(),
			effects.Check(),
			effects.Read(),
		)
		if err != nil {
			var exitErr *effects.ExitError
			if errors.As(err, &exitErr) {
				return &Error{fmt.Sprintf(
					"Could not determine the git remote URL for '%s'. "+
						"Ensure 'origin' remote is configured there.", target,
				)}
			}
			return err
		}
		remote = strings.TrimSpace(string(result.Stdout))
	}

	// The staging directory itself is outside the effects chokepoint, as it
	// was in the Python: a private temporary tree that no preview needs to
	// name and that this function always removes itself.
	tmp, err := os.MkdirTemp("", "selfdoc-gh-pages-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// Init a fresh repo and create the gh-pages branch.
	if _, err := runGit([]string{"init"}, tmp, h); err != nil {
		return err
	}
	if _, err := runGit([]string{"checkout", "-b", "gh-pages"}, tmp, h); err != nil {
		return err
	}

	// Copy the HTML output into the temp repo.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		src := util.PathJoin(outputDir, entry.Name())
		dst := util.PathJoin(tmp, entry.Name())
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := h.CopyTree(src, dst, false); err != nil {
				return err
			}
		} else if err := h.CopyFile(src, dst); err != nil {
			return err
		}
	}

	// Prevent Jekyll processing on GitHub.
	if err := h.Write(util.PathJoin(tmp, ".nojekyll"), nil, effects.ModeDefault); err != nil {
		return err
	}

	// Commit all files.
	if _, err := runGit([]string{"add", "."}, tmp, h); err != nil {
		return err
	}
	if _, err := runGit([]string{"commit", "-m", "docs: v" + version}, tmp, h); err != nil {
		return err
	}

	// Force-push to gh-pages on the remote.
	pushed, err := h.Run(
		[]string{"git", "push", "--force", remote, "gh-pages"},
		effects.Cwd(tmp),
		effects.CaptureOutput(),
		effects.Check(),
		effects.Timeout(deployTimeout),
		effects.Resource("gh-pages:"+remote),
		effects.Grant("force-push"),
	)
	if err != nil {
		if errors.Is(err, effects.ErrTimeout) {
			return &Error{
				"GitHub Pages deploy timed out after 120s while pushing to gh-pages",
			}
		}
		var exitErr *effects.ExitError
		if errors.As(err, &exitErr) {
			return &Error{fmt.Sprintf(
				"Failed to push to gh-pages branch:\n%s", strings.TrimSpace(string(exitErr.Stderr)),
			)}
		}
		return err
	}
	if pushed.Unsettled {
		// Recorded, not pushed. Nothing was deployed, so say nothing.
		return nil
	}

	fmt.Printf("Deployed docs v%s to GitHub Pages (gh-pages branch)\n", version)
	return nil
}

// remoteURLPrefixes are the transport prefixes that identify target as a git
// remote URL rather than a local repository path.
var remoteURLPrefixes = []string{"http://", "https://", "ssh://", "git://", "file://"}

// looksLikeRemoteURL reports whether target is a git remote URL rather than a
// local repository path.
func looksLikeRemoteURL(target string) bool {
	for _, prefix := range remoteURLPrefixes {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	// scp-style syntax: user@host:path. A local repository path this function
	// would accept never carries a ':' in its first segment.
	head, _, _ := strings.Cut(target, "/")
	return strings.Contains(head, "@") && strings.Contains(head, ":")
}

// runGit runs one git command in dir, reporting a non-zero exit as an [Error].
//
// A recorded run is returned as it is: forwarding it onward keeps the preview
// going, where reading an exit code off it would truncate the preview at step
// one and hide the force-push this whole staging sequence exists to perform.
func runGit(args []string, dir string, h *effects.Handle) (effects.Result, error) {
	result, err := h.Run(
		append([]string{"git"}, args...),
		effects.Cwd(dir),
		effects.CaptureOutput(),
	)
	if err != nil {
		return result, err
	}
	if result.Unsettled {
		return result, nil
	}
	if result.ExitCode != 0 {
		return result, &Error{fmt.Sprintf(
			"git %s failed (exit %d):\n%s",
			strings.Join(args, " "), result.ExitCode, strings.TrimSpace(string(result.Stderr)),
		)}
	}
	return result, nil
}
