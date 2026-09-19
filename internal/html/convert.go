package html

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/stricttools/selfdoc/internal/util"

	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/icons"
	"github.com/stricttools/selfdoc/internal/tokenizer"
)

// HeadingAnchor is one heading and the element id it will carry on the
// built page.
//
// A renderer walking the same tokens the anchors were assigned from looks
// each one up by its token index.
type HeadingAnchor struct {
	// Index is the heading's position in the token list it came from.
	Index int
	// Level is the heading level, 1 through 6.
	Level int
	// Text is the heading's Markdown text, as written.
	Text string
	// Anchor is the element id the heading carries on the built page.
	Anchor string
	// IsPageTitle marks the first H1, which the page chrome emits as the
	// page title heading instead of the body renderer emitting it.
	IsPageTitle bool
}

// PageTitleAnchor returns the element id the page-title H1 carries.
//
// Part of the anchor authority: the page title is rendered as an H1 by the
// page chrome rather than by the body renderer, so both sides ask this
// function instead of slugifying the title themselves. Like every other
// heading, the title is slugified from its RENDERED inline form, so
// "# The `build` command" anchors the same way whether the words reach the
// page through frontmatter or through markdown.
func PageTitleAnchor(title string) string { return Slugify(inlineFormat(title)) }

// AssignHeadingAnchors assigns the final element id to every heading in
// tokens.
//
// This is the one place heading anchors are decided. The HTML renderer
// emits these ids and the search index links to them, so the two cannot
// drift: a repeated heading gets "setup", "setup-1", "setup-2" in both.
// Because the input is the block token list, a "#"-prefixed line inside a
// fenced code block is code and never becomes an anchor.
//
// The first H1 is not rendered in the body -- the page chrome emits it as
// the page title heading, whose id comes from the page title. Pass
// pageTitle (the frontmatter title, else the H1 text) to get that id
// right; it is reported with IsPageTitle true. A nil pageTitle means the
// H1's own text is the title.
//
// The result is in document order.
func AssignHeadingAnchors(tokens []tokenizer.Token, pageTitle *string) []HeadingAnchor {
	seenSlugs := map[string]int{} // base slug -> how many times it has been used
	pageTitleConsumed := false
	var anchors []HeadingAnchor

	for index, token := range tokens {
		heading, ok := token.(tokenizer.Heading)
		if !ok {
			continue
		}
		if heading.Level == 1 && !pageTitleConsumed {
			pageTitleConsumed = true
			title := heading.Text
			if pageTitle != nil {
				title = *pageTitle
			}
			anchors = append(anchors, HeadingAnchor{
				Index:       index,
				Level:       heading.Level,
				Text:        heading.Text,
				Anchor:      PageTitleAnchor(title),
				IsPageTitle: true,
			})
			continue
		}
		slug := Slugify(inlineFormat(heading.Text))
		if count, seen := seenSlugs[slug]; seen {
			seenSlugs[slug] = count + 1
			slug = slug + "-" + strconv.Itoa(count+1)
		} else {
			seenSlugs[slug] = 0
		}
		anchors = append(anchors, HeadingAnchor{
			Index:       index,
			Level:       heading.Level,
			Text:        heading.Text,
			Anchor:      slug,
			IsPageTitle: false,
		})
	}
	return anchors
}

// renderHeading renders a Heading token to HTML.
//
// The second result is false for the first H1, which the page chrome
// consumes as the page title. headingAnchor is this token's entry from
// [AssignHeadingAnchors].
func renderHeading(token tokenizer.Heading, headingAnchor HeadingAnchor) (string, bool) {
	if headingAnchor.IsPageTitle {
		return "", false
	}
	content := inlineFormat(token.Text)
	slug := headingAnchor.Anchor
	readable := strings.ReplaceAll(tagRE.ReplaceAllString(content, ""), "_", " ")
	level := strconv.Itoa(token.Level)
	anchor := `<a class="heading-link" href="#` + slug + `"` +
		` aria-label="Link to section: ` + EscapeHTML(readable) + `">#</a>`
	return "<h" + level + ` id="` + slug + `">` + anchor + content + "</h" + level + ">", true
}

// renderTable renders a Table token to HTML, wrapped in .table-wrap with a
// caption taken from the most recent heading, for accessibility.
func renderTable(token tokenizer.Table, tokens []tokenizer.Token, idx int) string {
	tableHTML := ParseTable(token.Rows)
	captionText := ""
	for prevIdx := idx - 1; prevIdx >= 0; prevIdx-- {
		prevHeading, ok := tokens[prevIdx].(tokenizer.Heading)
		if !ok {
			continue
		}
		rendered := inlineFormat(prevHeading.Text)
		captionText = util.PythonStrip(tagRE.ReplaceAllString(rendered, ""))
		break
	}
	if captionText != "" {
		const open = `<table class="data tm-table" role="grid">`
		tableHTML = strings.Replace(
			tableHTML, open,
			open+`<caption class="sr-only">`+EscapeHTML(captionText)+"</caption>",
			1,
		)
	}
	return `<div class="table-wrap">` + tableHTML + `</div>`
}

// renderDefinitionList renders a DefinitionList token to HTML.
func renderDefinitionList(token tokenizer.DefinitionList) string {
	var dlItems []string
	for _, entry := range token.Entries {
		dlItems = append(dlItems, "<dt><dfn>"+inlineFormat(entry.Term)+"</dfn></dt>")
		for _, defnText := range entry.Definitions {
			dlItems = append(dlItems, "<dd>"+inlineFormat(defnText)+"</dd>")
		}
	}
	return `<div class="glossary">` + "\n<dl>\n" +
		strings.Join(dlItems, "\n") +
		"\n</dl>\n</div>"
}

// renderBlock dispatches a single block token to its HTML renderer.
//
// headingAnchors maps a token index to its [HeadingAnchor]. The second
// result is false when the token produces no output -- a blank line, or the
// first H1.
func renderBlock(token tokenizer.Token, tokens []tokenizer.Token, idx int,
	headingAnchors map[int]HeadingAnchor,
	runButton, lineNumbers bool, codeIcons string) (string, bool) {
	switch tok := token.(type) {
	case tokenizer.Heading:
		return renderHeading(tok, headingAnchors[idx])
	case tokenizer.CodeBlock:
		// Per-block run and line-number annotations, or the global config.
		return renderCodeBlock(
			tok.Lang, tok.Lines, tok.Annotations,
			tok.Run || runButton,
			tok.LineNumbers || lineNumbers,
			tok.LineStart, codeIcons,
		), true
	case tokenizer.Table:
		return renderTable(tok, tokens, idx), true
	case tokenizer.UnorderedList:
		var items strings.Builder
		for _, item := range tok.Items {
			items.WriteString("<li>" + inlineFormat(item) + "</li>")
		}
		return "<ul>" + items.String() + "</ul>", true
	case tokenizer.OrderedList:
		var items strings.Builder
		for _, item := range tok.Items {
			items.WriteString("<li>" + inlineFormat(item) + "</li>")
		}
		return "<ol>" + items.String() + "</ol>", true
	case tokenizer.Blockquote:
		return parseBlockquote(tok.Lines), true
	case tokenizer.DefinitionList:
		return renderDefinitionList(tok), true
	case tokenizer.ThematicBreak:
		return "<hr>", true
	case tokenizer.BlankLine:
		return "", false
	case tokenizer.Paragraph:
		return "<p>" + inlineFormat(strings.Join(tok.Lines, " ")) + "</p>", true
	}
	return "", false
}

// MdToHTML converts Markdown text to the HTML a page's body carries.
//
// It handles headings, code blocks (with tabs and annotations), inline
// code, paragraphs, unordered and ordered lists, links, bold, italic and
// tables.
//
// metadata is the page's frontmatter. Its "auto_steps" and "auto_api" keys
// override the corresponding global settings from cfg.
//
// cfg is the project config. Its "auto_detect" key -- an object with
// optional bool keys "steps" and "api_entries" -- controls whether the
// heuristics run globally; per-page metadata takes precedence. "run_button",
// "line_numbers" and "code_icons" configure code blocks. Either map may be
// nil.
func MdToHTML(text string, metadata, cfg map[string]any) string {
	tokens := tokenizer.Tokenize(text)
	var htmlParts []string
	// The single heading scan: ids come from here, and the search index
	// reads the same assignment from the same tokens.
	headingAnchors := map[int]HeadingAnchor{}
	for _, ha := range AssignHeadingAnchors(tokens, nil) {
		headingAnchors[ha.Index] = ha
	}
	cfgRunButton := truthy(cfg["run_button"])
	cfgLineNumbers := truthy(cfg["line_numbers"])
	cfgCodeIcons := "colorful"
	if cfg != nil {
		if s, ok := cfg["code_icons"].(string); ok {
			cfgCodeIcons = s
		}
	}

	for idx, token := range tokens {
		rendered, ok := renderBlock(
			token, tokens, idx, headingAnchors,
			cfgRunButton, cfgLineNumbers, cfgCodeIcons,
		)
		if ok {
			htmlParts = append(htmlParts, rendered)
		}
	}

	result := strings.Join(htmlParts, "\n")

	// Group consecutive code blocks into tabs.
	result = groupCodeTabs(result)

	// Add class="steps" to an <ol> after a step/guide/tutorial heading.
	// Skipped when opted out per page ("auto_steps": false) or globally
	// ("auto_detect.steps": false).
	autoDetect, _ := cfg["auto_detect"].(map[string]any)
	autoStepsGlobal := true
	if v, ok := autoDetect["steps"]; ok {
		autoStepsGlobal = truthy(v)
	}
	runSteps := autoStepsGlobal
	if v, ok := metadata["auto_steps"]; ok && v != nil {
		runSteps = truthy(v)
	}
	if runSteps {
		result = applyStepGuides(result)
	}

	// Wrap h3/h4 + code block + description in API entry cards. Skipped
	// when opted out per page ("auto_api": false) or globally
	// ("auto_detect.api_entries": false).
	autoAPIGlobal := false
	if v, ok := autoDetect["api_entries"]; ok {
		autoAPIGlobal = truthy(v)
	}
	runAPI := autoAPIGlobal
	if v, ok := metadata["auto_api"]; ok && v != nil {
		runAPI = truthy(v)
	}
	if runAPI {
		result = wrapAPIEntries(result)
	}

	// Give every author-declared definition site an id, so the glossary
	// can link to it. A term is only ever declared, never inferred from
	// prose.
	result = assignDefinitionIDs(result)

	// Mark the first image as a high-priority LCP candidate. Every image
	// starts with loading="lazy" from inlineFormat; the first one is
	// promoted to eager loading with high fetch priority.
	result = strings.Replace(
		result, `loading="lazy"`, `fetchpriority="high" loading="eager"`, 1,
	)

	return result
}

// truthy reports whether v is true to Python: a set bool, a non-empty
// string, a non-zero number, or a non-empty collection.
//
// A config value reaches here as whatever the JSON decoder produced, and
// the Python this ports read every one of these keys with plain "or" and
// "if" tests rather than a type check.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}

// renderCodeBlock renders a single fenced code block to HTML.
//
// It handles diff highlighting, inline code annotations, build-time syntax
// highlighting via chroma, optional line numbers and language icons.
func renderCodeBlock(lang string, codeLines []string, annotations []tokenizer.Annotation,
	run, lineNumbers bool, lineStart int, codeIcons string) string {
	// Diff-style line highlighting.
	isDiff := lang == "diff"
	if !isDiff {
		for _, cl := range codeLines {
			if strings.HasPrefix(cl, "+") || strings.HasPrefix(cl, "-") {
				isDiff = true
				break
			}
		}
	}

	var codeContent string
	switch {
	case isDiff:
		codeContent = renderDiffLines(codeLines)
	case lang != "":
		codeContent = highlightCode(lang, strings.Join(codeLines, "\n"))
	default:
		codeContent = EscapeHTML(strings.Join(codeLines, "\n"))
	}

	// Replace annotation markers "// [N]" or "# [N]" with badge elements.
	// The markers were already HTML-escaped by the highlighter, so the
	// escaped forms are what is matched.
	for _, annotation := range annotations {
		badge := `<span class="code-annotation" data-note="` +
			EscapeHTML(annotation.Note) + `" tabindex="0">` +
			EscapeHTML(annotation.Key) + "</span>"
		key := EscapeHTML(annotation.Key)
		codeContent = strings.ReplaceAll(codeContent, "// ["+key+"]", badge)
		codeContent = strings.ReplaceAll(codeContent, "# ["+key+"]", badge)
	}

	// Wrap each line in a <span class="tm-code-line"> for line numbers.
	// The numbers themselves are a CSS counter over these wrappers, so
	// nothing here emits a number and a copied selection is code and
	// nothing else.
	if lineNumbers && !isDiff {
		wrapped := strings.Split(codeContent, "\n")
		for i, rawLine := range wrapped {
			wrapped[i] = `<span class="tm-code-line">` + rawLine + "</span>"
		}
		codeContent = strings.Join(wrapped, "\n")
	}

	runAttr := ""
	if run {
		runAttr = ` data-run="true"`
	}
	hasLN := lineNumbers && !isDiff
	lnClass, lnAttr, lnStyle := "", "", ""
	if hasLN {
		lnClass = " tm-code-numbered"
		lnAttr = ` data-line-start="` + strconv.Itoa(lineStart) + `"`
		// Inline counter-reset so line numbering starts at the right
		// value: counter-reset sets the initial value and
		// counter-increment fires before display, so the reset is to
		// lineStart - 1.
		lnStyle = ` style="counter-reset:tm-code-line ` + strconv.Itoa(lineStart-1) + `"`
	}
	// The framework's code chrome: a <figure class="tm-code"> wrapping the
	// label bar and the <pre>. The bar is always emitted, because it is
	// where the copy button goes -- .tm-code-actions is the slot the sheet
	// reserves and the script fills.
	var label, aria, codeOpen string
	if lang != "" {
		escapedLang := EscapeHTML(lang)
		iconHTML := ""
		if codeIcons != "none" {
			if svg, ok := icons.GetIcon(lang, codeIcons); ok {
				iconHTML = svg + " "
			}
		}
		label = `<span class="tm-code-label">` + iconHTML + escapedLang + "</span>"
		aria = "Code: " + escapedLang
		codeOpen = `<code class="language-` + escapedLang + `">`
	} else {
		label = `<span class="tm-code-label"></span>`
		aria = "Code block"
		codeOpen = "<code>"
	}
	return `<figure class="tm-code` + lnClass + `"` + runAttr + lnStyle + ">" +
		`<figcaption class="tm-code-bar">` + label +
		`<span class="tm-code-actions"></span></figcaption>` +
		`<pre tabindex="0" aria-label="` + aria + `"` + lnAttr + ">" +
		codeOpen + codeContent + "</code></pre>" +
		"</figure>"
}

// highlightCode highlights source in the named language, returning the
// spans that go inside this package's own <pre><code> wrapper.
//
// An unknown language, or any failure inside chroma, falls back to the
// escaped source -- the same answer the Pygments path gave when its lexer
// lookup raised. The fallback is a rendering decision, not a silent
// substitution of a different highlighter: no language was recognized, so
// there is nothing to highlight.
//
// Note one difference from the Pygments lookup this replaces: chroma's
// registry also resolves a name through file extensions and filename
// patterns, so a fence language Pygments would have rejected can find a
// lexer here.
func highlightCode(lang, source string) string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return EscapeHTML(source)
	}
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return EscapeHTML(source)
	}
	var buf bytes.Buffer
	if err := highlightFormatter.Format(&buf, highlightStyle, iterator); err != nil {
		return EscapeHTML(source)
	}
	// The tokenizer appends a trailing newline; strip it so there is no
	// extra blank line inside <code>.
	return strings.TrimRight(buf.String(), "\n")
}

// codeBlockWithLabelPrefix is the literal head of a code figure that
// carries a language label. Whether the label is really populated is
// decided by the check [hasCodeBlockWithLabel] performs after the match,
// standing in for the `(?!</span>)` lookahead RE2 cannot express.
const codeBlockWithLabelPrefix = `<figcaption class="tm-code-bar"><span class="tm-code-label">`

var codeBlockWithLabelRE = regexp.MustCompile(
	`<figure class="tm-code"[^>]*>` + codeBlockWithLabelPrefix,
)

// hasCodeBlockWithLabel reports whether s carries a code figure whose
// language label is not empty.
//
// The Python this ports asked the same question with a negative lookahead
// on the closing </span>; here the match is located first and the
// following text checked for it, which is the same decision without
// lookaround.
func hasCodeBlockWithLabel(s string) bool {
	for _, m := range codeBlockWithLabelRE.FindAllStringIndex(s, -1) {
		if !strings.HasPrefix(s[m[1]:], "</span>") {
			return true
		}
	}
	return false
}

var codeLabelTextRE = regexp.MustCompile(
	`(?s)<span class="tm-code-label">(?:<svg[^>]*>.*?</svg>\s*)?([^<]+)</span>`,
)

// groupCodeTabs groups consecutive code blocks into a tabbed interface.
//
// It detects runs of consecutive <figure class="tm-code"> elements with
// different language labels and wraps them in a tab container. Only blocks
// that have a language label are grouped.
func groupCodeTabs(html string) string {
	parts := strings.Split(html, "\n")
	var result []string
	i := 0

	for i < len(parts) {
		part := parts[i]
		if !hasCodeBlockWithLabel(part) {
			result = append(result, part)
			i++
			continue
		}
		// Collect consecutive code blocks with language labels.
		group := []string{part}
		j := i + 1
		for j < len(parts) && hasCodeBlockWithLabel(parts[j]) {
			group = append(group, parts[j])
			j++
		}
		if len(group) < 2 {
			result = append(result, part)
			i++
			continue
		}
		var tabs, panels []string
		for idx, blockHTML := range group {
			// Extract the language text from the code bar's label. The
			// label may carry an SVG icon before the text, so the match
			// takes the text node after any SVG.
			lang := "Tab " + strconv.Itoa(idx+1)
			if m := codeLabelTextRE.FindStringSubmatch(blockHTML); m != nil {
				lang = util.PythonStrip(m[1])
			}
			langID := strings.ReplaceAll(strings.ToLower(lang), " ", "-")
			active := ""
			selected := "false"
			if idx == 0 {
				active = " active"
				selected = "true"
			}
			escapedLangID := EscapeHTML(langID)
			tabs = append(tabs,
				`<button class="tab`+active+`" `+
					`role="tab" `+
					`id="tab-`+escapedLangID+`" `+
					`aria-selected="`+selected+`" `+
					`aria-controls="panel-`+escapedLangID+`" `+
					`data-lang="`+escapedLangID+`">`+
					EscapeHTML(lang)+`</button>`)
			panels = append(panels,
				`<div class="tab-panel`+active+`" `+
					`role="tabpanel" `+
					`id="panel-`+escapedLangID+`" `+
					`aria-labelledby="tab-`+escapedLangID+`" `+
					`data-lang="`+escapedLangID+`">`+
					blockHTML+`</div>`)
		}
		tabBar := `<div class="tab-bar" role="tablist">` + strings.Join(tabs, "") + `</div>`
		result = append(result,
			`<div class="code-tabs">`+tabBar+strings.Join(panels, "")+`</div>`)
		i = j
	}

	return strings.Join(result, "\n")
}

var (
	// Case-insensitive but NOT dot-all, matching the pattern this ports:
	// a heading the renderer emits never spans a newline, and letting the
	// match cross one would let a heading swallow the markup after it.
	anyHeadingRE = regexp.MustCompile(`(?i)<h[23]\s[^>]*>.*?</h[23]>`)
	kwStartRE    = regexp.MustCompile(`(?i)^(step|guide|tutorial)\b`)
	olOpenRE     = regexp.MustCompile(`^<ol(?:\s[^>]*)?>`)
	olClassRE    = regexp.MustCompile(`^\s[^>]*class=`)
)

// findUnclassedOL searches s from offset for an <ol> opening tag that
// carries no class attribute, returning the match's bounds.
//
// This is the hand-written form of `<ol(?!\s[^>]*class=)(?:\s[^>]*)?>|<ol>`.
// The negative lookahead is the whole point of the pattern -- an <ol> the
// author already classed must not be reclassed -- and RE2 cannot express
// it. Candidates are located by the plain tag pattern and the lookahead's
// own pattern is then matched against what follows "<ol"; when it hits,
// the candidate is rejected and the search resumes one byte later, which
// is what the regex engine did when both alternatives failed at a
// position. The second alternative of the original is unreachable: the
// first already matches a bare "<ol>".
func findUnclassedOL(s string, offset int) (start, end int, ok bool) {
	for i := offset; i < len(s); {
		rel := strings.Index(s[i:], "<ol")
		if rel < 0 {
			return 0, 0, false
		}
		pos := i + rel
		m := olOpenRE.FindStringIndex(s[pos:])
		if m == nil || olClassRE.MatchString(s[pos+3:]) {
			i = pos + 1
			continue
		}
		return pos, pos + m[1], true
	}
	return 0, 0, false
}

// applyStepGuides adds class="steps" to an <ol> that follows a
// step/guide/tutorial heading.
//
// Each unclassed <ol> is found in turn, then the 200 characters before it
// are searched for an h2 or h3 whose plain text begins with "step",
// "guide" or "tutorial", case-insensitively. When one is found with no
// intervening h2 or h3 between it and the <ol>, the list gets the class.
func applyStepGuides(html string) string {
	result := html
	offset := 0
	for {
		olStart, olEnd, ok := findUnclassedOL(result, offset)
		if !ok {
			break
		}
		// Search backward up to 200 characters for a keyword heading.
		lookbackStart := runesBack(result, olStart, 200)
		preceding := result[lookbackStart:olStart]
		var kwMatches [][]int
		for _, m := range anyHeadingRE.FindAllStringIndex(preceding, -1) {
			headingHTML := preceding[m[0]:m[1]]
			innerStart := strings.Index(headingHTML, ">") + 1
			innerEnd := strings.LastIndex(headingHTML, "<")
			innerHTML := headingHTML[innerStart:innerEnd]
			plainText := util.PythonStrip(strings.TrimLeft(tagRE.ReplaceAllString(innerHTML, ""), "#"))
			if kwStartRE.MatchString(plainText) {
				kwMatches = append(kwMatches, m)
			}
		}
		if len(kwMatches) == 0 {
			offset = olEnd
			continue
		}
		// Use the last keyword heading found, and reject the list when
		// another h2/h3 stands between that heading and it.
		lastKW := kwMatches[len(kwMatches)-1]
		if anyHeadingRE.MatchString(preceding[lastKW[1]:]) {
			offset = olEnd
			continue
		}
		oldTag := result[olStart:olEnd]
		newTag := `<ol class="steps">`
		if oldTag != "<ol>" {
			newTag = strings.Replace(oldTag, "<ol", `<ol class="steps"`, 1)
		}
		result = result[:olStart] + newTag + result[olEnd:]
		offset = olStart + len(newTag)
	}
	return result
}

var (
	identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*(\(.*\))?$`)
	apiEntryRE   = regexp.MustCompile(`(?s)(<h[34]\s[^>]*>.*?</h[34]>)\s*(<figure class="tm-code">.*?</figure>)\s*(<p>.*?</p>)`)
)

// wrapAPIEntries wraps an h3 or h4 heading, the code figure under it and
// the paragraph after that in a <div class="api-entry"> card.
//
// Two guards keep prose out of the cards: the heading's text must look
// like an identifier (snake_case, camelCase, PascalCase, dotted.method)
// rather than natural language, and the code block must be short -- at
// most three lines -- which is what tells a signature from example code.
func wrapAPIEntries(html string) string {
	var result strings.Builder
	lastEnd := 0
	for _, m := range apiEntryRE.FindAllStringSubmatchIndex(html, -1) {
		headingHTML := html[m[2]:m[3]]
		codeBlock := html[m[4]:m[5]]

		innerStart := strings.Index(headingHTML, ">") + 1
		innerEnd := strings.LastIndex(headingHTML, "<")
		innerHTML := headingHTML[innerStart:innerEnd]
		plainText := util.PythonStrip(strings.TrimLeft(tagRE.ReplaceAllString(innerHTML, ""), "#"))

		if !identifierRE.MatchString(plainText) {
			continue
		}
		if strings.Count(codeBlock, "\n") > 2 {
			continue
		}
		result.WriteString(html[lastEnd:m[0]])
		result.WriteString(`<div class="api-entry">` +
			headingHTML + "\n" + codeBlock + "\n" + html[m[6]:m[7]] +
			`</div>`)
		lastEnd = m[1]
	}
	result.WriteString(html[lastEnd:])
	return result.String()
}

// splitTableCells splits a markdown table line on its unescaped pipes,
// turning each cell's "\|" back into a literal pipe.
func splitTableCells(line string) []string {
	stripped := util.PythonStrip(line)
	stripped = strings.TrimPrefix(stripped, "|")
	// A trailing "\|" is an escaped pipe, not the row's closing delimiter.
	if strings.HasSuffix(stripped, "|") && !strings.HasSuffix(stripped, "\\|") {
		stripped = stripped[:len(stripped)-1]
	}
	parts := splitUnescapedPipes(stripped)
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.ReplaceAll(util.PythonStrip(p), "\\|", "|")
	}
	return out
}

// splitUnescapedPipes splits s on every "|" that is not directly preceded
// by a backslash.
//
// This is the hand-written form of the `(?<!\\)\|` split. Like the
// lookbehind, it looks exactly one character back, so a doubled backslash
// before a pipe still suppresses the split -- the same answer the Python
// gave, deliberately preserved.
func splitUnescapedPipes(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '|' {
			continue
		}
		if i > 0 && s[i-1] == '\\' {
			continue
		}
		out = append(out, s[start:i])
		start = i + 1
	}
	return append(out, s[start:])
}

var separatorCellRE = regexp.MustCompile(`^:?-+:?$`)

// ParseTable parses markdown table lines into an HTML <table>.
//
// It expects lines like:
//
//	| Header1 | Header2 |
//	| ------- | ------- |
//	| Cell1   | Cell2   |
//
// The separator line -- the one whose cells hold only "|", "-", ":" and
// spaces -- separates the header from the body rows, and its alignment
// markers produce text-align styles on the cells below. An escaped pipe in
// a cell is a literal pipe character.
func ParseTable(tableLines []string) string {
	if len(tableLines) == 0 {
		return ""
	}
	rows := make([][]string, 0, len(tableLines))
	for _, line := range tableLines {
		rows = append(rows, splitTableCells(line))
	}

	// Detect the separator row: every cell is a run of dashes with
	// optional alignment colons, or empty.
	separatorIdx := -1
	for idx, row := range rows {
		all := true
		for _, cell := range row {
			if !separatorCellRE.MatchString(cell) && cell != "" {
				all = false
				break
			}
		}
		if all {
			separatorIdx = idx
			break
		}
	}

	var alignments []string
	if separatorIdx >= 0 {
		for _, cell := range rows[separatorIdx] {
			switch {
			case cell == "":
				alignments = append(alignments, "")
			case strings.HasPrefix(cell, ":") && strings.HasSuffix(cell, ":"):
				alignments = append(alignments, "center")
			case strings.HasPrefix(cell, ":"):
				alignments = append(alignments, "left")
			case strings.HasSuffix(cell, ":"):
				alignments = append(alignments, "right")
			default:
				alignments = append(alignments, "")
			}
		}
	}

	align := func(colIdx int) string {
		if colIdx < len(alignments) && alignments[colIdx] != "" {
			return ` style="text-align: ` + alignments[colIdx] + `"`
		}
		return ""
	}
	// aria-sort IS the sort indicator the framework paints, so the server
	// states the order it rendered -- unsorted -- and the sorting script
	// moves the attribute rather than a class.
	th := func(content string, colIdx int) string {
		return `<th class="sortable" role="columnheader" aria-sort="none"` +
			align(colIdx) + ">" + inlineFormat(content) + "</th>"
	}
	td := func(content string, colIdx int) string {
		return `<td role="gridcell"` + align(colIdx) + ">" +
			inlineFormat(content) + "</td>"
	}
	rowHTML := func(row []string, cell func(string, int) string) string {
		var b strings.Builder
		b.WriteString(`<tr role="row">`)
		for i, c := range row {
			b.WriteString(cell(c, i))
		}
		b.WriteString("</tr>\n")
		return b.String()
	}

	// The framework's static table: table.data.tm-table[role=grid], with a
	// <tfoot> that is always present (it holds a row-cap note when one is
	// in force, and is empty otherwise).
	var b strings.Builder
	b.WriteString(`<table class="data tm-table" role="grid">` + "\n")
	if separatorIdx > 0 {
		b.WriteString("<thead>\n")
		for _, row := range rows[:separatorIdx] {
			b.WriteString(rowHTML(row, th))
		}
		b.WriteString("</thead>\n")
		b.WriteString("<tbody>\n")
		for _, row := range rows[separatorIdx+1:] {
			b.WriteString(rowHTML(row, td))
		}
		b.WriteString("</tbody>\n")
	} else {
		// No separator: every row is a body row.
		b.WriteString("<tbody>\n")
		for _, row := range rows {
			b.WriteString(rowHTML(row, td))
		}
		b.WriteString("</tbody>\n")
	}
	b.WriteString("<tfoot></tfoot>\n")
	b.WriteString("</table>")
	return b.String()
}

var admonitionMarkerRE = regexp.MustCompile(`^\[!([\p{L}\p{N}_]+)\]` + pySpaceClass + `*$`)

// parseBlockquote parses blockquote lines, detecting admonitions.
//
// When the first line names a recognized admonition type -- "[!NOTE]",
// "[!WARNING]" -- the quote renders as a styled callout. Otherwise it
// renders as a plain blockquote.
func parseBlockquote(bqLines []string) string {
	if len(bqLines) == 0 {
		return ""
	}
	if m := admonitionMarkerRE.FindStringSubmatch(util.PythonStrip(bqLines[0])); m != nil {
		admonitionType := strings.ToUpper(m[1])
		if callout, ok := calloutKinds[admonitionType]; ok {
			bodyLines := bqLines[1:]
			for len(bodyLines) > 0 && util.PythonStrip(bodyLines[0]) == "" {
				bodyLines = bodyLines[1:]
			}
			var bodyParts []string
			for _, para := range strings.Split(strings.Join(bodyLines, "\n"), "\n\n") {
				para = util.PythonStrip(para)
				if para != "" {
					bodyParts = append(bodyParts, "<p>"+inlineFormat(para)+"</p>")
				}
			}
			bodyHTML := strings.Join(bodyParts, "\n")
			title := pyCapitalize(admonitionType)
			return `<div class="tm-callout tm-callout-` + callout.Kind +
				`" role="` + callout.Role + `">` + "\n" +
				`<div class="tm-callout-title">` + callout.Icon + title + "</div>\n" +
				`<div class="tm-callout-body">` + bodyHTML + "</div>\n" +
				`</div>`
		}
	}

	var kept []string
	for _, line := range bqLines {
		if util.PythonStrip(line) != "" {
			kept = append(kept, line)
		}
	}
	return "<blockquote><p>" + inlineFormat(strings.Join(kept, " ")) + "</p></blockquote>"
}

// renderDiffLines renders code lines with diff-style highlighting.
//
// Every line is a .tm-code-line wrapper; an added line also carries
// .tm-code-add and a removed one .tm-code-del. The "+" or "-" marker
// itself is NOT emitted: the framework draws it as generated content in a
// gutter, so a copied selection is the code without the diff column.
// Emitting the character as well would print it twice.
func renderDiffLines(codeLines []string) string {
	parts := make([]string, 0, len(codeLines))
	for _, line := range codeLines {
		switch {
		case strings.HasPrefix(line, "+"):
			parts = append(parts,
				`<span class="tm-code-line tm-code-add">`+EscapeHTML(line[1:])+"</span>")
		case strings.HasPrefix(line, "-"):
			parts = append(parts,
				`<span class="tm-code-line tm-code-del">`+EscapeHTML(line[1:])+"</span>")
		default:
			parts = append(parts,
				`<span class="tm-code-line">`+EscapeHTML(line)+"</span>")
		}
	}
	return strings.Join(parts, "\n")
}

// codeSpanContent returns the text of a code span, with CommonMark's
// edge-space rule applied.
//
// A span that both begins and ends with a space drops one from each end,
// unless it is spaces all the way through. That rule is what lets a span
// hold a literal backtick at an edge: the three-character span "“ ` “"
// is one tick, not a tick with spaces around it.
func codeSpanContent(raw string) string {
	if len(raw) >= 2 && raw[0] == ' ' && raw[len(raw)-1] == ' ' && util.PythonStrip(raw) != "" {
		return raw[1 : len(raw)-1]
	}
	return raw
}

// inlineFormat applies the inline rules -- links, bold, italic, inline
// code -- to one run of Markdown text.
//
// Code spans are found first and left untouched by everything else. They
// are located by [directives.FindBacktickSpans], the same scanner the
// directive parser and the spell mask use, so a span delimited by a run of
// backticks -- "“x“", the form a writer needs when the code itself
// contains a backtick -- is one span here exactly as it is there.
func inlineFormat(text string) string {
	var parts strings.Builder
	pos := 0
	for _, span := range directives.FindBacktickSpans(text) {
		parts.WriteString(inlineFormatProse(text[pos:span.Start]))
		content := EscapeHTML(codeSpanContent(text[span.ContentStart:span.ContentEnd]))
		parts.WriteString("<code>" + content + "</code>")
		pos = span.End
	}
	parts.WriteString(inlineFormatProse(text[pos:]))
	return parts.String()
}

var (
	imageRE  = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	linkRE   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	statRE   = regexp.MustCompile(`==([^=]+?)==`)
	boldRE   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRE = regexp.MustCompile(`\*(.+?)\*`)
)

// inlineFormatProse applies every inline rule except code spans to one run
// of prose.
func inlineFormatProse(seg string) string {
	// Images, before links, because a link pattern also matches an image's
	// bracket-and-parenthesis tail.
	formatted := imageRE.ReplaceAllString(seg, `<img src="${2}" alt="${1}" loading="lazy">`)
	formatted = linkRE.ReplaceAllString(formatted, `<a href="${2}">${1}</a>`)
	// Inline stat markup: ==value== becomes a <data> element.
	formatted = statRE.ReplaceAllString(formatted, `<data value="${1}">${1}</data>`)
	formatted = boldRE.ReplaceAllString(formatted, `<strong>${1}</strong>`)
	formatted = italicRE.ReplaceAllString(formatted, `<em>${1}</em>`)
	return formatted
}
