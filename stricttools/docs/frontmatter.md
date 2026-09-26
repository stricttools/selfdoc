+++
title = "Frontmatter"
description = "The TOML block every selfdoc page and post opens with: the +++ fences, the declared key registry the schema validates it against, and how to fetch and run the converter that rewrites a retired block."
nav_group = "Guides"
nav_order = 5
+++

# Frontmatter

Every page under `.stricttools/docs/` and every post under the posts directory opens with a
frontmatter block: TOML between `+++` fences.

```markdown
+++
title = "API Reference"
description = "Complete API reference for mypackage."
nav_order = 20
+++

# API Reference

Your content here...
```

A block is read once, by one reader, and validated against a
strictspec-generated schema before anything downstream sees it. That has three
consequences an author notices:

- **A key the schema does not declare is refused**, naming the key and the
  page. It is never silently ignored.
- **A value's type is the declared one.** A date is a bare TOML local date
  (`2026-09-14`), tags are an array of strings, a switch is `true` or `false`,
  and `nav_order` is a whole number. A quoted date or a `draft = "yes"` is
  refused rather than coerced.
- **A post's required keys are the schema's**, so the `POST001`, `POST002`,
  `POST003` and `POST006` lints report what the validator found rather than
  re-deciding it.

## The key registry

Every key a block may carry is below. The table is derived from the schema, so
it cannot fall behind the validator.

| Key | Type | Required on a post | What it declares |
| --- | ---- | ------------------ | ---------------- |
| `title` | string | yes | The page or post title: the sidebar label, the document heading and the browser title. |
| `description` | string | no | The one-sentence description the meta description, the search result and the staleness pass all read. |
| `slug` | string | no | A post's published address segment. Derived from the title when absent, and immutable once published. |
| `tags` | array of strings | no | The tags the page carries into the search facets and the post listing. |
| `date` | date | yes | The publication date, as a TOML local date (2026-09-14). |
| `updated` | date | no | The modification date, as a TOML local date. Says when the page changed, where date says when it appeared. |
| `nav_group` | string | no | The sidebar group title this page's group is labelled with. |
| `nav_order` | integer | no | The page's sort position: among the top-level pages for a page at the docs root, and within its group for a page in a subdirectory. A page declaring none sorts after every page that does. |
| `type` | string | no | The declared page type, which selects the layout, the content region's class, the structured-data type and the search type facet. |
| `versioned` | boolean | no | false keeps the page out of the per-version archive, so it is published once at its stable address. |
| `feed` | boolean | no | false keeps the page out of the Atom feed. |
| `schema` | string | no | The structured-data type this page emits, overriding the one its page type would select. |
| `auto_steps` | boolean | no | false stops the renderer marking this page's ordered lists up as HowTo steps. |
| `auto_api` | boolean | no | false stops the renderer marking this page's API sections up as API documentation. |
| `glossary_links` | boolean | no | false opts the page out of automatic term links. Declared and validated here; the linker is designed to read it and does not yet. |
| `locale` | string | no | The locale this document is written in, when it is not the project's default. |
| `generated` | boolean | no | true marks a page selfdoc wrote, which gen may overwrite and delete. |
| `seeded` | boolean | no | true marks a description selfdoc emitted rather than a person, which gen may freely rewrite. |
| `draft` | boolean | no | true keeps the post out of every build that did not ask for drafts. |
| `directives` | boolean | yes | Whether the post may carry directive markers. Required on a post and has no default: a post about directive syntax reads like a post that uses it. |
| `version` | string | no | The version a release post announces. |
| `prev_version` | string | no | The version a release post's release succeeded. |
| `bump_type` | string | no | The bump a release post's release performed. |
| `release_url` | string | no | The release page a release post links to. |
| `registry_urls` | array of strings | no | The registry pages a release post links to. |
--- PASS: TestDumpKeyTable (0.00s)
PASS

Two more keys exist and belong to the reader rather than to an author:
`format_version` and `document_kind`. The reader appends both before validating
-- `document_kind` is what tells the schema whether it is reading a page or a
post -- and a block that writes either of them is refused.

## Pages and posts

Both kinds share one schema and one key registry; they differ only in what is
required. A page may declare nothing at all. A post must declare a `title`, a
`date` and a `directives` switch, and `blog post generate` additionally writes
the release keys (`version`, `prev_version`, `bump_type`, `release_url`,
`registry_urls`) onto a release post.

## Converting a retired block

Before the TOML format, frontmatter was a hand-parsed block between `---`
fences in a `key: value` dialect with no key registry. selfdoc refuses such a
block by name and prints the commands that fetch the converter and run it. The
converter lives in selfdoc's own repository and no release artifact carries it,
so a repository fetches it first:

```bash
curl -fsSL https://raw.githubusercontent.com/stricttools/selfdoc/main/scripts/convert-frontmatter-to-toml.py -o convert-frontmatter-to-toml.py
python3 convert-frontmatter-to-toml.py --dry-run --expect-files 30
python3 convert-frontmatter-to-toml.py --apply --expect-files 30
```

The dry run prints a unified diff of every block it would change and writes
nothing; `--apply` writes. `--expect-files` asserts how many files the run
changes, and a different number writes nothing. The script refuses -- naming the
file and the line -- anything it cannot convert with certainty: a date that is
not `YYYY-MM-DD`, a switch holding something other than `true`/`false`, a sort
position that is not a whole number, and any key the registry does not declare.
Resolve each refusal by hand and rerun.

The script converts hand-authored pages and posts. A generated page (one
declaring `generated = true`) is left alone: `selfdoc gen` rewrites those, and a
run of `selfdoc gen` after the conversion puts every generated page into the new
format while keeping the handwritten descriptions it finds on them.

### The two sort keys became one

`order` sorted the pages at the docs root and `nav_order` sorted the pages
inside a navigation group, so exactly one of the two ever governed a page.
`nav_order` is now the single sort key in both places, and `order` is refused.
The converter keeps the value that governed the page and drops the key that did
not, so a converted page sorts where it sorted before. A page at the docs root
that declared only `nav_order` is the one case that moves: that declaration was
inert before and is honoured now.

The key `project` is refused as well. Nothing read it.
