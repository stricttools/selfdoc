package golang

import (
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/extractors"
)

// fileContents is a package's files with their text, in the order they were
// read.
//
// Order is what decides which file answers for the package doc, so a Go map
// would make that answer arbitrary.
type fileContents struct {
	names  []string
	byName map[string]string
}

func newFileContents() *fileContents {
	return &fileContents{byName: map[string]string{}}
}

func (f *fileContents) add(name, content string) {
	if _, exists := f.byName[name]; !exists {
		f.names = append(f.names, name)
	}
	f.byName[name] = content
}

func (f *fileContents) len() int { return len(f.names) }

func (f *fileContents) get(name string) string { return f.byName[name] }

var (
	packageDeclRE      = regexp.MustCompile(`^package\s+(` + identifier + `)`)
	packageDeclAnywhRE = regexp.MustCompile(`(?m)^package\s+(` + identifier + `)`)

	declFuncRE  = regexp.MustCompile(`^func\s+(?:\(\s*` + identifier + `\s+\*?(` + identifier + `)\s*\)\s+)?(` + exported + `)(\(.*)`)
	declTypeRE  = regexp.MustCompile(`^type\s+(` + exported + `)\s+(.*)`)
	declConstRE = regexp.MustCompile(`^const\s+(` + exported + `)\s*(.*)`)
	declVarRE   = regexp.MustCompile(`^var\s+(` + exported + `)\s+(.*)`)

	blockConstMemberRE = regexp.MustCompile(`^(` + exported + `)\s*(.*)`)
	blockVarMemberRE   = regexp.MustCompile(`^(` + exported + `)\s+(.*)`)

	structDeclRE = regexp.MustCompile(`^type\s+(` + exported + `)\s+struct\s*\{`)

	structFieldRE = regexp.MustCompile(
		`^([A-Za-z]` + extractors.PyWordClass + `*)\s+` + // field name
			`(` + extractors.PyNonSpaceClass + `+(?:\s*\[.*?\])?)` + // type
			"(?:\\s+(`[^`]*`))?" + // optional struct tag
			`(?:\s*//\s*(.*))?` + // optional inline comment
			`\s*$`,
	)
)

// extractPackageDoc reports a package's name and its doc comment.
//
// The doc is the contiguous // comment block ADJACENT to a package
// declaration: a blank line between the two means the package is
// undocumented, which is the rule go doc applies. The comment that rule
// excludes is usually a generator's "Code generated ... DO NOT EDIT." banner,
// which describes the tool rather than the package and must never become the
// package's documentation.
//
// Files are tried in order, and the first one that carries both a package
// declaration and a comment adjacent to it wins -- Go's convention puts the
// package doc in doc.go or in the package's main file, and every other file
// repeats the bare declaration. A second pass reports the name alone when no
// file documents the package.
func extractPackageDoc(contents *fileContents) (string, string) {
	for _, name := range contents.names {
		lines := strings.Split(contents.get(name), "\n")
		for i, line := range lines {
			stripped := pyStrip(line)
			m := packageDeclRE.FindStringSubmatch(stripped)
			if m == nil {
				continue
			}
			packageName := m[1]
			doc := collectAdjacentCommentBlockAbove(lines, i)
			if doc != "" {
				return packageName, doc
			}
			// The package is declared here but undocumented; keep looking.
			break
		}
	}

	for _, name := range contents.names {
		if m := packageDeclAnywhRE.FindStringSubmatch(contents.get(name)); m != nil {
			return m[1], ""
		}
	}

	return "", ""
}

// collectCommentBlockAbove collects the contiguous // comment lines immediately
// above a line, crossing any blank lines between them.
func collectCommentBlockAbove(lines []string, targetLineIdx int) string {
	return extractors.CollectCommentLinesAbove(lines, targetLineIdx, "//", true)
}

// collectAdjacentCommentBlockAbove collects the contiguous // comment lines
// directly above a line, with no blank line between them. A blank line means
// the comment documents nothing, which is how go doc reads one.
func collectAdjacentCommentBlockAbove(lines []string, targetLineIdx int) string {
	return extractors.CollectCommentLinesAbove(lines, targetLineIdx, "//", false)
}

// goDeclaration is one exported declaration the scanner found.
type goDeclaration struct {
	// Kind is one of const, var, type, func or method, which is also the order
	// a reference page groups them in.
	Kind string
	// Name is the declared name, or Receiver.Name for a method.
	Name string
	// Signature is the declaration line with its trailing opening brace
	// removed.
	Signature string
	// Doc is the doc comment above the declaration.
	Doc string
}

// extractExportedDeclarations scans a Go file for its exported declarations.
//
// The scan is line-by-line and does not track block comments, so a declaration
// commented out inside /* ... */ is still reported -- one of the scanner's
// known behaviors.
func extractExportedDeclarations(source string) []goDeclaration {
	lines := strings.Split(source, "\n")
	var declarations []goDeclaration
	seen := map[string]bool{}

	appendDecl := func(kind, name, signature, doc string) {
		if seen[name] {
			return
		}
		seen[name] = true
		declarations = append(declarations, goDeclaration{
			Kind: kind, Name: name, Signature: signature, Doc: doc,
		})
	}

	for i, line := range lines {
		stripped := pyStrip(line)

		if m := declFuncRE.FindStringSubmatch(stripped); m != nil {
			receiverType, funcName := m[1], m[2]
			signature := pyRStrip(strings.TrimRight(stripped, "{"))
			doc := collectCommentBlockAbove(lines, i)

			if receiverType != "" {
				appendDecl("method", receiverType+"."+funcName, signature, doc)
			} else {
				appendDecl("func", funcName, signature, doc)
			}
			continue
		}

		if m := declTypeRE.FindStringSubmatch(stripped); m != nil {
			signature := pyRStrip(strings.TrimRight(stripped, "{"))
			appendDecl("type", m[1], signature, collectCommentBlockAbove(lines, i))
			continue
		}

		if strings.HasPrefix(stripped, "const (") {
			extractBlock(lines, i, "const", blockConstMemberRE, appendDecl)
			continue
		}

		if m := declConstRE.FindStringSubmatch(stripped); m != nil {
			appendDecl("const", m[1], stripped, collectCommentBlockAbove(lines, i))
			continue
		}

		if strings.HasPrefix(stripped, "var (") {
			extractBlock(lines, i, "var", blockVarMemberRE, appendDecl)
			continue
		}

		if m := declVarRE.FindStringSubmatch(stripped); m != nil {
			appendDecl("var", m[1], stripped, collectCommentBlockAbove(lines, i))
			continue
		}
	}

	return declarations
}

// extractBlock reads the exported members of a parenthesized const or var
// block.
//
// The block's own doc comment is attached to its first exported member, unless
// that member documents itself. A member is anything at the start of a line
// beginning with an uppercase letter, which is why a capitalized field key
// inside a composite literal in a var block is reported as a symbol.
func extractBlock(
	lines []string,
	blockStartIdx int,
	keyword string,
	memberRE *regexp.Regexp,
	appendDecl func(kind, name, signature, doc string),
) {
	blockDoc := collectCommentBlockAbove(lines, blockStartIdx)

	firstExported := true
	for i := blockStartIdx + 1; i < len(lines); i++ {
		stripped := pyStrip(lines[i])
		if strings.HasPrefix(stripped, ")") {
			break
		}

		m := memberRE.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}
		name := m[1]
		doc := collectCommentBlockAbove(lines, i)
		if firstExported && doc == "" && blockDoc != "" {
			doc = blockDoc
		}
		firstExported = false

		appendDecl(keyword, name, keyword+" "+stripped, doc)
	}
}

// goStruct is one exported struct type with its fields.
type goStruct struct {
	Name   string
	Doc    string
	Fields []goStructField
}

// goStructField is one field of a struct as the scanner read it.
type goStructField struct {
	Name string
	Type string
	// Tag is the struct tag with its surrounding backticks removed.
	Tag string
	// Comment is the field's inline comment, or its doc comment above when
	// there is no inline one.
	Comment string
}

// extractStructs scans a Go file for its exported struct type declarations.
func extractStructs(source string) []goStruct {
	lines := strings.Split(source, "\n")
	var structs []goStruct

	for i := 0; i < len(lines); i++ {
		stripped := pyStrip(lines[i])

		m := structDeclRE.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}
		structName := m[1]
		doc := collectCommentBlockAbove(lines, i)

		var fields []goStructField
		for j := i + 1; j < len(lines); j++ {
			fieldLine := pyStrip(lines[j])
			if strings.HasPrefix(fieldLine, "}") {
				break
			}
			if field, ok := parseStructField(fieldLine, lines, j); ok {
				fields = append(fields, field)
			}
		}

		structs = append(structs, goStruct{Name: structName, Doc: doc, Fields: fields})
	}

	return structs
}

// parseStructField reads one struct field line. It declines a blank line, a
// comment line, and anything that is not "Name Type [`tag`] [// comment]".
func parseStructField(fieldLine string, lines []string, lineIdx int) (goStructField, bool) {
	if fieldLine == "" || strings.HasPrefix(fieldLine, "//") {
		return goStructField{}, false
	}

	m := structFieldRE.FindStringSubmatch(fieldLine)
	if m == nil {
		return goStructField{}, false
	}

	tag := m[3]
	inlineComment := m[4]

	description := inlineComment
	if description == "" {
		description = collectCommentBlockAbove(lines, lineIdx)
	}

	return goStructField{
		Name:    m[1],
		Type:    m[2],
		Tag:     strings.Trim(tag, "`"),
		Comment: description,
	}, true
}

// formatStructTable renders a struct's fields as a Markdown table.
func formatStructTable(structInfo goStruct) (string, error) {
	rows := make([][]string, 0, len(structInfo.Fields))
	for _, field := range structInfo.Fields {
		tagDisplay := ""
		if field.Tag != "" {
			tagDisplay = "`" + field.Tag + "`"
		}
		rows = append(rows, []string{
			"`" + field.Name + "`",
			"`" + field.Type + "`",
			tagDisplay,
			field.Comment,
		})
	}
	return extractors.RenderTable([]string{"Field", "Type", "Tag", "Description"}, rows)
}
