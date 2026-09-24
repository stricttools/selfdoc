package page

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// The reference files under testdata were recorded from the Python renderer
// this package ports, by scripts/record_page_reference.py. Asserting byte
// equality against them is what makes "same emitted HTML" a measurement
// rather than a claim: a difference anywhere in a whole document -- one
// attribute, one newline, one JSON separator -- fails the case.
//
// The picker element ids come from a process-wide counter, so each recorded
// case ran in its own Python process and each Go case resets the counter
// before rendering.

// highlightedCodeRE matches the interior of a highlighted code element.
//
// The Go port of this package replaced Pygments with chroma, so a
// code block's token markup is deliberately not the Python's: different class
// names, and a different set of characters escaped. The interior is masked on
// both sides of a comparison, which leaves every byte of the page around it --
// the code element's own language class included, since the structured data
// reads the language off it -- under the byte-equality assertion.
var highlightedCodeRE = regexp.MustCompile(
	`(?s)(<code class="language-[^"]*">).*?(</code>)`)

// maskCodeInteriors replaces the token markup inside every highlighted code
// element with a placeholder.
func maskCodeInteriors(s string) string {
	return highlightedCodeRE.ReplaceAllString(s, "${1}<!--code-->${2}")
}

// resetSelectCounter puts the picker id counter back to its start, so a case
// renders the ids the recorded reference carries.
func resetSelectCounter() {
	selectCounter.Lock()
	selectCounter.next = 0
	selectCounter.Unlock()
}

// reference reads a recorded reference file.
func reference(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatalf("reading reference %s: %v", name, err)
	}
	return string(data)
}

// renderKeyed joins a generated page set the way the recorder wrote it: one
// section per output key, in sorted key order.
func renderKeyed(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString("===== KEY " + key + "\n" + files[key])
	}
	return b.String()
}

// assertEqual compares a rendered result against its reference and reports
// the first line that differs, because a whole document in a failure message
// is unreadable.
func assertEqual(t *testing.T, name, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			t.Fatalf("%s: line %d differs\n got: %q\nwant: %q",
				name, i+1, gotLines[i], wantLines[i])
		}
	}
	t.Fatalf("%s: %d lines rendered, reference has %d",
		name, len(gotLines), len(wantLines))
}

// testAuthor is the declared author every recorded case names.
func testAuthor() map[string]any {
	return map[string]any{
		"name":    "Test Author",
		"url":     "https://author.example",
		"same_as": []any{"https://github.com/testauthor"},
	}
}

// baseOptions is the starting point every recorded generate case shares: the
// build's own defaults plus the project name and author the recorder passed.
func baseOptions(files ...SourceFile) Options {
	opts := NewOptions()
	opts.ProjectName = "TestProject"
	opts.Author = testAuthor()
	opts.MarkdownFiles = files
	return opts
}

func themeMeta(t *testing.T, name string) *themes.Metadata {
	t.Helper()
	meta, err := themes.Meta(name)
	if err != nil {
		t.Fatalf("theme %q: %v", name, err)
	}
	return &meta
}

func src(mdPath, content string) SourceFile {
	return SourceFile{MdPath: mdPath, Content: content}
}

// TestGeneratedPagesMatchReference renders each recorded case and asserts the
// whole page set is byte-identical to what the Python produced.
func TestGeneratedPagesMatchReference(t *testing.T) {
	cases := map[string]func(t *testing.T) Options{
		"simple": func(*testing.T) Options {
			return baseOptions(src("index.md", "# My Page\n\nContent here.\n"))
		},
		"multipage": func(*testing.T) Options {
			return baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nA guide with prose.\n"),
				src("api.md", "# API\n\nReference material.\n"),
			)
		},
		"subdirs": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nContent.\n"),
				src("api/endpoints.md", "# Endpoints\n\nList.\n"),
				src("api/auth.md", "# Auth\n\nTokens.\n"),
				src("user_guide/quick-start.md", "# Quickstart\n\nSteps.\n"),
			)
			opts.Frontmatter = map[string]util.Frontmatter{
				"guide.md":         {"title": "User Guide", "order": int64(2)},
				"api/endpoints.md": {"nav_order": int64(2)},
				"api/auth.md":      {"nav_order": int64(1), "nav_group": "API Reference"},
			}
			return opts
		},
		"frontmatter": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome home.\n"),
				src("guide.md", "## Overview\n\nSome prose here.\n"),
			)
			opts.Frontmatter = map[string]util.Frontmatter{
				"guide.md": {
					"title":       "The Guide",
					"description": "A complete description of the guide page.",
					"tags":        []string{"deploy", "hosting"},
					"type":        "tutorial",
				},
			}
			opts.BaseURL = "https://example.com"
			return opts
		},
		"repo_dates": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nContent.\n"),
			)
			opts.Repo = "https://github.com/user/repo/"
			opts.DocsDirName = "docs"
			opts.Branch = "develop"
			opts.PageDates = map[string]PageDates{
				"index.md": {Published: "2024-06-01", Modified: "2026-03-15"},
				"guide.md": {Modified: "2025-01-02"},
			}
			opts.BaseURL = "https://example.com"
			opts.TwitterSite = "@example"
			opts.FeedURL = "feed.xml"
			opts.CriticalCSS = "body{margin:0}"
			return opts
		},
		"feedback_ga": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Home\n\nWorld.\n"))
			opts.Feedback = map[string]any{
				"webhook": "https://example.com/hook", "ga": "G-XXXXX",
			}
			return opts
		},
		"branding_auto": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Welcome\n\nSome intro text.\n"),
				src("api/one.md", "# One\n\nText.\n"),
				src("api/two.md", "# Two\n\nText.\n"),
			)
			opts.Branding = map[string]any{
				"tagline": "A test project", "logo": "logo.svg",
			}
			opts.ConfigDescription = "The project description."
			return opts
		},
		"branding_explicit": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Welcome\n\nIntro.\n"))
			opts.Branding = map[string]any{
				"tagline":            "A test project",
				"cta_text":           "Start",
				"cta_link":           "guide/",
				"secondary_cta_text": "Source",
				"secondary_cta_link": "https://example.com/src",
				"features": []any{
					map[string]any{"title": "Fast", "description": "Very fast."},
					map[string]any{
						"title": "Linked", "description": "Has a link.",
						"link": "guide/",
					},
				},
			}
			return opts
		},
		"pickers": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Test\n\nHello.\n"))
			opts.Version = "1.0.0"
			opts.MountVersion = "1.0.0"
			opts.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
			opts.BaseURL = "https://example.com"
			return opts
		},
		"locales": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Test\n\nHello.\n"),
				src("guide.md", "# Guide\n\nText.\n"),
			)
			opts.Version = "1.0.0"
			opts.MountLocale = "fr"
			opts.AvailableLocales = []LocaleEntry{
				{Code: "en", Label: "English", Default: true},
				{Code: "fr", Label: "French"},
			}
			opts.CurrentLocale = "fr"
			opts.BaseURL = "https://example.com"
			return opts
		},
		"archived": func(*testing.T) Options {
			opts := baseOptions(src("guide.md", "# Guide\n\nOld content.\n"))
			opts.Version = "0.9.0"
			opts.MountLocale = "en"
			opts.MountVersion = "0.9.0"
			opts.MountArchived = true
			opts.AvailableVersions = []VersionEntry{{"0.9.0"}, {"1.0.0"}}
			opts.CurrentLocale = "en"
			opts.AvailableLocales = []LocaleEntry{
				{Code: "en", Label: "English", Default: true},
				{Code: "fr", Label: "French"},
			}
			opts.BaseURL = "https://example.com"
			return opts
		},
		"glossary": func(*testing.T) Options {
			return baseOptions(
				src("index.md", "# Home\n\nWelcome. A parser is a core component.\n"),
				src("terms.md", "# Terms\n\n"+
					"Parser\n: Breaks input into tokens\n\n"+
					"Lexer\n: Tokenizes raw text\n\n"+
					"A <dfn>widget</dfn> is a reusable interface element.\n"),
			)
		},
		"itemlist": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Links\n\n"+
				"- [Alpha](https://alpha.com)\n"+
				"- [Beta](https://beta.com)\n"+
				"- Plain item\n"))
			opts.Frontmatter = map[string]util.Frontmatter{
				"index.md": {"schema": "itemlist"},
			}
			return opts
		},
		"itemlist_auto": func(*testing.T) Options {
			return baseOptions(src("index.md",
				"# Many\n\n- one\n- two\n- three\n- four\n- five\n- six\n"))
		},
		"code": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# API Reference\n\n"+
				"```python\nprint('hi')\n```\n"+
				"```go\nfmt.Println(1)\n```\n\n"+
				"## Plain\n\n```\nplain code\n```\n\n"+
				"### Config.load\n\n```python\ndef load(path): ...\n```\n\n"+
				"Load configuration from a file.\n"))
			opts.Repo = "https://github.com/user/repo"
			// The recorded page was rendered for a project whose pages sat
			// at docs/, and the edit link names the directory it is given;
			// this case measures the code block, not the layout.
			opts.DocsDirName = "docs/"
			opts.RunButton = true
			opts.LineNumbers = true
			opts.CodeIcons = "monochrome"
			opts.AutoDetect = map[string]any{"api_entries": true, "steps": true}
			return opts
		},
		"toc": func(*testing.T) Options {
			return baseOptions(src("index.md", "# Title\n\n"+
				"## Section One\n\nText.\n\n"+
				"### Nested\n\nMore.\n\n"+
				"## Section Two\n\nMore text.\n"))
		},
		"deploy_github_pages": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Home\n\nWelcome.\n"))
			opts.BaseURL = "https://example.com"
			opts.DeployTarget = "github-pages"
			return opts
		},
		"unversioned": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nText.\n"),
			)
			opts.Version = "1.0.0"
			opts.MountVersion = "1.0.0"
			opts.UnversionedPages = []SourceFile{
				src("about.md", "# About\n\nUs.\n"),
				src("blog/hello.md", "# Hello\n\nA post.\n"),
				src("blog/later.md", "# Later\n\nAnother post.\n"),
			}
			opts.UnversionedFrontmatter = map[string]util.Frontmatter{
				"about.md": {"title": "About Us", "nav_order": int64(2)},
				"blog/hello.md": {
					"title": "Hello", "type": "post", "date": "2026-01-01",
				},
				"blog/later.md": {
					"title": "Later", "type": "post", "date": "2026-02-01",
				},
			}
			return opts
		},
		"post_layout": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("news.md", "## One\n\nText.\n\n## Two\n\nMore.\n"),
			)
			opts.Frontmatter = map[string]util.Frontmatter{
				"news.md": {
					"title":       "A Post",
					"type":        "post",
					"description": "The post's own summary line.",
					"tags":        []string{"release"},
				},
			}
			opts.PageDates = map[string]PageDates{
				"news.md": {Published: "2026-06-01", Modified: "2026-06-29"},
			}
			opts.BaseURL = "https://example.com"
			return opts
		},
		"search_bar": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Home\n\nWelcome.\n"))
			opts.Search = "bar"
			return opts
		},
		"search_hidden": func(*testing.T) Options {
			opts := baseOptions(src("index.md", "# Home\n\nWelcome.\n"))
			opts.Search = "hidden"
			return opts
		},
		"theme_clean": func(t *testing.T) Options {
			opts := baseOptions(src("index.md", "# Home\n\nWelcome.\n"))
			opts.ThemeMeta = themeMeta(t, "clean")
			return opts
		},
		"theme_tinymoon": func(t *testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nText.\n"),
			)
			opts.ThemeMeta = themeMeta(t, "tinymoon")
			return opts
		},
		"mounted": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nText.\n"),
				src("blog/hello.md", "# Hello\n\nA post about things.\n"),
			)
			opts.Frontmatter = map[string]util.Frontmatter{
				"blog/hello.md": {"title": "Hello", "type": "post"},
			}
			opts.URLBuilder = urls.NewTopologyURLBuilder(
				"https://docs.example.com", "myproj")
			opts.BaseURL = "https://docs.example.com/myproj"
			opts.MountProject = "myproj"
			opts.FeedURL = "feed.xml"
			return opts
		},
		"no_page_nav": func(*testing.T) Options {
			opts := baseOptions(
				src("index.md", "# Home\n\nWelcome.\n"),
				src("guide.md", "# Guide\n\nText.\n"),
			)
			opts.PageNav = false
			opts.PageProgress = false
			opts.Glossary = false
			opts.HasCustomCSS = true
			return opts
		},
	}

	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		build := cases[name]
		t.Run(name, func(t *testing.T) {
			resetSelectCounter()
			files, err := GenerateHTML(build(t))
			if err != nil {
				t.Fatalf("GenerateHTML: %v", err)
			}
			assertEqual(t, name,
				maskCodeInteriors(renderKeyed(files)),
				maskCodeInteriors(reference(t, name)))
		})
	}
}

// TestNotFoundPageMatchesReference renders the 404 page for an unmounted and
// for a mounted project, and asserts both against the Python's output.
func TestNotFoundPageMatchesReference(t *testing.T) {
	t.Run("not_found", func(t *testing.T) {
		resetSelectCounter()
		nav := BuildNav(
			[]SourceFile{
				src("index.md", "# Home"),
				src("guide.md", "# Guide"),
				src("api/auth.md", "# Auth"),
				src("api/endpoints.md", "# Endpoints"),
				src("extra.md", "# Extra"),
				src("sixth.md", "# Sixth"),
			},
			map[string]util.Frontmatter{"guide.md": {"title": "The Guide"}},
			nil, nil,
		)
		got, err := Generate404Page(NotFoundOptions{
			ProjectName:  "TestProject",
			Version:      "1.0.0",
			HasCustomCSS: true,
			NavItems:     nav,
			BaseURL:      "https://example.com",
			Lang:         "en",
			FeedURL:      "feed.xml",
			CriticalCSS:  "body{margin:0}",
			ThemeMeta:    themeMeta(t, "minimal"),
		})
		if err != nil {
			t.Fatalf("Generate404Page: %v", err)
		}
		assertEqual(t, "not_found", got, reference(t, "not_found"))
	})

	t.Run("not_found_mounted", func(t *testing.T) {
		resetSelectCounter()
		nav := BuildNav([]SourceFile{
			src("index.md", "# Home"), src("guide.md", "# Guide"),
		}, nil, nil, nil)
		got, err := Generate404Page(NotFoundOptions{
			ProjectName:  "TestProject",
			NavItems:     nav,
			MountLocale:  "en",
			MountProject: "core",
			ThemeMeta:    themeMeta(t, "minimal"),
		})
		if err != nil {
			t.Fatalf("Generate404Page: %v", err)
		}
		assertEqual(t, "not_found_mounted", got,
			reference(t, "not_found_mounted"))
	})
}
