package html

import (
	"path"
	"regexp"
	"strings"

	"github.com/stricttools/selfdoc/internal/address"
)

// pageRefRE matches a reference attribute whose value may name a page of
// this docs tree.
//
// Only href and src values are rewritten: a ".md" path written anywhere
// else in the body is displayed text -- a code sample, a filename in prose
// -- and rewriting it would corrupt what the page says.
var pageRefRE = regexp.MustCompile(`\b(href|src)="([^"]*)"`)

// mdLinkRE matches the destination of a Markdown inline link or image --
// "[text](target)" and "![alt](target)" -- along with the optional title a
// destination may carry.
var mdLinkRE = regexp.MustCompile(
	`\]\(\s*<?([^()<>\s]*)>?(?:\s+(?:"[^"]*"|'[^']*'|\([^()]*\)))?\s*\)`)

// mdRefDefRE matches the destination of a Markdown reference definition --
// "[id]: target".
var mdRefDefRE = regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]]+\]:[ \t]*<?([^\s<>]+)>?`)

// offsiteRefPrefixes begin a reference that names something other than a
// page of this build: another origin, a scheme that is not a document, the
// current page's own fragment, or an absolute path.
var offsiteRefPrefixes = []string{
	"http://", "https://", "//", "mailto:", "data:", "javascript:", "tel:",
	"#", "/",
}

// PathHop returns the hop that reaches path from the page rendering the
// reference.
//
// A term can be defined on either side of the mount boundary, so the hop
// is chosen per target: prefix reaches the project's own pages and
// sitePrefix the site level, and under a mount those are two different
// roots. It is the answer for every reference that carries a bare target
// path and no unversioned marker: cross-page term links, breadcrumb
// ancestors, the glossary's source links.
func PathHop(p, prefix, sitePrefix string) string {
	if address.IsSiteLevel(p) {
		return sitePrefix
	}
	return prefix
}

// pageRefTarget returns where refPath, written on sourceMd, is emitted --
// as an href.
//
// refPath is relative to the source page's directory in docs/; the built
// page sits one level deeper than its source did ("guide.md" is emitted at
// "guide/index.html"), so the answer is expressed relative to pageDir, the
// emitted page's own directory. The second result is false when the
// reference names something outside the docs tree, which is not this
// build's page to address.
func pageRefTarget(sourceMd, refPath, pageDir string) (string, bool) {
	targetMd := path.Clean(path.Join(path.Dir(sourceMd), refPath))
	if targetMd == ".." || strings.HasPrefix(targetMd, "../") {
		return "", false
	}
	targetURL := HTMLPathToURL(MdToHTMLPath(targetMd))
	base := pageDir
	if base == "" {
		base = "."
	}
	rel := relPath(targetURL, base)
	if strings.HasSuffix(targetURL, "/") {
		if rel == "." {
			rel = "./"
		} else {
			rel += "/"
		}
	}
	return rel, true
}

// relPath is Python's posixpath.relpath for the two slash-separated,
// relative paths this package hands it.
//
// Go's path package has no relpath, and path/filepath.Rel works on the
// host's separator and refuses a pair it considers incomparable; these
// paths are always URL paths under one root, so the shared-prefix walk is
// the whole rule.
func relPath(target, base string) string {
	targetParts := splitPathParts(path.Clean("/" + target))
	baseParts := splitPathParts(path.Clean("/" + base))
	i := 0
	for i < len(targetParts) && i < len(baseParts) && targetParts[i] == baseParts[i] {
		i++
	}
	var out []string
	for range baseParts[i:] {
		out = append(out, "..")
	}
	out = append(out, targetParts[i:]...)
	if len(out) == 0 {
		return "."
	}
	return strings.Join(out, "/")
}

func splitPathParts(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

// RewriteInternalLinks rewrites the page references bodyHTML wrote to their
// emitted addresses.
//
// An author links to checks.md -- a path relative to the source
// file's own directory in docs/. Under directory addressing the page
// writing that link is emitted at "<page>/index.html", one level deeper
// than its source, so the sibling is reached at "../checks/". Every
// reference is therefore resolved back to a source path, mapped through
// [MdToHTMLPath], and re-expressed relative to the emitted page's
// directory: siblings, subdirectory pages, parent pages and the root index
// all come out right from the one rule.
//
// Fragments are kept through the rewrite ("checks.md#detail" becomes
// "../checks/#detail"); a bare "#anchor" addresses the page itself and is
// left as written.
//
// legacyHTMLLinks additionally treats a relative "*.html" reference as
// naming the same page's Markdown source. It is set only for archive
// builds, whose content comes from an immutable git tag and can predate
// this addressing -- links there cannot be fixed at source. A build of the
// working tree never gets that tolerance: source under edit must name
// pages the way the build emits them, and a stale ".html" link there is a
// defect LINK001 reports.
func RewriteInternalLinks(bodyHTML, mdPath string, legacyHTMLLinks bool) string {
	return replaceAllSubmatchFunc(pageRefRE, bodyHTML, func(whole string, groups []string) string {
		attr, ref := groups[1], groups[2]
		emitted := EmittedRef(mdPath, ref, legacyHTMLLinks)
		if emitted == ref {
			return whole
		}
		return attr + `="` + emitted + `"`
	})
}

// EmittedRef returns the href that ref, written on the page whose Markdown
// source is mdPath, is emitted as.
//
// It is the one rule [RewriteInternalLinks] applies, per reference: a
// reference naming a page of this docs tree comes back as the address the
// build gives that page, and every other reference -- another origin, a
// fragment, a path outside the tree, anything that is not a ".md" -- comes
// back as written, which is how the renderer emits it.
//
// legacyHTMLLinks is what [RewriteInternalLinks] documents: it additionally
// treats a relative "*.html" reference as naming the same page's Markdown
// source, and is set only for archive builds.
func EmittedRef(mdPath, ref string, legacyHTMLLinks bool) string {
	if ref == "" {
		return ref
	}
	for _, prefix := range offsiteRefPrefixes {
		if strings.HasPrefix(ref, prefix) {
			return ref
		}
	}
	p, sep, fragment := partition(ref, "#")
	var sourceRef string
	switch {
	case strings.HasSuffix(p, ".md"):
		sourceRef = p
	case legacyHTMLLinks && strings.HasSuffix(p, ".html"):
		sourceRef = strings.TrimSuffix(p, ".html") + ".md"
	default:
		return ref
	}
	pageDir := path.Dir(MdToHTMLPath(mdPath))
	if pageDir == "." {
		pageDir = ""
	}
	target, ok := pageRefTarget(mdPath, sourceRef, pageDir)
	if !ok {
		return ref
	}
	return target + sep + fragment
}

// SourceRefs returns every href a page built now from source, whose Markdown
// path is mdPath, would emit.
//
// A reference is anything the source addresses: a Markdown link or image
// destination, a reference definition's target, or a raw href/src attribute
// written into the Markdown. Each is mapped through [EmittedRef], the same
// rule the renderer applies to the converted body, so the answer is in the
// emitted spelling ("../guide/") rather than the authored one ("guide.md").
//
// It exists so a pass over a BUILT tree can tell a reference the current
// source still writes from one only an older rendering wrote. Comparing an
// emitted href against the Markdown text directly cannot do that: the two are
// never spelled the same.
func SourceRefs(source, mdPath string) map[string]bool {
	refs := map[string]bool{}
	add := func(ref string) {
		if ref == "" {
			return
		}
		refs[EmittedRef(mdPath, ref, false)] = true
	}
	for _, match := range mdLinkRE.FindAllStringSubmatch(source, -1) {
		add(match[1])
	}
	for _, match := range mdRefDefRE.FindAllStringSubmatch(source, -1) {
		add(match[1])
	}
	for _, match := range pageRefRE.FindAllStringSubmatch(source, -1) {
		add(match[2])
	}
	return refs
}

// MdToHTMLPath converts a ".md" path to a directory-index HTML path.
//
// "guide.md" becomes "guide/index.html" (served as "/guide/").
// "index.md" stays "index.html" (the root page, not "index/index.html").
// Subdirectory pages follow the same rule: "api/endpoints.md" becomes
// "api/endpoints/index.html".
func MdToHTMLPath(mdPath string) string {
	stem := strings.TrimSuffix(mdPath, ".md")
	if stem == "index" {
		return "index.html"
	}
	return stem + "/index.html"
}

// HTMLPathToURL converts an HTML file path to its clean URL form.
//
// "guide/index.html" becomes "guide/", and "index.html" stays
// "index.html" (the root page). Used for link hrefs, canonical URLs and
// sitemap entries.
func HTMLPathToURL(htmlPath string) string {
	if htmlPath == "index.html" {
		return "index.html"
	}
	if strings.HasSuffix(htmlPath, "/index.html") {
		return strings.TrimSuffix(htmlPath, "index.html")
	}
	return htmlPath
}

// HTMLToMdPath is the reverse of [MdToHTMLPath].
//
// "guide/index.html" becomes "guide.md", "index.html" becomes "index.md",
// and "api/endpoints/index.html" becomes "api/endpoints.md".
func HTMLToMdPath(htmlPath string) string {
	if htmlPath == "index.html" {
		return "index.md"
	}
	if strings.HasSuffix(htmlPath, "/index.html") {
		return strings.TrimSuffix(htmlPath, "/index.html") + ".md"
	}
	// Fallback for a path that is not a directory index.
	return strings.ReplaceAll(htmlPath, ".html", ".md")
}

// partition splits s at the first occurrence of sep, the way Python's
// str.partition does: the part before, the separator itself (empty when
// absent), and the part after.
func partition(s, sep string) (string, string, string) {
	i := strings.Index(s, sep)
	if i < 0 {
		return s, "", ""
	}
	return s[:i], sep, s[i+len(sep):]
}

// replaceAllSubmatchFunc is regexp's ReplaceAllStringFunc with the
// submatches the callback needs.
//
// The standard library hands the callback only the whole match, so a
// substitution that reads capture groups -- which every port of a Python
// re.sub with a function replacement does -- has to locate them itself.
func replaceAllSubmatchFunc(re *regexp.Regexp, s string, repl func(whole string, groups []string) string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		groups := make([]string, len(m)/2)
		for g := range groups {
			if m[2*g] < 0 {
				continue
			}
			groups[g] = s[m[2*g]:m[2*g+1]]
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(repl(groups[0], groups))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}
