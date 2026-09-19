package editor

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/stricttools/selfdoc/internal/blog/editor/registry"
	"github.com/stricttools/selfdoc/internal/util"
)

// The editor's publish surface: the descriptor, the plan, and the consent
// path.
//
// Publishing is the moment locally-authored posts become publicly readable,
// and it cannot be undone from the reader's side. The publish command already
// says so in the only place that decision belongs -- its strictcli
// declaration, where it is effect="mutating" and consequential=true with a
// named grant for the workflow it dispatches.
//
// So the editor does not restate any of that. It READS the declaration
// (through [Publisher.Descriptor], which projects the command's own schema),
// renders a consent dialog from it, and, on confirm, calls the command
// carrying the consent the human just gave. Three properties follow, and each
// is asserted by the suite:
//
//   - the dialog cannot drift from the command -- a classification changed in
//     the command's declaration changes what the dialog says, with nothing to
//     update here;
//   - a call without consent is refused by the framework, not by a check
//     written here that could be forgotten or bypassed;
//   - the refusal and the result both reach the author verbatim.
//
// Two things the surface must be honest about, because the command is:
//
//   - The scope is the repository, not the open post. The publish builds and
//     pushes every non-draft post the project declares. Saying "publish this
//     post" would be a lie about what the button does, so the descriptor
//     carries [ScopeNote] and the plan carries the actual list.
//   - Consent is per publish. Nothing is remembered: there is no
//     don't-ask-again, no stored answer, and no session in which the question
//     has already been asked. A second publish asks a second time.

// PublishCommand is the command the surface drives, as strictcli addresses
// it.
const PublishCommand = "blog.post.publish"

// Scope is what the publish reaches, stated the way the surface has to state
// it.
const Scope = "repository"

// ConsentParameter is the name of the parameter the browser sets to carry the
// human's consent.
const ConsentParameter = "approve_consequential"

// ScopeNote is rendered verbatim by the consent dialog. The command publishes
// a project, not a document, and a surface that implied otherwise would be
// describing something the button does not do.
const ScopeNote = "This publishes every non-draft post in this repository -- not just the " +
	"post open in the editor. Published posts become publicly readable and " +
	"cannot be unpublished from the reader's side."

// publishLock serializes publishes: one at a time, process-wide.
//
// The Python held this lock because the invocation changed the process's
// working directory and streams for its duration. This port changes neither
// -- the project directory is a parameter and the output goes to an injected
// writer -- and the lock stays anyway: a publish builds and pushes a whole
// repository, and two of them interleaving over one working tree is not
// something the editor has any reason to allow.
var publishLock sync.Mutex

// Grant is one grant the publish command declares, in the shape strictcli's
// own schema dump writes it.
type Grant struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Kind   string `json:"kind"`
}

// Descriptor is the publish command's declaration, as the consent dialog
// renders it.
//
// Effect, Consequential, Help and Grants are the COMMAND's own answers,
// projected out of its declaration by the [Publisher] rather than copied
// here. Command, Scope, ScopeNote and ConsentParameter are the surface's own:
// [PublishDescriptor] fills them from this package's constants and ignores
// whatever a Publisher put there, because what the button reaches is a
// property of the surface, not something the command declares.
type Descriptor struct {
	Command          string  `json:"command"`
	Effect           string  `json:"effect"`
	Consequential    bool    `json:"consequential"`
	Help             string  `json:"help"`
	Grants           []Grant `json:"grants"`
	Scope            string  `json:"scope"`
	ScopeNote        string  `json:"scope_note"`
	ConsentParameter string  `json:"consent_parameter"`
}

// Plan is what a publish of one repository would make public.
//
// Publishing and Withheld come from the publish's own post discovery, through
// [Publisher.Plan]: which posts publish is the project's answer, read off the
// same discovery the publish itself runs rather than derived in the browser.
// Repo, Path, Project and Assembly are filled by [PublishPlan] from the
// registry entry and the project config.
type Plan struct {
	Repo       string        `json:"repo"`
	Path       string        `json:"path"`
	Project    string        `json:"project"`
	Assembly   string        `json:"assembly"`
	Publishing []PostSummary `json:"publishing"`
	Withheld   []PostSummary `json:"withheld"`
}

// PublishResult is what a completed publish reports back to the author.
//
// Stdout carries everything the invocation wrote, because the invocation is
// handed one writer; Stderr is therefore always empty. The shell renders the
// two concatenated, so the author sees the whole output either way.
type PublishResult struct {
	Repo     string `json:"repo"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	OK       bool   `json:"ok"`
}

// Publisher is the publish surface's door into the CLI: the command's own
// declaration, the plan its discovery produces, and the consented
// invocation.
//
// The editor holds no CLI application of its own. The command layer
// implements this and injects it when it constructs the server, which is what
// keeps the engine free of a dependency on the command tree it is called
// from, and what lets the suite drive the whole surface against a publisher
// that records instead of pushing.
type Publisher interface {
	// Descriptor projects the publish command's own declaration: its
	// effect, whether it is consequential, its help and every grant it
	// declares.
	Descriptor() Descriptor
	// Plan reports what a publish of the project at projectDir would make
	// public, and what it would withhold.
	Plan(projectDir string) (Plan, error)
	// Publish invokes the publish for the project at projectDir, carrying
	// the consent the human gave, and writes everything the invocation
	// emits to out.
	//
	// A call the consent regime refuses must return a *Refusal, which the
	// editor answers as a 403 with the framework's message verbatim. A
	// non-zero exit code with no error is a failed publish, which the
	// editor answers as a 500 carrying whatever the invocation printed.
	Publish(projectDir string, approveConsequential bool, out io.Writer) (exitCode int, err error)
}

// Refusal reports that the consent regime refused the publish call.
//
// It is the one error shape a [Publisher] must return for a refusal, and the
// message is the framework's own, forwarded verbatim: nothing here decides
// whether a call is allowed, so nothing here words the refusal either.
type Refusal struct {
	// Message is the framework's refusal, verbatim.
	Message string
}

// Error returns the refusal, verbatim.
func (e *Refusal) Error() string { return e.Message }

// PublishDescriptor returns the publish declaration the consent dialog is
// rendered from.
func PublishDescriptor(publisher Publisher) Descriptor {
	descriptor := publisher.Descriptor()
	descriptor.Command = PublishCommand
	descriptor.Scope = Scope
	descriptor.ScopeNote = ScopeNote
	descriptor.ConsentParameter = ConsentParameter
	if descriptor.Grants == nil {
		descriptor.Grants = []Grant{}
	}
	return descriptor
}

// PublishPlan returns what a publish of entry would make public.
func PublishPlan(entry registry.Entry, publisher Publisher) (Plan, error) {
	path, err := RequireLocal(entry)
	if err != nil {
		return Plan{}, err
	}
	cfg, err := RepoConfig(entry)
	if err != nil {
		return Plan{}, err
	}
	plan, err := publisher.Plan(path)
	if err != nil {
		return Plan{}, err
	}

	assemblyConfig, _ := cfg["assembly"].(map[string]any)
	topologyConfig, _ := cfg["topology"].(map[string]any)

	plan.Repo = entry.Name()
	plan.Path = path
	plan.Project = util.PythonStrOrEmpty(topologyConfig["slug"])
	plan.Assembly = util.PythonStrOrEmpty(assemblyConfig["repo"])
	if plan.Publishing == nil {
		plan.Publishing = []PostSummary{}
	}
	if plan.Withheld == nil {
		plan.Withheld = []PostSummary{}
	}
	return plan, nil
}

// RunPublish invokes the publish for entry, carrying the consent the human
// gave.
//
// The consent is passed through untouched. False (or absent) is not handled
// here at all -- the consent regime refuses the call, which is the point: the
// refusal comes from the regime, not from a condition this function could
// forget to write.
//
// A refusal is a 403 carrying the framework's message; a non-zero exit is a
// 500 carrying whatever the invocation printed; any other error is returned
// as it came, so the server answers it as the unexpected failure it is.
func RunPublish(entry registry.Entry, publisher Publisher, approveConsequential bool) (PublishResult, error) {
	path, err := RequireLocal(entry)
	if err != nil {
		return PublishResult{}, err
	}

	var out bytes.Buffer
	publishLock.Lock()
	exitCode, publishErr := publisher.Publish(path, approveConsequential, &out)
	publishLock.Unlock()

	var refusal *Refusal
	if errors.As(publishErr, &refusal) {
		return PublishResult{}, &Error{
			Message: refusal.Message, Status: http.StatusForbidden,
		}
	}
	if publishErr != nil {
		return PublishResult{}, publishErr
	}

	result := PublishResult{
		Repo:     entry.Name(),
		Command:  PublishCommand,
		ExitCode: exitCode,
		Stdout:   out.String(),
		Stderr:   "",
		OK:       exitCode == 0,
	}
	if exitCode != 0 {
		message := util.PythonStrip(out.String())
		if message == "" {
			message = PublishCommand + " exited " +
				util.PythonStr(exitCode) + " without saying why"
		}
		return result, &Error{Message: message, Status: http.StatusInternalServerError}
	}
	return result, nil
}
