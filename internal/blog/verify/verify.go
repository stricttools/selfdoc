package verify

import (
	"fmt"

	"github.com/stricttools/selfdoc/internal/blog/site"
)

// check is one assertion: the name it reports under and the pass that makes
// it.
type check struct {
	name string
	run  func(*AssemblyTree) ([]Failure, error)
}

// assertions are the checks a run makes over the tree itself, in the order it
// makes them. The three reference checks are not here: one pass produces all
// three, so it is run once afterwards.
var assertions = []check{
	{"roster-agreement", CheckRosterAgreement},
	{"home-project", CheckHomeProject},
	{"manifest-identity", CheckManifestIdentity},
	{"manifest-pages-emitted", CheckManifestPagesEmitted},
	{"manifest-posts-emitted", CheckManifestPostsEmitted},
	{"shared-artifacts", CheckSharedArtifacts},
	{"page-metadata", CheckPageMetadata},
	{"site-chrome", CheckSiteChrome},
	{"unresolved-directives", CheckUnresolvedDirectives},
	{"routing-artifacts", CheckRoutingArtifacts},
	{"cross-project-links", CheckCrossProjectLinks},
	{"project-reachability", CheckProjectReachability},
}

// VerifyAssembly asserts every property the assembled tree has to have before
// a deploy.
//
// assemblyDir is the assembly repository checkout, holding site/, manifests/
// and the roster.
//
// canonicalBase is the site's canonical base URL. Absolute references --
// canonicals, sitemap entries, feed links -- are this site's when they sit
// under it, and somebody else's when they do not, so there is nothing to
// verify against without it, and an empty one is refused.
//
// fetch is the outbound fetch layer; nil selects [FetchURL].
//
// now is the clock the cache window is measured against, in seconds since the
// epoch. It is the caller's to supply -- the deploy passes the wall clock and
// a test passes a fixed instant -- so a run's verdict is a function of its
// inputs.
//
// Verification never writes: the updated outbound store rides on the report
// for the caller to persist.
func VerifyAssembly(
	assemblyDir string,
	canonicalBase string,
	fetch Fetcher,
	now float64,
) (*VerifyReport, error) {
	if canonicalBase == "" {
		return nil, errorf(
			"canonical_base is required: without it nothing can tell this " +
				"site's absolute URLs from anybody else's, and half the " +
				"assertions would pass by not looking.",
		)
	}
	if fetch == nil {
		fetch = FetchURL
	}

	tree, err := ReadTree(assemblyDir, canonicalBase)
	if err != nil {
		return nil, err
	}
	report := &VerifyReport{OutboundCache: map[string]any{}}

	for _, assertion := range assertions {
		report.Ran = append(report.Ran, assertion.name)
		failures, err := assertion.run(tree)
		if err != nil {
			return nil, err
		}
		report.Failures = append(report.Failures, failures...)
	}

	report.Ran = append(report.Ran,
		"internal-references", "sitemap-entries", "feed-links")
	references, err := CheckReferences(tree)
	if err != nil {
		return nil, err
	}
	report.Failures = append(report.Failures, references...)

	config, err := site.LoadOutbound(assemblyDir)
	if err != nil {
		return nil, err
	}
	cache, err := site.LoadOutboundCache(assemblyDir)
	if err != nil {
		return nil, err
	}
	report.OutboundCache = cache
	if config == nil {
		report.Skipped = append(report.Skipped, Skip{
			"outbound-links",
			fmt.Sprintf("no %s in %s: outbound link checking is not "+
				"configured for this assembly, so nothing about the site's "+
				"external links was verified. Declare the pages to check to "+
				"turn it on.", site.OutboundPath, assemblyDir),
		})
	} else {
		report.Ran = append(report.Ran, "outbound-links")
		failures, updated, requests, err := CheckOutboundLinks(
			tree, *config, cache, fetch, now,
		)
		if err != nil {
			return nil, err
		}
		report.Failures = append(report.Failures, failures...)
		report.OutboundCache = updated
		report.Requests = requests
	}

	sortFailures(report.Failures)
	return report, nil
}
