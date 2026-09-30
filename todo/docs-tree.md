# The docs tree

A site's docs become one tree of typed nodes, from the site down to inline
elements. Every ordering, grouping, link, directive, and generated page is a
node in that tree. This replaces `nav_order`, `nav_group`, the six directive
markers, committed generated pages, the `seeded` marker, and the ownership
bookkeeping around them.

Every decision below is the owner's ruling. Implement them as stated; anything
they do not settle is an open question for the owner, never a builder's choice.

## Why

- Generated pages stored an absolute `nav_order`, so adding one module
  renumbered every later page: 192 of 205 files in one rlsbl commit changed only
  that line.
- Since commit 14b325ca those numbers mix with hand-written pages' numbers at
  the docs root, so every consumer's sidebar is scrambled (rlsbl's guides sit
  after its generated pages). `nav_group` does nothing at the root.
- Generated pages mix generated content with authored descriptions in one file,
  which needs the `seeded` marker, `internal/ownership`, read-only pages that
  agents unlock to edit, and regeneration commits (306 in rlsbl's history).
- The directive markers render as literal text on GitHub and cannot nest, hold
  several sections, or carry inline content.
- The injected changelog silently replaces rlsbl's hand-written `changelog.md`
  guide.

The investigations behind these rulings, with prototypes and measurements, are
in `experiments/nav-order-churn/` (including `ordering/`),
`experiments/directive-syntax/`, and `experiments/derived-reference/`.

## Rulings

### The tree

- A site's structure is declared in one tree file per site.
- Every node has a kind. Kinds exist from the site down to inline elements
  (site, page, heading, list, table, paragraph, link, and so on); a kind
  constrains only what it names, so ordinary pages stay free while generated
  pages are checked exactly.
- A node orders its children either by an automatic rule or by an explicit
  list. The automatic rules are a closed set: `alphabetic` (by child ID),
  `source` (the order the generator declares, for example a CLI's command order,
  or the package tree with the root package first), and `newest-first` (by
  date, for posts). An explicit list must name every child: a child missing from
  it, or a name in it with no child, is a hard error.
- Inside an authored page, its content's order is its document order, checked
  against its kind.
- A group is a page whose body is optional: any page may have child pages,
  ordered by a rule or an explicit list, and a page without a body exists only
  in the tree file. URLs follow the tree (`rules/`, then `rules/deps-unused/`).
- Generated pages attach through generator nodes declared in the tree file where
  the author wants them (for example a node of kind `module-reference`); the
  generator supplies its children, ordered by `source`.
- Any two nodes claiming one address is a hard error naming both (this ends the
  silent changelog replacement).
- The cross-site supertree (a root whose children are every project's site and
  the posts) is designed into the node and link formats now, and built after
  the single-site version.

### Kinds

- strictspec defines the format of a kind declaration and of the tree file,
  validated like its other shared schemas.
- selfdoc ships the built-in kinds.
- A project declares its own kinds in its tree file: parameters, where the kind
  may appear, and the structure of its output, plus a script that expands it
  (today's `resolve(attrs, config, body)` contract). selfdoc checks the script's
  output against the declared structure. This replaces the `directives` map in
  `selfdoc.json`.

### Tags

- Every node written into Markdown is an HTML-style tag; the tag name is the
  node kind and the attributes are its parameters. Tags replace all six
  directive markers (`:-:`, `:<:`, `:@:`, `:=:`, `:::`, `:>:`), at every level,
  including inline inside a sentence. Block directives become an opening and
  closing tag pair with their content between.
- Link destinations are `<jump id="dep-floors"/>` marks, placeable anywhere. A
  link resolves to a node, and a link to a missing destination is a check
  error.
- Every level-2 heading must carry a jump mark. A jump mark on a level-1 heading
  is refused (the page itself is that address). Deeper headings are optional,
  and a link to an unmarked heading is an error.
- Checks that come with tags:
  - blank lines are required around block tags, so a tag cannot swallow the
    Markdown after it;
  - kind names that are HTML element names are refused, so `var` is renamed
    (for example to `value`);
  - malformed tags are reported, never passed through silently.
- selfdoc parses Markdown with its own tokenizer, which has no HTML-block rules;
  the tag handling must follow CommonMark's raw-HTML and HTML-block rules so
  pages render the same on GitHub (goldmark and pandoc agree on the reference
  behavior; see `experiments/directive-syntax/`).

### Generated reference pages

- Generated reference pages (one per module and per CLI command, and their
  index pages) are fully generated, locked (read-only), and never committed.
  `manifest.json` is generated and never committed either; the assembly, the
  editor, and link checks run generation first.
- Their descriptions are authored and committed in
  `.strictmetadata/docs/descriptions-for-generated-pages.toml`:

  ```toml
  [[description]]
  page = "rlsbl.checks"
  text = "The release checks rlsbl runs before tagging, and how each one refuses."
  describes = "sha256:..."
  ```

  One record per generated page; a duplicate page is refused, and records are
  sorted by `page`. A generated page with no record is an error, and so is a
  record whose page no longer exists.
- `describes` is the hash of the source-derived content (signatures, docstrings,
  a command's flags and help, in a canonical encoding), never the rendered
  Markdown bytes, so a change in how selfdoc renders pages makes nothing stale.
- When that content changes, the description is stale until its author rewrites
  it or confirms it with an explicit command naming the page; either way the new
  hash is recorded. Nothing confirms automatically.
- Hand-written guides keep their descriptions in their own front matter.
- Hand-written material for a reference page lives in authored section files,
  bound to the page in the tree file; the build places each inside the generated
  page, and each carries a `describes` hash exactly like a description, going
  stale the same way.
- Coverage is measured against these records and section files, not committed
  pages.
- The `seeded` marker, `seed_hash`, `internal/ownership`, and the committed
  `.docs-state/pages/` directory go away.

### Carried over

- Generated pages store no positions.
- The API index page drops its "covering N modules" count.
- `manifest.json` drops `last_gen`.
- The three hand-written pages that copied a generated rank (gamehome
  `src-dijkstra-tools-runtime_test.md`, claudestream `claudestream.md` and
  `claudestream-events.md`) are fixed by hand during migration.

### Migration

- Consumers are migrated with scripts that each have a dry run and report their
  counts, never with selfdoc commands. The scripts:
  - write each consumer's tree file from its current sidebar order (so rlsbl's
    home-page contents become its order) with explicit lists where authors
    ordered pages;
  - convert every directive marker to tags (691 uses in 28 repositories;
    `experiments/directive-syntax/survey/migrate/` is a working prototype);
  - insert jump marks on every level-2 heading from the heading's slug;
  - move every generated page's description into the descriptions file with its
    `describes` hash, then remove the committed generated pages;
  - move each consumer's `directives` map into kinds in its tree file.
- Hand-written syntax examples in prose (about 200 lines, mostly selfdoc's own
  directive guide) are rewritten by hand.

## Build order

Each step is committed and tested before the next starts; the owner rules on
anything a step finds that these rulings do not settle.

1. strictspec: the tree-file schema, the kind-declaration schema, and the
   descriptions-file schema, with validators, released so selfdoc can depend on
   them.
2. selfdoc, tags: the tag parser following CommonMark rules, the three tag
   checks, `jump` and link resolution to nodes, replacing the marker parser.
3. selfdoc, the tree: the tree file, kinds, ordering rules and explicit lists,
   pages as groups, generator nodes, address collisions, the sidebar and prev/next
   and search facets built from the tree; `nav_order` and `nav_group` removed.
4. selfdoc, generated pages: derived and locked, never committed; the
   descriptions file with source-derived hashes, staleness, rewrite and confirm;
   authored section files; coverage on records; `manifest.json` uncommitted;
   removal of `seeded`, `seed_hash`, and `internal/ownership`.
5. The migration scripts, run on selfdoc's own docs first.
6. A selfdoc release (it also carries the pending `.strictmetadata/` layout
   move), then the migration run on each consumer.

## Related todos to reconcile

Several todos here overlap this redesign (for example
`directive-descriptor-unification.md`, `directive-name-first-class-type.md`,
`front-matter-description-seeding-source.md`,
`front-matter-descriptions-never-heal.md`, `group-pages-skip-nested-subgroups.md`,
`generated-output-not-reconciled-against-generation-set.md`). Each is either
superseded by this one, absorbed into a build step, or still independent; say
which, with the reason, before building.
