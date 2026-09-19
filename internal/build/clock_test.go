package build

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/smm-h/stricttest/go/hygiene"
)

// The clock is the one thing a build reads that is not the project: a page
// with no declared date and no file behind it, and a feed whose pages state no
// date at all, both fall back to today. Both fallbacks take the date from the
// caller, so a build can be reproduced.
func TestTheClockIsAnInput(t *testing.T) {
	hygiene.Isolate(t)

	fixed := time.Date(2031, time.July, 4, 12, 0, 0, 0, time.UTC)

	t.Run("an overlay page with no date is dated from the stated clock", func(t *testing.T) {
		dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
		cfg, err := config.Load(dir)
		if err != nil {
			t.Fatalf("loading the fixture config: %v", err)
		}
		empty := ""
		opts := NewSingleOptions()
		opts.DirPath = dir
		opts.Config = cfg
		opts.MountLocale = &empty
		opts.MountVersion = &empty
		opts.VersionOverride = &empty
		opts.WriteBaselines = false
		opts.Now = fixed
		opts.OverlayDocs = map[string]string{"unsaved.md": "# Unsaved\n\nNo file behind it.\n"}
		opts.PageFilter = map[string]bool{"unsaved.md": true}

		result, err := BuildSingle(opts, effects.Unbound())
		if err != nil {
			t.Fatalf("BuildSingle: %v", err)
		}
		dates := result.PageDates["unsaved.md"]
		if dates.Modified != "2031-07-04" || dates.Published != "2031-07-04" {
			t.Errorf("the overlay page is dated %+v, want the stated clock's date", dates)
		}
	})

	t.Run("a feed whose pages state no date carries the stated clock's", func(t *testing.T) {
		sources := []page.SourceFile{{MdPath: "index.md", Content: "# Home\n\nWelcome.\n"}}
		addr, err := address.NewPageAddress("index.html", address.Coordinates{})
		if err != nil {
			t.Fatalf("addressing index.md: %v", err)
		}
		dir := t.TempDir()
		if _, err := GenerateAtomFeed(FeedOptions{
			OutputDir:     dir,
			ProjectName:   "Fixture",
			MarkdownFiles: sources,
			URLBuilder:    urls.NewSimpleURLBuilder("https://example.com"),
			PageAddresses: map[string]address.PageAddress{"index.md": addr},
			Now:           fixed,
		}, effects.Unbound()); err != nil {
			t.Fatalf("GenerateAtomFeed: %v", err)
		}
		assertCarries(t, "feed.xml", readFile(t, filepath.Join(dir, "feed.xml")),
			"<updated>2031-07-04T00:00:00Z</updated>")
	})
}
