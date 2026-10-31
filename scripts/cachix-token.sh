#!/usr/bin/env bash
# Print the Cachix auth token to stdout (for a pipe or $(...), never a file):
# CACHIX_AUTH_TOKEN when set (CI), otherwise the pass entry given as $1 (local).
set -euo pipefail

entry="${1:?usage: cachix-token.sh PASS_ENTRY}"
if [ -n "${CACHIX_AUTH_TOKEN:-}" ]; then
  printf '%s\n' "$CACHIX_AUTH_TOKEN"
elif command -v pass >/dev/null 2>&1; then
  pass show "$entry" | head -n1
else
  echo "[-] no Cachix token: set CACHIX_AUTH_TOKEN (CI secret) or install pass with entry $entry" >&2
  exit 1
fi
