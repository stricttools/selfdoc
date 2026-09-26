package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// The emitted-reference pass (LINK001) reads the built tree under
// the build output directory, which no check invalidates. A page's body there is whatever
// the last build rendered, so after a source doc comment changes, the built
// page still carries the old rendering -- and a link that rendering named is
// not evidence about the sources this run is checking.

// builtPage is one page of a hand-written build output, with body as the
// content region's markup.
func builtPage(t *testing.T, root, outputRel, body string) {
	t.Helper()
	write(t, filepath.Join(root, "stricttools", ".docs-cache", "build", filepath.FromSlash(outputRel)),
		"<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<title>Page</title>\n"+
			"</head>\n<body>\n<main id=\"tm-content\" class=\"content\">\n"+
			body+"\n</main>\n</body>\n</html>\n")
}

// goPackageDocProject writes a Go project whose package documentation carries
// one Markdown link, resolved onto an API page by a ref directive, plus a
// built tree whose version of that page carries staleLink instead.
func goPackageDocProject(t *testing.T, currentLink, staleLink string) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, configForSource(
		map[string]any{"path": "pkg/", "language": "go"},
	))
	write(t, filepath.Join(root, "go.mod"), "module example.com/proj\n\ngo 1.21\n")
	write(t, filepath.Join(root, "pkg", "html", "html.go"),
		"// Package html converts Markdown to HTML, and the whole picture is in\n"+
			"// ["+"the guide"+"]("+currentLink+") for a reader who wants it.\n"+
			"package html\n\n"+
			"// Convert converts markdown to HTML.\n"+
			"func Convert(s string) string { return s }\n")
	write(t, filepath.Join(root, "stricttools", "docs", "internal-html.md"),
		"+++\ndescription = \"The API page of the html package, described at a "+
			"length the description rules have nothing to say about.\"\n+++\n"+
			"# internal html\n\n:-: ref path=\"html\" lang=\"go\"\n")
	write(t, filepath.Join(root, "stricttools", "docs", "guide.md"),
		"+++\ndescription = \"The guide page this fixture links to, described at a "+
			"length the description rules have nothing to say about.\"\n+++\n"+
			"# Guide\n\nThe guide exists so the link from the package documentation resolves.\n")

	builtPage(t, root, "internal-html/index.html",
		`<p>Package html converts Markdown to HTML, and the whole picture is in `+
			`<a href="`+staleLink+`">the guide</a> for a reader who wants it.</p>`)
	builtPage(t, root, "guide/index.html", "<p>The guide.</p>")
	return root
}

func TestAStaleBuiltBodyIsNotEvidenceAboutTheSources(t *testing.T) {
	// The doc comment now links to the guide; the built page still carries
	// the manual link an earlier rendering had, and no manual page was ever
	// written.
	root := goPackageDocProject(t, "../guide/", "../manual/")

	result := checkFixture(t, root)

	if hasCode(result.Lints, "LINK001") {
		t.Fatalf("LINK001 reported a reference the current source does not carry: %v",
			withCode(result.Lints, "LINK001")[0].Message())
	}
}

func TestAStaleBuiltBodyIsNotEvidenceUnderADeclaredLocale(t *testing.T) {
	// A project declaring one locale names its pages "en/page.md" while
	// serving them from the output root, so the source of a built page is
	// found under either name.
	root := goPackageDocProject(t, "../guide/", "../manual/")
	projectConfig := configForSource(
		map[string]any{"path": "pkg/", "language": "go"},
	)
	projectConfig["locales"] = []any{
		map[string]any{"code": "en", "label": "English", "default": true},
	}
	writeConfig(t, root, projectConfig)

	result := checkFixture(t, root)

	if hasCode(result.Lints, "LINK001") {
		t.Fatalf("LINK001 reported a reference the current source does not carry: %v",
			withCode(result.Lints, "LINK001")[0].Message())
	}
}

func TestALinkTheCurrentSourceCarriesIsStillReported(t *testing.T) {
	// Here the source itself names the missing page, so the built tree and
	// the source agree and the finding is about the sources being checked.
	root := goPackageDocProject(t, "../manual/", "../manual/")

	result := checkFixture(t, root)

	if !hasCode(result.Lints, "LINK001") {
		t.Fatalf("LINK001 missing for a link the source carries: %v", codes(result.Lints))
	}
}

func TestALinkTheCurrentSourceWritesAsMarkdownIsStillReported(t *testing.T) {
	// The source writes the reference the way an author does -- a ".md"
	// path -- and the build emits it as the directory address that page is
	// served from. The two are never spelled the same, so reading the
	// Markdown for the emitted text alone suppresses a real finding.
	root := goPackageDocProject(t, "../guide/", "../guide/")
	write(t, filepath.Join(root, "stricttools", "docs", "guide.md"),
		"+++\ndescription = \"The guide page this fixture links from, described at "+
			"a length the description rules have nothing to say about.\"\n+++\n"+
			"# Guide\n\nSee the [manual](manual.md) for the rest of it.\n")
	builtPage(t, root, "guide/index.html",
		`<p>See the <a href="../manual/">manual</a> for the rest of it.</p>`)

	result := checkFixture(t, root)

	if !hasCode(result.Lints, "LINK001") {
		t.Fatalf("LINK001 missing for a link the source writes as Markdown: %v",
			codes(result.Lints))
	}
	if message := withCode(result.Lints, "LINK001")[0].Message(); !strings.Contains(
		message, "manual") {
		t.Errorf("message = %q", message)
	}
}
