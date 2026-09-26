package cli

import (
	"path/filepath"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/selfdoc/internal/spellcorpus"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerSpellCorpus() {
	c.app.Command("spell-corpus",
		"Spell-check the docs of every selfdoc project beside this one, using the same engine 'selfdoc check' runs (SPELL001), each project against its own vocabulary: selfdoc's built-in baseline and the project's stricttools/vocabulary/terms.toml. Read-only over every project it visits",
		c.cmdSpellCorpus,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.SpellCorpus()),
		strictcli.WithFlags(
			strictcli.StringFlag("root", "Directory whose immediate subdirectories are searched for selfdoc.json. Omitted, the parent of the current directory is searched, i.e. this project's siblings", strictcli.Optional()),
			strictcli.BoolFlag("detail", "List each project's unknown words with a first location and any suggestion, after the summary table", strictcli.Default(true)),
		),
	)
}

func (c *cli) cmdSpellCorpus(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	root := optString(kwargs, "root")
	detail := strictcli.Get[bool](kwargs, "detail")
	handle := effects.FromContext(ctx)

	if root == "" {
		abs, err := filepath.Abs(c.dir())
		if err != nil {
			return c.fail(err)
		}
		root = filepath.Dir(abs)
	}

	document, exitCode, err := spellcorpus.RunSpellCorpus(root, handle)
	if err != nil {
		return c.fail(err)
	}

	ctx.Payload(document.Payload())
	if !ctx.JSON() {
		c.println(spellcorpus.RenderCorpusText(document, detail))
	}
	return strictcli.Exit(exitCode)
}
