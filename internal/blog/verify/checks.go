package verify

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/blog/chrome"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/sitedirectives"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/resolution"
	"github.com/stricttools/selfdoc/internal/util"
)

// Error is the failure every operation in this package reports when it cannot
// read the tree it was asked to verify: a missing roster, a manifest that is
// not JSON, a file that vanished between the walk and the read.
//
// A violated property is a Failure rather than an error: the point of a
// verification is to report every one of them at once.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

func errorf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// CheckRosterAgreement asserts that the roster, the site subtrees and the
// manifests name the same projects.
func CheckRosterAgreement(tree *AssemblyTree) ([]Failure, error) {
	declared := map[string]bool{}
	for _, slug := range tree.Roster.Slugs() {
		declared[slug] = true
	}
	var failures []Failure

	// The home project's own directories sit at the site root beside the
	// project subtrees, so they are named here rather than mistaken for
	// projects nobody declared.
	homeDirs, err := site.HomeOwnedRootNames(tree.ManifestsDir, tree.Home)
	if err != nil {
		return nil, err
	}

	subtrees := map[string]bool{}
	if entries, err := os.ReadDir(tree.SiteDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			info, err := os.Stat(filepath.Join(tree.SiteDir, name))
			if err != nil || !info.IsDir() {
				continue
			}
			if contains(site.SiteReservedDirs, name) || homeDirs[name] {
				continue
			}
			subtrees[name] = true
		}
	}

	for _, name := range sortedKeys(subtrees) {
		if declared[name] {
			continue
		}
		failures = append(failures, Failure{
			"roster-agreement", "site/" + name,
			fmt.Sprintf("a subtree no [[project]] block declares. Membership is "+
				"declared, never accumulated: add %s to the roster or "+
				"retire it.", util.PythonRepr(name)),
		})
	}
	// The home project has no subtree by definition: the site root is its
	// subtree. Its own assertions are CheckHomeProject's.
	for _, slug := range sortedKeys(declared) {
		if subtrees[slug] || slug == tree.Home {
			continue
		}
		failures = append(failures, Failure{
			"roster-agreement", slug,
			"declared in the roster but has no site/ subtree, so the site " +
				"would list a project it cannot serve.",
		})
	}

	if entries, err := os.ReadDir(tree.ManifestsDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			if _, ok := manifestOwner(strings.TrimSuffix(name, ".json"), declared); !ok {
				failures = append(failures, Failure{
					"roster-agreement", "manifests/" + name,
					"an orphan manifest: its slug is not declared in the roster.",
				})
			}
		}
	}

	for _, slug := range sortedKeys(declared) {
		if _, ok := tree.ManifestFiles[slug]; !ok {
			failures = append(failures, Failure{
				"roster-agreement", slug,
				"declared in the roster but has no manifests/<slug>.json, " +
					"so it appears in no listing, feed or sitemap.",
			})
		}
	}

	membership, err := site.LoadProjectsJSON(
		filepath.Join(tree.AssemblyDir, site.ProjectsPath),
	)
	if err != nil {
		return nil, err
	}
	recorded := make([]string, 0, len(membership))
	for slug := range membership {
		recorded = append(recorded, slug)
	}
	sort.Strings(recorded)
	for _, slug := range recorded {
		if declared[slug] {
			continue
		}
		failures = append(failures, Failure{
			"roster-agreement", site.ProjectsPath + ":" + slug,
			"a membership record for a project the roster does not declare.",
		})
	}
	return failures, nil
}

// CheckHomeProject asserts that the home project is served at the site root,
// once, and only there.
//
// Four properties, each a way the front page could quietly stop being the
// front page:
//
//   - No site/<home>/ subtree. The home project's content root is the site
//     root, so a subtree under its own slug is residue from before it was
//     named home -- two copies of the same pages, one of them stale and
//     neither one obviously wrong.
//   - No page of its at an address the assembly owns.
//   - Every site-level directive region it emitted is closed and holds
//     something. An empty region is a front page that lost its listing.
//   - It is absent from the generated listing and from nav: the front page
//     does not list itself.
func CheckHomeProject(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	home := tree.Home
	if home == "" {
		return failures, nil
	}

	if info, err := os.Stat(filepath.Join(tree.SiteDir, home)); err == nil && info.IsDir() {
		failures = append(failures, Failure{
			"home-project", "site/" + home,
			fmt.Sprintf("is a subtree for the home project, which is served at "+
				"the site root and has no subtree of its own. It is residue "+
				"from before %s was named home, and it serves a second, stale "+
				"copy of every page the site root already serves.",
				util.PythonRepr(home)),
		})
	}

	homeDirs, err := site.HomeOwnedRootNames(tree.ManifestsDir, home)
	if err != nil {
		return nil, err
	}
	for _, name := range sortedKeys(homeDirs) {
		if tree.Roster.Has(name) {
			failures = append(failures, Failure{
				"home-project", "site/" + name,
				fmt.Sprintf("is a directory the home project publishes at the "+
					"site root, and %s is also a declared project's subtree. "+
					"One of the two would overwrite the other; rename the home "+
					"project's page.", util.PythonRepr(name)),
			})
		}
	}

	published, err := site.HomePagePaths(tree.ManifestsDir, home)
	if err != nil {
		return nil, err
	}
	for _, collision := range site.HomeCollisions(published) {
		failures = append(failures, Failure{
			"home-project", "site/" + collision.Path,
			fmt.Sprintf("is published by the home project at an address the "+
				"assembly owns: %s.", collision.Why),
		})
	}

	for _, rel := range published {
		if !tree.Emitted[rel] {
			continue
		}
		pageHTML, err := tree.Read(rel)
		if err != nil {
			return nil, err
		}
		for _, name := range sitedirectives.FindUnclosedRegions(pageHTML) {
			failures = append(failures, Failure{
				"home-project", "site/" + rel,
				fmt.Sprintf("carries a site-level region %s that opens and "+
					"never closes, so nothing can re-render it.",
					util.PythonRepr(name)),
			})
		}
		for _, region := range sitedirectives.FindRegions(pageHTML) {
			if util.PythonStrip(region.Body) != "" {
				continue
			}
			failures = append(failures, Failure{
				"home-project", "site/" + rel,
				fmt.Sprintf("carries an empty site-level region %s: the page "+
					"was published with a placeholder where its generated "+
					"content belongs.", util.PythonRepr(region.Name)),
			})
		}
	}

	if tree.Emitted["nav.json"] {
		text, err := tree.Read("nav.json")
		if err != nil {
			return nil, err
		}
		decoded, decodeErr := config.DecodeDocument([]byte(text))
		nav, isTable := decoded.(map[string]any)
		if decodeErr == nil && isTable {
			listed := map[string]bool{}
			if projects, ok := nav["projects"].([]any); ok {
				for _, item := range projects {
					entry, ok := item.(map[string]any)
					if !ok {
						continue
					}
					listed[util.PythonStrOrEmpty(entry["slug"])] = true
				}
			}
			if listed[home] {
				failures = append(failures, Failure{
					"home-project", "site/nav.json",
					fmt.Sprintf("lists the home project %s among the projects. "+
						"The home project is the site root every nav points "+
						"back to, not an entry in the project set.",
						util.PythonRepr(home)),
				})
			}
		}
	}
	return failures, nil
}

// CheckManifestIdentity asserts that each manifest names its own directory and
// the version on disk.
func CheckManifestIdentity(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, slug := range sortedMapKeys(tree.OverlayFiles) {
		declared := util.PythonStrOrEmpty(tree.OverlayFiles[slug]["slug"])
		if declared != slug {
			failures = append(failures, Failure{
				"manifest-identity",
				fmt.Sprintf("manifests/%s-posts.json", slug),
				fmt.Sprintf("declares slug %s, but it is %s's post overlay. "+
					"One overlay belongs to one project.",
					util.PythonRepr(declared), slug),
			})
		}
	}
	for _, stem := range sortedMapKeys(tree.ManifestFiles) {
		data := tree.ManifestFiles[stem]
		slug := util.PythonStrOrEmpty(data["slug"])
		if slug != stem {
			failures = append(failures, Failure{
				"manifest-identity",
				fmt.Sprintf("manifests/%s.json", stem),
				fmt.Sprintf("declares slug %s, but it is the manifest for %s. "+
					"One manifest describes one directory.",
					util.PythonRepr(slug), util.PythonRepr(stem)),
			})
			continue
		}
		version := util.PythonStrOrEmpty(data["version"])
		if version == "" {
			continue
		}
		archived := filepath.Join(tree.SiteDir, slug, "v", version)
		if info, err := os.Stat(archived); err == nil && info.IsDir() {
			failures = append(failures, Failure{
				"manifest-identity",
				fmt.Sprintf("site/%s/v/%s", slug, version),
				fmt.Sprintf("the current version %s is also emitted as an "+
					"archive. The current version lives at the stable address; "+
					"only superseded ones sit under v/.", version),
			})
		}
		prefix := slug + "/"
		for _, pageRel := range tree.Pages {
			if !strings.HasPrefix(pageRel, prefix) {
				continue
			}
			if strings.HasPrefix(pageRel, slug+"/v/") {
				continue
			}
			// Posts are not under this prefix at all: they are site-level, at
			// blog/<post-slug>/. Their version disagreeing with the manifest's
			// is the normal state -- a post published between releases comes
			// from a working tree the released version knows nothing about --
			// and no project subtree check sees them.
			pageHTML, err := tree.Read(pageRel)
			if err != nil {
				return nil, err
			}
			found := map[string]bool{}
			for _, match := range versionAttrRE.FindAllStringSubmatch(pageHTML, -1) {
				found[match[1]] = true
			}
			for _, declaredVersion := range sortedKeys(found) {
				if declaredVersion != version {
					failures = append(failures, Failure{
						"manifest-identity", "site/" + pageRel,
						fmt.Sprintf("was built at version %s, but "+
							"manifests/%s.json says %s. The manifest and the "+
							"tree come from different builds.",
							declaredVersion, slug, version),
					})
				}
			}
		}
	}
	return failures, nil
}

// CheckManifestPagesEmitted asserts that every page a manifest lists resolves
// to an emitted file.
func CheckManifestPagesEmitted(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, manifest := range tree.Manifests {
		slug := util.PythonStrOrEmpty(manifest["slug"])
		isHome := tree.Home != "" && slug == tree.Home
		pages, _ := manifest["pages"].([]any)
		for _, item := range pages {
			page, _ := item.(map[string]any)
			pagePath := ""
			if page != nil {
				pagePath = util.PythonStrOrEmpty(page["path"])
			}
			target := shared.TargetOutputPath(shared.PageTarget(slug, pagePath, isHome))
			if !tree.Emitted[target] {
				failures = append(failures, Failure{
					"manifest-pages-emitted", slug + ":" + pagePath,
					fmt.Sprintf("is listed in the manifest but site/%s was not "+
						"emitted, so every listing linking to it is a 404.",
						target),
				})
			}
		}
	}
	return failures, nil
}

// CheckManifestPostsEmitted asserts that every post a manifest lists resolves
// to an emitted file.
func CheckManifestPostsEmitted(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, manifest := range tree.Manifests {
		slug := util.PythonStrOrEmpty(manifest["slug"])
		posts, _ := manifest["posts"].([]any)
		for _, item := range posts {
			post, _ := item.(map[string]any)
			postSlug := ""
			if post != nil {
				postSlug = util.PythonStrOrEmpty(post["slug"])
			}
			target := shared.TargetOutputPath(shared.PostTarget(postSlug))
			if !tree.Emitted[target] {
				failures = append(failures, Failure{
					"manifest-posts-emitted", slug + ":" + postSlug,
					fmt.Sprintf("is listed in the manifest but site/%s was not "+
						"emitted, so the blog index and the feed link to a 404.",
						target),
				})
			}
		}
	}
	return failures, nil
}

// CheckSharedArtifacts asserts that the cross-project files exist, parse, and
// are not empty.
func CheckSharedArtifacts(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure

	missing := func(rel, what string) {
		failures = append(failures, Failure{
			"shared-artifacts", "site/" + rel,
			fmt.Sprintf("%s is missing from the assembled tree.", what),
		})
	}
	unparsable := func(rel string, err error) {
		failures = append(failures, Failure{
			"shared-artifacts", "site/" + rel,
			fmt.Sprintf("does not parse: %v", err),
		})
	}

	required := [][2]string{
		{"index.html", "the home project's front page"},
		{"projects/index.html", "the generated project listing"},
		{"blog/index.html", "the blog index"},
		{"robots.txt", "robots.txt"},
		{"llms.txt", "the site-wide llms.txt"},
		{"404.html", "the root 404 page"},
	}
	for _, entry := range required {
		if !tree.Emitted[entry[0]] {
			missing(entry[0], entry[1])
		}
	}

	notFound, err := notFoundFailures(tree)
	if err != nil {
		return nil, err
	}
	failures = append(failures, notFound...)
	robots, err := robotsFailures(tree)
	if err != nil {
		return nil, err
	}
	failures = append(failures, robots...)
	llms, err := llmsFailures(tree)
	if err != nil {
		return nil, err
	}
	failures = append(failures, llms...)

	for _, entry := range [][2]string{
		{"sitemap.xml", "the sitemap"}, {"feed.xml", "the feed"},
	} {
		rel, what := entry[0], entry[1]
		if !tree.Emitted[rel] {
			missing(rel, what)
			continue
		}
		text, err := tree.Read(rel)
		if err != nil {
			return nil, err
		}
		if parseErr := parseXML(text); parseErr != nil {
			unparsable(rel, parseErr)
		}
	}

	if !tree.Emitted["nav.json"] {
		missing("nav.json", "the navigation data")
	} else {
		text, err := tree.Read("nav.json")
		if err != nil {
			return nil, err
		}
		if _, decodeErr := config.DecodeDocument([]byte(text)); decodeErr != nil {
			unparsable("nav.json", decodeErr)
		}
	}

	index, err := searchIndexFailures(tree)
	if err != nil {
		return nil, err
	}
	failures = append(failures, index...)
	return failures, nil
}

// parseXML reports whether text is a well-formed XML document.
func parseXML(text string) error {
	decoder := xml.NewDecoder(strings.NewReader(text))
	decoder.Strict = true
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// bodyText is the body of an HTML document with tags and whitespace flattened
// away.
func bodyText(pageHTML string) string {
	body := pageHTML
	if parts := strings.SplitN(body, "<body>", 2); len(parts) == 2 {
		body = parts[1]
	}
	body = strings.SplitN(body, "</body>", 2)[0]
	return strings.Join(util.PythonFields(tagRE.ReplaceAllString(body, " ")), " ")
}

// notFoundFailures asserts that the root 404 is a not-found page, not a second
// front page.
//
// The hosting provider serves 404.html with a 404 status for any address that
// matches no asset. If its body is the front page's, an unknown address
// renders the home page: a soft 404 that a crawler indexes as a duplicate of
// the site root and a reader mistakes for having arrived somewhere.
func notFoundFailures(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	if !tree.Emitted["404.html"] {
		return failures, nil
	}
	notFound, err := tree.Read("404.html")
	if err != nil {
		return nil, err
	}
	body := bodyText(notFound)
	if body == "" {
		failures = append(failures, Failure{
			"shared-artifacts", "site/404.html",
			"has an empty body, so an unknown address renders a blank page.",
		})
	}
	if tree.Emitted["index.html"] {
		front, err := tree.Read("index.html")
		if err != nil {
			return nil, err
		}
		if body == bodyText(front) {
			failures = append(failures, Failure{
				"shared-artifacts", "site/404.html",
				"renders the same body as the front page. An address that does " +
					"not exist would answer with the home page, which reads as " +
					"a page that exists.",
			})
		}
	}
	// The 404 sits at the site root, so the way back to each of the three is
	// the bare address -- written relative like every other link a reader
	// clicks, so it still works on a preview or a mirror.
	for _, way := range [][2]string{
		{"/", "./"},
		{"/projects/", "projects/"},
		{"/" + shared.PostsSegment + "/", shared.PostsSegment + "/"},
	} {
		rel, href := way[0], way[1]
		if !strings.Contains(notFound, `href="`+href+`"`) {
			failures = append(failures, Failure{
				"shared-artifacts", "site/404.html",
				fmt.Sprintf("offers no way back to %s: a dead end is what a "+
					"reader who lands here is left with.", rel),
			})
		}
	}
	return failures, nil
}

// robotsFailures asserts that robots.txt names the sitemap that is actually
// served, absolutely.
func robotsFailures(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	if !tree.Emitted["robots.txt"] {
		return failures, nil
	}
	robots, err := tree.Read("robots.txt")
	if err != nil {
		return nil, err
	}
	expected := fmt.Sprintf("Sitemap: %s/sitemap.xml", tree.CanonicalBase)
	if !strings.Contains(robots, expected) {
		failures = append(failures, Failure{
			"shared-artifacts", "site/robots.txt",
			fmt.Sprintf("does not carry %s. A crawler finds the sitemap by "+
				"being told where it is, and the site's one sitemap is at the "+
				"root.", util.PythonRepr(expected)),
		})
	}
	if !tree.Emitted["sitemap.xml"] {
		failures = append(failures, Failure{
			"shared-artifacts", "site/robots.txt",
			"names a sitemap the tree does not carry at site/sitemap.xml.",
		})
	}
	return failures, nil
}

// llmsFailures asserts that the site-wide llms.txt points at every project's
// own llms.txt.
//
// It composes by reference: a project missing from it is a project no
// model-facing reader is told exists.
func llmsFailures(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	if !tree.Emitted["llms.txt"] {
		return failures, nil
	}
	llms, err := tree.Read("llms.txt")
	if err != nil {
		return nil, err
	}
	for _, slug := range tree.Roster.Slugs() {
		if slug == tree.Home {
			continue
		}
		reference := fmt.Sprintf("%s/%s/llms.txt", tree.CanonicalBase, slug)
		if !strings.Contains(llms, reference) {
			failures = append(failures, Failure{
				"shared-artifacts", "site/llms.txt",
				fmt.Sprintf("does not reference %s, so %s is invisible to "+
					"anything reading the site's llms.txt.",
					reference, util.PythonRepr(slug)),
			})
		}
	}
	return failures, nil
}

// indexedPages returns the page count a pagefind language block reports, or 0.
func indexedPages(language any) int {
	block, ok := language.(map[string]any)
	if !ok {
		return 0
	}
	count, ok := block["page_count"].(int64)
	if !ok {
		return 0
	}
	return int(count)
}

// searchIndexFailures asserts that the search index answers searches, on what
// the runtime loads.
//
// This used to be "any file at all under pagefind/", which is not a statement
// about the index: pagefind writes its runtime JS and CSS whether or not it
// indexed a single page, so a directory holding only those passed while the
// site answered nothing. Three honest properties instead --
// pagefind-entry.json is the file the runtime fetches to discover its index,
// it parses, it reports at least one indexed page, and at least one per-page
// fragment exists to render a result from.
func searchIndexFailures(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	entryRel := "pagefind/pagefind-entry.json"

	if !tree.Emitted[entryRel] {
		failures = append(failures, Failure{
			"shared-artifacts", "site/" + entryRel,
			"is missing, so the search runtime has nothing to load and the " +
				"site answers no searches. It is written by the pagefind pass " +
				"over the assembled tree.",
		})
	} else {
		text, err := tree.Read(entryRel)
		if err != nil {
			return nil, err
		}
		entry, decodeErr := config.DecodeDocument([]byte(text))
		if decodeErr != nil {
			failures = append(failures, Failure{
				"shared-artifacts", "site/" + entryRel,
				fmt.Sprintf("does not parse: %v", decodeErr),
			})
			entry = nil
		}
		if entry != nil {
			var languages map[string]any
			if table, ok := entry.(map[string]any); ok {
				languages, _ = table["languages"].(map[string]any)
			}
			if len(languages) == 0 {
				failures = append(failures, Failure{
					"shared-artifacts", "site/" + entryRel,
					"declares no indexed language, so the search runtime " +
						"loads it and finds no index behind it.",
				})
			} else {
				indexed := 0
				for _, block := range languages {
					indexed += indexedPages(block)
				}
				if indexed < 1 {
					failures = append(failures, Failure{
						"shared-artifacts", "site/" + entryRel,
						fmt.Sprintf("reports no indexed page across %d declared "+
							"language(s), so every search returns nothing.",
							len(languages)),
					})
				}
			}
		}
	}

	fragments := false
	for rel := range tree.Emitted {
		if strings.HasPrefix(rel, "pagefind/fragment/") &&
			strings.HasSuffix(rel, ".pf_fragment") {
			fragments = true
			break
		}
	}
	if !fragments {
		failures = append(failures, Failure{
			"shared-artifacts", "site/pagefind/fragment",
			"holds no fragment, so a search that matches has no page record " +
				"to render a result from.",
		})
	}
	return failures, nil
}

// CheckReferences asserts that every emitted reference names a file the
// assembly wrote.
//
// One LINK001 pass over the assembled tree, reported under three checks: a
// sitemap entry, a feed link and a link on a page are three different things
// to get wrong.
func CheckReferences(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	// Nothing is exempt here: this is the assembled tree, where the
	// site-level regions' cross-project links are exactly what has to
	// resolve.
	diagnostics, err := resolution.CheckOutputResolution(
		tree.SiteDir, tree.CanonicalBase, "", nil,
	)
	if err != nil {
		return nil, err
	}
	for _, lint := range diagnostics {
		where := lint.File()
		base := ""
		if where != "" {
			base = path.Base(where)
		}
		var check string
		switch {
		case strings.HasSuffix(where, ".xml") && strings.Contains(base, "sitemap"):
			check = "sitemap-entries"
		case base == "feed.xml":
			check = "feed-links"
		default:
			check = "internal-references"
		}
		failures = append(failures, Failure{check, "site/" + where, lint.Message()})
	}
	foreign, err := foreignSitemapEntries(tree)
	if err != nil {
		return nil, err
	}
	return append(failures, foreign...), nil
}

// foreignSitemapEntries asserts that every <loc> in every sitemap is an
// address on this site.
//
// The resolution pass measures absolute URLs against the canonical base and
// says nothing about a URL that is not under it -- an external link on a page
// is not its business. A sitemap entry is different: a sitemap declares this
// site's own pages, so an entry on another host is either a stale address the
// site no longer answers or a page belonging to somebody else, and either way
// it is submitted to crawlers as ours. Verification is the only place a
// sitemap entry is checked at all, so an entry the resolution pass skips is an
// entry nothing checks.
func foreignSitemapEntries(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, rel := range tree.sortedEmitted() {
		if !(strings.HasSuffix(rel, ".xml") && strings.Contains(path.Base(rel), "sitemap")) {
			continue
		}
		text, err := tree.Read(rel)
		if err != nil {
			return nil, err
		}
		for _, match := range locRE.FindAllStringSubmatch(text, -1) {
			loc := html.UnescapeString(util.PythonStrip(match[1]))
			if loc == "" {
				continue
			}
			if _, ok := resolution.SiteRelativePath(loc, tree.CanonicalBase); ok {
				continue
			}
			failures = append(failures, Failure{
				"sitemap-entries", "site/" + rel,
				fmt.Sprintf("lists %s, which is not under the site's canonical "+
					"base %s. A sitemap declares this site's own pages, so an "+
					"entry on another host submits an address this site does "+
					"not serve.", loc, tree.CanonicalBase),
			})
		}
	}
	return failures, nil
}

// CheckPageMetadata asserts that every page has a title and a canonical under
// the canonical base.
//
// The 404 is the one page with no canonical, and its absence is the assertion
// rather than an exemption from one: it is the answer to every address the
// site does not serve, so it has no address of its own to name. Its title is
// still required -- a browser tab and a crawler both read it.
func CheckPageMetadata(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, pageRel := range tree.Pages {
		pageHTML, err := tree.Read(pageRel)
		if err != nil {
			return nil, err
		}
		title := titleRE.FindStringSubmatch(pageHTML)
		if title == nil || util.PythonStrip(title[1]) == "" {
			failures = append(failures, Failure{
				"page-metadata", "site/" + pageRel,
				"has no title, so every listing, tab and search result " +
					"showing it is blank.",
			})
		}
		if path.Base(pageRel) == NotFoundPage {
			if canonicalTagRE.MatchString(pageHTML) {
				failures = append(failures, Failure{
					"page-metadata", "site/" + pageRel,
					"declares a rel=canonical. A not-found page is the answer " +
						"to every address the site does not serve, so it has " +
						"no address of its own to declare canonical.",
				})
			}
			continue
		}
		var canonicals []string
		for _, tag := range canonicalTagRE.FindAllString(pageHTML, -1) {
			if match := hrefRE.FindStringSubmatch(tag); match != nil {
				canonicals = append(canonicals, match[1])
			}
		}
		if len(canonicals) == 0 {
			failures = append(failures, Failure{
				"page-metadata", "site/" + pageRel,
				"declares no rel=canonical. The site is reachable on more " +
					"than one host, so every page names which one is canonical.",
			})
			continue
		}
		for _, href := range canonicals {
			if _, ok := resolution.SiteRelativePath(href, tree.CanonicalBase); !ok {
				failures = append(failures, Failure{
					"page-metadata", "site/" + pageRel,
					fmt.Sprintf("declares the canonical %s, which is not under "+
						"the site's canonical base %s.", href, tree.CanonicalBase),
				})
			}
		}
	}
	return failures, nil
}

// CheckSiteChrome asserts that the site-level stylesheet exists and every page
// references it.
//
// This is the assertion the live site went without. The blog index, the
// project listing and the root 404 were wrapped by a function whose stylesheet
// parameter no caller passed, so they published as unstyled HTML and nothing
// said so -- the pages had titles, canonicals and links that all resolved, and
// every assertion made about a page passed.
//
// Two properties, one check. The asset has to be in the tree, and every page
// has to name it: a page naming its own subtree copy instead is a page a
// toolchain upgrade will not reach, and a page naming nothing is the defect
// itself. That the reference resolves is not asserted here -- it is a
// document-relative href like any other, and the LINK001 pass behind
// internal-references already measures it against the emitted tree.
func CheckSiteChrome(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	assets := false
	for rel := range tree.Emitted {
		if strings.HasPrefix(rel, chrome.Dir+"/") && strings.HasSuffix(rel, ".css") {
			assets = true
			break
		}
	}
	if !assets {
		failures = append(failures, Failure{
			"site-chrome", "site/" + chrome.Dir,
			"holds no stylesheet. The assembly serves one set of page chrome " +
				"for the whole site, and with none of it in the tree every " +
				"page is unstyled HTML.",
		})
	}

	for _, pageRel := range tree.Pages {
		pageHTML, err := tree.Read(pageRel)
		if err != nil {
			return nil, err
		}
		var referenced []string
		for _, tag := range stylesheetTagRE.FindAllString(pageHTML, -1) {
			match := hrefRE.FindStringSubmatch(tag)
			if match != nil && chrome.IsReference(match[1]) {
				referenced = append(referenced, match[1])
			}
		}
		if len(referenced) == 0 {
			failures = append(failures, Failure{
				"site-chrome", "site/" + pageRel,
				"links no page-chrome stylesheet, so it is served as " +
					"unstyled HTML.",
			})
			continue
		}
		for _, href := range referenced {
			target, ok := resolution.ReferenceTarget(pageRel, href)
			if ok && strings.HasPrefix(target, chrome.Dir+"/") {
				continue
			}
			failures = append(failures, Failure{
				"site-chrome", "site/" + pageRel,
				fmt.Sprintf("links the stylesheet %s, which is not the "+
					"site-level asset under %s/. A page pointing at a copy "+
					"inside its own subtree is a page the next presentation "+
					"fix will not reach.", href, chrome.Dir),
			})
		}
	}
	return failures, nil
}

// CheckUnresolvedDirectives asserts that no page carries a directive the build
// did not resolve.
//
// Code and preformatted blocks are excluded: the documentation of the
// directive syntax quotes every marker there is, and quoting one is not
// leaving one behind.
func CheckUnresolvedDirectives(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, pageRel := range tree.Pages {
		pageHTML, err := tree.Read(pageRel)
		if err != nil {
			return nil, err
		}
		prose := codeBlockRE.ReplaceAllString(pageHTML, " ")
		for _, match := range stubMarkerRE.FindAllString(prose, -1) {
			failures = append(failures, Failure{
				"unresolved-directives", "site/" + pageRel,
				fmt.Sprintf("carries an unresolved directive: %s", match),
			})
		}
		for _, match := range rawMarkerRE.FindAllStringSubmatch(prose, -1) {
			failures = append(failures, Failure{
				"unresolved-directives", "site/" + pageRel,
				fmt.Sprintf("carries a raw directive marker %s outside a code "+
					"block, so the directive was never parsed.",
					util.PythonRepr(match[1])),
			})
		}
	}
	return failures, nil
}

// CheckRoutingArtifacts asserts that no per-project routing file survived the
// graft.
//
// The assembly serves one set of headers, one worker and one not-found page
// for the whole site. A project's own copies -- and the pre-compressed
// variants its build emits for its own hosting -- fight them wherever they
// sit, so the graft filters them out and this is that filter, asserted.
//
// 404.html is in that set for a sharper reason than a fight: a subtree copy is
// not served at all. The provider answers an unmatched address from the root
// of what it serves, so a project's own 404 is an unreachable page that still
// has to satisfy every assertion made about a page, and it failed the
// canonical one on every project that published.
func CheckRoutingArtifacts(tree *AssemblyTree) ([]Failure, error) {
	var failures []Failure
	for _, rel := range tree.sortedEmitted() {
		name := path.Base(rel)
		atRoot := !strings.Contains(rel, "/")
		if atRoot && contains(SharedRoutingFiles, name) {
			continue
		}
		if contains(RoutingArtifactNames, name) || hasAnySuffix(name, RoutingArtifactSuffixes) {
			failures = append(failures, Failure{
				"routing-artifacts", "site/" + rel,
				"is a per-project deploy artifact. The assembly serves one " +
					"set of routing files for the whole site.",
			})
		}
	}
	return failures, nil
}

// ExtractLinkRegistry maps each emitted page to the other projects' addresses
// it links to.
//
// This is the half ValidateCrossProjectLinks was written against and never
// had: the function knows what every project publishes, but nothing produced
// the registry of what the pages actually link to. A reference is resolved
// against the emitted tree, turned back into the address form the manifests
// speak, and kept only when it leaves the project the page belongs to -- a
// link inside one project is the LINK001 pass's business, not this one's.
//
// Site-level pages are not read at all. A post has no project segment, so its
// first path segment is "blog" -- not a project, and treating it as one makes
// every link a post's own chrome writes back into the project that published
// it look like a cross-project link. Those links are generated by that
// project's build from its own addresses, and they reach pages the manifests
// never list (the glossary, the API and CLI indexes are published without
// appearing there). Whether they resolve is the LINK001 pass's question, and
// it answers it for every one.
func ExtractLinkRegistry(tree *AssemblyTree) (map[string][]string, error) {
	registry := map[string][]string{}
	for _, pageRel := range tree.Pages {
		if address.IsSiteLevel(pageRel) {
			continue
		}
		sourceProject := ""
		if index := strings.Index(pageRel, "/"); index >= 0 {
			sourceProject = pageRel[:index]
		}
		pageHTML, err := tree.Read(pageRel)
		if err != nil {
			return nil, err
		}
		var targets []string
		for _, reference := range resolution.PageReferences(pageHTML) {
			ref := reference.Ref
			if strings.HasPrefix(ref, "/") {
				continue
			}
			target, ok := resolution.ReferenceTarget(pageRel, ref)
			if !ok || strings.HasPrefix(target, "..") {
				continue
			}
			targetProject := ""
			if index := strings.Index(target, "/"); index >= 0 {
				targetProject = target[:index]
			}
			if targetProject == "" || targetProject == sourceProject {
				continue
			}
			if !tree.Roster.Has(targetProject) {
				continue
			}
			form := shared.OutputPathTarget(target)
			if form != "" && !contains(targets, form) {
				targets = append(targets, form)
			}
		}
		if len(targets) > 0 {
			registry[pageRel] = targets
		}
	}
	return registry, nil
}

// CheckCrossProjectLinks asserts that every link into another project names a
// page that project publishes.
func CheckCrossProjectLinks(tree *AssemblyTree) ([]Failure, error) {
	registry, err := ExtractLinkRegistry(tree)
	if err != nil {
		return nil, err
	}
	var failures []Failure
	for _, message := range shared.ValidateCrossProjectLinks(tree.Manifests, registry) {
		failures = append(failures, Failure{"cross-project-links", "site", message})
	}
	return failures, nil
}

// listingPageRel is the site-relative address of the generated project
// listing, the page whose whole job is to name every project the site
// publishes.
const listingPageRel = "projects/index.html"

// rootPageRel is the site root: the home project's front page, and where a
// reader arriving at the site starts.
const rootPageRel = "index.html"

// CheckProjectReachability asserts that every roster project's index page is
// reachable from an arrival page by following clickable links.
//
// A project nothing links is published and unreachable: a reader arriving at
// the site never sees it, and a crawler that follows links never finds it.
// The walk starts at the two pages a reader arrives through, the site root
// and "/projects/", and follows every clickable link that resolves to an
// emitted page, so a project the front page curates away is still reached
// through the listing, and one the listing leaves out is still reached
// through the sibling block every assembled page carries. A project the walk
// never reaches is the finding.
//
// The home project is not asked for: the site root is its own front page, so
// it is reached by being the destination rather than by being linked.
//
// Links are read the way the resolution rule reads them: as clickable anchors
// resolved against the page that writes them, so a document-relative link and
// an index.html written out both resolve to the same target.
func CheckProjectReachability(tree *AssemblyTree) ([]Failure, error) {
	reached := map[string]bool{}
	queue := []string{}
	for _, rel := range []string{rootPageRel, listingPageRel} {
		if tree.Emitted[rel] && !reached[rel] {
			// An absent arrival page is CheckSharedArtifacts's finding; this
			// check walks from whichever exist.
			reached[rel] = true
			queue = append(queue, rel)
		}
	}
	if len(queue) == 0 {
		return nil, nil
	}
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		pageHTML, err := tree.Read(rel)
		if err != nil {
			return nil, err
		}
		for _, ref := range resolution.NavigationReferences(pageHTML) {
			target, addresses := resolution.ReferenceTarget(rel, ref)
			if !addresses || reached[target] || !tree.Emitted[target] ||
				!strings.HasSuffix(target, ".html") {
				continue
			}
			reached[target] = true
			queue = append(queue, target)
		}
	}

	var failures []Failure
	for _, slug := range tree.Roster.Slugs() {
		if slug == tree.Home {
			continue
		}
		indexRel := slug + "/index.html"
		if reached[indexRel] {
			continue
		}
		failures = append(failures, Failure{
			"project-reachability", "site/" + indexRel,
			fmt.Sprintf(
				"is not reached by following links from the site root or the "+
					"project listing, though %s is published there. A project "+
					"no path of links arrives at is one a reader arriving at "+
					"the site never sees and a crawler following links never "+
					"reaches.",
				util.PythonRepr(slug),
			),
		})
	}
	return failures, nil
}

// contains reports whether items holds want.
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// sortedKeys returns the keys of a presence set, sorted.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedMapKeys returns the keys of a document mapping, sorted.
func sortedMapKeys(documents map[string]map[string]any) []string {
	keys := make([]string, 0, len(documents))
	for key := range documents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
