package editor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/build"
	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

// The preview is the publish, byte for byte.
//
// An authoring app whose preview is a second renderer is an authoring app
// that lies: what you approve on screen is not what readers get. There is one
// renderer here, and this file holds the assertion that says so -- save a
// post, build it through the real posts build, render the same source through
// the editor's preview path, compare the bytes.
//
// The comparison is only worth anything if it can fail, so the divergence
// tests mutate one input at a time and assert the bytes stop matching.

const (
	identityHelloName = "hello.md"
	identityHello     = "+++\ntitle = \"Hello World\"\ndate = 2024-01-15\nslug = \"hello-world\"\n" +
		"tags = [\"release\"]\ndraft = false\ndirectives = false\n+++\n" +
		"# Hello World\n\nThis is the post content.\n\n" +
		"## Setup\n\nFirst.\n\n## Setup\n\nSecond.\n\n" +
		"See [the index](../index.md) and [the other post](second.md).\n"
	identitySecondName = "second.md"
	identitySecond     = "+++\ntitle = \"Second Post\"\ndate = 2024-02-01\nslug = \"second-post\"\n" +
		"tags = []\ndraft = false\ndirectives = false\n+++\nSecond post body.\n"
	identityDraftName = "later.md"
	identityDraft     = "+++\ntitle = \"Later\"\ndate = 2024-05-01\nslug = \"later\"\n" +
		"tags = []\ndraft = true\ndirectives = false\n+++\nNot yet.\n"
)

// runPostsBuild builds the project's posts exactly as a publish builds them.
func runPostsBuild(t *testing.T, dir string, includeDrafts bool) {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	if _, err := build.Build(build.Options{
		DirPath:       dir,
		Config:        cfg,
		Target:        "posts",
		IncludeDrafts: includeDrafts,
		Stdout:        discardWriter{},
	}, effects.Unbound()); err != nil {
		t.Fatalf("the posts-only build: %v", err)
	}
}

// discardWriter swallows a build's progress lines.
type discardWriter struct{}

// Write reports every byte written and keeps none.
func (discardWriter) Write(payload []byte) (int, error) { return len(payload), nil }

// publishedBytes reads the file a posts build wrote for one slug.
func publishedBytes(t *testing.T, project, slug string) string {
	t.Helper()
	path := filepath.Join(project, ".stricttools", "docs-cache", "build", "blog", slug, "index.html")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// publishedProject writes a project with two posts and a draft, builds it the
// way a publish builds it, and returns the project and its registry entry.
func publishedProject(t *testing.T) (string, *State) {
	t.Helper()
	project := makeProject(t, map[string]string{
		identityHelloName:  identityHello,
		identitySecondName: identitySecond,
		identityDraftName:  identityDraft,
	})
	runPostsBuild(t, project, false)
	reg := writeRegistry(t, local("proj", project))
	return project, newState(t, reg, nil)
}

// previewOf renders one buffer through the editor's own preview path.
func previewOf(t *testing.T, state *State, name, source string) string {
	t.Helper()
	entry, err := state.Registry().Get(state.Registry().Names()[0])
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	html, err := RenderPreview(entry, name, source, effects.Unbound())
	if err != nil {
		t.Fatalf("RenderPreview(%s): %v", name, err)
	}
	return html
}

func TestByteIdentity(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	project, state := publishedProject(t)

	t.Run("preview equals the published render", func(t *testing.T) {
		html := previewOf(t, state, identityHelloName, identityHello)
		if html != publishedBytes(t, project, "hello-world") {
			t.Error("the preview is not the published bytes")
		}
	})

	t.Run("it holds for every published post", func(t *testing.T) {
		for name, slug := range map[string]string{
			identityHelloName: "hello-world", identitySecondName: "second-post",
		} {
			source := identityHello
			if name == identitySecondName {
				source = identitySecond
			}
			if previewOf(t, state, name, source) != publishedBytes(t, project, slug) {
				t.Errorf("the preview of %s is not its published bytes", name)
			}
		}
	})

	t.Run("site-level addressing survives the preview", func(t *testing.T) {
		// Posts are site-level citizens; their links resolve site-level. The
		// publish build and the preview share one definition of the
		// site-level build arguments, and the equality above is what proves
		// it -- this narrows the failure message when only addressing drifts.
		html := previewOf(t, state, identityHelloName, identityHello)
		published := publishedBytes(t, project, "hello-world")
		if strings.Join(sortedHrefs(html), "\n") != strings.Join(sortedHrefs(published), "\n") {
			t.Error("the previewed links are not the published ones")
		}
	})
}

func TestTheComparisonCanFail(t *testing.T) {
	// No vacuous pass: mutate one input and the bytes must stop matching.
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)
	project, state := publishedProject(t)

	t.Run("a changed body diverges", func(t *testing.T) {
		edited := strings.Replace(identityHello,
			"This is the post content.", "This is different content.", 1)
		if previewOf(t, state, identityHelloName, edited) ==
			publishedBytes(t, project, "hello-world") {
			t.Error("a changed body did not change the bytes")
		}
	})

	t.Run("a changed title diverges", func(t *testing.T) {
		edited := strings.Replace(identityHello,
			"title = \"Hello World\"", "title = \"Hello Moon\"", 1)
		if previewOf(t, state, identityHelloName, edited) ==
			publishedBytes(t, project, "hello-world") {
			t.Error("a changed title did not change the bytes")
		}
	})

	t.Run("one added character diverges", func(t *testing.T) {
		edited := strings.Replace(identityHello, "Second.\n", "Second..\n", 1)
		if previewOf(t, state, identityHelloName, edited) ==
			publishedBytes(t, project, "hello-world") {
			t.Error("one added character did not change the bytes")
		}
	})

	t.Run("comparing against another post diverges", func(t *testing.T) {
		if previewOf(t, state, identityHelloName, identityHello) ==
			publishedBytes(t, project, "second-post") {
			t.Error("two different posts rendered the same bytes")
		}
	})
}

func TestDrafts(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a draft preview matches a drafts build", func(t *testing.T) {
		// A draft has no publish; the drafts build is the same renderer.
		project := makeProject(t, map[string]string{identityDraftName: identityDraft})
		runPostsBuild(t, project, true)
		state := newState(t, writeRegistry(t, local("drafty", project)), nil)

		html := previewOf(t, state, identityDraftName, identityDraft)
		if html != publishedBytes(t, project, "later") {
			t.Error("the draft preview is not what the drafts build wrote")
		}
	})
}

// sortedHrefs returns every href a page emits, sorted, the way the Python's
// own comparison read them.
func sortedHrefs(html string) []string {
	found := []string{}
	for _, part := range strings.Split(html, `href="`)[1:] {
		found = append(found, strings.Split(part, `"`)[0])
	}
	sort.Strings(found)
	return found
}
