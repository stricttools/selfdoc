package cli

import (
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/selfdoc/internal/quality"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerQuality() {
	c.app.Command("quality", "Show documentation quality tier and metrics for the current project",
		c.cmdQuality,
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
		// A missing dirstat is the one condition with its own two-line
		// message, which goes to stderr as it stands rather than through the
		// "Error: " refusal every other user error takes.
		var missing *quality.DirstatMissingError
		if asError(err, &missing) {
			c.eprintf("%s\n", missing.Error())
			return strictcli.Exit(1)
		}
		return c.fail(err)
	}

	ctx.Payload(result.Payload())
	if !ctx.JSON() {
		c.println(quality.FormatSingleText(result))
	}
	return strictcli.Exit(0)
}
