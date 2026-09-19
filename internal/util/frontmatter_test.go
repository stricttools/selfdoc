package util

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/scripts"
)

// TestReadFrontmatterAcceptsTOML drives the reader with the blocks a page and
// a post carry, and pins the Go value each declared lexeme class binds to.
func TestReadFrontmatterAcceptsTOML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		kind       Kind
		want       Frontmatter
		wantKeys   []string
		wantBody   string
		wantLine   int
		wantFields []FrontmatterField
	}{
		{
			name:     "no block at all",
			input:    "# Title\n\nBody\n",
			kind:     KindPage,
			want:     Frontmatter{},
			wantBody: "# Title\n\nBody\n",
			wantLine: 0,
		},
		{
			name:     "empty block",
			input:    "+++\n+++\nBody",
			kind:     KindPage,
			want:     Frontmatter{},
			wantBody: "Body",
			wantLine: 2,
		},
		{
			name: "every declared lexeme class",
			input: "+++\n" +
				"title = \"Hello\"\n" +
				"nav_order = 12\n" +
				"draft = true\n" +
				"feed = false\n" +
				"date = 2026-09-14\n" +
				"tags = [\"a\", \"b\"]\n" +
				"+++\n\n\nbody\n",
			kind: KindPage,
			want: Frontmatter{
				"title":     "Hello",
				"nav_order": int64(12),
				"draft":     true,
				"feed":      false,
				"date":      "2026-09-14",
				"tags":      []string{"a", "b"},
			},
			wantKeys: []string{"title", "nav_order", "draft", "feed", "date", "tags"},
			wantFields: []FrontmatterField{
				{Key: "title", Value: "Hello"},
				{Key: "nav_order", Value: int64(12)},
				{Key: "draft", Value: true},
				{Key: "feed", Value: false},
				{Key: "date", Value: FrontmatterDate("2026-09-14")},
				{Key: "tags", Value: []string{"a", "b"}},
			},
			wantBody: "body\n",
			wantLine: 10,
		},
		{
			name: "a value carrying a colon and a double quote",
			input: "+++\n" +
				"title = \"selfdoc vs the Competition: Generators Compared\"\n" +
				"description = \"She said \\\"hello\\\" twice\"\n" +
				"+++\nB",
			kind: KindPage,
			want: Frontmatter{
				"title":       "selfdoc vs the Competition: Generators Compared",
				"description": `She said "hello" twice`,
			},
			wantBody: "B",
			wantLine: 4,
		},
		{
			name:  "a post carrying everything it must",
			input: "+++\ntitle = \"P\"\ndate = 2026-01-02\ndirectives = false\n+++\nB",
			kind:  KindPost,
			want: Frontmatter{
				"title": "P", "date": "2026-01-02", "directives": false,
			},
			wantBody: "B",
			wantLine: 5,
		},
		{
			name:     "a comment inside the block is not a key",
			input:    "+++\n# a note\ntitle = \"T\"\n+++\nB",
			kind:     KindPage,
			want:     Frontmatter{"title": "T"},
			wantKeys: []string{"title"},
			wantBody: "B",
			wantLine: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReadFrontmatter(tt.input, "page.md", tt.kind)
			if err != nil {
				t.Fatalf("ReadFrontmatter: %v", err)
			}
			if !reflect.DeepEqual(got.Values, tt.want) {
				t.Errorf("values = %#v, want %#v", got.Values, tt.want)
			}
			if got.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", got.Body, tt.wantBody)
			}
			if got.Consumed != tt.wantLine {
				t.Errorf("consumed = %d, want %d", got.Consumed, tt.wantLine)
			}
			if tt.wantKeys != nil && !reflect.DeepEqual(got.Keys(), tt.wantKeys) {
				t.Errorf("keys = %#v, want %#v", got.Keys(), tt.wantKeys)
			}
			if tt.wantFields != nil && !reflect.DeepEqual(got.Fields, tt.wantFields) {
				t.Errorf("fields = %#v, want %#v", got.Fields, tt.wantFields)
			}
		})
	}
}

// TestTheRetiredFenceRefusalPrintsAnExecutableRemedy asserts that the remedy
// the refusal prints can be executed as printed by the repository holding the
// document.
//
// The converter lives in selfdoc's own checkout and no release artifact carries
// it, so a refusal naming "scripts/<name>" named a path the refused repository
// does not have: the printed remedy has to fetch the script before it runs it.
func TestTheRetiredFenceRefusalPrintsAnExecutableRemedy(t *testing.T) {
	t.Parallel()
	_, err := ReadFrontmatter("---\ntitle: Hello\n---\nBody\n", "page.md", KindPage)
	if err == nil {
		t.Fatal("a retired block was accepted")
	}
	message := err.Error()
	for _, want := range []string{
		scripts.Fetch(scripts.ConvertFrontmatter),
		scripts.Run(scripts.ConvertFrontmatter, "--dry-run"),
		scripts.Run(scripts.ConvertFrontmatter, "--apply"),
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not print %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, scripts.RepoPath(scripts.ConvertFrontmatter)+" --") {
		t.Errorf("the refusal tells the repository to run a path it does not have:\n%s", message)
	}
}

// TestReadFrontmatterRefusals covers every block the reader will not accept,
// and what each refusal has to say.
func TestReadFrontmatterRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		kind      Kind
		wantParts []string
	}{
		{
			name:      "the retired fence names the converter and the page",
			input:     "---\ntitle: Hello\n---\nBody\n",
			kind:      KindPage,
			wantParts: []string{"page.md", "---", "+++", scripts.ConvertFrontmatter},
		},
		{
			name:      "an unclosed fence is refused rather than ignored",
			input:     "+++\ntitle = \"X\"\n\nBody\n",
			kind:      KindPage,
			wantParts: []string{"page.md", "never closes"},
		},
		{
			name:      "an undeclared key is refused",
			input:     "+++\ntitle = \"X\"\nbogus = 1\n+++\nB",
			kind:      KindPage,
			wantParts: []string{"page.md", "Unknown key bogus"},
		},
		{
			name:      "order is refused, having collapsed into nav_order",
			input:     "+++\norder = 5\n+++\nB",
			kind:      KindPage,
			wantParts: []string{"Unknown key order"},
		},
		{
			name:      "project is refused",
			input:     "+++\nproject = \"x\"\n+++\nB",
			kind:      KindPage,
			wantParts: []string{"Unknown key project"},
		},
		{
			name:      "a reader-supplied key may not be written by an author",
			input:     "+++\ndocument_kind = \"post\"\n+++\nB",
			kind:      KindPage,
			wantParts: []string{"document_kind", "which the reader supplies"},
		},
		{
			name:      "a date written as anything but a date is refused",
			input:     "+++\ntitle = \"P\"\ndate = \"nope\"\ndirectives = false\n+++\nB",
			kind:      KindPost,
			wantParts: []string{"$.date", "Expected a date"},
		},
		{
			name:      "a post with no title is refused",
			input:     "+++\ndate = 2026-01-02\ndirectives = false\n+++\nB",
			kind:      KindPost,
			wantParts: []string{"title", "document_kind == \"post\""},
		},
		{
			name:      "a page with no title is accepted, the same block",
			input:     "+++\ndate = 2026-01-02\n+++\nB",
			kind:      KindPage,
			wantParts: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadFrontmatter(tt.input, "page.md", tt.kind)
			if tt.wantParts == nil {
				if err != nil {
					t.Fatalf("ReadFrontmatter: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ReadFrontmatter accepted the block")
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("refusal %q does not mention %q", err.Error(), part)
				}
			}
		})
	}
}

// TestFrontmatterErrorNamesTheKey pins the lift of a diagnostic's key out of
// the validator's message, which is what the post lints are coded by.
func TestFrontmatterErrorNamesTheKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		kind  Kind
		field string
	}{
		{"missing title", "+++\ndate = 2026-01-02\ndirectives = false\n+++\nB", KindPost, "title"},
		{"missing date", "+++\ntitle = \"P\"\ndirectives = false\n+++\nB", KindPost, "date"},
		{"missing directives", "+++\ntitle = \"P\"\ndate = 2026-01-02\n+++\nB", KindPost, "directives"},
		{"mistyped date", "+++\ntitle = \"P\"\ndate = 7\ndirectives = false\n+++\nB", KindPost, "date"},
		{"mistyped directives", "+++\ntitle = \"P\"\ndate = 2026-01-02\ndirectives = 1\n+++\nB", KindPost, "directives"},
		{"unknown key", "+++\nbogus = 1\n+++\nB", KindPage, "bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadFrontmatter(tt.input, "post.md", tt.kind)
			var blockErr *FrontmatterError
			if !errors.As(err, &blockErr) {
				t.Fatalf("want a *FrontmatterError, got %v", err)
			}
			if !blockErr.Names(tt.field) {
				t.Fatalf("refusal %q does not name %q", err.Error(), tt.field)
			}
		})
	}
}

// TestSplitFrontmatterIsTheOneFenceReader pins the split every caller that
// wants only the body goes through -- the staleness pass and the editor's
// spelling analysis among them.
func TestSplitFrontmatterIsTheOneFenceReader(t *testing.T) {
	t.Parallel()
	block, body, consumed, err := SplitFrontmatter("+++\na = 1\n+++\n\nB\n", "p.md")
	if err != nil {
		t.Fatalf("SplitFrontmatter: %v", err)
	}
	if block != "a = 1" || body != "B\n" || consumed != 4 {
		t.Fatalf("got (%q, %q, %d)", block, body, consumed)
	}
	if _, err := StripFrontmatter("---\na: 1\n---\nB\n", "p.md"); err == nil {
		t.Fatal("StripFrontmatter accepted the retired fence")
	}
}

// TestRenderFrontmatterRoundTrips writes a block through the emitter surface
// and reads it back, so every emitter's escaping is the reader's.
func TestRenderFrontmatterRoundTrips(t *testing.T) {
	t.Parallel()
	fields := []FrontmatterField{
		{Key: "title", Value: `A "quoted" title: with a colon`},
		{Key: "date", Value: FrontmatterDate("2026-09-14")},
		{Key: "nav_order", Value: int64(3)},
		{Key: "draft", Value: false},
		{Key: "tags", Value: []string{"a b", `c"d`}},
	}
	rendered, err := RenderFrontmatter(fields)
	if err != nil {
		t.Fatalf("RenderFrontmatter: %v", err)
	}
	if !strings.HasPrefix(rendered, "+++\n") || !strings.HasSuffix(rendered, "+++\n") {
		t.Fatalf("rendered block is not fenced: %q", rendered)
	}
	block, err := ReadFrontmatter(rendered+"B\n", "p.md", KindPage)
	if err != nil {
		t.Fatalf("reading back what was rendered: %v", err)
	}
	if !reflect.DeepEqual(block.Fields, fields) {
		t.Errorf("round trip = %#v, want %#v", block.Fields, fields)
	}
}

// TestRenderFrontmatterRefusesWhatItCannotSpell keeps an emitter from writing
// a value the schema declares no lexeme class for.
func TestRenderFrontmatterRefusesWhatItCannotSpell(t *testing.T) {
	t.Parallel()
	if _, err := RenderFrontmatter([]FrontmatterField{
		{Key: "tags", Value: map[string]string{"a": "b"}},
	}); err == nil {
		t.Fatal("RenderFrontmatter accepted a value it has no spelling for")
	}
	if _, err := RenderFrontmatter([]FrontmatterField{
		{Key: "date", Value: FrontmatterDate("14-09-2026")},
	}); err == nil {
		t.Fatal("RenderFrontmatter accepted a date that is not a local date")
	}
}

func TestParsePythonInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"12", 12, true},
		{"-0", 0, true},
		{"+5", 5, true},
		{"1_000", 1000, true},
		{"1__0", 0, false},
		{"_1", 0, false},
		{"1_", 0, false},
		{"0x10", 0, false},
		{"1.5", 0, false},
		{"", 0, false},
		// Python's arbitrary-precision int accepts this; int64 does not.
		{"9223372036854775808", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, ok := ParsePythonInt(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Errorf("ParsePythonInt(%q) = (%d, %t), want (%d, %t)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParsePythonFloat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1.5", 1.5, true},
		{".5", 0.5, true},
		{"1e3", 1000, true},
		{"1E-3", 0.001, true},
		{"-1.25", -1.25, true},
		{"1_000.5", 1000.5, true},
		{"5.", 5, true},
		{"inf", 0, true},
		{"-Infinity", 0, true},
		{"0x1p-2", 0, false},
		{"1d", 0, false},
		{"", 0, false},
		{".", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, ok := ParsePythonFloat(tt.in)
			if ok != tt.ok {
				t.Fatalf("ParsePythonFloat(%q) ok = %t, want %t", tt.in, ok, tt.ok)
			}
			// The infinities are checked by their spelling, not their value.
			if ok && tt.want != 0 && got != tt.want {
				t.Errorf("ParsePythonFloat(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestParsePythonFloatRejectsHex records the one place Go's parser is more
// permissive than Python's: Python's float() has no hexadecimal syntax.
func TestParsePythonFloatRejectsHex(t *testing.T) {
	t.Parallel()
	if _, ok := ParsePythonFloat("0x1p-2"); ok {
		t.Error("ParsePythonFloat accepted a hexadecimal float literal")
	}
}

// TestFrontmatterKeyRowsAreTheSchema pins the derivation itself: every key the
// schema declares appears exactly once, in the schema's own order, and the two
// keys the reader supplies appear not at all.
func TestFrontmatterKeyRowsAreTheSchema(t *testing.T) {
	t.Parallel()
	rows, err := FrontmatterKeyRows()
	if err != nil {
		t.Fatalf("FrontmatterKeyRows: %v", err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Key] {
			t.Errorf("%s appears twice in the registry", row.Key)
		}
		seen[row.Key] = true
		if row.Type == "" {
			t.Errorf("%s declares no type", row.Key)
		}
		if row.Description == "" {
			t.Errorf("%s declares no description, which the table renders", row.Key)
		}
	}
	for _, reserved := range readerSuppliedKeys {
		if seen[reserved] {
			t.Errorf("the table offers %q, which the reader supplies", reserved)
		}
	}
	// The three keys a post must carry are the schema's conditional-required
	// constraints, read back through the same derivation the table renders.
	for _, key := range []string{"title", "date", "directives"} {
		required := false
		for _, row := range rows {
			if row.Key == key {
				required = row.RequiredOnAPost
			}
		}
		if !required {
			t.Errorf("%s is not marked required on a post", key)
		}
	}
	// Every key the reader accepts is in the table. A block carrying one the
	// table omits would be undocumented.
	block, err := ReadFrontmatter("+++\nglossary_links = false\n+++\nB", "p.md", KindPage)
	if err != nil {
		t.Fatalf("reading a declared key: %v", err)
	}
	if _, ok := block.Values["glossary_links"]; !ok || !seen["glossary_links"] {
		t.Error("glossary_links is readable but not in the rendered registry")
	}
}
