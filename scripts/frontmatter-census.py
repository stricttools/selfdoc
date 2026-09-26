#!/usr/bin/env python3
"""Census of the frontmatter keys the fleet's selfdoc projects actually write.

Walks every sibling project that carries a selfdoc.json, reads the frontmatter
block of every page under its configured docs directory and every post under
its posts directory, and reports which keys appear, how often, in how many
projects, and with which value spellings. It exists so the key registry the
frontmatter schema declares can be checked against what is written rather than
against what the code happens to read.

The reader is deliberately format-agnostic: it accepts both the retired "---"
block and the "+++" TOML block, so the census can be run before, during and
after a conversion and say which files are still on the old fence.

Usage:

    scripts/frontmatter-census.py                 # the fleet beside this repo
    scripts/frontmatter-census.py --root DIR      # another directory of projects
    scripts/frontmatter-census.py --values KEY    # every distinct value of one key
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import Counter, defaultdict
from pathlib import Path

# Directories that never hold authored pages: build output, dependency trees,
# version-control internals and the archive of retired projects.
SKIP_DIRS = {
    "_build", "node_modules", ".git", ".venv", "venv", "dist", "build",
    ".selfdoc-cache", "__pycache__", ".archive",
}


def project_dirs(root: Path) -> list[Path]:
    """Every immediate subdirectory of root that carries a selfdoc.json."""
    found = []
    for entry in sorted(root.iterdir()):
        if not entry.is_dir() or entry.name.startswith("."):
            continue
        if (entry / "selfdoc.json").is_file():
            found.append(entry)
    return found


def configured(project: Path) -> tuple[Path, Path]:
    """The project's docs directory and posts directory."""
    docs_rel, posts_rel = "stricttools/docs/", "stricttools/posts/"
    try:
        config = json.loads((project / "selfdoc.json").read_text(encoding="utf-8"))
    except (OSError, ValueError):
        config = {}
    if isinstance(config.get("docs"), str):
        docs_rel = config["docs"]
    posts = config.get("posts")
    if isinstance(posts, dict) and isinstance(posts.get("dir"), str):
        posts_rel = posts["dir"]
    return project / docs_rel, project / posts_rel


def markdown_files(directory: Path):
    """Every .md file under directory, skipping build output and vendor trees."""
    if not directory.is_dir():
        return
    for dirpath, dirnames, filenames in os.walk(directory):
        dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS)
        for name in sorted(filenames):
            if name.endswith(".md"):
                yield Path(dirpath) / name


def read_block(text: str) -> tuple[str, list[tuple[str, str]]] | None:
    """The fence and the key/value pairs of a document's frontmatter block.

    Returns None for a document with no block at all. The value is the raw
    text after the first colon (old fence) or the first equals sign (TOML
    fence), untrimmed of quoting, because the census reports spellings.
    """
    lines = text.split("\n")
    if not lines:
        return None
    fence = lines[0].strip()
    if fence not in ("---", "+++"):
        return None
    end = None
    for index in range(1, len(lines)):
        if lines[index].strip() == fence:
            end = index
            break
    if end is None:
        return None
    separator = ":" if fence == "---" else "="
    pairs = []
    for raw in lines[1:end]:
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        cut = line.find(separator)
        if cut == -1:
            continue
        pairs.append((line[:cut].strip().strip('"'), line[cut + 1:].strip()))
    return fence, pairs


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root", default=str(Path(__file__).resolve().parent.parent.parent),
        help="directory holding the projects to census (default: this repository's parent)")
    parser.add_argument(
        "--values", default="", metavar="KEY",
        help="print every distinct value spelling of one key instead of the summary")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    if not root.is_dir():
        print(f"error: {root} is not a directory", file=sys.stderr)
        return 1

    key_files = Counter()
    key_projects = defaultdict(set)
    key_values = defaultdict(Counter)
    fence_counts = Counter()
    kind_counts = Counter()
    projects = project_dirs(root)

    for project in projects:
        docs_dir, posts_dir = configured(project)
        for kind, directory in (("page", docs_dir), ("post", posts_dir)):
            for path in markdown_files(directory):
                try:
                    text = path.read_text(encoding="utf-8")
                except (OSError, UnicodeDecodeError):
                    continue
                block = read_block(text)
                if block is None:
                    continue
                fence, pairs = block
                fence_counts[fence] += 1
                generated = any(
                    key == "generated" and value.strip('"').strip("'") == "true"
                    for key, value in pairs)
                kind_counts[(kind, "generated" if generated else "authored")] += 1
                for key, value in pairs:
                    key_files[key] += 1
                    key_projects[key].add(project.name)
                    key_values[key][value] += 1

    if args.values:
        for value, count in key_values[args.values].most_common():
            print(f"{count:6d}  {value}")
        return 0

    print(f"projects with a selfdoc.json: {len(projects)}")
    for fence, count in sorted(fence_counts.items()):
        print(f"blocks fenced with {fence}: {count}")
    for (kind, origin), count in sorted(kind_counts.items()):
        print(f"{kind}s, {origin}: {count}")
    print()
    print(f"{'key':<20} {'files':>7} {'projects':>9}")
    for key, count in sorted(key_files.items(), key=lambda kv: (-kv[1], kv[0])):
        print(f"{key:<20} {count:>7} {len(key_projects[key]):>9}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
