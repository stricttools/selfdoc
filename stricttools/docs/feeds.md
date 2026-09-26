+++
title = "Atom Feeds"
description = "How selfdoc generates an Atom feed from your documentation pages, with date handling, page exclusion, and feed size limits."
nav_group = "Guides"
nav_order = 12
+++

# Atom Feeds

Every selfdoc site gets a `feed.xml` in the build output. It is a standard Atom 1.0 feed that RSS readers, news aggregators, and automation tools can subscribe to. No setup required -- it happens automatically during `selfdoc build`.

## How It Works

The build pipeline collects all documentation pages, extracts their titles and first sentences, sorts them by date (most recent first), and writes a valid Atom XML file. Each page becomes one `<entry>` in the feed with a title, link, summary, and updated timestamp.

Every generated HTML page includes a `<link rel="alternate" type="application/atom+xml">` tag in the `<head>`, so browsers and feed readers can auto-discover the feed.

## Date Handling

selfdoc determines page dates from 2 sources, checked in priority order. Accurate dates are important because they control the sort order of feed entries and the feed-level `<updated>` element that readers use to detect new content. The 2 sources are:

1. **Frontmatter fields** -- `date` and `updated` in your page's frontmatter, each written as a bare TOML local date. If `updated` is present, it takes precedence. Format: `YYYY-MM-DD`.
2. **File modification time** -- if no frontmatter date is set, selfdoc falls back to the file's last-modified timestamp from git (or the filesystem if git is unavailable).

```markdown
+++
title = "My Page"
date = 2025-03-15
updated = 2025-04-02
+++
```

The feed-level `<updated>` element uses the most recent date across all pages.

## Excluding Pages from the Feed

Set `feed = false` in a page's frontmatter to keep it out of the feed. This is useful for pages that are not meaningful as feed entries -- like the glossary, changelog, or auto-generated API reference pages.

```markdown
+++
title = "Glossary"
feed = false
+++
```

Pages without this field (or with `feed = true`) are included by default.

## Limiting Feed Size

By default the feed includes all pages sorted by date, which can grow large for documentation sites with hundreds of pages. For large sites, cap the number of entries with the `feed_max_entries` config option in `selfdoc.json` to keep the feed lightweight for subscribers:

```json
{
  "feed_max_entries": 20
}
```

This keeps only the 20 most recently updated pages in the feed. Older entries are dropped. The value must be a positive integer; omit it or set it to `null` for no limit.

## Subscribing

Point any Atom/RSS reader at `https://your-site.example.com/feed.xml` to subscribe to documentation updates. Most modern readers auto-detect the feed from any page URL thanks to the `<link rel="alternate" type="application/atom+xml">` tag that selfdoc includes in every HTML page head.

Common readers that work:

- **Feedly, Inoreader, NewsBlur** -- paste your site URL, they find the feed automatically
- **Thunderbird, NetNewsWire** -- add the feed URL directly
- **GitHub Actions / webhooks** -- poll `feed.xml` to trigger automations when docs update

> [!TIP]
> Make sure `base_url` is set in your `selfdoc.json`. The feed uses absolute URLs for entry links, and without `base_url` those links will be broken.

Next: [llms.txt](../llms-txt/) -->
