# The "Built with selfdoc" footer still links to the old repository name

## Context

The module moved to `github.com/stricttools/selfdoc` (the 0.43.0 release), and
every install line, doc-link comment, proxy pin, and raw-script address moved
with it. One user-visible string did not: the footer every rendered page and
post carries, `Built with <a href="https://github.com/smm-h/selfdoc">selfdoc</a>`.
It is a hard-coded literal in two places, and the rendered-output fixtures
embed it dozens of times.

## Problem

The link still resolves only because GitHub redirects a renamed repository,
and that redirect lasts only until the old name is reused. Every site selfdoc
builds advertises an address the project no longer owns. Two literals state
one fact, and the fixtures state it again in every rendered page.

## Affected files

- `internal/blog/shared/pages.go` — the post footer literal.
- `internal/page/wrap.go` — the page footer literal.
- `internal/e2e/fixture_test.go` (`GeneratorLink`) and
  `internal/page/corpus_test.go` (`opts.Repo`) — test inputs naming the same
  address.
- `internal/page/testdata/*.txt` and `internal/blog/shared/testdata/*.html` —
  rendered fixtures embedding the footer; there is no update flag that
  regenerates them, so each changes by hand or by a one-off script.
- `demo/index.html` — a committed rendered demo carrying the footer.

## Solutions

1. **One authority, derived.** Replace both literals with one exported
   constant next to `GoModulePath` in `internal/blog/assembly/pins.go` (or a
   new `internal/identity` value), so the repository address is stated once
   and the footer, the proxy pin, and the raw-script address all read it; then
   rewrite the fixtures with a dry-run-capable script that asserts the
   occurrence count. Pros: the fact lives in one place, and the next rename
   is one edit. Cons: the fixture rewrite touches many files at once.
2. **Edit the two literals and the fixtures, nothing else.** Smallest diff.
   Cons: leaves two copies of the address and the same drift next time.
3. **Give the rendered-output tests an update mode** (an environment-free
   flag such as `go test ./internal/page -run Corpus -args -update`, written
   to the fixtures only when passed), then change the literal once and
   regenerate. Pros: every future fixture change stops being hand work.
   Cons: a new test surface that must refuse to run in CI.

The most correct choice regardless of effort is 1 combined with 3.

## Effort

Small for the literals; the fixture rewrite is mechanical (a count-asserted
script). Half a day including the update mode.
