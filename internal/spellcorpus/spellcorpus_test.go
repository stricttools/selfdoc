package spellcorpus

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/fleet"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/smm-h/stricttest/go/hygiene"
)

// isolate binds the test-environment isolation floor. It also points the
// spelling engine's accept list at a throwaway HOME, so a term this machine
// happens to have accepted cannot change a verdict here.
func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// handle is the effects handle every call runs under: unbound, so the fleet
// enumeration's scratch copies really happen.
func handle() *effects.Handle { return effects.Unbound() }

// write writes content to path, creating its directory.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// cleanPage is a page of ordinary prose with nothing misspelled in it.
const cleanPage = "+++\ntitle = \"Home\"\ndescription = \"" +
	"A page of ordinary prose that says something concrete about the " +
	"project and its documentation for the reader.\"\n+++\n\n" +
	"# Home\n\nThis page is spelled correctly.\n"

// corpusProject writes a project named name under root, whose docs tree holds
// the given pages.
func corpusProject(t *testing.T, root, name string, pages map[string]string) string {
	t.Helper()
	projectDir := filepath.Join(root, name)
	projectConfig := map[string]any{
		"source":        []any{map[string]any{"path": "src/", "language": "python"}},
		"base_url":      "https://example.com",
		"version":       "1.0.0",
		"versions":      []any{map[string]any{"version": "1.0.0"}},
		"locales":       []any{map[string]any{"code": "en", "label": "English", "default": true}},
		"search_engine": "pagefind",
		"author":        map[string]any{"name": "Test Author", "url": "https://author.example"},
		"docs":          ".stricttools/docs/",
		"output":        ".stricttools/docs-cache/build/",
	}
	encoded, err := json.Marshal(projectConfig)
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	write(t, filepath.Join(projectDir, "selfdoc.json"), string(encoded))
	write(t, filepath.Join(projectDir, "src", "__init__.py"), `"""Example package."""`+"\n")
	write(t, filepath.Join(projectDir, ".stricttools", "docs", ".keep"), "")
	for relPath, content := range pages {
		write(t, filepath.Join(projectDir, ".stricttools", "docs", relPath), content)
	}
	return projectDir
}

func TestScanProjectReportsPerProjectFindings(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	corpusProject(t, root, "alpha", map[string]string{
		"index.md": strings.ReplaceAll(cleanPage, "spelled correctly", "spelled correclty"),
	})

	found, err := fleet.DiscoverFleet(root)
	if err != nil {
		t.Fatalf("DiscoverFleet: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("projects = %d, want 1", len(found))
	}
	report, err := ScanProject(found[0], spelling.LoadWordlist(), spelling.Vocab{})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if report.Pages != 1 {
		t.Errorf("pages = %d, want 1", report.Pages)
	}
	words := make([]string, 0, len(report.UniqueWords()))
	for _, counted := range report.UniqueWords() {
		words = append(words, counted.Word)
	}
	if len(words) != 1 || words[0] != "correclty" {
		t.Errorf("unknown words = %v, want [correclty]", words)
	}
}

func TestScanProjectReportsAnUnreadableProjectWithoutFailing(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	write(t, filepath.Join(root, "broken", "selfdoc.json"), "{ not json")

	found, err := fleet.DiscoverFleet(root)
	if err != nil {
		t.Fatalf("DiscoverFleet: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("projects = %d, want 1", len(found))
	}
	report, err := ScanProject(found[0], spelling.LoadWordlist(), spelling.Vocab{})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if report.Error == "" {
		t.Error("a project that cannot be read reported no error")
	}
	if len(report.Misspellings) != 0 {
		t.Errorf("misspellings = %v, want none", report.Misspellings)
	}
}

func TestScanProjectReportsAMissingDocsDirectory(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	projectDir := corpusProject(t, root, "alpha", nil)
	if err := os.RemoveAll(filepath.Join(projectDir, ".stricttools", "docs")); err != nil {
		t.Fatalf("remove the docs tree: %v", err)
	}

	found, err := fleet.DiscoverFleet(root)
	if err != nil {
		t.Fatalf("DiscoverFleet: %v", err)
	}
	report, err := ScanProject(found[0], spelling.LoadWordlist(), spelling.Vocab{})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if !strings.Contains(report.Error, "docs directory not found") {
		t.Errorf("error = %q", report.Error)
	}
}

func TestScanProjectSurveysPostsAtTheirOwnPaths(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	projectDir := corpusProject(t, root, "alpha", map[string]string{"index.md": cleanPage})
	// A draft is surveyed too: a draft's prose is still prose, and a term
	// it introduces belongs on the accept list before the draft ships.
	write(t, filepath.Join(projectDir, ".stricttools", "posts", "hello.md"),
		"+++\ntitle = \"Hello\"\ndate = 2024-01-15\ndraft = true\ndirectives = false\n+++\n"+
			"This post says correclty.\n")

	found, err := fleet.DiscoverFleet(root)
	if err != nil {
		t.Fatalf("DiscoverFleet: %v", err)
	}
	report, err := ScanProject(found[0], spelling.LoadWordlist(), spelling.Vocab{})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if report.Pages != 2 {
		t.Errorf("pages = %d, want 2 (the docs page and the post)", report.Pages)
	}
	if len(report.Misspellings) != 1 {
		t.Fatalf("misspellings = %v, want one", report.Misspellings)
	}
	want := filepath.Join(".stricttools", "posts", "hello.md")
	if report.Misspellings[0].File != want {
		t.Errorf("file = %q, want %q", report.Misspellings[0].File, want)
	}
}

func TestCorpusRunExitsNonZeroWhenAWordIsFlagged(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	corpusProject(t, root, "alpha", map[string]string{
		"index.md": strings.ReplaceAll(cleanPage, "spelled correctly", "spelled correclty"),
	})

	document, exitCode, err := RunSpellCorpus(root, handle())
	if err != nil {
		t.Fatalf("RunSpellCorpus: %v", err)
	}
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(RenderCorpusText(document, true), "correclty") {
		t.Errorf("report = %q", RenderCorpusText(document, true))
	}
}

func TestCorpusRunExitsZeroOnACleanSweep(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	corpusProject(t, root, "alpha", map[string]string{"index.md": cleanPage})

	document, exitCode, err := RunSpellCorpus(root, handle())
	if err != nil {
		t.Fatalf("RunSpellCorpus: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0: %v", exitCode, document.Projects)
	}
	if !strings.Contains(RenderCorpusText(document, true), "total flagged: 0") {
		t.Errorf("report = %q", RenderCorpusText(document, true))
	}
}

func TestCorpusDocumentIsMachineReadable(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	corpusProject(t, root, "alpha", map[string]string{
		"index.md": strings.ReplaceAll(cleanPage, "spelled correctly", "spelled correclty"),
	})

	document, _, err := RunSpellCorpus(root, handle())
	if err != nil {
		t.Fatalf("RunSpellCorpus: %v", err)
	}
	payload := document.Payload()

	if payload["total"] != 1 {
		t.Errorf("total = %v, want 1", payload["total"])
	}
	projects, _ := payload["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("projects = %v", payload["projects"])
	}
	entry, _ := projects[0].(map[string]any)
	if entry["project"] != "alpha" {
		t.Errorf("project = %v, want alpha", entry["project"])
	}
	if entry["error"] != nil {
		t.Errorf("error = %v, want null", entry["error"])
	}
	misspellings, _ := entry["misspellings"].([]any)
	if len(misspellings) != 1 {
		t.Fatalf("misspellings = %v", entry["misspellings"])
	}
	found, _ := misspellings[0].(map[string]any)
	if found["word"] != "correclty" {
		t.Errorf("word = %v, want correclty", found["word"])
	}
	if found["file"] != "index.md" {
		t.Errorf("file = %v, want index.md", found["file"])
	}
	if line, _ := found["line"].(int); line < 1 {
		t.Errorf("line = %v, want at least 1", found["line"])
	}
	if column, _ := found["column"].(int); column < 1 {
		t.Errorf("column = %v, want at least 1", found["column"])
	}
	if _, isList := found["suggestions"].([]any); !isList {
		t.Errorf("suggestions = %T, want a list", found["suggestions"])
	}
	// The payload has to encode: the command writes it into a JSON
	// envelope.
	if _, err := json.Marshal(payload); err != nil {
		t.Errorf("the payload does not encode: %v", err)
	}
}

func TestCorpusTextRenderingReadsTheSameDocument(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	corpusProject(t, root, "alpha", map[string]string{
		"index.md": strings.ReplaceAll(cleanPage, "spelled correctly", "spelled correclty"),
	})

	document, _, err := RunSpellCorpus(root, handle())
	if err != nil {
		t.Fatalf("RunSpellCorpus: %v", err)
	}

	detailed := RenderCorpusText(document, true)
	for _, fragment := range []string{
		"alpha", "total flagged: 1", "correclty", "index.md:",
		"project", "pages", "flagged", "unique",
	} {
		if !strings.Contains(detailed, fragment) {
			t.Errorf("the detailed report does not carry %q:\n%s", fragment, detailed)
		}
	}

	summary := RenderCorpusText(document, false)
	if strings.Contains(summary, "correclty") {
		t.Errorf("the summary report carries the detail block:\n%s", summary)
	}
	if !strings.Contains(summary, "total flagged: 1") {
		t.Errorf("the summary report does not carry the total:\n%s", summary)
	}
}

func TestCorpusReportRowShape(t *testing.T) {
	document := CorpusDocument{
		Root:           "/root",
		AcceptListPath: "/accept.txt",
		AcceptedTerms:  3,
		WordlistWords:  170000,
		Projects: []ProjectSpellReport{
			{Name: "alpha", Pages: 4, Misspellings: []spelling.Misspelling{
				{File: "a.md", Line: 2, Column: 5, Word: "teh",
					Suggestions: []string{"the", "ten"}},
				{File: "b.md", Line: 9, Column: 1, Word: "teh"},
			}},
			{Name: "broken", Error: "could not be read"},
		},
		Total: 2,
	}

	lines := strings.Split(RenderCorpusText(document, true), "\n")
	want := []string{
		"Word list: 170000 words. Accept list: 3 terms (/accept.txt).",
		"",
		"project                      pages  flagged  unique",
		"alpha                            4        2       1",
		"broken                           -        -       -  could not be read",
		"",
		"total flagged: 2",
		"",
		"alpha:",
		"  teh                      x2    a.md:2:5  -> the, ten",
	}
	if len(lines) != len(want) {
		t.Fatalf("report has %d lines, want %d:\n%s",
			len(lines), len(want), strings.Join(lines, "\n"))
	}
	for index, expected := range want {
		if lines[index] != expected {
			t.Errorf("line %d = %q, want %q", index, lines[index], expected)
		}
	}
}

func TestCorpusRunWritesNothingIntoTheProjects(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	projectDir := corpusProject(t, root, "alpha", map[string]string{"index.md": cleanPage})

	before := treeOf(t, projectDir)
	if _, _, err := RunSpellCorpus(root, handle()); err != nil {
		t.Fatalf("RunSpellCorpus: %v", err)
	}
	after := treeOf(t, projectDir)

	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("the sweep changed the project tree:\nbefore %v\nafter  %v",
			before, after)
	}
}

// treeOf lists every file under dir, relative to it, sorted.
func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(walked string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relPath, relErr := filepath.Rel(dir, walked)
		if relErr != nil {
			return relErr
		}
		found = append(found, relPath)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(found)
	return found
}

func TestUniqueWordsOrdering(t *testing.T) {
	report := ProjectSpellReport{Misspellings: []spelling.Misspelling{
		{Word: "beta"}, {Word: "Alpha"}, {Word: "beta"}, {Word: "gamma"},
		{Word: "Alpha"}, {Word: "beta"},
	}}
	got := report.UniqueWords()
	want := []WordCount{
		{Word: "beta", Count: 3},
		{Word: "Alpha", Count: 2},
		{Word: "gamma", Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
	for index, expected := range want {
		if got[index] != expected {
			t.Errorf("entry %d = %v, want %v", index, got[index], expected)
		}
	}
}

// TestScanProjectFallsBackToTheLayoutDocsRoot pins the docs root a project
// that declares no "docs" key is surveyed at: the layout's own default, the
// same directory every other reader falls back to, not the path selfdoc used
// before the layout moved under one hidden directory.
func TestScanProjectFallsBackToTheLayoutDocsRoot(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	projectDir := corpusProject(t, root, "alpha", map[string]string{
		"index.md": strings.ReplaceAll(cleanPage, "spelled correctly", "spelled correclty"),
	})

	undeclared := fleet.FleetProject{
		Name: "alpha",
		Path: projectDir,
		Config: config.Config{
			"source": []any{map[string]any{"path": "src/", "language": "python"}},
		},
	}
	report, err := ScanProject(undeclared, spelling.LoadWordlist(), spelling.Vocab{})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if report.Error != "" {
		t.Fatalf("error = %q, want the layout's docs root surveyed", report.Error)
	}
	if report.Pages != 1 {
		t.Errorf("pages = %d, want 1", report.Pages)
	}
}
