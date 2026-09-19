package extractors

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/prose"
)

// sectionHeaders are the section headers recognized in Google-style
// docstrings.
var sectionHeaders = map[string]bool{
	"Args":              true,
	"Arguments":         true,
	"Returns":           true,
	"Return":            true,
	"Raises":            true,
	"Yields":            true,
	"Yield":             true,
	"Attributes":        true,
	"Note":              true,
	"Notes":             true,
	"Example":           true,
	"Examples":          true,
	"References":        true,
	"See Also":          true,
	"Todo":              true,
	"Keyword Args":      true,
	"Keyword Arguments": true,
}

// DocSections is a Google-style docstring parsed into its parts.
type DocSections struct {
	// Description is everything before the first section header.
	Description string
	// Params are the entries of the Args, Arguments, Keyword Args and
	// Keyword Arguments sections, in order.
	Params []DocParam
	// Returns is the text of the Returns or Yields section, nil when the
	// docstring has neither.
	Returns *string
	// Raises are the entries of the Raises section, in order.
	Raises []DocRaise
}

// DocParam is one documented parameter.
type DocParam struct {
	// Name is the parameter name as the docstring writes it, carrying any
	// variadic or keyword prefix.
	Name string
	// Type is the parenthesized type the docstring gives, nil when it gives
	// none.
	Type *string
	// Description is the parameter's documentation, with continuation lines
	// joined on.
	Description string
}

// DocRaise is one documented exception.
type DocRaise struct {
	// Type is the exception type the docstring names.
	Type string
	// Description is when it is raised.
	Description string
}

// ParseDocstringSections parses a Google-style docstring into its structured
// sections. It is how the quality measurement learns which parameters and
// return values a symbol's own documentation covers.
func ParseDocstringSections(text string) DocSections {
	lines := strings.Split(text, "\n")
	var descriptionLines []string
	var params []DocParam
	var returns *string
	var raises []DocRaise

	i := 0

	// Collect description lines (everything before the first section header).
	for i < len(lines) {
		stripped := pyStrip(lines[i])
		if _, ok := matchSectionHeader(stripped); ok {
			break
		}
		descriptionLines = append(descriptionLines, stripped)
		i++
	}

	description := pyStrip(strings.Join(descriptionLines, "\n"))

	for i < len(lines) {
		stripped := pyStrip(lines[i])
		headerName, ok := matchSectionHeader(stripped)
		if !ok {
			i++
			continue
		}

		i++

		// The section's content indentation is set by its first line.
		baseIndent := 4
		if i < len(lines) {
			baseIndent = indentOf(lines[i])
		}

		switch headerName {
		case "Args", "Arguments", "Keyword Args", "Keyword Arguments":
			for i < len(lines) {
				contentLine := lines[i]
				if pyStrip(contentLine) == "" {
					if sectionContinuesAfterBlank(lines, i, baseIndent) {
						i++
						continue
					}
					break
				}

				currentIndent := indentOf(contentLine)
				if currentIndent < baseIndent {
					break
				}

				textStripped := pyStrip(contentLine)
				if currentIndent == baseIndent && isParamLine(textStripped) {
					namePart, descPart := splitParamLine(textStripped)
					var paramType *string
					paramName := namePart
					if strings.HasSuffix(namePart, ")") {
						if parenIdx := strings.LastIndex(namePart, "("); parenIdx > 0 {
							t := pyStrip(namePart[parenIdx+1 : len(namePart)-1])
							paramType = &t
							paramName = pyStrip(namePart[:parenIdx])
						}
					}
					params = append(params, DocParam{
						Name:        paramName,
						Type:        paramType,
						Description: descPart,
					})
				} else if currentIndent > baseIndent && len(params) > 0 {
					params[len(params)-1].Description += " " + textStripped
				}
				i++
			}

		case "Returns", "Return", "Yields", "Yield":
			var returnLines []string
			for i < len(lines) {
				contentLine := lines[i]
				if pyStrip(contentLine) == "" {
					if sectionContinuesAfterBlank(lines, i, baseIndent) {
						i++
						continue
					}
					break
				}

				if indentOf(contentLine) < baseIndent {
					break
				}

				returnLines = append(returnLines, pyStrip(contentLine))
				i++
			}
			if len(returnLines) > 0 {
				joined := strings.Join(returnLines, " ")
				returns = &joined
			}

		case "Raises":
			for i < len(lines) {
				contentLine := lines[i]
				if pyStrip(contentLine) == "" {
					if sectionContinuesAfterBlank(lines, i, baseIndent) {
						i++
						continue
					}
					break
				}

				currentIndent := indentOf(contentLine)
				if currentIndent < baseIndent {
					break
				}

				textStripped := pyStrip(contentLine)
				if currentIndent == baseIndent && isParamLine(textStripped) {
					excName, excDesc := splitParamLine(textStripped)
					raises = append(raises, DocRaise{Type: excName, Description: excDesc})
				} else if currentIndent > baseIndent && len(raises) > 0 {
					raises[len(raises)-1].Description += " " + textStripped
				}
				i++
			}

		default:
			// Skip other sections (Note, Examples, and the rest).
			for i < len(lines) {
				contentLine := lines[i]
				if pyStrip(contentLine) == "" {
					if sectionContinuesAfterBlank(lines, i, baseIndent) {
						i++
						continue
					}
					break
				}

				if indentOf(contentLine) < baseIndent {
					break
				}
				i++
			}
		}
	}

	return DocSections{
		Description: description,
		Params:      params,
		Returns:     returns,
		Raises:      raises,
	}
}

// sectionContinuesAfterBlank reports whether the blank line at index i is
// inside a section rather than ending it, by looking past it for a line still
// indented to at least baseIndent.
func sectionContinuesAfterBlank(lines []string, i, baseIndent int) bool {
	j := i + 1
	for j < len(lines) && pyStrip(lines[j]) == "" {
		j++
	}
	if j >= len(lines) {
		return false
	}
	return indentOf(lines[j]) >= baseIndent
}

// FormatDocstring transforms Google-style docstring sections into markdown.
//
// Section headers like "Args:", "Returns:" and "Raises:" followed by indented
// "name: description" lines become bold headers with bullet lists, so the
// markdown converter renders them as structured HTML instead of collapsing the
// whitespace.
//
// Source-wrapped prose (Go, JSDoc and KDoc doc comments wrap at around 75
// columns) is first normalized with prose.JoinWrappedLines so a soft-wrapped
// sentence becomes one line; blank-line paragraph breaks, indented
// preformatted blocks,
// fenced code, list items and doctest lines are left verbatim.
//
// baseLevel is the level of the heading this text is emitted under, or 1 for
// the page title when it is emitted alone. Headings the doc comment wrote are
// renested beneath it; see DemoteDocHeadings. It has no default, because every
// caller knows where it is putting the text and a wrong guess puts a second H1
// on the page.
func FormatDocstring(docstring string, baseLevel int) string {
	docstring = DemoteDocHeadings(docstring, baseLevel)
	docstring = prose.JoinWrappedLines(docstring)
	lines := strings.Split(docstring, "\n")
	var out []string
	i := 0

	for i < len(lines) {
		line := lines[i]
		stripped := pyStrip(line)

		headerName, ok := matchSectionHeader(stripped)
		if ok {
			// A blank line before the header gives markdown its spacing.
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, "**"+headerName+":**")
			out = append(out, "")
			i++

			baseIndent := 4
			if i < len(lines) {
				baseIndent = indentOf(lines[i])
			}

			for i < len(lines) {
				contentLine := lines[i]

				if pyStrip(contentLine) == "" {
					if sectionContinuesAfterBlank(lines, i, baseIndent) {
						// Still in the section -- keep the blank line.
						out = append(out, "")
						i++
						continue
					}
					break
				}

				currentIndent := indentOf(contentLine)
				if currentIndent < baseIndent {
					break
				}

				text := pyStrip(contentLine)

				switch {
				case currentIndent == baseIndent && isParamLine(text):
					// A new list item: "name: description" or
					// "name (type): description".
					name, desc := splitParamLine(text)
					if desc != "" {
						out = append(out, "- `"+name+"`: "+desc)
					} else {
						out = append(out, "- `"+name+"`")
					}
				case currentIndent == baseIndent:
					// A plain line at the base indent, such as a Returns
					// section carrying only a description.
					out = append(out, "- "+text)
				default:
					// A continuation of the previous item.
					out = append(out, "  "+text)
				}

				i++
			}
			continue
		}

		out = append(out, line)
		i++
	}

	return strings.Join(out, "\n")
}

// matchSectionHeader reports whether stripped is a recognized section header
// like "Args:", and names it.
func matchSectionHeader(stripped string) (string, bool) {
	if !strings.HasSuffix(stripped, ":") {
		return "", false
	}
	candidate := pyStrip(stripped[:len(stripped)-1])
	if sectionHeaders[candidate] {
		return candidate, true
	}
	return "", false
}

// isParamLine reports whether text looks like "name: description" or
// "name (type): description".
func isParamLine(text string) bool {
	colonIdx := strings.Index(text, ":")
	if colonIdx <= 0 {
		return false
	}
	before := pyStrip(text[:colonIdx])
	if strings.HasSuffix(before, ")") {
		if parenIdx := strings.LastIndex(before, "("); parenIdx > 0 {
			before = pyStrip(before[:parenIdx])
		}
	}
	before = strings.TrimLeft(before, "*")
	if before == "" {
		return false
	}
	for _, r := range before {
		if isPyAlnum(r) || r == '_' || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// splitParamLine splits "name: description" into its two parts, and handles
// "name (type): description" by leaving the parenthesized type on the name.
func splitParamLine(text string) (string, string) {
	colonIdx := strings.Index(text, ":")
	name := pyStrip(text[:colonIdx])
	desc := pyStrip(text[colonIdx+1:])
	return name, desc
}
