package check

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// versionProject writes a project whose config document is the given one, with
// one source module and one page, plus the optional manifests a detected
// version is read from.
func versionProject(
	t *testing.T, projectConfig map[string]any, pyprojectVersion string,
) string {
	t.Helper()
	isolate(t)
	root := t.TempDir()
	writeConfig(t, root, projectConfig)
	write(t, filepath.Join(root, "mylib", "__init__.py"), `"""Lib."""`+"\n")
	write(t, filepath.Join(root, ".strictmetadata", "docs", "index.md"),
		"+++\ntitle = \"Test\"\ndescription = \"A test project for version consistency "+
			"checking across builds\"\n+++\n\n# Test\n")
	if pyprojectVersion != "" {
		write(t, filepath.Join(root, "pyproject.toml"),
			"[project]\nname = \"mylib\"\nversion = \""+pyprojectVersion+"\"\n")
	}
	return root
}

func TestVersionConsistencyRules(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		configVersion    string
		versions         []any
		pyprojectVersion string
		want             []string
		absent           []string
	}{
		{
			name:             "version-mismatch-with-project-manifest the config disagrees with the manifest",
			configVersion:    "2.0.0",
			pyprojectVersion: "1.0.0",
			want:             []string{"version-mismatch-with-project-manifest"},
		},
		{
			name:             "version-mismatch-with-project-manifest is silent when they agree",
			configVersion:    "1.0.0",
			pyprojectVersion: "1.0.0",
			absent:           []string{"version-mismatch-with-project-manifest"},
		},
		{
			name:          "version-mismatch-with-project-manifest is silent with no manifest to detect from",
			configVersion: "1.0.0",
			absent:        []string{"version-mismatch-with-project-manifest"},
		},
		{
			name:             "version-mismatch-with-versions-array the versions array's last entry disagrees",
			configVersion:    "2.0.0",
			pyprojectVersion: "2.0.0",
			versions:         []any{map[string]any{"version": "1.0.0"}},
			want:             []string{"version-mismatch-with-versions-array"},
		},
		{
			name:             "version-mismatch-with-versions-array is silent when they agree",
			configVersion:    "2.0.0",
			pyprojectVersion: "2.0.0",
			versions:         []any{map[string]any{"version": "2.0.0"}},
			absent:           []string{"version-mismatch-with-versions-array"},
		},
		{
			name:             "version-mismatch-with-versions-array is silent with no versions array",
			configVersion:    "2.0.0",
			pyprojectVersion: "2.0.0",
			absent:           []string{"version-mismatch-with-versions-array"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projectConfig := pythonProjectConfig()
			projectConfig["version"] = testCase.configVersion
			if testCase.versions != nil {
				projectConfig["versions"] = testCase.versions
			}
			root := versionProject(t, projectConfig, testCase.pyprojectVersion)
			result := checkFixture(t, root)
			for _, code := range testCase.want {
				if !hasCode(result.Lints, code) {
					t.Errorf("%s missing; got %v", code, codes(result.Lints))
				}
			}
			for _, code := range testCase.absent {
				if hasCode(result.Lints, code) {
					t.Errorf("%s fired: %v", code,
						messagesOf(withCode(result.Lints, code)))
				}
			}
		})
	}
}

func TestVER004GeneratedRootFileVersion(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		template        string
		generated       string
		writeGenerated  bool
		versionOverride string
		want            bool
	}{
		{
			name:           "a generated file one release behind",
			template:       "# Project\n\nVersion :-: var key=\"project.version\"\n",
			generated:      "# Project\n\nVersion 0.9.0\n",
			writeGenerated: true,
			want:           true,
		},
		{
			name:           "a generated file carrying the expected version",
			template:       "# Project\n\nVersion :-: var key=\"project.version\"\n",
			generated:      "# Project\n\nVersion 1.0.0\n",
			writeGenerated: true,
			want:           false,
		},
		{
			name:           "a template that does not interpolate the version",
			template:       "# Project\n\nNo version here.\n",
			generated:      "# Project\n\nNo version here.\n",
			writeGenerated: true,
			want:           false,
		},
		{
			name:           "a template whose output was never generated",
			template:       "# Project\n\nVersion :-: var key=\"project.version\"\n",
			writeGenerated: false,
			want:           false,
		},
		{
			name:            "an override names the version the file must carry",
			template:        "# Project\n\nVersion :-: var key=\"project.version\"\n",
			generated:       "# Project\n\nVersion 1.0.0\n",
			writeGenerated:  true,
			versionOverride: "1.1.0",
			want:            true,
		},
		{
			name:            "an override the file already carries is silent",
			template:        "# Project\n\nVersion :-: var key=\"project.version\"\n",
			generated:       "# Project\n\nVersion 1.1.0\n",
			writeGenerated:  true,
			versionOverride: "1.1.0",
			want:            false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projectConfig := pythonProjectConfig()
			projectConfig["root_files"] = []any{".strictmetadata/docs/_README.md"}
			root := versionProject(t, projectConfig, "1.0.0")
			write(t, filepath.Join(root, ".strictmetadata", "docs", "_README.md"), testCase.template)
			if testCase.writeGenerated {
				write(t, filepath.Join(root, "README.md"), testCase.generated)
			}

			result, err := CheckDocs(
				root, nil, false, "", testCase.versionOverride, handle(),
			)
			if err != nil {
				t.Fatalf("CheckDocs: %v", err)
			}
			fired := hasCode(result.Lints, "version-mismatch-in-generated-root-file")
			if fired != testCase.want {
				t.Fatalf("version-mismatch-in-generated-root-file fired = %v, want %v: %v",
					fired, testCase.want, messagesOf(withCode(result.Lints, "version-mismatch-in-generated-root-file")))
			}
			if !fired {
				return
			}
			diagnostic := withCode(result.Lints, "version-mismatch-in-generated-root-file")[0]
			if diagnostic.File() != "README.md" {
				t.Errorf("file = %q, want README.md", diagnostic.File())
			}
			if !strings.Contains(diagnostic.Message(), "--version-override") {
				t.Errorf("message = %q, want it to name the remedy flag",
					diagnostic.Message())
			}
		})
	}
}

// gitInit sets up a repository in dir and commits everything in it.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
}

// runGit runs one git command in dir, failing the test when it does.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// multiVersionProject writes a project with two declared versions whose older
// tag carries no docs directory, so extracting it fails and version-tag-not-extractable fires.
func multiVersionProject(t *testing.T) string {
	t.Helper()
	isolate(t)
	root := filepath.Join(t.TempDir(), "proj")
	projectConfig := configForSource(
		map[string]any{"path": "src/", "language": "python"},
	)
	projectConfig["version"] = "0.2.0"
	projectConfig["versions"] = []any{
		map[string]any{"version": "0.1.0"},
		map[string]any{"version": "0.2.0"},
	}
	projectConfig["locales"] = []any{
		map[string]any{"code": "en", "label": "English", "default": true},
	}
	writeConfig(t, root, projectConfig)
	write(t, filepath.Join(root, "src", "__init__.py"), `"""Pkg."""`+"\n")

	gitInit(t, root)
	runGit(t, root, "tag", "v0.1.0")

	write(t, filepath.Join(root, ".strictmetadata", "docs", "index.md"), "# Project\n\nWelcome.\n")
	runGit(t, root, "add", ".strictmetadata/docs/")
	runGit(t, root, "commit", "-m", "add docs")
	runGit(t, root, "tag", "v0.2.0")

	return root
}

func TestVersionFilterControlsVER001(t *testing.T) {

	t.Run("without a filter version-tag-not-extractable fires", func(t *testing.T) {
		root := multiVersionProject(t)
		result, err := CheckDocs(root, nil, true, "", "", handle())
		if err != nil {
			t.Fatalf("CheckDocs: %v", err)
		}
		matching := withCode(result.Lints, "version-tag-not-extractable")
		if len(matching) == 0 {
			t.Fatalf("version-tag-not-extractable missing; got %v", codes(result.Lints))
		}
		if !strings.Contains(matching[0].Message(), "0.1.0") {
			t.Errorf("message = %q, want it to name the version", matching[0].Message())
		}
		if matching[0].File() != "[0.1.0]" {
			t.Errorf("file = %q, want the version label", matching[0].File())
		}
	})

	t.Run("with a filter the whole pass is skipped", func(t *testing.T) {
		root := multiVersionProject(t)
		result, err := CheckDocs(root, nil, true, "0.2.0", "", handle())
		if err != nil {
			t.Fatalf("CheckDocs: %v", err)
		}
		if hasCode(result.Lints, "version-tag-not-extractable") {
			t.Errorf("version-tag-not-extractable fired under a version filter: %v",
				messagesOf(withCode(result.Lints, "version-tag-not-extractable")))
		}
	})

	t.Run("the other checks still run under a filter", func(t *testing.T) {
		root := multiVersionProject(t)
		write(t, filepath.Join(root, ".strictmetadata", "docs", "api.md"),
			"# API\n\n:-: ref path=\"nonexistent_module\"\n")
		runGit(t, root, "add", ".strictmetadata/docs/api.md")
		runGit(t, root, "commit", "-m", "add broken directive")

		result, err := CheckDocs(root, nil, true, "0.2.0", "", handle())
		if err != nil {
			t.Fatalf("CheckDocs: %v", err)
		}
		failed := 0
		for _, directiveResult := range result.DirectiveResults {
			if directiveResult.Outcome == StatusFailed {
				failed++
			}
		}
		if failed == 0 {
			t.Errorf("no directive failed; got %+v", result.DirectiveResults)
		}
		if hasCode(result.Lints, "version-tag-not-extractable") {
			t.Error("version-tag-not-extractable fired under a version filter")
		}
	})
}
