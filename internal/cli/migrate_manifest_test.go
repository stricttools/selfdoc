package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// `selfdoc layout migrate` also carries a repository's committed manifest from
// the schema before the vocabulary to the current one: with the move of the
// directories when the repository is on the previous layout, and alone when it
// is already on this one.

// outdatedManifest is a manifest document as a selfdoc before the vocabulary
// wrote it.
const outdatedManifest = `{
  "schema_version": 1,
  "name": "Test Project",
  "slug": "test-project",
  "version": "1.0.0",
  "description": "",
  "language": "python",
  "base_url": "",
  "pages": [],
  "posts": [],
  "last_gen": "2026-01-01T00:00:00+00:00",
  "theme": "minimal"
}
`

// projectTerms is a terms file accepting one word and rejecting one term.
const projectTerms = `format_version = 1

[[accepted]]
word = "frobnitz"
meaning = "The widget."
aliases = ["frobnitzes"]

[[rejected]]
pattern = "whilst"
kind = "word"
reason = "US English."
`

// committedVocabulary reads the vocabulary object of the manifest committed at
// HEAD, failing unless it is on schema 2.
func committedVocabulary(t *testing.T, dir, rel string) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(gitOutput(t, dir, "show", "HEAD:"+rel)), &document); err != nil {
		t.Fatalf("the committed %s is not JSON: %v", rel, err)
	}
	if document["schema_version"] != float64(2) {
		t.Fatalf("the committed %s declares schema_version %v, want 2", rel, document["schema_version"])
	}
	vocabularyDocument, _ := document["vocabulary"].(map[string]any)
	return vocabularyDocument
}

var wantProjectVocabulary = map[string]any{
	"accepted": []any{map[string]any{"word": "frobnitz", "aliases": []any{"frobnitzes"}}},
	"rejected": []any{map[string]any{"pattern": "whilst", "kind": "word"}},
}

func TestMigrateConvertsTheManifestInTheSameCommitAsTheMove(t *testing.T) {
	isolate(t)
	dir := previousLayoutProject(t, false)
	previous := filepath.Join(dir, layout.PreviousRoot)
	testproject.WriteText(t, filepath.Join(previous, layout.DocsStateName, "manifest.json"), outdatedManifest)
	testproject.WriteText(t, filepath.Join(previous, layout.VocabularyName, layout.ManifestFileName),
		layout.DirectoryManifestContent(layout.Owner))
	testproject.WriteText(t, filepath.Join(previous, layout.VocabularyName, "terms.toml"), projectTerms)
	testproject.Git(t, dir, "add", layout.PreviousRoot)
	testproject.Git(t, dir, "commit", "-q", "-m", "a manifest and a vocabulary")

	dry := run(t, dir, "layout", "migrate", "--dry-run")
	want := "rewrite " + layout.ManifestRel + ": schema_version 1 -> 2"
	if dry.ExitCode != 0 || !strings.Contains(dry.Stdout, want) {
		t.Fatalf("the plan does not carry %q (exit %d):\n%s\n%s", want, dry.ExitCode, dry.Stdout, dry.Stderr)
	}
	if result := run(t, dir, "layout", "migrate"); result.ExitCode != 0 {
		t.Fatalf("the move failed:\n%s\n%s", result.Stdout, result.Stderr)
	}
	if got := committedVocabulary(t, dir, layout.ManifestRel); !reflect.DeepEqual(got, wantProjectVocabulary) {
		t.Errorf("the committed vocabulary is %v, want %v", got, wantProjectVocabulary)
	}
	if status := gitOutput(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Errorf("the move left uncommitted changes:\n%s", status)
	}
	if subjects := gitOutput(t, dir, "log", "-2", "--format=%s"); strings.Count(subjects, "selfdoc layout migrate") != 1 {
		t.Errorf("the move and the conversion are not one commit:\n%s", subjects)
	}
}

// outdatedCurrentLayoutProject is a committed repository already on this
// layout whose committed manifest and post manifest an older selfdoc wrote:
// what a repository moved by the .selfdoc/ move script, or by an unreleased
// selfdoc, holds.
func outdatedCurrentLayoutProject(t *testing.T) string {
	t.Helper()
	dir := testproject.Make(t, nil)
	testproject.WriteText(t, filepath.Join(dir, filepath.FromSlash(layout.ManifestRel)), outdatedManifest)
	testproject.WriteText(t, filepath.Join(dir, filepath.FromSlash(layout.PostManifestRel)), outdatedManifest)
	testproject.WriteText(t, filepath.Join(dir, filepath.FromSlash(layout.TermsRel)), projectTerms)
	testproject.WriteText(t, filepath.Join(dir, "stricttools", "posts", "2026-01-01-hello.md"),
		"---\ntitle = \"Hello\"\ndate = \"2026-01-01\"\n---\n\nHello.\n")
	testproject.Git(t, dir, "init", "-q")
	testproject.Git(t, dir, "add", ".")
	testproject.Git(t, dir, "commit", "-q", "-m", "an outdated manifest")
	return dir
}

// Every reader in the project refuses the outdated manifest, naming the
// conversion; running it clears every refusal.
func TestAnOutdatedManifestIsRefusedNamingMigrateAndMigrateConvertsIt(t *testing.T) {
	isolate(t)
	dir := outdatedCurrentLayoutProject(t)
	for _, argv := range [][]string{
		{"gen", "--no-auto-commit"},
		{"check", "--no-auto-commit"},
	} {
		result := run(t, dir, argv...)
		if result.ExitCode == 0 || !strings.Contains(result.Stderr, "'selfdoc layout migrate'") {
			t.Errorf("%v exited %d without naming the conversion:\n%s", argv, result.ExitCode, result.Stderr)
		}
	}

	before := treeListing(t, dir)
	dry := run(t, dir, "layout", "migrate", "--dry-run")
	for _, want := range []string{
		"rewrite " + layout.ManifestRel + ": schema_version 1 -> 2",
		"rewrite " + layout.PostManifestRel + ": schema_version 1 -> 2",
	} {
		if dry.ExitCode != 0 || !strings.Contains(dry.Stdout, want) {
			t.Errorf("the plan does not carry %q (exit %d):\n%s\n%s", want, dry.ExitCode, dry.Stdout, dry.Stderr)
		}
	}
	if strings.Contains(dry.Stdout, "move "+layout.PreviousRoot) {
		t.Errorf("a repository already on the layout was planned a move:\n%s", dry.Stdout)
	}
	if after := treeListing(t, dir); after != before {
		t.Errorf("the dry run changed the tree")
	}

	if result := run(t, dir, "layout", "migrate"); result.ExitCode != 0 {
		t.Fatalf("the conversion failed:\n%s\n%s", result.Stdout, result.Stderr)
	}
	for _, rel := range []string{layout.ManifestRel, layout.PostManifestRel} {
		if got := committedVocabulary(t, dir, rel); !reflect.DeepEqual(got, wantProjectVocabulary) {
			t.Errorf("the committed %s vocabulary is %v, want %v", rel, got, wantProjectVocabulary)
		}
	}
	for _, argv := range [][]string{
		{"gen", "--no-auto-commit"},
		{"check", "--no-auto-commit"},
	} {
		if result := run(t, dir, argv...); strings.Contains(result.Stderr, "'selfdoc layout migrate'") {
			t.Errorf("%v still refuses after the conversion:\n%s", argv, result.Stderr)
		}
	}

	again := run(t, dir, "layout", "migrate")
	if again.ExitCode == 0 || !strings.Contains(again.Stderr, "Nothing to migrate") {
		t.Errorf("a converted repository: exit %d\n%s", again.ExitCode, again.Stderr)
	}
}

// withoutVocabulary removes the vocabulary directory from a project on this
// layout and commits the removal: the state the .selfdoc/ move script leaves,
// because the layout before .stricttools/ had no vocabulary directory to move.
func withoutVocabulary(t *testing.T, dir string) {
	t.Helper()
	testproject.Git(t, dir, "rm", "-q", "-r", "--ignore-unmatch", layout.VocabularyRel)
	if err := os.RemoveAll(layout.Path(dir, layout.VocabularyRel)); err != nil {
		t.Fatal(err)
	}
	testproject.Git(t, dir, "commit", "-q", "--allow-empty", "-m", "no vocabulary directory")
}

// A repository on this layout with no vocabulary directory -- what the
// .selfdoc/ move script leaves -- ends the way the .stricttools/ move ends it:
// the vocabulary directory's grant and an empty terms file, committed, so
// `selfdoc vocabulary accept` works afterwards. That holds whether or not its
// manifests still need converting.
func TestMigrateGivesAProjectWithNoVocabularyDirectoryOne(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		outdated bool
	}{
		{"with the manifests to convert", true},
		{"with the manifests already converted", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			isolate(t)
			var dir string
			if testCase.outdated {
				dir = outdatedCurrentLayoutProject(t)
			} else {
				dir = testproject.Make(t, nil)
				testproject.Git(t, dir, "init", "-q")
				testproject.Git(t, dir, "add", ".")
				testproject.Git(t, dir, "commit", "-q", "-m", "on this layout")
			}
			withoutVocabulary(t, dir)
			if result := run(t, dir, "vocabulary", "accept", "frobnitz", "--meaning", "The widget.", "--no-auto-commit"); result.ExitCode == 0 {
				t.Fatalf("the fixture has a vocabulary directory selfdoc may write into:\n%s", result.Stdout)
			}

			dry := run(t, dir, "layout", "migrate", "--dry-run")
			for _, want := range []string{
				"write " + layout.DirectoryManifestRel(layout.VocabularyName),
				"write " + layout.TermsRel,
			} {
				if dry.ExitCode != 0 || !strings.Contains(dry.Stdout, want) {
					t.Errorf("the plan does not carry %q (exit %d):\n%s\n%s", want, dry.ExitCode, dry.Stdout, dry.Stderr)
				}
			}
			if result := run(t, dir, "layout", "migrate"); result.ExitCode != 0 {
				t.Fatalf("the migration failed:\n%s\n%s", result.Stdout, result.Stderr)
			}
			if got := gitOutput(t, dir, "show", "HEAD:"+layout.DirectoryManifestRel(layout.VocabularyName)); got != layout.DirectoryManifestContent(layout.Owner) {
				t.Errorf("the committed vocabulary grant is %q", got)
			}
			if got := gitOutput(t, dir, "show", "HEAD:"+layout.TermsRel); got != vocabulary.EmptyTerms {
				t.Errorf("the committed terms file is %q, want the empty vocabulary", got)
			}
			if status := gitOutput(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
				t.Errorf("the migration left uncommitted changes:\n%s", status)
			}
			if result := run(t, dir, "vocabulary", "accept", "frobnitz", "--meaning", "The widget.", "--no-auto-commit"); result.ExitCode != 0 {
				t.Errorf("vocabulary accept still refuses after the migration:\n%s", result.Stderr)
			}
		})
	}
}
