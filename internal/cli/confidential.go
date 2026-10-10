package cli

import (
	"time"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/confidential"
	"github.com/stricttools/selfdoc/internal/effects"
	strictconfidential "github.com/stricttools/strictspec/go/confidential"
	"github.com/stricttools/strictspec/go/lifecycle"
)

// publicOutputAllowed refuses a public output outright when the repository's
// lifecycle-and-license record leaves no releasable that may publish it.
func (c *cli) publicOutputAllowed(h *effects.Handle, output lifecycle.Output) error {
	repo, err := confidential.Locate(h, c.dir())
	if err != nil {
		return err
	}
	return confidential.PublicOutputAllowed(repo.Record, output, time.Now())
}

// confidentialTermRefusal scans what a public output would publish against
// the confidential-term list, with the entries that apply to the repository
// holding dir and that repository's resolutions, and returns the refusal
// naming every unresolved hit, or nil. what names the output in the refusal;
// texts reads the content. The list's status (a missing list has no terms)
// is reported on the command's answer stream: it is information, not a
// refusal.
func (c *cli) confidentialTermRefusal(h *effects.Handle, dir, what string, texts func(*strictconfidential.Matcher) ([]confidential.Text, error)) error {
	loc, err := strictconfidential.DefaultLocation()
	if err != nil {
		return err
	}
	list, err := strictconfidential.Load(loc)
	if err != nil {
		return err
	}
	repo, err := confidential.Locate(h, dir)
	if err != nil {
		return err
	}
	names, err := strictconfidential.RepositoryNames(repo.Root, repo.Origin)
	if err != nil {
		return err
	}
	read, err := texts(list.For(names))
	if err != nil {
		return err
	}
	status, err := confidential.Check(list, repo, what, read)
	if err != nil {
		return err
	}
	c.printf("Confidential terms: %s\n", status)
	return nil
}

// assemblyScreen is the screen the assembly's publishing commands hold their
// output to, as every other publishing command is held: a source checkout
// whose record lets no releasable publish public documentation is refused
// outright, and every page a built tree holds is scanned against the
// confidential-term list, with the resolutions of the repository holding it.
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
			return c.confidentialTermRefusal(h, dir, what, func(*strictconfidential.Matcher) ([]confidential.Text, error) {
				return confidential.DirTexts(dir)
			})
		},
	}
}

// refRefusal scans every tracked text file of the git tree at ref against the
// confidential-term list, for a dispatch whose output the assembly builds
// from that ref, and returns the refusal naming every unresolved hit, or nil.
func (c *cli) refRefusal(h *effects.Handle, ref string) error {
	return c.confidentialTermRefusal(h, c.dir(), "assembly push of "+ref, func(m *strictconfidential.Matcher) ([]confidential.Text, error) {
		repo, err := confidential.Locate(h, c.dir())
		if err != nil {
			return nil, err
		}
		return confidential.RefTexts(h, repo.Root, ref, m)
	})
}
