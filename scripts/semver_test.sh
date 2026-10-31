#!/usr/bin/env bash
# Tests for scripts/semver.sh (pure: no git, no files).
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
fails=0
expect() { # want, args...
  local want="$1"; shift
  local got
  got="$("$here/semver.sh" "$@" 2>/dev/null)" || got="<exit $?>"
  if [ "$got" = "$want" ]; then echo "[+] semver.sh $* -> $got"; else echo "[-] semver.sh $*: got '$got', want '$want'"; fails=$((fails + 1)); fi
}

expect 0.1.1 patch 0.1.0
expect 0.2.0 minor 0.1.9
expect 1.0.0 major 0.9.3
expect 1.10.0 minor 1.9.7
expect 2.0.0 major 1.12.40
expect '<exit 2>' patch 0.1          # not X.Y.Z
expect '<exit 2>' patch v0.1.0       # VERSION holds no "v"
expect '<exit 2>' patch 01.2.3       # leading zero
expect '<exit 2>' patch 1.2.3-rc1    # pre-releases unsupported
expect '<exit 2>' build 1.2.3        # unknown part
expect '<exit 2>'                    # no args

[ "$fails" = 0 ] || { echo "[-] $fails semver test(s) failed"; exit 1; }
echo "[+] semver tests passed"
