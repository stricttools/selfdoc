package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/config"
	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/page"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/testisolation/go/hygiene"
)

func TestPartitionPages(t *testing.T) {
	hygiene.Isolate(t)

	tests := []struct {
		name            string
		docs            map[string]string
		wantVersioned   []string
		wantUnversioned []string
		wantSite        []string
	}{
		{
			name:          "a page declaring nothing is versioned",
			docs:          map[string]string{"guide.md": "# Guide\n\nSome guide.\n"},
			wantVersioned: []string{"index.md", "guide.md"},
		},
		{
			name: "versioned: false puts a page in the unversioned set",
			docs: map[string]string{
				"about.md": "+++\ntitle = \"About\"\nversioned = false\n+++\n\n# About\n\nUnversioned.\n",
			},
			wantVersioned:   []string{"index.md"},
			wantUnversioned: []string{"about.md"},
		},
		{
			name: "versioned: true is the default said out loud",
			docs: map[string]string{
				"guide.md": "+++\ntitle = \"Guide\"\nversioned = true\n+++\n\n# Guide\n\nVersioned.\n",
			},
			wantVersioned: []string{"index.md", "guide.md"},
		},
		{
			name: "a page under the posts prefix is site-level",
			docs: map[string]string{
				"blog/hello.md": "+++\ntitle = \"Hello\"\n+++\n\n# Hello\n\nA post.\n",
			},
			wantVersioned: []string{"index.md"},
			wantSite:      []string{"blog/hello.md"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
			for relPath, content := range test.docs {
				testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", filepath.FromSlash(relPath)), content)
			}
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatalf("loading the fixture config: %v", err)
			}
			partition, err := PartitionPages(cfg, filepath.Join(dir, ".stricttools", "docs"), dir, effects.Unbound())
			if err != nil {
				t.Fatalf("PartitionPages: %v", err)
			}
			assertSetIs(t, "the versioned set", partition.Versioned, test.wantVersioned)
			assertSetIs(t, "the unversioned set", partition.Unversioned, test.wantUnversioned)
			assertSetIs(t, "the site-level set", partition.Site, test.wantSite)
			for _, mdPath := range test.wantUnversioned {
				if _, carried := partition.UnversionedMarkdown[mdPath]; !carried {
					t.Errorf("%s carries no resolved content for the sidebar", mdPath)
				}
			}
		})
	}
}

// assertSetIs checks that a page-path set holds exactly the wanted paths.
func assertSetIs(t *testing.T, what string, got map[string]bool, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s holds %v, want %v", what, sortedKeys(got), want)
		return
	}
	for _, mdPath := range want {
		if !got[mdPath] {
			t.Errorf("%s holds %v, want %v", what, sortedKeys(got), want)
			return
		}
	}
}

func TestBuildUnversionedPages(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("an unversioned page sits at the stable mount", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"about.md": "+++\ntitle = \"About\"\nversioned = false\n+++\n\n# About\n\nUnversioned.\n",
		}})
		if !built.reported("index.html") {
			t.Error("the versioned home page was not reported written")
		}
		if !built.reported("about/index.html") {
			t.Error("the unversioned page was not reported written")
		}
	})

	t.Run("a single-version project gets no archive tree", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"guide.md": "+++\ntitle = \"Guide\"\n+++\n\n# Guide\n\nVersioned guide.\n",
		}})
		if !built.reported("guide/index.html") {
			t.Error("the versioned page was not reported written")
		}
		if built.exists("v") {
			t.Error("the only version there is got an archive copy")
		}
	})

	t.Run("a project whose every page is unversioned still builds", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"index.md": "+++\ntitle = \"Home\"\nversioned = false\n+++\n\n# Home\n\nUnversioned home.\n",
		}})
		if !built.reported("index.html") {
			t.Error("the unversioned home page was not reported written")
		}
		if built.exists("v") {
			t.Error("a project with no versioned page got an archive tree")
		}
	})

	t.Run("no page carries a version or locale segment", func(t *testing.T) {
		built := buildFixture(t, fixture{Docs: map[string]string{
			"guide.md": "+++\ntitle = \"Guide\"\n+++\n\n# Guide\n\nVersioned guide.\n",
		}})
		for path := range built.written {
			if !strings.HasSuffix(path, ".html") {
				continue
			}
			rel, err := filepath.Rel(built.output, path)
			if err != nil {
				t.Fatalf("relativizing %s: %v", path, err)
			}
			for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
				if part == "1.0.0" || part == "en" {
					t.Errorf("%s carries a %q segment", rel, part)
				}
			}
		}
	})
}

func TestBuildSinglePageFilter(t *testing.T) {
	hygiene.Isolate(t)

	dir := testproject.Make(t, map[string]any{"docs": ".stricttools/docs/", "output": ".stricttools/docs-cache/build/"})
	testproject.WriteText(t, filepath.Join(dir, ".stricttools", "docs", "guide.md"),
		"+++\ntitle = \"Guide\"\n+++\n\n# Guide\n\nFiltered guide.\n")
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}

	t.Run("a filter keeps only the pages it names", func(t *testing.T) {
		opts := NewSingleOptions()
		opts.DirPath = dir
		opts.Config = cfg
		opts.PageFilter = map[string]bool{"index.md": true}
		result, err := BuildSingle(opts, effects.Unbound())
		if err != nil {
			t.Fatalf("BuildSingle: %v", err)
		}
		if !hasSourcePath(result.MarkdownFiles, "index.md") {
			t.Error("the named page was filtered out")
		}
		if hasSourcePath(result.MarkdownFiles, "guide.md") {
			t.Error("a page the filter did not name was built")
		}
		for outputKey := range result.HTMLFiles {
			if strings.Contains(outputKey, "guide") {
				t.Errorf("a filtered-out page was emitted at %s", outputKey)
			}
		}
	})

	t.Run("an empty filter builds nothing", func(t *testing.T) {
		opts := NewSingleOptions()
		opts.DirPath = dir
		opts.Config = cfg
		opts.PageFilter = map[string]bool{}
		result, err := BuildSingle(opts, effects.Unbound())
		if err != nil {
			t.Fatalf("BuildSingle: %v", err)
		}
		if len(result.HTMLFiles) != 0 || len(result.MarkdownFiles) != 0 ||
			len(result.Frontmatter) != 0 || len(result.NavItems) != 0 {
			t.Errorf("an empty filter produced %d pages and %d nav items",
				len(result.HTMLFiles), len(result.NavItems))
		}
	})
}

// hasSourcePath reports whether a source list holds a page at mdPath.
func hasSourcePath(sources []page.SourceFile, mdPath string) bool {
	for _, src := range sources {
		if src.MdPath == mdPath {
			return true
		}
	}
	return false
}
