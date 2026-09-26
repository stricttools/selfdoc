+++
title = "Blog Posts"
description = "How to create, manage, and publish blog posts in selfdoc, covering frontmatter, the required directive declaration every post carries, release-generated posts, revision tracking, the post lints check runs, publishing documentation without a release, the declared roster, the home project served at the site root with its curated listing, project retirement, the single canonical hostname the site serves content on, the four machine-readable files the assembly writes at the site root, the generated deploy workflow and its pins, the verification every deploy has to pass, the local preview that assembles the whole site from checkouts and serves it before anything ships, and the theme override that builds every checkout under one theme so a theme can be judged on the whole site at once."
nav_group = "Guides"
nav_order = 19
+++

# Blog Posts

selfdoc includes a blog system for publishing chronological content alongside your documentation. Blog posts are Markdown files with a TOML frontmatter block, stored in a dedicated directory within your project. Posts are unversioned -- they exist outside the multi-version docs system -- and are published to the unified documentation assembly alongside your API reference and guides.

The blog system is part of the `selfdoc` binary, which carries the post commands (`selfdoc blog post new`, `selfdoc blog post list`, `selfdoc blog post publish`, and the rest) alongside the assembly infrastructure for multi-project documentation sites.

## Configuration

Blog posts require a `posts` section in your `selfdoc.json`:

```json
{
  "posts": {
    "dir": ".stricttools/posts/",
    "repo": "owner/posts-archive"
  }
}
```

- `dir` -- directory where post Markdown files live (defaults to `.stricttools/posts/` if omitted)
- `repo` -- optional GitHub repository for archiving resolved post content

You also need `topology.slug` configured so that posts are attributed to your project in the unified site:

```json
{
  "topology": {
    "slug": "myproject"
  },
  "assembly": {
    "repo": "owner/assembly-repo"
  }
}
```

## Creating Posts

### Manual creation

Use `selfdoc blog post new` to scaffold a new post file:

```bash
selfdoc blog post new --title "My First Post"
```

This creates a file like `.stricttools/posts/2026-07-29-my-first-post.md` with a frontmatter template:

```toml
+++
title = "My First Post"
date = 2026-07-29
slug = "my-first-post"
tags = []
draft = true
directives = false
+++
```

The filename is date-prefixed (`YYYY-MM-DD-slug.md`). The command errors if a file with that name already exists.

### Release-generated posts

`selfdoc blog post generate` creates posts automatically from release metadata. This is typically called by release tooling (e.g., rlsbl post-release hooks) rather than manually:

```bash
selfdoc blog post generate \
  --from-release \
  --version 1.2.0 \
  --prev-version 1.1.0 \
  --bump-type minor \
  --description "Added widget support" \
  --project-name "My Project" \
  --changelog-file .rlsbl/changes/1.2.0.md \
  --release-url "https://github.com/owner/repo/releases/tag/v1.2.0" \
  --registry-url "https://pypi.org/project/myproject/1.2.0/"
```

Generated release posts are created with `draft = false`, `directives = false`, and release-specific frontmatter fields (`version`, `prev_version`, `bump_type`, `release_url`, `registry_urls`). The command also updates the project manifest with the new post entry.

Use `--dry-run` to preview the generated content without writing files.

## Frontmatter Format

Every post requires a frontmatter block -- TOML between `+++` fences, validated against the key registry the frontmatter guide carries. Required and optional fields:

### Required fields

| Field | Type | Description |
| --- | --- | --- |
| `title` | string | Post title. Must be non-empty. |
| `date` | date | Publication date, written as a bare TOML local date (`2026-07-29`). |
| `directives` | boolean | Whether the post may carry directive markers. No default: a post that omits the key raises `POST006` at discovery. |

A post is authored content that may or may not embed code-extracted material, and a post *about* directive syntax reads exactly like a post that uses it. So the author declares which it is rather than the reader guessing. Declaring `directives = false` and then writing a marker raises `POST007`, naming the marker and the line it sits on -- markers inside fenced code blocks and backtick code spans are examples of the syntax, not uses of it, and are never counted. Declaring `directives = true` resolves the post's directives exactly as a documentation page's are resolved.

Documentation pages carry no such key. The whole `.stricttools/docs/` tree is directive territory by construction; only posts declare.

### Optional fields

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `slug` | string | derived from title | URL-safe identifier. Auto-generated from the title using kebab-case if omitted. |
| `tags` | list | `[]` | List of tag strings for categorization and search filtering. |
| `draft` | boolean | `false` | When `true`, the post is excluded from publishing and the unified site. |
| `locale` | string | none | Locale code for localized posts. |

### Release-specific fields

These fields are set by `selfdoc blog post generate --from-release` and are not typically written by hand:

| Field | Type | Description |
| --- | --- | --- |
| `version` | string | The released version number. |
| `prev_version` | string | Previous version for upgrade context. |
| `bump_type` | string | Semver bump type (`patch`, `minor`, `major`). |
| `release_url` | string | URL to the GitHub release page. |
| `registry_urls` | list | Package registry URLs (PyPI, npm, etc.). |

### Slug immutability

Once a post is published (appears in the manifest), its slug cannot change. The `selfdoc check` command enforces this: if a post file's slug differs from what was recorded in the manifest, it raises a `POST005` error. This prevents broken URLs in the unified site.

## Listing Posts

View all discovered posts with:

```bash
selfdoc blog post list
```

Output shows each post's date, title, slug, and draft status:

```
2026-07-29  Widget Support  (widget-support)
2026-07-15  Getting Started  (getting-started)  [DRAFT]

2 post(s) found.
```

Posts are sorted newest-first, with same-date posts sorted alphabetically by slug.

## Publishing Posts

`selfdoc blog post publish` pushes non-draft posts to the documentation assembly:

```bash
selfdoc blog post publish
```

The publish flow:

1. Discovers all posts in the configured posts directory
2. Filters out drafts (only non-draft posts are published)
3. Records content revisions for each post (see Revision Tracking below)
4. Builds post HTML locally using selfdoc's build pipeline
5. Pushes built HTML and the post-manifest to the assembly repo via the GitHub Git Data API (atomic commit, no clone required)
6. Dispatches a `shared-only` rebuild to regenerate cross-project elements (blog index, RSS feed, sitemap)
7. Optionally archives resolved Markdown to a separate posts repository (if `posts.repo` is configured)

Publishing is separate from a full documentation release. You can publish new posts without releasing a new version of your software -- the assembly workflow rebuilds only the posts subtree and shared elements.

## Revision Tracking

selfdoc tracks content revisions for blog posts via a sidecar file at `.stricttools/docs-state/revisions.json`. Revision tracking is automatic -- it happens during `selfdoc blog post publish`.

### How it works

Each post's body content (with frontmatter stripped) is hashed using SHA-256. Whitespace is normalized before hashing so that insignificant formatting changes do not trigger false revisions. A new revision entry is appended only when the content hash changes compared to the most recent revision.

Each revision records:

- `content_hash` -- SHA-256 of the normalized body text
- `timestamp` -- UTC ISO-8601 timestamp of when the revision was recorded
- `summary` -- optional human-readable description of the change

The revisions file structure:

```json
{
  "posts": {
    "my-first-post": {
      "revisions": [
        {
          "content_hash": "a1b2c3...",
          "timestamp": "2026-07-29T12:00:00+00:00",
          "summary": "Initial publish"
        },
        {
          "content_hash": "d4e5f6...",
          "timestamp": "2026-08-05T09:30:00+00:00"
        }
      ]
    }
  }
}
```

The revisions sidecar is published to the assembly alongside the post-manifest, stored as `manifests/{slug}-revisions.json`.

## Post Validation

`selfdoc check` runs five post-specific validation checks:

| Code | Description |
| --- | --- |
| `POST001` | Missing `date` field in frontmatter |
| `POST002` | Missing or empty `title` field |
| `POST003` | `date` field is not in `YYYY-MM-DD` format |
| `POST004` | Duplicate slug across posts |
| `POST005` | Slug changed after publication (immutability violation) |

These checks run as part of `selfdoc check` for standalone blog projects. For unified docs-site projects, post checks run alongside the full documentation validation suite.

Suppress specific checks with `--ignore`:

```bash
selfdoc check --ignore POST003
```

## Integration with the Assembly

Blog posts integrate into the unified multi-project documentation site through the assembly system. The assembly is a GitHub repository that collects built documentation from multiple projects and deploys the combined site.

### How posts reach the unified site

1. **Per-project build**: `selfdoc build --target posts` builds post HTML and generates a `post-manifest.json` containing metadata for all non-draft posts.

2. **Assembly push**: `selfdoc blog post publish` pushes each built post into `site/blog/{post-slug}/` in the assembly repo -- the site level, under no project slug -- and the post-manifest into `manifests/{slug}-posts.json`. The listing page the build renders for the project's own standalone site is not pushed: the assembled site's blog index is generated from every project's manifests.

3. **Shared regeneration**: The assembly workflow runs `selfdoc assembly integrate`, which grafts the dispatched build into the assembly tree and then regenerates the shared cross-project elements from all per-project manifests and post overlays:
   - A blog index page listing all posts across all projects, sorted newest-first
   - An Atom RSS feed aggregating posts from every project
   - An XML sitemap including all post URLs
   - A navigation JSON file with project and blog links
   - A homepage listing all projects

4. **Post overlay merging**: Post-manifest files (`*-posts.json`) act as overlays. When the shared elements generator finds a post overlay for a project, it replaces that project's posts in the base manifest with the overlay's posts, which is why deleting a post and republishing removes it from the site. A full build does not delete the overlay -- it folds its own posts into it, so a release's posts appear without the overlay's out-of-band posts being lost.

### Post URLs in the unified site

Posts are served at `/blog/{post-slug}/` in the assembled site, with no project segment. For example, a post with slug `widget-support` is available at `/blog/widget-support/` whichever project wrote it.

The project a post came from is a row on the blog index, not part of its address: the site has one blog, and every project's posts share its slug namespace. Two projects publishing the same post slug is a hard error -- refused when the assembly merges their manifests, and refused again by the graft before it can overwrite the other project's file.

### One hostname

A Cloudflare Pages project can carry several custom domains, and all of them serve the same site. Left alone that means every page is reachable at more than one address, which splits ranking signals between duplicates. The site resolves this to one hostname: `topology.docs_base`, and nothing else serves content.

selfdoc emits nothing for this. A host that should answer somewhere else is a redirect rule on the DNS zone, applied where the site is hosted; the assembly writes no `_worker.js` and reads no retired-host setting. An assembly tree that still carries a `_worker.js` from a deploy made before this was true is refused by `selfdoc assembly verify` and deleted by the next integration's shared-element pass.

The shared homepage and blog index carry a `rel="canonical"` link pointing at the canonical host, so a crawler that reaches them through a non-canonical domain still records the canonical address. Set `topology.posts_base` to the canonical blog URL; it is a path on the docs site, not a separate host.

#### Historical addresses

An address shape the site has retired -- `/<slug>/<locale>/<version>/<rest>` and `/<slug>/posts/<post>/` are the two with links in the wild -- is answered by the site's root 404. Nothing rewrites it. `/<slug>/<page>/` is the current scheme and is served; archived versions are served at `/<slug>/v/<version>/`.

#### Operator steps (outside selfdoc)

Two pieces of this topology live on platform dashboards and are not automated:

1. **Custom domains and their redirects.** Every hostname the site answers on is attached to the assembly's Cloudflare Pages project under its *Custom domains* tab, and every hostname that should redirect instead is a zone rule written there.
2. **Search Console.** Register a **Domain property** for the root domain rather than one URL-prefix property per subdomain. A Domain property covers the canonical host, any retired subdomain, and the apex in a single property, so the consolidation is visible as redirects instead of appearing as unrelated sites competing with each other.

### The machine-readable files at the site root

Every constituent build writes a `robots.txt`, an `llms.txt`, a `sitemap.xml` and a `404.html` at its own output root, where the graft buries them under `<slug>/` and no crawler reads them. The four the site serves are generated once, for the whole site, by the shared-element generator.

* **`sitemap.xml`** lists every project's pages and every post. Each `<loc>` is absolute under the canonical base -- the sitemap protocol has no relative form -- and an empty or root-relative base is refused.
* **`robots.txt`** names that sitemap by absolute URL, and carries the same crawler policy the per-project template declares. Both read one declaration (`internal/robots.Agents`), so a crawler the site allows cannot be one its projects disallow.
* **`llms.txt`** composes the per-project files **by reference**: one line per project, with its name, a link to its own `llms.txt`, and the one-line description from its manifest, plus a link to the blog. A project whose manifest states no description gets `(no description)` rather than a bare link, so the gap is visible to whoever can fill it instead of reading as an entry with nothing to add. It never inlines their contents -- an inlined copy would be a second, staler rendering of a document its owner republishes on its own deploys.
* **`404.html`** is what Cloudflare Pages serves, with a 404 status, for an address that matches no asset. Its body is deliberately not the front page's: an unknown address that renders the home page is a soft 404, which a crawler indexes as a duplicate of the site root and a reader mistakes for having arrived somewhere. It links home, the project listing and the blog -- relatively, like every other link a reader clicks, so a dead address on a preview offers its way back into the preview. It asks not to be indexed (`<meta name="robots" content="noindex">`), declares no canonical and carries no structured data: it is the answer to every address the site does not serve, so it has no address of its own to describe.

The home project is left out of `llms.txt` for the same reason it is left out of the listing: it is the site root the file is served from, not one of the projects it points at.

### Every assembled page ends by naming the site's other tools

A project's documentation used to link nothing outside its own subtree, so a
reader who arrived from a search result saw one tool and no sign that the site
published any others. Every page an assembly build produces now ends, just
before the footer, with a section headed **More tools from this site**: one
entry per other project on the roster, with its name and the one-line
description from its manifest, in name order.

* The home project is left out of the list, for the same reason it is left out
  of `llms.txt` and the listing: it is the site root every page already reaches,
  not one of the tools the site publishes. So is the project whose page it is.
* The roster reaches the build as an argument on the build call, never as
  ambient state. A build handed none -- which is what a project deploying on its
  own passes -- emits no section at all; there is no assembled site around it
  and nothing to invent siblings from.
* Each link is written against the page's own hop back to the site root, so it
  resolves on a preview and a mirror as well as on the deployed host. Posts hop
  differently from documentation pages, because the graft lifts a post out of
  the project's subtree to the site root.
* The section sits outside the element the search indexer reads a page's body
  from, and is marked `data-pagefind-ignore`, so the same list of projects is
  not indexed once per page on the site.
* **The assembly regenerates the section on every deploy**, over every page in
  the tree rather than over the subtree being deployed. What a build renders is
  the membership of the day it ran, and the assembly is never rebuilt whole: a
  project's subtree is replaced only when that project deploys. Without the
  sweep, a project joining or changing its description would reach nobody
  else's pages, and a retirement would leave its address linked from every
  other project's -- addresses the tree no longer serves, which the
  whole-tree verification then refuses every following deploy over. A page
  published before the section existed gains one on the sweep, so an old
  subtree converges too.

### What a search engine reads off the two generated pages

The project listing at `/projects/` and the blog index at `/blog/` are the
site's own pages rather than any project's, so nothing upstream writes their
metadata. The shared-element generator writes it from the manifests it already
holds:

* A **meta description**. The listing's names the site, says it carries the
  documentation of every project published on it, and then names as many of
  those projects as fit -- whole names only, cut at a name boundary, never
  mid-word. The blog index's names the site's blog and counts nothing: a post
  count is wrong on the next post published, and nothing re-fetches a search
  engine's cached copy of it. Both are clamped to the length a search result
  renders before it truncates.
* A **`CollectionPage`** JSON-LD document carrying a **`BreadcrumbList`** --
  Home, then the page. Its URLs are absolute under the canonical base, which is
  the one place on these pages an absolute address is right: a breadcrumb URL
  states where a page sits on a site, it is not a link a reader clicks. The
  rendered links stay document-relative, and the resolution rule that refuses an
  absolute self-link reads anchors and reference attributes, so it never sees
  these.

### The deploy workflow is a generated artifact

The assembly repo's `.github/workflows/deploy.yml` is generated from the
project's `selfdoc.json`, not hand-written. It is deliberately thin: check
out the assembly, install the toolchain, clone the dispatched project, run
`selfdoc assembly integrate`, deploy the result to Cloudflare Pages. Every
decision the deploy makes lives in the command, so it can be tested without
dispatching a real deploy.

`selfdoc assembly init` writes that file when the assembly repo is created,
and **`selfdoc assembly sync-workflow` rewrites it afterwards**. Run it (or
let the release path run it) whenever selfdoc changes: it regenerates the
workflow, compares it against the deployed copy, and pushes only when the
bytes differ. Without it the deployed workflow stays frozen at whatever the
template said the day the repo was created.

The workflow's install line pins **both** tools it installs. selfdoc is
installed with `go install <module>@v<version>`, pinned by default to the
version of the binary that generated the file; pagefind is `pip install
'pagefind[bin]==<version>'`, a CI-only tool nothing here installs, so its pin
is PyPI's current release at sync time. Each pin is rewritten by every
`sync-workflow` run, so they track releases rather than capping them, and a
deploy can never pick up a tool whose behavior the deployed workflow does not
know about. `--pin-selfdoc` and `--pin-pagefind` name either one explicitly.

The release path names the selfdoc pin rather than taking the default: the
binary a post-release hook finds on PATH was built before the release's own
version bump, so its version names a release the module proxy has never been
asked for.

Before writing anything, `sync-workflow` checks that each pinned version is
actually published -- the pagefind pin against PyPI, the selfdoc pin against
the Go module proxy -- and refuses the whole run when one is not. The default
selfdoc pin is the *running* binary, which in a checkout is built from an
unreleased commit: writing that pin would produce a workflow whose `go
install` cannot resolve, and the failure would surface on the assembly
repository at the next dispatch instead of here.

### Verification before the deploy

The deploy reads the tree it assembled before it commits or pushes any of
it. `selfdoc assembly integrate` runs the verification itself, after the
search index and before the commit, so a tree that fails is a tree that
never reaches the site. There is no flag that turns it off.

What it asserts, each failure naming its offender:

| Property | What a failure means |
|---|---|
| Roster, `site/` subtrees and `manifests/` name the same projects | An undeclared subtree, a declared project with nothing to serve, or an orphan manifest of any kind |
| Each manifest describes the tree beside it | Its slug names another directory, its version disagrees with the pages, or its current version is sitting in the archive tree |
| Every page and post a manifest lists was emitted | A listing, feed or sitemap row that leads to a 404 |
| The shared artifacts exist, parse, and say what they are for | A missing or malformed front page, project listing, blog index, `nav.json`, sitemap, feed, `robots.txt`, `llms.txt`, root 404, or an empty search index; a 404 whose body repeats the front page or offers no way back; a `robots.txt` naming no sitemap or one the tree does not carry; an `llms.txt` missing a declared project |
| Every reference resolves | An internal link, canonical, sitemap entry or feed link naming a file the assembly did not write |
| Every page is addressable | A page with no title, or a canonical that is not under the site's canonical base |
| Nothing half-built or per-project leaked in | An unresolved directive marker, or a project's own `_headers`, `_redirects` or pre-compressed copies |
| Cross-project links land somewhere | A link from one project's page into a page no other project publishes |
| Every project is reachable | A declared project whose index page no chain of clickable links from the site root or the `/projects/` listing arrives at, so it is published where no reader arriving at the site and no crawler following links ever gets to it. Either arrival page may curate what it shows. The home project is exempt: the site root is its own front page |

`selfdoc assembly verify --assembly-dir <checkout> --canonical-base <url>`
runs the same assertions by hand against a checkout. It is read-only.

#### Outbound links

External links are checked only on pages the assembly declares, in a
committed `outbound.toml` at the assembly root:

```toml
cache_days = 7

[[page]]
path = "index.html"

[[page]]
path = "blog/index.html"
```

Both keys are required and unknown keys are refused. Results are stored by
address in `outbound-cache.json` and trusted for `cache_days`, so a deploy
that changes nothing sends no requests. **With no `outbound.toml` there is
no outbound check**, and every run says so on stderr rather than passing
quietly.

### Previewing the whole site before it ships

`selfdoc assembly preview` assembles the site from local checkouts and
serves it, so the last look at a change happens before anything is
published rather than after:

```bash
selfdoc assembly preview \
  --home ~/Projects/portfolio \
  --repo ~/Projects/pgdesign \
  --repo ~/Projects/rlsbl \
  --canonical-base https://smmh.dev \
  --out ~/scratch/preview \
  --port 8790 \
  --build
```

Every flag is required except `--theme`. `--home` names the one
project served at the site root; `--repo` is repeated once per other
project and each is served under the slug its own `selfdoc.json` declares.
`--canonical-base` is the **deployed** base, not the loopback address: the
preview shows the pages with the canonical links, sitemap entries and
cross-project links they would ship with, and verifies those.
`--build` / `--no-build` has no default because the choice is the point:
`--build` is the honest preview of what would ship, `--no-build`
re-assembles whatever each checkout already has in `.stricttools/docs-cache/build`, which is
how you iterate after one edit without rebuilding every project.

Curation cuts both ways here: the home project's `.stricttools/docs/projects.toml`
names the projects the front page lists, and **a listed slug with no
manifest is a hard error**. So a preview has to include every non-external
project the listing names, not just the ones being changed. Leaving a
roster project out of the listing stays legal -- it simply is not listed.

The pipeline is the deploy's, step for step, with the remote-coupled steps
replaced by their local equivalents rather than skipped:

| Step | What runs |
|---|---|
| Build | Each checkout is built by the toolchain running the command -- the home project through `selfdoc build --target home`, everybody else through `selfdoc build`, exactly as `assembly integrate` does. The home project builds last, so its front page reads the other projects' freshly grafted manifests. |
| Graft | `split_build_output` and the same pruning graft: the home project at the site root, everybody else under `site/<slug>/`, posts site-level under `blog/`, per-project `_headers`, `_redirects` and `404.html` left behind. |
| Membership | A roster rendered from the checkouts named on the command line, and a `projects.json` written by the same `record_membership` the deploy uses. Dropping a `--repo` on a rerun retires that project from the tree. |
| Shared elements | The real `generate_shared_files`: listing, blog index, `nav.json`, feed, sitemap, `robots.txt`, `llms.txt`, root 404, `_headers`, the site chrome asset, the re-pointing pass that aims every grafted page at it, the link repair pass that rewrites every link naming the site's own base document-relatively, and the sibling-block pass that regenerates every page's **More tools from this site** section from the membership as it is now. |
| Search | The pagefind pass over the assembled tree. |
| Verification | The real `verify_assembly`, printed **first and loudly**. |

Verification reports; it does not block. A deploy refuses a tree that
fails, but a preview exists to be looked at when something is wrong, so
the report is printed and the server starts either way.

The server binds `127.0.0.1` only. It serves each directory's
`index.html`, redirects an address missing its trailing slash, serves
every file under its real content type, and answers an address the tree
does not carry with the tree's own `404.html` **and a 404 status** -- a
404 page served as `200` is how a broken link survives a preview.

`--out` is refused when it sits inside a git working tree at a path git
does not ignore. A preview writes thousands of generated files, and a
generated site dropped into a checkout is untracked noise in every
`git status` run in it -- including other sessions' -- and gets committed
by accident. Choose a directory outside every repository, or gitignore the
path first.

`--dry-run` is refused at parse time with its reason: the command exists to
produce output you look at, and every step after the build reads what the
step before it wrote.

### Posts-only vs full builds

The assembly workflow distinguishes between posts-only and full documentation dispatches:

- **Posts-only** (`scope: "posts"`): rebuilds only the posts subtree, preserving the rest of the project's documentation. Uses `selfdoc build --target posts`.
- **Full build**: rebuilds all documentation including posts, and folds its posts into the post overlay.
- **Shared-only** (`scope: "shared-only"`): regenerates only cross-project elements (blog index, feed, sitemap) without rebuilding any project's documentation. Used after post publishes, documentation publishes and retirements.

Every scope reconciles membership first: whatever else a dispatch is doing, it makes the tree match `roster.toml` before it makes it match the build.

## Publishing Without a Release

Two commands put content on the live site with no tag and no release. Both build locally, push straight into the assembly repository through the Git Data API, and then dispatch a shared-only rebuild.

```bash
selfdoc blog post publish    # non-draft posts
selfdoc blog publish-docs    # the project's documentation
```

`blog publish-docs` builds the docs the same way the deploy does, applies the same deploy-artifact exclusions (`_headers`, `_redirects`, `_worker.js`, `.gz`, `.br`), and pushes the project's subtree, its manifest, its published-file record and its membership entry in one commit. Content travels as bytes, so images and fonts survive intact. Deletions travel with it: a page the project published before and no longer builds is removed in the same commit.

Both commands are consequential -- they make locally-authored writing publicly readable -- so they prompt unless `--approve-consequential` is passed. Neither can create membership: publishing into a slug `roster.toml` does not declare is a hard error naming the block that would have to exist.

### What a build owns

A full build used to replace `site/{slug}/` wholesale, which meant a release destroyed anything published into that subtree since the last one. It prunes to its own output instead.

Every publisher -- the release-time integrate, `blog publish-docs`, `blog post publish` -- records the paths it produced in `manifests/{slug}-files.json`:

```json
{
  "schema_version": 2,
  "slug": "myproject",
  "owners": {
    "release": ["myproject/index.html", "myproject/reference/index.html"],
    "docs": ["myproject/index.html", "myproject/hotfix/index.html"],
    "posts": ["blog/widget-support/index.html"]
  }
}
```

Every path is relative to `site/`, so one record names both the project's own pages, under its slug, and its posts, at the site level under `blog/`. That single namespace is also what lets one project's claim be checked against another's: the graft refuses to write over a post path another project's record claims.

A publisher removes a path only when it produced that path before and does not produce it now, and never when another publisher currently claims it. So:

| Situation | Outcome |
| --- | --- |
| A page the new build dropped | Pruned |
| A page published between releases that the build never produced | Kept |
| A page both a release and a documentation publish produce | Refreshed by whichever ran last |
| A post published out of band, on a release that does not carry it | Kept |

An absent record means nobody has published anything for that project yet, so nothing is removed -- a publisher never deletes what it cannot show it wrote.

## Membership: the Roster

The projects the unified site serves are declared in `roster.toml`, hand-edited and committed in the assembly repository:

```toml
home = "mysite"

[[project]]
slug = "mysite"
repo = "owner/mysite"

[[project]]
slug = "myproject"
repo = "owner/myproject"

[[project]]
slug = "otherproject"
repo = "owner/otherproject"
```

`slug` and `repo` are both required, unknown keys are a hard error, a duplicate slug is a hard error, and a slug that collides with one of the assembly's own directories (`blog`, `projects`, `pagefind`) is a hard error. A missing file is a hard error too: membership has no empty default, because a deploy that guessed at it would be accumulating membership again.

`home` is required as well, and names exactly one of the declared projects -- see below.

### The home project

One declared project is the site's front page. `home = "<slug>"` says which, and there is no default: a roster with no `home`, or a `home` naming a slug no `[[project]]` block declares, is a hard error. A site needs a front page, and selfdoc will not pick one.

The home project is an ordinary project -- a real repository that dispatches its own deploys like any other. Being home changes four things:

* **Its pages are emitted at the site root.** `index.md` becomes `/`, `cv.md` becomes `/cv/`. No project segment, no locale segment, no version segment, and no archives.
* **It is left out of the generated listing and out of `nav.json`.** The front page does not list itself.
* **The addresses the assembly owns are refused to it.** A page that would land on `blog/`, `projects/`, `v/` or `pagefind/` is a hard error at the graft and again in `assembly verify`, which also refuses a leftover `site/<home>/` subtree and a home directory that shadows another project's slug.
* **It cannot be retired.** Retiring the front page would leave the site without one; name another project home first.

The site-wide artifacts the home project's own build writes for standalone hosting -- `sitemap.xml`, `robots.txt`, `404.html`, `feed.xml`, `llms.txt`, `nav.json`, the routing files and the compressed variants -- are dropped on the way in. The assembly writes the ones the site serves.

#### The curated listing

The home project declares which projects the site shows, and how, in `.stricttools/docs/projects.toml`:

```toml
[[category]]
name = "Frameworks"

  [[category.project]]
  slug = "myproject"
  blurb = "One line about it."

[[category]]
name = "Elsewhere"

  [[category.project]]
  slug = "thing"
  name = "Thing"
  blurb = "A project with no docs section here."
  url = "https://example.org/thing"
```

The listing is content, so it lives with the content. An entry without `url` names a project the site serves: its display name and version come from that project's manifest, which is why declaring a `name` for one is refused -- there would be two sources for the same fact. An entry with `url` is external and therefore carries its own `name`.

Validation is strict: unknown keys, a missing or empty `blurb`, a category with no entries, a duplicate slug, and a listed slug the assembly has no manifest for are each a hard error naming the offender. A roster project the listing leaves out is legal -- curation is selection.

The deploy copies the file in beside the manifests, and both renderings read it: the generated `/projects/` page and the front page's cards.

#### Site-level directives

Two directives are available to the home project's pages:

| Directive | Renders |
| --- | --- |
| `:-: projects-cards` | the curated listing, with each project's live version |
| `:-: blog-highlights limit="N"` | the N most recent posts across every project |

They resolve twice, from the same code. Once at build time -- which is why the home project builds through `selfdoc build --target home --site-manifests <dir>` rather than through `selfdoc build`: a version badge is read from the assembly's manifests, and no project's own repository holds them. Without that context the command refuses to build at all, naming what is missing; a plain `selfdoc build` refuses too, on the unknown directive. Neither ever emits an empty region.

Then again on **every** deploy, including deploys the home project has nothing to do with. Each resolved region is left in the emitted HTML inside a `<selfdoc-region>` element, and the shared-element generator rewrites its contents from the current manifests. That is what keeps a front-page version badge current when the project it names releases: the prose and the design stay authored, the mechanical parts cannot go stale.

`projects.json` beside it is **derived** state, rewritten by every deploy: it records what each declared project last deployed (`repo`, `ref`, `version`) and is what `selfdoc assembly rebuild` replays. It cannot gain a key on its own -- a dispatch for a slug the roster does not declare is refused, and a slug the roster declares under a different repository is refused too.

Every deploy reconciles the tree to the declaration. A project that is no longer declared loses its site subtree, all of its manifest kinds, its `projects.json` record, and -- because the search index is rebuilt from scratch whenever anything went -- its entries in the index.

### Retiring a project

```bash
selfdoc assembly retire --slug oldproject
```

One operation: the `[[project]]` block leaves the roster and, in the same commit, every path the project owns is deleted; the shared-only dispatch that follows regenerates the listing, blog index, feed, sitemap and search index without it. It is consequential -- the only command in either CLI that removes published content -- and retiring a slug the roster does not declare is a hard error naming the ones it does.

## Building Posts Locally

Preview posts locally before publishing:

```bash
# Build posts only (no versioned docs)
selfdoc build --target posts

# Include draft posts in the build
selfdoc build --target posts --drafts
```

Built HTML is written to the configured output directory: each post at `.stricttools/docs-cache/build/blog/{post-slug}/`, plus a listing page at `.stricttools/docs-cache/build/blog/` for the project's own standalone site.
