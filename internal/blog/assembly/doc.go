// Package assembly carries the assembly's operations: the deploy workflow it
// generates, the dispatches it sends, the build-and-graft body that deploy
// runs, and the two publishers that write into it without cloning it.
//
// The model half -- the roster, the published-file records, the graft rule,
// membership reconciliation, the tag resolution -- is
// [github.com/stricttools/selfdoc/internal/blog/site], which touches neither the
// network nor the GitHub API. This package is everything that does:
//
//   - The generated workflow. [GenerateWorkflowYAML] renders the one file the
//     assembly repository holds at [site.WorkflowPath], pinned to an exact
//     toolchain ([ToolchainPins]) that [CheckPinsArePublished] refuses unless
//     both registries actually serve it. [AssemblyInit] is that file plus the
//     three others a fresh assembly repository starts life with.
//   - The dispatches. [AssemblyPush] and [AssemblyRebuild] return the endpoint
//     and payload of a repository_dispatch event; the command layer sends
//     them. [AssemblyStatus] returns the argv that reads recent runs.
//   - The deploy body. [IntegrateProject] is what the workflow's one integrate
//     step runs: build the cloned source project, graft it in, reconcile
//     membership, regenerate the shared cross-project files, index, verify,
//     commit and push -- inside a retry loop that re-syncs to the remote every
//     attempt so two concurrent deploys converge instead of clobbering each
//     other.
//   - The Git Data API publishers. [PushFilesToRepo] writes a commit without a
//     clone, uploading only the blobs whose bytes differ; [PublishProjectDocs]
//     and [RetireProject] are the two operations built on it, and
//     [FetchRemoteText] is how they read the assembly they never cloned --
//     with absence and failure kept apart, which is what [RemoteReadError]
//     exists for.
//
// Every subprocess launch and every write goes through an explicit
// [github.com/stricttools/selfdoc/internal/effects.Handle], passed in by the caller.
// The two registry reads are plain GETs: they change nothing, so they do not
// go through the handle -- a recorded read would have nothing to record and a
// preview still needs the answer.
package assembly
