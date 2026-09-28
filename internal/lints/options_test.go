package lints

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/strictspec"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The shipped options registry is the rendering of the lint registry, so the
// two cannot disagree about which lints exist or how far each can be lowered.
func TestTheShippedOptionsRegistryIsRenderedFromTheLintRegistry(t *testing.T) {
	want := RenderOptionsRegistry(Registered())
	if string(optionsRegistryDocument) != want {
		t.Errorf("internal/lints/options.toml is not the rendering of lints.toml. " +
			"Regenerate it with 'go run ./internal/lints/genoptions' from the repository root")
	}
}

// One option per lint, named as the lint is, filed under docs, taking no
// scope, and ranked by the lint's registered severity so that no option can
// raise a lint above it.
func TestEveryLintIsAnOptionRankedByItsSeverity(t *testing.T) {
	options := OptionsRegistry()
	if got, want := len(options.Names()), Registered().Len(); got != want {
		t.Fatalf("the options registry declares %d options, the lint registry %d lints", got, want)
	}
	for _, name := range Registered().Codes() {
		option, found := options.Option(name)
		if !found {
			t.Errorf("lint %s has no option", name)
			continue
		}
		spec, _ := Registered().Spec(name)
		declaration := option.Declaration
		wantValues, wantDefault := "warn > off", "warn"
		if spec.Severity == "error" {
			wantValues, wantDefault = "error > warn > off", "error"
		}
		if declaration.Values != wantValues || declaration.Default != wantDefault ||
			declaration.Subject != "docs" || declaration.Scope != "none" ||
			declaration.Description != spec.Description {
			t.Errorf("option %s = %+v, want values %q, default %q, subject docs, scope none, the lint's description",
				name, declaration, wantValues, wantDefault)
		}
	}
}

func TestTheShippedOptionsRegistryPassesStrictspec(t *testing.T) {
	if _, err := BuildOptionsRegistry(optionsRegistryDocument); err != nil {
		t.Fatal(err)
	}
}

// writeEntries writes one subject document into a repository's options
// directory.
func writeEntries(t *testing.T, repo, file, content string) {
	t.Helper()
	dir := filepath.Join(repo, filepath.FromSlash(strictspec.OptionsDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsWithNoOptionsDirectoryRunEveryLintAtItsSeverity(t *testing.T) {
	hygiene.Isolate(t)
	settings, err := LoadSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range Registered().Codes() {
		spec, _ := Registered().Spec(name)
		want := SettingWarn
		if spec.Severity == "error" {
			want = SettingError
		}
		if got := settings.Current(name); got != want {
			t.Errorf("%s runs at %q, want %q", name, got, want)
		}
	}
}

func TestSettingsApplyDropsOffAndLowersWarn(t *testing.T) {
	hygiene.Isolate(t)
	repo := t.TempDir()
	writeEntries(t, repo, "docs.toml", `format_version = 1

[[entry]]
id = "selfdoc:missing-frontmatter-description"
current = "warn"
ideal = "error"
reason = "the landing page is being rewritten"

[[entry]]
id = "selfdoc:low-numeric-data-density"
current = "off"
ideal = "off"
reason = "reference pages list no quantities"
`)
	settings, err := LoadSettings(repo)
	if err != nil {
		t.Fatal(err)
	}
	applied := settings.Apply([]LintResult{
		MustLintResult("a.md", nil, "missing-frontmatter-description", "m"),
		MustLintResult("a.md", nil, "low-numeric-data-density", "m"),
		MustLintResult("a.md", nil, "missing-page-title", "m"),
	})
	if len(applied) != 2 {
		t.Fatalf("applied = %v, want the off lint dropped", applied)
	}
	if applied[0].Code() != "missing-frontmatter-description" || applied[0].Severity() != "warning" {
		t.Errorf("the lint at warn is %s/%s", applied[0].Code(), applied[0].Severity())
	}
	if applied[1].Code() != "missing-page-title" || applied[1].Severity() != "error" {
		t.Errorf("a lint without an entry is %s/%s", applied[1].Code(), applied[1].Severity())
	}
	if CheckExitCode(applied[:1], nil, nil, nil) != 0 {
		t.Error("a lint at warn blocks the run")
	}
}

func TestSettingsRefuseWhatStrictspecRefuses(t *testing.T) {
	hygiene.Isolate(t)
	repo := t.TempDir()
	writeEntries(t, repo, "docs.toml", `format_version = 1

[[entry]]
id = "selfdoc:low-numeric-data-density"
current = "error"
ideal = "off"
reason = "no lint can be raised above its registered severity"
`)
	_, err := LoadSettings(repo)
	var refused *OptionsError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want an *OptionsError", err)
	}
	for _, want := range []string{"STRICTSPEC_OPTIONS_UNDECLARED_CURRENT", ".strictmetadata/options/docs.toml"} {
		if !strings.Contains(refused.Message, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, refused.Message)
		}
	}
}
