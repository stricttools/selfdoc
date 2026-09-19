package tokenizer

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// pySpace and pyWord are this package's short spellings of Python's str-mode
// \s and \w, which every pattern below was written against. Go's own two are
// narrower: its \s omits the four information separators U+001C through
// U+001F, and its \w is ASCII-only.
const (
	pySpace = util.PythonSpaceClass
	pyWord  = util.PythonWordClass
)

// pyDigit is the character class of Python's str-mode \d: a Unicode decimal
// digit.
const pyDigit = `[\p{Nd}]`

var (
	reThematicBreak  = regexp.MustCompile(`^(---+|\*\*\*+|___+)$`)
	reHeading        = regexp.MustCompile(`^(#{1,6})` + pySpace + `+(.+)$`)
	reTableRow       = regexp.MustCompile(`^\|.+\|$`)
	reUnordered      = regexp.MustCompile(`^[-*]` + pySpace + `+`)
	reOrdered        = regexp.MustCompile(`^` + pyDigit + `+\.` + pySpace + `+`)
	reAnnotation     = regexp.MustCompile(`^\[(` + pyDigit + `+)\]:` + pySpace + `*(.+)$`)
	reAdmonition     = regexp.MustCompile(`^\[!(` + pyWord + `+)\]`)
	reDirectiveOpen  = regexp.MustCompile(`^:::(` + pyWord + `+)(?:` + pySpace + `+(.+))?$`)
	reDirectiveClose = regexp.MustCompile(`^:::$`)
	reQuoteMarker    = regexp.MustCompile(`^>` + pySpace + `?`)
)

// Tokenize splits Markdown content into a slice of block tokens.
//
// Line numbers on every token are 1-based, counting the first line of the
// input as 1. The tokens cover every line once -- no gaps and no overlaps --
// which is what lets a caller map any diagnostic back to a source line.
//
// The dispatch order is fixed and each step below is tried in turn: fenced
// code block, thematic break, heading, ":::" directive, table, unordered
// list, ordered list, blockquote, blank line, definition list, and finally a
// paragraph, which absorbs everything no earlier step claimed. The order is
// what makes "---" a thematic break rather than a heading, and what keeps a
// "#" line inside a fence out of the heading branch.
func Tokenize(content string) []Token {
	lines := strings.Split(content, "\n")
	n := len(lines)
	var tokens []Token
	i := 0

	for i < n {
		line := lines[i]

		// 1. Fenced code block.
		if strings.HasPrefix(line, "```") {
			start := i
			infoParts := util.PythonFields(util.PythonStrip(line[3:]))
			lang := ""
			if len(infoParts) > 0 {
				lang = infoParts[0]
			}
			flags := infoParts
			if len(flags) > 0 {
				flags = flags[1:]
			}
			runFlag := contains(flags, "run")
			validateFlag := contains(flags, "validate")
			// Line numbers annotation: "lines" or "lines=N".
			lnFlag := false
			lnStart := 1
			for _, part := range flags {
				switch {
				case part == "lines":
					lnFlag = true
				case strings.HasPrefix(part, "lines="):
					lnFlag = true
					if parsed, ok := util.ParsePythonInt(part[len("lines="):]); ok {
						lnStart = int(parsed)
					}
				}
			}
			var codeLines []string
			i++
			for i < n && !strings.HasPrefix(lines[i], "```") {
				codeLines = append(codeLines, lines[i])
				i++
			}
			if i < n {
				i++ // skip closing ```
			}

			// Greedily consume annotation lines after the fence.
			var annotations []Annotation
			for i < n {
				m := reAnnotation.FindStringSubmatch(lines[i])
				if m == nil {
					break
				}
				annotations = setAnnotation(annotations, m[1], m[2])
				i++
			}

			tokens = append(tokens, CodeBlock{
				Span:        Span{StartLine: start + 1, EndLine: i},
				Lang:        lang,
				Lines:       codeLines,
				Annotations: annotations,
				Run:         runFlag,
				LineNumbers: lnFlag,
				LineStart:   lnStart,
				Validate:    validateFlag,
			})
			continue
		}

		// 2. Thematic break, before the heading branch so "---" does not
		// become a heading.
		if reThematicBreak.MatchString(line) {
			tokens = append(tokens, ThematicBreak{
				Span: Span{StartLine: i + 1, EndLine: i + 1},
			})
			i++
			continue
		}

		// 3. Heading.
		if m := reHeading.FindStringSubmatch(line); m != nil {
			tokens = append(tokens, Heading{
				Span:  Span{StartLine: i + 1, EndLine: i + 1},
				Level: len(m[1]),
				Text:  m[2],
			})
			i++
			continue
		}

		// 4. Directive (":::name arg ... :::").
		if m := reDirectiveOpen.FindStringSubmatch(line); m != nil {
			start := i
			name := m[1]
			arg := m[2]
			var body []string
			i++
			for i < n && !reDirectiveClose.MatchString(lines[i]) {
				body = append(body, lines[i])
				i++
			}
			if i < n {
				i++ // skip closing :::
			}
			tokens = append(tokens, Directive{
				Span: Span{StartLine: start + 1, EndLine: i},
				Name: name,
				Arg:  arg,
				Body: body,
			})
			continue
		}

		// 5. Table.
		if reTableRow.MatchString(util.PythonStrip(line)) {
			start := i
			var rows []string
			for i < n && reTableRow.MatchString(util.PythonStrip(lines[i])) {
				rows = append(rows, util.PythonStrip(lines[i]))
				i++
			}
			tokens = append(tokens, Table{
				Span: Span{StartLine: start + 1, EndLine: i},
				Rows: rows,
			})
			continue
		}

		// 6. Unordered list.
		if reUnordered.MatchString(line) {
			start := i
			var items []string
			for i < n && reUnordered.MatchString(lines[i]) {
				items = append(items, stripPrefix(reUnordered, lines[i]))
				i++
			}
			tokens = append(tokens, UnorderedList{
				Span:  Span{StartLine: start + 1, EndLine: i},
				Items: items,
			})
			continue
		}

		// 7. Ordered list.
		if reOrdered.MatchString(line) {
			start := i
			var items []string
			for i < n && reOrdered.MatchString(lines[i]) {
				items = append(items, stripPrefix(reOrdered, lines[i]))
				i++
			}
			tokens = append(tokens, OrderedList{
				Span:  Span{StartLine: start + 1, EndLine: i},
				Items: items,
			})
			continue
		}

		// 8. Blockquote.
		if strings.HasPrefix(line, ">") {
			start := i
			var quoteLines []string
			for i < n && strings.HasPrefix(lines[i], ">") {
				quoteLines = append(quoteLines, stripPrefix(reQuoteMarker, lines[i]))
				i++
			}
			admonition := ""
			if len(quoteLines) > 0 {
				if m := reAdmonition.FindStringSubmatch(quoteLines[0]); m != nil {
					admonition = m[1]
				}
			}
			tokens = append(tokens, Blockquote{
				Span:           Span{StartLine: start + 1, EndLine: i},
				Lines:          quoteLines,
				AdmonitionType: admonition,
			})
			continue
		}

		// 9. Blank line.
		if util.PythonStrip(line) == "" {
			tokens = append(tokens, BlankLine{
				Span: Span{StartLine: i + 1, EndLine: i + 1},
			})
			i++
			continue
		}

		// 10. Definition list.
		if util.PythonStrip(line) != "" && i+1 < n && strings.HasPrefix(lines[i+1], ": ") {
			start := i
			var entries []DefinitionEntry
			for i < n {
				termLine := util.PythonStrip(lines[i])
				if termLine == "" {
					// A blank line might separate groups: skip the blanks
					// and see whether the next non-blank line starts
					// another term-and-definition pair.
					j := i
					for j < n && util.PythonStrip(lines[j]) == "" {
						j++
					}
					if j < n && j+1 < n && util.PythonStrip(lines[j]) != "" &&
						strings.HasPrefix(lines[j+1], ": ") {
						i = j
						continue
					}
					break
				}
				// The next line must carry a definition.
				if i+1 >= n || !strings.HasPrefix(lines[i+1], ": ") {
					break
				}
				term := termLine
				i++
				var defs []string
				for i < n && strings.HasPrefix(lines[i], ": ") {
					defs = append(defs, lines[i][2:])
					i++
				}
				entries = append(entries, DefinitionEntry{Term: term, Definitions: defs})
			}
			// The end line includes any blanks between groups that were
			// consumed: they are part of this token.
			tokens = append(tokens, DefinitionList{
				Span:    Span{StartLine: start + 1, EndLine: i},
				Entries: entries,
			})
			continue
		}

		// 11. Paragraph (the fallback).
		start := i
		var paraLines []string
		for i < n {
			current := lines[i]
			if util.PythonStrip(current) == "" {
				break
			}
			if strings.HasPrefix(current, "```") {
				break
			}
			if reHeading.MatchString(current) {
				break
			}
			if reThematicBreak.MatchString(current) {
				break
			}
			if reUnordered.MatchString(current) {
				break
			}
			if reOrdered.MatchString(current) {
				break
			}
			if reTableRow.MatchString(util.PythonStrip(current)) {
				break
			}
			if strings.HasPrefix(current, ">") {
				break
			}
			if reDirectiveOpen.MatchString(current) {
				break
			}
			// Definition-list guard: do not absorb a line whose next line
			// starts a definition.
			if i+1 < n && strings.HasPrefix(lines[i+1], ": ") {
				break
			}
			paraLines = append(paraLines, current)
			i++
		}
		tokens = append(tokens, Paragraph{
			Span:  Span{StartLine: start + 1, EndLine: i},
			Lines: paraLines,
		})
	}

	return tokens
}

// setAnnotation records key with note, reproducing a Python dict assignment: a
// repeated key keeps its original position and takes the new note.
func setAnnotation(annotations []Annotation, key, note string) []Annotation {
	for idx := range annotations {
		if annotations[idx].Key == key {
			annotations[idx].Note = note
			return annotations
		}
	}
	return append(annotations, Annotation{Key: key, Note: note})
}

// stripPrefix removes re's anchored match from the front of line, which is
// Python's re.sub(pattern, "", line, count=1) for an anchored pattern.
func stripPrefix(re *regexp.Regexp, line string) string {
	if loc := re.FindStringIndex(line); loc != nil {
		return line[:loc[0]] + line[loc[1]:]
	}
	return line
}

// contains reports whether items holds want.
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
