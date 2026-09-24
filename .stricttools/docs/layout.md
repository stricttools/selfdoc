+++
title = "The .stricttools/ layout"
description = "Where selfdoc keeps a repository's state: one hidden directory of function-named directories, a manifest inside each one naming its owner, a derived ignore file, the two layout commands, and the ordered procedure that moves a repository onto the layout."
nav_group = "Guides"
nav_order = 4
+++

# The .stricttools/ layout

Every directory selfdoc owns in a repository lives under one hidden directory at
the repository root, `.stricttools/`, and every directory under it is named for
its FUNCTION rather than for the tool that writes it. Another tool's state sits
beside selfdoc's, under its own function name, in the same directory.

## The directories selfdoc claims

| Directory | Side | Commitment | What it holds |
| --------- | ---- | ---------- | ------------- |
| `.stricttools/docs/` | handwritten | committed | The pages you write, the underscore-prefixed templates they include, and the docs configuration that sits beside them (`projects.toml`, `cv.toml`, `custom.css`). |
| `.stricttools/docs-state/` | generated | committed | What selfdoc generates and the repository keeps: `manifest.json`, `post-manifest.json`, `revisions.json`, `hashes/hashes.json`, `data/` and the generated pages under `pages/`. |
| `.stricttools/docs-cache/` | generated | uncommitted | What selfdoc generates and the repository throws away: the built site at `build/` and one extracted checkout per archived version at `versions/`. |
| `.stricttools/posts/` | handwritten | committed | The project's blog posts. |
| `.stricttools/vocabulary/` | handwritten | committed | The project's accepted and rejected vocabulary. |

The same table, as the data a fleet check reads, comes from the tool itself:

```bash
selfdoc layout dump
```

It prints one object per directory -- its name and path, its side, its
commitment, a description, the manifest that grants it and the exact content
that manifest must hold, and the paths it replaced -- so a description of the
layout is generated from one source rather than restated per repository.

### Side: handwritten or generated, never both

A directory is entirely handwritten or entirely generated. A page selfdoc
generates carries a marker comment under its frontmatter, and that marker is
what tells the two apart on disk: a marked page in a handwritten directory, or
an unmarked one in a generated directory, is a defect `selfdoc layout validate`
reports with the move to make.

### Two docs roots, one set of addresses

The pages you write and the pages selfdoc generates live in different
directories and publish into one URL namespace: a page's address comes from its
path relative to whichever root it sits in, so `.stricttools/docs/guide.md` and
`.stricttools/docs-state/pages/internal-build.md` publish at `/guide/` and
`/internal-build/`. Two pages that would take the same address are refused by
the build, with both files named -- there is no rule about which one wins.

A handwritten page also owns its NAME: `selfdoc gen` generates no page for a
module whose page you have written yourself.

## Ownership: one owner per directory

Each directory under `.stricttools/` carries a `manifest.toml` that names the
tool that owns it. The file is one line long:

```toml
owner = "selfdoc"
```

That line is the permission to write. selfdoc writes into a directory under
`.stricttools/` only when that directory's manifest names selfdoc as its owner,
and refuses with the exact file and the exact line to write when it does not.
Granting the permission is the repository's own act, so only two things write
a manifest: `selfdoc init`, which a repository runs to adopt selfdoc and which
writes the manifests of `.stricttools/docs/`, `.stricttools/docs-state/` and
`.stricttools/docs-cache/` (keeping one that already names selfdoc, and refusing
one that names another tool), and the move script below, which writes the
manifests of the directories it moves content into. No other command writes a
manifest or creates `.stricttools/`.

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

`.stricttools/.gitignore` keeps the uncommitted directories out of the
repository. It is derived from the commitment each tool declares rather than
written by hand, and selfdoc owns only the block between its two marker
comments:

```gitignore
# BEGIN selfdoc -- derived from selfdoc's layout declaration
docs-cache/*
!docs-cache/manifest.toml
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

It is the one entry inside `.stricttools/` allowed to start with a dot.
`.stricttools/` is hidden already, and nothing inside it needs to be.

## Checking a repository

```bash
selfdoc layout validate
```

It answers for the repository it runs in: every directory under
`.stricttools/` carries a `manifest.toml` naming a tool this machine has, and
every directory selfdoc claims names selfdoc; every directory selfdoc owns holds
only what its side allows; nothing inside starts with a dot except the derived
ignore file; and that file carries what the commitment declarations render. Each
problem names its remedy. With `--json` it publishes the same answer as a
payload, which is what a fleet-wide check reads.

## Moving a repository onto the layout

selfdoc reads this layout and no other. A repository still carrying a
`.selfdoc/` directory, or declaring a `docs`, `output` or `posts.dir` path
outside `.stricttools/`, is refused by every command that reads project state,
with the move named. There is no migrator inside selfdoc and no dual reading.

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

Then create `.stricttools/` itself. It is the repository's own grant of
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
`.stricttools/docs-state/pages/`, every other page into `.stricttools/docs/` --
rewrites the paths the moved content names as a second commit, and then builds
the site and refuses to finish unless the URL set
is identical to the one the last build before the move published. Page addresses
come from the path relative to the docs root, so the move keeps every URL, and
the comparison is what proves it.

The dry run also prints the manifests it will write inside `.stricttools/`.
Those manifests are part of the move's first commit, so the permission is
committed alongside the files it permits.

The script also resolves the selfdoc binary that verifying build runs before it
writes anything -- `--selfdoc` when it names one, otherwise `selfdoc` on `PATH`,
otherwise `bin/selfdoc` in the project -- and refuses with the repository
untouched when there is none.

### What the config declares afterwards

```json
{
  "docs": ".stricttools/docs/",
  "output": ".stricttools/docs-cache/build/"
}
```

Both keys still name a directory, and both have to name one inside
`.stricttools/`. A project that declares neither gets these.

### A version tagged before the move

A multi-version build extracts each archived version's docs out of its git tag.
A tag made before the move carries the old layout, and no build can read it, so
the archived pages of such a version stay as they were last built. Versions
tagged after the move build from their tags as before.
