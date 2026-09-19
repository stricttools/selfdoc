package unified

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/html"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// buildConstituents builds every constituent project once per docs-site
// version and locale.
//
// A docs-site version that pins a constituent to a version of its own -- the
// "projects" map on the version entry -- is built from that constituent's own
// tag instead of from its working tree, so an archived docs-site version
// really serves the constituent content of its day. The pinning is only read
// for a superseded docs-site version: the current one is always the working
// tree.
func (b *unifiedBuild) buildConstituents() error {
	for _, verEntry := range b.versions {
		verStr := util.PythonStrOrEmpty(verEntry["version"])
		isLatest := verStr == b.latestVersion
		pinning, _ := verEntry["projects"].(map[string]any)

		for _, projectEntry := range unifiedProjects(b.unifiedConfig) {
			slug := ProjectSlug(projectEntry)
			navTitle := ProjectNavTitle(projectEntry)
			projectPath, err := ResolveProjectPath(projectEntry, b.dirPath)
			if err != nil {
				return err
			}
			projConfig, err := config.Load(projectPath)
			if err != nil {
				return err
			}
			if projConfig == nil {
				return &config.ConfigError{Message: "No selfdoc.json found in constituent project '" +
					util.PythonStrOrEmpty(projectEntry["path"]) + "' (resolved to '" + projectPath + "')"}
			}

			buildDir := projectPath
			projVersion := util.DetectProjectVersion(projectPath, "")
			if pinnedVersion, ok := pinning[slug]; ok && !isLatest {
				pinned := util.PythonStrOrEmpty(pinnedVersion)
				buildDir, err = build.ExtractVersionContent(pinned, projConfig, projectPath, b.handle)
				if err != nil {
					return err
				}
				projVersion = pinned
			}

			for _, locale := range b.locales {
				localeCode := util.PythonStrOrEmpty(locale["code"])
				mountLocale := address.LocaleSegment(localeCode, b.locales)
				projectHome, err := address.NewPageAddress("index.html", address.Coordinates{
					Locale: mountLocale, Project: slug, Version: verStr, Archived: !isLatest,
				})
				if err != nil {
					return err
				}
				partition := b.projectPagePartitions[slug]

				versionOverride := projVersion
				if versionOverride == "" {
					versionOverride = verStr
				}
				opts := build.NewSingleOptions()
				opts.DirPath = buildDir
				opts.Config = projConfig
				opts.MountLocale = &mountLocale
				opts.MountProject = slug
				opts.MountVersion = &verStr
				opts.MountArchived = !isLatest
				opts.VersionOverride = &versionOverride
				opts.LocaleOverride = &localeCode
				opts.AvailableVersions = b.versions
				opts.AvailableLocales = b.locales
				opts.CurrentLocale = localeCode
				if len(partition.Unversioned) > 0 {
					opts.PageFilter = partition.Versioned
					opts.UnversionedMarkdown = partition.UnversionedMarkdown
					opts.UnversionedFrontmatter = partition.UnversionedFrontmatter
				}

				result, err := build.BuildSingle(opts, b.handle)
				if err != nil {
					return err
				}
				if err := b.writeHTMLFiles(result.HTMLFiles); err != nil {
					return err
				}
				if err := b.copyOtherFiles(
					result.OtherFiles, result.DocsDir, projectHome.Mount); err != nil {
					return err
				}

				// One card per project, from the pass a visitor actually
				// sees first: the current version in the default locale.
				if isLatest && localeCode == b.defaultLocaleCode {
					b.projectCards = append(b.projectCards, projectCard{
						Slug:        slug,
						NavTitle:    navTitle,
						Description: result.ConfigDescription,
						Version:     versionOverride,
						Home:        projectHome.URL(),
					})
				}
			}
		}
	}
	return nil
}

// buildConstituentUnversioned builds each constituent's "versioned: false"
// pages, once per locale at the version-free mount.
func (b *unifiedBuild) buildConstituentUnversioned() error {
	for _, projectEntry := range unifiedProjects(b.unifiedConfig) {
		slug := ProjectSlug(projectEntry)
		projectPath, err := ResolveProjectPath(projectEntry, b.dirPath)
		if err != nil {
			return err
		}
		projConfig, err := config.Load(projectPath)
		if err != nil {
			return err
		}
		partition := b.projectPagePartitions[slug]
		if len(partition.Unversioned) == 0 {
			continue
		}

		for _, locale := range b.locales {
			localeCode := util.PythonStrOrEmpty(locale["code"])
			mountLocale := address.LocaleSegment(localeCode, b.locales)
			mountAddr, err := address.NewPageAddress("index.html", address.Coordinates{
				Locale: mountLocale, Project: slug,
			})
			if err != nil {
				return err
			}

			empty := ""
			opts := build.NewSingleOptions()
			opts.DirPath = projectPath
			opts.Config = projConfig
			opts.MountLocale = &mountLocale
			opts.MountProject = slug
			opts.MountVersion = &empty
			opts.VersionOverride = &empty
			opts.LocaleOverride = &localeCode
			opts.AvailableVersions = b.versions
			opts.AvailableLocales = b.locales
			opts.CurrentLocale = localeCode
			opts.PageFilter = partition.Unversioned

			result, err := build.BuildSingle(opts, b.handle)
			if err != nil {
				return err
			}
			if err := b.writeHTMLFiles(result.HTMLFiles); err != nil {
				return err
			}
			if err := b.copyOtherFiles(result.OtherFiles, result.DocsDir, mountAddr.Mount); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildCommon builds the docs-site's own cross-cutting pages, once per locale,
// under the "common" mount at the current docs-site version.
//
// The default locale's pass is kept: it is what the landing page, the
// auxiliary documents and the shared stylesheet are built from.
func (b *unifiedBuild) buildCommon() error {
	for _, locale := range b.locales {
		localeCode := util.PythonStrOrEmpty(locale["code"])
		mountLocale := address.LocaleSegment(localeCode, b.locales)
		commonAddr, err := address.NewPageAddress("index.html", address.Coordinates{
			Locale: mountLocale, Project: "common", Version: b.latestVersion,
		})
		if err != nil {
			return err
		}

		opts := build.NewSingleOptions()
		opts.DirPath = b.dirPath
		opts.Config = b.config
		opts.MountLocale = &mountLocale
		opts.MountProject = "common"
		opts.MountVersion = &b.latestVersion
		opts.VersionOverride = &b.latestVersion
		opts.LocaleOverride = &localeCode
		opts.AvailableVersions = b.versions
		opts.AvailableLocales = b.locales
		opts.CurrentLocale = localeCode
		if len(b.dsPartition.Unversioned) > 0 {
			opts.PageFilter = b.dsPartition.Versioned
			opts.UnversionedMarkdown = b.dsPartition.UnversionedMarkdown
			opts.UnversionedFrontmatter = b.dsPartition.UnversionedFrontmatter
		}

		result, err := build.BuildSingle(opts, b.handle)
		if err != nil {
			return err
		}
		if err := b.writeHTMLFiles(result.HTMLFiles); err != nil {
			return err
		}
		if err := b.copyOtherFiles(result.OtherFiles, result.DocsDir, commonAddr.Mount); err != nil {
			return err
		}

		if localeCode == b.defaultLocaleCode {
			var builder urls.URLBuilder
			if result.BaseURL != "" {
				builder = urls.NewSimpleURLBuilder(result.BaseURL)
			}
			b.common = &commonBuild{
				markdownFiles:     result.MarkdownFiles,
				frontmatter:       result.Frontmatter,
				pageDates:         result.PageDates,
				projectName:       result.ProjectName,
				version:           result.Version,
				docsDir:           result.DocsDir,
				hasCustomCSS:      result.HasCustomCSS,
				configDescription: result.ConfigDescription,
				baseURL:           result.BaseURL,
				urlBuilder:        builder,
				feedURL:           result.FeedURL,
				lang:              result.Lang,
			}
		}
	}
	return nil
}

// buildCommonUnversioned builds the docs-site's own "versioned: false" pages,
// once per locale at the version-free "common" mount, and folds the default
// locale's pass into the pass the site-level files are built from.
func (b *unifiedBuild) buildCommonUnversioned() error {
	if len(b.dsPartition.Unversioned) == 0 {
		return nil
	}
	for _, locale := range b.locales {
		localeCode := util.PythonStrOrEmpty(locale["code"])
		mountLocale := address.LocaleSegment(localeCode, b.locales)
		mountAddr, err := address.NewPageAddress("index.html", address.Coordinates{
			Locale: mountLocale, Project: "common",
		})
		if err != nil {
			return err
		}

		empty := ""
		opts := build.NewSingleOptions()
		opts.DirPath = b.dirPath
		opts.Config = b.config
		opts.MountLocale = &mountLocale
		opts.MountProject = "common"
		opts.MountVersion = &empty
		opts.VersionOverride = &empty
		opts.LocaleOverride = &localeCode
		opts.AvailableVersions = b.versions
		opts.AvailableLocales = b.locales
		opts.CurrentLocale = localeCode
		opts.PageFilter = b.dsPartition.Unversioned

		result, err := build.BuildSingle(opts, b.handle)
		if err != nil {
			return err
		}
		if err := b.writeHTMLFiles(result.HTMLFiles); err != nil {
			return err
		}
		if err := b.copyOtherFiles(result.OtherFiles, result.DocsDir, mountAddr.Mount); err != nil {
			return err
		}

		if localeCode != b.defaultLocaleCode || b.common == nil {
			continue
		}
		// These pages mount one level shallower than the versioned ones,
		// so their addresses come from their own pass.
		addresses := map[string]address.PageAddress{}
		for _, source := range result.MarkdownFiles {
			addr, err := address.NewPageAddress(
				html.MdToHTMLPath(source.MdPath),
				address.Coordinates{Locale: mountLocale, Project: "common"})
			if err != nil {
				return err
			}
			addresses[source.MdPath] = addr
		}
		b.common.unversionedAddresses = addresses
		b.common.markdownFiles = mergeSources(b.common.markdownFiles, result.MarkdownFiles)
		b.common.frontmatter = mergeFrontmatter(b.common.frontmatter, result.Frontmatter)
		b.common.pageDates = mergePageDates(b.common.pageDates, result.PageDates)
	}
	return nil
}

// buildSiteLevelPages builds every project's posts into the shared site-level
// tree, once per owning project, with no mount at all.
//
// Posts have no project segment: whichever project wrote one, it is emitted at
// "blog/<slug>/" under the site root, as the standalone build emits it. The
// per-project listing page is dropped here -- the assembly renders one blog
// index for the whole site. Because the slug namespace is shared, every
// project's slugs are checked against every other's before anything is built.
func (b *unifiedBuild) buildSiteLevelPages() error {
	listingPage := address.PostsPrefix + ".md"

	var claims []build.SlugClaim
	for _, owner := range sortedKeys(b.projectSitePages) {
		spec := b.projectSitePages[owner]
		for _, mdPath := range sortedKeys(spec.pages) {
			if mdPath == listingPage {
				continue
			}
			claims = append(claims, build.SlugClaim{
				Slug:   strings.TrimSuffix(pythonBasename(mdPath), ".md"),
				Source: owner,
			})
		}
	}
	if err := build.CheckPostSlugUniqueness(claims); err != nil {
		return err
	}

	for _, owner := range sortedKeys(b.projectSitePages) {
		spec := b.projectSitePages[owner]
		postPages := map[string]bool{}
		for mdPath := range spec.pages {
			if mdPath != listingPage {
				postPages[mdPath] = true
			}
		}
		if len(postPages) == 0 {
			continue
		}

		docsDirName := strings.TrimRight(configString(spec.config, "docs"), "/")
		opts := build.SiteLevelBuildArgs(spec.config, docsDirName)
		opts.DirPath = spec.dirPath
		opts.PageFilter = postPages
		// The site's posts share one locale -- the docs-site's default --
		// rather than each carrying its own project's, because they are
		// citizens of the site and sit in one locale search filter.
		localeCode := b.defaultLocaleCode
		opts.LocaleOverride = &localeCode
		opts.CurrentLocale = localeCode

		result, err := build.BuildSingle(opts, b.handle)
		if err != nil {
			return err
		}
		if err := b.writeHTMLFiles(result.HTMLFiles); err != nil {
			return err
		}
	}
	return nil
}

// writeHTMLFiles writes one pass's HTML documents, minified.
//
// The keys are written in sorted order, so a preview names them the same way
// on every run and the auxiliary files read a stable page order.
func (b *unifiedBuild) writeHTMLFiles(htmlFiles map[string]string) error {
	for _, relPath := range sortedKeys(htmlFiles) {
		outPath := filepath.Join(b.outputDir, filepath.FromSlash(relPath))
		if err := b.handle.MkdirAll(filepath.Dir(outPath)); err != nil {
			return err
		}
		if err := b.handle.Write(outPath,
			[]byte(build.MinifyHTML(htmlFiles[relPath])), effects.ModeDefault); err != nil {
			return err
		}
		b.written.add(outPath)
	}
	return nil
}

// copyOtherFiles copies one pass's non-Markdown assets into its own mount.
func (b *unifiedBuild) copyOtherFiles(otherFiles []string, docsDir, mount string) error {
	for _, relPath := range otherFiles {
		src := filepath.Join(docsDir, filepath.FromSlash(relPath))
		dst := filepath.Join(b.outputDir, filepath.FromSlash(mount), filepath.FromSlash(relPath))
		if err := b.handle.MkdirAll(filepath.Dir(dst)); err != nil {
			return err
		}
		if err := b.handle.CopyFile(src, dst); err != nil {
			return err
		}
		b.written.add(dst)
	}
	return nil
}

// unionPaths is the union of two page-path sets.
func unionPaths(first, second map[string]bool) map[string]bool {
	union := make(map[string]bool, len(first)+len(second))
	for path := range first {
		union[path] = true
	}
	for path := range second {
		union[path] = true
	}
	return union
}

// mergeSources overlays extra onto base: a path both carry takes extra's
// content, and a path only extra carries is appended. The result is sorted by
// docs-relative path.
func mergeSources(base, extra []page.SourceFile) []page.SourceFile {
	merged := append([]page.SourceFile(nil), base...)
	for _, source := range extra {
		replaced := false
		for i := range merged {
			if merged[i].MdPath == source.MdPath {
				merged[i] = source
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, source)
		}
	}
	slices.SortFunc(merged, func(a, c page.SourceFile) int {
		return strings.Compare(a.MdPath, c.MdPath)
	})
	return merged
}

// mergeFrontmatter overlays extra's entries onto a copy of base.
func mergeFrontmatter(base, extra map[string]util.Frontmatter) map[string]util.Frontmatter {
	merged := make(map[string]util.Frontmatter, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

// mergePageDates overlays extra's entries onto a copy of base.
func mergePageDates(base, extra map[string]page.PageDates) map[string]page.PageDates {
	merged := make(map[string]page.PageDates, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}
