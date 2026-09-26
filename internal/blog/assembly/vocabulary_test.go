package assembly

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// A deploy puts a project's vocabulary on a site other projects already
// publish to. Two vocabularies that disagree about a word -- one project's
// rejected pattern covering a word another project, or selfdoc's baseline,
// accepts -- are refused before anything is written, naming both projects, the
// word, the pattern and the fixes.

// withVocabulary returns a manifest document carrying the given vocabulary.
func withVocabulary(document map[string]any, accepted []any, rejected []any) map[string]any {
	if accepted == nil {
		accepted = []any{}
	}
	if rejected == nil {
		rejected = []any{}
	}
	document["vocabulary"] = map[string]any{"accepted": accepted, "rejected": rejected}
	return document
}

func acceptedWord(word string) any {
	return map[string]any{"word": word, "aliases": []any{}}
}

func rejectedPattern(pattern, kind string) any {
	return map[string]any{"pattern": pattern, "kind": kind}
}

// requireConflict asserts err is the cross-project refusal and names every
// fragment.
func requireConflict(t *testing.T, err error, fragments ...string) {
	t.Helper()
	var conflicts *vocabulary.ConflictsError
	if !errors.As(err, &conflicts) {
		t.Fatalf("err = %v, want the vocabulary conflict refusal", err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("the refusal does not name %q:\n%s", fragment, err)
		}
	}
}

func TestIntegrateRefusesAPatternCoveringAnotherProjectsWordAndTheFixClearsIt(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.WriteJSON("manifests/beta.json", withVocabulary(
		integrateManifest("beta", "Beta", "2.0.0", nil), []any{acceptedWord("gizmo")}, nil))
	tree.Commit()
	tree.WriteJSON("source/alpha/stricttools/.docs-state/manifest.json", withVocabulary(
		integrateManifest("alpha", "Alpha", "1.0.0", nil), nil, []any{rejectedPattern("gizmo", "word")}))

	_, err := tree.Integrate(nil)
	requireConflict(t, err, "alpha rejects the word \"gizmo\"", "a word beta accepts",
		"'selfdoc vocabulary remove gizmo'", "'selfdoc vocabulary reject <word> --kind word --reason <text>'",
		"remove the word in beta")
	if got := tree.ReadJSON("manifests/alpha.json")["version"]; got != "0.9.0" {
		t.Errorf("the refused deploy wrote alpha's manifest: version %v", got)
	}
	if tree.Pushes() != 0 {
		t.Errorf("the refused deploy pushed")
	}

	// The fix in alpha: remove the rejection, which is what regenerating the
	// manifest after 'selfdoc vocabulary remove gizmo' publishes.
	tree.WriteJSON("source/alpha/stricttools/.docs-state/manifest.json", withVocabulary(
		integrateManifest("alpha", "Alpha", "1.0.0", nil), nil, nil))
	tree.MustIntegrate(nil)
}

func TestIntegrateRefusesAnotherProjectsPatternCoveringTheIncomingWord(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.WriteJSON("manifests/beta.json", withVocabulary(
		integrateManifest("beta", "Beta", "2.0.0", nil), nil, []any{rejectedPattern("ify", "suffix")}))
	tree.Commit()
	tree.WriteJSON("source/alpha/stricttools/.docs-state/manifest.json", withVocabulary(
		integrateManifest("alpha", "Alpha", "1.0.0", nil), []any{acceptedWord("gizmify")}, nil))

	_, err := tree.Integrate(nil)
	requireConflict(t, err, "beta rejects the suffix \"ify\"", "\"gizmify\", a word alpha accepts",
		"'selfdoc vocabulary remove gizmify'")

	// The fix in alpha: remove the word.
	tree.WriteJSON("source/alpha/stricttools/.docs-state/manifest.json", withVocabulary(
		integrateManifest("alpha", "Alpha", "1.0.0", nil), nil, nil))
	tree.MustIntegrate(nil)
}

func TestIntegrateRefusesAPatternCoveringABaselineWord(t *testing.T) {
	tree := newAssemblyTree(t)
	tree.WriteJSON("source/alpha/stricttools/.docs-state/manifest.json", withVocabulary(
		integrateManifest("alpha", "Alpha", "1.0.0", nil), nil, []any{rejectedPattern("treeish", "word")}))
	_, err := tree.Integrate(nil)
	requireConflict(t, err, "alpha rejects the word \"treeish\"", vocabulary.BaselineSource,
		"'selfdoc vocabulary remove treeish'")
}

// The documentation publisher takes the other projects' vocabularies from its
// caller and refuses the same way, before the commit.
func TestPublishRefusesAVocabularyConflictBeforeCommitting(t *testing.T) {
	gh := publishFixture(t, nil)
	manifestPath := writeManifestFile(t, withVocabulary(
		map[string]any{"schema_version": 2, "slug": "alpha", "version": "1.0.0"},
		nil, []any{rejectedPattern("gizmo", "word")}))
	beta := vocabulary.Published{Project: "beta", Accepted: []vocabulary.Accepted{{Word: "gizmo"}}}
	_, err := PublishProjectDocs(PublishOptions{
		Repo: testRepo, Slug: "alpha", OutputDir: buildTree(t), Version: "1.0.0",
		ManifestPath: manifestPath, Peers: []vocabulary.Published{beta},
	}, effects.Unbound())
	requireConflict(t, err, "alpha rejects the word \"gizmo\"", "a word beta accepts")
	if gh.Commits() != 0 {
		t.Errorf("the refused publish committed")
	}
	if _, err := PublishProjectDocs(PublishOptions{
		Repo: testRepo, Slug: "alpha", OutputDir: buildTree(t), Version: "1.0.0",
		ManifestPath: manifestPath, Peers: nil,
	}, effects.Unbound()); err != nil {
		t.Errorf("the publish with no disagreeing project refused: %v", err)
	}
}

// writeManifestFile writes a manifest document to a file of its own and
// returns the path.
func writeManifestFile(t *testing.T, document map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}
