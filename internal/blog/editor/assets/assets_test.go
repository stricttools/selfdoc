package assets

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/testisolation/go/hygiene"
)

// isolate binds the environment-isolation floor. Resolving a "~" path reads
// the home directory out of the environment, so a test that read the
// developer's real home would answer differently on every machine.
//
// Nothing here calls t.Parallel: hygiene mutates process-wide variables.
func isolate(t *testing.T) {
	t.Helper()
	hygiene.Isolate(t)
}

// fakeTinymoon writes a tinymoon asset tree holding every file the editor
// shell needs, minus the ones omit names, and returns its path.
func fakeTinymoon(t *testing.T, dir string, omit ...string) string {
	t.Helper()
	root := filepath.Join(dir, "assets")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("writing the tree: %v", err)
	}
	for _, rel := range TinymoonRequired {
		if contains(omit, rel) {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte("/* stub */\n"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return root
}

func TestTheEditorTierIsPartOfWhatIsRequired(t *testing.T) {
	for _, rel := range TinymoonEditorTier {
		if !contains(TinymoonRequired, rel) {
			t.Errorf("%s is part of the editor tier but is not required", rel)
		}
	}
}

func TestACompleteTreeResolves(t *testing.T) {
	isolate(t)
	root := fakeTinymoon(t, t.TempDir())

	resolved, err := ResolveTinymoon(root)
	if err != nil {
		t.Fatalf("ResolveTinymoon: %v", err)
	}
	if resolved.Source != root {
		t.Errorf("Source = %q, want %q", resolved.Source, root)
	}
	if _, err := fs.Stat(resolved.FS, "js/editor.js"); err != nil {
		t.Errorf("the resolved tree does not carry js/editor.js: %v", err)
	}
}

func TestAnIncompleteTreeIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		omit  []string
		wants []string
	}{
		{
			name:  "without the editor module",
			omit:  []string{"js/editor.js"},
			wants: []string{"missing 1 file(s) the editor loads: js/editor.js.", "The editor tier ("},
		},
		{
			name:  "without the completion module",
			omit:  []string{"js/completion.js"},
			wants: []string{"js/completion.js", "--tinymoon-assets <path-to-tinymoon>/assets."},
		},
		{
			name:  "without the editor stylesheet",
			omit:  []string{"css/editor.css"},
			wants: []string{"css/editor.css", "newer than the released tinymoon package"},
		},
		{
			name: "without two of them at once",
			omit: []string{"js/editor.js", "css/editor.css"},
			// Every missing file is named at once, in the required order.
			wants: []string{"missing 2 file(s) the editor loads: css/editor.css, js/editor.js."},
		},
		{
			name: "without a framework sheet the tier does not name",
			omit: []string{"css/tokens.css"},
			wants: []string{
				"missing 1 file(s) the editor loads: css/tokens.css.",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			isolate(t)
			root := fakeTinymoon(t, t.TempDir(), testCase.omit...)

			_, err := ResolveTinymoon(root)
			if err == nil {
				t.Fatalf("a tree missing %v resolved", testCase.omit)
			}
			var assetsError *Error
			if !errors.As(err, &assetsError) {
				t.Fatalf("error type = %T, want *assets.Error", err)
			}
			if !strings.HasPrefix(err.Error(), "The tinymoon assets at "+root+" are ") {
				t.Errorf("error %q does not name the tree it read", err)
			}
			for _, want := range testCase.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not carry %q", err, want)
				}
			}
		})
	}
}

func TestARefusalOverAFrameworkSheetDoesNotBlameTheEditorTier(t *testing.T) {
	isolate(t)
	root := fakeTinymoon(t, t.TempDir(), "css/tokens.css")

	_, err := ResolveTinymoon(root)
	if err == nil {
		t.Fatal("an incomplete tree resolved")
	}
	if strings.Contains(err.Error(), "The editor tier (") {
		t.Errorf("error %q blames the editor tier for a framework sheet", err)
	}
}

func TestAPathThatIsNotADirectoryIsRefused(t *testing.T) {
	isolate(t)
	nowhere := filepath.Join(t.TempDir(), "nowhere")

	_, err := ResolveTinymoon(nowhere)
	want := "--tinymoon-assets " + nowhere + " is not a directory. Point it at " +
		"a tinymoon checkout's 'assets' directory."
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestATildePathIsExpanded(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	fakeTinymoon(t, home)

	resolved, err := ResolveTinymoon("~/assets")
	if err != nil {
		t.Fatalf("ResolveTinymoon: %v", err)
	}
	if want := filepath.Join(home, "assets"); resolved.Source != want {
		t.Errorf("Source = %q, want %q", resolved.Source, want)
	}
}

func TestTheEmbeddedFrameworkCarriesEverythingTheShellLoads(t *testing.T) {
	// The Python surface refused when no tinymoon was installed; a Go build
	// always has the framework, so what is left to verify is that the version
	// this build compiles in really holds the editor tier.
	isolate(t)

	resolved, err := ResolveTinymoon("")
	if err != nil {
		t.Fatalf("ResolveTinymoon(\"\"): %v", err)
	}
	if !strings.HasPrefix(resolved.Source, "embedded tinymoon ") {
		t.Errorf("Source = %q, want it to name the embedded framework", resolved.Source)
	}
	if !strings.Contains(resolved.Source, "github.com/smm-h/tinymoon") {
		t.Errorf("Source = %q, want it to name the module", resolved.Source)
	}
	for _, rel := range TinymoonRequired {
		if _, err := fs.Stat(resolved.FS, rel); err != nil {
			t.Errorf("the embedded framework does not carry %s: %v", rel, err)
		}
	}
}

// shellReferences is every tinymoon path the shell page and the app module
// name directly, read the way the shell itself addresses them.
func shellReferences(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, name := range []string{"index.html", "app.js"} {
		text := uiFile(t, name)
		for _, chunk := range strings.Split(text, "/tinymoon/")[1:] {
			chunk = strings.Split(chunk, `"`)[0]
			chunk = strings.Split(chunk, "'")[0]
			chunk = strings.Split(chunk, ")")[0]
			found[chunk] = true
		}
	}
	return found
}

// uiFile reads one of the editor's own front-end files.
func uiFile(t *testing.T, name string) string {
	t.Helper()
	content, err := fs.ReadFile(UI(), name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(content)
}

func TestEveryReferencedFileIsRequired(t *testing.T) {
	// Whatever the shell names directly is what the tree is checked for, or a
	// tree could pass the check and 404 on load.
	missing := make([]string, 0)
	for rel := range shellReferences(t) {
		if !contains(TinymoonRequired, rel) {
			missing = append(missing, rel)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf(
			"the shell loads %v but the asset check does not require them: a "+
				"tree without them would pass and then 404", missing,
		)
	}
}

func TestTheRequiredListIsNotPadded(t *testing.T) {
	// Every entry earns its place: a direct reference, or the editor tier.
	references := shellReferences(t)
	unused := make([]string, 0)
	for _, rel := range TinymoonRequired {
		if !references[rel] && !contains(TinymoonEditorTier, rel) {
			unused = append(unused, rel)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf(
			"%v are required but nothing in the shell loads them and they are "+
				"not part of the editor tier", unused,
		)
	}
}

func TestThePackagedUIHoldsTheApp(t *testing.T) {
	for _, name := range []string{"index.html", "app.js", "app.css"} {
		if _, err := fs.Stat(UI(), name); err != nil {
			t.Errorf("the packaged front-end does not carry %s: %v", name, err)
		}
	}
}

func TestTheAppMountsTheTinymoonEditorComponent(t *testing.T) {
	source := uiFile(t, "app.js")
	for _, want := range []string{"createEditor", "setDecorations"} {
		if !strings.Contains(source, want) {
			t.Errorf("the app does not call %s", want)
		}
	}
}

func TestTheShellWiresTheAssistanceLanes(t *testing.T) {
	// The two decoration lanes and the completion reach the server.
	source := uiFile(t, "app.js")
	cases := map[string][]string{
		"the buffer is sent for analysis":       {"/analysis?path="},
		"spelling goes into the underlay":       {"setDecorations(", "tm-deco-spell"},
		"lints go into the gutter":              {"setGutterMarkers(", "tm-editor-marker-error"},
		"the findings have a keyboard control":  {"renderFindings", "setSelection("},
		"completion asks for link targets":      {"/api/link-targets?q=", "onCompletionContext"},
		"the completion trigger is a link tail": {"LINK_TOKEN"},
	}
	for name, wants := range cases {
		for _, want := range wants {
			if !strings.Contains(source, want) {
				t.Errorf("%s: the app does not carry %q", name, want)
			}
		}
	}
}

func TestThePublishSurfaceIsHonest(t *testing.T) {
	source := uiFile(t, "app.js")

	lowered := strings.ToLower(source)
	for _, banned := range []string{"publish this post", "publish post"} {
		if strings.Contains(lowered, banned) {
			t.Errorf("the button claims to publish one post: %q", banned)
		}
	}
	if !strings.Contains(source, "Publish repository") {
		t.Error("the button does not name the repository")
	}
	for _, field := range []string{"scope_note", "consequential", "effect", "grants"} {
		if !strings.Contains(source, "descriptor."+field) {
			t.Errorf("the dialog does not render descriptor.%s", field)
		}
	}
	if !strings.Contains(source, "plan.publishing") {
		t.Error("the dialog does not list what will publish")
	}
	// No don't-ask-again: nothing about consent outlives the dialog.
	for _, banned := range []string{"localStorage", "sessionStorage"} {
		if strings.Contains(source, banned) {
			t.Errorf("consent could be stored in %s", banned)
		}
	}
	if got := strings.Count(source, "approve_consequential"); got != 1 {
		t.Errorf("approve_consequential appears %d times, want 1", got)
	}
}

func TestTheEmbeddedFrontEndIsTheOneTheShellPageDeclares(t *testing.T) {
	page := uiFile(t, "index.html")
	for _, want := range []string{`href="/ui/app.css"`, `src="/ui/app.js"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the shell page does not carry %q", want)
		}
	}
}
