// Package preview builds the whole assembly from local checkouts and serves it
// on loopback.
//
// The preview is the look-before-you-ship step: it does everything a deploy
// does to produce the tree, stops short of everything a deploy does to publish
// it, and then serves the result so a person can look at it.
//
// The point is that it looks at the *real* tree. Every step below is the
// production function the deploy itself calls -- the same build, the same
// [github.com/stricttools/selfdoc/internal/blog/site.SplitBuildOutput] graft, the
// same [github.com/stricttools/selfdoc/internal/blog/assembly.GenerateSharedFiles]
// (chrome asset included), the same
// [github.com/stricttools/selfdoc/internal/blog/verify.VerifyAssembly]. A preview
// that rendered through a second implementation would be a picture of
// something that is not going to be published, which is worse than no preview
// at all.
//
// What the deploy does that a preview cannot are the remote-coupled steps: it
// clones from tags, it pushes manifests and membership records to the assembly
// repository, and it deploys. Those have local equivalents here, built from the
// same functions' outputs into the preview tree -- a roster rendered by
// [github.com/stricttools/selfdoc/internal/blog/site.RenderRoster] from the
// checkouts named on the command line, a projects.json written by
// [github.com/stricttools/selfdoc/internal/blog/site.RecordMembership], manifests
// copied by the graft. The tree the preview serves is therefore an assembly
// checkout in every respect verification can see, which is why verification
// runs against it unchanged.
//
// Verification REPORTS, it does not block. A deploy refuses to publish a tree
// that fails; a preview exists to be looked at when something is wrong, so the
// report is printed first and loudly and the server starts either way.
//
// The output directory is refused when it sits inside a git working tree at a
// path that is not ignored. A build tree dropped into a checkout pollutes
// "git status" for every other session sharing it, and a few thousand
// generated files are exactly the kind of thing that gets committed by
// accident.
package preview
