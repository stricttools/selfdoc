// Package editor is the authoring app's local server: registry, documents,
// preview, stream.
//
// A single-user, local-only HTTP server on the standard library alone. It
// serves seven things:
//
//   - the shell (the editor's own page and module, plus tinymoon's asset tree);
//   - the registry, and each local entry's posts;
//   - document read and write -- a write lands in the working tree, atomically;
//   - previews, rendered in memory and pushed down one server-sent-events
//     channel;
//   - analysis of an unsaved buffer -- spelling and lint findings, from the
//     engines the check itself runs;
//   - link targets across every registered repository, addressed the way a
//     post has to address them;
//   - the publish surface -- the command's own declaration, the list of posts
//     a publish would make public, and the consented invocation.
//
// Analysis is a SIBLING of the preview, not a passenger on it. They fail
// independently and that is the whole reason: a buffer that cannot render --
// no date in its frontmatter, a directive nothing answers -- is the buffer
// whose diagnostics are worth the most, and one endpoint that renders and
// analyses would lose them to the render's refusal. They also differ in what
// they produce: a preview is one document broadcast to every listener on the
// event stream, while analysis answers the request that asked for it.
//
// The preview is the part with a property worth stating. It goes through
// [github.com/stricttools/selfdoc/internal/render.Post], which is the *publish*
// renderer handed an in-memory buffer instead of a file: same directive
// resolution, same HTML pass, same site-level addressing, and no write
// anywhere. So what the author approves on screen is the bytes readers get,
// and asking for a preview cannot change the tree it previews. Both halves
// are asserted by the suite.
//
// Remote registry entries are validated but not served. Every path that would
// have to reach one refuses with "remote entries not yet served" rather than
// half-working.
package editor

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/render"
	"github.com/stricttools/selfdoc/internal/util"
)

// Error reports that a request cannot be served, for the reason the message
// states, and carries the status the server answers it with.
//
// It is the Go counterpart of the Python surface's EditorError hierarchy,
// where each subclass carried a status attribute: the status is a field here
// rather than a type, so a caller reads one answer instead of matching a
// hierarchy. Recognize it with errors.As to render a refusal as one line
// instead of an unexpected internal failure.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error and by the
	// {"error": ...} body the server answers with.
	Message string
	// Status is the HTTP status the server answers: 400 for a request that
	// cannot be served, 404 for a thing that does not exist here, 501 for a
	// remote entry, 403 for a refused publish and 500 for a failed one.
	Status int
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

// badRequest builds a 400: the request cannot be served.
func badRequest(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Status: http.StatusBadRequest}
}

// notFound builds a 404: the thing asked for does not exist here.
func notFound(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Status: http.StatusNotFound}
}

// remoteNotServed builds a 501: a remote registry entry was reached.
// Validated, not yet served.
func remoteNotServed(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Status: http.StatusNotImplemented}
}

// -- repository access ------------------------------------------------------

// RequireLocal returns the working tree of entry, or refuses.
//
// The refusal is the one every path that would have to reach a remote entry
// answers, so a remote entry is never half-served.
func RequireLocal(entry registry.Entry) (string, error) {
	if local, ok := entry.(*registry.LocalRepo); ok {
		return local.Path(), nil
	}
	target := "?"
	if remote, ok := entry.(*registry.RemoteRepo); ok {
		target = remote.Repo()
	}
	return "", remoteNotServed(
		"remote entries not yet served: %s points at %s. The registry "+
			"validates remote entries in full, but serving one is not "+
			"implemented -- register a local working tree instead.",
		util.PythonRepr(entry.Name()), target,
	)
}

// RepoConfig returns the project config of a local entry, or a refusal naming
// the entry.
func RepoConfig(entry registry.Entry) (config.Config, error) {
	path, err := RequireLocal(entry)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	var configError *config.ConfigError
	if errors.As(err, &configError) {
		return nil, badRequest("%s: %s", entry.Name(), configError.Message)
	}
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, badRequest(
			"%s: no selfdoc.json in %s. The editor edits selfdoc projects; "+
				"this path is not one.",
			entry.Name(), path,
		)
	}
	return cfg, nil
}

// PostsDirOf returns the absolute posts directory of a local entry.
//
// A nil cfg loads the entry's config.
func PostsDirOf(entry registry.Entry, cfg config.Config) (string, error) {
	if cfg == nil {
		loaded, err := RepoConfig(entry)
		if err != nil {
			return "", err
		}
		cfg = loaded
	}
	path, err := RequireLocal(entry)
	if err != nil {
		return "", err
	}
	return util.PathJoin(path, postsDirRel(cfg)), nil
}

// postsDirRel reads the configured posts directory, relative to the project
// root, defaulting to the conventional one.
func postsDirRel(cfg config.Config) string {
	postsConfig, _ := cfg["posts"].(map[string]any)
	relative := util.PythonStrOrEmpty(postsConfig["dir"])
	if relative == "" {
		return defaultPostsDir
	}
	return relative
}

// defaultPostsDir is where a project keeps its posts when it declares no
// posts block.
var defaultPostsDir = layout.PostsDefault

// PostSummary is one post as the sidebar and the publish plan carry it: only
// what a list needs, because the post bodies are fetched one at a time, when
// one is opened.
type PostSummary struct {
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Date  string   `json:"date"`
	Slug  string   `json:"slug"`
	Draft bool     `json:"draft"`
	Tags  []string `json:"tags"`
}

// RepoPosts returns every post a local entry declares, newest first.
func RepoPosts(entry registry.Entry, handle *effects.Handle) ([]PostSummary, error) {
	cfg, err := RepoConfig(entry)
	if err != nil {
		return nil, err
	}
	path, err := RequireLocal(entry)
	if err != nil {
		return nil, err
	}
	postsDir, err := PostsDirOf(entry, cfg)
	if err != nil {
		return nil, err
	}
	discovered, err := posts.Discover(postsDir, path, handle)
	var postError *posts.PostError
	if errors.As(err, &postError) {
		return nil, badRequest("%s: %s", entry.Name(), postError.Error())
	}
	if err != nil {
		return nil, err
	}

	summaries := make([]PostSummary, 0, len(discovered))
	for _, post := range discovered {
		tags := post.Tags
		if tags == nil {
			tags = []string{}
		}
		summaries = append(summaries, PostSummary{
			Path:  post.Path,
			Title: post.Title,
			Date:  post.Date,
			Slug:  post.Slug,
			Draft: post.Draft,
			Tags:  append([]string{}, tags...),
		})
	}
	return summaries, nil
}

// SafeRel returns a post path relative to the posts directory, or a refusal.
//
// The editor addresses documents by a path the browser supplies, so this is
// the boundary where a path that leaves the posts directory has to stop.
func SafeRel(rel string) (string, error) {
	if rel == "" {
		return "", badRequest("a document path is required")
	}
	normalized := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", badRequest(
			"document path %s must be relative to the posts directory",
			util.PythonRepr(rel),
		)
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." || part == "." || part == "" {
			return "", badRequest(
				"document path %s must not contain '..' or empty segments",
				util.PythonRepr(rel),
			)
		}
	}
	if !strings.HasSuffix(normalized, ".md") {
		return "", badRequest(
			"document path %s must name a .md file", util.PythonRepr(rel),
		)
	}
	return normalized, nil
}

// PostPathOf returns the absolute path of one post inside a local entry.
func PostPathOf(entry registry.Entry, rel string) (string, error) {
	// The entry is resolved before the path is judged, which is the order the
	// Python evaluated them in: a remote entry is refused as a remote entry
	// even when the path it was given is also unusable.
	postsDir, err := PostsDirOf(entry, nil)
	if err != nil {
		return "", err
	}
	safe, err := SafeRel(rel)
	if err != nil {
		return "", err
	}
	return util.PathJoin(postsDir, safe), nil
}

// ReadPost returns the saved source of one post.
func ReadPost(entry registry.Entry, rel string) (string, error) {
	full, err := PostPathOf(entry, rel)
	if err != nil {
		return "", err
	}
	info, statErr := os.Stat(full)
	if statErr != nil || !info.Mode().IsRegular() {
		return "", notFound("%s: no post at %s", entry.Name(), rel)
	}
	content, readErr := os.ReadFile(full)
	if readErr != nil {
		return "", readErr
	}
	return string(content), nil
}

// SavePost writes a buffer to the working tree, atomically, and returns the
// file it wrote.
//
// Atomic because the tree is shared with everything else that reads it -- a
// build, a check, another editor -- and a half-written post is a post that
// fails to parse for whatever looked at it mid-write.
func SavePost(entry registry.Entry, rel, content string, handle *effects.Handle) (string, error) {
	full, err := PostPathOf(entry, rel)
	if err != nil {
		return "", err
	}
	if err := handle.MkdirAll(parentDir(full)); err != nil {
		return "", err
	}
	if err := handle.AtomicWrite(full, []byte(content), effects.ModeDefault); err != nil {
		return "", err
	}
	return full, nil
}

// parentDir is the directory component of a forward-slash path, the way
// Python's os.path.dirname reads one.
func parentDir(path string) string {
	index := strings.LastIndex(path, "/")
	if index < 0 {
		return ""
	}
	if index == 0 {
		return "/"
	}
	return path[:index]
}

// PostSlug returns the slug a buffer would publish under.
func PostSlug(rel, content string) (string, error) {
	safe, err := SafeRel(rel)
	if err != nil {
		return "", err
	}
	parsed, parseErr := posts.Parse(content, safe, "")
	var postError *posts.PostError
	if errors.As(parseErr, &postError) {
		return "", badRequest("%s", postError.Error())
	}
	if parseErr != nil {
		return "", parseErr
	}
	return parsed.Slug, nil
}

// RenderPreview renders a buffer to the exact HTML publishing it would
// produce.
//
// One renderer: this is [github.com/stricttools/selfdoc/internal/render.Post],
// which is the build's own page pass over an in-memory overlay. The bytes
// equal what a posts-target build writes for the same source saved to disk,
// and nothing is written anywhere.
//
// Drafts are the one case with no published counterpart to equal, so a buffer
// that declares "draft: true" is rendered as the drafts build renders it. The
// decision is read off the buffer, never off a mode the server carries: the
// same buffer previews the same way every time.
func RenderPreview(entry registry.Entry, rel, content string, handle *effects.Handle) (string, error) {
	path, err := RequireLocal(entry)
	if err != nil {
		return "", err
	}
	safe, err := SafeRel(rel)
	if err != nil {
		return "", err
	}
	cfg, err := RepoConfig(entry)
	if err != nil {
		return "", err
	}

	block, err := util.ReadFrontmatter(content, safe, util.KindPost)
	if err != nil {
		return "", badRequest("%s", err.Error())
	}
	isDraft, _ := block.Values["draft"].(bool)

	html, renderErr := render.Post(render.PostOptions{
		DirPath:       path,
		SourcePath:    safe,
		Content:       content,
		Config:        cfg,
		IncludeDrafts: isDraft,
	}, handle)
	var postError *posts.PostError
	if errors.As(renderErr, &postError) {
		return "", badRequest("%s", postError.Error())
	}
	if renderErr != nil {
		// The Python caught RuntimeError here and named the entry; every
		// other refusal the render can answer is one of those.
		return "", badRequest("%s: %s", entry.Name(), renderErr.Error())
	}
	return html, nil
}

// PreviewAddress returns where a previewed post is served, mirroring its
// published address.
func PreviewAddress(slug string) string {
	return fmt.Sprintf("blog/%s/index.html", slug)
}
