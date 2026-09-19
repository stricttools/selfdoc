package build

import (
	"sort"

	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/urls"
	"github.com/stricttools/selfdoc/internal/util"
)

// sortedSources returns the sources ordered by their docs-relative path.
//
// The Python this replaces iterated insertion-ordered dicts whose order came
// out of os.walk; every collection in this package is sorted instead, so a
// build's output does not depend on a directory listing.
func sortedSources(sources []page.SourceFile) []page.SourceFile {
	sorted := append([]page.SourceFile(nil), sources...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MdPath < sorted[j].MdPath })
	return sorted
}

// sourceContent returns the content of one source by path, "" when the
// collection does not hold it.
func sourceContent(sources []page.SourceFile, mdPath string) string {
	for _, src := range sources {
		if src.MdPath == mdPath {
			return src.Content
		}
	}
	return ""
}

// mergeSources overlays extra onto base: a path both carry takes extra's
// content, and a path only extra carries is appended. The result is sorted.
//
// This is what the Python's dict-merge spelling did when the build folded the
// unversioned and site-level pages into the versioned ones for the site-level
// files.
func mergeSources(base, extra []page.SourceFile) []page.SourceFile {
	merged := append([]page.SourceFile(nil), base...)
	for _, src := range extra {
		replaced := false
		for i := range merged {
			if merged[i].MdPath == src.MdPath {
				merged[i] = src
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, src)
		}
	}
	return sortedSources(merged)
}

// filterSources keeps the sources whose path the filter names. A nil filter
// keeps everything.
func filterSources(sources []page.SourceFile, filter map[string]bool) []page.SourceFile {
	if filter == nil {
		return sources
	}
	kept := make([]page.SourceFile, 0, len(sources))
	for _, src := range sources {
		if filter[src.MdPath] {
			kept = append(kept, src)
		}
	}
	return kept
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

// MakeURLBuilder returns the URL builder a project's config asks for.
//
// A topology block naming both a docs base and a slug gets the topology
// builder, which mounts the project under its slug and serves the site-level
// pages from the site root. Otherwise a declared base URL gets the simple
// builder. A config declaring neither gets nil, and every caller that emits an
// absolute URL refuses rather than inventing one.
func MakeURLBuilder(config map[string]any) urls.URLBuilder {
	topology, _ := config["topology"].(map[string]any)
	docsBase := util.PythonStrOrEmpty(topology["docs_base"])
	slug := util.PythonStrOrEmpty(topology["slug"])
	if docsBase != "" && slug != "" {
		return urls.NewTopologyURLBuilder(docsBase, slug)
	}
	if baseURL := util.PythonStrOrEmpty(config["base_url"]); baseURL != "" {
		return urls.NewSimpleURLBuilder(baseURL)
	}
	return nil
}
