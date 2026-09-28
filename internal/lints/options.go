package lints

// Each lint is an option. selfdoc's options registry (options.toml, validated
// by strictspec's built-in options-registry schema) declares one option per
// lint, named as the lint is, filed under the docs subject, and ranked by the
// lint's registered severity: an error lint may be run as error, warn or off,
// a warning lint as warn or off. No lint can be raised above its registered
// severity.
//
// A repository's entries live in .strictmetadata/options/. [LoadSettings]
// reads them through strictspec, which validates the shape of every entry and
// judges the selfdoc namespace against this registry, and returns the value
// each lint runs at. [Settings.Apply] then drops a lint that is off and
// reports a lint at warn as a warning, which never blocks.

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/stricttools/strictspec/go/strictspec"
)

// OptionsTool is the namespace selfdoc's options live in: an entry names one
// as selfdoc:<lint name>.
const OptionsTool = "selfdoc"

// OptionsSubject is the subject every selfdoc option is filed under, so every
// entry for one lives in .strictmetadata/options/docs.toml.
const OptionsSubject = "docs"

// The values a lint's option ranks. A repository's entry names one of them.
const (
	SettingError = "error"
	SettingWarn  = "warn"
	SettingOff   = "off"
)

// OptionsRegistryFormatVersion is the format version of the options registry
// selfdoc ships: the document's format_version, the one strictspec's built-in
// options-registry schema reads.
const OptionsRegistryFormatVersion = 1

// optionsRegistryName is the shipped registry's file name, as the messages and
// the generator spell it.
const optionsRegistryName = "options.toml"

//go:embed options.toml
var optionsRegistryDocument []byte

// OptionsRegistryDocument returns the shipped options registry exactly as it
// is embedded: the bytes of options.toml, which `selfdoc options registry`
// prints. The caller gets a copy, so the embedded document cannot be altered.
func OptionsRegistryDocument() []byte {
	return append([]byte(nil), optionsRegistryDocument...)
}

// OptionID is the id an entry names a lint's option by.
func OptionID(name string) string { return OptionsTool + ":" + name }

// optionRanking is the ranking and default of the option governing a lint of
// the given registered severity.
func optionRanking(severity string) (values, def string) {
	if severity == "error" {
		return SettingError + " > " + SettingWarn + " > " + SettingOff, SettingError
	}
	return SettingWarn + " > " + SettingOff, SettingWarn
}

// RenderOptionsRegistry renders the options registry a lint registry implies:
// one option per lint, in registry order. The shipped options.toml is this
// rendering of the shipped lint registry, written by
// `go run ./internal/lints/genoptions`, and a test holds the two together.
func RenderOptionsRegistry(reg *Registry) string {
	var out strings.Builder
	out.WriteString("# selfdoc's options registry: one option per lint, validated by strictspec's\n")
	out.WriteString("# built-in options-registry schema. Generated from lints.toml by\n")
	out.WriteString("# `go run ./internal/lints/genoptions`; never edit it by hand.\n\n")
	fmt.Fprintf(&out, "format_version = %d\n", OptionsRegistryFormatVersion)
	for _, name := range reg.Names() {
		spec, _ := reg.Spec(name)
		values, def := optionRanking(spec.Severity)
		fmt.Fprintf(&out, "\n[[option]]\nname = %s\nsubject = %s\nvalues = %s\ndefault = %s\nscope = \"none\"\ndescription = %s\n",
			strconv.Quote(name), strconv.Quote(OptionsSubject), strconv.Quote(values),
			strconv.Quote(def), strconv.Quote(spec.Description))
	}
	return out.String()
}

var (
	optionsOnce     sync.Once
	optionsRegistry *strictspec.CheckedOptionsRegistry
	optionsErr      error
)

// BuildOptionsRegistry validates registry-document bytes through strictspec,
// the shape against the built-in options-registry schema and then the
// registry rules, and returns the checked registry.
func BuildOptionsRegistry(raw []byte) (*strictspec.CheckedOptionsRegistry, error) {
	document, diags := strictspec.ReadOptionsRegistry(raw)
	if len(diags) > 0 {
		return nil, &LintRegistryError{Message: optionsRegistryName + " is not a valid options registry:" + renderDiagnostics(diags)}
	}
	checked, diags := strictspec.ValidateOptionsRegistry(document)
	if len(diags) > 0 {
		return nil, &LintRegistryError{Message: optionsRegistryName + " is not a valid options registry:" + renderDiagnostics(diags)}
	}
	return checked, nil
}

// OptionsRegistry returns selfdoc's shipped options registry, validated on
// first use. It panics when the embedded document is malformed, as
// [Registered] does: this build of selfdoc would not know its own options.
func OptionsRegistry() *strictspec.CheckedOptionsRegistry {
	optionsOnce.Do(func() { optionsRegistry, optionsErr = BuildOptionsRegistry(optionsRegistryDocument) })
	if optionsErr != nil {
		panic(optionsErr)
	}
	return optionsRegistry
}

// renderDiagnostics lists strictspec diagnostics one per line, each with its
// code, the way every refusal here prints them.
func renderDiagnostics(diags []strictspec.Diagnostic) string {
	var out strings.Builder
	for _, d := range diags {
		fmt.Fprintf(&out, "\n  %s: %s", d.Code, d.Message)
	}
	return out.String()
}

// OptionsError is a repository whose option entries cannot be read: a subject
// document of the wrong shape, or a selfdoc entry strictspec refuses. The
// message carries strictspec's catalogued diagnostics, each naming its file.
type OptionsError struct {
	Message string
}

func (e *OptionsError) Error() string { return e.Message }

// Settings is the value each lint runs at in one repository: the current value
// of its entry, or its registered severity when the repository has none.
type Settings struct {
	current map[string]string
}

// Current returns the value a lint runs at.
func (s Settings) Current(name string) string {
	if value, ok := s.current[name]; ok {
		return value
	}
	spec, _ := Registered().Spec(name)
	_, def := optionRanking(spec.Severity)
	return def
}

// LoadSettings reads a repository's option entries and returns the value each
// lint runs at.
//
// Every subject document is shape-validated by strictspec's loader, whichever
// tool's entries it holds, and a document of the wrong shape is refused. The
// selfdoc namespace is then judged against selfdoc's registry, and any entry
// strictspec refuses is refused here with its diagnostic. Another tool's
// entries are not judged.
func LoadSettings(repoRoot string) (Settings, error) {
	loaded, err := strictspec.LoadOptionsEntries(repoRoot)
	if err != nil {
		return Settings{}, err
	}
	if len(loaded.Invalid) > 0 {
		var detail strings.Builder
		for _, file := range loaded.Invalid {
			fmt.Fprintf(&detail, "\n %s/%s:%s", strictspec.OptionsDir, file.File, renderDiagnostics(file.Diagnostics))
		}
		return Settings{}, &OptionsError{Message: fmt.Sprintf(
			"selfdoc cannot read this repository's options: a document in %s/ is not a valid options-entries document. Fix what each diagnostic names:%s",
			strictspec.OptionsDir, detail.String())}
	}
	accepted, diags := strictspec.ValidateOptionsNamespace(OptionsTool, OptionsRegistry(), loaded.Entries)
	if len(diags) > 0 {
		return Settings{}, &OptionsError{Message: fmt.Sprintf(
			"selfdoc cannot read this repository's options: strictspec refuses %d %s entr%s in %s/. Fix what each diagnostic names:%s",
			len(diags), OptionsTool, plural(len(diags), "y", "ies"), strictspec.OptionsDir, renderDiagnostics(diags))}
	}
	settings := Settings{current: map[string]string{}}
	for _, classified := range accepted {
		settings.current[strings.TrimPrefix(classified.Entry.ID, OptionsTool+":")] = classified.Entry.Current
	}
	return settings, nil
}

// plural picks a word ending by count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Apply returns the lints a run reports under these settings: a lint whose
// option is off is dropped, one at warn is reported as a warning, which never
// blocks, and one at error keeps its registered severity.
//
// It can only lower a diagnostic's severity from the registry's answer: an
// option ranks no value above the registered severity.
func (s Settings) Apply(results []LintResult) []LintResult {
	out := make([]LintResult, 0, len(results))
	for _, result := range results {
		switch s.Current(result.name) {
		case SettingOff:
			continue
		case SettingWarn:
			result.severity = "warning"
		}
		out = append(out, result)
	}
	return out
}
