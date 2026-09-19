package build

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/docs"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/staleness"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// BuildResult is everything one locale-and-version pass built, handed back to
// the caller instead of written.
//
// [Build] does all of the output IO; this type is what it writes from, and
// what the in-memory render path reads instead of a file.
type BuildResult struct {
	// HTMLFiles maps each page's output key -- the mount-prefixed path the
	// file is written at -- to its full HTML document.
	HTMLFiles map[string]string
	// MarkdownFiles are the pass's resolved Markdown sources, sorted by
	// docs-relative path.
	MarkdownFiles []page.SourceFile
	// Frontmatter maps a page's source path to its parsed metadata, with an
	// entry only for a page that declares some.
	Frontmatter map[string]util.Frontmatter
	// PageDates maps a page's source path to the dates it states.
	PageDates map[string]page.PageDates
	// NavItems is the sidebar navigation tree, which the site-level files
	// read too.
	NavItems []page.NavItem
	// ProjectName is the project's name.
	ProjectName string
	// Version is the version this pass built.
	Version string
	// Config is the config the pass ran under, locale docs directory
	// included.
	Config config.Config
	// DocsDir is the directory the pass read its templates from.
	DocsDir string
	// OtherFiles are the non-Markdown assets under DocsDir, relative to it
	// and sorted.
	OtherFiles []string
	// HasCustomCSS says whether the docs tree ships a custom.css.
	HasCustomCSS bool
	// RawThemeCSS is the theme's composed stylesheet, before minification.
	RawThemeCSS string
	// ThemeMeta is the theme's metadata.
	ThemeMeta *themes.Metadata
	// CriticalCSS is the minified above-the-fold fragment every page
	// inlines.
	CriticalCSS string
	// ConfigDescription is the project-level description.
	ConfigDescription string
	// BaseURL is the site's base address, "" when the config declares none.
	BaseURL string
	// FeedURL is where the Atom feed sits relative to a page.
	FeedURL string
	// Lang is the language tag the pass's pages declare.
	Lang string
	// URLBuilder is the builder the pass's absolute URLs came from. It is
	// nil on a result [BuildSingle] returned: the caller that needs one
	// builds it from the config.
	URLBuilder urls.URLBuilder
}

// SingleOptions is everything one [BuildSingle] pass takes.
//
// It mirrors the keyword arguments of the Python function it replaces, with
// two omissions: that function also took the current version and an is-latest
// flag, and passed both to a renderer that read neither.
//
// Start from [NewSingleOptions] rather than from the zero value: the baselines
// are written by default, and a zero-valued struct would silently stop
// advancing them.
type SingleOptions struct {
	// DirPath is the project root.
	DirPath string
	// Config is a pre-loaded config. Nil loads selfdoc.json from DirPath.
	Config config.Config
	// MountLocale is the locale segment of this pass's output mount, e.g.
	// "en". Point it at "" for an unmounted pass; leave it nil to have the
	// coordinate computed from the config alongside MountVersion.
	MountLocale *string
	// MountProject is the constituent-project segment of the mount on a
	// unified site, "" on a standalone one.
	MountProject string
	// MountVersion is the version these pages are built from, e.g.
	// "0.7.0". Point it at "" for pages that are not version-scoped; leave
	// it nil to have the coordinate computed from the config alongside
	// MountLocale. A version does not imply a version segment: the current
	// version is emitted at the stable address.
	MountVersion *string
	// MountArchived marks this pass a superseded version, emitted under
	// "v/<version>/" beside the stable tree.
	MountArchived bool
	// VersionOverride replaces the detected version string. Nil detects it
	// from the project's manifests.
	VersionOverride *string
	// LocaleOverride is the locale this pass builds, which prefixes the
	// staleness baselines and can become the pages' language tag. Nil means
	// the pass is not locale-scoped.
	LocaleOverride *string
	// AvailableVersions and AvailableLocales are the configured versions
	// and locales the two pickers offer.
	AvailableVersions []map[string]any
	AvailableLocales  []map[string]any
	// VersionPages maps a version to the output keys it holds, so the
	// version picker never offers a version that does not have the page.
	VersionPages map[string]map[string]bool
	// CurrentLocale is the locale being built, for the locale picker's
	// selected option and the locale facet.
	CurrentLocale string
	// PageFilter keeps only the named pages. Nil builds every page the walk
	// found.
	PageFilter map[string]bool
	// UnversionedMarkdown and UnversionedFrontmatter are the persistent
	// pages every version's sidebar shows. They reach the navigation and
	// nothing else.
	UnversionedMarkdown    map[string]string
	UnversionedFrontmatter map[string]util.Frontmatter
	// OverlayDocs maps a docs-relative .md path to Markdown source held in
	// memory. Entries are resolved like files on disk and override
	// same-named files, so pages that were never written can be built.
	OverlayDocs map[string]string
	// WriteBaselines advances the staleness baselines under
	// the hash store. False makes the whole call write nothing at all,
	// which -- together with OverlayDocs -- is what makes an in-memory
	// render possible.
	WriteBaselines bool
	// Siblings are the other projects published on the assembled site this
	// build's output is grafted into. Each built page ends with a section
	// linking them. Empty -- which is what a standalone build passes --
	// emits no section at all: a project deployed on its own has no
	// siblings, and there is nothing to invent them from.
	Siblings []SiblingProject
	// SiteName is the name of the assembled site this build's output is
	// grafted into, which every page's document title ends with. It is
	// stated by the caller for the same reason Siblings is: a standalone
	// build has no site around it, states none, and its pages end their
	// titles at the project.
	SiteName string
	// Now supplies the modification date of a page that states none and has
	// no file behind it. The zero value takes the current time.
	Now time.Time
}

// NewSingleOptions returns the options one pass starts from: the baselines are
// advanced, which is what every build does.
func NewSingleOptions() SingleOptions {
	return SingleOptions{WriteBaselines: true}
}

// BuildSingle builds the HTML for a single version and locale of a project's
// docs.
//
// It loads the config, resolves every template's directives, wraps each page
// in its chrome, adds the image dimensions read off the files on disk, and
// hands the whole result back: [Build] does the output IO.
//
// One thing is written here, and only here: the staleness baselines under
// the hash store. Turn WriteBaselines off and the call touches nothing at
// all.
//
// The mount is the single input that decides where pages land and how they
// reach the output root; the addressing authority turns it plus a page path
// into every address the page has.
func BuildSingle(opts SingleOptions, h *effects.Handle) (BuildResult, error) {
	cfg := opts.Config
	if cfg == nil {
		loaded, err := config.Load(opts.DirPath)
		if err != nil {
			return BuildResult{}, err
		}
		cfg = loaded
	}
	if cfg == nil {
		return BuildResult{}, errors.New(
			"No selfdoc.json found. Run 'selfdoc init' to initialize.")
	}

	mountLocale, mountVersion := resolveMountCoordinates(cfg, opts.MountLocale, opts.MountVersion)

	docsDirName := strings.TrimRight(configString(cfg, "docs"), "/")
	docsDir := filepath.Join(opts.DirPath, docsDirName)
	outputDir := filepath.Join(opts.DirPath, strings.TrimRight(configString(cfg, "output"), "/"))

	if !isDir(docsDir) {
		return BuildResult{}, fmt.Errorf(
			"Docs directory '%s' not found. Create it or run 'selfdoc init'.",
			configString(cfg, "docs"))
	}

	allDocs, err := docs.ResolveAll(cfg, docsDir, opts.DirPath, opts.OverlayDocs, h)
	if err != nil {
		return BuildResult{}, err
	}

	markdownFiles := make([]page.SourceFile, 0, len(allDocs))
	frontmatter := map[string]util.Frontmatter{}
	for relPath, doc := range allDocs {
		markdownFiles = append(markdownFiles, page.SourceFile{MdPath: relPath, Content: doc.Resolved})
		if len(doc.Frontmatter) > 0 {
			frontmatter[relPath] = doc.Frontmatter
		}
	}
	markdownFiles = sortedSources(markdownFiles)

	otherFiles, err := collectOtherFiles(docsDir, outputDir)
	if err != nil {
		return BuildResult{}, err
	}

	// The content and description hashes the staleness detection compares
	// against. The build always proceeds -- staleness is only enforced at
	// check time -- and it always writes fresh baselines and never
	// enforces, so no skeleton exemption is needed and the exempt set is
	// passed empty. Keys carry the locale so two locales' same-named pages
	// do not collide.
	if opts.WriteBaselines {
		stalenessDocs := docs.StalenessDocs(allDocs)
		if opts.LocaleOverride != nil && *opts.LocaleOverride != "" {
			prefixed := make(map[string]staleness.Doc, len(stalenessDocs))
			for relPath, doc := range stalenessDocs {
				prefixed[*opts.LocaleOverride+"/"+relPath] = doc
			}
			stalenessDocs = prefixed
		}
		if _, _, err := staleness.UpdateHashes(
			stalenessDocs, opts.DirPath, false, nil, nil, map[string]bool{}, h,
		); err != nil {
			return BuildResult{}, err
		}
	}

	markdownFiles = filterSources(markdownFiles, opts.PageFilter)
	if opts.PageFilter != nil {
		filtered := map[string]util.Frontmatter{}
		for relPath, meta := range frontmatter {
			if opts.PageFilter[relPath] {
				filtered[relPath] = meta
			}
		}
		frontmatter = filtered
	}

	pageDates := computePageDates(markdownFiles, frontmatter, docsDir, opts.Now)

	changelogPath, err := changelogSource(cfg, opts.DirPath, false)
	if err != nil {
		return BuildResult{}, err
	}
	if changelogPath != "" && (opts.PageFilter == nil || opts.PageFilter["changelog.md"]) {
		content, readErr := os.ReadFile(changelogPath)
		if readErr != nil {
			return BuildResult{}, readErr
		}
		// Injected as if it were docs/changelog.md, so it flows through
		// the normal pipeline: nav, previous/next, HTML wrapping.
		markdownFiles = mergeSources(markdownFiles,
			[]page.SourceFile{{MdPath: "changelog.md", Content: string(content)}})
		frontmatter["changelog.md"] = util.Frontmatter{
			"title":     "Changelog",
			"nav_order": int64(999),
			"feed":      false,
		}
	}

	themeName := configString(cfg, "theme")
	if themeName == "" {
		themeName = "minimal"
	}
	rawThemeCSS, err := html.GetCSS(themeName)
	if err != nil {
		return BuildResult{}, err
	}
	themeMeta, err := themes.Meta(themeName)
	if err != nil {
		return BuildResult{}, err
	}

	projectName := filepath.Base(mustAbs(opts.DirPath))
	hasCustomCSS := isFile(filepath.Join(docsDir, "custom.css"))

	if len(markdownFiles) == 0 {
		if opts.PageFilter != nil {
			// A filtered pass may legitimately hold no page -- every page
			// of this locale may be unversioned, so the versioned filter
			// yields nothing.
			version := ""
			if opts.VersionOverride != nil {
				version = *opts.VersionOverride
			}
			return BuildResult{
				HTMLFiles:         map[string]string{},
				MarkdownFiles:     nil,
				Frontmatter:       map[string]util.Frontmatter{},
				PageDates:         map[string]page.PageDates{},
				NavItems:          nil,
				ProjectName:       projectName,
				Version:           version,
				Config:            cfg,
				DocsDir:           docsDir,
				OtherFiles:        nil,
				HasCustomCSS:      hasCustomCSS,
				RawThemeCSS:       rawThemeCSS,
				ThemeMeta:         &themeMeta,
				CriticalCSS:       "",
				ConfigDescription: util.PythonStrOrEmpty(cfg["description"]),
				BaseURL:           util.PythonStrOrEmpty(cfg["base_url"]),
				FeedURL:           "feed.xml",
				Lang:              langOf(cfg),
			}, nil
		}
		return BuildResult{}, fmt.Errorf(
			"No .md files found in '%s'. Nothing to build.", configString(cfg, "docs"))
	}

	version := util.DetectProjectVersion(opts.DirPath, "")
	if opts.VersionOverride != nil {
		version = *opts.VersionOverride
	}

	branch, err := detectBranch(cfg, opts.DirPath, h)
	if err != nil {
		return BuildResult{}, err
	}

	baseURL := util.PythonStrOrEmpty(cfg["base_url"])
	urlBuilder := MakeURLBuilder(cfg)

	// With more than one locale configured the locale code is the
	// authoritative language tag; a single-locale project's config "lang"
	// decides.
	lang := langOf(cfg)
	if opts.LocaleOverride != nil && len(opts.AvailableLocales) > 1 {
		lang = *opts.LocaleOverride
	}

	author, _ := cfg["author"].(map[string]any)
	configDescription := util.PythonStrOrEmpty(cfg["description"])
	feedURL := "feed.xml"

	criticalCSS, _ := ExtractCriticalCSS(rawThemeCSS)
	criticalCSS = MinifyCSS(criticalCSS)

	unversionedPages := overlayToSources(opts.UnversionedMarkdown)

	genOpts := page.NewOptions()
	genOpts.MarkdownFiles = markdownFiles
	genOpts.ProjectName = projectName
	genOpts.Version = version
	genOpts.HasCustomCSS = hasCustomCSS
	genOpts.Repo = util.PythonStrOrEmpty(cfg["repo"])
	genOpts.DocsDirName = configString(cfg, "docs")
	genOpts.BaseURL = baseURL
	genOpts.URLBuilder = urlBuilder
	genOpts.Frontmatter = frontmatter
	genOpts.Lang = lang
	genOpts.PageDates = pageDates
	genOpts.Author = author
	genOpts.FeedURL = feedURL
	genOpts.CriticalCSS = criticalCSS
	genOpts.TwitterSite = util.PythonStrOrEmpty(cfg["twitter"])
	genOpts.Search = util.PythonStrOrEmpty(cfg["search"])
	genOpts.Feedback, _ = cfg["feedback"].(map[string]any)
	genOpts.Branch = branch
	genOpts.Branding, _ = cfg["branding"].(map[string]any)
	genOpts.ConfigDescription = configDescription
	genOpts.SiteName = opts.SiteName
	genOpts.AutoDetect, _ = cfg["auto_detect"].(map[string]any)
	genOpts.ThemeMeta = &themeMeta
	genOpts.DeployTarget = deployProvider(cfg)
	genOpts.RunButton = configBool(cfg, "run_button", false)
	genOpts.LineNumbers = configBool(cfg, "line_numbers", false)
	genOpts.PageNav = configBool(cfg, "page_nav", true)
	genOpts.PageProgress = configBool(cfg, "page_progress", true)
	if icons := util.PythonStrOrEmpty(cfg["code_icons"]); icons != "" {
		genOpts.CodeIcons = icons
	}
	genOpts.Glossary = configBool(cfg, "glossary", true)
	genOpts.MountLocale = mountLocale
	genOpts.MountProject = opts.MountProject
	genOpts.MountVersion = mountVersion
	genOpts.MountArchived = opts.MountArchived
	genOpts.AvailableVersions = versionEntries(opts.AvailableVersions)
	genOpts.AvailableLocales = localeEntries(opts.AvailableLocales)
	genOpts.VersionPages = opts.VersionPages
	genOpts.CurrentLocale = opts.CurrentLocale
	genOpts.SchemaTypes = stringMap(cfg["schema_types"])
	genOpts.UnversionedPages = unversionedPages
	genOpts.UnversionedFrontmatter = opts.UnversionedFrontmatter

	htmlFiles, err := page.GenerateHTML(genOpts)
	if err != nil {
		return BuildResult{}, err
	}

	// The pages carry output keys, mount included, so the mount is stripped
	// before the key is turned back into a docs-relative Markdown path.
	mountAddr, err := address.NewPageAddress("index.html", address.Coordinates{
		Locale:   mountLocale,
		Project:  opts.MountProject,
		Version:  mountVersion,
		Archived: opts.MountArchived,
	})
	if err != nil {
		return BuildResult{}, err
	}
	mount := mountAddr.Mount
	for outputKey := range htmlFiles {
		pagePath := outputKey
		if mount != "" {
			pagePath = outputKey[len(mount)+1:]
		}
		htmlFiles[outputKey] = WithRefreshedSiblings(
			AddImageDimensions(
				htmlFiles[outputKey], docsDir, html.HTMLToMdPath(pagePath)),
			siteRootHop(outputKey), opts.Siblings)
	}

	navItems := page.BuildNav(markdownFiles, frontmatter, unversionedPages, opts.UnversionedFrontmatter)

	return BuildResult{
		HTMLFiles:         htmlFiles,
		MarkdownFiles:     markdownFiles,
		Frontmatter:       frontmatter,
		PageDates:         pageDates,
		NavItems:          navItems,
		ProjectName:       projectName,
		Version:           version,
		Config:            cfg,
		DocsDir:           docsDir,
		OtherFiles:        otherFiles,
		HasCustomCSS:      hasCustomCSS,
		RawThemeCSS:       rawThemeCSS,
		ThemeMeta:         &themeMeta,
		CriticalCSS:       criticalCSS,
		ConfigDescription: configDescription,
		BaseURL:           baseURL,
		FeedURL:           feedURL,
		Lang:              lang,
	}, nil
}

// resolveMountCoordinates answers the locale and version segments of this
// pass's mount.
//
// Both unstated is the case the config decides: the default locale's segment
// and the first configured version. Either stated explicitly is taken as
// given, and an unstated companion is empty rather than inferred.
func resolveMountCoordinates(cfg config.Config, mountLocale, mountVersion *string) (string, string) {
	if mountLocale == nil && mountVersion == nil {
		locales := configList(cfg, "locales")
		versions := configList(cfg, "versions")
		if len(locales) > 0 && len(versions) > 0 {
			return address.LocaleSegment(
					util.PythonStrOrEmpty(defaultLocaleEntry(locales)["code"]), locales),
				util.PythonStrOrEmpty(versions[0]["version"])
		}
		return "", ""
	}
	locale, version := "", ""
	if mountLocale != nil {
		locale = *mountLocale
	}
	if mountVersion != nil {
		version = *mountVersion
	}
	return locale, version
}

// collectOtherFiles lists the non-Markdown assets under docsDir, relative to
// it and sorted. The output directory is skipped: it holds a previous build's
// artifacts, not the project's assets.
func collectOtherFiles(docsDir, outputDir string) ([]string, error) {
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	var other []string
	walkErr := filepath.WalkDir(docsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			absDir, absErr := filepath.Abs(path)
			if absErr != nil {
				return absErr
			}
			if absDir == absOutput || strings.HasPrefix(absDir, absOutput+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(docsDir, path)
		if relErr != nil {
			return relErr
		}
		if filepath.ToSlash(rel) == layout.ManifestFileName {
			// The docs directory's ownership manifest is layout state
			// rather than a page asset: it is never published.
			return nil
		}
		other = append(other, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(other)
	return other, nil
}

// computePageDates answers the two dates every page states.
//
// The modification date is the frontmatter "updated", else "date", else the
// file's own mtime; the publication date is "date", else the mtime -- never
// "updated", which says when the page changed rather than when it appeared. A
// page with no file behind it (an overlay) is dated now: injecting it would
// have written the file just now, so now is the same date.
func computePageDates(
	markdownFiles []page.SourceFile,
	frontmatter map[string]util.Frontmatter,
	docsDir string,
	now time.Time,
) map[string]page.PageDates {
	dates := make(map[string]page.PageDates, len(markdownFiles))
	for _, src := range markdownFiles {
		meta := frontmatter[src.MdPath]
		updated, hasUpdated := meta["updated"]
		declared, hasDate := meta["date"]
		switch {
		case hasUpdated && hasDate:
			dates[src.MdPath] = page.PageDates{
				Published: util.PythonStr(declared),
				Modified:  util.PythonStr(updated),
			}
		case hasUpdated:
			dates[src.MdPath] = page.PageDates{Modified: util.PythonStr(updated)}
		case hasDate:
			dates[src.MdPath] = page.PageDates{
				Published: util.PythonStr(declared),
				Modified:  util.PythonStr(declared),
			}
		default:
			stamp := now
			if info, err := os.Stat(filepath.Join(docsDir, src.MdPath)); err == nil && info.Mode().IsRegular() {
				stamp = info.ModTime()
			} else if stamp.IsZero() {
				stamp = time.Now()
			}
			formatted := stamp.Format("2006-01-02")
			dates[src.MdPath] = page.PageDates{Published: formatted, Modified: formatted}
		}
	}
	return dates
}

// rootChangelogNames are the root filenames the changelog page is
// auto-detected from, in order.
var rootChangelogNames = []string{"CHANGELOG.md", "Changelog.md", "changelog.md"}

// changelogSource is the path of the changelog document this site publishes,
// "" when it has none.
//
// Two ways in, and the config decides which:
//
//   - "changelog" names the file. A declared name that is not there is an
//     error -- the page was asked for, so its absence is a broken build rather
//     than a page that quietly does not appear.
//   - Absent, the project root's CHANGELOG.md is used when it exists. That
//     convention reads "the root changelog is this project's changelog", which
//     holds for a standalone repository and fails in a workspace whose root
//     file rolls up several independently versioned projects; such a project
//     declares the key instead.
//
// missingOK is for the scans that ask which pages a PAST version held: an
// older checkout need not have the file, and that is an answer rather than a
// failure.
func changelogSource(cfg config.Config, dirPath string, missingOK bool) (string, error) {
	if declared := util.PythonStrOrEmpty(cfg["changelog"]); declared != "" {
		candidate := filepath.Join(dirPath, declared)
		if isFile(candidate) {
			return candidate, nil
		}
		if missingOK {
			return "", nil
		}
		return "", fmt.Errorf(
			"changelog: '%s' does not exist under %s. The config names the "+
				"changelog document to publish, so it has to be a file; remove "+
				"the key to fall back to the project root's CHANGELOG.md.",
			declared, dirPath)
	}
	for _, name := range rootChangelogNames {
		candidate := filepath.Join(dirPath, name)
		if isFile(candidate) {
			return candidate, nil
		}
	}
	return "", nil
}

// detectBranch answers the branch the edit links point at: the declared one,
// else the repository's current branch, else "main".
func detectBranch(cfg config.Config, dirPath string, h *effects.Handle) (string, error) {
	if branch := util.PythonStrOrEmpty(cfg["branch"]); branch != "" {
		return branch, nil
	}
	result, err := h.Run([]string{"git", "symbolic-ref", "--short", "HEAD"},
		effects.CaptureOutput(), effects.Timeout(5*time.Second),
		effects.Cwd(dirPath), effects.Read())
	if err == nil && result.ExitCode == 0 {
		if branch := util.PythonStrip(string(result.Stdout)); branch != "" {
			return branch, nil
		}
	}
	return "main", nil
}

// mustAbs resolves a path against the working directory, answering the path
// unchanged when it cannot.
func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// langOf is the language tag a config declares, "en" when it declares none.
func langOf(cfg config.Config) string {
	if lang := util.PythonStrOrEmpty(cfg["lang"]); lang != "" {
		return lang
	}
	return "en"
}

// deployProvider is the hosting provider the config's deploy block names.
func deployProvider(cfg config.Config) string {
	deploy, _ := cfg["deploy"].(map[string]any)
	return util.PythonStrOrEmpty(deploy["provider"])
}

// configBool reads a boolean config key, answering fallback for a key that is
// absent or carries something else.
func configBool(cfg config.Config, key string, fallback bool) bool {
	if value, ok := cfg[key].(bool); ok {
		return value
	}
	return fallback
}

// configList reads a list-of-objects config key, dropping any element that is
// not an object.
func configList(cfg config.Config, key string) []map[string]any {
	raw, _ := cfg[key].([]any)
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// defaultLocaleEntry is the locale a config marks default, else the first one
// it declares.
func defaultLocaleEntry(locales []map[string]any) map[string]any {
	for _, locale := range locales {
		if isDefault, ok := locale["default"].(bool); ok && isDefault {
			return locale
		}
	}
	return locales[0]
}

// versionEntries narrows configured version objects to what the version
// picker reads.
func versionEntries(versions []map[string]any) []page.VersionEntry {
	entries := make([]page.VersionEntry, 0, len(versions))
	for _, version := range versions {
		entries = append(entries, page.VersionEntry{
			Version: util.PythonStrOrEmpty(version["version"]),
		})
	}
	return entries
}

// localeEntries narrows configured locale objects to what the locale picker
// and the hreflang block read.
func localeEntries(locales []map[string]any) []page.LocaleEntry {
	entries := make([]page.LocaleEntry, 0, len(locales))
	for _, locale := range locales {
		isDefault, _ := locale["default"].(bool)
		entries = append(entries, page.LocaleEntry{
			Code:    util.PythonStrOrEmpty(locale["code"]),
			Label:   util.PythonStrOrEmpty(locale["label"]),
			Default: isDefault,
		})
	}
	return entries
}

// stringMap narrows a config object whose values are all strings.
func stringMap(value any) map[string]string {
	declared, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(declared))
	for key, item := range declared {
		out[key] = util.PythonStrOrEmpty(item)
	}
	return out
}

// overlayToSources turns a path-to-Markdown mapping into sorted sources.
func overlayToSources(overlay map[string]string) []page.SourceFile {
	if len(overlay) == 0 {
		return nil
	}
	sources := make([]page.SourceFile, 0, len(overlay))
	for mdPath, content := range overlay {
		sources = append(sources, page.SourceFile{MdPath: mdPath, Content: content})
	}
	return sortedSources(sources)
}
