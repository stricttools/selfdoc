package cli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/stricttools/selfdoc/internal/blog/assembly/fakegh"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/cli/faketool"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// `selfdoc assembly republish-all` publishes every project on the roster again
// from local checkouts, in one pass, and sends one deploy request. The fake gh
// here is a repository that keeps what the run wrote, so a test can read back
// the manifests the site holds afterwards.

// republishSite is a fake assembly repository and the checkouts publishing to
// it.
type republishSite struct {
	t *testing.T
	// state is the fake gh's state directory.
	state string
	// home and alpha are the checkouts of the roster's two projects.
	home, alpha string
}

// workflowPinning is a deploy workflow installing the given selfdoc.
func workflowPinning(version string) string {
	return "jobs:\n  deploy:\n    steps:\n      - run: |\n          go install github.com/stricttools/selfdoc@v" + version + "\n"
}

// newRepublishSite installs the fake gh holding a roster of home and alpha,
// with its deploy workflow pinning pin, and writes both checkouts.
func newRepublishSite(t *testing.T, pin string, roster []site.RosterEntry) *republishSite {
	t.Helper()
	bin := isolate(t)
	toolState := t.TempDir()
	installFakeTool(t, filepath.Join(bin, "pagefind"))
	t.Setenv(faketool.StateEnv, toolState)
	if err := os.Link(fakeRepoGHBinary, filepath.Join(bin, "gh")); err != nil {
		data, readErr := os.ReadFile(fakeRepoGHBinary)
		if readErr != nil {
			t.Fatalf("reading the fake gh: %v", readErr)
		}
		if err := os.WriteFile(filepath.Join(bin, "gh"), data, 0o755); err != nil {
			t.Fatalf("installing the fake gh: %v", err)
		}
	}
	state := t.TempDir()
	t.Setenv(fakegh.StateEnv, state)
	if roster == nil {
		roster = []site.RosterEntry{{Slug: "home", Repo: "owner/home"}, {Slug: "alpha", Repo: "owner/alpha"}}
	}
	s := &republishSite{t: t, state: state}
	s.seed(map[string]string{
		site.RosterPath:   site.RenderRoster(roster, "home"),
		site.WorkflowPath: workflowPinning(pin),
		// What an older selfdoc published: manifests without a vocabulary,
		// which the site's reader refuses until this run replaces them.
		"manifests/alpha.json": `{"schema_version": 1, "slug": "alpha", "name": "Alpha"}`,
		"manifests/home.json":  `{"schema_version": 1, "slug": "home", "name": "Home"}`,
	})

	s.home = homeSiteProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly", "pages_project": "site"},
		"topology": map[string]any{"slug": "home", "docs_base": "https://example.com"},
	})
	writeManifestDocument(t, s.home, "home", "Home", []any{map[string]any{"word": "gizmo", "aliases": []any{}}}, nil)
	commitAll(t, s.home)

	s.alpha = projectCheckout(t, "alpha", "")
	return s
}

// projectCheckout is a committed project checkout declaring slug, with the
// given terms file (none when empty) and the manifest 'selfdoc gen' writes
// from it.
func projectCheckout(t *testing.T, slug, terms string) string {
	t.Helper()
	dir := testproject.Make(t, map[string]any{
		"name":     strings.ToUpper(slug[:1]) + slug[1:],
		"topology": map[string]any{"slug": slug},
		"assembly": map[string]any{"repo": "owner/assembly"},
	})
	if terms != "" {
		writeText(t, filepath.Join(dir, filepath.FromSlash(layout.TermsRel)), terms)
	}
	if result := run(t, dir, "gen", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen in %s failed:\n%s\n%s", slug, result.Stdout, result.Stderr)
	}
	commitAll(t, dir)
	return dir
}

// writeManifestDocument writes a project's manifest by hand, on the current
// schema: what 'selfdoc gen' writes for a project whose own gen reaches the
// assembly.
func writeManifestDocument(t *testing.T, dir, slug, name string, accepted, rejected []any) {
	t.Helper()
	if accepted == nil {
		accepted = []any{}
	}
	if rejected == nil {
		rejected = []any{}
	}
	testproject.WriteJSON(t, filepath.Join(dir, filepath.FromSlash(layout.ManifestRel)), map[string]any{
		"schema_version": 2, "name": name, "slug": slug, "version": "0.0.0",
		"description": name + " docs", "language": "", "base_url": "https://example.com",
		"pages": []any{map[string]any{"path": "index.md", "title": name, "type": "doc", "headings": []any{}}},
		"posts": []any{}, "last_gen": "2026-01-01T00:00:00+00:00", "theme": "minimal",
		"vocabulary": map[string]any{"accepted": accepted, "rejected": rejected},
	})
}

// commitAll commits everything in a checkout, initializing the repository
// first when it has none.
func commitAll(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		testproject.Git(t, dir, "init", "-q")
	}
	testproject.Git(t, dir, "add", "-A")
	testproject.Git(t, dir, "commit", "-q", "--allow-empty", "-m", "fixture")
}

// seed writes files into the fake repository.
func (s *republishSite) seed(files map[string]string) {
	s.t.Helper()
	blobs := s.blobs()
	for path, content := range files {
		blobs[path] = base64.StdEncoding.EncodeToString([]byte(content))
	}
	data, err := json.Marshal(blobs)
	if err != nil {
		s.t.Fatalf("encoding the fake repository: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.state, "blobs.json"), data, 0o644); err != nil {
		s.t.Fatalf("writing the fake repository: %v", err)
	}
}

// blobs is the fake repository's file table, base64 per path.
func (s *republishSite) blobs() map[string]string {
	s.t.Helper()
	table := map[string]string{}
	data, err := os.ReadFile(filepath.Join(s.state, "blobs.json"))
	if os.IsNotExist(err) {
		return table
	}
	if err != nil {
		s.t.Fatalf("reading the fake repository: %v", err)
	}
	if err := json.Unmarshal(data, &table); err != nil {
		s.t.Fatalf("decoding the fake repository: %v", err)
	}
	return table
}

// siteManifests reads the manifests the fake repository holds the way the
// site's deploy reads them.
func (s *republishSite) siteManifests() error {
	s.t.Helper()
	documents := map[string][]byte{}
	for path, encodedContent := range s.blobs() {
		name, found := strings.CutPrefix(path, "manifests/")
		if !found {
			continue
		}
		content, err := base64.StdEncoding.DecodeString(encodedContent)
		if err != nil {
			s.t.Fatalf("decoding %s: %v", path, err)
		}
		documents[name] = content
	}
	_, err := site.ManifestsFromDocuments(documents, "manifests")
	return err
}

// calls are the fake gh's recorded invocations, joined.
func (s *republishSite) calls() []string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.state, "calls.jsonl"))
	if err != nil {
		return nil
	}
	var joined []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call fakegh.CallRecord
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			s.t.Fatalf("decoding a recorded call: %v", err)
		}
		joined = append(joined, call.Joined())
	}
	return joined
}

// count is how many recorded calls contain needle.
func (s *republishSite) count(needle string) int {
	found := 0
	for _, call := range s.calls() {
		if strings.Contains(call, needle) {
			found++
		}
	}
	return found
}

// republish runs the command over the site's two checkouts.
func (s *republishSite) republish(extra ...string) strictcli.Result {
	s.t.Helper()
	argv := append([]string{"assembly", "republish-all", "--home", s.home, "--repo", s.alpha}, extra...)
	return run(s.t, s.alpha, argv...)
}

func TestRepublishAllDryRunBuildsAndChecksAndPublishesNothing(t *testing.T) {
	s := newRepublishSite(t, testVersion, nil)
	result := s.republish("--dry-run")
	if result.ExitCode != 0 {
		t.Fatalf("the dry run failed: exit %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	for _, want := range []string{"Dry run", "owner/assembly", "alpha 1.0.0:", "home", "the home project", "Nothing was published."} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("the dry run does not print %q:\n%s", want, result.Stdout)
		}
	}
	if s.count("/git/blobs") != 0 || s.count("/dispatches") != 0 {
		t.Errorf("the dry run published or dispatched: %v", s.calls())
	}
	if !exists(filepath.Join(s.alpha, "stricttools", ".docs-cache", "build", "index.html")) {
		t.Error("the dry run did not build alpha")
	}
}

// The site's reader refuses the manifests an older selfdoc published, naming
// this command; running it replaces them, and the reader accepts the site.
func TestRepublishAllReplacesEveryManifestAndSendsOneDeployRequest(t *testing.T) {
	s := newRepublishSite(t, testVersion, nil)
	before := s.siteManifests()
	if before == nil || !strings.Contains(before.Error(), "selfdoc assembly republish-all") {
		t.Fatalf("the site's reader did not refuse the old manifests naming the republish: %v", before)
	}

	result := s.republish()
	if result.ExitCode != 0 {
		t.Fatalf("the republish failed: exit %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if err := s.siteManifests(); err != nil {
		t.Errorf("the site's reader still refuses after the republish: %v", err)
	}
	if got := s.count("/dispatches"); got != 1 {
		t.Errorf("the republish sent %d deploy request(s), want one", got)
	}
	if got := s.count("--method PATCH /repos/owner/assembly/git/refs/heads/main"); got != 2 {
		t.Errorf("the republish moved the branch %d time(s), want one commit per project", got)
	}
	calls := strings.Join(s.calls(), "\n")
	if strings.Index(calls, "/dispatches") < strings.LastIndex(calls, "/git/refs/heads/main") {
		t.Errorf("the deploy request was sent before the last publish")
	}
}

func TestRepublishAllRefusesAnOutdatedManifestAndMigrateClearsIt(t *testing.T) {
	s := newRepublishSite(t, testVersion, nil)
	writeText(t, filepath.Join(s.alpha, filepath.FromSlash(layout.ManifestRel)), outdatedManifest)
	commitAll(t, s.alpha)

	result := s.republish("--dry-run")
	if result.ExitCode == 0 {
		t.Fatal("an outdated manifest was republished")
	}
	for _, want := range []string{s.alpha, "'selfdoc layout migrate'", "nothing was built"} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, result.Stderr)
		}
	}
	if exists(filepath.Join(s.alpha, "stricttools", ".docs-cache", "build")) {
		t.Error("the refused run built alpha")
	}

	if migrated := run(t, s.alpha, "layout", "migrate"); migrated.ExitCode != 0 {
		t.Fatalf("the conversion failed:\n%s\n%s", migrated.Stdout, migrated.Stderr)
	}
	// The hand-written manifest named another project; regenerate it the way
	// the project would after the conversion.
	if result := run(t, s.alpha, "gen", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen after the conversion failed:\n%s", result.Stderr)
	}
	if result := s.republish("--dry-run"); result.ExitCode != 0 {
		t.Errorf("the dry run after the conversion failed:\n%s", result.Stderr)
	}
}

func TestRepublishAllRefusesCheckoutsThatAreNotTheRoster(t *testing.T) {
	s := newRepublishSite(t, testVersion, []site.RosterEntry{
		{Slug: "home", Repo: "owner/home"}, {Slug: "alpha", Repo: "owner/alpha"}, {Slug: "beta", Repo: "owner/beta"},
	})
	result := s.republish("--dry-run")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "add --repo <checkout of beta>") {
		t.Fatalf("a roster project with no checkout: exit %d\n%s", result.ExitCode, result.Stderr)
	}
	beta := projectCheckout(t, "beta", "")
	fixed := run(t, s.alpha, "assembly", "republish-all", "--home", s.home, "--repo", s.alpha, "--repo", beta, "--dry-run")
	if fixed.ExitCode != 0 {
		t.Errorf("with beta's checkout added: exit %d\n%s", fixed.ExitCode, fixed.Stderr)
	}

	swapped := run(t, s.alpha, "assembly", "republish-all", "--home", s.alpha, "--repo", s.home, "--repo", beta, "--dry-run")
	if swapped.ExitCode == 0 || !strings.Contains(swapped.Stderr, "pass the checkout of 'home' as --home") {
		t.Errorf("a --home that is not the roster's home: exit %d\n%s", swapped.ExitCode, swapped.Stderr)
	}
}

func TestRepublishAllRefusesAnOlderDeployPinAndSyncWorkflowClearsIt(t *testing.T) {
	s := newRepublishSite(t, "9.9.8", nil)
	result := s.republish("--dry-run")
	want := "'selfdoc assembly sync-workflow --pin-selfdoc " + testVersion + "'"
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, want) {
		t.Fatalf("an older pin: exit %d, want the refusal naming %s\n%s", result.ExitCode, want, result.Stderr)
	}
	synced := runWith(t, Options{Dir: s.home, Registry: stubRegistry()},
		"assembly", "sync-workflow", "--pin-selfdoc", testVersion, "--pin-pagefind", "1.4.0")
	if synced.ExitCode != 0 {
		t.Fatalf("sync-workflow failed:\n%s\n%s", synced.Stdout, synced.Stderr)
	}
	if result := s.republish("--dry-run"); result.ExitCode != 0 {
		t.Errorf("the dry run after moving the pin failed:\n%s", result.Stderr)
	}
}

// Two projects on one site whose vocabularies disagree are refused before
// anything is built, every conflict listed; the fix the refusal names clears
// it.
func TestRepublishAllRefusesVocabularyConflictsAndTheFixClearsThem(t *testing.T) {
	s := newRepublishSite(t, testVersion, nil)
	s.alpha = projectCheckout(t, "alpha", `format_version = 1

[[rejected]]
pattern = "gizmo"
kind = "word"
reason = "The project's own name for it is widget."

[[rejected]]
pattern = "treeish"
kind = "word"
reason = "Say tree-like."
`)
	result := s.republish("--dry-run")
	if result.ExitCode == 0 {
		t.Fatal("disagreeing vocabularies were republished")
	}
	for _, want := range []string{
		"alpha rejects the word \"gizmo\"", "a word home accepts",
		"alpha rejects the word \"treeish\"", "selfdoc's built-in baseline",
		"'selfdoc vocabulary remove gizmo'", "'selfdoc vocabulary reject <word> --kind word --reason <text>'",
	} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, result.Stderr)
		}
	}
	if exists(filepath.Join(s.alpha, "stricttools", ".docs-cache", "build")) {
		t.Error("the refused run built alpha")
	}

	for _, pattern := range []string{"gizmo", "treeish"} {
		if removed := run(t, s.alpha, "vocabulary", "remove", pattern, "--no-auto-commit"); removed.ExitCode != 0 {
			t.Fatalf("removing %s failed:\n%s", pattern, removed.Stderr)
		}
	}
	if result := run(t, s.alpha, "gen", "--no-auto-commit"); result.ExitCode != 0 {
		t.Fatalf("gen after the fix failed:\n%s", result.Stderr)
	}
	if result := s.republish("--dry-run"); result.ExitCode != 0 {
		t.Errorf("the dry run after the fix failed:\n%s", result.Stderr)
	}
}
