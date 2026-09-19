// Package spellcorpus is the corpus-wide spelling run: the same engine, every
// sibling project.
//
// `selfdoc check` spell-checks the project it is run in. This runs the
// identical engine over every selfdoc project that lives beside it, which is
// how the shared accept list gets populated: one sweep surfaces the technical
// vocabulary the whole fleet uses, and the terms that are genuine get added
// once, for everyone.
//
// Strictly read-only over the projects it visits. Directives are not resolved
// -- resolution runs a project's extractors over its source, and a survey has
// no business doing that in someone else's repository -- so what is scanned is
// the raw Markdown body of every docs template and every post. Posts are read
// straight off disk rather than through post discovery, which means drafts are
// surveyed too: a draft's prose is still prose, and a term it introduces
// belongs on the accept list before the draft ships. A project whose config
// cannot be loaded is reported and skipped, never fatal.
package spellcorpus

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/fleet"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/stricttools/selfdoc/internal/util"
)

// ProjectSpellReport is what the sweep found in one project.
type ProjectSpellReport struct {
	// Name is the project directory's name.
	Name string
	// Path is the project directory.
	Path string
	// Pages is how many documents were scanned -- docs templates plus
	// posts.
	Pages int
	// Misspellings are the unknown words, in document order.
	Misspellings []spelling.Misspelling
	// Error is why the project could not be read, empty when it was.
	Error string
}

// WordCount is one unknown word with how many times it occurred.
type WordCount struct {
	// Word is the unrecognized word as written.
	Word string
	// Count is how many times the sweep saw it.
	Count int
}

// UniqueWords returns the report's unknown words with their occurrence counts,
// commonest first and then alphabetically by lowercased spelling.
func (r ProjectSpellReport) UniqueWords() []WordCount {
	return uniqueWords(r.Misspellings)
}

// uniqueWords counts the words of a misspelling list, commonest first.
func uniqueWords(misspellings []spelling.Misspelling) []WordCount {
	counts := map[string]int{}
	var order []string
	for _, miss := range misspellings {
		if _, seen := counts[miss.Word]; !seen {
			order = append(order, miss.Word)
		}
		counts[miss.Word]++
	}
	result := make([]WordCount, 0, len(order))
	for _, word := range order {
		result = append(result, WordCount{Word: word, Count: counts[word]})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return strings.ToLower(result[i].Word) < strings.ToLower(result[j].Word)
	})
	return result
}

// CorpusDocument is one sweep's whole finding.
//
// It is the one computation behind both renderings: [CorpusDocument.Payload]
// is the machine document the command's JSON envelope carries, and
// [RenderCorpusText] is the human report, so the two can never disagree. The
// Python held this as the payload dictionary itself and rendered out of it;
// here the typed value is the shared origin and the dictionary is derived from
// it.
type CorpusDocument struct {
	// Root is the absolute directory whose subdirectories were searched.
	Root string
	// AcceptListPath is where the accept list was read from.
	AcceptListPath string
	// AcceptedTerms is how many terms the accept list carries.
	AcceptedTerms int
	// WordlistWords is how many words the vendored list carries.
	WordlistWords int
	// Projects are the per-project reports, in discovery order.
	Projects []ProjectSpellReport
	// Total is how many unknown words the whole sweep found.
	Total int
}

// Payload renders the document as the machine payload the sweep carries.
func (d CorpusDocument) Payload() map[string]any {
	projects := make([]any, 0, len(d.Projects))
	for _, report := range d.Projects {
		misspellings := make([]any, 0, len(report.Misspellings))
		for _, miss := range report.Misspellings {
			suggestions := make([]any, 0, len(miss.Suggestions))
			for _, suggestion := range miss.Suggestions {
				suggestions = append(suggestions, suggestion)
			}
			misspellings = append(misspellings, map[string]any{
				"file":        miss.File,
				"line":        miss.Line,
				"column":      miss.Column,
				"word":        miss.Word,
				"suggestions": suggestions,
			})
		}
		var errorValue any
		if report.Error != "" {
			errorValue = report.Error
		}
		projects = append(projects, map[string]any{
			"project":      report.Name,
			"pages":        report.Pages,
			"error":        errorValue,
			"misspellings": misspellings,
		})
	}
	return map[string]any{
		"root":           d.Root,
		"accept_list":    d.AcceptListPath,
		"accepted_terms": d.AcceptedTerms,
		"wordlist_words": d.WordlistWords,
		"projects":       projects,
		"total":          d.Total,
	}
}

// ScanProject spell-checks one project's docs tree and its posts.
//
// vocab is the word list to accept against and accepted is the accept list.
// The report's Error is set instead of results when the project could not be
// read.
func ScanProject(
	project fleet.FleetProject, vocab, accepted spelling.Vocab,
) (ProjectSpellReport, error) {
	if !project.Loaded() {
		return ProjectSpellReport{
			Name: project.Name, Path: project.Path, Error: project.Error,
		}, nil
	}

	declaredDocs := util.PythonStrOrEmpty(project.Config["docs"])
	if declaredDocs == "" {
		declaredDocs = layout.DocsDefault
	}
	docsDir := util.PathJoin(project.Path, strings.TrimRight(declaredDocs, "/"))
	if !isDir(docsDir) {
		return ProjectSpellReport{
			Name: project.Name, Path: project.Path,
			Error: "docs directory not found: " + docsDir,
		}, nil
	}

	postsConfig, _ := project.Config["posts"].(map[string]any)
	postsRel := layout.PostsDefault
	if postsConfig != nil {
		if declared, present := postsConfig["dir"]; present {
			postsRel, _ = declared.(string)
		}
	}
	postsDir := ""
	if postsRel != "" {
		postsDir = util.PathJoin(project.Path, postsRel)
	}

	bodies, err := fleet.LoadDocsBodies(docsDir)
	if err != nil {
		return ProjectSpellReport{
			Name: project.Name, Path: project.Path, Error: err.Error(),
		}, nil
	}
	posts := map[string]fleet.DocBody{}
	if postsDir != "" {
		posts, err = fleet.LoadDocsBodies(postsDir)
		if err != nil {
			return ProjectSpellReport{
				Name: project.Name, Path: project.Path, Error: err.Error(),
			}, nil
		}
	}

	// Posts are keyed by their own path from the project root, matching
	// what `selfdoc check` reports, so a finding names a file a reader can
	// open.
	slice := make(map[string]fleet.DocBody, len(bodies)+len(posts))
	for relPath, payload := range bodies {
		slice[relPath] = payload
	}
	for relPath, payload := range posts {
		slice[util.PathJoin(strings.TrimRight(postsRel, "/"), relPath)] = payload
	}

	report := ProjectSpellReport{
		Name: project.Name, Path: project.Path, Pages: len(slice),
	}
	relPaths := make([]string, 0, len(slice))
	for relPath := range slice {
		relPaths = append(relPaths, relPath)
	}
	sort.Strings(relPaths)
	for _, relPath := range relPaths {
		payload := slice[relPath]
		found, err := spelling.CheckText(
			payload.Body, relPath, vocab, accepted,
			payload.FrontmatterLines, true,
		)
		if err != nil {
			return ProjectSpellReport{}, err
		}
		report.Misspellings = append(report.Misspellings, found...)
	}
	return report, nil
}

// RunSpellCorpus sweeps every selfdoc project under root and returns what it
// found, with the exit code the command terminates on.
//
// root is the directory whose immediate subdirectories are searched for a
// selfdoc.json. The exit code is 1 when any unknown word was found -- a
// misspelling is an error, and the accept list is the sanctioned answer for a
// genuine term -- and 0 on a clean sweep. A project that could not be read is
// reported but does not by itself fail the sweep.
func RunSpellCorpus(root string, handle *effects.Handle) (CorpusDocument, int, error) {
	vocab := spelling.LoadWordlist()
	accepted, err := spelling.LoadAcceptList("")
	if err != nil {
		return CorpusDocument{}, 0, err
	}

	projects, err := fleet.DiscoverFleet(root)
	if err != nil {
		return CorpusDocument{}, 0, err
	}

	reports := make([]ProjectSpellReport, 0, len(projects))
	total := 0
	for _, project := range projects {
		report, err := ScanProject(project, vocab, accepted)
		if err != nil {
			return CorpusDocument{}, 0, err
		}
		total += len(report.Misspellings)
		reports = append(reports, report)
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return CorpusDocument{}, 0, err
	}

	document := CorpusDocument{
		Root:           absoluteRoot,
		AcceptListPath: spelling.AcceptListPath(),
		AcceptedTerms:  len(accepted),
		WordlistWords:  len(vocab),
		Projects:       reports,
		Total:          total,
	}
	exitCode := 0
	if total > 0 {
		exitCode = 1
	}
	return document, exitCode, nil
}

// RenderCorpusText renders a sweep as the human report.
//
// The summary table plus, when detail, each project's unknown words with a
// first location and any suggestion.
func RenderCorpusText(document CorpusDocument, detail bool) string {
	lines := []string{
		fmt.Sprintf(
			"Word list: %d words. Accept list: %d terms (%s).",
			document.WordlistWords, document.AcceptedTerms, document.AcceptListPath,
		),
		"",
		fmt.Sprintf("%-28s %5s %8s %7s", "project", "pages", "flagged", "unique"),
	}
	for _, project := range document.Projects {
		if project.Error != "" {
			lines = append(lines, fmt.Sprintf(
				"%-28s %5s %8s %7s  %s",
				project.Name, "-", "-", "-", project.Error,
			))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"%-28s %5d %8d %7d",
			project.Name, project.Pages,
			len(project.Misspellings), len(project.UniqueWords()),
		))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("total flagged: %d", document.Total))

	if detail {
		for _, project := range document.Projects {
			if len(project.Misspellings) == 0 {
				continue
			}
			lines = append(lines, "")
			lines = append(lines, project.Name+":")
			first := map[string]spelling.Misspelling{}
			for _, miss := range project.Misspellings {
				if _, held := first[miss.Word]; !held {
					first[miss.Word] = miss
				}
			}
			for _, counted := range project.UniqueWords() {
				miss := first[counted.Word]
				where := fmt.Sprintf("%s:%d:%d", miss.File, miss.Line, miss.Column)
				hint := ""
				if len(miss.Suggestions) > 0 {
					hint = "  -> " + strings.Join(miss.Suggestions, ", ")
				}
				lines = append(lines, fmt.Sprintf(
					"  %-24s x%-4d %s%s", counted.Word, counted.Count, where, hint,
				))
			}
		}
	}

	return strings.Join(lines, "\n")
}
