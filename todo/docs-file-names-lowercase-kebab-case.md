# Enforce lowercase kebab-case file names under .stricttools/docs/

## Context

Every page and template selfdoc reads lives under a project's
`.stricttools/docs/`. The names follow no enforced rule today. Most pages are
lowercase kebab-case (`check-guide.md`, `gen-index.md`), but across the family
there are also:

- the `_README.md` and `_CLAUDE.md` templates in nearly every project;
- `DESIGN.md` in strictspec and testuniverse;
- underscore-prefixed lowercase files such as go-toml-edit's `_plan.md` and
  deeper ones such as `_ts-port-spec.md` and `_effects-contract.md`.

A page's file name becomes its URL path, so mixed casing produces URLs like
`/DESIGN/` beside `/check-guide/`, and on case-insensitive file systems two
names differing only in case collide.

## Problem

Nothing stops a new page from being named `Design.md`, `design_notes.md`, or
`DesignNotes.md`. Each spelling is a different URL, links to it are easy to get
wrong, and agents creating pages pick names inconsistently.

## Solutions

### A. Lowercase kebab-case everywhere, templates included (recommended)

`selfdoc check` refuses any `.md` file under `.stricttools/docs/` whose name,
after an optional leading underscore, is not lowercase kebab-case
(`^_?[a-z0-9]+(-[a-z0-9]+)*\.md$`). The underscore keeps its current meaning
(a source that is not published as a page). The README and CLAUDE templates
become `_readme.md` and `_claude.md`; their outputs keep the conventional
`README.md` and `CLAUDE.md` names, which live outside `.stricttools/docs/`.
The refusal names the file and the conforming name, and a migration renames
existing files and rewrites links to them.

- Pro: one rule with no exceptions; URLs are uniform.
- Con: every project's two templates are renamed in one sweep, plus the links
  and any tooling that names them (rlsbl's scaffold, selfdoc's own init).

### B. Pages only, templates exempt

The rule applies to published pages; `_README.md` and `_CLAUDE.md` stay as they
are by name.

- Pro: no template renames.
- Con: a named exception list inside the rule.

### C. Warning instead of refusal

- Pro: nothing breaks.
- Con: warnings are ignored; the inconsistency persists.

## Affected files

- The check that validates the docs tree (a new code beside the existing layout
  checks), with a red-green test per refusal and a test that performs the
  rename the refusal suggests.
- `selfdoc init` and the templates it writes, so new projects start conforming.
- Link resolution and URL generation, if any code assumes the uppercase
  template names.
- The docs page describing the docs layout.
- Every family project's `.stricttools/docs/` for the rename, done through a
  migration command rather than by hand.

## Effort

Solution A: about a day for the check, init, and migration, plus a sweep across
the family's projects. Solution B: half a day.
