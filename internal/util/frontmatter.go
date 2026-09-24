package util

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tomledit "github.com/smm-h/go-toml-edit"
	"github.com/stricttools/selfdoc/internal/scripts"
	"github.com/stricttools/strictspec/go/strictspec"
)

// Fence opens and closes a frontmatter block. Everything between the opening
// fence line and the next line that is exactly the fence is TOML.
const Fence = "+++"

// RetiredFence opened the hand-parsed block selfdoc read before the TOML
// format. A document still opening with it is refused by name, with the
// converter that rewrites it -- there is no second reader and no fallback.
const RetiredFence = "---"

// ConverterScript is the dry-run-capable script that rewrites a retired block
// into a TOML one, as it is spelled inside selfdoc's own checkout. A refusal
// prints the fetch-and-run commands instead, because the repository holding the
// refused document does not have this path.
var ConverterScript = scripts.RepoPath(scripts.ConvertFrontmatter)

// Frontmatter is the read surface of a parsed frontmatter block: the declared
// keys mapped to their values.
//
// A value is one of string, bool, int64, float64 or []string -- the Go
// spellings of the lexeme classes the frontmatter schema declares. A date is
// carried as the string it was written as ("2026-09-14"), so every reader of a
// date reads a string; the WRITE surface distinguishes it (see
// [FrontmatterField] and [FrontmatterDate]).
type Frontmatter = map[string]any

// Kind is the document kind a block is read as. It selects which of the two
// kinds the frontmatter schema validates the block against, and it is supplied
// by the call site rather than written by an author: the docs walk reads pages,
// the post discovery reads posts.
type Kind string

const (
	// KindPage is a page under a project's docs directory. Every key is
	// optional.
	KindPage Kind = "page"
	// KindPost is a post under a project's posts directory. A post must
	// carry a title, a date and a directive declaration.
	KindPost Kind = "post"
)

// readerSuppliedKeys are the keys [ReadFrontmatter] appends to a block before
// validating it. An author writing one of them is refused, so each has exactly
// one writer.
var readerSuppliedKeys = []string{"format_version", "document_kind"}

// blockFormatVersion is the value every frontmatter block's format-version
// gate carries. The reader supplies it; no author writes it.
const blockFormatVersion = 1

// FrontmatterDate is a frontmatter date value on the WRITE surface: it renders
// as a bare TOML local date rather than a quoted string.
//
// The read surface carries a date as a plain string, because every reader of a
// date wants its text. This type exists so a block read and written back out
// keeps the spelling it had.
type FrontmatterDate string

// FrontmatterField is one key and its value, in the order the block writes it.
//
// Value is one of string, bool, int64, float64, []string or [FrontmatterDate];
// [RenderFrontmatter] refuses anything else rather than guessing a spelling.
type FrontmatterField struct {
	Key   string
	Value any
}

// Block is a parsed frontmatter block and the body behind it.
type Block struct {
	// Values is the read surface: every declared key mapped to its value.
	Values Frontmatter
	// Fields is the write surface: the same keys in the order the block
	// wrote them, each carrying the value in the form it renders back as.
	Fields []FrontmatterField
	// Body is the document behind the block, with its leading blank lines
	// removed.
	Body string
	// Consumed is the number of source lines before the body's first line --
	// the opening fence, the block, the closing fence and any blank lines
	// stripped after it -- so a caller can map a body line number back to
	// its line in the source. It is zero when the document carries no block.
	Consumed int
}

// Keys returns the block's keys in the order it wrote them.
func (b Block) Keys() []string {
	keys := make([]string, 0, len(b.Fields))
	for _, field := range b.Fields {
		keys = append(keys, field.Key)
	}
	return keys
}

// FrontmatterDiagnostic is one refusal of a frontmatter block.
type FrontmatterDiagnostic struct {
	// Code is the validator's error code, empty for a refusal the reader
	// raised before validation.
	Code string
	// Path is the document path the diagnostic sits at: "$" for the block
	// itself, "$.<key>" for one key.
	Path string
	// Field is the key the diagnostic is about, empty when it names none.
	// It is the key from Path when the path names one, and otherwise the key
	// lifted out of the message.
	Field string
	// Message is the diagnostic's own prose.
	Message string
}

// FrontmatterError is a block a document may not carry.
type FrontmatterError struct {
	// Source is the document the block came from, as its caller names it.
	Source string
	// Kind is the document kind the block was read as.
	Kind Kind
	// Summary is the one-line statement of what is wrong.
	Summary string
	// Diagnostics are the validator's refusals, empty for a refusal the
	// reader raised before validation.
	Diagnostics []FrontmatterDiagnostic
}

func (e *FrontmatterError) Error() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %s", e.Source, e.Summary)
	for _, d := range e.Diagnostics {
		fmt.Fprintf(&out, "\n  %s: %s", d.Path, d.Message)
		if d.Code != "" {
			fmt.Fprintf(&out, " [%s]", d.Code)
		}
	}
	return out.String()
}

// Names reports whether any diagnostic is about the given key.
func (e *FrontmatterError) Names(field string) bool {
	for _, d := range e.Diagnostics {
		if d.Field == field {
			return true
		}
	}
	return false
}

// HasCode reports whether any diagnostic carries the given validator code.
func (e *FrontmatterError) HasCode(code string) bool {
	for _, d := range e.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// SplitFrontmatter splits a document into its raw frontmatter block and its
// body, without reading either.
//
// This is the whole of the fence handling, and the only place that knows what a
// fence looks like: [ReadFrontmatter] validates what this returns, and a caller
// that wants nothing but the body -- the staleness pass hashing a page's own
// text -- calls this directly instead of carrying a second reading of the same
// fence.
//
// A document whose first line is not the fence carries no block: the block is
// empty, the body is the document, and consumed is zero. A document that opens
// the fence and never closes it is refused, as is one opening the retired
// fence. source names the document in every refusal.
func SplitFrontmatter(text, source string) (block, body string, consumed int, err error) {
	lines := strings.Split(text, "\n")
	first := strings.TrimSpace(lines[0])
	if first == RetiredFence {
		return "", "", 0, &FrontmatterError{
			Source: source,
			Summary: fmt.Sprintf(
				"frontmatter opens with the retired %q fence. Frontmatter is TOML "+
					"between %q fences; fetch the converter and run it: `%s`, "+
					"then `%s`, then `%s`",
				RetiredFence, Fence,
				scripts.Fetch(scripts.ConvertFrontmatter),
				scripts.Run(scripts.ConvertFrontmatter, "--dry-run"),
				scripts.Run(scripts.ConvertFrontmatter, "--apply")),
		}
	}
	if first != Fence {
		return "", text, 0, nil
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == Fence {
			end = index
			break
		}
	}
	if end == -1 {
		return "", "", 0, &FrontmatterError{
			Source: source,
			Summary: fmt.Sprintf(
				"frontmatter opens with %q and never closes it: the block ends at the "+
					"next line that is exactly %q", Fence, Fence),
		}
	}
	rest := strings.Join(lines[end+1:], "\n")
	body = strings.TrimLeft(rest, "\n")
	return strings.Join(lines[1:end], "\n"), body, end + 1 + (len(rest) - len(body)), nil
}

// StripFrontmatter returns a document's body: everything behind its frontmatter
// block.
func StripFrontmatter(text, source string) (string, error) {
	_, body, _, err := SplitFrontmatter(text, source)
	return body, err
}

// ReadFrontmatter reads a document's frontmatter block as the given kind.
//
// The block is TOML, validated against the frontmatter schema by the generated
// validator: every key it may carry is declared there, an undeclared key is
// refused rather than ignored, and each value's lexeme class is the declared
// one. A post's title, date and directive declaration are required by the same
// schema, so the surfaces that report a missing one read the verdict here
// rather than checking the fact again.
//
// source names the document in every refusal.
func ReadFrontmatter(text, source string, kind Kind) (Block, error) {
	raw, body, consumed, err := SplitFrontmatter(text, source)
	if err != nil {
		return Block{}, err
	}
	for _, reserved := range readerSuppliedKeys {
		if declaredKey(raw, reserved) {
			return Block{}, &FrontmatterError{
				Source: source,
				Kind:   kind,
				Summary: fmt.Sprintf(
					"frontmatter declares %q, which the reader supplies: remove it",
					reserved),
			}
		}
	}

	augmented := raw
	if augmented != "" && !strings.HasSuffix(augmented, "\n") {
		augmented += "\n"
	}
	augmented += fmt.Sprintf("format_version = %d\ndocument_kind = %s\n",
		blockFormatVersion, tomledit.QuoteString(string(kind)))

	if _, diags := ValidateBytes([]byte(augmented), "toml"); len(diags) > 0 {
		return Block{}, &FrontmatterError{
			Source:      source,
			Kind:        kind,
			Summary:     "frontmatter is not a valid " + string(kind) + " block",
			Diagnostics: liftDiagnostics(diags),
		}
	}

	value, loadErr := strictspec.LoadValue([]byte(augmented), "toml")
	if loadErr != nil {
		// Unreachable: the document just validated, which parses it.
		return Block{}, &FrontmatterError{
			Source: source, Kind: kind, Summary: loadErr.Error()}
	}

	block := Block{Values: Frontmatter{}, Body: body, Consumed: consumed}
	for _, entry := range value.Entries() {
		if entry.Key == "format_version" || entry.Key == "document_kind" {
			continue
		}
		read, written := bindValue(entry.Value)
		block.Values[entry.Key] = read
		block.Fields = append(block.Fields, FrontmatterField{Key: entry.Key, Value: written})
	}
	return block, nil
}

// bindValue converts one validated frontmatter value into its read spelling and
// its write spelling. They differ for a date alone: read as the text every
// consumer of a date wants, written back as the bare local date it was.
func bindValue(v strictspec.Value) (read any, written any) {
	switch v.Kind() {
	case strictspec.KindString:
		s, _ := v.AsString()
		return s, s
	case strictspec.KindInteger:
		n, _ := v.Int()
		return n, n
	case strictspec.KindFloat:
		f, _ := v.Float()
		return f, f
	case strictspec.KindBool:
		b, _ := v.Bool()
		return b, b
	case strictspec.KindDate, strictspec.KindDatetime, strictspec.KindTime:
		lexeme, _ := v.Datetime()
		return lexeme, FrontmatterDate(lexeme)
	case strictspec.KindArray:
		items := make([]string, 0, len(v.Items()))
		for _, item := range v.Items() {
			s, _ := item.AsString()
			items = append(items, s)
		}
		return items, items
	}
	return nil, nil
}

// fieldInMessage lifts the key a diagnostic is about out of its message, for
// the diagnostics whose path names the record rather than the key.
//
// It is pinned to the validator's message templates -- "Missing required field
// <key> at <path>.", "Field <key> at <path> is required when ...", "Unknown key
// <key> at <path>." -- which is the only handle the runtime offers: the public
// diagnostic carries the code, the path and the rendered message, and not the
// slots the message was rendered from. A template that moves leaves the key
// unnamed rather than misnamed, and the tests that assert a named key are what
// report the move.
var fieldInMessage = regexp.MustCompile(
	`^(?:Missing required field|Field|Unknown key) ([A-Za-z_][A-Za-z0-9_]*)\b`)

// liftDiagnostics converts validator diagnostics into the reader's own, lifting
// the key each one is about.
func liftDiagnostics(diags []strictspec.Diagnostic) []FrontmatterDiagnostic {
	out := make([]FrontmatterDiagnostic, 0, len(diags))
	for _, d := range diags {
		lifted := FrontmatterDiagnostic{Code: d.Code, Path: d.Path, Message: d.Message}
		if key, ok := strings.CutPrefix(d.Path, "$."); ok {
			lifted.Field = key
		} else if match := fieldInMessage.FindStringSubmatch(d.Message); match != nil {
			lifted.Field = match[1]
		}
		out = append(out, lifted)
	}
	return out
}

// declaredKey reports whether a raw block declares key at its top level.
//
// It reads the block's own text rather than a parse, because it runs before
// validation: a block declaring a reader-supplied key must be refused by name
// instead of failing as a duplicate key once the reader appends its own.
func declaredKey(raw, key string) bool {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		if strings.Trim(strings.TrimSpace(name), `"'`) == key {
			return true
		}
	}
	return false
}

// RenderFrontmatter renders fields as a fenced TOML frontmatter block,
// terminated by a newline.
//
// Every value is spelled by go-toml-edit's own renderers, so a key or a string
// this writes escapes the way the library that reads it escapes. A value of a
// type the frontmatter schema declares no lexeme class for is refused rather
// than guessed at.
func RenderFrontmatter(fields []FrontmatterField) (string, error) {
	var out strings.Builder
	out.WriteString(Fence + "\n")
	for _, field := range fields {
		rendered, err := renderFrontmatterValue(field.Value)
		if err != nil {
			return "", fmt.Errorf("frontmatter key %q: %w", field.Key, err)
		}
		out.WriteString(tomledit.QuoteKey(field.Key) + " = " + rendered + "\n")
	}
	out.WriteString(Fence + "\n")
	return out.String(), nil
}

// localDateRe is TOML's local-date lexeme, which a [FrontmatterDate] must be
// written as: rendering one unquoted is only correct when it really is one.
var localDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// renderFrontmatterValue spells one frontmatter value as TOML.
func renderFrontmatterValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return tomledit.QuoteString(typed), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		return tomledit.FormatFloat(typed), nil
	case FrontmatterDate:
		if !localDateRe.MatchString(string(typed)) {
			return "", fmt.Errorf("date %q is not written as YYYY-MM-DD", string(typed))
		}
		return string(typed), nil
	case []string:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, tomledit.QuoteString(item))
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	}
	return "", fmt.Errorf("no TOML spelling for a value of type %T", value)
}

// pythonIntRe is Python's int() literal grammar for a base-10 ASCII string:
// an optional sign, then digits with single underscores allowed between them.
var pythonIntRe = regexp.MustCompile(`^[+-]?[0-9](?:_?[0-9])*$`)

// pythonFloatRe is Python's float() literal grammar: an optional sign, then a
// digit-or-point mantissa with single underscores allowed between digits, then
// an optional decimal exponent. Hexadecimal floats, which Go's parser accepts
// and Python's does not, are excluded by construction.
var pythonFloatRe = regexp.MustCompile(
	`^[+-]?(?:[0-9](?:_?[0-9])*\.?(?:[0-9](?:_?[0-9])*)?|\.[0-9](?:_?[0-9])*)(?:[eE][+-]?[0-9](?:_?[0-9])*)?$`)

// pythonInfNanRe is the rest of Python's float() grammar: the special values,
// case-insensitive, with an optional sign.
var pythonInfNanRe = regexp.MustCompile(`^(?i:[+-]?(?:inf|infinity|nan))$`)

// ParsePythonInt parses s the way Python's int() does for a base-10 string,
// reporting whether it is a valid integer literal. Underscores between digits
// are accepted, as Python accepts them; a hexadecimal or octal prefix is not.
//
// One bound Python does not have: a literal outside the int64 range is
// reported invalid, where Python's arbitrary-precision int would accept it.
func ParsePythonInt(s string) (int64, bool) {
	if !pythonIntRe.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParsePythonFloat parses s the way Python's float() does, reporting whether it
// is a valid float literal. It accepts underscores between digits and the
// signed spellings of inf, infinity and nan, and rejects the hexadecimal float
// syntax Go's own parser would otherwise accept.
func ParsePythonFloat(s string) (float64, bool) {
	if !pythonFloatRe.MatchString(s) && !pythonInfNanRe.MatchString(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, "_", ""), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// FrontmatterKeyRow is one row of the frontmatter key registry as the
// documentation renders it.
type FrontmatterKeyRow struct {
	// Key is the key's name.
	Key string
	// Type is its declared lexeme class, spelled for a reader.
	Type string
	// RequiredOnAPost reports whether a post must carry it.
	RequiredOnAPost bool
	// Description is the key's declared description.
	Description string
}

// FrontmatterKeyRows answers the key registry, in the order the schema
// declares it, with the reader-supplied trailer left out: the two keys it
// carries are the reader's and never an author's, so they belong in prose
// rather than in a table of what a page may write.
//
// The rows are DERIVED from the schema the generated validator embeds, so the
// documentation's table and the validator cannot disagree about which keys
// exist, what type each carries, or which ones a post must have.
func FrontmatterKeyRows() ([]FrontmatterKeyRow, error) {
	values, order, err := DecodeTOMLOrdered([]byte(embeddedSchema[embeddedMainFile]))
	if err != nil {
		return nil, fmt.Errorf("decoding the embedded frontmatter schema: %w", err)
	}
	types, _ := values["types"].(map[string]any)
	root, _ := types["FrontmatterDocument"].(map[string]any)
	fields, _ := root["fields"].(map[string]any)

	requiredOnAPost := map[string]bool{}
	if constraints, ok := root["constraints"].([]map[string]any); ok {
		for _, constraint := range constraints {
			if form, _ := constraint["form"].(string); form != "conditional-required" {
				continue
			}
			when, _ := constraint["when"].(map[string]any)
			if value, _ := when["value"].(string); value != string(KindPost) {
				continue
			}
			if field, _ := constraint["field"].(string); field != "" {
				requiredOnAPost[field] = true
			}
		}
	}

	reserved := map[string]bool{}
	for _, key := range readerSuppliedKeys {
		reserved[key] = true
	}

	var rows []FrontmatterKeyRow
	seen := map[string]bool{}
	for _, path := range order {
		if len(path) != 4 || path[0] != "types" || path[1] != "FrontmatterDocument" ||
			path[2] != "fields" {
			continue
		}
		name := path[3]
		if seen[name] || reserved[name] {
			continue
		}
		seen[name] = true
		field, _ := fields[name].(map[string]any)
		declared, _ := field["type"].(string)
		spelled := declared
		if declared == "array" {
			item, _ := field["item"].(map[string]any)
			itemType, _ := item["type"].(string)
			spelled = "array of " + itemType + "s"
		}
		description, _ := field["description"].(string)
		rows = append(rows, FrontmatterKeyRow{
			Key:             name,
			Type:            spelled,
			RequiredOnAPost: requiredOnAPost[name],
			Description:     description,
		})
	}
	return rows, nil
}

// RenderFrontmatterKeyTable renders the key registry as the Markdown table the
// documentation carries.
func RenderFrontmatterKeyTable() (string, error) {
	rows, err := FrontmatterKeyRows()
	if err != nil {
		return "", err
	}
	lines := []string{
		"| Key | Type | Required on a post | What it declares |",
		"| --- | ---- | ------------------ | ---------------- |",
	}
	for _, row := range rows {
		required := "no"
		if row.RequiredOnAPost {
			required = "yes"
		}
		lines = append(lines, fmt.Sprintf("| `%s` | %s | %s | %s |",
			row.Key, row.Type, required, row.Description))
	}
	return strings.Join(lines, "\n"), nil
}
