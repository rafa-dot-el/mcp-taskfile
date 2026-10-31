#!/usr/bin/env bash
# Architecture gate for option C: consumers import this repo as source
# (`flake = false`) and build package.nix with their own pkgs.
#   1. the consumer gains exactly one lock node and no second nixpkgs
#   2. package.nix builds against a different nixpkgs (tests/consumer, nixos-25.11)
#   3. that build uses the consumer's go_1_26 and reports VERSION
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
lock="$root/tests/consumer/flake.lock"
fixture="git+file://$root?dir=tests/consumer"
fail() { echo "[-] $*"; exit 1; }

nodes="$(jq '.nodes | length' "$lock")"
is_source="$(jq -r '.nodes["mcp-taskfile"].flake' "$lock")"
nixpkgs_nodes="$(jq '[.nodes[] | select(.locked.repo == "nixpkgs")] | length' "$lock")"
[ "$nodes" = "3" ] && [ "$is_source" = "false" ] && [ "$nixpkgs_nodes" = "1" ] ||
  fail "consumer lock: $nodes nodes, mcp-taskfile flake=$is_source, $nixpkgs_nodes nixpkgs; want 3, false, 1"
echo "[+] consumer lock gains one source-only node and no second nixpkgs"

own_rev="$(jq -r '.nodes.nixpkgs.locked.rev' "$root/flake.lock")"
consumer_rev="$(nix eval --raw "$fixture#consumerNixpkgsRev")"
[ "$own_rev" != "$consumer_rev" ] ||
  fail "fixture consumer uses this repo's nixpkgs ($own_rev); the build check would prove nothing"

out="$(nix build --no-link --print-out-paths "$fixture#mcp-taskfile")"
echo "[+] package.nix builds on consumer nixpkgs ${consumer_rev:0:12}: $out"

want_go="go$(nix eval --raw "$fixture#consumerGoVersion")"
got_go="$(go version -m "$out/bin/mcp-taskfile" | awk 'NR==1 {print $2}')"
[ "$got_go" = "$want_go" ] || fail "binary built with $got_go, consumer go_1_26 is $want_go"
echo "[+] binary embeds the consumer's $got_go"

want_ver="mcp-taskfile $(cat "$root/VERSION")"
got_ver="$("$out/bin/mcp-taskfile" --version)"
[ "$got_ver" = "$want_ver" ] || fail "--version prints '$got_ver', want '$want_ver'"
echo "[+] --version prints '$got_ver'"
