+++
title = "Check Guide"
description = "Run selfdoc check to validate directives, measure two-tier coverage and read what a skeleton-only symbol really means, execute marked examples, spell-check page prose, lint blog posts alongside documentation pages, and apply every registered lint rule with its declared severity -- suppressing warnings only, never errors."
nav_group = "Guides"
nav_order = 10
+++

# Check Guide

`selfdoc check` is your documentation linter. It validates every directive in your templates, measures how much of your public API is documented, runs SEO best-practice checks, and detects stale descriptions. Run it locally before pushing, or wire it into CI for automated enforcement.

## What It Does

A single `selfdoc check` run performs three categories of analysis that together cover directive correctness, API documentation coverage, and SEO best practices. Each category produces structured output with file paths, line numbers, and actionable messages:

1. **Directive validation** -- resolves every directive marker in your `stricttools/docs/` templates and reports whether each one succeeds or fails.
2. **Coverage analysis** -- counts public/exported symbols in your source code and checks how many are referenced by directives.
3. **SEO linting** -- scans templates for heading structure, meta description, alt text, contrast ratio, and other best practices.

```bash
selfdoc check
```

## Directive Validation

Every directive in your docs is resolved against the actual source code. If a module path is wrong, a target symbol does not exist, or a custom directive script throws an error, the check reports it with file and line number:

```
Directives
  index.md:12  ref path="mypackage"        OK
  api.md:8     ref path="mypackage.core"    OK
  api.md:20    table-schema path="bad.path" FAILED: Module not found
```

Fix failures by correcting the directive's `path` or `target` attribute to match your actual source code.

## Coverage

Coverage measures how much of your public API surface is documented. selfdoc walks your `source` directories, extracts public symbols using each entry's language extractor, and checks whether each symbol appears in the resolved content of a directive.

```
Coverage: 15/23 symbols documented (65%)
          18/23 symbols referenced (78%)
Unreferenced symbols:
  mypackage/core.py: helper_function, InternalConfig
Skeleton-only symbols:
  Each is named only on a generated page whose frontmatter still declares
  seeded = true, so that page's description is the one selfdoc emitted and
  nobody has rewritten. The symbols' own doc comments are not the cause and
  rewriting them changes nothing here: edit each page's frontmatter
  description instead.
  Pages whose description to edit:
    stricttools/.docs-state/pages/mypackage-core.md
  Symbols they leave undocumented:
  mypackage/core.py: Pipeline
```

A symbol is **referenced** when a directive on any page names it, and **documented** when a directive on a page that is not a bare generated skeleton names it. A page is a skeleton when its frontmatter declares both `generated = true` and `seeded = true`: its description is the placeholder `selfdoc gen` emitted and nobody has rewritten.

So a skeleton-only symbol is never a statement about that symbol's doc comment -- the comment can be complete and the symbol still lands here. The cause is the page, and the fix is on the page: rewrite its frontmatter description in your own words. `selfdoc gen` keeps a description it did not write, so the edit stays.

### Coverage threshold

Set `coverage_threshold` in your `selfdoc.json` to the **fraction** of public symbols that must be documented. It is a number between `0.0` and `1.0`, and it defaults to `1.0` -- full coverage. Below the threshold, `selfdoc check` prints the shortfall and exits with code 1, which is what makes it usable as a CI check against coverage regressions:

```json
{
  "coverage_threshold": 0.8
}
```

### Excluding modules

Modules listed in `gen.exclude` are excluded from both coverage calculations and auto-generated documentation pages. Use this for intentionally-internal modules, test helpers, or implementation details that should never appear in public-facing documentation. Excluded symbols do not count against your coverage threshold:

```json
{
  "gen": {
    "exclude": ["mypackage._internal", "mypackage.tests"]
  }
}
```

## Lint Rules

Every `selfdoc check` invocation runs the whole lint registry: SEO and page structure, description staleness and source drift, cross-references and symbol documentation, example validation, CLI reference completeness, version consistency, blog posts, and unified sites. Each rule has a unique code, a severity, and an actionable message explaining what is wrong and how to fix it. Errors cause a non-zero exit; warnings are informational. Each code and its severity are declared once, in the lint registry embedded in the binary, and the table below is rendered from it.

:-: table-lints

### Spelling (SPELL001)

Every documentation page and every published post is spell-checked against a
vendored English word list of about 172,000 words -- a pinned snapshot of the
English Speller Database at its large size, carrying US, British and Canadian
spellings, so `colour` and `color` are equally correct. The list ships as
package data beside upstream's copyright notice, which travels with it as
redistribution requires.

Structure comes from the block tokenizer, so fenced code blocks and directive
blocks are never scanned; inline code spans, link destinations, URLs, and
directive markers are blanked before a line is read. Tokens that look like
machinery rather than English -- anything carrying a digit, an underscore, a
slash, a dotted qualified name, or an interior capital such as `parseConfig`
-- are skipped whole. Hyphenated compounds are checked part by part, and a
possessive is accepted from its base word. Each finding names the file, the
line, the column, and an edit-distance-one suggestion when one exists.

Genuine terms the general word list cannot know -- project names, tool names,
technical vocabulary -- belong in the project's vocabulary. The spell check
accepts a word when selfdoc's built-in baseline or the project's own
`stricttools/vocabulary/terms.toml` accepts it, in any casing, and reads no
file outside the repository: the same committed docs get the same verdict on
every machine.

SPELL001 is error severity and cannot be suppressed. Fixing the prose or
accepting the term are the two available answers, which is the point: a
misspelling on a published page is a defect, and the vocabulary records the
deliberate decision that a word is not one. Each finding names the command
that accepts the word.

`selfdoc spell-corpus` runs the same engine over every selfdoc project sitting
beside this one, each against its own vocabulary, and prints each project's
unknown words with a first location. It is strictly read-only over the
projects it visits.

### The vocabulary (VOCAB)

`stricttools/vocabulary/terms.toml` holds the words the project's pages may use
and the terms they may not. Each accepted word carries its meaning; each
rejected term carries how it matches and why it is rejected:

```toml
format_version = 1

[[accepted]]
word = "selfdoc"
meaning = "This project: the code-aware static site generator."
aliases = ["selfdoc's"]

[[rejected]]
pattern = "in order to"
kind = "phrase"
reason = "Say to."
```

A rejected term's `kind` is `word` (a whole word), `phrase` (a whole phrase,
its words separated by any whitespace), `suffix` (the end of a longer word) or
`prefix` (the start of one). Every comparison is case-insensitive. Each array is
kept sorted by word, case-folded. The file is validated against a schema, so a
missing meaning, an unknown kind or an unknown key is refused with the file
named, and a word both accepted and rejected -- in the project's file, or across
it and the baseline -- stops the check before any page is judged.

Edit the file with the vocabulary commands rather than by hand; each one keeps
the file's comments and order and refuses duplicates and conflicts:

```bash
selfdoc vocabulary accept <word> --meaning "<what it means here>"
selfdoc vocabulary reject <pattern> --kind <word|phrase|suffix|prefix> --reason "<why>"
selfdoc vocabulary remove <word>
```

`stricttools/vocabulary/review.toml` holds words proposed for acceptance that
nobody has reviewed yet, each with a guessed meaning, a confidence from 0 to 1,
and the doc lines the guess came from:

```toml
format_version = 1

[[pending]]
word = "frobnitz"
meaning = "The widget the release pipeline turns."
confidence = 0.8
evidence = ["stricttools/docs/index.md:12: The frobnitz turns once per release."]
```

A pending word is not accepted: the spell check reports it like any unknown
word, and the finding names the commands that resolve it:

```bash
selfdoc vocabulary approve <word>                        # accept it with the proposed meaning
selfdoc vocabulary approve <word> --meaning "<corrected>"  # accept it with a corrected one
selfdoc vocabulary drop <word>                           # delete the proposal
```

The VOCAB lints hold the file to its purpose, each naming the command that
clears it: an accepted word no page uses (VOCAB001), an entry accepted or
rejected twice (VOCAB002), an accepted word a rejected suffix or prefix covers
(VOCAB003), a page whose prose uses a rejected term (VOCAB004), and an array out
of order (VOCAB005).

### Suppressing rules

Suppress specific lint rules globally in your config or per invocation via CLI flags. Both sources are merged, so you can set baseline suppressions in config and add per-run overrides as needed. Use suppression sparingly since each rule catches real SEO or accessibility issues:

```json
{
  "lint_ignore": ["SEO007", "SEO008"]
}
```

Or per invocation with `--ignore`:

```bash
selfdoc check --ignore SEO007,SEO008
```

Both sources are combined -- CLI flags and config are merged.

Suppression reaches warning-severity codes only. Naming an error-severity code -- in `lint_ignore` or in `--ignore` -- is a hard error that names the code and its severity, and the run stops before any checking happens. An error says the build is wrong: a broken emitted reference, a missing description, a post whose slug moved. Silencing it hides the defect instead of resolving it, which is how a genuinely broken build once passed its own check. Fix the defect, or change the rule's severity in the registry if the rule itself is wrong.

## Staleness Detection

selfdoc tracks SHA-256 hashes of each page's raw template body (directives unresolved) and its frontmatter description. When the content changes but the description stays the same, it raises a STALE001 error. This catches the common case where you update a page's content but forget to revise the description that feeds into meta tags and search results.

Hashes are stored in `stricttools/.docs-state/hashes/hashes.json` and auto-committed after each check (unless you pass `--no-auto-commit` or `--dry-run`).

## Example Validation

Every Python and JSON code block is parsed during `selfdoc check`, and a block that does not parse raises `EXAMPLE001`. Parsing proves only that a snippet is well-formed, not that it still works: an example calling a function you renamed six months ago parses perfectly and is completely wrong. To catch that class, mark the block `validate` and configure a validator for its language:

````markdown
```python validate
from mylib import greet

print(greet("world"))
```
````

```json
{
  "examples": {
    "python": "uv run --directory python python {file}",
    "go": "scripts/validate-example-go.sh {file}",
    "ts": "scripts/validate-example-ts.sh {file}"
  }
}
```

selfdoc writes each marked block to a scratch file suffixed for its language, substitutes the path for `{file}`, and runs the command from the project root with a 60-second timeout. A non-zero exit becomes an `EXAMPLE002` error naming the exit code and the last five lines of the validator's output. A marked block whose language has no configured command becomes an `EXAMPLE003` error rather than being skipped, so an unhonored marker can never masquerade as a passing one.

The marker is opt-in per block: unmarked blocks are never executed and keep the `EXAMPLE001` syntax check exactly as before. Validators run without a sandbox, so configure commands that compile, type-check, and register rather than ones that execute arbitrary payloads.

## Output Formats

### Text (default)

Human-readable output with colored status indicators, file paths, line numbers, and rule codes. This is the default format designed for local development where you read the output directly in a terminal and fix issues one by one:

```bash
selfdoc check
```

### Machine output

Machine-readable output for CI integration, custom tooling, or programmatic analysis. `--json` is the framework's machine mode: stdout carries exactly one document, the envelope, and the check report is its `payload` member. The report holds the same information as the text output, structured as arrays of objects with consistent field names for easy parsing:

```bash
selfdoc check --json
```

The payload is an object with `directives`, `coverage`, `lints`, and `exit_code` fields. Example structure:

```text
{
  "interface_version": 1,
  "app": "selfdoc",
  "command": "check",
  "exit_code": 0,
  "payload": {
    "directives": [{"file": "index.md", "line": 12, "status": "OK", ...}],
    "coverage": {"total_public": 23, "referenced": 15, ...},
    "lints": [{"code": "SEO006", "severity": "error", ...}],
    "exit_code": 0
  }
}
```

The payload's shape is declared as a JSON Schema on the command itself and validated before it is written, so a document that deviates fails the run instead of reaching a consumer. `selfdoc --dump-schema` publishes the declaration.

## Exit Codes

`selfdoc check` uses standard exit codes to signal pass or fail, making it safe to use as a CI gate or pre-commit hook. It exits with code 0 when everything passes, and code 1 when any of these conditions are true:

- A directive resolution failed
- Any lint has severity `error`
- Coverage is below the `coverage_threshold` fraction

Warnings alone do not cause a non-zero exit. This makes it safe to use in CI -- warnings are informational, errors block the pipeline.

> [!TIP]
> Run `selfdoc check --dry-run` to see staleness results without writing hash files to disk. Useful for previewing what would change.

Next: [rlsbl Integration](../rlsbl-integration/)
