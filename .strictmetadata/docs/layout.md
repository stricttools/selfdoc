+++
title = "The .strictmetadata/ layout"
description = "Where selfdoc keeps a repository's state: one hidden directory of function-named directories, generated ones behind a dot, a manifest inside each naming its owner, shared directories strictspec owns, a strictcli program's schema and test-coverage directories, the ignore file each uncommitted directory carries, and the commands that inspect, check and migrate it, the migration also converting plain-string root_files entries."
nav_group = "Guides"
nav_order = 4
+++

# The .strictmetadata/ layout

Every directory selfdoc owns in a repository lives under one hidden directory at
the repository root, `.strictmetadata/`, and every directory under it is named for its
FUNCTION rather than for the tool that writes it. Another tool's state sits
beside selfdoc's, under its own function name, in the same directory. No
directory under `.strictmetadata/` is named for a tool. The root starts with a
dot as `.git` does: a tool that makes a project commit its files hides them, so
the project's own top level stays tidy.

## The directories selfdoc claims

| Directory | Side | Commitment | What it holds |
| --------- | ---- | ---------- | ------------- |
| `.strictmetadata/docs/` | handwritten | committed | The pages you write, the underscore-prefixed templates they include, and the docs configuration that sits beside them (`projects.toml`, `cv.toml`, `custom.css`). |
| `.strictmetadata/.docs-state/` | generated | committed | What selfdoc generates and the repository keeps: `manifest.json`, `post-manifest.json`, `revisions.json`, `hashes/hashes.json`, `data/` and the generated pages under `pages/`. |
| `.strictmetadata/.docs-cache/` | generated | uncommitted | What selfdoc generates and the repository throws away: the built site at `build/` and one extracted checkout per archived version at `versions/`. |
| `.strictmetadata/posts/` | handwritten | committed | The project's blog posts. |
| `.strictmetadata/vocabulary/` | handwritten | committed | The project's accepted and rejected vocabulary (`terms.toml`) and the words awaiting review (`review.toml`), read by the spell check. See [the check guide](../check-guide/). |

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
generates carries it behind a leading dot. A listing of `.strictmetadata/` then
shows the directories a person edits and hides the ones a tool rewrites. The
dot is derived from the side the declaration states, never typed beside it, and
`selfdoc layout validate` refuses a directory selfdoc owns whose dot disagrees
with its side, naming the rename that fixes it.

The dot means that at the top level of `.strictmetadata/` only. Deeper down it means
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
path relative to whichever root it sits in, so `.strictmetadata/docs/guide.md` and
`.strictmetadata/.docs-state/pages/internal-build.md` publish at `/guide/` and
`/internal-build/`. Two pages that would take the same address are refused by
the build, with both files named -- there is no rule about which one wins.

A handwritten page also owns its NAME: `selfdoc gen` generates no page for a
module whose page you have written yourself.

## Ownership: one owner per directory

Each directory under `.strictmetadata/` carries a `manifest.toml` that names the
tool that owns it. The file is one line long:

```toml
owner = "selfdoc"
```

That line is the permission to write. selfdoc writes into a directory under
`.strictmetadata/` only when that directory's manifest names selfdoc as its owner,
and refuses with the exact file and the exact line to write when it does not.
Granting the permission is the repository's own act, so only the adopting and
moving commands write a manifest: `selfdoc init`, which a repository runs to
adopt selfdoc and which writes the manifests of `.strictmetadata/docs/`,
`.strictmetadata/.docs-state/`, `.strictmetadata/.docs-cache/` and
`.strictmetadata/vocabulary/` (keeping one that already names selfdoc, and refusing
one that names another tool); `selfdoc layout migrate`, which carries the
manifests of the directories it moves and writes the vocabulary directory's
when the repository had none, the previous manifests naming selfdoc being the
grant, and writes the vocabulary directory's for a repository already on
`.strictmetadata/` that has none (what the move from the older `.selfdoc/` layout
leaves); and the move script for the older `.selfdoc/` layout, which writes the
manifests of the directories it moves content into.

A manifest is also what makes a directory exist. git carries no empty
directory, so a directory whose content has not been written yet -- a project
that has no vocabulary files yet, say -- exists in the repository because its
manifest does.

`owner` is the whole of what a manifest declares. The file is validated against
a schema, so an unknown key is refused rather than silently ignored, and the
named owner has to be one this machine has: `selfdoc`, `strictspec`,
`strictcli`, or a name that resolves to an executable on `PATH`.

### Directories several tools share

A directory several tools read and write names `strictspec` as its owner:
`.strictmetadata/options/`, a repository's option entries (see
[the check guide](../check-guide/)), and `.strictmetadata/upstream/`, a fork's
declared upstream. strictspec holds the schemas those directories are validated
against, and it is a library every reading tool links rather than a command, so
`selfdoc layout validate` accepts such a directory without looking for a
`strictspec` executable on `PATH`. Each tool writes only its own entries there.

### A strictcli program's directories

`.strictmetadata/.cli-schema/` holds the program's help document
(`schema.json`, the output of `<app> help --json`), and
`.strictmetadata/.cli-test-coverage/` holds its committed test-coverage manifest
(`manifest.json`) and the uncommitted per-process shards (`shards/`). Their
manifests name `strictcli`, a library its programs link rather than a command,
so `selfdoc layout validate` accepts them without looking for a `strictcli`
executable on `PATH`.

Reading and writing are open to anyone. The owner is what validates.

## Ignore files

`.strictmetadata/` holds directories only, with `go.mod` -- the stub module that
keeps the tree out of a Go module's zip -- as the one file beside them. A
directory that needs ignore rules carries them in its own `.gitignore`. Each
uncommitted directory selfdoc owns gets one derived from its declaration:

```gitignore
# Written by selfdoc from its layout declaration: what this directory holds
# is not committed; its manifest and this file are.
*
!.gitignore
!manifest.toml
```

Its contents are ignored and its manifest is not: the permission travels with
the repository while the contents do not, so a fresh checkout -- including the
one a multi-version build extracts out of a git tag -- does not have to be
granted again. `selfdoc build` rewrites the file when it is out of date; commit
the result.

## Checking a repository

```bash
selfdoc layout validate
```

It answers for the repository it runs in: every directory under
`.strictmetadata/` carries a `manifest.toml` naming an owner this machine has, and
every directory selfdoc claims names selfdoc; every directory selfdoc owns
starts with a dot exactly when it is generated, and holds only what its side
allows; nothing inside selfdoc's committed directories starts with a dot; every
uncommitted directory selfdoc owns carries the `.gitignore` its declaration
derives; and `.strictmetadata/` holds directories and `go.mod` only. Each
problem names its remedy. With `--json` it publishes the same answer as a
payload, which is what a fleet-wide check reads.

## Moving a repository off `stricttools/` or `.stricttools/`

The layout before this one kept the same directories, under the same names,
under a visible `stricttools/` root. The layout before that kept them under a
hidden `.stricttools/` root, each under its bare function name, generated ones
included. selfdoc reads neither any more: every command refuses a repository
that keeps a directory whose manifest names selfdoc under either root and none
under `.strictmetadata/`, naming the root and the one command that moves it:

```bash
selfdoc layout migrate --dry-run
selfdoc layout migrate
```

The dry run prints the plan, one path per line, and changes nothing. The move
creates `.strictmetadata/` -- the manifests naming selfdoc are the grant -- and
then:

- moves `stricttools/docs`, `.docs-state`, `.docs-cache`, `posts` and
  `vocabulary` to `.strictmetadata/` under the same names; from `.stricttools/`
  it moves `docs`, `posts` and `vocabulary` under the same names, and
  `docs-state` and `docs-cache` to `.strictmetadata/.docs-state` and
  `.strictmetadata/.docs-cache`;
- writes the `.gitignore` of each uncommitted directory, and removes selfdoc's
  block from the previous root's `.gitignore`, deleting that file, and the
  previous root itself, when nothing else is left in them;
- rewrites every `selfdoc.json` value naming a moved path (`docs`, `output`,
  `root_files`, a custom directive's script) and the header line of every
  generated root file;
- moves a `CLAUDE.md` an earlier selfdoc generated at the repository root to
  `.claude/CLAUDE.md` (see below);
- converts every `root_files` entry that is a plain template string into the
  object naming its template and the output selfdoc generated from it, keeping
  the rest of `selfdoc.json` byte for byte (see [Root Files](../root-files/));
- writes an empty `.strictmetadata/vocabulary/terms.toml` when the project has none;
- moves strictcli's files and converts the ignore file (see below);
- converts the manifests (see below);
- commits the whole move, unless `--no-auto-commit` is passed.

Only selfdoc's directories move: another tool's directory under the previous
root stays where it is, with its lines of the ignore file. A repository already
on this layout with its manifests converted, its ignore files converted, no
`.strictcli/` left, and no generated `CLAUDE.md` at its root, one part-way through a move (selfdoc's directories under a previous root
and `.strictmetadata/`, with the `git mv` commands that put them back), one
keeping selfdoc's directories under both previous roots, and one that never
used a previous layout are each refused with what to do instead.

### Moving strictcli's files and converting the ignore file

On any repository, a previous layout or this one, the move also takes care of
two things an earlier layout left behind:

- **`.strictcli/` directories.** Every `.strictcli/` directory in the tree moves
  into the `.strictmetadata/` beside it: `schema.json` to
  `.strictmetadata/.cli-schema/schema.json`, `test-coverage.json` to
  `.strictmetadata/.cli-test-coverage/manifest.json`, and the shard files under
  `coverage/` to `.strictmetadata/.cli-test-coverage/shards/`. Each directory
  that receives a file gets a `manifest.toml` naming `strictcli`, and
  `.cli-test-coverage/` gets the `.gitignore` that keeps `shards/` out of the
  repository. The stub `go.mod` is deleted (the one at `.strictmetadata/go.mod`
  covers the new place), an empty `coverage/` is dropped, and the emptied
  `.strictcli/` is removed. For the repository root's `.strictcli/`, the move
  also deletes `.rlsbl/bases/.strictcli/go.mod`, drops `.strictcli/go.mod` from
  `.rlsbl/managed-files.json`, and removes the root `.gitignore`'s `coverage/`
  line. A file in `.strictcli/` that is none of these, or one git does not
  track, is refused by name, and so is a file whose new place is taken.
- **The ignore file at the top of `.strictmetadata/`.** An earlier selfdoc
  derived `.strictmetadata/.gitignore`; the move deletes it and writes the
  `.gitignore` of each uncommitted directory instead. A file still holding
  another tool's lines besides selfdoc's block is refused, listing them, so
  they can be moved into the directory they are for.

### Moving the generated CLAUDE.md under `.claude/`

The `_CLAUDE.md` template generates `.claude/CLAUDE.md`, which Claude Code loads
as it loads a `CLAUDE.md` at the project root (see
[Root Files](../root-files/)). An earlier selfdoc generated it at the repository
root. Such a file -- one whose first line is selfdoc's generated-file header --
is refused by `selfdoc gen` and reported by `selfdoc check` as
`generated-claude-md-at-repository-root`, both naming the move, and
`selfdoc layout migrate` moves it to `.claude/CLAUDE.md` and commits: in the
same commit as the directories for a repository on a previous layout, and on its
own for one already on `.strictmetadata/`. The dry run prints it as
`move CLAUDE.md -> .claude/CLAUDE.md`. The move is refused when
`.claude/CLAUDE.md` exists already, naming the `git rm` of whichever of the two
is not current, and when git ignores `.claude/CLAUDE.md`, naming the ignore file,
its line, and a narrower rule. A hand-written root `CLAUDE.md` is not selfdoc's,
and stays where it is.

### Converting the manifests

A project's manifests -- `.strictmetadata/.docs-state/manifest.json`, and the
posts-only `post-manifest.json` beside it -- record the project's vocabulary:
every word `.strictmetadata/vocabulary/terms.toml` accepts with its aliases, and
every pattern it rejects with its kind. That is manifest `schema_version` 2.
A manifest an older selfdoc wrote is on `schema_version` 1 and records none,
and every selfdoc command that reads one refuses it, naming
`selfdoc layout migrate`: reading it would pass off "no vocabulary recorded" as
"a project that accepts and rejects nothing".

`selfdoc layout migrate` converts each manifest on `schema_version` 1 to 2,
keeping every field it carried and adding the vocabulary of the project's terms
file, in the same commit as the move. A repository already on `.strictmetadata/`
whose manifests are on `schema_version` 1 -- one moved from `.selfdoc/` -- gets
the conversion alone, as its own commit. One whose
`.strictmetadata/vocabulary/` carries no `manifest.toml` -- one moved from
`.selfdoc/`, a layout that had no vocabulary directory -- also gets that
directory's grant, and `.strictmetadata/vocabulary/terms.toml` empty when the
project has none, in the same commit:

```bash
selfdoc layout migrate --dry-run
selfdoc layout migrate
```

## Moving a repository from the `.selfdoc/` layout

Three layouts ago, selfdoc kept its state in a `.selfdoc/` directory and its pages
in `docs/`. A repository still carrying a `.selfdoc/` directory, or declaring a
`docs`, `output` or `posts.dir` path outside `.strictmetadata/`, is refused by every
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

Then create `.strictmetadata/` itself. It is the repository's own grant of
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
`.strictmetadata/.docs-state/pages/`, every other page into `.strictmetadata/docs/` --
rewrites the paths the moved content names as a second commit, removes the
directories the move emptied under `.selfdoc/` and `.selfdoc/` itself (a file
still inside is named and left, and the script refuses), runs
`selfdoc layout migrate` to convert the moved manifests and write the
vocabulary directory the old layout never had (a third commit; see "Converting
the manifests" above), and then builds the site and refuses to finish unless the
URL set is identical to the one the last build before the move published. The
selfdoc the script runs must therefore be one whose `layout migrate` converts
manifests and writes a missing vocabulary directory. Page addresses
come from the path relative to the docs root, so the move keeps every URL, and
the comparison is what proves it.

The dry run also prints the manifests it will write inside `.strictmetadata/`.
Those manifests are part of the move's first commit, so the permission is
committed alongside the files it permits.

The script also resolves the selfdoc binary that verifying build runs before it
writes anything -- `--selfdoc` when it names one, otherwise `selfdoc` on `PATH`,
otherwise `bin/selfdoc` in the project -- and refuses with the repository
untouched when there is none.

### What the config declares afterwards

```json
{
  "docs": ".strictmetadata/docs/",
  "output": ".strictmetadata/.docs-cache/build/"
}
```

Both keys still name a directory, and both have to name one inside
`.strictmetadata/`. A project that declares neither gets these.

### A version tagged before the move

A multi-version build extracts each archived version's docs out of its git tag.
A tag made before the move carries the old layout, and no build can read it, so
the archived pages of such a version stay as they were last built. Versions
tagged after the move build from their tags as before.
