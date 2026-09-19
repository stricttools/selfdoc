package listing

import (
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/util"
)

// CheckAgainst returns an error unless every listed slug can actually be
// rendered, naming source in the diagnostic.
//
// Two failures, both naming the slug: an entry the assembly is supposed to
// serve but has no manifest for, and the home project listing itself -- the
// front page is not one of the projects the front page lists.
func CheckAgainst(listing Listing, manifests []map[string]any, homeSlug string, source string) error {
	known := map[string]bool{}
	for _, manifest := range manifests {
		known[util.PythonStrOrEmpty(manifest["slug"])] = true
	}
	missing := make([]string, 0)
	for _, entry := range listing.Entries() {
		if !entry.Project.External() && !known[entry.Project.Slug] {
			missing = append(missing, entry.Project.Slug)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		names := make([]string, 0, len(known))
		for slug := range known {
			names = append(names, slug)
		}
		sort.Strings(names)
		served := strings.Join(names, ", ")
		if served == "" {
			served = "(none)"
		}
		return errorf(
			"%s lists %s, which the assembly has no manifest for, so the "+
				"listing would print a card for a project this site does not "+
				"serve. Either the project has never deployed, or the entry "+
				"names an external project and is missing its 'url' and "+
				"'name'. Served projects: %s.",
			source, strings.Join(missing, ", "), served,
		)
	}
	if homeSlug != "" {
		for _, slug := range listing.Slugs() {
			if slug == homeSlug {
				return errorf(
					"%s lists %s, which is the home project -- the page the "+
						"listing appears on. The home project is left out of "+
						"the listing it renders.",
					source, util.PythonRepr(homeSlug),
				)
			}
		}
	}
	return nil
}

// RenderHTML returns the curated listing as one HTML fragment.
//
// This is the single renderer behind both surfaces: the generated /projects/
// page passes a heading, the front page's cards directive does not. Version
// badges come from the manifests, so a card is as current as the last deploy
// of the project it names.
//
// siteHop is the hop from the page holding the fragment back to the site root,
// and every card for a project the site serves is addressed through it. A card
// for an external project keeps the absolute URL the listing declares: that
// one really does name somebody else's server.
//
// The cards are stated in the framework's own card vocabulary --
// .card-grid for the responsive grid, .card for the box,
// .card-title-row/.card-title for the head, .card-badges and .badge for the
// version chip -- with .project-* hooks riding alongside for the rules only a
// project card needs. A private class surface no theme knew about is what made
// these render as full-width unstyled boxes.
func RenderHTML(listing Listing, manifests []map[string]any, siteHop string, homeSlug string, heading string) (string, error) {
	if err := CheckAgainst(listing, manifests, homeSlug, SourceFile); err != nil {
		return "", err
	}
	bySlug := map[string]map[string]any{}
	for _, manifest := range manifests {
		bySlug[util.PythonStrOrEmpty(manifest["slug"])] = manifest
	}

	parts := []string{`<section class="project-list">`}
	if heading != "" {
		parts = append(parts, "  <h1>"+escape(heading)+"</h1>")
	}
	for _, category := range listing.Categories {
		parts = append(parts, `  <section class="project-category">`)
		parts = append(parts, "    <h2>"+escape(category.Name)+"</h2>")
		parts = append(parts, `    <div class="card-grid project-grid">`)
		for _, project := range category.Projects {
			var name, href, version string
			if project.External() {
				name = project.Name
				href = project.URL
			} else {
				manifest := bySlug[project.Slug]
				name = util.PythonStrOrEmpty(manifest["name"])
				if name == "" {
					name = project.Slug
				}
				href = siteHop + project.Slug + "/"
				version = util.PythonStrOrEmpty(manifest["version"])
				if version == config.UnversionedVersion {
					// A project that declares it has no public version
					// deploys under the literal. It is not a version, so
					// there is no badge to draw for it.
					version = ""
				}
			}
			parts = append(parts, `      <article class="card project-card">`)
			parts = append(parts, `        <div class="card-title-row">`)
			parts = append(parts,
				`          <h3 class="card-title">`+
					`<a href="`+escape(href)+`">`+
					escape(name)+"</a></h3>",
			)
			parts = append(parts, "        </div>")
			badges := make([]string, 0, 2)
			if version != "" {
				label := "v" + version
				if version == "0.0.0" {
					label = "monorepo"
				}
				badges = append(badges,
					`<span class="badge badge-neutral version-badge">`+
						escape(label)+"</span>",
				)
			}
			if project.External() {
				badges = append(badges,
					`<span class="badge badge-neutral external-badge">`+
						"external</span>",
				)
			}
			if len(badges) > 0 {
				parts = append(parts,
					`        <div class="card-badges">`+
						strings.Join(badges, "")+"</div>",
				)
			}
			parts = append(parts,
				`        <p class="project-blurb">`+
					escape(project.Blurb)+"</p>",
			)
			if project.Repo != "" {
				// The arrow is what makes the line read as a link rather than
				// as a label; it is decorative, so it is hidden from the
				// accessibility tree and the link's name stays "Repository".
				parts = append(parts,
					`        <a class="project-repo" `+
						`href="`+escape(project.Repo)+`">Repository`+
						`<span aria-hidden="true">&#8599;</span></a>`,
				)
			}
			parts = append(parts, "      </article>")
		}
		parts = append(parts, "    </div>")
		parts = append(parts, "  </section>")
	}
	parts = append(parts, "</section>")
	return strings.Join(parts, "\n"), nil
}
