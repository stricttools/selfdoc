package html

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/stricttools/selfdoc/internal/util"
)

// PygmentsScope is the selector every highlight rule is written under. It
// matches the markup a rendered code block carries, and nothing outside a
// code block.
const PygmentsScope = ".tm-code code"

// PygmentsVarPrefix is the prefix every generated highlight custom
// property carries.
const PygmentsVarPrefix = "--sd-hl-"

// pygmentsNeutral says what a highlight property means when the style
// being resolved has no opinion about it.
//
// A custom property has to resolve to something on both sides of the
// light/dark split, and the neutral answer differs by property: an
// unstyled token inherits its colour, paints no background, and carries no
// weight, slant or decoration of its own.
var pygmentsNeutral = map[string]string{
	"color":            "inherit",
	"background":       "transparent",
	"background-color": "transparent",
	"border":           "none",
	"font-weight":      "normal",
	"font-style":       "normal",
	"text-decoration":  "none",
}

// chromaStyleNames maps a Pygments style name onto the chroma style that
// carries the same palette, for the names where the two registries differ.
//
// Only one of the names selfdoc's themes declare needs mapping: Pygments'
// "default" style is registered in chroma under the name "pygments". The
// other five -- "monokai", "friendly", "native", "xcode" and
// "github-dark" -- are spelled the same in both.
var chromaStyleNames = map[string]string{
	"default": "pygments",
}

// hlDecl is one CSS declaration of a highlight rule.
type hlDecl struct {
	prop  string
	value string
}

// hlRule is one highlight rule: a selector inside [PygmentsScope] and the
// declarations the resolved style makes for it.
type hlRule struct {
	selector string
	decls    []hlDecl
}

// structuralTokens are the chroma token types that describe a code block's
// scaffolding rather than a token inside it.
//
// Chroma injects its own layout CSS into these (a flex line, the line
// number gutter's padding, the line table's border spacing) and selfdoc
// renders line numbers as its own markup, so a rule for any of them would
// style something the page does not have. Pygments' equivalent rules -- the
// unscoped "pre" and the two "linenos" shapes -- were dropped for the same
// reason. LineHighlight is deliberately not in the set: it is the
// counterpart of Pygments' ".hll", which was kept.
var structuralTokens = map[chroma.TokenType]bool{
	chroma.Background:       true,
	chroma.PreWrapper:       true,
	chroma.Line:             true,
	chroma.LineNumbers:      true,
	chroma.LineNumbersTable: true,
	chroma.LineTable:        true,
	chroma.LineTableTD:      true,
	chroma.LineLink:         true,
	chroma.CodeLine:         true,
}

// chromaStyle resolves a Pygments style name to the chroma style that
// replaces it.
//
// An unknown name is an error naming it, rather than chroma's own silent
// fall back to a substitute style: a theme that asks for a palette this
// build cannot produce should say so, not paint a different one.
func chromaStyle(name string) (*chroma.Style, error) {
	lookup := name
	if mapped, ok := chromaStyleNames[name]; ok {
		lookup = mapped
	}
	if style, ok := styles.Registry[strings.ToLower(lookup)]; ok {
		return style, nil
	}
	return nil, fmt.Errorf("unknown highlight style %q", name)
}

// entryDecls renders one resolved style entry as ordered declarations.
//
// The properties and their order are chroma's own
// [chromahtml.StyleEntryToCSS], read off the entry's fields instead of its
// rendered string so nothing has to parse CSS back apart.
func entryDecls(e chroma.StyleEntry) []hlDecl {
	var out []hlDecl
	if e.Colour.IsSet() {
		out = append(out, hlDecl{"color", e.Colour.String()})
	}
	if e.Background.IsSet() {
		out = append(out, hlDecl{"background-color", e.Background.String()})
	}
	if e.Bold == chroma.Yes {
		out = append(out, hlDecl{"font-weight", "bold"})
	}
	if e.Italic == chroma.Yes {
		out = append(out, hlDecl{"font-style", "italic"})
	}
	if e.Underline == chroma.Yes {
		out = append(out, hlDecl{"text-decoration", "underline"})
	}
	return out
}

// pygmentsRules returns the highlight rules style makes inside scope.
//
// The scope itself carries the style's background entry, and every token
// class chroma knows carries what the style resolves for it with the
// background subtracted -- the same reduction chroma's own stylesheet
// writer performs, so a token painted in the background's own colour emits
// nothing. Token types are walked in their numeric order, which makes two
// runs over the same style produce byte-identical CSS.
func pygmentsRules(style *chroma.Style, scope string) []hlRule {
	var out []hlRule
	bg := style.Get(chroma.Background)
	if decls := entryDecls(bg); len(decls) > 0 {
		out = append(out, hlRule{selector: scope, decls: decls})
	}
	types := make([]int, 0, len(chroma.StandardTypes))
	for tt := range chroma.StandardTypes {
		types = append(types, int(tt))
	}
	sort.Ints(types)
	for _, ti := range types {
		tt := chroma.TokenType(ti)
		if structuralTokens[tt] {
			continue
		}
		class := chroma.StandardTypes[tt]
		if class == "" {
			continue
		}
		decls := entryDecls(style.Get(tt).Sub(bg))
		if len(decls) == 0 {
			continue
		}
		out = append(out, hlRule{selector: scope + " ." + class, decls: decls})
	}
	return out
}

// pygmentsVar returns the custom-property name that carries prop for
// selector.
//
// It is derived from the token classes the selector names, so the variable
// a reader meets in the rule says which token it paints:
// ".tm-code code .kd" plus "color" becomes "--sd-hl-kd-color".
func pygmentsVar(selector, prop, scope string) string {
	tail := util.PythonStrip(strings.TrimPrefix(selector, scope))
	if tail == "" {
		tail = "base"
	}
	slug := strings.Trim(slugPartRE.ReplaceAllString(strings.ToLower(tail), "-"), "-")
	propSlug := strings.Trim(slugPartRE.ReplaceAllString(strings.ToLower(prop), "-"), "-")
	return PygmentsVarPrefix + slug + "-" + propSlug
}

// GeneratePygmentsCSS generates the syntax-highlight CSS, tokenized across
// the light/dark split.
//
// Two highlight styles are resolved -- one per colour scheme -- and neither
// of them reaches a rule as a literal. Every declaration either style makes
// becomes a custom property defined three times over (the default scheme,
// an explicitly chosen dark one, and the system fallback for a reader with
// no choice recorded) and referenced once by a single set of rules. A
// token's colour is therefore a value in the token layer and the rules that
// paint it are scheme-agnostic, which is the shape the framework's
// conformance checker requires and, independent of that, the only spelling
// where the two schemes cannot drift apart rule by rule.
//
// The three token blocks are spelled ":root", `html[data-theme="dark"]` and
// "html:not([data-theme])" inside a prefers-color-scheme query -- the same
// CSS-only three-state resolution the tinymoon theme uses, so a reader who
// has expressed no preference and runs no JavaScript still gets the dark
// palette.
//
// lightStyle and darkStyle are Pygments style names, as a theme's
// companion JSON declares them; see [chromaStyleNames] for the one name
// whose chroma spelling differs. An unknown name is an error.
//
// A variable name is derived from a selector, so two selectors that slug
// the same would share one value and paint one of the two tokens wrong.
// Nothing in chroma's class vocabulary collides today; a style that
// introduced one would be a silent miscolouring, so it is an error.
func GeneratePygmentsCSS(lightStyle, darkStyle string) (string, error) {
	scope := PygmentsScope
	lightSt, err := chromaStyle(lightStyle)
	if err != nil {
		return "", err
	}
	darkSt, err := chromaStyle(darkStyle)
	if err != nil {
		return "", err
	}
	light := pygmentsRules(lightSt, scope)
	dark := pygmentsRules(darkSt, scope)

	type key struct{ selector, prop string }
	lightValues := map[key]string{}
	darkValues := map[key]string{}
	for _, rule := range light {
		for _, d := range rule.decls {
			lightValues[key{rule.selector, d.prop}] = d.value
		}
	}
	for _, rule := range dark {
		for _, d := range rule.decls {
			darkValues[key{rule.selector, d.prop}] = d.value
		}
	}

	// Selectors in the order the light style states them, then any the
	// dark style adds -- a stable order, so two builds of the same styles
	// produce byte-identical CSS.
	var order []string
	seen := map[string]bool{}
	for _, rule := range append(append([]hlRule{}, light...), dark...) {
		if !seen[rule.selector] {
			seen[rule.selector] = true
			order = append(order, rule.selector)
		}
	}

	var rules, lightTokens, darkTokens []string
	assigned := map[string]key{}
	for _, selector := range order {
		var props []string
		for _, source := range [][]hlRule{light, dark} {
			for _, rule := range source {
				if rule.selector != selector {
					continue
				}
				for _, d := range rule.decls {
					if !containsString(props, d.prop) {
						props = append(props, d.prop)
					}
				}
			}
		}
		var declarations []string
		for _, prop := range props {
			name := pygmentsVar(selector, prop, scope)
			owner, ok := assigned[name]
			if !ok {
				owner = key{selector, prop}
				assigned[name] = owner
			}
			if owner != (key{selector, prop}) {
				return "", fmt.Errorf(
					"highlight variable %q would be shared by %q/%s and %q/%s",
					name, owner.selector, owner.prop, selector, prop,
				)
			}
			neutral, ok := pygmentsNeutral[prop]
			if !ok {
				neutral = "initial"
			}
			lightTokens = append(lightTokens, "  "+name+": "+valueOr(lightValues, key{selector, prop}, neutral)+";")
			darkTokens = append(darkTokens, "  "+name+": "+valueOr(darkValues, key{selector, prop}, neutral)+";")
			declarations = append(declarations, prop+": var("+name+");")
		}
		rules = append(rules, selector+" { "+strings.Join(declarations, " ")+" }")
	}

	lightBlock := ":root {\n" + strings.Join(lightTokens, "\n") + "\n}"
	darkBlock := "html[data-theme=\"dark\"] {\n" + strings.Join(darkTokens, "\n") + "\n}"
	systemBlock := "@media (prefers-color-scheme: dark) {\n" +
		"html:not([data-theme]) {\n" +
		strings.Join(darkTokens, "\n") +
		"\n}\n}"
	return strings.Join([]string{
		lightBlock, darkBlock, systemBlock, strings.Join(rules, "\n"),
	}, "\n\n"), nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func valueOr[K comparable](m map[K]string, k K, fallback string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return fallback
}

// highlightFormatter emits the spans a highlighted code block carries: CSS
// classes rather than inline styles, and no surrounding <pre> or <code>,
// because this package emits its own code chrome around them. It is the
// equivalent of the Pygments formatter's nowrap mode.
var highlightFormatter = chromahtml.New(
	chromahtml.WithClasses(true),
	chromahtml.PreventSurroundingPre(true),
)

// highlightStyle is handed to the formatter because its interface requires
// one. In classes mode no value from it reaches the output -- the
// stylesheet [GeneratePygmentsCSS] writes is what paints the classes -- so
// which style this is cannot affect a rendered page.
var highlightStyle = styles.Registry["pygments"]
