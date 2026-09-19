package assembly

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/blog/site"
)

const (
	testPagesProject  = "smmh"
	testCanonicalBase = "https://docs.smmh.dev"
)

// workflowYAML renders the workflow the assertions read, failing the test when
// the generator refuses.
func workflowYAML(t *testing.T) string {
	t.Helper()
	yaml, err := GenerateWorkflowYAML(
		testPagesProject, testCanonicalBase, testPins,
	)
	if err != nil {
		t.Fatalf("generating the workflow: %v", err)
	}
	return yaml
}

func TestWorkflowHasItsStructuralMarkers(t *testing.T) {
	yaml := workflowYAML(t)
	for _, marker := range []string{"name:", "on:", "jobs:", "deploy:", "ubuntu-latest"} {
		if !strings.Contains(yaml, marker) {
			t.Errorf("the workflow is missing %q", marker)
		}
	}
}

func TestWorkflowHasTheDispatchTrigger(t *testing.T) {
	yaml := workflowYAML(t)
	for _, marker := range []string{"repository_dispatch", DispatchEventType} {
		if !strings.Contains(yaml, marker) {
			t.Errorf("the workflow is missing %q", marker)
		}
	}
}

func TestWorkflowHasConcurrencyAndQueue(t *testing.T) {
	yaml := workflowYAML(t)
	// queue: max enables FIFO queuing of up to 100 pending runs, which is what
	// keeps two projects' deploys from racing each other for the branch.
	for _, marker := range []string{
		"assembly-deploy", "cancel-in-progress: false", "queue: max",
	} {
		if !strings.Contains(yaml, marker) {
			t.Errorf("the workflow is missing %q", marker)
		}
	}
}

func TestWorkflowHasPermissions(t *testing.T) {
	yaml := workflowYAML(t)
	for _, marker := range []string{"permissions:", "contents: write"} {
		if !strings.Contains(yaml, marker) {
			t.Errorf("the workflow is missing %q", marker)
		}
	}
}

func TestWorkflowCheckoutsAreTwoAndTheFirstIsDeep(t *testing.T) {
	yaml := workflowYAML(t)
	if got := strings.Count(yaml, "actions/checkout@v4"); got != 2 {
		t.Fatalf("found %d checkout steps, want 2", got)
	}
	// The first checkout clones the whole history, which is what the push
	// retry loop resets against; the second clones the source project.
	if !strings.Contains(yaml, "fetch-depth: 0") {
		t.Error("the assembly checkout is not a full clone")
	}
	if !strings.Contains(yaml, "path: source/") {
		t.Error("the source checkout does not clone into source/")
	}
}

func TestWorkflowInstallsTheBinaryFromTheGoModule(t *testing.T) {
	yaml := workflowYAML(t)
	if !strings.Contains(yaml, "actions/setup-go@v5") {
		t.Error("the workflow sets up no Go toolchain")
	}
	want := "go install " + GoModulePath + "@v" + pinnedSelfdoc
	if !strings.Contains(yaml, want) {
		t.Errorf("the workflow does not run %q", want)
	}
}

func TestWorkflowInstallsPagefindThroughPip(t *testing.T) {
	yaml := workflowYAML(t)
	if !strings.Contains(yaml, "actions/setup-python@v5") {
		t.Error("the workflow sets up no Python")
	}
	if !strings.Contains(yaml, WorkflowPythonVersion) {
		t.Errorf("the workflow names no Python version")
	}
	want := "pip install 'pagefind[bin]==" + pinnedPagefind + "'"
	if !strings.Contains(yaml, want) {
		t.Errorf("the workflow does not run %q", want)
	}
}

func TestNoToolTheDeployInstallsFloats(t *testing.T) {
	yaml := workflowYAML(t)
	var install []string
	for _, line := range strings.Split(yaml, "\n") {
		if strings.Contains(line, "go install ") || strings.Contains(line, "pip install ") {
			install = append(install, line)
		}
	}
	if len(install) != 2 {
		t.Fatalf("the toolchain is installed by %d line(s), want 2: %v", len(install), install)
	}
	joined := strings.Join(install, "\n")
	for _, spec := range []string{"@v" + pinnedSelfdoc, "pagefind[bin]==" + pinnedPagefind} {
		if !strings.Contains(joined, spec) {
			t.Errorf("%s is missing: that tool floats", spec)
		}
	}
}

func TestWorkflowInvokesTheIntegrateCommand(t *testing.T) {
	yaml := workflowYAML(t)
	if !strings.Contains(yaml, "selfdoc assembly integrate") {
		t.Error("the workflow does not invoke the integrate command")
	}
}

func TestWorkflowHandsEveryPayloadFieldToIntegrate(t *testing.T) {
	yaml := workflowYAML(t)
	for _, pair := range [][2]string{
		{"--slug", "slug"},
		{"--version", "version"},
		{"--ref", "ref"},
		{"--source-repo", "repo"},
		{"--scope", "scope"},
	} {
		want := pair[0] + " '${{ github.event.client_payload." + pair[1] + " }}'"
		if !strings.Contains(yaml, want) {
			t.Errorf("the workflow does not pass %s", want)
		}
	}
}

func TestWorkflowHandsTheConfigValuesToIntegrate(t *testing.T) {
	yaml := workflowYAML(t)
	if want := "--canonical-base '" + testCanonicalBase + "'"; !strings.Contains(yaml, want) {
		t.Errorf("the workflow does not pass %s", want)
	}
}

// TestWorkflowPassesNoRetiredRedirectFlag covers the flag the deploy used to
// carry for the redirect worker: the worker is gone, the flag with it, and a
// regenerated workflow that still named it would fail at parse time on every
// deploy.
func TestWorkflowPassesNoRetiredRedirectFlag(t *testing.T) {
	if yaml := workflowYAML(t); strings.Contains(yaml, "--legacy-blog-host") {
		t.Error("the generated workflow still passes --legacy-blog-host")
	}
}

func TestWorkflowScopeIsACommandFlagNotAShellBranch(t *testing.T) {
	yaml := workflowYAML(t)
	if !strings.Contains(yaml, "--scope '${{ github.event.client_payload.scope }}'") {
		t.Error("the scope does not reach the command as a flag")
	}
	for _, banned := range []string{"SCOPE=", `[ "$SCOPE"`} {
		if strings.Contains(yaml, banned) {
			t.Errorf("the workflow still branches on %q in shell", banned)
		}
	}
}

func TestWorkflowCloneStepSkipsASharedOnlyDispatch(t *testing.T) {
	yaml := workflowYAML(t)
	lines := strings.Split(yaml, "\n")
	for index, line := range lines {
		if !strings.Contains(line, "Clone source project") {
			continue
		}
		window := strings.Join(lines[index:min(index+3, len(lines))], "\n")
		if !strings.Contains(window, "github.event.client_payload.scope != 'shared-only'") {
			t.Fatalf("the clone step has no shared-only condition: %q", window)
		}
		return
	}
	t.Fatal("the clone step is not in the workflow")
}

func TestWorkflowEmbedsNoInlineInterpreter(t *testing.T) {
	// The deploy body is in a command, not in embedded interpreters.
	yaml := workflowYAML(t)
	for _, marker := range []string{
		"python3 -c", "python -c", "python3 -m", "python -m",
		"import json", "json.dump", "bash -c", "sh -c",
	} {
		if strings.Contains(yaml, marker) {
			t.Errorf("the workflow still embeds %q", marker)
		}
	}
}

func TestWorkflowEmbedsNoRecursiveDeletion(t *testing.T) {
	yaml := workflowYAML(t)
	for _, marker := range []string{"rm -rf", "rm -r ", "rm -f "} {
		if strings.Contains(yaml, marker) {
			t.Errorf("the workflow still embeds %q", marker)
		}
	}
}

func TestWorkflowEmbedsNoRetryLoopOrGitPlumbing(t *testing.T) {
	yaml := workflowYAML(t)
	for _, marker := range []string{
		"for attempt", "git fetch", "git reset", "git commit",
		"git push", "git config", "git add", "cp -r", "find ",
	} {
		if strings.Contains(yaml, marker) {
			t.Errorf("the workflow still embeds %q", marker)
		}
	}
}

func TestWorkflowStepCountIsThin(t *testing.T) {
	// checkout, setup-go, setup-python, install, clone, integrate, deploy --
	// and no more. The Python had six because it installed both tools from
	// one package manager; the Go binary needs a toolchain of its own.
	yaml := workflowYAML(t)
	steps := 0
	for _, line := range strings.Split(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- name:") || strings.HasPrefix(trimmed, "- uses:") {
			steps++
		}
	}
	if steps != 7 {
		t.Fatalf("the workflow has %d steps, want 7", steps)
	}
}

func TestWorkflowDeploysThroughWrangler(t *testing.T) {
	yaml := workflowYAML(t)
	want := "wrangler pages deploy site/ --project-name '" + testPagesProject + "'"
	if !strings.Contains(yaml, want) {
		t.Errorf("the workflow does not run %q", want)
	}
	for _, secret := range []string{"CF_ACCOUNT_ID", "CF_PAGES_API_TOKEN"} {
		if !strings.Contains(yaml, secret) {
			t.Errorf("the deploy step does not read %s", secret)
		}
	}
}

func TestWorkflowTrimsATrailingSlashFromTheCanonicalBase(t *testing.T) {
	yaml, err := GenerateWorkflowYAML(
		testPagesProject, testCanonicalBase+"/", testPins,
	)
	if err != nil {
		t.Fatalf("generating the workflow: %v", err)
	}
	if !strings.Contains(yaml, "--canonical-base '"+testCanonicalBase+"'") {
		t.Error("the trailing slash reached the generated command line")
	}
}

func TestWorkflowRefusesWithoutAPagesProject(t *testing.T) {
	if _, err := GenerateWorkflowYAML("", testCanonicalBase, testPins); err == nil {
		t.Fatal("a workflow was generated with no deploy target")
	}
}

func TestWorkflowRefusesWithoutACanonicalBase(t *testing.T) {
	if _, err := GenerateWorkflowYAML(testPagesProject, "", testPins); err == nil {
		t.Fatal("a workflow was generated with no canonical base")
	}
}

func TestWorkflowRefusesToGenerateWithoutPins(t *testing.T) {
	// The generator renders pins and never invents them.
	_, err := GenerateWorkflowYAML(
		testPagesProject, testCanonicalBase, ToolchainPins{},
	)
	if err == nil {
		t.Fatal("a workflow was generated with no pins")
	}
}

// -- assembly init -----------------------------------------------------------

// initFiles renders the four files a fresh assembly repository starts with.
func initFiles(t *testing.T) map[string]string {
	t.Helper()
	files, err := AssemblyInit(
		testPagesProject, testCanonicalBase, testPins,
	)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return files
}

func TestInitReturnsTheFourFiles(t *testing.T) {
	files := initFiles(t)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		site.WorkflowPath, ".gitignore", site.ProjectsPath, site.RosterPath,
	}
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("init wrote %v, want %v", names, want)
	}
}

func TestInitProjectsJSONIsAnEmptyObject(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(initFiles(t)[site.ProjectsPath]), &parsed); err != nil {
		t.Fatalf("the membership record is not JSON: %v", err)
	}
	if len(parsed) != 0 {
		t.Fatalf("a fresh assembly already records %v", parsed)
	}
}

func TestInitGitignoreCoversTheCIOnlyDirectories(t *testing.T) {
	gitignore := initFiles(t)[".gitignore"]
	for _, want := range []string{"node_modules", "dist/", "source/", ".wrangler/"} {
		if !strings.Contains(gitignore, want) {
			t.Errorf("the .gitignore does not cover %q", want)
		}
	}
}

func TestInitWorkflowMatchesTheGenerator(t *testing.T) {
	if got := initFiles(t)[site.WorkflowPath]; got != workflowYAML(t) {
		t.Fatal("init writes a workflow the generator would not produce")
	}
}

func TestInitRosterIsTheScaffoldedDeclaration(t *testing.T) {
	roster := initFiles(t)[site.RosterPath]
	if roster != site.RenderRoster(nil, "") {
		t.Fatal("init writes a roster the renderer would not produce")
	}
}

func TestInitRefusesIncompletePins(t *testing.T) {
	if _, err := AssemblyInit(
		testPagesProject, testCanonicalBase, ToolchainPins{Selfdoc: "1.0"},
	); err == nil {
		t.Fatal("a fresh assembly was scaffolded with an incomplete toolchain")
	}
}
