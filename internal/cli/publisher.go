package cli

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/editor"
	"github.com/stricttools/selfdoc/internal/blog/posts"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/smm-h/strictcli/go/strictcli"
)

// publishCommandPath is the dotted path of the command the editor's publish
// button reaches.
const publishCommandPath = "blog.post.publish"

// publisher is the editor's door into this command tree.
//
// The editor holds no application of its own: it is handed this, so the engine
// never depends on the command tree it is called from, and the suite can drive
// the publish surface against a publisher that records instead of pushing.
//
// Each Publish builds a FRESH application pointed at the stated directory and
// writing to the stated writer. Nothing chdirs, and nothing redirects a
// process-wide stream: the directory and the destination are declarations on
// the application, so two publishes of two repositories cannot interfere.
type publisher struct {
	// options are the running editor's own options, which every invocation
	// is derived from -- the toolchain registry in particular.
	options Options
}

// Descriptor projects the publish command's own declaration out of the
// application's schema, so the consent dialog renders what the command really
// declares rather than a copy of it kept here.
func (p *publisher) Descriptor() editor.Descriptor {
	schema := New(p.options).DumpSchemaDict()
	entry := schemaCommand(schema, publishCommandPath)

	descriptor := editor.Descriptor{}
	if entry == nil {
		return descriptor
	}
	descriptor.Effect, _ = entry["effect"].(string)
	descriptor.Consequential, _ = entry["consequential"].(bool)
	descriptor.Help, _ = entry["help"].(string)
	if grants, ok := entry["grants"].([]any); ok {
		for _, raw := range grants {
			grant, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := grant["name"].(string)
			reason, _ := grant["reason"].(string)
			kind, _ := grant["kind"].(string)
			descriptor.Grants = append(descriptor.Grants, editor.Grant{
				Name: name, Reason: reason, Kind: kind,
			})
		}
	}
	return descriptor
}

// Plan reports what a publish of projectDir would make public, read off the
// same post discovery the publish itself runs.
func (p *publisher) Plan(projectDir string) (editor.Plan, error) {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return editor.Plan{}, err
	}
	postsDir := filepath.Join(projectDir, strings.TrimRight(postsDirOf(cfg), "/"))
	discovered, err := posts.Discover(postsDir, "", effects.Unbound())
	if err != nil {
		return editor.Plan{}, err
	}

	plan := editor.Plan{Publishing: []editor.PostSummary{}, Withheld: []editor.PostSummary{}}
	for _, post := range discovered {
		summary := editor.PostSummary{
			Path:  post.Path,
			Title: post.Title,
			Date:  post.Date,
			Slug:  post.Slug,
			Draft: post.Draft,
			Tags:  post.Tags,
		}
		if post.Draft {
			plan.Withheld = append(plan.Withheld, summary)
		} else {
			plan.Publishing = append(plan.Publishing, summary)
		}
	}
	return plan, nil
}

// Publish runs the publish command in process against projectDir, writing
// everything it emits to out.
func (p *publisher) Publish(projectDir string, approveConsequential bool, out io.Writer) (int, error) {
	options := p.options
	options.Dir = projectDir
	options.Stdout = out
	options.Stderr = out

	var callOptions []strictcli.CallOption
	if approveConsequential {
		callOptions = append(callOptions, strictcli.WithApproveConsequential())
	}

	result, err := New(options).Call(publishCommandPath, map[string]any{}, callOptions...)
	if err != nil {
		// The consent regime can only refuse a call that carried no consent,
		// so that is the one case wrapped as the refusal the editor answers
		// as a 403 with the framework's own message, forwarded verbatim.
		// With consent given, an error is something else and says so.
		if !approveConsequential {
			return 1, &editor.Refusal{Message: err.Error()}
		}
		return 1, err
	}
	code, _ := result.(int)
	return code, nil
}

// schemaCommand walks a dumped schema to the entry at a dotted command path.
func schemaCommand(schema map[string]any, path string) map[string]any {
	segments := strings.Split(path, ".")
	current := schema
	for index, segment := range segments {
		if index == len(segments)-1 {
			commands, ok := current["commands"].(map[string]any)
			if !ok {
				return nil
			}
			entry, _ := commands[segment].(map[string]any)
			return entry
		}
		groups, ok := current["groups"].(map[string]any)
		if !ok {
			return nil
		}
		current, ok = groups[segment].(map[string]any)
		if !ok {
			return nil
		}
	}
	return nil
}
