package cli

import (
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/selfdoc/internal/quality"
	"github.com/stricttools/strictcli/go/strictcli"
)

func (c *cli) registerQuality() {
	c.app.Command("quality", "Show documentation quality tier and metrics for the current project",
		c.handler((*cli).cmdQuality),
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.Quality()),
	)
}

func (c *cli) cmdQuality(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	// The score is read off the project's own files, so the config is
	// loaded first for its refusals -- a repository still laid out the way
	// selfdoc used to lay one out is refused here as everywhere else. A
	// directory with no selfdoc.json is still scored: that is tier 1.
	if _, outcome, ok := c.loadConfig(); !ok {
		return outcome
	}

	result, err := quality.Run(c.dir(), handle)
	if err != nil {
		return c.fail(err)
	}

	ctx.Payload(result.Payload())
	if !ctx.JSON() {
		c.println(quality.FormatSingleText(result))
	}
	return strictcli.Exit(0)
}
