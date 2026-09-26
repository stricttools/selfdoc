// Package manifest generates and reads a project's manifest: the JSON record
// of what a build published -- the project's identity and version, its pages
// with their heading anchors, and its posts.
//
// The manifest is what a reader that cannot run the build asks. The assembly
// composes a site out of one manifest per project, the editor draws its
// outline and its link targets from the pages' headings, and the slug
// immutability check reads the committed copy out of git to see what a post
// was published as.
//
// # The document's bytes are the contract
//
// The manifest is committed, so every byte of it shows up in a diff. Its keys
// are written in declaration order rather than sorted, two-space indented,
// with every non-ASCII character escaped -- the shape the Python encoder
// wrote, reproduced here so the port does not rewrite every project's
// committed manifest on its first run. A write is skipped entirely when
// nothing but the generation timestamp would change.
//
// # The reader is tolerant, and reads one schema
//
// Compat is the one door every read path goes through. It takes the fields it
// knows and ignores every other key, so a later selfdoc can add a field without
// breaking this reader. It refuses every schema_version but [SchemaVersion]: a
// higher one declares a document this reader cannot claim to understand, and
// a lower one -- or none -- carries no vocabulary, so reading it would pass off
// "no vocabulary recorded" as "a project that accepts and rejects nothing".
// [Convert] turns a document of the previous schema into this one.
package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/tokenizer"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// DefaultTheme is the theme a project's manifest records when its config
// names none. It is the same default the build applies, and the assembly's
// chrome reads this field to decide which stylesheet a project's pages
// reference.
const DefaultTheme = "minimal"

// SchemaVersion is the manifest format this package writes, and the only one
// it reads. A document declaring another is refused rather than read on this
// version's terms.
const SchemaVersion = 2

// PreviousSchemaVersion is the format before this one: the same document
// without the vocabulary. [Convert] reads it; nothing else does.
const PreviousSchemaVersion = 1

// DefaultOutputName is the filename a project's manifest is written under
// inside selfdoc's generated-state directory.
const DefaultOutputName = "manifest.json"

// Heading is one heading on a page, with the element id the built page
// carries for it.
type Heading struct {
	// Level is the heading level, 1 through 6.
	Level int64
	// Text is the heading's Markdown text, as written.
	Text string
	// Anchor is the element id the heading carries on the built page.
	Anchor string
}

// Page is one documentation page as the manifest records it.
type Page struct {
	// Path is the page's path relative to the docs directory.
	Path string
	// Title is the page's title: its frontmatter title, else its first
	// heading's text.
	Title string
	// Type is the page's declared type, "doc" when it declares none.
	Type string
	// Headings are the page's headings with their final anchors, in
	// document order.
	Headings []Heading
}

// Post is one blog post as the manifest records it.
type Post struct {
	// Path is the post's source path.
	Path string
	// Title is the post's title.
	Title string
	// Date is the post's publication date.
	Date string
	// Slug is the address the post is published at, which never changes
	// once published.
	Slug string
	// Tags are the post's tags.
	Tags []string
}

// AcceptedWord is one word the project's vocabulary accepts, as the manifest
// records it: the word and its aliases, without the meaning.
type AcceptedWord struct {
	// Word is the word as the project's terms file spells it.
	Word string
	// Aliases are other spellings accepted with it.
	Aliases []string
}

// RejectedPattern is one pattern the project's vocabulary rejects, as the
// manifest records it: the pattern and how it matches, without the reason.
type RejectedPattern struct {
	// Pattern is the rejected text.
	Pattern string
	// Kind is how the pattern matches: one of [vocabulary.Kinds].
	Kind string
}

// Vocabulary is the project's own vocabulary as the manifest records it,
// generated from the project's terms file and nothing else. The site it is
// published to reads it to refuse two projects that disagree about a word.
type Vocabulary struct {
	// Accepted are the accepted words, in the terms file's order.
	Accepted []AcceptedWord
	// Rejected are the rejected patterns, in the terms file's order.
	Rejected []RejectedPattern
}

// VocabularyOf is what a manifest records of a project's terms file.
func VocabularyOf(terms vocabulary.List) Vocabulary {
	recorded := Vocabulary{Accepted: []AcceptedWord{}, Rejected: []RejectedPattern{}}
	for _, entry := range terms.Accepted {
		aliases := append([]string{}, entry.Aliases...)
		recorded.Accepted = append(recorded.Accepted, AcceptedWord{Word: entry.Word, Aliases: aliases})
	}
	for _, entry := range terms.Rejected {
		recorded.Rejected = append(recorded.Rejected, RejectedPattern{Pattern: entry.Pattern, Kind: entry.Kind})
	}
	return recorded
}

// Published is the recorded vocabulary in the shape the cross-project check
// reads, under the project's slug.
func (v Vocabulary) Published(slug string) vocabulary.Published {
	published := vocabulary.Published{Project: slug}
	for _, entry := range v.Accepted {
		published.Accepted = append(published.Accepted, vocabulary.Accepted{
			Word: entry.Word, Aliases: append([]string(nil), entry.Aliases...), Source: slug,
		})
	}
	for _, entry := range v.Rejected {
		published.Rejected = append(published.Rejected, vocabulary.Rejected{
			Pattern: entry.Pattern, Kind: entry.Kind, Source: slug,
		})
	}
	return published
}

// Manifest is a project's published record.
type Manifest struct {
	// SchemaVersion is the format the document declared.
	SchemaVersion int64
	// Name is the project's display name.
	Name string
	// Slug is the project's address on the assembly.
	Slug string
	// Version is the project's version.
	Version string
	// Description is the project's one-line description.
	Description string
	// Language is the project's primary source language, "" when it
	// publishes no code.
	Language string
	// BaseURL is the site's base address.
	BaseURL string
	// Pages are the documentation pages the build published.
	Pages []Page
	// Posts are the blog posts the build published.
	Posts []Post
	// LastGen is when the manifest was generated, in ISO-8601 with a UTC
	// offset.
	LastGen string
	// Theme is the theme the build rendered against. The assembly's
	// site-level chrome asset is keyed by it, so a manifest that does not
	// carry one leaves every page referencing the default theme's
	// stylesheet regardless of what its project configured.
	Theme string
	// Vocabulary is the project's own accepted words and rejected patterns.
	Vocabulary Vocabulary
}

// Doc is one resolved docs page in the shape this package reads it.
//
// It is the docs resolution's per-page tuple, narrowed to the three members
// the manifest uses: the frontmatter for the title and the type, the resolved
// content for the headings -- so a directive-generated heading is recorded --
// and the raw content for the title fallback.
type Doc struct {
	// Frontmatter is the page's parsed metadata block.
	Frontmatter util.Frontmatter
	// Resolved is the page's content with every directive resolved.
	Resolved string
	// Raw is the page's pre-resolution template content.
	Raw string
}

// ToKebab converts a name to a kebab-case slug: lowercased, with spaces and
// underscores becoming hyphens, every other non-alphanumeric character
// dropped, runs of hyphens collapsed and the ends trimmed.
func ToKebab(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	var kept strings.Builder
	for _, char := range slug {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			kept.WriteRune(char)
		}
	}
	slug = kept.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	return strings.Trim(slug, "-")
}

// extractTitle returns a page's title: its frontmatter title, else the text
// of its first Markdown heading, else the empty string.
func extractTitle(frontmatter util.Frontmatter, rawContent string) string {
	if title := util.PythonStrOrEmpty(frontmatter["title"]); title != "" {
		return title
	}
	for _, line := range util.PythonSplitLines(rawContent) {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "#") {
			return strings.TrimSpace(strings.TrimLeft(stripped, "#"))
		}
	}
	return ""
}

// pageHeadings returns a page's headings with the anchors the built page
// carries.
//
// The anchors come from the same assignment the HTML renderer and the search
// index use, so a manifest anchor is always an id that exists on the page --
// de-duplicated ("setup", "setup-1") and free of "#" lines inside fenced code
// blocks.
func pageHeadings(frontmatter util.Frontmatter, content string) []Heading {
	// A frontmatter title renames the page's H1, and its anchor with it.
	// Without one the H1's own text is the title, which is what the anchor
	// assignment falls back to.
	var pageTitle *string
	if title := util.PythonStrOrEmpty(frontmatter["title"]); title != "" {
		pageTitle = &title
	}
	headings := []Heading{}
	for _, anchor := range html.AssignHeadingAnchors(tokenizer.Tokenize(content), pageTitle) {
		headings = append(headings, Heading{
			Level:  int64(anchor.Level),
			Text:   anchor.Text,
			Anchor: anchor.Anchor,
		})
	}
	return headings
}

// Generate builds a Manifest from a project's config and its resolved docs,
// and writes it to <outputName> inside selfdoc's generated-state directory.
//
// The write is skipped when everything but the generation timestamp is
// unchanged, so a gen over untouched content does not dirty the working
// tree. The returned Manifest is what was built either way.
func Generate(
	projectConfig map[string]any,
	dirPath string,
	allDocs map[string]Doc,
	posts []Post,
	outputName string,
	handle *effects.Handle,
) (*Manifest, error) {
	absolute, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, err
	}

	// Name: from the config, else the directory's own name.
	name := util.PythonStrOrEmpty(projectConfig["name"])
	if name == "" {
		name = filepath.Base(absolute)
	}

	// Slug: from the topology config, else the kebab-cased name.
	slug := ""
	if topology, ok := projectConfig["topology"].(map[string]any); ok {
		slug = util.PythonStrOrEmpty(topology["slug"])
	}
	if slug == "" {
		slug = ToKebab(name)
	}

	// Version: from the config, else detected from the project's manifests.
	version := util.PythonStrOrEmpty(projectConfig["version"])
	if version == "" {
		version = util.DetectProjectVersion(absolute, "")
	}

	// Language: the primary language, from the first source entry.
	language := ""
	if source, ok := projectConfig["source"].([]any); ok && len(source) > 0 {
		if first, ok := source[0].(map[string]any); ok {
			language = util.PythonStrOrEmpty(first["language"])
		}
	}

	// Theme: what the chrome asset for this project's pages is built from.
	theme := util.PythonStrOrEmpty(projectConfig["theme"])
	if theme == "" {
		theme = DefaultTheme
	}

	pages := []Page{}
	for _, relPath := range sortedKeys(allDocs) {
		doc := allDocs[relPath]
		pageType := "doc"
		if declared, ok := doc.Frontmatter["type"]; ok {
			pageType = util.PythonStrOrEmpty(declared)
		}
		pages = append(pages, Page{
			Path:  relPath,
			Title: extractTitle(doc.Frontmatter, doc.Raw),
			Type:  pageType,
			// The headings are read from the resolved content -- what the
			// renderer sees -- so a directive-generated heading is included.
			Headings: pageHeadings(doc.Frontmatter, doc.Resolved),
		})
	}

	recorded := make([]Post, 0, len(posts))
	for _, post := range posts {
		if post.Tags == nil {
			post.Tags = []string{}
		}
		recorded = append(recorded, post)
	}

	terms, err := vocabulary.LoadProject(dirPath)
	if err != nil {
		return nil, err
	}

	manifest := &Manifest{
		SchemaVersion: SchemaVersion,
		Name:          name,
		Slug:          slug,
		Version:       version,
		Description:   util.PythonStrOrEmpty(projectConfig["description"]),
		Language:      language,
		BaseURL:       util.PythonStrOrEmpty(projectConfig["base_url"]),
		Pages:         pages,
		Posts:         recorded,
		LastGen:       isoUTC(time.Now()),
		Theme:         theme,
		Vocabulary:    VocabularyOf(terms),
	}

	if err := layout.EnsureDir(handle, dirPath, layout.DocsStateRel); err != nil {
		return nil, err
	}
	manifestPath := layout.Path(dirPath, layout.DocsStateRel+"/"+outputName)

	document := manifest.document()
	if unchangedExceptTimestamp(manifestPath, document) {
		return manifest, nil
	}
	if err := handle.AtomicWrite(manifestPath, encode(document), effects.ModeDefault); err != nil {
		return nil, err
	}
	return manifest, nil
}

// unchangedExceptTimestamp reports whether the manifest already on disk
// carries the same content as document, ignoring the generation timestamp.
//
// An unreadable or corrupt file answers false, so it is rewritten.
func unchangedExceptTimestamp(manifestPath string, document object) bool {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}
	decoded, err := config.DecodeDocument(content)
	if err != nil {
		return false
	}
	existing, ok := decoded.(map[string]any)
	if !ok {
		return false
	}
	fresh, ok := plain(document).(map[string]any)
	if !ok {
		return false
	}
	delete(existing, "last_gen")
	delete(fresh, "last_gen")
	return reflect.DeepEqual(existing, fresh)
}

// document renders the manifest as the JSON object the file carries, in the
// key order the file is written in.
func (m *Manifest) document() object {
	pages := make([]any, 0, len(m.Pages))
	for _, page := range m.Pages {
		headings := make([]any, 0, len(page.Headings))
		for _, heading := range page.Headings {
			headings = append(headings, object{
				{"level", heading.Level},
				{"text", heading.Text},
				{"anchor", heading.Anchor},
			})
		}
		pages = append(pages, object{
			{"path", page.Path},
			{"title", page.Title},
			{"type", page.Type},
			{"headings", headings},
		})
	}
	posts := make([]any, 0, len(m.Posts))
	for _, post := range m.Posts {
		tags := make([]any, 0, len(post.Tags))
		for _, tag := range post.Tags {
			tags = append(tags, tag)
		}
		posts = append(posts, object{
			{"path", post.Path},
			{"title", post.Title},
			{"date", post.Date},
			{"slug", post.Slug},
			{"tags", tags},
		})
	}
	return object{
		{"schema_version", m.SchemaVersion},
		{"name", m.Name},
		{"slug", m.Slug},
		{"version", m.Version},
		{"description", m.Description},
		{"language", m.Language},
		{"base_url", m.BaseURL},
		{"pages", pages},
		{"posts", posts},
		{"last_gen", m.LastGen},
		{"theme", m.Theme},
		{"vocabulary", m.Vocabulary.document()},
	}
}

// document renders the vocabulary as the object the file carries.
func (v Vocabulary) document() object {
	accepted := make([]any, 0, len(v.Accepted))
	for _, entry := range v.Accepted {
		aliases := make([]any, 0, len(entry.Aliases))
		for _, alias := range entry.Aliases {
			aliases = append(aliases, alias)
		}
		accepted = append(accepted, object{{"word", entry.Word}, {"aliases", aliases}})
	}
	rejected := make([]any, 0, len(v.Rejected))
	for _, entry := range v.Rejected {
		rejected = append(rejected, object{{"pattern", entry.Pattern}, {"kind", entry.Kind}})
	}
	return object{{"accepted", accepted}, {"rejected", rejected}}
}

// Compat builds a Manifest from a parsed manifest document.
//
// Every read path goes through here: the file reader, the git reader, and the
// assembly's own raw decode. It is a tolerant reader -- it takes the fields
// it knows about and ignores every other key, which is the contract that lets
// a later selfdoc add a field without breaking an older reader.
//
// source names where the document came from, for the error message; pass ""
// when there is nothing useful to name. A schema_version above SchemaVersion
// is an error: this reader cannot honestly read a document whose format it
// does not know. One below it, or none at all, is an [*OutdatedError].
func Compat(data map[string]any, source string) (*Manifest, error) {
	declared, err := declaredVersion(data)
	if err != nil {
		return nil, err
	}
	switch {
	case intOf(declared) > SchemaVersion:
		context := ""
		if source != "" {
			context = " in " + source
		}
		return nil, fmt.Errorf(
			"Unsupported manifest schema_version %s%s (max supported: %d)",
			numberText(declared), context, SchemaVersion)
	case intOf(declared) < SchemaVersion:
		_, present := data["schema_version"]
		return nil, &OutdatedError{Declared: intOf(declared), Absent: !present, Source: source}
	}
	return fromData(data), nil
}

// OutdatedError is a manifest of a schema before [SchemaVersion]: written by an
// older selfdoc, and carrying no vocabulary.
//
// Its message is the one a project's own reader gives, naming the command that
// converts the project's committed manifest. The assembly's reader words its
// own, since there the fix is republishing the project rather than converting
// a file this machine holds.
type OutdatedError struct {
	// Declared is the schema_version the document declared, 1 when it
	// declared none.
	Declared int64
	// Absent reports that the document declared no schema_version at all.
	Absent bool
	// Source names where the document came from.
	Source string
}

func (e *OutdatedError) Error() string {
	where := "The manifest"
	if e.Source != "" {
		where = e.Source
	}
	return fmt.Sprintf(
		"%s %s, and this selfdoc reads manifest schema_version %d only: that version records the project's vocabulary, which an older manifest does not. Convert it with 'selfdoc layout migrate', which rewrites the manifest with the vocabulary of %s and commits it.",
		where, e.Declares(), SchemaVersion, layout.TermsRel)
}

// Declares words what the document declared, for a message.
func (e *OutdatedError) Declares() string {
	if e.Absent {
		return fmt.Sprintf("declares no schema_version, which reads as %d", PreviousSchemaVersion)
	}
	return fmt.Sprintf("declares schema_version %d", e.Declared)
}

// declaredVersion is the document's schema_version, the previous version when
// it declares none. A value that is not a number at all is an error: the
// document declares a format in a spelling no reader can compare.
func declaredVersion(data map[string]any) (any, error) {
	raw, ok := data["schema_version"]
	if !ok {
		return int64(PreviousSchemaVersion), nil
	}
	switch raw.(type) {
	case int64, int, float64:
		return raw, nil
	default:
		return nil, fmt.Errorf(
			"Unsupported manifest schema_version %v (not a number)", raw)
	}
}

// fromData reads every field the manifest carries, without looking at the
// declared schema.
func fromData(data map[string]any) *Manifest {
	declared, _ := declaredVersion(data)
	return &Manifest{
		SchemaVersion: intOf(declared),
		Name:          stringOf(data["name"]),
		Slug:          stringOf(data["slug"]),
		Version:       stringOf(data["version"]),
		Description:   stringOf(data["description"]),
		Language:      stringOf(data["language"]),
		BaseURL:       stringOf(data["base_url"]),
		Pages:         pagesOf(data["pages"]),
		Posts:         postsOf(data["posts"]),
		LastGen:       stringOf(data["last_gen"]),
		Theme:         util.PythonStrOrEmpty(data["theme"]),
		Vocabulary:    vocabularyOf(data["vocabulary"]),
	}
}

// Convert rewrites a manifest document of [PreviousSchemaVersion] as one of
// [SchemaVersion]: every field it carried, unchanged, plus the vocabulary of
// terms. The generation timestamp is kept, since nothing was generated.
//
// A document of any other schema is refused: this is the one conversion there
// is, and a document already on this schema has nothing to convert.
func Convert(raw []byte, terms vocabulary.List, source string) ([]byte, error) {
	data, err := decodeObject(raw, source)
	if err != nil {
		return nil, err
	}
	declared, err := declaredVersion(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if intOf(declared) != PreviousSchemaVersion {
		return nil, fmt.Errorf(
			"%s declares schema_version %s, and only a manifest of schema_version %d is converted",
			source, numberText(declared), PreviousSchemaVersion)
	}
	converted := fromData(data)
	converted.SchemaVersion = SchemaVersion
	converted.Vocabulary = VocabularyOf(terms)
	return encode(converted.document()), nil
}

// DeclaredSchema answers the schema_version a manifest document declares, the
// previous version when it declares none.
func DeclaredSchema(raw []byte, source string) (int64, error) {
	data, err := decodeObject(raw, source)
	if err != nil {
		return 0, err
	}
	declared, err := declaredVersion(data)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", source, err)
	}
	return intOf(declared), nil
}

// decodeObject decodes a manifest document that must be a JSON object.
func decodeObject(raw []byte, source string) (map[string]any, error) {
	decoded, err := config.DecodeDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	data, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: the manifest is not a JSON object", source)
	}
	return data, nil
}

// Load reads a manifest file and returns what it records.
//
// It returns a nil Manifest and a nil error when the file does not exist --
// "this project has no manifest" is an answer, not a failure.
func Load(path string) (*Manifest, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, err := decodeObject(content, path)
	if err != nil {
		return nil, err
	}
	return Compat(data, path)
}

// LoadFromGit reads the committed manifest out of the repository's HEAD,
// bypassing the working-tree copy.
//
// This is what the slug immutability check compares against: a post's slug
// must match the one it was published under, and by the time the check runs
// gen has already rewritten the on-disk manifest with the new slug.
//
// It returns a nil Manifest and a nil error when the directory is not a
// repository, when the repository has no commits, or when the manifest has
// never been committed.
func LoadFromGit(dirPath string, handle *effects.Handle) (*Manifest, error) {
	manifestRef := "HEAD:" + layout.ManifestRel
	existence, err := handle.Run(
		[]string{"git", "cat-file", "-e", manifestRef},
		effects.Cwd(dirPath),
		effects.CaptureOutput(),
		effects.Timeout(10*time.Second),
		effects.Read(),
	)
	if err != nil {
		return nil, err
	}
	switch existence.ExitCode {
	case 128:
		// Not a repository, or no commits yet -- HEAD does not resolve.
		return nil, nil
	case 1:
		// The manifest has never been committed.
		return nil, nil
	case 0:
	default:
		return nil, fmt.Errorf("Unexpected git error (exit %d): %s",
			existence.ExitCode, strings.TrimSpace(string(existence.Stderr)))
	}

	contents, err := handle.Run(
		[]string{"git", "show", manifestRef},
		effects.Cwd(dirPath),
		effects.CaptureOutput(),
		effects.Timeout(10*time.Second),
		effects.Read(),
	)
	if err != nil {
		return nil, err
	}
	if contents.ExitCode != 0 {
		return nil, fmt.Errorf("Failed to read manifest from git: %s",
			strings.TrimSpace(string(contents.Stderr)))
	}
	source := layout.ManifestRel + " at git HEAD"
	data, err := decodeObject(contents.Stdout, source)
	if err != nil {
		return nil, err
	}
	return Compat(data, source)
}

// -- The document's bytes ---------------------------------------------------

// field is one key and its value, in the position the key is written in.
type field struct {
	key   string
	value any
}

// object is a JSON object that carries its key order, because the manifest's
// keys are written in declaration order rather than sorted: sorting would
// rewrite every committed manifest in the fleet and move the pages into the
// middle of the document.
type object []field

// encode renders a document as the Python encoder wrote it: two-space
// indented, every non-ASCII character escaped, and "{}" / "[]" for an empty
// object or list.
func encode(document object) []byte {
	var out bytes.Buffer
	encodeValue(&out, document, "")
	out.WriteByte('\n')
	return out.Bytes()
}

// encodeValue writes one value at the given indentation.
func encodeValue(out *bytes.Buffer, value any, indent string) {
	switch typed := value.(type) {
	case object:
		if len(typed) == 0 {
			out.WriteString("{}")
			return
		}
		out.WriteString("{\n")
		inner := indent + "  "
		for index, entry := range typed {
			out.WriteString(inner)
			out.WriteString(util.PythonJSONString(entry.key))
			out.WriteString(": ")
			encodeValue(out, entry.value, inner)
			if index < len(typed)-1 {
				out.WriteByte(',')
			}
			out.WriteByte('\n')
		}
		out.WriteString(indent)
		out.WriteByte('}')
	case []any:
		if len(typed) == 0 {
			out.WriteString("[]")
			return
		}
		out.WriteString("[\n")
		inner := indent + "  "
		for index, item := range typed {
			out.WriteString(inner)
			encodeValue(out, item, inner)
			if index < len(typed)-1 {
				out.WriteByte(',')
			}
			out.WriteByte('\n')
		}
		out.WriteString(indent)
		out.WriteByte(']')
	case string:
		out.WriteString(util.PythonJSONString(typed))
	case int64:
		out.WriteString(strconv.FormatInt(typed, 10))
	case bool:
		if typed {
			out.WriteString("true")
			return
		}
		out.WriteString("false")
	case nil:
		out.WriteString("null")
	default:
		// Every value the manifest carries is one of the cases above; a new
		// field that is not says so in the document rather than silently
		// encoding as something else.
		out.WriteString(util.PythonJSONString(fmt.Sprintf("%v", typed)))
	}
}

// plain converts an ordered document into the unordered value model a decoded
// document arrives as, so the two can be compared.
func plain(value any) any {
	switch typed := value.(type) {
	case object:
		converted := map[string]any{}
		for _, entry := range typed {
			converted[entry.key] = plain(entry.value)
		}
		return converted
	case []any:
		converted := make([]any, 0, len(typed))
		for _, item := range typed {
			converted = append(converted, plain(item))
		}
		return converted
	default:
		return value
	}
}

// -- Value readings ---------------------------------------------------------

// numberText renders a declared version the way the Python's message did.
func numberText(declared any) string {
	switch typed := declared.(type) {
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	case float64:
		return util.PythonFloatRepr(typed)
	default:
		return fmt.Sprintf("%v", declared)
	}
}

// intOf reads a decoded number as an integer.
func intOf(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

// stringOf reads a decoded value as a string, answering "" for anything that
// is not one -- which is what a reader that never crashes on a malformed
// manifest needs, and what the Python's plain .get() produced for an absent
// key.
func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// pagesOf reads a decoded pages list.
func pagesOf(value any) []Page {
	pages := []Page{}
	items, ok := value.([]any)
	if !ok {
		return pages
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pages = append(pages, Page{
			Path:     stringOf(entry["path"]),
			Title:    stringOf(entry["title"]),
			Type:     stringOf(entry["type"]),
			Headings: headingsOf(entry["headings"]),
		})
	}
	return pages
}

// headingsOf reads a decoded headings list.
func headingsOf(value any) []Heading {
	headings := []Heading{}
	items, ok := value.([]any)
	if !ok {
		return headings
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		headings = append(headings, Heading{
			Level:  intOf(entry["level"]),
			Text:   stringOf(entry["text"]),
			Anchor: stringOf(entry["anchor"]),
		})
	}
	return headings
}

// postsOf reads a decoded posts list.
func postsOf(value any) []Post {
	posts := []Post{}
	items, ok := value.([]any)
	if !ok {
		return posts
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		posts = append(posts, Post{
			Path:  stringOf(entry["path"]),
			Title: stringOf(entry["title"]),
			Date:  stringOf(entry["date"]),
			Slug:  stringOf(entry["slug"]),
			Tags:  tagsOf(entry["tags"]),
		})
	}
	return posts
}

// vocabularyOf reads a decoded vocabulary object.
func vocabularyOf(value any) Vocabulary {
	recorded := Vocabulary{Accepted: []AcceptedWord{}, Rejected: []RejectedPattern{}}
	table, ok := value.(map[string]any)
	if !ok {
		return recorded
	}
	if items, ok := table["accepted"].([]any); ok {
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				recorded.Accepted = append(recorded.Accepted, AcceptedWord{
					Word: stringOf(entry["word"]), Aliases: tagsOf(entry["aliases"]),
				})
			}
		}
	}
	if items, ok := table["rejected"].([]any); ok {
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				recorded.Rejected = append(recorded.Rejected, RejectedPattern{
					Pattern: stringOf(entry["pattern"]), Kind: stringOf(entry["kind"]),
				})
			}
		}
	}
	return recorded
}

// tagsOf reads a decoded list of strings: a post's tags, a word's aliases.
func tagsOf(value any) []string {
	tags := []string{}
	items, ok := value.([]any)
	if !ok {
		return tags
	}
	for _, item := range items {
		if tag, ok := item.(string); ok {
			tags = append(tags, tag)
		}
	}
	return tags
}

// -- Helpers ----------------------------------------------------------------

// isoUTC renders a moment the way Python's datetime.isoformat() does for a
// UTC-aware datetime: microsecond precision, dropped entirely when the
// microsecond is zero, and a "+00:00" offset.
func isoUTC(moment time.Time) string {
	moment = moment.UTC()
	if moment.Nanosecond()/1000 == 0 {
		return moment.Format("2006-01-02T15:04:05+00:00")
	}
	return moment.Format("2006-01-02T15:04:05.000000+00:00")
}

// sortedKeys returns the keys of m in ascending order, which is the order the
// pages are recorded in.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
