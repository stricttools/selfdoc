+++
title = "Theming"
description = "Customize your selfdoc site with the three built-in themes, CSS custom properties, dark mode support, the build-time theme override that lets one theme be judged on real pages, and a visual design tool for iterating on styles."
nav_group = "Guides"
nav_order = 40
+++

# Theming

selfdoc ships with three built-in themes and a comprehensive set of CSS custom properties that you can override without touching theme internals. Dark mode, high contrast, and print stylesheets work out of the box.

## Choosing a Theme

Set the `theme` field in your `selfdoc.json` configuration to select which built-in theme to use for your documentation site. The theme controls colors, typography, layout, and component styling. If omitted, the minimal theme is used by default:

```json
{
  "theme": "minimal"
}
```

Three themes are available:

- **minimal** (default) -- GitHub-inspired styling with Inter and JetBrains Mono fonts. Blue accent color (`#0969da`), light sidebar background, dark topbar. Content-focused and familiar.
- **clean** -- Stripe-inspired styling with system fonts and a purple accent (`#5046e4`). White topbar, borderless code blocks, slightly taller line height (1.7 vs 1.6). Feels more polished and modern.
- **tinymoon** -- the [tinymoon](https://github.com/smm-h/tinymoon) framework itself, not an imitation of it: dark by default, sharp corners everywhere, three vendored fonts (IBM Plex Sans for prose, IBM Plex Mono for anything that reads as data, Space Grotesk for headings), hairline borders with an accent glow instead of shadows, and a 100-180ms motion vocabulary. Blue accent (`#2d6cf4`). Tables, badges, breadcrumbs and metadata are all set in mono, and a faint grain overlay sits over the page.

All three include full dark mode, high contrast, reduced motion, and print support.

### What differs about tinymoon

Three things are worth knowing before choosing it.

**It is not a stylesheet in this repository.** minimal and clean are single CSS files selfdoc owns. tinymoon is an *overlay* on the [tinymoon](https://github.com/smm-h/tinymoon) framework, which selfdoc depends on: the stylesheet a page receives is the framework's own sheets -- `tokens`, `base`, `shell`, `primitives`, `widgets`, `prose`, in that order, byte for byte out of the dependency -- with selfdoc's overlay appended. The overlay carries the parts of a selfdoc page the framework has no shape for and a *bridge* that defines selfdoc's custom-property names as references to the framework's tokens. Upgrading the framework upgrades the theme.

**It rests in dark.** The framework's `:root` is the dark palette and the light one is a reassignment, which is the reverse of the other two themes. A `custom.css` override still lands on the same custom property names, but a `:root` override will be changing the *dark* values. The light palette is in `html[data-theme="light"]` for the explicit choice, and in `html:not([data-theme])` inside a `@media (prefers-color-scheme: light)` block for the system one -- so all three toggle states resolve in CSS alone, with no JavaScript involved in painting the right scheme.

**Its stylesheet is written into `css/` and ships files beside it.** The framework's `@font-face` rules address `../fonts/`, so the theme's stylesheet goes to `css/style.css` at the output root with the four woff2 faces in `fonts/`. Nothing is fetched from a font CDN and nothing is base64 in the stylesheet, so the faces are cached once for the whole site instead of re-downloaded with every sheet. In an assembled site the same payload is the site-level page chrome, under one content-hashed directory: a framework upgrade renames the directory, so no cache can serve the previous bytes against the new markup.

## Previewing a Theme

To see a theme on real pages without editing any project's configuration, pass `--theme` to a build:

```bash
selfdoc build --no-auto-commit --theme tinymoon
```

The override applies to that build only and is never written back to `selfdoc.json`. `selfdoc assembly preview --theme <name>` applies the same override to every checkout in an assembled preview at once -- which is the point: judging a theme means seeing the whole site under it, not one page. An unknown name is refused against the theme registry. With `--no-build`, where the override cannot reach the builds themselves, the preview checks that each checkout's existing build output really was produced under that theme -- an equality against the stylesheet a build writes, not a guess -- and hard-errors naming any checkout that was not.

## CSS Custom Properties

Every visual aspect of a selfdoc site is controlled by CSS custom properties defined in `:root`. Override any of them in a `custom.css` file (see next section) to change colors, fonts, or spacing without forking the theme.

### Colors

| Property | minimal | clean |
|---|---|---|
| `--bg` | `#ffffff` | `#ffffff` |
| `--text` | `#1a1a1a` | `#1a1f36` |
| `--text-secondary` | `#555` | `#5e697b` |
| `--heading` | `#111` | `#0a2540` |
| `--link` | `#0969da` | `#5046e4` |
| `--link-hover` | `#0550ae` | `#3b35a8` |
| `--border` | `#d0d7de` | `#e3e8ee` |
| `--shadow-sm` | `rgba(0,0,0,0.08)` | `rgba(0,0,0,0.04)` |

### Sidebar

| Property | minimal | clean |
|---|---|---|
| `--sidebar-bg` | `#f6f8fa` | `#ffffff` |
| `--sidebar-text` | `#444` | `#5e697b` |
| `--sidebar-active` | `#0969da` | `#5046e4` |
| `--sidebar-hover-bg` | `#e8ecef` | `#f7f8fa` |

### Topbar

| Property | minimal | clean |
|---|---|---|
| `--topbar-bg` | `#24292f` | `#ffffff` |
| `--topbar-text` | `#ffffff` | `#0a2540` |

### Code

| Property | minimal | clean |
|---|---|---|
| `--code-bg` | `#f4f4f8` | `#f7f8fa` |
| `--code-border` | `#e0e0e0` | `transparent` |

### Components

| Property | minimal | clean |
|---|---|---|
| `--badge-bg` | `#1a7f37` | `#5046e4` |
| `--badge-text` | `#ffffff` | `#ffffff` |
| `--admonition-tip` | `#1a7f37` | `#1ea672` |
| `--admonition-important` | `#8250df` | `#5046e4` |
| `--admonition-warning` | `#6d4a00` | `#c4841d` |
| `--admonition-caution` | `#cf222e` | `#cd3d64` |

### Typography

| Property | minimal | clean |
|---|---|---|
| `--font-body` | `'Inter', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif` | `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif` |
| `--font-mono` | `'JetBrains Mono', ui-monospace, 'Cascadia Code', Consolas, monospace` | `"SF Mono", "Cascadia Code", "Fira Code", Consolas, monospace` |

### Layout (clean only)

The clean theme defines two extra CSS custom properties not present in the minimal theme, controlling shadow depth and border radius for a more polished, card-like appearance. The minimal theme accesses these via CSS fallback syntax so they can still be overridden in `custom.css`:

| Property | clean default |
|---|---|
| `--shadow-md` | `0 1px 3px rgba(0,0,0,0.04)` |
| `--radius` | `6px` |

Minimal uses these via CSS fallbacks (e.g., `var(--radius, 4px)`) so they can still be overridden in custom.css for either theme.

## Custom Styles

Create a `stricttools/docs/custom.css` file in your project to override any CSS custom property or add your own styling rules. selfdoc automatically detects this file during build and injects it after the theme stylesheet, so your rules take precedence over the built-in theme values.

Example -- change the accent color and body font:

```css
:root {
    --link: #e63946;
    --link-hover: #c1121f;
    --sidebar-active: #e63946;
    --badge-bg: #e63946;
    --font-body: 'IBM Plex Sans', system-ui, sans-serif;
}
```

No build configuration is needed. Just save the file and run `selfdoc build`.

## Dark Mode

Both themes ship with a complete dark color palette that covers every CSS custom property, ensuring consistent contrast and readability across all page elements including code blocks, tables, and admonitions. Dark mode activates in two ways:

1. **Automatic** -- the `prefers-color-scheme: dark` media query matches the user's OS setting. This is the default behavior with no configuration needed.
2. **Manual toggle** -- clicking the theme toggle button in the topbar sets `data-theme="dark"` (or `"light"`) on the root element, overriding the OS preference.

### How the selectors work

The theme CSS uses a layered selector strategy with two complementary rules that handle both automatic OS-preference detection via the `prefers-color-scheme` media query and explicit user toggle interactions via the `data-theme` attribute on the root element:

- `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { ... } }` -- applies dark colors when the OS prefers dark, unless the user has explicitly toggled to light.
- `[data-theme="dark"] { ... }` -- applies dark colors unconditionally when the toggle is set to dark, regardless of OS preference.

### Overriding dark mode colors

To customize dark mode colors in your `custom.css` file, you must mirror the same dual-selector pattern used by the built-in themes so that your overrides apply in both automatic and manual dark mode:

```css
/* Override dark mode accent */
@media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
        --link: #ff6b6b;
        --sidebar-active: #ff6b6b;
    }
}
[data-theme="dark"] {
    --link: #ff6b6b;
    --sidebar-active: #ff6b6b;
}
```

### Dark mode property values

The following table shows selected dark mode defaults for both the minimal and clean themes, useful as a reference when deciding which CSS custom properties to override in your `custom.css` dark mode rules:

| Property | minimal dark | clean dark |
|---|---|---|
| `--bg` | `#0d1117` | `#0a2540` |
| `--text` | `#c9d1d9` | `#e3e8ee` |
| `--heading` | `#e6edf3` | `#ffffff` |
| `--link` | `#58a6ff` | `#9b93ff` |
| `--sidebar-bg` | `#161b22` | `#0a2540` |
| `--code-bg` | `#161b22` | `#0d2e4f` |
| `--border` | `#30363d` | `#2a2f45` |
| `--topbar-bg` | `#010409` | `#07192b` |

## High Contrast

Both themes respond to the `prefers-contrast: more` media query for users who need stronger visual contrast between text, borders, and backgrounds. This is a browser or OS-level accessibility setting that requires no configuration on your part.

In light mode, high contrast overrides strengthen text and borders:

| Property | minimal HC | clean HC |
|---|---|---|
| `--text` | `#000` | `#000` |
| `--heading` | `#000` | `#000` |
| `--border` | `#000` | `#000` |
| `--code-border` | `#000` | `#000` |
| `--link` | `#023b95` | `#1a0a91` |
| `--text-secondary` | `#333` | `#333` |

In dark mode with high contrast, the overrides flip to maximum brightness:

| Property | minimal HC dark | clean HC dark |
|---|---|---|
| `--text` | `#fff` | `#fff` |
| `--heading` | `#fff` | `#fff` |
| `--border` | `#fff` | `#fff` |
| `--link` | `#a8d4ff` | `#d4d0ff` |

To override high contrast values in `custom.css`, nest your rules in the matching media query:

```css
@media (prefers-contrast: more) {
    :root {
        --link: #0000cc;
    }
}
```

## Design Tool

The `demo/index.html` file is a standalone design tool for visually experimenting with theme settings without modifying any project files. Open it in a browser to see a full documentation page with an interactive settings panel that updates in real time.

Click the gear icon in the topbar to open the panel. It provides 16 knobs organized by category:

- **Layout** -- sidebar position (left/right/hidden), sidebar width, TOC position (right/inline), content width
- **Typography** -- font kind (sans/serif/mono/system), font size, heading transform (none/uppercase/small-caps), heading separator (none/line/dots)
- **Spacing** -- density (compact/default/relaxed)
- **Colors** -- light/dark mode, accent color, topbar color, gradient strip (on/off)
- **Components** -- border radius, code block style (default/minimal/bordered), table style (default/striped/bordered)

### Exporting settings

After adjusting the design knobs to your liking, click **Copy CSS to clipboard** to export your changes. The tool generates a `custom.css` snippet containing only the CSS custom properties you modified, ready to paste into your project's `stricttools/docs/custom.css` file.

You can also click **Copy link** to get a URL that encodes all current settings, making it easy to share a design with collaborators.

## Print Stylesheet

Both themes include a comprehensive `@media print` block that produces clean, readable printed output suitable for PDF export. The layout switches to single-column with no max-width constraint, and all interactive and navigation elements are hidden when printing:

- Sidebar, table of contents, mobile TOC
- Topbar and hamburger menu
- Navigation links (previous/next), breadcrumbs
- Search dialog and triggers
- Copy/run buttons on code blocks
- Theme toggle, feedback widget, edit links
- Hero section, feature grid, reading progress bar
- Site footer

The layout switches to single-column with no max-width constraint. Colors reset to the light-mode palette for legibility on paper. Page breaks are avoided inside code blocks, tables, images, API entries, admonitions, and blockquotes.

To add custom print rules in `custom.css`:

```css
@media print {
    :root {
        --text: #000;
    }
    .my-custom-element {
        display: none !important;
    }
}
```
