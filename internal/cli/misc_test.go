package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// The refusals of the commands whose whole surface is a refusal plus a call
// into the engine.

// -- deploy -----------------------------------------------------------------

// deployProject is a project with an output tree and the given deploy block.
func deployProject(t *testing.T, deploy map[string]any) string {
	t.Helper()
	overrides := map[string]any{"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/"}
	if deploy != nil {
		overrides["deploy"] = deploy
	}
	dir := postProject(t, overrides)
	writeText(t, filepath.Join(dir, "stricttools", ".docs-cache", "build", "index.html"), "<html></html>\n")
	return dir
}

func TestDeployRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "deploy", "--approve-consequential")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No selfdoc.json") {
		t.Errorf("the refusal is not the missing config's: %s", result.Stderr)
	}
}

func TestDeployRefusesWithoutAnOutputTree(t *testing.T) {
	isolate(t)
	dir := postProject(t, map[string]any{"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/"})
	result := run(t, dir, "deploy", "--approve-consequential")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "Output directory 'stricttools/.docs-cache/build' not found") {
		t.Errorf("the refusal is not the missing output's: %s", result.Stderr)
	}
}

func TestDeployRefusesWithoutADeploySection(t *testing.T) {
	isolate(t)
	dir := deployProject(t, nil)
	result := run(t, dir, "deploy", "--approve-consequential")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No 'deploy' section in selfdoc.json") {
		t.Errorf("the refusal does not name the section: %s", result.Stderr)
	}
}

func TestDeployRefusesAnUnnamedPagesProject(t *testing.T) {
	// The one provider that reads "project" cannot run without it, and the
	// config loader is where that is settled: the refusal arrives as an
	// unusable configuration rather than as something the command discovers.
	isolate(t)
	dir := deployProject(t, map[string]any{"provider": "cloudflare-pages"})
	result := run(t, dir, "deploy", "--approve-consequential")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "deploy.project") {
		t.Errorf("the refusal does not name the key: %s", result.Stderr)
	}
}

// -- gen-data ---------------------------------------------------------------

func TestGenDataRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "gen-data", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No selfdoc.json") {
		t.Errorf("the refusal is not the missing config's: %s", result.Stderr)
	}
}

func TestGenDataSaysSoWhenNothingIsConfigured(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	result := run(t, dir, "gen-data", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("exit code is %d, want 0: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "No gen-data scripts configured.") {
		t.Errorf("an unconfigured project is not reported:\n%s", result.Stdout)
	}
}

// -- gen --------------------------------------------------------------------

func TestGenRefusesWithoutAConfig(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "gen", "--no-auto-commit")
	if result.ExitCode != 1 {
		t.Fatalf("exit code is %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No selfdoc.json") {
		t.Errorf("the refusal is not the missing config's: %s", result.Stderr)
	}
}

// -- spell-corpus -----------------------------------------------------------

func TestSpellCorpusReadsTheNamedRootAndEmitsThePayload(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	project := filepath.Join(root, "one")
	writeText(t, filepath.Join(project, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \""+longDescription+"\"\n+++\n\n# Home\n\nPlain prose.\n")
	writeText(t, filepath.Join(project, "selfdoc.json"), `{"base_url":"https://example.com",`+
		`"author":{"name":"Test Author","url":"https://author.example"},`+
		`"docs":"stricttools/docs/","output":"stricttools/.docs-cache/build/","unversioned":true,`+
		`"locales":[{"code":"en","label":"English","default":true}]}`)

	result := run(t, t.TempDir(), "spell-corpus", "--root", root, "--json")
	payload := payloadOf(t, result)
	if payload["root"] != root {
		t.Errorf("the sweep reports root %v, want %s", payload["root"], root)
	}
	projects, ok := payload["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("the sweep visited %v", payload["projects"])
	}
	if projects[0].(map[string]any)["project"] != "one" {
		t.Errorf("the visited project is %v", projects[0])
	}
}

func TestSpellCorpusIsReadOnly(t *testing.T) {
	// It reads every project it visits and writes nothing, so a preview has
	// nothing to record and the classification says so.
	if walk(t)["spell-corpus"]["effect"] != "read_only" {
		t.Error("spell-corpus is not read_only")
	}
}

// -- quality ----------------------------------------------------------------

func TestQualityEmitsItsPayload(t *testing.T) {
	isolate(t)
	dir := postProject(t, nil)
	result := run(t, dir, "quality", "--json")
	if result.ExitCode != 0 {
		// dirstat is an external tool; without it the command says so on
		// stderr and exits 1, which is the other declared outcome.
		if strings.Contains(result.Stderr, "dirstat") {
			t.Skip("dirstat is not installed: quality reports that and exits 1")
		}
		t.Fatalf("quality failed: %s", result.Stderr)
	}
	payload := payloadOf(t, result)
	for _, key := range []string{
		"project", "path", "tier", "tier_name", "code_loc", "test_loc",
		"source_loc", "doc_loc", "doc_files", "doc_ratio", "content_grade",
		"selfdoc", "next_step",
	} {
		if _, ok := payload[key]; !ok {
			t.Errorf("the payload carries no %q member: %v", key, payload)
		}
	}
	if payload["path"] != dir {
		t.Errorf("quality scored %v, want the stated directory %s", payload["path"], dir)
	}
}
