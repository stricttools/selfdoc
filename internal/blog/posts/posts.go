// Package posts discovers and validates a project's blog posts.
//
// A post is a dated Markdown file with frontmatter, sitting under the posts
// directory. This package is the whole of what makes one a post: the required
// fields, the required directive declaration, the derived slug and its
// immutability once published, and the type and version keys a post carries
// without declaring them.
//
// # Two callers, one meaning
//
// [Discover] reads the files on disk; [Parse] takes one post's source as a
// string. The editor's render path calls Parse on a buffer that may never be
// saved, so both agree on what a post's source means -- there is one
// definition of a valid post rather than one per entry point.
//
// # The refusals carry coordinates
//
// Every refusal is a [PostError] naming the post's path relative to the posts
// directory, and the line inside the post file when the defect sits at one.
// The check surface turns one of these into a POST diagnostic, and a
// diagnostic's file and line are read by editors, CI annotations and the JSON
// output -- none of which parse prose.
package posts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/util"
)

// PostError reports an invalid post, with the coordinates of where it is
// invalid.
//
// Path is relative to the posts directory, as a Post's own path is. Line is
// the post file's own line number, or nil for a defect that sits at no
// particular line (a missing frontmatter field).
//
// Code is the POST lint code the refusal is reported under when the refusal
// came from the frontmatter schema -- the missing title, the missing or
// misspelled date, the missing or non-boolean directive declaration. It is
// empty for the refusals this package decides on its own (the slug rules and
// the directive-marker scan), which the check surface codes by their message.
type PostError struct {
	Message string
	Path    string
	Line    *int
	Code    string
}

// Error renders the refusal.
func (e *PostError) Error() string { return e.Message }

// Post is one post's metadata, in the shape the build, the listing pages and
// the editor read it.
type Post struct {
	// Path is the post's path relative to the posts directory, with
	// forward slashes.
	Path string
	// Title is the post's declared title.
	Title string
	// Date is the post's publication date, YYYY-MM-DD.
	Date string
	// Slug is the address the post is published at: its declared slug,
	// else its title kebab-cased.
	Slug string
	// Tags are the post's tags, empty when it declares none.
	Tags []string
	// Draft reports whether the post is withheld from a build that did not
	// ask for drafts.
	Draft bool
	// Directives is the post's own declaration of whether it may carry
	// directive markers. It is required in the frontmatter and has no
	// default.
	Directives bool
	// Type is always "post", and Versioned always false: a post is a page
	// discovered from a different directory, served from one address
	// forever rather than once per version.
	Type      string
	Versioned bool

	// The pass-through fields, carried verbatim from the frontmatter for
	// whatever reads them. They are untyped because the frontmatter
	// dialect yields a string, a bool, a number or a list depending on how
	// the value was written, and this package interprets none of them.
	Locale       any
	Version      any
	PrevVersion  any
	BumpType     any
	ReleaseURL   any
	RegistryURLs any

	// Content is the post's body: its source with the frontmatter block
	// stripped and its directives NOT resolved.
	Content string
	// Frontmatter is the post's parsed metadata block with the injected
	// keys applied -- "type", "versioned" and a defaulted "tags".
	Frontmatter util.Frontmatter
	// FrontmatterFields is the block as it is written back out: the keys in
	// the order they appeared in the source, then whichever injected keys
	// the source did not declare, each carrying the value in the form it
	// renders back as.
	//
	// Go maps carry no order, and the page the build injects for this post
	// is rendered by writing the frontmatter back out -- so the order has
	// to be carried rather than recovered.
	FrontmatterFields []util.FrontmatterField
}

// ManifestPost narrows p to the slice a project's manifest records.
func (p Post) ManifestPost() manifest.Post {
	return manifest.Post{
		Path:  p.Path,
		Title: p.Title,
		Date:  p.Date,
		Slug:  p.Slug,
		Tags:  p.Tags,
	}
}

// ManifestPosts converts a whole discovery result for the manifest writer,
// keeping the order it came in.
func ManifestPosts(all []Post) []manifest.Post {
	out := make([]manifest.Post, 0, len(all))
	for _, post := range all {
		out = append(out, post.ManifestPost())
	}
	return out
}

// Parse parses and validates one post's Markdown source.
//
// relPath is the post's path relative to the posts directory; it is named in
// every refusal and carried on the result. publishedSlug is the slug this
// post was published under, when it has one -- a different derived slug is a
// slug immutability violation. Pass "" when the post has never been
// published.
func Parse(raw, relPath, publishedSlug string) (Post, error) {
	// The frontmatter schema is what a post's title, date and directive
	// declaration are required by: the block is validated as a post, and a
	// refusal naming one of those keys becomes its POST code here. Nothing
	// below re-checks a fact the schema already decided.
	block, readErr := util.ReadFrontmatter(raw, relPath, util.KindPost)
	if readErr != nil {
		return Post{}, frontmatterRefusal(readErr, relPath)
	}
	frontmatter, content, keys := block.Values, block.Body, block.Fields

	title := frontmatter["title"]
	date := pyStrValue(frontmatter["date"])
	declaration, _ := frontmatter["directives"].(bool)

	if !declaration {
		// Line numbers are the post file's own: the scan runs over the
		// body, so the frontmatter it sits behind is added back.
		frontmatterOffset := strings.Count(raw, "\n") - strings.Count(content, "\n")
		if found := directives.FindDirectiveMarkers(content); len(found) > 0 {
			line := found[0].LineNumber + frontmatterOffset
			return Post{}, &PostError{
				Message: fmt.Sprintf(
					"Post %s: declares 'directives = false' but line "+
						"%d carries the directive marker '%s'. Declare "+
						"'directives = true' to have it resolved, or remove "+
						"the marker.",
					relPath, line, found[0].Marker),
				Path: relPath,
				Line: &line,
			}
		}
	}

	// -- Auto-generate slug if missing ----------------------------------

	slug := ""
	if declared, ok := frontmatter["slug"]; ok && pyTruthy(declared) {
		slug = pyStrValue(declared)
	} else {
		slug = manifest.ToKebab(pyStrValue(title))
	}

	// -- Slug immutability check ----------------------------------------

	if publishedSlug != "" && publishedSlug != slug {
		return Post{}, &PostError{
			Message: fmt.Sprintf(
				"Post %s: slug changed from %s to %s. Slug immutability "+
					"violation -- slugs cannot change once published.",
				relPath, pythonRepr(publishedSlug), pythonRepr(slug)),
			Path: relPath,
		}
	}

	// -- Inject type and versioned --------------------------------------

	keys = withField(keys, frontmatter, "type", "post")
	frontmatter["type"] = "post"
	keys = withField(keys, frontmatter, "versioned", false)
	frontmatter["versioned"] = false

	// -- Defaults for optional fields -----------------------------------

	if _, declared := frontmatter["tags"]; !declared {
		keys = append(keys, util.FrontmatterField{Key: "tags", Value: []string{}})
		frontmatter["tags"] = []string{}
	}
	tags := frontmatterStrings(frontmatter, "tags")

	draft := pyTruthy(frontmatter["draft"])

	return Post{
		Path:              relPath,
		Title:             pyStrValue(title),
		Date:              date,
		Slug:              slug,
		Tags:              tags,
		Draft:             draft,
		Directives:        declaration,
		Type:              "post",
		Versioned:         false,
		Locale:            frontmatter["locale"],
		Version:           frontmatter["version"],
		PrevVersion:       frontmatter["prev_version"],
		BumpType:          frontmatter["bump_type"],
		ReleaseURL:        frontmatter["release_url"],
		RegistryURLs:      frontmatter["registry_urls"],
		Content:           content,
		Frontmatter:       frontmatter,
		FrontmatterFields: keys,
	}, nil
}

// frontmatterRefusal turns the frontmatter reader's refusal into this
// package's own, carrying the POST code the refused key decides.
//
// The mapping is from the KEY the validator's diagnostic names, not from its
// prose: a post's title, date and directive declaration are required by the
// schema, so a diagnostic about one of them is that key's lint and nothing
// else has to establish the same fact.
func frontmatterRefusal(err error, relPath string) *PostError {
	refusal := &PostError{
		// The reader's own refusal already opens with the post's path.
		Message: "Post " + err.Error(),
		Path:    relPath,
	}
	var blockErr *util.FrontmatterError
	if !errors.As(err, &blockErr) {
		return refusal
	}
	switch {
	case blockErr.Names("title"):
		refusal.Code = "POST002"
	case blockErr.Names("date"):
		refusal.Code = "POST001"
		if blockErr.HasCode("STRICTSPEC_TYPE_NOT_DATE") {
			refusal.Code = "POST003"
		}
	case blockErr.Names("directives"):
		refusal.Code = "POST006"
	}
	return refusal
}

// Discover discovers, validates and returns the posts under postsDir, sorted
// newest-first and then by slug.
//
// A postsDir that is not a directory holds no posts, which is an answer rather
// than a failure.
//
// projectRoot optionally names the repository the posts belong to. When it is
// given, slug immutability is enforced against the COMMITTED manifest read out
// of git HEAD rather than the copy on disk, because gen may already have
// rewritten that copy with the new slug by the time this runs. A directory that
// is not a repository, a repository with no commits, and a manifest that was
// never committed each leave the check with nothing to compare against, and it
// is skipped.
//
// Files are read in sorted order, so a duplicate-slug refusal always names the
// same pair in the same direction. The Python walked in directory-listing
// order and could name either post as the second one.
func Discover(postsDir, projectRoot string, handle *effects.Handle) ([]Post, error) {
	info, err := os.Stat(postsDir)
	if err != nil || !info.IsDir() {
		return []Post{}, nil
	}

	// Build a lookup from path -> slug for the committed manifest's posts.
	publishedSlugs := map[string]string{}
	if projectRoot != "" {
		committed, err := manifest.LoadFromGit(projectRoot, handle)
		if err != nil {
			return nil, err
		}
		if committed != nil {
			for _, entry := range committed.Posts {
				publishedSlugs[entry.Path] = entry.Slug
			}
		}
	}

	posts := []Post{}
	if err := walkPosts(postsDir, postsDir, func(relPath, raw string) error {
		post, err := Parse(raw, relPath, publishedSlugs[relPath])
		if err != nil {
			return err
		}
		posts = append(posts, post)
		return nil
	}); err != nil {
		return nil, err
	}

	// -- Validate slug uniqueness ---------------------------------------

	seen := map[string]string{}
	for _, post := range posts {
		if first, ok := seen[post.Slug]; ok {
			return nil, &PostError{
				Message: fmt.Sprintf(
					"Duplicate slug %s: used by both %s and %s",
					pythonRepr(post.Slug), pythonRepr(first),
					pythonRepr(post.Path)),
				Path: post.Path,
			}
		}
		seen[post.Slug] = post.Path
	}

	// -- Sort: newest first, then slug ascending for ties ---------------

	sort.SliceStable(posts, func(i, j int) bool {
		left, right := dateSortKey(posts[i].Date), dateSortKey(posts[j].Date)
		if left != right {
			return left > right
		}
		return posts[i].Slug < posts[j].Slug
	})

	return posts, nil
}

// walkPosts visits every .md file under dir, top-down and in sorted order,
// calling visit with the file's path relative to root (forward slashes) and
// its contents.
func walkPosts(dir, root string, visit func(relPath, raw string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var subdirs []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			subdirs = append(subdirs, filepath.Join(dir, name))
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(root, fullPath)
		if err != nil {
			return err
		}
		if err := visit(filepath.ToSlash(relPath), string(raw)); err != nil {
			return err
		}
	}
	for _, subdir := range subdirs {
		if err := walkPosts(subdir, root, visit); err != nil {
			return err
		}
	}
	return nil
}

// dateSortKey turns a YYYY-MM-DD date into the integer the sort compares. The
// date has already been validated against the accepted spelling, so it always
// parses.
func dateSortKey(date string) int64 {
	value, err := strconv.ParseInt(strings.ReplaceAll(date, "-", ""), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// withField appends the injected key to the block's written order, with the
// value it is injected with, when the source did not declare it. A key the
// source declared keeps the position it was written at, which is what
// assigning to an existing dict key does in Python, and takes the injected
// value there.
func withField(
	fields []util.FrontmatterField, frontmatter util.Frontmatter, key string, value any,
) []util.FrontmatterField {
	if _, declared := frontmatter[key]; declared {
		for index := range fields {
			if fields[index].Key == key {
				fields[index].Value = value
			}
		}
		return fields
	}
	return append(fields, util.FrontmatterField{Key: key, Value: value})
}

// frontmatterStrings reads a frontmatter key as a list of strings.
//
// The bracket syntax yields a list; a bare value yields the one-element list
// holding it, which is how every other reader of a tags-like key interprets
// one. The Python carried a bare value through as the scalar it was written
// as, and a manifest then recorded a string where a list belonged.
func frontmatterStrings(frontmatter util.Frontmatter, key string) []string {
	switch typed := frontmatter[key].(type) {
	case []string:
		return append([]string{}, typed...)
	case nil:
		return []string{}
	case string:
		if typed == "" {
			return []string{}
		}
		return []string{typed}
	default:
		return []string{pyStrValue(typed)}
	}
}

// pyTruthy reports whether a frontmatter value is truthy the way Python's
// bool() judges it: an absent value, a false, an empty string, a zero and an
// empty list are all false.
func pyTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case []string:
		return len(typed) > 0
	default:
		return true
	}
}

// pyStrValue renders a frontmatter value the way Python's str() does.
func pyStrValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return util.PythonFloatRepr(typed)
	case []string:
		return pythonRepr(typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// pythonRepr renders a frontmatter value the way Python's repr does, for the
// refusals that quote an offending value.
func pythonRepr(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case string:
		return pythonStrRepr(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return util.PythonFloatRepr(typed)
	case []string:
		parts := make([]string, len(typed))
		for index, item := range typed {
			parts[index] = pythonStrRepr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// pythonStrRepr quotes s the way Python's repr(str) does: single quotes
// unless the string contains a single quote and no double quote, the short
// escapes for backslash, tab, newline and carriage return, a \xNN escape for
// every other non-printable below U+0100, and \uXXXX or \UXXXXXXXX above it.
func pythonStrRepr(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = '"'
	}
	var builder strings.Builder
	builder.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			builder.WriteByte('\\')
			builder.WriteRune(r)
		case r == '\t':
			builder.WriteString(`\t`)
		case r == '\n':
			builder.WriteString(`\n`)
		case r == '\r':
			builder.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&builder, `\x%02x`, r)
		case r < 0x7f:
			builder.WriteRune(r)
		case unicode.IsPrint(r):
			builder.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&builder, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&builder, `\u%04x`, r)
		default:
			fmt.Fprintf(&builder, `\U%08x`, r)
		}
	}
	builder.WriteByte(quote)
	return builder.String()
}
