#!/usr/bin/env bash
#
# Run the live OSF integration tests against a real private project.
#
# Setup once:  cp .env.example .env  &&  edit .env with your PAT + GUIDs
# Then:        ./scripts/live-test.sh
#
# Extra args pass straight through to `go test`, e.g.:
#   ./scripts/live-test.sh -run TestLive_PushNewThenNewVersion
#
# Nothing secret lives in this file — credentials come from .env or your shell,
# so it is safe to commit and edit.

set -euo pipefail

# Work from the repo root regardless of where this is invoked.
cd "$(dirname "$0")/.."

# Load .env if present (KEY=VALUE lines), exporting each to the environment.
if [ -f .env ]; then
	set -a
	# shellcheck disable=SC1091
	. ./.env
	set +a
fi

: "${OSF_TEST_TOKEN:?set OSF_TEST_TOKEN (copy .env.example to .env and fill it in)}"
: "${OSF_TEST_PROJECT:?set OSF_TEST_PROJECT (copy .env.example to .env and fill it in)}"

exec go test -tags live -count=1 -v ./integration/live/... "$@"
