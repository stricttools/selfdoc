package assembly

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/site"
	"github.com/stricttools/selfdoc/internal/util"
)

// DispatchEventType is the repository_dispatch event type the generated
// workflow listens for.
const DispatchEventType = "project-updated"

// SharedOnlyScope is the scope a dispatch carries when it asks for the
// cross-project elements to be regenerated and nothing else.
//
// It is what "post publish", "docs publish" and "assembly retire" send after
// their own commit: each writes content through the Git Data API, which no
// workflow ran, so the listing, the feed, the sitemap and the search index are
// stale until a deploy regenerates them.
const SharedOnlyScope = "shared-only"

// Dispatch is one repository_dispatch event: the endpoint it is POSTed to and
// the fields the workflow reads out of it.
//
// A project dispatch names Slug, Version, Ref and Repo and leaves Scope empty,
// which the workflow reads as a full build. A shared-elements dispatch names
// Scope alone. [Dispatch.PayloadJSON] renders whichever of the two this is.
type Dispatch struct {
	// Endpoint is the gh api path the event is sent to.
	Endpoint string
	// Slug is the project being rebuilt.
	Slug string
	// Version is the version being deployed.
	Version string
	// Ref is the git ref (tag or branch) the source is cloned at.
	Ref string
	// Repo is the source project's repository.
	Repo string
	// Scope is [SharedOnlyScope] for a shared-elements rebuild, and empty for
	// a project dispatch.
	Scope string
}

// projectClientPayload is the client_payload of a project dispatch, in the key
// order the Python surface emitted.
type projectClientPayload struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	Ref     string `json:"ref"`
	Repo    string `json:"repo"`
}

// sharedClientPayload is the client_payload of a shared-elements dispatch.
type sharedClientPayload struct {
	Scope string `json:"scope"`
}

// dispatchBody is the request body both shapes share.
type dispatchBody struct {
	EventType     string `json:"event_type"`
	ClientPayload any    `json:"client_payload"`
}

// PayloadJSON renders the request body this dispatch POSTs.
//
// The two shapes are distinct documents rather than one document with empty
// keys: a project dispatch states all four project fields and no scope, and a
// shared-elements dispatch states the scope and nothing else, which is what
// the workflow's own condition on the scope was written against.
func (d Dispatch) PayloadJSON() ([]byte, error) {
	body := dispatchBody{EventType: DispatchEventType}
	if d.Scope != "" {
		body.ClientPayload = sharedClientPayload{Scope: d.Scope}
	} else {
		body.ClientPayload = projectClientPayload{
			Slug: d.Slug, Version: d.Version, Ref: d.Ref, Repo: d.Repo,
		}
	}
	return json.Marshal(body)
}

// DispatchEndpoint is the gh api path a repository_dispatch on repo is sent to.
func DispatchEndpoint(repo string) string {
	return "/repos/" + repo + "/dispatches"
}

// AssemblyPush returns the dispatch that rebuilds one project in the assembly.
//
// assemblyRepo is the assembly repository (for example
// "smm-h/docs-assembly"); sourceRepo is the source project's repository; slug
// is the project slug; version is the version being deployed; ref is the git
// ref the workflow clones the source at.
func AssemblyPush(assemblyRepo, sourceRepo, slug, version, ref string) Dispatch {
	return Dispatch{
		Endpoint: DispatchEndpoint(assemblyRepo),
		Slug:     slug,
		Version:  version,
		Ref:      ref,
		Repo:     sourceRepo,
	}
}

// SharedOnlyDispatch returns the dispatch that regenerates the assembly's
// cross-project elements without touching any project's files.
func SharedOnlyDispatch(assemblyRepo string) Dispatch {
	return Dispatch{
		Endpoint: DispatchEndpoint(assemblyRepo),
		Scope:    SharedOnlyScope,
	}
}

// AssemblyStatus returns the gh argv lists that read the assembly's recent
// workflow runs.
//
// repo is the assembly repository identifier (for example
// "smm-h/docs-assembly").
func AssemblyStatus(repo string) [][]string {
	return [][]string{{
		"gh",
		"api",
		"/repos/" + repo + "/actions/runs",
		"--jq",
		".workflow_runs[:5] | .[] | {status, conclusion, created_at, html_url}",
	}}
}

// AssemblyRebuild returns one dispatch per project the assembly records.
//
// repo is the assembly repository identifier. projects is the derived
// membership record "assembly integrate" wrote, which always carries repo, ref
// and version for every project.
//
// An incomplete record is a hard error naming the project and the fields it
// lacks. A missing version used to become the literal string "latest", which
// travelled through the dispatch payload and back into the membership record
// -- so the assembly's own record then claimed a version nobody released, and
// every later rebuild replayed it.
//
// The dispatches come back in slug order. The Python replayed the record's own
// key order, which a decoded JSON document no longer carries, and a sorted
// replay is the one order that is the same on every run.
func AssemblyRebuild(repo string, projects map[string]any) ([]Dispatch, error) {
	slugs := make([]string, 0, len(projects))
	for slug := range projects {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	dispatches := make([]Dispatch, 0, len(slugs))
	for _, slug := range slugs {
		record, _ := projects[slug].(map[string]any)
		var missing []string
		for _, field := range site.MembershipFields {
			if membershipField(record, field) == "" {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			return nil, errorf(
				"the assembly's membership record for %s is missing %s. "+
					"'assembly integrate' writes all of %s for every "+
					"project, so this entry was hand-edited or written by an "+
					"older deploy. Fix %s, or dispatch %s from its own "+
					"repository with 'selfdoc assembly push' -- there is no "+
					"default for a version, and inventing one records docs "+
					"under a version nobody released.",
				util.PythonRepr(slug), strings.Join(missing, ", "),
				strings.Join(site.MembershipFields, ", "),
				site.ProjectsPath, util.PythonRepr(slug),
			)
		}
		dispatches = append(dispatches, AssemblyPush(
			repo,
			membershipField(record, "repo"),
			slug,
			membershipField(record, "version"),
			membershipField(record, "ref"),
		))
	}
	return dispatches, nil
}

// membershipField is one field of a membership record as a string, empty for
// every value Python read as absent -- a missing key, null, the empty string,
// false and zero.
func membershipField(record map[string]any, field string) string {
	if record == nil {
		return ""
	}
	value, ok := record[field]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if !typed {
			return ""
		}
	case float64:
		if typed == 0 {
			return ""
		}
	}
	return util.PythonStr(value)
}
