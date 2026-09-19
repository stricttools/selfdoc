package build

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/prose"
	"github.com/stricttools/selfdoc/internal/robots"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// outputKeyToURL is the URL path for an emitted HTML file, keyed from the
// output root.
//
// The directory-index form the addressing authority produces, applied to a
// full output key: "index.html" is the site root ("") and "guide/index.html"
// is "guide/". A sitemap entry and a canonical link therefore name the same
// URL, which they did not while the sitemap spelled the home page
// "index.html".
func outputKeyToURL(outputKey string) string {
	if outputKey == "index.html" {
		return ""
	}
	if strings.HasSuffix(outputKey, "/index.html") {
		return strings.TrimSuffix(outputKey, "index.html")
	}
	return outputKey
}

// GenerateSitemap renders a sitemap.xml document for the given HTML paths.
//
// htmlPaths are output keys, which may carry a locale or version prefix while
// pageDates is keyed by the unprefixed Markdown path -- so a date lookup that
// misses retries against the path's last component and against everything
// below the first two prefix segments. The last-modified date is the page's
// modification date; a page with none gets a bare location entry.
func GenerateSitemap(htmlPaths []string, urlBuilder urls.URLBuilder, pageDates map[string]page.PageDates) string {
	sorted := append([]string(nil), htmlPaths...)
	sort.Strings(sorted)

	entries := make([]string, 0, len(sorted))
	for _, path := range sorted {
		mdPath := html.HTMLToMdPath(path)
		url := outputKeyToURL(path)
		dates, found := pageDates[mdPath]
		if !found {
			parts := strings.Split(mdPath, "/")
			if len(parts) > 1 {
				dates, found = pageDates[parts[len(parts)-1]]
				if !found && len(parts) > 2 {
					dates, found = pageDates[strings.Join(parts[2:], "/")]
				}
			}
		}
		fullURL := urlBuilder.PageURL(url)
		if found && dates.Modified != "" {
			entries = append(entries, fmt.Sprintf(
				"  <url><loc>%s</loc><lastmod>%s</lastmod></url>", fullURL, dates.Modified))
			continue
		}
		entries = append(entries, fmt.Sprintf("  <url><loc>%s</loc></url>", fullURL))
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n" +
		strings.Join(entries, "\n") + "\n</urlset>\n"
}

// firstContentParagraph returns the first prose paragraph of Markdown content
// as one line.
//
// Leading headings, blank lines and fenced code blocks are skipped, and then
// the whole first paragraph is returned through [prose.FirstParagraph] --
// soft-wrapped physical lines joined. llms.txt and Atom-feed summaries carry
// the complete first paragraph: no character cap, no ellipsis.
func firstContentParagraph(content string) string {
	var proseLines []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		stripped := util.PythonStrip(line)
		if strings.HasPrefix(stripped, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(stripped, "#") {
			continue
		}
		if stripped == "" {
			if len(proseLines) > 0 {
				break
			}
			continue
		}
		proseLines = append(proseLines, stripped)
	}
	return prose.FirstParagraph(strings.Join(proseLines, "\n"))
}

// GenerateLLMSTxt renders llms.txt: a brief index naming every page, its title
// and its first paragraph.
//
// pageAddresses maps every listed page's Markdown path to its address, and is
// required: a page's absolute URL is its mounted address, and this function
// has no way to work one out on its own.
func GenerateLLMSTxt(
	projectName string,
	markdownFiles []page.SourceFile,
	urlBuilder urls.URLBuilder,
	pageAddresses map[string]address.PageAddress,
) (string, error) {
	lines := []string{"# " + projectName + " Documentation", ""}

	// The project description is the first non-heading line of index.md.
	description := ""
	for _, line := range strings.Split(sourceContent(markdownFiles, "index.md"), "\n") {
		trimmed := util.PythonStrip(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			description = trimmed
			break
		}
	}
	if description != "" {
		lines = append(lines, "> "+description, "")
	}

	lines = append(lines, "## Pages", "")

	for _, src := range sortedSources(markdownFiles) {
		title := page.ExtractTitle(src.Content, strings.ReplaceAll(src.MdPath, ".md", ""))
		addr, ok := pageAddresses[src.MdPath]
		if !ok {
			return "", fmt.Errorf("llms.txt: no address for page %q", src.MdPath)
		}
		url := urlBuilder.PageURL(addr.Stable)
		lines = append(lines, fmt.Sprintf("- [%s](%s): %s",
			title, url, firstContentParagraph(src.Content)))
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// GenerateLLMSFullTxt renders llms-full.txt: the full text of every page as
// plain Markdown.
//
// Each page's section opens with its title as a heading and a comment naming
// its source path, and the sections are separated by a thematic break.
func GenerateLLMSFullTxt(projectName string, markdownFiles []page.SourceFile) string {
	parts := []string{"# " + projectName + " Documentation", ""}
	for _, src := range sortedSources(markdownFiles) {
		base := filepath.Base(src.MdPath)
		fallback := util.TitleCase(strings.NewReplacer("-", " ", "_", " ").Replace(
			strings.TrimSuffix(base, filepath.Ext(base))))
		title := page.ExtractTitle(src.Content, fallback)
		parts = append(parts,
			"## "+title,
			"<!-- path: "+src.MdPath+" -->",
			"",
			util.PythonStrip(src.Content),
			"",
			"---",
			"")
	}
	return strings.Join(parts, "\n") + "\n"
}

// FeedEntry is one Atom entry: the date it sorts by and the XML that renders
// it.
type FeedEntry struct {
	// Date is the entry's date, in ISO form, which the feed sorts by.
	Date string
	// XML is the rendered entry element.
	XML string
}

// MakeFeedEntry renders one Atom feed entry.
//
// It takes abstract page metadata rather than raw Markdown, so the per-project
// feed and the assembly's site-wide one build their entries the same way. An
// entry with no summary omits the element rather than carrying an empty one.
func MakeFeedEntry(title, url, date, summary string) FeedEntry {
	entry := "  <entry>\n" +
		"    <title>" + util.EscapeHTML(title) + "</title>\n" +
		`    <link href="` + url + `"/>` + "\n" +
		"    <id>" + url + "</id>\n" +
		"    <updated>" + date + "T00:00:00Z</updated>\n"
	if summary != "" {
		entry += "    <summary>" + util.EscapeHTML(summary) + "</summary>\n"
	}
	entry += "  </entry>"
	return FeedEntry{Date: date, XML: entry}
}

// FeedOptions is everything the Atom feed is built from.
type FeedOptions struct {
	// OutputDir is the site's output root, where feed.xml is written.
	OutputDir string
	// ProjectName and Description title and subtitle the feed.
	ProjectName string
	Description string
	// MarkdownFiles are the pages the feed lists.
	MarkdownFiles []page.SourceFile
	// Frontmatter maps a page's path to its metadata. A page declaring
	// "feed: false" is left out.
	Frontmatter map[string]util.Frontmatter
	// PageDates maps a page's path to its dates.
	PageDates map[string]page.PageDates
	// URLBuilder builds the absolute URLs the entries carry.
	URLBuilder urls.URLBuilder
	// PageAddresses maps every listed page to its address, and is required:
	// an entry's link and id are its mounted address.
	PageAddresses map[string]address.PageAddress
	// MaxEntries truncates the feed to the most recent entries. Nil keeps
	// every entry.
	MaxEntries *int
	// Now supplies the feed-level date for a project whose pages state
	// none. The zero value takes the current time.
	Now time.Time
}

// GenerateAtomFeed writes the site's Atom feed and returns the path written.
//
// Entries are ordered by date, most recent first. A post uses its declared
// publication date and a documentation page its modification date, which is
// what puts a newly published post at the top of a feed whose docs were built
// the same day.
func GenerateAtomFeed(opts FeedOptions, h *effects.Handle) (string, error) {
	var entries []FeedEntry
	for _, src := range sortedSources(opts.MarkdownFiles) {
		meta := opts.Frontmatter[src.MdPath]
		if feed, declared := meta["feed"].(bool); declared && !feed {
			continue
		}

		addr, ok := opts.PageAddresses[src.MdPath]
		if !ok {
			return "", fmt.Errorf("feed: no address for page %q", src.MdPath)
		}

		title := util.PythonStrOrEmpty(meta["title"])
		if title == "" {
			title = page.ExtractTitle(src.Content, strings.ReplaceAll(src.MdPath, ".md", ""))
		}

		// Posts use their publication date; docs use the modification
		// date the build computed.
		pageDate := ""
		declaredDate := util.PythonStrOrEmpty(meta["date"])
		if util.PythonStrOrEmpty(meta["type"]) == "post" && declaredDate != "" {
			pageDate = declaredDate
		} else if dates, found := opts.PageDates[src.MdPath]; found {
			pageDate = dates.Modified
		}

		entries = append(entries, MakeFeedEntry(
			title,
			opts.URLBuilder.PageURL(addr.Stable),
			pageDate,
			firstContentParagraph(src.Content)))
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Date > entries[j].Date })

	if opts.MaxEntries != nil && len(entries) > *opts.MaxEntries {
		entries = entries[:*opts.MaxEntries]
	}

	mostRecent := ""
	for _, src := range opts.MarkdownFiles {
		if dates, found := opts.PageDates[src.MdPath]; found && dates.Modified > mostRecent {
			mostRecent = dates.Modified
		}
	}
	if mostRecent == "" {
		now := opts.Now
		if now.IsZero() {
			now = time.Now()
		}
		mostRecent = now.Format("2006-01-02")
	}

	subtitleLine := ""
	if opts.Description != "" {
		subtitleLine = "  <subtitle>" + util.EscapeHTML(opts.Description) + "</subtitle>\n"
	}

	renderedEntries := make([]string, 0, len(entries))
	for _, entry := range entries {
		renderedEntries = append(renderedEntries, entry.XML)
	}

	feedSelfURL := opts.URLBuilder.FeedURL()
	feedHomeURL := opts.URLBuilder.PageURL("")
	feedXML := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<feed xmlns="http://www.w3.org/2005/Atom">` + "\n" +
		"  <title>" + util.EscapeHTML(opts.ProjectName) + " Documentation</title>\n" +
		`  <link href="` + feedSelfURL + `" rel="self"/>` + "\n" +
		`  <link href="` + feedHomeURL + `"/>` + "\n" +
		"  <id>" + feedHomeURL + "</id>\n" +
		"  <updated>" + mostRecent + "T00:00:00Z</updated>\n" +
		subtitleLine +
		strings.Join(renderedEntries, "\n") + "\n" +
		"</feed>\n"

	feedPath := filepath.Join(opts.OutputDir, "feed.xml")
	if err := h.Write(feedPath, []byte(feedXML), effects.ModeDefault); err != nil {
		return "", err
	}
	return feedPath, nil
}

// GenerateRobotsTxt writes robots.txt and returns the path written.
//
// The crawler policy itself is declared in the robots package, so this file
// and the assembly's site-wide robots.txt cannot drift apart. With
// hasSitemapIndex the file names sitemap-index.xml instead of sitemap.xml.
func GenerateRobotsTxt(outputDir string, urlBuilder urls.URLBuilder, hasSitemapIndex bool, h *effects.Handle) (string, error) {
	sitemapFile := "sitemap.xml"
	if hasSitemapIndex {
		sitemapFile = "sitemap-index.xml"
	}
	content := robots.RenderRobotsTxt(urlBuilder.AssetURL(sitemapFile))
	path := filepath.Join(outputDir, "robots.txt")
	if err := h.Write(path, []byte(content), effects.ModeDefault); err != nil {
		return "", err
	}
	return path, nil
}

// GenerateHeaders writes the Cloudflare Pages _headers file and returns the
// path written.
//
// It carries the site's security headers and the immutable cache policy for
// everything whose bytes cannot change without its address changing.
func GenerateHeaders(outputDir string, h *effects.Handle) (string, error) {
	content := "/*\n" +
		"  Strict-Transport-Security: max-age=31536000; includeSubDomains; preload\n" +
		"  X-Content-Type-Options: nosniff\n" +
		"  X-Frame-Options: DENY\n" +
		"  Referrer-Policy: strict-origin-when-cross-origin\n" +
		"  Permissions-Policy: camera=(), microphone=(), geolocation=()\n" +
		"  X-XSS-Protection: 0\n" +
		"\n" +
		"/style.css\n" +
		"  Cache-Control: public, max-age=31536000, immutable\n" +
		"\n" +
		// A framework theme writes its sheet into css/ and ships the faces
		// beside it; those are the framework's own bytes and never change
		// without the stylesheet changing too.
		"/css/*\n" +
		"  Cache-Control: public, max-age=31536000, immutable\n" +
		"\n" +
		"/fonts/*\n" +
		"  Cache-Control: public, max-age=31536000, immutable\n" +
		"\n" +
		"/*.svg\n" +
		"  Cache-Control: public, max-age=31536000, immutable\n"
	path := filepath.Join(outputDir, "_headers")
	if err := h.Write(path, []byte(content), effects.ModeDefault); err != nil {
		return "", err
	}
	return path, nil
}

// GeneratePerLocaleSitemaps writes one sitemap per locale at
// "<locale>/sitemap.xml", and returns the paths written in locale order.
func GeneratePerLocaleSitemaps(
	outputDir string,
	localeCodes []string,
	perLocaleStableHTML map[string][]string,
	urlBuilder urls.URLBuilder,
	pageDates map[string]page.PageDates,
	h *effects.Handle,
) ([]string, error) {
	var written []string
	for _, localeCode := range localeCodes {
		content := GenerateSitemap(perLocaleStableHTML[localeCode], urlBuilder, pageDates)
		localeDir := filepath.Join(outputDir, localeCode)
		if err := h.MkdirAll(localeDir); err != nil {
			return written, err
		}
		sitemapPath := filepath.Join(localeDir, "sitemap.xml")
		if err := h.Write(sitemapPath, []byte(content), effects.ModeDefault); err != nil {
			return written, err
		}
		written = append(written, sitemapPath)
	}
	return written, nil
}

// GenerateSitemapIndex writes sitemap-index.xml at the output root, listing
// the per-locale sitemaps in sorted locale order, and returns the path
// written.
func GenerateSitemapIndex(outputDir string, localeCodes []string, urlBuilder urls.URLBuilder, h *effects.Handle) (string, error) {
	sorted := append([]string(nil), localeCodes...)
	sort.Strings(sorted)
	entries := make([]string, 0, len(sorted))
	for _, code := range sorted {
		entries = append(entries, "  <sitemap><loc>"+
			urlBuilder.AssetURL(code+"/sitemap.xml")+"</loc></sitemap>")
	}
	content := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n" +
		strings.Join(entries, "\n") + "\n</sitemapindex>\n"
	path := filepath.Join(outputDir, "sitemap-index.xml")
	if err := h.Write(path, []byte(content), effects.ModeDefault); err != nil {
		return "", err
	}
	return path, nil
}

// AuxiliaryOptions is everything the site-level files are built from.
type AuxiliaryOptions struct {
	// OutputDir is the site's output root.
	OutputDir string
	// ProjectName and Version name the project the site documents.
	ProjectName string
	Version     string
	// MarkdownFiles are every page the site published, which the social
	// cards, llms.txt and the feed are built from.
	MarkdownFiles []page.SourceFile
	// HTMLPaths are the stable output keys the sitemap lists.
	HTMLPaths []string
	// BaseURL is the site's base address.
	BaseURL string
	// HasCustomCSS says whether the project ships a custom.css.
	HasCustomCSS bool
	// Repo is the repository URL.
	Repo string
	// URLBuilder builds the absolute URLs every generated document carries.
	URLBuilder urls.URLBuilder
	// Lang is the language tag the 404 page declares.
	Lang string
	// PageDates and Frontmatter are the per-page metadata the sitemap and
	// the feed read.
	PageDates   map[string]page.PageDates
	Frontmatter map[string]util.Frontmatter
	// Description is the project-level description the feed subtitles.
	Description string
	// FeedURL is where the Atom feed sits relative to a page.
	FeedURL string
	// CriticalCSS is the stylesheet fragment the 404 page inlines.
	CriticalCSS string
	// AccentColor paints the social cards and the favicon.
	AccentColor string
	// ThemeMeta is the theme's metadata.
	ThemeMeta *themes.Metadata
	// Deploy is the config's deploy block, which decides whether a _headers
	// file is written.
	Deploy map[string]any
	// FeedMaxEntries truncates the feed. Nil keeps every entry.
	FeedMaxEntries *int
	// HasSitemapIndex points robots.txt at the sitemap index instead of the
	// single sitemap.
	HasSitemapIndex bool
	// MountLocale and MountProject are the mount coordinates the current
	// pages sit under: the 404 sits at the output root, but the pages it
	// links to live under a mount.
	MountLocale  string
	MountProject string
	// PageAddresses maps every page in MarkdownFiles to its address. It is
	// required: the feed and llms.txt emit absolute page URLs, and a page's
	// URL is its mounted address -- there is no sensible mountless answer
	// to fall back on. Both emit the stable address, which is the one that
	// keeps working.
	PageAddresses map[string]address.PageAddress
}

// GenerateAuxiliaryFiles writes the files that belong to the site rather than
// to a page: the social cards, the sitemap, llms.txt and llms-full.txt, the
// Atom feed, the 404 page, the favicon, robots.txt and -- for a Cloudflare
// Pages deploy -- the _headers file.
//
// It returns the paths written.
func GenerateAuxiliaryFiles(opts AuxiliaryOptions, h *effects.Handle) (map[string]bool, error) {
	if opts.PageAddresses == nil {
		return nil, errors.New(
			"page_addresses is required: auxiliary files emit absolute page " +
				"URLs, which are the pages' mounted addresses")
	}
	written := map[string]bool{}

	// Social cards, one per page. Every card carries the same bytes -- the
	// card is painted from the accent colour and nothing else -- so the
	// PNG is built once and written under each page's name.
	pngBytes, err := GenerateOGPNGBasic(opts.AccentColor)
	if err != nil {
		return written, err
	}
	for _, src := range sortedSources(opts.MarkdownFiles) {
		slug := strings.TrimSuffix(src.MdPath, ".md")
		pngPath := filepath.Join(opts.OutputDir, "og-"+slug+".png")
		if err := h.MkdirAll(filepath.Dir(pngPath)); err != nil {
			return written, err
		}
		if err := h.Write(pngPath, pngBytes, effects.ModeDefault); err != nil {
			return written, err
		}
		written[pngPath] = true
	}

	sitemapPath := filepath.Join(opts.OutputDir, "sitemap.xml")
	sitemapContent := GenerateSitemap(opts.HTMLPaths, opts.URLBuilder, opts.PageDates)
	if err := h.Write(sitemapPath, []byte(sitemapContent), effects.ModeDefault); err != nil {
		return written, err
	}
	written[sitemapPath] = true

	llmsTxt, llmsErr := GenerateLLMSTxt(opts.ProjectName, opts.MarkdownFiles, opts.URLBuilder, opts.PageAddresses)
	if llmsErr != nil {
		return written, llmsErr
	}
	llmsPath := filepath.Join(opts.OutputDir, "llms.txt")
	if err := h.Write(llmsPath, []byte(llmsTxt), effects.ModeDefault); err != nil {
		return written, err
	}
	written[llmsPath] = true

	llmsFullPath := filepath.Join(opts.OutputDir, "llms-full.txt")
	llmsFull := GenerateLLMSFullTxt(opts.ProjectName, opts.MarkdownFiles)
	if err := h.Write(llmsFullPath, []byte(llmsFull), effects.ModeDefault); err != nil {
		return written, err
	}
	written[llmsFullPath] = true

	feedPath, feedErr := GenerateAtomFeed(FeedOptions{
		OutputDir:     opts.OutputDir,
		ProjectName:   opts.ProjectName,
		Description:   opts.Description,
		MarkdownFiles: opts.MarkdownFiles,
		Frontmatter:   opts.Frontmatter,
		PageDates:     opts.PageDates,
		URLBuilder:    opts.URLBuilder,
		PageAddresses: opts.PageAddresses,
		MaxEntries:    opts.FeedMaxEntries,
	}, h)
	if feedErr != nil {
		return written, feedErr
	}
	written[feedPath] = true

	// The 404 page, and only for a project serving its own output root.
	// "404.html" is a hosting-provider convention answered at the root of
	// what is served, and a mounted project's output root is a subdirectory
	// of somebody else's site: no request ever reaches a subtree's copy,
	// the site's own root 404 answers instead, and the buried copy is an
	// unreachable page that still has to satisfy every assertion made
	// about a page.
	if opts.URLBuilder == nil || !opts.URLBuilder.Mounted() {
		var versionedMd, unversionedMd []page.SourceFile
		for _, src := range sortedSources(opts.MarkdownFiles) {
			if opts.PageAddresses[src.MdPath].Version != "" {
				versionedMd = append(versionedMd, src)
				continue
			}
			unversionedMd = append(unversionedMd, src)
		}
		navItems := page.BuildNav(versionedMd, opts.Frontmatter, unversionedMd, opts.Frontmatter)
		notFoundHTML, buildErr := page.Generate404Page(page.NotFoundOptions{
			ProjectName:  opts.ProjectName,
			Version:      opts.Version,
			HasCustomCSS: opts.HasCustomCSS,
			NavItems:     navItems,
			BaseURL:      opts.BaseURL,
			URLBuilder:   opts.URLBuilder,
			Lang:         opts.Lang,
			FeedURL:      opts.FeedURL,
			CriticalCSS:  opts.CriticalCSS,
			ThemeMeta:    opts.ThemeMeta,
			MountLocale:  opts.MountLocale,
			MountProject: opts.MountProject,
		})
		if buildErr != nil {
			return written, buildErr
		}
		notFoundPath := filepath.Join(opts.OutputDir, "404.html")
		if err := h.Write(notFoundPath, []byte(notFoundHTML), effects.ModeDefault); err != nil {
			return written, err
		}
		written[notFoundPath] = true
	}

	faviconPath := filepath.Join(opts.OutputDir, "favicon.svg")
	if err := h.Write(faviconPath,
		[]byte(GenerateFaviconSVG(opts.ProjectName, opts.AccentColor)), effects.ModeDefault); err != nil {
		return written, err
	}
	written[faviconPath] = true

	robotsPath, robotsErr := GenerateRobotsTxt(opts.OutputDir, opts.URLBuilder, opts.HasSitemapIndex, h)
	if robotsErr != nil {
		return written, robotsErr
	}
	written[robotsPath] = true

	if util.PythonStrOrEmpty(opts.Deploy["provider"]) == "cloudflare-pages" {
		headersPath, headersErr := GenerateHeaders(opts.OutputDir, h)
		if headersErr != nil {
			return written, headersErr
		}
		written[headersPath] = true
	}

	return written, nil
}
