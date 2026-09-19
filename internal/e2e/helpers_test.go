//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stricttools/selfdoc/internal/themes"
)

// served returns the tree label is served from, and its path on that tree.
func served(fixture *Fixture, label string) (*servedSite, string) {
	if path, ok := AssemblyPages[label]; ok {
		return fixture.Assembly, path
	}
	return fixture.Standalone, StandalonePages[label]
}

// open navigates to the page class label, wherever it is served from.
func open(t *testing.T, page playwright.Page, fixture *Fixture, label string) *servedSite {
	t.Helper()
	site, path := served(fixture, label)
	gotoPath(t, page, site, path, label)
	return site
}

// gotoPath navigates to one address on one tree and fails unless it answered.
func gotoPath(t *testing.T, page playwright.Page, site *servedSite, path, label string) playwright.Response {
	t.Helper()
	response, err := page.Goto(site.url(path), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
	})
	if err != nil {
		t.Fatalf("[%s] %s (%s): %v", site.theme, label, path, err)
	}
	if response == nil || !response.Ok() {
		status := "nothing"
		if response != nil {
			status = strconv.Itoa(response.Status())
		}
		t.Fatalf("[%s] %s (%s) answered %s", site.theme, label, path, status)
	}
	return response
}

// boxesIntersect reports whether two bounding boxes overlap by more than
// tolerance pixels.
func boxesIntersect(a, b *playwright.Rect, tolerance float64) bool {
	if a == nil || b == nil {
		return false
	}
	return a.X < b.X+b.Width-tolerance &&
		b.X < a.X+a.Width-tolerance &&
		a.Y < b.Y+b.Height-tolerance &&
		b.Y < a.Y+a.Height-tolerance
}

// settleAnimations waits for every finite animation to finish.
//
// A computed style sampled mid-fade is the composite of the element over
// whatever is behind it, which turns a passing contrast ratio into a failing
// one at random. Infinite animations are skipped so this can never hang.
func settleAnimations(t *testing.T, page playwright.Page) {
	t.Helper()
	eval(t, page, `() => Promise.all(
            document.getAnimations()
              .filter(a => {
                  const t = a.effect && a.effect.getTiming();
                  return t && t.iterations !== Infinity;
              })
              .map(a => a.finished.catch(() => {}))
        )`)
}

// visibleCount is how many elements matching selector the browser actually
// paints.
func visibleCount(t *testing.T, page playwright.Page, selector string) int {
	t.Helper()
	return evalInt(t, page, `(sel) => Array.from(document.querySelectorAll(sel)).filter(el => {
            const style = getComputedStyle(el);
            if (style.display === 'none' || style.visibility === 'hidden') return false;
            if (parseFloat(style.opacity) === 0) return false;
            const box = el.getBoundingClientRect();
            return box.width > 0 && box.height > 0;
        }).length`, selector)
}

// scrollPageTo scrolls the content region to top and waits until it arrived.
//
// The scrolling element is #tm-content, not the document: every theme states
// the framework's application frame, which is fixed to the viewport with the
// content column as its only scroller.
//
// The pages set scroll-behavior: smooth, so assigning scrollTop starts an
// animation rather than moving the column, and a geometry reading taken a
// fixed number of milliseconds later is a reading of some frame in the middle
// of it -- a layout no reader ever sees. What every assertion here is about is
// the layout at rest, so the animation is turned off for the jump and the
// arrival is waited for.
func scrollPageTo(t *testing.T, page playwright.Page, top int) {
	t.Helper()
	eval(t, page, `(t) => {
            const el = document.getElementById('tm-content');
            el.style.scrollBehavior = 'auto';
            el.scrollTop = t;
        }`, top)
	if _, err := page.WaitForFunction(
		"(t) => { const el = document.getElementById('tm-content');"+
			" return Math.abs(el.scrollTop - t) < 2"+
			" || el.scrollTop >= el.scrollHeight - el.clientHeight - 2; }",
		top,
		playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)},
	); err != nil {
		t.Fatalf("the content column never reached %d: %v", top, err)
	}
}

// setViewport resizes the page and gives the layout the settle the Python
// suite gives it.
func setViewport(t *testing.T, page playwright.Page, width, height int, settleMS float64) {
	t.Helper()
	if err := page.SetViewportSize(width, height); err != nil {
		t.Fatalf("setting the viewport to %dx%d: %v", width, height, err)
	}
	page.WaitForTimeout(settleMS)
}

// waitForNetworkIdle waits until the page has stopped fetching.
func waitForNetworkIdle(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	}); err != nil {
		t.Fatalf("waiting for the network to go idle: %v", err)
	}
}

// -- reading what the browser answers -------------------------------------------

// eval evaluates script in the page and fails the test when it raises.
func eval(t *testing.T, page playwright.Page, script string, arg ...any) any {
	t.Helper()
	value, err := page.Evaluate(script, arg...)
	if err != nil {
		t.Fatalf("evaluating in the page: %v", err)
	}
	return value
}

// evalMap evaluates script and reads its result as an object.
func evalMap(t *testing.T, page playwright.Page, script string, arg ...any) map[string]any {
	t.Helper()
	value := eval(t, page, script, arg...)
	if value == nil {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("the page answered %#v, which is not an object", value)
	}
	return object
}

// evalInt evaluates script and reads its result as a whole number.
func evalInt(t *testing.T, page playwright.Page, script string, arg ...any) int {
	t.Helper()
	return int(evalFloat(t, page, script, arg...))
}

// evalFloat evaluates script and reads its result as a number.
func evalFloat(t *testing.T, page playwright.Page, script string, arg ...any) float64 {
	t.Helper()
	return asFloat(t, eval(t, page, script, arg...))
}

// evalString evaluates script and reads its result as a string, reporting
// whether the page answered null.
func evalString(t *testing.T, page playwright.Page, script string, arg ...any) (string, bool) {
	t.Helper()
	value := eval(t, page, script, arg...)
	if value == nil {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("the page answered %#v, which is not a string", value)
	}
	return text, true
}

// evalStrings evaluates script and reads its result as a list of strings.
func evalStrings(t *testing.T, page playwright.Page, script string, arg ...any) []string {
	t.Helper()
	value := eval(t, page, script, arg...)
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("the page answered %#v, which is not a list", value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("the page answered %#v inside a list of strings", item)
		}
		out = append(out, text)
	}
	return out
}

// asFloat reads a number out of what the page answered.
func asFloat(t *testing.T, value any) float64 {
	t.Helper()
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case int64:
		return float64(number)
	}
	t.Fatalf("the page answered %#v, which is not a number", value)
	return 0
}

// mapFloat reads one numeric member out of an object the page answered.
func mapFloat(t *testing.T, object map[string]any, key string) float64 {
	t.Helper()
	return asFloat(t, object[key])
}

// mapString reads one string member out of an object the page answered.
func mapString(object map[string]any, key string) string {
	text, _ := object[key].(string)
	return text
}

// mapBool reads one boolean member out of an object the page answered.
func mapBool(object map[string]any, key string) bool {
	flag, _ := object[key].(bool)
	return flag
}

// -- the search surfaces ----------------------------------------------------------

// searchSurface is the selectors one theme's search is driven through.
type searchSurface struct {
	overlay string
	input   string
	result  string
}

// The two search surfaces, because a theme that composes the framework draws
// its own. A framework theme loads the framework's command palette over
// Pagefind's query API and never loads Pagefind's shipped widget; every other
// theme mounts the widget in selfdoc's own dialog. The selectors differ, what
// a reader does does not.
var (
	frameworkSurface = searchSurface{
		overlay: "dialog.tm-palette",
		input:   ".tm-palette-input",
		result:  ".tm-palette-item",
	}
	widgetSurface = searchSurface{
		overlay: "#search-dialog",
		input:   ".pagefind-ui__search-input",
		result:  ".pagefind-ui__result",
	}
)

// isFrameworkTheme reports whether the theme composes a framework, and
// therefore draws its own search. Read from the theme registry rather than
// listed, so a theme that starts or stops composing one joins the right sweep
// by itself.
func isFrameworkTheme(t *testing.T, theme string) bool {
	t.Helper()
	framework, err := themes.FrameworkOf(theme)
	if err != nil {
		t.Fatalf("reading the framework block of %s: %v", theme, err)
	}
	return framework != nil
}

// surfaceOf is the search surface this theme draws.
func surfaceOf(t *testing.T, fixture *Fixture) searchSurface {
	t.Helper()
	if isFrameworkTheme(t, fixture.Theme) {
		return frameworkSurface
	}
	return widgetSurface
}

// awaitOverlay waits until the search overlay is really painted, not merely
// present.
//
// The framework's palette animates in, so an assertion sampled the instant its
// input appears reads an opacity of 0 and calls a perfectly open overlay
// invisible. Waiting on what the assertion measures is the honest wait: the
// same predicate, given time to become true.
func awaitOverlay(t *testing.T, page playwright.Page, surface searchSurface) {
	t.Helper()
	if _, err := page.WaitForFunction(
		`(sel) => Array.from(document.querySelectorAll(sel)).some(el => {
            const style = getComputedStyle(el);
            if (style.display === 'none' || style.visibility === 'hidden') return false;
            if (parseFloat(style.opacity) === 0) return false;
            const box = el.getBoundingClientRect();
            return box.width > 0 && box.height > 0;
        })`,
		surface.overlay,
		playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(15000)},
	); err != nil {
		t.Fatalf("the search overlay %s never painted: %v", surface.overlay, err)
	}
}

// openSearch opens search the way a reader does, and waits for it to be there.
func openSearch(t *testing.T, page playwright.Page, fixture *Fixture) searchSurface {
	t.Helper()
	surface := surfaceOf(t, fixture)
	if err := page.Keyboard().Press("Control+k"); err != nil {
		t.Fatalf("pressing Ctrl+K: %v", err)
	}
	if _, err := page.WaitForSelector(surface.input, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(15000),
	}); err != nil {
		t.Fatalf("[%s] the search input %s never appeared: %v",
			fixture.Theme, surface.input, err)
	}
	awaitOverlay(t, page, surface)
	return surface
}
