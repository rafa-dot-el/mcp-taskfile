#!/usr/bin/env bash
# semver.sh <major|minor|patch> <X.Y.Z>: print the next version. Pure.
# Exit 2 on bad input (no "v" prefix, no leading zeros, no pre-releases).
set -euo pipefail

usage() { echo "[-] usage: semver.sh <major|minor|patch> <X.Y.Z>" >&2; exit 2; }
[ "$#" = 2 ] || usage
part="$1" version="$2"
num='(0|[1-9][0-9]*)'
[[ "$version" =~ ^$num\.$num\.$num$ ]] || { echo "[-] not a X.Y.Z version: '$version'" >&2; exit 2; }
major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}"

case "$part" in
  major) echo "$((major + 1)).0.0" ;;
  minor) echo "$major.$((minor + 1)).0" ;;
  patch) echo "$major.$minor.$((patch + 1))" ;;
  *) usage ;;
esac
