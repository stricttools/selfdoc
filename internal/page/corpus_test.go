package page

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/tokenizer"
	"github.com/stricttools/selfdoc/internal/util"
)

// TestDocsCorpusMatchesReference renders a frozen copy of the repository's own
// documentation templates and asserts every byte against the Python's output.
//
// The fixtures above are written for the behaviours they test; this case is
// the opposite -- real documents, with the frontmatter, the tables, the long
// code blocks, the declared glossary terms, the nested directory and the
// page lengths that real documentation has. It is where a difference the
// fixtures do not reach shows up.
//
// Directives are deliberately left unresolved: both renderers see the same
// marker text, so the case measures the page chrome rather than the directive
// resolver.
func TestDocsCorpusMatchesReference(t *testing.T) {
	resetSelectCounter()
	files, frontmatter := readCorpus(t)
	opts := NewOptions()
	opts.MarkdownFiles = files
	opts.ProjectName = "selfdoc"
	opts.Version = "0.38.1"
	opts.Frontmatter = frontmatter
	opts.Author = testAuthor()
	opts.BaseURL = "https://selfdoc.smmh.dev"
	opts.Repo = "https://github.com/smm-h/selfdoc"
	// The corpus was frozen while this project's pages sat at docs/, and the
	// edit link names the directory it is given; this case measures the page
	// chrome, not the layout.
	opts.DocsDirName = "docs/"
	opts.FeedURL = "feed.xml"
	opts.ThemeMeta = themeMeta(t, "tinymoon")

	rendered, err := GenerateHTML(opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	assertEqual(t, "docs_corpus",
		maskCodeInteriors(renderKeyed(rendered)),
		maskCodeInteriors(reference(t, "docs_corpus")))
}

// readCorpus reads the frozen corpus in the order the recorder walked it: a
// directory's own files, sorted, then its subdirectories.
//
// A template that states neither an H1 nor a frontmatter title is refused by
// the renderer, so one gets an H1 named after its path -- the same fallback
// the recorder applied.
func readCorpus(t *testing.T) ([]SourceFile, map[string]util.Frontmatter) {
	t.Helper()
	root := filepath.Join("testdata", "corpus")
	var files []SourceFile
	frontmatter := map[string]util.Frontmatter{}

	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading corpus directory %s: %v", dir, err)
		}
		var names, subdirs []string
		for _, entry := range entries {
			if entry.IsDir() {
				subdirs = append(subdirs, entry.Name())
				continue
			}
			if strings.HasSuffix(entry.Name(), ".md") &&
				!strings.HasPrefix(entry.Name(), "_") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		sort.Strings(subdirs)
		for _, name := range names {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("reading corpus file %s: %v", name, err)
			}
			rel, err := filepath.Rel(root, filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("relativizing %s: %v", name, err)
			}
			rel = filepath.ToSlash(rel)
			block, err := util.ReadFrontmatter(string(raw), rel, util.KindPage)
			if err != nil {
				t.Fatalf("reading corpus file %s: %v", name, err)
			}
			meta, body := block.Values, block.Body
			hasH1 := false
			for _, tok := range tokenizer.Tokenize(body) {
				if h, ok := tok.(tokenizer.Heading); ok && h.Level == 1 {
					hasH1 = true
					break
				}
			}
			if !hasH1 && fmString(meta, "title") == "" {
				body = "# " + strings.ReplaceAll(rel, ".md", "") + "\n\n" + body
			}
			files = append(files, SourceFile{MdPath: rel, Content: body})
			if len(meta) > 0 {
				frontmatter[rel] = meta
			}
		}
		for _, subdir := range subdirs {
			walk(filepath.Join(dir, subdir))
		}
	}
	walk(root)

	if len(files) == 0 {
		t.Fatal("the frozen corpus is empty")
	}
	return files, frontmatter
}
