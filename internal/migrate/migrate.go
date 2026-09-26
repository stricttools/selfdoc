// Package migrate moves a repository off the layout before this one: selfdoc's
// directories under the hidden .stricttools/ root, each under its bare
// function name, onto the visible stricttools/ root, where a generated
// directory's name starts with a dot.
//
// It is the engine of `selfdoc layout migrate`. [Plan] reads the repository
// and returns every step the move takes, or refuses; [Apply] performs the plan
// through an effects handle, so a dry run records the same steps it prints.
//
// Only the directories whose manifest names selfdoc move. Another tool's
// directory under .stricttools/ stays where it is, and so does the rest of
// that directory's ignore file.
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
	// Moves are the directories that move, in name order.
	Moves []Move
	// Writes are the files the move creates: the derived ignore file under
	// the new root, and the vocabulary files a repository without them
	// needs.
	Writes []Write
	// Rewrites are the files rewritten in place: the ignore file under the
	// previous root when another tool's lines stay in it, selfdoc.json, and
	// the generated root files whose header names a moved template.
	Rewrites []Rewrite
	// Deletes are the files the move deletes: the previous root's ignore file
	// when selfdoc's block was all it held.
	Deletes []string
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
	lines = append(lines, "create "+layout.Root+"/")
	for _, move := range p.Moves {
		lines = append(lines, fmt.Sprintf("move %s/ -> %s/", move.From, move.To))
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
		lines = append(lines, fmt.Sprintf("delete %s (it held only selfdoc's block)", deleted))
	}
	if p.RemovePreviousRoot {
		lines = append(lines, fmt.Sprintf("remove %s/ (nothing else is left in it)", layout.PreviousRoot))
	}
	return lines
}

// gitTimeout bounds the one git probe the plan makes.
const gitTimeout = 10 * time.Second

// NotNeededError is a repository the move has nothing to do for: already on
// this layout, or never on the previous one.
type NotNeededError struct {
	Message string
}

func (e *NotNeededError) Error() string { return e.Message }

// PartialError is a repository holding selfdoc's directories under both
// roots: a move that was begun and not finished, which this command refuses to
// guess the rest of.
type PartialError struct {
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
		back = append(back, fmt.Sprintf("  git mv %s/%s %s/%s", layout.Root, entry, layout.PreviousRoot, dir.Name))
	}
	return fmt.Sprintf(
		"This repository is part-way through the move: %s still holds %s, and %s already holds %s. selfdoc layout migrate moves a repository in one step and will not guess how the rest was meant to go. Put selfdoc's directories back under %s/ and run it again:\n%s",
		layout.PreviousRoot+"/", strings.Join(e.Previous, ", "),
		layout.Root+"/", strings.Join(e.Current, ", "),
		layout.PreviousRoot, strings.Join(back, "\n"))
}

// PlanMigration reads a repository and returns its move, or refuses.
func PlanMigration(baseDir string, h *effects.Handle) (Plan, error) {
	if _, err := os.Stat(filepath.Join(baseDir, layout.DeprecatedRoot)); err == nil {
		return Plan{}, layout.RefuseOldLayout(baseDir, "", "", "")
	}
	previous, err := layout.PreviousSelfdocEntries(baseDir)
	if err != nil {
		return Plan{}, err
	}
	current, err := layout.CurrentSelfdocEntries(baseDir)
	if err != nil {
		return Plan{}, err
	}
	switch {
	case len(previous) == 0 && len(current) > 0:
		return Plan{}, &NotNeededError{Message: fmt.Sprintf(
			"Nothing to migrate: %s/ already holds selfdoc's directories (%s), and %s/ holds none.",
			layout.Root, strings.Join(current, ", "), layout.PreviousRoot)}
	case len(previous) == 0:
		return Plan{}, &NotNeededError{Message: fmt.Sprintf(
			"Nothing to migrate: this repository has no %s/ directory holding one whose %s names selfdoc. A repository that has not adopted selfdoc runs 'selfdoc init'.",
			layout.PreviousRoot, layout.ManifestFileName)}
	case len(current) > 0:
		return Plan{}, &PartialError{Previous: previous, Current: current}
	}

	var plan Plan
	moved := map[string]bool{}
	for _, name := range previous {
		dir, claimed := layout.Lookup(name)
		if !claimed {
			return Plan{}, fmt.Errorf(
				"%s/%s names selfdoc as its owner, and selfdoc claims no directory by that name. Resolve its ownership before migrating: it is not selfdoc's to move",
				layout.PreviousRoot, name)
		}
		from := layout.PreviousRoot + "/" + name
		plan.Moves = append(plan.Moves, Move{From: from, To: dir.Rel()})
		moved[name] = true
		tracked, err := trackedFiles(baseDir, from, h)
		if err != nil {
			return Plan{}, err
		}
		for _, file := range tracked {
			plan.Commit = append(plan.Commit, file, dir.Rel()+strings.TrimPrefix(file, from))
		}
	}

	// Another tool's lines in an ignore file already under the new root stay
	// where they are, beside selfdoc's block.
	ignoreRel := layout.Root + "/" + layout.IgnoreFileName
	existingIgnore, err := os.ReadFile(layout.IgnorePath(baseDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Plan{}, err
	}
	plan.Writes = append(plan.Writes, Write{
		Path: ignoreRel, Why: "selfdoc's block, for the new names",
		Content: []byte(layout.RenderIgnore(string(existingIgnore))),
	})
	plan.Commit = append(plan.Commit, ignoreRel)

	if err := plan.planVocabulary(baseDir, moved); err != nil {
		return Plan{}, err
	}
	if err := plan.planPreviousIgnore(baseDir, moved); err != nil {
		return Plan{}, err
	}
	configPaths, err := plan.planConfig(baseDir, moved)
	if err != nil {
		return Plan{}, err
	}
	if err := plan.planRootFileHeaders(baseDir, configPaths); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// planVocabulary adds the vocabulary directory's manifest when the previous
// root had no vocabulary directory, and an empty terms file when the
// vocabulary directory holds none. The previous root's manifests naming
// selfdoc are the grant for both.
func (p *Plan) planVocabulary(baseDir string, moved map[string]bool) error {
	if !moved[layout.VocabularyName] {
		manifestRel := layout.DirectoryManifestRel(layout.VocabularyName)
		p.Writes = append(p.Writes, Write{
			Path: manifestRel, Why: "the vocabulary directory's grant",
			Content: []byte(layout.DirectoryManifestContent(layout.Owner)),
		})
		p.Commit = append(p.Commit, manifestRel)
	}
	previousTerms := filepath.Join(baseDir, layout.PreviousRoot, layout.VocabularyName, filepath.Base(layout.TermsRel))
	if _, err := os.Stat(previousTerms); err == nil && moved[layout.VocabularyName] {
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

// planPreviousIgnore removes selfdoc's block from the previous root's ignore
// file: the file goes when the block was all it held, and the previous root
// goes when nothing else is left in it.
func (p *Plan) planPreviousIgnore(baseDir string, moved map[string]bool) error {
	ignoreRel := layout.PreviousRoot + "/" + layout.IgnoreFileName
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
			p.Deletes = append(p.Deletes, ignoreRel)
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
	entries, err := os.ReadDir(filepath.Join(baseDir, layout.PreviousRoot))
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
func (p *Plan) planConfig(baseDir string, moved map[string]bool) ([]string, error) {
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
		if movedPath(value, moved) {
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
// whose template moved, so the file names the template where it now is.
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
		outputPath := filepath.Join(baseDir, outputName)
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
func movedPath(value string, moved map[string]bool) bool {
	rest, found := strings.CutPrefix(value, layout.PreviousRoot+"/")
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

// Apply performs a plan: creates the new root, moves the directories, writes,
// rewrites and deletes the files, and removes the previous root when nothing
// is left in it. Under a dry-run handle every step is recorded instead.
func Apply(h *effects.Handle, baseDir string, plan Plan) error {
	if err := h.MkdirAll(filepath.Join(baseDir, layout.Root)); err != nil {
		return err
	}
	created := map[string]bool{filepath.Join(baseDir, layout.Root): true}
	for _, move := range plan.Moves {
		to := filepath.Join(baseDir, filepath.FromSlash(move.To))
		if err := h.Rename(filepath.Join(baseDir, filepath.FromSlash(move.From)), to); err != nil {
			return err
		}
		created[to] = true
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
		if err := h.Remove(filepath.Join(baseDir, filepath.FromSlash(deleted))); err != nil {
			return err
		}
	}
	if plan.RemovePreviousRoot {
		return h.Rmdir(filepath.Join(baseDir, layout.PreviousRoot))
	}
	return nil
}
