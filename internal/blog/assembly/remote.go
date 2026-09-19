package assembly

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/blog/shared"
	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/util"
)

// RemoteReadError is a read from the assembly repository that did not succeed
// and did not 404.
//
// It is kept distinct from the absent-file outcome because the two used to be
// the same value. Every publisher treats an absent published-file record or an
// absent membership record as the real initial state and writes a fresh one
// over it; a rate limit, an expired token or a 502 returned that same "nothing
// there" and the fresh record destroyed whatever the read failed to see. A
// failure now stops the operation before anything is written.
type RemoteReadError struct {
	// Message is the diagnostic, rendered verbatim by Error.
	Message string
}

// Error returns the diagnostic.
func (e *RemoteReadError) Error() string { return e.Message }

// remoteReadErrorf builds a [RemoteReadError] from a format string.
func remoteReadErrorf(format string, args ...any) error {
	return &RemoteReadError{Message: fmt.Sprintf(format, args...)}
}

// ghHTTPStatusRE matches the HTTP status gh reports in its error line -- "gh:
// Not Found (HTTP 404)", "gh: API rate limit exceeded ... (HTTP 403)". A
// failure with no status at all did not reach the API (DNS, timeout, gh
// missing) and is never absence.
var ghHTTPStatusRE = regexp.MustCompile(`\(HTTP (\d{3})\)`)

// ghHTTPStatus returns the HTTP status gh reported, or 0 if it reported none.
func ghHTTPStatus(stderr string) int {
	match := ghHTTPStatusRE.FindStringSubmatch(stderr)
	if match == nil {
		return 0
	}
	status, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return status
}

// FetchRemoteText returns the text of path on repo's default branch.
//
// Absence and failure are two outcomes, not one. Only an explicit HTTP 404
// means the file is not there, and only then does missingOK turn it into the
// empty string -- the real initial state of a record nothing has written yet.
// Every other outcome is a [RemoteReadError] naming operation, the path and
// what gh said, because a caller that read "" as "nothing published yet" would
// then write a record that erases what it could not read.
//
// operation describes what the read is for, so the error says which operation
// was abandoned rather than only which path was unreadable. Empty names the
// read itself.
func FetchRemoteText(
	h *effects.Handle,
	repo, path string,
	missingOK bool,
	operation string,
) (string, error) {
	what := operation
	if what == "" {
		what = fmt.Sprintf("read %s from %s", path, repo)
	}
	result, err := h.Run(
		[]string{"gh", "api", "/repos/" + repo + "/contents/" + path, "--jq", ".content"},
		effects.CaptureOutput(),
		effects.Timeout(ghAPITimeout),
		effects.Read(),
	)
	if err != nil {
		if errors.Is(err, effects.ErrTimeout) {
			return "", remoteReadErrorf(
				"%s: reading %s from %s failed, and the failure is not an "+
					"absent file, so it cannot be read as one: gh api timed "+
					"out after 30s. Nothing was written. Re-run once the read "+
					"succeeds.",
				what, path, repo,
			)
		}
		return "", err
	}
	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(string(result.Stderr))
		detail := stderr
		if detail == "" {
			detail = fmt.Sprintf("gh api exited %d with no output", result.ExitCode)
		}
		if ghHTTPStatus(stderr) == 404 {
			if missingOK {
				return "", nil
			}
			return "", remoteReadErrorf(
				"%s: %s does not exist on %s (%s).", what, path, repo, detail,
			)
		}
		return "", remoteReadErrorf(
			"%s: reading %s from %s failed, and the failure is not an absent "+
				"file, so it cannot be read as one: %s. Nothing was written. "+
				"Re-run once the read succeeds.",
			what, path, repo, detail,
		)
	}
	encoded := strings.Join(util.PythonFields(string(result.Stdout)), "")
	if encoded == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", remoteReadErrorf(
			"%s: could not decode %s from %s: %s", what, path, repo, err,
		)
	}
	if !utf8.Valid(decoded) {
		return "", remoteReadErrorf(
			"%s: could not decode %s from %s: not valid UTF-8",
			what, path, repo,
		)
	}
	return string(decoded), nil
}

// RemoteTextFetcher is [FetchRemoteText] bound to one effects handle, in the
// shape [site.StagePublishedRecord] takes.
//
// The model half of the assembly never reaches the network; the publisher that
// owns the remote supplies its own reader, and this is that reader.
func RemoteTextFetcher(h *effects.Handle) site.RemoteTextFetcher {
	return func(repo, path, operation string) (string, error) {
		return FetchRemoteText(h, repo, path, true, operation)
	}
}

// LoadRemoteRoster returns the roster declared on the assembly repository.
//
// An absent roster is its own error naming the block that has to exist; a
// failed read is a [RemoteReadError], never mistaken for one.
func LoadRemoteRoster(h *effects.Handle, repo string) (*site.Roster, error) {
	source := repo + ":" + site.RosterPath
	text, err := FetchRemoteText(
		h, repo, site.RosterPath, true,
		"read the assembly roster on "+repo,
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, site.MissingRosterError(source)
	}
	return site.ParseRoster(text, source)
}

// RemotePostClaims maps every site-level post path another project claims to
// its claimant.
//
// The remote counterpart of [site.ForeignPostClaims]. A publisher that writes
// through the Git Data API has no assembly clone to read, so it asks the
// assembly for one published-file record per declared project other than its
// own. That is one API read per project, which is the price of the site-level
// blog being a single namespace: without it the publish would find out about
// the collision only after it had already overwritten the other project's
// post.
//
// A project with no record yet claims nothing. A record that cannot be read is
// a [RemoteReadError], never an empty claim set.
func RemotePostClaims(
	h *effects.Handle,
	repo, slug string,
	others []string,
) (map[string]string, error) {
	seen := map[string]bool{}
	unique := make([]string, 0, len(others))
	for _, other := range others {
		if !seen[other] {
			seen[other] = true
			unique = append(unique, other)
		}
	}
	sort.Strings(unique)

	claims := map[string]string{}
	for _, other := range unique {
		if other == slug {
			continue
		}
		recordPath := "manifests/" + other + "-files.json"
		raw, err := FetchRemoteText(
			h, repo, recordPath, true,
			fmt.Sprintf(
				"check %s's post paths against %s's claims on %s",
				util.PythonRepr(slug), util.PythonRepr(other), repo,
			),
		)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		record, err := site.ParseFilesManifest(raw, repo+":"+recordPath)
		if err != nil {
			return nil, err
		}
		for _, owner := range site.PublishOwners {
			for _, path := range record[owner] {
				if strings.SplitN(path, "/", 2)[0] != shared.PostsSegment {
					continue
				}
				if _, claimed := claims[path]; !claimed {
					claims[path] = other
				}
			}
		}
	}
	return claims, nil
}

// FetchRemoteManifests returns the assembly's per-project manifests, read off
// the repository through the Git Data API.
//
// It is the remote counterpart of [site.LoadAssemblyManifests], for the
// publishers and checks that never clone the assembly. The home project's
// pages render from these -- a version badge and a post highlight come from no
// project's own repository -- so a command that builds or checks the home
// project reads them from the assembly itself.
//
// Only the per-project manifests are fetched: the sidecars beside them
// (published-file records, revision sidecars, the listing copy) are not
// manifests, and asking for each one is an API read that answers nothing.
func FetchRemoteManifests(
	h *effects.Handle,
	repo, branch string,
) ([]map[string]any, error) {
	if branch == "" {
		branch = DefaultBranch
	}
	paths, err := ListRemotePaths(h, repo, branch)
	if err != nil {
		return nil, err
	}
	documents := map[string][]byte{}
	for _, path := range paths {
		name, ok := strings.CutPrefix(path, "manifests/")
		if !ok || strings.Contains(name, "/") || !site.IsManifestDocument(name) {
			continue
		}
		text, err := FetchRemoteText(
			h, repo, path, false,
			"read the assembly's manifests on "+repo,
		)
		if err != nil {
			return nil, err
		}
		documents[name] = []byte(text)
	}
	return site.ManifestsFromDocuments(documents, repo+":manifests")
}
