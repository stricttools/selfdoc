package page

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/tokenizer"
)

var (
	// htmlTagRE strips inline markup from a heading, a list item or a
	// definition so the text can go into an attribute or a JSON string.
	htmlTagRE = regexp.MustCompile(`<[^>]+>`)
	// firstParagraphRE finds the first paragraph element of a rendered body,
	// newlines included -- a soft-wrapped paragraph spans several lines.
	firstParagraphRE = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	// tocHeadingIDRE reads the id off a heading's opening tag, starting
	// after the tag name.
	tocHeadingIDRE = regexp.MustCompile(`^[ \t\n\r\f\x0b]+id="([^"]+)">`)
	// tocHeadingLinkRE matches the heading's own anchor element, which is
	// what separates the heading's chrome from its text.
	tocHeadingLinkRE = regexp.MustCompile(`^<a[^>]*class="heading-link"[^>]*>[^<]*</a>`)
	// tocAnyTagRE matches one element tag of the heading's chrome, for the
	// scan that walks past whatever stands before the anchor.
	tocAnyTagRE = regexp.MustCompile(`^<[^>]+>`)
)

// ExtractTitle returns the text of the first H1 heading in Markdown content,
// or fallback when the content has none.
//
// It reads the block token list rather than scanning lines, so a
// "#"-prefixed line inside a fenced code block is code and never a title.
func ExtractTitle(mdContent, fallback string) string {
	for _, tok := range tokenizer.Tokenize(mdContent) {
		if h, ok := tok.(tokenizer.Heading); ok && h.Level == 1 {
			return h.Text
		}
	}
	return fallback
}

// extractFirstParagraph returns the plain text of the first paragraph element
// of rendered HTML, with inline markup stripped, or "" when there is none.
func extractFirstParagraph(bodyHTML string) string {
	m := firstParagraphRE.FindStringSubmatch(bodyHTML)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(htmlTagRE.ReplaceAllString(m[1], ""))
}

// buildTOC extracts the H2 and H3 headings of a rendered body and builds the
// table of contents, or "" when the page carries fewer than two of them.
//
// It is a scanner rather than one pattern because the pattern it replaces
// closed each heading with a backreference to the tag it opened, which RE2
// cannot express. What it reproduces, including the parts that only show on
// malformed input:
//
//   - A heading is an "<h2" or "<h3" whose opening tag carries an id.
//   - Between that tag and the heading text stands the heading's own anchor
//     element. Whatever else stands before the anchor -- markup the renderer
//     did not emit -- is skipped one element at a time.
//   - The heading text runs to the next matching close tag ON THE SAME LINE.
//     A newline before it is what the original pattern's non-DOTALL wildcard
//     refused, and the refusal is not the end of the attempt: the scan walks
//     on past the anchor, exactly as the pattern's lazy repetition did, so a
//     heading whose text spans a newline ends up pairing its own id with a
//     LATER heading's text. That output is odd, and it is the output the
//     pattern produced, so it is the output here.
func buildTOC(bodyHTML string) string {
	type tocHeading struct {
		tag  string
		slug string
		text string
	}
	var headings []tocHeading

	for i := 0; i < len(bodyHTML); {
		next := strings.IndexByte(bodyHTML[i:], '<')
		if next < 0 {
			break
		}
		pos := i + next
		var tag string
		switch {
		case strings.HasPrefix(bodyHTML[pos:], "<h2"):
			tag = "h2"
		case strings.HasPrefix(bodyHTML[pos:], "<h3"):
			tag = "h3"
		default:
			i = pos + 1
			continue
		}
		idMatch := tocHeadingIDRE.FindStringSubmatch(bodyHTML[pos+3:])
		if idMatch == nil {
			i = pos + 1
			continue
		}
		afterOpen := pos + 3 + len(idMatch[0])
		closeTag := "</" + tag + ">"

		textStart, textEnd := -1, -1
		for scan := afterOpen; ; {
			for scan < len(bodyHTML) && bodyHTML[scan] != '<' {
				scan++
			}
			if scan >= len(bodyHTML) {
				break
			}
			if anchor := tocHeadingLinkRE.FindString(bodyHTML[scan:]); anchor != "" {
				candidate := scan + len(anchor)
				offset := strings.Index(bodyHTML[candidate:], closeTag)
				if offset >= 0 && !strings.ContainsRune(
					bodyHTML[candidate:candidate+offset], '\n') {
					textStart, textEnd = candidate, candidate+offset
					break
				}
				// The heading text cannot cross a newline, so this anchor
				// does not end the run before the text; the scan walks on
				// past it.
			}
			skipped := tocAnyTagRE.FindString(bodyHTML[scan:])
			if skipped == "" {
				break
			}
			scan += len(skipped)
		}
		if textStart < 0 {
			i = pos + 1
			continue
		}

		headings = append(headings, tocHeading{
			tag:  tag,
			slug: idMatch[1],
			text: bodyHTML[textStart:textEnd],
		})
		i = textEnd + len(closeTag)
	}

	if len(headings) < 2 {
		return ""
	}

	items := make([]string, 0, len(headings))
	for _, h := range headings {
		clean := strings.TrimSpace(htmlTagRE.ReplaceAllString(h.text, ""))
		indentCls := ""
		if h.tag == "h3" {
			indentCls = " sub"
		}
		items = append(items,
			`<a class="docs-toc-item`+indentCls+`" href="#`+h.slug+`">`+
				html.EscapeHTML(clean)+`</a>`)
	}
	// The anchor spelling of the framework's table of contents: real URLs,
	// so the entries work with scripting off. The scrollspy sets
	// aria-current="page" on the entry the reader has scrolled to, which is
	// the static counterpart of the active class a router would toggle.
	return `<nav class="docs-toc" aria-label="On this page">` +
		`<div class="docs-toc-head">Contents</div>` +
		strings.Join(items, "\n") +
		`</nav>`
}

// buildBreadcrumbs builds the breadcrumb trail for a non-index page.
//
// A flat page like "guide/index.html" produces "Home / Guide"; a
// subdirectory page like "api/endpoints/index.html" produces
// "Home / Api / Endpoints" with the intermediate directory linked. An
// intermediate directory with no index page of its own is rendered as static
// text rather than as a link to a page no build wrote.
//
// prefix is the hop to this page's own mount root, which the intermediate
// crumbs are pages of. sitePrefix is the hop to the site level, for an
// ancestor that lives there -- a post's ancestor is "blog/", which under a
// mount is served from the site root rather than the project's subtree; pass
// prefix for it wherever the two roots coincide. homeHrefValue is where the
// "Home" crumb points, which is not always this mount's own index.
func buildBreadcrumbs(
	htmlPath, pageTitle, prefix string,
	existingPages map[string]bool,
	homeHrefValue, sitePrefix string,
) string {
	logicalPath := strings.TrimSuffix(htmlPath, "/index.html")
	parts := strings.Split(logicalPath, "/")

	var crumbs strings.Builder
	crumbs.WriteString(`<li class="tm-crumb">` +
		`<a class="tm-crumb-link" href="` + homeHrefValue + `">Home</a></li>`)
	for i, dirName := range parts[:len(parts)-1] {
		dirPath := strings.Join(parts[:i+1], "/")
		label := html.EscapeHTML(pythonCapitalize(dirName))
		target := dirPath + "/index.html"
		if existingPages[target] {
			hop := html.PathHop(target, prefix, sitePrefix)
			crumbs.WriteString(`<li class="tm-crumb"><a class="tm-crumb-link" ` +
				`href="` + hop + html.HTMLPathToURL(target) + `">` + label + `</a></li>`)
		} else {
			crumbs.WriteString(`<li class="tm-crumb">` +
				`<span class="tm-crumb-static">` + label + `</span></li>`)
		}
	}
	// The final segment is the current page: never a link, and the one
	// crumb carrying aria-current.
	crumbs.WriteString(`<li class="tm-crumb"><span class="tm-crumb-current" ` +
		`aria-current="page">` + html.EscapeHTML(pageTitle) + `</span></li>`)

	// The separators are drawn in CSS between list items -- there is no
	// separator element to emit.
	return `<nav class="tm-crumbs-nav" aria-label="Breadcrumb">` +
		`<ol class="tm-crumbs">` + crumbs.String() + `</ol></nav>`
}
