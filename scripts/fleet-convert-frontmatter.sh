#!/usr/bin/env bash
# Convert every roster project's frontmatter to TOML with the converter in
# this repository, regenerate its generated pages with this repository's
# build of selfdoc, check, and commit.
#
# Usage: scripts/fleet-convert-frontmatter.sh (--dry-run | --apply) [--skip slug,slug]
#
# --dry-run runs the converter's dry run in every project and prints its
# counts. --apply converts, runs `bin/selfdoc gen` (which commits the
# generated pages itself), requires `bin/selfdoc check` at zero errors,
# commits the hand-authored pages with safegit, and adds a non-user-facing
# changelog entry. A project that refuses a file, or whose check reports an
# error, is left as it stands and reported; the loop continues.
set -uo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
converter="$here/scripts/convert-frontmatter-to-toml.py"
selfdoc="$here/bin/selfdoc"
[ -x "$selfdoc" ] || { echo "build $selfdoc first: go build -o bin/selfdoc ." >&2; exit 2; }

mode="${1:-}"
case "$mode" in
  --dry-run|--apply) ;;
  *) echo "usage: $0 (--dry-run | --apply) [--skip slug,slug]" >&2; exit 2 ;;
esac
shift
skip=","
while [ $# -gt 0 ]; do
  case "$1" in
    --skip) skip=",${2:-},"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

cache_repo="smm-h/selfdoc-cache"
projects_root="${PROJECTS_ROOT:-$HOME/Projects/stricttools/tools}"
roster=$(gh api "repos/$cache_repo/contents/roster.toml" --jq .content | base64 -d)
home=$(printf '%s\n' "$roster" | sed -n 's/^home = "\(.*\)"/\1/p')

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
  if [ -n "$(git -C "$dir" status --porcelain)" ]; then
    echo "$slug: working tree not clean; skipped"; failed+=("$slug"); continue
  fi
  echo "== $slug ($dir)"
  plan=$("$converter" --project "$dir" --dry-run --include-generated 2>&1 | tail -1)
  echo "   $plan"
  [ "$mode" = "--dry-run" ] && continue
  count=$(printf '%s' "$plan" | sed -n 's/^\([0-9]*\) file(s) to convert, 0 refused\.$/\1/p')
  if [ -z "$count" ]; then echo "   refusals or unreadable plan; left alone"; failed+=("$slug"); continue; fi
  if [ "$count" = 0 ]; then echo "   nothing to convert"; continue; fi
  if ! "$converter" --project "$dir" --apply --include-generated --expect-files "$count" >/dev/null 2>&1; then
    echo "   converter failed"; failed+=("$slug"); continue
  fi
  (cd "$dir" && "$selfdoc" gen >/dev/null 2>&1) || { echo "   gen failed"; failed+=("$slug"); continue; }
  errors=$(cd "$dir" && "$selfdoc" check 2>&1 | grep -c "error:")
  if [ "$errors" != 0 ]; then
    echo "   check reports $errors error(s); left uncommitted for review"; failed+=("$slug"); continue
  fi
  files=$(git -C "$dir" status --porcelain | awk '{print $2}')
  if [ -n "$files" ]; then
    (cd "$dir" && safegit commit -m "docs: frontmatter is TOML between +++ fences" -- $files >/dev/null 2>&1 \
      && rlsbl changelog add --commits HEAD --no-user-facing >/dev/null 2>&1) \
      || { echo "   commit or changelog failed"; failed+=("$slug"); continue; }
  fi
  echo "   converted $count file(s), committed"
done < <(printf '%s\n' "$roster" | sed -n 's/^slug = "\(.*\)"/\1/p')

echo
if [ ${#failed[@]} -gt 0 ]; then echo "needs attention: ${failed[*]}"; exit 1; fi
echo "all done"
