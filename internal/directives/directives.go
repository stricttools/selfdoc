package directives

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/util"
)

// directiveNamePattern is the directive-name grammar: a letter, then word
// characters or hyphens. The error messages quote it in its Python spelling,
// `[a-zA-Z][\w-]*`, because that is the spelling every document and every
// piece of documentation carries.
const directiveNamePattern = `[a-zA-Z][\p{L}\p{N}_-]*`

// attrKeyPattern is the attribute-key grammar. Keys may contain hyphens (as
// in schema-dir), matching the directive-name character class.
const attrKeyPattern = `[\p{L}\p{N}_-]+`

var (
	// One-liner: :-: name [attrs]
	onelinerRe = regexp.MustCompile(`^:-:` + pySpaceClass + `+(` + pyNotSpaceClass + `+)(.*)$`)

	// Block open: :<: name [attrs]
	blockOpenRe = regexp.MustCompile(`^:<:` + pySpaceClass + `+(` + pyNotSpaceClass + `+)(.*)$`)

	// Attribute line: :@: key="value"
	attrLineRe = regexp.MustCompile(`^:@:` + pySpaceClass + `+(.+)$`)

	// Body separator: :=:
	bodySepRe = regexp.MustCompile(`^:=:$`)

	// Body line: ::: content (strip the 4-char prefix "::: "), or bare :::
	// for an empty body line.
	bodyLineRe      = regexp.MustCompile(`^::: (.*)$`)
	bodyLineEmptyRe = regexp.MustCompile(`^:::$`)

	// Block close: :>:
	blockCloseRe = regexp.MustCompile(`^:>:$`)

	// Fenced code block delimiter (``` or ~~~, optionally with an info string)
	fenceRe = regexp.MustCompile("^(`{3,}|~{3,})")

	// Attribute key="value" pair extractor.
	attrKVRe = regexp.MustCompile(`(` + attrKeyPattern + `)="([^"]*)"`)

	// Inline one-liner: :-: name [attrs] (non-anchored, for pass 2)
	inlineRe = regexp.MustCompile(`:-:` + pySpaceClass + `+(` + directiveNamePattern + `)((?:` + pySpaceClass + `+` + attrKeyPattern + `="[^"]*")*)`)

	// The directive-name grammar, anchored.
	directiveNameRe = regexp.MustCompile(`^` + directiveNamePattern + `$`)

	// The directive-name grammar, unanchored, for measuring how much of a
	// malformed token is a valid prefix.
	directiveNamePrefixRe = regexp.MustCompile(`^` + directiveNamePattern)

	// Any of the six markers standing at the start of a line, and the
	// self-closing marker used inline. Detection only: no name validation, no
	// attribute parsing, no well-formedness requirement -- the question these
	// answer is "does this markdown carry directive syntax at all?", which
	// must be answerable for a document that was never meant to be resolved.
	markerLineRe   = regexp.MustCompile(`^(:-:|:<:|:@:|:=:|:::|:>:)(` + pySpaceClass + `|$)`)
	inlineMarkerRe = regexp.MustCompile(`:-:` + pySpaceClass + `+` + directiveNamePattern)

	// A word character anywhere, for the malformed-name check.
	wordCharRe = regexp.MustCompile(pyWordClass)
)

// DirectiveError reports a malformed directive: an unknown or ungrammatical
// name, an unexpected line inside a block, or a block still open at EOF.
type DirectiveError struct {
	Message string
}

func (e *DirectiveError) Error() string { return e.Message }

func directiveErrorf(format string, args ...any) *DirectiveError {
	return &DirectiveError{Message: fmt.Sprintf(format, args...)}
}

// InlineOutputError reports an inline directive whose resolver returned more
// than one line. An inline directive is substituted into the middle of a line
// of prose, so multi-line output has nowhere to go.
type InlineOutputError struct {
	// Name is the directive that produced the offending output.
	Name string
}

func (e *InlineOutputError) Error() string {
	return fmt.Sprintf(
		"Inline directive '%s' returned multi-line output; "+
			"only single-line output is allowed for inline directives.",
		e.Name,
	)
}

// Directive is one parsed directive block.
//
// Body is never nil: a directive with no body carries an empty slice, matching
// the Python dataclass's default_factory=list, so a resolver may index it
// without a nil check. Column is set only for an inline directive, and is a
// CHARACTER offset into its line.
type Directive struct {
	Name  string
	Attrs map[string]string
	// AttrOrder names every key in Attrs in the order the source wrote it,
	// first appearance winning for a repeated key. A Go map has no order,
	// and the check report quotes a directive back to its author, who reads
	// the quote against the template they typed.
	AttrOrder  []string
	Body       []string
	LineNumber int
	Inline     bool
	Column     *int
}

// NameSet is a set of directive names. A nil NameSet means "accept any name";
// an empty non-nil one accepts nothing.
type NameSet = map[string]struct{}

// Resolver renders one directive occurrence into markdown.
//
// It receives the directive's name, its parsed attributes and its body lines,
// and returns the text that replaces the directive. An error aborts the whole
// resolution -- this is the Python resolver callable's raise.
type Resolver func(name string, attrs map[string]string, body []string) (string, error)

// parseAttrs extracts every key="value" pair from text, with the keys in
// source order. A repeated key keeps its last value and its first position,
// as Python's dict(findall(...)) does.
func parseAttrs(text string) (map[string]string, []string) {
	attrs := map[string]string{}
	var order []string
	for _, m := range attrKVRe.FindAllStringSubmatch(text, -1) {
		if _, seen := attrs[m[1]]; !seen {
			order = append(order, m[1])
		}
		attrs[m[1]] = m[2]
	}
	return attrs, order
}

// validateDirectiveName refuses name when validNames is non-nil and does not
// carry it.
func validateDirectiveName(name string, validNames NameSet, lineNumber int) error {
	if validNames == nil {
		return nil
	}
	if _, ok := validNames[name]; ok {
		return nil
	}
	return directiveErrorf("Unknown directive '%s' at line %d", name, lineNumber)
}

// ValidateDirectiveNames checks that each name matches the directive-name
// grammar, `[a-zA-Z][\w-]*`, and returns a [DirectiveError] naming the first
// one that does not.
func ValidateDirectiveNames(names []string) error {
	for _, name := range names {
		if !directiveNameRe.MatchString(name) {
			return directiveErrorf(
				"Invalid directive name '%s': must match [a-zA-Z][\\w-]*", name)
		}
	}
	return nil
}

// eventKind distinguishes the three things walkBlocks reports.
type eventKind int

const (
	evLine eventKind = iota
	evDirective
	evUnclosed
)

// event is one logical unit walkBlocks produced: a pass-through line, a
// complete directive, or a block left open at EOF.
type event struct {
	kind      eventKind
	line      string
	name      string
	attrs     map[string]string
	attrOrder []string
	body      []string
	lineNum   int
}

// walkBlocks is the shared state machine for fence tracking and directive
// detection. It is the one reader of the marker grammar: both
// [ParseDirectives] and [ResolveDirectives] consume its events, so they can
// never disagree about what is a directive.
//
// The four states are idle, in_fence, in_block_attrs and in_block_body, and
// the machine only leaves the last two through a close marker or EOF.
func walkBlocks(content string, validNames NameSet) ([]event, error) {
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	var events []event

	// Fence tracking.
	fenceChar := byte(0)
	fenceLen := 0

	const (
		stateIdle = iota
		stateInFence
		stateInBlockAttrs
		stateInBlockBody
	)
	state := stateIdle

	// Block accumulation.
	blockName := ""
	blockAttrs := map[string]string{}
	var blockAttrOrder []string
	var blockBody []string
	blockLine := 0

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1
		stripped := util.PythonStrip(line)

		// Fence tracking applies in idle and in_fence only; inside a block, a
		// fence line is an unexpected line.
		if state == stateIdle || state == stateInFence {
			if m := fenceRe.FindStringSubmatch(stripped); m != nil {
				marker := m[1]
				if state == stateIdle {
					fenceChar = marker[0]
					fenceLen = len(marker)
					state = stateInFence
					events = append(events, event{kind: evLine, line: line})
					continue
				}
				if marker[0] == fenceChar && len(marker) >= fenceLen {
					state = stateIdle
					fenceChar = 0
					fenceLen = 0
				}
				events = append(events, event{kind: evLine, line: line})
				continue
			}
		}

		if state == stateInFence {
			events = append(events, event{kind: evLine, line: line})
			continue
		}

		switch state {
		case stateIdle:
			if m := onelinerRe.FindStringSubmatch(stripped); m != nil {
				name := m[1]
				if err := validateDirectiveName(name, validNames, lineNum); err != nil {
					return nil, err
				}
				attrs, order := parseAttrs(m[2])
				events = append(events, event{
					kind:      evDirective,
					name:      name,
					attrs:     attrs,
					attrOrder: order,
					body:      []string{},
					lineNum:   lineNum,
				})
				continue
			}

			if m := blockOpenRe.FindStringSubmatch(stripped); m != nil {
				blockName = m[1]
				if err := validateDirectiveName(blockName, validNames, lineNum); err != nil {
					return nil, err
				}
				blockAttrs, blockAttrOrder = parseAttrs(m[2])
				blockBody = []string{}
				blockLine = lineNum
				state = stateInBlockAttrs
				continue
			}

			events = append(events, event{kind: evLine, line: line})

		case stateInBlockAttrs:
			if m := attrLineRe.FindStringSubmatch(stripped); m != nil {
				lineAttrs, lineOrder := parseAttrs(m[1])
				for _, k := range lineOrder {
					if _, seen := blockAttrs[k]; !seen {
						blockAttrOrder = append(blockAttrOrder, k)
					}
					blockAttrs[k] = lineAttrs[k]
				}
				continue
			}

			if bodySepRe.MatchString(stripped) {
				state = stateInBlockBody
				continue
			}

			if blockCloseRe.MatchString(stripped) {
				events = append(events, closedBlock(blockName, blockAttrs, blockAttrOrder, blockBody, blockLine))
				state = stateIdle
				continue
			}

			return nil, unexpectedLine(lineNum, stripped)

		case stateInBlockBody:
			if m := bodyLineRe.FindStringSubmatch(stripped); m != nil {
				blockBody = append(blockBody, m[1])
				continue
			}
			if bodyLineEmptyRe.MatchString(stripped) {
				blockBody = append(blockBody, "")
				continue
			}

			if blockCloseRe.MatchString(stripped) {
				events = append(events, closedBlock(blockName, blockAttrs, blockAttrOrder, blockBody, blockLine))
				state = stateIdle
				continue
			}

			return nil, unexpectedLine(lineNum, stripped)
		}
	}

	if state == stateInBlockAttrs || state == stateInBlockBody {
		events = append(events, event{kind: evUnclosed, name: blockName, lineNum: blockLine})
	}
	return events, nil
}

// closedBlock snapshots the accumulated block state into a directive event,
// so a later block cannot mutate an already-reported one.
func closedBlock(name string, attrs map[string]string, attrOrder []string, body []string, lineNum int) event {
	snapAttrs := make(map[string]string, len(attrs))
	for k, v := range attrs {
		snapAttrs[k] = v
	}
	snapOrder := make([]string, len(attrOrder))
	copy(snapOrder, attrOrder)
	snapBody := make([]string, len(body))
	copy(snapBody, body)
	return event{
		kind:      evDirective,
		name:      name,
		attrs:     snapAttrs,
		attrOrder: snapOrder,
		body:      snapBody,
		lineNum:   lineNum,
	}
}

func unexpectedLine(lineNum int, stripped string) error {
	return directiveErrorf(
		"Unexpected line inside directive block at line %d: %s",
		lineNum, util.PythonRepr(stripped))
}

// ParseDirectives extracts every directive from markdown content, in document
// order.
//
// Both standalone directives (pass 1) and inline ones (pass 2) are returned;
// directives inside fenced code blocks and backtick code spans are ignored. A
// directive opened and never closed, or a name validNames does not carry, is a
// [DirectiveError].
func ParseDirectives(content string, validNames NameSet) ([]Directive, error) {
	events, err := walkBlocks(content, validNames)
	if err != nil {
		return nil, err
	}

	var out []Directive
	for _, ev := range events {
		switch ev.kind {
		case evDirective:
			out = append(out, Directive{
				Name:       ev.name,
				Attrs:      ev.attrs,
				AttrOrder:  ev.attrOrder,
				Body:       ev.body,
				LineNumber: ev.lineNum,
			})
		case evUnclosed:
			return nil, directiveErrorf(
				"Unclosed directive '%s' opened at line %d", ev.name, ev.lineNum)
		}
	}

	// Pass 2: scan non-directive, non-fenced lines for inline directives.
	// Re-scan the original content with its own fence tracking so the line
	// numbers are the source's.
	if content != "" {
		fenceChar := byte(0)
		fenceLen := 0
		for lineIdx, line := range strings.Split(content, "\n") {
			stripped := util.PythonStrip(line)
			if m := fenceRe.FindStringSubmatch(stripped); m != nil {
				marker := m[1]
				if fenceChar == 0 {
					fenceChar = marker[0]
					fenceLen = len(marker)
				} else if marker[0] == fenceChar && len(marker) >= fenceLen {
					fenceChar = 0
					fenceLen = 0
				}
				continue
			}
			if fenceChar != 0 {
				continue
			}
			// A standalone directive is pass 1's business.
			if onelinerRe.MatchString(stripped) {
				continue
			}
			found, err := FindInlineDirectives(line, lineIdx+1, validNames)
			if err != nil {
				return nil, err
			}
			out = append(out, found...)
		}
	}

	return out, nil
}

// FindInlineDirectives finds the inline :-: directives in one line, skipping
// backtick code spans.
//
// Each returned directive has Inline true and Column set to the match's
// character offset in the original (unmasked) line.
func FindInlineDirectives(line string, lineNum int, validNames NameSet) ([]Directive, error) {
	masked, placeholders := MaskBacktickSpans(line)
	var out []Directive
	for _, loc := range inlineRe.FindAllStringSubmatchIndex(masked, -1) {
		name := masked[loc[2]:loc[3]]
		if err := validateDirectiveName(name, validNames, lineNum); err != nil {
			return nil, err
		}
		attrs, attrOrder := parseAttrs(masked[loc[4]:loc[5]])
		col := utf8.RuneCountInString(line[:originalOffset(masked, placeholders, loc[0])])
		out = append(out, Directive{
			Name:       name,
			Attrs:      attrs,
			AttrOrder:  attrOrder,
			Body:       []string{},
			LineNumber: lineNum,
			Inline:     true,
			Column:     &col,
		})
	}
	return out, nil
}

// originalOffset maps a byte offset in a masked line back to its byte offset
// in the line before masking, by adding back the width every placeholder
// standing before it removed.
func originalOffset(masked string, placeholders []string, offset int) int {
	adjusted := offset
	for i, original := range placeholders {
		phText := placeholderText(i)
		phPos := strings.Index(masked, phText)
		if phPos < 0 || phPos >= offset {
			break
		}
		adjusted += len(original) - len(phText)
	}
	return adjusted
}

// Marker is one directive marker found by [FindDirectiveMarkers]: the 1-based
// line it stands on and the marker itself.
type Marker struct {
	LineNumber int
	Marker     string
}

// FindDirectiveMarkers finds every directive marker in content, by line, in
// document order.
//
// Fenced code blocks and backtick code spans are skipped, so a post that
// writes `:-: ref` as an example of the syntax carries no marker.
//
// This is the detection counterpart of [ParseDirectives]: it reports syntax
// without resolving, validating names, or requiring a block to be closed. A
// document that declares it holds no directives is checked with this, because
// parsing it would fail on the very markers the check exists to report.
func FindDirectiveMarkers(content string) []Marker {
	var markers []Marker
	fenceChar := byte(0)
	fenceLen := 0

	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	for lineIdx, line := range lines {
		stripped := util.PythonStrip(line)
		if m := fenceRe.FindStringSubmatch(stripped); m != nil {
			fence := m[1]
			if fenceChar == 0 {
				fenceChar = fence[0]
				fenceLen = len(fence)
			} else if fence[0] == fenceChar && len(fence) >= fenceLen {
				fenceChar = 0
				fenceLen = 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}

		masked, _ := MaskBacktickSpans(line)
		lineNum := lineIdx + 1

		if m := markerLineRe.FindStringSubmatch(util.PythonStrip(masked)); m != nil {
			markers = append(markers, Marker{LineNumber: lineNum, Marker: m[1]})
			continue
		}

		for range inlineMarkerRe.FindAllStringIndex(masked, -1) {
			markers = append(markers, Marker{LineNumber: lineNum, Marker: ":-:"})
		}
	}

	return markers
}

// resolveLineInline resolves the inline :-: directives in one line, skipping
// backtick code spans.
func resolveLineInline(line string, resolver Resolver) (string, error) {
	masked, placeholders := MaskBacktickSpans(line)

	var b strings.Builder
	prev := 0
	for _, loc := range inlineRe.FindAllStringSubmatchIndex(masked, -1) {
		name := masked[loc[2]:loc[3]]
		attrs, _ := parseAttrs(masked[loc[4]:loc[5]])
		result, err := resolver(name, attrs, []string{})
		if err != nil {
			return "", err
		}
		if strings.Contains(result, "\n") {
			return "", &InlineOutputError{Name: name}
		}
		b.WriteString(masked[prev:loc[0]])
		b.WriteString(result)
		prev = loc[1]
	}
	b.WriteString(masked[prev:])

	return UnmaskBacktickSpans(b.String(), placeholders), nil
}

// resolveInlinePass is pass 2: scan the resolved output lines for inline :-:
// directives and resolve them.
//
// Lines inside fenced code blocks are passed through, tracked with the same
// fence pattern walkBlocks uses, and so are directives inside backtick code
// spans on each line.
func resolveInlinePass(output []string, resolver Resolver) ([]string, error) {
	result := make([]string, 0, len(output))
	fenceChar := byte(0)
	fenceLen := 0

	for i, line := range output {
		n := i + 1
		stripped := util.PythonStrip(line)

		if m := fenceRe.FindStringSubmatch(stripped); m != nil {
			marker := m[1]
			if fenceChar == 0 {
				fenceChar = marker[0]
				fenceLen = len(marker)
			} else if marker[0] == fenceChar && len(marker) >= fenceLen {
				fenceChar = 0
				fenceLen = 0
			}
			result = append(result, line)
			continue
		}

		if fenceChar != 0 {
			result = append(result, line)
			continue
		}

		if err := refuseMalformedInlineNames(line, n); err != nil {
			return nil, err
		}

		if inlineRe.MatchString(line) {
			resolved, err := resolveLineInline(line, resolver)
			if err != nil {
				return nil, err
			}
			line = resolved
		}
		result = append(result, line)
	}
	return result, nil
}

// malformedTokenRe finds each :-: token on a line so its name can be judged.
var malformedTokenRe = regexp.MustCompile(`:-:` + pySpaceClass + `+(` + pyNotSpaceClass + `+)`)

// refuseMalformedInlineNames refuses a directive name that starts with a
// letter but is not grammatical, before resolution silently leaves it in the
// output.
//
// A token like "name)" is fine -- a valid name with trailing punctuation --
// but "my.directive" is malformed, because word characters follow the invalid
// character. Backtick spans are masked, so a directive inside a code span is
// not judged.
func refuseMalformedInlineNames(line string, lineNum int) error {
	if !strings.Contains(line, ":-: ") {
		return nil
	}
	checkLine, _ := MaskBacktickSpans(line)
	for _, m := range malformedTokenRe.FindAllStringSubmatch(checkLine, -1) {
		token := m[1]
		first, _ := utf8.DecodeRuneInString(token)
		if !unicode.IsLetter(first) {
			continue
		}
		if directiveNameRe.MatchString(token) {
			continue
		}
		// The token starts with a letter but is not wholly grammatical.
		// Measure the grammatical prefix: word characters after it mean a
		// malformed name rather than trailing punctuation. A first letter
		// outside ASCII matches no prefix at all, which leaves the whole
		// token as the remainder.
		remainder := token
		if prefix := directiveNamePrefixRe.FindString(token); prefix != "" {
			remainder = token[len(prefix):]
		}
		if wordCharRe.MatchString(remainder) {
			return directiveErrorf(
				"Malformed directive name '%s' at line %d: "+
					"names must match [a-zA-Z][\\w-]*", token, lineNum)
		}
	}
	return nil
}

// ResolveDirectives replaces each directive in content with the output of
// resolver, leaving non-directive content unchanged.
//
// Directives inside fenced code blocks are left as they stand -- they are not
// directives. Resolution runs in two passes, exactly as parsing does: the
// block machine first, then an inline pass over the resolver's own output, so
// a directive that expands to text carrying an inline directive resolves that
// one too.
func ResolveDirectives(content string, resolver Resolver, validNames NameSet) (string, error) {
	events, err := walkBlocks(content, validNames)
	if err != nil {
		return "", err
	}

	var output []string
	for _, ev := range events {
		switch ev.kind {
		case evLine:
			output = append(output, ev.line)
		case evDirective:
			resolved, rerr := resolver(ev.name, ev.attrs, ev.body)
			if rerr != nil {
				return "", rerr
			}
			output = append(output, strings.Split(resolved, "\n")...)
		case evUnclosed:
			return "", directiveErrorf("Unclosed directive '%s' during resolution", ev.name)
		}
	}

	output, err = resolveInlinePass(output, resolver)
	if err != nil {
		return "", err
	}

	return strings.Join(output, "\n"), nil
}
