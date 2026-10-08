package cli

import (
	"fmt"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/blog/unifiedcheck"
	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/strictcli/go/strictcli"
	"github.com/stricttools/strictspec/go/strictspec"
)

func (c *cli) registerCheck() {
	c.app.Command("check",
		"Check documentation coverage, directive resolution, and lint rules, each lint at the value its option in "+strictspec.OptionsDir+"/docs.toml sets (off: not reported; warn: reported, never blocking; error: as registered). "+
			"A read-only verdict: it writes nothing and commits nothing. The staleness and drift baseline it compares against, "+
			layout.HashesRel+", is written and committed by the commands that generate content, selfdoc gen among them, and by selfdoc baseline accept, "+
			"and an archived version it checks is read from a temporary extraction, not from selfdoc's version cache",
		c.handler((*cli).cmdCheck),
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.Check()),
		strictcli.WithFlags(
			strictcli.StringFlag("version-override", "Project version that version-bearing generated content is expected to embed (version-mismatch-in-generated-root-file), instead of the version currently recorded in the project manifest (VERSION, pyproject.toml or package.json). Pass the same value given to 'selfdoc gen --version-override' so the check runs correctly in the release window between generation and the version bump", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdCheck(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	versionOverride := optString(kwargs, "version_override")
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.loadConfig()
	if !ok {
		return outcome
	}
	// The repository's lint options are read before any work is done, so an
	// entry strictspec refuses stops the run instead of being half applied.
	settings, err := lints.LoadSettings(c.dir())
	if err != nil {
		return c.fail(err)
	}

	// The check is a read-only verdict, so it is told to write nothing: the
	// hash store is compared against and never advanced, and an archived
	// version is extracted to a temporary directory instead of the cache.
	//
	// The project's configuration decides which check runs. There is no
	// refusal here: one binary answers for a unified docs-site and for an
	// ordinary project alike, and a project's posts are checked either way.
	var result *check.CheckResult
	if cfg != nil && cfg["unified"] != nil {
		result, err = unifiedcheck.CheckUnified(cfg, c.dir(), true, handle)
	} else {
		result, err = check.CheckDocs(
			c.dir(), c.withSiteDirectives(cfg, handle), true, "",
			versionOverride, handle,
		)
	}
	if err != nil {
		return c.fail(err)
	}

	// Each lint at the value the repository's options set for it.
	result.Lints = settings.Apply(result.Lints)

	belowThreshold := check.CoverageBelowThreshold(result, cfg)
	exitCode := check.CheckResultExitCode(result, cfg)

	// The payload is supplied in both modes -- the framework decides what to
	// do with it -- and the human report is written only outside machine
	// mode, where stdout carries the envelope and nothing else.
	ctx.Payload(check.SerializeCheckResult(result, exitCode))

	if !ctx.JSON() {
		check.PrintResults(c.out(), result)

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
