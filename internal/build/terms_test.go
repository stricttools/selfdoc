package build

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

func TestBuildTermAnchors(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a project that declares no term gets no glossary page", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "# Test Project\n\n## Overview\n\n" +
				"Testproject is a documentation generator.\n",
		}})
		if built.exists("glossary/index.html") {
			t.Error("a prose-only site got a glossary page")
		}
	})

	t.Run("every declaration form reaches the glossary", func(t *testing.T) {
		tests := []struct {
			name string
			page string
			term string
		}{
			{
				name: "a definition list",
				page: "# Terms\n\nAPI\n: Application Programming Interface\n",
				term: "API",
			},
			{
				name: "an inline dfn element",
				page: "# Terms\n\nA <dfn>widget</dfn> is a reusable interface element.\n",
				term: "widget",
			},
			{
				name: "the glossary directive",
				page: "# Terms\n\n:<: list-glossary\n:=:\n" +
					"::: **Widget**: A reusable interface element\n:>:\n",
				term: "Widget",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				built := buildFixture(t, fixture{Docs: map[string]string{"terms.md": test.page}})
				assertCarries(t, "glossary/index.html",
					built.page(t, "glossary/index.html"), test.term)
			})
		}
	})

	t.Run("every definition site carries an element id", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"terms.md": "# Terms\n\nAPI\n: Application Programming Interface\n\n" +
				"A <dfn>widget</dfn> is a reusable interface element.\n",
		}})
		assertCarries(t, "terms/index.html", built.page(t, "terms/index.html"),
			`id="term-api"`, `id="term-widget"`)
	})

	t.Run("a glossary source link lands on a real id", func(t *testing.T) {
		// The live defect: the glossary linked a slugified term, an id no
		// page ever emitted, so the link scrolled nowhere.
		built := buildFixture(t, fixture{Docs: map[string]string{
			"terms.md": "# Terms\n\nAPI\n: Application Programming Interface\n",
		}})
		glossary := built.page(t, "glossary/index.html")
		match := regexp.MustCompile(`<a href="([^"]*)">Source</a>`).FindStringSubmatch(glossary)
		if match == nil {
			t.Fatal("the glossary carries no Source link")
		}
		_, fragment, found := strings.Cut(match[1], "#")
		if !found {
			t.Fatalf("the Source link %q carries no fragment", match[1])
		}
		assertCarries(t, "terms/index.html", built.page(t, "terms/index.html"),
			`id="`+fragment+`"`)
	})

	t.Run("a cross-page term link lands on a real id", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"terms.md": "# Terms\n\nWidget\n: A reusable interface element\n",
			"guide.md": "# Guide\n\nEvery Widget needs a home somewhere.\n",
		}})
		guide := built.page(t, "guide/index.html")
		match := regexp.MustCompile(`<a href="([^"]*)" class="term-link"`).FindStringSubmatch(guide)
		if match == nil {
			t.Fatal("no cross-page term link was emitted")
		}
		_, fragment, _ := strings.Cut(match[1], "#")
		assertCarries(t, "terms/index.html", built.page(t, "terms/index.html"),
			`id="`+fragment+`"`)
	})

	t.Run("a term id never reuses a heading's", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"terms.md": "# Terms\n\n## Term Api\n\nSome prose about the section.\n\n" +
				"API\n: Application Programming Interface\n",
		}})
		content := built.page(t, "terms/index.html")
		seen := map[string]bool{}
		for _, match := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(content, -1) {
			if seen[match[1]] {
				t.Errorf("the id %q is emitted twice", match[1])
			}
			seen[match[1]] = true
		}
	})

	t.Run("a definition site links its glossary entry", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"terms.md": "# Terms\n\nAPI\n: Application Programming Interface. Widely used.\n",
		}})
		content := built.page(t, "terms/index.html")
		match := regexp.MustCompile(
			`<dfn id="term-api"[^>]*>\s*<a class="term-def-link" href="([^"]*)"`,
		).FindStringSubmatch(content)
		if match == nil {
			t.Fatal("the definition site is not a link")
		}
		if !strings.HasSuffix(match[1], "glossary/#term-api") {
			t.Errorf("the definition site links %q, want the glossary entry", match[1])
		}
		_, fragment, _ := strings.Cut(match[1], "#")
		assertCarries(t, "glossary/index.html",
			built.page(t, "glossary/index.html"), `id="`+fragment+`"`)
	})

	t.Run("only the definition site links on its own page", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"terms.md": "# Terms\n\nWidget\n: A reusable interface element\n\n" +
				"Every Widget lives in a tree.\n",
		}})
		content := built.page(t, "terms/index.html")
		if count := strings.Count(content, `class="term-def-link"`); count != 1 {
			t.Errorf("the page carries %d definition links, want one", count)
		}
		assertLacks(t, "terms/index.html", content, `class="term-link"`)
	})
}

func TestBuildPageSummaryRule(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	// Which pages print their frontmatter description above the H1. On a
	// reference page that is a useful summary of what the page covers; on
	// the home page and on a post it is the page's own opening line said
	// twice inside one viewport, because both open with a lead paragraph an
	// author wrote. The rule is read off the page's identity, never off its
	// prose.
	const summaryBlock = `<div class="page-summary">`

	t.Run("a reference page keeps its summary", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "# Home\n\nWelcome.\n",
			"reference.md": "+++\ndescription = \"Every flag the command takes.\"\n+++\n" +
				"# Reference\n\nThe flags follow.\n",
		}})
		assertCarries(t, "reference/index.html", built.page(t, "reference/index.html"),
			summaryBlock, "Every flag the command takes.")
	})

	t.Run("the home page does not repeat its description", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "+++\ndescription = \"A curated index of the tools I build.\"\n+++\n" +
				"# Home\n\nA curated index of the tools I build, each linking to " +
				"its documentation.\n",
		}})
		index := built.page(t, "index.html")
		assertLacks(t, "index.html", index, summaryBlock)
		// Only the on-page block is suppressed; the meta tag is untouched.
		assertCarries(t, "index.html", index,
			`name="description" content="A curated index of the tools I build."`)
	})

	t.Run("a post does not repeat its description", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "# Home\n\nWelcome.\n",
			"writing.md": "+++\ntype = \"post\"\ndescription = \"Why the release flow waits for CI.\"\n+++\n" +
				"# Waiting for CI\n\nWhy the release flow waits for CI, and what it costs.\n",
		}})
		page := built.page(t, "writing/index.html")
		assertLacks(t, "writing/index.html", page, summaryBlock)
		assertCarries(t, "writing/index.html", page,
			`name="description" content="Why the release flow waits for CI."`)
	})
}

func TestBuildPageType(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("the derived type reaches the page as a search filter", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"changelog.md": "# Changelog\n\nWhat changed.\n",
		}})
		assertCarries(t, "changelog/index.html", built.page(t, "changelog/index.html"),
			`data-pagefind-filter="type:changelog"`)
	})

	t.Run("a declared type becomes a class a theme can style", func(t *testing.T) {
		// Only a frontmatter-declared type counts: a derived facet type is
		// a search filter, not a design decision.
		built := buildFixture(t, fixture{Docs: map[string]string{
			"cv.md": "+++\ntitle = \"CV\"\ntype = \"cv\"\n" +
				"description = \"The curriculum vitae page of this small site.\"\n" +
				"+++\n\n# CV\n\nA record.\n",
		}})
		assertCarries(t, "cv/index.html", built.page(t, "cv/index.html"),
			`<main id="tm-content" class="content page-cv"`)
	})

	t.Run("a page declaring no type carries no class", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"changelog.md": "# Changelog\n\nWhat changed.\n",
		}})
		page := built.page(t, "changelog/index.html")
		assertCarries(t, "changelog/index.html", page, `<main id="tm-content" class="content"`)
		assertLacks(t, "changelog/index.html", page, "page-changelog")
	})
}

func TestBuildEndToEndWithDirectives(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{
		Files: map[string]string{
			"src/__init__.py": `"""Example module for testing selfdoc builds."""` + "\n\n\n" +
				"def calculate_total(items, tax_rate):\n" +
				`    """Calculate the total price including tax.` + "\n\n" +
				"    Args:\n" +
				"        items: List of item prices.\n" +
				"        tax_rate: Tax rate as a decimal.\n\n" +
				"    Returns:\n" +
				"        The total price with tax applied.\n" +
				`    """` + "\n" +
				"    return sum(items) * (1 + tax_rate)\n",
		},
		Docs: map[string]string{
			"index.md": "+++\ntitle = \"API Reference\"\ndescription = \"Auto-generated API docs\"\n" +
				"date = 2025-01-01\n+++\n\n# API Reference\n\n:-: ref path=\"src\"\n",
			"notes.md": "+++\ntitle = \"Notes\"\ndescription = \"Important notes\"\n" +
				"date = 2025-01-01\n+++\n\n# Notes\n\n" +
				":<: callout-note\n:=:\n::: This is an important note.\n:>:\n",
			"glossary.md": "+++\ntitle = \"Glossary\"\ndescription = \"Key terms\"\n" +
				"date = 2025-01-01\n+++\n\n# Glossary\n\n" +
				":<: list-glossary\n:=:\n" +
				"::: **Directive**: A block in Markdown that selfdoc resolves\n" +
				"::: **Extractor**: A language-specific module that reads code\n:>:\n",
		},
	})

	for _, outputKey := range []string{
		"index.html", "notes/index.html", "glossary/index.html", "style.css",
	} {
		if !built.exists(outputKey) {
			t.Errorf("%s was not written", outputKey)
		}
	}

	index := built.page(t, "index.html")
	assertCarries(t, "index.html", index, "<!DOCTYPE html>", "calculate_total")
	assertLacks(t, "index.html", index, "selfdoc:")

	assertCarries(t, "notes/index.html", built.page(t, "notes/index.html"),
		`<div class="callout callout-note">`, "callout-title", "important note")

	assertCarries(t, "glossary/index.html", built.page(t, "glossary/index.html"),
		"<dl>", `<dt><dfn id="term-directive">Directive</dfn></dt>`,
		"<dd>", "language-specific module")
}
