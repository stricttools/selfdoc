// Package confidential is selfdoc's half of two rules: the publishing rules
// of the lifecycle-and-license record
// (.strictmetadata/lifecycle-and-license/lifecycle-and-license.toml), and the
// confidential-term list.
//
//   - A repository whose record has no releasable that may publish (every
//     license in effect is proprietary) refuses a public output outright
//     ([PublicOutputAllowed]).
//   - Every page and post a public output carries -- a deploy, a published
//     post, a documentation publish, an assembly push -- is scanned against
//     the confidential-term list (an age-encrypted list outside every
//     repository, decrypted in memory; a missing list has no terms), and a hit
//     the repository's resolutions (.strictmetadata/confidential-hits/
//     resolutions.toml) do not resolve refuses the output, every hit at once
//     ([Check]).
//
// selfdoc writes nothing for either rule: resolutions are recorded with
// rlsbl's confidential commands.
package confidential

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/strictspec/go/confidential"
	"github.com/stricttools/strictspec/go/lifecycle"
)

// Repository is the repository a selfdoc command runs in, as far as the
// publishing rules and the confidential-term scan need it.
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

// Text is one published text: its page or file path, and its content.
type Text struct {
	Page    string
	Content string
}

// Fix is what a refusal of an unresolved hit tells the agent to do.
const Fix = "Judge each hit. When you are at least 70% certain it is a false positive, record it with " +
	"`rlsbl confidential judge-false-positive --hit <id> --certainty <70-100> --reason <one line, without the term>`, " +
	"run in this repository. Every other hit goes to the owner, who either has it fixed (reword the source of the page " +
	"or post, rebuild, and run the command again) or approves publishing it, hit by hit: " +
	"`rlsbl confidential approve-hit --hit <id> --reason <the owner's reason>`. rlsbl accepts the id of a hit the " +
	"repository's tracked files or next push carry; a hit only built output carries is fixed in its source."

// Check refuses, naming what (the output), every hit of list in texts that
// the resolutions of repo do not resolve. Hits are found with the entries
// that apply to repo (an entry's except scope names a repository by its
// directory's or its origin's name). The list's status is returned for the
// caller to report: a missing list has no terms, which the output says.
func Check(list *confidential.List, repo Repository, what string, texts []Text) (string, error) {
	names, err := confidential.RepositoryNames(repo.Root, repo.Origin)
	if err != nil {
		return "", err
	}
	m := list.For(names)
	var hits []confidential.Hit
	for _, t := range texts {
		hits = append(hits, m.Scan(t.Page, t.Content)...)
	}
	if len(hits) == 0 {
		return list.Status(), nil
	}
	resolutions, err := confidential.LoadResolutions(repo.Root)
	if err != nil {
		return "", err
	}
	o := confidential.Resolve(hits, resolutions)
	if err := o.Refusal("the "+what, Fix); err != nil {
		return "", fmt.Errorf("refusing the %s: %w", what, err)
	}
	return fmt.Sprintf("%s; %d hit(s) resolved:\n%s", list.Status(), len(o.Resolved), strings.TrimRight(o.ReportResolved(), "\n")), nil
}

// FilesTexts are the texts of files (page path to content), in page order,
// leaving out binary content.
func FilesTexts(files map[string][]byte) []Text {
	pages := make([]string, 0, len(files))
	for page := range files {
		pages = append(pages, page)
	}
	sort.Strings(pages)
	var out []Text
	for _, page := range pages {
		if isText(files[page]) {
			out = append(out, Text{Page: page, Content: string(files[page])})
		}
	}
	return out
}

// DirTexts are the texts of every regular file under dir, each page the
// file's slash-separated path relative to dir, leaving out binary content.
func DirTexts(dir string) ([]Text, error) {
	var out []Text
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
		if isText(data) {
			out = append(out, Text{Page: filepath.ToSlash(rel), Content: string(data)})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading %s to scan it for confidential terms: %w", dir, err)
	}
	return out, nil
}

// RefTexts are the texts of every tracked text file of the git tree at ref,
// in the work tree at root, each page the file's path in the tree. When every
// entry of m is a literal, git grep picks the files holding one ignoring
// case first; a pattern entry reads every file. Every git question is a
// declared read, so it runs under --dry-run too.
func RefTexts(h *effects.Handle, root, ref string, m *confidential.Matcher) ([]Text, error) {
	if m.Len() == 0 {
		return nil, nil
	}
	var pages []string
	terms, literal := m.Terms()
	if literal {
		argv := []string{"git", "grep", "-I", "-i", "-l", "-z", "-F"}
		for _, t := range terms {
			argv = append(argv, "-e", t)
		}
		argv = append(argv, ref, "--")
		grep, err := h.Run(argv, effects.Cwd(root), effects.CaptureOutput(), effects.Read())
		if err != nil {
			return nil, fmt.Errorf("searching %s for confidential terms: %w", ref, err)
		}
		switch grep.ExitCode {
		case 0:
		case 1:
			// git grep's answer for no match.
			return nil, nil
		default:
			return nil, fmt.Errorf("searching %s for confidential terms: git grep exited %d: %s", ref, grep.ExitCode, grep.StderrString())
		}
		for _, entry := range strings.Split(string(grep.Stdout), "\x00") {
			if entry != "" {
				pages = append(pages, strings.TrimPrefix(entry, ref+":"))
			}
		}
	} else {
		ls, err := h.Run([]string{"git", "ls-tree", "-r", "-z", "--name-only", ref}, effects.Cwd(root), effects.CaptureOutput(), effects.Read())
		if err != nil {
			return nil, fmt.Errorf("listing the files of %s: %w", ref, err)
		}
		if ls.ExitCode != 0 {
			return nil, fmt.Errorf("listing the files of %s: git ls-tree exited %d: %s", ref, ls.ExitCode, ls.StderrString())
		}
		for _, entry := range strings.Split(string(ls.Stdout), "\x00") {
			if entry != "" {
				pages = append(pages, entry)
			}
		}
	}
	var out []Text
	for _, page := range pages {
		show, err := h.Run([]string{"git", "show", ref + ":" + page}, effects.Cwd(root), effects.CaptureOutput(), effects.Read())
		if err != nil {
			return nil, fmt.Errorf("reading %s at %s: %w", page, ref, err)
		}
		if show.ExitCode != 0 {
			return nil, fmt.Errorf("reading %s at %s: git show exited %d: %s", page, ref, show.ExitCode, show.StderrString())
		}
		if isText(show.Stdout) {
			out = append(out, Text{Page: page, Content: string(show.Stdout)})
		}
	}
	return out, nil
}

// isText reports whether data is UTF-8 text holding no NUL byte.
func isText(data []byte) bool {
	return bytes.IndexByte(data, 0) < 0 && utf8.Valid(data)
}
