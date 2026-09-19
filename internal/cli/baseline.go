package cli

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/check"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerBaseline() {
	group := c.app.Group("baseline", "Manage the content and description hash baselines that drive staleness (STALE001) and source-drift (DRIFT001) detection during selfdoc check")

	group.Command("accept",
		"Accept a reviewed staleness or drift dead-end by advancing a page's stored content and description hash baseline to its current values. Use this only after a human has confirmed the page's content changed but its existing frontmatter description was reviewed and is still accurate. Each named page must currently be reporting a STALE001 or DRIFT001 error; accepting clears that error so selfdoc check passes without rewriting an already-correct description.",
		c.cmdBaselineAccept,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("page", "Page identifier(s) to accept, named exactly as shown in 'selfdoc check' output (e.g. 'en/index.md'). Each page must currently report a STALE001 or DRIFT001 error; pages are named explicitly with no glob or --all shortcut so acceptance stays a deliberate per-page action.",
				strictcli.Variadic(), strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.BoolFlag("auto-commit", "Automatically commit the updated content hash tracking file to git after accepting the named pages. Omitted, it commits; pass --no-auto-commit to leave it uncommitted", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdBaselineAccept(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	pages := stringList(kwargs, "page")
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	// The same config the check runs on: a page of the assembly's home
	// project carries site-level markers, and a baseline that resolved none of
	// them would refuse every such page as carrying an unknown directive
	// rather than accepting the one the reviewer named.
	accepted, err := check.AcceptBaselines(
		pages, c.dir(), c.withSiteDirectives(cfg, handle), handle,
	)
	if err != nil {
		return c.fail(err)
	}

	c.printf("Accepted new baseline for %d page(s):\n", len(accepted))
	names := make([]string, 0, len(accepted))
	for _, entry := range accepted {
		c.printf("  %s (cleared %s)\n", entry.Page, entry.Code)
		names = append(names, entry.Page)
	}

	if autoCommit {
		if _, _, err := gitcommit.AutoCommit(
			[]string{hashStorePath},
			"selfdoc baseline accept: "+strings.Join(names, ", "),
			c.dir(), handle,
		); err != nil {
			return c.fail(err)
		}
	}
	return strictcli.Exit(0)
}
