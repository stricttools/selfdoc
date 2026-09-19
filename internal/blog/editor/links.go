package editor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/util"
)

// Cross-repository link targets, addressed the way a post has to address
// them.
//
// A post is a site citizen. It is emitted at "blog/<post-slug>/" on the site
// root -- in a standalone build and on the assembled site alike -- while a
// project's documentation is served under that project's own slug. So a link
// from a post to a project page is not the address that project's own pages
// use between themselves: it is the project-mounted address, reached from two
// directories down.
//
// Both halves of that sentence are read from the authority rather than
// restated here. [github.com/stricttools/selfdoc/internal/blog/shared.PageTarget]
// decides where a manifest page lives on the site, and
// [github.com/stricttools/selfdoc/internal/blog/shared.PostTarget] decides where a
// post lives; the number of hops back to the site root is derived from the
// second, so a change to either address scheme moves these links with it.
//
// # What is offered
//
// Every local registry entry's manifest: each page (title and
// address) and each of its headings (the manifests carry heading anchors, and
// the anchor a manifest records is the id the built page really has). The
// result is one flat list across every repository, because a post's link does
// not care which project wrote the page it points at.
//
// # The home project
//
// The assembly's roster names one project served at the site root instead of
// under its slug, and its pages carry no project segment. The editor cannot
// know which project that is: the roster lives in the assembly repository,
// and the editor never contacts it. So every target is offered at its
// project-mounted address -- which is the right answer for every project but
// one, and a wrong answer the deploy's own reference check reports rather
// than a wrong page silently served.

// PostDepth is how many directories a post sits below the site root. Derived
// from the post address itself ("blog/<slug>/index.html" -> 2) so the hop and
// the address can never disagree.
var PostDepth = len(strings.Split(shared.TargetOutputPath(shared.PostTarget("slug")), "/")) - 1

// ToSiteRoot is the relative hop from a post's own directory back to the site
// root.
var ToSiteRoot = strings.Repeat("../", PostDepth)

// ManifestRel is where a project keeps the manifest the editor reads.
const ManifestRel = layout.ManifestRel

// ManifestError reports that a manifest exists but cannot be read, and the
// message says how.
type ManifestError struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *ManifestError) Error() string { return e.Message }

// Target is one link target: a page, or a heading on one.
//
// Each target carries the address twice: Address is site-relative (what the
// assembly serves it at) and Href is what a post writes to reach it. Both are
// derived, never typed out.
//
// PageTitle and Level are carried by a section target only, which is why they
// are pointers: a page target's own title is Title, and a page has no heading
// level. The Python built two differently-keyed dicts for the two kinds, and
// the absent members are how that shape is reproduced.
type Target struct {
	Kind      string
	Repo      string
	Slug      string
	Project   string
	Title     string
	Page      string
	PageTitle *string
	Level     *int
	Anchor    string
	Address   string
	Href      string
}

// jsonMembers states the members the payload carries, in order: a section
// target carries the page it sits on and its heading level, and a page target
// carries neither key at all -- which is the shape the Python built as two
// differently-keyed dicts.
func (t Target) jsonMembers() jsonObject {
	members := jsonObject{
		{"kind", t.Kind},
		{"repo", t.Repo},
		{"slug", t.Slug},
		{"project", t.Project},
		{"title", t.Title},
		{"page", t.Page},
	}
	if t.Kind == "section" {
		pageTitle := ""
		if t.PageTitle != nil {
			pageTitle = *t.PageTitle
		}
		members = append(members,
			jsonMember{"page_title", pageTitle},
			jsonMember{"level", t.Level},
		)
	}
	return append(members,
		jsonMember{"anchor", t.Anchor},
		jsonMember{"address", t.Address},
		jsonMember{"href", t.Href},
	)
}

// TargetHref returns the link a post writes to reach pagePath of
// projectSlug.
//
// Document-relative, from the post's own emitted directory: the site has to
// resolve under any mount point, so an origin-absolute link would be a defect
// the reference check reports.
func TargetHref(projectSlug, pagePath, anchor string) string {
	href := ToSiteRoot + shared.PageTarget(projectSlug, pagePath, false)
	if anchor != "" {
		return href + "#" + anchor
	}
	return href
}

// LoadManifest reads one project manifest.
//
// It returns a nil manifest and no error when there is no manifest at path.
// Absence is genuine: a project that has never been built has no manifest,
// and offering nothing from it is the correct answer.
//
// A file that exists and is not readable JSON is a [ManifestError]. It was
// written by a build that meant something by it, so guessing is worse than
// saying so.
func LoadManifest(path string) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, &ManifestError{Message: fmt.Sprintf(
			"%s is not a readable manifest: %v", path, err)}
	}
	var document any
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, &ManifestError{Message: fmt.Sprintf(
			"%s is not a readable manifest: %v", path, err)}
	}
	manifest, ok := document.(map[string]any)
	if !ok {
		return nil, &ManifestError{Message: fmt.Sprintf(
			"%s is not a readable manifest: not a JSON object", path)}
	}
	return manifest, nil
}

// ManifestTargets returns every link target one manifest offers, pages first,
// then the sections of each page.
func ManifestTargets(manifest map[string]any, repoName string) []Target {
	slug := util.PythonStrOrEmpty(manifest["slug"])
	project := util.PythonStrOrEmpty(manifest["name"])
	if project == "" {
		project = slug
	}

	targets := []Target{}
	pages, _ := manifest["pages"].([]any)
	for _, rawPage := range pages {
		page, isMap := rawPage.(map[string]any)
		if !isMap {
			continue
		}
		path := util.PythonStrOrEmpty(page["path"])
		if path == "" {
			continue
		}
		title := util.PythonStrOrEmpty(page["title"])
		if title == "" {
			title = path
		}
		targets = append(targets, Target{
			Kind:    "page",
			Repo:    repoName,
			Slug:    slug,
			Project: project,
			Title:   title,
			Page:    path,
			Anchor:  "",
			Address: shared.PageTarget(slug, path, false),
			Href:    TargetHref(slug, path, ""),
		})

		headings, _ := page["headings"].([]any)
		for _, rawHeading := range headings {
			heading, isHeadingMap := rawHeading.(map[string]any)
			if !isHeadingMap {
				continue
			}
			anchor := util.PythonStrOrEmpty(heading["anchor"])
			text := util.PythonStrOrEmpty(heading["text"])
			if anchor == "" || text == "" {
				continue
			}
			pageTitle := title
			target := Target{
				Kind:      "section",
				Repo:      repoName,
				Slug:      slug,
				Project:   project,
				Title:     text,
				Page:      path,
				PageTitle: &pageTitle,
				Level:     headingLevel(heading["level"]),
				Anchor:    anchor,
				Address:   shared.PageTarget(slug, path, false) + "#" + anchor,
				Href:      TargetHref(slug, path, anchor),
			}
			targets = append(targets, target)
		}
	}
	return targets
}

// headingLevel reads a manifest heading's level, which a decoded document
// carries as a number. A heading that declares none carries none.
func headingLevel(declared any) *int {
	switch typed := declared.(type) {
	case float64:
		level := int(typed)
		return &level
	case int:
		level := typed
		return &level
	case int64:
		level := int(typed)
		return &level
	default:
		return nil
	}
}

// TargetIndex is every registry entry's link targets, re-read when a manifest
// changes.
//
// The editor asks for completions on a keystroke, so the manifests are not
// re-parsed each time -- but they are also not cached forever: the key is the
// manifest's own size and modification time, so a build that rewrites one is
// picked up on the next keystroke with no restart.
//
// It is safe to use from several goroutines: the cache carries its own mutex,
// because the completions of two open shells are two concurrent requests.
type TargetIndex struct {
	registry *registry.Registry

	mu    sync.Mutex
	cache map[string]cachedTargets
}

// cachedTargets is one entry's targets, keyed on the manifest's stamp.
type cachedTargets struct {
	stamp   manifestStamp
	targets []Target
}

// manifestStamp is a manifest's identity for the cache: its modification time
// and its size.
type manifestStamp struct {
	modTimeNanos int64
	size         int64
}

// NewTargetIndex builds the index over a registry.
func NewTargetIndex(reg *registry.Registry) *TargetIndex {
	return &TargetIndex{registry: reg, cache: map[string]cachedTargets{}}
}

// entryTargets returns one entry's targets, from the cache when the manifest
// has not moved.
func (index *TargetIndex) entryTargets(entry registry.Entry) ([]Target, error) {
	path, err := RequireLocal(entry)
	if err != nil {
		// A remote entry is validated but not served; its manifest is in a
		// repository this editor does not fetch.
		return nil, nil
	}
	manifestPath := util.PathJoin(path, ManifestRel)

	info, statErr := os.Stat(manifestPath)
	if statErr != nil {
		index.mu.Lock()
		delete(index.cache, entry.Name())
		index.mu.Unlock()
		return nil, nil
	}
	stamp := manifestStamp{modTimeNanos: info.ModTime().UnixNano(), size: info.Size()}

	index.mu.Lock()
	cached, found := index.cache[entry.Name()]
	index.mu.Unlock()
	if found && cached.stamp == stamp {
		return cached.targets, nil
	}

	manifest, loadErr := LoadManifest(manifestPath)
	if loadErr != nil {
		return nil, loadErr
	}
	targets := []Target{}
	if manifest != nil {
		targets = ManifestTargets(manifest, entry.Name())
	}

	index.mu.Lock()
	index.cache[entry.Name()] = cachedTargets{stamp: stamp, targets: targets}
	index.mu.Unlock()
	return targets, nil
}

// AllTargets returns every target from every local registry entry, in
// registry order.
func (index *TargetIndex) AllTargets() ([]Target, error) {
	found := []Target{}
	for _, entry := range index.registry.Entries {
		targets, err := index.entryTargets(entry)
		if err != nil {
			return nil, err
		}
		found = append(found, targets...)
	}
	return found, nil
}

// Search returns the targets matching query, pages before their own sections,
// and at most limit of them.
//
// The match is a case-insensitive substring over what an author would type:
// the title, the address, the project's name and the page path. An empty
// query matches everything, which is what makes the popup useful the moment a
// link is opened rather than only after some prefix is typed.
func (index *TargetIndex) Search(query string, limit int) ([]Target, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	all, err := index.AllTargets()
	if err != nil {
		return nil, err
	}
	found := []Target{}
	for _, target := range all {
		if needle != "" && !matchesTarget(target, needle) {
			continue
		}
		found = append(found, target)
		if len(found) >= limit {
			break
		}
	}
	return found, nil
}

// matchesTarget reports whether one target answers a lowercased needle.
func matchesTarget(target Target, needle string) bool {
	for _, field := range []string{target.Title, target.Address, target.Project, target.Page} {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}
