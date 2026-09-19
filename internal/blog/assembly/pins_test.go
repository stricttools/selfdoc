package assembly

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The versions the pin tests name explicitly. They live beside the stub
// registry that has to claim them published, because the two are one decision:
// a pin the stub does not serve is an unpublished pin and the check under test
// refuses it.
const (
	pinnedSelfdoc  = "0.36.0"
	pinnedPagefind = "1.4.0"
)

// stubRegistry stands in for the two registries a pin comes from, over a fixed
// set of published versions.
//
// Nothing in this package's tests is allowed to dial a registry -- the
// isolation floor denies it -- so every path that checks a pin answers from
// this instead. Published is mutable: a test that cares which versions exist
// rewrites the entry for its package.
type stubRegistry struct {
	// Published maps a PyPI distribution to the versions it serves, newest
	// last.
	Published map[string][]string
	// GoVersions is the selfdoc versions the module proxy serves.
	GoVersions []string
	// Asked records every PyPI distribution the code under test asked about.
	Asked []string
	// AskedGo records every selfdoc version it asked the module proxy about.
	AskedGo []string
	// PyPIError, when set, is what every PyPI read fails with.
	PyPIError error
}

// newStubRegistry serves the versions every pin test starts from.
func newStubRegistry() *stubRegistry {
	return &stubRegistry{
		Published: map[string][]string{
			"pagefind": {"1.3.0", pinnedPagefind},
			"selfdoc":  {"0.35.0", pinnedSelfdoc},
		},
		GoVersions: []string{"0.35.0", pinnedSelfdoc},
	}
}

// Registry is the seam the code under test takes.
func (s *stubRegistry) Registry() Registry {
	return Registry{PyPI: s.pypi, GoModule: s.goModule}
}

// pypi answers one metadata read.
func (s *stubRegistry) pypi(pkg string) (map[string]any, error) {
	s.Asked = append(s.Asked, pkg)
	if s.PyPIError != nil {
		return nil, s.PyPIError
	}
	versions, ok := s.Published[pkg]
	if !ok {
		return nil, errorf("%s is not a package on PyPI (stub)", pkg)
	}
	releases := map[string]any{}
	for _, version := range versions {
		releases[version] = []any{
			map[string]any{"filename": pkg + "-" + version + ".whl"},
		}
	}
	return map[string]any{
		"info":     map[string]any{"version": versions[len(versions)-1]},
		"releases": releases,
	}, nil
}

// goModule answers one module-proxy probe.
func (s *stubRegistry) goModule(version string) (bool, error) {
	s.AskedGo = append(s.AskedGo, version)
	return slices.Contains(s.GoVersions, version), nil
}

// testPins is a complete set of pins the stub registry serves.
var testPins = ToolchainPins{Selfdoc: pinnedSelfdoc, Pagefind: pinnedPagefind}

// -- the pins themselves -----------------------------------------------------

func TestPinsRefuseToBeEmpty(t *testing.T) {
	for _, test := range []struct {
		name  string
		pins  ToolchainPins
		field string
	}{
		{"no selfdoc", ToolchainPins{Pagefind: "1.0"}, "Selfdoc"},
		{"no pagefind", ToolchainPins{Selfdoc: "1.0"}, "Pagefind"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.pins.Validate()
			if err == nil {
				t.Fatal("an incomplete pin set was accepted")
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Fatalf("err = %q, want it to name %s", err, test.field)
			}
		})
	}
}

func TestCompletePinsValidate(t *testing.T) {
	if err := testPins.Validate(); err != nil {
		t.Fatalf("a complete pin set was refused: %v", err)
	}
}

func TestPyPIPinsNamesOnlyWhatPyPIServes(t *testing.T) {
	// The selfdoc pin is a Go module version, so it is not in here: the
	// module proxy answers for it, and asking PyPI about a distribution that
	// no longer exists there would refuse every pin set.
	want := map[string]string{"pagefind": pinnedPagefind}
	if got := testPins.PyPIPins(); !reflect.DeepEqual(got, want) {
		t.Fatalf("PyPIPins() = %v, want %v", got, want)
	}
}

// -- the registry addresses --------------------------------------------------

func TestPyPIURLTargetsTheJSONAPI(t *testing.T) {
	if got := PyPIURL("pagefind"); got != "https://pypi.org/pypi/pagefind/json" {
		t.Fatalf("PyPIURL = %q", got)
	}
}

func TestGoModulePathIsTheModuleRoot(t *testing.T) {
	// The entry point is the module root, so the generated workflow installs
	// the module itself and the binary takes its name from the last path
	// element. A path with a cmd/ suffix would name a package that no longer
	// exists and the deploy's "go install" would fail at dispatch.
	if GoModulePath != "github.com/stricttools/selfdoc" {
		t.Fatalf("GoModulePath = %q", GoModulePath)
	}
}

func TestGoProxyURLTargetsTheVersionInfoEndpoint(t *testing.T) {
	want := "https://proxy.golang.org/github.com/stricttools/selfdoc/@v/v0.39.0.info"
	if got := GoProxyURL("0.39.0"); got != want {
		t.Fatalf("GoProxyURL = %q, want %q", got, want)
	}
}

// -- the published check -----------------------------------------------------

func TestCheckPinsAcceptsPublishedVersions(t *testing.T) {
	stub := newStubRegistry()
	if err := CheckPinsArePublished(testPins, stub.Registry()); err != nil {
		t.Fatalf("published pins were refused: %v", err)
	}
	if !slices.Contains(stub.Asked, "pagefind") {
		t.Error("the pagefind pin was never checked against PyPI")
	}
	if !slices.Contains(stub.AskedGo, pinnedSelfdoc) {
		t.Error("the selfdoc pin was never checked against the module proxy")
	}
}

func TestAnUnpublishedPagefindPinNamesTheRegistryAndVersion(t *testing.T) {
	stub := newStubRegistry()
	pins := ToolchainPins{Selfdoc: pinnedSelfdoc, Pagefind: "7.7.7"}
	err := CheckPinsArePublished(pins, stub.Registry())
	if err == nil {
		t.Fatal("an unpublished pagefind pin was accepted")
	}
	for _, want := range []string{"7.7.7", "pypi.org", "pagefind"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestAnUnpublishedSelfdocPinNamesTheModuleProxy(t *testing.T) {
	stub := newStubRegistry()
	pins := ToolchainPins{Selfdoc: "4.5.6", Pagefind: pinnedPagefind}
	err := CheckPinsArePublished(pins, stub.Registry())
	if err == nil {
		t.Fatal("an unpublished selfdoc pin was accepted")
	}
	for _, want := range []string{"4.5.6", "proxy.golang.org", "go install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestAReleaseWithNoFilesCountsAsUnpublished(t *testing.T) {
	// A version whose artifacts were removed cannot be pip-installed.
	stub := newStubRegistry()
	stub.Published["pagefind"] = []string{"1.2.3"}
	registry := Registry{
		PyPI: func(pkg string) (map[string]any, error) {
			return map[string]any{
				"info":     map[string]any{"version": "1.2.3"},
				"releases": map[string]any{"1.2.3": []any{}},
			}, nil
		},
		GoModule: stub.goModule,
	}
	pins := ToolchainPins{Selfdoc: pinnedSelfdoc, Pagefind: "1.2.3"}
	if err := CheckPinsArePublished(pins, registry); err == nil ||
		!strings.Contains(err.Error(), "1.2.3") {
		t.Fatalf("err = %v, want the unpublished refusal", err)
	}
}

func TestMetadataWithNoReleasesIsRefused(t *testing.T) {
	registry := Registry{
		PyPI: func(pkg string) (map[string]any, error) {
			return map[string]any{"info": map[string]any{"version": "1.0"}}, nil
		},
		GoModule: func(string) (bool, error) { return true, nil },
	}
	err := CheckPinsArePublished(testPins, registry)
	if err == nil || !strings.Contains(err.Error(), "lists no releases") {
		t.Fatalf("err = %v, want the no-releases refusal", err)
	}
}

func TestARegistryThatCannotBeReachedIsAHardError(t *testing.T) {
	stub := newStubRegistry()
	stub.PyPIError = errorf("could not read https://pypi.org/pypi/pagefind/json: refused")
	err := CheckPinsArePublished(testPins, stub.Registry())
	if err == nil || !strings.Contains(err.Error(), "pypi.org") {
		t.Fatalf("err = %v, want the unreachable-registry error", err)
	}
}

func TestAModuleProxyThatCannotAnswerIsNeverReadAsUnpublished(t *testing.T) {
	registry := Registry{
		PyPI: newStubRegistry().pypi,
		GoModule: func(string) (bool, error) {
			return false, errorf("could not read %s: HTTP 503", GoProxyURL(pinnedSelfdoc))
		},
	}
	err := CheckPinsArePublished(testPins, registry)
	if err == nil {
		t.Fatal("an unanswerable probe was accepted")
	}
	if strings.Contains(err.Error(), "is not served") {
		t.Fatalf("a failed probe was reported as an unpublished pin: %q", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %q, want it to carry the probe's own failure", err)
	}
}

func TestCheckPinsRefusesAnIncompletePinSet(t *testing.T) {
	stub := newStubRegistry()
	err := CheckPinsArePublished(ToolchainPins{Pagefind: pinnedPagefind}, stub.Registry())
	if err == nil || !strings.Contains(err.Error(), "Selfdoc") {
		t.Fatalf("err = %v, want the incomplete-pins refusal", err)
	}
	if len(stub.Asked) != 0 {
		t.Error("an incomplete pin set was taken to the registry anyway")
	}
}

// -- reading the registry ----------------------------------------------------

func TestRegistryLatestVersionReadsTheCurrentRelease(t *testing.T) {
	stub := newStubRegistry()
	got, err := RegistryLatestVersion("pagefind", stub.pypi)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != pinnedPagefind {
		t.Fatalf("latest = %q, want %q", got, pinnedPagefind)
	}
}

func TestRegistryLatestVersionRefusesMetadataWithNoVersion(t *testing.T) {
	_, err := RegistryLatestVersion("pagefind", func(string) (map[string]any, error) {
		return map[string]any{"info": map[string]any{}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "names no current version") {
		t.Fatalf("err = %v, want the no-version refusal", err)
	}
}

// -- resolution --------------------------------------------------------------

func TestResolvePinsTakesExplicitValuesVerbatim(t *testing.T) {
	stub := newStubRegistry()
	pins, err := ResolveToolchainPins(PinOptions{
		SelfdocVersion:  "0.35.0",
		PagefindVersion: "1.3.0",
		Registry:        stub.Registry(),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := ToolchainPins{Selfdoc: "0.35.0", Pagefind: "1.3.0"}
	if pins != want {
		t.Fatalf("pins = %+v, want %+v", pins, want)
	}
	if len(stub.Asked) != 0 {
		t.Fatalf("explicit pins asked the registry about %v", stub.Asked)
	}
}

func TestResolvePinsRefusesAnUnstatedSelfdocVersion(t *testing.T) {
	// The binary is the module root, so this package cannot import it to read
	// the running version. The caller states it; an omission is refused by
	// name rather than filled in.
	stub := newStubRegistry()
	_, err := ResolveToolchainPins(PinOptions{Registry: stub.Registry()})
	if err == nil {
		t.Fatal("a pin set with no selfdoc version resolved anyway")
	}
	if !strings.Contains(err.Error(), "Selfdoc") {
		t.Fatalf("err = %q, want it to name the Selfdoc field", err)
	}
}

func TestResolvePinsAsksTheRegistryOnlyForPagefind(t *testing.T) {
	stub := newStubRegistry()
	pins, err := ResolveToolchainPins(PinOptions{
		SelfdocVersion: pinnedSelfdoc,
		Registry:       stub.Registry(),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !reflect.DeepEqual(stub.Asked, []string{"pagefind"}) {
		t.Fatalf("the resolver asked about %v, want [pagefind]", stub.Asked)
	}
	if pins.Pagefind != pinnedPagefind {
		t.Fatalf("pagefind pin = %q", pins.Pagefind)
	}
}

func TestResolvePinsPropagatesARegistryFailure(t *testing.T) {
	stub := newStubRegistry()
	stub.PyPIError = errorf("could not read https://pypi.org/pypi/pagefind/json: refused")
	_, err := ResolveToolchainPins(PinOptions{
		SelfdocVersion: pinnedSelfdoc,
		Registry:       stub.Registry(),
	})
	if err == nil {
		t.Fatal("a registry failure resolved to a pin anyway")
	}
}
