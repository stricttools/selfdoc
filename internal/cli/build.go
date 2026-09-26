package cli

import (
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/blog/unified"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/smm-h/strictcli/go/strictcli"
)

// hashStorePath is the content-hash store every build and check auto-commits.
var hashStorePath = layout.HashesRel

// hashStoreMessage is the commit message that store is committed under.
const hashStoreMessage = "selfdoc: update content hashes"

func (c *cli) registerBuild() {
	c.app.Command("build", "Build the documentation site from templates and source code",
		c.cmdBuild,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.BoolFlag("auto-commit", "Automatically commit updated content hash tracking files to git after the build. Omitted, it commits; pass --no-auto-commit to leave them uncommitted", strictcli.Optional()),
			strictcli.StringFlag("locale", "Build only the specified locale instead of all (e.g., 'en')", strictcli.Optional()),
			strictcli.StringFlag("version", "Build only the specified version instead of all (e.g., '1.0.0')", strictcli.Optional()),
			strictcli.BoolFlag("drafts", "Include posts marked as draft in the build output alongside published posts. Omitted, drafts are left out; pass --drafts to include them", strictcli.Optional()),
			strictcli.StringFlag("target", "Build target: 'site' for the whole documentation site, 'posts' for a posts-only build, 'unified' for a unified multi-project site, 'home' for the assembly's home project (the one served at the site root, whose pages carry site-level directives). Omitted, 'site' is built", strictcli.Optional()),
			strictcli.StringFlag("site-manifests", "Path to the assembly's manifests directory. Required by --target home: the home project's front page renders the curated listing with each project's live version and the recent posts across the whole site, and only the assembly's manifests carry those", strictcli.Optional()),
			strictcli.StringFlag("theme", "Theme name that overrides the one selfdoc.json declares, for this build only (e.g. 'tinymoon'). Omitted, the config decides. Nothing is written back to selfdoc.json -- this exists so the same pages can be built under a different theme and looked at, without editing every project's config to do it", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdBuild(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	drafts := absentMeans(kwargs, "drafts", false)
	target := absentMeans(kwargs, "target", "site")
	locale := optString(kwargs, "locale")
	version := optString(kwargs, "version")
	theme := optString(kwargs, "theme")
	siteManifests := optString(kwargs, "site_manifests")
	handle := effects.FromContext(ctx)

	// A present-but-invalid selfdoc.json is a user error like any other: it
	// prints the message and exits 1, rather than ending on a traceback.
	cfg, outcome, ok := c.loadConfig()
	if !ok {
		return outcome
	}

	switch target {
	case "home":
		written, err := sitedirectives.BuildHomeProject(c.dir(), siteManifests, theme, drafts, handle)
		if err != nil {
			return c.fail(err)
		}
		c.printf("Built %d file(s) (home)\n", len(written))
		return strictcli.Exit(0)
	case "unified":
		written, err := unified.BuildUnified(c.dir(), cfg, theme, drafts, handle)
		if err != nil {
			return c.fail(err)
		}
		c.printf("Built %d file(s) (unified)\n", len(written))
		return strictcli.Exit(0)
	case "posts":
		written, err := build.Build(build.Options{
			DirPath:       c.dir(),
			Config:        cfg,
			IncludeDrafts: drafts,
			Target:        "posts",
			Theme:         theme,
			Stdout:        c.out(),
		}, handle)
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
		c.printf("Built %d file(s) to %s\n", len(written), outputDirOf(cfg))
		return strictcli.Exit(0)
	case "site":
		return c.buildSite(kwargs, cfg, handle, locale, version, theme, drafts, autoCommit)
	default:
		return c.failf("Error: unknown build target '%s'. "+
			"Valid targets: 'site', 'posts', 'unified', 'home'.", target)
	}
}

// buildSite is the whole-site build plus the lint pass that follows it.
func (c *cli) buildSite(
	kwargs map[string]any,
	cfg config.Config,
	handle *effects.Handle,
	locale, version, theme string,
	drafts, autoCommit bool,
) strictcli.Outcome {
	written, err := build.Build(build.Options{
		DirPath:       c.dir(),
		Config:        cfg,
		VersionFilter: version,
		LocaleFilter:  locale,
		IncludeDrafts: drafts,
		Theme:         theme,
		Stdout:        c.out(),
	}, handle)
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

	c.printf("Built %d file(s) to %s\n", len(written), outputDirOf(cfg))

	result, err := check.CheckDocs(c.dir(), cfg, false, version, "", handle)
	if err != nil {
		return c.fail(err)
	}
	kept := check.FilterLints(result.Lints, ignoreCodesFrom(nil, cfg))

	warnCount := 0
	errorCount := 0
	for _, lint := range kept {
		c.println(formatLint(lint))
		switch lint.Severity() {
		case "warning":
			warnCount++
		case "error":
			errorCount++
		}
	}
	if warnCount > 0 {
		c.printf("%d SEO warning(s) found.\n", warnCount)
	}
	if errorCount > 0 {
		c.printf("%d error(s) found.\n", errorCount)
	}

	// Reduced verdict: the build already reported directive failures inline
	// and measures no coverage, so only the lints reach the shared rules.
	if lints.CheckExitCode(kept, nil, nil, nil) != 0 {
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}

// outputDirOf is the output directory a build reports writing to, exactly as
// the config spells it.
func outputDirOf(cfg config.Config) string {
	return config.OutputRel(cfg)
}

// formatLint renders one diagnostic in the compiler-style form both the build
// and the posts check print.
func formatLint(lint lints.LintResult) string {
	linePart := ""
	if line := lint.Line(); line != nil {
		linePart = ":" + strconv.Itoa(*line)
	}
	var b strings.Builder
	b.WriteString(lint.Severity())
	b.WriteString(": [")
	b.WriteString(lint.Code())
	b.WriteString("] ")
	b.WriteString(lint.File())
	b.WriteString(linePart)
	b.WriteString(" - ")
	b.WriteString(lint.Message())
	return b.String()
}
