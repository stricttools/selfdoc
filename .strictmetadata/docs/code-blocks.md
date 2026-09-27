+++
title = "Code Blocks"
description = "Syntax highlighting, language icons, line numbers, run buttons, validated examples, annotations, diff highlighting, code tabs, and copy buttons in selfdoc."
nav_group = "Guides"
nav_order = 8
+++

# Code Blocks

Fenced code blocks in selfdoc get automatic syntax highlighting, language labels, and a copy button. Beyond the basics, you can enable line numbers, interactive run buttons, inline annotations, diff highlighting, and automatic language tabs.

## Syntax Highlighting

selfdoc uses [chroma](https://github.com/alecthomas/chroma) for build-time syntax highlighting, supporting hundreds of languages out of the box with no client-side JavaScript required. Highlighting happens during the build, so the output is static HTML with CSS classes. Just specify the language after the opening fence:

````markdown
```python
def greet(name: str) -> str:
    return f"Hello, {name}!"
```
````

If the language is not recognized, the code renders as plain text. A theme declares a light style and a dark style by name, and the generated stylesheet defines each token's colour three times over -- the light scheme, the dark one, and the system fallback for a reader who has recorded no preference -- so the two schemes cannot drift apart rule by rule.

> [!NOTE]
> The highlighter is compiled into the binary. There is nothing to install, and no mode in which a code block comes out plain because a dependency was missing.

## Language Icons

Each code block with a recognized language label gets a small SVG icon next to the language name in the header bar. The icons provide a visual cue that helps readers quickly identify the language, especially in tabbed code blocks showing the same concept in multiple languages. Control the icon style with `code_icons` in your config:

```json
{
  "code_icons": "colorful"
}
```

| Value | Description |
| ----- | ----------- |
| `colorful` (default) | Full-color SVG icons for recognized languages |
| `monochrome` | Single-color icons that match the theme |
| `none` | No icons, just the language text label |

## Line Numbers

Enable line numbers globally or per block to help readers reference specific lines in discussions, reviews, or tutorials. Numbers are rendered via CSS counters in the gutter, so they do not get selected when a user copies code from the block. Enable globally with `line_numbers` in your config:

```json
{
  "line_numbers": true
}
```

Line numbers appear in the gutter via CSS counters, so they are not selectable when copying code. You can also enable line numbers per block using the `line_numbers` annotation in your fence:

````markdown
```python line_numbers
def example():
    pass
```
````

Per-block annotations can also set the starting line number with `line_start`:

````markdown
```python line_numbers line_start=42
    # This line is numbered 42
    return result
```
````

## Run Buttons

Enable interactive run buttons that let readers execute code examples in an online playground without leaving your documentation site. When enabled, each code block with a recognized language gets a "Run" button that opens the code in an appropriate external environment. Enable globally with `run_button` in your config:

```json
{
  "run_button": true
}
```

When enabled, code blocks get a "Run" button that opens the code in an appropriate online playground. You can also enable run buttons per block with the `run` annotation:

````markdown
```python run
print("Hello, world!")
```
````

## Validated Examples

Mark a fenced block `validate` to declare it a complete, self-contained program, and `selfdoc check` will execute it against the validator you configure for that language. This catches the examples that parse cleanly but no longer work -- a renamed function, a changed signature, a removed keyword argument -- which syntax checking alone can never see. The marker is opt-in because most documentation snippets are deliberately partial:

````markdown
```python validate
from mylib import greet

print(greet("world"))
```
````

Validators are declared per language in `selfdoc.json` under `examples`. Each value is a command template, and `{file}` is replaced with the path of the assembled snippet:

```json
{
  "examples": {
    "python": "uv run --directory python python {file}",
    "go": "scripts/validate-example-go.sh {file}"
  }
}
```

Commands run from the project root, with a 60-second timeout. selfdoc writes the block's raw text to a scratch file whose extension names the language (`.py`, `.go`, `.ts`), passes the path in, and reports a non-zero exit as an `EXAMPLE002` error carrying the last five lines of the validator's output. A `validate` marker whose language has no configured command is an `EXAMPLE003` error -- never a silent skip, because a marker that validates nothing is indistinguishable from a passing one.

Blocks without the marker are untouched: they still get the `EXAMPLE001` syntax check and are never executed.

> [!NOTE]
> There is no sandbox. Validators compile, type-check, and register -- they are not a harness for untrusted code, and the snippets they run are your own documentation.

## Annotations

Inline annotations turn numbered comments like `// [1]` or `# [1]` into small clickable badges that reveal explanatory text on hover or focus. This lets you walk readers through code step by step without cluttering the source with long inline comments. Add annotation definitions after the closing fence:

````markdown
```python
config = load_config()  # [1]
result = build(config)  # [2]
```
[1]: Reads selfdoc.json from the current directory
[2]: Resolves directives and writes HTML output
````

The numbered markers in the code become small badge elements. Click or focus a badge to see the annotation text. This is useful for walking through code step by step without cluttering the code itself with long comments.

## Diff Highlighting

Use the `diff` language identifier to get line-level add/remove coloring that visually distinguishes additions from deletions. selfdoc also auto-detects diff-style content in any code block where lines start with `+` or `-` characters. Use it to show migration steps or API changes:

````markdown
```diff
-old_function()
+new_function(with_args=True)
 unchanged_line()
```
````

Lines starting with `+` are highlighted green, lines starting with `-` are highlighted red, and lines starting with a space are neutral. selfdoc also auto-detects diff-style content in any code block where lines start with `+` or `-`.

## Code Tabs

When you place two or more fenced code blocks with different languages next to each other with no content between them, selfdoc automatically groups them into a tabbed interface. This is useful for showing the same concept in multiple languages, alternative installation methods, or platform-specific instructions. Only blocks with language labels participate in tab grouping:

````markdown
```bash
pip install requests
```

```shell
npm install -g typescript
```
````

This renders as a single code block with "bash" and "shell" tabs. The user clicks a tab to see that version. Only blocks with language labels participate in tab grouping -- unlabeled blocks are left standalone.

> [!TIP]
> Code tabs are great for showing the same concept in multiple languages, or alternative installation methods. Put the most common option first.

## Copy Button

Every code block gets a copy-to-clipboard button automatically. No config needed. It appears in the top-right corner of the block on hover and copies the raw code content (without line numbers or annotations).

Next: [Custom Directives](../custom-directives/) -->
