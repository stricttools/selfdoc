package assembly

import (
	"os"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// RefreshSiblingBlocks regenerates the sibling block on every page in the
// assembled tree and returns the site-relative paths it changed.
//
// The block a page ends with -- every other project the site publishes, each
// with the one-line description its manifest carries -- is rendered at build
// time from the manifests present that day. The assembly is never rebuilt
// whole: a project's subtree is replaced only when that project deploys. So a
// roster change reaches one project's pages and no others, and every other
// project keeps listing the membership of its own last deploy. A project that
// retires leaves its address linked from every page it was never published
// beside, and the verification that reads the whole tree then refuses every
// following deploy over links no single project's rebuild can reach.
//
// This runs over every emitted page on every deploy, beside the chrome
// re-pointing and the link repair and for the same reason: sweeping pages the
// dispatch did not write is what lets the tree converge on the membership as
// it is now.
//
// Which project a page belongs to decides which sibling it does not list --
// its own -- and is read from the published-file records, the one place that
// tells a post apart from the project that published it. A page under a
// project's slug that no record claims belongs to that project by its address.
// A page neither answers for is the site's own: it lists every project, and
// is never given a block it does not already carry, because the site's
// generated pages are written fresh by every deploy and carry none by design.
//
// pages are site-relative HTML paths, as [chrome.EmittedPages] returns them.
func RefreshSiblingBlocks(
	siteDir, manifestsDir string,
	manifests []map[string]any,
	homeSlug string,
	pages []string,
	h *effects.Handle,
) ([]string, error) {
	owners, err := pageOwners(manifestsDir, manifests)
	if err != nil {
		return nil, err
	}
	slugs := map[string]bool{}
	for _, manifest := range manifests {
		if slug := util.PythonStrOrEmpty(manifest["slug"]); slug != "" {
			slugs[slug] = true
		}
	}

	ordered := make([]string, len(pages))
	copy(ordered, pages)
	sort.Strings(ordered)

	// The list for a slug is the same on every page of that project, so it is
	// rendered from the manifests once and reused.
	siblings := map[string][]build.SiblingProject{}
	changed := make([]string, 0)
	for _, pageRel := range ordered {
		selfSlug, published := pageProject(pageRel, owners, slugs)
		list, known := siblings[selfSlug]
		if !known {
			list = build.SiblingsFromManifests(manifests, homeSlug, selfSlug)
			siblings[selfSlug] = list
		}
		path := sitePath(siteDir, pageRel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pageHTML := string(raw)
		if _, _, carries := build.SiblingsBlockRange(pageHTML); !carries && !published {
			continue
		}
		refreshed := build.WithRefreshedSiblings(
			pageHTML, chrome.SiteRootPrefix(pageRel), list,
		)
		if refreshed == pageHTML {
			continue
		}
		if err := h.AtomicWrite(path, []byte(refreshed), effects.ModeDefault); err != nil {
			return nil, err
		}
		changed = append(changed, pageRel)
	}
	return changed, nil
}

// pageProject is the slug of the project that published the page at pageRel,
// and whether a project published it at all.
//
// A page no project published is the site's own -- the generated listing, the
// blog index, the 404 page -- and belongs to no slug, so it leaves nothing out
// of its list.
func pageProject(pageRel string, owners map[string]string, slugs map[string]bool) (string, bool) {
	if slug, claimed := owners[pageRel]; claimed {
		return slug, true
	}
	head := pageRel
	if index := strings.Index(pageRel, "/"); index >= 0 {
		head = pageRel[:index]
	} else {
		head = ""
	}
	if head != "" && slugs[head] {
		return head, true
	}
	return "", false
}

// pageOwners maps every site-relative HTML page a project published to that
// project's slug, read from the published-file records.
//
// The records are what the graft writes: one per project, naming every path
// that project put in the tree, its site-level posts included. They are the
// only answer to which project a page at a site-level address came from.
//
// A path two records claim is kept for the first slug in sorted order, so the
// pass is the same on every run. The graft refuses a project writing over
// another's post, so a contested path means a record left behind by a publish
// that is no longer the tree's; either answer lists the same projects but one.
func pageOwners(manifestsDir string, manifests []map[string]any) (map[string]string, error) {
	slugs := make([]string, 0, len(manifests))
	for _, manifest := range manifests {
		if slug := util.PythonStrOrEmpty(manifest["slug"]); slug != "" {
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)
	owners := map[string]string{}
	for _, slug := range slugs {
		record, err := site.LoadFilesManifest(site.FilesManifestPath(manifestsDir, slug))
		if err != nil {
			return nil, err
		}
		for _, paths := range record {
			for _, path := range paths {
				if !strings.HasSuffix(path, ".html") {
					continue
				}
				if _, claimed := owners[path]; claimed {
					continue
				}
				owners[path] = slug
			}
		}
	}
	return owners, nil
}
