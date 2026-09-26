+++
title = "Glossary Guide"
description = "selfdoc's glossary: declare a term with a dfn tag, a definition list or the list-glossary directive -- prose declares nothing -- and get auto-linking, term anchors and a generated page."
nav_group = "Guides"
nav_order = 14
+++

# Glossary Guide

selfdoc can build a glossary from terms defined across your documentation. Define a term once, and selfdoc auto-links the first mention on every other page back to its definition. You also get a generated glossary page that aggregates all terms alphabetically.

## Defining Terms

A term becomes a glossary entry only because you declared it. There are 3 declaration forms: inline `<dfn>` tags embedded naturally in your prose, Markdown definition lists, and the `list-glossary` content directive for dedicated reference sections. All three register terms in the site-wide glossary and enable auto-linking across pages.

selfdoc never guesses a term from prose. A sentence like "The export represents the whole build" is prose, not a declaration, no matter which heading it follows.

### Inline `<dfn>` tags

Wrap a term in `<dfn>` tags anywhere in your Markdown content to mark it as a glossary entry. selfdoc extracts the term name from the tag text and uses the surrounding paragraph as its definition. This is ideal for introducing terms naturally in prose, right where readers first encounter them:

```markdown
A <dfn>directive</dfn> is a structured marker in your Markdown templates
that selfdoc resolves at build time by extracting content from source code.
```

This creates a glossary entry for "directive" with the paragraph text as its definition.

### Definition lists

A Markdown definition list -- a term line followed by one or more lines starting with `: ` -- declares each of its terms:

```markdown
Directive
: A structured marker that selfdoc resolves at build time.

Extractor
: A language-specific module that reads source code.
```

### The `list-glossary` directive

For a dedicated glossary section with multiple curated terms, use the `list-glossary` content directive. It renders as a styled HTML definition list (`<dl>/<dt>/<dd>`) and registers every term in the site-wide glossary for auto-linking. Each term is defined with `**Term**: Definition` syntax:

```markdown
:<: list-glossary
:=:
::: **Directive**: A structured marker that selfdoc resolves at build time.
::: **Extractor**: A language-specific module that reads source code to fulfill directives.
::: **Frontmatter**: YAML metadata at the top of a Markdown file.
:>:
```

This renders as a styled definition list (`<dl>/<dt>/<dd>`) and registers each term in the site-wide glossary. The `glossary` alias also works in place of `list-glossary`.

## Auto-linking

Once a term is defined (via either method), selfdoc auto-links the first occurrence of that term on every other page. The link points to the term's definition, either on the glossary page or on whichever page defined it.

Auto-linking is case-insensitive for matching but preserves the original casing in the rendered text. Only the 1st mention per page gets linked -- subsequent mentions are left as plain text to avoid cluttering the page.

> [!NOTE]
> Auto-linking skips content inside glossary blocks themselves to prevent circular links. Terms inside code blocks and headings are also left alone.

## The Generated Glossary Page

When `glossary` is `true` in your `selfdoc.json` (which it is by default), selfdoc generates an alphabetically sorted glossary page that collects every declared term across your entire site. Each entry shows the term, its definition, and a Source link back to the exact definition site on the page that declared it. Declare no terms and there is no glossary page at all -- selfdoc never emits an empty one.

Every definition site carries an id of the form `term-<slug>`, in its own namespace so it can never take an id a heading owns. That id is what the Source link and the cross-page term links scroll to.

### From the definition back to the glossary

The definition site works in the other direction too: the `<dfn>` you wrote becomes a link to its glossary entry, with the definition's first sentence as its tooltip. Other mentions of the term on that same page stay plain -- the page already defines it.

## Configuration

The glossary feature is controlled by a single boolean in `selfdoc.json`. When enabled, selfdoc generates an alphabetically sorted glossary page collecting every term across the site and activates auto-linking of first mentions on each page back to their definitions:

```json
{
  "glossary": true
}
```

- `true` (default) -- glossary page is generated, auto-linking is active
- `false` -- no glossary page, no auto-linking (but `<dfn>` tags and `list-glossary` directives still render normally on their own pages)

## Tips

- Define terms close to where they are first explained. Readers who follow the auto-link land right in context.
- Use `list-glossary` for a curated reference section. Use inline `<dfn>` for terms introduced naturally in prose.
- The glossary page filename is based on whether you have a `glossary-terms.md` template in `.stricttools/docs/`. If you do, its content is used as the page body above the auto-generated term list.
- In unified (multi-project) builds, terms from all projects are merged into a single glossary with project attribution.

Next: [Multi-Language Support](../multi-language/) -->
