package cli

import (
	"reflect"
	"testing"

	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/strictspec/go/strictspec"
)

// `selfdoc options registry` prints selfdoc's options registry, so a tool that
// reads every tool's options (strictcode) learns selfdoc's rankings by running
// it. It needs no selfdoc project: the registry is the binary's own.

func TestOptionsRegistryPrintsTheEmbeddedRegistryAsTOML(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "options", "registry")
	if result.ExitCode != 0 {
		t.Fatalf("options registry exited %d: %s", result.ExitCode, result.Stderr)
	}
	if result.Stdout != string(lints.OptionsRegistryDocument()) {
		t.Fatalf("the printed registry is not the embedded one:\n%s", result.Stdout)
	}

	document, diags := strictspec.ReadOptionsRegistry([]byte(result.Stdout))
	if len(diags) > 0 {
		t.Fatalf("strictspec cannot read the printed registry: %v", diags)
	}
	checked, diags := strictspec.ValidateOptionsRegistry(document)
	if len(diags) > 0 {
		t.Fatalf("strictspec refuses the printed registry: %v", diags)
	}
	if !reflect.DeepEqual(checked.Names(), lints.OptionsRegistry().Names()) {
		t.Fatalf("the printed registry declares %v, want %v", checked.Names(), lints.OptionsRegistry().Names())
	}
	if !reflect.DeepEqual(checked.Names(), lints.Registered().Names()) {
		t.Fatalf("the printed registry declares %v, want one option per lint %v", checked.Names(), lints.Registered().Names())
	}
	for _, name := range checked.Names() {
		got, _ := checked.Option(name)
		want, _ := lints.OptionsRegistry().Option(name)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("option %s is %+v, want the embedded %+v", name, got, want)
		}
	}
}

func TestOptionsRegistryPublishesTheSameDeclarationsAsJSON(t *testing.T) {
	isolate(t)
	result := run(t, t.TempDir(), "--json", "options", "registry")
	if result.ExitCode != 0 {
		t.Fatalf("options registry --json exited %d: %s", result.ExitCode, result.Stderr)
	}
	payload := payloadOf(t, result)
	if payload["format_version"] != float64(1) {
		t.Errorf("format_version = %v, want 1", payload["format_version"])
	}
	document, diags := strictspec.ReadOptionsRegistry(lints.OptionsRegistryDocument())
	if len(diags) > 0 {
		t.Fatalf("strictspec cannot read the embedded registry: %v", diags)
	}
	declared, ok := payload["option"].([]any)
	if !ok || len(declared) != len(document.Options) {
		t.Fatalf("option = %#v, want %d declarations", payload["option"], len(document.Options))
	}
	for index, raw := range declared {
		got := raw.(map[string]any)
		want := document.Options[index]
		for field, value := range map[string]string{
			"name": want.Name, "subject": want.Subject, "values": want.Values,
			"default": want.Default, "scope": want.Scope, "description": want.Description,
		} {
			if got[field] != value {
				t.Errorf("option %d %s = %v, want %q", index, field, got[field], value)
			}
		}
	}
}
