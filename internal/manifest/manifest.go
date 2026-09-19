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
// # The reader is tolerant
//
// Compat is the one door every read path goes through. It takes the fields it
// knows and ignores every other key, so a manifest written by a later selfdoc
// still reads here; a schema_version above the supported one is the single
// hard refusal, because that declares a document this reader cannot claim to
// understand.
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
)

// DefaultTheme is the theme a project's manifest records when its config
// names none. It is the same default the build applies, and the assembly's
// chrome reads this field to decide which stylesheet a project's pages
// reference.
const DefaultTheme = "minimal"

// SchemaVersion is the manifest format this package writes, and the highest
// it reads. A document declaring more is refused rather than read on this
// version's terms.
const SchemaVersion = 1

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
	}
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
// does not know.
func Compat(data map[string]any, source string) (*Manifest, error) {
	declared := any(int64(SchemaVersion))
	if raw, ok := data["schema_version"]; ok {
		declared = raw
	}
	above, err := aboveSupportedVersion(declared)
	if err != nil {
		return nil, err
	}
	if above {
		context := ""
		if source != "" {
			context = " in " + source
		}
		return nil, fmt.Errorf(
			"Unsupported manifest schema_version %s%s (max supported: %d)",
			numberText(declared), context, SchemaVersion)
	}
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
	}, nil
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
	decoded, err := config.DecodeDocument(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	data, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: the manifest is not a JSON object", path)
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
	decoded, err := config.DecodeDocument(contents.Stdout)
	if err != nil {
		return nil, fmt.Errorf("git HEAD: %w", err)
	}
	data, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("git HEAD: the manifest is not a JSON object")
	}
	return Compat(data, "git HEAD")
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

// aboveSupportedVersion reports whether a declared schema_version is beyond
// what this reader supports. A value that is not a number at all is an error:
// the document declares a format in a spelling no reader can compare.
func aboveSupportedVersion(declared any) (bool, error) {
	switch typed := declared.(type) {
	case int64:
		return typed > SchemaVersion, nil
	case int:
		return int64(typed) > SchemaVersion, nil
	case float64:
		return typed > SchemaVersion, nil
	default:
		return false, fmt.Errorf(
			"Unsupported manifest schema_version %v (not a number)", declared)
	}
}

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

// tagsOf reads a decoded tags list.
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
