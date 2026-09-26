package cli

import (
	"encoding/json"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/migrate"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/smm-h/strictcli/go/strictcli"
)

func (c *cli) registerLayout() {
	group := c.app.Group("layout",
		"Inspect, check and migrate the per-repository directories selfdoc owns under "+layout.Root+"/")

	group.Command("dump",
		"Print selfdoc's layout declaration: every directory it claims, whether the directory is handwritten or generated, whether the repository commits it, the "+layout.ManifestFileName+" that grants it and what that file must hold, and the paths it replaced",
		c.cmdLayoutDump,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.LayoutDump()),
	)

	group.Command("validate",
		"Check this repository's "+layout.Root+"/ directory: every directory carries a "+layout.ManifestFileName+" naming a tool this machine has, every directory selfdoc claims names selfdoc, a directory selfdoc owns starts with a dot exactly when it is generated, every directory selfdoc owns holds only what its side allows, nothing inside selfdoc's committed directories starts with a dot, and the derived ignore file is what selfdoc's declaration renders",
		c.cmdLayoutValidate,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.LayoutValidate()),
	)

	group.Command("migrate",
		"Move this repository off the layout before this one: every directory under "+layout.PreviousRoot+"/ whose "+layout.ManifestFileName+" names selfdoc moves under "+layout.Root+"/, a generated one behind a dot ("+migrationExample()+"). It creates "+layout.Root+"/ (the manifests naming selfdoc are the grant), writes the derived ignore file for the new names, removes selfdoc's block from "+layout.PreviousRoot+"/"+layout.IgnoreFileName+" (the file and "+layout.PreviousRoot+"/ go when nothing else is left), rewrites every selfdoc.json value naming a moved path and every generated root file's header, writes "+layout.TermsRel+" empty when the project has none, and commits. Another tool's directories stay where they are. Refuses a repository already migrated, part-way through a move, or never on the previous layout; --dry-run prints the plan and changes nothing",
		c.cmdLayoutMigrate,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.PayloadSchema(payloadschemas.LayoutMigrate()),
		strictcli.WithFlags(
			strictcli.BoolFlag("auto-commit", "Commit the move and every file it wrote or rewrote. Omitted, it commits; pass --no-auto-commit to leave the move uncommitted", strictcli.Optional()),
		),
	)
}

// migrationExample names every claimed directory's move, the way the migrate
// help states it: derived from the declaration, so the help cannot drift from
// the names the move writes.
func migrationExample() string {
	moves := make([]string, 0, len(layout.Declared()))
	for _, dir := range layout.Declared() {
		moves = append(moves, layout.PreviousRoot+"/"+dir.Name+" -> "+dir.Rel())
	}
	return strings.Join(moves, ", ")
}

func (c *cli) cmdLayoutMigrate(ctx *strictcli.Context, kwargs map[string]any) strictcli.Outcome {
	autoCommit := absentMeans(kwargs, "auto_commit", true)
	handle := effects.FromContext(ctx)

	plan, err := migrate.PlanMigration(c.dir(), handle)
	if err != nil {
		return c.fail(err)
	}
	if !ctx.JSON() {
		if handle.Previewing() {
			c.println("Dry run: the move would take these steps, and nothing is changed:")
		} else {
			c.println("Moving this repository onto the " + layout.Root + "/ layout:")
		}
		for _, line := range plan.Lines() {
			c.println("  " + line)
		}
	}
	if err := migrate.Apply(handle, c.dir(), plan); err != nil {
		return c.fail(err)
	}
	committed := false
	if autoCommit {
		committed, _, err = gitcommit.AutoCommit(
			plan.Commit,
			"selfdoc layout migrate: move selfdoc's directories from "+layout.PreviousRoot+"/ to "+layout.Root+"/",
			c.dir(), handle,
		)
		if err != nil {
			return c.fail(err)
		}
	}

	moves := make([]any, 0, len(plan.Moves))
	for _, move := range plan.Moves {
		moves = append(moves, map[string]any{"from": move.From, "to": move.To})
	}
	writes := make([]any, 0, len(plan.Writes))
	for _, write := range plan.Writes {
		writes = append(writes, write.Path)
	}
	rewrites := make([]any, 0, len(plan.Rewrites))
	for _, rewrite := range plan.Rewrites {
		changes := make([]any, 0, len(rewrite.Changes))
		for _, change := range rewrite.Changes {
			changes = append(changes, change)
		}
		rewrites = append(rewrites, map[string]any{"path": rewrite.Path, "changes": changes})
	}
	deletes := make([]any, 0, len(plan.Deletes))
	for _, deleted := range plan.Deletes {
		deletes = append(deletes, deleted)
	}
	ctx.Payload(map[string]any{
		"previous_root":         layout.PreviousRoot,
		"root":                  layout.Root,
		"moves":                 moves,
		"writes":                writes,
		"rewrites":              rewrites,
		"deletes":               deletes,
		"removed_previous_root": plan.RemovePreviousRoot,
		"committed":             committed,
	})
	if !ctx.JSON() && !handle.Previewing() {
		c.printf("Moved %d directories under %s/.", len(plan.Moves), layout.Root)
		if committed {
			c.printf(" Committed.")
		}
		c.println()
	}
	return strictcli.Exit(0)
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
			"path":             dir.Rel(),
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
