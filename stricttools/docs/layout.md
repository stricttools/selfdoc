+++
title = "The stricttools/ layout"
description = "Where selfdoc keeps a repository's state: one directory of function-named directories, generated ones behind a dot, a manifest inside each naming its owner, a derived ignore file, and the commands that inspect, check and migrate it."
nav_group = "Guides"
nav_order = 4
+++

# The stricttools/ layout

Every directory selfdoc owns in a repository lives under one directory at the
repository root, `stricttools/`, and every directory under it is named for its
FUNCTION rather than for the tool that writes it. Another tool's state sits
beside selfdoc's, under its own function name, in the same directory. No
directory under `stricttools/` is named for a tool.

## The directories selfdoc claims

| Directory | Side | Commitment | What it holds |
| --------- | ---- | ---------- | ------------- |
| `stricttools/docs/` | handwritten | committed | The pages you write, the underscore-prefixed templates they include, and the docs configuration that sits beside them (`projects.toml`, `cv.toml`, `custom.css`). |
| `stricttools/.docs-state/` | generated | committed | What selfdoc generates and the repository keeps: `manifest.json`, `post-manifest.json`, `revisions.json`, `hashes/hashes.json`, `data/` and the generated pages under `pages/`. |
| `stricttools/.docs-cache/` | generated | uncommitted | What selfdoc generates and the repository throws away: the built site at `build/` and one extracted checkout per archived version at `versions/`. |
| `stricttools/posts/` | handwritten | committed | The project's blog posts. |
| `stricttools/vocabulary/` | handwritten | committed | The project's accepted and rejected vocabulary (`terms.toml`) and the words awaiting review (`review.toml`), read by the spell check. See [the check guide](../check-guide/). |

The same table, as the data a fleet check reads, comes from the tool itself:

```bash
selfdoc layout dump
```

It prints one object per directory -- its name and path, its side, its
commitment, a description, the manifest that grants it and the exact content
that manifest must hold, and the paths it replaced -- so a description of the
layout is generated from one source rather than restated per repository.

### Names: a generated directory starts with a dot

A directory you write carries its function name as it is; a directory selfdoc
generates carries it behind a leading dot. A listing of `stricttools/` then
shows the directories a person edits and hides the ones a tool rewrites. The
dot is derived from the side the declaration states, never typed beside it, and
`selfdoc layout validate` refuses a directory selfdoc owns whose dot disagrees
with its side, naming the rename that fixes it.

The dot means that at the top level of `stricttools/` only. Deeper down it means
nothing, so nothing inside selfdoc's committed directories starts with one. A
directory another tool owns is that tool's to name: selfdoc holds it to the
manifest rule below and judges nothing else about it.

### Side: handwritten or generated, never both

A directory is entirely handwritten or entirely generated. A page selfdoc
generates carries a marker comment under its frontmatter, and that marker is
what tells the two apart on disk: a marked page in a handwritten directory, or
an unmarked one in a generated directory, is a defect `selfdoc layout validate`
reports with the move to make.

### Two docs roots, one set of addresses

The pages you write and the pages selfdoc generates live in different
directories and publish into one URL namespace: a page's address comes from its
path relative to whichever root it sits in, so `stricttools/docs/guide.md` and
`stricttools/.docs-state/pages/internal-build.md` publish at `/guide/` and
`/internal-build/`. Two pages that would take the same address are refused by
the build, with both files named -- there is no rule about which one wins.

A handwritten page also owns its NAME: `selfdoc gen` generates no page for a
module whose page you have written yourself.

## Ownership: one owner per directory

Each directory under `stricttools/` carries a `manifest.toml` that names the
tool that owns it. The file is one line long:

```toml
owner = "selfdoc"
```

That line is the permission to write. selfdoc writes into a directory under
`stricttools/` only when that directory's manifest names selfdoc as its owner,
and refuses with the exact file and the exact line to write when it does not.
Granting the permission is the repository's own act, so only the adopting and
moving commands write a manifest: `selfdoc init`, which a repository runs to
adopt selfdoc and which writes the manifests of `stricttools/docs/`,
`stricttools/.docs-state/`, `stricttools/.docs-cache/` and
`stricttools/vocabulary/` (keeping one that already names selfdoc, and refusing
one that names another tool); `selfdoc layout migrate`, which carries the
manifests of the directories it moves and writes the vocabulary directory's
when the repository had none, the previous manifests naming selfdoc being the
grant; and the move script for the older `.selfdoc/` layout, which writes the
manifests of the directories it moves content into.

A manifest is also what makes a directory exist. git carries no empty
directory, so a directory whose content has not been written yet -- a project
that has no vocabulary files yet, say -- exists in the repository because its
manifest does.

`owner` is the whole of what a manifest declares. The file is validated against
a schema, so an unknown key is refused rather than silently ignored, and the
named owner has to be a tool this machine has: either `selfdoc`, or a name that
resolves to an executable on `PATH`.

Reading and writing are open to anyone. The owner is what validates.

## The derived ignore file

`stricttools/.gitignore` keeps the uncommitted directories out of the
repository. It is derived from the commitment each tool declares rather than
written by hand, and selfdoc owns only the block between its two marker
comments:

```gitignore
# BEGIN selfdoc -- derived from selfdoc's layout declaration
.docs-cache/*
!.docs-cache/manifest.toml
# END selfdoc
```

An uncommitted directory contributes two lines rather than one. Its contents are
ignored and its manifest is not: the permission travels with the repository
while the contents do not, so a fresh checkout -- including the one a
multi-version build extracts out of a git tag -- does not have to be granted
again.

Every other line belongs to whoever wrote it and is left alone, so several tools
write their own blocks into one file. `selfdoc build` rewrites selfdoc's block
when it is out of date; commit the result.

Its name starts with a dot because git reads it under no other name, not because
it is generated.

## Checking a repository

```bash
selfdoc layout validate
```

It answers for the repository it runs in: every directory under
`stricttools/` carries a `manifest.toml` naming a tool this machine has, and
every directory selfdoc claims names selfdoc; every directory selfdoc owns
starts with a dot exactly when it is generated, and holds only what its side
allows; nothing inside selfdoc's committed directories starts with a dot; and
the derived ignore file carries what the commitment declarations render. Each
problem names its remedy. With `--json` it publishes the same answer as a
payload, which is what a fleet-wide check reads.

## Moving a repository off `.stricttools/`

The layout before this one kept the same directories under a hidden
`.stricttools/` root, each under its bare function name. selfdoc reads that
layout no more: every command refuses a repository that keeps a directory whose
manifest names selfdoc under `.stricttools/` and none under `stricttools/`,
naming the one command that moves it:

```bash
selfdoc layout migrate --dry-run
selfdoc layout migrate
```

The dry run prints the plan, one path per line, and changes nothing. The move
creates `stricttools/` -- the manifests naming selfdoc are the grant -- and then:

- moves `.stricttools/docs`, `posts` and `vocabulary` to `stricttools/` under
  the same names, and `.stricttools/docs-state` and `docs-cache` to
  `stricttools/.docs-state` and `stricttools/.docs-cache`;
- writes the derived ignore file for the new names, and removes selfdoc's block
  from `.stricttools/.gitignore`, deleting that file, and `.stricttools/`
  itself, when nothing else is left in them;
- rewrites every `selfdoc.json` value naming a moved path (`docs`, `output`,
  `root_files`, a custom directive's script) and the header line of every
  generated root file;
- writes an empty `stricttools/vocabulary/terms.toml` when the project has none;
- converts the manifests (see below);
- commits the whole move, unless `--no-auto-commit` is passed.

Only selfdoc's directories move: another tool's directory under `.stricttools/`
stays where it is, with its lines of the ignore file. A repository already on
this layout with its manifests converted, one part-way through a move
(selfdoc's directories under both roots, with the `git mv` commands that put
them back), and one that never used the previous layout are each refused with
what to do instead.

### Converting the manifests

A project's manifests -- `stricttools/.docs-state/manifest.json`, and the
posts-only `post-manifest.json` beside it -- record the project's vocabulary:
every word `stricttools/vocabulary/terms.toml` accepts with its aliases, and
every pattern it rejects with its kind. That is manifest `schema_version` 2.
A manifest an older selfdoc wrote is on `schema_version` 1 and records none,
and every selfdoc command that reads one refuses it, naming
`selfdoc layout migrate`: reading it would pass off "no vocabulary recorded" as
"a project that accepts and rejects nothing".

`selfdoc layout migrate` converts each manifest on `schema_version` 1 to 2,
keeping every field it carried and adding the vocabulary of the project's terms
file, in the same commit as the move. A repository already on `stricttools/`
whose manifests are on `schema_version` 1 -- one moved from `.selfdoc/`, or by
an earlier selfdoc -- gets the conversion alone, as its own commit:

```bash
selfdoc layout migrate --dry-run
selfdoc layout migrate
```

## Moving a repository from the `.selfdoc/` layout

Two layouts ago, selfdoc kept its state in a `.selfdoc/` directory and its pages
in `docs/`. A repository still carrying a `.selfdoc/` directory, or declaring a
`docs`, `output` or `posts.dir` path outside `stricttools/`, is refused by every
command that reads project state, with the move named. There is no migrator
inside selfdoc for this move and no dual reading: a script writes the current
layout directly, in one step.

The refusal prints the whole ordered procedure, prerequisites included, so the
chain below is never discovered one refusal at a time.

### Before the move

The move holds its own result to the URL set the site publishes today, and that
set comes from a build made with the last release that reads the old layout.
Three things have to be true before that build runs:

1. `selfdoc.json` declares a `versions` array and a `locales` array. The build
   refuses a config carrying neither.
2. No document is still on the retired `---` frontmatter block -- the generated
   pages included. The build refuses each one, and the converter leaves a
   generated page alone unless `--include-generated` tells it to take it:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/stricttools/selfdoc/main/scripts/convert-frontmatter-to-toml.py -o convert-frontmatter-to-toml.py
   python3 convert-frontmatter-to-toml.py --include-generated --dry-run
   python3 convert-frontmatter-to-toml.py --include-generated --apply
   ```

3. The pre-flip build has run, writing the sitemap the move compares against:

   ```bash
   go run github.com/smm-h/selfdoc@v0.41.0 build
   ```

Then create `stricttools/` itself. It is the repository's own grant of
permission: the script writes each directory's `manifest.toml` inside it and
refuses while the directory is absent.

### The move

A repository is moved once, by hand. The move script lives in selfdoc's own
repository and no release artifact carries it, so a repository fetches it first
-- which is what the refusal prints:

```bash
curl -fsSL https://raw.githubusercontent.com/stricttools/selfdoc/main/scripts/move-to-stricttools-layout.py -o move-to-stricttools-layout.py
python3 move-to-stricttools-layout.py --dry-run
python3 move-to-stricttools-layout.py --apply
```

The dry run prints every manifest it would write, every move and every content
rewrite, and changes nothing. The apply run writes the manifests and moves the
tracked files as one commit -- a page carrying the generated marker into
`stricttools/.docs-state/pages/`, every other page into `stricttools/docs/` --
rewrites the paths the moved content names as a second commit, runs
`selfdoc layout migrate` to convert the moved manifests (a third commit; see
"Converting the manifests" above), and then builds the site and refuses to
finish unless the URL set is identical to the one the last build before the move
published. The selfdoc the script runs must therefore be one whose
`layout migrate` converts manifests. Page addresses
come from the path relative to the docs root, so the move keeps every URL, and
the comparison is what proves it.

The dry run also prints the manifests it will write inside `stricttools/`.
Those manifests are part of the move's first commit, so the permission is
committed alongside the files it permits.

The script also resolves the selfdoc binary that verifying build runs before it
writes anything -- `--selfdoc` when it names one, otherwise `selfdoc` on `PATH`,
otherwise `bin/selfdoc` in the project -- and refuses with the repository
untouched when there is none.

### What the config declares afterwards

```json
{
  "docs": "stricttools/docs/",
  "output": "stricttools/.docs-cache/build/"
}
```

Both keys still name a directory, and both have to name one inside
`stricttools/`. A project that declares neither gets these.

### A version tagged before the move

A multi-version build extracts each archived version's docs out of its git tag.
A tag made before the move carries the old layout, and no build can read it, so
the archived pages of such a version stay as they were last built. Versions
tagged after the move build from their tags as before.
