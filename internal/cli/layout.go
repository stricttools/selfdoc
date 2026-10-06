package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gen"
	"github.com/stricttools/selfdoc/internal/gitcommit"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/migrate"
	"github.com/stricttools/selfdoc/internal/payloadschemas"
	"github.com/stricttools/strictcli/go/strictcli"
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
		"Check this repository's "+layout.Root+"/ directory: every directory carries a "+layout.ManifestFileName+" naming a tool this machine has, every directory selfdoc claims names selfdoc, a directory selfdoc owns starts with a dot exactly when it is generated, every directory selfdoc owns holds only what its side allows, nothing inside selfdoc's committed directories starts with a dot, every uncommitted directory selfdoc owns carries the "+layout.IgnoreFileName+" its declaration derives, and "+layout.Root+"/ holds directories and "+strings.Join(layout.RootFiles, ", ")+" only",
		c.cmdLayoutValidate,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.PayloadSchema(payloadschemas.LayoutValidate()),
	)

	group.Command("migrate",
		"Move this repository off a layout before this one: every directory whose "+layout.ManifestFileName+" names selfdoc under "+layout.PreviousRoot+"/ (the layout before this one, whose generated directories already start with a dot) or under "+layout.EarlierRoot+"/ (the layout before that, which named every directory bare) moves under "+layout.Root+"/, a generated one behind a dot ("+migrationExample()+"). It creates "+layout.Root+"/ (the manifests naming selfdoc are the grant), writes each uncommitted directory's own "+layout.IgnoreFileName+", removes selfdoc's block from the previous root's "+layout.IgnoreFileName+" (the file and the previous root go when nothing else is left), rewrites every selfdoc.json value naming a moved path and every generated root file's header, moves a CLAUDE.md an earlier selfdoc generated at the repository root to "+gen.ClaudeOutputRel+" (where selfdoc generates it now, and Claude Code reads it), writes "+layout.TermsRel+" empty when the project has none, converts the manifests ("+layout.ManifestRel+" and "+layout.PostManifestRel+") from schema_version "+strconv.Itoa(manifest.PreviousSchemaVersion)+" to "+strconv.Itoa(manifest.SchemaVersion)+", which records the project's accepted words and rejected patterns from "+layout.TermsRel+", and commits. In every repository it also converts the "+layout.RootIgnoreRel+" an earlier selfdoc derived into one "+layout.IgnoreFileName+" per uncommitted directory, and moves strictcli's files out of every .strictcli/ directory into the "+layout.Root+"/ beside it: schema.json to "+layout.Root+"/.cli-schema/schema.json, test-coverage.json to "+layout.Root+"/.cli-test-coverage/manifest.json, and the coverage/ shards to "+layout.Root+"/.cli-test-coverage/shards/, writing each directory's "+layout.ManifestFileName+" naming strictcli and the "+layout.IgnoreFileName+" that keeps the shards out of the repository, deleting the stub go.mod, and removing the emptied .strictcli/ and an empty coverage/; for the repository root's .strictcli/ it also deletes .rlsbl/bases/.strictcli/go.mod, drops .strictcli/go.mod from .rlsbl/managed-files.json, and removes the root .gitignore's coverage/ line. A repository already on "+layout.Root+"/ whose manifests are on schema_version "+strconv.Itoa(manifest.PreviousSchemaVersion)+" gets the manifest conversion alone, and one whose "+layout.VocabularyRel+"/ carries no "+layout.ManifestFileName+" -- a repository moved from the older .selfdoc/ layout -- also gets that directory's grant and "+layout.TermsRel+" empty when the project has none, and one whose generated CLAUDE.md is still at its root gets that move. Another tool's directories stay where they are, and so does a hand-written CLAUDE.md. Refuses a repository already migrated with its manifests converted, its vocabulary directory granted, its ignore files converted, no .strictcli/ left, and no generated CLAUDE.md at its root; a .strictcli/ holding a file it does not know the place of, or one git does not track; a .strictcli/ file whose new place is taken; an "+layout.RootIgnoreRel+" holding another tool's lines; part-way through a move; holding selfdoc's directories under both previous roots; never on a previous layout; holding "+gen.ClaudeOutputRel+" beside a generated root CLAUDE.md; or ignoring "+gen.ClaudeOutputRel+" in git. --dry-run prints the plan and changes nothing",
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
	var moves []string
	for _, previousLayout := range layout.PreviousLayouts {
		for _, dir := range layout.Declared() {
			moves = append(moves, previousLayout.Root+"/"+previousLayout.EntryName(dir)+" -> "+dir.Rel())
		}
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
		switch {
		case handle.Previewing():
			c.println("Dry run: the migration would take these steps, and nothing is changed:")
		case len(plan.Moves) > 0:
			c.println("Moving this repository onto the " + layout.Root + "/ layout:")
		default:
			c.println("Bringing this repository onto the current " + layout.Root + "/ layout:")
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
		message := "selfdoc layout migrate: " + plan.Summary()
		committed, _, err = gitcommit.AutoCommit(plan.Commit, message, c.dir(), handle)
		if err != nil {
			return c.fail(err)
		}
	}

	moves := make([]any, 0, len(plan.Moves))
	for _, move := range plan.Moves {
		moves = append(moves, map[string]any{"from": move.From, "to": move.To})
	}
	fileMoves := make([]any, 0, len(plan.FileMoves))
	for _, move := range plan.FileMoves {
		fileMoves = append(fileMoves, map[string]any{"from": move.From, "to": move.To})
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
		deletes = append(deletes, deleted.Path)
	}
	cliMoves := make([]any, 0, len(plan.CLIMoves))
	for _, move := range plan.CLIMoves {
		cliMoves = append(cliMoves, map[string]any{"from": move.From, "to": move.To})
	}
	shardMoves := make([]any, 0, len(plan.ShardMoves))
	for _, move := range plan.ShardMoves {
		files := make([]any, 0, len(move.Files))
		for _, file := range move.Files {
			files = append(files, file)
		}
		shardMoves = append(shardMoves, map[string]any{"from": move.From, "to": move.To, "files": files})
	}
	removedDirs := make([]any, 0, len(plan.RemoveDirs))
	for _, removed := range plan.RemoveDirs {
		removedDirs = append(removedDirs, removed)
	}
	ctx.Payload(map[string]any{
		"previous_root":         plan.PreviousRoot,
		"root":                  layout.Root,
		"moves":                 moves,
		"file_moves":            fileMoves,
		"cli_moves":             cliMoves,
		"shard_moves":           shardMoves,
		"removed_directories":   removedDirs,
		"writes":                writes,
		"rewrites":              rewrites,
		"deletes":               deletes,
		"removed_previous_root": plan.RemovePreviousRoot,
		"committed":             committed,
	})
	if !ctx.JSON() && !handle.Previewing() {
		var sentences []string
		if len(plan.Moves) > 0 {
			sentences = append(sentences, fmt.Sprintf("Moved %d directories under %s/.", len(plan.Moves), layout.Root))
		}
		if len(plan.CLIDirs) > 0 {
			sentences = append(sentences, fmt.Sprintf("Moved strictcli's files out of %d .strictcli/ directory(s).", len(plan.CLIDirs)))
		}
		if len(plan.Writes) > 0 || len(plan.Rewrites) > 0 || len(plan.Deletes) > 0 {
			sentences = append(sentences, fmt.Sprintf("Wrote %d file(s), rewrote %d, and deleted %d.", len(plan.Writes), len(plan.Rewrites), len(plan.Deletes)))
		}
		for _, move := range plan.FileMoves {
			sentences = append(sentences, fmt.Sprintf("Moved %s to %s.", move.From, move.To))
		}
		if committed {
			sentences = append(sentences, "Committed.")
		}
		c.println(strings.Join(sentences, " "))
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
			"ignore_path":      ignorePath(dir),
			"ignore_content":   ignoreContent(dir),
		})
	}
	return map[string]any{
		"tool":          layout.Owner,
		"root":          layout.Root,
		"manifest_file": layout.ManifestFileName,
		"root_files":    rootFiles(),
		"directories":   directories,
	}
}

// ignorePath is where a directory's own ignore file sits, and empty for a
// committed directory, which carries none.
func ignorePath(dir layout.Directory) string {
	if dir.Commitment != layout.Uncommitted {
		return ""
	}
	return layout.DirectoryIgnoreRel(dir.Name)
}

// ignoreContent is what a directory's own ignore file holds, and empty for a
// committed directory.
func ignoreContent(dir layout.Directory) string {
	if dir.Commitment != layout.Uncommitted {
		return ""
	}
	return layout.DirectoryIgnoreContent()
}

// rootFiles lists the files the root may hold beside its directories.
func rootFiles() []any {
	files := make([]any, 0, len(layout.RootFiles))
	for _, file := range layout.RootFiles {
		files = append(files, file)
	}
	return files
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
