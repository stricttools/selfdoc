// Package revisions tracks post revisions in a sidecar revisions.json.
//
// Content changes to blog posts are tracked using SHA-256 hashes of the
// rendered body text. A new revision is appended only when the body content
// actually changes, so a frontmatter-only edit is invisible.
//
// The sidecar file sits in selfdoc's generated-state directory -- separate from the
// manifest, so no manifest format change can reach it.
package revisions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// Revision is one recorded revision of a post.
type Revision struct {
	// ContentHash is the SHA-256 of the post's normalized body.
	ContentHash string
	// Timestamp is when the revision was recorded, in UTC, ISO 8601.
	Timestamp string
	// Summary is an optional human-readable note. An empty summary is left
	// out of the written document rather than written as an empty string.
	Summary string
}

// PostRevisions is one post's recorded revisions, oldest first.
type PostRevisions struct {
	// Slug is the post's slug identifier.
	Slug string
	// Revisions are the recorded revisions, oldest first.
	Revisions []Revision
}

// Document is the parsed sidecar: every post that has a recorded revision, in
// the order the file declares them.
//
// The order is part of the document rather than an implementation detail: the
// sidecar is rewritten whole on every recording, and a rewrite that reordered
// the posts would produce a diff on every publish.
type Document struct {
	// Posts are the recorded posts, in document order.
	Posts []PostRevisions
}

// Find returns the entry for slug, or nil when the document carries none.
func (d *Document) Find(slug string) *PostRevisions {
	for index := range d.Posts {
		if d.Posts[index].Slug == slug {
			return &d.Posts[index]
		}
	}
	return nil
}

// ComputePostContentHash computes a deterministic SHA-256 hash of a post's
// body text.
//
// The body is the rendered content with frontmatter already stripped.
// Whitespace is normalized (per-line trim plus blank-line collapsing) so that
// insignificant formatting changes do not trigger false revisions.
//
// Site-context-dependent values (base URLs, theme names) must NOT appear in
// the input -- callers pass only the body text.
func ComputePostContentHash(body string) string {
	sum := sha256.Sum256([]byte(normalizeBody(body)))
	return hex.EncodeToString(sum[:])
}

// normalizeBody normalizes body text for deterministic hashing.
//
// Leading and trailing whitespace is stripped per line and runs of blank lines
// collapse into a single blank line, which makes the hash stable across
// trivial whitespace edits.
func normalizeBody(body string) string {
	var result []string
	previousBlank := false
	for _, line := range util.PythonSplitLines(body) {
		trimmed := util.PythonStrip(line)
		if trimmed == "" {
			if !previousBlank {
				result = append(result, "")
			}
			previousBlank = true
			continue
		}
		result = append(result, trimmed)
		previousBlank = false
	}
	// Strip leading and trailing blank lines from the whole result.
	return util.PythonStrip(strings.Join(result, "\n"))
}

// LoadRevisions loads revisions.json from dirPath's generated-state directory.
//
// An absent file is an empty document, not an error: a project that has
// published nothing yet has no revisions to read. A file that exists and is
// not readable as the document is an error.
func LoadRevisions(dirPath string) (*Document, error) {
	path := layout.Path(dirPath, layout.RevisionsRel)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return &Document{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	document, err := parseDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return document, nil
}

// SaveRevisions writes revisions.json into dirPath's generated-state directory,
// atomically, and returns the path it wrote.
func SaveRevisions(handle *effects.Handle, document *Document, dirPath string) (string, error) {
	if err := layout.EnsureDir(handle, dirPath, layout.DocsStateRel); err != nil {
		return "", err
	}
	path := layout.Path(dirPath, layout.RevisionsRel)
	if err := handle.AtomicWrite(path, render(document), effects.ModeDefault); err != nil {
		return "", err
	}
	return path, nil
}

// RecordRevision records a revision for a post when its body content changed.
//
// It computes the content hash, compares it against the latest revision for
// this slug, and appends a new entry only when the hash differs. The returned
// bool reports whether a revision was appended; when it is false nothing was
// written.
//
// dirPath is the project root, slug the post's identifier, body the rendered
// body text with frontmatter stripped, and summary an optional note that is
// left out of the document when empty.
func RecordRevision(
	handle *effects.Handle,
	dirPath string,
	slug string,
	body string,
	summary string,
) (bool, error) {
	contentHash := ComputePostContentHash(body)
	document, err := LoadRevisions(dirPath)
	if err != nil {
		return false, err
	}
	post := document.Find(slug)
	if post == nil {
		document.Posts = append(document.Posts, PostRevisions{Slug: slug})
		post = &document.Posts[len(document.Posts)-1]
	}
	if count := len(post.Revisions); count > 0 && post.Revisions[count-1].ContentHash == contentHash {
		return false, nil
	}
	post.Revisions = append(post.Revisions, Revision{
		ContentHash: contentHash,
		Timestamp:   isoTimestamp(time.Now().UTC()),
		Summary:     summary,
	})
	if _, err := SaveRevisions(handle, document, dirPath); err != nil {
		return false, err
	}
	return true, nil
}

// isoTimestamp renders moment the way Python's
// datetime.now(timezone.utc).isoformat() does: microsecond precision, the
// fractional part omitted entirely when it is zero, and an explicit "+00:00"
// offset.
func isoTimestamp(moment time.Time) string {
	if moment.Nanosecond()/1000 == 0 {
		return moment.Format("2006-01-02T15:04:05") + "+00:00"
	}
	return moment.Format("2006-01-02T15:04:05.000000") + "+00:00"
}

// GetPostRevisions returns the recorded revisions for a post, oldest first,
// and an empty slice for a post with none.
func GetPostRevisions(dirPath, slug string) ([]Revision, error) {
	document, err := LoadRevisions(dirPath)
	if err != nil {
		return nil, err
	}
	post := document.Find(slug)
	if post == nil {
		return nil, nil
	}
	return post.Revisions, nil
}

// GetLastUpdated returns the timestamp of the most recent revision for a post.
// The bool reports whether the post has any revision at all.
func GetLastUpdated(dirPath, slug string) (string, bool, error) {
	revisions, err := GetPostRevisions(dirPath, slug)
	if err != nil {
		return "", false, err
	}
	if len(revisions) == 0 {
		return "", false, nil
	}
	return revisions[len(revisions)-1].Timestamp, true, nil
}

// -- the document's bytes ---------------------------------------------------

// render renders document the way the Python wrote it: json.dumps with
// indent=2 and no key sorting, plus a trailing newline.
//
// The order is declared here rather than delegated to a sorting encoder,
// because the Python's insertion order and a sorted order disagree -- a
// revision writes "content_hash", "timestamp", "summary", while sorting would
// put the summary in the middle and rewrite every existing sidecar's bytes.
func render(document *Document) []byte {
	var out bytes.Buffer
	out.WriteString("{\n")
	out.WriteString(`  "posts": `)
	if len(document.Posts) == 0 {
		out.WriteString("{}")
	} else {
		out.WriteString("{\n")
		for index, post := range document.Posts {
			out.WriteString("    " + util.PythonJSONString(post.Slug) + ": {\n")
			out.WriteString(`      "revisions": `)
			if len(post.Revisions) == 0 {
				out.WriteString("[]")
			} else {
				out.WriteString("[\n")
				for entryIndex, revision := range post.Revisions {
					out.WriteString("        {\n")
					fields := []struct{ key, value string }{
						{"content_hash", revision.ContentHash},
						{"timestamp", revision.Timestamp},
					}
					if revision.Summary != "" {
						fields = append(fields, struct{ key, value string }{
							"summary", revision.Summary,
						})
					}
					for fieldIndex, field := range fields {
						out.WriteString("          " +
							util.PythonJSONString(field.key) + ": " +
							util.PythonJSONString(field.value))
						if fieldIndex < len(fields)-1 {
							out.WriteString(",")
						}
						out.WriteString("\n")
					}
					out.WriteString("        }")
					if entryIndex < len(post.Revisions)-1 {
						out.WriteString(",")
					}
					out.WriteString("\n")
				}
				out.WriteString("      ]")
			}
			out.WriteString("\n    }")
			if index < len(document.Posts)-1 {
				out.WriteString(",")
			}
			out.WriteString("\n")
		}
		out.WriteString("  }")
	}
	out.WriteString("\n}\n")
	return out.Bytes()
}

// parseDocument reads the sidecar, keeping the posts in the order the file
// declares them.
//
// encoding/json decodes an object into an unordered map, so the posts object
// is walked with the token stream instead. Everything under a post is ordinary
// structured data and is decoded normally.
func parseDocument(raw []byte) (*Document, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	postsRaw, ok := top["posts"]
	if !ok {
		return &Document{}, nil
	}
	keys, values, err := orderedObject(postsRaw)
	if err != nil {
		return nil, err
	}
	document := &Document{}
	for index, slug := range keys {
		var entry struct {
			Revisions []struct {
				ContentHash string `json:"content_hash"`
				Timestamp   string `json:"timestamp"`
				Summary     string `json:"summary"`
			} `json:"revisions"`
		}
		if err := json.Unmarshal(values[index], &entry); err != nil {
			return nil, err
		}
		post := PostRevisions{Slug: slug}
		for _, revision := range entry.Revisions {
			post.Revisions = append(post.Revisions, Revision{
				ContentHash: revision.ContentHash,
				Timestamp:   revision.Timestamp,
				Summary:     revision.Summary,
			})
		}
		document.Posts = append(document.Posts, post)
	}
	return document, nil
}

// orderedObject returns a JSON object's keys and raw values in document order.
func orderedObject(raw json.RawMessage) ([]string, []json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, nil, errors.New(`"posts" is not a JSON object`)
	}
	var keys []string
	var values []json.RawMessage
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, nil, errors.New(`"posts" carries a non-string key`)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		values = append(values, value)
	}
	if _, err := decoder.Token(); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	return keys, values, nil
}
