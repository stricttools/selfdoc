package assembly

import (
	stdhtml "html"
	"os"
	"path"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/resolution"
)

// RelativizeSiteLinks re-expresses every clickable link in the assembled tree
// that names the site's own base as a document-relative reference, and returns
// the site-relative paths it changed.
//
// A link a reader clicks has to resolve under whatever mount the tree is
// served from -- production, a preview, a mirror -- so the one addressed at
// "https://<site>/blog/" is wrong in a way the file-existence half of the
// resolution check can never see: the page it names really is there, on
// production, which is where the click quietly goes from everywhere else.
// [github.com/stricttools/selfdoc/internal/resolution] states the rule and refuses a
// tree that breaks it.
//
// This runs over every page in the tree, beside the chrome re-pointing pass
// and for the same reason. The assembly is never rebuilt whole: a project's
// subtree is replaced only when that project deploys, so pages an older
// toolchain wrote outlive it, and a rule the verification applies to the whole
// tree would otherwise refuse every deploy over pages the dispatch did not
// write and could not fix. Sweeping them here is what lets the tree converge.
//
// pages are site-relative HTML paths, as [chrome.EmittedPages] returns them.
// canonicalBase is the site's own base URL; a reference to any other host is
// somebody else's address and is left alone.
func RelativizeSiteLinks(
	siteDir, canonicalBase string, pages []string, h *effects.Handle,
) ([]string, error) {
	base := strings.TrimRight(canonicalBase, "/")
	if base == "" {
		return nil, errorf("canonical base is required to recognise a link that names this site")
	}
	changed := make([]string, 0)
	for _, pageRel := range pages {
		path := sitePath(siteDir, pageRel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pageHTML := string(raw)
		hop := chrome.SiteRootPrefix(pageRel)
		var refused error
		rewritten := resolution.RewriteNavigationReferences(
			pageHTML,
			func(ref string) (string, bool) {
				relative, ours, err := relativeSiteHref(ref, base, hop, siteDir)
				if err != nil && refused == nil {
					refused = errorf("%s: %s", pageRel, err)
				}
				return relative, ours
			},
		)
		if refused != nil {
			return nil, refused
		}
		if rewritten == pageHTML {
			continue
		}
		if err := h.AtomicWrite(path, []byte(rewritten), effects.ModeDefault); err != nil {
			return nil, err
		}
		changed = append(changed, pageRel)
	}
	return changed, nil
}

// relativeSiteHref returns the document-relative spelling of an href that
// names this site, and whether it named it at all.
//
// hop is the reference from the page carrying the link back to the site root,
// and siteDir the tree the link has to resolve in. Whether the link is this
// site's is asked of the resolution package, which is the authority the check
// itself reads; the cut is then made on the attribute as written, so whatever
// escaping the rest of the reference carries is kept untouched. An attribute
// the resolution package calls ours but whose base is spelled some other way
// than the value that answered the question -- an entity in the base, say --
// cannot be cut at a guessed offset and is a hard error: leaving it alone
// publishes a page the verification then refuses with no diagnostic naming it.
func relativeSiteHref(ref, base, hop, siteDir string) (string, bool, error) {
	if _, ours := resolution.SiteRelativePath(stdhtml.UnescapeString(ref), base); !ours {
		return "", false, nil
	}
	rest := ""
	switch {
	case ref == base:
	case strings.HasPrefix(ref, base+"/"):
		rest = ref[len(base)+1:]
	default:
		return "", false, errorf(
			"the link %q names this site, but its base is not spelled the way "+
				"the site's own base is and cannot be re-expressed relative to "+
				"the page", ref,
		)
	}
	relative := hop + directorySlashed(siteDir, rest)
	if relative == "" {
		// The site root, addressed from a page at the site root.
		relative = "./"
	}
	return relative, true, nil
}

// directorySlashed is rest with a trailing slash on its path part when that
// part names a directory the tree emitted a page into.
//
// A base written without its trailing slash -- "https://<site>/blog" -- names
// no file: what the build emitted is "blog/index.html", which a reader reaches
// through the directory. Cutting the spelling as written would produce a
// reference to a file that is not there, and the resolution check refuses the
// tree over it. Anything else -- a file in the tree, a path naming nothing --
// keeps the spelling it was written with.
func directorySlashed(siteDir, rest string) string {
	cut := len(rest)
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		cut = i
	}
	pathPart, tail := rest[:cut], rest[cut:]
	if pathPart == "" || strings.HasSuffix(pathPart, "/") {
		return rest
	}
	// Cleaned against the root so the lookup cannot leave the tree: a
	// remainder that climbs out is asked about as the path it resolves to.
	inside := path.Clean("/" + stdhtml.UnescapeString(pathPart))
	page := sitePath(siteDir, strings.TrimPrefix(inside, "/")+"/index.html")
	if info, err := os.Stat(page); err != nil || info.IsDir() {
		return rest
	}
	return pathPart + "/" + tail
}
