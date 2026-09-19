package extractors

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/util"
)

// The three character classes below are the exported spellings of Python's
// \s, \S and \w for a ported regexp. Go's versions of all three are
// ASCII-only, where Python's are Unicode-aware for text patterns, so a
// pattern copied across unchanged would quietly stop matching a no-break
// space or a non-Latin identifier. Every extractor ports Python regexes, so
// the classes are exported here rather than restated in each one.
//
// The values are [util]'s: one authority for what Python's classes denote,
// aliased under the names the extractors were written against.
const (
	// PySpaceClass is Python's \s: the ASCII whitespace characters, the four
	// ASCII separator controls, and the Unicode whitespace code points.
	PySpaceClass = util.PythonSpaceClass

	// PyNonSpaceClass is Python's \S, the complement of PySpaceClass.
	PyNonSpaceClass = util.PythonNonSpaceClass

	// PyWordClass is Python's \w where it matches an identifier character: a
	// letter, a digit or an underscore, in any script.
	PyWordClass = util.PythonWordClass
)

// pySpaceClass and pyWordClass are the in-package spellings of the two classes
// this file's own patterns are built from.
const (
	pySpaceClass = PySpaceClass
	pyWordClass  = PyWordClass
)

// pyStrip is Python's str.strip() with no argument.
func pyStrip(s string) string { return util.PythonStrip(s) }

// pyLStrip is Python's str.lstrip() with no argument.
func pyLStrip(s string) string { return util.PythonLStrip(s) }

// pyRStrip is Python's str.rstrip() with no argument.
func pyRStrip(s string) string { return util.PythonRStrip(s) }

// indentOf counts the leading whitespace characters of s, the quantity every
// docstring-section scanner compares indentation with.
func indentOf(s string) int {
	return len([]rune(s)) - len([]rune(pyLStrip(s)))
}

// isPyAlnum reports whether r is alphanumeric by Python's str.isalnum rule.
func isPyAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r)
}

// FormatError renders a message as the block-quoted marker selfdoc leaves in
// place of a directive it could not resolve.
func FormatError(message string) string {
	return "> *[selfdoc: " + message + "]*"
}

// SymbolHeading is a heading naming something the extractor read out of source.
//
// A class, a function, a struct field, a table, a module path -- every one of
// them is a token the generator copied from code, and it is emitted as a code
// span so the page says so.
//
// That is a claim about what the text is, and two readers act on it. A human
// sees a symbol set in the page's code face rather than a word in its prose
// face. The spell checker sees a code span and does not read it: it masks
// inline code, so an identifier it cannot recognize as English -- JSONResponse,
// returncode -- stops being a misspelling the moment the generator marks it as
// what it is. What remains flagged is docstring prose, which is the author's
// writing and the author's to fix; an identifier written into a sentence is
// backticked by whoever wrote the sentence.
//
// The heading's anchor is unaffected: anchors are slugified from the rendered
// inline form, and rendering a code span leaves the same text, so #jsonresponse
// still addresses the same heading.
func SymbolHeading(level int, name string) string {
	return strings.Repeat("#", level) + " `" + name + "`"
}

// SymbolSpan is a token the extractor read out of source, inline in generated
// prose.
//
// The heading form of the same claim is SymbolHeading; this is what a generated
// list item, a table cell or a rendered label uses. A name that came from code
// is set in the code face and skipped by the spell checker, whichever generated
// structure it appears in.
func SymbolSpan(name string) string {
	return "`" + name + "`"
}

// SymbolHeadingPattern matches a heading that names name, wherever the heading
// came from.
//
// The coverage measurement asks one question of a page -- does any heading on it
// name this symbol -- and pages come from two writers. SymbolHeading writes the
// code-span form; an author writing a reference page by hand writes whichever
// form reads well to them. The optional qualifier admits a method named by its
// owner (Pipeline.Execute).
func SymbolHeadingPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?m)^#{2,4}` + pySpaceClass + "+`?(?:" + pyWordClass + `+\.)?` +
			regexp.QuoteMeta(name) + "`?" + pySpaceClass + `*$`,
	)
}

// atxHeading matches an ATX heading, unindented. CommonMark allows up to three
// leading spaces, but a doc comment that indents a "#" is far more likely to be
// showing a shell prompt or a comment character than writing a heading, so only
// column zero counts here.
var atxHeading = regexp.MustCompile(`^(#{1,6})(` + pySpaceClass + `.*)?$`)

// codeFence matches a fenced code block's delimiter. Headings inside one are
// content.
var codeFence = regexp.MustCompile(`^` + pySpaceClass + "*(```|~~~)")

// DemoteDocHeadings renests the headings a doc comment wrote, under the one
// above them.
//
// A doc comment is written as if it owned a document -- Go's own convention is
// "# Usage", and a KDoc, docstring or JSDoc block can carry any markdown -- but
// on a generated reference page it is a subsection of a symbol, and the page
// already has exactly one H1: its title. Emitted verbatim, a package doc with
// headings puts a second H1 on the page, which is a hard error, and the symbol
// the doc belongs to stops being its parent in the outline.
//
// baseLevel is the level of the heading the text sits under: the symbol's own
// heading, or 1 -- the page title -- for a directive that emits the doc alone.
// Every heading shifts by the same amount, so the doc's internal structure is
// kept; the shallowest one becomes the direct child of baseLevel. Nothing
// shifts when the doc is already nested deeply enough, and nothing goes past
// H6, markdown's floor.
func DemoteDocHeadings(text string, baseLevel int) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")

	inFence := false
	shallowest := -1
	for _, line := range lines {
		if codeFence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := atxHeading.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			if shallowest < 0 || level < shallowest {
				shallowest = level
			}
		}
	}

	if shallowest < 0 {
		return text
	}
	delta := (baseLevel + 1) - shallowest
	if delta <= 0 {
		return text
	}

	inFence = false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if codeFence.MatchString(line) {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		var m []string
		if !inFence {
			m = atxHeading.FindStringSubmatch(line)
		}
		if m != nil {
			level := len(m[1]) + delta
			if level > 6 {
				level = 6
			}
			out = append(out, strings.Repeat("#", level)+m[2])
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ParseCommaSet splits a comma-separated attribute value into its distinct,
// trimmed, non-empty parts.
//
// The result is sorted, where the Python set it replaces iterated in an
// arbitrary order. Only one caller iterates it -- ApplyExcludeKeys, to name the
// first excluded key a document does not carry -- so sorting turns an arbitrary
// choice among several missing keys into a stable one.
func ParseCommaSet(value string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = pyStrip(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	sort.Strings(out)
	return out
}

// ExcludeKeysFromAttrs reads a directive's exclude attribute as a key set.
// An absent or empty attribute excludes nothing.
func ExcludeKeysFromAttrs(attrs map[string]string) []string {
	if attrs["exclude"] == "" {
		return nil
	}
	return ParseCommaSet(attrs["exclude"])
}

// ApplyExcludeKeys drops excludeKeys from data.
//
// It returns the filtered object, or -- when data does not carry one of the
// keys -- a nil object and the error marker naming it. Excluding a key that is
// not there is a mistake in the directive rather than a no-op: the author
// believes they are hiding something, and silently rendering it would publish
// the very value they meant to withhold.
func ApplyExcludeKeys(data *JSONObject, excludeKeys []string, displayPath string) (*JSONObject, string) {
	if len(excludeKeys) == 0 {
		return data, ""
	}
	for _, key := range excludeKeys {
		if !data.Has(key) {
			return nil, FormatError("exclude key '" + key + "' not found in '" + displayPath + "'")
		}
	}
	excluded := map[string]bool{}
	for _, key := range excludeKeys {
		excluded[key] = true
	}
	out := NewJSONObject()
	for _, key := range data.Keys() {
		if excluded[key] {
			continue
		}
		value, _ := data.Get(key)
		out.Set(key, value)
	}
	return out, ""
}

// ReadSource reads a source file as text.
func ReadSource(filepath string) (string, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ExtractBraceBlock returns the content between the brace at openBracePos and
// its match, exclusive of both, and reports whether the braces matched.
//
// String literals are skipped, so a brace inside one does not change the depth,
// and a backslash escapes the next byte. openBracePos is a byte offset into
// source, which is what a regexp match index and strings.Index both report.
func ExtractBraceBlock(source string, openBracePos int) (string, bool) {
	if openBracePos < 0 || openBracePos >= len(source) || source[openBracePos] != '{' {
		return "", false
	}

	depth := 0
	pos := openBracePos
	inString := false
	var stringChar byte

	for pos < len(source) {
		ch := source[pos]

		if inString {
			if ch == '\\' {
				pos += 2
				continue
			}
			if ch == stringChar {
				inString = false
			}
			pos++
			continue
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			pos++
			continue
		}

		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return source[openBracePos+1 : pos], true
			}
		}

		pos++
	}

	return "", false
}

// CollectCommentLinesAbove collects the contiguous comment lines above
// startLine whose trimmed form begins with prefix, walking upward and stopping
// at the first line that does not.
//
// The prefix and one optional following space are removed from each line. With
// skipBlankLines the walk crosses blank lines between the declaration and the
// comment block; without it, a blank line means the declaration has no comment.
// startLine is a zero-based index into lines.
func CollectCommentLinesAbove(lines []string, startLine int, prefix string, skipBlankLines bool) string {
	if startLine <= 0 {
		return ""
	}

	idx := startLine - 1

	if skipBlankLines {
		for idx >= 0 && pyStrip(lines[idx]) == "" {
			idx--
		}
	}

	if idx < 0 {
		return ""
	}

	var commentLines []string
	for idx >= 0 {
		stripped := pyStrip(lines[idx])
		if !strings.HasPrefix(stripped, prefix) {
			break
		}
		text := stripped[len(prefix):]
		text = strings.TrimPrefix(text, " ")
		commentLines = append(commentLines, text)
		idx--
	}

	if len(commentLines) == 0 {
		return ""
	}

	for i, j := 0, len(commentLines)-1; i < j; i, j = i+1, j-1 {
		commentLines[i], commentLines[j] = commentLines[j], commentLines[i]
	}
	return strings.Join(commentLines, "\n")
}

var (
	whitespaceOnlyLine = regexp.MustCompile(`(?m)^[ \t]+$`)
	leadingWhitespace  = regexp.MustCompile(`(?m)(^[ \t]*)(?:[^ \t\n])`)
)

// Dedent removes the longest common leading whitespace from every line of text,
// reproducing Python's textwrap.dedent.
//
// Lines made only of whitespace are emptied first and then ignored when the
// common margin is measured, so a blank line indented less than the block does
// not defeat the dedent.
func Dedent(text string) string {
	text = whitespaceOnlyLine.ReplaceAllString(text, "")

	margin := ""
	haveMargin := false
	for _, m := range leadingWhitespace.FindAllStringSubmatch(text, -1) {
		indent := m[1]
		switch {
		case !haveMargin:
			margin = indent
			haveMargin = true
		case strings.HasPrefix(indent, margin):
			// Current line more indented than the margin: keep the margin.
		case strings.HasPrefix(margin, indent):
			margin = indent
		default:
			// Neither is a prefix of the other, so they differ inside the
			// shorter one: the margin is the part they agree on.
			for i := 0; i < len(margin) && i < len(indent); i++ {
				if margin[i] != indent[i] {
					margin = margin[:i]
					break
				}
			}
		}
	}

	if margin == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = strings.TrimPrefix(line, margin)
	}
	return strings.Join(lines, "\n")
}
