// Package lints owns the lint-code registry and the verdict rules every check
// entry point shares.
//
// The embedded lints.toml is the single authority for every lint code selfdoc
// can emit: its severity and its one-line description are declared there and
// nowhere else. [LintResult] derives its severity from the registry, so a
// construction site cannot state a severity and cannot emit an unregistered
// code -- both are refused rather than discouraged. The lint-code enum in the
// declared check payload schema is pinned to the registry by a test, and the
// check guide's lint-rule table is rendered from the registry by the
// "table-lints" directive rather than repeated by hand. Each lint is also an
// option (options.go), whose registry is rendered from this one.
//
// selfdoc's check command, the unified check, the post-build lint pass and the
// posts-only check all decide the same question -- does this run pass? -- and
// they used to decide it with four copies of the same three conditions. One
// copy had drifted: it compared documented against total public symbols
// directly, hardcoding a 100% coverage requirement, so a project that lowered
// its coverage threshold passed one check and failed the other on identical
// state. This package owns the rules.
package lints

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

// DefaultCoverageThreshold is the fraction of public symbols that must be
// documented when a project does not say otherwise. It mirrors the historical
// CLI default.
const DefaultCoverageThreshold = 1.0

// registryDocumentName is the document's name as the error messages spell it.
const registryDocumentName = "lints.toml"

//go:embed lints.toml
var registryDocument []byte

// LintSpec is the registry entry for a single lint code.
type LintSpec struct {
	Code        string
	Severity    string // "error" or "warning"
	Description string
}

// LintRegistryError reports a registry document that failed strictspec
// validation: the lint registry is malformed, so selfdoc cannot know what its
// own lint codes are.
type LintRegistryError struct {
	Message string
}

func (e *LintRegistryError) Error() string { return e.Message }

// Registry is a loaded, validated lint registry.
//
// It keeps the document's order, which is documentation order: the lint-rule
// table renders in it, grouping the families rather than sorting the codes
// alphabetically.
type Registry struct {
	order []string
	specs map[string]LintSpec
}

// Codes returns every registered code in registry (documentation) order.
func (r *Registry) Codes() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Len returns how many codes the registry carries.
func (r *Registry) Len() int { return len(r.order) }

// Spec returns the code's registry entry, reporting whether the registry
// carries it.
func (r *Registry) Spec(code string) (LintSpec, bool) {
	spec, ok := r.specs[code]
	return spec, ok
}

// Has reports whether the registry carries code.
func (r *Registry) Has(code string) bool {
	_, ok := r.specs[code]
	return ok
}

// BuildRegistry validates raw registry-document bytes and binds them into a
// [Registry].
//
// strictspec is the boundary validator: the document is checked against its
// schema by the generated validator, and only a wholly valid document is
// bound. Any diagnostic is a [LintRegistryError].
func BuildRegistry(raw []byte) (*Registry, error) {
	document, diags := ValidateBytes(raw, "toml")
	if len(diags) > 0 {
		var detail strings.Builder
		for _, d := range diags {
			fmt.Fprintf(&detail, "\n  %s: %s [%s]", d.Path, d.Message, d.Code)
		}
		return nil, &LintRegistryError{Message: fmt.Sprintf(
			"%s is not a valid lint registry:%s",
			registryDocumentName, detail.String())}
	}
	reg := &Registry{
		order: make([]string, 0, len(document.Lints)),
		specs: make(map[string]LintSpec, len(document.Lints)),
	}
	for _, entry := range document.Lints {
		reg.order = append(reg.order, entry.Code)
		reg.specs[entry.Code] = LintSpec{
			Code:        entry.Code,
			Severity:    entry.Severity,
			Description: entry.Description,
		}
	}
	return reg, nil
}

// Load reads, validates and binds the embedded registry document.
//
// This is the error-returning door. Production code reads [Registered]
// instead, which loads once.
func Load() (*Registry, error) { return BuildRegistry(registryDocument) }

var (
	registeredOnce sync.Once
	registered     *Registry
	registeredErr  error
)

// Registered returns the shipped lint registry, loading and validating the
// embedded document on first use.
//
// It panics when the document is malformed, which is the Go counterpart of the
// Python surface's import-time crash: the document is embedded in the binary,
// so a diagnostic here means this build of selfdoc does not know what its own
// lint codes are. Use [Load] where an error is wanted.
func Registered() *Registry {
	registeredOnce.Do(func() { registered, registeredErr = Load() })
	if registeredErr != nil {
		panic(registeredErr)
	}
	return registered
}

// UnknownLintCodeError reports a code the registry does not carry.
//
// Every emittable code is declared in the registry document. An undeclared
// code would carry no severity, would be rejected by the JSON output schema,
// and would be invisible to the documentation table -- so it is refused at the
// construction site instead.
type UnknownLintCodeError struct {
	Message string
}

func (e *UnknownLintCodeError) Error() string { return e.Message }

// LintSeverity returns the registered severity for code, or an
// [UnknownLintCodeError].
func LintSeverity(code string) (string, error) {
	spec, ok := Registered().Spec(code)
	if !ok {
		return "", &UnknownLintCodeError{Message: fmt.Sprintf(
			"lint code '%s' is not in the registry. Every emittable code "+
				"must be declared in the lint registry (internal/lints/%s) "+
				"with its severity and description.", code, registryDocumentName)}
	}
	return spec.Severity, nil
}

// LintResult is a single lint diagnostic.
//
// Its severity is not a construction argument: it is read from the registry
// for the given code. That is what keeps severities out of the construction
// sites scattered across the check modules, and what makes an unregistered
// code impossible to emit. Every field is read-only through an accessor,
// because a diagnostic's severity is the registry's answer for its code and
// nothing downstream may rewrite it after the fact.
type LintResult struct {
	file     string
	line     *int
	code     string
	message  string
	severity string
}

// NewLintResult builds a diagnostic for code, deriving its severity from the
// registry.
//
// line is nil for a diagnostic that belongs to a file rather than to one of
// its lines. An unregistered code is an [UnknownLintCodeError].
func NewLintResult(file string, line *int, code, message string) (LintResult, error) {
	severity, err := LintSeverity(code)
	if err != nil {
		return LintResult{}, err
	}
	var lineCopy *int
	if line != nil {
		v := *line
		lineCopy = &v
	}
	return LintResult{
		file:     file,
		line:     lineCopy,
		code:     code,
		message:  message,
		severity: severity,
	}, nil
}

// MustLintResult is [NewLintResult] for a call site whose code is a literal
// declared in the registry document, and panics when it is not.
//
// The panic is the point: an unregistered literal is a defect in this
// repository rather than a condition a run can encounter, and the alternative
// -- an error return threaded through every emission site -- would make the
// emission sites decide what to do about a code that cannot exist.
func MustLintResult(file string, line *int, code, message string) LintResult {
	result, err := NewLintResult(file, line, code, message)
	if err != nil {
		panic(err)
	}
	return result
}

// File returns the diagnostic's path, relative to the docs directory.
func (l LintResult) File() string { return l.file }

// Line returns the 1-based line the diagnostic sits on, or nil when it
// belongs to the file as a whole.
func (l LintResult) Line() *int {
	if l.line == nil {
		return nil
	}
	v := *l.line
	return &v
}

// Code returns the diagnostic's registered lint code.
func (l LintResult) Code() string { return l.code }

// Message returns the diagnostic's human-readable message.
func (l LintResult) Message() string { return l.message }

// Severity returns the registry's severity for this diagnostic's code.
func (l LintResult) Severity() string { return l.severity }

// LintTableRow is one row of the rendered lint-rule table.
type LintTableRow struct {
	Code        string
	Severity    string
	Description string
}

// LintTableRows returns a row per registered lint, in registry (documentation)
// order. The documentation's lint-rule table is rendered from this and nothing
// else.
func LintTableRows() []LintTableRow {
	reg := Registered()
	out := make([]LintTableRow, 0, reg.Len())
	for _, code := range reg.Codes() {
		spec, _ := reg.Spec(code)
		out = append(out, LintTableRow{
			Code:        spec.Code,
			Severity:    spec.Severity,
			Description: spec.Description,
		})
	}
	return out
}

// RenderLintTable renders the registry as the Markdown table the documentation
// carries.
func RenderLintTable() string {
	lines := []string{
		"| Code | Severity | What it checks |",
		"| ---- | -------- | -------------- |",
	}
	for _, row := range LintTableRows() {
		lines = append(lines, fmt.Sprintf("| %s | %s | %s |",
			row.Code, row.Severity, row.Description))
	}
	return strings.Join(lines, "\n")
}

// Coverage is what the verdict rules need from a run's coverage measurement:
// how many public symbols there are and how many of them are documented.
//
// The concrete coverage type is the check package's, which measures far more
// than this; declaring the dependency as an interface here keeps this package
// below check in the import graph, as the Python module it ports was (the
// former blog package depended on selfdoc-core but not on selfdoc, so a
// posts-only install must reach the verdict without the check module
// present).
//
// Pass a nil Coverage for a run that measured no coverage. A non-nil
// interface holding a nil pointer is not the same thing and will panic.
type Coverage interface {
	TotalPublic() int
	Documented() int
}

// DirectiveOutcome is what the verdict rules need from one directive's
// resolution result: its status, which is "OK" or "FAILED".
//
// As with [Coverage], the concrete type is the check package's.
type DirectiveOutcome interface {
	Status() string
}

// CoverageBelowThreshold reports whether documented coverage is under the
// configured threshold.
//
// config is the project configuration the run was driven by, read for
// "coverage_threshold"; a nil config means no configuration is in play, which
// uses [DefaultCoverageThreshold]. The answer is false whenever there is
// nothing to measure -- no coverage at all, or a project with no public
// symbols.
func CoverageBelowThreshold(coverage Coverage, config map[string]any) bool {
	if coverage == nil || coverage.TotalPublic() <= 0 {
		return false
	}
	threshold := DefaultCoverageThreshold
	if config != nil {
		if declared, ok := numberValue(config["coverage_threshold"]); ok {
			threshold = declared
		}
	}
	return float64(coverage.Documented())/float64(coverage.TotalPublic()) < threshold
}

// numberValue reads a JSON-decoded configuration value as a float, reporting
// whether it was a number at all. A decoder yields float64, but a
// hand-constructed configuration in a test may carry any integer type, and a
// threshold written as 1 in a config file must mean the same as 1.0.
func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// CheckExitCode computes the process exit code for a check run.
//
// This is the single definition of "did this check pass": a run fails when a
// directive failed to resolve, when any lint is error-severity, or when
// documented coverage is under the configured threshold. It returns 1 when the
// run fails and 0 when it passes.
//
// lints are the diagnostics the run produced, with the repository's lint
// options already applied ([Settings.Apply]). directiveResults are the per-directive
// resolution results, and are nil for a reduced entry point that resolved no
// directives (the post-build lint pass, the posts-only check). coverage is nil
// for an entry point that measured no coverage.
func CheckExitCode(
	lints []LintResult,
	directiveResults []DirectiveOutcome,
	coverage Coverage,
	config map[string]any,
) int {
	for _, dr := range directiveResults {
		if dr.Status() == "FAILED" {
			return 1
		}
	}
	for _, lint := range lints {
		if lint.severity == "error" {
			return 1
		}
	}
	if CoverageBelowThreshold(coverage, config) {
		return 1
	}
	return 0
}
