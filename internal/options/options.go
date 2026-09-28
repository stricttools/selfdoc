// Package options writes selfdoc's entries into a repository's options
// directory, .strictmetadata/options/: the engine of `selfdoc options set`.
//
// The directory belongs to strictspec, whose manifest names it as the owner:
// several tools file their entries there, each writing only its own. selfdoc
// writes only entries of its own namespace (selfdoc:<lint name>), into the
// subject document its registry declares, and validates the result with
// strictspec before anything is written: the shape of every subject document,
// and the selfdoc namespace as a whole with the new entry in place.
package options

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tomledit "github.com/smm-h/go-toml-edit"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/strictspec/go/strictspec"
)

// directoryName is the options directory's name under the layout root.
var directoryName = strings.TrimPrefix(strictspec.OptionsDir, layout.Root+"/")

// The actions [Set] reports.
const (
	ActionCreated   = "created"
	ActionUpdated   = "updated"
	ActionUnchanged = "unchanged"
)

// Result is what one [Set] wrote.
type Result struct {
	// ID is the entry's option, as selfdoc:<lint name>.
	ID string
	// File is the subject document, relative to the repository root.
	File string
	// Action is whether the entry was created, updated, or already held
	// these values.
	Action string
	// Current, Ideal and Reason are the entry's values.
	Current, Ideal, Reason string
	// Class is strictspec's ranking classification of the entry.
	Class strictspec.OptionsClass
	// Written are the files written, relative to the repository root: the
	// subject document, and the directory's manifest when it was absent.
	Written []string
}

// NamespaceError is an id outside selfdoc's namespace.
type NamespaceError struct {
	ID string
}

func (e *NamespaceError) Error() string {
	tool, _, found := strings.Cut(e.ID, ":")
	if !found {
		return fmt.Sprintf(
			"%q is not an option id: an id is <tool>:<name>, and selfdoc writes its own, %s:<lint name>.",
			e.ID, lints.OptionsTool)
	}
	return fmt.Sprintf(
		"%q is an option of %s, not of selfdoc: selfdoc writes only its own entries, %s:<lint name>. %s's entries are written by %s's own command, or by hand.",
		e.ID, tool, lints.OptionsTool, tool, tool)
}

// Set writes one selfdoc entry, or updates the one the subject document
// already holds for the same option, after strictspec accepts the result.
//
// The options directory and its manifest, naming strictspec, are created when
// absent; a manifest naming anyone else is refused, and so is an id outside
// the selfdoc namespace. Every write goes through the handle, so a dry run
// records the same writes a real run performs.
func Set(h *effects.Handle, repoRoot, id, current, ideal, reason string) (Result, error) {
	if !strings.HasPrefix(id, lints.OptionsTool+":") {
		return Result{}, &NamespaceError{ID: id}
	}
	manifestMissing, err := checkManifest(repoRoot)
	if err != nil {
		return Result{}, err
	}
	loaded, err := strictspec.LoadOptionsEntries(repoRoot)
	if err != nil {
		return Result{}, err
	}
	if len(loaded.Invalid) > 0 {
		var detail strings.Builder
		for _, file := range loaded.Invalid {
			fmt.Fprintf(&detail, "\n %s/%s:", strictspec.OptionsDir, file.File)
			for _, d := range file.Diagnostics {
				fmt.Fprintf(&detail, "\n  %s: %s", d.Code, d.Message)
			}
		}
		return Result{}, fmt.Errorf(
			"selfdoc will not edit %s/ while a document in it is not a valid options-entries document. Fix what each diagnostic names:%s",
			strictspec.OptionsDir, detail.String())
	}

	name := strings.TrimPrefix(id, lints.OptionsTool+":")
	subjectFile := lints.OptionsSubject + ".toml"
	if option, found := lints.OptionsRegistry().Option(name); found {
		subjectFile = option.Declaration.Subject + ".toml"
	}

	// The namespace as it will stand: the entry the subject document holds
	// for this option replaced, or the new one added at its end.
	candidates := make([]strictspec.OptionsEntry, 0, len(loaded.Entries)+1)
	existing, unchanged := -1, false
	inSubject := 0
	for _, entry := range loaded.Entries {
		if entry.File == subjectFile {
			inSubject++
		}
		if entry.File == subjectFile && entry.ID == id && !entry.HasScope && existing < 0 {
			existing = entry.Index
			unchanged = entry.Current == current && entry.Ideal == ideal && entry.Reason == reason
			entry.Current, entry.Ideal, entry.Reason = current, ideal, reason
		}
		candidates = append(candidates, entry)
	}
	if existing < 0 {
		candidates = append(candidates, strictspec.OptionsEntry{
			File: subjectFile, Index: inSubject, ID: id,
			Current: current, Ideal: ideal, Reason: reason,
		})
	}
	accepted, diags := strictspec.ValidateOptionsNamespace(lints.OptionsTool, lints.OptionsRegistry(), candidates)
	if len(diags) > 0 {
		var detail strings.Builder
		for _, d := range diags {
			fmt.Fprintf(&detail, "\n  %s: %s", d.Code, d.Message)
		}
		return Result{}, fmt.Errorf(
			"strictspec refuses %s as it would stand with this entry, so nothing was written:%s",
			strictspec.OptionsDir+"/", detail.String())
	}
	var class strictspec.OptionsClass
	for _, entry := range accepted {
		if entry.Entry.File == subjectFile && entry.Entry.ID == id && !entry.Entry.HasScope {
			class = entry.Class
		}
	}

	result := Result{
		ID: id, File: strictspec.OptionsDir + "/" + subjectFile,
		Current: current, Ideal: ideal, Reason: reason, Class: class,
	}
	if unchanged {
		result.Action = ActionUnchanged
		return result, nil
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(result.File))
	content, err := render(path, existing, id, current, ideal, reason)
	if err != nil {
		return Result{}, err
	}
	// The bytes about to be written are held to the shape once more, so a
	// rendering defect is refused rather than written.
	if _, shape := strictspec.ReadOptionsEntries(subjectFile, content); len(shape) > 0 {
		return Result{}, fmt.Errorf("the rendered %s is not a valid options-entries document: %s: %s",
			result.File, shape[0].Code, shape[0].Message)
	}

	directory := filepath.Join(repoRoot, filepath.FromSlash(strictspec.OptionsDir))
	if err := h.MkdirAll(directory); err != nil {
		return Result{}, err
	}
	if manifestMissing {
		manifestRel := strictspec.OptionsDir + "/" + layout.ManifestFileName
		if err := h.AtomicWrite(filepath.Join(repoRoot, filepath.FromSlash(manifestRel)),
			[]byte(layout.DirectoryManifestContent(layout.SharedOwner)), effects.ModeDefault); err != nil {
			return Result{}, err
		}
		result.Written = append(result.Written, manifestRel)
	}
	if err := h.AtomicWrite(path, content, modeOf(path)); err != nil {
		return Result{}, err
	}
	result.Written = append(result.Written, result.File)
	result.Action = ActionCreated
	if existing >= 0 {
		result.Action = ActionUpdated
	}
	return result, nil
}

// checkManifest answers whether the options directory's manifest is missing,
// and refuses one that names an owner other than strictspec.
func checkManifest(repoRoot string) (bool, error) {
	manifest, err := layout.ReadDirectoryManifest(repoRoot, directoryName)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if manifest.Owner != layout.SharedOwner {
		return false, fmt.Errorf(
			"%s/%s declares %q as the owner of %s/, and that directory belongs to %q: several tools share it, and strictspec holds its schemas. Write this line instead:\n%s",
			strictspec.OptionsDir, layout.ManifestFileName, manifest.Owner, strictspec.OptionsDir,
			layout.SharedOwner, strings.TrimRight(layout.DirectoryManifestContent(layout.SharedOwner), "\n"))
	}
	return false, nil
}

// entriesHeader opens a new subject document with the format_version the
// options-entries schema requires.
const entriesHeader = "format_version = 1\n"

// render returns the subject document with the entry written: the values of
// entry index updated in place when index is not negative, or a new entry
// appended. Every other line of the document is kept as it stands.
func render(path string, index int, id, current, ideal, reason string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw = []byte(entriesHeader)
	} else if err != nil {
		return nil, err
	}
	document, err := tomledit.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	fields := [][2]string{{"current", current}, {"ideal", ideal}, {"reason", reason}}
	if index >= 0 {
		for _, field := range fields {
			if err := document.Set("entry["+strconv.Itoa(index)+"]."+field[0], field[1]); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		}
		return document.Bytes(), nil
	}
	if err := document.NewArrayTable("entry"); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, field := range append([][2]string{{"id", id}}, fields...) {
		if err := document.SetCreate("entry[-1]."+field[0], field[1]); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return withBlankLines(document.Bytes()), nil
}

// withBlankLines puts one blank line before every [[header]] written flush
// against what precedes it, so an entry the command wrote looks like one
// written by hand.
func withBlankLines(raw []byte) []byte {
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var out []string
	for index, line := range lines {
		if index > 0 && strings.HasPrefix(line, "[[") && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// modeOf is the mode a file already has, or the ordinary mode of a file a
// person edits when it is new.
func modeOf(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}
