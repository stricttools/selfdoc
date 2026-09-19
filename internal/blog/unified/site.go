package unified

import (
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/util"
)

// writeLandingPage writes the page listing every constituent project.
//
// The landing page is a page of the "common" mount like any other, so its own
// address decides both hops: out to the output root for the shared stylesheet,
// and out and back in for each project card.
func (b *unifiedBuild) writeLandingPage() error {
	landingAddr, err := address.NewPageAddress("projects/index.html", address.Coordinates{
		Locale:  address.LocaleSegment(b.defaultLocaleCode, b.locales),
		Project: "common",
		Version: b.latestVersion,
	})
	if err != nil {
		return err
	}
	landingBody := generateLandingPage(b.projectCards, landingAddr.ToSiteRoot())
	landingFull := `<!DOCTYPE html>` +
		`<html lang="en">` +
		`<head><meta charset="utf-8">` +
		`<title>Projects - ` + b.common.projectName + `</title>` +
		`<link rel="stylesheet"` +
		` href="` + landingAddr.ToSiteRoot() + b.themeMeta.CSSRel + `">` +
		`</head>` +
		`<body>` +
		`<h1>Projects</h1>` +
		landingBody +
		`</body></html>`

	landingPath := filepath.Join(b.outputDir, filepath.FromSlash(landingAddr.OutputKey))
	if err := b.handle.MkdirAll(filepath.Dir(landingPath)); err != nil {
		return err
	}
	if err := b.handle.Write(landingPath, []byte(landingFull), effects.ModeDefault); err != nil {
		return err
	}
	b.written.add(landingPath)
	return nil
}

// writeSharedAssets writes the one stylesheet the whole unified site is
// painted with, the files that have to travel with it, and the docs-site's own
// custom.css.
//
// The sheet is the theme's, the highlight rules and the landing page's card
// styles appended to it: a unified site serves one stylesheet to every mount,
// so the card styles cannot live in a sheet of the landing page's own.
func (b *unifiedBuild) writeSharedAssets() error {
	cssPath := filepath.Join(b.outputDir, filepath.FromSlash(b.themeMeta.CSSRel))
	if err := b.handle.MkdirAll(filepath.Dir(cssPath)); err != nil {
		return err
	}

	themeCSS := b.rawThemeCSS
	pygmentsCSS, err := html.GeneratePygmentsCSS(b.themeMeta.PygmentsLight, b.themeMeta.PygmentsDark)
	if err != nil {
		return err
	}
	if pygmentsCSS != "" {
		themeCSS = themeCSS + "\n\n/* Pygments syntax highlighting */\n" + pygmentsCSS
	}
	themeCSS += "\n\n" + projectGridCSS

	if err := b.handle.Write(cssPath,
		[]byte(build.MinifyCSS(themeCSS)), effects.ModeDefault); err != nil {
		return err
	}
	b.written.add(cssPath)

	// A framework theme's fonts and modules travel with its stylesheet:
	// the sheets address them by relative URL.
	assets, err := themes.Assets(b.themeMeta.Name)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		dst := filepath.Join(b.outputDir, filepath.FromSlash(asset.Dest))
		if err := b.handle.MkdirAll(filepath.Dir(dst)); err != nil {
			return err
		}
		content, readErr := asset.Bytes()
		if readErr != nil {
			return readErr
		}
		if err := b.handle.Write(dst, content, effects.ModeDefault); err != nil {
			return err
		}
		b.written.add(dst)
	}

	if b.common.hasCustomCSS {
		customCSSDst := filepath.Join(b.outputDir, "custom.css")
		if err := b.handle.CopyFile(
			filepath.Join(b.common.docsDir, "custom.css"), customCSSDst); err != nil {
			return err
		}
		b.written.add(customCSSDst)
	}
	return nil
}

// writeAuxiliaryFiles writes the documents that describe the site rather than
// a page: the social cards, the sitemap, llms.txt, the Atom feed, the 404
// page, the favicon, robots.txt and the Cloudflare headers.
//
// They are built from the docs-site's own default-locale pass, whose pages
// mount under "common" -- so that is the mount their addresses are computed
// at, with the version-free pages keeping the addresses of their own pass.
func (b *unifiedBuild) writeAuxiliaryFiles() error {
	var htmlPaths []string
	for _, path := range b.written.order {
		if !strings.HasSuffix(path, ".html") {
			continue
		}
		rel, err := filepath.Rel(b.outputDir, path)
		if err != nil {
			return err
		}
		htmlPaths = append(htmlPaths, filepath.ToSlash(rel))
	}

	mountLocale := address.LocaleSegment(b.defaultLocaleCode, b.locales)
	pageAddresses := map[string]address.PageAddress{}
	for _, source := range b.common.markdownFiles {
		addr, err := address.NewPageAddress(
			html.MdToHTMLPath(source.MdPath),
			address.Coordinates{Locale: mountLocale, Project: "common", Version: b.latestVersion})
		if err != nil {
			return err
		}
		pageAddresses[source.MdPath] = addr
	}
	for mdPath, addr := range b.common.unversionedAddresses {
		pageAddresses[mdPath] = addr
	}

	deploy, _ := b.config["deploy"].(map[string]any)
	themeMeta := b.themeMeta
	auxWritten, err := build.GenerateAuxiliaryFiles(build.AuxiliaryOptions{
		OutputDir:      b.outputDir,
		ProjectName:    b.common.projectName,
		Version:        b.common.version,
		MarkdownFiles:  b.common.markdownFiles,
		HTMLPaths:      htmlPaths,
		BaseURL:        b.common.baseURL,
		HasCustomCSS:   b.common.hasCustomCSS,
		Repo:           util.PythonStrOrEmpty(b.config["repo"]),
		URLBuilder:     b.common.urlBuilder,
		Lang:           b.common.lang,
		PageDates:      b.common.pageDates,
		Frontmatter:    b.common.frontmatter,
		Description:    b.common.configDescription,
		FeedURL:        b.common.feedURL,
		CriticalCSS:    b.criticalCSS,
		AccentColor:    themeMeta.AccentColor,
		ThemeMeta:      &themeMeta,
		Deploy:         deploy,
		FeedMaxEntries: feedMaxEntries(b.config),
		// The auxiliary files describe the docs-site's own common pages.
		MountLocale:   mountLocale,
		MountProject:  "common",
		PageAddresses: pageAddresses,
	}, b.handle)
	b.written.merge(auxWritten)
	return err
}

// writeRootRedirect writes the stub at the output root that sends a visitor to
// the docs-site's own home page, and the Cloudflare rule that does the same
// before a document is ever served.
func (b *unifiedBuild) writeRootRedirect() error {
	home, err := address.NewPageAddress("index.html", address.Coordinates{
		Locale:  address.LocaleSegment(b.defaultLocaleCode, b.locales),
		Project: "common",
	})
	if err != nil {
		return err
	}
	// Document-relative, with no leading slash. The build output is not
	// always served from an origin root: an assembly serves it under
	// /<slug>/ and GitHub Pages project sites under /<repo>/. A
	// root-relative hop escapes that subtree; a document-relative one
	// resolves correctly in every case, origin root included. The stub sits
	// at the output root, so the pinned address is already the hop.
	redirectURL := home.Stable
	// Absolute canonical: a root-relative one resolves against whatever host
	// served the stub, so every alias of the site would claim to be
	// canonical.
	canonicalURL := b.common.urlBuilder.PageURL(redirectURL)
	rootIndex := "<!DOCTYPE html>\n" +
		"<html>\n" +
		"<head>\n" +
		`  <meta http-equiv="refresh" content="0;url=` + redirectURL + `">` + "\n" +
		`  <link rel="canonical" href="` + canonicalURL + `">` + "\n" +
		"</head>\n" +
		"<body>\n" +
		`  <script>window.location.replace("` + redirectURL + `")</script>` + "\n" +
		`  <p>Redirecting to <a href="` + redirectURL + `">` + redirectURL + `</a></p>` + "\n" +
		"</body>\n" +
		"</html>\n"

	rootIndexPath := filepath.Join(b.outputDir, "index.html")
	if err := b.handle.Write(rootIndexPath, []byte(rootIndex), effects.ModeDefault); err != nil {
		return err
	}
	b.written.add(rootIndexPath)

	// Cloudflare only ever reads the _redirects at the deployed site root,
	// where there is no document to resolve a relative target against --
	// this rule stays site-absolute. Deliberate: the standalone deployment
	// owns its origin root, and on the assembly site the worker owns
	// redirects instead of this file.
	redirectsPath := filepath.Join(b.outputDir, "_redirects")
	if err := b.handle.Write(redirectsPath,
		[]byte("/ /"+redirectURL+" 302\n"), effects.ModeDefault); err != nil {
		return err
	}
	b.written.add(redirectsPath)
	return nil
}

// feedMaxEntries is the feed's entry cap, nil when the config declares none.
func feedMaxEntries(cfg config.Config) *int {
	value, ok := cfg["feed_max_entries"].(int64)
	if !ok {
		return nil
	}
	capped := int(value)
	return &capped
}
