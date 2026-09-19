package cli

import (
	"encoding/json"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerLayout() {
	group := c.app.Group("layout",
		"Inspect and check the per-repository directories selfdoc owns under "+layout.Root+"/")

	group.Command("dump",
		"Print selfdoc's layout declaration: every directory it claims, whether the directory is handwritten or generated, whether the repository commits it, the "+layout.ManifestFileName+" that grants it and what that file must hold, and the paths it replaced",
		c.cmdLayoutDump,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.LayoutDump()),
	)

	group.Command("validate",
		"Check this repository's "+layout.Root+"/ directory: every directory carries a "+layout.ManifestFileName+" naming a tool this machine has, every directory selfdoc claims names selfdoc, every directory selfdoc owns holds only what its side allows, nothing inside starts with a dot except the derived ignore file, and that ignore file is what selfdoc's declaration renders",
		c.cmdLayoutValidate,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.LayoutValidate()),
	)
}

// layoutDeclaration is the document `selfdoc layout dump` prints, and the
// payload it publishes: the same bytes either way, so a consumer reading the
// human output reads the contract.
func layoutDeclaration() map[string]any {
	directories := make([]any, 0, len(layout.Declared()))
	for _, dir := range layout.Declared() {
		deprecated := make([]any, 0, len(dir.DeprecatedNames))
		for _, name := range dir.DeprecatedNames {
			deprecated = append(deprecated, name)
		}
		directories = append(directories, map[string]any{
			"name":             dir.Name,
			"path":             layout.Root + "/" + dir.Name,
			"side":             string(dir.Side),
			"commitment":       string(dir.Commitment),
			"description":      dir.Description,
			"deprecated_names": deprecated,
			"manifest_path":    layout.DirectoryManifestRel(dir.Name),
			"manifest_content": layout.DirectoryManifestContent(layout.Owner),
		})
	}
	return map[string]any{
		"tool":          layout.Owner,
		"root":          layout.Root,
		"manifest_file": layout.ManifestFileName,
		"ignore_file":   layout.Root + "/" + layout.IgnoreFileName,
		"directories":   directories,
	}
}

func (c *cli) cmdLayoutDump(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	declaration := layoutDeclaration()
	ctx.Payload(declaration)
	if !ctx.JSON() {
		rendered, err := json.MarshalIndent(declaration, "", "  ")
		if err != nil {
			return c.fail(err)
		}
		c.println(string(rendered))
	}
	return strictcli.Exit(0)
}

func (c *cli) cmdLayoutValidate(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	problems, err := layout.Validate(c.dir())
	if err != nil {
		return c.fail(err)
	}
	reported := make([]any, 0, len(problems))
	for _, problem := range problems {
		reported = append(reported, map[string]any{
			"check":   problem.Check,
			"message": problem.Message,
		})
	}
	ctx.Payload(map[string]any{
		"root":     layout.Root,
		"ok":       len(problems) == 0,
		"problems": reported,
	})
	if !ctx.JSON() {
		if len(problems) == 0 {
			c.printf("%s/ is laid out as selfdoc declares it.\n", layout.Root)
		} else {
			for _, problem := range problems {
				c.eprintf("%s\n", problem.Error())
			}
			c.eprintf("%d layout problem(s) found.\n", len(problems))
		}
	}
	if len(problems) > 0 {
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}
