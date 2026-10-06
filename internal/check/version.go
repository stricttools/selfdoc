package check

import (
	"fmt"
	selfdocconfig "github.com/stricttools/selfdoc/internal/config"
	"os"
	"path/filepath"
	"strings"

	"github.com/stricttools/selfdoc/internal/directives"
	"github.com/stricttools/selfdoc/internal/gen"
	"github.com/stricttools/selfdoc/internal/lints"
	"github.com/stricttools/selfdoc/internal/util"
)

// checkVersionConsistency checks a project's version against its own
// declarations.
//
// version-mismatch-with-project-manifest: selfdoc.json's "version" differs from the version detected from the
// project manifest. version-mismatch-with-versions-array: the last entry of the "versions" array does not
// match selfdoc.json's "version".
func checkVersionConsistency(config map[string]any, dirPath string) []lints.LintResult {
	var results []lints.LintResult

	configVersion := configString(config, "version", "")

	if configVersion != "" {
		detected := util.DetectProjectVersion(dirPath, "")
		if detected != "" && detected != configVersion {
			results = append(results, lints.MustLintResult(
				"selfdoc.json", nil, "version-mismatch-with-project-manifest",
				fmt.Sprintf(
					"Config version '%s' does not match detected project version '%s'",
					configVersion, detected,
				),
			))
		}
	}

	versions := configList(config, "versions")
	if configVersion != "" && len(versions) > 0 {
		lastVersion := ""
		if entry, isMap := versions[len(versions)-1].(map[string]any); isMap {
			lastVersion = configString(entry, "version", "")
		}
		if lastVersion != "" && lastVersion != configVersion {
			results = append(results, lints.MustLintResult(
				"selfdoc.json", nil, "version-mismatch-with-versions-array",
				fmt.Sprintf(
					"Last entry in versions array ('%s') does not match config version ('%s')",
					lastVersion, configVersion,
				),
			))
		}
	}

	return results
}

// checkVersionMatch checks that version-bearing generated content is not stale
// (version-mismatch-in-generated-root-file).
//
// A root file generated from a template that interpolates
// `var key="project.version"` carries a RESOLVED version literal on disk.
// Generation runs before the version bump in a release, so without
// `selfdoc gen --version-override` those committed files end up one release
// behind -- silently. This check turns that lag into a hard failure by
// requiring the generated file to embed the expected version.
//
// The expected version is versionOverride when given (the about-to-be-released
// version, matching what the orchestrator passes to gen), otherwise the
// version detected from the project manifest.
func checkVersionMatch(
	config map[string]any, dirPath, versionOverride string,
) ([]lints.LintResult, error) {
	expected := versionOverride
	if expected == "" {
		expected = util.DetectProjectVersion(dirPath, "")
	}
	if expected == "" {
		return nil, nil
	}

	var results []lints.LintResult

	for _, rootFile := range selfdocconfig.RootFiles(config) {
		templatePath := rootFile.Template
		fullTemplate := filepath.Join(dirPath, templatePath)
		if !isFile(fullTemplate) {
			// A missing template is reported by gen, not here.
			continue
		}
		raw, err := os.ReadFile(fullTemplate)
		if err != nil {
			return nil, err
		}
		carries, err := hasVersionVarDirective(string(raw))
		if err != nil {
			return nil, err
		}
		if !carries {
			continue
		}

		for _, outputName := range rootFile.Outputs {
			outputPath := filepath.Join(dirPath, filepath.FromSlash(outputName))
			if !isFile(outputPath) {
				// Not generated yet -- gen's concern, not a version
				// mismatch.
				continue
			}
			generated, err := os.ReadFile(outputPath)
			if err != nil {
				return nil, err
			}
			if strings.Contains(string(generated), expected) {
				continue
			}
			results = append(results, lints.MustLintResult(
				outputName, nil, "version-mismatch-in-generated-root-file",
				fmt.Sprintf(
					"Generated root file '%s' embeds the project version from '%s' "+
						"but does not contain the expected version '%s'. Regenerate "+
						"with 'selfdoc gen --version-override %s' so the committed "+
						"file is not one release behind.",
					outputName, templatePath, expected, expected,
				),
			))
		}
	}

	return results, nil
}

// hasVersionVarDirective reports whether a template interpolates the project
// version through a var directive.
func hasVersionVarDirective(template string) (bool, error) {
	parsed, err := directives.ParseDirectives(template, nil)
	if err != nil {
		return false, err
	}
	for _, directive := range parsed {
		if directive.Name == "var" && directive.Attrs["key"] == "project.version" {
			return true, nil
		}
	}
	return false, nil
}

// checkRootClaudeLocation reports a CLAUDE.md at the repository root that an
// earlier selfdoc generated (generated-claude-md-at-repository-root): selfdoc
// generates it at .claude/CLAUDE.md now, and Claude Code would load both.
func checkRootClaudeLocation(dirPath string) []lints.LintResult {
	if !gen.HasGeneratedHeader(filepath.Join(dirPath, gen.PreviousClaudeOutputRel)) {
		return nil
	}
	return []lints.LintResult{lints.MustLintResult(
		gen.PreviousClaudeOutputRel, nil, "generated-claude-md-at-repository-root",
		gen.PreviousClaudeOutputMessage(),
	)}
}
