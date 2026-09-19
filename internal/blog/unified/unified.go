package unified

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// injectedPosts is one docs tree's injected post pages, remembered so they can
// be removed again whatever the build does.
type injectedPosts struct {
	files   []string
	docsDir string
}

// sitePageOwner is one project's site-level pages and what they are built
// from: the posts of every project land in the one "blog/" tree the site
// shares, so the pages are collected per owner and built once per owner.
type sitePageOwner struct {
	dirPath string
	config  config.Config
	pages   map[string]bool
}

// BuildUnified builds a unified documentation site from several constituent
// projects and returns the paths it wrote.
//
// dirPath is the docs-site's own project root. cfg is a pre-loaded config; nil
// loads selfdoc.json from dirPath. theme overrides the theme the config
// declares, for this build only -- empty means the config decides.
// includeDrafts includes the draft posts of every project.
//
// The docs-site's "unified" block names the constituents; its "versions" and
// "locales" arrays drive the passes. Each constituent is built from its own
// selfdoc.json under its own slug, the docs-site's own pages under "common",
// and every project's posts once into the shared site-level tree. Posts are
// injected into each project's docs tree before the build and removed
// afterwards whether the build succeeded or failed.
func BuildUnified(
	dirPath string, cfg config.Config, theme string, includeDrafts bool, h *effects.Handle,
) (written map[string]bool, err error) {
	if cfg == nil {
		loaded, err := config.Load(dirPath)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	}
	if cfg == nil {
		return nil, errors.New("No selfdoc.json found. Run 'selfdoc init' to initialize.")
	}

	if theme != "" {
		// Validated against the registry rather than trusted: a misspelled
		// name would otherwise fail deep in the render with a less useful
		// message.
		known := themes.List()
		if !slices.Contains(known, theme) {
			return nil, &config.ConfigError{Message: fmt.Sprintf(
				"unknown theme %s; available themes: %s",
				util.PythonRepr(theme), strings.Join(known, ", "))}
		}
		overridden := make(config.Config, len(cfg))
		for key, value := range cfg {
			overridden[key] = value
		}
		overridden["theme"] = theme
		cfg = overridden
	}

	if cfg["unified"] == nil {
		return nil, &config.ConfigError{Message: "No 'unified' section in selfdoc.json"}
	}
	unifiedConfig, _ := cfg["unified"].(map[string]any)
	if cfg["versions"] == nil {
		return nil, &config.ConfigError{
			Message: "selfdoc.json requires 'versions' array for unified builds."}
	}
	if cfg["locales"] == nil {
		return nil, &config.ConfigError{
			Message: "selfdoc.json requires 'locales' array for unified builds."}
	}

	if err := validateRlsblWorkspace(dirPath, unifiedConfig); err != nil {
		return nil, err
	}

	locales := configList(cfg, "locales")
	versions := configList(cfg, "versions")
	// An array declared empty names no locale and no version to build, so
	// there is no default locale and no latest version to read.
	if len(locales) == 0 {
		return nil, &config.ConfigError{
			Message: "selfdoc.json declares an empty 'locales' array for a unified build."}
	}
	if len(versions) == 0 {
		return nil, &config.ConfigError{
			Message: "selfdoc.json declares an empty 'versions' array for a unified build."}
	}
	defaultLocaleCode := build.DefaultLocaleOf(cfg)
	latestVersion := util.PythonStrOrEmpty(versions[len(versions)-1]["version"])

	outputRel := strings.TrimRight(configString(cfg, "output"), "/")
	outputDir := filepath.Join(dirPath, outputRel)
	docsDirName := strings.TrimRight(configString(cfg, "docs"), "/")
	docsDir := filepath.Join(dirPath, docsDirName)
	if !isDir(docsDir) {
		return nil, fmt.Errorf(
			"Docs directory '%s' not found. Create it or run 'selfdoc init'.", docsDirName)
	}

	if _, err := os.Stat(outputDir); err == nil {
		if err := h.RmTree(outputDir); err != nil {
			return nil, err
		}
	}
	// Creating anything under the layout root goes through EnsureDir: it
	// reads the directory's ownership manifest before writing and refreshes
	// the derived ignore file after.
	if err := layout.EnsureDir(h, dirPath, outputRel); err != nil {
		return nil, err
	}

	// The theme is the docs-site's: one stylesheet is served to every mount.
	themeName := configString(cfg, "theme")
	if themeName == "" {
		themeName = "minimal"
	}
	rawThemeCSS, err := html.GetCSS(themeName)
	if err != nil {
		return nil, err
	}
	themeMeta, err := themes.Meta(themeName)
	if err != nil {
		return nil, err
	}
	criticalCSS, _ := build.ExtractCriticalCSS(rawThemeCSS)
	criticalCSS = build.MinifyCSS(criticalCSS)

	var allInjected []injectedPosts

	// Every page injection wrote is removed whatever happens next. The
	// setup below injects one constituent at a time and can fail on a
	// later one -- an unreadable config, an unresolvable path -- so a
	// cleanup that only wrapped the build body would leave the earlier
	// constituents' generated docs/blog/*.md and docs/blog.md behind in
	// their source trees. A cleanup failure is reported only when the
	// build itself had nothing to say: its own error is the informative
	// one.
	defer func() {
		for _, entry := range allInjected {
			cleanupErr := build.CleanupInjectedPosts(entry.files, entry.docsDir, h)
			if cleanupErr != nil && err == nil {
				err = cleanupErr
			}
		}
	}()

	// The pages of every constituent, partitioned by how they mount.
	projectPagePartitions := map[string]build.Partition{}
	projectSitePages := map[string]sitePageOwner{}
	for _, projectEntry := range unifiedProjects(unifiedConfig) {
		slug := ProjectSlug(projectEntry)
		projectPath, err := ResolveProjectPath(projectEntry, dirPath)
		if err != nil {
			return nil, err
		}
		projConfig, err := config.Load(projectPath)
		if err != nil {
			return nil, err
		}
		if projConfig == nil {
			continue
		}
		projDocsDir := filepath.Join(
			projectPath, strings.TrimRight(configString(projConfig, "docs"), "/"))
		injected, err := build.InjectPostsIntoDocs(
			projectPath, projConfig, projDocsDir, includeDrafts, h)
		if err != nil {
			return nil, err
		}
		if len(injected) > 0 {
			allInjected = append(allInjected, injectedPosts{files: injected, docsDir: projDocsDir})
		}
		partition, err := build.PartitionPages(projConfig, projDocsDir, projectPath, h)
		if err != nil {
			return nil, err
		}
		projectPagePartitions[slug] = partition
		// Posts are site-level on the unified site too: every project's
		// posts land in the one "blog/" tree, so their slugs have to be
		// unique across projects, not just within one.
		projectSitePages[slug] = sitePageOwner{
			dirPath: projectPath,
			config:  projConfig,
			pages:   partition.Site,
		}
	}

	// The docs-site's own pages are a mount like any other.
	docsSiteDocsDir := filepath.Join(dirPath, strings.TrimRight(configString(cfg, "docs"), "/"))
	injected, err := build.InjectPostsIntoDocs(dirPath, cfg, docsSiteDocsDir, includeDrafts, h)
	if err != nil {
		return nil, err
	}
	if len(injected) > 0 {
		allInjected = append(allInjected, injectedPosts{files: injected, docsDir: docsSiteDocsDir})
	}
	dsPartition, err := build.PartitionPages(cfg, docsSiteDocsDir, dirPath, h)
	if err != nil {
		return nil, err
	}
	projectSitePages["common"] = sitePageOwner{
		dirPath: dirPath,
		config:  cfg,
		pages:   dsPartition.Site,
	}

	body := &unifiedBuild{
		dirPath:               dirPath,
		config:                cfg,
		unifiedConfig:         unifiedConfig,
		locales:               locales,
		versions:              versions,
		defaultLocaleCode:     defaultLocaleCode,
		latestVersion:         latestVersion,
		outputDir:             outputDir,
		projectPagePartitions: projectPagePartitions,
		dsPartition:           dsPartition,
		rawThemeCSS:           rawThemeCSS,
		themeMeta:             themeMeta,
		criticalCSS:           criticalCSS,
		projectSitePages:      projectSitePages,
		written:               newWrittenSet(),
		handle:                h,
	}
	return body.written.paths, body.run()
}

// unifiedBuild is one unified build in progress: everything [BuildUnified]
// resolved, plus what the passes accumulate.
//
// It is a type rather than a long parameter list so the post cleanup can wrap
// the whole body, and so every pass writes through one recorded set.
type unifiedBuild struct {
	dirPath           string
	config            config.Config
	unifiedConfig     map[string]any
	locales           []map[string]any
	versions          []map[string]any
	defaultLocaleCode string
	latestVersion     string
	outputDir         string
	// projectPagePartitions maps a constituent's slug to its page
	// partition; a constituent with no selfdoc.json has no entry.
	projectPagePartitions map[string]build.Partition
	// dsPartition is the docs-site's own page partition.
	dsPartition build.Partition
	rawThemeCSS string
	themeMeta   themes.Metadata
	criticalCSS string
	// projectSitePages maps every project -- the constituents and
	// "common" -- to its site-level pages.
	projectSitePages map[string]sitePageOwner

	written      *writtenSet
	handle       *effects.Handle
	projectCards []projectCard
	// common is the docs-site's default-locale pass, whose data the
	// site-level files are built from.
	common *commonBuild
}

// commonBuild is the docs-site's own default-locale pass: what the landing
// page, the shared stylesheet and the auxiliary documents read.
type commonBuild struct {
	markdownFiles        []page.SourceFile
	frontmatter          map[string]util.Frontmatter
	pageDates            map[string]page.PageDates
	projectName          string
	version              string
	docsDir              string
	hasCustomCSS         bool
	configDescription    string
	baseURL              string
	urlBuilder           urls.URLBuilder
	feedURL              string
	lang                 string
	unversionedAddresses map[string]address.PageAddress
}

// run is the core unified build: every page of every mount, then everything
// that belongs to the site rather than to a page.
func (b *unifiedBuild) run() error {
	// The build owns the "v/" and "blog/" segments at the top of every
	// mount; an author page may not take either.
	for _, slug := range sortedKeys(b.projectPagePartitions) {
		partition := b.projectPagePartitions[slug]
		if err := build.CheckReservedPagePaths(
			unionPaths(partition.Versioned, partition.Unversioned)); err != nil {
			return err
		}
	}
	if err := build.CheckReservedPagePaths(
		unionPaths(b.dsPartition.Versioned, b.dsPartition.Unversioned)); err != nil {
		return err
	}

	if err := b.buildConstituents(); err != nil {
		return err
	}
	if err := b.buildConstituentUnversioned(); err != nil {
		return err
	}
	if err := b.buildCommon(); err != nil {
		return err
	}
	if err := b.buildCommonUnversioned(); err != nil {
		return err
	}
	if err := b.buildSiteLevelPages(); err != nil {
		return err
	}
	if b.common == nil {
		return errors.New(
			"Unified build produced no pass for the default locale: " +
				"the site-level files have nothing to be built from.")
	}
	if b.common.urlBuilder == nil {
		return &config.ConfigError{Message: "selfdoc.json declares no 'base_url'; the " +
			"unified site-level files and the root redirect state absolute URLs " +
			"and there is nothing to build them from."}
	}
	if err := b.writeLandingPage(); err != nil {
		return err
	}
	if err := b.writeSharedAssets(); err != nil {
		return err
	}
	if err := b.writeAuxiliaryFiles(); err != nil {
		return err
	}
	if err := b.writeRootRedirect(); err != nil {
		return err
	}

	// Pagefind indexes the whole unified site -- every project's pages under
	// one index at the output root, which is the same place every page's
	// asset hop points at. It runs after the last HTML file and before
	// compression, as the single-project build does.
	if err := build.RunPagefind(b.outputDir, b.handle); err != nil {
		return err
	}

	compressCount, err := build.CompressOutput(b.outputDir, b.handle)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Pre-compressed %d files (gzip + brotli)\n", compressCount)
	return nil
}
