package html

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/themes"
)

// Syntax highlighting is a token layer, not two piles of literals.
//
// A highlighter hands out a stylesheet full of raw colour literals, once
// per colour scheme. Pasting both into a page produced hundreds of raw
// colour literals in a built site's stylesheet and, worse, two independent
// rule sets that could drift apart token by token -- a class styled in the
// light scheme and forgotten in the dark one renders as whatever it
// inherits, and nothing says so.
//
// GeneratePygmentsCSS resolves both styles into one set of custom
// properties and one set of rules that reference them. What is asserted
// here is that property: no rule carries a literal, every variable a rule
// names is defined on both sides of the split, and the three token blocks
// are spelled the way a scheme-switching page needs them.

// stylePairs is every light/dark pair the shipped themes declare, plus the
// defaults, under the Pygments names a theme's companion JSON uses.
var stylePairs = [][2]string{
	{"default", "monokai"},
	{"xcode", "github-dark"},
	{"friendly", "native"},
}

var (
	cssRuleRE   = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	hexRE       = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	colorFnRE   = regexp.MustCompile(`\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb|color)\s*\(`)
	cssVarUseRE = regexp.MustCompile(`var\((--[\w-]+)\)`)
)

// cssBlocks returns the body of every top-level and nested block in css,
// keyed by its selector with runs of whitespace collapsed.
func cssBlocks(css string) map[string]string {
	out := map[string]string{}
	for _, m := range cssRuleRE.FindAllStringSubmatch(css, -1) {
		out[strings.Join(strings.Fields(m[1]), " ")] = m[2]
	}
	return out
}

// definedVars returns the custom properties a block body defines.
func definedVars(body string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(body, ";") {
		prop, sep, value := partition(part, ":")
		if sep != "" && strings.HasPrefix(strings.TrimSpace(prop), "--") {
			out[strings.TrimSpace(prop)] = strings.TrimSpace(value)
		}
	}
	return out
}

func isTokenBlock(selector string) bool {
	return strings.HasPrefix(selector, ":root") || strings.HasPrefix(selector, "html")
}

func generate(t *testing.T, light, dark string) string {
	t.Helper()
	css, err := GeneratePygmentsCSS(light, dark)
	if err != nil {
		t.Fatalf("GeneratePygmentsCSS(%q, %q): %v", light, dark, err)
	}
	return css
}

func TestNoHighlightRuleCarriesAColourLiteral(t *testing.T) {
	for _, pair := range stylePairs {
		css := generate(t, pair[0], pair[1])
		for selector, body := range cssBlocks(css) {
			if isTokenBlock(selector) {
				continue
			}
			for _, part := range strings.Split(body, ";") {
				prop, sep, value := partition(part, ":")
				if sep == "" {
					continue
				}
				if hexRE.MatchString(value) || colorFnRE.MatchString(value) {
					t.Errorf("%v: %s { %s: %s }", pair, selector,
						strings.TrimSpace(prop), strings.TrimSpace(value))
				}
			}
		}
	}
}

func TestEveryHighlightVariableIsDefinedOnBothSidesOfTheSplit(t *testing.T) {
	for _, pair := range stylePairs {
		blocks := cssBlocks(generate(t, pair[0], pair[1]))
		lightDefs := definedVars(blocks[":root"])
		darkDefs := definedVars(blocks[`html[data-theme="dark"]`])
		used := map[string]bool{}
		for selector, body := range blocks {
			if isTokenBlock(selector) {
				continue
			}
			for _, m := range cssVarUseRE.FindAllStringSubmatch(body, -1) {
				used[m[1]] = true
			}
		}
		if len(used) == 0 {
			t.Fatalf("%v: no rule references a variable at all", pair)
		}
		for name := range used {
			if _, ok := lightDefs[name]; !ok {
				t.Errorf("%v: %s is used but not defined for the light scheme", pair, name)
			}
			if _, ok := darkDefs[name]; !ok {
				t.Errorf("%v: %s is used but not defined for the dark scheme", pair, name)
			}
		}
		if len(lightDefs) != len(darkDefs) {
			t.Errorf("%v: %d light definitions against %d dark ones",
				pair, len(lightDefs), len(darkDefs))
		}
		for name := range lightDefs {
			if _, ok := darkDefs[name]; !ok {
				t.Errorf("%v: %s is defined only for the light scheme", pair, name)
			}
		}
	}
}

func TestTheThreeTokenBlocksAreSpelledForASchemeSwitch(t *testing.T) {
	for _, pair := range stylePairs {
		css := generate(t, pair[0], pair[1])
		blocks := cssBlocks(css)
		for _, selector := range []string{":root", `html[data-theme="dark"]`, "html:not([data-theme])"} {
			if _, ok := blocks[selector]; !ok {
				t.Errorf("%v: no %s block", pair, selector)
			}
		}
		mustContain(t, css, "@media (prefers-color-scheme: dark)")
	}
}

func TestEveryHighlightRuleStaysInsideTheCodeBlockScope(t *testing.T) {
	// The equivalent of Pygments' unscoped "pre" and "linenos" rules never
	// reaches a page.
	for _, pair := range stylePairs {
		for selector := range cssBlocks(generate(t, pair[0], pair[1])) {
			if isTokenBlock(selector) || strings.HasPrefix(selector, "@media") {
				continue
			}
			if selector != PygmentsScope && !strings.HasPrefix(selector, PygmentsScope+" ") {
				t.Errorf("%v: rule outside the scope: %s", pair, selector)
			}
		}
	}
}

func TestTheTwoSchemesReallyDiffer(t *testing.T) {
	// A tokenization that collapsed both styles into one would pass every
	// assertion above.
	for _, pair := range stylePairs {
		blocks := cssBlocks(generate(t, pair[0], pair[1]))
		light := definedVars(blocks[":root"])
		dark := definedVars(blocks[`html[data-theme="dark"]`])
		same := true
		for name, value := range light {
			if dark[name] != value {
				same = false
				break
			}
		}
		if same {
			t.Errorf("%v: the two schemes define identical values", pair)
		}
	}
}

func TestEveryHighlightVariableCarriesThePrefix(t *testing.T) {
	for _, pair := range stylePairs {
		for _, m := range cssVarUseRE.FindAllStringSubmatch(generate(t, pair[0], pair[1]), -1) {
			if !strings.HasPrefix(m[1], PygmentsVarPrefix) {
				t.Errorf("%v: %s carries no %s prefix", pair, m[1], PygmentsVarPrefix)
			}
		}
	}
}

func TestTheHighlightSheetIsReproducible(t *testing.T) {
	// Two runs over the same styles must produce byte-identical CSS, or a
	// rebuild churns the stylesheet for no reason.
	for _, pair := range stylePairs {
		if generate(t, pair[0], pair[1]) != generate(t, pair[0], pair[1]) {
			t.Errorf("%v: two runs disagree", pair)
		}
	}
}

func TestTheDarkBlockIsFlatNotNested(t *testing.T) {
	css := generate(t, "default", "monokai")
	i := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if i < 0 {
		t.Fatal("no system-fallback block")
	}
	mustNotContain(t, css[i:], ":root:not([data-theme='light']) {")
}

// Every style name a shipped theme declares must resolve, or that theme's
// pages carry no highlighting at all.
func TestEveryShippedThemesStyleNamesResolve(t *testing.T) {
	for _, name := range themes.List() {
		meta, err := themes.Meta(name)
		if err != nil {
			t.Fatalf("themes.Meta(%q): %v", name, err)
		}
		if _, err := GeneratePygmentsCSS(meta.PygmentsLight, meta.PygmentsDark); err != nil {
			t.Errorf("theme %q: %v", name, err)
		}
	}
}

func TestAnUnknownStyleNameIsAnError(t *testing.T) {
	if _, err := GeneratePygmentsCSS("no-such-style", "monokai"); err == nil {
		t.Error("an unknown light style was accepted")
	}
	if _, err := GeneratePygmentsCSS("default", "no-such-style"); err == nil {
		t.Error("an unknown dark style was accepted")
	}
}

// A highlighted code block carries chroma's short token classes, which is
// the one accepted divergence from the Pygments output this replaces.
func TestAHighlightedCodeBlockCarriesTokenClasses(t *testing.T) {
	html := MdToHTML("```python\ndef f():\n    return 1\n```\n", nil, nil)
	mustContain(t, html,
		`<code class="language-python">`,
		`<span class="k">`,  // keyword: "def" and "return"
		`<span class="nf">`, // function name: "f"
	)
	// And the classes the sheet paints are the ones the markup uses.
	css := generate(t, "default", "monokai")
	for _, class := range []string{"k", "nf"} {
		if !strings.Contains(css, PygmentsScope+" ."+class+" {") {
			t.Errorf("the stylesheet paints no .%s rule", class)
		}
	}
}

func TestAnUnknownFenceLanguageFallsBackToEscapedText(t *testing.T) {
	html := MdToHTML("```notalanguage\n<b>x</b> & y\n```\n", nil, nil)
	mustContain(t, html, "&lt;b&gt;x&lt;/b&gt; &amp; y")
	// Nothing inside the <code> element is marked up: no lexer answered,
	// so there was nothing to highlight.
	_, _, inCode := partition(html, `<code class="language-notalanguage">`)
	code, _, _ := partition(inCode, "</code>")
	mustNotContain(t, code, "<span")
}

func TestTheGeneratedVariableNamesAreDerivedFromTheirSelectors(t *testing.T) {
	if got := pygmentsVar(PygmentsScope+" .kd", "color", PygmentsScope); got != "--sd-hl-kd-color" {
		t.Errorf("got %q, want %q", got, "--sd-hl-kd-color")
	}
	if got := pygmentsVar(PygmentsScope, "background-color", PygmentsScope); got != "--sd-hl-base-background-color" {
		t.Errorf("got %q, want %q", got, "--sd-hl-base-background-color")
	}
}

func TestTheVariableNamesAreUnique(t *testing.T) {
	// A collision would make two selectors share one value and paint one
	// of the two tokens wrong, so the generator refuses it. Nothing in
	// chroma's class vocabulary collides today; this asserts that.
	for _, pair := range stylePairs {
		css := generate(t, pair[0], pair[1])
		names := definedVars(cssBlocks(css)[":root"])
		var sorted []string
		for name := range names {
			sorted = append(sorted, name)
		}
		sort.Strings(sorted)
		for i := 1; i < len(sorted); i++ {
			if sorted[i] == sorted[i-1] {
				t.Errorf("%v: %s defined twice", pair, sorted[i])
			}
		}
	}
}
