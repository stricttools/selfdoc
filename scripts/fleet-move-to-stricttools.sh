#!/usr/bin/env bash
# Move every roster project onto the stricttools/ layout with the move
# script in this repository, using this repository's build of selfdoc for
# everything after the move and the installed (previous) selfdoc for the one
# build the move needs before it: the sitemap of the old layout, always
# rebuilt rather than read from an earlier build's output.
#
# Usage: scripts/fleet-move-to-stricttools.sh (--dry-run | --apply) [--skip slug,slug] [--old-selfdoc <path>]
#
# --old-selfdoc names a selfdoc binary that still reads the old layout, for
# the pre-move build; it defaults to the installed `selfdoc`, which is wrong
# once the new-layout release is installed.
#
# --dry-run runs the move script's dry run in every project not yet moved
# and prints its plan. --apply, per project: archives regenerable caches the
# move would refuse (the version cache, bytecode caches), builds once with
# the installed selfdoc so the old layout's sitemap exists, creates
# `stricttools/` (the person's step, done here on the person's order), runs
# the move script with --apply (two commits), then in every project on the
# layout: builds with this repository's selfdoc, compares the URL set before
# and after, archives the old build output, regenerates, checks, validates
# the layout, commits what the build and gen rewrote, and covers the move
# commits with a non-user-facing changelog entry.
set -uo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
mover="$here/scripts/move-to-stricttools-layout.py"
selfdoc="$here/bin/selfdoc"
installed="$(command -v selfdoc || true)"
[ -x "$selfdoc" ] || { echo "build $selfdoc first: go build -o bin/selfdoc ." >&2; exit 2; }

mode="${1:-}"
case "$mode" in
  --dry-run|--apply) ;;
  *) echo "usage: $0 (--dry-run | --apply) [--skip slug,slug] [--old-selfdoc <path>]" >&2; exit 2 ;;
esac
shift
skip=","
while [ $# -gt 0 ]; do
  case "$1" in
    --skip) skip=",${2:-},"; shift 2 ;;
    --old-selfdoc) installed="${2:-}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

cache_repo="smm-h/selfdoc-cache"
projects_root="${PROJECTS_ROOT:-$HOME/Projects/stricttools/tools}"
roster=$(gh api "repos/$cache_repo/contents/roster.toml" --jq .content | base64 -d)

archive() { saferm delete -r --on-error abort --description "$1" "$2" >/dev/null 2>&1; }

url_set() { grep -o '<loc>[^<]*</loc>' "$1" 2>/dev/null | sort -u; }

post_move() {
  local dir="$1" slug="$2"
  # The move carries every tracked file out of .selfdoc/; what stays behind
  # is untracked, regenerable content (the version cache), and selfdoc
  # refuses a repository that still has the directory at all.
  if [ -d "$dir/.selfdoc" ]; then
    if [ -n "$(git -C "$dir" ls-files .selfdoc)" ]; then echo "   .selfdoc/ still holds tracked files"; return 1; fi
    archive "the retired .selfdoc/ directory after the layout move; only the regenerable version cache remained in it" "$dir/.selfdoc"
  fi
  (cd "$dir" && "$selfdoc" build >/dev/null 2>&1) || { echo "   build failed on the new layout"; (cd "$dir" && "$selfdoc" build 2>&1 | grep -v warning: | tail -2 | sed 's/^/     /'); return 1; }
  local before="$dir/stricttools/.docs-cache/sitemap-before-move.xml" after="$dir/stricttools/.docs-cache/build/sitemap.xml"
  if [ -f "$before" ]; then
    if [ "$(url_set "$before")" != "$(url_set "$after")" ]; then
      echo "   URL set changed by the move:"; diff <(url_set "$before") <(url_set "$after") | head -6 | sed 's/^/     /'; return 1
    fi
    archive "the pre-move sitemap capture, verified equal to the post-move sitemap" "$before"
  fi
  [ -e "$dir/docs/_build" ] && archive "old build output; the build now writes under stricttools/.docs-cache/" "$dir/docs/_build"
  [ -d "$dir/docs" ] && [ -z "$(ls -A "$dir/docs")" ] && rmdir "$dir/docs"
  (cd "$dir" && "$selfdoc" gen >/dev/null 2>&1) || { echo "   gen failed"; return 1; }
  local errors; errors=$(cd "$dir" && "$selfdoc" check 2>&1 | grep -c "error:")
  [ "$errors" = 0 ] || { echo "   check reports $errors error(s)"; (cd "$dir" && "$selfdoc" check 2>&1 | grep "error:" | head -3 | sed 's/^/     /'); return 1; }
  (cd "$dir" && "$selfdoc" layout validate >/dev/null 2>&1) || { echo "   layout validate failed"; (cd "$dir" && "$selfdoc" layout validate 2>&1 | head -3 | sed 's/^/     /'); return 1; }
  local files; files=$(git -C "$dir" status --porcelain | awk '{print $2}')
  if [ -n "$files" ]; then
    (cd "$dir" && safegit commit -m "layout: the derived ignore file and the regenerated pages on the new layout" -- $files >/dev/null 2>&1) || { echo "   commit failed"; return 1; }
  fi
  local hashes; hashes=$(git -C "$dir" log --format=%h --grep='^layout: ' -3 | tr '\n' ',' | sed 's/,$//')
  if [ -n "$hashes" ]; then
    (cd "$dir" && rlsbl changelog add --commits "$hashes" --no-user-facing >/dev/null 2>&1) || echo "   (no changelog entry: not an rlsbl member or already covered)"
  fi
  echo "   on the layout, verified, committed"
}

failed=()
while IFS= read -r slug; do
  [ -z "$slug" ] && continue
  case "$skip" in *",$slug,"*) echo "$slug: skipped on request"; continue ;; esac
  dir=""
  for candidate in "$projects_root"/*; do
    if [ -d "$candidate/.git" ] && [ "$(basename "$candidate" | tr 'A-Z' 'a-z')" = "$slug" ]; then
      dir="$candidate"; break
    fi
  done
  [ -z "$dir" ] && { echo "$slug: no local checkout; skipped"; continue; }
  echo "== $slug ($dir)"
  if [ -d "$dir/stricttools/docs" ]; then
    [ "$mode" = "--dry-run" ] && { echo "   already moved; --apply would verify and commit"; continue; }
    post_move "$dir" "$slug" || failed+=("$slug")
    continue
  fi
  if [ -n "$(git -C "$dir" status --porcelain)" ]; then
    echo "   working tree not clean; skipped"; failed+=("$slug"); continue
  fi
  if [ "$mode" = "--dry-run" ]; then
    mkdir -p "$dir/stricttools"
    "$mover" --project "$dir" --selfdoc "$selfdoc" --dry-run 2>&1 | tail -3 | sed 's/^/   /'
    rmdir "$dir/stricttools" 2>/dev/null
    continue
  fi
  # The URL set the move must preserve is the one the old layout builds
  # NOW, never a sitemap an earlier build left behind: an old capture makes
  # every page added or removed since then look like a consequence of the
  # move. So the old layout is always rebuilt first.
  [ -n "$installed" ] || { echo "   no installed selfdoc to build the old layout's sitemap"; failed+=("$slug"); continue; }
  (cd "$dir" && "$installed" build >/dev/null 2>&1) || { echo "   the old-layout build failed"; (cd "$dir" && "$installed" build 2>&1 | grep -v warning: | tail -2 | sed 's/^/     /'); failed+=("$slug"); continue; }
  # After the build: a build runs the project's Python directives, which
  # recreates their bytecode caches, so the caches are archived last.
  [ -e "$dir/.selfdoc/cache" ] && archive "pre-move cleanup: the regenerable version cache" "$dir/.selfdoc/cache"
  while IFS= read -r pyc; do [ -n "$pyc" ] && archive "pre-move cleanup: regenerable Python bytecode cache" "$pyc"; done < <(find "$dir/docs" "$dir/.selfdoc" -type d -name __pycache__ 2>/dev/null)
  mkdir -p "$dir/stricttools"
  if ! "$mover" --project "$dir" --selfdoc "$selfdoc" --apply > "$dir/move-to-stricttools.log.local-only" 2>&1; then
    if [ ! -d "$dir/stricttools/docs" ]; then echo "   move refused; see $dir/move-to-stricttools.log.local-only"; tail -2 "$dir/move-to-stricttools.log.local-only" | sed 's/^/     /'; failed+=("$slug"); continue; fi
  fi
  post_move "$dir" "$slug" || failed+=("$slug")
done < <(printf '%s\n' "$roster" | sed -n 's/^slug = "\(.*\)"/\1/p')

echo
if [ ${#failed[@]} -gt 0 ]; then echo "needs attention: ${failed[*]}"; exit 1; fi
echo "all done"
