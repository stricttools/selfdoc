# The docs tree

A site's docs become one tree of nodes, from the site down to inline elements.
Every node has a node type. Every ordering, grouping, link, generated page, and
piece of generated content is a node in that tree. This replaces `nav_order`,
`nav_group`, the six directive markers, committed generated pages, the `seeded`
marker, and the ownership bookkeeping around them.

Every decision below is the owner's ruling. Implement them as stated; anything
they do not settle is an open question for the owner, never a builder's choice.
Open questions are listed at the end.

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
`experiments/directive-syntax/`, `experiments/derived-reference/`, and
`experiments/docs-tree-formats/` (drafted schemas, a built-in node type
catalogue, and complete example trees for selfdoc, rlsbl, and strictcode; the
drafts predate some rulings below, and the rulings win).

## Rulings

### Vocabulary

- Every node has a **node type** (field `type`), following the unist/mdast
  vocabulary for Markdown syntax trees. The word "kind" is not used. A
  parameter's type is its **value type**.
- A node type backed by code is a **component**, as in MDX: its code (built
  into selfdoc or a project script) takes parameters, a body, and the project's
  configuration, may compute anything, and returns content that must fit the
  node type's **content model** (the declared grammar of what a node type may
  contain). One term covers every depth: `<ref/>` and `<value/>` inside a page,
  and the tree-file nodes that produce whole groups of pages (the API and CLI
  reference). A component's output is generated content; everything else is
  authored.
- "Directive", "expander", and "generator node" are not used. The built-in that
  renders the reference table of node types is `table-node-types`.

### The tree file

- One tree file per site, at `.strictmetadata/docs/tree.toml`.
- Nodes are flat `[[node]]` records keyed by their full `path` (for example
  `rules/deps-unused`); the parent is implied by the path, and the order records
  are written in carries no meaning.
- Every node declares its `type`; nothing defaults.
- A node with a body names its Markdown file explicitly (`body = "..."`). Every
  docs Markdown file must be bound to one node; an unbound file is an error.
  Pages can move in the tree without moving files.
- A page with a body owns its title (front matter or level-1 heading, one rule
  for both the sidebar and the page). The tree file carries `title` and
  `description` only for body-less nodes.
- Node IDs (a path's segments) match `[A-Za-z0-9_][A-Za-z0-9._-]*`; two sibling
  paths differing only in letter case are a collision error.
- The site's ID, and the facts `selfdoc.json` held about structure, move into the
  tree file: `topology.slug` becomes the site ID, `gen.exclude` the module
  reference node's `exclude` parameter, `changelog` the changelog node's `file`
  parameter, and `directives` becomes node type declarations with scripts. The
  old keys are refused with the new location named. `source` stays in
  `selfdoc.json` (the `ref` tag reads it too).
- Parameter values in the tree file are TOML strings parsed by the same rules as
  tag attributes, except list parameters, which are TOML arrays of strings.

### Ordering and groups

- A node orders its children with one `order` key holding either a rule or an
  explicit list. The rules are a closed set: `alphabetic` (by child ID),
  `newest-first` (by date), and `source` (the order a component declares, for
  example a CLI's command order or the package tree with the root package
  first). An explicit list must name every child: a missing child or a name
  with no child is a hard error.
- Page-group components always order by `source`; an `order` key on one is refused.
- Inside an authored page, content is in document order, checked against the
  page's node type.
- A group is a page whose body is optional. A body-less group's URL serves a
  generated listing: its title and description from the tree file and its child
  pages with their descriptions, in order.
- URLs follow the tree (`rules/`, then `rules/deps-unused/`). Generated pages
  sit under their component's node with the site's own package prefix dropped
  (`/api/checks/`, not `/api/rlsbl-checks/`; `/cli/check/`). Module pages are
  flat under their component, never nested along the package tree. Every old
  URL gets a permanent redirect to its new one.
- Any two nodes claiming one address is a hard error naming both.
- rlsbl's release history is published as `release-notes`; its hand-written
  `changelog` guide keeps its address. Other projects' release history keeps
  `changelog`.
- The cross-site supertree (a root whose children are every project's site and
  the posts) is designed into the node and link formats now, and built after
  the single-site version. Posts belong only to the supertree.

### Node types

- strictspec defines the format of a node type declaration and of the tree file,
  validated like its other shared schemas, and gains a new error-code area for
  tree and node type rules (shape, ordering lists, declarations), the way
  `OPTIONS` was granted. selfdoc keeps error codes only for what needs a build
  (expansions, links, staleness).
- selfdoc ships the built-in node types. A project declares its own in its tree
  file (parameters, where the node type may appear, its content model) with the
  script that is its component (today's `resolve(attrs, config, body)`
  contract); selfdoc checks the script's output against the content model.
- Node type names are lowercase ASCII kebab-case, `^[a-z][a-z0-9]*(-[a-z0-9]+)*$`,
  for every node type.
- No node type may be named after an HTML element, with no exemption. Node types
  for Markdown constructs use CommonMark/GFM terms: `strong-emphasis` and
  `raw-html`, and `markdown-table`, `markdown-link`, `markdown-image` where the
  spec has no single term.
- A content model is a simple grammar: items matched in order, greedily, with
  no backtracking; a declaration whose content model would need backtracking is
  refused.

### Tags and links

- Every node written into Markdown is an HTML-style tag; the tag name is the
  node type and the attributes are its parameters. Tags replace all six
  directive markers (`:-:`, `:<:`, `:@:`, `:=:`, `:::`, `:>:`) at every level,
  including inline. Block tags are an opening and closing pair with their
  content between. `var` is renamed `value`.
- Checks that come with tags: blank lines are required around block tags;
  malformed tags are reported, never passed through; tag handling follows
  CommonMark's raw-HTML and HTML-block rules, so pages render the same on
  GitHub (selfdoc's own tokenizer has no HTML-block rules today).
- Link destinations are `<jump id="..."/>` marks, placeable anywhere. Every
  level-2 heading must carry one; one on a level-1 heading is refused (the page
  is that address). Two identical jump IDs on one page are an error, including
  when one page expands the same module's reference twice.
- Links are ordinary Markdown links; selfdoc resolves each relative URL to a node
  and its jump mark, and a link to a missing node or mark is an error.
  Cross-site links use the address form `site:path#jump`.
- A reference expansion that cannot be resolved is a hard error naming the page,
  the tag, and what was missing (today it renders a `[selfdoc: ...]` block quote
  and the build continues).
- Root-file templates (`_README.md`, `_CLAUDE.md`) use the same tags and link
  checks, and are not nodes in the tree.

### Generated reference pages and their descriptions

- Generated reference pages (one per module and per CLI command, and their index
  pages) are fully generated, locked, and never committed; so is
  `manifest.json` (the assembly, editor, and link checks generate first). This
  follows the family rule's exception for generated build intermediates.
- Subcommands are sections of their group's page by default; a command gets its
  own page when its strictcli declaration says so (a new field in strictcli's
  command declaration, read by selfdoc).
- Descriptions are authored and committed in
  `.strictmetadata/docs/descriptions-for-generated-pages.toml`:

  ```toml
  [[description]]
  page = "module:rlsbl.checks"
  text = "The release checks rlsbl runs before tagging, and how each one refuses."
  describes = "sha256:..."
  ```

  Page IDs carry their component's prefix: `module:`, `command:`,
  `index:<component node type>` (so a site has at most one component of each
  node type). One record per page; a duplicate is refused; records are sorted by
  `page`. A generated page without a record is an error, and so is a record
  whose page no longer exists. Records hold one language; a site declaring a
  second locale is refused with a message that translated descriptions are not
  supported yet.
- `describes` hashes the source-derived content (signatures, docstrings, a
  command's flags and help, in a canonical encoding), never rendered Markdown.
  The file records which encoding it uses; when selfdoc changes the encoding, a
  migration script re-records a hash only where the source is unchanged under
  the old encoding, and reports the count.
- When the source-derived content changes, the description is stale until its
  author rewrites it or confirms it with an explicit command naming the page;
  either way the new hash is recorded. Nothing confirms automatically.
- Hand-written guides keep their descriptions in their own front matter.
- Hand-written material for a reference page lives in authored section files,
  bound to the page in the tree file and placed in a named slot the generated
  page's node type declares (for example `overview` after the title, `notes` at
  the end; several sections in one slot follow the tree file's order). Each
  section records its own `describes` hash in its front matter and goes stale
  the same way.
- Coverage is measured against these records and section files.
- The `seeded` marker, `seed_hash`, `internal/ownership`, the committed
  `.docs-state/pages/`, and `last_gen` go away. The API index page drops its
  "covering N modules" count.
- A prerequisite: selfdoc's extractors return only rendered Markdown, so a
  structured extraction interface across every extractor must exist before
  `describes` can hash source-derived content.

### Old versions

- Docs for old released versions are no longer published.

### Migration

- Consumers are migrated with scripts that each have a dry run and report their
  counts, never with selfdoc commands. They write each consumer's tree file from
  its current sidebar order, convert every directive marker to tags (691 uses in
  28 repositories; `experiments/directive-syntax/survey/migrate/` is a working
  prototype), insert jump marks on level-2 headings from heading slugs, move
  generated pages' descriptions into the descriptions file with their hashes,
  remove the committed generated pages, write redirects for every moved URL, and
  move `selfdoc.json`'s structural keys into the tree file.
- The three hand-written pages that copied a generated page's rank (gamehome
  `src-dijkstra-tools-runtime_test.md`, claudestream `claudestream.md` and
  `claudestream-events.md`) are fixed by hand; so are the hand-written syntax
  examples in prose (about 200 lines, mostly selfdoc's own directive guide).

## Build order

Each step is committed and tested before the next starts; the owner rules on
anything a step finds that these rulings do not settle.

1. selfdoc: the structured extraction interface across every extractor.
2. strictspec: the tree-file, node-type-declaration, and descriptions-file
   schemas, their validators, and the new error-code area, released.
3. selfdoc, tags: the tag parser following CommonMark rules, its checks, `jump`,
   link resolution, replacing the marker parser.
4. selfdoc, the tree: the tree file, node types, ordering, groups and listings,
   page-group components, addresses, redirects, and the sidebar, prev/next, and search
   built from the tree; `nav_order` and `nav_group` removed.
5. selfdoc, generated pages: derived and locked, never committed; descriptions
   with hashes, staleness, rewrite and confirm; section files in slots;
   coverage; the removals listed above. strictcli gains the own-page field for
   commands.
6. The migration scripts, run on selfdoc's own docs first.
7. A selfdoc release (it also carries the pending `.strictmetadata/` layout
   move), then the migration run on each consumer.

## Open questions

None.

## Related todos to reconcile

Several todos here overlap this redesign (for example
`directive-descriptor-unification.md`, `directive-name-first-class-type.md`,
`front-matter-description-seeding-source.md`,
`front-matter-descriptions-never-heal.md`, `group-pages-skip-nested-subgroups.md`,
`generated-output-not-reconciled-against-generation-set.md`). Each is either
superseded by this one, absorbed into a build step, or still independent; say
which, with the reason, before building.
