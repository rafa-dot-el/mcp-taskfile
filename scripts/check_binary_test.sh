#!/usr/bin/env bash
# Tests for scripts/check-binary.sh against tiny real binaries: one clean, one
# with a foreign /nix/store reference, one with a build-machine path.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0

# build NAME STRING: static, stripped, trimpath binary that embeds STRING.
build() {
  local name="$1" str="$2"
  mkdir -p "$work/$name"
  printf 'module fixture\n\ngo 1.26\n' >"$work/$name/go.mod"
  printf 'package main\n\nimport "os"\n\nvar s = "%s"\n\nfunc main() { os.Stdout.WriteString(s) }\n' "$str" >"$work/$name/main.go"
  (cd "$work/$name" && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$work/$name.bin" .)
}

expect() { # want (0|1), name
  local want="$1" name="$2" out rc
  out="$("$here/check-binary.sh" "$work/$name.bin" 2>&1)"
  rc=$?
  if [ "$rc" = "$want" ]; then echo "[+] $name -> exit $rc"; else echo "[-] $name: exit $rc, want $want"; echo "$out"; fails=$((fails + 1)); fi
}

build clean 'hello'
build allowed '/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-tzdata-2025b/share/zoneinfo'
build foreign '/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-evil-1.0/bin/evil'
build homepath ' /home/someone/src/project/main.go'

expect 0 clean
expect 0 allowed
expect 1 foreign
expect 1 homepath

[ "$fails" = 0 ] || { echo "[-] $fails check-binary test(s) failed"; exit 1; }
echo "[+] check-binary tests passed"
