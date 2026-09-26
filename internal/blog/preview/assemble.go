package preview

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/assembly"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/blog/verify"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/themes"
	"github.com/stricttools/selfdoc/internal/util"
)

// Summary is what a preview assembled: where it wrote, what it served, and
// what verification made of the result.
type Summary struct {
	// OutDir is the preview tree's root, absolute.
	OutDir string
	// SiteDir is the assembled site inside it -- the directory the server
	// serves.
	SiteDir string
	// Home is the slug of the project served at the site root.
	Home string
	// Slugs is every slug the preview serves, sorted.
	Slugs []string
	// Shared is the shared cross-project files that were written.
	Shared []string
	// Report is what verification found. Its failures are reported rather
	// than raised -- see the package documentation.
	Report *verify.VerifyReport
}

// PreviewAssembly assembles every named checkout into outDir and verifies the
// result.
//
// homeDir is the checkout of the home project -- the one served at the site
// root. Required: a site needs a front page. projectDirs are the checkouts of
// the other projects, each served under its own declared slug, and may be
// empty.
//
// outDir is where the preview tree is written. It is refused when it sits
// un-ignored inside a git working tree; see [OutDirRefusal].
//
// canonicalBase is the site's canonical base URL. This is the DEPLOYED base,
// not the loopback address the preview is served from: the pages carry the
// canonicals they would ship with, and verification asserts against those.
//
// build decides whether each checkout's build runs. False previews whatever is
// already in each checkout's build output directory, which is what the suite
// does and what a second look after one edit wants.
//
// theme is a theme name every checkout is built under, overriding each
// project's own configured theme for this preview only. Empty means every
// project keeps its configured theme, which is what a deploy always does. With
// build it reaches each build. Without it, the build trees already on disk have
// to have been produced under that theme, and a checkout whose has not is a
// hard error naming it -- see [BuiltUnderTheme].
func PreviewAssembly(
	homeDir string,
	projectDirs []string,
	outDir string,
	canonicalBase string,
	build bool,
	theme string,
	handle *effects.Handle,
) (*Summary, error) {
	if canonicalBase == "" {
		return nil, errorf(
			"canonical_base is required: it is the base every page's canonical " +
				"and every sitemap entry is written against, and the preview " +
				"asserts the deployed addresses, not the loopback ones.")
	}
	if theme != "" {
		known := themes.List()
		if !slices.Contains(known, theme) {
			return nil, errorf("unknown theme %s; available themes: %s",
				util.PythonRepr(theme), strings.Join(known, ", "))
		}
	}
	if err := RefuseUnsafeOutDir(outDir, handle); err != nil {
		return nil, err
	}

	outDir, err := filepath.Abs(outDir)
	if err != nil {
		return nil, err
	}
	siteDir := filepath.Join(outDir, "site")
	manifestsDir := filepath.Join(outDir, "manifests")

	// The home project builds LAST: its front page renders every other
	// project's live version out of the manifests beside it, so the others
	// have to be grafted before it is built.
	ordered, err := site.ResolveCheckouts(homeDir, projectDirs)
	if err != nil {
		return nil, err
	}
	checkouts := map[string]string{}
	homeSlug := ""
	for _, item := range ordered {
		checkouts[item.Slug] = item.SourceDir
		if item.Home {
			homeSlug = item.Slug
		}
	}

	slugs := make([]string, 0, len(checkouts))
	for slug := range checkouts {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	// A theme reaches a page twice: the build inlines its critical part into
	// the page's own head, and the page then references the site's chrome
	// asset for the rest. With build both come from theme. Without it only
	// the second one can, so the first has to be checked rather than assumed
	// -- otherwise the preview would serve one theme's pages against another
	// theme's stylesheet and look like a rendering bug. The check is an
	// equality, not a guess: it recomputes the stylesheet the build writes and
	// compares.
	if theme != "" && !build {
		var stale []string
		for _, slug := range slugs {
			built, builtErr := BuiltUnderTheme(checkouts[slug], theme)
			if builtErr != nil {
				return nil, builtErr
			}
			if !built {
				stale = append(stale, slug)
			}
		}
		if len(stale) > 0 {
			return nil, errorf(
				"--no-build was given with --theme %s, but the build tree of %s "+
					"was not produced under that theme (or under the theme as it "+
					"stands now). Their pages carry another theme's inlined "+
					"styles, so serving them against %s's stylesheet would look "+
					"like a rendering fault rather than the theme. Rebuild those "+
					"checkouts under --theme %s, or run the preview with --build.",
				util.PythonRepr(theme), strings.Join(stale, ", "), theme, theme)
		}
	}

	if err := handle.MkdirAll(manifestsDir); err != nil {
		return nil, err
	}
	if err := handle.MkdirAll(siteDir); err != nil {
		return nil, err
	}

	// The roster is the assembly's declaration of membership, and a preview
	// declares exactly the checkouts it was given. The repository field is
	// what a deploy checks a dispatch's origin against, which a preview has no
	// dispatch to check, so it records where the content actually came from:
	// this machine.
	rosterPath := filepath.Join(outDir, site.RosterPath)
	entries := make([]site.RosterEntry, 0, len(slugs))
	for _, slug := range slugs {
		entries = append(entries, site.RosterEntry{Slug: slug, Repo: "local/" + slug})
	}
	rosterText := site.RenderRoster(entries, homeSlug)
	if err := handle.Write(rosterPath, []byte(rosterText), effects.ModeDefault); err != nil {
		return nil, err
	}
	roster, err := site.ParseRoster(rosterText, rosterPath)
	if err != nil {
		return nil, err
	}

	// Reconcile first, so a rerun with a project dropped from the command
	// line loses that project's subtree, manifests and record instead of
	// serving a stale copy of it.
	if _, err := site.ReconcileMembership(outDir, roster, handle); err != nil {
		return nil, err
	}

	projectsJSON := filepath.Join(outDir, site.ProjectsPath)
	membership := roster.Entries()
	for _, item := range ordered {
		if build {
			// The preview is the assembled site, so its pages carry the same
			// sibling block a deploy writes, read off the manifests written
			// so far.
			siblings, siblingsErr := assembly.SiblingsFor(
				manifestsDir, outDir, item.Slug,
			)
			if siblingsErr != nil {
				return nil, siblingsErr
			}
			siteName, siteNameErr := assembly.SiteNameFor(
				manifestsDir, outDir,
			)
			if siteNameErr != nil {
				return nil, siteNameErr
			}
			if buildErr := assembly.BuildSourceProject(assembly.BuildOptions{
				SourceDir:    item.SourceDir,
				Scope:        "full",
				Home:         item.Home,
				ManifestsDir: manifestsDir,
				Theme:        theme,
				Siblings:     siblings,
				SiteName:     siteName,
			}, handle); buildErr != nil {
				return nil, buildErr
			}
		}
		if _, graftErr := assembly.ApplyProjectFiles(assembly.GraftOptions{
			AssemblyDir: outDir,
			SourceDir:   item.SourceDir,
			Slug:        item.Slug,
			Scope:       "full",
			Home:        item.Home,
		}, handle); graftErr != nil {
			return nil, graftErr
		}
		version, versionErr := site.DetectLatestVersion(item.SourceDir)
		if versionErr != nil {
			return nil, versionErr
		}
		if _, recordErr := site.RecordMembership(
			projectsJSON, membership, item.Slug, "", "local", version, handle,
		); recordErr != nil {
			return nil, recordErr
		}
	}

	shared, err := assembly.GenerateSharedFiles(assembly.SharedFilesOptions{
		SiteDir:       siteDir,
		ManifestsDir:  manifestsDir,
		CanonicalBase: canonicalBase,
		DocsBase:      canonicalBase,
		HomeSlug:      homeSlug,
		Theme:         theme,
	}, handle)
	if err != nil {
		return nil, err
	}
	if err := assembly.IndexSite(siteDir, handle); err != nil {
		return nil, err
	}

	report, err := verify.VerifyAssembly(outDir, canonicalBase, nil, verify.Now())
	if err != nil {
		return nil, err
	}
	return &Summary{
		OutDir:  outDir,
		SiteDir: siteDir,
		Home:    homeSlug,
		Slugs:   slugs,
		Shared:  shared,
		Report:  report,
	}, nil
}
