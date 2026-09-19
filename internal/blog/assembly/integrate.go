package assembly

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/verify"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// The values the deploy passes for every input it does not read from the
// dispatch. Each is a default the caller states rather than one this package
// fills in: the command resolves an absent flag to the constant and passes it,
// so the engine never has to guess what "unset" meant.
const (
	// DefaultAttempts is how many times a deploy re-syncs with the remote and
	// retries the push before failing.
	DefaultAttempts = 3
	// DefaultRetryDelay is how long a deploy waits between attempts.
	DefaultRetryDelay = 5 * time.Second
	// DefaultBranch is the assembly branch the deploy commits and pushes to.
	DefaultBranch = "main"
	// DefaultGitUserName is the identity the deploy's commit carries.
	DefaultGitUserName = "github-actions[bot]"
	// DefaultGitUserEmail is that identity's address.
	DefaultGitUserEmail = "github-actions[bot]@users.noreply.github.com"
)

// PushGrant is the grant on the running command that authorizes the deploy's
// push to the assembly's branch -- the content the live site serves.
const PushGrant = "assembly-commit"

// VerifyOptions is what one [VerifyBeforeDeploy] takes.
type VerifyOptions struct {
	// AssemblyDir is the assembly repository checkout to verify.
	AssemblyDir string
	// CanonicalBase is the site's canonical base URL. Required.
	CanonicalBase string
	// Fetch is the outbound fetch layer; nil selects the real one.
	Fetch verify.Fetcher
	// Now is the clock the outbound cache window is measured against, in
	// seconds since the epoch. Zero takes the wall clock.
	Now float64
	// Stderr is where the not-checked lines go. Nil writes to the process's
	// standard error.
	Stderr io.Writer
}

// VerifyBeforeDeploy verifies the assembled tree, or refuses to let the deploy
// continue.
//
// It returns the checks that ran. A check that could not run says so on stderr
// rather than passing quietly -- an assertion nobody made must not look like
// one that held.
//
// The outbound results this produces are written back into the checkout so the
// next deploy inherits them; that write is the deploy's, not the
// verification's, which is why it happens here and not inside
// [verify.VerifyAssembly].
//
// A tree that failed verification is an error naming every offender, before
// anything is committed or pushed.
func VerifyBeforeDeploy(opts VerifyOptions, h *effects.Handle) ([]string, error) {
	now := opts.Now
	if now == 0 {
		now = float64(time.Now().Unix())
	}
	report, err := verify.VerifyAssembly(
		opts.AssemblyDir, opts.CanonicalBase, opts.Fetch, now,
	)
	if err != nil {
		return nil, err
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	for _, skip := range report.Skipped {
		fmt.Fprintf(stderr, "verify: %s was NOT checked -- %s\n", skip.Check, skip.Reason)
	}
	if !report.OK() {
		return nil, errorf("%s", report.ErrorText())
	}
	if len(report.OutboundCache) > 0 {
		rendered, err := site.RenderOutboundCache(report.OutboundCache)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(opts.AssemblyDir, site.OutboundCachePath)
		if err := h.Write(path, []byte(rendered), effects.ModeDefault); err != nil {
			return nil, err
		}
	}
	return report.Ran, nil
}

// IntegrateOptions is what one [IntegrateProject] takes.
type IntegrateOptions struct {
	// Slug is the project being integrated; its site subtree is
	// "site/<slug>/". Required unless Scope is [SharedOnlyScope].
	Slug string
	// Version is the version being integrated, recorded in the membership
	// record and in the commit message.
	Version string
	// Ref is the git ref the source project was cloned at, recorded in the
	// membership record.
	Ref string
	// SourceRepo is the source project's repository, recorded in the
	// membership record.
	SourceRepo string
	// Scope is what this dispatch replaces, one of [site.IntegrateScopes].
	// Empty means a full project build.
	Scope string
	// CanonicalBase is the absolute canonical base URL of the assembly site.
	// Required: it targets the redirect worker and the rel=canonical links.
	CanonicalBase string
	// AssemblyDir is the assembly repository checkout being updated. Empty
	// means the current directory.
	AssemblyDir string
	// SourceDir is the cloned source project. Empty means
	// "<AssemblyDir>/source/<Slug>", where the deploy workflow clones it.
	SourceDir string
	// Branch is the assembly branch to commit and push to. Empty means
	// [DefaultBranch].
	Branch string
	// Attempts is how many times to re-sync with the remote and retry the
	// push before failing. Fewer than one is an error rather than a value
	// this fills in -- the command resolves an absent flag to
	// [DefaultAttempts] and passes it.
	Attempts int
	// RetryDelay is how long to wait between attempts. Zero waits not at all,
	// which is what a test wants; the deploy passes [DefaultRetryDelay].
	RetryDelay time.Duration
	// GitUserName and GitUserEmail are the identity the commit carries. Empty
	// means [DefaultGitUserName] and [DefaultGitUserEmail].
	GitUserName  string
	GitUserEmail string
	// SkipBuild leaves the source checkout's existing build output alone
	// instead of rebuilding it. It is inverted on purpose: building is what a
	// deploy does, so the zero value is the deploy's behaviour and only a
	// caller that has already built -- a test, a second look at one graft --
	// has to say anything.
	SkipBuild bool
	// Theme overrides the theme every checkout declares, for this run only.
	// Empty leaves every project on its configured theme, which is what a
	// deploy does.
	Theme string
	// Fetch is the outbound fetch layer the verification uses; nil selects the
	// real one.
	Fetch verify.Fetcher
	// Now is the clock the verification's outbound cache window is measured
	// against, in seconds since the epoch. Zero takes the wall clock.
	Now float64
	// Stderr is where the graft's advisory and the verification's not-checked
	// lines go. Nil writes to the process's standard error.
	Stderr io.Writer
}

// IntegrateSummary is what one [IntegrateProject] run did.
type IntegrateSummary struct {
	// Scope is the scope actually run, with an empty dispatch resolved to
	// "full".
	Scope string
	// Slug is the project integrated, empty for a shared-elements run.
	Slug string
	// Version is the version integrated.
	Version string
	// Touched is the paths the graft changed.
	Touched []string
	// Shared is the cross-project files regenerated.
	Shared []string
	// Retired is every slug the reconciliation removed from the tree.
	Retired []string
	// Verified is the checks the pre-deploy verification ran.
	Verified []string
	// Attempt is the attempt that pushed.
	Attempt int
	// Committed reports whether a commit was created at all. False means the
	// assembly was already current.
	Committed bool
}

// IntegrateProject integrates one dispatched project into the assembly
// repository checkout and pushes the result.
//
// This is the body the generated deploy workflow used to embed as shell and
// inline interpreter snippets. It builds the cloned source project, then --
// inside a retry loop that re-syncs to the remote every attempt, so two
// concurrent deploys converge instead of clobbering each other -- grafts the
// build into the tree, refreshes the project's manifest and membership record,
// regenerates the shared cross-project files, rebuilds the search index,
// verifies, commits and pushes.
func IntegrateProject(opts IntegrateOptions, h *effects.Handle) (*IntegrateSummary, error) {
	scope := opts.Scope
	if scope == "" {
		scope = "full"
	}
	if !slices.Contains(site.IntegrateScopes, scope) {
		return nil, errorf(
			"unknown scope %s; expected one of %s",
			util.PythonRepr(scope), strings.Join(site.IntegrateScopes, ", "),
		)
	}
	if opts.Slug == "" && scope != SharedOnlyScope {
		return nil, errorf("slug is required for a project-scoped integrate")
	}
	if opts.Attempts < 1 {
		return nil, errorf("attempts must be at least 1")
	}

	assemblyDir := opts.AssemblyDir
	if assemblyDir == "" {
		assemblyDir = "."
	}
	branch := opts.Branch
	if branch == "" {
		branch = DefaultBranch
	}
	gitUserName := opts.GitUserName
	if gitUserName == "" {
		gitUserName = DefaultGitUserName
	}
	gitUserEmail := opts.GitUserEmail
	if gitUserEmail == "" {
		gitUserEmail = DefaultGitUserEmail
	}
	sourceDir := opts.SourceDir
	if sourceDir == "" {
		sourceDir = filepath.Join(assemblyDir, "source", opts.Slug)
	}
	siteDir := filepath.Join(assemblyDir, "site")
	manifestsDir := filepath.Join(assemblyDir, "manifests")
	projectsJSON := filepath.Join(assemblyDir, site.ProjectsPath)

	summary := &IntegrateSummary{
		Scope:   scope,
		Slug:    opts.Slug,
		Version: opts.Version,
	}

	// The roster is read here as well as inside the loop, for one fact the
	// build itself needs: whether this slug is the home project, which decides
	// both how it is built and where its output lands. The reading inside the
	// loop stays authoritative -- it happens after the re-sync, so a
	// retirement or a change of home another deploy committed is honoured
	// there.
	isHome := false
	if opts.Slug != "" {
		roster, err := site.LoadRoster(assemblyDir)
		if err != nil {
			return nil, err
		}
		isHome = opts.Slug == roster.Home
	}

	if scope != SharedOnlyScope && !opts.SkipBuild {
		// The sibling block every built page ends with is stated here, from
		// the manifests this checkout already holds. A build that is handed
		// none emits no block, which is what a standalone build gets.
		siblings, err := SiblingsFor(manifestsDir, assemblyDir, opts.Slug)
		if err != nil {
			return nil, err
		}
		// The site name every built page's title ends with is stated here
		// for the same reason: it is the assembly's fact, not the
		// checkout's, and a build handed none titles its pages the way a
		// standalone deploy does.
		siteName, err := SiteNameFor(manifestsDir, assemblyDir)
		if err != nil {
			return nil, err
		}
		if err := BuildSourceProject(BuildOptions{
			SourceDir:    sourceDir,
			Scope:        scope,
			Home:         isHome,
			ManifestsDir: manifestsDir,
			Theme:        opts.Theme,
			Siblings:     siblings,
			SiteName:     siteName,
		}, h); err != nil {
			return nil, err
		}
	}

	lastPushError := ""
	for attempt := 1; attempt <= opts.Attempts; attempt++ {
		summary.Attempt = attempt

		// Re-sync to the remote first: another project's deploy may have
		// committed since this run started, and its files must remain ours.
		if _, err := runStep(h, []string{"git", "fetch", "origin", branch}, stepOptions{
			Cwd: assemblyDir, Step: "git fetch", Timeout: gitTimeout,
			Resource: "git-fetch:" + assemblyDir, Check: true,
		}); err != nil {
			return nil, err
		}
		if _, err := runStep(h, []string{"git", "reset", "--hard", "origin/" + branch}, stepOptions{
			Cwd: assemblyDir, Step: "git reset", Timeout: gitTimeout,
			Resource: "git-reset:" + assemblyDir, Check: true,
		}); err != nil {
			return nil, err
		}

		if err := h.MkdirAll(manifestsDir); err != nil {
			return nil, err
		}

		// The roster is read after the re-sync, so a retirement another deploy
		// committed a minute ago is honoured by this one too. Every scope
		// reconciles: membership is declared, and the deploy's job is to make
		// the tree match the declaration whatever else it is doing.
		roster, err := site.LoadRoster(assemblyDir)
		if err != nil {
			return nil, err
		}
		if scope != SharedOnlyScope && !roster.Has(opts.Slug) {
			return nil, errorf(
				"%s is not declared in %s on the assembly repository, so "+
					"this dispatch has nothing to publish into. Add a "+
					"[[project]] block naming slug = %s and its repo, then "+
					"dispatch again.",
				util.PythonRepr(opts.Slug), site.RosterPath,
				util.PythonRepr(opts.Slug),
			)
		}
		reconciled, err := site.ReconcileMembership(assemblyDir, roster, h)
		if err != nil {
			return nil, err
		}
		summary.Retired = reconciled.Retired

		if scope != SharedOnlyScope {
			touched, err := ApplyProjectFiles(GraftOptions{
				AssemblyDir: assemblyDir,
				SourceDir:   sourceDir,
				Slug:        opts.Slug,
				Scope:       scope,
				Home:        opts.Slug == roster.Home,
				Stderr:      opts.Stderr,
			}, h)
			if err != nil {
				return nil, err
			}
			summary.Touched = touched
			if _, err := site.RecordMembership(
				projectsJSON, roster.Entries(), opts.Slug,
				opts.SourceRepo, opts.Ref, opts.Version, h,
			); err != nil {
				return nil, err
			}
		}

		sharedFiles, err := GenerateSharedFiles(SharedFilesOptions{
			SiteDir:       siteDir,
			ManifestsDir:  manifestsDir,
			CanonicalBase: opts.CanonicalBase,
			DocsBase:      opts.CanonicalBase,
			HomeSlug:      roster.Home,
			Theme:         opts.Theme,
		}, h)
		if err != nil {
			return nil, err
		}
		summary.Shared = sharedFiles

		if err := IndexSite(siteDir, h); err != nil {
			return nil, err
		}

		// Everything the deploy publishes exists now, and nothing has left
		// this checkout yet. This is the last moment a broken tree can be
		// refused instead of served, so it is where it is refused.
		verified, err := VerifyBeforeDeploy(VerifyOptions{
			AssemblyDir:   assemblyDir,
			CanonicalBase: opts.CanonicalBase,
			Fetch:         opts.Fetch,
			Now:           opts.Now,
			Stderr:        opts.Stderr,
		}, h)
		if err != nil {
			return nil, err
		}
		summary.Verified = verified

		staged := []string{"site", "manifests", site.ProjectsPath}
		if isFile(filepath.Join(assemblyDir, site.OutboundCachePath)) {
			staged = append(staged, site.OutboundCachePath)
		}
		if _, err := runStep(h, append([]string{"git", "add"}, staged...), stepOptions{
			Cwd: assemblyDir, Step: "git add", Timeout: gitTimeout,
			Resource: "git-add:" + assemblyDir, Check: true,
		}); err != nil {
			return nil, err
		}

		label := "deploy: shared elements"
		if scope != SharedOnlyScope {
			label = fmt.Sprintf("deploy: %s %s",
				opts.Slug, site.VersionLabel(opts.Version))
		}
		commit, err := runStep(h, []string{
			"git", "-c", "user.name=" + gitUserName,
			"-c", "user.email=" + gitUserEmail,
			"commit", "-m", label,
		}, stepOptions{
			Cwd: assemblyDir, Step: "git commit", Timeout: gitTimeout,
			Resource: "git-commit:" + assemblyDir, CaptureOutput: true,
		})
		if err != nil {
			return nil, err
		}
		if commit == nil {
			// Preview mode: nothing ran, so there is no push to attempt.
			return summary, nil
		}
		summary.Committed = commit.ExitCode == 0

		push, err := runStep(h, []string{"git", "push", "origin", "HEAD:" + branch}, stepOptions{
			Cwd: assemblyDir, Step: "git push", Timeout: gitTimeout,
			Resource: "git-push:" + assemblyDir, Grant: PushGrant,
			CaptureOutput: true,
		})
		if err != nil {
			return nil, err
		}
		if push == nil || push.ExitCode == 0 {
			return summary, nil
		}
		lastPushError = strings.TrimSpace(string(push.Stderr))
		if attempt < opts.Attempts {
			time.Sleep(opts.RetryDelay)
		}
	}

	return nil, errorf(
		"assembly push failed after %d attempt(s): %s",
		opts.Attempts, lastPushError,
	)
}
