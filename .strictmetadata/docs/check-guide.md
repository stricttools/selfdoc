+++
title = "Check Guide"
description = "Run selfdoc check to validate directives, measure two-tier coverage and read what a skeleton-only symbol really means, execute marked examples, spell-check page prose, lint blog posts alongside documentation pages, apply every registered lint rule at the value its option sets, never above its declared severity, and do all of it without writing anything."
nav_group = "Guides"
nav_order = 10
+++

# Check Guide

`selfdoc check` is your documentation linter. It validates every directive in your templates, measures how much of your public API is documented, runs SEO best-practice checks, and detects stale descriptions. Run it locally before pushing, or wire it into CI for automated enforcement.

## What It Does

A single `selfdoc check` run performs three categories of analysis that together cover directive correctness, API documentation coverage, and SEO best practices. Each category produces structured output with file paths, line numbers, and actionable messages:

1. **Directive validation** -- resolves every directive marker in your `.strictmetadata/docs/` templates and reports whether each one succeeds or fails.
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
    .strictmetadata/.docs-state/pages/mypackage-core.md
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

Every `selfdoc check` invocation runs the whole lint registry: SEO and page structure, description staleness and source drift, cross-references and symbol documentation, example validation, CLI reference completeness, version consistency, blog posts, and unified sites. Each rule has a unique name, a severity, and an actionable message explaining what is wrong and how to fix it. Errors cause a non-zero exit; warnings are informational. Each name and its severity are declared once, in the lint registry embedded in the binary, and the table below is rendered from it. A repository changes how a lint runs through its option; see [Lint options](#lint-options).

:-: table-lints

### Spelling (unknown-word)

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
`.strictmetadata/vocabulary/terms.toml` accepts it, in any casing, and reads no
file outside the repository: the same committed docs get the same verdict on
every machine.

unknown-word is error severity. Fixing the prose or accepting the term are the
answers it expects, which is the point: a misspelling on a published page is a
defect, and the vocabulary records the deliberate decision that a word is not
one. Each finding names the command
that accepts the word.

`selfdoc spell-corpus` runs the same engine over every selfdoc project sitting
beside this one, each against its own vocabulary, and prints each project's
unknown words with a first location. It is strictly read-only over the
projects it visits.

### The vocabulary

`.strictmetadata/vocabulary/terms.toml` holds the words the project's pages may use
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

A word of selfdoc's built-in baseline is changed only in selfdoc itself, for
every project, never by a project. So `selfdoc vocabulary reject` refuses a
pattern that covers any baseline word, listing every one it covers: narrow the
pattern by rejecting the specific words meant instead, one entry each (for
example `selfdoc vocabulary reject youngish --kind word --reason "<why>"` in
place of the suffix `ish`, which would also reject the baseline's `treeish`). A
pattern that covers a word the project itself accepts is refused with the
command that removes that word.

`.strictmetadata/vocabulary/review.toml` holds words proposed for acceptance that
nobody has reviewed yet, each with a guessed meaning, a confidence from 0 to 1,
and the doc lines the guess came from:

```toml
format_version = 1

[[pending]]
word = "frobnitz"
meaning = "The widget the release pipeline turns."
confidence = 0.8
evidence = [".strictmetadata/docs/index.md:12: The frobnitz turns once per release."]
```

A pending word is not accepted: the spell check reports it like any unknown
word, and the finding names the commands that resolve it:

```bash
selfdoc vocabulary approve <word>                        # accept it with the proposed meaning
selfdoc vocabulary approve <word> --meaning "<corrected>"  # accept it with a corrected one
selfdoc vocabulary drop <word>                           # delete the proposal
```

The vocabulary lints hold the file to its purpose, each naming the command that
clears it: an accepted word no page uses (unused-accepted-word), an entry accepted or
rejected twice (duplicate-vocabulary-entry), an accepted word a rejected suffix or prefix covers
(accepted-word-matches-rejected-pattern, resolved by narrowing the project's pattern when the word is the
baseline's, and by removing the word or narrowing the pattern when it is the
project's), a page whose prose uses a rejected term (rejected-term-in-prose), and an array out
of order (unsorted-vocabulary-entries).

### Lint options

Each lint is an option: `selfdoc:<lint name>`, filed in the repository's
`.strictmetadata/options/docs.toml`. It is how a repository changes how a lint
runs, and every entry carries its reason, so how a repository
departs from selfdoc's defaults is visible in one place. An error lint runs at
`error > warn > off`, a warning lint at `warn > off`, and each runs at its
registered severity until an entry says otherwise. No entry can raise a lint
above its registered severity.

| Value | What the check does |
| ----- | ------------------- |
| `error` | Reports the lint as an error, which fails the run. |
| `warn` | Reports the lint as a warning, which never fails the run. |
| `off` | Does not report the lint. |

An entry names the value the repository runs today (`current`) and the value
it should run (`ideal`), and `selfdoc options set` writes it:

```bash
selfdoc options set selfdoc:low-numeric-data-density --current off --ideal off --reason "reference pages list no quantities"
```

```toml
format_version = 1

[[entry]]
id = "selfdoc:low-numeric-data-density"
current = "off"
ideal = "off"
reason = "reference pages list no quantities"
```

The command creates `.strictmetadata/options/` and its `manifest.toml`, which
names `strictspec` as the owner, when they are absent, updates the entry
already there for the same option, and keeps every other line of the file.
strictspec validates the result before anything is written. An entry ranking
`current` below `ideal` is debt the repository owes; strictcode reports it,
selfdoc does not.

`selfdoc check` and `selfdoc build` read the entries before any work is done.
Every document in the directory is held to strictspec's entry schema, and the
`selfdoc:` entries to selfdoc's registry, so a value the option does not
declare, an entry in another subject file, a scope, an entry equal to the
default, a repeated entry, or a `current` ranked above its `ideal` stops the
run with strictspec's diagnostic, naming the file and the fix. Another tool's
entries are held to the schema and nothing else.

`selfdoc options registry` prints the registry every selfdoc option is declared
in: the TOML document selfdoc ships, in the shape of strictspec's built-in
options-registry schema, with each option's subject, ranked values, default,
scope, and description. A tool that reads every tool's options, such as
strictcode, learns selfdoc's rankings by running it. It needs no selfdoc
project, and with `--json` the same declarations are the payload.

## Staleness Detection

selfdoc tracks SHA-256 hashes of each page's raw template body (directives unresolved) and its frontmatter description. When the content changes but the description stays the same, it raises a stale-page-description error. This catches the common case where you update a page's content but forget to revise the description that feeds into meta tags and search results.

Hashes are stored in `.strictmetadata/.docs-state/hashes/hashes.json`. `selfdoc check` only compares against them and writes nothing; `selfdoc gen` records and commits them.

## Example Validation

Every Python and JSON code block is parsed during `selfdoc check`, and a block that does not parse raises `code-block-syntax-error`. Parsing proves only that a snippet is well-formed, not that it still works: an example calling a function you renamed six months ago parses perfectly and is completely wrong. To catch that class, mark the block `validate` and configure a validator for its language:

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

selfdoc writes each marked block to a scratch file suffixed for its language, substitutes the path for `{file}`, and runs the command from the project root with a 60-second timeout. A non-zero exit becomes an `code-block-validation-failed` error naming the exit code and the last five lines of the validator's output. A marked block whose language has no configured command becomes an `code-block-validator-not-configured` error rather than being skipped, so an unhonored marker can never masquerade as a passing one.

The marker is opt-in per block: unmarked blocks are never executed and keep the `code-block-syntax-error` syntax check exactly as before. Validators run without a sandbox, so configure commands that compile, type-check, and register rather than ones that execute arbitrary payloads.

## Output Formats

### Text (default)

Plain-text output with status indicators, file paths, line numbers, and lint names, one finding per line, with no ANSI escapes. This is the default format:

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
  "interface_version": 3,
  "app": "selfdoc",
  "command": "check",
  "exit_code": 0,
  "payload": {
    "directives": [{"file": "index.md", "line": 12, "status": "OK", ...}],
    "coverage": {"total_public": 23, "referenced": 15, ...},
    "lints": [{"name": "missing-frontmatter-description", "severity": "error", ...}],
    "exit_code": 0
  },
  "output": null,
  ...
}
```

The payload's shape is declared as a JSON Schema on the command itself and validated before it is written, so a document that deviates fails the run instead of reaching a consumer. `selfdoc help --json` prints the help document, which publishes the declaration.

## Exit Codes

`selfdoc check` uses standard exit codes to signal pass or fail, making it safe to use as a CI gate or pre-commit hook. It exits with code 0 when everything passes, and code 1 when any of these conditions are true:

- A directive resolution failed
- Any lint has severity `error`
- Coverage is below the `coverage_threshold` fraction

Warnings alone do not cause a non-zero exit. This makes it safe to use in CI -- warnings are informational, errors block the pipeline.

> [!TIP]
> Run `selfdoc check --dry-run` to see staleness results without writing hash files to disk. Useful for previewing what would change.

Next: [rlsbl Integration](../rlsbl-integration/)
