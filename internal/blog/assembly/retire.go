package assembly

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// RetireSummary is what one [RetireProject] call did.
type RetireSummary struct {
	// Slug is the project retired.
	Slug string
	// Deleted is every repository path the commit removed, sorted.
	Deleted []string
	// Remaining is the slugs the roster still declares, sorted.
	Remaining []string
	// Push is the commit that carried the retirement.
	Push PushResult
}

// RetireProject removes slug from the assembly's roster and tree in one commit.
//
// Retirement is a roster edit plus the reconciliation that edit implies, done
// together so the published site never lags the declaration: the [[project]]
// block goes, the derived membership record loses its entry, and every path the
// project owns -- its whole section and all its manifest kinds -- is deleted in
// the same commit. What remains is the shared elements, which the caller
// regenerates by dispatching a shared-only rebuild ([SharedOnlyDispatch]); that
// pass also rebuilds the search index, so the retired project stops answering
// searches.
//
// The rewritten roster lists the remaining projects in slug order. The Python
// rewrote them in the document's own declaration order, which the parsed roster
// no longer carries.
func RetireProject(h *effects.Handle, repo, slug, branch string) (*RetireSummary, error) {
	roster, err := LoadRemoteRoster(h, repo)
	if err != nil {
		return nil, err
	}
	if !roster.Has(slug) {
		declared := strings.Join(roster.Slugs(), ", ")
		if declared == "" {
			declared = "(none)"
		}
		return nil, errorf(
			"%s is not declared in %s on %s, so there is nothing to retire. "+
				"Declared projects: %s.",
			util.PythonRepr(slug), site.RosterPath, repo, declared,
		)
	}
	if slug == roster.Home {
		return nil, errorf(
			"%s is the home project: %s on %s names it home, so it is the "+
				"site's front page and every page it serves is at the site "+
				"root. Retiring it would leave the site with no front page. "+
				"Name another declared project home first, then retire this "+
				"one.",
			util.PythonRepr(slug), site.RosterPath, repo,
		)
	}

	entries := roster.Entries()
	var remaining []site.RosterEntry
	var remainingSlugs []string
	for _, name := range roster.Slugs() {
		if name == slug {
			continue
		}
		remaining = append(remaining, entries[name])
		remainingSlugs = append(remainingSlugs, name)
	}

	recordPath := "manifests/" + slug + "-files.json"
	// A project that published nothing outside its subtree has no record, and
	// that is a real state. A failed read is not: retirement would compute an
	// empty claim set and leave the project's posts on the site-level blog
	// with nothing left to explain where they came from.
	raw, err := FetchRemoteText(
		h, repo, recordPath, true,
		"retire "+util.PythonRepr(slug)+" from "+repo,
	)
	if err != nil {
		return nil, err
	}
	record, err := site.ParseFilesManifest(raw, repo+":"+recordPath)
	if err != nil {
		return nil, err
	}
	claimed := map[string]bool{}
	for _, owner := range site.PublishOwners {
		for _, path := range record[owner] {
			if !strings.HasPrefix(path, slug+"/") {
				claimed[path] = true
			}
		}
	}
	claimedPaths := make([]string, 0, len(claimed))
	for path := range claimed {
		claimedPaths = append(claimedPaths, path)
	}
	sort.Strings(claimedPaths)

	remotePaths, err := ListRemotePaths(h, repo, branch)
	if err != nil {
		return nil, err
	}
	deleted := site.ProjectPaths(remotePaths, slug, claimedPaths)

	files := map[string][]byte{
		site.RosterPath: []byte(site.RenderRoster(remaining, roster.Home)),
	}

	// No membership record yet is a real state; a failed read is not, and
	// would silently leave the retired project's entry behind.
	rawMembership, err := FetchRemoteText(
		h, repo, site.ProjectsPath, true,
		"retire "+util.PythonRepr(slug)+" from "+repo,
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rawMembership) != "" {
		membership, err := decodeMembership(repo, rawMembership)
		if err != nil {
			return nil, err
		}
		if _, present := membership[slug]; present {
			delete(membership, slug)
			rendered, err := site.RenderProjectsJSON(membership)
			if err != nil {
				return nil, err
			}
			files[site.ProjectsPath] = []byte(rendered)
		}
	}

	push, err := PushFilesToRepo(
		h, repo, files, "assembly: retire "+slug, branch, deleted,
	)
	if err != nil {
		return nil, err
	}
	return &RetireSummary{
		Slug:      slug,
		Deleted:   deleted,
		Remaining: remainingSlugs,
		Push:      push,
	}, nil
}

// decodeMembership decodes the derived membership record read off a remote,
// naming the repository in every refusal.
func decodeMembership(repo, raw string) (map[string]any, error) {
	var document any
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return nil, errorf("%s:%s is not valid JSON: %s", repo, site.ProjectsPath, err)
	}
	membership, ok := document.(map[string]any)
	if !ok {
		return nil, errorf("%s:%s must contain a JSON object", repo, site.ProjectsPath)
	}
	return membership, nil
}
