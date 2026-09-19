package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/smm-h/stricttest/go/hygiene"
)

// isolate binds the environment isolation floor -- a throwaway HOME, an empty
// global git config with a throwaway identity, only the file:// git transport,
// no ambient credentials -- and returns a directory at the FRONT of PATH, so a
// fake tool written there shadows any real one. The system directories stay
// behind it so git stays reachable.
//
// Nothing here calls t.Parallel: hygiene mutates process-wide variables.
func isolate(t *testing.T) string {
	t.Helper()
	hygiene.Isolate(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	// The Cloudflare credentials are read from the environment, so every test
	// starts from a known-absent state whatever the developer's shell holds.
	for _, name := range []string{
		"CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN",
		"CF_ACCOUNT_ID", "CF_PAGES_API_TOKEN",
	} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	return bin
}

// isolateWithoutTools binds the isolation floor and points PATH at an empty
// directory, so nothing is installed as far as the code under test can see.
func isolateWithoutTools(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
	t.Setenv("PATH", t.TempDir())
}

// fakeTool writes an executable shell script named name into dir.
func fakeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
}

// recorder writes a fake tool that appends its whole argv and a chosen
// environment variable to a log file and succeeds. The returned function reads
// back one line per invocation.
func recorder(t *testing.T, bin, name, envVar string) func() []string {
	t.Helper()
	log := filepath.Join(t.TempDir(), name+".argv")
	body := "printf '%s' \"$*\" >> " + quote(log)
	if envVar != "" {
		body += "; printf ' env=%s' \"$" + envVar + "\" >> " + quote(log)
	}
	body += "; printf '\\n' >> " + quote(log)
	fakeTool(t, bin, name, body)
	return func() []string {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatalf("reading %s: %v", log, err)
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// git runs a real git command in dir as test scaffolding.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := effects.Unbound().Run(
		append([]string{"git"}, args...),
		effects.Cwd(dir),
		effects.CaptureOutput(),
	)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("git %s exited %d: %s", strings.Join(args, " "), res.ExitCode, res.Stderr)
	}
	return string(res.Stdout)
}

// siteTree writes a small built site -- one page, one asset in a
// subdirectory -- and returns its directory.
func siteTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "site.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

// --- the push target ------------------------------------------------------

// TestGitHubPagesTarget covers the required explicit target. The deploy
// force-pushes a gh-pages branch, so the target is never inferred from the
// process's working directory: an empty one is refused, and a path that is
// neither a remote URL nor a directory is refused by name.
//
// Every refusal names the command the reader ran, "selfdoc deploy", rather
// than the internal function that produced it.
func TestGitHubPagesTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  func(t *testing.T, dir string) string
		wantErr string
	}{
		{
			name:    "an empty target is refused",
			target:  func(*testing.T, string) string { return "" },
			wantErr: "selfdoc deploy requires an explicit push target",
		},
		{
			name: "a path that does not exist is refused",
			target: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "nope")
			},
			wantErr: "selfdoc deploy target",
		},
		{
			name: "a file is not a repository directory",
			target: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "file")
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return path
			},
			wantErr: "is neither a git remote URL nor an existing directory",
		},
		{
			name: "a directory with no origin remote is refused by name",
			target: func(t *testing.T, dir string) string {
				repo := filepath.Join(dir, "repo")
				if err := os.MkdirAll(repo, 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				git(t, repo, "init")
				return repo
			},
			wantErr: "Ensure 'origin' remote is configured there.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			dir := t.TempDir()
			err := GitHubPages(siteTree(t), "1.0.0", tt.target(t, dir), effects.Unbound())
			var deployErr *Error
			if !errors.As(err, &deployErr) {
				t.Fatalf("GitHubPages error = %v, want a *deploy.Error", err)
			}
			if !strings.Contains(deployErr.Message, tt.wantErr) {
				t.Errorf("message %q does not contain %q", deployErr.Message, tt.wantErr)
			}
		})
	}
}

// TestLooksLikeRemoteURL covers the classification that decides whether a
// target is resolved through git at all.
func TestLooksLikeRemoteURL(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"https://github.com/owner/repo.git", true},
		{"http://example.com/repo", true},
		{"ssh://git@github.com/owner/repo", true},
		{"git://example.com/repo", true},
		{"file:///srv/repo.git", true},
		{"git@github.com:owner/repo.git", true},
		{"user@host:path/to/repo", true},
		{".", false},
		{"/home/user/project", false},
		{"../sibling", false},
		{"relative/path", false},
		{"path/with@and:inside/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := looksLikeRemoteURL(tt.target); got != tt.want {
				t.Errorf("looksLikeRemoteURL(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

// TestGitHubPagesResolvesTheTargetsOwnRemote covers the reason the target
// exists: the remote comes from THAT repository, not from the process's
// working directory, which is deliberately made a different repository here.
func TestGitHubPagesResolvesTheTargetsOwnRemote(t *testing.T) {
	isolate(t)
	root := t.TempDir()

	// The repository the caller names, whose origin must win.
	intended := filepath.Join(root, "intended")
	if err := os.MkdirAll(intended, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	git(t, intended, "init")
	bare := initBareRemote(t, filepath.Join(root, "intended-remote.git"))
	git(t, intended, "remote", "add", "origin", bare)

	// The repository the process happens to sit in, whose origin must not.
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	git(t, elsewhere, "init")
	wrong := initBareRemote(t, filepath.Join(root, "elsewhere-remote.git"))
	git(t, elsewhere, "remote", "add", "origin", wrong)
	hygiene.Chdir(t, elsewhere)

	if err := GitHubPages(siteTree(t), "1.0.0", intended, effects.Unbound()); err != nil {
		t.Fatalf("GitHubPages: %v", err)
	}

	assertGhPages(t, bare, "docs: v1.0.0")
	if out := git(t, wrong, "branch", "--list", "gh-pages"); strings.TrimSpace(out) != "" {
		t.Errorf("the working directory's own remote received the push: %q", out)
	}
}

// TestGitHubPagesUsesARemoteURLVerbatim covers the other target form: a URL is
// pushed to as given, with no git resolution step in front of it.
func TestGitHubPagesUsesARemoteURLVerbatim(t *testing.T) {
	isolate(t)
	bare := initBareRemote(t, filepath.Join(t.TempDir(), "remote.git"))

	if err := GitHubPages(siteTree(t), "2.3.4", "file://"+bare, effects.Unbound()); err != nil {
		t.Fatalf("GitHubPages: %v", err)
	}
	assertGhPages(t, bare, "docs: v2.3.4")
}

// TestGitHubPagesPublishesTheWholeTree covers what reaches the branch: every
// file and directory of the built output, plus the .nojekyll file that stops
// GitHub from running Jekyll over it.
func TestGitHubPagesPublishesTheWholeTree(t *testing.T) {
	isolate(t)
	bare := initBareRemote(t, filepath.Join(t.TempDir(), "remote.git"))

	if err := GitHubPages(siteTree(t), "1.0.0", "file://"+bare, effects.Unbound()); err != nil {
		t.Fatalf("GitHubPages: %v", err)
	}
	listing := git(t, bare, "ls-tree", "-r", "--name-only", "gh-pages")
	for _, want := range []string{".nojekyll", "index.html", "assets/site.css"} {
		if !strings.Contains(listing, want) {
			t.Errorf("gh-pages does not carry %s:\n%s", want, listing)
		}
	}
}

// TestGitHubPagesForcePushesOverUnrelatedHistory covers the force: a gh-pages
// branch already holding an unrelated commit is replaced, not merged with.
func TestGitHubPagesForcePushesOverUnrelatedHistory(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	bare := initBareRemote(t, filepath.Join(root, "remote.git"))

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	git(t, seed, "init")
	git(t, seed, "checkout", "-b", "gh-pages")
	if err := os.WriteFile(filepath.Join(seed, "stale.html"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	git(t, seed, "add", ".")
	git(t, seed, "commit", "-m", "stale publication")
	git(t, seed, "push", "file://"+bare, "gh-pages")

	if err := GitHubPages(siteTree(t), "9.9.9", "file://"+bare, effects.Unbound()); err != nil {
		t.Fatalf("GitHubPages: %v", err)
	}
	listing := git(t, bare, "ls-tree", "-r", "--name-only", "gh-pages")
	if strings.Contains(listing, "stale.html") {
		t.Errorf("the previous publication was merged with rather than replaced:\n%s", listing)
	}
	assertGhPages(t, bare, "docs: v9.9.9")
}

// TestGitHubPagesReportsAFailedPush covers the push failure: the remote's own
// stderr is carried into the error.
func TestGitHubPagesReportsAFailedPush(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "not-a-repo.git")

	err := GitHubPages(siteTree(t), "1.0.0", "file://"+missing, effects.Unbound())
	var deployErr *Error
	if !errors.As(err, &deployErr) {
		t.Fatalf("GitHubPages error = %v, want a *deploy.Error", err)
	}
	if !strings.HasPrefix(deployErr.Message, "Failed to push to gh-pages branch:\n") {
		t.Errorf("message = %q, want the push failure naming the remote's stderr", deployErr.Message)
	}
}

// TestGitHubPagesLeavesNoStagingDirectory covers the temporary tree: the
// staging repository is removed whether the deploy succeeded or failed.
func TestGitHubPagesLeavesNoStagingDirectory(t *testing.T) {
	isolate(t)
	before := stagingDirs(t)
	bare := initBareRemote(t, filepath.Join(t.TempDir(), "remote.git"))
	if err := GitHubPages(siteTree(t), "1.0.0", "file://"+bare, effects.Unbound()); err != nil {
		t.Fatalf("GitHubPages: %v", err)
	}
	if err := GitHubPages(siteTree(t), "1.0.0", "file:///nope.git", effects.Unbound()); err == nil {
		t.Fatal("a push to a missing remote succeeded")
	}
	if after := stagingDirs(t); after != before {
		t.Errorf("staging directories = %d, want the %d there were before", after, before)
	}
}

// stagingDirs counts the deploy's own temporary staging directories.
func stagingDirs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "selfdoc-gh-pages-") {
			count++
		}
	}
	return count
}

// initBareRemote creates a bare repository at path and returns it. The suite's
// git transport lockdown allows file://, which is what every push here uses.
func initBareRemote(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	git(t, path, "init", "--bare")
	return path
}

// assertGhPages fails unless the remote's gh-pages branch tip carries subject.
func assertGhPages(t *testing.T, bare, subject string) {
	t.Helper()
	got := strings.TrimSpace(git(t, bare, "log", "-1", "--format=%s", "gh-pages"))
	if got != subject {
		t.Errorf("gh-pages tip subject = %q, want %q", got, subject)
	}
}

// --- Cloudflare Pages ------------------------------------------------------

// TestCloudflarePagesRequiresWrangler covers the presence check, which names
// both the install and the authentication step.
func TestCloudflarePagesRequiresWrangler(t *testing.T) {
	isolateWithoutTools(t)
	err := CloudflarePages(t.TempDir(), "docs-site", "1.0.0", effects.Unbound())
	var deployErr *Error
	if !errors.As(err, &deployErr) {
		t.Fatalf("CloudflarePages error = %v, want a *deploy.Error", err)
	}
	for _, want := range []string{
		"wrangler CLI not found. Install it with: npm install -g wrangler",
		"Then authenticate with: wrangler login",
	} {
		if !strings.Contains(deployErr.Message, want) {
			t.Errorf("message %q does not contain %q", deployErr.Message, want)
		}
	}
}

// TestCloudflarePagesArgv covers the argv handed to wrangler: the output
// directory as a positional, the project name and the commit message as
// single-token flags, with the version prefixed by "v".
func TestCloudflarePagesArgv(t *testing.T) {
	bin := isolate(t)
	read := recorder(t, bin, "wrangler", "")
	out := t.TempDir()

	if err := CloudflarePages(out, "docs-site", "1.2.3", effects.Unbound()); err != nil {
		t.Fatalf("CloudflarePages: %v", err)
	}
	lines := read()
	want := "pages deploy " + out + " --project-name=docs-site --commit-message=v1.2.3"
	if len(lines) != 1 || lines[0] != want {
		t.Errorf("wrangler argv = %v, want [%s]", lines, want)
	}
}

// TestCloudflarePagesReportsAFailure covers the non-zero exit: the error names
// the exit code and carries wrangler's own stderr, trimmed.
func TestCloudflarePagesReportsAFailure(t *testing.T) {
	bin := isolate(t)
	fakeTool(t, bin, "wrangler", "echo 'Authentication error [code: 10000]' >&2; exit 7")

	err := CloudflarePages(t.TempDir(), "docs-site", "1.0.0", effects.Unbound())
	var deployErr *Error
	if !errors.As(err, &deployErr) {
		t.Fatalf("CloudflarePages error = %v, want a *deploy.Error", err)
	}
	want := "Cloudflare Pages deploy failed (exit 7):\nAuthentication error [code: 10000]"
	if deployErr.Message != want {
		t.Errorf("message = %q, want %q", deployErr.Message, want)
	}
}

// TestResolveCloudflareEnv covers the credential bridge: the CF_-prefixed
// names this fleet uses are copied to the CLOUDFLARE_-prefixed ones wrangler
// reads, and an already-set CLOUDFLARE_ value is never overwritten.
func TestResolveCloudflareEnv(t *testing.T) {
	tests := []struct {
		name    string
		set     map[string]string
		wantID  string
		wantTok string
	}{
		{
			name:    "the CF_ names are bridged",
			set:     map[string]string{"CF_ACCOUNT_ID": "acct", "CF_PAGES_API_TOKEN": "tok"},
			wantID:  "acct",
			wantTok: "tok",
		},
		{
			name: "an existing CLOUDFLARE_ value wins",
			set: map[string]string{
				"CLOUDFLARE_ACCOUNT_ID": "already", "CF_ACCOUNT_ID": "acct",
				"CLOUDFLARE_API_TOKEN": "already-tok", "CF_PAGES_API_TOKEN": "tok",
			},
			wantID:  "already",
			wantTok: "already-tok",
		},
		{
			name:    "nothing set stays nothing set",
			set:     nil,
			wantID:  "",
			wantTok: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			for name, value := range tt.set {
				t.Setenv(name, value)
			}
			ResolveCloudflareEnv()
			if got := os.Getenv("CLOUDFLARE_ACCOUNT_ID"); got != tt.wantID {
				t.Errorf("CLOUDFLARE_ACCOUNT_ID = %q, want %q", got, tt.wantID)
			}
			if got := os.Getenv("CLOUDFLARE_API_TOKEN"); got != tt.wantTok {
				t.Errorf("CLOUDFLARE_API_TOKEN = %q, want %q", got, tt.wantTok)
			}
		})
	}
}

// TestCloudflarePagesChildSeesTheBridgedCredentials covers why the bridge
// writes to this process's environment: the child inherits it, so wrangler
// sees the token without the deploy having to hand it an explicit environment.
func TestCloudflarePagesChildSeesTheBridgedCredentials(t *testing.T) {
	bin := isolate(t)
	read := recorder(t, bin, "wrangler", "CLOUDFLARE_API_TOKEN")
	t.Setenv("CF_PAGES_API_TOKEN", "bridged-token")

	if err := CloudflarePages(t.TempDir(), "docs-site", "1.0.0", effects.Unbound()); err != nil {
		t.Fatalf("CloudflarePages: %v", err)
	}
	lines := read()
	if len(lines) != 1 || !strings.HasSuffix(lines[0], " env=bridged-token") {
		t.Errorf("wrangler invocation = %v, want the bridged token in its environment", lines)
	}
}

// --- dispatch --------------------------------------------------------------

func TestDispatch(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		// wantTool is the fake binary the dispatch must reach, or "" when the
		// dispatch must refuse before running anything.
		wantTool string
		wantErr  string
	}{
		{name: "cloudflare-pages", provider: "cloudflare-pages", wantTool: "wrangler"},
		{name: "github-pages", provider: "github-pages", wantTool: "git"},
		{
			name:     "an unknown provider",
			provider: "netlify",
			wantErr:  "Unknown deploy provider 'netlify'",
		},
		{
			name:     "an absent provider",
			provider: "",
			wantErr:  "Unknown deploy provider ''",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := isolate(t)
			read := recorder(t, bin, "wrangler", "")
			bare := initBareRemote(t, filepath.Join(t.TempDir(), "remote.git"))

			err := Dispatch(
				Config{Provider: tt.provider, Project: "docs-site"},
				siteTree(t), "1.0.0", "file://"+bare, effects.Unbound(),
			)

			if tt.wantErr != "" {
				var unknown *UnknownProviderError
				if !errors.As(err, &unknown) {
					t.Fatalf("Dispatch error = %v, want an *UnknownProviderError", err)
				}
				if unknown.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", unknown.Error(), tt.wantErr)
				}
				if len(read()) != 0 {
					t.Error("the refused dispatch ran wrangler anyway")
				}
				return
			}
			if err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			switch tt.wantTool {
			case "wrangler":
				if len(read()) != 1 {
					t.Errorf("wrangler invocations = %v, want one", read())
				}
			case "git":
				assertGhPages(t, bare, "docs: v1.0.0")
				if len(read()) != 0 {
					t.Error("the github-pages dispatch ran wrangler")
				}
			}
		})
	}
}

// --- preview mode ----------------------------------------------------------

// preview dispatches body through a mutating strictcli command under
// --dry-run and returns the effect log's rendered lines.
func preview(t *testing.T, body func(h *effects.Handle) error) []string {
	t.Helper()
	var bodyErr error
	app := strictcli.NewApp("deploytest", "0.0.0", "harness for deploy previews")
	app.Command("do", "run a deploy",
		func(ctx *strictcli.Context, _ map[string]any) strictcli.Outcome {
			bodyErr = body(effects.FromContext(ctx))
			return strictcli.Exit(0)
		},
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithGrants(
			strictcli.Grant{
				Name:   "deploy",
				Reason: "publishes the built site to the configured Cloudflare Pages project",
				Kind:   strictcli.ProcMutate,
			},
			strictcli.Grant{
				Name:   "force-push",
				Reason: "replaces the remote gh-pages branch wholesale",
				Kind:   strictcli.ProcMutate,
			},
		),
	)
	app.Test([]string{"do", "--dry-run"})
	if bodyErr != nil {
		t.Fatalf("the previewed deploy failed: %v", bodyErr)
	}
	var details []string
	for _, record := range app.EffectLog() {
		details = append(details, record["detail"].(string))
	}
	return details
}

// TestCloudflarePagesUnderPreview covers the dry-run path: the upload is
// recorded, wrangler never runs, and nothing is reported as deployed.
func TestCloudflarePagesUnderPreview(t *testing.T) {
	bin := isolate(t)
	read := recorder(t, bin, "wrangler", "")
	out := t.TempDir()

	details := preview(t, func(h *effects.Handle) error {
		return CloudflarePages(out, "docs-site", "1.0.0", h)
	})

	if len(read()) != 0 {
		t.Error("wrangler ran under --dry-run")
	}
	joined := strings.Join(details, "\n")
	if !strings.Contains(joined, "wrangler pages deploy") {
		t.Errorf("the effect log does not name the upload:\n%s", joined)
	}
}

// TestGitHubPagesUnderPreview covers the dry-run path of the staging
// sequence: every step is recorded, including the force-push the whole
// sequence exists to perform, and the remote is untouched.
func TestGitHubPagesUnderPreview(t *testing.T) {
	isolate(t)
	bare := initBareRemote(t, filepath.Join(t.TempDir(), "remote.git"))
	site := siteTree(t)

	details := preview(t, func(h *effects.Handle) error {
		return GitHubPages(site, "1.0.0", "file://"+bare, h)
	})

	joined := strings.Join(details, "\n")
	for _, want := range []string{
		"git init",
		"git checkout -b gh-pages",
		"index.html",
		".nojekyll",
		"git add .",
		"git commit -m docs: v1.0.0",
		"git push --force file://" + bare + " gh-pages",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the effect log does not name %q:\n%s", want, joined)
		}
	}
	if out := git(t, bare, "branch", "--list", "gh-pages"); strings.TrimSpace(out) != "" {
		t.Errorf("the remote received a branch under --dry-run: %q", out)
	}
}
