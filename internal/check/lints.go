package check

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/spelling"
	"github.com/stricttools/selfdoc/internal/tokenizer"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// directiveMarkers are the six marker spellings a directive can open with. A
// fenced block carrying one is a syntax example rather than a program, so the
// syntax tier leaves it alone.
var directiveMarkers = []string{":-:", ":<:", ":>:", ":@:", ":=:", ":::"}

// meaninglessAlt is the alt text SEO014 refuses outright: a word that names
// the medium rather than the content.
var meaninglessAlt = map[string]bool{
	"image": true, "screenshot": true, "photo": true, "picture": true,
	"img": true, "pic": true, "figure": true, "graphic": true,
}

// filenameExts matches alt text that is really a filename.
var filenameExts = regexp.MustCompile(`(?i)\.(png|jpg|jpeg|gif|svg|webp)$`)

// genericAnchors is the link text SEO015 refuses: text that describes the act
// of clicking rather than the destination.
var genericAnchors = map[string]bool{
	"click here": true, "here": true, "this link": true, "this page": true,
	"link": true, "read more": true, "more": true, "learn more": true,
}

// dqSuffixes are the kind words DQ001 strips before comparing a description
// against a page or symbol name, so "Config module" and "config" compare
// equal.
var dqSuffixes = map[string]bool{
	"module": true, "class": true, "function": true, "package": true,
	"type": true, "interface": true, "method": true,
}

// dqNonAlphanumeric matches every character DQ001's normalization drops.
var dqNonAlphanumeric = regexp.MustCompile(`[^a-z0-9\s]`)

// emptyAltPattern matches an image with no alt text at all.
const emptyAltMarker = "![]("

// altTextPattern captures the alt text of every image reference on a line.
var altTextPattern = regexp.MustCompile(`!\[([^\]]*)\]\(`)

// anchorTextPattern captures the link text of every link on a line.
var anchorTextPattern = regexp.MustCompile(`\[([^\]]+)\]\(`)

// markdownLinkPattern captures the text and target of every Markdown link.
var markdownLinkPattern = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

// refDirectivePattern matches a ref directive marker in a raw page body, which
// is what makes a page an API reference page as far as DQ003 is concerned.
var refDirectivePattern = regexp.MustCompile(`:-:` + util.PythonSpaceClass + `*ref` + util.PythonSpaceClass)

// minHelpLength is the shortest help text CLI002 accepts.
const minHelpLength = 50

// descriptionFloor and descriptionCeiling bound the meta description a page
// publishes: SEO009 reports one below the floor, SEO010 one above the
// ceiling. The ceiling is what a search result renders before it cuts the
// description off; nothing in selfdoc cuts it, so a longer description
// reaches the page whole and the engine decides where it ends.
const (
	descriptionFloor   = 110
	descriptionCeiling = 160
)

// normalizeDQ reduces a description or a title to the words DQ001 compares:
// lowercased, punctuation dropped, separators turned into spaces and the kind
// words removed.
func normalizeDQ(text string) string {
	normalized := strings.ToLower(util.PythonStrip(text))
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = dqNonAlphanumeric.ReplaceAllString(normalized, "")
	words := util.PythonFields(normalized)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if !dqSuffixes[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

// isIndexTemplate reports whether a docs template is the one the build renders
// as the project's index page: "index.md" at the root of what a locale serves.
//
// A project declaring a locale keeps its templates under that locale's own
// segment, so the segment is stripped before the name is read. A page named
// "index.md" anywhere deeper is an ordinary page: the build gives it its own
// directory rather than the mount root.
func isIndexTemplate(relPath, localePrefix string) bool {
	if localePrefix != "" {
		relPath = strings.TrimPrefix(relPath, localePrefix+"/")
	}
	return relPath == "index.md"
}

// runLints runs every page-level lint rule over a set of documentation
// templates and returns the diagnostics in rule order.
//
// allDocs maps each page's reporting path to its parsed frontmatter, its
// resolved content and its raw body -- the docs walk's own result, with the
// project's published posts merged in by the caller. projectRoot is the
// directory every reported path is relative to and every relative config path
// resolves against; docsDir is the tree the pages were walked from, which sits
// inside it. resolvedDirectives are the successfully resolved directives, nil
// for a caller that resolved none.
//
// The Python signature carried the resolver too and never read it; it is left
// out here rather than accepted and ignored.
func runLints(
	allDocs map[string]docs.Doc,
	projectRoot string,
	docsDir string,
	config map[string]any,
	resolvedDirectives []ResolvedDirective,
	vocab *vocabulary.Vocabulary,
	handle *effects.Handle,
) ([]lints.LintResult, error) {
	var results []lints.LintResult

	// Per-page directive lookup for the rules that read what a page's
	// directives resolved to.
	pageDirectives := map[string][]ResolvedDirective{}
	for _, resolved := range resolvedDirectives {
		pageDirectives[resolved.File] = append(pageDirectives[resolved.File], resolved)
	}

	projectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	projectName := filepath.Base(projectRoot)
	// The locale segment a page's template path carries, which is what tells
	// the project's index template apart from a page named "index.md" inside
	// a subdirectory.
	localePrefix := localePrefixOf(config)

	// EXAMPLE002/EXAMPLE003 -- validator command templates keyed by fenced
	// language. An absent config means the feature is off, which turns
	// every "validate" marker in the tree into an EXAMPLE003.
	exampleCommands := configDict(config, "examples")

	knownPages := map[string]bool{}
	for relPath := range allDocs {
		knownPages[relPath] = true
	}

	// SPELL001 and VOCAB004 -- the vocabulary is the caller's, loaded once
	// for the whole run, so a malformed list stops the run before any page
	// is judged rather than reporting what a fixed list would have accepted.
	spellVocab := spelling.LoadWordlist()
	spellAccepted := vocab.SpellVocab()
	rejectedMatchers := make([]vocabulary.Matcher, 0, len(vocab.AllRejected()))
	for _, rejected := range vocab.AllRejected() {
		rejectedMatchers = append(rejectedMatchers, vocabulary.NewMatcher(rejected))
	}
	// The authored documents a directive can render prose out of, walked
	// once for the whole run rather than once per page.
	spellDocuments := authoredDataDocuments(
		docsDir,
		util.PathJoin(projectRoot, configString(config, "output", layout.OutputDefault)),
	)

	for _, relPath := range sortedKeys(allDocs) {
		doc := allDocs[relPath]
		metadata := doc.Frontmatter
		bodyContent := doc.Raw
		fmOffset := doc.FrontmatterLines
		tokens := tokenizer.Tokenize(bodyContent)

		var headingTokens []tokenizer.Heading
		var h1Tokens []tokenizer.Heading
		for _, token := range tokens {
			heading, isHeading := token.(tokenizer.Heading)
			if !isHeading {
				continue
			}
			headingTokens = append(headingTokens, heading)
			if heading.Level == 1 {
				h1Tokens = append(h1Tokens, heading)
			}
		}

		// SEO001 -- multiple H1 headings in the Markdown source.
		// SEO013 -- no title source at all.
		if len(h1Tokens) > 1 {
			results = append(results, lints.MustLintResult(
				relPath, nil, "SEO001",
				fmt.Sprintf(
					"Multiple H1 headings (%d found); use a single '# ' heading per page",
					len(h1Tokens),
				),
			))
		}
		_, hasFrontmatterTitle := frontmatterString(metadata, "title")
		if len(h1Tokens) == 0 && !hasFrontmatterTitle {
			results = append(results, lints.MustLintResult(
				relPath, nil, "SEO013",
				"No title source: add a '# Heading' or set 'title:' in frontmatter",
			))
		}

		// SEO002 -- heading level gaps.
		prevLevel := 0
		for _, heading := range headingTokens {
			level := heading.Level
			if prevLevel > 0 && level > prevLevel+1 {
				results = append(results, lints.MustLintResult(
					relPath, lineOf(heading.Start()+fmOffset), "SEO002",
					fmt.Sprintf(
						"Heading level jumps from H%d to H%d (skips H%d)",
						prevLevel, level, prevLevel+1,
					),
				))
			}
			prevLevel = level
		}

		// SEO003 -- empty alt text, in text-bearing tokens only.
		for _, token := range tokens {
			if !tokenizer.IsTextBearing(token) {
				continue
			}
			for offset, line := range tokenizer.TokenTextLines(token) {
				if strings.Contains(line, emptyAltMarker) {
					results = append(results, lints.MustLintResult(
						relPath, lineOf(token.Start()+offset+fmOffset), "SEO003",
						"Image with empty alt text",
					))
				}
			}
		}

		// SEO004 -- the document title this page renders is too long.
		//
		// What renders is not the page's own title: the wrapper composes it
		// out of the page title and the names of what publishes the page.
		// The rule therefore measures what [page.DocumentTitle] produces, so
		// no page is reported for a length it never carries. It measures the
		// standalone composition, which is what a checkout can answer: the
		// name of an assembled site is the assembly's fact, recorded in the
		// home project's manifest, and no project's own tree holds it.
		pageTitle := ""
		titleValue, titlePresent := metadata["title"]
		switch {
		case titlePresent && titleValue != nil:
			pageTitle = util.PythonStr(titleValue)
		case len(h1Tokens) > 0:
			pageTitle = h1Tokens[0].Text
		}
		if pageTitle != "" {
			rendered := page.DocumentTitle(page.DocumentTitleParts{
				PageTitle:   pageTitle,
				ProjectName: projectName,
				IsIndexPage: isIndexTemplate(relPath, localePrefix),
			})
			if runeLen(rendered) > page.DocumentTitleLimit {
				results = append(results, lints.MustLintResult(
					relPath, nil, "SEO004",
					fmt.Sprintf(
						`Title too long for SEO (%d chars): "%s"`,
						runeLen(rendered), rendered,
					),
				))
			}
		}

		// SEO006 -- missing description.
		if _, present := metadata["description"]; !present {
			results = append(results, lints.MustLintResult(
				relPath, nil, "SEO006", "No 'description' in frontmatter",
			))
		}

		// SEO009 -- description too short.
		// SEO010 -- frontmatter description too long.
		var effectiveDesc string
		descriptionValue, descriptionPresent := metadata["description"]
		if descriptionPresent && descriptionValue != nil {
			if text, isString := descriptionValue.(string); isString && runeLen(text) > descriptionCeiling {
				results = append(results, lints.MustLintResult(
					relPath, nil, "SEO010",
					fmt.Sprintf(
						"Frontmatter description is %d chars (max %d)",
						runeLen(text), descriptionCeiling,
					),
				))
			}
			effectiveDesc = util.PythonStr(descriptionValue)
		} else {
			// Auto-extracted from the first paragraph, skipping the
			// leading heading, blank-line and code-block tokens. The
			// effective description is the complete first sentence
			// of the whole paragraph -- the same unit the build
			// emits into the meta tag -- not just the first physical
			// line. No character cap here; SEO009 and SEO010 stay
			// advisory.
			for _, token := range tokens {
				switch shaped := token.(type) {
				case tokenizer.Heading, tokenizer.BlankLine, tokenizer.CodeBlock:
					continue
				case tokenizer.Paragraph:
					effectiveDesc = prose.FirstSentence(
						strings.Join(shaped.Lines, "\n"),
					)
				}
				break
			}
		}

		// SEO009 fires only when there IS a description to measure.
		// With no frontmatter description and no paragraph found the
		// effective description is empty, which SEO006 already covers.
		if effectiveDesc != "" && runeLen(effectiveDesc) < descriptionFloor {
			results = append(results, lints.MustLintResult(
				relPath, nil, "SEO009",
				fmt.Sprintf(
					"Effective description is only %d chars (aim for %d-%d)",
					runeLen(effectiveDesc), descriptionFloor, descriptionCeiling,
				),
			))
		}

		// SEO007 -- paragraph length after a heading.
		//
		// One threshold set applies to every page type: a generated
		// page's lead-in is held to the same band as a hand-written
		// one. The only suppressions are structural -- a directive
		// supplies the content the paragraph would otherwise carry.
		for index, token := range tokens {
			heading, isHeading := token.(tokenizer.Heading)
			if !isHeading || (heading.Level != 2 && heading.Level != 3) {
				continue
			}
			headingText := util.PythonStrip(heading.Text)
			nextIndex := -1
			for scan := index + 1; scan < len(tokens); scan++ {
				if _, isBlank := tokens[scan].(tokenizer.BlankLine); isBlank {
					continue
				}
				nextIndex = scan
				break
			}
			if nextIndex < 0 {
				continue
			}
			// A heading followed directly by a directive: the
			// directive is the content.
			if _, isDirective := tokens[nextIndex].(tokenizer.Directive); isDirective {
				continue
			}
			paragraph, isParagraph := tokens[nextIndex].(tokenizer.Paragraph)
			if !isParagraph {
				continue
			}
			joined := make([]string, 0, len(paragraph.Lines))
			for _, line := range paragraph.Lines {
				joined = append(joined, util.PythonStrip(line))
			}
			wordCount := len(util.PythonFields(strings.Join(joined, " ")))
			if wordCount >= 30 && wordCount <= 80 {
				continue
			}
			// A directive after the short paragraph (blank lines
			// allowed between) expands into content, so suppress.
			hasDirectiveAfter := false
			for scan := nextIndex + 1; scan < len(tokens); scan++ {
				if _, isBlank := tokens[scan].(tokenizer.BlankLine); isBlank {
					continue
				}
				if _, isDirective := tokens[scan].(tokenizer.Directive); isDirective {
					hasDirectiveAfter = true
				}
				break
			}
			if hasDirectiveAfter {
				continue
			}
			results = append(results, lints.MustLintResult(
				relPath, lineOf(heading.Start()+fmOffset), "SEO007",
				fmt.Sprintf(
					"First paragraph after '%s' is %d words (aim for 30-80 for AI citation)",
					headingText, wordCount,
				),
			))
		}

		// SEO008 -- statistics density, over prose content tokens only.
		var proseWords []string
		for _, token := range tokens {
			switch shaped := token.(type) {
			case tokenizer.Paragraph:
				for _, line := range shaped.Lines {
					proseWords = append(proseWords, util.PythonFields(line)...)
				}
			case tokenizer.UnorderedList:
				for _, item := range shaped.Items {
					proseWords = append(proseWords, util.PythonFields(item)...)
				}
			case tokenizer.OrderedList:
				for _, item := range shaped.Items {
					proseWords = append(proseWords, util.PythonFields(item)...)
				}
			case tokenizer.Blockquote:
				for _, line := range shaped.Lines {
					proseWords = append(proseWords, util.PythonFields(line)...)
				}
			case tokenizer.DefinitionList:
				for _, entry := range shaped.Entries {
					proseWords = append(proseWords, util.PythonFields(entry.Term)...)
					for _, definition := range entry.Definitions {
						proseWords = append(proseWords, util.PythonFields(definition)...)
					}
				}
			}
		}
		totalWords := len(proseWords)
		if totalWords >= 200 {
			numericCount := 0
			for _, word := range proseWords {
				if CountsAsStatistic(word) {
					numericCount++
				}
			}
			expected := totalWords / 200
			if expected < 1 {
				expected = 1
			}
			if numericCount < expected {
				results = append(results, lints.MustLintResult(
					relPath, nil, "SEO008",
					fmt.Sprintf(
						"Page has %d words but only %d numeric data points "+
							"(recommend at least %d for AI citation)",
						totalWords, numericCount, expected,
					),
				))
			}
		}

		// SEO011 -- an empty heading section: a heading followed by a
		// same-or-higher-level heading with no content between them.
		lastHeadingLine, lastHeadingLevel := 0, 0
		haveLastHeading := false
		for _, token := range tokens {
			heading, isHeading := token.(tokenizer.Heading)
			if isHeading && (heading.Level == 2 || heading.Level == 3) {
				if haveLastHeading && heading.Level <= lastHeadingLevel {
					results = append(results, lints.MustLintResult(
						relPath, lineOf(lastHeadingLine+fmOffset), "SEO011",
						fmt.Sprintf(
							"H%d heading has no content before next H%d heading",
							lastHeadingLevel, heading.Level,
						),
					))
				}
				lastHeadingLine, lastHeadingLevel = heading.Start(), heading.Level
				haveLastHeading = true
				continue
			}
			if isHeading {
				continue
			}
			if _, isBlank := token.(tokenizer.BlankLine); isBlank {
				continue
			}
			haveLastHeading = false
		}

		// SEO014 -- meaningless alt text, in text-bearing tokens only.
		for _, token := range tokens {
			if !tokenizer.IsTextBearing(token) {
				continue
			}
			for offset, line := range tokenizer.TokenTextLines(token) {
				for _, match := range altTextPattern.FindAllStringSubmatch(line, -1) {
					alt := match[1]
					if alt == "" {
						continue // an empty alt is SEO003
					}
					lowered := strings.ToLower(alt)
					meaningless := meaninglessAlt[lowered] ||
						runeLen(alt) == 1 ||
						filenameExts.MatchString(lowered)
					if meaningless {
						results = append(results, lints.MustLintResult(
							relPath, lineOf(token.Start()+offset+fmOffset), "SEO014",
							fmt.Sprintf(
								"Meaningless alt text '%s'; use a descriptive alternative",
								alt,
							),
						))
					}
				}
			}
		}

		// SEO015 -- generic anchor text, in text-bearing tokens only.
		for _, token := range tokens {
			if !tokenizer.IsTextBearing(token) {
				continue
			}
			for offset, line := range tokenizer.TokenTextLines(token) {
				for _, match := range anchorTextPattern.FindAllStringSubmatch(line, -1) {
					text := util.PythonStrip(match[1])
					if genericAnchors[strings.ToLower(text)] {
						results = append(results, lints.MustLintResult(
							relPath, lineOf(token.Start()+offset+fmOffset), "SEO015",
							fmt.Sprintf(
								"Generic anchor text '%s'; use descriptive link text",
								text,
							),
						))
					}
				}
			}
		}

		// XREF001 -- a Markdown link to a .md page the docs tree does
		// not carry.
		for _, token := range tokens {
			if !tokenizer.IsTextBearing(token) {
				continue
			}
			for offset, line := range tokenizer.TokenTextLines(token) {
				for _, match := range markdownLinkPattern.FindAllStringSubmatch(line, -1) {
					target := match[2]
					if strings.HasPrefix(target, "http://") ||
						strings.HasPrefix(target, "https://") ||
						strings.HasPrefix(target, "#") ||
						strings.HasPrefix(target, "mailto:") {
						continue
					}
					target = strings.SplitN(target, "#", 2)[0]
					if !strings.HasSuffix(target, ".md") {
						continue
					}
					if strings.HasPrefix(target, "/") {
						target = strings.TrimLeft(target, "/")
					} else {
						target = path.Join(path.Dir(relPath), target)
					}
					target = strings.ReplaceAll(target, `\`, "/")
					if !knownPages[target] {
						results = append(results, lints.MustLintResult(
							relPath, lineOf(token.Start()+offset+fmOffset), "XREF001",
							fmt.Sprintf("link to '%s' resolves to unknown page", target),
						))
					}
				}
			}
		}

		// DQ001 -- the description restates the symbol or page name.
		descText := util.PythonStrOrEmpty(metadata["description"])
		if descText != "" {
			pageTitle := util.PythonStrOrEmpty(metadata["title"])
			if pageTitle == "" && len(h1Tokens) > 0 {
				pageTitle = h1Tokens[0].Text
			}
			if pageTitle == "" {
				stem := pythonSplitExt(path.Base(relPath))
				stem = strings.ReplaceAll(stem, "_", " ")
				pageTitle = strings.ReplaceAll(stem, "-", " ")
			}
			normDesc := normalizeDQ(descText)
			normTitle := normalizeDQ(pageTitle)
			if normDesc != "" && normTitle != "" {
				restated := normDesc == normTitle
				// The substring check only applies when the
				// shorter side is at least half the longer, so
				// a two-word title inside a twenty-word
				// description is not a restatement.
				if !restated {
					shorter, longer := normDesc, normDesc
					if runeLen(normDesc) > runeLen(normTitle) {
						shorter = normTitle
					}
					if runeLen(normDesc) < runeLen(normTitle) {
						longer = normTitle
					}
					if len(util.PythonFields(shorter)) >= 2 &&
						float64(runeLen(shorter)) >= float64(runeLen(longer))*0.5 {
						restated = strings.Contains(normTitle, normDesc) ||
							strings.Contains(normDesc, normTitle)
					}
				}
				if !restated {
					descWords := wordSet(normDesc)
					titleWords := wordSet(normTitle)
					if len(descWords) > 0 && len(titleWords) > 0 {
						overlap := 0
						for word := range descWords {
							if titleWords[word] {
								overlap++
							}
						}
						maxLen := len(descWords)
						if len(titleWords) > maxLen {
							maxLen = len(titleWords)
						}
						if float64(overlap)/float64(maxLen) > 0.8 {
							restated = true
						}
					}
				}
				if restated {
					results = append(results, lints.MustLintResult(
						relPath, nil, "DQ001",
						"description restates the symbol name",
					))
				}
			}
		}

		// DQ002 -- description too short.
		if descriptionPresent && descriptionValue != nil {
			length := runeLen(util.PythonStr(descriptionValue))
			if length < 20 {
				results = append(results, lints.MustLintResult(
					relPath, nil, "DQ002",
					fmt.Sprintf(
						"description too short (%d chars, minimum 20)", length,
					),
				))
			}
			// DQ003 -- a page that references functions needs a
			// substantive description.
			if refDirectivePattern.MatchString(bodyContent) && length < 30 {
				results = append(results, lints.MustLintResult(
					relPath, nil, "DQ003",
					fmt.Sprintf(
						"page with ref directive has short description "+
							"(%d chars, minimum 30 for API reference pages)",
						length,
					),
				))
			}
		}

		// EXAMPLE001 -- code block syntax validation.
		// EXAMPLE002/EXAMPLE003 -- opt-in semantic validation.
		for _, token := range tokens {
			block, isCodeBlock := token.(tokenizer.CodeBlock)
			if !isCodeBlock {
				continue
			}

			// The semantic tier is opted into per block, never
			// inferred. A block the tier owns is not also parsed
			// below -- the validator's own diagnostic supersedes a
			// second-hand syntax message. A marker with no
			// configured command owns nothing, so EXAMPLE003 is
			// raised and the block falls through to the syntax
			// tier.
			if block.Validate {
				commandTemplate, configured := exampleCommands[block.Lang]
				if configured && commandTemplate != nil {
					template, isString := commandTemplate.(string)
					if !isString {
						return nil, fmt.Errorf(
							"the \"examples\" entry for %q declares %s where a "+
								"command string belongs",
							block.Lang, util.PythonTypeName(commandTemplate),
						)
					}
					lint, err := validateExampleBlock(
						block, relPath, template, projectRoot, handle,
					)
					if err != nil {
						return nil, err
					}
					if lint != nil {
						results = append(results, *lint)
					}
					continue
				}
				results = append(results, lints.MustLintResult(
					relPath, lineOf(block.Start()), "EXAMPLE003",
					fmt.Sprintf(
						"code block marked 'validate' but no validator is "+
							"configured for language '%s': add "+
							`"examples": {"%s": "<command> {file}"}`+
							" to selfdoc.json, or drop the marker",
						block.Lang, block.Lang,
					),
				))
			}

			switch block.Lang {
			case "python", "py", "python3":
				if len(block.Lines) < 3 || carriesDirectiveMarker(block.Lines) {
					continue
				}
				verdict, err := checkPythonSyntax(
					strings.Join(block.Lines, "\n"), handle,
				)
				if err != nil {
					return nil, err
				}
				if verdict.Status != "syntax" {
					continue
				}
				results = append(results, lints.MustLintResult(
					relPath, lineOf(block.Start()+verdict.Line), "EXAMPLE001",
					"Python syntax error in code block: "+verdict.Message,
				))
			case "json":
				if len(block.Lines) < 1 || carriesDirectiveMarker(block.Lines) {
					continue
				}
				document := strings.Join(block.Lines, "\n")
				if failure := CheckJSONSyntax(document); failure != nil {
					results = append(results, lints.MustLintResult(
						relPath,
						lineOf(block.Start()+failure.Line([]rune(document))),
						"EXAMPLE001",
						"JSON syntax error in code block: "+failure.Message,
					))
				}
			}
		}

		// SPELL001 -- prose spelling. One engine, shared with the
		// corpus-wide sweep: this surface only turns its findings into
		// diagnostics. Posts are in allDocs by the time the rules run,
		// so they are checked on the same terms as documentation pages.
		rawMisspellings := spelling.CheckText(
			bodyContent, relPath, spellVocab, spellAccepted, fmOffset, true,
		)
		for _, miss := range rawMisspellings {
			results = append(results, lints.MustLintResult(
				relPath, lineOf(miss.Line), "SPELL001",
				miss.Describe()+"."+spellRemedy(miss.Word, vocab),
			))
		}

		// VOCAB004 -- a rejected term in the page's prose, on the lines
		// the spell check reads.
		results = append(results, rejectedTermLints(
			relPath, bodyContent, fmOffset, rejectedMatchers,
		)...)

		// SPELL001 over what a directive rendered. The raw body carries
		// a marker where the reader sees text, so prose that came out
		// of a data file -- a CV declared in TOML, a curated listing's
		// blurbs -- was never scanned at all and shipped its typos. The
		// resolved body is scanned too, and anything the raw scan
		// already reported is dropped so a page's own prose is never
		// reported twice.
		rendered, err := spellRenderedDirectives(
			relPath, bodyContent, doc.Resolved, rawMisspellings,
			pageDirectives[relPath], spellDocuments, projectRoot,
			spellVocab, spellAccepted, vocab,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, rendered...)
	}

	// SEO012 -- WCAG contrast ratios.
	results = checkContrast(results, config, docsDir)

	// PARAM001 -- parameter documentation completeness.
	// RETURN001 -- return type documentation.
	baseDir := projectRoot
	for _, resolved := range resolvedDirectives {
		if resolved.Name != "ref" || resolved.SourceEntry == nil {
			continue
		}
		target := resolved.Attrs["target"]
		if target == "" {
			continue
		}
		resolvedPath := resolved.SourceEntry.Extractor.ResolvePath(
			resolved.Attrs["path"], []string{resolved.SourceEntry.Path}, baseDir,
		)
		if resolvedPath == "" {
			continue
		}
		details, err := resolved.SourceEntry.Extractor.SymbolDetails(resolvedPath, target)
		if err != nil {
			return nil, err
		}
		if details == nil {
			continue
		}
		for _, param := range details.Params {
			if !param.Documented {
				results = append(results, lints.MustLintResult(
					resolved.File, nil, "PARAM001",
					fmt.Sprintf("parameter '%s' not documented", param.Name),
				))
			}
		}
		if details.ReturnType != nil &&
			*details.ReturnType != "None" && *details.ReturnType != "NoneType" &&
			!details.ReturnDocumented {
			results = append(results, lints.MustLintResult(
				resolved.File, nil, "RETURN001",
				fmt.Sprintf("return type '%s' not documented", *details.ReturnType),
			))
		}
	}

	return results, nil
}

// carriesDirectiveMarker reports whether any of a fenced block's lines carries
// a directive marker, which makes the block a syntax example rather than a
// program.
func carriesDirectiveMarker(blockLines []string) bool {
	for _, line := range blockLines {
		for _, marker := range directiveMarkers {
			if strings.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}

// frontmatterString reads a frontmatter key as the truthiness test the rules
// apply to it: the rendered value, and whether it is non-empty.
func frontmatterString(frontmatter map[string]any, key string) (string, bool) {
	value := util.PythonStrOrEmpty(frontmatter[key])
	return value, value != ""
}

// wordSet is a whitespace-split string as a set, for the token-overlap
// measurement.
func wordSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range util.PythonFields(text) {
		set[word] = true
	}
	return set
}

// runeLen is a string's length in characters, which is what Python's len() of
// a str reports and what every length threshold in these rules was written
// against.
func runeLen(text string) int {
	return len([]rune(text))
}

// authoredDataDocuments returns every authored data document in the docs tree,
// as absolute paths.
//
// A page whose whole body is a directive -- the CV, the curated project
// listing -- keeps its prose in a TOML document beside the templates. Those
// documents are where a reader edits, so they are where a misspelling in the
// rendered page is reported. They are found by walking rather than asked of
// the directive, because a directive that reads a fixed document declares no
// attributes at all.
//
// The build output is skipped: it holds copies of the same documents, and
// reporting a word at its line in a generated copy would send a reader to a
// file the next build overwrites.
func authoredDataDocuments(docsDir, outputDir string) []string {
	skip, err := filepath.Abs(outputDir)
	if err != nil {
		return nil
	}
	var found []string
	_ = filepath.WalkDir(docsDir, func(walked string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		absoluteRoot, absErr := filepath.Abs(walked)
		if absErr != nil {
			return nil
		}
		if absoluteRoot == skip || strings.HasPrefix(absoluteRoot, skip+string(os.PathSeparator)) {
			return nil
		}
		names, readErr := os.ReadDir(walked)
		if readErr != nil {
			return nil
		}
		for _, name := range names {
			if name.IsDir() || !strings.HasSuffix(name.Name(), ".toml") {
				continue
			}
			found = append(found, filepath.Join(absoluteRoot, name.Name()))
		}
		return nil
	})
	return found
}

// pythonSplitExt returns a filename without its extension, the way Python's
// os.path.splitext does: the split is at the LAST dot that is not the first
// character, so ".hidden" has no extension and "a.b.md" keeps "a.b".
func pythonSplitExt(name string) string {
	index := strings.LastIndex(name, ".")
	if index <= 0 {
		return name
	}
	return name[:index]
}
