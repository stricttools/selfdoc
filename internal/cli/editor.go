package cli

import (
	"github.com/stricttools/selfdoc/internal/blog/editor"
	"github.com/stricttools/selfdoc/internal/blog/editor/assets"
	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/blog/serving"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerEditor(parent *strictcli.Group) {
	group := parent.Group("editor", "Run and inspect the local authoring app for blog posts")

	group.Command("list-repos",
		"List every repository the editor registry declares, with its kind and where it points. Reads the hand-written registry TOML, validates every entry in full, and prints one line per entry -- a local entry's working tree, or a remote entry's repository, ref and whether it declares that rendering runs against a checkout.",
		c.cmdEditorListRepos,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithFlags(
			strictcli.StringFlag("registry", "Path to the editor registry TOML. Omitted, the machine-local registry at ~/Projects/ark/selfdoc-registry.toml is read.", strictcli.Optional()),
		),
	)

	group.Command("serve",
		"Run the local authoring app: a browser UI over the registry's repositories, with the tinymoon editor component on the left and a live preview on the right. The preview is the publish renderer over the unsaved buffer, so what you approve is byte-for-byte what publishing produces, and rendering a preview writes nothing. Saving writes the buffer into the repository's working tree. Binds 127.0.0.1 only.",
		c.cmdEditorServe,
		strictcli.WithEffect(strictcli.EffectMutating),
		// Not consequential: nothing here reaches the world, and the only
		// writes are the ones the author asks for by pressing save. A
		// confirmation prompt at launch would be answering for edits that
		// have not been made yet.
		//
		// It also cannot honestly preview, and says so at parse time. A
		// refusal is the only correct answer rather than a recorded run,
		// because the saves happen on request threads that carry none of the
		// dispatch context -- they would execute for real under a preview
		// that claimed to be recording them.
		strictcli.WithDryRunUnsupported(
			"'editor serve' is an interactive server: its writes are the saves "+
				"you make at the keyboard while it runs, so at launch there is "+
				"nothing to record. Worse, those saves happen on request threads "+
				"that carry no preview context, so they would execute for real. Run "+
				"it without --dry-run, and preview a save by not pressing save."),
		strictcli.WithFlags(
			strictcli.IntFlag("port", "Port to bind on 127.0.0.1. Required and has no default: the editor writes working trees and answers without authentication, so which port it occupies is a decision the caller states rather than inherits.", strictcli.Required()),
			strictcli.StringFlag("registry", "Path to the editor registry TOML. Omitted, the machine-local registry at ~/Projects/ark/selfdoc-registry.toml is read.", strictcli.Optional()),
			strictcli.StringFlag("tinymoon-assets", "Path to a tinymoon checkout's 'assets' directory. Omitted, the installed tinymoon package is used. The editor tier (js/editor.js, js/completion.js, css/editor.css) is newer than the released package, so a checkout is currently the only complete source.", strictcli.Optional()),
		),
	)
}

func (c *cli) cmdEditorListRepos(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	parsed, err := registry.Load(optString(kwargs, "registry"))
	if err != nil {
		return c.fail(err)
	}
	for _, line := range parsed.RenderList() {
		c.println(line)
	}
	return strictcli.Exit(0)
}

func (c *cli) cmdEditorServe(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	handle := effects.FromContext(ctx)

	parsed, err := registry.Load(optString(kwargs, "registry"))
	if err != nil {
		return c.fail(err)
	}

	tinymoon, err := assets.ResolveTinymoon(optString(kwargs, "tinymoon_assets"))
	if err != nil {
		return c.fail(err)
	}

	state, err := editor.NewState(editor.StateOptions{
		Registry:  parsed,
		Tinymoon:  tinymoon.FS,
		Publisher: &publisher{options: c.opts},
		Handle:    handle,
	})
	if err != nil {
		return c.fail(err)
	}

	c.printf("Registry: %s (%d repository(ies))\n", parsed.Path, parsed.Len())
	c.printf("tinymoon assets: %s\n", tinymoon.Source)

	code, err := editor.Serve(state, strictcli.Get[int](kwargs, "port"), func(port int) {
		c.printf("Editor: http://%s:%d/  (Ctrl-C to stop)\n", serving.Host, port)
	})
	if err != nil {
		return c.fail(err)
	}
	return strictcli.Exit(code)
}
