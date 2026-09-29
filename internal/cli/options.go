package cli

import (
	"fmt"

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
		"Print selfdoc's options registry, and write this repository's entries for selfdoc's options in "+strictspec.OptionsDir+"/. Each lint is an option, "+lints.OptionsTool+":<lint name>, filed under "+lints.OptionsSubject+".toml: an error lint runs at error > warn > off, a warning lint at warn > off, and the check applies each entry's current value (off: not reported; warn: reported, never blocking; error: as registered)")

	group.Command("registry",
		"Print selfdoc's options registry: one option per lint, "+lints.OptionsTool+":<lint name>, with the subject it is filed under, the values it ranks strongest first, its default, its scope, and what the lint checks. The TOML printed is the registry document selfdoc ships, in the shape of strictspec's built-in options-registry schema, so a tool that reads every tool's options learns selfdoc's rankings from it; with --json the same declarations are the payload. Needs no selfdoc project",
		c.cmdOptionsRegistry,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.OptionsRegistry()),
	)

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

// optionsRegistryPayload is selfdoc's options registry as the payload of
// `selfdoc options registry` carries it: the declarations strictspec reads out
// of the shipped document, in the document's order.
func optionsRegistryPayload() (map[string]any, error) {
	document, diags := strictspec.ReadOptionsRegistry(lints.OptionsRegistryDocument())
	if len(diags) > 0 {
		return nil, fmt.Errorf("selfdoc's shipped options registry is not a valid options registry: %v", diags)
	}
	declared := make([]any, 0, len(document.Options))
	for _, option := range document.Options {
		requires := make([]any, 0, len(option.Requires))
		for _, name := range option.Requires {
			requires = append(requires, name)
		}
		declared = append(declared, map[string]any{
			"name":        option.Name,
			"subject":     option.Subject,
			"values":      option.Values,
			"default":     option.Default,
			"scope":       option.Scope,
			"requires":    requires,
			"description": option.Description,
		})
	}
	return map[string]any{
		"format_version": lints.OptionsRegistryFormatVersion,
		"option":         declared,
	}, nil
}

// cmdOptionsRegistry prints the shipped options registry document as it is
// embedded, byte for byte, after strictspec has validated it.
func (c *cli) cmdOptionsRegistry(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	// The registry is validated, shape and ranking rules alike, before it is
	// published: a malformed one is refused rather than printed.
	if _, err := lints.BuildOptionsRegistry(lints.OptionsRegistryDocument()); err != nil {
		return c.fail(err)
	}
	payload, err := optionsRegistryPayload()
	if err != nil {
		return c.fail(err)
	}
	ctx.Payload(payload)
	if !ctx.JSON() {
		c.printf("%s", lints.OptionsRegistryDocument())
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
