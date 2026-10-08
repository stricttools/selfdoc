+++
title = "Staleness Detection"
description = "How selfdoc detects stale frontmatter descriptions by hashing page content, which commands record the baseline that the read-only selfdoc check compares against, the two alternative ways to clear a stale-page-description or description-drifted-from-source finding, and how to accept a reviewed dead-end."
nav_group = "Guides"
nav_order = 18
+++

# Staleness Detection

You update a page's content but forget to update the frontmatter `description`. Now your meta tags, OG cards, and search snippets show outdated text. selfdoc catches this automatically by tracking content hashes.

## The Problem

Frontmatter descriptions feed into `<meta name="description">`, Open Graph tags, Twitter cards, search index summaries, and the Atom feed. When the page body changes significantly but the description stays the same, those external-facing representations go stale. Users see one thing in search results and something different on the page.

This is easy to miss during normal editing. You change the content, the page looks fine, and you move on -- never noticing the description no longer matches.

## How It Works

selfdoc maintains a hash store at `.strictmetadata/.docs-state/hashes/hashes.json` that tracks each page's content and description independently. By comparing current hashes against stored baselines on every `selfdoc check` run, it detects when content has drifted from its description. `selfdoc check` only compares: it writes nothing and commits nothing. The baseline is recorded by `selfdoc gen`, which every release runs before `selfdoc check`, by `selfdoc build`, and by `selfdoc baseline accept`. For each documentation page with a frontmatter description, it tracks two SHA-256 hashes:

- **Content hash** -- computed from the page's raw template body: frontmatter stripped, directives left unresolved, and each directive marker's attribute values canonicalized. A directive whose output changes (a version bump, a renamed symbol) therefore does not trip staleness, and neither does a mechanical `path="x"` -> `path="y"` rename
- **Description hash** -- computed from the frontmatter `description` string

On each run of `selfdoc check`, the current hashes are compared against the stored ones. The logic is straightforward:

| Content changed? | Description changed? | Result |
| --- | --- | --- |
| No | No | OK -- nothing changed |
| No | Yes | OK -- description updated independently |
| Yes | Yes | OK -- both updated together |
| Yes | No | **stale-page-description** -- content changed but description did not |

Only the last case triggers a staleness error.

## The stale-page-description Error

When staleness is detected, `selfdoc check` reports an error-level diagnostic with the affected filename and a clear message explaining that the content changed but the description did not. This is classified as an error (not a warning) because stale descriptions degrade search results, social cards, and AI summaries:

```
stale-page-description [error] getting-started.md: content changed but frontmatter
description was not updated (possible stale description)
```

This is an error, not a warning -- it causes `selfdoc check` to exit with code 1. If you have checks in CI or in an rlsbl pre-checks hook, stale descriptions block the pipeline.

## Fixing It

Update the frontmatter `description` to reflect the current page content. A changed description clears the error on the next `selfdoc check`, and the next `selfdoc gen` records the new baseline hashes. Aim for 110-160 characters that accurately summarize what the page covers after the content change:

```markdown
+++
title = "Getting Started"
description = "Install selfdoc, initialize a project, and build your first documentation site in under five minutes."
+++
```

Then run `selfdoc check` again: the error is gone.

## Accepting a Reviewed Dead-End

Sometimes the content changed but the existing description is still accurate -- a generated page whose listing order shifted, or a prose correction that does not change what the page is about. Use `selfdoc baseline accept` to advance the baseline:

```
selfdoc baseline accept en/index.md en/cli-index.md
```

`selfdoc baseline accept <page> [<page>...]` is a deliberate, auditable action that means "reviewed: the content changed, and the existing frontmatter description is still accurate." It advances each named page's baseline to the current content and description hashes -- exactly as if the description had been rewritten -- so the next `selfdoc check` passes without touching the description.

Acceptance is intentionally per-page and unforgiving:

- Name each page explicitly, exactly as it appears in `selfdoc check` output (e.g. `en/cli-index.md`). There is no `--all`, no glob, and no `--force`.
- Accepting a page that does not exist, has no recorded baseline, or is not currently reporting stale-page-description or description-drifted-from-source is a hard error -- "nothing to accept" never silently succeeds.
- The two courses are alternatives, not steps. Editing the description clears the finding on its own, so a page whose description was just rewritten is already cleared and accepting it afterwards is the "nothing to accept" error. Accept is for the other course: the description was reviewed against the change and deliberately left as it is.
- The same guardrails apply to description-drifted-from-source (source-docstring and CLI-schema drift); accepting advances every tracked hash for the page.

Like `selfdoc gen`, the command commits the updated `.strictmetadata/.docs-state/hashes/hashes.json` by default; pass `--no-auto-commit` to leave the change for a larger manual commit.

## Hash Storage

Hashes are stored in `.strictmetadata/.docs-state/hashes/hashes.json`, a JSON file mapping each page path to its content and description SHA-256 hashes. The file is written atomically -- to a temporary file, then renamed over the old one -- so an interrupted run cannot leave a half-written store. Commit it: it is the baseline for future comparisons.

```json
{
  "getting-started.md": {
    "content": "a1b2c3d4...",
    "description": "e5f6g7h8..."
  }
}
```

Without it, every page looks new and no staleness is detected.

### Store schema version

The store carries a `_hash_version` field. When the meaning of the stored hashes changes, the version is bumped and any older store is discarded wholesale and re-baselined on the next `selfdoc gen` (nothing is silently reused). Two bumps so far:

- **v1 -> v2** switched the content hash from resolved output to the raw template body, so directive output changes (like a version bump) no longer trip staleness.
- **v2 -> v3** added a per-page `seed_hash` and canonicalizes directive marker lines before hashing, so a pure `path="x"` -> `path="y"` rename no longer changes the content hash.

Because a bump discards the old store, run `selfdoc gen` once after upgrading to re-record the baseline. (Releases run `selfdoc gen` then `selfdoc check`, so this happens automatically.)

### Description ownership (`seed_hash`)

Descriptions are handwritten; machine-emitted text is only ever a placeholder. The store records a per-page `seed_hash` -- the SHA-256 of the description text `selfdoc gen` last emitted for that page -- so selfdoc can tell a machine placeholder apart from a hand edit:

```json
{
  "gen-index.md": {
    "content": "a1b2c3d4...",
    "description": "e5f6g7h8...",
    "seed_hash": "9a8b7c6d..."
  }
}
```

Only `selfdoc gen` writes `seed_hash`. The `content` and `description` hashes are recorded by every command that writes the store -- `selfdoc gen` and `selfdoc build` -- and each of them holds the baseline of a page with an outstanding stale-page-description. `selfdoc gen` also measures the drift hashes (`source_docstring` and `schema_hash`) as `selfdoc check` measures them, and holds the baseline of a page with an outstanding description-drifted-from-source, so the store it leaves reports what the next `selfdoc check` reports. A description certifies the drift hashes stored beside it, so a command that records a changed description pairs it with the drift hashes it measured: `selfdoc gen` records the current ones, while `selfdoc build` drops them, and the next `selfdoc gen` records them again without a finding. An edited description therefore clears description-drifted-from-source whichever of these commands runs first after the edit. Each writer merges rather than overwriting, so none of them clobbers a field it does not write.

This is what lets `selfdoc gen` safely regenerate: a description is reseeded only when it is machine-owned (it matches the recorded `seed_hash` or a known machine template), and a description you rewrote by hand is preserved -- even if a stale `seeded: true` marker was left in the frontmatter. The same predicate drives the stale-page-description/description-drifted-from-source exemption: only genuinely machine-generated descriptions are exempt from the staleness hold, so a generated page you describe by hand is checked like any other page.

## A Read-Only Check

`selfdoc check` computes all hashes, compares them against the stored baselines, and reports any stale or drifted pages, and it never writes `.strictmetadata/.docs-state/hashes/hashes.json` or commits anything. Run it as often as you like; the working tree is the same afterwards. The store changes only when a command that generates content (`selfdoc gen`, `selfdoc build`) or `selfdoc baseline accept` writes it, and those commit it unless you pass `--no-auto-commit`.

> [!TIP]
> New pages (ones not yet in the hash store) never trigger stale-page-description. Staleness is only detected once their initial hashes are recorded. Run `selfdoc gen` after adding new pages to establish the baseline.

Next: [Glossary](../glossary-terms/)
