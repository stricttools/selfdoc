package site

import (
	"path/filepath"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// Checkout is one project checkout a command was handed, under the slug its
// selfdoc.json declares.
type Checkout struct {
	// Slug is the project's address on the assembled site.
	Slug string
	// SourceDir is the checkout, absolute.
	SourceDir string
	// Home reports whether this is the home project: the one served at the
	// site root rather than under its slug.
	Home bool
}

// ResolveCheckouts reads the slug every named checkout declares and returns
// the checkouts in the order they are built: every project in the order given,
// then the home project last, since its front page renders every other
// project's facts out of their manifests.
//
// homeDir is the home project's checkout and is required. Two checkouts
// declaring one slug are refused: one slug is one project's section of the
// site.
func ResolveCheckouts(homeDir string, projectDirs []string) ([]Checkout, error) {
	homeDir, err := filepath.Abs(homeDir)
	if err != nil {
		return nil, err
	}
	homeSlug, err := ReadSlug(homeDir)
	if err != nil {
		return nil, err
	}
	taken := map[string]string{homeSlug: homeDir}
	var ordered []Checkout
	for _, raw := range projectDirs {
		sourceDir, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		slug, err := ReadSlug(sourceDir)
		if err != nil {
			return nil, err
		}
		if existing, clash := taken[slug]; clash {
			return nil, errorf(
				"two checkouts declare the slug %s: %s and %s. One slug is one "+
					"project's section of the site, so both cannot be served.",
				util.PythonRepr(slug), existing, sourceDir)
		}
		taken[slug] = sourceDir
		ordered = append(ordered, Checkout{Slug: slug, SourceDir: sourceDir})
	}
	return append(ordered, Checkout{Slug: homeSlug, SourceDir: homeDir, Home: true}), nil
}

// ReadSlug returns the assembly slug the project at sourceDir declares.
func ReadSlug(sourceDir string) (string, error) {
	cfg, err := config.Load(sourceDir)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", errorf(
			"%s carries no selfdoc.json, so it is not a project the assembly "+
				"can serve.", sourceDir)
	}
	topology, _ := cfg["topology"].(map[string]any)
	slug := util.PythonStrip(util.PythonStrOrEmpty(topology["slug"]))
	if slug == "" {
		return "", errorf(
			"%s/selfdoc.json declares no topology.slug, so there is no address "+
				"to publish it at. The slug is the project's path segment on "+
				"the assembled site.", sourceDir)
	}
	return slug, nil
}
