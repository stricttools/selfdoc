# The spell check should read the per-repository vocabulary directory

## Context

`selfdoc layout dump` declares `.stricttools/vocabulary/` as "the project's
accepted and rejected vocabulary, read by the spell check and the glossary",
but `internal/spelling/spelling.go` reads only the machine-wide accept list
at `~/Projects/ark/spelling-accept.txt`. A repository whose published pages
use its own project name in prose (its heading, its first sentence) fails
`SPELL001` on every machine whose accept list lacks the name, and passes on
the one machine where someone added it.

## Problem

`selfdoc check` is a release-blocking check (rlsbl runs it during the
release), and its verdict depends on a file outside the repository under
check. The same committed docs pass on one workstation and fail on another.
The fleet rule "release-blocking checks own their inputs" names this exact
shape.

## Solutions

1. Read an accept list from `.stricttools/vocabulary/` (for example
   `spelling-accept.txt` in the same one-word-per-line format) in addition
   to the machine-wide list, so a project can accept its own vocabulary in
   a committed file. The machine-wide list keeps its role for words shared
   across projects.
2. Read only the per-repository list and retire the machine-wide one,
   migrating each project's needed words into its own file.

Option 1 is the smaller change and keeps the shared list; option 2 makes
the check own its inputs completely, at the cost of a migration across
every project.

## Affected files

- `internal/spelling/spelling.go` (the accept-list resolution)
- `internal/layout/layout.go` (the vocabulary directory declaration is
  already there)
- the check guide page describing the accept list

## Effort

Small for option 1; medium for option 2 because of the migration.
