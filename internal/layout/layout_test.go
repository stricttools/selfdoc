package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/scripts"
	"github.com/smm-h/stricttest/go/hygiene"
)

// grant writes one directory's ownership manifest, which is what makes the
// directory exist and what permits its owner to write into it.
func grant(t *testing.T, dir, name, owner string) {
	t.Helper()
	write(t, DirectoryManifestPath(dir, name), DirectoryManifestContent(owner))
}

// owned is a repository that grants selfdoc every directory it claims.
func owned(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, claimed := range Declared() {
		grant(t, dir, claimed.Name, Owner)
	}
	return dir
}

// rooted is a repository that has the tool-state directory and nothing in it,
// so a test can grant exactly what it wants to grant.
func rooted(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, Root), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", Root, err)
	}
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestTheDeclarationCoversEveryDirectorySelfdocWrites(t *testing.T) {
	hygiene.Isolate(t)
	claimed := map[string]bool{}
	for _, dir := range Declared() {
		claimed[dir.Name] = true
	}
	for _, rel := range []string{
		DocsRel, GeneratedPagesRel, ManifestRel, PostManifestRel, RevisionsRel,
		HashesRel, DataRel, OutputRel, VersionsRel, PostsRel, VocabularyRel,
	} {
		name, ok := FunctionOf(rel)
		if !ok {
			t.Errorf("%s is not a path under %s", rel, Root)
			continue
		}
		if !claimed[name] {
			t.Errorf("%s sits in %s, which the declaration does not claim", rel, name)
		}
	}
}

// The manifest is a closed record: the owner is the whole of what it declares,
// so a key nobody reads is refused rather than silently ignored.
func TestTheManifestRefusesAnUnknownKey(t *testing.T) {
	hygiene.Isolate(t)
	dir := rooted(t)
	write(t, DirectoryManifestPath(dir, DocsName), "owner = \"selfdoc\"\nsince = \"2026\"\n")
	_, err := ReadDirectoryManifest(dir, DocsName)
	if err == nil {
		t.Fatal("a manifest carrying an undeclared key was accepted")
	}
	if !strings.Contains(err.Error(), "since") {
		t.Errorf("the refusal does not name the undeclared key: %v", err)
	}
}

func TestTheManifestNeedsAnOwner(t *testing.T) {
	hygiene.Isolate(t)
	dir := rooted(t)
	write(t, DirectoryManifestPath(dir, DocsName), "\n")
	_, err := ReadDirectoryManifest(dir, DocsName)
	if err == nil {
		t.Fatal("a manifest declaring no owner was accepted")
	}
	if !strings.Contains(err.Error(), OwnerKey) {
		t.Errorf("the refusal does not name the missing key: %v", err)
	}
}

// The format-version gate has exactly one author: the reader supplies it, so a
// manifest that writes it is refused rather than accepted twice over.
func TestTheManifestRefusesTheReaderSuppliedGate(t *testing.T) {
	hygiene.Isolate(t)
	dir := rooted(t)
	write(t, DirectoryManifestPath(dir, DocsName), "format_version = 1\nowner = \"selfdoc\"\n")
	_, err := ReadDirectoryManifest(dir, DocsName)
	if err == nil {
		t.Fatal("a manifest declaring the reader-supplied gate was accepted")
	}
	if !strings.Contains(err.Error(), "format_version") {
		t.Errorf("the refusal does not name the key: %v", err)
	}
}

func TestCreatingADirectoryNeedsItsManifest(t *testing.T) {
	hygiene.Isolate(t)
	dir := rooted(t)
	grant(t, dir, PostsName, Owner)

	err := EnsureDir(effects.Unbound(), dir, DocsStateRel)
	if err == nil {
		t.Fatal("a directory with no manifest was written into")
	}
	for _, want := range []string{DirectoryManifestRel(DocsStateName), `owner = "selfdoc"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
	if _, statErr := os.Stat(Path(dir, DocsStateRel)); !os.IsNotExist(statErr) {
		t.Errorf("the directory was created anyway (stat err = %v)", statErr)
	}
}

func TestADirectoryAnotherToolOwnsIsRefused(t *testing.T) {
	hygiene.Isolate(t)
	dir := rooted(t)
	grant(t, dir, DocsStateName, "someothertool")
	err := EnsureDir(effects.Unbound(), dir, DocsStateRel)
	if err == nil || !strings.Contains(err.Error(), "someothertool") {
		t.Fatalf("err = %v, want a refusal naming the declared owner", err)
	}
}

func TestCreatingADirectoryWritesTheDerivedIgnoreFile(t *testing.T) {
	hygiene.Isolate(t)
	dir := owned(t)
	if err := EnsureDir(effects.Unbound(), dir, OutputRel); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if info, err := os.Stat(Path(dir, OutputRel)); err != nil || !info.IsDir() {
		t.Fatalf("the output directory was not created: %v", err)
	}
	ignore := read(t, IgnorePath(dir))
	if !strings.Contains(ignore, DocsCacheName+"/*") {
		t.Errorf("the derived ignore file does not ignore the uncommitted directory:\n%s", ignore)
	}
	// The permission travels with the repository even though the contents do
	// not, so a fresh checkout does not have to be granted again.
	if !strings.Contains(ignore, "!"+DocsCacheName+"/"+ManifestFileName) {
		t.Errorf("the derived ignore file swallows the uncommitted directory's manifest:\n%s", ignore)
	}
	if strings.Contains(ignore, DocsStateName+"/") {
		t.Errorf("the derived ignore file ignores a committed directory:\n%s", ignore)
	}
}

func TestTheDerivedIgnoreFileLeavesOtherToolsLinesAlone(t *testing.T) {
	hygiene.Isolate(t)
	existing := "# BEGIN othertool\nother-cache/\n# END othertool\n"
	rendered := RenderIgnore(existing)
	for _, want := range []string{"# BEGIN othertool", "other-cache/", "# END othertool", DocsCacheName + "/*"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the rendered ignore file lost %q:\n%s", want, rendered)
		}
	}
	// Rendering what was rendered changes nothing: the block is replaced in
	// place rather than appended again.
	if again := RenderIgnore(rendered); again != rendered {
		t.Errorf("a second render differs:\n%s\n---\n%s", rendered, again)
	}
}

func TestTheOldLayoutIsRefused(t *testing.T) {
	hygiene.Isolate(t)
	for _, testCase := range []struct {
		name                          string
		deprecated                    string
		docs, output, posts, wantName string
	}{
		{
			name:       "the tool-state directory selfdoc used before",
			deprecated: DeprecatedRoot,
			wantName:   DeprecatedRoot + "/",
		},
		{
			name:     "a docs path outside the layout",
			docs:     "docs/",
			wantName: `"docs"`,
		},
		{
			name:     "an output path outside the layout",
			output:   "docs/_build/",
			wantName: `"output"`,
		},
		{
			name:     "a posts path outside the layout",
			posts:    "posts/",
			wantName: `"posts.dir"`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			if testCase.deprecated != "" {
				if err := os.MkdirAll(filepath.Join(dir, testCase.deprecated), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			err := RefuseOldLayout(dir, testCase.docs, testCase.output, testCase.posts)
			if err == nil {
				t.Fatal("the old layout was accepted")
			}
			if !strings.Contains(err.Error(), testCase.wantName) {
				t.Errorf("the refusal does not name what it found: %v", err)
			}
			if !strings.Contains(err.Error(), scripts.Move) {
				t.Errorf("the refusal does not name the move script: %v", err)
			}
		})
	}
}

// TestTheOldLayoutRefusalPrintsAnExecutableRemedy asserts that the remedy the
// refusal prints can be executed as printed by the repository being refused.
//
// The move script lives in selfdoc's own checkout and no release artifact
// carries it, so a refusal naming "python3 scripts/<name>" named a path the
// refused repository does not have: the printed remedy has to fetch the script
// before it runs it.
func TestTheOldLayoutRefusalPrintsAnExecutableRemedy(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DeprecatedRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	err := RefuseOldLayout(dir, "", "", "")
	if err == nil {
		t.Fatal("the old layout was accepted")
	}
	message := err.Error()
	for _, want := range []string{
		scripts.Fetch(scripts.Move),
		scripts.Run(scripts.Move, "--dry-run"),
		scripts.Run(scripts.Move, "--apply"),
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not print %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, "python3 "+scripts.RepoPath(scripts.Move)) {
		t.Errorf("the refusal tells the repository to run a path it does not have:\n%s", message)
	}
}

// TestTheOldLayoutRefusalPrintsTheWholeMigrationProcedure asserts that the
// refusal that starts the migration prints every step it depends on, in order.
//
// The move has a chain of prerequisites -- a config the pre-flip build accepts,
// documents the pre-flip build can read, a build that publishes the URL set the
// move preserves, and the repository's own grant of permission -- and each one
// used to be discovered only by running into the next refusal.
func TestTheOldLayoutRefusalPrintsTheWholeMigrationProcedure(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, DeprecatedRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	err := RefuseOldLayout(dir, "", "", "")
	if err == nil {
		t.Fatal("the old layout was accepted")
	}
	message := err.Error()
	for _, want := range []string{
		// The config the pre-flip build demands.
		`"versions"`,
		`"locales"`,
		// The frontmatter conversion, generated pages included -- which the
		// converter leaves alone unless it is told otherwise.
		scripts.Run(scripts.ConvertFrontmatter, "--include-generated", "--dry-run"),
		scripts.Run(scripts.ConvertFrontmatter, "--include-generated", "--apply"),
		// The build whose sitemap the move compares its result against.
		"go run github.com/smm-h/selfdoc@v0.41.0 build",
		// The grant of permission the move refuses without.
		"mkdir " + Root,
		// The move itself.
		scripts.Run(scripts.Move, "--dry-run"),
		scripts.Run(scripts.Move, "--apply"),
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not print %q:\n%s", want, message)
		}
	}
	// The steps are numbered, so the order is stated rather than implied.
	for _, step := range []string{"1.", "2.", "3.", "4.", "5."} {
		if !strings.Contains(message, step) {
			t.Errorf("the refusal does not number step %q:\n%s", step, message)
		}
	}
}

func TestTheNewLayoutIsAccepted(t *testing.T) {
	hygiene.Isolate(t)
	dir := owned(t)
	if err := RefuseOldLayout(dir, DocsDefault, OutputDefault, PostsDefault); err != nil {
		t.Errorf("a moved repository was refused: %v", err)
	}
	// An undeclared key is the default, which is inside the layout.
	if err := RefuseOldLayout(dir, "", "", ""); err != nil {
		t.Errorf("a repository declaring nothing was refused: %v", err)
	}
}
