package unified

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/html"
)

// projectCard is one constituent project's entry on the landing page.
type projectCard struct {
	// Slug is the URL segment the project is mounted under.
	Slug string
	// NavTitle is the heading the card carries.
	NavTitle string
	// Description is the project's own config description, "" when it
	// states none.
	Description string
	// Version is the version the project's pages were built from.
	Version string
	// Home is the project's home page, as a path from the output root.
	Home string
}

// generateLandingPage renders the landing page's body: one card per
// constituent project, in the order they were built.
//
// toSiteRoot is the hop from the landing page's own directory back to the
// output root. Every card addresses a page in a different mount, so it climbs
// out to the root and back in; a site-root path would only resolve if the site
// were served from an origin root, which an assembly's mount and a GitHub
// Pages project site both are not.
func generateLandingPage(projectsInfo []projectCard, toSiteRoot string) string {
	cards := make([]string, 0, len(projectsInfo))
	for _, info := range projectsInfo {
		title := html.EscapeHTML(info.NavTitle)
		desc := html.EscapeHTML(info.Description)
		version := html.EscapeHTML(info.Version)
		link := toSiteRoot + info.Home
		card := `<div class="project-card">` +
			`<h3><a href="` + link + `">` + title + `</a></h3>`
		if desc != "" {
			card += `<p>` + desc + `</p>`
		}
		if version != "" {
			card += `<span class="project-version">v` + version + `</span>`
		}
		card += `</div>`
		cards = append(cards, card)
	}
	return `<div class="project-grid">` + strings.Join(cards, "\n") + `</div>`
}

// projectGridCSS styles the landing page's project cards. It is appended to
// the shared stylesheet, which is the only sheet a unified site serves.
const projectGridCSS = `.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 1.5rem;
  margin: 2rem 0;
}
.project-card {
  border: 1px solid var(--border, #e0e0e0);
  border-radius: 8px;
  padding: 1.5rem;
  transition: box-shadow 0.2s;
}
.project-card:hover {
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}
.project-card h3 {
  margin: 0 0 0.5rem 0;
}
.project-card h3 a {
  text-decoration: none;
  color: var(--link, #0969da);
}
.project-card p {
  margin: 0 0 0.5rem 0;
  color: var(--text-secondary, #666);
}
.project-version {
  font-size: 0.85em;
  color: var(--text-secondary, #666);
  background: var(--code-bg, #f6f8fa);
  padding: 0.1em 0.4em;
  border-radius: 3px;
}
.term-project {
  font-size: 0.85em;
  color: var(--text-secondary, #666);
}
`
