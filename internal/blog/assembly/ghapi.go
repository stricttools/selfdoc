package assembly

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
)

// ghAPITimeout bounds every gh api call. A hung API call in CI would hold the
// deploy's concurrency slot until the job's own limit killed it.
const ghAPITimeout = 30 * time.Second

// ghCall is one gh api invocation, as the caller declares it.
type ghCall struct {
	// Args are the arguments after "gh api".
	Args []string
	// Input is the request body, written to gh's standard input. Nil for a
	// call with no body.
	Input []byte
	// Step names the operation in every diagnostic.
	Step string
	// Read declares a GET-shaped call, which changes nothing and therefore
	// executes in every mode.
	Read bool
	// Resource is the opaque token naming what this call produces, for the
	// preview's own rendering.
	Resource string
	// Grant names the grant on the running command that authorizes this call.
	Grant string
}

// ghAPI runs one gh api call and returns its stdout, stripped of its trailing
// newline.
//
// [ghCall.Read] declares a GET-shaped call, which changes nothing and
// therefore executes in every mode. A non-read call is a network mutation:
// under a command's --dry-run it is recorded and the returned carrier has no
// stdout to read, so the framework truncates the preview at that point -- the
// honest outcome for an API whose next step needs a SHA that was never minted.
func ghAPI(h *effects.Handle, call ghCall) (string, error) {
	argv := append([]string{"gh", "api"}, call.Args...)
	options := []effects.Option{
		effects.CaptureOutput(),
		effects.Timeout(ghAPITimeout),
	}
	if call.Input != nil {
		options = append(options, effects.Stdin(call.Input))
	}
	if call.Read {
		options = append(options, effects.Read())
	}
	if call.Resource != "" {
		options = append(options, effects.Resource(call.Resource))
	}
	if call.Grant != "" {
		options = append(options, effects.Grant(call.Grant))
	}
	result, err := h.Run(argv, options...)
	if err != nil {
		if errors.Is(err, effects.ErrTimeout) {
			return "", errorf("%s: gh api timed out after 30s", call.Step)
		}
		return "", err
	}
	if result.Unsettled {
		return "", nil
	}
	if result.ExitCode != 0 {
		return "", errorf("%s: %s", call.Step, strings.TrimSpace(string(result.Stderr)))
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

// PushResult is what one [PushFilesToRepo] call did.
//
// Changed is false when every file already matched the branch and nothing was
// deleted -- no commit exists in that case and SHA is the untouched head. A
// caller that regenerates an artifact on every release reads this to tell
// "wrote it" from "it was already right".
type PushResult struct {
	// SHA is the commit the branch points at after the push.
	SHA string
	// Changed reports whether a commit was created at all.
	Changed bool
	// Uploaded is every path whose blob was uploaded, in push order.
	Uploaded []string
	// Deleted is every path the commit removed, in push order.
	Deleted []string
}

// remoteTree is the Trees API response this package reads.
type remoteTree struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"tree"`
}

// remoteBlobSHAs returns path -> blob sha for every file in treeSHA,
// recursively.
//
// The method is pinned to GET on purpose: "gh api -f" attaches a request body,
// which makes gh choose POST, and POST is not a route on this endpoint --
// GitHub answers 404 and the caller reads it as an assembly that is not there.
func remoteBlobSHAs(h *effects.Handle, repo, treeSHA string) (map[string]string, error) {
	raw, err := ghAPI(h, ghCall{
		Args: []string{
			"--method", "GET",
			"/repos/" + repo + "/git/trees/" + treeSHA, "-f", "recursive=1",
		},
		Step: "list tree",
		Read: true,
	})
	if err != nil {
		return nil, err
	}
	tree := remoteTree{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &tree); err != nil {
			return nil, errorf("list tree: unparseable response: %s", err)
		}
	}
	if tree.Truncated {
		return nil, errorf(
			"list tree: %s's tree is too large for the Trees API to return "+
				"in one response, so unchanged files cannot be identified. "+
				"Push through a clone instead.",
			repo,
		)
	}
	shas := map[string]string{}
	for _, entry := range tree.Tree {
		if entry.Type == "blob" {
			shas[entry.Path] = entry.SHA
		}
	}
	return shas, nil
}

// treeEntry is one entry of a Trees API create request. SHA is a pointer so a
// deletion can carry a JSON null, which is how the API spells one.
type treeEntry struct {
	Path string  `json:"path"`
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	SHA  *string `json:"sha"`
}

// PushFilesToRepo pushes files and deletions to a remote repository in one
// commit through the Git Data API.
//
// It uses the GitHub REST API (through the gh CLI) to create blobs, a tree, a
// commit, and to update the branch ref -- all without cloning.
//
// Content travels as bytes and reaches the blob API base64-encoded, so an
// image round-trips byte-identically.
//
// Every path is hashed locally with git's own blob hash and compared against
// the remote tree: a file whose bytes already match uploads no blob and
// contributes no tree entry. When nothing differs and nothing is deleted, no
// commit is created at all and the current head SHA comes back -- which is
// what makes a regenerating writer idempotent.
//
// deletePaths are removed from the branch in the same commit. A path that is
// already absent is not an error -- the commit just does not mention it.
//
// Paths are pushed in sorted order. The Python pushed them in the mapping's
// own insertion order, which a Go map does not have, and sorted is the one
// order that is the same on every run.
//
// It returns an error when there is neither a file nor a deletion to push, and
// when any API call fails.
func PushFilesToRepo(
	h *effects.Handle,
	repo string,
	files map[string][]byte,
	message string,
	branch string,
	deletePaths []string,
) (PushResult, error) {
	if len(files) == 0 && len(deletePaths) == 0 {
		return PushResult{}, errorf("files dict is empty -- nothing to push")
	}
	if branch == "" {
		return PushResult{}, errorf("no branch named for the push to %s", repo)
	}

	// 1. The current HEAD SHA.
	headSHA, err := ghAPI(h, ghCall{
		Args: []string{"/repos/" + repo + "/git/ref/heads/" + branch, "--jq", ".object.sha"},
		Step: "get HEAD ref",
		Read: true,
	})
	if err != nil {
		return PushResult{}, err
	}

	// 2. The current tree SHA.
	treeSHA, err := ghAPI(h, ghCall{
		Args: []string{"/repos/" + repo + "/git/commits/" + headSHA, "--jq", ".tree.sha"},
		Step: "get tree SHA",
		Read: true,
	})
	if err != nil {
		return PushResult{}, err
	}

	// 3. What the branch already holds, so unchanged files stay home.
	remoteSHAs, err := remoteBlobSHAs(h, repo, treeSHA)
	if err != nil {
		return PushResult{}, err
	}

	// 4. A blob for each changed file.
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var entries []treeEntry
	var uploaded []string
	var deleted []string
	for _, path := range paths {
		data := files[path]
		if remoteSHAs[path] == site.GitBlobSHA1(data) {
			continue
		}
		payload, err := json.Marshal(map[string]string{
			"content":  base64.StdEncoding.EncodeToString(data),
			"encoding": "base64",
		})
		if err != nil {
			return PushResult{}, err
		}
		blobSHA, err := ghAPI(h, ghCall{
			Args: []string{
				"--method", "POST", "/repos/" + repo + "/git/blobs",
				"--jq", ".sha", "--input", "-",
			},
			Input:    payload,
			Step:     "create blob for " + path,
			Resource: "gh-blob:" + repo + "/" + path,
		})
		if err != nil {
			return PushResult{}, err
		}
		sha := blobSHA
		entries = append(entries, treeEntry{
			Path: path, Mode: "100644", Type: "blob", SHA: &sha,
		})
		uploaded = append(uploaded, path)
	}

	// 5. A null sha on an existing path is how the Trees API spells deletion.
	for _, path := range deletePaths {
		if _, present := remoteSHAs[path]; !present {
			continue
		}
		entries = append(entries, treeEntry{
			Path: path, Mode: "100644", Type: "blob", SHA: nil,
		})
		deleted = append(deleted, path)
	}

	if len(entries) == 0 {
		return PushResult{SHA: headSHA, Changed: false}, nil
	}

	// 6. The new tree.
	treePayload, err := json.Marshal(map[string]any{
		"base_tree": treeSHA, "tree": entries,
	})
	if err != nil {
		return PushResult{}, err
	}
	newTreeSHA, err := ghAPI(h, ghCall{
		Args: []string{
			"--method", "POST", "/repos/" + repo + "/git/trees",
			"--jq", ".sha", "--input", "-",
		},
		Input:    treePayload,
		Step:     "create tree",
		Resource: "gh-tree:" + repo,
	})
	if err != nil {
		return PushResult{}, err
	}

	// 7. The commit.
	commitPayload, err := json.Marshal(map[string]any{
		"message": message, "tree": newTreeSHA, "parents": []string{headSHA},
	})
	if err != nil {
		return PushResult{}, err
	}
	newCommitSHA, err := ghAPI(h, ghCall{
		Args: []string{
			"--method", "POST", "/repos/" + repo + "/git/commits",
			"--jq", ".sha", "--input", "-",
		},
		Input:    commitPayload,
		Step:     "create commit",
		Resource: "gh-commit:" + repo,
	})
	if err != nil {
		return PushResult{}, err
	}

	// 8. The ref update.
	refPayload, err := json.Marshal(map[string]string{"sha": newCommitSHA})
	if err != nil {
		return PushResult{}, err
	}
	if _, err := ghAPI(h, ghCall{
		Args: []string{
			"--method", "PATCH", "/repos/" + repo + "/git/refs/heads/" + branch,
			"--jq", ".object.sha", "--input", "-",
		},
		Input:    refPayload,
		Step:     "update ref",
		Resource: "gh-ref:" + repo + "/" + branch,
	}); err != nil {
		return PushResult{}, err
	}

	return PushResult{
		SHA:      newCommitSHA,
		Changed:  true,
		Uploaded: uploaded,
		Deleted:  deleted,
	}, nil
}

// ListRemotePaths returns every file path on repo's branch, recursively.
//
// This is how a publisher that never clones the assembly learns what is
// already there: which of a project's pages exist remotely, so the ones the
// local build no longer produces can be deleted in the same commit that
// uploads the ones it does.
func ListRemotePaths(h *effects.Handle, repo, branch string) ([]string, error) {
	if branch == "" {
		return nil, errorf("no branch named for the listing of %s", repo)
	}
	headSHA, err := ghAPI(h, ghCall{
		Args: []string{"/repos/" + repo + "/git/ref/heads/" + branch, "--jq", ".object.sha"},
		Step: "get HEAD ref",
		Read: true,
	})
	if err != nil {
		return nil, err
	}
	treeSHA, err := ghAPI(h, ghCall{
		Args: []string{"/repos/" + repo + "/git/commits/" + headSHA, "--jq", ".tree.sha"},
		Step: "get tree SHA",
		Read: true,
	})
	if err != nil {
		return nil, err
	}
	shas, err := remoteBlobSHAs(h, repo, treeSHA)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(shas))
	for path := range shas {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
