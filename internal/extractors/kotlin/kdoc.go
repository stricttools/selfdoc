package kotlin

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// The KDoc patterns: the links a doc comment writes, and the tags it carries.
var (
	// labeledLinkRe matches a KDoc link with a label: [label][ClassName].
	labeledLinkRe = regexp.MustCompile(`\[([^\]]+)\]\[([^\]]+)\]`)

	// simpleLinkRe matches a bare KDoc link: [ClassName].
	simpleLinkRe = regexp.MustCompile(`\[([^\]]+)\]`)

	// paramBracketRe and paramPlainRe match the two spellings of @param.
	paramBracketRe = regexp.MustCompile(`^@param` + spaceClass + `*\[(` + wordClass + `+)\]` + spaceClass + `*(.*)`)
	paramPlainRe   = regexp.MustCompile(`^@param` + spaceClass + `+(` + wordClass + `+)` + spaceClass + `*(.*)`)

	// The one-tag-per-pattern set the doc renderer dispatches on.
	returnTagRe      = regexp.MustCompile(`^@returns?` + spaceClass + `+(.*)`)
	throwsTagRe      = regexp.MustCompile(`^@(?:throws|exception)` + spaceClass + `+([^` + spaceChars + `]+)` + spaceClass + `*(.*)`)
	propertyTagRe    = regexp.MustCompile(`^@property` + spaceClass + `+(` + wordClass + `+)` + spaceClass + `*(.*)`)
	constructorTagRe = regexp.MustCompile(`^@constructor` + spaceClass + `+(.*)`)
	receiverTagRe    = regexp.MustCompile(`^@receiver` + spaceClass + `+(.*)`)
	sampleTagRe      = regexp.MustCompile(`^@sample` + spaceClass + `+([^` + spaceChars + `]+)`)
	seeTagRe         = regexp.MustCompile(`^@see` + spaceClass + `+([^` + spaceChars + `]+)`)
	authorTagRe      = regexp.MustCompile(`^@author` + spaceClass + `+(.*)`)
	sinceTagRe       = regexp.MustCompile(`^@since` + spaceClass + `+(.*)`)

	// anyReturnTagRe is the presence test the quality measurement asks: does
	// this doc comment say what the function returns.
	anyReturnTagRe = regexp.MustCompile(`@returns?` + spaceClass)

	// paramNameRe matches either spelling of @param, wherever it appears.
	paramNameRe = regexp.MustCompile(`@param` + spaceClass + `*\[(` + wordClass + `+)\]|@param` +
		spaceClass + `+(` + wordClass + `+)`)

	// propertyNameRe matches an @property tag, wherever it appears.
	propertyNameRe = regexp.MustCompile(`@property` + spaceClass + `+(` + wordClass + `+)`)

	// propertyHeaderRe matches an @property tag and the whitespace that
	// separates it from its description. The Python pattern read the
	// description with a lookahead for the next tag, which RE2 cannot express,
	// so kdocPropertyDocs finds that boundary by hand.
	propertyHeaderRe = regexp.MustCompile(`@property` + spaceClass + `+(` + wordClass +
		`+)` + spaceClass + `+`)
)

// extractKDocBlock reads the KDoc block above a declaration.
//
// It walks upward from the line before the declaration and does NOT cross a
// blank line: Kotlin does not associate a doc comment separated from its
// declaration, and neither does this. The comment markers are removed and the
// text is returned as the author wrote it, one line per line.
func extractKDocBlock(lines []string, declLineIdx int) string {
	if declLineIdx <= 0 {
		return ""
	}

	idx := declLineIdx - 1
	stripped := strip(lines[idx])
	if stripped == "" {
		return ""
	}

	// A KDoc block written on one line.
	if m := singleLineKDocRe.FindStringSubmatch(stripped); m != nil {
		return m[1]
	}

	// Anything else has to end a block comment to be one.
	if !strings.HasSuffix(stripped, "*/") {
		return ""
	}

	var docLines []string
	found := false
	for idx >= 0 {
		line := strip(lines[idx])
		if strings.HasPrefix(line, "/**") {
			// The block's first line: the /** marker is not content.
			text := line[3:]
			text = strings.TrimSuffix(text, "*/")
			text = strip(text)
			if text != "" {
				docLines = append(docLines, text)
			}
			found = true
			break
		}

		// A middle or last line: the leading * and the closing */ are not
		// content either.
		text := line
		text = strings.TrimSuffix(text, "*/")
		text = strip(text)
		if strings.HasPrefix(text, "*") {
			text = text[1:]
			text = strings.TrimPrefix(text, " ")
		}
		docLines = append(docLines, text)
		idx--
	}

	if !found {
		// The walk reached the top of the file without an opening marker, so
		// what it collected was not a KDoc block.
		return ""
	}

	for i, j := 0, len(docLines)-1; i < j; i, j = i+1, j-1 {
		docLines[i], docLines[j] = docLines[j], docLines[i]
	}
	return strings.Join(docLines, "\n")
}

// extractModuleDoc reads the module-level KDoc comment: the first block at the
// top of the file, before any package, import or declaration line.
func extractModuleDoc(source string) string {
	var docLines []string
	inKDoc := false

	for _, line := range strings.Split(source, "\n") {
		stripped := strip(line)

		if !inKDoc {
			// A blank line or a regular comment before the block is not it.
			if stripped == "" || strings.HasPrefix(stripped, "//") {
				continue
			}

			if m := singleLineKDocRe.FindStringSubmatch(stripped); m != nil {
				return m[1]
			}

			if strings.HasPrefix(stripped, "/**") {
				inKDoc = true
				if text := strip(stripped[3:]); text != "" {
					docLines = append(docLines, text)
				}
				continue
			}

			// Anything else means the file opens with no module doc.
			return ""
		}

		if strings.HasSuffix(stripped, "*/") {
			text := stripped
			text = strings.TrimPrefix(text, "*")
			text = strip(dropLastTwo(text))
			if text != "" {
				docLines = append(docLines, text)
			}
			return strings.Join(docLines, "\n")
		}

		text := stripped
		if strings.HasPrefix(text, "*") {
			text = text[1:]
			text = strings.TrimPrefix(text, " ")
		}
		docLines = append(docLines, text)
	}

	return ""
}

// convertBracketLinks renders KDoc's square-bracket links as code spans:
// [ClassName] becomes a code span, and [label][ClassName] becomes the label
// with the symbol beside it.
func convertBracketLinks(text string) string {
	text = labeledLinkRe.ReplaceAllString(text, "${1} (`${2}`)")
	return simpleLinkRe.ReplaceAllString(text, "`${1}`")
}

// kdocParamNames lists the parameter names a doc comment documents, through
// either @param spelling or an @property tag.
func kdocParamNames(kdocText string) map[string]bool {
	names := map[string]bool{}
	for _, m := range paramNameRe.FindAllStringSubmatch(kdocText, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		names[name] = true
	}
	for _, m := range propertyNameRe.FindAllStringSubmatch(kdocText, -1) {
		names[m[1]] = true
	}
	return names
}

// hasReturnDoc reports whether a doc comment carries a @return or @returns tag.
func hasReturnDoc(kdocText string) bool {
	return anyReturnTagRe.MatchString(kdocText)
}

// kdocPropertyDocs reads the @property descriptions out of a doc comment,
// keyed by the property each names.
//
// A description runs from its tag to the next tag or the end of the comment,
// across as many lines as the author wrote it on, and its whitespace is
// collapsed so it reads as one table cell.
func kdocPropertyDocs(docText string) map[string]string {
	docs := map[string]string{}

	for offset := 0; offset < len(docText); {
		m := propertyHeaderRe.FindStringSubmatchIndex(docText[offset:])
		if m == nil {
			break
		}
		name := docText[offset+m[2] : offset+m[3]]
		descStart := offset + m[1]

		descEnd := nextTagIndex(docText, descStart)
		docs[name] = collapseWhitespace(strip(docText[descStart:descEnd]))
		offset = descEnd
	}

	return docs
}

// nextTagIndex is the index of the next KDoc tag at or after start, or the end
// of the text when there is none. A tag is an "@" followed by a word
// character, which is the boundary the Python lookahead tested for.
func nextTagIndex(text string, start int) int {
	for i := start; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		rest := text[i+1:]
		if rest == "" {
			continue
		}
		if isWordRune([]rune(rest)[0]) {
			return i
		}
	}
	return len(text)
}

// collapseWhitespace joins the words of s with single spaces, which is what
// Python's " ".join(s.split()) does.
func collapseWhitespace(s string) string {
	return strings.Join(util.PythonFields(s), " ")
}

// parseKDoc renders a KDoc comment as Markdown.
//
// Every tag Kotlin documents is recognized: @param in both spellings and
// @property accumulate into their own sections, @return, @throws, @exception,
// @constructor, @receiver, @sample, @see, @author and @since each render a bold
// label, @suppress renders nothing, and the square-bracket links become code
// spans wherever they appear.
//
// A tag's description continues onto the lines below it until a blank line or
// the next tag, which is how KDoc is written.
func parseKDoc(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	var out []string
	var params [][2]string
	var properties [][2]string
	i := 0

	// flush empties the accumulated sections, which every non-accumulating tag
	// and every ordinary line does before it emits.
	flush := func() {
		if len(params) > 0 {
			flushEntries(&out, params, "Parameters")
			params = nil
		}
		if len(properties) > 0 {
			flushEntries(&out, properties, "Properties")
			properties = nil
		}
	}

	// continuation reads the lines a tag's description continues onto.
	continuation := func(desc string) string {
		i++
		for i < len(lines) && strip(lines[i]) != "" && !strings.HasPrefix(strip(lines[i]), "@") {
			desc += " " + strip(lines[i])
			i++
		}
		return desc
	}

	for i < len(lines) {
		line := lines[i]
		stripped := strip(line)

		m := paramBracketRe.FindStringSubmatch(stripped)
		if m == nil {
			m = paramPlainRe.FindStringSubmatch(stripped)
		}
		if m != nil {
			desc := continuation(strip(m[2]))
			params = append(params, [2]string{m[1], convertBracketLinks(desc)})
			continue
		}

		if m := returnTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			desc := continuation(strip(m[1]))
			out = append(out, "**Returns:** "+convertBracketLinks(desc))
			continue
		}

		if m := throwsTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			excClass := m[1]
			desc := continuation(strip(m[2]))
			if desc != "" {
				out = append(out, "**Throws:** `"+excClass+"` "+convertBracketLinks(desc))
			} else {
				out = append(out, "**Throws:** `"+excClass+"`")
			}
			continue
		}

		if m := propertyTagRe.FindStringSubmatch(stripped); m != nil {
			desc := continuation(strip(m[2]))
			properties = append(properties, [2]string{m[1], convertBracketLinks(desc)})
			continue
		}

		if m := constructorTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			desc := continuation(strip(m[1]))
			out = append(out, "**Constructor:** "+convertBracketLinks(desc))
			continue
		}

		if m := receiverTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			desc := continuation(strip(m[1]))
			out = append(out, "**Receiver:** "+convertBracketLinks(desc))
			continue
		}

		if m := sampleTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			out = append(out, "**Sample:** `"+m[1]+"`")
			i++
			continue
		}

		if m := seeTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			out = append(out, "**See:** `"+m[1]+"`")
			i++
			continue
		}

		if m := authorTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			out = append(out, "**Author:** "+strip(m[1]))
			i++
			continue
		}

		if m := sinceTagRe.FindStringSubmatch(stripped); m != nil {
			flush()
			out = append(out, "**Since:** "+strip(m[1]))
			i++
			continue
		}

		// @suppress says nothing to a reader of the page.
		if strings.HasPrefix(stripped, "@suppress") {
			i++
			continue
		}

		flush()
		out = append(out, convertBracketLinks(line))
		i++
	}

	flush()

	return strings.Join(out, "\n")
}

// flushEntries emits an accumulated @param or @property section as a bold
// label and a bullet list.
func flushEntries(out *[]string, entries [][2]string, label string) {
	if len(*out) > 0 && (*out)[len(*out)-1] != "" {
		*out = append(*out, "")
	}
	*out = append(*out, "**"+label+":**", "")
	for _, entry := range entries {
		if entry[1] != "" {
			*out = append(*out, "- `"+entry[0]+"`: "+entry[1])
		} else {
			*out = append(*out, "- `"+entry[0]+"`")
		}
	}
	*out = append(*out, "")
}

// dropLastTwo removes the last two bytes of s, or all of it when it is
// shorter, reproducing Python's s[:-2] on a line whose closing marker is all
// it carries.
func dropLastTwo(s string) string {
	if len(s) < 2 {
		return ""
	}
	return s[:len(s)-2]
}
