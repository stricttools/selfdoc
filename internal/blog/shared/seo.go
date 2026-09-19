package shared

import (
	"encoding/json"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// DescriptionMax is the longest meta description a search result renders
// before it truncates, and the ceiling every description generated here is
// clamped to.
const DescriptionMax = 160

// SiteName is the name the assembled site goes by: the home project's display
// name, which is the name on the front page a reader arrives at.
//
// A tree that declares no home project has no front page of its own, so it
// falls back to the same title the aggregated feed carries.
func SiteName(manifests []map[string]any, homeSlug string) string {
	if homeSlug != "" {
		for _, manifest := range manifests {
			if util.PythonStrOrEmpty(manifest["slug"]) != homeSlug {
				continue
			}
			if name := util.PythonStrOrEmpty(manifest["name"]); name != "" {
				return name
			}
		}
	}
	return DefaultFeedTitle
}

// ProjectsDescription is the meta description of the project listing: the site
// by name, what the listing is, and as many of the listed projects as fit.
//
// The names are what a reader searching for one of these tools types, so they
// are the part of the sentence that earns the page a result. They are added
// whole, in listing order, until the next one would push the sentence past
// [DescriptionMax] -- never cut mid-name, and never left with a dangling
// separator.
func ProjectsDescription(manifests []map[string]any, homeSlug string) string {
	lead := SiteName(manifests, homeSlug) +
		" carries the documentation of every project published on this " +
		"site, each kept current with its own releases."
	sentence := lead + " Projects: "
	added := 0
	for _, manifest := range sortedByName(manifests, homeSlug) {
		name := util.PythonStrOrEmpty(manifest["name"])
		if name == "" {
			continue
		}
		separator := ""
		if added > 0 {
			separator = ", "
		}
		// The closing period is part of what has to fit.
		if len([]rune(sentence+separator+name))+1 > DescriptionMax {
			break
		}
		sentence += separator + name
		added++
	}
	if added == 0 {
		return clampDescription(lead)
	}
	return clampDescription(sentence + ".")
}

// BlogDescription is the meta description of the site-wide blog index.
//
// It counts nothing. A post count in a description is wrong on the next post
// published, and nothing regenerates a search engine's cached copy of it.
func BlogDescription(manifests []map[string]any, homeSlug string) string {
	return clampDescription(
		"The blog of " + SiteName(manifests, homeSlug) +
			": release notes, design notes and engineering write-ups " +
			"from every project whose documentation this site publishes.",
	)
}

// clampDescription cuts a description to [DescriptionMax] at a word boundary
// and closes it with a period, leaving no dangling separator.
func clampDescription(text string) string {
	runes := []rune(text)
	if len(runes) <= DescriptionMax {
		return text
	}
	cut := string(runes[:DescriptionMax])
	if space := strings.LastIndex(cut, " "); space > 0 {
		cut = cut[:space]
	}
	cut = strings.TrimRight(cut, " ,;:.-")
	return cut + "."
}

// crumb is one BreadcrumbList entry. The field order is the order the document
// states them in, which is what the JSON encoder writes.
type crumb struct {
	Type     string `json:"@type"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Item     string `json:"item"`
}

// breadcrumbList is the trail from the site root to the page.
type breadcrumbList struct {
	Type            string  `json:"@type"`
	ItemListElement []crumb `json:"itemListElement"`
}

// collectionPage is the structured-data document a shared listing page
// carries: what the page collects, and where it sits on the site.
type collectionPage struct {
	Context     string         `json:"@context"`
	Type        string         `json:"@type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	URL         string         `json:"url"`
	Breadcrumb  breadcrumbList `json:"breadcrumb"`
}

// CollectionPageJSONLD is the structured data for one of the site's listing
// pages: a CollectionPage carrying the trail that reaches it.
//
// Every URL in it is absolute under canonicalBase. A breadcrumb URL is a
// crawler's statement of where a page sits on a site, not a link a reader
// clicks, so it is the one place on these pages an absolute address is the
// right answer -- the rendered links stay document-relative.
//
// pathSegment is the page's one path segment under the site root ("projects",
// "blog").
func CollectionPageJSONLD(name, description, canonicalBase, pathSegment string) (string, error) {
	base := strings.TrimRight(canonicalBase, "/") + "/"
	pageURL := base + pathSegment + "/"
	doc := collectionPage{
		Context:     "https://schema.org",
		Type:        "CollectionPage",
		Name:        name,
		Description: description,
		URL:         pageURL,
		Breadcrumb: breadcrumbList{
			Type: "BreadcrumbList",
			ItemListElement: []crumb{
				{Type: "ListItem", Position: 1, Name: "Home", Item: base},
				{Type: "ListItem", Position: 2, Name: name, Item: pageURL},
			},
		},
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
