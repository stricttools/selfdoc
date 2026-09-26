# Spelling accept-list is machine-global; projects need a per-repo list

## Problem

The spell check's accept-list is a single machine-global file (the
shared list under the user's projects root). Consequences:

- A project whose docs legitimately use domain words (tool-specific
  vocabulary, git terms, protocol names) must edit the MACHINE's list to
  get `selfdoc check` green. The acceptance is then invisible to the
  repository: a fresh clone on another machine fails `selfdoc check`
  until someone reconstructs the words, and nothing in the repo records
  which words it depends on.
- The global list has no version history — an accidental deletion or a
  typo'd addition is silent and unrecoverable.
- Two projects' vocabularies pollute each other: a word accepted for one
  project's domain is accepted everywhere.

Observed live: a project's documentation pass needed a dozen domain
words accepted; the only mechanism was the global file, and the project
now carries an ad-hoc copy of the applied list in its scratch directory
as the sole record.

## Solution sketch

A per-repository accept-list file (e.g. `.selfdoc/spelling-accept.txt`
or a key in selfdoc.json), committed with the project, MERGED with the
global list at check time. Project-local words live with the project and
travel with clones; the global list keeps truly machine-wide vocabulary.
Precedence is additive (either list accepts a word); no override
semantics needed.

## Affected

The spell-check directive's accept-list loading; docs for the config
surface.

## Effort

Small: one extra file read and a set union at check time, plus the
config/docs plumbing.
