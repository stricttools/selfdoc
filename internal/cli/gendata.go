package cli

import (
	"path/filepath"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gendata"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerGenData() {
	c.app.Command("gen-data", "Generate data files by running sandboxed scripts via bwrap",
		c.cmdGenData,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.BoolFlag("auto-commit", "Automatically commit the generated data output files to git after script execution. Omitted, it commits; pass --no-auto-commit to leave them uncommitted", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdGenData(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	handle := effects.FromContext(ctx)

	cfg, outcome, ok := c.requireConfig()
	if !ok {
		return outcome
	}

	generated, err := gendata.GenerateData(cfg, c.dir(), handle)
	if err != nil {
		return c.fail(err)
	}

	if len(generated) == 0 {
		c.println("No gen-data scripts configured.")
		return strictcli.Exit(0)
	}

	c.printf("Generated %d data file(s):\n", len(generated))
	for _, path := range generated {
		c.printf("  %s\n", path)
	}
	if autoCommit {
		written := make([]string, 0, len(generated))
		for _, path := range generated {
			rel, err := filepath.Rel(c.dir(), path)
			if err != nil {
				rel = path
			}
			written = append(written, rel)
		}
		if _, _, err := gitcommit.AutoCommit(
			written, "selfdoc gen-data: update generated data", c.dir(), handle,
		); err != nil {
			return c.fail(err)
		}
	}
	return strictcli.Exit(0)
}
