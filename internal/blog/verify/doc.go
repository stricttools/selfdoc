// Package verify answers whether a built assembly tree is fit to deploy.
//
// The deploy used to be the first reader of the tree it published: the graft,
// the shared generator and the search index each did their part, the result
// was committed, and whatever was wrong with it became the live site. This
// package is the reading that happens before the push -- one pass over the
// assembled tree asserting every property the site depends on, each failure
// naming its offender.
//
// # What is asserted
//
//   - Membership agrees in both directions. The declared roster, the site/
//     subtrees and the files under manifests/ name the same projects: no
//     undeclared subtree, no declared project missing, no orphan manifest of
//     any kind.
//   - Each manifest describes the tree it sits next to. Its slug names its own
//     directory, its version is the version the emitted pages carry (and is
//     not sitting in the archive tree as though it were superseded), and every
//     page and post it lists resolves to a file that exists.
//   - The shared artifacts exist, parse, and say what they are for. Front
//     page, blog index, project listing, nav.json, sitemap, feed, search
//     index, robots, llms.txt and the root 404. Three of those are asserted on
//     their content rather than their existence: the 404 body is not the front
//     page's and offers a way back, robots.txt names the sitemap the tree
//     actually carries, and llms.txt references every declared project's own
//     llms.txt.
//   - Every reference resolves. Internal links, canonicals, sitemap entries
//     and feed links all go through the resolution package -- the same LINK001
//     pass a single project's build is checked with, run over the assembled
//     tree.
//   - Every page is addressable. A title, and a canonical under the site's
//     canonical base.
//   - Every page is styled by the site's own chrome. The site-level stylesheet
//     is in the tree and every page links it. A page linking a copy inside its
//     own subtree, or none at all, fails: the shared pages published as
//     unstyled HTML for as long as nothing asserted this.
//   - Nothing half-built or per-project leaked in. No unresolved directive
//     markers, and none of the per-project routing artifacts the graft filters
//     out.
//   - Cross-project links land somewhere. Extracted from the emitted pages and
//     checked against what the manifests say exists.
//   - Every project is reachable. Following clickable links from the site
//     root and the project listing arrives at every declared project's index
//     page, so no project is published at an address a reader arriving at
//     the site never sees and a crawler following links never reaches. Either
//     arrival page may curate what it shows. The home project is exempt: the
//     site root is its own front page.
//   - Outbound links still answer, when the assembly declares a list of pages
//     to check them on. See [github.com/stricttools/selfdoc/internal/blog/site.LoadOutbound].
//
// The outbound cache is the one piece of state a verification produces.
// Verification itself never writes: [VerifyAssembly] returns the updated cache
// and the caller decides whether to keep it, which is why the "assembly
// verify" command is read-only and the deploy -- which does write, and commits
// the result -- is where the cache actually persists between runs.
package verify
