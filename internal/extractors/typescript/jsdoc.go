package typescript

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// JSDoc is one JSDoc block parsed into its parts.
//
// It is exported because the Svelte extractor reads component documentation
// out of a <script> block with this same parser: a Svelte component's doc
// comment is a JSDoc block, and parsing it twice in two dialects would let the
// two drift.
type JSDoc struct {
	// Description is every line of the block that is not a tag, joined with
	// newlines, with leading and trailing blank lines dropped.
	Description string
	// Params are the block's @param tags, in order.
	Params []JSDocParam
	// Returns is the text of the @returns or @return tag. It is nil when the
	// block carries neither, and points at an empty string for a bare tag --
	// a distinction two callers act on differently, so it is not collapsed.
	Returns *string
	// Tags are the block's other tags, in order.
	Tags []JSDocTag
}

// JSDocParam is one @param tag: the parameter it names and what it says about
// it.
type JSDocParam struct {
	Name        string
	Description string
}

// JSDocTag is one tag that is neither @param nor @returns.
type JSDocTag struct {
	Tag         string
	Name        string
	Description string
}

var (
	// jsdocLinePrefix is the " * " a JSDoc block indents its lines with.
	jsdocLinePrefix = regexp.MustCompile(`^` + pySpace + `*\*` + pySpace + `?`)

	// jsdocTag matches a tag line: "@param ...", "@returns ...".
	jsdocTag = regexp.MustCompile(`^@(` + pyWord + `+)` + pySpace + `*(.*)`)

	// jsdocParamRest matches what follows "@param": an optional braced type,
	// then the parameter name, then its description.
	jsdocParamRest = regexp.MustCompile(
		`^(?:\{[^}]*\}` + pySpace + `+)?(` + pyWord + `+)` + pySpace + `*(.*)`)

	// jsdocReturnRest matches what follows "@returns": an optional braced
	// type, then the description.
	jsdocReturnRest = regexp.MustCompile(`^(?:\{[^}]*\}` + pySpace + `+)?(.*)`)

	// jsdocBlock matches a JSDoc block whose opener is followed by a newline.
	// A single-line /** ... */ is deliberately not matched: the module-doc
	// scan wants a block comment, and findJSDocBefore -- which does admit the
	// single-line form -- locates its blocks by scanning rather than by this
	// pattern.
	jsdocBlock = regexp.MustCompile(`(?s)/\*\*` + pySpace + `*\n(.*?)\*/`)

	// jsdocInline matches a JSDoc block's content with the surrounding
	// whitespace trimmed, for a block collected line by line.
	jsdocInline = regexp.MustCompile(`(?s)/\*\*` + pySpace + `*(.*?)` + pySpace + `*\*/`)

	// exportKeyword matches the "export" keyword at the start of the text
	// following a JSDoc block.
	exportKeyword = regexp.MustCompile(`^export` + pySpace)
)

// ParseJSDocText parses the raw content of a JSDoc block -- the text between
// "/**" and "*/" -- into its description, parameters, return text and other
// tags.
func ParseJSDocText(rawJSDoc string) JSDoc {
	lines := strings.Split(rawJSDoc, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, jsdocLinePrefix.ReplaceAllString(line, ""))
	}

	var descriptionParts []string
	var params []JSDocParam
	var returns *string
	var tags []JSDocTag

	for _, line := range cleaned {
		tagMatch := jsdocTag.FindStringSubmatch(line)
		if tagMatch == nil {
			descriptionParts = append(descriptionParts, line)
			continue
		}
		tagName := tagMatch[1]
		tagRest := pyStrip(tagMatch[2])

		switch tagName {
		case "param":
			if m := jsdocParamRest.FindStringSubmatch(tagRest); m != nil {
				params = append(params, JSDocParam{
					Name:        m[1],
					Description: pyStrip(m[2]),
				})
			}
		case "returns", "return":
			text := tagRest
			if m := jsdocReturnRest.FindStringSubmatch(tagRest); m != nil {
				text = pyStrip(m[1])
			}
			value := text
			returns = &value
		default:
			tags = append(tags, JSDocTag{Tag: tagName, Description: tagRest})
		}
	}

	for len(descriptionParts) > 0 && pyStrip(descriptionParts[len(descriptionParts)-1]) == "" {
		descriptionParts = descriptionParts[:len(descriptionParts)-1]
	}
	for len(descriptionParts) > 0 && pyStrip(descriptionParts[0]) == "" {
		descriptionParts = descriptionParts[1:]
	}

	return JSDoc{
		Description: strings.Join(descriptionParts, "\n"),
		Params:      params,
		Returns:     returns,
		Tags:        tags,
	}
}

// findJSDocBefore is the JSDoc block that ends right before pos, with only
// whitespace between the block and pos. It returns nil when there is none.
func findJSDocBefore(source string, pos int) *JSDoc {
	if pos < 0 || pos > len(source) {
		return nil
	}
	stripped := pyRStrip(source[:pos])
	if !strings.HasSuffix(stripped, "*/") {
		return nil
	}
	endIdx := strings.LastIndex(stripped, "*/")
	searchStart := strings.LastIndex(stripped[:endIdx], "/**")
	if searchStart == -1 {
		return nil
	}
	parsed := ParseJSDocText(stripped[searchStart+3 : endIdx])
	return &parsed
}

// formatJSDocAsMarkdown renders a parsed JSDoc block as the Markdown a
// reference page carries: the description, then the parameters as a list, then
// the return text.
//
// baseLevel is the level of the heading the text is emitted under; headings the
// block wrote are renested beneath it.
func formatJSDocAsMarkdown(jsdoc *JSDoc, baseLevel int) string {
	var parts []string
	if jsdoc.Description != "" {
		parts = append(parts, extractors.DemoteDocHeadings(jsdoc.Description, baseLevel))
	}

	if len(jsdoc.Params) > 0 {
		parts = append(parts, "", "**Parameters:**")
		for _, p := range jsdoc.Params {
			desc := ""
			if p.Description != "" {
				desc = " -- " + p.Description
			}
			parts = append(parts, "- `"+p.Name+"`"+desc)
		}
	}

	if jsdoc.Returns != nil && *jsdoc.Returns != "" {
		parts = append(parts, "", "**Returns:** "+*jsdoc.Returns)
	}

	return strings.Join(parts, "\n")
}

// extractModuleJSDoc is the module-level JSDoc block of a source file: the
// first block comment in the file, when nothing but whitespace precedes it and
// it is not attached to the declaration below it.
//
// Attachment is decided by what follows the block. An import means the block
// documents the module. An export means it depends: an @module tag claims the
// module, @param or @returns tags make it the declaration's documentation, and
// otherwise a blank line between the two separates them. Anything else is a
// standalone module doc.
func extractModuleJSDoc(source string) *JSDoc {
	match := jsdocBlock.FindStringSubmatchIndex(source)
	if match == nil {
		return nil
	}

	if pyStrip(source[:match[0]]) != "" {
		return nil
	}

	raw := source[match[2]:match[3]]
	afterPos := match[1]
	rest := source[afterPos:]
	afterText := pyLStrip(rest)

	if strings.HasPrefix(afterText, "import ") || strings.HasPrefix(afterText, "import{") {
		parsed := ParseJSDocText(raw)
		return &parsed
	}

	if exportKeyword.MatchString(afterText) {
		parsed := ParseJSDocText(raw)
		for _, tag := range parsed.Tags {
			if tag.Tag == "module" {
				return &parsed
			}
		}
		if len(parsed.Params) > 0 || (parsed.Returns != nil && *parsed.Returns != "") {
			return nil
		}
		gap := rest[:len(rest)-len(afterText)]
		if strings.Count(gap, "\n") >= 2 {
			return &parsed
		}
		return nil
	}

	parsed := ParseJSDocText(raw)
	return &parsed
}
