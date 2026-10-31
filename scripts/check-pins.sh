#!/usr/bin/env bash
# The dev shell (devenv.yaml/devenv.lock) and the package (flake.lock) must use
# the same nixpkgs revision, so the dev Go is the build Go.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
fail() { echo "[-] $*"; exit 1; }

flake_rev="$(jq -r '.nodes.nixpkgs.locked.rev' "$root/flake.lock")"
devenv_rev="$(jq -r '.nodes.nixpkgs.locked.rev' "$root/devenv.lock")"
grep -q "github:NixOS/nixpkgs/$flake_rev\$" "$root/devenv.yaml" ||
  fail "devenv.yaml does not pin nixpkgs $flake_rev (flake.lock)"
[ "$flake_rev" = "$devenv_rev" ] ||
  fail "devenv.lock nixpkgs $devenv_rev != flake.lock nixpkgs $flake_rev"
others="$(jq -r --arg r "$flake_rev" '[.nodes[] | select(.locked.repo == "nixpkgs" and .locked.rev != $r)] | length' "$root/devenv.lock")"
[ "$others" = "0" ] || fail "devenv.lock carries $others other nixpkgs revision(s)"
echo "[+] dev shell and package share nixpkgs $flake_rev"
