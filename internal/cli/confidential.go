package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
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

// assemblyScreen is the confidential-name screen the assembly's publishing
// commands hold their output to, as every other publishing command is held:
// a source checkout whose record lets no releasable publish public
// documentation is refused outright, and every page a built tree holds is
// scanned against the confidential-name index.
func (c *cli) assemblyScreen(h *effects.Handle) assembly.Screen {
	return assembly.Screen{
		Allow: func(sourceDir string) error {
			repo, err := confidential.Locate(h, sourceDir)
			if err != nil {
				return err
			}
			return confidential.PublicOutputAllowed(repo.Record, lifecycle.PublicDocs, time.Now())
		},
		Scan: func(what, dir string) error {
			return c.confidentialNameRefusal(what, func(names []string) ([]confidential.Finding, error) {
				return confidential.ScanDir(names, dir)
			})
		},
	}
}

// refRefusal scans every tracked text file of the git tree at ref against the
// confidential-name index, for a dispatch whose output the assembly builds
// from that ref, and returns the refusal naming each file, line, and term, or
// nil when nothing matched.
func (c *cli) refRefusal(h *effects.Handle, ref string) error {
	repo, err := confidential.Locate(h, c.dir())
	if err != nil {
		return err
	}
	indexPath, err := index.DefaultPath()
	if err != nil {
		return err
	}
	names, err := confidential.Names(indexPath)
	if err != nil {
		return err
	}
	findings, err := confidential.ScanRef(h, repo.Root, ref, names)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		return nil
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, fmt.Sprintf("%s, line %d, column %d: %s", f.Page, f.Line, f.Column, f.Term))
	}
	return fmt.Errorf("refusing the assembly push: the assembly builds the documentation from %s, whose tracked files name terms the confidential-name index protects:\n  %s\n"+
		"Remove each term from the files it came from, commit, and push the ref that carries the removal (for a versioned project, a release's tag). "+
		"The index is %s; commands in the confidential repositories keep it current", ref, strings.Join(lines, "\n  "), indexPath)
}
