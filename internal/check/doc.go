// Package check validates a project's documentation: every directive resolves,
// every public symbol is covered, and every lint rule holds.
//
// The command's three answers come out of here. [CheckDocs] produces the whole
// verdict -- per-directive resolution results, the coverage measurement and
// every diagnostic -- [PrintResults] renders it for a reader and
// [SerializeCheckResult] renders it for a machine, and [AcceptBaselines]
// advances the staleness baseline of a page a person has reviewed.
//
// # Post checks are here too
//
// The Python this ports split post validation (the post lints) out into the
// former blog package and reached it back through a registered hook, because
// the two packages shipped as separate installs and the docs generator could
// not import it.
// One binary has no such boundary: [CheckPosts] and [PostErrorLint] live beside
// the rules that consume them and [CheckDocs] calls them directly.
//
// # Every rule runs over posts as well as pages
//
// A post is a page on the site, so the lint slice [CheckDocs] judges is the
// docs tree plus the project's published posts, each keyed by its own source
// path so a diagnostic names a file a reader can open. Coverage and the
// staleness baselines are keyed by docs-tree page and see only the docs tree:
// a post is not one of those.
package check
