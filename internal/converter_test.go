package internal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/util"
)

// The frontmatter converter is scripts/convert-frontmatter-to-toml.py: the
// one-way rewrite of a retired "---" block into the TOML block the reader
// accepts, run per project by hand. These cases drive the real script over a
// real directory and then read what it wrote through the real reader, so the
// script and the reader cannot drift apart.

// converterScript is the script's path relative to the module root.
const converterScript = "scripts/convert-frontmatter-to-toml.py"

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH: the converter is a Python script")
	}
}

// runConverter runs the converter over dir and returns its combined output and
// its exit status.
func runConverter(t *testing.T, root, dir string, args ...string) (string, int) {
	t.Helper()
	full := append([]string{filepath.Join(root, converterScript), "--path", dir}, args...)
	cmd := exec.Command("python3", full...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	status := 0
	var exitErr *exec.ExitError
	if err != nil {
		if !asExitError(err, &exitErr) {
			t.Fatalf("running the converter: %v\n%s", err, out)
		}
		status = exitErr.ExitCode()
	}
	return string(out), status
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

// writeFixture writes one document into a fresh directory and answers its path.
func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestConverterDryRunWritesNothingAndCountsWhatItWouldWrite covers the dry run
// the discipline requires before any apply: the diff of every block it would
// change, the file count, and an untouched tree.
func TestConverterDryRunWritesNothingAndCountsWhatItWouldWrite(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	before := "---\ntitle: One\nnav_order: 3\n---\n# One\n"
	writeFixture(t, dir, "one.md", before)
	writeFixture(t, dir, "two.md", "---\ntitle: Two\n---\n# Two\n")

	out, status := runConverter(t, root, dir, "--dry-run", "--expect-files", "2")
	if status != 0 {
		t.Fatalf("dry run exited %d:\n%s", status, out)
	}
	for _, want := range []string{"2 file(s) to convert", "-title: One", `+title = "One"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run does not report %q:\n%s", want, out)
		}
	}
	if got := readFile(t, filepath.Join(dir, "one.md")); got != before {
		t.Errorf("the dry run wrote to the tree:\n%s", got)
	}
}

// TestConverterRefusesAWrongFileCount is the assertion the batch discipline
// asks for: a run that would touch a different number of files than the caller
// stated writes nothing.
func TestConverterRefusesAWrongFileCount(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	before := "---\ntitle: One\n---\n# One\n"
	writeFixture(t, dir, "one.md", before)

	out, status := runConverter(t, root, dir, "--apply", "--expect-files", "4")
	if status == 0 {
		t.Fatalf("the converter accepted a wrong file count:\n%s", out)
	}
	if !strings.Contains(out, "Nothing was written") {
		t.Errorf("the refusal does not say the tree was left alone:\n%s", out)
	}
	if got := readFile(t, filepath.Join(dir, "one.md")); got != before {
		t.Errorf("the refused run wrote to the tree:\n%s", got)
	}
}

// TestConverterHandlesTheHardValues covers the three spellings the retired
// dialect made ambiguous and TOML does not: a value carrying a colon, a value
// carrying a double quote, and the bracket list a post's tags were written as.
func TestConverterHandlesTheHardValues(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	writeFixture(t, dir, "post.md", strings.Join([]string{
		"---",
		"title: selfdoc vs the Competition: Generators Compared",
		`description: "She said "hello" twice, at 09:30"`,
		"date: 2026-06-29",
		"tags: [comparison, static-site-generators]",
		"draft: false",
		"directives: false",
		"---",
		"Body.",
	}, "\n"))

	if out, status := runConverter(t, root, dir, "--apply", "--expect-files", "1"); status != 0 {
		t.Fatalf("the converter refused the hard values:\n%s", out)
	}

	block, err := util.ReadFrontmatter(readFile(t, filepath.Join(dir, "post.md")), "post.md", util.KindPost)
	if err != nil {
		t.Fatalf("the reader refused what the converter wrote: %v", err)
	}
	want := map[string]any{
		"title":       "selfdoc vs the Competition: Generators Compared",
		"description": `She said "hello" twice, at 09:30`,
		"date":        "2026-06-29",
		"draft":       false,
		"directives":  false,
	}
	for key, value := range want {
		if block.Values[key] != value {
			t.Errorf("%s = %#v, want %#v", key, block.Values[key], value)
		}
	}
	tags, _ := block.Values["tags"].([]string)
	if len(tags) != 2 || tags[0] != "comparison" || tags[1] != "static-site-generators" {
		t.Errorf("tags = %#v, want the two declared items", block.Values["tags"])
	}
}

// TestConverterRefusalsNameTheFileAndTheLine covers every value the converter
// will not give a TOML spelling on its own authority.
func TestConverterRefusalsNameTheFileAndTheLine(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	for _, testCase := range []struct {
		name    string
		content string
		wantsIn []string
	}{
		{
			name:    "a date that is not a date",
			content: "---\ntitle: T\ndate: Jan 15 2025\n---\nB\n",
			wantsIn: []string{"page.md:3", "date", "YYYY-MM-DD"},
		},
		{
			name:    "a boolean key holding prose",
			content: "---\ntitle: T\ndraft: maybe\n---\nB\n",
			wantsIn: []string{"page.md:3", "draft", "boolean"},
		},
		{
			name:    "an integer key holding prose",
			content: "---\ntitle: T\nnav_order: first\n---\nB\n",
			wantsIn: []string{"page.md:3", "nav_order", "integer"},
		},
		{
			name:    "an undeclared key",
			content: "---\ntitle: T\nbogus: 1\n---\nB\n",
			wantsIn: []string{"page.md:3", "bogus", "not a declared frontmatter key"},
		},
		{
			name:    "the project key the schema refuses",
			content: "---\ntitle: T\nproject: selfdoc\n---\nB\n",
			wantsIn: []string{"page.md:3", "project", "delete the line"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, "page.md", testCase.content)
			out, status := runConverter(t, root, dir, "--apply")
			if status == 0 {
				t.Fatalf("the converter accepted it:\n%s", out)
			}
			for _, want := range testCase.wantsIn {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal does not mention %q:\n%s", want, out)
				}
			}
			if got := readFile(t, filepath.Join(dir, "page.md")); got != testCase.content {
				t.Errorf("a refused run still wrote:\n%s", got)
			}
		})
	}
}

// TestConverterCollapsesTheTwoSortKeys covers the one key that did not simply
// change spelling.
//
// Before the collapse, "order" sorted the pages at the docs root and
// "nav_order" sorted the pages inside a group, so exactly one of the two ever
// governed a page. The converted document keeps the value that governed and
// drops the key that did not.
func TestConverterCollapsesTheTwoSortKeys(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	docsDir := filepath.Join(dir, ".stricttools", "docs")
	writeFixture(t, docsDir, "top.md", "---\ntitle: Top\norder: 40\nnav_order: 3\n---\nB\n")
	writeFixture(t, docsDir, filepath.Join("guides", "inner.md"),
		"---\ntitle: Inner\norder: 40\nnav_order: 3\n---\nB\n")
	writeFixture(t, docsDir, "renamed.md", "---\ntitle: Renamed\norder: 7\n---\nB\n")

	if out, status := runConverter(t, root, docsDir, "--apply", "--expect-files", "3",
		"--project", dir); status != 0 {
		t.Fatalf("the converter refused:\n%s", out)
	}

	for _, testCase := range []struct {
		path string
		want int64
	}{
		{filepath.Join(docsDir, "top.md"), 40},
		{filepath.Join(docsDir, "guides", "inner.md"), 3},
		{filepath.Join(docsDir, "renamed.md"), 7},
	} {
		block, err := util.ReadFrontmatter(readFile(t, testCase.path), testCase.path, util.KindPage)
		if err != nil {
			t.Fatalf("%s: %v", testCase.path, err)
		}
		if block.Values["nav_order"] != testCase.want {
			t.Errorf("%s nav_order = %#v, want %d",
				testCase.path, block.Values["nav_order"], testCase.want)
		}
		if _, declared := block.Values["order"]; declared {
			t.Errorf("%s still declares order", testCase.path)
		}
	}
}

// TestConverterLeavesGeneratedPagesToGen pins the one document class the
// script does not touch: a page selfdoc wrote is rewritten by `selfdoc gen`
// once the emitters write TOML, so converting it here would only produce a
// diff the next gen throws away.
func TestConverterLeavesGeneratedPagesToGen(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	before := "---\ntitle: mylib\ngenerated: true\nseeded: true\n---\n# mylib\n"
	writeFixture(t, dir, "mylib.md", before)

	out, status := runConverter(t, root, dir, "--apply", "--expect-files", "0")
	if status != 0 {
		t.Fatalf("the converter refused a generated page instead of skipping it:\n%s", out)
	}
	if got := readFile(t, filepath.Join(dir, "mylib.md")); got != before {
		t.Errorf("a generated page was converted:\n%s", got)
	}
}

// TestConverterIsIdempotent covers a second run over a converted tree: a
// document already carrying a TOML block is not a document to change.
func TestConverterIsIdempotent(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	writeFixture(t, dir, "one.md", "---\ntitle: One\n---\n# One\n")
	if out, status := runConverter(t, root, dir, "--apply", "--expect-files", "1"); status != 0 {
		t.Fatalf("first run:\n%s", out)
	}
	converted := readFile(t, filepath.Join(dir, "one.md"))
	if out, status := runConverter(t, root, dir, "--apply", "--expect-files", "0"); status != 0 {
		t.Fatalf("second run:\n%s", out)
	}
	if got := readFile(t, filepath.Join(dir, "one.md")); got != converted {
		t.Errorf("the second run rewrote a converted document:\n%s", got)
	}
}

// TestConverterRequiresAMode covers the mode choice: a run that states neither
// --dry-run nor --apply does nothing and says so.
func TestConverterRequiresAMode(t *testing.T) {
	requirePython3(t)
	root := moduleRoot(t)
	dir := t.TempDir()
	writeFixture(t, dir, "one.md", "---\ntitle: One\n---\n# One\n")
	out, status := runConverter(t, root, dir)
	if status == 0 {
		t.Fatalf("the converter ran with no mode:\n%s", out)
	}
	if !strings.Contains(out, "--dry-run") || !strings.Contains(out, "--apply") {
		t.Errorf("the refusal names neither mode:\n%s", out)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
