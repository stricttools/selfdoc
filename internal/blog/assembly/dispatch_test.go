package assembly

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
)

// dispatchPayload decodes the request body a dispatch POSTs.
func dispatchPayload(t *testing.T, dispatch Dispatch) map[string]any {
	t.Helper()
	encoded, err := dispatch.PayloadJSON()
	if err != nil {
		t.Fatalf("rendering the payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}
	return payload
}

// clientPayload is the client_payload of a dispatch.
func clientPayload(t *testing.T, dispatch Dispatch) map[string]any {
	t.Helper()
	payload := dispatchPayload(t, dispatch)
	client, ok := payload["client_payload"].(map[string]any)
	if !ok {
		t.Fatalf("the payload carries no client_payload: %v", payload)
	}
	return client
}

// payloadKeys is the client_payload's key set, sorted.
func payloadKeys(client map[string]any) []string {
	keys := make([]string, 0, len(client))
	for key := range client {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// -- assembly push -----------------------------------------------------------

func TestPushEndpointTargetsTheAssemblyRepo(t *testing.T) {
	dispatch := AssemblyPush("owner/assembly", "owner/proj", "myslug", "1.0.0", "v1.0.0")
	if dispatch.Endpoint != "/repos/owner/assembly/dispatches" {
		t.Fatalf("endpoint = %q", dispatch.Endpoint)
	}
}

func TestPushEndpointNamesTheAssemblyAndNotTheSource(t *testing.T) {
	dispatch := AssemblyPush("org/assembly-repo", "org/source-repo", "s", "1", "v1")
	if !strings.Contains(dispatch.Endpoint, "org/assembly-repo") {
		t.Error("the endpoint does not name the assembly repository")
	}
	if strings.Contains(dispatch.Endpoint, "org/source-repo") {
		t.Error("the endpoint names the source repository")
	}
}

func TestPushEventType(t *testing.T) {
	dispatch := AssemblyPush("owner/assembly", "owner/proj", "myslug", "1.0.0", "v1.0.0")
	if got := dispatchPayload(t, dispatch)["event_type"]; got != DispatchEventType {
		t.Fatalf("event_type = %v", got)
	}
}

func TestPushClientPayloadFields(t *testing.T) {
	dispatch := AssemblyPush("owner/assembly", "owner/proj", "myslug", "1.0.0", "v1.0.0")
	client := clientPayload(t, dispatch)
	want := []string{"ref", "repo", "slug", "version"}
	if got := payloadKeys(client); !reflect.DeepEqual(got, want) {
		t.Fatalf("client_payload keys = %v, want %v", got, want)
	}
	if client["slug"] != "myslug" || client["version"] != "1.0.0" ||
		client["ref"] != "v1.0.0" || client["repo"] != "owner/proj" {
		t.Fatalf("client_payload = %v", client)
	}
}

func TestPushPayloadCarriesTheSourceRepoNotTheAssembly(t *testing.T) {
	dispatch := AssemblyPush("org/assembly-repo", "org/source-repo", "s", "1", "v1")
	client := clientPayload(t, dispatch)
	if client["repo"] != "org/source-repo" {
		t.Fatalf("repo = %v", client["repo"])
	}
	encoded, _ := dispatch.PayloadJSON()
	if strings.Contains(string(encoded), "org/assembly-repo") {
		t.Fatalf("the payload names the assembly repository: %s", encoded)
	}
}

// -- the shared-elements dispatch --------------------------------------------

func TestSharedOnlyDispatchCarriesTheScopeAlone(t *testing.T) {
	// The scope is the whole of what it says: the workflow's clone step is
	// conditioned on it, and there is no project to name.
	dispatch := SharedOnlyDispatch("owner/assembly")
	if dispatch.Endpoint != "/repos/owner/assembly/dispatches" {
		t.Fatalf("endpoint = %q", dispatch.Endpoint)
	}
	client := clientPayload(t, dispatch)
	if got := payloadKeys(client); !reflect.DeepEqual(got, []string{"scope"}) {
		t.Fatalf("client_payload keys = %v, want [scope]", got)
	}
	if client["scope"] != SharedOnlyScope {
		t.Fatalf("scope = %v", client["scope"])
	}
}

func TestSharedOnlyPayloadIsTheBytesTheAPITakes(t *testing.T) {
	encoded, err := SharedOnlyDispatch("owner/assembly").PayloadJSON()
	if err != nil {
		t.Fatalf("rendering the payload: %v", err)
	}
	want := `{"event_type":"project-updated","client_payload":{"scope":"shared-only"}}`
	if string(encoded) != want {
		t.Fatalf("payload = %s, want %s", encoded, want)
	}
}

// -- assembly status ---------------------------------------------------------

func TestStatusQueriesTheRepositorysWorkflowRuns(t *testing.T) {
	commands := AssemblyStatus("owner/docs-assembly")
	if len(commands) < 1 {
		t.Fatal("status asks nothing")
	}
	if commands[0][0] != "gh" {
		t.Fatalf("the status command is %q, not gh", commands[0][0])
	}
	joined := strings.Join(commands[0], " ")
	for _, want := range []string{"owner/docs-assembly", "actions/runs"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the status command does not name %q: %s", want, joined)
		}
	}
}

// -- assembly rebuild --------------------------------------------------------

func TestRebuildOfAnEmptyAssemblyDispatchesNothing(t *testing.T) {
	dispatches, err := AssemblyRebuild("owner/assembly", map[string]any{})
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(dispatches) != 0 {
		t.Fatalf("an empty assembly produced %d dispatch(es)", len(dispatches))
	}
}

func TestRebuildDispatchesOncePerProject(t *testing.T) {
	projects := map[string]any{
		"proj-a": map[string]any{"repo": "owner/proj-a", "ref": "v1.0.0", "version": "1.0.0"},
		"proj-b": map[string]any{"repo": "owner/proj-b", "ref": "v2.0.0", "version": "2.0.0"},
		"proj-c": map[string]any{"repo": "owner/proj-c", "ref": "main", "version": "3.0.0"},
	}
	dispatches, err := AssemblyRebuild("owner/assembly", projects)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(dispatches) != 3 {
		t.Fatalf("dispatched %d project(s), want 3", len(dispatches))
	}
	var slugs []string
	for _, dispatch := range dispatches {
		slugs = append(slugs, dispatch.Slug)
	}
	if !reflect.DeepEqual(slugs, []string{"proj-a", "proj-b", "proj-c"}) {
		t.Fatalf("dispatch order = %v, want slug order", slugs)
	}
}

func TestRebuildReplaysTheRecordedMembership(t *testing.T) {
	projects := map[string]any{
		"alpha": map[string]any{
			"repo": "org/alpha-repo", "ref": "v3.0.0", "version": "3.0.0",
		},
	}
	dispatches, err := AssemblyRebuild("org/assembly", projects)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(dispatches) != 1 {
		t.Fatalf("dispatched %d project(s), want 1", len(dispatches))
	}
	client := clientPayload(t, dispatches[0])
	for key, want := range map[string]string{
		"slug": "alpha", "repo": "org/alpha-repo",
		"ref": "v3.0.0", "version": "3.0.0",
	} {
		if client[key] != want {
			t.Errorf("%s = %v, want %q", key, client[key], want)
		}
	}
	if dispatches[0].Endpoint != "/repos/org/assembly/dispatches" {
		t.Errorf("endpoint = %q", dispatches[0].Endpoint)
	}
}

func TestRebuildRefusesAnIncompleteMembershipRecord(t *testing.T) {
	// A missing version used to become the literal string "latest". That
	// string travelled into the dispatch payload and back out into the
	// membership record, so the assembly's own record then claimed a version
	// nobody ever released.
	for _, test := range []struct {
		name   string
		record map[string]any
		field  string
	}{
		{"no version", map[string]any{"repo": "org/alpha-repo", "ref": "v3.0.0"}, "version"},
		{"no repo", map[string]any{"ref": "v3.0.0", "version": "3.0.0"}, "repo"},
		{"no ref", map[string]any{"repo": "org/alpha-repo", "version": "3.0.0"}, "ref"},
		{"empty version", map[string]any{
			"repo": "org/alpha-repo", "ref": "v3.0.0", "version": "",
		}, "version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := AssemblyRebuild("org/assembly", map[string]any{"alpha": test.record})
			if err == nil {
				t.Fatal("an incomplete record was dispatched anyway")
			}
			for _, want := range []string{"alpha", test.field} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %q, want it to name %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "latest") {
				t.Errorf("the refusal offers the invented version: %q", err)
			}
		})
	}
}

func TestRebuildRefusesAnEntryThatIsNotARecord(t *testing.T) {
	_, err := AssemblyRebuild("org/assembly", map[string]any{"alpha": "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("err = %v, want the incomplete-record refusal", err)
	}
}

// A record whose version is the unversioned literal is complete: the literal
// is a real answer to "what did this project deploy", and a rebuild replays it
// rather than refusing the project as hand-edited.
func TestRebuildReplaysAnUnversionedProject(t *testing.T) {
	projects := map[string]any{
		"portfolio": map[string]any{
			"repo": "org/portfolio", "ref": "main",
			"version": config.UnversionedVersion,
		},
	}
	dispatches, err := AssemblyRebuild("org/assembly", projects)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(dispatches) != 1 {
		t.Fatalf("dispatched %d project(s), want 1", len(dispatches))
	}
	client := clientPayload(t, dispatches[0])
	for key, want := range map[string]string{
		"slug": "portfolio", "repo": "org/portfolio",
		"ref": "main", "version": config.UnversionedVersion,
	} {
		if client[key] != want {
			t.Errorf("%s = %v, want %q", key, client[key], want)
		}
	}
}
