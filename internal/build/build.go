package build

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// Options is everything one [Build] takes.
type Options struct {
	// DirPath is the project root.
	DirPath string
	// Config is a pre-loaded config. Nil loads selfdoc.json from DirPath.
	Config config.Config
	// VersionFilter builds only the named version. Empty builds every
	// configured version.
	VersionFilter string
	// LocaleFilter builds only the named locale. Empty builds every
	// configured locale.
	LocaleFilter string
	// IncludeDrafts includes the draft posts in the output.
	IncludeDrafts bool
	// Target selects what is built: "posts" for a posts-only build, empty
	// for a full one.
	Target string
	// Theme overrides the theme the config declares, for this build only.
	// Empty means the config decides. Nothing is written back to
	// selfdoc.json -- the override lives as long as the call does.
	Theme string
	// Stdout is where the build's progress lines go. Nil writes to the
	// process's standard output.
	Stdout io.Writer
	// Siblings are the other projects published on the assembled site this
	// build's output is grafted into. Each built page ends with a section
	// linking them. Empty -- which is what a standalone build passes --
	// emits no section: a project deployed on its own has no siblings.
	Siblings []SiblingProject
	// SiteName is the name of the assembled site this build's output is
	// grafted into, which every page's document title ends with. Empty --
	// which is what a standalone build passes -- ends a title at the
	// project: a project deployed on its own has no site above it.
	SiteName string
	// Now supplies the date a page with no date and no file behind it
	// carries. The zero value takes the current time.
	Now time.Time
}

// Build builds a project's documentation site and returns the paths it wrote.
//
// Locales are the outer loop and versions the inner one. Each combination is
// built from either the working tree (the current version) or a git tag
// extracted into the version cache (a superseded one). The current version of
// every page is emitted at the stable address and every older version under
// "v/<version>/"; the pages marked "versioned: false" are emitted once at the
// version-free mount, and the site-level pages (the posts and their listing)
// once with no mount at all.
//
// After the pages come the files that belong to the site: the theme
// stylesheet and its assets, the social cards, the sitemaps, llms.txt, the
// Atom feed, the 404 page, the favicon, robots.txt, the redirect stubs, the
// Pagefind index and the compressed companions.
func Build(opts Options, h *effects.Handle) (map[string]bool, error) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	cfg := opts.Config
	if cfg == nil {
		loaded, err := config.Load(opts.DirPath)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	}
	if cfg == nil {
		return nil, errors.New(
			"No selfdoc.json found. Run 'selfdoc init' to initialize.")
	}

	if opts.Theme != "" {
		// Validated against the registry rather than trusted: a misspelled
		// name would otherwise fail deep in the render with a less useful
		// message.
		known := themes.List()
		if !contains(known, opts.Theme) {
			return nil, &config.ConfigError{Message: fmt.Sprintf(
				"unknown theme %s; available themes: %s",
				util.PythonRepr(opts.Theme), strings.Join(known, ", "))}
		}
		overridden := make(config.Config, len(cfg))
		for key, value := range cfg {
			overridden[key] = value
		}
		overridden["theme"] = opts.Theme
		cfg = overridden
	}

	if cfg["versions"] == nil {
		return nil, &config.ConfigError{Message: "selfdoc.json requires 'versions' array. " +
			`Add: "versions": [{"version": "1.0.0"}]`}
	}
	if cfg["locales"] == nil {
		return nil, &config.ConfigError{Message: "selfdoc.json requires 'locales' array. " +
			`Add: "locales": [{"code": "en", "label": "English", "default": true}]`}
	}

	locales := configList(cfg, "locales")
	versions := configList(cfg, "versions")
	if len(locales) == 0 {
		return nil, &config.ConfigError{Message: "selfdoc.json declares an empty 'locales' array. " +
			`Add: "locales": [{"code": "en", "label": "English", "default": true}]`}
	}
	if len(versions) == 0 {
		return nil, &config.ConfigError{Message: "selfdoc.json declares an empty 'versions' array. " +
			`Add: "versions": [{"version": "1.0.0"}]`}
	}
	defaultLocaleCode := util.PythonStrOrEmpty(defaultLocaleEntry(locales)["code"])
	latestVersion := util.PythonStrOrEmpty(versions[len(versions)-1]["version"])

	buildVersions := versions
	if opts.VersionFilter != "" {
		matching := filterEntries(versions, "version", opts.VersionFilter)
		if len(matching) == 0 {
			return nil, fmt.Errorf("Version '%s' not found in config. Available: %s",
				opts.VersionFilter, strings.Join(entryValues(versions, "version"), ", "))
		}
		buildVersions = matching
	}

	buildLocales := locales
	if opts.LocaleFilter != "" {
		matching := filterEntries(locales, "code", opts.LocaleFilter)
		if len(matching) == 0 {
			return nil, fmt.Errorf("Locale '%s' not found in config. Available: %s",
				opts.LocaleFilter, strings.Join(entryValues(locales, "code"), ", "))
		}
		buildLocales = matching
	}

	outputDir := filepath.Join(opts.DirPath, strings.TrimRight(configString(cfg, "output"), "/"))

	// The docs directory is verified before the output directory is
	// touched: creating the output could implicitly create the docs
	// parent, masking a missing docs/ error.
	docsDirName := strings.TrimRight(configString(cfg, "docs"), "/")
	docsDirCheck := filepath.Join(opts.DirPath, docsDirName)
	if !isDir(docsDirCheck) {
		return nil, fmt.Errorf(
			"Docs directory '%s' not found. Create it or run 'selfdoc init'.",
			configString(cfg, "docs"))
	}

	// An authored page on a path the build writes itself is refused before
	// the output directory is touched and long before injection runs.
	// Injection checks the tree it writes into; these are the locale trees
	// it never sees, whose pages the partition would still carry.
	if err := CheckReservedAuthoredPages(docsDirCheck, opts.DirPath); err != nil {
		return nil, err
	}
	for _, locale := range buildLocales {
		localeDocsDir, err := ResolveLocaleDocsDir(
			opts.DirPath, docsDirName, util.PythonStrOrEmpty(locale["code"]), locales)
		if err != nil {
			// A missing locale directory is reported by the build proper,
			// with its own message; this check has nothing to read.
			continue
		}
		if err := CheckReservedAuthoredPages(localeDocsDir, opts.DirPath); err != nil {
			return nil, err
		}
	}

	if _, err := os.Stat(outputDir); err == nil {
		if err := h.RmTree(outputDir); err != nil {
			return nil, err
		}
	}
	// The output directory is one of selfdoc's own, so it is created
	// through the layout: the repository's ownership row is what permits
	// it, and the derived ignore file is brought up to date with it.
	if err := layout.EnsureDir(h, opts.DirPath, strings.TrimRight(configString(cfg, "output"), "/")); err != nil {
		return nil, err
	}
	if err := h.MkdirAll(outputDir); err != nil {
		return nil, err
	}

	latestDocsDir := filepath.Join(opts.DirPath, docsDirName)

	if opts.Target == "posts" {
		return BuildPostsOnly(
			opts.DirPath, cfg, outputDir, docsDirName, latestDocsDir,
			opts.IncludeDrafts, opts.Siblings, opts.SiteName, h)
	}

	// The posts are injected into the docs tree so the normal pipeline
	// discovers them. They are site-level pages: built once, with no mount,
	// from the tree they were just written into.
	injectedPostFiles, err := InjectPostsIntoDocs(
		opts.DirPath, cfg, latestDocsDir, opts.IncludeDrafts, h)
	if err != nil {
		return nil, err
	}
	sitePages := map[string]bool{}
	for _, injected := range injectedPostFiles {
		rel, relErr := filepath.Rel(latestDocsDir, injected)
		if relErr != nil {
			return nil, relErr
		}
		sitePages[filepath.ToSlash(rel)] = true
	}

	// The partition is per locale. Page paths are relative to the locale's
	// own docs directory, so a single partition of the top-level docs/ tree
	// would yield locale-prefixed paths that no per-locale build can match
	// -- which silently filtered every page out of a localized build.
	partitions := map[string]Partition{}
	for _, locale := range buildLocales {
		localeCode := util.PythonStrOrEmpty(locale["code"])
		localeDocsDir, resolveErr := ResolveLocaleDocsDir(opts.DirPath, docsDirName, localeCode, locales)
		if resolveErr != nil {
			cleanupInjected(injectedPostFiles, latestDocsDir, h)
			return nil, resolveErr
		}
		localeConfig := withDocsDir(cfg, opts.DirPath, localeDocsDir)
		partition, partitionErr := PartitionPages(localeConfig, localeDocsDir, opts.DirPath, h)
		if partitionErr != nil {
			cleanupInjected(injectedPostFiles, latestDocsDir, h)
			return nil, partitionErr
		}
		// The build owns two top-level segments and an authored page may
		// not take either. The post pages are exempt: the build put them
		// there.
		authored := map[string]bool{}
		for mdPath := range partition.Versioned {
			authored[mdPath] = true
		}
		for mdPath := range partition.Unversioned {
			authored[mdPath] = true
		}
		if err := CheckReservedPagePaths(authored); err != nil {
			cleanupInjected(injectedPostFiles, latestDocsDir, h)
			return nil, err
		}
		partitions[localeCode] = partition
	}

	written, bodyErr := buildBody(bodyInputs{
		dirPath:           opts.DirPath,
		config:            cfg,
		locales:           locales,
		versions:          versions,
		defaultLocaleCode: defaultLocaleCode,
		latestVersion:     latestVersion,
		buildVersions:     buildVersions,
		buildLocales:      buildLocales,
		outputDir:         outputDir,
		docsDirName:       docsDirName,
		partitions:        partitions,
		sitePages:         sitePages,
		siblings:          opts.Siblings,
		siteName:          opts.SiteName,
		now:               opts.Now,
		stdout:            stdout,
	}, h)
	cleanupInjected(injectedPostFiles, latestDocsDir, h)
	if bodyErr != nil {
		return written, bodyErr
	}
	return written, nil
}

// cleanupInjected removes the injected post pages, swallowing a cleanup
// failure so it cannot mask the build's own error.
func cleanupInjected(injected []string, docsDir string, h *effects.Handle) {
	_ = CleanupInjectedPosts(injected, docsDir, h)
}

// bodyInputs is what [buildBody] needs from [Build], which has already
// resolved the config, the filters and the partitions.
type bodyInputs struct {
	dirPath           string
	config            config.Config
	locales           []map[string]any
	versions          []map[string]any
	defaultLocaleCode string
	latestVersion     string
	buildVersions     []map[string]any
	buildLocales      []map[string]any
	outputDir         string
	docsDirName       string
	// partitions maps a locale code to that locale's page partition,
	// because page paths are relative to the locale's own docs directory.
	partitions map[string]Partition
	sitePages  map[string]bool
	siblings   []SiblingProject
	siteName   string
	now        time.Time
	stdout     io.Writer
}

// latestBuild is the pass whose data the site-level files are built from: the
// current version in the default locale, with the unversioned and site-level
// pages folded in.
type latestBuild struct {
	markdownFiles     []page.SourceFile
	pageAddresses     map[string]address.PageAddress
	frontmatter       map[string]util.Frontmatter
	pageDates         map[string]page.PageDates
	projectName       string
	version           string
	docsDir           string
	hasCustomCSS      bool
	rawThemeCSS       string
	themeMeta         *themes.Metadata
	criticalCSS       string
	configDescription string
	baseURL           string
	urlBuilder        urls.URLBuilder
	feedURL           string
	lang              string
}

// buildBody is the core build: every page of every mount, then everything
// that belongs to the site rather than to a page.
//
// It is separate from [Build] so the post pages injected into the docs tree
// are cleaned up whether it succeeds or fails.
func buildBody(in bodyInputs, h *effects.Handle) (map[string]bool, error) {
	written := map[string]bool{}
	contentPages := 0
	var latest *latestBuild
	var last *latestBuild
	// The stable output keys per locale, for the sitemaps. Archived
	// versions are excluded: they canonicalize to the stable address, so
	// listing them would ask a crawler to index a page that points away
	// from itself.
	perLocaleStableHTML := map[string][]string{}
	var localeOrder []string

	// Every version's content is extracted once, up front, and which pages
	// each version holds is learned from it -- the version picker is
	// rendered inside each page and offers a version only when that version
	// has the page.
	buildDirs := map[string]string{}
	for _, verEntry := range in.buildVersions {
		verStr := util.PythonStrOrEmpty(verEntry["version"])
		if verStr == in.latestVersion {
			buildDirs[verStr] = in.dirPath
			continue
		}
		cacheDir, err := ExtractVersionContent(verStr, in.config, in.dirPath, h)
		if err != nil {
			return written, err
		}
		buildDirs[verStr] = cacheDir
	}
	versionPages := map[string]map[string]map[string]bool{}
	for _, locale := range in.buildLocales {
		localeCode := util.PythonStrOrEmpty(locale["code"])
		versionPages[localeCode] = map[string]map[string]bool{}
		for _, verEntry := range in.buildVersions {
			verStr := util.PythonStrOrEmpty(verEntry["version"])
			paths, err := VersionedHTMLPaths(
				buildDirs[verStr], in.docsDirName, localeCode, in.locales, in.config)
			if err != nil {
				return written, err
			}
			versionPages[localeCode][verStr] = paths
		}
	}

	for _, locale := range in.buildLocales {
		localeCode := util.PythonStrOrEmpty(locale["code"])
		mountLocale := address.LocaleSegment(localeCode, in.locales)
		perLocaleStableHTML[localeCode] = nil
		localeOrder = append(localeOrder, localeCode)
		partition := in.partitions[localeCode]

		for _, verEntry := range in.buildVersions {
			verStr := util.PythonStrOrEmpty(verEntry["version"])
			isLatest := verStr == in.latestVersion
			archived := !isLatest
			mountAddr, err := address.NewPageAddress("index.html", address.Coordinates{
				Locale: mountLocale, Version: verStr, Archived: archived,
			})
			if err != nil {
				return written, err
			}
			outputSubdir := mountAddr.Mount

			buildDir := buildDirs[verStr]
			localeDocsDir, err := ResolveLocaleDocsDir(buildDir, in.docsDirName, localeCode, in.locales)
			if err != nil {
				return written, err
			}
			localeConfig := withDocsDir(in.config, buildDir, localeDocsDir)

			singleOpts := NewSingleOptions()
			singleOpts.DirPath = buildDir
			singleOpts.Config = localeConfig
			singleOpts.MountLocale = &mountLocale
			singleOpts.MountVersion = &verStr
			singleOpts.MountArchived = archived
			singleOpts.VersionOverride = &verStr
			singleOpts.LocaleOverride = &localeCode
			singleOpts.Siblings = in.siblings
			singleOpts.SiteName = in.siteName
			singleOpts.AvailableVersions = in.versions
			singleOpts.AvailableLocales = in.locales
			singleOpts.VersionPages = versionPages[localeCode]
			singleOpts.CurrentLocale = localeCode
			singleOpts.Now = in.now
			if len(partition.Unversioned) > 0 {
				singleOpts.PageFilter = partition.Versioned
				singleOpts.UnversionedMarkdown = partition.UnversionedMarkdown
				singleOpts.UnversionedFrontmatter = partition.UnversionedFrontmatter
			}

			result, err := BuildSingle(singleOpts, h)
			if err != nil {
				return written, err
			}

			pageCount, err := writePages(result.HTMLFiles, in.outputDir, written, h)
			if err != nil {
				return written, err
			}
			contentPages += pageCount

			if err := copyOtherFiles(
				result.OtherFiles, result.DocsDir,
				filepath.Join(in.outputDir, filepath.FromSlash(outputSubdir)), written, h,
			); err != nil {
				return written, err
			}

			// Only the current version is listed in a sitemap; an archive
			// is canonicalized away.
			if isLatest {
				for _, relPath := range sortedKeys(result.HTMLFiles) {
					if strings.HasSuffix(relPath, ".html") &&
						!contains(perLocaleStableHTML[localeCode], relPath) {
						perLocaleStableHTML[localeCode] = append(
							perLocaleStableHTML[localeCode], relPath)
					}
				}
			}

			snapshot := &latestBuild{
				markdownFiles: result.MarkdownFiles,
				pageAddresses: addressesFor(result.MarkdownFiles, address.Coordinates{
					Locale: mountLocale, Version: verStr,
				}),
				frontmatter:       result.Frontmatter,
				pageDates:         result.PageDates,
				projectName:       result.ProjectName,
				version:           result.Version,
				docsDir:           result.DocsDir,
				hasCustomCSS:      result.HasCustomCSS,
				rawThemeCSS:       result.RawThemeCSS,
				themeMeta:         result.ThemeMeta,
				criticalCSS:       result.CriticalCSS,
				configDescription: result.ConfigDescription,
				baseURL:           result.BaseURL,
				urlBuilder:        MakeURLBuilder(in.config),
				feedURL:           result.FeedURL,
				lang:              result.Lang,
			}
			last = snapshot
			if isLatest && localeCode == in.defaultLocaleCode {
				latest = snapshot
			}
		}
	}

	// When a version filter excluded the current version, the last pass
	// built is what the site-level files are built from.
	if latest == nil {
		latest = last
	}
	if latest == nil {
		return written, errors.New(
			"Build produced no pass at all: no locale and version combination was built.")
	}

	// The unversioned pages, once per locale, with no version.
	var unversionedLatest *latestBuild
	for _, locale := range in.buildLocales {
		localeCode := util.PythonStrOrEmpty(locale["code"])
		mountLocale := address.LocaleSegment(localeCode, in.locales)
		partition := in.partitions[localeCode]
		if len(partition.Unversioned) == 0 {
			continue
		}
		// Unversioned pages sit at the stable mount, beside the current
		// version's pages.
		uvMountAddr, err := address.NewPageAddress("index.html", address.Coordinates{Locale: mountLocale})
		if err != nil {
			return written, err
		}

		localeDocsDir, err := ResolveLocaleDocsDir(in.dirPath, in.docsDirName, localeCode, in.locales)
		if err != nil {
			return written, err
		}
		localeConfig := withDocsDir(in.config, in.dirPath, localeDocsDir)

		empty := ""
		uvOpts := NewSingleOptions()
		uvOpts.DirPath = in.dirPath
		uvOpts.Config = localeConfig
		uvOpts.MountLocale = &mountLocale
		uvOpts.MountVersion = &empty
		uvOpts.VersionOverride = &empty
		uvOpts.LocaleOverride = &localeCode
		uvOpts.Siblings = in.siblings
		uvOpts.SiteName = in.siteName
		uvOpts.AvailableVersions = in.versions
		uvOpts.AvailableLocales = in.locales
		uvOpts.VersionPages = versionPages[localeCode]
		uvOpts.CurrentLocale = localeCode
		uvOpts.PageFilter = partition.Unversioned
		uvOpts.Now = in.now

		uvResult, err := BuildSingle(uvOpts, h)
		if err != nil {
			return written, err
		}

		pageCount, err := writePages(uvResult.HTMLFiles, in.outputDir, written, h)
		if err != nil {
			return written, err
		}
		contentPages += pageCount

		if err := copyOtherFiles(
			uvResult.OtherFiles, uvResult.DocsDir,
			filepath.Join(in.outputDir, filepath.FromSlash(uvMountAddr.Mount)), written, h,
		); err != nil {
			return written, err
		}

		// Unversioned pages are stable addresses: they belong in the
		// sitemap.
		for _, relPath := range sortedKeys(uvResult.HTMLFiles) {
			if strings.HasSuffix(relPath, ".html") &&
				!contains(perLocaleStableHTML[localeCode], relPath) {
				perLocaleStableHTML[localeCode] = append(perLocaleStableHTML[localeCode], relPath)
			}
		}

		if localeCode == in.defaultLocaleCode {
			unversionedLatest = &latestBuild{
				markdownFiles: uvResult.MarkdownFiles,
				frontmatter:   uvResult.Frontmatter,
				pageDates:     uvResult.PageDates,
				pageAddresses: addressesFor(uvResult.MarkdownFiles, address.Coordinates{Locale: mountLocale}),
			}
		}
	}

	// The site-level pages (the posts), once, with no mount at all. They
	// are read from the top-level docs tree, which is where they were
	// injected, so a localized project's posts are not dropped on the floor
	// by a per-locale partition that never sees them.
	var siteLatest *latestBuild
	if len(in.sitePages) > 0 {
		siteOpts := SiteLevelBuildArgs(in.config, in.docsDirName)
		siteOpts.DirPath = in.dirPath
		siteOpts.PageFilter = in.sitePages
		siteOpts.Now = in.now
		siteOpts.SiteName = in.siteName
		siteResult, err := BuildSingle(siteOpts, h)
		if err != nil {
			return written, err
		}
		for _, relPath := range sortedKeys(siteResult.HTMLFiles) {
			outPath := filepath.Join(in.outputDir, filepath.FromSlash(relPath))
			if err := h.MkdirAll(filepath.Dir(outPath)); err != nil {
				return written, err
			}
			if err := h.Write(outPath,
				[]byte(MinifyHTML(siteResult.HTMLFiles[relPath])), effects.ModeDefault); err != nil {
				return written, err
			}
			written[outPath] = true
			contentPages++
			if !contains(perLocaleStableHTML[in.defaultLocaleCode], relPath) {
				perLocaleStableHTML[in.defaultLocaleCode] = append(
					perLocaleStableHTML[in.defaultLocaleCode], relPath)
			}
		}
		siteLatest = &latestBuild{
			markdownFiles: siteResult.MarkdownFiles,
			frontmatter:   siteResult.Frontmatter,
			pageDates:     siteResult.PageDates,
			pageAddresses: addressesFor(siteResult.MarkdownFiles, address.Coordinates{}),
		}
	}

	// A build that wrote no page at all is a build whose page filters
	// matched nothing. It still writes assets, a sitemap and redirect
	// stubs, so nothing downstream notices -- which is how a localized
	// project with an unversioned page shipped an empty site.
	if contentPages == 0 {
		return written, fmt.Errorf(
			"Build produced no content pages: every page filter matched nothing. "+
				"Check that the pages under '%s' resolve for the configured locales.",
			configString(in.config, "docs"))
	}

	// The unversioned and site-level pages are folded into the pass the
	// site-level files are built from. Those pages mount elsewhere, so
	// their addresses come from their own pass, not from the versioned one.
	for _, extra := range []*latestBuild{unversionedLatest, siteLatest} {
		if extra == nil {
			continue
		}
		latest.markdownFiles = mergeSources(latest.markdownFiles, extra.markdownFiles)
		latest.frontmatter = mergeFrontmatter(latest.frontmatter, extra.frontmatter)
		latest.pageDates = mergePageDates(latest.pageDates, extra.pageDates)
		latest.pageAddresses = mergeAddresses(latest.pageAddresses, extra.pageAddresses)
	}

	if err := writeSharedAssets(in, latest, written, h); err != nil {
		return written, err
	}

	if err := writeSiteLevelFiles(in, latest, perLocaleStableHTML, localeOrder, written, h); err != nil {
		return written, err
	}

	redirectsPath, err := writeRedirectStubs(in, latest, written, h)
	if err != nil {
		return written, err
	}

	if err := writeConfigRedirects(in, latest, redirectsPath, written, h); err != nil {
		return written, err
	}

	// Pagefind indexes the pages this build just wrote and emits both the
	// index and the UI bundle every page references into pagefind/ at the
	// output root. It runs after the last HTML file -- the 404 page and the
	// redirect stubs included -- and before compression, so the index
	// covers the whole site and its own assets get compressed with it.
	if err := RunPagefind(in.outputDir, h); err != nil {
		return written, err
	}
	if _, err := PruneUnreferencedPagefindWidget(in.outputDir, h); err != nil {
		return written, err
	}

	compressCount, err := CompressOutput(in.outputDir, h)
	if err != nil {
		return written, err
	}
	fmt.Fprintf(in.stdout, "Pre-compressed %d files (gzip + brotli)\n", compressCount)

	fmt.Fprintln(in.stdout, "OG cards: basic")

	return written, nil
}

// writeSharedAssets writes the theme stylesheet, the files that have to travel
// with it, and the project's own custom.css.
func writeSharedAssets(in bodyInputs, latest *latestBuild, written map[string]bool, h *effects.Handle) error {
	themeMeta := latest.themeMeta
	// A framework theme writes into css/ with the framework's fonts beside
	// it, because the framework's @font-face rules address ../fonts/.
	themeName := ""
	if themeMeta != nil {
		themeName = themeMeta.Name
	}
	if themeName == "" {
		themeName = configString(in.config, "theme")
	}
	if themeName == "" {
		themeName = "minimal"
	}
	cssRel, err := themes.CSSRel(themeName)
	if err != nil {
		return err
	}
	cssPath := filepath.Join(in.outputDir, filepath.FromSlash(cssRel))
	if err := h.MkdirAll(filepath.Dir(cssPath)); err != nil {
		return err
	}

	themeCSS := latest.rawThemeCSS
	light, dark := "default", "monokai"
	if themeMeta != nil {
		light, dark = themeMeta.PygmentsLight, themeMeta.PygmentsDark
	}
	pygmentsCSS, err := html.GeneratePygmentsCSS(light, dark)
	if err != nil {
		return err
	}
	if pygmentsCSS != "" {
		themeCSS = themeCSS + "\n\n/* Pygments syntax highlighting */\n" + pygmentsCSS
	}
	if err := h.Write(cssPath, []byte(MinifyCSS(themeCSS)), effects.ModeDefault); err != nil {
		return err
	}
	written[cssPath] = true

	// A standalone site carries its own copy of whatever the theme's
	// stylesheet references and is not itself -- today, the faces. There is
	// no assembly under a standalone deploy to serve them.
	assets, err := themes.Assets(themeName)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		dst := filepath.Join(in.outputDir, filepath.FromSlash(asset.Dest))
		if err := h.MkdirAll(filepath.Dir(dst)); err != nil {
			return err
		}
		content, readErr := asset.Bytes()
		if readErr != nil {
			return readErr
		}
		if err := h.Write(dst, content, effects.ModeDefault); err != nil {
			return err
		}
		written[dst] = true
	}

	if latest.hasCustomCSS {
		customCSSDst := filepath.Join(in.outputDir, "custom.css")
		if err := h.CopyFile(filepath.Join(latest.docsDir, "custom.css"), customCSSDst); err != nil {
			return err
		}
		written[customCSSDst] = true
	}
	return nil
}

// writeSiteLevelFiles writes the auxiliary files, the per-locale sitemaps and
// the sitemap index.
func writeSiteLevelFiles(
	in bodyInputs, latest *latestBuild,
	perLocaleStableHTML map[string][]string, localeOrder []string,
	written map[string]bool, h *effects.Handle,
) error {
	if latest.urlBuilder == nil {
		return &config.ConfigError{Message: "selfdoc.json declares neither 'base_url' nor a " +
			"'topology' block with both 'docs_base' and 'slug'; the site-level " +
			"files emit absolute URLs and there is nothing to build them from."}
	}

	// The sitemap lists stable addresses across every locale, and nothing
	// else.
	var allStableHTMLPaths []string
	for _, localeCode := range localeOrder {
		allStableHTMLPaths = append(allStableHTMLPaths, perLocaleStableHTML[localeCode]...)
	}

	accentColor := "#0969da"
	if latest.themeMeta != nil {
		accentColor = latest.themeMeta.AccentColor
	}
	deploy, _ := in.config["deploy"].(map[string]any)

	auxWritten, err := GenerateAuxiliaryFiles(AuxiliaryOptions{
		OutputDir:       in.outputDir,
		ProjectName:     latest.projectName,
		Version:         latest.version,
		MarkdownFiles:   latest.markdownFiles,
		HTMLPaths:       allStableHTMLPaths,
		BaseURL:         latest.baseURL,
		HasCustomCSS:    latest.hasCustomCSS,
		Repo:            util.PythonStrOrEmpty(in.config["repo"]),
		URLBuilder:      latest.urlBuilder,
		Lang:            latest.lang,
		PageDates:       latest.pageDates,
		Frontmatter:     latest.frontmatter,
		Description:     latest.configDescription,
		FeedURL:         latest.feedURL,
		CriticalCSS:     latest.criticalCSS,
		AccentColor:     accentColor,
		ThemeMeta:       latest.themeMeta,
		Deploy:          deploy,
		FeedMaxEntries:  feedMaxEntries(in.config),
		HasSitemapIndex: len(in.buildLocales) > 1,
		// 404.html sits at the output root; its sidebar links into the
		// stable mount, where every current page lives.
		MountLocale:   address.LocaleSegment(in.defaultLocaleCode, in.locales),
		PageAddresses: latest.pageAddresses,
	}, h)
	for path := range auxWritten {
		written[path] = true
	}
	if err != nil {
		return err
	}

	if len(in.buildLocales) > 1 {
		sitemapPaths, err := GeneratePerLocaleSitemaps(
			in.outputDir, localeOrder, perLocaleStableHTML,
			latest.urlBuilder, latest.pageDates, h)
		for _, path := range sitemapPaths {
			written[path] = true
		}
		if err != nil {
			return err
		}
		indexPath, err := GenerateSitemapIndex(in.outputDir, localeOrder, latest.urlBuilder, h)
		if err != nil {
			return err
		}
		written[indexPath] = true
	}
	return nil
}

// writeRedirectStubs writes the root redirect into the stable mount and the
// Cloudflare _redirects file, and returns the path of the latter.
//
// A single-locale standalone site mounts its current version at the output
// root, so its home page IS the root index -- there is nothing to redirect to,
// and writing a stub would overwrite the page.
func writeRedirectStubs(
	in bodyInputs, latest *latestBuild, written map[string]bool, h *effects.Handle,
) (string, error) {
	stableHome, err := address.NewPageAddress("index.html", address.Coordinates{
		Locale: address.LocaleSegment(in.defaultLocaleCode, in.locales),
	})
	if err != nil {
		return "", err
	}
	// Document-relative, with no leading slash. The build output is not
	// always served from an origin root: an assembly serves it under
	// /<slug>/ and GitHub Pages project sites under /<repo>/. A
	// root-relative hop escapes that subtree; a document-relative one
	// resolves correctly in every case, origin root included. The stub sits
	// at the output root, so the pinned address is already the hop.
	redirectURL := stableHome.Stable
	writeRootStub := redirectURL != ""

	if writeRootStub {
		// The canonical must be absolute: a root-relative one resolves
		// against whatever host served the stub, so every alias of the
		// site would claim to be canonical.
		canonicalURL := latest.urlBuilder.PageURL(redirectURL)
		rootIndexPath := filepath.Join(in.outputDir, "index.html")
		if err := h.Write(rootIndexPath,
			[]byte(redirectStubHTML(redirectURL, canonicalURL)), effects.ModeDefault); err != nil {
			return "", err
		}
		written[rootIndexPath] = true
	}

	// Cloudflare only ever reads the _redirects at the deployed site root,
	// where there is no document to resolve a relative target against --
	// these rules stay site-absolute. Deliberate: a standalone deployment
	// owns its origin root, and on the unified site the assembly worker
	// owns redirects instead of this file.
	redirectsPath := filepath.Join(in.outputDir, "_redirects")
	redirectsContent := ""
	if writeRootStub {
		redirectsContent = "/ /" + redirectURL + " 302\n"
	}
	if err := h.Write(redirectsPath, []byte(redirectsContent), effects.ModeDefault); err != nil {
		return "", err
	}
	written[redirectsPath] = true
	return redirectsPath, nil
}

// redirectStubHTML renders a meta-refresh stub pointing at targetURL, with
// canonicalURL as its canonical link.
func redirectStubHTML(targetURL, canonicalURL string) string {
	return "<!DOCTYPE html>\n" +
		"<html>\n" +
		"<head>\n" +
		`  <meta http-equiv="refresh" content="0;url=` + targetURL + `">` + "\n" +
		`  <link rel="canonical" href="` + canonicalURL + `">` + "\n" +
		"</head>\n" +
		"<body>\n" +
		`  <script>window.location.replace("` + targetURL + `")</script>` + "\n" +
		`  <p>Redirecting to <a href="` + targetURL + `">` + targetURL + `</a></p>` + "\n" +
		"</body>\n" +
		"</html>\n"
}

// writeConfigRedirects writes the config-declared page redirects, expanded
// across every locale and version combination.
func writeConfigRedirects(
	in bodyInputs, latest *latestBuild, redirectsPath string,
	written map[string]bool, h *effects.Handle,
) error {
	configRedirects := configList(in.config, "redirects")
	if len(configRedirects) == 0 {
		return nil
	}
	redirectCount := 0
	for _, entry := range configRedirects {
		fromSlug := util.PythonStrOrEmpty(entry["from"])
		toSlug := util.PythonStrOrEmpty(entry["to"])
		for _, locale := range in.buildLocales {
			mountLocale := address.LocaleSegment(util.PythonStrOrEmpty(locale["code"]), in.locales)
			for _, verEntry := range in.buildVersions {
				verStr := util.PythonStrOrEmpty(verEntry["version"])
				archived := verStr != in.latestVersion
				// Both ends of the redirect are pages of this mount, so
				// both come from the addressing authority.
				coords := address.Coordinates{
					Locale: mountLocale, Version: verStr, Archived: archived,
				}
				fromAddr, err := address.NewPageAddress(fromSlug+"/index.html", coords)
				if err != nil {
					return err
				}
				toAddr, err := address.NewPageAddress(toSlug+"/index.html", coords)
				if err != nil {
					return err
				}
				oldPath := filepath.Join(in.outputDir, filepath.FromSlash(fromAddr.OutputKey))
				// A page that already exists -- a cached old-version page
				// -- is not replaced by a stub.
				if _, statErr := os.Stat(oldPath); statErr == nil {
					continue
				}
				// The target path within the site, and the
				// document-relative hop from the stub's own directory to
				// it (see the root stub above for why a root-relative hop
				// is wrong).
				targetPath := toAddr.URL()
				stubDir := path.Dir(fromAddr.OutputKey)
				targetURL := posixRelpath(targetPath, stubDir) + "/"
				// Absolute canonical (see the root stub above).
				targetCanonical := latest.urlBuilder.PageURL(targetPath)

				if err := h.MkdirAll(filepath.Dir(oldPath)); err != nil {
					return err
				}
				if err := h.Write(oldPath,
					[]byte(redirectStubHTML(targetURL, targetCanonical)), effects.ModeDefault); err != nil {
					return err
				}
				written[oldPath] = true

				appender, err := h.OpenAppend(redirectsPath)
				if err != nil {
					return err
				}
				if _, err := io.WriteString(appender,
					"/"+fromAddr.URL()+" /"+targetPath+" 301\n"); err != nil {
					appender.Close()
					return err
				}
				if err := appender.Close(); err != nil {
					return err
				}
				redirectCount++
			}
		}
	}
	if redirectCount > 0 {
		fmt.Fprintf(in.stdout, "Generated %d redirect(s)\n", redirectCount)
	}
	return nil
}

// writePages writes one pass's HTML documents, minified, and returns how many
// it wrote. The keys are written in sorted order, so a preview names them the
// same way on every run.
func writePages(
	htmlFiles map[string]string, outputDir string, written map[string]bool, h *effects.Handle,
) (int, error) {
	count := 0
	for _, relPath := range sortedKeys(htmlFiles) {
		outPath := filepath.Join(outputDir, filepath.FromSlash(relPath))
		if err := h.MkdirAll(filepath.Dir(outPath)); err != nil {
			return count, err
		}
		if err := h.Write(outPath, []byte(MinifyHTML(htmlFiles[relPath])), effects.ModeDefault); err != nil {
			return count, err
		}
		written[outPath] = true
		count++
	}
	return count, nil
}

// copyOtherFiles copies one pass's non-Markdown assets into its mount.
func copyOtherFiles(
	otherFiles []string, docsDir, destDir string, written map[string]bool, h *effects.Handle,
) error {
	for _, relPath := range otherFiles {
		src := filepath.Join(docsDir, filepath.FromSlash(relPath))
		dst := filepath.Join(destDir, filepath.FromSlash(relPath))
		if err := h.MkdirAll(filepath.Dir(dst)); err != nil {
			return err
		}
		if err := h.CopyFile(src, dst); err != nil {
			return err
		}
		written[dst] = true
	}
	return nil
}

// addressesFor maps every source's Markdown path to its address under the
// given coordinates.
func addressesFor(sources []page.SourceFile, coords address.Coordinates) map[string]address.PageAddress {
	addresses := make(map[string]address.PageAddress, len(sources))
	for _, src := range sources {
		addr, err := address.NewPageAddress(html.MdToHTMLPath(src.MdPath), coords)
		if err != nil {
			// A page whose path the addressing authority refuses never
			// reached this far: the reserved-path checks ran before the
			// build did.
			continue
		}
		addresses[src.MdPath] = addr
	}
	return addresses
}

// mergeAddresses overlays extra's entries onto a copy of base.
func mergeAddresses(base, extra map[string]address.PageAddress) map[string]address.PageAddress {
	merged := make(map[string]address.PageAddress, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

// withDocsDir copies a config with its "docs" key repointed at localeDocsDir,
// expressed relative to baseDir the way the config's own key is.
func withDocsDir(cfg config.Config, baseDir, localeDocsDir string) config.Config {
	copied := make(config.Config, len(cfg))
	for key, value := range cfg {
		copied[key] = value
	}
	rel, err := filepath.Rel(baseDir, localeDocsDir)
	if err != nil {
		rel = localeDocsDir
	}
	copied["docs"] = filepath.ToSlash(rel)
	return copied
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

// filterEntries keeps the config entries whose key carries the given value.
func filterEntries(entries []map[string]any, key, value string) []map[string]any {
	var matching []map[string]any
	for _, entry := range entries {
		if util.PythonStrOrEmpty(entry[key]) == value {
			matching = append(matching, entry)
		}
	}
	return matching
}

// entryValues lists one key's value across config entries, for a diagnostic
// that names what was available.
func entryValues(entries []map[string]any, key string) []string {
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		values = append(values, util.PythonStrOrEmpty(entry[key]))
	}
	return values
}

// contains reports whether a slice holds a value.
func contains[T comparable](items []T, want T) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// posixRelpath is the relative path from base to target in slash notation --
// Python's posixpath.relpath, which every generated redirect hop was written
// against.
func posixRelpath(target, base string) string {
	if base == "" || base == "." {
		if target == "" {
			return "."
		}
		return target
	}
	targetParts := splitSlash(target)
	baseParts := splitSlash(base)
	common := 0
	for common < len(targetParts) && common < len(baseParts) && targetParts[common] == baseParts[common] {
		common++
	}
	parts := make([]string, 0, len(baseParts)-common+len(targetParts)-common)
	for i := common; i < len(baseParts); i++ {
		parts = append(parts, "..")
	}
	parts = append(parts, targetParts[common:]...)
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

// splitSlash splits a slash-separated path into its non-empty components.
func splitSlash(p string) []string {
	var parts []string
	for _, part := range strings.Split(p, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}
