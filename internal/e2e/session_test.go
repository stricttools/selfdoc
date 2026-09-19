//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"runtime/debug"
	"sort"
	"sync"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stricttools/selfdoc/internal/blog/preview"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// playwrightModule is the driver binding whose version decides which Node
// driver and which browser build the suite needs.
const playwrightModule = "github.com/playwright-community/playwright-go"

// setupCommand is the one-time installation this suite needs, named by every
// skip that fires because the browser is not there.
//
// The version is read from the build's own dependency list rather than written
// out, so the command can never name a version this binary was not built
// against.
func setupCommand() string {
	command := "go run " + playwrightModule + "/cmd/playwright"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return command + " install chromium (at the version go.mod pins)"
	}
	for _, dep := range info.Deps {
		if dep.Path == playwrightModule {
			return command + "@" + dep.Version + " install chromium"
		}
	}
	return command + " install chromium (at the version go.mod pins)"
}

// Themes is every theme the toolchain ships.
//
// Kept in step with the theme registry by a fixture guard rather than by
// memory: a theme that ships without joining the sweep is a theme nothing
// looks at.
var Themes = []string{"clean", "minimal", "tinymoon"}

// Widths are the widths the layout sweep visits, in order -- the monotonicity
// assertion reads them as a series.
var Widths = []int{700, 1000, 1280, 1440, 1920}

// AssemblyPages is one address per page class the assembled site serves. The
// suite asserts against page CLASSES, not against pages, so a class nobody
// listed here is a class the browser never looks at.
var AssemblyPages = map[string]string{
	"home":             "/",
	"docs":             "/alpha/",
	"docs-tables":      "/alpha/tables/",
	"docs-terms":       "/alpha/terms/",
	"docs-glossary":    "/alpha/glossary/",
	"docs-unversioned": "/beta/",
	"post":             "/blog/the-first-post/",
	"shared-projects":  "/projects/",
	"shared-blog":      "/blog/",
	"cv":               "/cv/",
}

// StandalonePages are the page classes only a project's own standalone site
// carries.
var StandalonePages = map[string]string{
	"standalone-current": "/",
	"standalone-archive": "/v/0.1.0/",
}

// AllPages is every page class, wherever it is served from.
var AllPages = func() []string {
	labels := []string{}
	for label := range AssemblyPages {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	standalone := []string{}
	for label := range StandalonePages {
		standalone = append(standalone, label)
	}
	sort.Strings(standalone)
	return append(labels, standalone...)
}()

// TOCPages are the page classes that carry a documentation table of contents.
// A post reads top to bottom and carries none at any width -- defect 2.
var TOCPages = []string{
	"docs", "docs-tables", "docs-terms", "docs-unversioned",
	"standalone-current", "standalone-archive",
}

// -- the one build per theme ---------------------------------------------------

// Fixture is the two served trees of one theme's fixture site.
type Fixture struct {
	Theme      string
	Assembly   *servedSite
	Standalone *servedSite
	Summary    *preview.Summary
}

// builtFixture is one theme's build attempt: the trees, or why they are not
// there.
type builtFixture struct {
	once    sync.Once
	fixture *Fixture
	err     error
	stops   []func()
	root    string
}

var (
	fixturesMu sync.Mutex
	fixtures   = map[string]*builtFixture{}
)

// fixtureFor returns the served trees of theme, building them the first time
// any test asks.
//
// Session-scoped, so each theme's trees are built once and every test in that
// theme's sweep looks at the same bytes. Building is the expensive part --
// three documentation builds, a graft, the shared generation and two Pagefind
// runs per theme.
func fixtureFor(t *testing.T, theme string) *Fixture {
	t.Helper()
	fixturesMu.Lock()
	entry, ok := fixtures[theme]
	if !ok {
		entry = &builtFixture{}
		fixtures[theme] = entry
	}
	fixturesMu.Unlock()

	entry.once.Do(func() {
		root, err := os.MkdirTemp("", "selfdoc-e2e-"+theme+"-")
		if err != nil {
			entry.err = err
			return
		}
		entry.root = root
		summary := buildFixtureSite(t, root, theme)
		standaloneDir := buildStandaloneProject(t, root, theme)

		assemblySite, stopAssembly, err := serve(summary.SiteDir, theme)
		if err != nil {
			entry.err = fmt.Errorf("serving the assembled tree: %w", err)
			return
		}
		entry.stops = append(entry.stops, stopAssembly)
		standaloneSite, stopStandalone, err := serve(standaloneDir, theme)
		if err != nil {
			entry.err = fmt.Errorf("serving the standalone tree: %w", err)
			return
		}
		entry.stops = append(entry.stops, stopStandalone)
		entry.fixture = &Fixture{
			Theme: theme, Assembly: assemblySite,
			Standalone: standaloneSite, Summary: summary,
		}
	})

	if entry.err != nil {
		t.Fatalf("[%s] building the fixture site: %v", theme, entry.err)
	}
	if entry.fixture == nil {
		// The build failed inside the once through t.Fatalf, which stops the
		// test that ran it; a later test would otherwise read a nil fixture.
		t.Fatalf("[%s] the fixture site was never built", theme)
	}
	return entry.fixture
}

// teardownFixtures stops every server and removes every built tree.
func teardownFixtures() {
	fixturesMu.Lock()
	defer fixturesMu.Unlock()
	for _, entry := range fixtures {
		for _, stop := range entry.stops {
			stop()
		}
		if entry.root != "" {
			_ = os.RemoveAll(entry.root)
		}
	}
}

// -- the one browser per test binary --------------------------------------------

var (
	driver     *playwright.Playwright
	browser    playwright.Browser
	browserErr error
)

// TestMain launches the one Chromium the whole binary shares, runs the suite,
// and then stops every preview server, removes every built tree and closes the
// browser.
func TestMain(m *testing.M) {
	driver, browserErr = playwright.Run()
	if browserErr == nil {
		browser, browserErr = driver.Chromium.Launch()
	}
	code := m.Run()
	teardownFixtures()
	if browser != nil {
		_ = browser.Close()
	}
	if driver != nil {
		_ = driver.Stop()
	}
	os.Exit(code)
}

// requireDeps skips the test unless everything the fixture needs is installed,
// and binds the environment isolation floor.
func requireDeps(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	requireBrowser(t)
}

// requireBrowser skips the test when the Playwright browser is not installed.
func requireBrowser(t *testing.T) {
	t.Helper()
	if browserErr != nil {
		t.Skipf("the Playwright browser is not available (%v). Install it "+
			"once with: %s", browserErr, setupCommand())
	}
}

// newPage opens a fresh browser context and one page in it.
//
// The browser is shared for the whole binary and the isolation that counts is
// the context: each test gets its own localStorage, so a dismissed notice or a
// stored theme choice never leaks into the next one.
func newPage(t *testing.T) playwright.Page {
	t.Helper()
	return newPageWith(t, playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	})
}

// newPageWith opens a fresh context with the given options and one page in it.
func newPageWith(t *testing.T, options playwright.BrowserNewContextOptions) playwright.Page {
	t.Helper()
	context, err := browser.NewContext(options)
	if err != nil {
		t.Fatalf("opening a browser context: %v", err)
	}
	t.Cleanup(func() { _ = context.Close() })
	page, err := context.NewPage()
	if err != nil {
		t.Fatalf("opening a page: %v", err)
	}
	return page
}

// forEachTheme runs body as one subtest per theme, with that theme's fixture.
func forEachTheme(t *testing.T, body func(t *testing.T, fixture *Fixture)) {
	t.Helper()
	for _, theme := range Themes {
		t.Run(theme, func(t *testing.T) {
			requireDeps(t)
			body(t, fixtureFor(t, theme))
		})
	}
}
