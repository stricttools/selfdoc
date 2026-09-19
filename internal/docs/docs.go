// Package docs is the shared resolution pipeline for a project's docs/
// templates: it walks the docs directory, parses each page's frontmatter,
// and resolves every directive the page carries.
//
// The build, the check and gen all start from the same walk, so the answer to
// "what pages does this project have, and what do they say once their
// directives are resolved?" has one definition rather than three.
//
// # One resolved-document type
//
// Doc is that one type. The packages downstream of the walk each read a
// narrower slice of a page -- the manifest wants the frontmatter, the resolved
// text and the raw template; the staleness store wants the frontmatter and the
// raw template -- and each declares its own struct for that slice so it does
// not depend on this package. The conversions live here, so a caller hands the
// walk's result to either of them without restating the mapping.
package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/catalog"
	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/layout"
	"github.com/stricttools/selfdoc/internal/manifest"
	"github.com/stricttools/selfdoc/internal/resolver"
	"github.com/stricttools/selfdoc/internal/staleness"
	"github.com/stricttools/selfdoc/internal/util"
)

// Doc is one docs-tree page, parsed and resolved.
//
// Frontmatter is the page's parsed metadata block, empty when the page carries
// none. Resolved is the body with every directive replaced by what answered
// it. Raw is the same body BEFORE resolution, which is what the content hash
// covers -- so a directive whose output moved does not read as a page edit.
// FrontmatterLines is how many source lines the frontmatter occupied, which is
// what maps a body line number back to its line in the file; it is zero when
// there is no frontmatter.
type Doc struct {
	Frontmatter      util.Frontmatter
	Resolved         string
	Raw              string
	FrontmatterLines int
}

// ManifestDoc narrows d to the slice the manifest writer reads.
func (d Doc) ManifestDoc() manifest.Doc {
	return manifest.Doc{
		Frontmatter: d.Frontmatter,
		Resolved:    d.Resolved,
		Raw:         d.Raw,
	}
}

// StalenessDoc narrows d to the slice the hash store reads.
func (d Doc) StalenessDoc() staleness.Doc {
	return staleness.Doc{
		Frontmatter: d.Frontmatter,
		Raw:         d.Raw,
	}
}

// ManifestDocs converts a whole walk result for the manifest writer, keeping
// every key as it stands.
func ManifestDocs(all map[string]Doc) map[string]manifest.Doc {
	out := make(map[string]manifest.Doc, len(all))
	for relPath, doc := range all {
		out[relPath] = doc.ManifestDoc()
	}
	return out
}

// StalenessDocs converts a whole walk result for the hash store, keeping every
// key as it stands.
//
// The store keys a page by its docs-relative path prefixed with its locale
// when the project declares more than one, so a caller that prefixes does so
// after this call.
func StalenessDocs(all map[string]Doc) map[string]staleness.Doc {
	out := make(map[string]staleness.Doc, len(all))
	for relPath, doc := range all {
		out[relPath] = doc.StalenessDoc()
	}
	return out
}

// ValidNames is the set of directive names this project may use: every
// built-in plus every name the config's "directives" key declares.
//
// A custom name that does not match the directive-name grammar is a
// DirectiveError. The declared names are checked in sorted order, so a config
// with two malformed names always names the same one -- the Python iterated a
// set and named whichever the hash order put first.
func ValidNames(config map[string]any) (directives.NameSet, error) {
	custom := customDirectiveNames(config)
	if err := directives.ValidateDirectiveNames(custom); err != nil {
		return nil, err
	}
	valid := catalog.AllBuiltinDirectives()
	for _, name := range custom {
		valid[name] = struct{}{}
	}
	return valid, nil
}

// customDirectiveNames returns the config's declared custom directive names,
// sorted. A config with no "directives" mapping declares none.
func customDirectiveNames(config map[string]any) []string {
	declared, ok := config["directives"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Roots returns the directories a project's pages come from, in the order
// [ResolveAll] walks them: the handwritten root the config names, then the
// generated root selfdoc writes its own pages into.
//
// docsDir names the handwritten root; pass "" to take it from the config's
// "docs" key, resolved against baseDir. baseDir is the project root every
// relative path in the config resolves against.
//
// Every caller that has to find a page by its docs-relative path asks here,
// so the two-root namespace is declared once rather than re-derived.
func Roots(config map[string]any, docsDir, baseDir string) []string {
	if docsDir == "" {
		docsDir = filepath.Join(baseDir, trimmedConfigPath(config, "docs", layout.DocsDefault))
	}
	return []string{docsDir, layout.Path(baseDir, layout.GeneratedPagesRel)}
}

// FindPage returns the path of the page at relPath -- a docs-relative path
// with forward slashes -- looked up across the roots [Roots] reports, and
// reports whether one of them holds it.
//
// A page keeps its address wherever it is authored, so a caller that reads a
// page off disk must look in both roots or it will report a generated page
// missing.
func FindPage(config map[string]any, docsDir, baseDir, relPath string) (string, bool) {
	for _, root := range Roots(config, docsDir, baseDir) {
		candidate := filepath.Join(root, filepath.FromSlash(relPath))
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// ResolveMarkdown parses and resolves one Markdown source into a Doc.
//
// Every page in a walk result goes through here, whether it came off disk or
// out of an overlay, and so does any caller that has to resolve a page the
// walk never sees.
func ResolveMarkdown(
	content, source string, resolve directives.Resolver, validNames directives.NameSet,
) (Doc, error) {
	block, err := util.ReadFrontmatter(content, source, util.KindPage)
	if err != nil {
		return Doc{}, err
	}
	// The frontmatter's height is the difference in line counts rather than
	// the reader's own count, because that is the number the Python
	// reported and the number every consumer of a body line offset was
	// written against.
	frontmatterLines := strings.Count(content, "\n") - strings.Count(block.Body, "\n")
	resolved, err := directives.ResolveDirectives(block.Body, resolve, validNames)
	if err != nil {
		return Doc{}, err
	}
	return Doc{
		Frontmatter:      block.Values,
		Resolved:         resolved,
		Raw:              block.Body,
		FrontmatterLines: frontmatterLines,
	}, nil
}

// ResolveAll walks a project's docs directory and resolves every .md template
// in it, keyed by each page's path relative to the docs directory with forward
// slashes.
//
// docsDir names the directory to walk; pass "" to take it from the config's
// "docs" key, resolved against baseDir. baseDir is the project root every
// relative path in the config resolves against.
//
// overlay maps a docs-relative path to Markdown source held in memory. Each
// entry is parsed and resolved exactly like a file on disk and then replaces
// (or adds to) the walked result, so a caller can render content that was
// never written -- an editor buffer, or the post pages the build would
// otherwise inject into the docs tree.
//
// Two kinds of file in the tree are not pages: the build output directory,
// which would otherwise feed a previous build's artifacts back in, and an
// underscore-prefixed template, which is a partial included by a page rather
// than a page of its own.
//
// # Two roots, one namespace
//
// A project's pages come from two directories: the handwritten one the config
// names, and the generated one selfdoc writes beside the rest of its generated
// state. A page's key is its path relative to whichever root it came from, so
// the two roots merge into one namespace and a page keeps its address wherever
// it is authored. Two pages that would take the same address are a
// [CollisionError] naming both files, never a silent win for one of them.
//
// Directories and files are read in sorted order. The Python walked in
// directory-listing order, which is arbitrary, and its callers sort where they
// need determinism; the only thing the order decides here is which of two
// unresolvable pages reports its error first.
func ResolveAll(
	config map[string]any,
	docsDir string,
	baseDir string,
	overlay map[string]string,
	handle *effects.Handle,
) (map[string]Doc, error) {
	roots := Roots(config, docsDir, baseDir)
	outputDir := filepath.Join(baseDir, trimmedConfigPath(config, "output", layout.OutputDefault))

	pageResolver, err := resolver.MakeResolver(config, baseDir, handle)
	if err != nil {
		return nil, err
	}
	validNames, err := ValidNames(config)
	if err != nil {
		return nil, err
	}

	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}

	result := map[string]Doc{}
	origin := map[string]string{}
	for _, root := range roots {
		if err := walkDocs(root, root, absOutput, func(relPath, content string) error {
			if previous, taken := origin[relPath]; taken {
				return &CollisionError{
					RelPath: relPath,
					First:   previous,
					Second:  filepath.Join(root, filepath.FromSlash(relPath)),
				}
			}
			doc, err := ResolveMarkdown(content, relPath, pageResolver.Resolve, validNames)
			if err != nil {
				return err
			}
			origin[relPath] = filepath.Join(root, filepath.FromSlash(relPath))
			result[relPath] = doc
			return nil
		}); err != nil {
			return nil, err
		}
	}

	for _, relPath := range sortedKeys(overlay) {
		doc, err := ResolveMarkdown(overlay[relPath], relPath, pageResolver.Resolve, validNames)
		if err != nil {
			return nil, err
		}
		result[relPath] = doc
	}

	return result, nil
}

// trimmedConfigPath reads a directory-valued config key, falling back to
// fallback when it is absent or not a string, and drops the trailing slashes
// the declared form conventionally carries.
func trimmedConfigPath(config map[string]any, key, fallback string) string {
	value, ok := config[key].(string)
	if !ok {
		value = fallback
	}
	return strings.TrimRight(value, "/")
}

// walkDocs visits every .md page under dir, top-down and in sorted order,
// calling visit with the page's path relative to root (forward slashes) and
// its contents.
//
// A directory at or under absOutput is pruned: it holds build output, not
// templates.
func walkDocs(dir, root, absOutput string, visit func(relPath, content string) error) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if absDir == absOutput || strings.HasPrefix(absDir, absOutput+string(filepath.Separator)) {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// A docs directory that does not exist holds no pages, which is
		// what os.walk reported for one too: it yields nothing rather
		// than failing.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var subdirs []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			subdirs = append(subdirs, filepath.Join(dir, name))
			continue
		}
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(root, fullPath)
		if err != nil {
			return err
		}
		if err := visit(filepath.ToSlash(relPath), string(content)); err != nil {
			return err
		}
	}
	for _, subdir := range subdirs {
		if err := walkDocs(subdir, root, absOutput, visit); err != nil {
			return err
		}
	}
	return nil
}

// sortedKeys returns the keys of an overlay in sorted order, so two overlay
// entries that both fail to resolve always report the same one.
func sortedKeys(overlay map[string]string) []string {
	keys := make([]string, 0, len(overlay))
	for key := range overlay {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// CollisionError is two pages, one handwritten and one generated, claiming the
// same address.
//
// The two docs roots merge into one URL namespace, so a page's path relative to
// its own root is its address. Two files that produce one address would make
// the published page depend on which root was walked first; this is the refusal
// instead, and it names both files.
type CollisionError struct {
	// RelPath is the address both files claim, relative to their roots.
	RelPath string
	// First is the file found first, in the handwritten root.
	First string
	// Second is the file found second, in the generated root.
	Second string
}

func (e *CollisionError) Error() string {
	return fmt.Sprintf(
		"two pages claim the address %q: %s and %s. Delete one of them, or rename it: "+
			"a handwritten page and a generated page cannot publish to the same address.",
		e.RelPath, e.First, e.Second)
}
