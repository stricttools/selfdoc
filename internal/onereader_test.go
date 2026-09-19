package internal_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/util"
)

// retiredFrontmatterReaders are the readers of a frontmatter block that this
// repository once carried beside the one in package util: the hand-rolled
// key/value parser, the staleness pass's own body stripper, and the
// generated-marker string match in gen. Each answered a narrower question with
// its own idea of what a fence and a key look like, and the three drifted --
// one accepted a fence the others refused.
//
// util.ReadFrontmatter and util.SplitFrontmatter replaced all three. A name
// below reappearing anywhere in the module means a fourth reader is being
// written, which is the thing this test exists to stop.
var retiredFrontmatterReaders = []string{
	"ParseFrontmatter",
	"frontmatterValue",
	"frontmatterKeyOrder",
	"hasGeneratedMarker(",
	"staleness.StripFrontmatter",
}

// TestOneFrontmatterReader asserts that no retired frontmatter reader is back.
//
// It greps the module's own Go sources. That is a blunt instrument, and it is
// the right one: a second reader is introduced by writing a function, and the
// only thing a compiler check could see is a function nobody calls.
func TestOneFrontmatterReader(t *testing.T) {
	root := moduleRoot(t)
	// gen's own unexported marker reader kept its name, and reads through
	// util.ReadFrontmatter; the grep below therefore looks for a bare call
	// rather than the name, and this file names the strings it searches for,
	// so both are excluded from the walk.
	self := filepath.Join(root, "internal", "onereader_test.go")

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || path == self {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, name := range retiredFrontmatterReaders {
			if name == "hasGeneratedMarker(" && strings.HasPrefix(rel, filepath.Join("internal", "gen")) {
				// gen still asks the question; it asks the one reader.
				continue
			}
			if strings.Contains(text, name) {
				t.Errorf("%s carries the retired frontmatter reader %q: "+
					"every caller reads a block through util.ReadFrontmatter "+
					"or util.SplitFrontmatter", rel, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// untrackedContentDirs hold Markdown this repository does not author: build
// output, an installed dependency tree, and the demo site's own copy.
var untrackedContentDirs = map[string]bool{
	"_build":       true,
	"node_modules": true,
	"demo":         true,
	".venv":        true,
	"venv":         true,
}

// TestNoRetiredFrontmatterFenceInTheTree asserts that no page, post or fixture
// still opens with the retired fence.
//
// The reader refuses such a document by name, so one left in the tree is a
// build or a test that fails on the file it was given rather than on anything
// it was testing.
func TestNoRetiredFrontmatterFenceInTheTree(t *testing.T) {
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] || untrackedContentDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(data), "---\n") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		t.Errorf("%s opens with the retired \"---\" frontmatter fence: "+
			"convert it with scripts/convert-frontmatter-to-toml.py", rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// TestFrontmatterDocumentationCarriesTheDerivedKeyTable is the freshness check
// on the one place the key registry is written out for a reader.
//
// docs/frontmatter.md carries the table verbatim; the table is derived from the
// schema the generated validator embeds. A key added, removed, retyped or
// redescribed in the schema therefore fails here until the page is regenerated
// from the renderer, so the page can never quietly describe a registry that no
// longer exists.
func TestFrontmatterDocumentationCarriesTheDerivedKeyTable(t *testing.T) {
	root := moduleRoot(t)
	page := filepath.Join(root, ".stricttools", "docs", "frontmatter.md")
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	table, err := util.RenderFrontmatterKeyTable()
	if err != nil {
		t.Fatalf("rendering the key registry: %v", err)
	}
	if !strings.Contains(string(data), table) {
		t.Errorf("docs/frontmatter.md does not carry the key registry as the schema "+
			"declares it. The table it must carry is:\n\n%s", table)
	}
}
