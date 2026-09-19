package cli

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// assemblyProject creates a project with the given config overrides applied
// over the shape every assembly command reads.
func assemblyProject(t *testing.T, overrides map[string]any) string {
	t.Helper()
	return postProject(t, overrides)
}

// stubRegistry answers both pin registries: PyPI serves one published
// pagefind release, and the module proxy serves every selfdoc version.
func stubRegistry() assembly.Registry {
	return assembly.Registry{
		PyPI: func(pkg string) (map[string]any, error) {
			return map[string]any{
				"info": map[string]any{"version": "1.4.0"},
				"releases": map[string]any{
					"1.4.0": []any{map[string]any{"filename": "pagefind-1.4.0.tar.gz"}},
				},
			}, nil
		},
		GoModule: func(version string) (bool, error) { return true, nil },
	}
}

// encoded is what a contents read sees on stdout: the remote reader asks gh
// for the "content" member alone, so the reply is the base64 document.
func encoded(text string) string {
	return base64.StdEncoding.EncodeToString([]byte(text)) + "\n"
}

// -- refusals before anything reaches the network ---------------------------

func TestAssemblyCommandsRefuseWithoutAConfig(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	for _, command := range []string{"init", "push", "status", "rebuild"} {
		result := run(t, dir, "assembly", command)
		if result.ExitCode != 1 {
			t.Errorf("assembly %s exited %d with no config", command, result.ExitCode)
		}
		if !strings.Contains(result.Stderr, "No selfdoc.json") {
			t.Errorf("assembly %s: the refusal is not the missing config's: %s", command, result.Stderr)
		}
	}
}

func TestAssemblyCommandsRefuseWithoutAnAssemblyRepo(t *testing.T) {
	isolate(t)
	dir := assemblyProject(t, nil)
	for _, command := range []string{"init", "push", "status", "rebuild"} {
		result := run(t, dir, "assembly", command)
		if result.ExitCode != 1 {
			t.Errorf("assembly %s exited %d with no assembly.repo", command, result.ExitCode)
		}
		if !strings.Contains(result.Stderr, "assembly.repo not configured") {
			t.Errorf("assembly %s: the refusal does not name the key: %s", command, result.Stderr)
		}
	}
}

func TestAssemblyInitRequiresAPagesProject(t *testing.T) {
	isolate(t)
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "myproject", "docs_base": "https://docs.example.com"},
	})
	result := run(t, dir, "assembly", "init")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "pages_project") {
		t.Errorf("the refusal does not name the key: %s", result.Stderr)
	}
}

func TestAssemblyInitRequiresADocsBase(t *testing.T) {
	isolate(t)
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly", "pages_project": "site"},
		"topology": map[string]any{"slug": "myproject"},
	})
	result := run(t, dir, "assembly", "init")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "docs_base") {
		t.Errorf("the refusal does not name the key: %s", result.Stderr)
	}
}

// -- assembly push ----------------------------------------------------------

func TestAssemblyPushDispatchesAgainstTheConfiguredRepo(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly", "pages_project": "site"},
		"topology": map[string]any{"slug": "myproject", "docs_base": "https://docs.example.com"},
	})
	testproject.Git(t, dir, "init")
	testproject.Git(t, dir, "add", "selfdoc.json")
	testproject.Git(t, dir, "commit", "-m", "initial")
	testproject.Git(t, dir, "tag", "v1.0.0")

	tools.Reply(
		toolReply{Match: "repo view", Stdout: "owner/source-repo\n"},
		toolReply{Match: "", Stdout: ""},
	)

	result := run(t, dir, "assembly", "push")
	if result.ExitCode != 0 {
		t.Fatalf("assembly push failed: %s\n%s", result.Stdout, result.Stderr)
	}
	dispatches := tools.Matching("/repos/owner/assembly/dispatches")
	if len(dispatches) != 1 {
		t.Fatalf("expected one dispatch, got %d: %v", len(dispatches), tools.Calls())
	}
	if !strings.Contains(dispatches[0].Input, `"slug"`) ||
		!strings.Contains(dispatches[0].Input, "myproject") {
		t.Errorf("the dispatch payload does not name the project: %s", dispatches[0].Input)
	}
	if !strings.Contains(result.Stdout, "Dispatched assembly rebuild for myproject v1.0.0 (ref: v1.0.0)") {
		t.Errorf("the summary is not the declared one:\n%s", result.Stdout)
	}
}

// -- assembly rebuild -------------------------------------------------------

func rebuildProject(t *testing.T) (*fakeTools, string) {
	t.Helper()
	tools := newFakeTools(t, "gh")
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
	})
	return tools, dir
}

func TestRebuildReportsAMalformedMembershipRecord(t *testing.T) {
	tools, dir := rebuildProject(t)
	tools.Reply(toolReply{Match: "/contents/", Stdout: encoded("{not json")})

	result := run(t, dir, "assembly", "rebuild")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stderr, "projects.json") {
		t.Errorf("the refusal does not name the record: %s", result.Stderr)
	}
}

func TestRebuildReportsAFailedRead(t *testing.T) {
	// A rate limit is not an assembly with no projects.
	tools, dir := rebuildProject(t)
	tools.Reply(toolReply{Match: "/contents/", Code: 1,
		Stderr: "gh: API rate limit exceeded (HTTP 403)"})

	result := run(t, dir, "assembly", "rebuild")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stderr, "rate limit") {
		t.Errorf("the refusal does not forward what gh said: %s", result.Stderr)
	}
	if strings.Contains(result.Stderr, "No projects configured") {
		t.Errorf("a failed read was reported as an empty assembly: %s", result.Stderr)
	}
}

func TestRebuildReportsAnAbsentMembershipRecord(t *testing.T) {
	tools, dir := rebuildProject(t)
	tools.Reply(toolReply{Match: "/contents/", Code: 1, Stderr: "gh: Not Found (HTTP 404)"})

	result := run(t, dir, "assembly", "rebuild")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stderr, "projects.json") {
		t.Errorf("the refusal does not name the record: %s", result.Stderr)
	}
}

func TestRebuildDispatchesForEveryRegisteredProject(t *testing.T) {
	tools, dir := rebuildProject(t)
	record, err := json.Marshal(map[string]any{
		"alpha": map[string]any{"repo": "owner/alpha", "ref": "v1.0.0", "version": "1.0.0"},
		"beta":  map[string]any{"repo": "owner/beta", "ref": "v2.0.0", "version": "2.0.0"},
	})
	if err != nil {
		t.Fatalf("encoding the record: %v", err)
	}
	tools.Reply(
		toolReply{Match: "/contents/", Stdout: encoded(string(record))},
		toolReply{Match: "", Stdout: ""},
	)

	result := run(t, dir, "assembly", "rebuild")
	if result.ExitCode != 0 {
		t.Fatalf("assembly rebuild failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Dispatched 2 rebuild(s).") {
		t.Errorf("the summary is not the declared one:\n%s", result.Stdout)
	}
	if got := len(tools.Matching("dispatches")); got != 2 {
		t.Errorf("expected two dispatches, got %d", got)
	}
}

func TestRebuildSaysSoWhenTheAssemblyHasNoProjects(t *testing.T) {
	tools, dir := rebuildProject(t)
	tools.Reply(toolReply{Match: "/contents/", Stdout: encoded("{}")})

	result := run(t, dir, "assembly", "rebuild")
	if result.ExitCode != 0 {
		t.Fatalf("exit code is %d, want 0: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "No projects configured in assembly.") {
		t.Errorf("an empty assembly is not reported:\n%s", result.Stdout)
	}
}

// -- assembly status --------------------------------------------------------

func TestStatusSaysSoWhenThereAreNoRuns(t *testing.T) {
	tools, dir := rebuildProject(t)
	tools.Reply(toolReply{Match: "", Stdout: ""})

	result := run(t, dir, "assembly", "status")
	if result.ExitCode != 0 {
		t.Fatalf("exit code is %d, want 0: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "No recent assembly builds found.") {
		t.Errorf("an empty run list is not reported:\n%s", result.Stdout)
	}
}

// -- assembly redirects -----------------------------------------------------

func TestRedirectsPrintsTheRule(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "assembly", "redirects",
		"--slug", "proj", "--docs-base", "https://docs.smmh.dev")
	if result.ExitCode != 0 {
		t.Fatalf("assembly redirects failed: %s", result.Stderr)
	}
	if result.Stdout != "/* https://docs.smmh.dev/proj/:splat 301\n" {
		t.Errorf("the rule is %q", result.Stdout)
	}
}

func TestRedirectsRejectsAnUnderscoredFlag(t *testing.T) {
	// strictcli flags are kebab-case: --docs_base is not a flag.
	isolate(t)
	result := run(t, t.TempDir(), "assembly", "redirects",
		"--slug", "proj", "--docs_base", "https://docs.smmh.dev")
	if result.ExitCode == 0 {
		t.Fatal("an underscored flag was accepted")
	}
}

func TestRedirectsNamesTheMissingFlag(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "assembly", "redirects", "--slug", "proj")
	if result.ExitCode == 0 {
		t.Fatal("the command ran without its base URL")
	}
	if !strings.Contains(result.Stderr, "--docs-base") {
		t.Errorf("the refusal does not name the flag: %s", result.Stderr)
	}
}

// -- assembly retire --------------------------------------------------------

func TestRetireRequiresASlug(t *testing.T) {
	// --slug declares presence required, so the handler never runs.
	isolate(t)
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "keeper"},
	})
	result := run(t, dir, "assembly", "retire")
	if result.ExitCode == 0 {
		t.Fatal("retire ran with no slug")
	}
	if !strings.Contains(result.Stderr, "--slug") {
		t.Errorf("the refusal does not name the flag: %s", result.Stderr)
	}
}

func TestRetireIsConsequential(t *testing.T) {
	// It deletes published content; nothing else in the tree does.
	entry := walk(t)["assembly.retire"]
	if entry["consequential"] != true {
		t.Error("retire is not consequential")
	}
	if entry["effect"] != "mutating" {
		t.Errorf("retire is classified %v", entry["effect"])
	}
	var names []string
	for _, raw := range entry["grants"].([]any) {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "assembly-commit,assembly-dispatch" {
		t.Errorf("the declared grants are %v", names)
	}
}

func TestRetireReportsAnUnknownSlug(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "keeper"},
	})
	roster := site.RenderRoster([]site.RosterEntry{
		{Slug: "keeper", Repo: "owner/keeper"},
		{Slug: "home", Repo: "owner/home"},
	}, "home")
	tools.Reply(toolReply{Match: "roster.toml", Stdout: encoded(roster)})

	result := runWith(t, Options{Dir: dir}, "assembly", "retire", "--slug", "never-existed")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "nothing to retire") {
		t.Errorf("the refusal is not the roster's: %s", result.Stderr)
	}
}

// -- assembly init ----------------------------------------------------------

// initProjectDir is the project every `assembly init` case runs in.
func initProjectDir(t *testing.T, repo, pagesProject string) string {
	t.Helper()
	return assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": repo, "pages_project": pagesProject},
		"topology": map[string]any{"slug": "myproject", "docs_base": "https://docs.example.com"},
	})
}

func TestInitCreatesThePagesProjectWhenCredentialsArePresent(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "tok-456")

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s\n%s", result.Stdout, result.Stderr)
	}

	wrangler := tools.Matching("wrangler")
	if len(wrangler) != 1 {
		t.Fatalf("expected one wrangler call, got %d: %v", len(wrangler), tools.Calls())
	}
	for _, want := range []string{"pages", "project", "create"} {
		if !strings.Contains(wrangler[0].Joined(), want) {
			t.Errorf("the wrangler call does not carry %q: %s", want, wrangler[0].Joined())
		}
	}
	if !strings.Contains(result.Stdout, "Created CF Pages project:") {
		t.Errorf("the creation is not reported:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Assembly repository initialized: owner/docs-assembly") {
		t.Errorf("the completion is not reported:\n%s", result.Stdout)
	}
}

func TestInitAcceptsTheAlternativeCredentialNames(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	t.Setenv("CF_ACCOUNT_ID", "")
	t.Setenv("CF_PAGES_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct-alt")
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok-alt")

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s\n%s", result.Stdout, result.Stderr)
	}
	if got := len(tools.Matching("wrangler")); got != 1 {
		t.Fatalf("expected one wrangler call, got %d", got)
	}
	if !strings.Contains(result.Stdout, "Created CF Pages project:") {
		t.Errorf("the creation is not reported:\n%s", result.Stdout)
	}
}

func TestInitWarnsWhenThePagesProjectCannotBeCreated(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "tok-456")
	tools.Reply(
		toolReply{Match: "wrangler", Code: 1, Stderr: "project already exists"},
		toolReply{Match: "", Code: 0},
	)

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("a failed Pages creation stopped init: %s", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "CF Pages project creation failed") {
		t.Errorf("no warning printed: %s", result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Assembly repository initialized:") {
		t.Errorf("init did not complete:\n%s", result.Stdout)
	}
}

func TestInitSkipsThePagesProjectWithoutCredentials(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	for _, name := range []string{
		"CF_ACCOUNT_ID", "CF_PAGES_API_TOKEN",
		"CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN",
	} {
		t.Setenv(name, "")
	}

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s", result.Stderr)
	}
	if got := len(tools.Matching("wrangler")); got != 0 {
		t.Errorf("wrangler ran with no credentials")
	}
	if !strings.Contains(result.Stderr, "CF_ACCOUNT_ID/CF_PAGES_API_TOKEN not set") {
		t.Errorf("no warning printed: %s", result.Stderr)
	}
	if got := len(tools.Matching("secret")); got != 0 {
		t.Errorf("a secret was set with no credentials")
	}
}

func TestInitUsesTheConfiguredPagesProject(t *testing.T) {
	// The Pages project name is the configured one, not the repo basename.
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "my-org/my-docs-assembly", "unified-site")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "tok-456")

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s", result.Stderr)
	}
	wrangler := tools.Matching("wrangler")
	if len(wrangler) != 1 {
		t.Fatalf("expected one wrangler call, got %d", len(wrangler))
	}
	if !strings.Contains(wrangler[0].Joined(), "unified-site") {
		t.Errorf("the configured project is not named: %s", wrangler[0].Joined())
	}
	if strings.Contains(wrangler[0].Joined(), "my-docs-assembly") {
		t.Errorf("the repo basename leaked into the call: %s", wrangler[0].Joined())
	}
}

func TestInitSetsBothSecrets(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "tok-456")

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s", result.Stderr)
	}
	secrets := tools.Matching("secret set")
	if len(secrets) != 2 {
		t.Fatalf("expected two secret calls, got %d", len(secrets))
	}
	for _, want := range []string{"CF_ACCOUNT_ID acct-123", "CF_PAGES_API_TOKEN tok-456"} {
		found := false
		for _, call := range secrets {
			if strings.Contains(call.Joined(), strings.Fields(want)[0]) &&
				strings.Contains(call.Joined(), "--body "+strings.Fields(want)[1]) {
				found = true
			}
		}
		if !found {
			t.Errorf("no secret call carries %q: %v", want, secrets)
		}
	}
	for _, want := range []string{
		"Set GitHub secret: CF_ACCOUNT_ID", "Set GitHub secret: CF_PAGES_API_TOKEN",
	} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("no confirmation printed for %q:\n%s", want, result.Stdout)
		}
	}
}

func TestInitWarnsWhenASecretCannotBeSet(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "tok-456")
	tools.Reply(
		toolReply{Match: "secret set", Code: 1, Stderr: "permission denied"},
		toolReply{Match: "", Code: 0},
	)

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("a failed secret stopped init: %s", result.Stderr)
	}
	for _, want := range []string{
		"Failed to set CF_ACCOUNT_ID secret", "Failed to set CF_PAGES_API_TOKEN secret",
	} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("no warning for %q: %s", want, result.Stderr)
		}
	}
	if !strings.Contains(result.Stdout, "Assembly repository initialized:") {
		t.Errorf("init did not complete:\n%s", result.Stdout)
	}
}

func TestInitWithOnlyAnAccountIdSetsOnlyThatSecret(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "docs-assembly")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s", result.Stderr)
	}
	if got := len(tools.Matching("wrangler")); got != 0 {
		t.Error("wrangler ran with only half the credentials")
	}
	if got := len(tools.Matching("CF_ACCOUNT_ID")); got != 1 {
		t.Errorf("expected one CF_ACCOUNT_ID secret call, got %d", got)
	}
	if !strings.Contains(result.Stderr, "CF_ACCOUNT_ID/CF_PAGES_API_TOKEN not set") {
		t.Errorf("no warning printed: %s", result.Stderr)
	}
}

func TestInitWritesTheWorkflowTheConfigDescribes(t *testing.T) {
	tools := newFakeTools(t, "gh", "npx")
	dir := initProjectDir(t, "owner/docs-assembly", "unified-site")
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_PAGES_API_TOKEN", "tok-456")

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()}, "assembly", "init")
	if result.ExitCode != 0 {
		t.Fatalf("assembly init failed: %s", result.Stderr)
	}

	pushed := map[string]string{}
	for _, call := range tools.Matching("/contents/") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
			t.Fatalf("decoding a contents payload: %v", err)
		}
		content, err := base64.StdEncoding.DecodeString(payload["content"].(string))
		if err != nil {
			t.Fatalf("decoding a pushed file: %v", err)
		}
		for _, arg := range call.Argv {
			if index := strings.Index(arg, "/contents/"); index >= 0 {
				pushed[arg[index+len("/contents/"):]] = string(content)
			}
		}
	}

	workflow, ok := pushed[".github/workflows/deploy.yml"]
	if !ok {
		t.Fatalf("no deploy workflow pushed; pushed %v", keysOf(pushed))
	}
	// The project init creates is the project the deploy targets.
	if !strings.Contains(workflow, "--project-name 'unified-site'") {
		t.Errorf("the workflow does not deploy to the configured project:\n%s", workflow)
	}
}

// keysOf lists a map's keys, for a failure message.
func keysOf(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}

// -- assembly sync-workflow -------------------------------------------------

// TestSyncWorkflowPushesToTheAssemblysBranch pins the branch the workflow sync
// commits to. The command names no branch of its own, and a push that resolves
// to an empty one asks GitHub for "/git/ref/heads/" -- a path the API answers
// 404 to, which the release's post-release hook reported as "get HEAD ref: gh:
// Not Found (HTTP 404)" while every other assembly command worked.
func TestSyncWorkflowPushesToTheAssemblysBranch(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly", "pages_project": "site"},
		"topology": map[string]any{"slug": "myproject", "docs_base": "https://docs.example.com"},
	})
	tools.Reply(
		toolReply{Match: "git/ref/heads", Stdout: "headsha\n"},
		toolReply{Match: "git/commits/headsha", Stdout: "treesha\n"},
		toolReply{Match: "git/trees/treesha", Stdout: "{\"tree\":[]}\n"},
		toolReply{Match: "git/blobs", Stdout: "blobsha\n"},
		toolReply{Match: "git/trees --jq", Stdout: "newtreesha\n"},
		toolReply{Match: "git/commits --jq", Stdout: "newcommitsha\n"},
		toolReply{Match: "git/refs/heads", Stdout: "newcommitsha\n"},
	)

	result := runWith(t, Options{Dir: dir, Registry: stubRegistry()},
		"assembly", "sync-workflow", "--pin-selfdoc", "0.1.0")
	if result.ExitCode != 0 {
		t.Fatalf("sync-workflow failed: exit %d\n%s\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	reads := tools.Matching("/git/ref/heads/")
	if len(reads) != 1 {
		t.Fatalf("expected one HEAD ref read, got %d: %v", len(reads), tools.Calls())
	}
	if !strings.Contains(reads[0].Joined(), "/git/ref/heads/main") {
		t.Errorf("the HEAD ref read names no branch: %s", reads[0].Joined())
	}
	writes := tools.Matching("/git/refs/heads/")
	if len(writes) != 1 {
		t.Fatalf("expected one ref update, got %d: %v", len(writes), tools.Calls())
	}
	if !strings.Contains(writes[0].Joined(), "/git/refs/heads/main") {
		t.Errorf("the ref update names no branch: %s", writes[0].Joined())
	}
}

// TestRetireCommitsOnTheAssemblysBranch pins the branch a retirement reads and
// writes. Retirement lists the branch's paths to work out what to delete and
// then commits on it, and both calls take the branch from the command; an
// empty one renders "/git/ref/heads/", which GitHub answers 404 to.
func TestRetireCommitsOnTheAssemblysBranch(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := assemblyProject(t, map[string]any{
		"assembly": map[string]any{"repo": "owner/assembly"},
		"topology": map[string]any{"slug": "keeper"},
	})
	roster := site.RenderRoster([]site.RosterEntry{
		{Slug: "keeper", Repo: "owner/keeper"},
		{Slug: "goner", Repo: "owner/goner"},
		{Slug: "home", Repo: "owner/home"},
	}, "home")
	tools.Reply(
		toolReply{Match: "roster.toml", Stdout: encoded(roster)},
		toolReply{Match: "goner-files.json", Stdout: ""},
		toolReply{Match: "git/ref/heads", Stdout: "headsha\n"},
		toolReply{Match: "git/commits/headsha", Stdout: "treesha\n"},
		toolReply{
			Match:  "git/trees/treesha",
			Stdout: "{\"tree\":[{\"path\":\"site/goner/index.html\",\"type\":\"blob\",\"sha\":\"b1\"}]}\n",
		},
		toolReply{Match: "git/blobs", Stdout: "blobsha\n"},
		toolReply{Match: "git/trees --jq", Stdout: "newtreesha\n"},
		toolReply{Match: "git/commits --jq", Stdout: "newcommitsha\n"},
		toolReply{Match: "git/refs/heads", Stdout: "newcommitsha\n"},
		toolReply{Match: "", Stdout: ""},
	)

	result := runWith(t, Options{Dir: dir},
		"assembly", "retire", "--slug", "goner", "--approve-consequential")
	if result.ExitCode != 0 {
		t.Fatalf("retire failed: exit %d\n%s\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	// Both the listing and the push read the ref, and the push writes it.
	refCalls := 0
	for _, call := range tools.Calls() {
		joined := call.Joined()
		if !strings.Contains(joined, "/git/ref/heads/") &&
			!strings.Contains(joined, "/git/refs/heads/") {
			continue
		}
		refCalls++
		if !strings.Contains(joined, "heads/main") {
			t.Errorf("a ref call names no branch: %s", joined)
		}
	}
	if refCalls != 3 {
		t.Errorf("expected two ref reads and one ref update, got %d calls: %v",
			refCalls, tools.Calls())
	}
}

// unversionedPushProject is a project that declares it has no public version,
// on a branch that exists on its origin -- the state `assembly push` has to
// dispatch.
func unversionedPushProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	testproject.WriteJSON(t, filepath.Join(dir, "selfdoc.json"), map[string]any{
		"base_url":      "https://example.com",
		"author":        testproject.Author(),
		"search_engine": "pagefind",
		"unversioned":   true,
		"locales": []any{map[string]any{
			"code": "en", "label": "English", "default": true,
		}},
		"assembly": map[string]any{"repo": "owner/assembly", "pages_project": "site"},
		"topology": map[string]any{
			"slug": "portfolio", "docs_base": "https://docs.example.com",
		},
	})
	testproject.Git(t, dir, "init", "--initial-branch=main")
	testproject.Git(t, dir, "add", "selfdoc.json")
	testproject.Git(t, dir, "commit", "-m", "initial")
	origin := filepath.Join(t.TempDir(), "origin.git")
	testproject.Git(t, dir, "clone", "--bare", dir, origin)
	testproject.Git(t, dir, "remote", "add", "origin", origin)
	return dir
}

func TestAssemblyPushDispatchesAnUnversionedProjectAtItsBranch(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := unversionedPushProject(t)
	tools.Reply(
		toolReply{Match: "repo view", Stdout: "owner/portfolio\n"},
		toolReply{Match: "", Stdout: ""},
	)

	result := run(t, dir, "assembly", "push")
	if result.ExitCode != 0 {
		t.Fatalf("assembly push failed: %s\n%s", result.Stdout, result.Stderr)
	}
	dispatches := tools.Matching("/repos/owner/assembly/dispatches")
	if len(dispatches) != 1 {
		t.Fatalf("expected one dispatch, got %d: %v", len(dispatches), tools.Calls())
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(dispatches[0].Input), &body); err != nil {
		t.Fatalf("decoding the payload: %v\n%s", err, dispatches[0].Input)
	}
	payload, _ := body["client_payload"].(map[string]any)
	for key, want := range map[string]string{
		"slug": "portfolio", "repo": "owner/portfolio",
		"ref": "main", "version": "unversioned",
	} {
		if payload[key] != want {
			t.Errorf("%s = %v, want %q", key, payload[key], want)
		}
	}
	if !strings.Contains(result.Stdout, "Dispatched assembly rebuild for portfolio (unversioned) (ref: main)") {
		t.Errorf("the summary is not the declared one:\n%s", result.Stdout)
	}
}

// A branch the assembly cannot clone is refused before anything is dispatched:
// the deploy would fetch a ref that is not there.
func TestAssemblyPushRefusesABranchOriginDoesNotCarry(t *testing.T) {
	tools := newFakeTools(t, "gh")
	dir := unversionedPushProject(t)
	testproject.Git(t, dir, "checkout", "-b", "unpushed")
	tools.Reply(
		toolReply{Match: "repo view", Stdout: "owner/portfolio\n"},
		toolReply{Match: "", Stdout: ""},
	)

	result := run(t, dir, "assembly", "push")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "unpushed") {
		t.Errorf("the refusal does not name the branch: %s", result.Stderr)
	}
	if len(tools.Matching("/dispatches")) != 0 {
		t.Error("the refusal dispatched anyway")
	}
}
