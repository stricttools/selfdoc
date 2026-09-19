package preview

import (
	"os"
	"path/filepath"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/util"
)

// ReadSlug returns the assembly slug the project at sourceDir declares.
func ReadSlug(sourceDir string) (string, error) {
	cfg, err := config.Load(sourceDir)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", errorf(
			"%s carries no selfdoc.json, so it is not a project the assembly "+
				"can serve.", sourceDir)
	}
	topology, _ := cfg["topology"].(map[string]any)
	slug := util.PythonStrip(util.PythonStrOrEmpty(topology["slug"]))
	if slug == "" {
		return "", errorf(
			"%s/selfdoc.json declares no topology.slug, so there is no address "+
				"to publish it at. The slug is the project's path segment on "+
				"the assembled site.", sourceDir)
	}
	return slug, nil
}

// ExpectedStylesheet is the stylesheet "selfdoc build" writes for theme, byte
// for byte.
//
// Recomputed the same way the build computes it -- the theme's CSS, the
// highlight rules its metadata names, minified together -- so a comparison
// against a build's "style.css" is an equality rather than a resemblance. It is
// deliberately sensitive to the theme file changing after a build: a page
// rendered against an older version of the theme is exactly the thing the
// caller wants to be told about.
func ExpectedStylesheet(theme string) (string, error) {
	meta, err := themes.Meta(theme)
	if err != nil {
		return "", err
	}
	css, err := html.GetCSS(theme)
	if err != nil {
		return "", err
	}
	highlight, err := html.GeneratePygmentsCSS(meta.PygmentsLight, meta.PygmentsDark)
	if err != nil {
		return "", err
	}
	if highlight != "" {
		css = css + "\n\n/* Pygments syntax highlighting */\n" + highlight
	}
	return build.MinifyCSS(css), nil
}

// BuiltUnderTheme reports whether sourceDir's existing build output was made
// with theme.
//
// False for a checkout with no build output at all: nothing to graft is not the
// same claim as grafting something styled correctly, and the graft will fail on
// its own terms a moment later anyway.
func BuiltUnderTheme(sourceDir, theme string) (bool, error) {
	cssRel, err := themes.CSSRel(theme)
	if err != nil {
		return false, err
	}
	path := filepath.Join(layout.Path(sourceDir, layout.OutputRel), filepath.FromSlash(cssRel))
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	expected, err := ExpectedStylesheet(theme)
	if err != nil {
		return false, err
	}
	return string(content) == expected, nil
}
