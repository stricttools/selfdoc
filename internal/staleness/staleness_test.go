// Tests for the staleness store: the four hashes, the three checks, the
// baseline hold, the skeleton exemption, the two-writer merge, and the
// document's recorded bytes.
//
// Every hash and every message here is the Python implementation's own: the
// store bytes in testdata were written by scripts/record_staleness_manifest_bytes.py,
// and the message fragments are the strings selfdoc_core/staleness.py emits.
package staleness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/selfdoc/internal/extractors"
	pythonextractor "github.com/stricttools/selfdoc/internal/extractors/python"
	"github.com/stricttools/selfdoc/internal/testproject"
	"github.com/stricttools/selfdoc/internal/util"
	"github.com/smm-h/stricttest/go/hygiene"
)

// requirePython3 skips a test that reads a Python source tree when there is
// no interpreter to read it with.
func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH: the Python extractor reads every tree through it")
	}
}

// docs builds the page map from (relPath, description, body) triples, with a
// nil description standing for a page whose frontmatter declares none.
func docs(pages ...[3]string) map[string]Doc {
	built := map[string]Doc{}
	for _, page := range pages {
		frontmatter := util.Frontmatter{}
		if page[1] != "" {
			frontmatter["description"] = page[1]
		}
		built[page[0]] = Doc{Frontmatter: frontmatter, Raw: page[2]}
	}
	return built
}

// readStore reads the store file as a generic document, for the tests that
// assert on what reached the disk rather than on what the loader returns.
func readStore(t *testing.T, baseDir string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(HashesPath(baseDir))
	if err != nil {
		t.Fatalf("reading the store: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("parsing the store: %v", err)
	}
	return document
}

// -- ComputeContentHash -----------------------------------------------------

func TestContentHashIsConsistent(t *testing.T) {
	content := "# Hello\n\nSome content here."
	first := ComputeContentHash(content)
	if first != ComputeContentHash(content) {
		t.Error("the same content hashed two different ways")
	}
	if len(first) != 64 {
		t.Errorf("digest is %d characters, want a 64-character SHA-256 hex digest", len(first))
	}
}

func TestContentHashDiffersForDifferentContent(t *testing.T) {
	if ComputeContentHash("# Hello") == ComputeContentHash("# Goodbye") {
		t.Error("two different bodies hashed the same")
	}
}

// TestContentHashTakesTheBodyTheReaderHandsOut covers the seam this pass used
// to straddle: it once carried a frontmatter stripper of its own, and now
// hashes what the one frontmatter reader returned behind the block. A page and
// its body must therefore hash the same.
func TestContentHashTakesTheBodyTheReaderHandsOut(t *testing.T) {
	page := "+++\ntitle = \"Test\"\ndescription = \"A page\"\n+++\n# Hello\n\nBody."
	body, err := util.StripFrontmatter(page, "p.md")
	if err != nil {
		t.Fatalf("StripFrontmatter: %v", err)
	}
	if ComputeContentHash(body) != ComputeContentHash("# Hello\n\nBody.") {
		t.Error("the frontmatter reached the content hash")
	}
}

func TestContentHashWithNoFrontmatter(t *testing.T) {
	if got := ComputeContentHash("# Just content\n\nNo frontmatter."); len(got) != 64 {
		t.Errorf("digest is %d characters, want 64", len(got))
	}
}

// TestContentHashCanonicalization covers what a directive marker line
// contributes to the hash: its attribute VALUES do not, everything else does.
func TestContentHashCanonicalization(t *testing.T) {
	tests := []struct {
		name   string
		before string
		after  string
		// same is true when the two bodies must hash identically.
		same bool
	}{
		{
			name:   "an inline marker's attribute rename is canonicalized",
			before: "# Page\n\nSome prose.\n\n:-: ref path=\"mylib.old\" lang=\"python\"\n",
			after:  "# Page\n\nSome prose.\n\n:-: ref path=\"mylib.new\" lang=\"python\"\n",
			same:   true,
		},
		{
			name:   "a block attribute line's rename is canonicalized",
			before: "# Page\n\n:<: table\n:@: path=\"data/old.json\"\n:>:\n",
			after:  "# Page\n\n:<: table\n:@: path=\"data/new.json\"\n:>:\n",
			same:   true,
		},
		{
			name:   "a prose change beside a directive line is still a change",
			before: "# Page\n\nOriginal prose.\n\n:-: ref path=\"mylib.mod\"\n",
			after:  "# Page\n\nRewritten prose.\n\n:-: ref path=\"mylib.mod\"\n",
		},
		{
			name:   "a directive NAME change is still a change",
			before: "# Page\n\n:-: ref path=\"mylib.mod\"\n",
			after:  "# Page\n\n:-: include path=\"mylib.mod\"\n",
		},
		{
			name:   "a key=\"value\" pair in prose is not canonicalized",
			before: "# Page\n\nSet config foo=\"bar\" in your file.\n",
			after:  "# Page\n\nSet config foo=\"baz\" in your file.\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			equal := ComputeContentHash(test.before) == ComputeContentHash(test.after)
			if equal != test.same {
				t.Errorf("hashes equal = %v, want %v", equal, test.same)
			}
		})
	}
}

// -- ComputeDescriptionHash -------------------------------------------------

func TestDescriptionHashIsConsistent(t *testing.T) {
	description := "A short description of the page"
	first := ComputeDescriptionHash(description)
	if first != ComputeDescriptionHash(description) {
		t.Error("the same description hashed two different ways")
	}
	if len(first) != 64 {
		t.Errorf("digest is %d characters, want 64", len(first))
	}
}

func TestDescriptionHashDiffersForDifferentDescriptions(t *testing.T) {
	if ComputeDescriptionHash("Description A") == ComputeDescriptionHash("Description B") {
		t.Error("two different descriptions hashed the same")
	}
}

// -- LoadHashes -------------------------------------------------------------

func TestLoadHashesWithNoFile(t *testing.T) {
	store, err := LoadHashes(t.TempDir())
	if err != nil {
		t.Fatalf("loading an absent store: %v", err)
	}
	if len(store) != 0 {
		t.Errorf("loaded %v from a project with no store", store)
	}
}

func TestLoadHashesReadsTheCurrentVersion(t *testing.T) {
	base := testproject.Dir(t)
	writeStoreFile(t, base, `{
  "_hash_version": 3,
  "page.md": {
    "content": "hash_of_raw_body",
    "description": "desc_hash",
    "seed_hash": "seed_digest"
  }
}`)
	store, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the store: %v", err)
	}
	want := Entry{Content: "hash_of_raw_body", Description: "desc_hash", SeedHash: "seed_digest"}
	if store["page.md"] != want {
		t.Errorf("loaded %+v, want %+v", store["page.md"], want)
	}
}

// TestLoadHashesDiscardsOlderVersions covers the two migrations: nothing a v1
// or v2 file holds is reusable under v3's rules, so an older store is
// discarded wholesale and the project re-baselines.
func TestLoadHashesDiscardsOlderVersions(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{
			name: "a pre-versioning store",
			document: `{"page.md": {"content": "old_hash_of_resolved_content",
			            "description": "desc_hash"}}`,
		},
		{
			name: "a v2 store",
			document: `{"_hash_version": 2, "page.md": {"content": "hash_of_raw_body",
			            "description": "desc_hash"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := testproject.Dir(t)
			writeStoreFile(t, base, test.document)
			store, err := LoadHashes(base)
			if err != nil {
				t.Fatalf("loading the store: %v", err)
			}
			if len(store) != 0 {
				t.Errorf("loaded %v, want an empty store", store)
			}
		})
	}
}

// writeStoreFile writes a store document by hand, for the load tests.
func writeStoreFile(t *testing.T, baseDir, document string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(HashesPath(baseDir)), 0o755); err != nil {
		t.Fatalf("creating the store directory: %v", err)
	}
	if err := os.WriteFile(HashesPath(baseDir), []byte(document), 0o644); err != nil {
		t.Fatalf("writing the store: %v", err)
	}
}

// -- SaveHashes -------------------------------------------------------------

func TestSaveHashesCreatesTheDirectory(t *testing.T) {
	base := testproject.Dir(t)
	store := Store{"page.md": {Content: "aaa", Description: "bbb"}}
	if err := SaveHashes(store, base, effects.Unbound()); err != nil {
		t.Fatalf("saving the store: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(HashesPath(base)))
	if err != nil {
		t.Fatalf("reading the store directory: %v", err)
	}
	// One file and no temporary residue: the write is atomic.
	if len(entries) != 1 || entries[0].Name() != "hashes.json" {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the store directory holds %v, want just hashes.json", names)
	}
	loaded, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the store back: %v", err)
	}
	if loaded["page.md"] != store["page.md"] {
		t.Errorf("round-tripped %+v, want %+v", loaded["page.md"], store["page.md"])
	}
}

func TestSaveHashesOverwritesTheWholeStore(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	if err := SaveHashes(Store{"a.md": {Content: "1", Description: "2"}}, base, handle); err != nil {
		t.Fatalf("saving the first store: %v", err)
	}
	if err := SaveHashes(Store{"b.md": {Content: "3", Description: "4"}}, base, handle); err != nil {
		t.Fatalf("saving the second store: %v", err)
	}
	store, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the store: %v", err)
	}
	if _, ok := store["a.md"]; ok {
		t.Error("the first store's page survived the second save")
	}
	if want := (Entry{Content: "3", Description: "4"}); store["b.md"] != want {
		t.Errorf("loaded %+v, want %+v", store["b.md"], want)
	}
}

func TestSaveHashesStampsTheVersion(t *testing.T) {
	base := testproject.Dir(t)
	if err := SaveHashes(Store{"page.md": {Content: "aaa", Description: "bbb"}}, base, effects.Unbound()); err != nil {
		t.Fatalf("saving the store: %v", err)
	}
	if version := readStore(t, base)["_hash_version"]; version != float64(HashVersion) {
		t.Errorf("the store records version %v, want %d", version, HashVersion)
	}
}

// TestTheStoresBytesAreThePythonsBytes is the parity check that keeps every
// existing repository's store from being rewritten by the port: the key sort
// order, the two-space indent, the omission of unrecorded fields and the
// ensure_ascii escaping are all the Python's.
func TestTheStoresBytesAreThePythonsBytes(t *testing.T) {
	base := testproject.Dir(t)
	store := Store{
		"en/index.md": {
			Content:     strings.Repeat("a", 64),
			Description: strings.Repeat("b", 64),
		},
		"en/cli-build.md": {
			Content:     strings.Repeat("c", 64),
			Description: strings.Repeat("d", 64),
			SchemaHash:  strings.Repeat("e", 64),
			SeedHash:    strings.Repeat("f", 64),
		},
		"en/référence.md": {
			Content:         strings.Repeat("1", 64),
			Description:     strings.Repeat("2", 64),
			SourceDocstring: strings.Repeat("3", 64),
		},
	}
	if err := SaveHashes(store, base, effects.Unbound()); err != nil {
		t.Fatalf("saving the store: %v", err)
	}
	got, err := os.ReadFile(HashesPath(base))
	if err != nil {
		t.Fatalf("reading the store: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "python_hashes.json"))
	if err != nil {
		t.Fatalf("reading the recorded bytes: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("wrote\n%s\nwant\n%s", got, want)
	}
}

// -- CheckStaleness ---------------------------------------------------------

func TestCheckStaleness(t *testing.T) {
	tests := []struct {
		name            string
		stored          Store
		contentHash     string
		descriptionHash string
		wantMessage     bool
	}{
		{
			name:            "a new page is never stale",
			stored:          Store{},
			contentHash:     "c_hash",
			descriptionHash: "d_hash",
		},
		{
			name:            "unchanged content is never stale",
			stored:          Store{"page.md": {Content: "same", Description: "desc"}},
			contentHash:     "same",
			descriptionHash: "desc",
		},
		{
			name:            "a description rewritten with the content is not stale",
			stored:          Store{"page.md": {Content: "old_c", Description: "old_d"}},
			contentHash:     "new_c",
			descriptionHash: "new_d",
		},
		{
			name:            "content changed and the description did not",
			stored:          Store{"page.md": {Content: "old_c", Description: "same_d"}},
			contentHash:     "new_c",
			descriptionHash: "same_d",
			wantMessage:     true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := CheckStaleness("page.md", test.contentHash, test.descriptionHash, test.stored)
			if (message != "") != test.wantMessage {
				t.Fatalf("message = %q, want a message: %v", message, test.wantMessage)
			}
			if !test.wantMessage {
				return
			}
			if !strings.Contains(message, "page.md") || !strings.Contains(message, "stale description") {
				t.Errorf("message %q names neither the page nor the diagnosis", message)
			}
		})
	}
}

// -- CheckDrift -------------------------------------------------------------

func TestCheckDrift(t *testing.T) {
	tests := []struct {
		name            string
		stored          Store
		docstringHash   string
		descriptionHash string
		wantMessage     bool
	}{
		{
			name:            "no source docstrings, nothing to measure",
			stored:          Store{},
			docstringHash:   "",
			descriptionHash: "d_hash",
		},
		{
			name:            "a new page has no baseline to drift from",
			stored:          Store{},
			docstringHash:   "sd_hash",
			descriptionHash: "d_hash",
		},
		{
			name:            "a stored entry with no docstring hash yet",
			stored:          Store{"page.md": {Content: "c", Description: "d"}},
			docstringHash:   "sd_hash",
			descriptionHash: "d",
		},
		{
			name:            "unchanged sources",
			stored:          Store{"page.md": {Content: "c", Description: "d", SourceDocstring: "same"}},
			docstringHash:   "same",
			descriptionHash: "d",
		},
		{
			name:            "sources changed and so did the description",
			stored:          Store{"page.md": {Content: "c", Description: "old_d", SourceDocstring: "old_sd"}},
			docstringHash:   "new_sd",
			descriptionHash: "new_d",
		},
		{
			name:            "sources changed and the description did not",
			stored:          Store{"page.md": {Content: "c", Description: "same_d", SourceDocstring: "old_sd"}},
			docstringHash:   "new_sd",
			descriptionHash: "same_d",
			wantMessage:     true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := CheckDrift("page.md", test.docstringHash, test.descriptionHash, test.stored)
			if (message != "") != test.wantMessage {
				t.Fatalf("message = %q, want a message: %v", message, test.wantMessage)
			}
			if !test.wantMessage {
				return
			}
			for _, fragment := range []string{
				"page.md",
				"possible documentation drift",
				"review the page against the changed docstrings",
				"edit the page's frontmatter description, which clears this on its own",
				"`selfdoc baseline accept page.md`",
				"new baseline",
			} {
				if !strings.Contains(message, fragment) {
					t.Errorf("message %q does not carry %q", message, fragment)
				}
			}
		})
	}
}

// -- CheckSchemaDrift -------------------------------------------------------

func TestCheckSchemaDrift(t *testing.T) {
	tests := []struct {
		name            string
		stored          Store
		schemaHash      string
		descriptionHash string
		wantMessage     bool
	}{
		{
			name:            "no schema hash, nothing to measure",
			stored:          Store{},
			schemaHash:      "",
			descriptionHash: "d_hash",
		},
		{
			name:            "a new page has no baseline to drift from",
			stored:          Store{},
			schemaHash:      "s_hash",
			descriptionHash: "d_hash",
		},
		{
			name:            "a stored entry with no schema hash yet",
			stored:          Store{"cli-run.md": {Content: "c", Description: "d"}},
			schemaHash:      "s_hash",
			descriptionHash: "d",
		},
		{
			name:            "an unchanged schema",
			stored:          Store{"cli-run.md": {Content: "c", Description: "d", SchemaHash: "same"}},
			schemaHash:      "same",
			descriptionHash: "d",
		},
		{
			name:            "the schema changed and so did the description",
			stored:          Store{"cli-run.md": {Content: "c", Description: "old_d", SchemaHash: "old_s"}},
			schemaHash:      "new_s",
			descriptionHash: "new_d",
		},
		{
			name:            "the schema changed and the description did not",
			stored:          Store{"cli-run.md": {Content: "c", Description: "same_d", SchemaHash: "old_s"}},
			schemaHash:      "new_s",
			descriptionHash: "same_d",
			wantMessage:     true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := CheckSchemaDrift("cli-run.md", test.schemaHash, test.descriptionHash, test.stored)
			if (message != "") != test.wantMessage {
				t.Fatalf("message = %q, want a message: %v", message, test.wantMessage)
			}
			if !test.wantMessage {
				return
			}
			for _, fragment := range []string{
				"cli-run.md",
				"CLI schema changed",
				"review the page against the changed CLI schema",
				"edit the page's frontmatter description, which clears this on its own",
				"`selfdoc baseline accept cli-run.md`",
			} {
				if !strings.Contains(message, fragment) {
					t.Errorf("message %q does not carry %q", message, fragment)
				}
			}
		})
	}
}

// -- ComputeSchemaHash ------------------------------------------------------

func TestComputeSchemaHash(t *testing.T) {
	slice := map[string]any{"name": "build", "help": "Build the project", "flags": []any{}}
	first, err := ComputeSchemaHash(slice)
	if err != nil {
		t.Fatalf("hashing a schema slice: %v", err)
	}
	again, err := ComputeSchemaHash(slice)
	if err != nil {
		t.Fatalf("hashing a schema slice again: %v", err)
	}
	if first != again {
		t.Error("the same schema slice hashed two different ways")
	}
	if len(first) != 64 {
		t.Errorf("digest is %d characters, want 64", len(first))
	}

	changedHelp, err := ComputeSchemaHash(map[string]any{
		"name": "build", "help": "Build the project with options", "flags": []any{},
	})
	if err != nil {
		t.Fatalf("hashing the changed help: %v", err)
	}
	if changedHelp == first {
		t.Error("a changed help text did not change the hash")
	}

	addedFlag, err := ComputeSchemaHash(map[string]any{
		"name": "build", "help": "Build the project",
		"flags": []any{map[string]any{"name": "verbose"}},
	})
	if err != nil {
		t.Fatalf("hashing the added flag: %v", err)
	}
	if addedFlag == first {
		t.Error("an added flag did not change the hash")
	}
}

// -- ExtractModuleDocstring and ComputeSourceDocstringHash ------------------

func TestExtractModuleDocstring(t *testing.T) {
	requirePython3(t)
	hygiene.Isolate(t)
	base := testproject.Dir(t)
	extractor := pythonextractor.New()
	tests := []struct {
		name   string
		file   string
		source string
		want   string
	}{
		{
			name:   "a module docstring",
			file:   "mod.py",
			source: "\"\"\"This is the module docstring.\"\"\"\n\ndef foo(): pass\n",
			want:   "This is the module docstring.",
		},
		{
			name:   "no module docstring",
			file:   "bare.py",
			source: "def foo(): pass\n",
		},
		{
			name:   "a syntax error",
			file:   "bad.py",
			source: "def broken(\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(base, test.file)
			if err := os.WriteFile(path, []byte(test.source), 0o644); err != nil {
				t.Fatalf("writing the source: %v", err)
			}
			got, err := ExtractModuleDocstring(path, extractor)
			if err != nil {
				t.Fatalf("extracting the docstring: %v", err)
			}
			if got != test.want {
				t.Errorf("extracted %q, want %q", got, test.want)
			}
		})
	}

	t.Run("a nonexistent file", func(t *testing.T) {
		got, err := ExtractModuleDocstring(filepath.Join(base, "nonexistent.py"), extractor)
		if err != nil {
			t.Fatalf("extracting from an absent file: %v", err)
		}
		if got != "" {
			t.Errorf("extracted %q from an absent file", got)
		}
	})

	t.Run("an extractor that reads no documentation", func(t *testing.T) {
		got, err := ExtractModuleDocstring("whatever.go", extractors.NewStub("go"))
		if err != nil {
			t.Fatalf("extracting through the base: %v", err)
		}
		if got != "" {
			t.Errorf("extracted %q through an extractor that reads nothing", got)
		}
	})
}

func TestComputeSourceDocstringHash(t *testing.T) {
	requirePython3(t)
	hygiene.Isolate(t)
	base := testproject.Dir(t)
	extractor := pythonextractor.New()
	write := func(name, source string) string {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("writing the source: %v", err)
		}
		return path
	}
	first := write("a.py", "\"\"\"Module A.\"\"\"\n")
	second := write("b.py", "\"\"\"Module B.\"\"\"\n")
	undocumented := write("plain.py", "x = 1\n")

	documented, err := ComputeSourceDocstringHash([]SourceFile{
		{Path: first, Extractor: extractor}, {Path: second, Extractor: extractor},
	})
	if err != nil {
		t.Fatalf("hashing two documented modules: %v", err)
	}
	if len(documented) != 64 {
		t.Errorf("digest is %d characters, want 64", len(documented))
	}

	reversed, err := ComputeSourceDocstringHash([]SourceFile{
		{Path: second, Extractor: extractor}, {Path: first, Extractor: extractor},
	})
	if err != nil {
		t.Fatalf("hashing them in the other order: %v", err)
	}
	if reversed != documented {
		t.Error("the input order changed the hash; the files are sorted by path")
	}

	none, err := ComputeSourceDocstringHash([]SourceFile{{Path: undocumented, Extractor: extractor}})
	if err != nil {
		t.Fatalf("hashing an undocumented module: %v", err)
	}
	if none != "" {
		t.Errorf("hashed %q for a file carrying no documentation, want no hash", none)
	}

	unread, err := ComputeSourceDocstringHash([]SourceFile{
		{Path: filepath.Join(base, "main.go"), Extractor: extractors.NewStub("go")},
	})
	if err != nil {
		t.Fatalf("hashing through an extractor that reads nothing: %v", err)
	}
	if unread != "" {
		t.Errorf("hashed %q through an extractor that reads nothing", unread)
	}
}

// -- UpdateHashes -----------------------------------------------------------

func TestUpdateHashesWritesTheStore(t *testing.T) {
	base := testproject.Dir(t)
	pages := docs([3]string{"page.md", "A page about things", "# Page\n\nSome content."})
	if _, _, err := UpdateHashes(pages, base, false, nil, nil, nil, effects.Unbound()); err != nil {
		t.Fatalf("updating the hashes: %v", err)
	}
	entry, ok := readStore(t, base)["page.md"].(map[string]any)
	if !ok {
		t.Fatal("the store carries no entry for page.md")
	}
	for _, field := range []string{"content", "description"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("the entry carries no %q", field)
		}
	}
}

func TestUpdateHashesUnderDryRunWritesNothing(t *testing.T) {
	base := testproject.Dir(t)
	pages := docs([3]string{"page.md", "A page about things", "# Page\n\nSome content."})
	if _, _, err := UpdateHashes(pages, base, true, nil, nil, nil, effects.Unbound()); err != nil {
		t.Fatalf("updating the hashes: %v", err)
	}
	if _, err := os.Stat(HashesPath(base)); !os.IsNotExist(err) {
		t.Errorf("a dry run left a store behind (stat error %v)", err)
	}
}

func TestUpdateHashesDetectsStaleness(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	first := docs([3]string{"page.md", "Original description", "# Page\n\nOriginal content."})
	stale, _, err := UpdateHashes(first, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("a new page reported %v", stale)
	}

	second := docs([3]string{"page.md", "Original description", "# Page\n\nCompletely new content."})
	stale, _, err = UpdateHashes(second, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("reported %v, want one stale page", stale)
	}
	if stale[0].Page != "page.md" || !strings.Contains(stale[0].Message, "stale description") {
		t.Errorf("reported %+v", stale[0])
	}
}

func TestUpdateHashesIsQuietWhenBothChange(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	if _, _, err := UpdateHashes(
		docs([3]string{"page.md", "Desc v1", "# Page\n\nContent v1."}),
		base, false, nil, nil, nil, handle,
	); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	stale, _, err := UpdateHashes(
		docs([3]string{"page.md", "Desc v2", "# Page\n\nContent v2."}),
		base, false, nil, nil, nil, handle,
	)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("reported %v when both the content and the description changed", stale)
	}
}

// TestUpdateHashesExemptsSkeletonPages covers the deadlock the exemption
// exists to prevent: a generated, machine-seeded page cannot be hand-fixed,
// so its baseline must advance rather than be held in a perpetual error.
func TestUpdateHashesExemptsSkeletonPages(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	first := docs([3]string{"page.md", "Seeded description", "# Page\n\nOriginal content."})
	if _, _, err := UpdateHashes(first, base, false, nil, nil, nil, handle); err != nil {
		t.Fatalf("the first pass: %v", err)
	}

	second := docs([3]string{"page.md", "Seeded description", "# Page\n\nDifferent content."})
	stale, _, err := UpdateHashes(second, base, false, nil, nil, map[string]bool{"page.md": true}, handle)
	if err != nil {
		t.Fatalf("the exempt pass: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("an exempt page reported %v", stale)
	}

	// The baseline advanced rather than being merely silenced: a third pass
	// over the same content is clean even without the exemption.
	stale, _, err = UpdateHashes(second, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the third pass: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("the exempt page's baseline was held: %v", stale)
	}
}

func TestUpdateHashesStillReportsANonSkeletonPage(t *testing.T) {
	// The control for the exemption above: the same mismatch on a page that
	// is not exempt still reports.
	base := testproject.Dir(t)
	handle := effects.Unbound()
	if _, _, err := UpdateHashes(
		docs([3]string{"page.md", "Hand description", "# Page\n\nOriginal content."}),
		base, false, nil, nil, nil, handle,
	); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	stale, _, err := UpdateHashes(
		docs([3]string{"page.md", "Hand description", "# Page\n\nDifferent content."}),
		base, false, nil, nil, map[string]bool{"other.md": true}, handle,
	)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 1 || stale[0].Page != "page.md" {
		t.Errorf("reported %v, want page.md stale", stale)
	}
}

func TestUpdateHashesSkipsPagesWithNoDescription(t *testing.T) {
	base := testproject.Dir(t)
	stale, _, err := UpdateHashes(
		docs([3]string{"no-desc.md", "", "# No Description\n\nJust content."}),
		base, false, nil, nil, nil, effects.Unbound(),
	)
	if err != nil {
		t.Fatalf("updating the hashes: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("reported %v for a page with no description", stale)
	}
	if _, ok := readStore(t, base)["no-desc.md"]; ok {
		t.Error("a page with no description was recorded")
	}
}

// TestUpdateHashesKeysPassThroughUntouched covers the locale prefix: the
// caller keys allDocs, and the store records whatever keys it was given.
func TestUpdateHashesKeysPassThroughUntouched(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "a locale-prefixed key", key: "en/page.md"},
		{name: "a bare key", key: "page.md"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := testproject.Dir(t)
			pages := docs([3]string{test.key, "A test page", "# Test\n\nSome content."})
			if _, _, err := UpdateHashes(pages, base, false, nil, nil, nil, effects.Unbound()); err != nil {
				t.Fatalf("updating the hashes: %v", err)
			}
			document := readStore(t, base)
			if _, ok := document[test.key]; !ok {
				t.Errorf("the store carries no %q; it holds %v", test.key, keysOf(document))
			}
			for key := range document {
				if strings.HasPrefix(key, "_") {
					continue
				}
				if key != test.key {
					t.Errorf("the store carries an unexpected key %q", key)
				}
			}
		})
	}
}

// keysOf lists a document's keys, for a failure message.
func keysOf(document map[string]any) []string {
	keys := []string{}
	for key := range document {
		keys = append(keys, key)
	}
	return keys
}

// -- The baseline hold ------------------------------------------------------

func TestStalenessPersistsUntilTheDescriptionIsRewritten(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	if _, _, err := UpdateHashes(
		docs([3]string{"page.md", "old desc", "# Page\n\nOriginal content."}),
		base, false, nil, nil, nil, handle,
	); err != nil {
		t.Fatalf("the first pass: %v", err)
	}

	changed := docs([3]string{"page.md", "old desc", "# Page\n\nCompletely rewritten content."})
	stale, _, err := UpdateHashes(changed, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("the first report was %v, want one stale page", stale)
	}

	stale, _, err = UpdateHashes(changed, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the third pass: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("the error did not persist: %v", stale)
	}

	rewritten := docs([3]string{
		"page.md", "new desc matching new content", "# Page\n\nCompletely rewritten content.",
	})
	stale, _, err = UpdateHashes(rewritten, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the fourth pass: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("the error survived the description rewrite: %v", stale)
	}
}

func TestTheHoldFreezesEveryFieldOfAPageWithErrors(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	if _, _, err := UpdateHashes(
		docs([3]string{"page.md", "old desc", "# Page\n\nOriginal content."}),
		base, false, nil, nil, nil, handle,
	); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	before, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the baseline: %v", err)
	}

	stale, _, err := UpdateHashes(
		docs([3]string{"page.md", "old desc", "# Page\n\nNew content here."}),
		base, false, nil, nil, nil, handle,
	)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("reported %v, want one stale page", stale)
	}
	after, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the held baseline: %v", err)
	}
	if after["page.md"] != before["page.md"] {
		t.Errorf("the held entry moved: %+v, was %+v", after["page.md"], before["page.md"])
	}
}

func TestSchemaDriftPersistsUntilTheDescriptionIsRewritten(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	pages := docs([3]string{"cli-build.md", "Build the project", "# Build\n\nContent."})

	_, drift, err := UpdateHashes(pages, base, false, nil, map[string]string{"cli-build.md": "schema_v1"}, nil, handle)
	if err != nil {
		t.Fatalf("the baseline pass: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("a new page reported %v", drift)
	}
	store, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the baseline: %v", err)
	}
	if store["cli-build.md"].SchemaHash != "schema_v1" {
		t.Fatalf("the schema hash recorded as %q", store["cli-build.md"].SchemaHash)
	}

	changed := map[string]string{"cli-build.md": "schema_v2"}
	_, drift, err = UpdateHashes(pages, base, false, nil, changed, nil, handle)
	if err != nil {
		t.Fatalf("the drift pass: %v", err)
	}
	if len(drift) != 1 || !strings.Contains(drift[0].Message, "CLI schema changed") {
		t.Fatalf("reported %v, want one schema drift", drift)
	}

	_, drift, err = UpdateHashes(pages, base, false, nil, changed, nil, handle)
	if err != nil {
		t.Fatalf("the second drift pass: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("the schema drift did not persist: %v", drift)
	}

	fixed := docs([3]string{"cli-build.md", "Build the project with new flags", "# Build\n\nContent."})
	_, drift, err = UpdateHashes(fixed, base, false, nil, changed, nil, handle)
	if err != nil {
		t.Fatalf("the pass after the rewrite: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("the drift survived the description rewrite: %v", drift)
	}
}

// TestSourceDocstringDriftIsMeasuredThroughTheExtractor covers the whole
// drift path: a module page's baseline is the docstring hash of the sources
// its directives resolved to, and a changed docstring with an unchanged
// description reports.
func TestSourceDocstringDriftIsMeasuredThroughTheExtractor(t *testing.T) {
	requirePython3(t)
	hygiene.Isolate(t)
	base := testproject.Dir(t)
	handle := effects.Unbound()
	source := filepath.Join(base, "mod.py")
	if err := os.WriteFile(source, []byte("\"\"\"Original docstring.\"\"\"\n\ndef foo(): pass\n"), 0o644); err != nil {
		t.Fatalf("writing the source: %v", err)
	}
	pages := docs([3]string{"mod.md", "Original docstring.", "# Mod\n\nContent."})
	// A fresh extractor per pass: the Go extractor remembers a file it has
	// already read for the lifetime of the instance, and a single selfdoc run
	// builds one extractor and reads each file once.
	directivesWith := func(extractor extractors.Extractor) map[string][]PageDirective {
		return map[string][]PageDirective{
			"mod.md": {{
				PathArg:     source,
				SourceEntry: &extractors.SourceEntry{Path: base, Language: "python", Extractor: extractor},
			}},
		}
	}

	_, drift, err := UpdateHashes(pages, base, false, directivesWith(pythonextractor.New()), nil, nil, handle)
	if err != nil {
		t.Fatalf("the baseline pass: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("a new page reported %v", drift)
	}
	store, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the baseline: %v", err)
	}
	if store["mod.md"].SourceDocstring == "" {
		t.Fatal("the baseline recorded no source-docstring hash")
	}

	if err := os.WriteFile(source, []byte("\"\"\"Updated docstring with new info.\"\"\"\n\ndef foo(): pass\n"), 0o644); err != nil {
		t.Fatalf("rewriting the source: %v", err)
	}
	_, drift, err = UpdateHashes(pages, base, false, directivesWith(pythonextractor.New()), nil, nil, handle)
	if err != nil {
		t.Fatalf("the drift pass: %v", err)
	}
	if len(drift) != 1 || !strings.Contains(drift[0].Message, "documentation drift") {
		t.Errorf("reported %v, want one documentation drift", drift)
	}
}

// -- Raw-body hashing -------------------------------------------------------

// TestOnlyDirectiveOutputChangingIsNotStaleness covers why the content hash
// reads the raw body: a version directive's output moves on every release,
// and a page whose prose nobody touched is not stale.
func TestOnlyDirectiveOutputChangingIsNotStaleness(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	raw := "# Page\n\nVersion: :-: var key=\"project.version\"\n"
	pages := docs([3]string{"page.md", "A page about the project", raw})

	stale, _, err := UpdateHashes(pages, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("a new page reported %v", stale)
	}

	// The resolved output would now read 1.1.0 instead of 1.0.0; the raw
	// body is unchanged, which is what the hash covers.
	stale, _, err = UpdateHashes(pages, base, false, nil, nil, nil, handle)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("a changed directive output reported %v", stale)
	}
}

func TestProseChangingIsStaleness(t *testing.T) {
	base := testproject.Dir(t)
	handle := effects.Unbound()
	if _, _, err := UpdateHashes(
		docs([3]string{"page.md", "A page about things", "# Page\n\nOriginal prose.\n"}),
		base, false, nil, nil, nil, handle,
	); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	stale, _, err := UpdateHashes(
		docs([3]string{"page.md", "A page about things", "# Page\n\nRewritten prose.\n"}),
		base, false, nil, nil, nil, handle,
	)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if len(stale) != 1 || !strings.Contains(stale[0].Message, "stale description") {
		t.Errorf("reported %v, want one stale page", stale)
	}
}

// -- The two-writer merge ---------------------------------------------------

func TestAdvancingAPageKeepsTheGenOwnedSeedHash(t *testing.T) {
	base := testproject.Dir(t)
	writeStoreFile(t, base, `{
  "_hash_version": 3,
  "page.md": {"seed_hash": "the_seed_digest"}
}`)
	pages := docs([3]string{"page.md", "A page about things", "# Page\n\nProse.\n"})
	if _, _, err := UpdateHashes(pages, base, false, nil, nil, nil, effects.Unbound()); err != nil {
		t.Fatalf("updating the hashes: %v", err)
	}
	store, err := LoadHashes(base)
	if err != nil {
		t.Fatalf("loading the store: %v", err)
	}
	entry := store["page.md"]
	if entry.SeedHash != "the_seed_digest" {
		t.Errorf("the seed hash is %q, want it kept", entry.SeedHash)
	}
	if entry.Content == "" || entry.Description == "" {
		t.Errorf("the advancing fields were not written: %+v", entry)
	}
}

// -- Recorded parity with the Python's own digests --------------------------

// TestRecordedDigests pins three digests the Python implementation printed
// for the same inputs. They cover the two places a divergence would be
// invisible until every project's baseline had silently reset: the canonical
// JSON the schema hash is computed over, and the frontmatter-stripped,
// marker-canonicalized body the content hash is computed over.
func TestRecordedDigests(t *testing.T) {
	schemaSlice := map[string]any{
		"name": "build",
		"help": "Build – it",
		"flags": []any{
			map[string]any{"name": "verbose", "default": true},
			map[string]any{"name": "n", "default": 1.5},
		},
		"args": []any{},
	}
	got, err := ComputeSchemaHash(schemaSlice)
	if err != nil {
		t.Fatalf("hashing the schema slice: %v", err)
	}
	const wantSchema = "51e76ba786dfaef02515675a21d8833084e491dede320e9cb745a1e4a7657ef3"
	if got != wantSchema {
		t.Errorf("schema digest %s, want the Python's %s", got, wantSchema)
	}

	const wantContent = "0f9cdd2107863b5332a13206b66c3ae3ba5621ca4c2d2b8b3ac9a397b1321cdb"
	if got := ComputeContentHash("# T\n\n:-: ref path=\"q\"\n"); got != wantContent {
		t.Errorf("content digest %s, want the Python's %s", got, wantContent)
	}
}

// TestAnIndentedMarkerLineIsStillCanonicalized covers the leading-whitespace
// reading: a marker inside a list item is a marker, so its attribute values
// are blanked like any other's.
func TestAnIndentedMarkerLineIsStillCanonicalized(t *testing.T) {
	tests := []struct{ name, before, after string }{
		{
			name:   "space-indented",
			before: "  :-: ref path=\"a\"\n",
			after:  "  :-: ref path=\"b\"\n",
		},
		{
			name:   "tab-indented",
			before: "\t:@: path=\"a\"\n",
			after:  "\t:@: path=\"b\"\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if ComputeContentHash(test.before) != ComputeContentHash(test.after) {
				t.Error("an indented marker line's attribute value reached the hash")
			}
		})
	}
}
