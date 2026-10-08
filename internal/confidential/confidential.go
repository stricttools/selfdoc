// Package confidential is selfdoc's half of the confidential-name rules of
// the lifecycle-and-license record
// (.strictmetadata/lifecycle-and-license/lifecycle-and-license.toml).
//
// A repository is confidential while one of its releasables has a proprietary
// license period in effect, and public otherwise; a repository without a record
// is public. Three things follow for selfdoc:
//
//   - every mutating command keeps the machine-local confidential-name index
//     (<os.UserConfigDir()>/strictspec/confidential-names.toml) current for the
//     repository it runs in: [Refresh] upserts a confidential repository's
//     names, keyed by its record's open releasable-name identities, and removes
//     a public repository's entry. A read-only command never writes the index.
//   - every page and post a public output carries -- a deploy, a published
//     post, a documentation publish -- is scanned against every name in the
//     index, in every repository, and a match refuses the output ([ScanDir],
//     [ScanFiles], [Refusal]).
//   - a repository whose record has no releasable that may publish (every
//     license in effect is proprietary) refuses the output outright
//     ([PublicOutputAllowed]).
//
// Every index write goes through a [Writer] backed by the command's effects
// handle, so --dry-run records the write instead of making it.
package confidential

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/strictspec/go/lifecycle"
	"github.com/stricttools/strictspec/go/lifecycle/index"
)

// Writer backs lifecycle.FileWriter with a selfdoc effects handle.
type Writer struct {
	Handle *effects.Handle
}

// WriteFile replaces the file at path atomically, so a concurrent reader of
// the shared index never sees half of it.
func (w Writer) WriteFile(path string, data []byte) error {
	return w.Handle.AtomicWrite(path, data, effects.ModeDefault)
}

// MkdirAll creates the directory at path and its parents.
func (w Writer) MkdirAll(path string) error {
	return w.Handle.MkdirAll(path)
}

// Repository is the repository a selfdoc command runs in, as far as the
// confidential-name rules need it.
type Repository struct {
	// Root is the git work tree's root, or the project directory itself when
	// it is in no git work tree.
	Root string
	// InGit reports whether the project directory is in a git work tree.
	InGit bool
	// Origin is the origin remote's URL, empty when there is none; its last
	// path segment is one of the repository's names.
	Origin string
	// Record is the repository's lifecycle-and-license record; an absent
	// record is the empty, public record.
	Record *lifecycle.Record
}

// Locate finds the repository dir belongs to, its origin remote, and its
// record. Both git questions are declared reads, so they run under --dry-run
// too.
func Locate(h *effects.Handle, dir string) (Repository, error) {
	repo := Repository{Root: dir}
	top, err := h.Run([]string{"git", "rev-parse", "--show-toplevel"},
		effects.Cwd(dir), effects.CaptureOutput(), effects.Read())
	if err != nil {
		return Repository{}, fmt.Errorf("finding the git repository of %s: %w", dir, err)
	}
	if top.ExitCode == 0 {
		repo.InGit = true
		repo.Root = strings.TrimSpace(top.StdoutString())
		remote, err := h.Run([]string{"git", "config", "--get", "remote.origin.url"},
			effects.Cwd(repo.Root), effects.CaptureOutput(), effects.Read())
		if err != nil {
			return Repository{}, fmt.Errorf("reading the origin remote of %s: %w", repo.Root, err)
		}
		switch remote.ExitCode {
		case 0:
			repo.Origin = strings.TrimSpace(remote.StdoutString())
		case 1:
			// git's answer for an unset key: the repository has no origin.
		default:
			return Repository{}, fmt.Errorf("reading the origin remote of %s: git exited %d: %s",
				repo.Root, remote.ExitCode, remote.StderrString())
		}
	}
	record, err := lifecycle.Load(repo.Root)
	if err != nil {
		return Repository{}, err
	}
	repo.Record = record
	return repo, nil
}

// Refresh brings the confidential-name index at indexPath up to date for repo
// on the date of on, writing through w: a confidential repository's entry,
// keyed by its record's open releasable-name identities, is upserted with the
// names it protects, and a public repository's entry is removed. Only mutating
// commands call it. No remote is needed: the origin, when there is one, only
// adds its name to the repository's names.
func Refresh(w lifecycle.FileWriter, indexPath string, repo Repository, on time.Time) error {
	names, err := index.RepositoryNames(repo.Root, repo.Origin)
	if err != nil {
		return err
	}
	update, err := index.Plan(repo.Record, on, names...)
	if err != nil {
		return fmt.Errorf("recording this repository's confidential names in the index at %s: %w", indexPath, err)
	}
	idx, err := index.Load(indexPath)
	if err != nil {
		return fmt.Errorf("reading the confidential-name index: %w", err)
	}
	if err := idx.Apply(w, update); err != nil {
		return fmt.Errorf("bringing this repository's entry in the confidential-name index at %s in step with its record: %w", indexPath, err)
	}
	return nil
}

// PublicOutputAllowed refuses output when the record has releasables with a
// license in effect on the date of on and every one of them is proprietary:
// there is then no releasable the output may publish for. It returns the
// lifecycle library's own refusal, which names the rule, the subject, the
// period, and what to do. A record with no license in effect allows it.
func PublicOutputAllowed(record *lifecycle.Record, output lifecycle.Output, on time.Time) error {
	var subjects []string
	seen := map[string]bool{}
	for _, l := range record.Licenses() {
		if l.Contains(on) && !seen[l.Subject] {
			seen[l.Subject] = true
			subjects = append(subjects, l.Subject)
		}
	}
	var refusal error
	for _, subject := range subjects {
		err := record.PublicOutputAllowed(subject, output, on)
		if err == nil {
			return nil
		}
		if refusal == nil {
			refusal = err
		}
	}
	return refusal
}

// Finding is one confidential name in a published page or post: the page's
// path, the 1-based line and column, and the name as the index spells it.
type Finding struct {
	Page   string
	Line   int
	Column int
	Term   string
}

// ScanFiles scans every file of files (page path to content) against names.
func ScanFiles(names []string, files map[string][]byte) []Finding {
	pages := make([]string, 0, len(files))
	for page := range files {
		pages = append(pages, page)
	}
	sort.Strings(pages)
	var findings []Finding
	for _, page := range pages {
		findings = append(findings, scanPage(names, page, files[page])...)
	}
	return findings
}

// ScanDir scans every regular file under dir against names; each finding's
// page is the file's slash-separated path relative to dir.
func ScanDir(names []string, dir string) ([]Finding, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var findings []Finding
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		findings = append(findings, scanPage(names, filepath.ToSlash(rel), data)...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning %s for confidential names: %w", dir, err)
	}
	return findings, nil
}

func scanPage(names []string, page string, data []byte) []Finding {
	var findings []Finding
	for _, m := range index.ScanTerms(string(data), names) {
		findings = append(findings, Finding{Page: page, Line: m.Line, Column: m.Column, Term: m.Term})
	}
	return findings
}

// ScanRef scans every tracked text file of the git tree at ref, in the work
// tree at root, against names; each finding's page is the file's path in the
// tree. git grep picks the files holding a name ignoring case, and each is
// read at ref and matched on whole tokens, as every other scan matches. Both
// git questions are declared reads, so they run under --dry-run too.
func ScanRef(h *effects.Handle, root, ref string, names []string) ([]Finding, error) {
	if len(names) == 0 {
		return nil, nil
	}
	argv := []string{"git", "grep", "-I", "-i", "-l", "-z", "-F"}
	for _, n := range names {
		argv = append(argv, "-e", n)
	}
	argv = append(argv, ref, "--")
	grep, err := h.Run(argv, effects.Cwd(root), effects.CaptureOutput(), effects.Read())
	if err != nil {
		return nil, fmt.Errorf("searching %s for confidential names: %w", ref, err)
	}
	switch grep.ExitCode {
	case 0:
	case 1:
		// git grep's answer for no match.
		return nil, nil
	default:
		return nil, fmt.Errorf("searching %s for confidential names: git grep exited %d: %s", ref, grep.ExitCode, grep.StderrString())
	}
	var findings []Finding
	for _, entry := range strings.Split(string(grep.Stdout), "\x00") {
		if entry == "" {
			continue
		}
		page := strings.TrimPrefix(entry, ref+":")
		show, err := h.Run([]string{"git", "show", ref + ":" + page}, effects.Cwd(root), effects.CaptureOutput(), effects.Read())
		if err != nil {
			return nil, fmt.Errorf("reading %s at %s: %w", page, ref, err)
		}
		if show.ExitCode != 0 {
			return nil, fmt.Errorf("reading %s at %s: git show exited %d: %s", page, ref, show.ExitCode, show.StderrString())
		}
		findings = append(findings, scanPage(names, page, show.Stdout)...)
	}
	return findings, nil
}

// Refusal is the error a public output with findings refuses with: it names
// the output, every page, line, column, and term, and the index the names come
// from. It is nil when there are no findings.
func Refusal(output string, findings []Finding, indexPath string) error {
	if len(findings) == 0 {
		return nil
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, fmt.Sprintf("%s, line %d, column %d: %s", f.Page, f.Line, f.Column, f.Term))
	}
	return fmt.Errorf("refusing the %s: what it would publish names terms the confidential-name index protects:\n  %s\n"+
		"Remove each term from the pages and posts it came from, rebuild, and run the command again. "+
		"The index is %s; commands in the confidential repositories keep it current",
		output, strings.Join(lines, "\n  "), indexPath)
}

// Names reads the index at indexPath and returns every name it holds.
func Names(indexPath string) ([]string, error) {
	idx, err := index.Load(indexPath)
	if err != nil {
		return nil, fmt.Errorf("reading the confidential-name index: %w", err)
	}
	return idx.Names(), nil
}
