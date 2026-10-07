package cli

import (
	"time"

	"github.com/stricttools/selfdoc/internal/confidential"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/strictcli/go/strictcli"
	"github.com/stricttools/strictspec/go/lifecycle"
	"github.com/stricttools/strictspec/go/lifecycle/index"
)

// refreshIndexOnMutatingCommands wraps the handler of every mutating command
// so that, before the command's own work, it brings the machine-local
// confidential-name index up to date for the repository the command runs in
// (see confidential.Refresh). Read-only commands are left as they are, so a
// read-only command never writes the index. It runs once, after every command
// is registered.
func (c *cli) refreshIndexOnMutatingCommands() {
	for _, cmd := range c.app.Commands() {
		c.wrapIndexRefresh(cmd)
	}
	var walk func(g *strictcli.Group)
	walk = func(g *strictcli.Group) {
		for _, cmd := range g.Commands {
			c.wrapIndexRefresh(cmd)
		}
		for _, sub := range g.Groups {
			walk(sub)
		}
	}
	for _, g := range c.app.Groups() {
		walk(g)
	}
}

func (c *cli) wrapIndexRefresh(cmd *strictcli.Command) {
	if cmd.Effect != strictcli.EffectMutating || cmd.Handler == nil {
		return
	}
	next := cmd.Handler
	cmd.Handler = func(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
		if err := c.refreshIndex(effects.FromContext(ctx)); err != nil {
			return c.fail(err)
		}
		return next(ctx, kwargs)
	}
}

// refreshIndex upserts or removes this repository's entry in the
// confidential-name index, writing through h.
func (c *cli) refreshIndex(h *effects.Handle) error {
	repo, err := confidential.Locate(h, c.dir())
	if err != nil {
		return err
	}
	indexPath, err := index.DefaultPath()
	if err != nil {
		return err
	}
	return confidential.Refresh(confidential.Writer{Handle: h}, indexPath, repo, time.Now())
}

// publicOutputAllowed refuses a public output outright when the repository's
// lifecycle-and-license record leaves no releasable that may publish it.
func (c *cli) publicOutputAllowed(h *effects.Handle, output lifecycle.Output) error {
	repo, err := confidential.Locate(h, c.dir())
	if err != nil {
		return err
	}
	return confidential.PublicOutputAllowed(repo.Record, output, time.Now())
}

// confidentialNameRefusal scans what a public output would publish against
// every name in the confidential-name index, in every repository, and returns
// the refusal naming each page, line, and term, or nil when nothing matched.
// what names the output in the refusal; scan reads the content.
func (c *cli) confidentialNameRefusal(what string, scan func(names []string) ([]confidential.Finding, error)) error {
	indexPath, err := index.DefaultPath()
	if err != nil {
		return err
	}
	names, err := confidential.Names(indexPath)
	if err != nil {
		return err
	}
	findings, err := scan(names)
	if err != nil {
		return err
	}
	return confidential.Refusal(what, findings, indexPath)
}
