#!/usr/bin/env python3
"""Rewrite hand-authored frontmatter blocks from the retired fence to TOML.

selfdoc's frontmatter is TOML between "+++" fences, validated against a
declared key registry. Documents written before that carry a "---" block in a
hand-rolled "key: value" dialect, which selfdoc now refuses by name. This
script is the one-way conversion, run per project by hand.

It converts hand-authored pages and posts by default. A generated page (one
declaring `generated: true`) is left alone unless --include-generated is
passed: those are rewritten by `selfdoc gen` once the emitters write TOML, but
gen reads the existing page first to keep a handwritten description, and it
cannot read a retired block, so a project converts its generated pages once
right before that first gen.

Two things it will not do, both of which it refuses by naming the file and the
line rather than guessing:

  * a value it cannot give a certain TOML spelling -- a date that is not
    YYYY-MM-DD, a boolean key holding something other than true/false, an
    integer key holding something else;
  * a key the registry does not declare, including `project`, which nothing
    reads and which the schema refuses.

The key `order` is rewritten to `nav_order`, which is now the one sort key.
A page at the docs root sorted by `order` and a page in a subdirectory sorted
by `nav_order`, so a document carrying both keeps whichever of the two
governed it and drops the other.

Usage:

    scripts/convert-frontmatter-to-toml.py --dry-run --expect-files 29
    scripts/convert-frontmatter-to-toml.py --apply --expect-files 29

    scripts/convert-frontmatter-to-toml.py --dry-run --path stricttools/docs

Exactly one of --dry-run and --apply is required. --expect-files asserts how
many files the run changes; a different number is a hard error and nothing is
written.
"""

from __future__ import annotations

import argparse
import difflib
import json
import os
import re
import stat
import sys
from pathlib import Path

# The key registry, mirroring .strictspec/frontmatter.schema.toml. A key that is
# not here has no TOML spelling this script may invent, so it is refused.
STRING_KEYS = {
    "title", "description", "slug", "nav_group", "type", "schema", "locale",
    "version", "prev_version", "bump_type", "release_url",
}
DATE_KEYS = {"date", "updated"}
BOOL_KEYS = {
    "draft", "directives", "versioned", "feed", "generated", "seeded",
    "auto_steps", "auto_api", "glossary_links",
}
INTEGER_KEYS = {"nav_order"}
ARRAY_KEYS = {"tags", "registry_urls"}

# Keys the schema refuses outright, each with what to do instead.
REFUSED_KEYS = {
    "project": "nothing reads it and the schema refuses it; delete the line",
    "document_kind": "the frontmatter reader supplies it; delete the line",
    "format_version": "the frontmatter reader supplies it; delete the line",
}

RETIRED_FENCE = "---"
FENCE = "+++"
LOCAL_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
INTEGER = re.compile(r"^[+-]?\d+$")

SKIP_DIRS = {"_build", "node_modules", ".git", ".venv", "venv", "dist", "__pycache__"}


class Refusal(Exception):
    """A document this script will not convert, with its coordinates."""

    def __init__(self, path: Path, line: int, message: str):
        super().__init__(f"{path}:{line}: {message}")
        self.path = path
        self.line = line


def toml_string(value: str) -> str:
    """value as a TOML basic string, escaped the way TOML escapes."""
    out = ['"']
    for char in value:
        if char == "\\":
            out.append("\\\\")
        elif char == '"':
            out.append('\\"')
        elif char == "\b":
            out.append("\\b")
        elif char == "\t":
            out.append("\\t")
        elif char == "\n":
            out.append("\\n")
        elif char == "\f":
            out.append("\\f")
        elif char == "\r":
            out.append("\\r")
        elif ord(char) < 0x20 or ord(char) == 0x7F:
            out.append(f"\\u{ord(char):04x}")
        else:
            out.append(char)
    out.append('"')
    return "".join(out)


def unwrap(value: str) -> str:
    """One pair of wrapping quotes removed, as the retired reader removed it.

    The retired dialect stripped a wrapping pair BEFORE interpreting anything
    else, so `tags: "[a, b]"` was still a list and `draft: "true"` was still a
    boolean. Conversion has to read values the same way or it changes meanings.
    """
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    return value


def bracket_items(value: str) -> list[str] | None:
    """The items of a `[a, b, c]` list, or None when value is not one."""
    if not (value.startswith("[") and value.endswith("]")):
        return None
    return [item.strip() for item in value[1:-1].split(",") if item.strip()]


def convert_value(path: Path, line: int, key: str, raw: str) -> str:
    """The TOML spelling of one retired-dialect value."""
    value = unwrap(raw)
    if key in ARRAY_KEYS:
        items = bracket_items(value)
        if items is None:
            # A bare value where a list belongs: the retired readers coerced
            # it to a one-item list, which is a meaning this script may carry
            # over because the coercion was total.
            items = [value] if value else []
        return "[" + ", ".join(toml_string(unwrap(item)) for item in items) + "]"
    if key in DATE_KEYS:
        if not LOCAL_DATE.match(value):
            raise Refusal(path, line,
                          f"{key} is {value!r}, which is not a YYYY-MM-DD date; "
                          f"rewrite it by hand and rerun")
        return value
    if key in BOOL_KEYS:
        if value.lower() not in ("true", "false"):
            raise Refusal(path, line,
                          f"{key} is {value!r}, and the schema types it as a "
                          f"boolean; write true or false and rerun")
        return value.lower()
    if key in INTEGER_KEYS:
        if not INTEGER.match(value):
            raise Refusal(path, line,
                          f"{key} is {value!r}, and the schema types it as an "
                          f"integer; write a whole number and rerun")
        return str(int(value))
    if key in STRING_KEYS:
        return toml_string(value)
    raise Refusal(path, line, f"{key} is not a declared frontmatter key")


def read_block(text: str) -> tuple[list[str], list[str]] | None:
    """A retired block's lines and the document's remaining lines."""
    lines = text.split("\n")
    if not lines or lines[0].strip() != RETIRED_FENCE:
        return None
    for index in range(1, len(lines)):
        if lines[index].strip() == RETIRED_FENCE:
            return lines[1:index], lines[index + 1:]
    return None


def collapse_order(path: Path, pairs: list[tuple[int, str, str]], top_level: bool):
    """Fold `order` into `nav_order`, the one sort key.

    `order` sorted the pages at the docs root and `nav_order` sorted the pages
    inside a group, so exactly one of the two governed any given page. The
    surviving key takes the value that governed, and the other line goes.
    """
    has_order = any(key == "order" for _, key, _ in pairs)
    has_nav_order = any(key == "nav_order" for _, key, _ in pairs)
    if not has_order:
        return pairs, ""
    order_value = next(value for _, key, value in pairs if key == "order")
    if not has_nav_order:
        renamed = [(line, "nav_order" if key == "order" else key, value)
                   for line, key, value in pairs]
        return renamed, "order -> nav_order"
    if top_level:
        kept = [(line, key, order_value if key == "nav_order" else value)
                for line, key, value in pairs if key != "order"]
        return kept, f"order {order_value} kept as nav_order (page sorts at the docs root)"
    kept = [(line, key, value) for line, key, value in pairs if key != "order"]
    nav_value = next(value for _, key, value in pairs if key == "nav_order")
    return kept, f"order dropped, nav_order {nav_value} kept (page sorts inside its group)"


def convert(path: Path, text: str, top_level: bool,
            include_generated: bool = False) -> tuple[str, str] | None:
    """The converted document and a note on what the collapse did, or None.

    None means the document needs no conversion: it carries no retired block,
    or it is a generated page this script leaves to `selfdoc gen` unless
    include_generated asks for it (see --include-generated).
    """
    block = read_block(text)
    if block is None:
        return None
    block_lines, rest = block

    pairs: list[tuple[int, str, str]] = []
    for offset, raw in enumerate(block_lines):
        line_number = offset + 2  # the fence is line 1
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if ":" not in stripped:
            raise Refusal(path, line_number,
                          f"{stripped!r} carries no key separator; the retired "
                          f"reader ignored it silently, so decide by hand what it meant")
        key, _, value = stripped.partition(":")
        key, value = key.strip(), value.strip()
        if key in REFUSED_KEYS:
            raise Refusal(path, line_number, f"{key}: {REFUSED_KEYS[key]}")
        pairs.append((line_number, key, value))

    if not include_generated and any(
            key == "generated" and unwrap(value).lower() == "true"
            for _, key, value in pairs):
        return None

    pairs, note = collapse_order(path, pairs, top_level)

    converted = [FENCE]
    for line_number, key, value in pairs:
        converted.append(f"{key} = {convert_value(path, line_number, key, value)}")
    converted.append(FENCE)
    return "\n".join(converted + rest), note


def block_diff(path: Path, before: str, after: str) -> str:
    """A unified diff of the two frontmatter blocks alone."""

    def head(text: str) -> list[str]:
        lines = text.split("\n")
        fence = lines[0].strip()
        for index in range(1, len(lines)):
            if lines[index].strip() == fence:
                return lines[:index + 1]
        return lines

    return "".join(difflib.unified_diff(
        [line + "\n" for line in head(before)],
        [line + "\n" for line in head(after)],
        fromfile=f"a/{path}", tofile=f"b/{path}"))


def markdown_files(root: Path):
    """Every .md file under root, in sorted order."""
    if root.is_file():
        yield root
        return
    if not root.is_dir():
        return
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS)
        for name in sorted(filenames):
            if name.endswith(".md"):
                yield Path(dirpath) / name


def configured_paths(project: Path) -> list[Path]:
    """The project's docs directory and its posts directory."""
    docs_rel, posts_rel = "stricttools/docs/", "stricttools/posts/"
    config_path = project / "selfdoc.json"
    if config_path.is_file():
        try:
            config = json.loads(config_path.read_text(encoding="utf-8"))
        except ValueError:
            config = {}
        if isinstance(config.get("docs"), str):
            docs_rel = config["docs"]
        posts = config.get("posts")
        if isinstance(posts, dict) and isinstance(posts.get("dir"), str):
            posts_rel = posts["dir"]
    return [project / docs_rel, project / posts_rel]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true",
                      help="print the diff of every block that would change and write nothing")
    mode.add_argument("--apply", action="store_true",
                      help="write the converted documents")
    parser.add_argument("--project", default=".", help="the project root (default: .)")
    parser.add_argument("--path", action="append", default=[],
                        help="a file or directory to convert; repeatable. "
                             "Defaults to the project's docs and posts directories.")
    parser.add_argument("--include-generated", action="store_true",
                        help="also convert generated pages (`generated = true`). "
                             "`selfdoc gen` preserves a generated page's handwritten "
                             "description by reading the page, which it cannot do "
                             "through a retired block, so a project converts its "
                             "generated pages once, right before the first gen with "
                             "the TOML-writing emitters, and gen rewrites them after")
    parser.add_argument("--expect-files", type=int, default=None,
                        help="assert how many files the run changes")
    args = parser.parse_args()

    project = Path(args.project).resolve()
    roots = [Path(p) for p in args.path] if args.path else configured_paths(project)
    docs_root = configured_paths(project)[0].resolve()

    changed: list[tuple[Path, str, str, str]] = []
    refusals: list[str] = []
    for root in roots:
        for path in markdown_files(root):
            try:
                text = path.read_text(encoding="utf-8")
            except (OSError, UnicodeDecodeError) as exc:
                refusals.append(f"{path}:1: cannot read: {exc}")
                continue
            try:
                resolved = path.resolve()
                top_level = (resolved.parent == docs_root)
                result = convert(path, text, top_level, args.include_generated)
            except Refusal as refusal:
                refusals.append(str(refusal))
                continue
            if result is None:
                continue
            converted, note = result
            if converted != text:
                changed.append((path, text, converted, note))

    for path, before, after, note in changed:
        print(block_diff(path, before, after), end="")
        if note:
            print(f"# {path}: {note}")

    print(f"\n{len(changed)} file(s) to convert, {len(refusals)} refused.")
    for refusal in refusals:
        print(f"REFUSED {refusal}", file=sys.stderr)

    if refusals:
        print("\nNothing was written: every refusal above has to be resolved first.",
              file=sys.stderr)
        return 1
    if args.expect_files is not None and args.expect_files != len(changed):
        print(f"\nExpected {args.expect_files} file(s), found {len(changed)}. "
              f"Nothing was written.", file=sys.stderr)
        return 1
    if args.dry_run:
        return 0

    for path, _, after, _ in changed:
        # A generated page is write-protected by `selfdoc gen`; the mode is
        # lifted for the write and put back, so the page stays as protected
        # as gen left it.
        mode = path.stat().st_mode
        path.chmod(mode | stat.S_IWUSR)
        try:
            path.write_text(after, encoding="utf-8")
        finally:
            path.chmod(mode)
    print(f"Converted {len(changed)} file(s).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
