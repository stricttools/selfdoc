package lints

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// -- The registry itself ------------------------------------------------------

func TestEmbeddedDocumentValidates(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("the embedded registry document is not valid: %v", err)
	}
	if reg.Len() == 0 {
		t.Fatal("the registry is empty")
	}
}

func TestEveryEntryIsKeyedByItsOwnCode(t *testing.T) {
	reg := Registered()
	for _, code := range reg.Codes() {
		spec, ok := reg.Spec(code)
		if !ok {
			t.Fatalf("%s is not retrievable from the registry", code)
		}
		if spec.Code != code {
			t.Errorf("entry keyed %s carries code %s", code, spec.Code)
		}
	}
}

func TestEverySeverityIsErrorOrWarning(t *testing.T) {
	reg := Registered()
	for _, code := range reg.Codes() {
		spec, _ := reg.Spec(code)
		if spec.Severity != "error" && spec.Severity != "warning" {
			t.Errorf("%s: severity %q is outside the closed set", code, spec.Severity)
		}
	}
}

func TestEveryEntryHasADescription(t *testing.T) {
	reg := Registered()
	for _, code := range reg.Codes() {
		spec, _ := reg.Spec(code)
		if strings.TrimSpace(spec.Description) == "" {
			t.Errorf("%s: no description for the documentation table", code)
		}
	}
}

func TestRegistryKeepsDocumentOrder(t *testing.T) {
	document, diags := ValidateBytes(registryDocument, "toml")
	if len(diags) > 0 {
		t.Fatalf("the embedded registry document is not valid: %v", diags)
	}
	want := make([]string, 0, len(document.Lints))
	for _, entry := range document.Lints {
		want = append(want, entry.Code)
	}
	if !reflect.DeepEqual(Registered().Codes(), want) {
		t.Fatalf("Codes() = %v, want the document order %v", Registered().Codes(), want)
	}
}

// -- Emission is structurally constrained -------------------------------------

func TestLintResultDerivesSeverityFromTheRegistry(t *testing.T) {
	for _, code := range []string{"multiple-top-level-headings", "skipped-heading-level"} {
		want, err := LintSeverity(code)
		if err != nil {
			t.Fatalf("LintSeverity(%q) = %v", code, err)
		}
		got := MustLintResult("a.md", intp(1), code, "m")
		if got.Severity() != want {
			t.Errorf("%s: severity %q, want %q", code, got.Severity(), want)
		}
	}
}

func TestLintResultRefusesAnUnregisteredCode(t *testing.T) {
	_, err := NewLintResult("a.md", intp(1), "NOPE001", "m")
	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	var unknown *UnknownLintCodeError
	if !errors.As(err, &unknown) {
		t.Fatalf("error %T is not an *UnknownLintCodeError", err)
	}
	if !strings.Contains(err.Error(), "NOPE001") {
		t.Errorf("message %q does not name the code", err.Error())
	}
	if !strings.Contains(err.Error(), "lints.toml") {
		t.Errorf("message %q does not name the registry document", err.Error())
	}
}

func TestLintSeverityRefusesAnUnregisteredCode(t *testing.T) {
	_, err := LintSeverity("NOPE001")
	want := "lint code 'NOPE001' is not in the registry. Every emittable code " +
		"must be declared in the lint registry (internal/lints/lints.toml) with its severity and description."
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestLintResultAccessorsReturnCopies(t *testing.T) {
	line := 7
	result := MustLintResult("a.md", &line, "multiple-top-level-headings", "m")
	line = 99
	if got := result.Line(); got == nil || *got != 7 {
		t.Fatalf("Line() = %v, want 7 -- the constructor did not copy", got)
	}
	returned := result.Line()
	*returned = 42
	if got := result.Line(); *got != 7 {
		t.Fatalf("Line() = %d after the caller wrote to a returned pointer", *got)
	}
}

func TestLintResultLineIsOptional(t *testing.T) {
	result := MustLintResult("a.md", nil, "multiple-top-level-headings", "m")
	if result.Line() != nil {
		t.Fatalf("Line() = %v, want nil", result.Line())
	}
}

// -- The documentation table is derived ---------------------------------------

func TestRenderLintTableIsTheRegistry(t *testing.T) {
	rendered := RenderLintTable()
	rows := strings.Split(rendered, "\n")
	if rows[0] != "| Code | Severity | What it checks |" {
		t.Fatalf("header row = %q", rows[0])
	}
	if rows[1] != "| ---- | -------- | -------------- |" {
		t.Fatalf("separator row = %q", rows[1])
	}
	reg := Registered()
	if len(rows) != reg.Len()+2 {
		t.Fatalf("table has %d rows, want %d", len(rows), reg.Len()+2)
	}
	for i, code := range reg.Codes() {
		spec, _ := reg.Spec(code)
		want := "| " + spec.Code + " | " + spec.Severity + " | " + spec.Description + " |"
		if rows[i+2] != want {
			t.Errorf("row %d = %q, want %q", i+2, rows[i+2], want)
		}
	}
}

func TestLintTableRowsIsRegistryOrder(t *testing.T) {
	got := make([]string, 0)
	for _, row := range LintTableRows() {
		got = append(got, row.Code)
	}
	if !reflect.DeepEqual(got, Registered().Codes()) {
		t.Fatalf("LintTableRows order = %v, want %v", got, Registered().Codes())
	}
}

// -- The verdict rules --------------------------------------------------------

// stubCoverage is a coverage measurement standing in for the check package's
// own, which measures far more than the verdict rules read.
type stubCoverage struct {
	total      int
	documented int
}

func (c stubCoverage) TotalPublic() int { return c.total }
func (c stubCoverage) Documented() int  { return c.documented }

// stubOutcome is one directive's resolution status.
type stubOutcome struct{ status string }

func (o stubOutcome) Status() string { return o.status }

func TestCoverageBelowThreshold(t *testing.T) {
	tests := []struct {
		label    string
		coverage Coverage
		config   map[string]any
		want     bool
	}{
		{label: "nothing measured is not below anything", coverage: nil, want: false},
		{
			label:    "a project with no public symbols is not below anything",
			coverage: stubCoverage{total: 0, documented: 0},
			want:     false,
		},
		{
			label:    "full coverage meets the default threshold",
			coverage: stubCoverage{total: 4, documented: 4},
			want:     false,
		},
		{
			label:    "partial coverage is below the default threshold",
			coverage: stubCoverage{total: 4, documented: 3},
			want:     true,
		},
		{
			label:    "a lowered threshold is honored",
			coverage: stubCoverage{total: 4, documented: 3},
			config:   map[string]any{"coverage_threshold": 0.5},
			want:     false,
		},
		{
			label:    "a threshold written as an integer means the same as the float",
			coverage: stubCoverage{total: 4, documented: 3},
			config:   map[string]any{"coverage_threshold": 1},
			want:     true,
		},
		{
			label:    "a configuration with no threshold uses the default",
			coverage: stubCoverage{total: 4, documented: 3},
			config:   map[string]any{"base_url": "https://example.test"},
			want:     true,
		},
		{
			label:    "a threshold just above the measurement is below",
			coverage: stubCoverage{total: 4, documented: 3},
			config:   map[string]any{"coverage_threshold": 0.76},
			want:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			if got := CoverageBelowThreshold(tc.coverage, tc.config); got != tc.want {
				t.Fatalf("CoverageBelowThreshold = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckExitCode(t *testing.T) {
	warning := MustLintResult("a.md", intp(1), "first-paragraph-length-out-of-range", "a warning")
	errorLint := MustLintResult("a.md", intp(1), "broken-emitted-reference", "a broken link")

	tests := []struct {
		label      string
		lints      []LintResult
		directives []DirectiveOutcome
		coverage   Coverage
		config     map[string]any
		want       int
	}{
		{label: "an empty run passes", want: 0},
		{
			label: "warnings alone pass",
			lints: []LintResult{warning},
			want:  0,
		},
		{
			label: "an error-severity lint fails",
			lints: []LintResult{warning, errorLint},
			want:  1,
		},
		{
			label:      "a resolved directive passes",
			directives: []DirectiveOutcome{stubOutcome{status: "OK"}},
			want:       0,
		},
		{
			label: "an unresolved directive fails",
			directives: []DirectiveOutcome{
				stubOutcome{status: "OK"}, stubOutcome{status: "FAILED"},
			},
			want: 1,
		},
		{
			label:    "coverage under the threshold fails",
			coverage: stubCoverage{total: 4, documented: 1},
			want:     1,
		},
		{
			label:    "coverage under a lowered threshold passes",
			coverage: stubCoverage{total: 4, documented: 1},
			config:   map[string]any{"coverage_threshold": 0.2},
			want:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			got := CheckExitCode(tc.lints, tc.directives, tc.coverage, tc.config)
			if got != tc.want {
				t.Fatalf("CheckExitCode = %d, want %d", got, tc.want)
			}
		})
	}
}

// -- Document validation ------------------------------------------------------

// validRegistry is a well-formed minimal registry document, the base every
// malformed variant below is derived from.
const validRegistry = "format_version = 1\n" +
	"[[lints]]\n" +
	"code = \"multiple-top-level-headings\"\n" +
	"severity = \"error\"\n" +
	"description = \"Multiple H1 headings on a page.\"\n"

func diagCodes(t *testing.T, document string) []string {
	t.Helper()
	_, diags := ValidateBytes([]byte(document), "toml")
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return codes
}

func TestMinimalValidRegistryHasNoDiagnostics(t *testing.T) {
	if codes := diagCodes(t, validRegistry); len(codes) != 0 {
		t.Fatalf("the minimal registry produced %v", codes)
	}
}

func TestMalformedRegistriesAreRejected(t *testing.T) {
	tests := []struct {
		label    string
		document string
		want     string
	}{
		{
			label:    "a code outside the grammar",
			document: strings.Replace(validRegistry, `code = "multiple-top-level-headings"`, `code = "seo1"`, 1),
			want:     "STRICTSPEC_VALUE_STRING_REGEX",
		},
		{
			label:    "a severity outside the enum",
			document: strings.Replace(validRegistry, `severity = "error"`, `severity = "loud"`, 1),
			want:     "STRICTSPEC_TYPE_NOT_ENUM_MEMBER",
		},
		{
			label:    "an unknown key",
			document: validRegistry + "extra_junk = \"x\"\n",
			want:     "STRICTSPEC_KEY_UNKNOWN",
		},
		{
			label: "a missing required field",
			document: strings.Replace(validRegistry,
				"description = \"Multiple H1 headings on a page.\"\n", "", 1),
			want: "STRICTSPEC_TYPE_MISSING_REQUIRED",
		},
		{
			label:    "no format_version marker",
			document: strings.Replace(validRegistry, "format_version = 1\n", "", 1),
			want:     "STRICTSPEC_GATE_ABSENT",
		},
		{
			label: "a duplicate code",
			document: validRegistry + "[[lints]]\n" +
				"code = \"multiple-top-level-headings\"\n" +
				"severity = \"warning\"\n" +
				"description = \"dup\"\n",
			want: "STRICTSPEC_INTRA_UNIQUE_BY",
		},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			codes := diagCodes(t, tc.document)
			if !containsString(codes, tc.want) {
				t.Fatalf("diagnostics %v do not include %s", codes, tc.want)
			}
		})
	}
}

func TestBuildRegistryRefusesAMalformedDocument(t *testing.T) {
	bad := strings.Replace(validRegistry, `severity = "error"`, `severity = "loud"`, 1)
	_, err := BuildRegistry([]byte(bad))
	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	var regErr *LintRegistryError
	if !errors.As(err, &regErr) {
		t.Fatalf("error %T is not a *LintRegistryError", err)
	}
	if !strings.HasPrefix(err.Error(), "lints.toml is not a valid lint registry:\n") {
		t.Fatalf("message %q does not name the document", err.Error())
	}
	if !strings.Contains(err.Error(), "STRICTSPEC_TYPE_NOT_ENUM_MEMBER") {
		t.Fatalf("message %q does not name the diagnostic", err.Error())
	}
}

func TestBuildRegistryBindsAValidDocument(t *testing.T) {
	reg, err := BuildRegistry([]byte(validRegistry))
	if err != nil {
		t.Fatalf("BuildRegistry returned %v", err)
	}
	if !reflect.DeepEqual(reg.Codes(), []string{"multiple-top-level-headings"}) {
		t.Fatalf("codes = %v, want [multiple-top-level-headings]", reg.Codes())
	}
	spec, ok := reg.Spec("multiple-top-level-headings")
	if !ok {
		t.Fatal("the bound registry does not carry multiple-top-level-headings")
	}
	if spec.Severity != "error" {
		t.Errorf("severity = %q, want error", spec.Severity)
	}
	if spec.Description != "Multiple H1 headings on a page." {
		t.Errorf("description = %q", spec.Description)
	}
}

// -- helpers ------------------------------------------------------------------

func intp(v int) *int { return &v }

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// A lint's name is also the name of the option that governs it, so it follows
// the option-name grammar with no digits: lowercase words joined by hyphens.
func TestEveryLintNameIsLowercaseKebabCaseWithoutDigits(t *testing.T) {
	name := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	for _, code := range Registered().Codes() {
		if !name.MatchString(code) {
			t.Errorf("lint %q is not lowercase words joined by hyphens", code)
		}
	}
}
