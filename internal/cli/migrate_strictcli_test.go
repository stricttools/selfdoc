package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/strictclisupport"
	"github.com/stricttools/selfdoc/internal/testproject"
)

// `selfdoc layout migrate` also moves strictcli's files out of every
// .strictcli/ directory into the .strictmetadata/ beside it, and converts the
// ignore file an earlier selfdoc derived at the top of .strictmetadata/ into
// one per uncommitted directory.

// stubModule is the stub go.mod rlsbl scaffolds into a private directory.
const stubModule = "module private.invalid/rlsbl-private\n"

// cliSchema is a strictcli schema whose project_id is the fixture's project.
const cliSchema = `{
  "schema_version": 2,
  "project_id": "testproj",
  "name": "testproj",
  "version": "1.0.0",
  "help": "A test program",
  "commands": {},
  "groups": {}
}
`

// strictcliProject is a committed repository on this layout as an earlier
// selfdoc and strictcli left it: the ignore file derived at the top of
// .strictmetadata/, a root .strictcli/ holding the schema, the coverage
// manifest, the stub module and an ignored shard, a nested .strictcli/ holding
// only an empty coverage/, rlsbl's scaffold base of the stub and its
// managed-files entry, and a root .gitignore whose coverage/ line ignored the
// shards.
func strictcliProject(t *testing.T) string {
	t.Helper()
	dir := initialized(t)
	removeFixtureFile(t, layout.Path(dir, layout.DirectoryIgnoreRel(layout.DocsCacheName)))
	writeText(t, layout.Path(dir, layout.RootIgnoreRel),
		"# BEGIN selfdoc -- derived from selfdoc's layout declaration\n.docs-cache/*\n!.docs-cache/manifest.toml\n# END selfdoc\n")
	writeText(t, layout.Path(dir, layout.Root+"/go.mod"), stubModule)
	writeText(t, filepath.Join(dir, ".strictcli", "schema.json"), cliSchema)
	writeText(t, filepath.Join(dir, ".strictcli", "test-coverage.json"), "[\n  \"greet\"\n]\n")
	writeText(t, filepath.Join(dir, ".strictcli", "go.mod"), stubModule)
	writeText(t, filepath.Join(dir, ".strictcli", "coverage", "4242.jsonl"), "{\"command\":\"greet\"}\n")
	if err := os.MkdirAll(filepath.Join(dir, "pkg", ".strictcli", "coverage"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(dir, ".rlsbl", "bases", ".strictcli", "go.mod"), stubModule)
	writeText(t, filepath.Join(dir, ".rlsbl", "managed-files.json"),
		"{\n  \"version\": 1,\n  \"files\": {\n    \".gitignore\": \"aa\",\n    \".strictcli/go.mod\": \"bb\"\n  }\n}\n")
	writeText(t, filepath.Join(dir, ".gitignore"), "node_modules/\ncoverage/\ndist/\n")
	testproject.Git(t, dir, "init", "-q")
	testproject.Git(t, dir, "add", "-A")
	testproject.Git(t, dir, "commit", "-q", "-m", "the layout before strictcli's files moved")
	return dir
}

// removeFixtureFile removes a file a fixture does not want, inside the
// fixture's own temporary directory.
func removeFixtureFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateDryRunPlansTheStrictcliMoveAndChangesNothing(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	before := treeListing(t, dir)
	result := run(t, dir, "layout", "migrate", "--dry-run")
	if result.ExitCode != 0 {
		t.Fatalf("the dry run failed:\n%s\n%s", result.Stdout, result.Stderr)
	}
	for _, want := range []string{
		"move .strictcli/schema.json -> .strictmetadata/.cli-schema/schema.json",
		"move .strictcli/test-coverage.json -> .strictmetadata/.cli-test-coverage/manifest.json",
		"move 1 coverage shard file(s) .strictcli/coverage/ -> .strictmetadata/.cli-test-coverage/shards/",
		"write .strictmetadata/.cli-schema/manifest.toml",
		"write .strictmetadata/.cli-test-coverage/manifest.toml",
		"write .strictmetadata/.cli-test-coverage/.gitignore",
		"write .strictmetadata/.docs-cache/.gitignore",
		"delete .strictmetadata/.gitignore",
		"delete .strictcli/go.mod",
		"delete .rlsbl/bases/.strictcli/go.mod",
		`rewrite .rlsbl/managed-files.json: the ".strictcli/go.mod" entry removed`,
		"rewrite .gitignore: the coverage/ line removed",
		"remove .strictcli/coverage/",
		"remove .strictcli/",
		"remove pkg/.strictcli/coverage/",
		"remove pkg/.strictcli/",
		"remove .rlsbl/bases/.strictcli/",
	} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("the plan does not carry %q:\n%s", want, result.Stdout)
		}
	}
	if strings.Contains(result.Stdout, "pkg/.strictmetadata") {
		t.Errorf("an empty nested .strictcli/ got a .strictmetadata/ of its own:\n%s", result.Stdout)
	}
	if after := treeListing(t, dir); after != before {
		t.Errorf("the dry run changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMigrateMovesStrictcliFilesAndCommits(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	human := run(t, dir, "layout", "migrate")
	if human.ExitCode != 0 {
		t.Fatalf("the move failed:\n%s\n%s", human.Stdout, human.Stderr)
	}
	if !strings.Contains(human.Stdout, "Moved strictcli's files out of 2 .strictcli/ directory(s).") {
		t.Errorf("the report miscounts the .strictcli/ directories:\n%s", human.Stdout)
	}
	if got := readText(t, filepath.Join(dir, ".strictmetadata", ".cli-schema", "schema.json")); got != cliSchema {
		t.Errorf("the schema moved as %q", got)
	}
	if got := readText(t, filepath.Join(dir, ".strictmetadata", ".cli-test-coverage", "manifest.json")); got != "[\n  \"greet\"\n]\n" {
		t.Errorf("the coverage manifest moved as %q", got)
	}
	if !exists(filepath.Join(dir, ".strictmetadata", ".cli-test-coverage", "shards", "4242.jsonl")) {
		t.Error("the shard did not move")
	}
	for _, owned := range []string{".cli-schema", ".cli-test-coverage"} {
		raw := readText(t, filepath.Join(dir, ".strictmetadata", owned, layout.ManifestFileName))
		if raw != layout.DirectoryManifestContent(layout.CLIOwner) {
			t.Errorf("%s/manifest.toml = %q, want strictcli's grant", owned, raw)
		}
	}
	if got := readText(t, filepath.Join(dir, ".strictmetadata", ".cli-test-coverage", ".gitignore")); got != "shards/\n" {
		t.Errorf("the coverage directory's ignore file is %q", got)
	}
	for _, gone := range []string{".strictcli", "pkg/.strictcli", ".rlsbl/bases/.strictcli", layout.RootIgnoreRel} {
		if exists(filepath.Join(dir, filepath.FromSlash(gone))) {
			t.Errorf("%s is still there", gone)
		}
	}
	if exists(filepath.Join(dir, "pkg", ".strictmetadata")) {
		t.Error("an empty nested .strictcli/ got a .strictmetadata/ of its own")
	}
	if got := readText(t, filepath.Join(dir, ".gitignore")); got != "node_modules/\ndist/\n" {
		t.Errorf(".gitignore = %q, want the coverage/ line gone and the rest kept", got)
	}
	var managed map[string]any
	if err := json.Unmarshal([]byte(readText(t, filepath.Join(dir, ".rlsbl", "managed-files.json"))), &managed); err != nil {
		t.Fatalf("managed-files.json is no longer JSON: %v", err)
	}
	if files, _ := managed["files"].(map[string]any); files == nil || files[".strictcli/go.mod"] != nil || files[".gitignore"] != "aa" {
		t.Errorf("managed-files.json files = %v", managed["files"])
	}
	if got := readText(t, layout.Path(dir, layout.DirectoryIgnoreRel(layout.DocsCacheName))); got != layout.DirectoryIgnoreContent() {
		t.Errorf("the cache directory's ignore file is %q", got)
	}

	// Committed, with nothing left over but the ignored shard.
	if status := gitOutput(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("the move left uncommitted changes:\n%s", status)
	}
	if tracked := gitOutput(t, dir, "ls-files", ".strictmetadata/.cli-test-coverage/shards"); tracked != "" {
		t.Errorf("the shard was committed: %s", tracked)
	}

	// The result is a layout selfdoc accepts, the schema is read where it now
	// sits, and a second run has nothing to do.
	if validate := run(t, dir, "layout", "validate"); validate.ExitCode != 0 {
		t.Errorf("layout validate after the move exited %d:\n%s", validate.ExitCode, validate.Stderr)
	}
	if !strictclisupport.UsesStrictcli(nil, dir) {
		t.Error("the moved schema is not read")
	}
	again := run(t, dir, "layout", "migrate", "--dry-run")
	if again.ExitCode == 0 || !strings.Contains(again.Stderr, "Nothing to migrate") {
		t.Errorf("a second run did not refuse as having nothing to do:\n%s\n%s", again.Stdout, again.Stderr)
	}
}

// A file the move does not know the place of is refused by name; moving it
// away lets the move through.
func TestMigrateRefusesAStrictcliFileItCannotPlace(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	writeText(t, filepath.Join(dir, ".strictcli", "checks.toml"), "app = \"testproj\"\n")
	testproject.Git(t, dir, "add", ".strictcli/checks.toml")
	testproject.Git(t, dir, "commit", "-q", "-m", "a checks file")
	before := treeListing(t, dir)
	result := run(t, dir, "layout", "migrate", "--dry-run")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, ".strictcli/checks.toml") {
		t.Fatalf("the move did not refuse naming the file:\n%s\n%s", result.Stdout, result.Stderr)
	}
	if after := treeListing(t, dir); after != before {
		t.Error("the refused move changed the tree")
	}
	testproject.Git(t, dir, "mv", ".strictcli/checks.toml", "checks.toml")
	testproject.Git(t, dir, "commit", "-q", "-m", "the checks file moves")
	if result := run(t, dir, "layout", "migrate"); result.ExitCode != 0 {
		t.Fatalf("the remedy did not let the move through:\n%s\n%s", result.Stdout, result.Stderr)
	}
}

// A schema git does not track is not carried into a committed directory.
func TestMigrateRefusesAnUntrackedStrictcliSchema(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	writeText(t, filepath.Join(dir, "pkg", ".strictcli", "schema.json"), cliSchema)
	result := run(t, dir, "layout", "migrate", "--dry-run")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "pkg/.strictcli/schema.json") {
		t.Fatalf("the move did not refuse the untracked schema:\n%s\n%s", result.Stdout, result.Stderr)
	}
}

// A file whose new place is already taken is a move begun and not finished.
func TestMigrateRefusesAStrictcliFileWhoseNewPlaceIsTaken(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	writeText(t, filepath.Join(dir, ".strictmetadata", ".cli-schema", "schema.json"), cliSchema)
	result := run(t, dir, "layout", "migrate", "--dry-run")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, ".strictmetadata/.cli-schema/schema.json") {
		t.Fatalf("the move did not refuse the taken place:\n%s\n%s", result.Stdout, result.Stderr)
	}
}

// Another tool's lines in the ignore file at the top of .strictmetadata/ are
// refused by content; moving them out lets the move through.
func TestMigrateRefusesAnotherToolsLinesInTheRootIgnoreFile(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	ignore := layout.Path(dir, layout.RootIgnoreRel)
	writeText(t, ignore, readText(t, ignore)+"\n# BEGIN othertool\nother/cache/\n# END othertool\n")
	result := run(t, dir, "layout", "migrate", "--dry-run")
	if result.ExitCode == 0 || !strings.Contains(result.Stderr, "other/cache/") {
		t.Fatalf("the move did not refuse the other tool's lines:\n%s\n%s", result.Stdout, result.Stderr)
	}
	writeText(t, ignore, "# BEGIN selfdoc -- derived from selfdoc's layout declaration\n.docs-cache/*\n# END selfdoc\n")
	if result := run(t, dir, "layout", "migrate", "--dry-run"); result.ExitCode != 0 {
		t.Fatalf("the remedy did not let the move through:\n%s\n%s", result.Stdout, result.Stderr)
	}
}

// A .strictcli/ inside a directory git ignores is not the repository's, and
// the move leaves it alone rather than refusing over it.
func TestMigrateLeavesAStrictcliInsideAnIgnoredDirectoryAlone(t *testing.T) {
	isolate(t)
	dir := strictcliProject(t)
	writeText(t, filepath.Join(dir, "scratch.local-only", "probe", ".strictcli", "schema.json"), cliSchema)
	writeText(t, filepath.Join(dir, "local", ".strictcli", "schema.json"), cliSchema)
	writeText(t, filepath.Join(dir, "local", ".gitignore"), ".strictcli/\n")
	writeText(t, filepath.Join(dir, ".gitignore"), "node_modules/\ncoverage/\ndist/\n*.local-only\n")
	testproject.Git(t, dir, "add", ".gitignore", "local/.gitignore")
	testproject.Git(t, dir, "commit", "-q", "-m", "ignored scratch")
	result := run(t, dir, "layout", "migrate")
	if result.ExitCode != 0 {
		t.Fatalf("the move refused over an ignored .strictcli/:\n%s\n%s", result.Stdout, result.Stderr)
	}
	for _, kept := range []string{"scratch.local-only/probe/.strictcli/schema.json", "local/.strictcli/schema.json"} {
		if !exists(filepath.Join(dir, filepath.FromSlash(kept))) {
			t.Errorf("%s was touched", kept)
		}
	}
}
