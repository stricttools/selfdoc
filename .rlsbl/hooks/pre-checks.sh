#!/usr/bin/env bash
# Pre-checks hook for the selfdoc releasable (user-owned).
#
# Its one job is to put the binary this release is about to ship on PATH:
# release steps 7 and 8 run `selfdoc gen` and `selfdoc check` as commands
# found on PATH, and without this they would run whatever selfdoc happens
# to be installed on the machine instead of the code being released.
#
# Everything this hook used to do besides that is now the release flow's own
# work: the bump-selfdoc step syncs selfdoc.json's "version" (and the last
# entry of its versions array), and the strictcli help document step runs
# `go run . help --json` at the repository root, which is both the member
# directory and the package the entry point now lives in.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "pre-checks: installing the selfdoc binary from this tree"
go install .
