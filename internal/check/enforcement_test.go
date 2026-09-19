package check

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/catalog"
)

// enforcementProject writes a project whose one page carries the given body.
func enforcementProject(t *testing.T, pageBody string) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, pythonProjectConfig())
	write(t, filepath.Join(root, "mylib", "__init__.py"), `"""My library."""`+"\n")
	write(t, filepath.Join(root, ".stricttools", "docs", "index.md"),
		"+++\ntitle = \"API\"\ndescription = \"API reference page describing the public "+
			"surface of the library in careful and complete detail\"\n+++\n\n"+
			"# API\n\n"+pageBody+"\n")
	return root
}

// pageLineReference matches the "index.md:<line>" coordinate an attribute
// refusal names.
var pageLineReference = regexp.MustCompile(`index\.md:\d+`)

func TestDirectiveAttributeEnforcement(t *testing.T) {

	t.Run("an unknown attribute is a hard error", func(t *testing.T) {
		root := enforcementProject(t, `:-: ref path="mylib" bogus="1"`)
		_, err := CheckDocs(root, nil, false, "", "", handle())
		if err == nil {
			t.Fatal("an unknown attribute was accepted")
		}
		var attrError *catalog.DirectiveAttrError
		if !errors.As(err, &attrError) {
			t.Fatalf("error type = %T, want *catalog.DirectiveAttrError: %v", err, err)
		}
		message := err.Error()
		if !pageLineReference.MatchString(message) {
			t.Errorf("message = %q, want it to name the page and line", message)
		}
		for _, fragment := range []string{"ref", "bogus", "path", "lang"} {
			if !strings.Contains(message, fragment) {
				t.Errorf("message %q does not carry %q", message, fragment)
			}
		}
	})

	t.Run("a missing required attribute is a hard error", func(t *testing.T) {
		root := enforcementProject(t, ":-: ref")
		_, err := CheckDocs(root, nil, false, "", "", handle())
		if err == nil {
			t.Fatal("a directive missing its required attribute was accepted")
		}
		message := err.Error()
		if !strings.Contains(message, "missing required") ||
			!strings.Contains(message, "path") {
			t.Errorf("message = %q", message)
		}
	})

	t.Run("an unknown attribute on a block directive is a hard error", func(t *testing.T) {
		root := enforcementProject(t,
			":<: callout-note title=\"x\"\n:=:\n::: hi\n:>:")
		_, err := CheckDocs(root, nil, false, "", "", handle())
		if err == nil {
			t.Fatal("an unknown attribute on a block directive was accepted")
		}
		var attrError *catalog.DirectiveAttrError
		if !errors.As(err, &attrError) {
			t.Fatalf("error type = %T, want *catalog.DirectiveAttrError: %v", err, err)
		}
	})

	t.Run("a declared attribute passes", func(t *testing.T) {
		root := enforcementProject(t, `:-: ref path="mylib" lang="python"`)
		result, err := CheckDocs(root, nil, false, "", "", handle())
		if err != nil {
			t.Fatalf("CheckDocs: %v", err)
		}
		found := false
		for _, directiveResult := range result.DirectiveResults {
			if directiveResult.Outcome == StatusOK {
				found = true
			}
		}
		if !found {
			t.Errorf("no directive resolved: %+v", result.DirectiveResults)
		}
	})
}
