package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
)

// Exit-code parity across the check's two project kinds.
//
// A unified docs-site and a standalone project must reach the same verdict for
// the same project state. They did not: the unified path compared documented
// against total public directly, hardcoding a 100% coverage requirement, while
// the standalone path honored the configured threshold. A project with a
// lowered threshold therefore passed one entry point and failed the other.
// One binary runs both through the same verdict rules now.

const longDescription = "A documentation page describing the library, its public functions and " +
	"the way each one is meant to be used in practice"

// loweredThresholdProjects creates a constituent project at 50% coverage plus
// a docs-site unifying it. Both configs lower the threshold to 0.4, so 50%
// documented coverage is a pass by the configured policy, and nothing else in
// either project emits an error-severity lint.
func loweredThresholdProjects(t *testing.T) (lib, site string) {
	t.Helper()
	root := t.TempDir()
	lib = filepath.Join(root, "lib")
	site = filepath.Join(root, "docs-site")
	testproject.Manifests(t, lib)
	testproject.Manifests(t, site)

	testproject.WriteJSON(t, filepath.Join(lib, "selfdoc.json"), map[string]any{
		"source":             []any{map[string]any{"path": "mylib/", "language": "python"}},
		"docs":               "stricttools/docs/",
		"output":             "stricttools/.docs-cache/build/",
		"base_url":           "https://example.com/lib",
		"author":             testproject.Author(),
		"search_engine":      "pagefind",
		"coverage_threshold": 0.4,
		"versions":           []any{map[string]any{"version": "1.0.0"}},
		"locales":            []any{map[string]any{"code": "en", "label": "English", "default": true}},
	})
	writeText(t, filepath.Join(lib, "mylib", "alpha.py"),
		"\"\"\"Alpha module.\"\"\"\n\n\ndef alpha():\n    \"\"\"Do alpha.\"\"\"\n    return 1\n")
	writeText(t, filepath.Join(lib, "mylib", "beta.py"),
		"\"\"\"Beta module.\"\"\"\n\n\ndef beta():\n    \"\"\"Do beta.\"\"\"\n    return 2\n")
	writeText(t, filepath.Join(lib, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Lib\"\ndescription = \""+longDescription+"\"\n+++\n\n# Lib\n\n"+
			`:-: ref path="mylib/alpha.py"`+"\n")

	testproject.WriteJSON(t, filepath.Join(site, "selfdoc.json"), map[string]any{
		"docs":               "stricttools/docs/",
		"output":             "stricttools/.docs-cache/build/",
		"base_url":           "https://example.com",
		"author":             testproject.Author(),
		"search_engine":      "pagefind",
		"coverage_threshold": 0.4,
		"versions":           []any{map[string]any{"version": "1.0.0"}},
		"locales":            []any{map[string]any{"code": "en", "label": "English", "default": true}},
		"unified":            map[string]any{"projects": []any{map[string]any{"path": "../lib"}}},
	})
	writeText(t, filepath.Join(site, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Docs\"\ndescription = \""+longDescription+"\"\n+++\n\n# Docs\n")

	return lib, site
}

func TestBothProjectKindsHonorTheConfiguredCoverageThreshold(t *testing.T) {
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	lib, site := loweredThresholdProjects(t)

	standalone := run(t, lib, "check", "--no-auto-commit")
	if standalone.ExitCode != 0 {
		t.Fatalf("the standalone check should pass at 50%% documented coverage "+
			"with a configured threshold of 0.4:\n%s\n%s", standalone.Stdout, standalone.Stderr)
	}

	unified := run(t, site, "check", "--no-auto-commit")
	if unified.ExitCode != standalone.ExitCode {
		t.Fatalf("the unified check reached a different verdict than the "+
			"standalone one for the same project state -- the unified path is "+
			"ignoring the configured coverage_threshold:\n%s\n%s",
			unified.Stdout, unified.Stderr)
	}
}

func TestTheUnifiedCheckEmitsThePayloadToo(t *testing.T) {
	// The payload is declared once and supplied unconditionally, so the
	// machine contract is the same document whichever project kind answered.
	requirePagefind(t)
	isolate(t)
	requirePython3(t)
	_, site := loweredThresholdProjects(t)

	result := run(t, site, "check", "--json", "--no-auto-commit")
	payload := payloadOf(t, result)
	for _, key := range []string{"directives", "coverage", "lints", "exit_code"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("the unified payload carries no %q member: %v", key, payload)
		}
	}
}

func TestTheCheckRunsThePostLintsForAnOrdinaryProject(t *testing.T) {
	// The posts-only check path is subsumed: the standalone check already
	// runs the post rules, so a project with a broken post fails here.
	requirePagefind(t)
	isolate(t)
	// The posts block is declared: an undeclared one means "this project has
	// no posts", and a post found at the conventional path anyway is a hard
	// error rather than a diagnostic.
	dir := postProject(t, map[string]any{
		"docs": "stricttools/docs/", "output": "stricttools/.docs-cache/build/",
		"posts": map[string]any{"dir": "stricttools/posts/"},
	})
	writeText(t, filepath.Join(dir, "stricttools", "docs", "index.md"),
		"+++\ntitle = \"Home\"\ndescription = \""+longDescription+"\"\n+++\n\n# Home\n")
	// A post whose date is not YYYY-MM-DD is POST003.
	writeText(t, filepath.Join(dir, "stricttools", "posts", "broken.md"),
		"+++\ntitle = \"Broken\"\ndate = \"15-01-2024\"\ndirectives = false\n+++\n\nBad date.\n")

	result := run(t, dir, "check", "--json", "--no-auto-commit")
	payload := payloadOf(t, result)
	found := false
	for _, raw := range payload["lints"].([]any) {
		if strings.HasPrefix(raw.(map[string]any)["code"].(string), "POST") {
			found = true
		}
	}
	if !found {
		t.Errorf("the check reported no POST diagnostic for a broken post: %v", payload["lints"])
	}
}
