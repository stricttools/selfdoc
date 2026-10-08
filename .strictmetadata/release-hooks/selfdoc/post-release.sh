#!/usr/bin/env bash
# Post-release hook for the selfdoc releasable. Runs after a successful
# release (non-fatal). Environment: RLSBL_VERSION is the released version.
#
# There is no standalone `selfdoc deploy` here any more: the standalone
# per-project Pages site is retired and now only serves 301s into the
# unified assembly site.

set -euo pipefail

echo "Post-release: v$RLSBL_VERSION"

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# Deploy secrets (CF_ACCOUNT_ID, CF_PAGES_API_TOKEN) come from the shared
# env file; rlsbl only sources it for pipeline env validation, not hooks.
if [ -f "$HOME/Projects/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$HOME/Projects/.env"
  set +a
fi

# Push to assembly for unified documentation site
if [ -f selfdoc.json ]; then
  if python3 -c "import json; c=json.load(open('selfdoc.json')); exit(0 if (c.get('assembly') or {}).get('repo') else 1)" 2>/dev/null; then
    # Whatever selfdoc is on PATH was built before this release's version
    # bump, so reinstall from the released tree: both commands below then
    # run the build this release shipped.
    echo "Installing the released selfdoc..."
    go install . || echo "Warning: go install . failed (non-fatal)"

    # The assembly repo's deploy workflow is a generated artifact, and this
    # is the only thing that regenerates the deployed copy. It runs before
    # the dispatch so the deploy this release triggers uses the workflow
    # this release wrote. The pin is the version just released: the module
    # proxy serves that one, and it is the selfdoc whose generator wrote
    # the workflow.
    echo "Syncing the assembly deploy workflow..."
    selfdoc assembly sync-workflow --pin-selfdoc "$RLSBL_VERSION" || echo "Warning: assembly sync-workflow failed (non-fatal)"

    echo "Pushing to documentation assembly..."
    # Bare: `assembly push` is mutating but not consequential -- it re-derives
    # already-public docs from the tag this release just pushed.
    selfdoc assembly push || echo "Warning: assembly push failed (non-fatal)"
  fi
fi
