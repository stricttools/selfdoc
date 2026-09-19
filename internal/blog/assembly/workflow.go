package assembly

import (
	"strings"

	"github.com/stricttools/selfdoc/internal/blog/site"
)

// WorkflowGoVersion is the Go toolchain the generated workflow sets up before
// it installs the selfdoc binary.
//
// A major.minor line rather than a patch: setup-go resolves it to the newest
// patch, which is what a fresh install of a Go program wants. The pinned thing
// is the program, not the compiler that builds it.
const WorkflowGoVersion = "1.26"

// WorkflowPythonVersion is the Python the generated workflow sets up before it
// installs pagefind, which is published as a Python distribution carrying a
// bundled binary.
const WorkflowPythonVersion = "3.12"

// workflowTemplate is the deploy workflow with every project-specific value
// left as a @@TOKEN@@ for [GenerateWorkflowYAML] to replace.
//
// The workflow is deliberately thin: checkout, install the pinned toolchain,
// invoke "selfdoc assembly integrate", deploy. Every decision the deploy makes
// (version detection, subtree replacement, artifact filtering, membership
// bookkeeping, the push retry loop and search indexing) is in that command,
// where it is importable and testable, instead of in embedded shell and inline
// interpreter snippets that only ever ran in CI.
const workflowTemplate = `name: Assembly Deploy

on:
  repository_dispatch:
    types: [project-updated]

permissions:
  contents: write

concurrency:
  group: assembly-deploy
  cancel-in-progress: false
  queue: max

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: "@@GO_VERSION@@"

      - uses: actions/setup-python@v5
        with:
          python-version: "@@PYTHON_VERSION@@"

      - name: Install tools
        run: |
          go install @@GO_MODULE@@@v@@SELFDOC_VERSION@@
          pip install 'pagefind[bin]==@@PAGEFIND_VERSION@@'

      - name: Clone source project
        if: github.event.client_payload.scope != 'shared-only'
        uses: actions/checkout@v4
        with:
          repository: ${{ github.event.client_payload.repo }}
          ref: ${{ github.event.client_payload.ref }}
          path: source/${{ github.event.client_payload.slug }}
          fetch-depth: 1

      - name: Integrate the project into the assembly
        run: >
          selfdoc assembly integrate
          --slug '${{ github.event.client_payload.slug }}'
          --version '${{ github.event.client_payload.version }}'
          --ref '${{ github.event.client_payload.ref }}'
          --source-repo '${{ github.event.client_payload.repo }}'
          --scope '${{ github.event.client_payload.scope }}'
          --canonical-base '@@CANONICAL_BASE@@'

      - name: Deploy to Cloudflare Pages
        run: npx wrangler pages deploy site/ --project-name '@@PAGES_PROJECT@@'
        env:
          CLOUDFLARE_ACCOUNT_ID: ${{ secrets.CF_ACCOUNT_ID }}
          CLOUDFLARE_API_TOKEN: ${{ secrets.CF_PAGES_API_TOKEN }}
`

// GenerateWorkflowYAML returns the GitHub Actions workflow YAML for assembly
// deployment.
//
// Every deploy-target value is templated from the project's selfdoc.json --
// nothing about the destination is baked into selfdoc itself.
//
// pagesProject is the Cloudflare Pages project the assembled site deploys to,
// from assembly.pages_project. Required.
//
// canonicalBase is the absolute canonical base URL of the assembly site, from
// topology.docs_base. Required.
//
// pins are the versions the install step names. Required and complete: this
// function renders pins, it never resolves them, so it reads neither the
// environment nor the network. [ResolveToolchainPins] does the resolving and
// [CheckPinsArePublished] refuses a pin nobody can install.
func GenerateWorkflowYAML(
	pagesProject string,
	canonicalBase string,
	pins ToolchainPins,
) (string, error) {
	if pagesProject == "" {
		return "", errorf(
			"GenerateWorkflowYAML requires a Cloudflare Pages project " +
				"(assembly.pages_project); there is no default.",
		)
	}
	if canonicalBase == "" {
		return "", errorf(
			"GenerateWorkflowYAML requires a canonical base URL " +
				"(topology.docs_base); there is no default.",
		)
	}
	if err := pins.Validate(); err != nil {
		return "", err
	}
	canonicalBase = strings.TrimRight(canonicalBase, "/")

	replacer := strings.NewReplacer(
		"@@GO_VERSION@@", WorkflowGoVersion,
		"@@PYTHON_VERSION@@", WorkflowPythonVersion,
		"@@GO_MODULE@@", GoModulePath,
		"@@PAGES_PROJECT@@", pagesProject,
		"@@CANONICAL_BASE@@", canonicalBase,
		"@@SELFDOC_VERSION@@", pins.Selfdoc,
		"@@PAGEFIND_VERSION@@", pins.Pagefind,
	)
	return replacer.Replace(workflowTemplate), nil
}

// GitignoreContent returns a .gitignore suitable for a CI-only assembly repo.
func GitignoreContent() string {
	return "node_modules/\n.wrangler/\ndist/\nsource/\n*.log\n"
}

// AssemblyInit returns filename -> content for a new assembly repository.
//
// pagesProject is the Cloudflare Pages project the workflow deploys to.
// canonicalBase is the absolute canonical base URL of the assembly site.
// pins are the toolchain versions the generated workflow installs.
func AssemblyInit(
	pagesProject string,
	canonicalBase string,
	pins ToolchainPins,
) (map[string]string, error) {
	workflow, err := GenerateWorkflowYAML(
		pagesProject, canonicalBase, pins,
	)
	if err != nil {
		return nil, err
	}
	projects, err := site.RenderProjectsJSON(map[string]any{})
	if err != nil {
		return nil, err
	}
	return map[string]string{
		site.WorkflowPath: workflow,
		".gitignore":      GitignoreContent(),
		site.RosterPath:   site.RenderRoster(nil, ""),
		site.ProjectsPath: projects,
	}, nil
}
