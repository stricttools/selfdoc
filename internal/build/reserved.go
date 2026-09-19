package build

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
	"github.com/stricttools/selfdoc/internal/util"
)

// reservedTopSegments are the top-level segments of a mount that belong to the
// build rather than to the author, each mapped to what the build does with it:
//
//   - "v" is where superseded versions are emitted ("v/<version>/<page>/"), so
//     a page named "v" would collide with the whole archive tree.
//   - "blog" is where posts are emitted, at the site level -- and where post
//     injection WRITES, which is why an authored page there is a file the
//     build would overwrite and then delete.
var reservedTopSegments = map[string]string{
	address.ArchivePrefix: fmt.Sprintf(
		"superseded versions are emitted at '%s/<version>/<page>/'",
		address.ArchivePrefix),
	address.PostsPrefix: "posts are emitted at 'blog/<slug>/'",
}

// CheckReservedPagePaths refuses a page whose top-level segment is a reserved
// one, naming the page and the segment.
//
// The paths are checked in sorted order, so a docs tree with two offending
// pages always names the same one.
func CheckReservedPagePaths(mdPaths map[string]bool) error {
	paths := make([]string, 0, len(mdPaths))
	for mdPath := range mdPaths {
		paths = append(paths, mdPath)
	}
	sort.Strings(paths)
	for _, mdPath := range paths {
		first, _, _ := strings.Cut(mdPath, "/")
		stem := strings.TrimSuffix(first, ".md")
		if reason, reserved := reservedTopSegments[stem]; reserved {
			return fmt.Errorf(
				"Page '%s' uses the reserved top-level path '%s': %s. Rename the page.",
				mdPath, stem, reason)
		}
	}
	return nil
}

// CheckReservedAuthoredPages refuses an authored page sitting where the build
// writes its own files.
//
// [CheckReservedPagePaths] runs on the partitioned page sets, and the
// site-level partition holds the pages injection just wrote, so it can never
// see an AUTHORED blog.md: injection had already overwritten it, and cleanup
// deleted it afterwards. This reads the tree itself, before anything writes
// into it, and refuses the file.
//
// docsDir is a docs tree (a locale's, or the root one injection writes into);
// baseDir is what the reported path is relative to, so the message names the
// file the way its author does. Pass "" for baseDir to report relative to
// docsDir.
func CheckReservedAuthoredPages(docsDir, baseDir string) error {
	stems := make([]string, 0, len(reservedTopSegments))
	for stem := range reservedTopSegments {
		stems = append(stems, stem)
	}
	sort.Strings(stems)

	relativeTo := baseDir
	if relativeTo == "" {
		relativeTo = docsDir
	}

	for _, stem := range stems {
		var found []string
		pageFile := filepath.Join(docsDir, stem+".md")
		if isFile(pageFile) {
			found = append(found, pageFile)
		}
		subdir := filepath.Join(docsDir, stem)
		if isDir(subdir) {
			nested, err := markdownFilesUnder(subdir)
			if err != nil {
				return err
			}
			found = append(found, nested...)
		}
		for _, path := range found {
			rel, err := filepath.Rel(relativeTo, path)
			if err != nil {
				return err
			}
			return fmt.Errorf(
				"Authored page '%s' sits on the reserved top-level path '%s': %s. "+
					"The build writes that path itself, so building would overwrite "+
					"the file and then delete it. Move or rename the page.",
				filepath.ToSlash(rel), stem, reservedTopSegments[stem])
		}
	}
	return nil
}

// markdownFilesUnder lists every .md file under root, in sorted order.
func markdownFilesUnder(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

// IsSiteLevelPage reports whether a docs-relative page is site-level -- a post
// or their listing.
//
// Site-level pages are built with no mount at all: the posts under
// "blog/<slug>.md" and the listing at "blog.md", which is emitted at "blog/"
// beside them.
func IsSiteLevelPage(mdPath string) bool {
	first, _, _ := strings.Cut(mdPath, "/")
	return strings.TrimSuffix(first, ".md") == address.PostsPrefix
}

// CheckPostSlugUniqueness refuses two posts claiming the same site-level
// address.
//
// Posts are emitted at "blog/<slug>/" with no project segment, so on a site
// assembled from several projects the slug namespace is shared. A repeat would
// silently overwrite one post with another, so it is an error naming both
// sources.
//
// Each claim pairs a slug with whatever produced the post -- a project slug, a
// file path.
func CheckPostSlugUniqueness(claims []SlugClaim) error {
	seen := map[string]string{}
	for _, claim := range claims {
		if previous, repeat := seen[claim.Slug]; repeat {
			return fmt.Errorf(
				"Duplicate post slug %s: claimed by both %s and %s. Posts are "+
					"emitted at '%s/<slug>/' with no project segment, so a slug "+
					"must be unique across every project on the site.",
				util.PythonRepr(claim.Slug), util.PythonRepr(previous), util.PythonRepr(claim.Source),
				address.PostsPrefix)
		}
		seen[claim.Slug] = claim.Source
	}
	return nil
}

// SlugClaim is one post's claim on a site-level address: the slug it wants and
// what produced it.
type SlugClaim struct {
	// Slug is the address segment the post is emitted under.
	Slug string
	// Source names whatever produced the post -- a project slug, a file path.
	Source string
}
