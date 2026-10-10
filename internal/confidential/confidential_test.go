package confidential

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/selfdoc/internal/effects"
	strictconfidential "github.com/stricttools/strictspec/go/confidential"
	"github.com/stricttools/strictspec/go/lifecycle"
	"github.com/stricttools/testisolation/go/hygiene"
)

var today = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func parseRecord(t *testing.T, src string) *lifecycle.Record {
	t.Helper()
	record, err := lifecycle.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parsing the record: %v", err)
	}
	return record
}

// widgetName is the widget's releasable-name identity.
const widgetName = "\n[[identities]]\nsubject = \"widget\"\nfacet = \"releasable-name\"\nvalue = \"widget\"\nregistry = \"\"\ntag_patterns = [\"v*\"]\nfrom = 2020-01-01\nreason = \"its name\"\n"

const proprietaryWidget = "format_version = 1\n\n[[licenses]]\nsubject = \"widget\"\nlicense = \"proprietary\"\nfrom = 2020-01-01\nreason = \"the widget is proprietary\"\n" + widgetName

const publicWidget = "format_version = 1\n\n[[licenses]]\nsubject = \"widget\"\nlicense = \"MIT\"\nfrom = 2020-01-01\nreason = \"the widget is public\"\n" + widgetName

const mixedLicenses = proprietaryWidget + "\n[[licenses]]\nsubject = \"gadget\"\nlicense = \"MIT\"\nfrom = 2020-01-01\nreason = \"the gadget client is public\"\n"

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

// portalList is a confidential-term list naming "portal", scoped
// everywhere but a repository named portal-works.
func portalList(t *testing.T) *strictconfidential.List {
	t.Helper()
	l, err := strictconfidential.FromEntries("the test list", []strictconfidential.Entry{
		{Term: "portal", Added: "2026-10-01", Reason: "a private project", Except: []string{"portal-works"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCheckRefusesAnUnresolvedHitAndAcceptsAResolvedOne(t *testing.T) {
	hygiene.Isolate(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "guide"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guide", "index.html"), []byte("<p>intro</p>\n<p>the Portal opens</p>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("portal\x00\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	texts, err := DirTexts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 1 || texts[0].Page != "guide/index.html" {
		t.Fatalf("texts %+v: the binary file is not a text", texts)
	}
	root := t.TempDir()
	repo := Repository{Root: root, Record: &lifecycle.Record{}}
	_, err = Check(portalList(t), repo, "deploy", texts)
	if err == nil {
		t.Fatal("a page carrying a term was accepted")
	}
	for _, want := range []string{"refusing the deploy", "guide/index.html, line 2, column 8: <p>the >>Portal<< opens</p>", "rlsbl confidential judge-false-positive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	id := regexp.MustCompile(`hit ([0-9a-f]{16})`).FindStringSubmatch(err.Error())[1]
	resolution := strictconfidential.Resolution{Hit: id, Location: "guide/index.html, line 2, column 8", Decision: strictconfidential.FalsePositive, Certainty: 80, Reason: "a door in a story", Recorded: "2026-10-01"}
	store := filepath.Join(root, filepath.FromSlash(strictconfidential.StoreFile))
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, strictconfidential.RenderResolutions([]strictconfidential.Resolution{resolution}), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := Check(portalList(t), repo, "deploy", texts)
	if err != nil {
		t.Fatalf("the resolved hit is still refused: %v", err)
	}
	if !strings.Contains(status, "1 hit(s) resolved") || !strings.Contains(status, "a door in a story") {
		t.Errorf("status %q", status)
	}
}

func TestAnEntryScopedAwayFromTheRepositoryDoesNotApply(t *testing.T) {
	hygiene.Isolate(t)
	repo := Repository{Root: filepath.Join(t.TempDir(), "portal-works"), Record: &lifecycle.Record{}}
	if _, err := Check(portalList(t), repo, "deploy", FilesTexts(map[string][]byte{"index.html": []byte("the portal\n")})); err != nil {
		t.Errorf("a repository the entry exempts was refused: %v", err)
	}
}

func TestAMissingListHasNoTermsAndSaysSo(t *testing.T) {
	hygiene.Isolate(t)
	list, err := strictconfidential.Load(strictconfidential.LocationIn(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	status, err := Check(list, Repository{Root: t.TempDir(), Record: &lifecycle.Record{}}, "deploy", FilesTexts(map[string][]byte{"a.html": []byte("anything\n")}))
	if err != nil || !strings.Contains(status, "does not exist, so there are no confidential terms") {
		t.Errorf("status %q, err %v", status, err)
	}
}

func TestFilesTextsAreInPageOrder(t *testing.T) {
	hygiene.Isolate(t)
	texts := FilesTexts(map[string][]byte{
		"site/blog/launch/index.html": []byte("<h1>Launch</h1>\n"),
		"manifests/home-posts.json":   []byte(`{"title": "news"}`),
		"site/logo.png":               []byte("\x00\x01"),
	})
	if len(texts) != 2 || texts[0].Page != "manifests/home-posts.json" || texts[1].Page != "site/blog/launch/index.html" {
		t.Errorf("texts %+v", texts)
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
