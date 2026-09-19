package verify

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// NotFoundPage is the page a hosting provider serves for an address that
// matches nothing. One per served root, which on this site means one, at the
// site root.
const NotFoundPage = "404.html"

// SharedRoutingFiles is what the assembly itself is allowed to serve at the
// site root. Every other routing artifact belongs to a single project's own
// standalone hosting and fights the site-wide one wherever it lands.
//
// A redirect worker is not among them: the assembly emits none, a host that
// should answer somewhere else is a rule on the DNS zone, and a "_worker.js"
// left at the root by a deploy that predates that is refused here and deleted
// by the next integration's shared-files pass.
var SharedRoutingFiles = []string{"_headers", NotFoundPage}

// RoutingArtifactNames is every routing file a per-project build emits for its
// own standalone hosting.
var RoutingArtifactNames = []string{
	"_headers", "_redirects", "_worker.js", NotFoundPage,
}

// RoutingArtifactSuffixes are the pre-compressed copies a standalone build
// writes beside every asset.
var RoutingArtifactSuffixes = []string{".gz", ".br"}

var (
	titleRE        = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	canonicalTagRE = regexp.MustCompile(
		`(?i)<link\b[^>]*\brel` + util.PythonSpaceClass + `*=` +
			util.PythonSpaceClass + `*["']?canonical["']?[^>]*>`)
	hrefRE = regexp.MustCompile(
		`(?i)\bhref` + util.PythonSpaceClass + `*=` +
			util.PythonSpaceClass + `*["']([^"']*)["']`)
	// stylesheetTagRE matches every rel=stylesheet element, so the page-chrome
	// assertion can read which stylesheets a page actually loads.
	stylesheetTagRE = regexp.MustCompile(
		`(?i)<link\b[^>]*\brel` + util.PythonSpaceClass + `*=` +
			util.PythonSpaceClass + `*["']?stylesheet["']?[^>]*>`)
	versionAttrRE = regexp.MustCompile(`data-default-version="([^"]*)"`)
	codeBlockRE   = regexp.MustCompile(`(?is)<pre\b.*?</pre>|<code\b.*?</code>`)
	// locRE matches a sitemap entry's address.
	locRE = regexp.MustCompile(`<loc>([^<]*)</loc>`)
	// stubMarkerRE matches the visible marker the build leaves where a
	// directive did not resolve.
	stubMarkerRE = regexp.MustCompile(`\[selfdoc:[^\]]*not yet resolved\]`)
	// rawMarkerRE matches the raw markers a template carries before one is
	// parsed at all.
	rawMarkerRE = regexp.MustCompile(
		`(?m)^` + util.PythonSpaceClass + `*(:-:|:&lt;:|:@:|:=:|:&gt;:)` +
			util.PythonSpaceClass + `*` + util.PythonNonSpaceClass)
	// tagRE is every HTML tag, replaced by a space when a body is flattened
	// into the text a reader sees.
	tagRE = regexp.MustCompile(`<[^>]*>`)
)

// AssemblyTree is the assembled tree, read once and handed to every check.
type AssemblyTree struct {
	// AssemblyDir is the assembly repository checkout.
	AssemblyDir string
	// SiteDir is the assembled site inside it.
	SiteDir string
	// ManifestsDir holds one manifest per project plus their sidecars.
	ManifestsDir string
	// CanonicalBase is the site's canonical base URL, without a trailing
	// slash.
	CanonicalBase string
	// Roster is the declared membership.
	Roster *site.Roster
	// Manifests is every project manifest with its post overlay applied.
	Manifests []map[string]any
	// ManifestFiles maps a slug to its base manifest, as written.
	ManifestFiles map[string]map[string]any
	// OverlayFiles maps a slug to its post overlay, as written.
	OverlayFiles map[string]map[string]any
	// Emitted is every file under SiteDir, site-relative.
	Emitted map[string]bool
	// Pages is every emitted ".html" file, sorted.
	Pages []string
	// Home is the slug of the declared project served at the site root.
	Home string
}

// Read returns the text of an emitted file, addressed site-relative.
func (t *AssemblyTree) Read(rel string) (string, error) {
	content, err := os.ReadFile(filepath.Join(
		t.SiteDir, filepath.Join(strings.Split(rel, "/")...),
	))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// emittedPaths returns every file under siteDir as a "/"-joined site-relative
// path. A directory that does not exist has emitted nothing.
func emittedPaths(siteDir string) (map[string]bool, error) {
	found := map[string]bool{}
	info, err := os.Stat(siteDir)
	if err != nil || !info.IsDir() {
		return found, nil
	}
	walkErr := filepath.WalkDir(siteDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(siteDir, path)
		if relErr != nil {
			return relErr
		}
		found[filepath.ToSlash(rel)] = true
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return found, nil
}

// ReadTree reads everything a verification needs out of assemblyDir.
func ReadTree(assemblyDir, canonicalBase string) (*AssemblyTree, error) {
	siteDir := filepath.Join(assemblyDir, "site")
	manifestsDir := filepath.Join(assemblyDir, "manifests")

	manifestFiles := map[string]map[string]any{}
	overlayFiles := map[string]map[string]any{}
	if info, err := os.Stat(manifestsDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(manifestsDir)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			if hasAnySuffix(name, site.ManifestSidecarSuffixes) {
				continue
			}
			path := filepath.Join(manifestsDir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			decoded, err := config.DecodeDocument(content)
			if err != nil {
				return nil, errorf("%s is not valid JSON: %v", path, err)
			}
			data, ok := decoded.(map[string]any)
			if !ok {
				return nil, errorf("%s must contain a JSON object", path)
			}
			stem := strings.TrimSuffix(name, ".json")
			if strings.HasSuffix(stem, "-posts") {
				overlayFiles[strings.TrimSuffix(stem, "-posts")] = data
			} else {
				manifestFiles[stem] = data
			}
		}
	}

	emitted, err := emittedPaths(siteDir)
	if err != nil {
		return nil, err
	}
	roster, err := site.LoadRoster(assemblyDir)
	if err != nil {
		return nil, err
	}
	manifests, err := site.LoadAssemblyManifests(manifestsDir)
	if err != nil {
		return nil, err
	}

	var pages []string
	for rel := range emitted {
		if strings.HasSuffix(rel, ".html") {
			pages = append(pages, rel)
		}
	}
	sort.Strings(pages)

	return &AssemblyTree{
		AssemblyDir:   assemblyDir,
		SiteDir:       siteDir,
		ManifestsDir:  manifestsDir,
		CanonicalBase: strings.TrimRight(canonicalBase, "/"),
		Roster:        roster,
		Manifests:     manifests,
		ManifestFiles: manifestFiles,
		OverlayFiles:  overlayFiles,
		Emitted:       emitted,
		Pages:         pages,
		Home:          roster.Home,
	}, nil
}

// sortedEmitted returns every emitted path, sorted, for the checks that walk
// the whole tree in a reproducible order.
func (t *AssemblyTree) sortedEmitted() []string {
	rels := make([]string, 0, len(t.Emitted))
	for rel := range t.Emitted {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels
}

// hasAnySuffix reports whether name ends with one of the suffixes.
func hasAnySuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// manifestOwner returns the declared slug a manifest file's stem belongs to,
// reporting false when no declared project owns it.
//
// The assembly's own model keeps this rule unexported, so it is restated here
// rather than reached into: a stem is either a declared slug outright, or a
// declared slug plus a "-<kind>" sidecar suffix.
func manifestOwner(stem string, declared map[string]bool) (string, bool) {
	if declared[stem] {
		return stem, true
	}
	if index := strings.LastIndex(stem, "-"); index > 0 {
		head := stem[:index]
		if declared[head] {
			return head, true
		}
	}
	return "", false
}
