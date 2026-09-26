+++
title = "Quality Guide"
description = "How selfdoc quality scores your project's documentation maturity across five tiers and assigns a content grade based on doc-to-source ratio."
nav_group = "Guides"
nav_order = 18
+++

# Quality Guide

`selfdoc quality` measures how mature your project's documentation setup is. It produces two independent scores: a **tier** (0--5) that tracks which selfdoc features you have adopted, and a **content grade** (A--F) that measures how much documentation you have relative to your source code.

```bash
selfdoc quality
```

## Tiers

Tiers are progressive milestones. Each tier requires everything from the tier below it plus one additional capability. The `compute_tier` function walks the list top-down and returns the highest tier whose requirements are met.

| Tier | Name | Requirement |
|------|------|-------------|
| 0 | None | No markdown documentation at all |
| 1 | Basic | At least one `.md` file exists in the project |
| 2 | Selfdoc | `selfdoc.json` is present and parseable |
| 3 | Templates | Root files are auto-generated (`.stricttools/docs/_README.md` configured in `root_files`) |
| 4 | Directives | At least one `:-:`, `:<:`, or `:>:` directive appears in `.stricttools/docs/` templates |
| 5 | Advanced | Custom directives are defined in `selfdoc.json`, or blog posts are configured |

Each tier after 0 builds on the previous one. A project cannot reach tier 4 without first having `selfdoc.json` (tier 2) and a root file template (tier 3).

### What each tier means in practice

- **Tier 0** -- The project has no markdown files. Start by creating a `README.md`.
- **Tier 1** -- Documentation exists but is entirely manual. Running `selfdoc init` will move you to tier 2.
- **Tier 2** -- selfdoc is configured but not yet generating root files. Add `.stricttools/docs/_README.md` to the `root_files` array in `selfdoc.json` and run `selfdoc gen`.
- **Tier 3** -- Root files are auto-generated, but docs are not connected to source code. Add directives like `:-: ref path="mypackage" lang="python"` to your templates.
- **Tier 4** -- Directives link documentation to source code. Define custom directives in `selfdoc.json` or configure blog posts to reach tier 5.
- **Tier 5** -- All selfdoc features are in use.

## Doc ratio

The doc ratio measures documentation volume relative to source code volume. It is computed as:

```
doc_ratio = doc_loc / source_loc
```

Where:

- **doc_loc** is the total line count of all `.md` files in the project, excluding `CHANGELOG.md`, files inside `todo/`, and auto-generated root file templates listed in `root_files`.
- **source_loc** is `code_loc - test_loc`: total lines in source files minus lines in test files.
- **code_loc** comes from `dirstat scan`, counting lines across all recognized code file extensions (`.py`, `.go`, `.ts`, `.js`, `.rs`, `.c`, `.cpp`, `.java`, `.rb`, `.sh`, and many others).
- **test_loc** is subtracted so that large test suites do not inflate source LOC and deflate the ratio. Test files are identified by directory name (`tests/`, `test/`, `__tests__/`, `testing/`), file naming conventions (`test_*.py`, `*_test.go`, `*.test.ts`, `*.spec.js`, `conftest.py`), and similar patterns.

Submodule paths (from `.gitmodules`) are excluded from both code and doc counts.

Directories like `.git`, `node_modules`, `.venv`, `__pycache__`, `vendor`, `dist`, `build`, and other build/cache directories are skipped during traversal.

## Content grade

The content grade maps the doc ratio to a letter:

| Grade | Doc ratio | Interpretation |
|-------|-----------|----------------|
| A | >= 30% | Thorough documentation |
| B | 15%--29% | Good coverage |
| C | 5%--14% | Moderate coverage |
| D | 1%--4% | Minimal documentation |
| F | < 1% | Nearly undocumented |
| - | n/a | No source code to compare against |

The grade is independent of the tier. A project at tier 5 with very little prose still gets a low grade. A project at tier 1 with extensive markdown can get an A.

## Example output

Running `selfdoc quality` on a project produces output like this:

```
myproject -- Tier 4 / 5 (Directives)

1,842 source LOC | 310 test LOC | 287 doc LOC (15.6%) | 8 files | Grade: B

Selfdoc:
  Auto-generated README    yes
  Auto-generated CLAUDE    yes
  Custom directives        -
  Blog posts               no
  Directive uses           12

Completed:
  Tier 1 -- Has markdown documentation
  Tier 2 -- selfdoc.json configured
  Tier 3 -- Auto-generated root files (README/CLAUDE)
  Tier 4 -- Directives connect docs to source code

To do:
  Tier 5 -- Define custom directives or configure blog posts
```

### Reading the output

The first line shows the project name, current tier, and tier name.

The metrics line shows:

- **source LOC** -- lines of production code (total code minus tests)
- **test LOC** -- lines of test code (shown only if nonzero)
- **doc LOC** -- lines of markdown documentation, with the percentage relative to source LOC
- **files** -- number of markdown files counted
- **Grade** -- the content grade letter

The Selfdoc section shows which selfdoc features are active. A dash (`-`) means the feature is not configured.

The Completed and To do sections list which tiers have been reached and what remains. The next actionable step is always the first item under "To do".

## Machine output

Pass `--json` for machine-readable output:

```bash
selfdoc quality --json
```

Stdout then carries exactly one document -- the strictcli envelope -- and the score is its `payload` member. The score object includes all fields: `tier`, `tier_name`, `code_loc`, `test_loc`, `source_loc`, `doc_loc`, `doc_files`, `doc_ratio`, `content_grade`, `selfdoc` (with feature flags), and `next_step`.

## Using quality to find improvement areas

1. **Low tier, any grade** -- Focus on tier progression. Follow the `next_step` suggestion in the output. Each tier unlocks a selfdoc capability.
2. **High tier, low grade** -- The tooling is set up but documentation is thin. Write more prose in your `.stricttools/docs/` templates. Add explanatory pages, guides, and examples.
3. **Grade D or F** -- The project has very little documentation relative to its size. Prioritize a getting-started guide and API reference pages.
4. **Tier 3 but no directives** -- Templates exist but are not connected to source code. Adding `:-: ref` directives ensures documentation stays in sync with the codebase.

## Prerequisites

`selfdoc quality` requires [dirstat](https://github.com/smm-h/dirstat) to count source lines. If dirstat is not installed, the command exits with an error and prints the install command:

```
go install github.com/smm-h/dirstat/cmd/dirstat@latest
```
