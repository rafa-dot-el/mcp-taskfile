#!/usr/bin/env bash
# Release-hygiene gate for one mcp-taskfile binary:
# static, stripped (no symbols/DWARF), -trimpath, CGO off, no build-machine
# paths, and no store references beyond the data Go's stdlib reads at runtime.
set -euo pipefail

bin="${1:?usage: check-binary.sh PATH}"
fail=0
bad() { echo "[-] $bin: $*"; fail=1; }

desc="$(file -L "$bin")"
case "$desc" in *"statically linked"*) ;; *) bad "not statically linked" ;; esac
readelf -S -W "$bin" | grep -qE '\.symtab|\.debug_' && bad "not stripped (.symtab or .debug_* present)"

info="$(go version -m "$bin")"
grep -q -- '-trimpath=true' <<<"$info" || bad "built without -trimpath"
grep -q 'CGO_ENABLED=0' <<<"$info" || bad "built with cgo"

home_refs="$(strings "$bin" | grep -c -E '(^|[^A-Za-z0-9_./-])/(home|Users|build)/' || true)"
[ "$home_refs" = "0" ] || bad "$home_refs build-machine paths (/home, /Users, /build)"

# nixpkgs patches Go's time, net and mime packages to read these store paths.
allowed='-(tzdata|iana-etc|mailcap)-'
extra="$(strings "$bin" | grep -oE '/nix/store/[a-z0-9]{32}-[^/]+' | sort -u | grep -vE -e "$allowed" || true)"
[ -z "$extra" ] || bad "unexpected store references: $(tr '\n' ' ' <<<"$extra")"

[ "$fail" = "0" ] || exit 1
echo "[+] $bin: static, stripped, trimpath, no cgo, no build paths, store refs limited to tzdata/iana-etc/mailcap"
