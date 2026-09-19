package posts

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/stricttest/go/hygiene"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH: the committed manifest is read through it")
	}
}

// run runs a command in dir and fails the test when it does not succeed.
func run(t *testing.T, dir string, argv ...string) {
	t.Helper()
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", argv, err, output)
	}
}

// source assembles a post's Markdown from its frontmatter lines and body.
func source(frontmatter []string, body string) string {
	return "+++\n" + strings.Join(frontmatter, "\n") + "\n+++\n" + body
}

// writePost writes a post under postsDir. A fixture that does not care about
// the directive declaration gets the quiet answer, as every post must declare
// one.
func writePost(t *testing.T, postsDir, name string, frontmatter []string, body string) {
	t.Helper()
	declares := false
	for _, line := range frontmatter {
		if strings.HasPrefix(line, "directives ") {
			declares = true
		}
	}
	if !declares {
		frontmatter = append(append([]string{}, frontmatter...), "directives = false")
	}
	path := filepath.Join(postsDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(source(frontmatter, body)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeManifest writes a manifest under .selfdoc/ naming the given posts, and
// returns its path.
func writeManifest(t *testing.T, dirPath string, posts string) string {
	t.Helper()
	path := filepath.Join(dirPath, ".stricttools", "docs-state", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	document := `{"schema_version": 1, "name": "test", "slug": "test",
		"version": "1.0.0", "description": "", "language": "python",
		"base_url": "", "pages": [], "posts": [` + posts + `],
		"last_gen": ""}`
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func discover(t *testing.T, postsDir, projectRoot string) []Post {
	t.Helper()
	all, err := Discover(postsDir, projectRoot, effects.Unbound())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return all
}

// discoverError expects a refusal and returns it as a PostError.
func discoverError(t *testing.T, postsDir, projectRoot string) *PostError {
	t.Helper()
	_, err := Discover(postsDir, projectRoot, effects.Unbound())
	if err == nil {
		t.Fatal("want a refusal")
	}
	postError, ok := err.(*PostError)
	if !ok {
		t.Fatalf("err = %T (%v), want *PostError", err, err)
	}
	return postError
}

// parseError expects a refusal and returns it as a PostError.
func parseError(t *testing.T, raw, relPath string) *PostError {
	t.Helper()
	_, err := Parse(raw, relPath, "")
	if err == nil {
		t.Fatal("want a refusal")
	}
	postError, ok := err.(*PostError)
	if !ok {
		t.Fatalf("err = %T (%v), want *PostError", err, err)
	}
	return postError
}

// initRepo creates a repository with one commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init", "--quiet")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(t, dir, "git", "add", "README.md")
	run(t, dir, "git", "commit", "--quiet", "-m", "init")
}

var baseFrontmatter = []string{"title = \"Hello World\"", "date = 2025-01-15"}

// -- Basic discovery --------------------------------------------------------

func TestDiscoverOnAnEmptyDirectoryFindsNothing(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	if err := os.MkdirAll(postsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if all := discover(t, postsDir, ""); len(all) != 0 {
		t.Errorf("found %d posts in an empty directory", len(all))
	}
}

func TestDiscoverOnAMissingDirectoryFindsNothing(t *testing.T) {
	hygiene.Isolate(t)
	if all := discover(t, filepath.Join(t.TempDir(), "does-not-exist"), ""); len(all) != 0 {
		t.Errorf("found %d posts under a directory that does not exist", len(all))
	}
}

func TestDiscoverReadsEveryFieldOfAPost(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "hello.md",
		[]string{"title = \"Hello World\"", "date = 2025-01-15"}, "Some body text.")

	all := discover(t, postsDir, "")
	if len(all) != 1 {
		t.Fatalf("found %d posts, want 1", len(all))
	}
	post := all[0]
	if post.Path != "hello.md" {
		t.Errorf("path = %q", post.Path)
	}
	if post.Title != "Hello World" {
		t.Errorf("title = %q", post.Title)
	}
	if post.Date != "2025-01-15" {
		t.Errorf("date = %q", post.Date)
	}
	if post.Slug != "hello-world" {
		t.Errorf("slug = %q", post.Slug)
	}
	if len(post.Tags) != 0 {
		t.Errorf("tags = %v, want none", post.Tags)
	}
	if post.Draft {
		t.Error("draft = true, want false")
	}
	if post.Type != "post" {
		t.Errorf("type = %q, want post", post.Type)
	}
	if post.Versioned {
		t.Error("versioned = true, want false")
	}
	if post.Content != "Some body text." {
		t.Errorf("content = %q", post.Content)
	}
}

// -- Sorting ----------------------------------------------------------------

func TestDiscoverSortsNewestFirst(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "old.md", []string{"title = \"Old\"", "date = 2024-06-01"}, "")
	writePost(t, postsDir, "mid.md", []string{"title = \"Mid\"", "date = 2025-01-01"}, "")
	writePost(t, postsDir, "new.md", []string{"title = \"New\"", "date = 2025-07-01"}, "")

	var dates []string
	for _, post := range discover(t, postsDir, "") {
		dates = append(dates, post.Date)
	}
	want := []string{"2025-07-01", "2025-01-01", "2024-06-01"}
	if !reflect.DeepEqual(dates, want) {
		t.Errorf("dates = %v, want %v", dates, want)
	}
}

func TestDiscoverBreaksADateTieBySlug(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "z.md", []string{"title = \"Zeta\"", "date = 2025-03-01"}, "")
	writePost(t, postsDir, "a.md", []string{"title = \"Alpha\"", "date = 2025-03-01"}, "")
	writePost(t, postsDir, "m.md", []string{"title = \"Mid\"", "date = 2025-03-01"}, "")

	var slugs []string
	for _, post := range discover(t, postsDir, "") {
		slugs = append(slugs, post.Slug)
	}
	want := []string{"alpha", "mid", "zeta"}
	if !reflect.DeepEqual(slugs, want) {
		t.Errorf("slugs = %v, want %v", slugs, want)
	}
}

// -- Slug handling ----------------------------------------------------------

func TestDiscoverDerivesASlugFromTheTitle(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "post.md",
		[]string{"title = \"My Great Post!\"", "date = 2025-01-01"}, "")
	if slug := discover(t, postsDir, "")[0].Slug; slug != "my-great-post" {
		t.Errorf("slug = %q, want my-great-post", slug)
	}
}

func TestDiscoverPrefersADeclaredSlug(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "post.md",
		[]string{"title = \"My Post\"", "date = 2025-01-01", "slug = \"custom-slug\""}, "")
	if slug := discover(t, postsDir, "")[0].Slug; slug != "custom-slug" {
		t.Errorf("slug = %q, want custom-slug", slug)
	}
}

// -- Tags -------------------------------------------------------------------

func TestDiscoverDefaultsTagsToNone(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "p.md", []string{"title = \"No Tags\"", "date = 2025-01-01"}, "")
	post := discover(t, postsDir, "")[0]
	if len(post.Tags) != 0 {
		t.Errorf("tags = %v, want none", post.Tags)
	}
	// The default is injected into the frontmatter too, because the page the
	// build writes for this post is rendered by writing the frontmatter back.
	if _, declared := post.Frontmatter["tags"]; !declared {
		t.Error("the frontmatter carries no injected tags key")
	}
}

func TestDiscoverReadsTagsFromTheFrontmatter(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "p.md",
		[]string{"title = \"Tagged\"", "date = 2025-01-01", "tags = [\"python\", \"testing\", \"ci\"]"}, "")
	want := []string{"python", "testing", "ci"}
	if tags := discover(t, postsDir, "")[0].Tags; !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v, want %v", tags, want)
	}
}

// -- Draft handling ---------------------------------------------------------

func TestDiscoverReadsTheDraftFlag(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "p.md",
		[]string{"title = \"Draft Post\"", "date = 2025-01-01", "draft = true"}, "")
	if !discover(t, postsDir, "")[0].Draft {
		t.Error("draft = false, want true")
	}
}

func TestDiscoverDefaultsDraftToFalse(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "p.md", []string{"title = \"Normal\"", "date = 2025-01-01"}, "")
	if discover(t, postsDir, "")[0].Draft {
		t.Error("draft = true, want false")
	}
}

// -- Injected fields --------------------------------------------------------

func TestDiscoverInjectsTypeAndVersioned(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "p.md", []string{"title = \"Typed\"", "date = 2025-01-01"}, "")
	post := discover(t, postsDir, "")[0]
	if post.Type != "post" {
		t.Errorf("type = %q, want post", post.Type)
	}
	if post.Versioned {
		t.Error("versioned = true, want false")
	}
	if post.Frontmatter["type"] != "post" {
		t.Errorf("frontmatter type = %#v, want post", post.Frontmatter["type"])
	}
	if post.Frontmatter["versioned"] != false {
		t.Errorf("frontmatter versioned = %#v, want false", post.Frontmatter["versioned"])
	}
}

// -- Validation errors ------------------------------------------------------

func TestParseRefusalsNameTheFieldAndThePost(t *testing.T) {
	hygiene.Isolate(t)
	cases := []struct {
		name        string
		frontmatter []string
		body        string
		wantParts   []string
		wantCode    string
	}{
		{
			name:        "no title",
			frontmatter: []string{"date = 2025-01-01", "directives = false"},
			wantParts:   []string{"Post p.md", "title"},
			wantCode:    "POST002",
		},
		{
			name:        "no date",
			frontmatter: []string{"title = \"No Date\"", "directives = false"},
			wantParts:   []string{"Post p.md", "date"},
			wantCode:    "POST001",
		},
		{
			name:        "a date that is not a date",
			frontmatter: []string{"title = \"Bad Date\"", `date = "Jan 15 2025"`, "directives = false"},
			wantParts:   []string{"Post p.md", "$.date", "Expected a date"},
			wantCode:    "POST003",
		},
		{
			name:        "no directive declaration",
			frontmatter: baseFrontmatter,
			wantParts:   []string{"Post p.md", "directives"},
			wantCode:    "POST006",
		},
		{
			name:        "a declaration that is not a boolean",
			frontmatter: append(append([]string{}, baseFrontmatter...), `directives = "maybe"`),
			wantParts:   []string{"Post p.md", "$.directives", "Expected a boolean"},
			wantCode:    "POST006",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			postError := parseError(t, source(testCase.frontmatter, testCase.body), "p.md")
			for _, part := range testCase.wantParts {
				if !strings.Contains(postError.Message, part) {
					t.Errorf("message %q does not mention %q", postError.Message, part)
				}
			}
			if postError.Code != testCase.wantCode {
				t.Errorf("code = %q, want %q", postError.Code, testCase.wantCode)
			}
			if postError.Path != "p.md" {
				t.Errorf("path = %q, want p.md", postError.Path)
			}
			// A missing frontmatter field sits at no line, and nil says so
			// rather than pointing at a wrong one.
			if postError.Line != nil {
				t.Errorf("line = %d, want none", *postError.Line)
			}
		})
	}
}

func TestDiscoveryCarriesTheSameRefusalsAsTheParser(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	// No directive declaration: the discovery path must refuse it too.
	if err := os.MkdirAll(postsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postsDir, "p.md"),
		[]byte(source(baseFrontmatter, "Body.\n")), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	postError := discoverError(t, postsDir, "")
	if !strings.Contains(postError.Message, "Field directives") {
		t.Errorf("message = %q", postError.Message)
	}
}

func TestARefusalNamesThePostFileNotItsDirectory(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, filepath.Join("nested", "p.md"), []string{"title = \"No Date\""}, "")
	if path := discoverError(t, postsDir, "").Path; path != "nested/p.md" {
		t.Errorf("path = %q, want nested/p.md", path)
	}
}

func TestDiscoverRefusesTwoPostsWithOneSlug(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "a.md", []string{"title = \"Same\"", "date = 2025-01-01"}, "")
	writePost(t, postsDir, "b.md", []string{"title = \"Same\"", "date = 2025-02-01"}, "")

	postError := discoverError(t, postsDir, "")
	want := "Duplicate slug 'same': used by both 'a.md' and 'b.md'"
	if postError.Message != want {
		t.Errorf("message = %q, want %q", postError.Message, want)
	}
	// The diagnostic is positioned at the SECOND post -- the one whose slug
	// has to change.
	if postError.Path != "b.md" {
		t.Errorf("path = %q, want b.md", postError.Path)
	}
}

// -- The directive declaration ----------------------------------------------

func TestParseAcceptsADeclarationOfFalseWithNoMarkers(t *testing.T) {
	hygiene.Isolate(t)
	post, err := Parse(
		source(append(append([]string{}, baseFrontmatter...), "directives = false"),
			"Just prose.\n"), "p.md", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if post.Directives {
		t.Error("directives = true, want false")
	}
}

func TestParseAcceptsADeclarationOfTrue(t *testing.T) {
	hygiene.Isolate(t)
	post, err := Parse(
		source(append(append([]string{}, baseFrontmatter...), "directives = true"),
			"Intro.\n\n:-: ref path=\"mylib\" target=\"alpha\"\n"), "p.md", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !post.Directives {
		t.Error("directives = false, want true")
	}
}

func TestParseRefusesADeclaredFalsePostCarryingAMarker(t *testing.T) {
	hygiene.Isolate(t)
	cases := []struct {
		name     string
		body     string
		wantLine int
		marker   string
	}{
		{
			name:     "the self-closing marker",
			body:     "Intro paragraph.\n\n:-: ref path=\"mylib\" target=\"alpha\"\n",
			wantLine: 8,
			marker:   ":-:",
		},
		{
			// Every marker type counts, not just the self-closing one.
			name:     "a block marker",
			body:     ":<: table-commands\n:@: path=\"cli.py\"\n:>:\n",
			wantLine: 6,
			marker:   ":<:",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			frontmatter := append(append([]string{}, baseFrontmatter...), "directives = false")
			postError := parseError(t, source(frontmatter, testCase.body), "p.md")
			if !strings.Contains(postError.Message, testCase.marker) {
				t.Errorf("message = %q, want the marker in it", postError.Message)
			}
			// The line is the post FILE's, not the body's: the frontmatter
			// is five lines (---, title, date, directives, ---).
			if postError.Line == nil || *postError.Line != testCase.wantLine {
				t.Errorf("line = %v, want %d", postError.Line, testCase.wantLine)
			}
			if !strings.Contains(postError.Message, "line "+strconv.Itoa(testCase.wantLine)) {
				t.Errorf("message = %q, want the line in it", postError.Message)
			}
		})
	}
}

func TestParseDoesNotReadAMarkerInsideCode(t *testing.T) {
	hygiene.Isolate(t)
	cases := []struct {
		name string
		body string
	}{
		{
			// A fenced example of the syntax is documentation, not a
			// directive.
			name: "inside a fence",
			body: "Here is the syntax:\n\n```\n:-: ref path=\"x\"\n```\n",
		},
		{
			// `:-: ref` written inline is prose about the syntax.
			name: "inside a code span",
			body: "Write `:-: ref path=\"x\"` to embed a symbol.\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			frontmatter := append(append([]string{}, baseFrontmatter...), "directives = false")
			post, err := Parse(source(frontmatter, testCase.body), "p.md", "")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if post.Directives {
				t.Error("directives = true, want false")
			}
		})
	}
}

func TestAStrayMarkerRefusalCarriesTheLineItSitsOn(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "p.md",
		[]string{"title = \"Prose\"", "date = 2025-01-01", "directives = false"},
		"First line.\n\nSecond line.\n\n:-: ref path=\"x\"\n")

	postError := discoverError(t, postsDir, "")
	content, err := os.ReadFile(filepath.Join(postsDir, "p.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := 0
	for index, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, ":-:") {
			want = index + 1
			break
		}
	}
	if postError.Line == nil || *postError.Line != want {
		t.Errorf("line = %v, want %d", postError.Line, want)
	}
}

// -- Optional fields and the body -------------------------------------------

func TestDiscoverCarriesThePassThroughFields(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	writePost(t, postsDir, "release.md", []string{
		"title = \"Release Notes\"",
		"date = 2025-06-01",
		"locale = \"en\"",
		"version = \"2.0.0\"",
		"prev_version = \"1.9.0\"",
		"bump_type = \"major\"",
		"release_url = \"https://github.com/org/repo/releases/v2.0.0\"",
		"registry_urls = [\"https://pypi.org/project/mylib/2.0.0\"]",
	}, "")

	post := discover(t, postsDir, "")[0]
	if post.Locale != "en" {
		t.Errorf("locale = %#v", post.Locale)
	}
	if post.Version != "2.0.0" {
		t.Errorf("version = %#v", post.Version)
	}
	if post.PrevVersion != "1.9.0" {
		t.Errorf("prevVersion = %#v", post.PrevVersion)
	}
	if post.BumpType != "major" {
		t.Errorf("bumpType = %#v", post.BumpType)
	}
	if post.ReleaseURL != "https://github.com/org/repo/releases/v2.0.0" {
		t.Errorf("releaseURL = %#v", post.ReleaseURL)
	}
	want := []string{"https://pypi.org/project/mylib/2.0.0"}
	if !reflect.DeepEqual(post.RegistryURLs, want) {
		t.Errorf("registryURLs = %#v, want %v", post.RegistryURLs, want)
	}
}

func TestDiscoverKeepsTheBodyUnresolved(t *testing.T) {
	hygiene.Isolate(t)
	postsDir := filepath.Join(t.TempDir(), "posts")
	body := "First paragraph.\n\nSecond paragraph."
	writePost(t, postsDir, "p.md", []string{"title = \"With Body\"", "date = 2025-01-01"}, body)
	if content := discover(t, postsDir, "")[0].Content; content != body {
		t.Errorf("content = %q, want %q", content, body)
	}
}

// -- The frontmatter key order ----------------------------------------------

// TestParseCarriesTheBlockAsTheReaderRead it pins that this package records
// exactly the keys the one frontmatter reader produced, in its order: the page
// the build writes for a post is that record written back out, so a key
// gained or lost here is a key gained or lost on the published page.
func TestParseCarriesTheBlockAsTheReaderReadIt(t *testing.T) {
	hygiene.Isolate(t)
	post, err := Parse(source([]string{
		"title = \"T\"", "date = 2025-01-01", "directives = false",
	}, "Body\n"), "p.md", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, field := range post.FrontmatterFields {
		if _, declared := post.Frontmatter[field.Key]; !declared {
			t.Errorf("the recorded order names %q, which the block does not carry", field.Key)
		}
	}
	if len(post.FrontmatterFields) != len(post.Frontmatter) {
		t.Errorf("order %v, block %v", post.FrontmatterFields, keysOf(post.Frontmatter))
	}
}

func TestParseRecordsTheFrontmatterOrderWithTheInjectedKeysLast(t *testing.T) {
	hygiene.Isolate(t)
	post, err := Parse(source([]string{
		"title = \"Hello\"", "date = 2025-01-01", "directives = false",
	}, "Body\n"), "p.md", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"title", "date", "directives", "type", "versioned", "tags"}
	var keys []string
	for _, field := range post.FrontmatterFields {
		keys = append(keys, field.Key)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

// TestParseKeepsADeclaredKeyInItsDeclaredPosition pins that injecting a key
// the source already declared does not move it to the end, which is what
// assigning to an existing dict key does in the Python.
func TestParseKeepsADeclaredKeyInItsDeclaredPosition(t *testing.T) {
	hygiene.Isolate(t)
	post, err := Parse(source([]string{
		"title = \"Hello\"", "tags = [\"a\", \"b\"]", "date = 2025-01-01",
		"type = \"something\"", "directives = false",
	}, "Body\n"), "p.md", "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"title", "tags", "date", "type", "directives", "versioned"}
	var keys []string
	for _, field := range post.FrontmatterFields {
		keys = append(keys, field.Key)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	// The declared value is overwritten even though the position is kept.
	if post.Frontmatter["type"] != "post" {
		t.Errorf("frontmatter type = %#v, want post", post.Frontmatter["type"])
	}
}

// -- Slug immutability ------------------------------------------------------

func TestSlugImmutabilityAcceptsAnUnchangedSlug(t *testing.T) {
	requireGit(t)
	hygiene.Isolate(t)
	base := t.TempDir()
	initRepo(t, base)
	postsDir := filepath.Join(base, ".stricttools", "posts")
	writePost(t, postsDir, "hello.md",
		[]string{"title = \"Hello\"", "date = 2025-01-01", "slug = \"hello\""}, "")
	_ = writeManifest(t, base, `{"path": "hello.md", "slug": "hello"}`)
	run(t, base, "git", "add", ".stricttools/docs-state/manifest.json")
	run(t, base, "git", "commit", "--quiet", "-m", "add manifest")

	all := discover(t, postsDir, base)
	if len(all) != 1 || all[0].Slug != "hello" {
		t.Errorf("discovered %+v", all)
	}
}

func TestSlugImmutabilityRefusesAChangedSlug(t *testing.T) {
	requireGit(t)
	hygiene.Isolate(t)
	base := t.TempDir()
	initRepo(t, base)
	postsDir := filepath.Join(base, ".stricttools", "posts")
	writePost(t, postsDir, "hello.md",
		[]string{"title = \"Hello\"", "date = 2025-01-01", "slug = \"hello-new\""}, "")
	_ = writeManifest(t, base, `{"path": "hello.md", "slug": "hello-old"}`)
	run(t, base, "git", "add", ".stricttools/docs-state/manifest.json")
	run(t, base, "git", "commit", "--quiet", "-m", "add manifest")

	postError := discoverError(t, postsDir, base)
	want := "Post hello.md: slug changed from 'hello-old' to 'hello-new'. " +
		"Slug immutability violation -- slugs cannot change once published."
	if postError.Message != want {
		t.Errorf("message = %q, want %q", postError.Message, want)
	}
}

// TestSlugImmutabilityReadsTheCommittedManifest is the scenario the git read
// exists for: gen has already rewritten the working-tree manifest with the new
// slug, so a reader of that copy would see no change at all.
func TestSlugImmutabilityReadsTheCommittedManifest(t *testing.T) {
	requireGit(t)
	hygiene.Isolate(t)
	base := t.TempDir()
	initRepo(t, base)
	postsDir := filepath.Join(base, ".stricttools", "posts")
	_ = writeManifest(t, base, `{"path": "hello.md", "slug": "hello-old"}`)
	run(t, base, "git", "add", ".stricttools/docs-state/manifest.json")
	run(t, base, "git", "commit", "--quiet", "-m", "add manifest")

	writePost(t, postsDir, "hello.md",
		[]string{"title = \"Hello\"", "date = 2025-01-01", "slug = \"hello-new\""}, "")
	writeManifest(t, base, `{"path": "hello.md", "slug": "hello-new"}`)

	postError := discoverError(t, postsDir, base)
	if !strings.Contains(postError.Message, "Slug immutability violation") {
		t.Errorf("message = %q", postError.Message)
	}
}

func TestSlugImmutabilityAcceptsAPostTheManifestDoesNotName(t *testing.T) {
	requireGit(t)
	hygiene.Isolate(t)
	base := t.TempDir()
	initRepo(t, base)
	postsDir := filepath.Join(base, ".stricttools", "posts")
	writePost(t, postsDir, "new-post.md",
		[]string{"title = \"New Post\"", "date = 2025-06-01", "slug = \"new-post\""}, "")
	_ = writeManifest(t, base, "")
	run(t, base, "git", "add", ".stricttools/docs-state/manifest.json")
	run(t, base, "git", "commit", "--quiet", "-m", "add manifest")

	all := discover(t, postsDir, base)
	if len(all) != 1 || all[0].Slug != "new-post" {
		t.Errorf("discovered %+v", all)
	}
}

// TestSlugImmutabilityIsSkippedWithNothingToCompareAgainst covers the three
// ways the committed manifest can be absent. In each one the check has no
// published slug to compare against and must pass the post through, even
// though the manifest sitting on disk names a different slug.
func TestSlugImmutabilityIsSkippedWithNothingToCompareAgainst(t *testing.T) {
	requireGit(t)
	cases := []struct {
		name string
		// setup prepares base and returns nothing; the post and the
		// on-disk manifest are written by the test body.
		setup func(t *testing.T, base string)
	}{
		{
			name:  "not a repository at all",
			setup: func(t *testing.T, base string) {},
		},
		{
			name: "a repository with no commits",
			setup: func(t *testing.T, base string) {
				run(t, base, "git", "init", "--quiet")
			},
		},
		{
			name: "a manifest that was never committed",
			setup: func(t *testing.T, base string) {
				initRepo(t, base)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hygiene.Isolate(t)
			base := t.TempDir()
			testCase.setup(t, base)
			postsDir := filepath.Join(base, ".stricttools", "posts")
			writePost(t, postsDir, "hello.md",
				[]string{"title = \"Hello\"", "date = 2025-01-01", "slug = \"hello-new\""}, "")
			_ = writeManifest(t, base, `{"path": "hello.md", "slug": "hello-old"}`)

			if all := discover(t, postsDir, base); len(all) != 1 {
				t.Errorf("discovered %d posts, want 1", len(all))
			}
		})
	}
}

func TestSlugImmutabilityIsSkippedWithoutAManifestPath(t *testing.T) {
	requireGit(t)
	hygiene.Isolate(t)
	base := t.TempDir()
	postsDir := filepath.Join(base, "posts")
	writePost(t, postsDir, "hello.md",
		[]string{"title = \"Hello\"", "date = 2025-01-01", "slug = \"hello\""}, "")
	if all := discover(t, postsDir, ""); len(all) != 1 {
		t.Errorf("discovered %d posts, want 1", len(all))
	}
}

// -- Conversions ------------------------------------------------------------

func TestManifestPostsNarrowToWhatTheManifestRecords(t *testing.T) {
	hygiene.Isolate(t)
	post := Post{
		Path: "hello.md", Title: "Hello", Date: "2025-01-01",
		Slug: "hello", Tags: []string{"go"}, Draft: true,
	}
	entries := ManifestPosts([]Post{post})
	if len(entries) != 1 {
		t.Fatalf("converted %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Path != "hello.md" || entry.Title != "Hello" ||
		entry.Date != "2025-01-01" || entry.Slug != "hello" ||
		!reflect.DeepEqual(entry.Tags, []string{"go"}) {
		t.Errorf("entry = %+v", entry)
	}
}

// -- Helpers ----------------------------------------------------------------

func keysOf(frontmatter util.Frontmatter) []string {
	keys := make([]string, 0, len(frontmatter))
	for key := range frontmatter {
		keys = append(keys, key)
	}
	return keys
}
