package sitedirectives

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/util"
)

// HomeListingPath returns where the home project declares its curated
// listing.
func HomeListingPath(dirPath string, cfg map[string]any) string {
	docsDir := util.PythonStrOrEmpty(cfg["docs"])
	if docsDir == "" {
		docsDir = layout.DocsDefault
	}
	return filepath.Join(dirPath, strings.TrimRight(docsDir, "/"), "projects.toml")
}

// BuildHomeProject builds the home project with the assembly's data in scope.
//
// This is the only build that can resolve a site-level directive, and the
// refusals below are why: the manifests are what a version badge and a post
// highlight are read from, and no project's own repository holds them. A
// missing context stops the build before a page is written -- there is no
// rendering of an empty region and no placeholder.
//
// Directive resolution happens per markdown source and never learns which
// emitted page it is writing into, so the regions come out of the build
// addressed from the output root. [RefreshOutputRegions] then re-renders each
// one against its own page's hop -- the same pass the assembly runs on every
// deploy, run here so the home project's own build output is correct on its
// own.
//
// The project's selfdoc.json is loaded here rather than taken as an argument,
// because this entry point's whole input is a directory: the checkout and the
// assembly's manifests directory. A caller that already holds both the config
// and the assembly's manifests calls [BuildHome].
func BuildHomeProject(
	dirPath string,
	siteManifests string,
	theme string,
	includeDrafts bool,
	h *effects.Handle,
) (map[string]bool, error) {
	if siteManifests == "" {
		return nil, errorf(
			"--site-manifests is required by --target home: the home "+
				"project's pages carry site-level directives (%s) that render "+
				"from the assembly's manifests, and this repository holds "+
				"none of them. Point it at the assembly checkout's "+
				"manifests/ directory.",
			strings.Join(SiteDirectives, ", "),
		)
	}
	if info, err := os.Stat(siteManifests); err != nil || !info.IsDir() {
		return nil, errorf(
			"--site-manifests names %s, which is not a directory. It is the "+
				"assembly checkout's manifests/ directory.",
			util.PythonRepr(siteManifests),
		)
	}

	cfg, err := config.Load(dirPath)
	if err != nil {
		return nil, err
	}
	manifests, err := site.LoadAssemblyManifests(siteManifests)
	if err != nil {
		return nil, err
	}
	context, err := HomeContext(dirPath, cfg, manifests)
	if err != nil {
		return nil, err
	}
	return BuildHome(dirPath, cfg, context, theme, includeDrafts, h)
}

// HomeContext is the site context the home project's directives resolve
// against: the assembly's manifests, and the listing this project curates.
//
// The listing is read from the project's own docs/projects.toml rather than
// from the copy the assembly keeps, because that file is the authored source
// and the copy is what a deploy wrote from it. A build and a check of the same
// working tree therefore answer from the same document.
func HomeContext(
	dirPath string,
	cfg map[string]any,
	manifests []map[string]any,
) (SiteContext, error) {
	var curated *listing.Listing
	listingPath := HomeListingPath(dirPath, cfg)
	if info, err := os.Stat(listingPath); err == nil && info.Mode().IsRegular() {
		loaded, err := listing.Load(listingPath)
		if err != nil {
			return SiteContext{}, err
		}
		curated = &loaded
	}
	return SiteContext{
		Manifests: manifests,
		Listing:   curated,
		HomeSlug:  util.PythonStrOrEmpty(topology(cfg)["slug"]),
	}, nil
}

// BuildHome builds the home project with an already-resolved site context.
//
// This is [BuildHomeProject] without the directory read: a caller that got the
// assembly's manifests from somewhere other than a checkout -- the Git Data
// API, say -- has the context already and builds through here.
//
// The site-level directives are registered into the config the build runs on
// rather than into the document on disk, which stays a document.
func BuildHome(
	dirPath string,
	cfg map[string]any,
	context SiteContext,
	theme string,
	includeDrafts bool,
	h *effects.Handle,
) (map[string]bool, error) {
	buildConfig := RegisterSiteDirectives(cfg, Directives(&context))

	// The home project's own pages carry the same sibling block every other
	// project's do, read off the context it is already building against.
	written, err := build.Build(build.Options{
		DirPath:       dirPath,
		Config:        buildConfig,
		IncludeDrafts: includeDrafts,
		Theme:         theme,
		Siblings: build.SiblingsFromManifests(
			context.Manifests, context.HomeSlug, context.HomeSlug,
		),
		// The home project's name is the site's name, so its own pages end
		// their titles with it exactly as every other project's do.
		SiteName: siteNameOf(context),
	}, h)
	if err != nil {
		return nil, err
	}

	outputDir := util.PythonStrOrEmpty(cfg["output"])
	if outputDir == "" {
		outputDir = layout.OutputDefault
	}
	if _, err := RefreshOutputRegions(
		filepath.Join(dirPath, strings.TrimRight(outputDir, "/")), context, h,
	); err != nil {
		return nil, err
	}
	return written, nil
}

// siteNameOf is the name the assembled site goes by, for the titles the home
// project's pages carry: the home project's own manifest name.
//
// A context naming no home project describes no site, so there is no name to
// end a title with and the pages read as a standalone deploy's.
func siteNameOf(context SiteContext) string {
	if context.HomeSlug == "" {
		return ""
	}
	return shared.SiteName(context.Manifests, context.HomeSlug)
}

// RegisterSiteDirectives returns cfg with registered added to its "directives"
// mapping, leaving cfg itself alone.
//
// A name the project declares for itself wins: the config document is the
// project's own declaration, and a registration that overwrote it would
// silently replace a script the author wrote.
func RegisterSiteDirectives(
	cfg map[string]any,
	registered map[string]resolver.BuiltinDirective,
) map[string]any {
	merged := make(map[string]any, len(cfg)+1)
	for key, value := range cfg {
		merged[key] = value
	}
	declared := map[string]any{}
	if own, isObject := cfg["directives"].(map[string]any); isObject {
		for name, script := range own {
			declared[name] = script
		}
	}
	for name, directive := range registered {
		if _, taken := declared[name]; taken {
			continue
		}
		declared[name] = directive
	}
	merged["directives"] = declared
	return merged
}

// topology is the config's topology block, or an empty one when it declares
// none.
func topology(cfg map[string]any) map[string]any {
	if block, isObject := cfg["topology"].(map[string]any); isObject {
		return block
	}
	return map[string]any{}
}
