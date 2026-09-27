+++
title = "Search"
description = "Configure Pagefind search in selfdoc: the required search_engine key, the UI modes, the Cmd/Ctrl+K shortcut, and the seven filters pages carry."
nav_group = "Guides"
nav_order = 6
+++

# Search

Every selfdoc site ships with full-text search out of the box. No external services, no API keys, no CDN: the build runs [Pagefind](https://pagefind.app/) over the pages it just wrote, and Pagefind emits both the index and the search UI into `pagefind/` at the output root. Users search from any page via the keyboard shortcut or the UI widget.

## Declaring the engine

`search_engine` is required in `selfdoc.json`. There is no default and nothing is inferred -- every site builds a search UI, so the engine behind it is declared:

```json
{
  "search_engine": "pagefind"
}
```

`pagefind` is the only valid value. The key is the extension point: a second engine would be a new value here, not a new mechanism, and an absent key is a hard error at config load rather than a silent choice.

Pagefind itself has to be installed for the build to index anything:

```bash
npm install -g pagefind
```

or, where a Python toolchain is already present:

```bash
pip install 'pagefind[bin]'
```

`selfdoc check` reports `SEARCH001` when it is missing, and `selfdoc build` stops rather than writing a site whose search dialog answers nothing.

## UI Modes

The search widget has 3 display modes that control how users reach search on your site. All modes support the Cmd/Ctrl+K keyboard shortcut and open the same dialog. The only difference is the visible entry point in the topbar. Set the mode with the `search` config key:

```json
{
  "search": "icon"
}
```

- **`icon`** (default) -- a magnifying glass button in the topbar. Click it or press Cmd/Ctrl+K to open the search dialog.
- **`bar`** -- a visible text input in the topbar. Always ready for typing without an extra click.
- **`hidden`** -- no visible widget. Search is still accessible via the Cmd/Ctrl+K keyboard shortcut.

> [!TIP]
> The keyboard shortcut **Cmd+K** (macOS) or **Ctrl+K** (Windows/Linux) works in all three modes. It opens the dialog and focuses the input; Escape closes it.

## How the index works

`selfdoc build` writes every page, then runs Pagefind over the finished output directory. Pagefind reads the pages themselves -- there is no separate JSON index to keep in step with the HTML -- and writes:

- the index it queries at search time,
- one fragment per indexed page, with that page's headings as sub-results, so a result can land on the relevant section rather than the top of the page,
- the search UI's own JavaScript and CSS, which is what each page loads.

Each page marks its indexed region with `data-pagefind-body` on the `<article>` element, so navigation, the topbar and the footer never pollute results.

Every page addresses `pagefind/` through its own relative hop back to the output root, so a built site searches correctly under any mount point -- an origin root, a subpath, or a project subtree of the unified assembly site.

## Search filters

Pages carry structured filter attributes across 7 dimensions, and the search dialog offers each of them as a filter group:

| Filter | Values | Where it comes from |
| ------ | ------ | ------------------- |
| `version` | version strings (e.g., `1.0.0`) | the version being built |
| `locale` | locale codes (e.g., `en`, `pt-BR`) | the locale being built |
| `group` | nav group names (e.g., `Guides`) | the page's section in the sidebar |
| `type` | `guide`, `api`, `cli`, `changelog`, `glossary`, or your own | page frontmatter `type`, or derived from what the page is |
| `target` | deploy target identifiers | the configured deploy provider |
| `project` | project names | the project the page belongs to (unified sites carry several) |
| `tags` | tag strings | page frontmatter `tags` |

Selecting values from more than one group narrows results to pages matching all of them; selecting several values within one group widens to any of them.

Results also carry the page's project, type and publication date as metadata, which is what the result list displays.

## Adding tags to pages

Add a `tags` field to your page frontmatter to make pages selectable under the `tags` filter. Use arrays for multiple tags per page:

```markdown
+++
title = "Deployment"
description = "Deploy your documentation site to Cloudflare Pages or GitHub Pages."
tags = ["deploy", "cloudflare", "hosting"]
+++
```

Each tag becomes its own filter value, so a page tagged `[deploy, hosting]` appears under both.

Next: [SEO](../seo/) -->
