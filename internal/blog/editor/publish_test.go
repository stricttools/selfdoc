package editor

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// The publish surface: the declaration, the plan, and the consent path.
//
// Three properties, and the third is the reason the other two exist.
//
//   - The dialog is rendered from the COMMAND's own declaration, projected
//     through the publisher, so it cannot describe an operation the command
//     does not perform. A classification changed in the command's declaration
//     changes what the surface says with nothing to update in the editor.
//   - The scope is the repository. The publish publishes every non-draft post
//     the project declares, so the surface says exactly that and the plan
//     carries the actual list, computed server-side.
//   - A call without consent is refused by the CONSENT REGIME, not here. Both
//     halves are driven: the refusal with no consent (and nothing runs), and
//     the success with it (and the publisher really runs).
//
// The publisher is a fake, because a real publish leaves the machine and the
// test floor forbids that. The consent path itself is the real one: the
// refusal is the publisher's, forwarded verbatim, and nothing in the editor
// decides whether a call is allowed.

// The refusal a consent regime answers an unconsented call with. The wording
// is the framework's in production; here it stands in for it, and the
// assertions are that it reaches the author unchanged.
const fakeRefusal = "blog.post.publish is consequential and requires confirmation: " +
	"pass --approve-consequential"

// fakePublisher is the command layer's publish surface, recorded instead of
// performed.
type fakePublisher struct {
	mu sync.Mutex

	// declared is what Descriptor answers. The zero value declares the
	// publish the real command declares.
	declared *Descriptor
	// publishing and withheld are what Plan answers.
	publishing []PostSummary
	withheld   []PostSummary
	planErr    error
	// output is what a consented publish writes, exitCode what it returns,
	// and failWith an error it returns instead of running.
	output   string
	exitCode int
	failWith error

	// attempts is every call, consented or not; published is the calls that
	// really ran.
	attempts  []publishAttempt
	published []publishAttempt
}

// publishAttempt is one recorded call.
type publishAttempt struct {
	projectDir string
	approve    bool
}

// Descriptor answers the publish command's own declaration.
func (p *fakePublisher) Descriptor() Descriptor {
	if p.declared != nil {
		return *p.declared
	}
	return Descriptor{
		Effect:        "mutating",
		Consequential: true,
		Help:          "Publish this project's posts to the assembly.",
		Grants: []Grant{{
			Name: "assembly-dispatch",
			Reason: "rebuilds and republishes the assembled site, which is " +
				"how a published post becomes readable",
			Kind: "network",
		}},
	}
}

// Plan answers what a publish would make public.
func (p *fakePublisher) Plan(projectDir string) (Plan, error) {
	if p.planErr != nil {
		return Plan{}, p.planErr
	}
	return Plan{Publishing: p.publishing, Withheld: p.withheld}, nil
}

// Publish records the call, refuses an unconsented one, and otherwise writes
// whatever the fixture declared.
func (p *fakePublisher) Publish(projectDir string, approveConsequential bool, out io.Writer) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts = append(p.attempts, publishAttempt{projectDir, approveConsequential})
	if p.failWith != nil {
		return 0, p.failWith
	}
	if !approveConsequential {
		return 0, &Refusal{Message: fakeRefusal}
	}
	p.published = append(p.published, publishAttempt{projectDir, approveConsequential})
	if _, err := io.WriteString(out, p.output); err != nil {
		return 1, err
	}
	return p.exitCode, nil
}

// publishedCount is how many calls really ran.
func (p *fakePublisher) publishedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.published)
}

// The posts the publish fixtures declare.
var (
	publishHello  = PostSummary{Path: "hello.md", Title: "Hello World", Date: "2024-01-15", Slug: "hello-world", Draft: false, Tags: []string{"release"}}
	publishSecond = PostSummary{Path: "second.md", Title: "Second Post", Date: "2024-02-01", Slug: "second-post", Draft: false, Tags: []string{"release"}}
	publishDraft  = PostSummary{Path: "later.md", Title: "Later", Date: "2024-05-01", Slug: "later", Draft: true, Tags: []string{}}
)

// newPublisher builds the fixture publisher: two posts publish, one is
// withheld, and a consented publish says what it did.
func newPublisher() *fakePublisher {
	return &fakePublisher{
		publishing: []PostSummary{publishHello, publishSecond},
		withheld:   []PostSummary{publishDraft},
		output:     "Published 2 post(s)\n",
	}
}

// publishProject writes the project the publish fixtures name: one that
// declares both a project slug and an assembly.
func publishProject(t *testing.T) string {
	t.Helper()
	return makeProjectWithConfig(t, map[string]string{
		"hello.md":  postHello,
		"later.md":  postDraft,
		"second.md": "+++\ntitle = \"Second Post\"\ndate = 2024-02-01\nslug = \"second-post\"\ntags = []\ndraft = false\ndirectives = false\n+++\nSecond post body.\n",
	}, map[string]any{
		"topology": map[string]any{"slug": "proj"},
		"assembly": map[string]any{"repo": "owner/assembly"},
	})
}

func TestTheDescriptorIsTheCommandsOwnDeclaration(t *testing.T) {
	hygiene.Isolate(t)
	publisher := newPublisher()
	declared := publisher.Descriptor()
	descriptor := PublishDescriptor(publisher)

	t.Run("the effect is the declared one", func(t *testing.T) {
		if descriptor.Effect != declared.Effect {
			t.Errorf("effect = %q, want %q", descriptor.Effect, declared.Effect)
		}
	})

	t.Run("it is consequential because the command says so", func(t *testing.T) {
		if !declared.Consequential {
			t.Fatal("the publish declares itself consequential")
		}
		if !descriptor.Consequential {
			t.Error("the surface does not carry it")
		}
	})

	t.Run("the help is the command's own", func(t *testing.T) {
		if descriptor.Help != declared.Help {
			t.Errorf("help = %q, want %q", descriptor.Help, declared.Help)
		}
	})

	t.Run("every declared grant reaches the surface", func(t *testing.T) {
		if len(declared.Grants) == 0 {
			t.Fatal("the publish declares at least one grant")
		}
		if len(descriptor.Grants) != len(declared.Grants) {
			t.Fatalf("grants = %v, want %v", descriptor.Grants, declared.Grants)
		}
		for index, grant := range declared.Grants {
			if descriptor.Grants[index] != grant {
				t.Errorf("grant %d = %v, want %v", index, descriptor.Grants[index], grant)
			}
		}
	})

	t.Run("the grant says what it reaches", func(t *testing.T) {
		if len(descriptor.Grants) != 1 {
			t.Fatalf("grants = %v, want one", descriptor.Grants)
		}
		grant := descriptor.Grants[0]
		if grant.Name != "assembly-dispatch" {
			t.Errorf("name = %q", grant.Name)
		}
		if !strings.Contains(grant.Reason, "rebuilds and republishes") {
			t.Errorf("reason = %q", grant.Reason)
		}
	})

	t.Run("it names the command it will call", func(t *testing.T) {
		if descriptor.Command != PublishCommand {
			t.Errorf("command = %q, want %q", descriptor.Command, PublishCommand)
		}
	})

	t.Run("it names the consent parameter", func(t *testing.T) {
		if descriptor.ConsentParameter != "approve_consequential" {
			t.Errorf("consent_parameter = %q", descriptor.ConsentParameter)
		}
	})

	t.Run("the surface states the scope, not the publisher", func(t *testing.T) {
		lying := *declared.clone()
		lying.Scope = "document"
		lying.ScopeNote = "publish this post"
		lying.Command = "post.something-else"
		lying.ConsentParameter = "ok"
		stated := PublishDescriptor(&fakePublisher{declared: &lying})
		if stated.Scope != Scope || stated.ScopeNote != ScopeNote {
			t.Error("a publisher's scope reached the surface")
		}
		if stated.Command != PublishCommand || stated.ConsentParameter != ConsentParameter {
			t.Error("a publisher's command or consent parameter reached the surface")
		}
	})
}

func TestTheScopeIsTheRepository(t *testing.T) {
	hygiene.Isolate(t)
	descriptor := PublishDescriptor(newPublisher())

	t.Run("the scope is declared", func(t *testing.T) {
		if descriptor.Scope != Scope || Scope != "repository" {
			t.Errorf("scope = %q, want repository", descriptor.Scope)
		}
	})

	t.Run("the note says every non-draft post of the repository", func(t *testing.T) {
		if !strings.Contains(descriptor.ScopeNote, "every non-draft post in this repository") {
			t.Errorf("note = %q", descriptor.ScopeNote)
		}
	})

	t.Run("the note never says this post", func(t *testing.T) {
		note := strings.ToLower(descriptor.ScopeNote)
		if strings.Contains(note, "this post") || strings.Contains(note, "publish this post") {
			t.Errorf("note = %q", descriptor.ScopeNote)
		}
	})

	t.Run("the note says it cannot be taken back", func(t *testing.T) {
		if !strings.Contains(descriptor.ScopeNote, "cannot be unpublished") {
			t.Errorf("note = %q", descriptor.ScopeNote)
		}
	})
}

func TestThePlanIsComputedHere(t *testing.T) {
	hygiene.Isolate(t)
	project := publishProject(t)
	entry := entryOf(t, "proj", project)
	publisher := newPublisher()

	plan, err := PublishPlan(entry, publisher)
	if err != nil {
		t.Fatalf("PublishPlan: %v", err)
	}

	t.Run("every non-draft post is listed", func(t *testing.T) {
		slugs := []string{}
		for _, post := range plan.Publishing {
			slugs = append(slugs, post.Slug)
		}
		if strings.Join(slugs, ",") != "hello-world,second-post" {
			t.Errorf("publishing = %v", slugs)
		}
	})

	t.Run("drafts are listed as withheld", func(t *testing.T) {
		if len(plan.Withheld) != 1 || plan.Withheld[0].Slug != "later" {
			t.Errorf("withheld = %v", plan.Withheld)
		}
	})

	t.Run("the plan names the repository and the assembly", func(t *testing.T) {
		if plan.Repo != "proj" {
			t.Errorf("repo = %q", plan.Repo)
		}
		if plan.Path != project {
			t.Errorf("path = %q, want %q", plan.Path, project)
		}
		if plan.Project != "proj" {
			t.Errorf("project = %q", plan.Project)
		}
		if plan.Assembly != "owner/assembly" {
			t.Errorf("assembly = %q", plan.Assembly)
		}
	})

	t.Run("a project with only drafts publishes nothing", func(t *testing.T) {
		drafty := publishProject(t)
		onlyDrafts := &fakePublisher{withheld: []PostSummary{publishDraft}}
		plan, err := PublishPlan(entryOf(t, "d", drafty), onlyDrafts)
		if err != nil {
			t.Fatalf("PublishPlan: %v", err)
		}
		if len(plan.Publishing) != 0 {
			t.Errorf("publishing = %v, want nothing", plan.Publishing)
		}
		if len(plan.Withheld) != 1 {
			t.Errorf("withheld = %v, want one", plan.Withheld)
		}
	})

	t.Run("a remote entry is refused", func(t *testing.T) {
		entry, err := writeRegistry(t, remote(t, "afar")).Get("afar")
		if err != nil {
			t.Fatalf("Get(afar): %v", err)
		}
		_, planErr := PublishPlan(entry, publisher)
		if editorError := wantEditorError(t, planErr); editorError.Status != 501 {
			t.Errorf("status = %d, want 501", editorError.Status)
		}
	})
}

func TestTheConsentRegimeRefusesAnUnconsentedCall(t *testing.T) {
	hygiene.Isolate(t)
	project := publishProject(t)
	entry := entryOf(t, "proj", project)

	t.Run("a call without consent is refused", func(t *testing.T) {
		publisher := newPublisher()
		_, err := RunPublish(entry, publisher, false)
		editorError := wantEditorError(t, err)
		if editorError.Status != 403 {
			t.Errorf("status = %d, want 403", editorError.Status)
		}
		if editorError.Message != fakeRefusal {
			t.Errorf("message = %q, want the refusal verbatim", editorError.Message)
		}
	})

	t.Run("nothing ran when the call was refused", func(t *testing.T) {
		publisher := newPublisher()
		if _, err := RunPublish(entry, publisher, false); err == nil {
			t.Fatal("want a refusal")
		}
		if publisher.publishedCount() != 0 {
			t.Error("the refused call published something")
		}
	})

	t.Run("the refusal is forwarded, not worded here", func(t *testing.T) {
		publisher := newPublisher()
		_, err := RunPublish(entry, publisher, false)
		editorError := wantEditorError(t, err)
		var refusal *Refusal
		if !errors.As(&Refusal{Message: fakeRefusal}, &refusal) {
			t.Fatal("a Refusal is not recognizable with errors.As")
		}
		if editorError.Message != (&Refusal{Message: fakeRefusal}).Error() {
			t.Errorf("message = %q", editorError.Message)
		}
	})
}

func TestAConsentedCallRuns(t *testing.T) {
	hygiene.Isolate(t)
	project := publishProject(t)
	entry := entryOf(t, "proj", project)

	t.Run("it succeeds and reports what it did", func(t *testing.T) {
		publisher := newPublisher()
		result, err := RunPublish(entry, publisher, true)
		if err != nil {
			t.Fatalf("RunPublish: %v", err)
		}
		if !result.OK || result.ExitCode != 0 {
			t.Errorf("result = %+v", result)
		}
		if result.Command != PublishCommand {
			t.Errorf("command = %q", result.Command)
		}
		if result.Repo != "proj" {
			t.Errorf("repo = %q", result.Repo)
		}
		if !strings.Contains(result.Stdout, "Published 2 post(s)") {
			t.Errorf("stdout = %q", result.Stdout)
		}
	})

	t.Run("the publish really ran, in the entry's own working tree", func(t *testing.T) {
		publisher := newPublisher()
		if _, err := RunPublish(entry, publisher, true); err != nil {
			t.Fatalf("RunPublish: %v", err)
		}
		if publisher.publishedCount() != 1 {
			t.Fatalf("published %d times, want once", publisher.publishedCount())
		}
		if publisher.published[0].projectDir != project {
			t.Errorf("ran in %q, want %q", publisher.published[0].projectDir, project)
		}
		if !publisher.published[0].approve {
			t.Error("the consent did not reach the publisher")
		}
	})

	t.Run("an error that is not a refusal comes back as it was", func(t *testing.T) {
		boom := errors.New("the assembly refused the write")
		publisher := newPublisher()
		publisher.failWith = boom
		_, err := RunPublish(entry, publisher, true)
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want the publisher's own error", err)
		}
	})

	t.Run("a nonzero exit is a failure carrying the reason", func(t *testing.T) {
		publisher := newPublisher()
		publisher.exitCode = 2
		publisher.output = "assembly.repo is not configured\n"
		_, err := RunPublish(entry, publisher, true)
		editorError := wantEditorError(t, err)
		if editorError.Status != 500 {
			t.Errorf("status = %d, want 500", editorError.Status)
		}
		if !strings.Contains(editorError.Message, "assembly.repo") {
			t.Errorf("message = %q", editorError.Message)
		}
	})

	t.Run("a nonzero exit that said nothing still says something", func(t *testing.T) {
		publisher := newPublisher()
		publisher.exitCode = 3
		publisher.output = "   \n"
		_, err := RunPublish(entry, publisher, true)
		editorError := wantEditorError(t, err)
		if editorError.Message != "blog.post.publish exited 3 without saying why" {
			t.Errorf("message = %q", editorError.Message)
		}
	})
}

func TestConsentIsNeverRemembered(t *testing.T) {
	hygiene.Isolate(t)
	entry := entryOf(t, "proj", publishProject(t))
	publisher := newPublisher()

	t.Run("a second publish needs its own consent", func(t *testing.T) {
		if _, err := RunPublish(entry, publisher, true); err != nil {
			t.Fatalf("RunPublish: %v", err)
		}
		if _, err := RunPublish(entry, publisher, false); err == nil {
			t.Fatal("the second call was not refused")
		}
		if publisher.publishedCount() != 1 {
			t.Errorf("published %d times, want once", publisher.publishedCount())
		}
	})
}

func TestThePublishEndpoint(t *testing.T) {
	hygiene.Isolate(t)
	project := publishProject(t)

	// serveWith binds an editor over the project with one publisher.
	serveWith := func(t *testing.T, publisher Publisher) int {
		t.Helper()
		reg := writeRegistry(t, local("proj", project))
		return serveEditor(t, newState(t, reg, publisher))
	}

	t.Run("the surface carries the descriptor and the plan", func(t *testing.T) {
		port := serveWith(t, newPublisher())
		status, body := requestJSON(t, port, "GET", "/api/repos/proj/publish", "")
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		descriptor, _ := body["descriptor"].(map[string]any)
		if descriptor["consequential"] != true {
			t.Errorf("consequential = %v", descriptor["consequential"])
		}
		if descriptor["scope"] != "repository" {
			t.Errorf("scope = %v", descriptor["scope"])
		}
		plan, _ := body["plan"].(map[string]any)
		publishing, _ := plan["publishing"].([]any)
		slugs := []string{}
		for _, raw := range publishing {
			post, _ := raw.(map[string]any)
			slugs = append(slugs, post["slug"].(string))
		}
		if strings.Join(slugs, ",") != "hello-world,second-post" {
			t.Errorf("publishing = %v", slugs)
		}
	})

	t.Run("a post without consent is refused verbatim", func(t *testing.T) {
		publisher := newPublisher()
		port := serveWith(t, publisher)
		status, body := requestJSON(t, port, "POST", "/api/repos/proj/publish", "{}")
		if status != 403 {
			t.Fatalf("status = %d, want 403", status)
		}
		if errorText(t, body) != fakeRefusal {
			t.Errorf("error = %q, want the refusal verbatim", errorText(t, body))
		}
		if publisher.publishedCount() != 0 {
			t.Error("the refused call published something")
		}
	})

	t.Run("a post with consent publishes", func(t *testing.T) {
		publisher := newPublisher()
		port := serveWith(t, publisher)
		status, body := requestJSON(t, port, "POST", "/api/repos/proj/publish",
			`{"approve_consequential": true}`)
		if status != 200 {
			t.Fatalf("status = %d, want 200: %v", status, body)
		}
		if body["ok"] != true {
			t.Errorf("ok = %v", body["ok"])
		}
		stdout, _ := body["stdout"].(string)
		if !strings.Contains(stdout, "Published 2 post(s)") {
			t.Errorf("stdout = %q", stdout)
		}
		if publisher.publishedCount() != 1 {
			t.Errorf("published %d times, want once", publisher.publishedCount())
		}
	})

	t.Run("explicit false is still a refusal", func(t *testing.T) {
		publisher := newPublisher()
		port := serveWith(t, publisher)
		status, _ := requestJSON(t, port, "POST", "/api/repos/proj/publish",
			`{"approve_consequential": false}`)
		if status != 403 {
			t.Errorf("status = %d, want 403", status)
		}
		if publisher.publishedCount() != 0 {
			t.Error("the refused call published something")
		}
	})

	t.Run("a non-boolean consent is refused", func(t *testing.T) {
		publisher := newPublisher()
		port := serveWith(t, publisher)
		status, body := requestJSON(t, port, "POST", "/api/repos/proj/publish",
			`{"approve_consequential": "yes"}`)
		if status != 400 {
			t.Fatalf("status = %d, want 400", status)
		}
		if !strings.Contains(errorText(t, body), "true or false") {
			t.Errorf("error = %q", errorText(t, body))
		}
		if publisher.publishedCount() != 0 {
			t.Error("the refused call published something")
		}
	})

	t.Run("a body that is not JSON is refused", func(t *testing.T) {
		publisher := newPublisher()
		port := serveWith(t, publisher)
		status, body := requestJSON(t, port, "POST", "/api/repos/proj/publish", "not json")
		if status != 400 {
			t.Fatalf("status = %d, want 400", status)
		}
		if !strings.Contains(errorText(t, body), "not JSON") {
			t.Errorf("error = %q", errorText(t, body))
		}
		if publisher.publishedCount() != 0 {
			t.Error("the refused call published something")
		}
	})

	t.Run("a body that is not an object is refused", func(t *testing.T) {
		port := serveWith(t, newPublisher())
		status, body := requestJSON(t, port, "POST", "/api/repos/proj/publish", "[1, 2]")
		if status != 400 {
			t.Fatalf("status = %d, want 400", status)
		}
		if !strings.Contains(errorText(t, body), "must be a JSON object") {
			t.Errorf("error = %q", errorText(t, body))
		}
	})

	t.Run("an empty body states no consent at all", func(t *testing.T) {
		publisher := newPublisher()
		port := serveWith(t, publisher)
		status, _ := requestJSON(t, port, "POST", "/api/repos/proj/publish", "")
		if status != 403 {
			t.Errorf("status = %d, want 403", status)
		}
		if publisher.publishedCount() != 0 {
			t.Error("the refused call published something")
		}
	})

	t.Run("the surface's body is the Python's own spelling", func(t *testing.T) {
		port := serveWith(t, newPublisher())
		_, text := request(t, port, "GET", "/api/repos/proj/publish", "")
		if !strings.HasPrefix(text, `{"descriptor": {"command": "blog.post.publish", `) {
			t.Errorf("the body opens with %q", text[:min(len(text), 80)])
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			t.Errorf("the body does not decode: %v", err)
		}
	})
}

// clone returns a copy of a descriptor, so a fixture can alter one member
// without rewriting the rest.
func (d Descriptor) clone() *Descriptor {
	copied := d
	copied.Grants = append([]Grant{}, d.Grants...)
	return &copied
}
