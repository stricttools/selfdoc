// Package migrate moves a repository off a layout before this one onto the
// hidden .strictmetadata/ root, where a generated directory's name starts with
// a dot: selfdoc's directories under the visible stricttools/ root, named as
// they are named now, or under the hidden .stricttools/ root before it, each
// under its bare function name.
//
// It is the engine of `selfdoc layout migrate`. [PlanMigration] reads the
// repository and returns every step the move takes, or refuses; [Apply]
// performs the plan through an effects handle, so a dry run records the same
// steps it prints.
//
// Only the directories whose manifest names selfdoc move. Another tool's
// directory under the previous root stays where it is, and so does the rest of
// that root's ignore file.
//
// The move also takes a CLAUDE.md an earlier selfdoc generated at the
// repository root to .claude/CLAUDE.md, where selfdoc generates it now, in the
// same commit: on its own for a repository already on this layout, and beside
// the directories for one that is not.
//
// The move also takes strictcli's files out of every .strictcli/ directory
// into the .strictmetadata/ beside it (see planCLIFiles), and converts the
// ignore file an earlier selfdoc derived at the top of .strictmetadata/ into
// one per uncommitted directory (see planIgnores): on their own for a
// repository already on this layout, and beside the directories for one that
// is not.
//
// The move also converts the repository's manifests -- the build manifest and
// the post manifest -- from the schema before the vocabulary to the current
// one, adding the vocabulary of the project's terms file. A repository already
// on this layout whose manifests are outdated gets that conversion alone, and
// one whose vocabulary directory carries no grant -- a repository moved from
// the older .selfdoc/ layout, which had no vocabulary directory -- gets the
// grant and an empty terms file, as the move writes them.
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/gen"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// Move is one directory's move, as paths relative to the repository root.
type Move struct {
	From string
	To   string
}

// Rewrite is one file rewritten in place: what changed, and the whole
// content it will hold.
type Rewrite struct {
	// Path is the file, relative to the repository root.
	Path string
	// Changes are the replacements made, one "old -> new" line each.
	Changes []string
	// Content is what the file holds after the rewrite.
	Content []byte
	// Mode is the file's mode, kept through the rewrite.
	Mode os.FileMode
}

// Write is one file the move creates.
type Write struct {
	// Path is the file, relative to the repository root.
	Path string
	// Why says what the file is.
	Why string
	// Content is what it holds.
	Content []byte
}

// Plan is every step of one repository's move, in the order [Apply] takes
// them.
type Plan struct {
	// PreviousRoot is the root the directories move from, and empty when
	// nothing moves.
	PreviousRoot string
	// Moves are the directories that move, in name order.
	Moves []Move
	// FileMoves are the generated root files that move to where selfdoc
	// generates them now: a CLAUDE.md an earlier selfdoc generated at the
	// repository root, to .claude/CLAUDE.md.
	FileMoves []Move
	// CLIMoves are strictcli's committed files moving out of a .strictcli/
	// directory: the schema and the test-coverage manifest.
	CLIMoves []Move
	// ShardMoves are the uncommitted coverage shards moving out of a
	// .strictcli/coverage/ directory.
	ShardMoves []ShardMove
	// Writes are the files the move creates: the ignore files of the
	// uncommitted directories, the vocabulary files a repository without them
	// needs, and the grants and ignore file of strictcli's directories.
	Writes []Write
	// Rewrites are the files rewritten in place: the ignore file under the
	// previous root when another tool's lines stay in it, selfdoc.json, and
	// the generated root files whose header names a moved template, at the
	// path each has once the file moves are done.
	Rewrites []Rewrite
	// Deletes are the files the move deletes: an ignore file whose only lines
	// were selfdoc's block, and the stub modules a .strictcli/ directory held.
	Deletes []Delete
	// RemoveDirs are the directories the move empties and removes, deepest
	// first: each .strictcli/ directory and what was left in it.
	RemoveDirs []string
	// CLIDirs are the .strictcli/ directories the move takes strictcli's
	// files out of, relative to the repository root.
	CLIDirs []string
	// RemovePreviousRoot is set when nothing is left under the previous root
	// once selfdoc's directories and its ignore file are gone.
	RemovePreviousRoot bool
	// Commit are the paths a commit of the move names: every tracked file
	// that moved, at both paths, and every file written, rewritten or
	// deleted.
	Commit []string
}

// Lines renders the plan one path per line, the way the command prints it.
func (p Plan) Lines() []string {
	var lines []string
	if len(p.Moves) > 0 {
		lines = append(lines, "create "+layout.Root+"/")
	}
	for _, move := range p.Moves {
		lines = append(lines, fmt.Sprintf("move %s/ -> %s/", move.From, move.To))
	}
	for _, move := range p.FileMoves {
		lines = append(lines, fmt.Sprintf("move %s -> %s (a generated root file, where selfdoc generates it now)", move.From, move.To))
	}
	for _, move := range p.CLIMoves {
		lines = append(lines, fmt.Sprintf("move %s -> %s", move.From, move.To))
	}
	for _, move := range p.ShardMoves {
		lines = append(lines, fmt.Sprintf("move %d coverage shard file(s) %s/ -> %s/", len(move.Files), move.From, move.To))
	}
	for _, write := range p.Writes {
		lines = append(lines, fmt.Sprintf("write %s (%s)", write.Path, write.Why))
	}
	for _, rewrite := range p.Rewrites {
		for _, change := range rewrite.Changes {
			lines = append(lines, fmt.Sprintf("rewrite %s: %s", rewrite.Path, change))
		}
	}
	for _, deleted := range p.Deletes {
		lines = append(lines, fmt.Sprintf("delete %s (%s)", deleted.Path, deleted.Why))
	}
	for _, removed := range p.RemoveDirs {
		lines = append(lines, fmt.Sprintf("remove %s/ (nothing is left in it)", removed))
	}
	if p.RemovePreviousRoot {
		lines = append(lines, fmt.Sprintf("remove %s/ (nothing else is left in it)", p.PreviousRoot))
	}
	return lines
}

// Summary says what the plan does in one clause per kind of step, the way
// the commit message and the command's headline state it.
func (p Plan) Summary() string {
	var clauses []string
	switch {
	case len(p.Moves) > 0:
		clauses = append(clauses, "move selfdoc's directories from "+p.PreviousRoot+"/ to "+layout.Root+"/")
	default:
		if p.writesVocabulary() {
			clauses = append(clauses, "write the vocabulary directory")
		}
		if p.convertsManifests() {
			clauses = append(clauses, fmt.Sprintf("convert the manifests to schema_version %d", manifest.SchemaVersion))
		}
	}
	if len(p.RemoveDirs) > 0 {
		clauses = append(clauses, "move strictcli's files from .strictcli/ to "+layout.Root+"/")
	}
	// A move off a previous root writes the ignore files as part of the move.
	if len(p.Moves) == 0 && p.convertsIgnores() {
		clauses = append(clauses, "give each uncommitted directory its own ignore file")
	}
	for _, move := range p.FileMoves {
		clauses = append(clauses, "move the generated "+move.From+" to "+move.To)
	}
	return strings.Join(clauses, " and ")
}

// writesVocabulary reports whether the plan writes the vocabulary directory's
// files.
func (p Plan) writesVocabulary() bool {
	for _, write := range p.Writes {
		if write.Path == layout.TermsRel || write.Path == layout.DirectoryManifestRel(layout.VocabularyName) {
			return true
		}
	}
	return false
}

// convertsManifests reports whether the plan converts or commits a manifest
// document.
func (p Plan) convertsManifests() bool {
	for _, rewrite := range p.Rewrites {
		for _, rel := range convertedManifests {
			if rewrite.Path == rel {
				return true
			}
		}
	}
	return false
}

// convertsIgnores reports whether the plan writes an uncommitted directory's
// ignore file or deletes the one at the top of the root.
func (p Plan) convertsIgnores() bool {
	for _, deleted := range p.Deletes {
		if deleted.Path == layout.RootIgnoreRel {
			return true
		}
	}
	for _, dir := range layout.IgnoredDirectories() {
		for _, write := range p.Writes {
			if write.Path == layout.DirectoryIgnoreRel(dir.Name) {
				return true
			}
		}
	}
	return false
}

// empty reports whether the plan has no step at all.
func (p Plan) empty() bool {
	return len(p.Moves) == 0 && len(p.FileMoves) == 0 && len(p.CLIMoves) == 0 &&
		len(p.ShardMoves) == 0 && len(p.Writes) == 0 && len(p.Rewrites) == 0 &&
		len(p.Deletes) == 0 && len(p.RemoveDirs) == 0 && !p.RemovePreviousRoot
}

// gitTimeout bounds the one git probe the plan makes.
const gitTimeout = 10 * time.Second

// NotNeededError is a repository the move has nothing to do for: already on
// this layout, or never on the previous one.
type NotNeededError struct {
	Message string
}

func (e *NotNeededError) Error() string { return e.Message }

// PartialError is a repository holding selfdoc's directories under a previous
// root and the current one: a move that was begun and not finished, which this
// command refuses to guess the rest of.
type PartialError struct {
	// From is the previous layout the directories still under it are in.
	From layout.PreviousLayout
	// Previous are selfdoc's directories still under the previous root, by
	// name.
	Previous []string
	// Current are selfdoc's directories already under the new root, by the
	// name each carries on disk.
	Current []string
}

func (e *PartialError) Error() string {
	var back []string
	for _, entry := range e.Current {
		dir, _ := layout.LookupFunction(entry)
		back = append(back, fmt.Sprintf("  git mv %s/%s %s/%s", layout.Root, entry, e.From.Root, e.From.EntryName(dir)))
	}
	return fmt.Sprintf(
		"This repository is part-way through the move: %s still holds %s, and %s already holds %s. selfdoc layout migrate moves a repository in one step and will not guess how the rest was meant to go. Put selfdoc's directories back under %s/ and run it again:\n%s",
		e.From.Root+"/", strings.Join(e.Previous, ", "),
		layout.Root+"/", strings.Join(e.Current, ", "),
		e.From.Root, strings.Join(back, "\n"))
}

// SeveralPreviousRootsError is a repository holding selfdoc's directories
// under more than one previous root, and none under the current one: which
// copy is the project's is not the move's to guess.
type SeveralPreviousRootsError struct {
	// Found are selfdoc's directories, as paths relative to the repository
	// root, under every previous root that holds any.
	Found []string
}

func (e *SeveralPreviousRootsError) Error() string {
	return fmt.Sprintf(
		"This repository keeps selfdoc's directories under more than one layout before this one (%s). selfdoc layout migrate moves a repository off one of them and will not guess which copy is the project's. Delete the copy that is not, and run it again.",
		strings.Join(e.Found, ", "))
}

// PlanMigration reads a repository and returns its move, or refuses.
func PlanMigration(baseDir string, h *effects.Handle) (Plan, error) {
	if _, err := os.Stat(filepath.Join(baseDir, layout.DeprecatedRoot)); err == nil {
		return Plan{}, layout.RefuseOldLayout(baseDir, "", "", "")
	}
	current, err := layout.CurrentSelfdocEntries(baseDir)
	if err != nil {
		return Plan{}, err
	}
	var from layout.PreviousLayout
	var previous, everyPrevious []string
	for _, previousLayout := range layout.PreviousLayouts {
		entries, err := layout.PreviousSelfdocEntries(baseDir, previousLayout.Root)
		if err != nil {
			return Plan{}, err
		}
		if len(entries) == 0 {
			continue
		}
		if previous == nil {
			from, previous = previousLayout, entries
		}
		for _, entry := range entries {
			everyPrevious = append(everyPrevious, previousLayout.Root+"/"+entry)
		}
	}
	switch {
	case len(previous) == 0 && len(current) > 0:
		var plan Plan
		if err := plan.planRootClaude(baseDir, h); err != nil {
			return Plan{}, err
		}
		if err := plan.planMissingVocabulary(baseDir); err != nil {
			return Plan{}, err
		}
		if err := plan.planManifests(baseDir, h, from, false, false); err != nil {
			return Plan{}, err
		}
		if err := plan.planIgnores(baseDir, nil); err != nil {
			return Plan{}, err
		}
		if err := plan.planCLIFiles(baseDir, h); err != nil {
			return Plan{}, err
		}
		if !plan.empty() {
			return plan, nil
		}
		return Plan{}, &NotNeededError{Message: fmt.Sprintf(
			"Nothing to migrate: %s/ already holds selfdoc's directories (%s), no layout before it (%s) holds any, %s carries its grant, the manifests are on schema_version %d, no %s at the repository root was generated by selfdoc, every uncommitted directory carries its own %s and %s holds none, and no %s/ directory is left.",
			layout.Root, strings.Join(current, ", "), previousRootsList(), layout.VocabularyRel+"/", manifest.SchemaVersion, gen.PreviousClaudeOutputRel, layout.IgnoreFileName, layout.Root+"/", previousCLIDir)}
	case len(previous) == 0:
		var plan Plan
		if err := plan.planCLIFiles(baseDir, h); err != nil {
			return Plan{}, err
		}
		if !plan.empty() {
			return plan, nil
		}
		return Plan{}, &NotNeededError{Message: fmt.Sprintf(
			"Nothing to migrate: this repository has no directory under a layout before this one (%s) whose %s names selfdoc. A repository that has not adopted selfdoc runs 'selfdoc init'.",
			previousRootsList(), layout.ManifestFileName)}
	case len(current) > 0:
		return Plan{}, &PartialError{From: from, Previous: previous, Current: current}
	case len(everyPrevious) > len(previous):
		return Plan{}, &SeveralPreviousRootsError{Found: everyPrevious}
	}

	plan := Plan{PreviousRoot: from.Root}
	moved := map[string]bool{}
	for _, name := range previous {
		dir, claimed := from.Lookup(name)
		if !claimed {
			return Plan{}, fmt.Errorf(
				"%s/%s names selfdoc as its owner, and selfdoc claims no directory by that name. Resolve its ownership before migrating: it is not selfdoc's to move",
				from.Root, name)
		}
		fromRel := from.Root + "/" + name
		plan.Moves = append(plan.Moves, Move{From: fromRel, To: dir.Rel()})
		moved[name] = true
		tracked, err := trackedFiles(baseDir, fromRel, h)
		if err != nil {
			return Plan{}, err
		}
		for _, file := range tracked {
			plan.Commit = append(plan.Commit, file, dir.Rel()+strings.TrimPrefix(file, fromRel))
		}
	}

	arriving := map[string]bool{}
	for name := range moved {
		if dir, claimed := from.Lookup(name); claimed {
			arriving[dir.Name] = true
		}
	}
	if err := plan.planIgnores(baseDir, arriving); err != nil {
		return Plan{}, err
	}

	if err := plan.planVocabulary(baseDir, from, moved); err != nil {
		return Plan{}, err
	}
	if err := plan.planManifests(baseDir, h, from, movedFunction(from, moved, layout.DocsStateName), movedFunction(from, moved, layout.VocabularyName)); err != nil {
		return Plan{}, err
	}
	if err := plan.planPreviousIgnore(baseDir, from, moved); err != nil {
		return Plan{}, err
	}
	configPaths, err := plan.planConfig(baseDir, from, moved)
	if err != nil {
		return Plan{}, err
	}
	if err := plan.planRootClaude(baseDir, h); err != nil {
		return Plan{}, err
	}
	if err := plan.planRootFileHeaders(baseDir, configPaths); err != nil {
		return Plan{}, err
	}
	if err := plan.planCLIFiles(baseDir, h); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// ExistingClaudeOutputError is a repository holding both a CLAUDE.md an
// earlier selfdoc generated at its root and a .claude/CLAUDE.md: which one is
// current is not the move's to guess.
type ExistingClaudeOutputError struct{}

func (e *ExistingClaudeOutputError) Error() string {
	return fmt.Sprintf(
		"%s at the repository root was generated by selfdoc, and %s, where selfdoc generates it now, exists as well. selfdoc layout migrate moves the root file there and will not guess which of the two is current. Remove the one that is not -- 'git rm %s', or 'git rm %s' -- commit, and run it again.",
		gen.PreviousClaudeOutputRel, gen.ClaudeOutputRel, gen.ClaudeOutputRel, gen.PreviousClaudeOutputRel)
}

// planRootClaude moves a CLAUDE.md an earlier selfdoc generated at the
// repository root to .claude/CLAUDE.md, refusing when a file is already there
// or when git ignores that path. A hand-written root CLAUDE.md is not
// selfdoc's, and stays.
func (p *Plan) planRootClaude(baseDir string, h *effects.Handle) error {
	if !gen.HasGeneratedHeader(filepath.Join(baseDir, gen.PreviousClaudeOutputRel)) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(baseDir, filepath.FromSlash(gen.ClaudeOutputRel))); err == nil {
		return &ExistingClaudeOutputError{}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := gen.RefuseIgnoredOutput(baseDir, gen.ClaudeOutputRel, h); err != nil {
		return err
	}
	p.FileMoves = append(p.FileMoves, Move{From: gen.PreviousClaudeOutputRel, To: gen.ClaudeOutputRel})
	p.Commit = append(p.Commit, gen.PreviousClaudeOutputRel, gen.ClaudeOutputRel)
	return nil
}

// previousRootsList names every previous root, the way a refusal lists them.
func previousRootsList() string {
	roots := make([]string, 0, len(layout.PreviousLayouts))
	for _, previousLayout := range layout.PreviousLayouts {
		roots = append(roots, previousLayout.Root+"/")
	}
	return strings.Join(roots, ", ")
}

// movedFunction reports whether the claimed directory with the given function
// name moves in this plan: whether its entry under the previous root moved.
func movedFunction(from layout.PreviousLayout, moved map[string]bool, name string) bool {
	dir, _ := layout.Lookup(name)
	return moved[from.EntryName(dir)]
}

// planVocabulary adds the vocabulary directory's manifest when the previous
// root had no vocabulary directory, and an empty terms file when the
// vocabulary directory holds none. The previous root's manifests naming
// selfdoc are the grant for both.
func (p *Plan) planVocabulary(baseDir string, from layout.PreviousLayout, moved map[string]bool) error {
	vocabularyMoves := movedFunction(from, moved, layout.VocabularyName)
	if !vocabularyMoves {
		manifestRel := layout.DirectoryManifestRel(layout.VocabularyName)
		p.Writes = append(p.Writes, Write{
			Path: manifestRel, Why: "the vocabulary directory's grant",
			Content: []byte(layout.DirectoryManifestContent(layout.Owner)),
		})
		p.Commit = append(p.Commit, manifestRel)
	}
	previousTerms := filepath.Join(baseDir, from.Root, layout.VocabularyName, filepath.Base(layout.TermsRel))
	if _, err := os.Stat(previousTerms); err == nil && vocabularyMoves {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	p.Writes = append(p.Writes, Write{
		Path: layout.TermsRel, Why: "an empty vocabulary",
		Content: []byte(vocabulary.EmptyTerms),
	})
	p.Commit = append(p.Commit, layout.TermsRel)
	return nil
}

// planMissingVocabulary gives a repository already on this layout the
// vocabulary directory it never had -- the grant and an empty terms file, as
// the move off the previous root writes them -- when the directory carries no
// grant. A repository moved onto this layout from the one before the previous
// root is one: that layout had no vocabulary directory to move. A directory
// that carries its grant is left as it stands, terms file or none.
func (p *Plan) planMissingVocabulary(baseDir string) error {
	_, err := layout.ReadDirectoryManifest(baseDir, layout.VocabularyName)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	manifestRel := layout.DirectoryManifestRel(layout.VocabularyName)
	p.Writes = append(p.Writes, Write{
		Path: manifestRel, Why: "the vocabulary directory's grant",
		Content: []byte(layout.DirectoryManifestContent(layout.Owner)),
	})
	p.Commit = append(p.Commit, manifestRel)
	if _, err := os.Stat(layout.Path(baseDir, layout.TermsRel)); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	p.Writes = append(p.Writes, Write{
		Path: layout.TermsRel, Why: "an empty vocabulary",
		Content: []byte(vocabulary.EmptyTerms),
	})
	p.Commit = append(p.Commit, layout.TermsRel)
	return nil
}

// convertedManifests are the manifest documents the conversion covers, by
// their path on this layout.
var convertedManifests = []string{layout.ManifestRel, layout.PostManifestRel}

// planManifests converts every manifest of the previous schema to the current
// one, with the vocabulary of the project's terms file. stateMoves and
// vocabularyMoves say whether the generated-state and vocabulary directories
// move in this plan, which decides where each file is read from now; the
// rewrite is written where the file sits once the move is done.
//
// A manifest already on the current schema on disk whose committed copy is
// not gets committed as it is: the committed copy is what every reader of the
// project's history reads.
func (p *Plan) planManifests(baseDir string, h *effects.Handle, from layout.PreviousLayout, stateMoves, vocabularyMoves bool) error {
	termsRel := layout.TermsRel
	if vocabularyMoves {
		termsRel = from.Root + "/" + layout.VocabularyName + "/" + filepath.Base(layout.TermsRel)
	}
	stateDir, _ := layout.Lookup(layout.DocsStateName)
	var terms *vocabulary.List
	for _, rel := range convertedManifests {
		readRel := rel
		if stateMoves {
			readRel = from.Root + "/" + from.EntryName(stateDir) + strings.TrimPrefix(rel, layout.DocsStateRel)
		}
		readPath := filepath.Join(baseDir, filepath.FromSlash(readRel))
		raw, err := os.ReadFile(readPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		info, err := os.Stat(readPath)
		if err != nil {
			return err
		}
		declared, err := manifest.DeclaredSchema(raw, readRel)
		if err != nil {
			return err
		}
		switch {
		case declared == manifest.PreviousSchemaVersion:
			if terms == nil {
				loaded, err := vocabulary.LoadTermsAt(filepath.Join(baseDir, filepath.FromSlash(termsRel)), termsRel)
				if err != nil {
					return err
				}
				terms = &loaded
			}
			converted, err := manifest.Convert(raw, *terms, readRel)
			if err != nil {
				return err
			}
			p.Rewrites = append(p.Rewrites, Rewrite{
				Path: rel,
				Changes: []string{fmt.Sprintf("schema_version %d -> %d, with the vocabulary of %s",
					manifest.PreviousSchemaVersion, manifest.SchemaVersion, layout.TermsRel)},
				Content: converted, Mode: info.Mode().Perm(),
			})
			p.Commit = append(p.Commit, rel)
		case declared == manifest.SchemaVersion && !stateMoves:
			committed, found := committedSchema(baseDir, rel, h)
			if found && committed != manifest.SchemaVersion {
				p.Rewrites = append(p.Rewrites, Rewrite{
					Path: rel,
					Changes: []string{fmt.Sprintf("schema_version %d on disk and %d at git HEAD: committed as it is",
						manifest.SchemaVersion, committed)},
					Content: raw, Mode: info.Mode().Perm(),
				})
				p.Commit = append(p.Commit, rel)
			}
		case declared != manifest.SchemaVersion:
			return fmt.Errorf(
				"%s declares schema_version %d, which this selfdoc neither reads nor converts: it reads %d and converts %d",
				readRel, declared, manifest.SchemaVersion, manifest.PreviousSchemaVersion)
		}
	}
	return nil
}

// committedSchema answers the schema_version of the copy of rel committed at
// HEAD, and false when there is no such copy or it does not parse.
func committedSchema(baseDir, rel string, h *effects.Handle) (int64, bool) {
	result, err := h.Run(
		[]string{"git", "show", "HEAD:" + rel},
		effects.Cwd(baseDir), effects.CaptureOutput(),
		effects.Timeout(gitTimeout), effects.Read(),
	)
	if err != nil || result.ExitCode != 0 {
		return 0, false
	}
	declared, err := manifest.DeclaredSchema(result.Stdout, "HEAD:"+rel)
	if err != nil {
		return 0, false
	}
	return declared, true
}

// planPreviousIgnore removes selfdoc's block from the previous root's ignore
// file: the file goes when the block was all it held, and the previous root
// goes when nothing else is left in it.
func (p *Plan) planPreviousIgnore(baseDir string, from layout.PreviousLayout, moved map[string]bool) error {
	ignoreRel := from.Root + "/" + layout.IgnoreFileName
	ignorePath := filepath.Join(baseDir, filepath.FromSlash(ignoreRel))
	ignoreGone := true
	raw, err := os.ReadFile(ignorePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		remaining := layout.WithoutIgnoreBlock(string(raw))
		if strings.TrimSpace(remaining) == "" {
			p.Deletes = append(p.Deletes, Delete{Path: ignoreRel, Why: "it held only selfdoc's block"})
		} else {
			ignoreGone = false
			info, statErr := os.Stat(ignorePath)
			if statErr != nil {
				return statErr
			}
			p.Rewrites = append(p.Rewrites, Rewrite{
				Path: ignoreRel, Changes: []string{"selfdoc's block removed; the other lines stay"},
				Content: []byte(remaining), Mode: info.Mode().Perm(),
			})
		}
		p.Commit = append(p.Commit, ignoreRel)
	}
	entries, err := os.ReadDir(filepath.Join(baseDir, from.Root))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if moved[entry.Name()] || (entry.Name() == layout.IgnoreFileName && ignoreGone) {
			continue
		}
		return nil
	}
	p.RemovePreviousRoot = true
	return nil
}

// planConfig rewrites every string value in selfdoc.json that names a path
// inside one of the moved directories, and returns the root-file template
// paths as the config declared them, for the header rewrite.
func (p *Plan) planConfig(baseDir string, from layout.PreviousLayout, moved map[string]bool) ([]string, error) {
	configPath := filepath.Join(baseDir, "selfdoc.json")
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("selfdoc.json is not valid JSON: %w", err)
	}
	values := map[string]bool{}
	collectStrings(document, values)
	var olds []string
	for value := range values {
		if movedPath(value, from, moved) {
			olds = append(olds, value)
		}
	}
	sort.Strings(olds)
	text := string(raw)
	var changes []string
	for _, old := range olds {
		oldLiteral, newLiteral := jsonString(old), jsonString(layout.MigratedPath(old))
		if !strings.Contains(text, oldLiteral) {
			return nil, fmt.Errorf(
				"selfdoc.json holds the value %s, spelled in a way this command cannot find to rewrite. Write it as %s, and run this again",
				oldLiteral, oldLiteral)
		}
		text = strings.ReplaceAll(text, oldLiteral, newLiteral)
		changes = append(changes, oldLiteral+" -> "+newLiteral)
	}
	var rootFiles []string
	if object, ok := document.(map[string]any); ok {
		if list, ok := object["root_files"].([]any); ok {
			for _, item := range list {
				if value, ok := item.(string); ok {
					rootFiles = append(rootFiles, value)
				}
			}
		}
	}
	if len(changes) == 0 {
		return rootFiles, nil
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, err
	}
	p.Rewrites = append(p.Rewrites, Rewrite{
		Path: "selfdoc.json", Changes: changes, Content: []byte(text), Mode: info.Mode().Perm(),
	})
	p.Commit = append(p.Commit, "selfdoc.json")
	return rootFiles, nil
}

// planRootFileHeaders rewrites the header line of every generated root file
// whose template moved, so the file names the template where it now is. A root
// file this plan also moves is read where it is and rewritten where it goes.
func (p *Plan) planRootFileHeaders(baseDir string, templates []string) error {
	for _, template := range templates {
		migrated := layout.MigratedPath(template)
		if migrated == template {
			continue
		}
		outputName, named := gen.RootFileOutputName(template)
		if !named {
			continue
		}
		readRel := outputName
		for _, move := range p.FileMoves {
			if move.To == outputName {
				readRel = move.From
			}
		}
		outputPath := filepath.Join(baseDir, filepath.FromSlash(readRel))
		raw, err := os.ReadFile(outputPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		oldHeader, newHeader := gen.RootFileHeaderLine(template), gen.RootFileHeaderLine(migrated)
		first, rest, _ := strings.Cut(string(raw), "\n")
		if first != oldHeader {
			continue
		}
		info, err := os.Stat(outputPath)
		if err != nil {
			return err
		}
		p.Rewrites = append(p.Rewrites, Rewrite{
			Path:    outputName,
			Changes: []string{"header names " + migrated},
			Content: []byte(newHeader + "\n" + rest),
			Mode:    info.Mode().Perm(),
		})
		p.Commit = append(p.Commit, outputName)
	}
	return nil
}

// movedPath reports whether a value names a path inside one of the moved
// directories: the directory itself, or something under it.
func movedPath(value string, from layout.PreviousLayout, moved map[string]bool) bool {
	rest, found := strings.CutPrefix(value, from.Root+"/")
	if !found {
		return false
	}
	head, _, _ := strings.Cut(rest, "/")
	return moved[head]
}

// collectStrings gathers every string value in a decoded JSON document.
func collectStrings(value any, into map[string]bool) {
	switch typed := value.(type) {
	case string:
		into[typed] = true
	case []any:
		for _, item := range typed {
			collectStrings(item, into)
		}
	case map[string]any:
		for _, item := range typed {
			collectStrings(item, into)
		}
	}
}

// jsonString is a string's JSON literal, the way selfdoc.json spells a path:
// quoted, with no HTML escaping.
func jsonString(value string) string {
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(out.String(), "\n")
}

// trackedFiles lists the tracked files under a directory, relative to the
// repository root. A directory outside any git repository tracks nothing.
func trackedFiles(baseDir, rel string, h *effects.Handle) ([]string, error) {
	result, err := h.Run(
		[]string{"git", "ls-files", "-z", "--", rel},
		effects.Cwd(baseDir), effects.CaptureOutput(),
		effects.Timeout(gitTimeout), effects.Read(),
	)
	if err != nil || result.ExitCode != 0 {
		return nil, nil
	}
	var files []string
	for _, name := range strings.Split(string(result.Stdout), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Apply performs a plan: creates the new root when directories move, moves
// them, moves the generated root files and strictcli's files, writes,
// rewrites and deletes the files, removes the emptied .strictcli/
// directories, and removes the previous root when nothing is left in it.
// Under a dry-run handle every step is recorded instead.
func Apply(h *effects.Handle, baseDir string, plan Plan) error {
	if len(plan.Moves) > 0 {
		if err := h.MkdirAll(filepath.Join(baseDir, layout.Root)); err != nil {
			return err
		}
	}
	created := map[string]bool{filepath.Join(baseDir, layout.Root): true}
	for _, move := range plan.Moves {
		to := filepath.Join(baseDir, filepath.FromSlash(move.To))
		if err := h.Rename(filepath.Join(baseDir, filepath.FromSlash(move.From)), to); err != nil {
			return err
		}
		created[to] = true
	}
	for _, move := range plan.FileMoves {
		to := filepath.Join(baseDir, filepath.FromSlash(move.To))
		if parent := filepath.Dir(to); !created[parent] {
			if err := h.MkdirAll(parent); err != nil {
				return err
			}
			created[parent] = true
		}
		if err := h.Rename(filepath.Join(baseDir, filepath.FromSlash(move.From)), to); err != nil {
			return err
		}
	}
	for _, move := range plan.CLIMoves {
		to := filepath.Join(baseDir, filepath.FromSlash(move.To))
		if parent := filepath.Dir(to); !created[parent] {
			if err := h.MkdirAll(parent); err != nil {
				return err
			}
			created[parent] = true
		}
		if err := h.Rename(filepath.Join(baseDir, filepath.FromSlash(move.From)), to); err != nil {
			return err
		}
	}
	for _, move := range plan.ShardMoves {
		to := filepath.Join(baseDir, filepath.FromSlash(move.To))
		if !created[to] {
			if err := h.MkdirAll(to); err != nil {
				return err
			}
			created[to] = true
		}
		for _, file := range move.Files {
			from := filepath.Join(baseDir, filepath.FromSlash(move.From), file)
			if err := h.Rename(from, filepath.Join(to, file)); err != nil {
				return err
			}
		}
	}
	for _, write := range plan.Writes {
		target := filepath.Join(baseDir, filepath.FromSlash(write.Path))
		if parent := filepath.Dir(target); !created[parent] {
			if err := h.MkdirAll(parent); err != nil {
				return err
			}
			created[parent] = true
		}
		if err := h.AtomicWrite(target, write.Content, 0o644); err != nil {
			return err
		}
	}
	for _, rewrite := range plan.Rewrites {
		target := filepath.Join(baseDir, filepath.FromSlash(rewrite.Path))
		if err := h.AtomicWrite(target, rewrite.Content, rewrite.Mode); err != nil {
			return err
		}
	}
	for _, deleted := range plan.Deletes {
		if err := h.Remove(filepath.Join(baseDir, filepath.FromSlash(deleted.Path))); err != nil {
			return err
		}
	}
	for _, removed := range plan.RemoveDirs {
		if err := h.Rmdir(filepath.Join(baseDir, filepath.FromSlash(removed))); err != nil {
			return err
		}
	}
	if plan.RemovePreviousRoot {
		return h.Rmdir(filepath.Join(baseDir, plan.PreviousRoot))
	}
	return nil
}
