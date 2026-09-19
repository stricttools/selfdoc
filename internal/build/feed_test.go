package build

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// addressesUnder maps every source's Markdown path to its address under the
// given coordinates -- the test-side counterpart of what the build hands the
// site-level file generators.
func addressesUnder(t *testing.T, sources []page.SourceFile, coords address.Coordinates) map[string]address.PageAddress {
	t.Helper()
	addresses := make(map[string]address.PageAddress, len(sources))
	for _, src := range sources {
		addr, err := address.NewPageAddress(html.MdToHTMLPath(src.MdPath), coords)
		if err != nil {
			t.Fatalf("addressing %s: %v", src.MdPath, err)
		}
		addresses[src.MdPath] = addr
	}
	return addresses
}

// feedDates lists the entry dates a feed carries, in document order.
func feedDates(feed string) []string {
	var dates []string
	for _, match := range regexp.MustCompile(
		`<updated>(\d{4}-\d{2}-\d{2})T00:00:00Z</updated>`).FindAllStringSubmatch(feed, -1) {
		dates = append(dates, match[1])
	}
	// The first is the feed's own updated element, not an entry's.
	if len(dates) > 0 {
		return dates[1:]
	}
	return nil
}

// renderFeed builds a feed from sources, frontmatter and dates, and returns
// its XML.
func renderFeed(
	t *testing.T,
	sources []page.SourceFile,
	frontmatter map[string]util.Frontmatter,
	dates map[string]page.PageDates,
	maxEntries *int,
) string {
	t.Helper()
	dir := t.TempDir()
	_, err := GenerateAtomFeed(FeedOptions{
		OutputDir:     dir,
		ProjectName:   "Test",
		Description:   "Test desc",
		MarkdownFiles: sources,
		Frontmatter:   frontmatter,
		PageDates:     dates,
		URLBuilder:    urls.NewSimpleURLBuilder("https://example.com"),
		PageAddresses: addressesUnder(t, sources, address.Coordinates{}),
		MaxEntries:    maxEntries,
	}, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateAtomFeed: %v", err)
	}
	return readFile(t, filepath.Join(dir, "feed.xml"))
}

func TestFeedEntryEscaping(t *testing.T) {
	t.Parallel()
	entry := MakeFeedEntry(
		"Config <file> & settings",
		"https://example.com/config/",
		"2024-03-10",
		`Use "key" < 5 & value > 3`)
	assertCarries(t, "the entry", entry.XML,
		"<title>Config &lt;file&gt; &amp; settings</title>",
		"<summary>Use &quot;key&quot; &lt; 5 &amp; value &gt; 3</summary>")
	trimmed := strings.TrimSpace(entry.XML)
	if !strings.HasPrefix(trimmed, "<entry>") || !strings.HasSuffix(trimmed, "</entry>") {
		t.Errorf("the entry is not one entry element:\n%s", entry.XML)
	}
}

func TestFeedOrdering(t *testing.T) {
	t.Parallel()

	t.Run("a post is dated by its frontmatter, not by its file", func(t *testing.T) {
		feed := renderFeed(t,
			[]page.SourceFile{{MdPath: "posts/hello.md", Content: "# Hello\nPost content."}},
			map[string]util.Frontmatter{
				"posts/hello.md": {"title": "Hello Post", "type": "post", "date": "2024-06-15"},
			},
			map[string]page.PageDates{
				"posts/hello.md": {Published: "2024-01-01", Modified: "2024-01-01"},
			}, nil)
		if dates := feedDates(feed); len(dates) != 1 || dates[0] != "2024-06-15" {
			t.Errorf("the post's entry is dated %v, want its declared publication date", dates)
		}
	})

	t.Run("a documentation page is dated by its modification date", func(t *testing.T) {
		feed := renderFeed(t,
			[]page.SourceFile{{MdPath: "guide.md", Content: "# Guide\nGuide content."}},
			map[string]util.Frontmatter{"guide.md": {"title": "Guide"}},
			map[string]page.PageDates{
				"guide.md": {Published: "2024-01-01", Modified: "2024-03-10"},
			}, nil)
		if dates := feedDates(feed); len(dates) != 1 || dates[0] != "2024-03-10" {
			t.Errorf("the page's entry is dated %v, want its modification date", dates)
		}
	})

	t.Run("posts and pages are ordered together, newest first", func(t *testing.T) {
		feed := renderFeed(t,
			[]page.SourceFile{
				{MdPath: "index.md", Content: "# Home\nWelcome."},
				{MdPath: "posts/hello.md", Content: "# Hello\nPost content."},
				{MdPath: "guide.md", Content: "# Guide\nGuide content."},
			},
			map[string]util.Frontmatter{
				"index.md":       {"title": "Home"},
				"posts/hello.md": {"title": "Hello Post", "type": "post", "date": "2024-06-15"},
				"guide.md":       {"title": "Guide"},
			},
			map[string]page.PageDates{
				"index.md":       {Published: "2024-01-01", Modified: "2024-01-01"},
				"posts/hello.md": {Published: "2024-06-15", Modified: "2024-06-15"},
				"guide.md":       {Published: "2024-03-10", Modified: "2024-03-10"},
			}, nil)
		want := []string{"2024-06-15", "2024-03-10", "2024-01-01"}
		got := feedDates(feed)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("the entries are dated %v, want %v", got, want)
		}
	})

	t.Run("a newer page comes before an older post", func(t *testing.T) {
		feed := renderFeed(t,
			[]page.SourceFile{
				{MdPath: "posts/old.md", Content: "# Old Post\nOld content."},
				{MdPath: "reference.md", Content: "# Reference\nRef content."},
			},
			map[string]util.Frontmatter{
				"posts/old.md": {"title": "Old Post", "type": "post", "date": "2024-02-01"},
				"reference.md": {"title": "Reference"},
			},
			map[string]page.PageDates{
				"posts/old.md": {Published: "2024-02-01", Modified: "2024-02-01"},
				"reference.md": {Published: "2024-01-01", Modified: "2024-08-20"},
			}, nil)
		if strings.Index(feed, "<title>Reference</title>") >
			strings.Index(feed, "<title>Old Post</title>") {
			t.Error("the older post comes before the newer page")
		}
	})

	t.Run("the cap keeps the most recent entries", func(t *testing.T) {
		limit := 2
		feed := renderFeed(t,
			[]page.SourceFile{
				{MdPath: "posts/latest.md", Content: "# Latest\nLatest post."},
				{MdPath: "posts/mid.md", Content: "# Mid\nMid post."},
				{MdPath: "old-doc.md", Content: "# Old Doc\nOld documentation."},
			},
			map[string]util.Frontmatter{
				"posts/latest.md": {"title": "Latest", "type": "post", "date": "2024-12-01"},
				"posts/mid.md":    {"title": "Mid", "type": "post", "date": "2024-06-01"},
				"old-doc.md":      {"title": "Old Doc"},
			},
			map[string]page.PageDates{
				"posts/latest.md": {Modified: "2024-12-01"},
				"posts/mid.md":    {Modified: "2024-06-01"},
				"old-doc.md":      {Modified: "2024-01-01"},
			}, &limit)
		if count := strings.Count(feed, "<entry>"); count != 2 {
			t.Errorf("the feed carries %d entries, want the declared cap of 2", count)
		}
		assertCarries(t, "the feed", feed, "<title>Latest</title>")
		assertLacks(t, "the feed", feed, "<title>Old Doc</title>")
	})
}
