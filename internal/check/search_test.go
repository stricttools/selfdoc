package check

import (
	"os"
	"path/filepath"
	"testing"
)

// codelessProject writes a project that declares no source, so the check runs
// without reaching any extractor -- which is what lets these cases control the
// PATH the indexer probe searches.
func codelessProject(t *testing.T) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	projectConfig := configForSource()
	writeConfig(t, root, projectConfig)
	write(t, filepath.Join(root, "stricttools", "docs", "index.md"),
		"+++\ndescription = \"A home page whose description is long enough to keep "+
			"the description rules quiet in this fixture.\"\n+++\n# Home\n\nWelcome.\n")
	return root
}

// emptyPath points PATH at a directory holding nothing, so neither the
// standalone indexer nor an interpreter that could run it resolves.
func emptyPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// fakePagefind puts an executable named pagefind, which exits zero for
// --version, at the front of PATH.
func fakePagefind(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	script := filepath.Join(binDir, "pagefind")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write the stub indexer: %v", err)
	}
	t.Setenv("PATH", binDir)
}

func TestSEARCH001ReportsAMissingIndexer(t *testing.T) {
	root := codelessProject(t)
	emptyPath(t)

	result := checkFixture(t, root)

	matching := withCode(result.Lints, "SEARCH001")
	if len(matching) != 1 {
		t.Fatalf("SEARCH001 count = %d, want 1: %v", len(matching), codes(result.Lints))
	}
	if matching[0].Severity() != "error" {
		t.Errorf("severity = %q, want error", matching[0].Severity())
	}
	if matching[0].File() != "selfdoc.json" {
		t.Errorf("file = %q, want selfdoc.json", matching[0].File())
	}
}

func TestSEARCH001IsSilentWhenTheIndexerAnswers(t *testing.T) {
	root := codelessProject(t)
	fakePagefind(t)

	result := checkFixture(t, root)

	if hasCode(result.Lints, "SEARCH001") {
		t.Errorf("SEARCH001 fired with an indexer on PATH: %v",
			messagesOf(withCode(result.Lints, "SEARCH001")))
	}
}
