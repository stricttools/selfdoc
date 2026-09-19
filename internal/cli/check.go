package cli

import (
	"fmt"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/blog/unifiedcheck"
	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerCheck() {
	c.app.Command("check",
		"Check documentation coverage, directive resolution, and lint rules -- and write: it "+
			"advances the content and description baseline of every page it does not report "+
			"stale or drifted in "+layout.HashesRel+" and commits the store, which is why "+
			"check is a mutating command and not a read-only one",
		c.cmdCheck,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.PayloadSchema(payloadschemas.Check()),
		strictcli.WithFlags(
			strictcli.StringFlag("ignore", "Comma-separated SEO codes to suppress (e.g., SEO007,SEO008)", strictcli.Optional()),
			strictcli.BoolFlag("auto-commit", "Automatically commit "+layout.HashesRel+", the staleness baseline store this run advanced, after checking. Omitted, it commits; pass --no-auto-commit to leave the store written but uncommitted -- the store is written either way", strictcli.Optional()),
			strictcli.StringFlag("version-override", "Project version that version-bearing generated content is expected to embed (VER004), instead of the version currently recorded in the project manifest (VERSION, pyproject.toml or package.json). Pass the same value given to 'selfdoc gen --version-override' so the check runs correctly in the release window between generation and the version bump", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdCheck(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	ignore := optString(kwargs, "ignore")
	versionOverride := optString(kwargs, "version_override")
	handle := effects.FromContext(ctx)

	// Validated before any work is done: a mistyped code suppresses nothing,
	// a check run that silently ignored the typo would report lints the
	// caller believes it silenced, and an error-severity code is not
	// suppressible at all.
	flagIgnoreCodes, err := lints.ParseIgnoreCodes(ignore, "--ignore")
	if err != nil {
		return c.fail(err)
	}

	cfg, outcome, ok := c.loadConfig()
	if !ok {
		return outcome
	}

	// No dryRun is threaded into the check: under --dry-run the hash write is
	// RECORDED by the effects chokepoint rather than executed, which both
	// preserves the old "report staleness without writing" behavior and makes
	// the preview honest about the write a real run would perform.
	//
	// The project's kind decides which check runs. There is no refusal here
	// any more: one binary answers for a unified docs-site and for an
	// ordinary project alike, and a project's posts are checked either way.
	var result *check.CheckResult
	if cfg != nil && cfg["unified"] != nil {
		result, err = unifiedcheck.CheckUnified(cfg, c.dir(), false, handle)
	} else {
		result, err = check.CheckDocs(
			c.dir(), c.withSiteDirectives(cfg, handle), false, "",
			versionOverride, handle,
		)
	}
	if err != nil {
		return c.fail(err)
	}

	if autoCommit {
		if _, _, err := gitcommit.AutoCommit(
			[]string{hashStorePath}, hashStoreMessage, c.dir(), handle,
		); err != nil {
			return c.fail(err)
		}
	}

	// The combined suppression set: the flag's codes and the project's own
	// lint_ignore, both already validated against the registry.
	result.Lints = check.FilterLints(result.Lints, ignoreCodesFrom(flagIgnoreCodes, cfg))

	belowThreshold := check.CoverageBelowThreshold(result, cfg)
	exitCode := check.CheckResultExitCode(result, cfg)

	// The payload is supplied in both modes -- the framework decides what to
	// do with it -- and the human report is written only outside machine
	// mode, where stdout carries the envelope and nothing else.
	ctx.Payload(check.SerializeCheckResult(result, exitCode))

	if !ctx.JSON() {
		check.PrintResults(c.out(), result, c.color())

		if belowThreshold {
			coverage := result.Coverage
			threshold := lints.DefaultCoverageThreshold
			if cfg != nil {
				if declared, ok := cfg["coverage_threshold"].(float64); ok {
					threshold = declared
				}
			}
			percent := 0.0
			if coverage.TotalPublic() > 0 {
				percent = float64(coverage.Documented()) * 100 / float64(coverage.TotalPublic())
			}
			c.printf("Coverage: %d/%d symbols documented (%.0f%%). Threshold is %.0f%%.\n",
				coverage.Documented(), coverage.TotalPublic(), percent, threshold*100)
		}
	}

	if exitCode != 0 {
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}

// withSiteDirectives returns cfg with the site-level directives registered
// against the assembly this project belongs to.
//
// A page of the assembly's home project carries markers that render from the
// whole assembled site -- the curated listing with every project's live
// version, the recent posts across every project -- and no project's own
// repository holds that state. The build that publishes such a page is handed
// the assembly's manifests; a check has to go and read them, which is what the
// registration below does the first time a page actually carries one of the
// markers. A project whose pages carry none never reaches the assembly at all.
//
// Every refusal is the directive's, naming it: a project with no 'assembly'
// block to read from, a project the assembly's roster does not name as its
// home, and a read that failed.
func (c *cli) withSiteDirectives(cfg config.Config, handle *effects.Handle) config.Config {
	resolve := func() (*sitedirectives.SiteContext, error) {
		repo := configString(cfg, "assembly", "repo")
		if repo == "" {
			return nil, fmt.Errorf(
				"it renders from an assembled site's manifests, and this " +
					"project's selfdoc.json declares no 'assembly' block, so " +
					"there is no assembly to read them from. Declare " +
					`"assembly": {"repo": "<owner>/<repo>"} if this project ` +
					"is the assembly's home project; otherwise the marker " +
					"belongs on that project's pages, not on these",
			)
		}
		slug := configString(cfg, "topology", "slug")
		roster, err := assembly.LoadRemoteRoster(handle, repo)
		if err != nil {
			return nil, err
		}
		if slug == "" || slug != roster.Home {
			return nil, fmt.Errorf(
				"only the assembly's home project carries it, and %s names "+
					"%s as its home project while this one is %s",
				repo, util.PythonRepr(roster.Home), util.PythonRepr(slug),
			)
		}
		manifests, err := assembly.FetchRemoteManifests(handle, repo, "")
		if err != nil {
			return nil, err
		}
		context, err := sitedirectives.HomeContext(c.dir(), cfg, manifests)
		if err != nil {
			return nil, err
		}
		return &context, nil
	}
	return sitedirectives.RegisterSiteDirectives(
		cfg, sitedirectives.LazyDirectives(resolve),
	)
}
