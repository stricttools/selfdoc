//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stricttools/selfdoc/internal/themes"
)

// themeSignatures is what each theme's documentation page computed for its
// body, recorded as the sweep runs and read once at the end of the session.
var (
	signaturesMu    sync.Mutex
	themeSignatures = map[string]string{}
)

// recordSignature records one theme's painted body signature.
func recordSignature(theme, signature string) {
	signaturesMu.Lock()
	defer signaturesMu.Unlock()
	themeSignatures[theme] = signature
}

// TestTheFixtureItself guards the fixture, so a silently thin site cannot pass
// everything.
//
// Every assertion in this package is only as good as the tree it runs against.
// A build that quietly stopped emitting archives, or a Pagefind run that
// indexed nothing, would turn most of the suite green by leaving it nothing to
// be wrong about.
func TestTheFixtureItself(t *testing.T) {
	t.Run("the sweep covers every theme the toolchain ships", func(t *testing.T) {
		shipped := themes.List()
		if strings.Join(sortedStrings(Themes), ",") != strings.Join(sortedStrings(shipped), ",") {
			t.Errorf("the sweep visits %v but the toolchain ships %v; a theme nothing "+
				"renders is a theme nothing checks", sortedStrings(Themes), sortedStrings(shipped))
		}
	})

	forEachTheme(t, func(t *testing.T, fixture *Fixture) {
		for _, label := range AllPages {
			t.Run("the page class "+label+" is served", func(t *testing.T) {
				page := newPage(t)
				site, path := served(fixture, label)
				response, err := page.Request().Head(site.url(path))
				if err != nil {
					t.Fatalf("asking for %s: %v", path, err)
				}
				if response.Status() >= 400 {
					t.Errorf("[%s] the fixture does not serve %s at %s: %d",
						fixture.Theme, label, path, response.Status())
				}
			})
		}

		t.Run("both search indexes were really built", func(t *testing.T) {
			for _, site := range []*servedSite{fixture.Assembly, fixture.Standalone} {
				entry := filepath.Join(site.siteDir, "pagefind", "pagefind-entry.json")
				if info, err := os.Stat(entry); err != nil || info.IsDir() {
					t.Errorf("[%s] Pagefind produced no index over %s",
						fixture.Theme, site.siteDir)
				}
			}
		})

		t.Run("the standalone tree really carries an archive", func(t *testing.T) {
			archive := filepath.Join(fixture.Standalone.siteDir, "v", "0.1.0", "index.html")
			if info, err := os.Stat(archive); err != nil || info.IsDir() {
				t.Errorf("[%s] the standalone build emitted no archive, so every version-UI "+
					"assertion would pass vacuously", fixture.Theme)
			}
		})

		t.Run("the table really overflows its own box", func(t *testing.T) {
			// Every table assertion is vacuous against a table that fits.
			//
			// Measured at TableWidth, which is where the table assertions run,
			// and in the axis that is really there: .table-wrap is overflow-x:
			// auto with no height constraint, so it scrolls sideways and never
			// vertically. The pinned first column and the header-alignment
			// assertion both need that sideways room, and the sticky header is
			// exercised against the page's scroll as well as the box's.
			page := newPage(t)
			wrap := tablePage(t, page, fixture)
			value, err := wrap.Evaluate(
				"(el) => ({down: el.scrollHeight - el.clientHeight,"+
					" across: el.scrollWidth - el.clientWidth,"+
					" client: el.clientWidth,"+
					" table: el.querySelector('table').getBoundingClientRect().width,"+
					" overflowing: el.classList.contains('has-overflow')})", nil)
			if err != nil {
				t.Fatalf("measuring the table wrapper: %v", err)
			}
			room := value.(map[string]any)
			if mapFloat(t, room, "across") <= 0 {
				t.Errorf("[%s] the fixture table fits inside its own box at %dpx, so "+
					"nothing above scrolled anything: %v", fixture.Theme, TableWidth, room)
			}
			if !mapBool(room, "overflowing") {
				t.Errorf("[%s] the wrapper overflows but was never marked `has-overflow`, "+
					"so the pinned first column is not applied: %v", fixture.Theme, room)
			}
		})

		t.Run("the page itself scrolls past the table", func(t *testing.T) {
			// The sticky header is asserted against the page scroll too. "The
			// page" is #tm-content: the frame is fixed to the viewport and the
			// content column is the only thing that moves.
			page := newPage(t)
			open(t, page, fixture, "docs-tables")
			setViewport(t, page, TableWidth, 900, 80)
			room := evalFloat(t, page,
				"() => { const el = document.getElementById('tm-content');"+
					" return el.scrollHeight - el.clientHeight; }")
			if room <= 900 {
				t.Errorf("[%s] the table page is only %vpx taller than the viewport, so "+
					"scrolling it moves nothing worth measuring", fixture.Theme, room)
			}
		})

		t.Run("the pages carry the theme they were built under", func(t *testing.T) {
			// A sweep that served one theme three times would prove nothing.
			page := newPage(t)
			open(t, page, fixture, "docs")
			waitForNetworkIdle(t, page)
			signature, _ := evalString(t, page, `() => {
                const s = getComputedStyle(document.body);
                return [s.fontFamily, s.backgroundColor, s.color].join('|');
            }`)
			if signature == "" {
				t.Fatalf("[%s] no computed body style at all", fixture.Theme)
			}
			recordSignature(fixture.Theme, signature)
		})

		t.Run("the fixture declares exactly one external link", func(t *testing.T) {
			page := newPage(t)
			open(t, page, fixture, "docs")
			external := evalStrings(t, page, `(origin) => Array.from(
                document.querySelectorAll('article a[href]'))
                .map(a => a.href)
                .filter(h => h.startsWith('http') && !h.startsWith(origin))`,
				fixture.Assembly.origin())
			if len(external) != 1 || external[0] != AllowedExternal {
				t.Errorf("[%s] the fixture's external links are %v; the navigation "+
					"allowlist covers exactly %s", fixture.Theme, external, AllowedExternal)
			}
		})
	})
}
