package check

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/stricttools/selfdoc/internal/vocabulary"
)

// defaultPostsDirRel is where a project keeps its posts when it declares no
// "posts" block: the convention every post surface reads.
var defaultPostsDirRel = layout.PostsDefault

// postsDirRel reads the configured posts directory, relative to the project
// root.
//
// An undeclared "posts" block answers the conventional directory, for every
// caller. The check's surfaces must agree on where a post is: a validation
// pass that answered "this project has no posts" while the lint slice read
// the conventional directory made an invalid post there a hard error instead
// of the POST diagnostic it is.
func postsDirRel(projectConfig map[string]any) string {
	return configString(configDict(projectConfig, "posts"), "dir", defaultPostsDirRel)
}

// postsDirectory returns the configured posts directory relative to the
// project root, and its absolute path.
//
// The absolute path is empty when the project has no posts directory on disk.
// One resolution for both post surfaces of the check -- the validation pass
// and the lint slice -- so neither can look somewhere the other does not, and
// both look where the build looks.
func postsDirectory(projectConfig map[string]any, dirPath string) (string, string) {
	relative := postsDirRel(projectConfig)
	absolute := util.PathJoin(dirPath, relative)
	if relative == "" || !isDir(absolute) {
		return relative, ""
	}
	return relative, absolute
}

// PostErrorLint turns one post refusal into its lint diagnostic.
//
// The mapping from a refusal to its POST code lives here and nowhere else, so
// every surface that reports post validation -- the check, and the editor
// judging an unsaved buffer -- says the same thing under the same code.
//
// The coordinates come off the error, not out of the message: the detection
// site knew the post's path (and, for a stray marker, its line), and
// everything downstream that positions a diagnostic reads the structured
// fields rather than parsing prose. The CODE comes off the error too for
// every refusal the frontmatter schema decided, which is where a post's
// required fields and its date's spelling are declared. The refusals this
// repository decides on its own -- the slug rules and the directive-marker
// scan -- carry no kind of their own and are still matched by message.
func PostErrorLint(err *posts.PostError, postsDirRelative string) lints.LintResult {
	message := err.Error()
	code := "POST001" // the fallback
	switch {
	// A refusal the frontmatter schema decided carries its own code: the
	// missing title, the missing or mistyped date, the missing or
	// non-boolean directive declaration are the schema's facts, and the
	// post parser read them off the validator's verdict.
	case err.Code != "":
		code = err.Code
	case strings.Contains(message, "Duplicate slug"):
		code = "POST004"
	case strings.Contains(message, "Slug immutability violation"):
		code = "POST005"
	case strings.Contains(message, "declares 'directives = false'"):
		code = "POST007"
	}

	return lints.MustLintResult(
		util.PathJoin(postsDirRelative, filepath.ToSlash(err.Path)),
		err.Line, code, message,
	)
}

// CheckPosts checks a project's blog posts for validation errors
// (POST001-POST007), returning one diagnostic for the first invalid post.
//
// The result is empty when the posts directory is not on disk and when every
// post is valid. A project that declares no "posts" block is read at the
// conventional posts directory, which is where the post lints read it. Each diagnostic
// is positioned at the offending post -- its path relative to the project, and
// its line where the defect has one -- taken from the refusal the detection
// site raised.
func CheckPosts(
	projectConfig map[string]any, dirPath string, handle *effects.Handle,
) ([]lints.LintResult, error) {
	postsDirRelative, postsDir := postsDirectory(projectConfig, dirPath)
	if postsDir == "" {
		return nil, nil
	}

	if _, err := posts.Discover(postsDir, dirPath, handle); err != nil {
		var postError *posts.PostError
		if errors.As(err, &postError) {
			return []lints.LintResult{PostErrorLint(postError, postsDirRelative)}, nil
		}
		return nil, err
	}

	return nil, nil
}

// postLintDocs resolves a project's published posts into the lint rules'
// slice.
//
// A post is a page on the site, so every rule that holds a documentation page
// to a standard holds a post to it too -- but no path used to reach them. The
// check never injected posts into the docs tree, and the build's lint pass
// runs after the injected files have been removed, so a post could carry any
// defect and both surfaces reported nothing.
//
// The conversion is not repeated here: [build.PostDocsPayloads] is the one
// place a post becomes a docs page (the build's injection and the in-memory
// render path both go through it), and this hands it the same published set
// the build would. Two things are then corrected, because a diagnostic has to
// name something a reader can open:
//
//   - the key is the post's own path, relative to the project root, not the
//     "blog/<slug>.md" address the docs tree would hold it at;
//   - the frontmatter line count is the SOURCE file's, not the rebuilt
//     frontmatter's. The conversion injects, drops and reorders keys, so its
//     line count differs from the file on disk while the body below it is
//     byte-identical -- taking the source's count makes every reported line
//     the post file's real line.
//
// Drafts are excluded, matching the build: an unpublished draft is not on the
// site, so the check does not judge it. The generated listing page is excluded
// too -- it has no source file, so a diagnostic about it would name nothing
// anyone can fix.
//
// Only ever called on a post set that post validation accepted: discovery
// refuses an invalid post, and the caller reports that as its own diagnostic
// rather than asking this to resolve a set that does not exist.
//
// overlay holds post source in memory, keyed by the post's path relative to
// the posts directory. Each entry replaces (or adds to) the post of that path,
// exactly as the in-memory render path overlays an editor's unsaved buffer --
// so the rules judge what is on screen rather than what is saved.
// includeDrafts judges drafts too, which is what an editor asks for: a draft
// is exactly what is being written.
func postLintDocs(
	projectConfig map[string]any,
	dirPath string,
	res *resolver.Resolver,
	validNames directives.NameSet,
	overlay map[string]string,
	includeDrafts bool,
	handle *effects.Handle,
) (map[string]docs.Doc, error) {
	postsDirRelative, postsDir := postsDirectory(projectConfig, dirPath)
	if postsDir == "" {
		return nil, nil
	}

	discovered, err := posts.Discover(postsDir, dirPath, handle)
	if err != nil {
		return nil, err
	}

	// The overlay replaces a post in place, so its neighbours -- and its
	// own position among them -- are whatever is saved. Same rule the
	// renderer follows, for the same reason: only the edited post is
	// unsaved.
	sources := map[string]string{}
	for _, relative := range sortedKeys(overlay) {
		content := overlay[relative]
		edited, parseErr := posts.Parse(content, relative, "")
		if parseErr != nil {
			return nil, parseErr
		}
		replaced := false
		for index, post := range discovered {
			if post.Path == relative {
				discovered[index] = edited
				replaced = true
				break
			}
		}
		if !replaced {
			discovered = append(discovered, edited)
		}
		sources[relative] = content
	}

	var published []posts.Post
	for _, post := range discovered {
		if includeDrafts || !post.Draft {
			published = append(published, post)
		}
	}
	if len(published) == 0 {
		return nil, nil
	}

	payloads, err := build.PostDocsPayloads(published)
	if err != nil {
		return nil, err
	}

	result := map[string]docs.Doc{}
	for _, post := range published {
		relPath := address.PostsPrefix + "/" + post.Slug + ".md"
		payload := payloads[relPath]
		doc, err := docs.ResolveMarkdown(payload, relPath, res.Resolve, validNames)
		if err != nil {
			return nil, err
		}

		sourceRel := util.PathJoin(
			strings.TrimRight(postsDirRelative, "/"), post.Path,
		)
		raw, held := sources[post.Path]
		if !held {
			bytes, readErr := os.ReadFile(filepath.Join(dirPath, sourceRel))
			if readErr != nil {
				return nil, readErr
			}
			raw = string(bytes)
		}
		sourceBody, err := util.StripFrontmatter(raw, sourceRel)
		if err != nil {
			return nil, err
		}
		fmLineCount := len(strings.Split(raw, "\n")) - len(strings.Split(sourceBody, "\n"))

		result[sourceRel] = docs.Doc{
			Frontmatter:      doc.Frontmatter,
			Resolved:         doc.Resolved,
			Raw:              doc.Raw,
			FrontmatterLines: fmLineCount,
		}
	}

	return result, nil
}

// LintPostBuffer runs the post-applicable lint rules over one in-memory post
// buffer.
//
// The editor's diagnostics come from here, and they are the check's own
// diagnostics: the same slice conversion over the same rules, with the buffer
// overlaid on the saved post set exactly as the renderer overlays it. Nothing
// about a rule is restated for the editor, so a finding on screen is a finding
// `selfdoc check` will report, worded identically.
//
// Two differences from the whole-project run, each deliberate:
//
//   - drafts are judged, because a draft is what is being written (and, as a
//     consequence, a link to a draft post resolves here where the check would
//     call it unknown -- the draft is on disk either way);
//   - only the buffer's own diagnostics are returned, because the rest of the
//     tree is not what the author is looking at.
//
// The rules run over the post slice alone, not the whole docs tree. The one
// cross-page rule, XREF001, resolves a link against the page's own directory,
// and a post's own directory is the posts directory -- so the docs pages could
// never have matched a post's link anyway, and the slice is the universe the
// whole-project run offers a post.
//
// sourcePath is the post's path relative to the posts directory and content is
// the buffer, frontmatter included. projectConfig may be nil, in which case it
// is loaded from selfdoc.json.
func LintPostBuffer(
	dirPath, sourcePath, content string,
	projectConfig map[string]any,
	handle *effects.Handle,
) ([]lints.LintResult, error) {
	if projectConfig == nil {
		loaded, err := config.Load(dirPath)
		if err != nil {
			return nil, err
		}
		projectConfig = loaded
	}
	if projectConfig == nil {
		return nil, errors.New(
			"No selfdoc.json found. Run 'selfdoc init' to initialize.",
		)
	}

	postsDirRelative, postsDir := postsDirectory(projectConfig, dirPath)
	if postsDir == "" {
		declared := postsDirRelative
		if declared == "" {
			declared = "(unset)"
		}
		return nil, fmt.Errorf(
			"%s has no posts directory at %s, so a post buffer has nowhere to "+
				"be checked against.", dirPath, declared,
		)
	}

	res, err := resolver.MakeResolver(projectConfig, dirPath, handle)
	if err != nil {
		return nil, err
	}
	validNames, err := docs.ValidNames(projectConfig)
	if err != nil {
		return nil, err
	}

	postDocs, err := postLintDocs(
		projectConfig, dirPath, res, validNames,
		map[string]string{sourcePath: content}, true, handle,
	)
	if err != nil {
		return nil, err
	}

	docsDir := filepath.Join(
		dirPath, strings.TrimRight(configString(projectConfig, "docs", layout.DocsDefault), "/"),
	)
	vocab, err := vocabulary.Load(dirPath)
	if err != nil {
		return nil, err
	}
	produced, err := runLints(postDocs, dirPath, docsDir, projectConfig, nil, vocab, handle)
	if err != nil {
		return nil, err
	}

	key := util.PathJoin(strings.TrimRight(postsDirRelative, "/"), sourcePath)
	var own []lints.LintResult
	for _, lint := range produced {
		if lint.File() == key {
			own = append(own, lint)
		}
	}
	return own, nil
}
