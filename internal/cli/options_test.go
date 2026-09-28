package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// Each lint is an option, selfdoc:<lint name>, filed in the repository's
// .strictmetadata/options/docs.toml. The check applies each entry's current
// value: off drops the lint, warn reports it without blocking, error is the
// registered severity.

// optionsDir is the repository's options directory.
func optionsDir(dir string) string {
	return filepath.Join(dir, ".strictmetadata", "options")
}

// writeOptions writes one subject document of the options directory, with the
// directory's manifest naming strictspec.
func writeOptions(t *testing.T, dir, file, content string) {
	t.Helper()
	writeText(t, filepath.Join(optionsDir(dir), "manifest.toml"), "owner = \"strictspec\"\n")
	writeText(t, filepath.Join(optionsDir(dir), file), content)
}

// entry renders one [[entry]] block.
func entry(id, current, ideal, reason string) string {
	return "[[entry]]\nid = \"" + id + "\"\ncurrent = \"" + current + "\"\nideal = \"" + ideal +
		"\"\nreason = \"" + reason + "\"\n"
}

// undescribedProject is an initialized project whose index page carries no
// description, so the check reports missing-frontmatter-description, an
// error-severity lint, and exits 1.
func undescribedProject(t *testing.T) string {
	t.Helper()
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	writeText(t, filepath.Join(dir, ".strictmetadata", "docs", "index.md"),
		"+++\ntitle = \"Test\"\n+++\n\n# Test\n\nContent.\n")
	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode != 1 || !strings.Contains(result.Stdout, "[missing-frontmatter-description]") {
		t.Fatalf("the undescribed project does not fail on missing-frontmatter-description: exit %d\n%s\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	return dir
}

// lintsNamed returns the payload's lints carrying the given name.
func lintsNamed(t *testing.T, payload map[string]any, name string) []map[string]any {
	t.Helper()
	var found []map[string]any
	for _, raw := range payload["lints"].([]any) {
		lint := raw.(map[string]any)
		if lint["name"] == name {
			found = append(found, lint)
		}
	}
	return found
}

func TestCheckReportsAnErrorLintAsANonBlockingWarningWhenItsOptionIsWarn(t *testing.T) {
	dir := undescribedProject(t)
	writeOptions(t, dir, "docs.toml", "format_version = 1\n\n"+
		entry("selfdoc:missing-frontmatter-description", "warn", "error", "the landing page is being rewritten"))

	result := run(t, dir, "check", "--json", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("exit %d, want 0 with the lint at warn:\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	found := lintsNamed(t, payloadOf(t, result), "missing-frontmatter-description")
	if len(found) == 0 {
		t.Fatal("a lint at warn was not reported")
	}
	for _, lint := range found {
		if lint["severity"] != "warning" {
			t.Errorf("severity = %v, want warning", lint["severity"])
		}
	}
}

func TestCheckDoesNotReportALintWhoseOptionIsOff(t *testing.T) {
	dir := undescribedProject(t)
	writeOptions(t, dir, "docs.toml", "format_version = 1\n\n"+
		entry("selfdoc:missing-frontmatter-description", "off", "error", "the landing page is being rewritten"))

	result := run(t, dir, "check", "--json", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("exit %d, want 0 with the lint off:\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if found := lintsNamed(t, payloadOf(t, result), "missing-frontmatter-description"); len(found) != 0 {
		t.Errorf("a lint that is off was reported: %v", found)
	}
}

// The post-build lint pass applies the same options as the check.
func TestBuildAppliesTheLintOptions(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	writeText(t, filepath.Join(dir, ".strictmetadata", "docs", "index.md"), "# Test\n\nContent.\n")
	if result := run(t, dir, "build", "--no-auto-commit"); result.ExitCode != 1 {
		t.Fatalf("the build of an undescribed page exited %d, want 1:\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	writeOptions(t, dir, "docs.toml", "format_version = 1\n\n"+
		entry("selfdoc:missing-frontmatter-description", "off", "error", "the landing page is being rewritten"))
	result := run(t, dir, "build", "--no-auto-commit")
	if result.ExitCode != 0 || strings.Contains(result.Stdout, "missing-frontmatter-description") {
		t.Errorf("the build with the lint off exited %d:\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
}

func TestCheckRefusesAnInvalidSelfdocEntryWithStrictspecsDiagnostic(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	writeOptions(t, dir, "docs.toml", "format_version = 1\n\n"+
		entry("selfdoc:missing-frontmatter-description", "loud", "error", "a value the option does not declare"))

	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "STRICTSPEC_OPTIONS_UNDECLARED_CURRENT") ||
		!strings.Contains(result.Stderr, ".strictmetadata/options/docs.toml") {
		t.Errorf("exit %d, want the strictspec refusal:\n%s", result.ExitCode, result.Stderr)
	}
}

// Another tool's entries are held to the shape every reader checks, and to
// nothing else: selfdoc does not judge another namespace.
func TestCheckValidatesOnlyTheShapeOfAnotherToolsEntries(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	writeOptions(t, dir, "changelog.toml", "format_version = 1\n\n"+
		entry("rlsbl:no-such-option", "whatever", "anything", "another tool judges this entry"))
	if result := run(t, dir, "check", "--no-auto-commit"); result.ExitCode != 0 {
		t.Errorf("another tool's entry was judged: exit %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}

	writeOptions(t, dir, "changelog.toml", entry("rlsbl:no-such-option", "whatever", "anything", "no format_version"))
	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "STRICTSPEC_GATE_ABSENT") ||
		!strings.Contains(result.Stderr, "changelog.toml") {
		t.Errorf("a document of the wrong shape was accepted: exit %d\n%s", result.ExitCode, result.Stderr)
	}
}

// Every refusal of a selfdoc entry that names a fix is cleared by that fix.
func TestEveryNamedFixOfARefusedEntryClearsIt(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	const header = "format_version = 1\n\n"
	good := entry("selfdoc:low-numeric-data-density", "off", "off", "reference pages list no quantities")
	for _, tc := range []struct {
		name   string
		files  map[string]string
		wants  []string
		fixed  map[string]string
		remove []string
	}{
		{
			name:   "an entry in the wrong subject file is moved to the one named",
			files:  map[string]string{"changelog.toml": header + good},
			wants:  []string{"STRICTSPEC_OPTIONS_WRONG_SUBJECT", "belongs in .strictmetadata/options/docs.toml; move it there"},
			fixed:  map[string]string{"docs.toml": header + good},
			remove: []string{"changelog.toml"},
		},
		{
			name:  "a scope on an option that takes none is removed",
			files: map[string]string{"docs.toml": header + strings.Replace(good, "[[entry]]\n", "[[entry]]\nscope = \"docs/api\"\n", 1)},
			wants: []string{"STRICTSPEC_OPTIONS_SCOPE_NOT_ACCEPTED", "remove the scope"},
			fixed: map[string]string{"docs.toml": header + good},
		},
		{
			name: "a redundant entry is removed",
			files: map[string]string{"docs.toml": header + good + "\n" +
				entry("selfdoc:missing-frontmatter-description", "error", "error", "restates the default")},
			wants: []string{"STRICTSPEC_OPTIONS_REDUNDANT", "remove the entry"},
			fixed: map[string]string{"docs.toml": header + good},
		},
		{
			name:  "a repeated entry loses one copy",
			files: map[string]string{"docs.toml": header + good + "\n" + good},
			wants: []string{"STRICTSPEC_OPTIONS_DUPLICATE_ENTRY", "remove one of them"},
			fixed: map[string]string{"docs.toml": header + good},
		},
		{
			name: "a misspelled option takes the suggested name",
			files: map[string]string{"docs.toml": header +
				entry("selfdoc:low-numeric-data-densty", "off", "off", "reference pages list no quantities")},
			wants: []string{"STRICTSPEC_OPTIONS_UNKNOWN_OPTION", `Did you mean "selfdoc:low-numeric-data-density"?`},
			fixed: map[string]string{"docs.toml": header + good},
		},
		{
			name: "an ideal the ranking does not declare is replaced by a declared value",
			files: map[string]string{"docs.toml": header +
				entry("selfdoc:low-numeric-data-density", "off", "loud", "reference pages list no quantities")},
			wants: []string{"STRICTSPEC_OPTIONS_UNDECLARED_IDEAL", `"warn > off"`},
			fixed: map[string]string{"docs.toml": header + good},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := initialized(t)
			for file, content := range tc.files {
				writeOptions(t, dir, file, content)
			}
			refused := run(t, dir, "check", "--no-auto-commit")
			if refused.ExitCode == 0 {
				t.Fatalf("the entry was accepted:\n%s", refused.Stdout)
			}
			for _, want := range tc.wants {
				if !strings.Contains(refused.Stderr, want) {
					t.Errorf("the refusal does not carry %q:\n%s", want, refused.Stderr)
				}
			}
			for _, file := range tc.remove {
				if err := os.Remove(filepath.Join(optionsDir(dir), file)); err != nil {
					t.Fatal(err)
				}
			}
			for file, content := range tc.fixed {
				writeOptions(t, dir, file, content)
			}
			if fixed := run(t, dir, "check", "--no-auto-commit"); fixed.ExitCode != 0 {
				t.Errorf("the fix did not clear the refusal: exit %d\n%s\n%s", fixed.ExitCode, fixed.Stdout, fixed.Stderr)
			}
		})
	}
}

func TestCheckNoLongerAcceptsAnIgnoreFlag(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	result := run(t, dir, "check", "--ignore", "low-numeric-data-density", "--no-auto-commit")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "--ignore") {
		t.Errorf("--ignore was accepted: exit %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
}

// A selfdoc.json still carrying lint_ignore is refused with the options entry
// to write for each code it names, as the exact command that writes it.
// Running those commands and removing the key clears the refusal.
func TestLintIgnoreIsRefusedNamingTheOptionsEntriesAndTheFixClearsIt(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	dir := initialized(t)
	configPath := filepath.Join(dir, "selfdoc.json")
	config := readJSON(t, configPath)
	config["lint_ignore"] = []any{"SEO007", "low-numeric-data-density"}
	testproject.WriteJSON(t, configPath, config)

	refused := run(t, dir, "check", "--no-auto-commit")
	if refused.ExitCode == 0 {
		t.Fatal("a selfdoc.json carrying lint_ignore was accepted")
	}
	commands := []string{
		"selfdoc options set selfdoc:first-paragraph-length-out-of-range --current off --ideal off --reason <text>",
		"selfdoc options set selfdoc:low-numeric-data-density --current off --ideal off --reason <text>",
	}
	for _, want := range append([]string{"lint_ignore"}, commands...) {
		if !strings.Contains(refused.Stderr, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, refused.Stderr)
		}
	}

	for _, command := range commands {
		argv := strings.Fields(strings.TrimPrefix(command, "selfdoc "))
		argv = append(argv[:len(argv)-1], "this project's reference pages carry no quantities", "--no-auto-commit")
		if result := run(t, dir, argv...); result.ExitCode != 0 {
			t.Fatalf("%v exited %d:\n%s", argv, result.ExitCode, result.Stderr)
		}
	}
	delete(config, "lint_ignore")
	testproject.WriteJSON(t, configPath, config)
	fixed := run(t, dir, "check", "--no-auto-commit")
	if fixed.ExitCode != 0 {
		t.Fatalf("the fix did not clear the refusal: exit %d\n%s\n%s", fixed.ExitCode, fixed.Stdout, fixed.Stderr)
	}
	for _, name := range []string{"first-paragraph-length-out-of-range", "low-numeric-data-density"} {
		if strings.Contains(fixed.Stdout, "["+name+"]") {
			t.Errorf("%s is still reported after its entry turned it off:\n%s", name, fixed.Stdout)
		}
	}
}

// A lint_ignore naming no lint at all is refused without a command to run.
func TestLintIgnoreNamingNoLintIsRefusedAsSuch(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	configPath := filepath.Join(dir, "selfdoc.json")
	config := readJSON(t, configPath)
	config["lint_ignore"] = []any{"SEO0O8"}
	testproject.WriteJSON(t, configPath, config)
	result := run(t, dir, "check", "--no-auto-commit")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "SEO0O8") ||
		!strings.Contains(result.Stderr, "names no lint") {
		t.Errorf("exit %d, want the refusal naming the unknown code:\n%s", result.ExitCode, result.Stderr)
	}
}

func TestOptionsSetCreatesTheDirectoryItsManifestAndTheEntry(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	testproject.Git(t, dir, "init", "-q")
	testproject.Git(t, dir, "add", "-A")
	testproject.Git(t, dir, "commit", "-q", "-m", "initial")

	result := run(t, dir, "--json", "options", "set", "selfdoc:low-numeric-data-density",
		"--current", "off", "--ideal", "off", "--reason", "reference pages list no quantities")
	if result.ExitCode != 0 {
		t.Fatalf("exit %d:\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if got := readText(t, filepath.Join(optionsDir(dir), "manifest.toml")); got != "owner = \"strictspec\"\n" {
		t.Errorf("manifest.toml = %q", got)
	}
	docs := readText(t, filepath.Join(optionsDir(dir), "docs.toml"))
	for _, want := range []string{"format_version = 1", `id = "selfdoc:low-numeric-data-density"`,
		`current = "off"`, `ideal = "off"`, `reason = "reference pages list no quantities"`} {
		if !strings.Contains(docs, want) {
			t.Errorf("docs.toml does not carry %q:\n%s", want, docs)
		}
	}
	payload := payloadOf(t, result)
	if payload["id"] != "selfdoc:low-numeric-data-density" || payload["file"] != ".strictmetadata/options/docs.toml" ||
		payload["action"] != "created" || payload["class"] != "settled" {
		t.Errorf("payload = %v", payload)
	}
	if status := gitOutput(t, dir, "status", "--porcelain", "--", ".strictmetadata/options"); strings.TrimSpace(status) != "" {
		t.Errorf("the entry was not committed:\n%s", status)
	}
	if subject := gitOutput(t, dir, "log", "-1", "--format=%s"); !strings.Contains(subject, "selfdoc options set selfdoc:low-numeric-data-density") {
		t.Errorf("the last commit is %q", subject)
	}

	// The entry it wrote is the one the check reads.
	if check := run(t, dir, "layout", "validate"); check.ExitCode != 0 {
		t.Errorf("layout validate after options set exited %d:\n%s", check.ExitCode, check.Stderr)
	}
}

func TestOptionsSetUpdatesAnExistingEntryAndKeepsTheRest(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	other := entry("othertool:docs-width", "narrow", "wide", "another tool's entry")
	writeOptions(t, dir, "docs.toml", "format_version = 1\n\n# the docs subject\n"+
		entry("selfdoc:low-numeric-data-density", "off", "warn", "first reason")+"\n"+other)

	result := run(t, dir, "--json", "options", "set", "selfdoc:low-numeric-data-density",
		"--current", "off", "--ideal", "off", "--reason", "second reason", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("exit %d:\n%s", result.ExitCode, result.Stderr)
	}
	docs := readText(t, filepath.Join(optionsDir(dir), "docs.toml"))
	if strings.Count(docs, "selfdoc:low-numeric-data-density") != 1 || !strings.Contains(docs, `reason = "second reason"`) ||
		!strings.Contains(docs, `ideal = "off"`) || strings.Contains(docs, "first reason") {
		t.Errorf("the entry was not updated in place:\n%s", docs)
	}
	if !strings.Contains(docs, other) || !strings.Contains(docs, "# the docs subject") {
		t.Errorf("the rest of the document was not kept:\n%s", docs)
	}
	if payloadOf(t, result)["action"] != "updated" {
		t.Errorf("payload = %v", payloadOf(t, result))
	}
}

func TestOptionsSetRefusesAnotherToolsNamespace(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	result := run(t, dir, "options", "set", "rlsbl:changelog-coverage",
		"--current", "off", "--ideal", "on", "--reason", "a fork", "--no-auto-commit")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "selfdoc:") || !strings.Contains(result.Stderr, "rlsbl") {
		t.Errorf("exit %d, want a refusal naming the namespace:\n%s", result.ExitCode, result.Stderr)
	}
	if exists(optionsDir(dir)) {
		t.Error("the refused command wrote the options directory")
	}
}

func TestOptionsSetRefusesAnInvalidEntryWithStrictspecsDiagnosticAndWritesNothing(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"selfdoc:low-numeric-data-density", "--current", "error", "--ideal", "off", "--reason", "r"}, "STRICTSPEC_OPTIONS_UNDECLARED_CURRENT"},
		{[]string{"selfdoc:low-numeric-data-density", "--current", "off", "--ideal", "warn", "--reason", ""}, "reason"},
		{[]string{"selfdoc:low-numeric-data-density", "--current", "warn", "--ideal", "off", "--reason", "r"}, "STRICTSPEC_OPTIONS_CURRENT_ABOVE_IDEAL"},
		{[]string{"selfdoc:low-numeric-data-density", "--current", "warn", "--ideal", "warn", "--reason", "r"}, "STRICTSPEC_OPTIONS_REDUNDANT"},
		{[]string{"selfdoc:no-such-lint", "--current", "off", "--ideal", "off", "--reason", "r"}, "STRICTSPEC_OPTIONS_UNKNOWN_OPTION"},
	} {
		argv := append([]string{"options", "set"}, append(tc.argv, "--no-auto-commit")...)
		result := run(t, dir, argv...)
		if result.ExitCode == 0 || !strings.Contains(result.Stderr, tc.want) {
			t.Errorf("%v: exit %d, want %q:\n%s", tc.argv, result.ExitCode, tc.want, result.Stderr)
		}
	}
	if exists(optionsDir(dir)) {
		t.Error("a refused command wrote the options directory")
	}
}

func TestOptionsSetDryRunWritesNothing(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	result := run(t, dir, "options", "set", "selfdoc:low-numeric-data-density",
		"--current", "off", "--ideal", "off", "--reason", "reference pages list no quantities", "--dry-run")
	if result.ExitCode != 0 {
		t.Fatalf("exit %d:\n%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "docs.toml") {
		t.Errorf("the dry run does not name the file it would write:\n%s", result.Stdout)
	}
	if exists(optionsDir(dir)) {
		t.Error("the dry run wrote the options directory")
	}
}

func TestOptionsSetRefusesADirectoryAnotherOwnerClaims(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	writeText(t, filepath.Join(optionsDir(dir), "manifest.toml"), "owner = \"othertool\"\n")
	result := run(t, dir, "options", "set", "selfdoc:low-numeric-data-density",
		"--current", "off", "--ideal", "off", "--reason", "r", "--no-auto-commit")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "strictspec") || !strings.Contains(result.Stderr, "othertool") {
		t.Errorf("exit %d, want a refusal naming both owners:\n%s", result.ExitCode, result.Stderr)
	}
	if exists(filepath.Join(optionsDir(dir), "docs.toml")) {
		t.Error("the refused command wrote docs.toml")
	}
	// The refusal names the line to write; writing it clears the refusal.
	if !strings.Contains(result.Stderr, "owner = \"strictspec\"") {
		t.Fatalf("the refusal does not name the line to write:\n%s", result.Stderr)
	}
	writeText(t, filepath.Join(optionsDir(dir), "manifest.toml"), "owner = \"strictspec\"\n")
	fixed := run(t, dir, "options", "set", "selfdoc:low-numeric-data-density",
		"--current", "off", "--ideal", "off", "--reason", "r", "--no-auto-commit")
	if fixed.ExitCode != 0 {
		t.Errorf("the fix did not clear the refusal: exit %d\n%s", fixed.ExitCode, fixed.Stderr)
	}
}

// An ideal of non-existent, as the --ideal help offers, records a value
// selfdoc does not offer yet: the entry is waiting on the tool.
func TestOptionsSetAcceptsAnIdealSelfdocDoesNotOfferYet(t *testing.T) {
	isolate(t)
	dir := initialized(t)
	result := run(t, dir, "--json", "options", "set", "selfdoc:low-numeric-data-density",
		"--current", "off", "--ideal", "non-existent", "--reason", "the lint should count table cells", "--no-auto-commit")
	if result.ExitCode != 0 {
		t.Fatalf("exit %d:\n%s", result.ExitCode, result.Stderr)
	}
	if class := payloadOf(t, result)["class"]; class != "waiting-on-tool" {
		t.Errorf("class = %v, want waiting-on-tool", class)
	}
	if check := run(t, dir, "check", "--json", "--no-auto-commit"); strings.Contains(check.Stderr, "STRICTSPEC_") {
		t.Errorf("the check refuses the entry options set wrote:\n%s", check.Stderr)
	}
}
