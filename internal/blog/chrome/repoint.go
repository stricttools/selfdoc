package chrome

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/themes"
)

// hrefPattern matches every href in a page, so a chrome reference can be
// recognised among them.
//
// The build writes three of them per page -- a preload, the async stylesheet
// and the <noscript> fallback -- and all three name the same file, so all
// three are rewritten by the same pass.
var hrefPattern = regexp.MustCompile(`href="([^"]*)"`)

// WriteAssets writes one asset per theme named and returns the
// theme-to-site-path mapping.
//
// Any other entry under [Dir] is deleted: an asset whose content changed took
// a new name, and the old name is a file no page references and every deploy
// would otherwise carry forever. A framework theme's payload is a directory,
// and is pruned whole for the same reason.
//
// themeNames may repeat and may arrive in any order; the assets are written in
// sorted order over the distinct names.
func WriteAssets(siteDir string, themeNames []string, h *effects.Handle) (map[string]string, error) {
	distinct := make([]string, 0, len(themeNames))
	seen := map[string]bool{}
	for _, theme := range themeNames {
		if !seen[theme] {
			seen[theme] = true
			distinct = append(distinct, theme)
		}
	}
	sort.Strings(distinct)

	wanted := map[string]string{}
	for _, theme := range distinct {
		css, err := CSS(theme)
		if err != nil {
			return nil, err
		}
		rel, err := AssetRel(theme, css)
		if err != nil {
			return nil, err
		}
		wanted[theme] = rel
		path := filepath.Join(siteDir, filepath.Join(strings.Split(rel, "/")...))
		if err := h.MkdirAll(filepath.Dir(path)); err != nil {
			return nil, err
		}
		if err := h.AtomicWrite(path, []byte(css), effects.ModeDefault); err != nil {
			return nil, err
		}
		// The framework's fonts and modules travel with its stylesheet, in
		// the layout the sheet addresses them by.
		payloadRoot := filepath.Dir(filepath.Dir(path))
		assets, err := themes.Assets(theme)
		if err != nil {
			return nil, err
		}
		for _, asset := range assets {
			dst := filepath.Join(payloadRoot, filepath.Join(strings.Split(asset.Dest, "/")...))
			if err := h.MkdirAll(filepath.Dir(dst)); err != nil {
				return nil, err
			}
			payload, err := asset.Bytes()
			if err != nil {
				return nil, err
			}
			if err := h.Write(dst, payload, effects.ModeDefault); err != nil {
				return nil, err
			}
		}
	}

	keep := map[string]bool{}
	for _, rel := range wanted {
		keep[entryName(rel)] = true
	}
	chromeDir := filepath.Join(siteDir, Dir)
	entries, err := os.ReadDir(chromeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return wanted, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if keep[name] {
			continue
		}
		path := filepath.Join(chromeDir, name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			if err := h.RmTree(path); err != nil {
				return nil, err
			}
		} else {
			if err := h.Remove(path); err != nil {
				return nil, err
			}
		}
	}
	return wanted, nil
}

// SiteRootPrefix is the hop from a site-relative page back to the site root.
//
// "" at the root, "../" one level in. The same fact the shared pages already
// carry as their search prefix: references are relative because the assembled
// tree has to resolve under any mount point, so a site-level asset is
// addressed by hopping out rather than by a leading slash.
func SiteRootPrefix(pageRel string) string {
	return strings.Repeat("../", strings.Count(pageRel, "/"))
}

// Href is the reference a page at pageRel writes to reach the chrome asset at
// assetRel.
func Href(pageRel, assetRel string) string {
	return SiteRootPrefix(pageRel) + assetRel
}

// PageTheme is which theme's asset a page at pageRel references.
//
// A page inside a project's subtree gets that project's theme. Everything else
// -- the home project's pages at the site root, the site-level blog, and the
// assembly's own shared pages -- gets the home project's: those addresses
// belong to the site rather than to one project, and the site reads as the
// front page reads.
func PageTheme(pageRel string, bySlug map[string]string, homeTheme string) string {
	head := ""
	if index := strings.Index(pageRel, "/"); index >= 0 {
		head = pageRel[:index]
	}
	if head != "" {
		if theme, known := bySlug[head]; known {
			return theme
		}
	}
	return homeTheme
}

// IsReference reports whether ref is a page's reference to its page-chrome
// stylesheet.
//
// Two spellings are recognised, because both are on a live site at once: a
// project build's own "style.css", relative to the page, and an earlier
// deploy's site-level asset under [Dir]. A custom stylesheet is neither and is
// left where it is -- "custom.css" is the project's content, not the chrome
// the assembly owns.
func IsReference(ref string) bool {
	if ref == "" {
		return false
	}
	for _, prefix := range []string{"http://", "https://", "//", "/", "#"} {
		if strings.HasPrefix(ref, prefix) {
			return false
		}
	}
	name := ref
	if index := strings.LastIndex(ref, "/"); index >= 0 {
		name = ref[index+1:]
	}
	if name == "style.css" {
		return true
	}
	return strings.Contains(ref, Dir+"/") && strings.HasSuffix(name, ".css")
}

// RepointPage returns pageHTML with every chrome reference aimed at assetRel.
func RepointPage(pageHTML, pageRel, assetRel string) string {
	href := Href(pageRel, assetRel)
	return hrefPattern.ReplaceAllStringFunc(pageHTML, func(match string) string {
		ref := hrefPattern.FindStringSubmatch(match)[1]
		if !IsReference(ref) {
			return match
		}
		return `href="` + href + `"`
	})
}

// RepointPages re-points every page named at the site-level chrome asset and
// returns the site-relative paths that actually changed, so a caller can say
// what a deploy touched.
//
// pages are site-relative HTML paths and assets is the theme-to-path mapping
// [WriteAssets] returned.
//
// A page whose theme has no asset is a page whose project declares a theme the
// assembly did not emit, and that is a hard error rather than a page left
// pointing at a file the graft no longer serves.
func RepointPages(siteDir string, pages []string, bySlug map[string]string,
	assets map[string]string, homeTheme string, h *effects.Handle) ([]string, error) {
	ordered := make([]string, len(pages))
	copy(ordered, pages)
	sort.Strings(ordered)

	changed := make([]string, 0)
	for _, pageRel := range ordered {
		theme := PageTheme(pageRel, bySlug, homeTheme)
		assetRel, emitted := assets[theme]
		if !emitted {
			return nil, fmt.Errorf(
				"site/%s needs the site-level chrome asset for the theme "+
					"%q, which this deploy did not emit. The asset set is "+
					"built from the themes the manifests declare, so a page "+
					"asking for another one means the manifest it belongs to "+
					"changed after the set was written.",
				pageRel, theme)
		}
		path := filepath.Join(siteDir, filepath.Join(strings.Split(pageRel, "/")...))
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pageHTML := string(raw)
		repointed := RepointPage(pageHTML, pageRel, assetRel)
		if repointed == pageHTML {
			continue
		}
		if err := h.AtomicWrite(path, []byte(repointed), effects.ModeDefault); err != nil {
			return nil, err
		}
		changed = append(changed, pageRel)
	}
	return changed, nil
}

// EmittedPages is every ".html" file under siteDir, site-relative and sorted.
//
// A siteDir that does not exist has emitted nothing, and is reported as the
// empty list rather than as an error: that is what the walk this replaces did,
// and the deploy calls it before it knows whether any project has grafted yet.
func EmittedPages(siteDir string) ([]string, error) {
	if _, err := os.Stat(siteDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	found := make([]string, 0)
	err := filepath.WalkDir(siteDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			return nil
		}
		rel, err := filepath.Rel(siteDir, path)
		if err != nil {
			return err
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}
