#!/usr/bin/env bash
# Prints X.Y.Z when HEAD is exactly tagged vX.Y.Z, otherwise 0.0.0-<short sha>.
set -euo pipefail
tag="$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || true)"
if [[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "${BASH_REMATCH[1]}"
else
  echo "0.0.0-$(git rev-parse --short=7 HEAD)"
fi
