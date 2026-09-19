package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
)

// postFrontmatter is a well-formed post frontmatter block whose description is
// long enough to keep the description rules out of these assertions.
const postFrontmatter = "+++\n" +
	"title = \"Hello World\"\n" +
	"date = 2024-01-15\n" +
	"slug = \"hello-world\"\n" +
	"draft = false\n" +
	"directives = false\n" +
	"description = \"A post description written at a comfortable length, so " +
	"that the description-length rules stay out of the assertions these " +
	"tests actually make.\"\n" +
	"+++\n"

// postsProject writes a project with the given posts in the default posts
// directory, plus one docs page.
func postsProject(t *testing.T, posts map[string]string) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	projectConfig := configForSource(
		map[string]any{"path": "src/", "language": "python"},
	)
	projectConfig["posts"] = map[string]any{"dir": ".stricttools/posts/"}
	writeConfig(t, root, projectConfig)
	write(t, filepath.Join(root, "src", "__init__.py"), `"""Example package."""`+"\n")
	// The posts directory exists even with no posts in it: a project that
	// declares one and has none is a different state from one that has no
	// directory at all, and both surfaces of the check look here.
	write(t, filepath.Join(root, ".stricttools", "posts", ".keep"), "")
	write(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \"A home page whose description is long "+
			"enough to keep the description rules quiet in this fixture.\"\n"+
			"+++\n# Test Project\n\nWelcome.\n")
	for name, content := range posts {
		write(t, filepath.Join(root, ".stricttools", "posts", name), content)
	}
	return root
}

// postPath is a post's reporting path, relative to the project root.
func postPath(name string) string {
	return filepath.Join(".stricttools", "posts", name)
}

func TestAPostIsHeldToThePageRules(t *testing.T) {
	post := postFrontmatter + "Intro paragraph.\n\n![](/img/x.png)\n"
	root := postsProject(t, map[string]string{"hello.md": post})

	result := checkFixture(t, root)

	matching := withCode(result.Lints, "SEO003")
	if len(matching) != 1 {
		t.Fatalf("SEO003 count = %d, want 1: %v", len(matching), messagesOf(matching))
	}
	if matching[0].File() != postPath("hello.md") {
		t.Errorf("file = %q, want %q", matching[0].File(), postPath("hello.md"))
	}
	// The frontmatter of the SOURCE file is eight lines, then the intro
	// paragraph, then a blank line: the image sits on line 11 of hello.md.
	// A line computed from the rebuilt frontmatter the conversion emits
	// would point at the wrong line of the file a reader opens.
	wantLine := 0
	for index, line := range strings.Split(post, "\n") {
		if strings.HasPrefix(line, "![](") {
			wantLine = index + 1
			break
		}
	}
	if wantLine != 11 {
		t.Fatalf("the fixture's image is on line %d, want 11", wantLine)
	}
	if matching[0].Line() == nil || *matching[0].Line() != wantLine {
		t.Errorf("line = %v, want %d", matching[0].Line(), wantLine)
	}
}

func TestACleanPostProducesNoDiagnostics(t *testing.T) {
	post := postFrontmatter +
		"# Hello World\n\nA paragraph of ordinary prose with nothing wrong " +
		"with it at all, long enough that the paragraph-length rule is " +
		"satisfied: it runs past thirty words without running past eighty, " +
		"which is the band every page on the site is held to regardless of " +
		"who or what wrote it.\n"
	root := postsProject(t, map[string]string{"hello.md": post})

	result := checkFixture(t, root)

	var own []lints.LintResult
	for _, diagnostic := range result.Lints {
		if diagnostic.File() == postPath("hello.md") {
			own = append(own, diagnostic)
		}
	}
	if len(own) != 0 {
		t.Errorf("a clean post produced %v", messagesOf(own))
	}
}

func TestAPostMissingADescriptionIsAnError(t *testing.T) {
	post := "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"draft = false\ndirectives = false\n+++\n# Hello World\n\nBody.\n"
	root := postsProject(t, map[string]string{"hello.md": post})

	result := checkFixture(t, root)

	found := false
	for _, diagnostic := range withCode(result.Lints, "SEO006") {
		if diagnostic.File() == postPath("hello.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("SEO006 did not name the post: %v",
			messagesOf(withCode(result.Lints, "SEO006")))
	}
}

func TestADraftIsNotLinted(t *testing.T) {
	draft := strings.ReplaceAll(
		strings.ReplaceAll(postFrontmatter, "draft = false", "draft = true"),
		"slug = \"hello-world\"", "slug = \"draft-post\"",
	) + "Intro paragraph.\n\n![](/img/x.png)\n"
	root := postsProject(t, map[string]string{"draft.md": draft})

	result := checkFixture(t, root)

	if hasCode(result.Lints, "SEO003") {
		t.Errorf("a draft was linted: %v", messagesOf(withCode(result.Lints, "SEO003")))
	}
}

func TestTheGeneratedListingPageIsNotLinted(t *testing.T) {
	post := postFrontmatter + "# Hello World\n\nBody prose.\n"
	root := postsProject(t, map[string]string{"hello.md": post})

	result := checkFixture(t, root)

	for _, diagnostic := range result.Lints {
		if diagnostic.File() == "blog" || diagnostic.File() == "blog.md" {
			t.Errorf("the listing was linted: %s %s",
				diagnostic.Code(), diagnostic.Message())
		}
	}
}

func TestAProjectWithNoPostsIsUnaffected(t *testing.T) {
	root := postsProject(t, nil)

	result := checkFixture(t, root)

	for _, diagnostic := range result.Lints {
		if strings.Contains(diagnostic.File(), ".stricttools") {
			t.Errorf("a diagnostic named a post path: %s %s",
				diagnostic.File(), diagnostic.Message())
		}
	}
}

// writePostFrontmatter writes a post whose frontmatter is the given lines,
// adding the quiet directives declaration when the case does not state one.
func writePostFrontmatter(
	t *testing.T, postsDir, name string, frontmatterLines []string, body string,
) {
	t.Helper()
	declaresDirectives := false
	for _, line := range frontmatterLines {
		if strings.HasPrefix(line, "directives ") {
			declaresDirectives = true
		}
	}
	if !declaresDirectives {
		frontmatterLines = append(frontmatterLines, "directives = false")
	}
	write(t, filepath.Join(postsDir, name),
		"+++\n"+strings.Join(frontmatterLines, "\n")+"\n+++\n"+body)
}

func TestCheckPostsCodeMapping(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		frontmatter []string
		body        string
		wantCode    string
		wantFile    string
		wantLine    int
		hasLine     bool
	}{
		{
			name:        "a missing title",
			frontmatter: []string{"date = 2025-01-01"},
			wantCode:    "POST002",
			wantFile:    filepath.Join("blog", "p.md"),
		},
		{
			name:        "a missing date",
			frontmatter: []string{"title = \"No Date\""},
			wantCode:    "POST001",
			wantFile:    filepath.Join("blog", "p.md"),
		},
		{
			name:        "a date that is not YYYY-MM-DD",
			frontmatter: []string{"title = \"Bad\"", `date = "Jan 15 2025"`},
			wantCode:    "POST003",
			wantFile:    filepath.Join("blog", "p.md"),
		},
		{
			name:        "a directive declaration that is not a boolean",
			frontmatter: []string{"title = \"Fine\"", "date = 2025-01-01", `directives = "maybe"`},
			wantCode:    "POST006",
			wantFile:    filepath.Join("blog", "p.md"),
		},
		{
			name: "a stray marker in a post that declared none",
			frontmatter: []string{
				"title = \"Prose\"", "date = 2025-01-01", "directives = false",
			},
			body:     "First line.\n\nSecond line.\n\n:-: ref path=\"x\"\n",
			wantCode: "POST007",
			wantFile: filepath.Join("blog", "p.md"),
			// The frontmatter is five lines, then the two prose
			// lines with their blank separators: the marker sits on
			// line 10 of the post file.
			wantLine: 10,
			hasLine:  true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			isolate(t)
			root := t.TempDir()
			writePostFrontmatter(t,
				filepath.Join(root, "blog"), "p.md",
				testCase.frontmatter, testCase.body,
			)
			projectConfig := map[string]any{
				"posts": map[string]any{"dir": "blog"},
			}

			results, err := CheckPosts(projectConfig, root, handle())
			if err != nil {
				t.Fatalf("CheckPosts: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("diagnostics = %v, want one", messagesOf(results))
			}
			if results[0].Code() != testCase.wantCode {
				t.Errorf("code = %q, want %q (%s)",
					results[0].Code(), testCase.wantCode, results[0].Message())
			}
			if results[0].File() != testCase.wantFile {
				t.Errorf("file = %q, want %q", results[0].File(), testCase.wantFile)
			}
			if testCase.hasLine {
				if results[0].Line() == nil || *results[0].Line() != testCase.wantLine {
					t.Errorf("line = %v, want %d", results[0].Line(), testCase.wantLine)
				}
			} else if results[0].Line() != nil {
				t.Errorf("line = %v, want none (the defect sits at no line)",
					*results[0].Line())
			}
		})
	}
}

func TestCheckPostsNamesTheNestedFile(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writePostFrontmatter(t,
		filepath.Join(root, "blog"), filepath.Join("nested", "p.md"),
		[]string{"title = \"No Date\""}, "",
	)

	results, err := CheckPosts(map[string]any{
		"posts": map[string]any{"dir": "blog"},
	}, root, handle())
	if err != nil {
		t.Fatalf("CheckPosts: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("diagnostics = %v, want one", messagesOf(results))
	}
	want := filepath.Join("blog", "nested", "p.md")
	if results[0].File() != want {
		t.Errorf("file = %q, want %q", results[0].File(), want)
	}
}

func TestCheckPostsDuplicateSlug(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	postsDir := filepath.Join(root, "blog")
	writePostFrontmatter(t, postsDir, "a.md",
		[]string{"title = \"Same\"", "date = 2025-01-01"}, "")
	writePostFrontmatter(t, postsDir, "b.md",
		[]string{"title = \"Same\"", "date = 2025-02-01"}, "")

	results, err := CheckPosts(map[string]any{
		"posts": map[string]any{"dir": "blog"},
	}, root, handle())
	if err != nil {
		t.Fatalf("CheckPosts: %v", err)
	}
	if len(results) != 1 || results[0].Code() != "POST004" {
		t.Fatalf("diagnostics = %v, want one POST004", messagesOf(results))
	}
	if results[0].File() != filepath.Join("blog", "a.md") &&
		results[0].File() != filepath.Join("blog", "b.md") {
		t.Errorf("file = %q, want one of the two posts", results[0].File())
	}
}

func TestCheckPostsWithNoPostsDirectory(t *testing.T) {
	isolate(t)
	root := t.TempDir()

	results, err := CheckPosts(map[string]any{}, root, handle())
	if err != nil {
		t.Fatalf("CheckPosts: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("diagnostics = %v, want none", messagesOf(results))
	}
}

func TestCheckPostsReadsTheConventionalDirectoryWithNoPostsBlock(t *testing.T) {
	// A project with no "posts" block still keeps its posts at the
	// conventional .stricttools/posts/, which is where the post-lint slice
	// reads them from. An invalid one there is a POST diagnostic, not a
	// hard error raised past the check's own reporting.
	isolate(t)
	root := t.TempDir()
	writePostFrontmatter(t,
		filepath.Join(root, ".stricttools", "posts"), "p.md",
		[]string{"title = \"No Date\""}, "",
	)

	results, err := CheckPosts(map[string]any{}, root, handle())
	if err != nil {
		t.Fatalf("CheckPosts: %v", err)
	}
	if len(results) != 1 || results[0].Code() != "POST001" {
		t.Fatalf("diagnostics = %v, want one POST001", messagesOf(results))
	}
	want := filepath.Join(".stricttools", "posts", "p.md")
	if results[0].File() != want {
		t.Errorf("file = %q, want %q", results[0].File(), want)
	}
}

func TestCheckDocsReportsPostValidation(t *testing.T) {
	root := postsProject(t, map[string]string{
		"broken.md": "+++\ntitle = \"No Date\"\ndirectives = false\n+++\nBody.\n",
	})

	result := checkFixture(t, root)

	if !hasCode(result.Lints, "POST001") {
		t.Fatalf("POST001 missing; got %v", codes(result.Lints))
	}
}

func TestLintPostBuffer(t *testing.T) {
	root := postsProject(t, map[string]string{
		"hello.md": postFrontmatter + "# Hello World\n\nBody prose.\n",
	})

	buffer := postFrontmatter + "Intro paragraph.\n\n![](/img/x.png)\n"
	results, err := LintPostBuffer(root, "hello.md", buffer, nil, handle())
	if err != nil {
		t.Fatalf("LintPostBuffer: %v", err)
	}

	if !hasCode(results, "SEO003") {
		t.Fatalf("SEO003 missing; got %v", messagesOf(results))
	}
	for _, diagnostic := range results {
		if diagnostic.File() != postPath("hello.md") {
			t.Errorf("a diagnostic named %q, want only the buffer's own post",
				diagnostic.File())
		}
	}
}

func TestLintPostBufferJudgesDrafts(t *testing.T) {
	root := postsProject(t, nil)

	draft := strings.ReplaceAll(postFrontmatter, "draft = false", "draft = true") +
		"Intro paragraph.\n\n![](/img/x.png)\n"
	results, err := LintPostBuffer(root, "draft.md", draft, nil, handle())
	if err != nil {
		t.Fatalf("LintPostBuffer: %v", err)
	}
	if !hasCode(results, "SEO003") {
		t.Errorf("a draft buffer produced %v, want the defect reported",
			messagesOf(results))
	}
}

func TestLintPostBufferWithoutAPostsDirectory(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "src/", "language": "python"},
	))
	write(t, filepath.Join(root, "src", "__init__.py"), `"""Pkg."""`+"\n")
	write(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\ndescription = \"A home page whose description is long enough.\"\n+++\n# Home\n")

	_, err := LintPostBuffer(root, "hello.md", postFrontmatter+"Body.\n", nil, handle())
	if err == nil {
		t.Fatal("a project with no posts directory accepted a buffer")
	}
	if !strings.Contains(err.Error(), "no posts directory") {
		t.Errorf("error = %q", err)
	}
}
