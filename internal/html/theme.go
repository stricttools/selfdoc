package html

import "github.com/stricttools/selfdoc/internal/themes"

// GetCSS returns the composed stylesheet for the named theme.
//
// It is [themes.CSS], re-exported so a page renderer reads its stylesheet
// and its highlight sheet from one package.
func GetCSS(themeName string) (string, error) { return themes.CSS(themeName) }

// ThemeCSSRel returns where this page's stylesheet sits, relative to the
// output root.
//
// A framework theme's sheet is written in "css/" with "fonts/" beside it,
// because the framework addresses its faces at "../fonts/". Every other
// theme keeps "style.css" at the root. Pages that render before a theme is
// known -- and the tests that build one by hand -- pass nil and get the
// plain answer.
func ThemeCSSRel(themeMeta *themes.Metadata) string {
	if themeMeta == nil || themeMeta.CSSRel == "" {
		return themes.DefaultCSSRel
	}
	return themeMeta.CSSRel
}
