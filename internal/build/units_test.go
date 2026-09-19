package build

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/robots"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

func TestMinifyCSS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		css      string
		absent   []string
		contains []string
	}{
		{
			name:     "comments are removed",
			css:      "body { /* page background */ color: red; }",
			absent:   []string{"/* page background */"},
			contains: []string{"color"},
		},
		{
			name:     "whitespace collapses and separators lose their padding",
			css:      "body  {\n  color :  red ;\n  margin : 0 ;\n}\n",
			absent:   []string{"  ", "\n"},
			contains: []string{"color:red"},
		},
		{
			name:     "a semicolon before a closing brace goes",
			css:      "body { color: red; margin: 0; }",
			absent:   []string{";}"},
			contains: []string{"margin:0}"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := MinifyCSS(test.css)
			for _, unwanted := range test.absent {
				if strings.Contains(result, unwanted) {
					t.Errorf("MinifyCSS(%q) = %q, which still carries %q",
						test.css, result, unwanted)
				}
			}
			for _, wanted := range test.contains {
				if !strings.Contains(result, wanted) {
					t.Errorf("MinifyCSS(%q) = %q, which does not carry %q",
						test.css, result, wanted)
				}
			}
		})
	}
}

func TestMinifyHTML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		html     string
		absent   []string
		contains []string
	}{
		{
			name:     "comments are removed",
			html:     "<div><!-- a comment --><p>text</p></div>",
			absent:   []string{"<!-- a comment -->"},
			contains: []string{"<p>text</p>"},
		},
		{
			name:     "whitespace inside pre is kept verbatim",
			html:     "<p>  hello  </p>\n<pre>  line1\n  line2  </pre>\n<p>world</p>",
			absent:   []string{"\n<p>"},
			contains: []string{"  line1\n  line2  "},
		},
		{
			name:     "whitespace inside code is kept verbatim",
			html:     "<code>  a  +  b  </code>",
			contains: []string{"  a  +  b  "},
		},
		{
			name:     "whitespace inside script is kept verbatim",
			html:     "<div>  <script>\n  var x = 1;\n</script>  </div>",
			contains: []string{"\n  var x = 1;\n"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := MinifyHTML(test.html)
			for _, unwanted := range test.absent {
				if strings.Contains(result, unwanted) {
					t.Errorf("MinifyHTML(%q) = %q, which still carries %q",
						test.html, result, unwanted)
				}
			}
			for _, wanted := range test.contains {
				if !strings.Contains(result, wanted) {
					t.Errorf("MinifyHTML(%q) = %q, which does not carry %q",
						test.html, result, wanted)
				}
			}
		})
	}
}

func TestExtractCriticalCSS(t *testing.T) {
	t.Parallel()
	t.Run("the marker splits the sheet", func(t *testing.T) {
		css := ":root { --bg: #fff; }\n" + CriticalCSSMarker + "\n.admonition { color: red; }"
		critical, full := ExtractCriticalCSS(css)
		if !strings.Contains(critical, ":root") {
			t.Errorf("the critical part lost the root block: %q", critical)
		}
		if strings.Contains(critical, ".admonition") {
			t.Errorf("the critical part carries a non-critical rule: %q", critical)
		}
		if !strings.Contains(full, ":root") || !strings.Contains(full, ".admonition") {
			t.Errorf("the full sheet is not the whole sheet: %q", full)
		}
	})
	t.Run("a sheet with no marker is critical in full", func(t *testing.T) {
		css := ":root { --bg: #fff; }\n.admonition { color: red; }"
		critical, full := ExtractCriticalCSS(css)
		if critical != full {
			t.Errorf("critical = %q, full = %q; with no marker they are one answer",
				critical, full)
		}
	})
}

// makePNG builds a minimal PNG carrying the given dimensions in its IHDR.
func makePNG(width, height uint32) []byte {
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(13))
	out.WriteString("IHDR")
	_ = binary.Write(&out, binary.BigEndian, width)
	_ = binary.Write(&out, binary.BigEndian, height)
	out.Write([]byte{8, 2, 0, 0, 0})
	out.Write([]byte{0, 0, 0, 0})
	return out.Bytes()
}

// makeGIF builds a minimal GIF header carrying the given dimensions.
func makeGIF(width, height uint16) []byte {
	var out bytes.Buffer
	out.WriteString("GIF89a")
	_ = binary.Write(&out, binary.LittleEndian, width)
	_ = binary.Write(&out, binary.LittleEndian, height)
	return out.Bytes()
}

// makeJPEG builds a minimal JPEG with one SOF0 segment carrying the
// dimensions, behind a filler APP0 segment the reader has to step over.
func makeJPEG(width, height uint16) []byte {
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8})
	out.Write([]byte{0xFF, 0xE0})
	_ = binary.Write(&out, binary.BigEndian, uint16(6))
	out.Write([]byte{0, 0, 0, 0})
	out.Write([]byte{0xFF, 0xC0})
	_ = binary.Write(&out, binary.BigEndian, uint16(11))
	out.Write([]byte{8})
	_ = binary.Write(&out, binary.BigEndian, height)
	_ = binary.Write(&out, binary.BigEndian, width)
	out.Write([]byte{3, 0, 0})
	return out.Bytes()
}

// makeWebPLossy builds a minimal VP8 WebP carrying the given dimensions.
func makeWebPLossy(width, height uint16) []byte {
	out := make([]byte, 30)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], 22)
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8 ")
	binary.LittleEndian.PutUint16(out[26:28], width&0x3FFF)
	binary.LittleEndian.PutUint16(out[28:30], height&0x3FFF)
	return out
}

// makeWebPLossless builds a minimal VP8L WebP carrying the given dimensions.
func makeWebPLossless(width, height uint32) []byte {
	out := make([]byte, 30)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], 22)
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8L")
	bits := (width-1)&0x3FFF | ((height-1)&0x3FFF)<<14
	binary.LittleEndian.PutUint32(out[21:25], bits)
	return out
}

// makeWebPExtended builds a minimal VP8X WebP carrying the given dimensions.
func makeWebPExtended(width, height int) []byte {
	out := make([]byte, 30)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], 22)
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8X")
	putUint24LE(out[24:27], width-1)
	putUint24LE(out[27:30], height-1)
	return out
}

// putUint24LE writes a three-byte little-endian unsigned integer.
func putUint24LE(dst []byte, value int) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
	dst[2] = byte(value >> 16)
}

func TestImageDimensionReaders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name          string
		filename      string
		content       []byte
		wantOK        bool
		width, height int
	}{
		{"png", "a.png", makePNG(640, 480), true, 640, 480},
		{"gif", "a.gif", makeGIF(300, 200), true, 300, 200},
		{"jpeg", "a.jpg", makeJPEG(800, 600), true, 800, 600},
		{"jpeg with an uppercase extension", "a.JPEG", makeJPEG(120, 90), true, 120, 90},
		{"jpeg with a bad start marker", "bad.jpg", []byte{0xFF, 0xD9, 0x00}, false, 0, 0},
		{"a truncated jpeg", "short.jpg", []byte{0xFF, 0xD8, 0xFF}, false, 0, 0},
		{"webp lossy", "a.webp", makeWebPLossy(1024, 768), true, 1024, 768},
		{"webp lossless", "b.webp", makeWebPLossless(400, 300), true, 400, 300},
		{"webp extended", "c.webp", makeWebPExtended(1600, 1200), true, 1600, 1200},
		{"a webp with a bad header", "bad.webp", []byte("NOTRIFF" + strings.Repeat("x", 20)), false, 0, 0},
		{"a truncated webp", "short.webp", []byte("RIFF"), false, 0, 0},
		{"an unsupported format", "a.bmp", []byte("BM"), false, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(dir, test.filename)
			if err := os.WriteFile(path, test.content, 0o644); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
			dims, ok := getImageDimensions(path)
			if ok != test.wantOK {
				t.Fatalf("getImageDimensions(%s) ok = %v, want %v", test.filename, ok, test.wantOK)
			}
			if ok && (dims.Width != test.width || dims.Height != test.height) {
				t.Errorf("getImageDimensions(%s) = %dx%d, want %dx%d",
					test.filename, dims.Width, dims.Height, test.width, test.height)
			}
		})
	}
	t.Run("a file that is not there", func(t *testing.T) {
		for _, name := range []string{"missing.jpg", "missing.webp", "missing.png", "missing.gif"} {
			if _, ok := getImageDimensions(filepath.Join(dir, name)); ok {
				t.Errorf("getImageDimensions(%s) reported a size for a file that is not there", name)
			}
		}
	})
}

func TestAddImageDimensions(t *testing.T) {
	t.Parallel()
	docsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(docsDir, "logo.png"), makePNG(64, 32), 0o644); err != nil {
		t.Fatalf("writing the fixture image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "notes.txt"), []byte("not an image"), 0o644); err != nil {
		t.Fatalf("writing the fixture file: %v", err)
	}

	tests := []struct {
		name     string
		html     string
		contains []string
		absent   []string
	}{
		{
			name:     "a local image gets its size",
			html:     `<img src="logo.png" alt="Logo">`,
			contains: []string{`width="64"`, `height="32"`},
		},
		{
			name:   "a file of an unsupported kind is left alone",
			html:   `<img src="notes.txt" alt="Notes">`,
			absent: []string{"width="},
		},
		{
			name:   "an external image is left alone",
			html:   `<img src="https://example.com/logo.png" alt="Logo">`,
			absent: []string{"width="},
		},
		{
			name:   "an image that is not there is left alone",
			html:   `<img src="missing.png" alt="Missing">`,
			absent: []string{"width="},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := AddImageDimensions(test.html, docsDir, "index.md")
			for _, wanted := range test.contains {
				if !strings.Contains(result, wanted) {
					t.Errorf("AddImageDimensions(%q) = %q, which does not carry %q",
						test.html, result, wanted)
				}
			}
			for _, unwanted := range test.absent {
				if strings.Contains(result, unwanted) {
					t.Errorf("AddImageDimensions(%q) = %q, which carries %q",
						test.html, result, unwanted)
				}
			}
		})
	}
}

func TestGenerateOGPNGBasic(t *testing.T) {
	t.Parallel()
	png, err := GenerateOGPNGBasic("#0969da")
	if err != nil {
		t.Fatalf("GenerateOGPNGBasic: %v", err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("the card does not open with the PNG signature: %q", png[:8])
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "card.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatalf("writing the card: %v", err)
	}
	dims, ok := readPNGDimensions(path)
	if !ok {
		t.Fatal("the card's own reader cannot read it")
	}
	if dims.Width != 1200 || dims.Height != 630 {
		t.Errorf("the card is %dx%d, want the recommended 1200x630", dims.Width, dims.Height)
	}
}

func TestGenerateFaviconSVG(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		projectName string
		want        string
	}{
		{"the project's initial", "selfdoc", ">S<"},
		{"an empty name falls back", "", ">D<"},
		{"a lowercase name is upper-cased", "abc", ">A<"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svg := GenerateFaviconSVG(test.projectName, "#123456")
			if !strings.Contains(svg, test.want) {
				t.Errorf("GenerateFaviconSVG(%q) = %q, which does not carry %q",
					test.projectName, svg, test.want)
			}
			if !strings.Contains(svg, `fill="#123456"`) {
				t.Errorf("the favicon does not carry the accent colour: %q", svg)
			}
		})
	}
}

func TestGenerateRobotsTxt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, err := GenerateRobotsTxt(dir, urls.NewSimpleURLBuilder("https://example.com"), false, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateRobotsTxt: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading robots.txt: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "User-agent: *\nAllow: /") {
		t.Errorf("robots.txt does not allow every agent:\n%s", text)
	}
	if !strings.Contains(text, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("robots.txt does not name the sitemap:\n%s", text)
	}
	for _, agent := range robots.Agents {
		if !strings.Contains(text, "User-agent: "+agent) {
			t.Errorf("robots.txt does not name %s:\n%s", agent, text)
		}
	}

	indexPath, err := GenerateRobotsTxt(dir, urls.NewSimpleURLBuilder("https://example.com"), true, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateRobotsTxt with a sitemap index: %v", err)
	}
	indexed, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading robots.txt: %v", err)
	}
	if !strings.Contains(string(indexed), "Sitemap: https://example.com/sitemap-index.xml") {
		t.Errorf("robots.txt does not name the sitemap index:\n%s", indexed)
	}
}

func TestGenerateHeaders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, err := GenerateHeaders(dir, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateHeaders: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading _headers: %v", err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "/*\n") {
		t.Errorf("_headers does not open with the site-wide block:\n%s", text)
	}
	for _, wanted := range []string{
		"Strict-Transport-Security: max-age=31536000; includeSubDomains; preload",
		"X-Content-Type-Options: nosniff",
		"X-Frame-Options: DENY",
		"Referrer-Policy: strict-origin-when-cross-origin",
		"Permissions-Policy: camera=(), microphone=(), geolocation=()",
		"X-XSS-Protection: 0",
		"/style.css",
		"/*.svg",
		"Cache-Control: public, max-age=31536000, immutable",
	} {
		if !strings.Contains(text, wanted) {
			t.Errorf("_headers does not carry %q:\n%s", wanted, text)
		}
	}
}

func TestGenerateSitemap(t *testing.T) {
	t.Parallel()
	builder := urls.NewSimpleURLBuilder("https://example.com")
	tests := []struct {
		name      string
		htmlPaths []string
		dates     map[string]page.PageDates
		contains  []string
		absent    []string
	}{
		{
			name:      "the home page is the site root, not index.html",
			htmlPaths: []string{"index.html"},
			contains:  []string{"<loc>https://example.com/</loc>"},
			absent:    []string{"index.html"},
		},
		{
			name:      "a directory index is its directory",
			htmlPaths: []string{"guide/index.html"},
			contains:  []string{"<loc>https://example.com/guide/</loc>"},
		},
		{
			name:      "a page's modification date becomes its lastmod",
			htmlPaths: []string{"guide/index.html"},
			dates:     map[string]page.PageDates{"guide.md": {Published: "2024-01-01", Modified: "2024-02-01"}},
			contains:  []string{"<lastmod>2024-02-01</lastmod>"},
		},
		{
			name:      "a page with no date gets a bare entry",
			htmlPaths: []string{"guide/index.html"},
			absent:    []string{"lastmod"},
		},
		{
			name:      "a prefixed path still finds its date",
			htmlPaths: []string{"en/1.0.0/guide/index.html"},
			dates:     map[string]page.PageDates{"guide.md": {Modified: "2024-03-01"}},
			contains:  []string{"<lastmod>2024-03-01</lastmod>"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sitemap := GenerateSitemap(test.htmlPaths, builder, test.dates)
			if !strings.HasPrefix(sitemap, `<?xml version="1.0" encoding="UTF-8"?>`) {
				t.Errorf("the sitemap has no XML declaration:\n%s", sitemap)
			}
			for _, wanted := range test.contains {
				if !strings.Contains(sitemap, wanted) {
					t.Errorf("the sitemap does not carry %q:\n%s", wanted, sitemap)
				}
			}
			for _, unwanted := range test.absent {
				if strings.Contains(sitemap, unwanted) {
					t.Errorf("the sitemap carries %q:\n%s", unwanted, sitemap)
				}
			}
		})
	}
}

func TestMakeFeedEntry(t *testing.T) {
	t.Parallel()
	entry := MakeFeedEntry("A & B", "https://example.com/a/", "2024-01-15", "A summary <here>")
	if entry.Date != "2024-01-15" {
		t.Errorf("the entry sorts by %q, want the date it was given", entry.Date)
	}
	for _, wanted := range []string{
		"<title>A &amp; B</title>",
		`<link href="https://example.com/a/"/>`,
		"<id>https://example.com/a/</id>",
		"<updated>2024-01-15T00:00:00Z</updated>",
		"<summary>A summary &lt;here&gt;</summary>",
	} {
		if !strings.Contains(entry.XML, wanted) {
			t.Errorf("the entry does not carry %q:\n%s", wanted, entry.XML)
		}
	}
	bare := MakeFeedEntry("T", "https://example.com/", "2024-01-01", "")
	if strings.Contains(bare.XML, "<summary>") {
		t.Errorf("an entry with no summary carries an empty element:\n%s", bare.XML)
	}
}

func TestGenerateAtomFeed(t *testing.T) {
	t.Parallel()
	sources := []page.SourceFile{
		{MdPath: "index.md", Content: "# Home\n\nThe home page.\n"},
		{MdPath: "guide.md", Content: "# Guide\n\nThe guide.\n"},
		{MdPath: "secret.md", Content: "# Secret\n\nNot in the feed.\n"},
	}
	addresses := map[string]address.PageAddress{}
	for _, src := range sources {
		addr, err := address.NewPageAddress(
			strings.TrimSuffix(src.MdPath, ".md")+"/index.html", address.Coordinates{})
		if err != nil {
			t.Fatalf("addressing %s: %v", src.MdPath, err)
		}
		addresses[src.MdPath] = addr
	}
	// index.md is emitted at the site root, not at "index/".
	rootAddr, err := address.NewPageAddress("index.html", address.Coordinates{})
	if err != nil {
		t.Fatalf("addressing index.md: %v", err)
	}
	addresses["index.md"] = rootAddr

	dir := t.TempDir()
	feedPath, err := GenerateAtomFeed(FeedOptions{
		OutputDir:     dir,
		ProjectName:   "Fixture",
		Description:   "A fixture project.",
		MarkdownFiles: sources,
		Frontmatter: map[string]util.Frontmatter{
			"secret.md": {"feed": false},
		},
		PageDates: map[string]page.PageDates{
			"index.md": {Modified: "2024-01-01"},
			"guide.md": {Modified: "2024-06-01"},
		},
		URLBuilder:    urls.NewSimpleURLBuilder("https://example.com"),
		PageAddresses: addresses,
	}, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateAtomFeed: %v", err)
	}
	content, err := os.ReadFile(feedPath)
	if err != nil {
		t.Fatalf("reading the feed: %v", err)
	}
	feed := string(content)

	for _, wanted := range []string{
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<feed xmlns="http://www.w3.org/2005/Atom">`,
		"<title>Fixture Documentation</title>",
		`<link href="https://example.com/feed.xml" rel="self"/>`,
		"<subtitle>A fixture project.</subtitle>",
		// The feed's own date is the most recent page's.
		"<updated>2024-06-01T00:00:00Z</updated>",
	} {
		if !strings.Contains(feed, wanted) {
			t.Errorf("the feed does not carry %q:\n%s", wanted, feed)
		}
	}
	if strings.Contains(feed, "Not in the feed") || strings.Contains(feed, "<title>Secret</title>") {
		t.Errorf("a page declaring feed: false is in the feed:\n%s", feed)
	}
	// Most recent first.
	if strings.Index(feed, "<title>Guide</title>") > strings.Index(feed, "<title>Home</title>") {
		t.Errorf("the entries are not newest-first:\n%s", feed)
	}
}

func TestGenerateAtomFeedTruncatesToMaxEntries(t *testing.T) {
	t.Parallel()
	sources := []page.SourceFile{
		{MdPath: "a.md", Content: "# A\n\nOne.\n"},
		{MdPath: "b.md", Content: "# B\n\nTwo.\n"},
		{MdPath: "c.md", Content: "# C\n\nThree.\n"},
	}
	addresses := map[string]address.PageAddress{}
	for _, src := range sources {
		addr, err := address.NewPageAddress(
			strings.TrimSuffix(src.MdPath, ".md")+"/index.html", address.Coordinates{})
		if err != nil {
			t.Fatalf("addressing %s: %v", src.MdPath, err)
		}
		addresses[src.MdPath] = addr
	}
	limit := 2
	dir := t.TempDir()
	feedPath, err := GenerateAtomFeed(FeedOptions{
		OutputDir:     dir,
		ProjectName:   "Fixture",
		MarkdownFiles: sources,
		PageDates: map[string]page.PageDates{
			"a.md": {Modified: "2024-01-01"},
			"b.md": {Modified: "2024-02-01"},
			"c.md": {Modified: "2024-03-01"},
		},
		URLBuilder:    urls.NewSimpleURLBuilder("https://example.com"),
		PageAddresses: addresses,
		MaxEntries:    &limit,
	}, effects.Unbound())
	if err != nil {
		t.Fatalf("GenerateAtomFeed: %v", err)
	}
	feed := readFile(t, feedPath)
	if count := strings.Count(feed, "<entry>"); count != 2 {
		t.Errorf("the feed carries %d entries, want the declared cap of 2:\n%s", count, feed)
	}
	if strings.Contains(feed, "<title>A</title>") {
		t.Errorf("the oldest entry was kept over a newer one:\n%s", feed)
	}
}

func TestGenerateLLMSFiles(t *testing.T) {
	t.Parallel()
	sources := []page.SourceFile{
		{MdPath: "index.md", Content: "# Home\n\nThe project in one line.\n"},
		{MdPath: "user_guide.md", Content: "A page with no heading at all.\n"},
	}
	addresses := map[string]address.PageAddress{}
	rootAddr, err := address.NewPageAddress("index.html", address.Coordinates{})
	if err != nil {
		t.Fatalf("addressing index.md: %v", err)
	}
	addresses["index.md"] = rootAddr
	guideAddr, err := address.NewPageAddress("user_guide/index.html", address.Coordinates{})
	if err != nil {
		t.Fatalf("addressing user_guide.md: %v", err)
	}
	addresses["user_guide.md"] = guideAddr

	brief, err := GenerateLLMSTxt("Fixture", sources,
		urls.NewSimpleURLBuilder("https://example.com"), addresses)
	if err != nil {
		t.Fatalf("GenerateLLMSTxt: %v", err)
	}
	for _, wanted := range []string{
		"# Fixture Documentation",
		"> The project in one line.",
		"## Pages",
		"- [Home](https://example.com/): The project in one line.",
	} {
		if !strings.Contains(brief, wanted) {
			t.Errorf("llms.txt does not carry %q:\n%s", wanted, brief)
		}
	}

	full := GenerateLLMSFullTxt("Fixture", sources)
	for _, wanted := range []string{
		"# Fixture Documentation",
		"## Home",
		"<!-- path: index.md -->",
		// A page with no heading falls back to its file name, title-cased
		// with the separators turned into spaces.
		"## User Guide",
		"<!-- path: user_guide.md -->",
	} {
		if !strings.Contains(full, wanted) {
			t.Errorf("llms-full.txt does not carry %q:\n%s", wanted, full)
		}
	}
}

func TestCompressOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<html><body>hello</body></html>"), 0o644); err != nil {
		t.Fatalf("writing the fixture page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), makePNG(4, 4), 0o644); err != nil {
		t.Fatalf("writing the fixture image: %v", err)
	}

	count, err := CompressOutput(dir, effects.Unbound())
	if err != nil {
		t.Fatalf("CompressOutput: %v", err)
	}
	if count != 1 {
		t.Errorf("CompressOutput compressed %d files, want only the one text file", count)
	}
	for _, companion := range []string{"index.html.gz", "index.html.br"} {
		if _, err := os.Stat(filepath.Join(dir, companion)); err != nil {
			t.Errorf("%s was not written: %v", companion, err)
		}
	}
	for _, absent := range []string{"logo.png.gz", "logo.png.br"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("%s was written for a file that is already compressed", absent)
		}
	}

	gzipped, err := os.Open(filepath.Join(dir, "index.html.gz"))
	if err != nil {
		t.Fatalf("opening the gzip companion: %v", err)
	}
	defer gzipped.Close()
	reader, err := gzip.NewReader(gzipped)
	if err != nil {
		t.Fatalf("the gzip companion is not valid gzip: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the gzip companion: %v", err)
	}
	if string(decoded) != "<html><body>hello</body></html>" {
		t.Errorf("the gzip companion decodes to %q, want the original bytes", decoded)
	}
}

func TestPosixRelpath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target, base, want string
	}{
		{"guide/", "", "guide/"},
		{"guide/", "old", "../guide"},
		{"en/guide/", "en/old", "../guide"},
		{"v/1.0.0/guide/", "v/1.0.0/old", "../guide"},
		{"a/", "a", "."},
	}
	for _, test := range tests {
		if got := posixRelpath(test.target, test.base); got != test.want {
			t.Errorf("posixRelpath(%q, %q) = %q, want %q",
				test.target, test.base, got, test.want)
		}
	}
}

// readFile reads a file, failing the test when it cannot.
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}
