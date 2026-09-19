package build

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/smm-h/stricttest/go/hygiene"
)

// indexFragment is one indexed page, as Pagefind recorded it.
type indexFragment struct {
	URL     string              `json:"url"`
	Content string              `json:"content"`
	Filters map[string][]string `json:"filters"`
	Meta    map[string]string   `json:"meta"`
}

// indexFragments reads every fragment the indexer wrote under an output root.
//
// Each fragment file is a short binary prefix followed by the fragment's JSON
// object, gzip-compressed.
func indexFragments(t *testing.T, outputDir string) []indexFragment {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(outputDir, "pagefind", "fragment", "*.pf_fragment"))
	if err != nil {
		t.Fatalf("listing the index fragments: %v", err)
	}
	sort.Strings(paths)
	fragments := make([]indexFragment, 0, len(paths))
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			t.Fatalf("opening %s: %v", path, openErr)
		}
		reader, gzipErr := gzip.NewReader(file)
		if gzipErr != nil {
			file.Close()
			t.Fatalf("%s is not gzip: %v", path, gzipErr)
		}
		raw, readErr := io.ReadAll(reader)
		file.Close()
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		start := strings.Index(string(raw), "{")
		if start < 0 {
			t.Fatalf("%s carries no JSON object", path)
		}
		var fragment indexFragment
		if err := json.Unmarshal(raw[start:], &fragment); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

func TestPagefindIndexesTheBuild(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	built := buildFixture(t, fixture{
		Config: map[string]any{"deploy": map[string]any{"provider": "github-pages"}},
		Docs: map[string]string{
			"index.md": "+++\ntitle = \"Home\"\ntags = [\"intro\", \"overview\"]\n+++\n\n" +
				"# Home\n\nWelcome to the documentation site.\n",
			"guides/deploying.md": "+++\ntitle = \"Deploying\"\ntype = \"guide\"\ntags = [\"deploy\"]\n+++\n\n" +
				"# Deploying\n\nPublish the built site to a static host.\n",
		},
	})
	fragments := indexFragments(t, built.output)

	t.Run("the build indexed its own output", func(t *testing.T) {
		if !built.exists(filepath.Join("pagefind", "pagefind-entry.json")) {
			t.Error("the index entry file was not written")
		}
	})

	t.Run("the query API and the widget bundle are the build's own", func(t *testing.T) {
		// The pages reference these; nothing fetches them from a CDN.
		for _, asset := range []string{"pagefind-ui.js", "pagefind-ui.css"} {
			if !built.exists(filepath.Join("pagefind", asset)) {
				t.Errorf("pagefind/%s was not written", asset)
			}
		}
		for _, absent := range []string{"search-index.json", "search.js"} {
			if built.exists(absent) {
				t.Errorf("%s was written: the build carries no index of its own", absent)
			}
		}
	})

	t.Run("every page is indexed, body and all", func(t *testing.T) {
		if len(fragments) < 2 {
			t.Fatalf("the index holds %d pages, want at least the two authored ones", len(fragments))
		}
		var content strings.Builder
		for _, fragment := range fragments {
			content.WriteString(fragment.Content)
			content.WriteString(" ")
		}
		assertCarries(t, "the indexed content", content.String(), "Publish the built site")
	})

	t.Run("the single-valued facets reach the index", func(t *testing.T) {
		guide := fragmentFor(t, fragments, "deploying")
		wants := map[string]string{
			"version": "1.0.0",
			"type":    "guide",
			"group":   "Guides",
			"target":  "github-pages",
		}
		for facet, want := range wants {
			values := guide.Filters[facet]
			if len(values) != 1 || values[0] != want {
				t.Errorf("the %s facet is %v, want [%s]", facet, values, want)
			}
		}
		if len(guide.Filters["project"]) == 0 {
			t.Error("the project facet is empty")
		}
	})

	t.Run("tags index as several values", func(t *testing.T) {
		home := fragmentFor(t, fragments, "")
		tags := append([]string(nil), home.Filters["tags"]...)
		sort.Strings(tags)
		if strings.Join(tags, ",") != "intro,overview" {
			t.Errorf("the home page's tags index as %v, want both declared tags", tags)
		}
	})

	t.Run("the result metadata reaches the index", func(t *testing.T) {
		guide := fragmentFor(t, fragments, "deploying")
		if guide.Meta["type"] != "guide" {
			t.Errorf("the result metadata records type %q, want guide", guide.Meta["type"])
		}
		if guide.Meta["project"] == "" {
			t.Error("the result metadata records no project")
		}
	})

	t.Run("a facet value is not indexed as body text", func(t *testing.T) {
		// The facet elements hold no text, so they add none to the page.
		guide := fragmentFor(t, fragments, "deploying")
		if strings.Contains(guide.Content, "github-pages") {
			t.Error("a facet value was indexed as body text")
		}
	})
}

// fragmentFor is the indexed page whose URL carries the given fragment of
// path; the empty string names the home page.
func fragmentFor(t *testing.T, fragments []indexFragment, inURL string) indexFragment {
	t.Helper()
	for _, fragment := range fragments {
		if inURL == "" {
			if strings.Trim(fragment.URL, "/") == "" {
				return fragment
			}
			continue
		}
		if strings.Contains(fragment.URL, inURL) {
			return fragment
		}
	}
	t.Fatalf("no indexed page names %q; the index holds %d pages", inURL, len(fragments))
	return indexFragment{}
}

func TestPruneUnreferencedPagefindWidget(t *testing.T) {
	hygiene.Isolate(t)
	testproject.RequirePagefind(t)

	t.Run("a tree whose pages load the widget keeps it", func(t *testing.T) {
		built := buildFixture(t, fixture{})
		for _, asset := range []string{"pagefind-ui.js", "pagefind-ui.css"} {
			if !built.exists(filepath.Join("pagefind", asset)) {
				t.Errorf("pagefind/%s was pruned from a tree that loads it", asset)
			}
		}
	})

	t.Run("a framework theme's tree does not", func(t *testing.T) {
		// The framework draws its own search surface over Pagefind's query
		// API, so the shipped widget is a payload no page references.
		built := buildFixture(t, fixture{Config: map[string]any{"theme": "tinymoon"}})
		for _, asset := range []string{"pagefind-ui.js", "pagefind-ui.css"} {
			if built.exists(filepath.Join("pagefind", asset)) {
				t.Errorf("pagefind/%s was kept in a tree no page loads it from", asset)
			}
		}
		// The index itself and the query API are never touched.
		if !built.exists(filepath.Join("pagefind", "pagefind-entry.json")) {
			t.Error("the index entry file was pruned")
		}
		if !built.exists(filepath.Join("pagefind", "pagefind.js")) {
			t.Error("the query API was pruned")
		}
	})
}
