package sitedirectives

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/listing"
	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// SiteDirectives is the directives this package resolves, in one place so the
// CLI, the build and the verifier all name the same set.
var SiteDirectives = []string{"projects-cards", "blog-highlights"}

// RegionTag is the element one region is wrapped in.
const RegionTag = "selfdoc-region"

// nameAttr is the attribute naming which directive wrote a region.
const nameAttr = "data-directive"

// argPrefix is the prefix a directive's own attributes ride under.
const argPrefix = "data-arg-"

// Error is the failure every operation here reports: a directive that cannot
// be rendered, and a page whose region opens and never closes.
//
// It is the Go counterpart of the RuntimeError the Python surface raised, and
// the one error type a caller needs to recognize with errors.As to render a
// refusal distinctly from an unexpected internal failure.
type Error struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return e.Message }

// errorf builds an [Error] from a format string.
func errorf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// regionRE matches one rendered region, with the paragraph a markdown
// converter may have wrapped around it.
//
// The wrapper is absorbed on re-render: a block element inside a "<p>" is not
// what any browser would keep. A region never contains another, so the
// non-greedy body is unambiguous.
var regionRE = regexp.MustCompile(
	`(?s)(?:<p>` + util.PythonSpaceClass + `*)?<` + RegionTag + `\b([^>]*)>` +
		`(.*?)` +
		`</` + RegionTag + `>(?:` + util.PythonSpaceClass + `*</p>)?`,
)

// openRE matches a region's opening tag, whether or not it is ever closed.
var openRE = regexp.MustCompile(`<` + RegionTag + `\b([^>]*)>`)

// closeRE matches a region's closing tag.
var closeRE = regexp.MustCompile(`</` + RegionTag + `>`)

// attrRE matches one attribute of a region's opening tag.
var attrRE = regexp.MustCompile(`([a-z][a-z0-9-]*)="([^"]*)"`)

// SiteContext is everything a site-level directive reads.
type SiteContext struct {
	// Manifests is every project manifest the assembly holds.
	Manifests []map[string]any

	// SiteHop is the hop from the page being rendered back to the site root
	// ("" at the root, "../" one level in). Every link a region writes is
	// relative to it, so a region resolves under any mount -- the deployed
	// host, a local preview, a mirror -- instead of only under the one base
	// URL the site was configured with. The refresh pass sets it per page,
	// which is the only place the page is known.
	SiteHop string

	// Listing is the home project's curated listing, or nil when it
	// declares none -- which "projects-cards" refuses, naming the file.
	Listing *listing.Listing

	// HomeSlug is the home project, which the listing never includes.
	HomeSlug string
}

// Region is one region a page carries, as the page states it.
//
// It replaces the raw regular-expression match the Python surface handed its
// callers: the verifier reads Name to say which directive a page lost and Body
// to notice that it holds nothing, and the refresh pass reads Attrs to
// re-render with the attributes the directive was written with.
type Region struct {
	// Name is the directive the region declares, or "" when it declares
	// none.
	Name string

	// Attrs is the directive's own attributes, from the region's
	// "data-arg-*" set.
	Attrs map[string]string

	// Body is everything between the region's two tags, verbatim.
	Body string
}

// renderAttrs renders a directive's attributes as the "data-arg-*" set a
// region carries, in sorted key order.
func renderAttrs(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		out.WriteString(" " + argPrefix + key + `="` +
			shared.EscapeHTML(attrs[key]) + `"`)
	}
	return out.String()
}

// parseAttrs returns a region's directive attributes, from its "data-arg-*"
// set.
func parseAttrs(text string) map[string]string {
	attrs := map[string]string{}
	for _, match := range attrRE.FindAllStringSubmatch(text, -1) {
		if !strings.HasPrefix(match[1], argPrefix) {
			continue
		}
		attrs[strings.TrimPrefix(match[1], argPrefix)] =
			html.UnescapeString(match[2])
	}
	return attrs
}

// regionName returns the directive a region declares, or "" when it declares
// none.
func regionName(attrsText string) string {
	for _, match := range attrRE.FindAllStringSubmatch(attrsText, -1) {
		if match[1] == nameAttr {
			return html.UnescapeString(match[2])
		}
	}
	return ""
}

// RenderRegion returns a resolved region: the body between its two tags.
func RenderRegion(name string, attrs map[string]string, context SiteContext) (string, error) {
	body, err := RenderDirectiveBody(name, attrs, context)
	if err != nil {
		return "", err
	}
	return "<" + RegionTag + ` ` + nameAttr + `="` + name + `"` +
		renderAttrs(attrs) + ">" +
		"\n" + body + "\n" +
		"</" + RegionTag + ">", nil
}

// FindRegions returns every region the page carries, in document order.
func FindRegions(pageHTML string) []Region {
	matches := regionRE.FindAllStringSubmatch(pageHTML, -1)
	regions := make([]Region, 0, len(matches))
	for _, match := range matches {
		regions = append(regions, Region{
			Name:  regionName(match[1]),
			Attrs: parseAttrs(match[1]),
			Body:  match[2],
		})
	}
	return regions
}

// RegionNames returns every site-level directive region the page carries.
func RegionNames(pageHTML string) []string {
	regions := FindRegions(pageHTML)
	names := make([]string, 0, len(regions))
	for _, region := range regions {
		names = append(names, region.Name)
	}
	return names
}

// FindUnclosedRegions returns the directives whose region opens and never
// closes.
func FindUnclosedRegions(pageHTML string) []string {
	var opened []string
	for _, match := range openRE.FindAllStringSubmatch(pageHTML, -1) {
		opened = append(opened, regionName(match[1]))
	}
	closed := len(closeRE.FindAllString(pageHTML, -1))
	if closed >= len(opened) {
		return nil
	}
	// The unclosed ones are the trailing openings: a region never nests, so
	// the openings pair with the closings in order.
	seen := map[string]bool{}
	var unclosed []string
	for _, name := range opened[closed:] {
		if seen[name] {
			continue
		}
		seen[name] = true
		unclosed = append(unclosed, name)
	}
	sort.Strings(unclosed)
	return unclosed
}

// RefreshRegions re-renders every site-level region in pageHTML from context.
//
// Idempotent by construction: the tags stay in the output, so the next deploy
// finds the same regions and rewrites their bodies again. A page with no region
// comes back unchanged.
//
// It returns an error naming source when a region opens and never closes, or
// when a region cannot be re-rendered. An empty source names nothing.
func RefreshRegions(pageHTML string, context SiteContext, source string) (string, error) {
	where := ""
	if source != "" {
		where = source + ": "
	}
	if unclosed := FindUnclosedRegions(pageHTML); len(unclosed) > 0 {
		quoted := make([]string, 0, len(unclosed))
		for _, name := range unclosed {
			quoted = append(quoted, util.PythonRepr(name))
		}
		return "", errorf(
			"%sthe site-level region(s) %s open and never close. A region is "+
				"written by selfdoc and delimited by a pair of sentinel "+
				"comments; an unpaired one means the emitted page was edited "+
				"by hand.",
			where, strings.Join(quoted, ", "),
		)
	}

	var out strings.Builder
	last := 0
	for _, span := range regionRE.FindAllStringSubmatchIndex(pageHTML, -1) {
		attrsText := pageHTML[span[2]:span[3]]
		rendered, err := RenderRegion(
			regionName(attrsText), parseAttrs(attrsText), context,
		)
		if err != nil {
			return "", errorf("%s%s", where, err.Error())
		}
		out.WriteString(pageHTML[last:span[0]])
		out.WriteString(rendered)
		last = span[1]
	}
	out.WriteString(pageHTML[last:])
	return out.String(), nil
}

// PageContext is context addressed from the page at pageRel.
//
// pageRel is the page's path relative to the root it is served from, and the
// hop back to that root is how many directories deep it sits. Every caller
// that renders a region into a known page goes through here, so the build-time
// pass and the deploy-time pass cannot disagree about where a region's links
// point.
func PageContext(context SiteContext, pageRel string) SiteContext {
	context.SiteHop = strings.Repeat("../", strings.Count(pageRel, "/"))
	return context
}

// RefreshOutputRegions re-renders every region in every HTML page under
// outputDir.
//
// It returns the output-relative paths that changed. The paths are also the
// addresses: the home project's output root is the site root, both in its own
// build and after the graft, so a page's depth in this tree is the hop its
// links need.
func RefreshOutputRegions(outputDir string, context SiteContext, h *effects.Handle) ([]string, error) {
	changed := []string{}
	err := filepath.WalkDir(outputDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			return nil
		}
		relative, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relative)
		pageHTML, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		refreshed, err := RefreshRegions(
			string(pageHTML), PageContext(context, rel), rel,
		)
		if err != nil {
			return err
		}
		if refreshed == string(pageHTML) {
			return nil
		}
		if err := h.AtomicWrite(path, []byte(refreshed), effects.ModeDefault); err != nil {
			return err
		}
		changed = append(changed, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}
