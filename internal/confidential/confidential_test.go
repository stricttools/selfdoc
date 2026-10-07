package confidential

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	"github.com/stricttools/strictspec/go/lifecycle"
	"github.com/stricttools/testisolation/go/hygiene"
)

// recordingWriter is a lifecycle.FileWriter that records the writes it is
// asked for and makes them, so a test can assert both what was written and
// that nothing was written.
type recordingWriter struct {
	writes []string
}

func (w *recordingWriter) WriteFile(path string, data []byte) error {
	w.writes = append(w.writes, path)
	return os.WriteFile(path, data, 0o644)
}

func (w *recordingWriter) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

var today = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func parseRecord(t *testing.T, src string) *lifecycle.Record {
	t.Helper()
	record, err := lifecycle.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parsing the record: %v", err)
	}
	return record
}

const proprietaryWidget = "format_version = 1\n\n[[licenses]]\nsubject = \"widget\"\nlicense = \"proprietary\"\nfrom = 2020-01-01\nreason = \"the widget is proprietary\"\n"

const mixedLicenses = proprietaryWidget + "\n[[licenses]]\nsubject = \"gadget\"\nlicense = \"MIT\"\nfrom = 2020-01-01\nreason = \"the gadget client is public\"\n"

func writeIndex(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRefreshUpsertsAConfidentialRepository(t *testing.T) {
	hygiene.Isolate(t)
	indexPath := filepath.Join(t.TempDir(), "strictspec", "confidential-names.toml")
	w := &recordingWriter{}
	repo := Repository{Origin: "https://git.invalid/example/gadget-works.git", Record: parseRecord(t, proprietaryWidget)}

	if err := Refresh(w, indexPath, repo, today); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := readFile(t, indexPath)
	for _, want := range []string{`origin = "git.invalid/example/gadget-works"`, `"widget"`, `"gadget-works"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the index does not hold %s:\n%s", want, got)
		}
	}

	// An unchanged upsert writes nothing.
	w.writes = nil
	if err := Refresh(w, indexPath, repo, today); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if len(w.writes) != 0 {
		t.Errorf("an unchanged refresh wrote %v", w.writes)
	}
}

func TestRefreshRemovesAPublicRepositoryAndKeepsTheOthers(t *testing.T) {
	hygiene.Isolate(t)
	indexPath := filepath.Join(t.TempDir(), "confidential-names.toml")
	writeIndex(t, indexPath, "format_version = 1\n\n[[repositories]]\norigin = \"git.invalid/example/gadget-works\"\nnames = [\"widget\"]\n\n[[repositories]]\norigin = \"github.com/example/portal\"\nnames = [\"portal\"]\n")
	w := &recordingWriter{}
	repo := Repository{Origin: "git@git.invalid:example/gadget-works.git", Record: parseRecord(t, "format_version = 1\n")}

	if err := Refresh(w, indexPath, repo, today); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := readFile(t, indexPath)
	if strings.Contains(got, "gadget-works") {
		t.Errorf("the public repository's entry was not removed:\n%s", got)
	}
	if !strings.Contains(got, `"portal"`) {
		t.Errorf("another repository's entry was removed:\n%s", got)
	}
}

func TestRefreshOfAPublicRepositoryWithoutAnOriginWritesNothing(t *testing.T) {
	hygiene.Isolate(t)
	indexPath := filepath.Join(t.TempDir(), "confidential-names.toml")
	w := &recordingWriter{}
	if err := Refresh(w, indexPath, Repository{Record: parseRecord(t, "format_version = 1\n")}, today); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(w.writes) != 0 {
		t.Errorf("refresh wrote %v", w.writes)
	}
}

func TestRefreshRefusesAConfidentialRepositoryWithoutAnOrigin(t *testing.T) {
	hygiene.Isolate(t)
	indexPath := filepath.Join(t.TempDir(), "confidential-names.toml")
	w := &recordingWriter{}
	err := Refresh(w, indexPath, Repository{Record: parseRecord(t, proprietaryWidget)}, today)
	if err == nil || !strings.Contains(err.Error(), "git remote add origin") {
		t.Fatalf("a confidential repository with no origin was not refused naming the fix: %v", err)
	}
	if len(w.writes) != 0 {
		t.Errorf("a refused refresh wrote %v", w.writes)
	}
}

func TestPublicOutputAllowed(t *testing.T) {
	hygiene.Isolate(t)
	cases := []struct {
		name   string
		record string
		refuse bool
	}{
		{"no record", "format_version = 1\n", false},
		{"every license proprietary", proprietaryWidget, true},
		{"a public client beside a proprietary server", mixedLicenses, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := PublicOutputAllowed(parseRecord(t, tc.record), lifecycle.PublicDocs, today)
			if tc.refuse {
				if err == nil {
					t.Fatal("the output was allowed")
				}
				if !strings.Contains(err.Error(), string(lifecycle.RuleProprietaryRefusesPublicOutput)) || !strings.Contains(err.Error(), "widget") {
					t.Errorf("the refusal does not name the rule and the subject: %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("the output was refused: %v", err)
			}
		})
	}
}

func TestScanDirNamesThePageLineAndTerm(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "guide"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guide", "index.html"), []byte("<p>intro</p>\n<p>the Portal opens</p>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>portals and portal-like things</p>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings, err := ScanDir([]string{"portal"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one (whole tokens only)", findings)
	}
	want := Finding{Page: "guide/index.html", Line: 2, Column: 8, Term: "portal"}
	if findings[0] != want {
		t.Errorf("finding = %+v, want %+v", findings[0], want)
	}

	err = Refusal("deploy", findings, "/index/path")
	if err == nil || !strings.Contains(err.Error(), "guide/index.html, line 2, column 8: portal") {
		t.Errorf("the refusal does not name the page, line, and term: %v", err)
	}
	if Refusal("deploy", nil, "/index/path") != nil {
		t.Error("no findings refused the output")
	}
}

func TestScanFilesScansEveryPublishedFile(t *testing.T) {
	hygiene.Isolate(t)
	findings := ScanFiles([]string{"widget"}, map[string][]byte{
		"site/blog/launch/index.html": []byte("<h1>Launch</h1>\n<p>about the widget</p>\n"),
		"manifests/home-posts.json":   []byte(`{"title": "Widget news"}`),
		"site/blog/other/index.html":  []byte("nothing here\n"),
	})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want two", findings)
	}
	if findings[0].Page != "manifests/home-posts.json" || findings[1].Page != "site/blog/launch/index.html" {
		t.Errorf("findings are not in page order: %+v", findings)
	}
}

func TestLocateReadsTheRepositoryRootOriginAndRecord(t *testing.T) {
	hygiene.Isolate(t)
	root := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", "https://git.invalid/example/gadget-works.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	recordDir := filepath.Join(root, ".strictmetadata", "lifecycle-and-license")
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordDir, "lifecycle-and-license.toml"), []byte(proprietaryWidget), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "docs-project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Locate(effects.Unbound(), project)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRoot, _ := filepath.EvalSymlinks(repo.Root)
	if !repo.InGit || gotRoot != wantRoot {
		t.Errorf("root = %q (in git %v), want %q", repo.Root, repo.InGit, root)
	}
	if repo.Origin != "https://git.invalid/example/gadget-works.git" {
		t.Errorf("origin = %q", repo.Origin)
	}
	if !repo.Record.Confidential(today) {
		t.Error("the record read from the repository root is not confidential")
	}

	outside, err := Locate(effects.Unbound(), t.TempDir())
	if err != nil {
		t.Fatalf("locate outside git: %v", err)
	}
	if outside.InGit || outside.Origin != "" || outside.Record.Present() {
		t.Errorf("a directory in no repository located as %+v", outside)
	}
}
