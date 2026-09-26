#!/usr/bin/env python3
"""Rewrite the paths of selfdoc's previous layout into the current one, in text.

The previous layout kept every function directory under a hidden
.stricttools/ root, each under its bare function name. The current layout
keeps them under a visible stricttools/ root, with a generated directory's
name behind a dot:

    .stricttools/docs/        -> stricttools/docs/
    .stricttools/posts/       -> stricttools/posts/
    .stricttools/vocabulary/  -> stricttools/vocabulary/
    .stricttools/docs-state/  -> stricttools/.docs-state/
    .stricttools/docs-cache/  -> stricttools/.docs-cache/

This is a text rewrite over tracked files (source, tests, fixtures, docs), not
a move: `selfdoc layout migrate` moves a repository's own directories. Both
spellings a path takes in Go source are covered -- the slash form
(".stricttools/docs-state/x") and the joined form
(filepath.Join(dir, ".stricttools", "docs-state")).

Usage:

    scripts/rewrite-layout-paths.py --dry-run [--expect-files N] [--expect-replacements N] -- <pathspec>...
    scripts/rewrite-layout-paths.py --apply   [--expect-files N] [--expect-replacements N] -- <pathspec>...

The pathspecs are handed to `git ls-files`, so only tracked files are touched.
A dry run prints every changed line; an apply run refuses unless the counts
it finds equal the ones stated, when stated.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

GENERATED = ("docs-state", "docs-cache")

# A mention starts where the previous character cannot continue a name, so
# "foo.stricttools" is left alone while "project/.stricttools" is rewritten.
BOUNDARY = r"(?<![\w.-])"

RULES: list[tuple[re.Pattern[str], str]] = []
for name in GENERATED:
    # Slash form: .stricttools/docs-state -> stricttools/.docs-state
    RULES.append((re.compile(BOUNDARY + re.escape(".stricttools/" + name) + r"(?![\w-])"),
                  "stricttools/." + name))
    # Joined form: ".stricttools", "docs-state" -> "stricttools", ".docs-state"
    RULES.append((re.compile(r'"\.stricttools",(\s*)"' + re.escape(name) + '"'),
                  r'"stricttools",\1".' + name + '"'))
# Everything else under the root keeps its name; the root loses its dot.
RULES.append((re.compile(BOUNDARY + r"\.stricttools(?![\w.-])"), "stricttools"))


def rewrite(text: str) -> tuple[str, int]:
    total = 0
    for pattern, replacement in RULES:
        text, count = pattern.subn(replacement, text)
        total += count
    return text, total


def tracked(pathspecs: list[str]) -> list[str]:
    result = subprocess.run(["git", "ls-files", "-z", "--", *pathspecs],
                            capture_output=True, text=True, check=True)
    return [name for name in result.stdout.split("\0") if name]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true", help="print every change, write nothing")
    mode.add_argument("--apply", action="store_true", help="write the changes")
    parser.add_argument("--expect-files", type=int, default=None,
                        help="refuse unless exactly this many files change")
    parser.add_argument("--expect-replacements", type=int, default=None,
                        help="refuse unless exactly this many replacements are made")
    parser.add_argument("pathspecs", nargs="+", help="git pathspecs naming the files to rewrite")
    args = parser.parse_args()

    planned = []
    replacements = 0
    for name in tracked(args.pathspecs):
        path = Path(name)
        if not path.is_file():
            continue
        try:
            before = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        after, count = rewrite(before)
        if count:
            planned.append((path, before, after, count))
            replacements += count

    for path, before, after, count in planned:
        print(f"{path}: {count} replacement(s)")
        if args.dry_run:
            for old, new in zip(before.split("\n"), after.split("\n")):
                if old != new:
                    print(f"  - {old.strip()}")
                    print(f"  + {new.strip()}")
    print(f"files: {len(planned)}, replacements: {replacements}")

    for label, found, expected in (("files", len(planned), args.expect_files),
                                   ("replacements", replacements, args.expect_replacements)):
        if expected is not None and found != expected:
            print(f"refused: found {found} {label}, expected {expected}", file=sys.stderr)
            return 1

    if args.dry_run:
        print("dry run: nothing was written.")
        return 0
    for path, _, after, _ in planned:
        mode = path.stat().st_mode
        if not mode & 0o200:
            path.chmod(mode | 0o200)
        path.write_text(after, encoding="utf-8")
        if not mode & 0o200:
            path.chmod(mode)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
