package assembly

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PyPIJSONURL is PyPI's JSON metadata endpoint, the registry the pagefind pin
// comes from. A pin is checked against it before it is written into a workflow
// -- see [CheckPinsArePublished].
const PyPIJSONURL = "https://pypi.org/pypi/{package}/json"

// GoProxyInfoURL is the Go module proxy's version-info endpoint for this
// module, the registry the selfdoc pin comes from.
//
// The selfdoc pin names a Go module version rather than a PyPI distribution:
// the workflow installs the binary with "go install
// github.com/stricttools/selfdoc@v<version>", so what has to exist is a tag the
// proxy serves, and PyPI has nothing to say about it.
const GoProxyInfoURL = "https://proxy.golang.org/github.com/stricttools/selfdoc/@v/v{version}.info"

// GoModulePath is the module the generated workflow installs the binary from.
// The entry point is the module root, so installing the module installs the
// binary and the last path element names it.
const GoModulePath = "github.com/stricttools/selfdoc"

// registryTimeout bounds every registry read. A pin check that hangs would
// hang a release.
const registryTimeout = 15 * time.Second

// PyPIURL is the metadata address of one PyPI distribution.
func PyPIURL(pkg string) string {
	return strings.Replace(PyPIJSONURL, "{package}", pkg, 1)
}

// GoProxyURL is the module proxy's info address for one selfdoc version.
func GoProxyURL(version string) string {
	return strings.Replace(GoProxyInfoURL, "{version}", version, 1)
}

// ToolchainPins is the exact versions the generated deploy workflow installs.
//
// Both tools on the install line are pinned, for one reason that applies
// equally to each: the deploy installs its toolchain fresh on every dispatch,
// so an unpinned name means "whatever was newest at dispatch time". A released
// flag change once broke every project's deploy at once that way.
//
// The pins are not the banned kind of ceiling: "assembly sync-workflow"
// rewrites the whole file on every release, so this is a regenerated lock, not
// an upper bound a human wrote once and forgot.
//
// One selfdoc version covers the whole toolchain now. The Python installed two
// distributions -- the docs generator and the former blog package -- and
// pinned each; one binary carries both, so there is one pin and one flag for
// it.
//
// Every field is required. [ResolveToolchainPins] is what turns an environment
// into a set of pins; this type only carries them.
type ToolchainPins struct {
	// Selfdoc is the selfdoc release the workflow installs with "go install".
	Selfdoc string
	// Pagefind is the pagefind release the workflow pip-installs.
	Pagefind string
}

// Validate reports the first empty field, naming it.
//
// Go cannot refuse an incomplete struct literal at construction the way
// Python's frozen dataclass refused it in __post_init__, so every consumer of
// a ToolchainPins calls this first: the renderer, the publication check, and
// the resolver on its way out.
func (p ToolchainPins) Validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{{"Selfdoc", p.Selfdoc}, {"Pagefind", p.Pagefind}} {
		if field.value == "" {
			return errorf(
				"ToolchainPins.%s is required: the generated workflow pins "+
					"every tool it installs, and there is no default version.",
				field.name,
			)
		}
	}
	return nil
}

// PyPIPins returns PyPI distribution name -> pinned version, for every pin
// PyPI actually serves.
//
// That is pagefind alone. The keys are registry names, not install
// specifiers: pagefind is installed as "pagefind[bin]" but published as
// "pagefind". The selfdoc pin is a Go module version and is checked against
// the module proxy instead -- see [CheckPinsArePublished].
func (p ToolchainPins) PyPIPins() map[string]string {
	return map[string]string{"pagefind": p.Pagefind}
}

// PyPIFetcher returns PyPI's JSON metadata document for a distribution.
type PyPIFetcher func(pkg string) (map[string]any, error)

// GoModuleProbe reports whether the Go module proxy serves a selfdoc version.
//
// The two outcomes are kept apart from the error: false means the proxy
// answered and does not have that version, and an error means the question
// could not be asked at all. A probe that cannot answer is never read as
// "published".
type GoModuleProbe func(version string) (bool, error)

// Registry is how the pin checks reach the two registries a pin comes from.
//
// A nil field selects the real reader -- [FetchPyPIMetadata] and
// [GoModuleVersionExists]. That is explicit mode selection, not a fallback:
// the choice is made once, before anything runs, and nothing here ever tries
// the network, fails, and answers from somewhere else.
type Registry struct {
	// PyPI reads a distribution's metadata document.
	PyPI PyPIFetcher
	// GoModule reports whether a selfdoc version is served.
	GoModule GoModuleProbe
}

// pypi is the configured PyPI reader, or the real one.
func (r Registry) pypi() PyPIFetcher {
	if r.PyPI != nil {
		return r.PyPI
	}
	return FetchPyPIMetadata
}

// goModule is the configured module probe, or the real one.
func (r Registry) goModule() GoModuleProbe {
	if r.GoModule != nil {
		return r.GoModule
	}
	return GoModuleVersionExists
}

// FetchPyPIMetadata returns PyPI's JSON metadata document for pkg.
//
// It is a GET: it changes nothing, which is why it does not go through the
// effects handle -- a recorded read would have nothing to record and a preview
// still needs the answer.
func FetchPyPIMetadata(pkg string) (map[string]any, error) {
	url := PyPIURL(pkg)
	body, status, err := httpGet(url)
	if err != nil {
		return nil, errorf("could not read %s: %s", url, err)
	}
	if status == http.StatusNotFound {
		return nil, errorf("%s is not a package on PyPI (%s returned 404)", pkg, url)
	}
	if status != http.StatusOK {
		return nil, errorf("could not read %s: HTTP %d", url, status)
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, errorf("could not read %s: %s", url, err)
	}
	return data, nil
}

// GoModuleVersionExists reports whether the Go module proxy serves version of
// this module.
//
// The proxy answers 404 or 410 for a version it does not have; every other
// non-200 status is a read that did not happen and is an error rather than a
// "no", so a proxy outage can never be read as an unpublished pin.
func GoModuleVersionExists(version string) (bool, error) {
	url := GoProxyURL(version)
	_, status, err := httpGet(url)
	if err != nil {
		return false, errorf("could not read %s: %s", url, err)
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound, http.StatusGone:
		return false, nil
	default:
		return false, errorf("could not read %s: HTTP %d", url, status)
	}
}

// httpGet performs one bounded GET and returns the body and the status.
func httpGet(url string) ([]byte, int, error) {
	client := &http.Client{Timeout: registryTimeout}
	response, err := client.Get(url)
	if err != nil {
		return nil, 0, unwrapURLError(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

// unwrapURLError strips net/http's "Get \"<url>\": " prefix, so a message
// that already names the URL does not name it twice.
func unwrapURLError(err error) error {
	text := err.Error()
	if index := strings.Index(text, `": `); index >= 0 && strings.HasPrefix(text, "Get \"") {
		return fmt.Errorf("%s", text[index+3:])
	}
	return err
}

// RegistryLatestVersion returns the version PyPI currently serves as pkg's
// latest.
func RegistryLatestVersion(pkg string, fetch PyPIFetcher) (string, error) {
	if fetch == nil {
		fetch = FetchPyPIMetadata
	}
	data, err := fetch(pkg)
	if err != nil {
		return "", err
	}
	version := ""
	if info, ok := data["info"].(map[string]any); ok {
		if text, ok := info["version"].(string); ok {
			version = text
		}
	}
	if version == "" {
		return "", errorf(
			"PyPI's metadata for %s names no current version (%s)",
			pkg, PyPIURL(pkg),
		)
	}
	return version, nil
}

// CheckPinsArePublished returns an error unless every pinned version can
// actually be installed.
//
// "assembly sync-workflow" defaults the selfdoc pin to the *running* binary's
// version, which in a development checkout sits ahead of the registry the
// moment work starts on the next version. Writing that pin produces a workflow
// whose install cannot resolve, and the failure surfaces at the next dispatch,
// on the assembly repository, far from whoever wrote it. So the pins are
// checked here, before anything is written.
//
// The two pins are checked against two different registries, because they name
// two different kinds of thing. The pagefind pin is a PyPI distribution: a
// version that exists but has no files is unpublished for this purpose, since
// pip cannot install it either. The selfdoc pin is a Go module version, and
// what has to exist is a tag the module proxy serves.
func CheckPinsArePublished(pins ToolchainPins, reg Registry) error {
	if err := pins.Validate(); err != nil {
		return err
	}

	for pkg, version := range pins.PyPIPins() {
		url := PyPIURL(pkg)
		data, err := reg.pypi()(pkg)
		if err != nil {
			return err
		}
		releases, ok := data["releases"].(map[string]any)
		if !ok {
			return errorf("PyPI's metadata for %s lists no releases (%s)", pkg, url)
		}
		if !hasFiles(releases[version]) {
			return errorf(
				"%s %s is not published on PyPI (%s). The generated workflow "+
					"would run 'pip install %s==%s', which cannot resolve, "+
					"and the deploy would fail on the assembly repository at "+
					"the next dispatch. Release %s %s first, or name a "+
					"published version explicitly.",
				pkg, version, url, pkg, version, pkg, version,
			)
		}
	}

	served, err := reg.goModule()(pins.Selfdoc)
	if err != nil {
		return err
	}
	if !served {
		return errorf(
			"selfdoc %s is not served by the Go module proxy (%s returned "+
				"no version). The generated workflow would run 'go install "+
				"%s@v%s', which cannot resolve, and the deploy would fail on "+
				"the assembly repository at the next dispatch. Release "+
				"selfdoc %s first, or name a published version explicitly.",
			pins.Selfdoc, GoProxyURL(pins.Selfdoc),
			GoModulePath, pins.Selfdoc, pins.Selfdoc,
		)
	}
	return nil
}

// hasFiles reports whether a PyPI releases entry names at least one file,
// which is Python's truthiness of the entry: a missing key, a null, and an
// empty file list are each "unpublished".
func hasFiles(entry any) bool {
	files, ok := entry.([]any)
	return ok && len(files) > 0
}

// PinOptions is what [ResolveToolchainPins] takes.
//
// A named version is taken verbatim. PagefindVersion is "" for "resolve it";
// SelfdocVersion has nothing to resolve from here and is required.
type PinOptions struct {
	// SelfdocVersion pins the binary the workflow installs. It is required:
	// this package cannot read the running binary's version -- the binary is
	// the module root and importing it is impossible -- so the caller states
	// it. internal/cli passes the version the binary was built with.
	SelfdocVersion string
	// PagefindVersion pins the indexer. Empty means PyPI's current release.
	PagefindVersion string
	// Registry is how the resolution reads PyPI. A zero Registry reads the
	// real one.
	Registry Registry
}

// ResolveToolchainPins resolves the two pins the generated workflow installs.
//
// An explicitly supplied version is taken verbatim. Otherwise:
//
//   - selfdoc has no fallback here: an empty SelfdocVersion is refused by
//     [ToolchainPins.Validate], naming the field. The version of the running
//     binary is the binary's own fact, and it is handed down through
//     internal/cli rather than read here.
//   - pagefind is PyPI's current release. pagefind is a CI-only tool this
//     module does not depend on, so there is no installed distribution to read
//     a version from -- the honest options are the registry's current release
//     or an explicitly named version, and the registry answer is the one that
//     keeps regenerating on every release the way the selfdoc pin does. This
//     is the one pin that does not describe the machine doing the generating;
//     that difference is deliberate and there is nowhere better to read it
//     from.
func ResolveToolchainPins(opts PinOptions) (ToolchainPins, error) {
	pins := ToolchainPins{
		Selfdoc:  opts.SelfdocVersion,
		Pagefind: opts.PagefindVersion,
	}
	if pins.Pagefind == "" {
		latest, err := RegistryLatestVersion("pagefind", opts.Registry.PyPI)
		if err != nil {
			return ToolchainPins{}, err
		}
		pins.Pagefind = latest
	}
	if err := pins.Validate(); err != nil {
		return ToolchainPins{}, err
	}
	return pins, nil
}
