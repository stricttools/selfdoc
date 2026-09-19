package chrome

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/util"
)

// Dir is the site-level directory the chrome assets are served from.
//
// It is one of the assembly's own directories, so no project may claim it as a
// slug -- see the assembly's reserved site directories.
const Dir = "_chrome"

// DefaultTheme is the theme a project gets when it names none.
//
// This is not a choice made here: it is the same default "selfdoc build"
// applies when a project's selfdoc.json carries no "theme" key, and the two
// have to agree or a page would be styled by a stylesheet its build never
// rendered against. Read from the manifest package rather than restated, so
// there is one value and not two that have to be kept equal by hand.
const DefaultTheme = manifest.DefaultTheme

// hashLength is how much of the digest goes in the file name. Twelve hex
// characters is 48 bits, which no site of this size collides in, and it keeps
// the name readable in a diff.
const hashLength = 12

// sharedPageCSS is the rules the assembly's own shared pages need and the
// theme does not carry.
//
// The project listing, the blog index and the not-found page are the
// assembly's pages, not any project's, so their classes are not in the theme's
// class surface; they are styled here, appended to the theme, in the same way
// the unified builder appends its project-grid rules.
//
// Everything else these pages use -- ".content", ".site-footer",
// ".version-badge" -- is the theme's, and is reused rather than restated.
const sharedPageCSS = `/* --- assembly shared pages --- */
.shared-page {
  max-width: 52rem;
  margin: 0 auto;
  padding: 2.5rem 1.5rem 3rem;
}
.shared-page > h1 {
  margin-bottom: 1.5rem;
}
.project-list, .blog-index {
  display: block;
}
.project-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1.25rem 1.5rem;
  margin: 0 0 1rem;
}
/* The listing renders a card heading as h3 inside a category, the
   name-ordered fallback as h2 with no category around it. */
.project-card h2, .project-card h3 {
  margin: 0 0 0.35rem;
  font-size: 1.15rem;
}
.project-card p {
  margin: 0.35rem 0 0;
  color: var(--text-secondary);
}
.project-category {
  margin: 0 0 2rem;
}
.project-category > h2 {
  font-size: 1rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-secondary);
  margin: 0 0 0.75rem;
}
.external-badge, .project-repo {
  font-size: 0.85em;
  color: var(--text-secondary);
}
.blog-entry {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.75rem;
  padding: 0.6rem 0;
  border-bottom: 1px solid var(--border);
}
.blog-entry time {
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
  min-width: 6.5rem;
}
.blog-entry .project-name {
  color: var(--text-secondary);
  font-size: 0.85em;
}
.not-found ul {
  margin: 0.75rem 0 0 1.25rem;
}
`

// siblingCSS is the presentation of the sibling block the assembly writes at
// the end of every page's main column.
//
// The block is the assembly's, not any project's: a standalone build never
// emits it, so the theme carries no rules for its class. It is styled here for
// the same reason the shared pages are, and out of the same tokens -- the
// theme's --border and --text-secondary flip with the light and dark palettes,
// so neither of these rules names a colour of its own.
//
// It sits beside the page footer in the document, and is separated from the
// article above it the way the footer is: one top border in the theme's border
// colour. The list carries no markers and lays out as a responsive grid, so a
// wide viewport reads several projects per row and a narrow one reads a column.
// The heading stays the heading element the block emits; only its size and
// colour are set here.
const siblingCSS = `/* --- the assembly's sibling block --- */
.sibling-projects {
  margin: 3rem 0 0;
  padding: 1.5rem 0 0;
  border-top: 1px solid var(--border);
}
.sibling-projects > h2 {
  margin: 0 0 0.9rem;
  font-size: 0.8125rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-secondary);
}
.sibling-projects ul {
  list-style: none;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
  gap: 0.75rem 1.5rem;
  margin: 0;
  padding: 0;
}
.sibling-projects li {
  margin: 0;
}
.sibling-projects li a {
  font-weight: 600;
}
.sibling-projects li span {
  display: block;
  margin-top: 0.15rem;
  font-size: 0.85em;
  line-height: 1.45;
  color: var(--text-secondary);
}
@media print {
  .sibling-projects {
    display: none;
  }
}
`

// ManifestTheme is the theme a loaded manifest declares, or the build's own
// default.
//
// A manifest that names no theme is every manifest published so far: the key
// is read here so a project that starts declaring one is honoured without
// another change on this side.
func ManifestTheme(loaded map[string]any) string {
	declared := util.PythonStrOrEmpty(loaded["theme"])
	if declared == "" {
		return DefaultTheme
	}
	return declared
}

// Themes returns the slug-to-theme mapping for a roster's manifests together
// with the home project's theme.
//
// Theme choice is per project, so the site-level asset is theme-keyed rather
// than singular: the assembly emits one asset per distinct theme its projects
// declare, and a page references the one its own project uses.
//
// override names one theme every project is treated as declaring. It exists
// for the preview's --theme, which builds every checkout under one theme so
// the whole site can be judged under it at once; the pages were rendered
// against that theme, so the asset they reference has to be that theme too,
// whatever each project's manifest says. Empty -- always, on a deploy -- means
// every project keeps its own.
func Themes(manifests []map[string]any, homeSlug string, override string) (map[string]string, string) {
	bySlug := map[string]string{}
	for _, loaded := range manifests {
		slug := util.PythonStrOrEmpty(loaded["slug"])
		if slug == "" {
			continue
		}
		if override != "" {
			bySlug[slug] = override
		} else {
			bySlug[slug] = ManifestTheme(loaded)
		}
	}
	if override != "" {
		return bySlug, override
	}
	if homeTheme, declared := bySlug[homeSlug]; declared {
		return bySlug, homeTheme
	}
	return bySlug, DefaultTheme
}

// CSS is the full stylesheet the site-level asset for a theme carries.
//
// The theme's own CSS plus the highlight rules its metadata names -- the same
// two pieces, in the same order, that a project's build writes into its own
// "style.css" -- plus the assembly's own rules, the shared pages' and the
// sibling block's, minified.
func CSS(theme string) (string, error) {
	meta, err := themes.Meta(theme)
	if err != nil {
		return "", err
	}
	css, err := html.GetCSS(theme)
	if err != nil {
		return "", err
	}
	highlightCSS, err := html.GeneratePygmentsCSS(meta.PygmentsLight, meta.PygmentsDark)
	if err != nil {
		return "", err
	}
	if highlightCSS != "" {
		css = css + "\n\n/* Pygments syntax highlighting */\n" + highlightCSS
	}
	css = css + "\n\n" + sharedPageCSS + "\n" + siblingCSS
	return build.MinifyCSS(css), nil
}

// AssetRel is the site-relative path the asset for a theme with the given
// composed stylesheet takes.
//
// A plain theme is one file. A framework theme is a directory --
// "_chrome/<theme>-<digest>/css/style.css" with "fonts/" and "js/" beside the
// "css/", because the framework's @font-face rules and the page's module
// imports are addressed relative to the sheet rather than to the site.
//
// The digest covers the composed stylesheet and the module payload. The
// stylesheet alone would be enough for a framework whose CSS changes whenever
// its JavaScript does, and that is not a property anything guarantees: a
// framework release that fixed only a module would leave the directory name
// unchanged and every cache would go on serving the old modules from it. What
// names the payload has to be everything inside it.
func AssetRel(theme string, css string) (string, error) {
	assets, err := themes.Assets(theme)
	if err != nil {
		return "", err
	}
	material := [][]byte{[]byte(css)}
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Dest, themes.ModulesDir+"/") {
			continue
		}
		payload, err := asset.Bytes()
		if err != nil {
			return "", err
		}
		material = append(material, []byte(asset.Dest), payload)
	}
	sum := sha256.Sum256(joinNUL(material))
	digest := hex.EncodeToString(sum[:])[:hashLength]
	if len(assets) == 0 {
		return Dir + "/" + theme + "-" + digest + ".css", nil
	}
	cssRel, err := themes.CSSRel(theme)
	if err != nil {
		return "", err
	}
	return Dir + "/" + theme + "-" + digest + "/" + cssRel, nil
}

// joinNUL concatenates the digest material with a NUL byte between the pieces,
// which is what keeps a boundary between a module's name and its bytes.
func joinNUL(pieces [][]byte) []byte {
	total := 0
	for _, piece := range pieces {
		total += len(piece) + 1
	}
	joined := make([]byte, 0, total)
	for i, piece := range pieces {
		if i > 0 {
			joined = append(joined, 0)
		}
		joined = append(joined, piece...)
	}
	return joined
}

// entryName is the name directly under [Dir] that a site-relative asset path
// belongs to.
//
// A plain theme's asset is that name itself; a framework theme's is the
// directory every one of its files sits under. The prune deletes by entry, so
// one name covers a whole framework payload.
func entryName(rel string) string {
	segments := strings.Split(rel, "/")
	if len(segments) < 2 {
		return ""
	}
	return segments[1]
}
