package cli

import (
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/options"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/strictspec/go/strictspec"
)

func (c *cli) registerOptions() {
	group := c.app.Group("options",
		"Write this repository's entries for selfdoc's options in "+strictspec.OptionsDir+"/. Each lint is an option, "+lints.OptionsTool+":<lint name>, filed under "+lints.OptionsSubject+".toml: an error lint runs at error > warn > off, a warning lint at warn > off, and the check applies each entry's current value (off: not reported; warn: reported, never blocking; error: as registered)")

	group.Command("set",
		"Write one selfdoc entry into "+strictspec.OptionsDir+"/"+lints.OptionsSubject+".toml, or update the entry already there for the same option, keeping every other line of the document. Creates "+strictspec.OptionsDir+"/ and its "+layout.ManifestFileName+", naming "+layout.SharedOwner+" as the owner, when absent. strictspec validates the result before anything is written -- every document's shape, and every selfdoc entry with this one in place -- and each refusal is its catalogued diagnostic: a value the option does not declare, a current ranked above the ideal, an entry equal to the default, an empty reason. Refuses an id outside the selfdoc namespace and a directory whose manifest names another owner",
		c.cmdOptionsSet,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.PayloadSchema(payloadschemas.OptionsSet()),
		strictcli.WithArgs(
			strictcli.NewArg("id", "The option, as "+lints.OptionsTool+":<lint name> (e.g. '"+lints.OptionsTool+":low-numeric-data-density'); the lint table in the check guide names every lint", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("current", "The value the repository runs the lint at today: error, warn or off for an error lint, warn or off for a warning lint", strictcli.Required()),
			strictcli.StringFlag("ideal", "The value the repository should run the lint at: one the option declares, never ranked below current, or non-existent when the right value is one selfdoc does not offer yet", strictcli.Required()),
			strictcli.StringFlag("reason", "Why the repository deviates from the lint's default, in one sentence. Required and never empty", strictcli.Required()),
			strictcli.BoolFlag("auto-commit", "Commit the files the command wrote. Omitted, it commits; pass --no-auto-commit to leave the write uncommitted", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdOptionsSet(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	id := strictcli.Get[string](kwargs, "id")
	current := strictcli.Get[string](kwargs, "current")
	ideal := strictcli.Get[string](kwargs, "ideal")
	reason := strictcli.Get[string](kwargs, "reason")
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	handle := effects.FromContext(ctx)

	// A repository still on a previous layout is refused here as everywhere,
	// but selfdoc.json is not read: its own refusals (a retired key, say)
	// name this command as their fix.
	if err := layout.RefuseUnmigrated(c.dir()); err != nil {
		return c.fail(err)
	}
	result, err := options.Set(handle, c.dir(), id, current, ideal, reason)
	if err != nil {
		return c.fail(err)
	}
	committed := false
	if autoCommit && len(result.Written) > 0 {
		committed, _, err = gitcommit.AutoCommit(result.Written, "selfdoc options set "+id, c.dir(), handle)
		if err != nil {
			return c.fail(err)
		}
	}
	written := make([]any, 0, len(result.Written))
	for _, file := range result.Written {
		written = append(written, file)
	}
	ctx.Payload(map[string]any{
		"id":        result.ID,
		"file":      result.File,
		"action":    result.Action,
		"current":   result.Current,
		"ideal":     result.Ideal,
		"reason":    result.Reason,
		"class":     string(result.Class),
		"written":   written,
		"committed": committed,
	})
	if !ctx.JSON() {
		switch {
		case handle.Previewing():
			c.printf("Dry run: would write %s in %s: current %s, ideal %s (%s).\n",
				result.ID, result.File, result.Current, result.Ideal, result.Class)
		case result.Action == options.ActionUnchanged:
			c.printf("%s already holds %s: current %s, ideal %s (%s).\n",
				result.File, result.ID, result.Current, result.Ideal, result.Class)
		default:
			c.printf("%s %s in %s: current %s, ideal %s (%s).",
				capitalized(result.Action), result.ID, result.File, result.Current, result.Ideal, result.Class)
			if committed {
				c.printf(" Committed.")
			}
			c.println()
		}
	}
	return strictcli.Exit(0)
}

// capitalized upper-cases a word's first letter.
func capitalized(word string) string {
	if word == "" {
		return word
	}
	return string(word[0]-'a'+'A') + word[1:]
}
