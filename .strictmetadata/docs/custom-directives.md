+++
title = "Custom Directives"
description = "Write custom directives as scripts: the resolve(attrs, config, body) interface, the out-of-process contract, registration in selfdoc.json, and two worked examples."
nav_group = "Guides"
nav_order = 9
+++

# Custom Directives

selfdoc's built-in directives cover common patterns like module references and schema tables, but sometimes you need something project-specific. Custom directives let you write a script that generates Markdown content at build time, driven by your own logic.

## How It Works

A custom directive is a script that receives the directive's attributes, the full project config and its body, and prints the Markdown that replaces the directive. You register it in `selfdoc.json`, then use it in your Markdown templates just like a built-in directive.

The script runs **out of process**, which is what lets a Go binary keep a Python contract:

- A script whose path ends in `.py` is loaded and called by an embedded Python driver. selfdoc runs `python3`, hands it the driver and your script's path, and writes one JSON object on the script's standard input: `{"attrs": {...}, "config": {...}, "body": [...], "base_dir": "..."}`. The driver imports your script, calls `resolve(attrs, config, body)`, and prints what it returns.
- Any other script is executed directly, with the same JSON object on standard input. Whatever it prints on standard output replaces the directive, so a directive can be written in any language that can read JSON and print text.

Either way, a failure is a hard error that stops the build: a script that cannot be found or imported, one with no callable `resolve`, one that raises, one that exits non-zero, and a machine with no `python3` are each reported by name. A published page reading "custom directive failed" where its API reference belongs is as easy to miss as any other paragraph, so nothing is ever substituted inline.

The run is declared as a read through the effects handle, so `--dry-run` resolves custom directives like any other run.

## Configuration

Map directive names to script paths in the `directives` section of `selfdoc.json`. Each entry associates a directive name (used in your Markdown templates) with a script file that contains the resolution logic. Paths are relative to the project root:

```json
{
  "directives": {
    "changelog": "scripts/changelog-directive.py",
    "endpoints": "scripts/endpoints-directive.py"
  }
}
```

Each Python script must define a `resolve` function at module level.

## The resolve Interface

```python
def resolve(attrs, config, body):
    """Generate Markdown content for this directive.

    Args:
        attrs: Dict of key-value attributes from the directive marker.
                Always a dict -- empty when the directive declared none.
        config: The full selfdoc.json config dict.
        body: List of body lines. Always a list -- empty for a
                self-closing directive.

    Returns:
        A string of Markdown content that replaces the directive marker.
    """
```

- **attrs** -- a `dict[str, str]` parsed from the directive's inline attributes. For `:-: my-directive path="foo" target="bar"`, this is `{"path": "foo", "target": "bar"}`. It is never `None`.
- **config** -- the full loaded `selfdoc.json` as a Python dict. Useful for reading `source`, `base_url`, or any custom fields you add.
- **body** -- for block directives (the `:<:` / `:>:` syntax), the lines between the opening and closing markers, as a list of strings. For self-closing directives (`:-:`) it is the empty list. It is never `None`.

Return a string of Markdown. selfdoc processes it through the normal Markdown-to-HTML pipeline, so you get headings, tables, code blocks, and inline formatting for free.

## Example: a changelog section

A directive that pulls one version's section out of the project's `CHANGELOG.md`:

```python
"""Render one version's section of CHANGELOG.md."""

import os


def resolve(attrs, config, body):
    version = attrs.get("path")
    if not version:
        raise ValueError("changelog directive needs path=\"<version>\"")

    with open("CHANGELOG.md", encoding="utf-8") as handle:
        lines = handle.read().splitlines()

    wanted = f"## {version}"
    collected = []
    inside = False
    for line in lines:
        if line.startswith("## "):
            if inside:
                break
            inside = line.strip() == wanted
            continue
        if inside:
            collected.append(line)

    if not collected:
        raise ValueError(f"CHANGELOG.md has no section for {version}")
    return "\n".join(collected).strip()
```

Register it and use it:

```markdown
:-: changelog path="v1.0.0"
```

Raising is the right move when the directive cannot do its job: the build stops and names the directive and the script, instead of publishing a page with a hole in it.

## Example: a table from a data file

A directive that reads a JSON file the project already maintains and renders it as a table. The body lines select which columns to render, so the same script serves several pages:

```python
"""Render endpoints.json as a Markdown table."""

import json


def resolve(attrs, config, body):
    path = attrs.get("path", "endpoints.json")
    with open(path, encoding="utf-8") as handle:
        rows = json.load(handle)

    # body is always a list; empty means "every column".
    columns = [line.strip() for line in body if line.strip()]
    if not columns:
        columns = sorted({key for row in rows for key in row})

    out = ["| " + " | ".join(columns) + " |",
           "| " + " | ".join("---" for _ in columns) + " |"]
    for row in rows:
        out.append("| " + " | ".join(str(row.get(c, "")) for c in columns) + " |")
    return "\n".join(out)
```

```markdown
:<: endpoints path="api/endpoints.json"
:=:
::: method
::: path
::: description
:>:
```

## Tips

- **Return Markdown, not HTML.** selfdoc processes your output through its full Markdown pipeline, so you get syntax highlighting, heading anchors, and table styling for free.
- **Use config for project info.** The `config` parameter gives you access to `base_url`, `source`, and any custom keys you add to `selfdoc.json`.
- **Body parameter for block directives.** If your directive uses block syntax (`:<:` / `:>:`), the lines between the markers arrive as `body`. This is useful for directives that transform or augment author-provided content.
- **Fail loudly.** Raise, or exit non-zero with a reason on standard error. selfdoc turns either into a build error naming the directive and the script.
- **Keep scripts in `scripts/`.** The convention is to put directive scripts in a `scripts/` directory at the project root, but any path relative to the project root works.
- **Where a custom name wins.** The dispatch order is content directives, then custom directives, then the language extractors. So a custom directive overrides a code-extraction name such as `ref` or `table-schema`, but it cannot override a content directive such as `callout-note` or `list-glossary` -- those are answered before your script is reached.

> [!WARNING]
> Custom directive scripts run as real programs during the build, with full access to the filesystem. Only register scripts you trust.

Next: [Check Guide](../check-guide/)
