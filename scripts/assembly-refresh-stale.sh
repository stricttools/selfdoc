#!/usr/bin/env bash
# Re-dispatch the assembly rebuild for every roster project whose cached
# docs lag its latest release tag.
#
# Usage: scripts/assembly-refresh-stale.sh (--dry-run | --apply) [--all] [--skip slug,slug]
#
# --all treats every roster project as stale, for a toolchain change that
# every published subtree must pick up (a new page chrome, a new section).
#
# --dry-run prints the comparison and the dispatches it would make.
# --apply dispatches them one at a time, waiting for each deploy run to
# finish before the next, so the deploys never race on the cache repository.
# --skip names slugs to leave alone (a project whose latest tag is not the
# version its docs should be built at, for example).
set -euo pipefail

mode="${1:-}"
case "$mode" in
  --dry-run|--apply) ;;
  *) echo "usage: $0 (--dry-run | --apply) [--all] [--skip slug,slug]" >&2; exit 2 ;;
esac
shift
all=0
skip=","
while [ $# -gt 0 ]; do
  case "$1" in
    --all) all=1; shift ;;
    --skip) skip=",${2:-},"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

cache_repo="smm-h/selfdoc-cache"
projects_root="${PROJECTS_ROOT:-$HOME/Projects/stricttools/tools}"

roster=$(gh api "repos/$cache_repo/contents/roster.toml" --jq .content | base64 -d)
home=$(printf '%s\n' "$roster" | sed -n 's/^home = "\(.*\)"/\1/p')

stale=()
while IFS= read -r slug; do
  [ -z "$slug" ] && continue
  [ "$slug" = "$home" ] && continue
  case "$skip" in *",$slug,"*) echo "$slug: skipped on request"; continue ;; esac
  dir=""
  for candidate in "$projects_root/$slug" "$projects_root"/*; do
    if [ -d "$candidate/.git" ] && [ "$(basename "$candidate" | tr 'A-Z' 'a-z')" = "$slug" ]; then
      dir="$candidate"; break
    fi
  done
  if [ -z "$dir" ]; then
    echo "$slug: no local checkout found under $projects_root; skipped" >&2
    continue
  fi
  cached=$(gh api "repos/$cache_repo/contents/manifests/$slug.json" --jq .content 2>/dev/null | base64 -d | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version",""))' || true)
  latest=$(git -C "$dir" describe --tags --abbrev=0 --match 'v*' 2>/dev/null | sed 's/^v//' || true)
  if [ -z "$latest" ]; then
    echo "$slug: no v* tag locally; skipped" >&2
    continue
  fi
  if [ "$cached" = "$latest" ] && [ "$all" = 0 ]; then
    echo "$slug: cache $cached = tag $latest"
  elif [ "$cached" = "$latest" ]; then
    echo "$slug: cache $cached = tag $latest  (refreshing anyway)"
    stale+=("$slug:$dir")
  else
    echo "$slug: cache ${cached:-<none>} -> tag $latest  (stale)"
    stale+=("$slug:$dir")
  fi
done < <(printf '%s\n' "$roster" | sed -n 's/^slug = "\(.*\)"/\1/p')

echo
echo "${#stale[@]} stale project(s)."
[ "$mode" = "--dry-run" ] && exit 0

for entry in "${stale[@]}"; do
  slug="${entry%%:*}"; dir="${entry#*:}"
  echo "== $slug: dispatching from $dir"
  (cd "$dir" && selfdoc assembly push)
  sleep 20
  run=$(gh run list --repo "$cache_repo" --limit 1 --json databaseId -q '.[0].databaseId')
  echo "== $slug: watching run $run"
  gh run watch "$run" --repo "$cache_repo" --exit-status >/dev/null && echo "== $slug: deploy succeeded" || echo "== $slug: deploy FAILED (run $run)"
done
