#!/usr/bin/env bash
# release.sh <major|minor|patch|current>
#   major|minor|patch: bump VERSION, signed commit "release: vX.Y.Z", signed tag
#   current:           signed tag for the VERSION already committed (no bump)
# Then pushes main + tag atomically to every remote in RELEASE_REMOTES.
# The tag push triggers CI: check:release, task ci, cache:push.
# RELEASE_GATE (default "task ci") runs before anything is written.
set -euo pipefail

part="${1:?usage: release.sh <major|minor|patch|current>}"
remotes="${RELEASE_REMOTES:-origin mirror}"
gate="${RELEASE_GATE:-task ci}"
here="$(cd "$(dirname "$0")" && pwd)"
root="$(git rev-parse --show-toplevel)"
cd "$root"
fail() { echo "[-] $*" >&2; exit 1; }

[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || fail "not on main"
[ -z "$(git status --porcelain)" ] || fail "working tree is not clean"
for r in $remotes; do
  git fetch --quiet "$r" main --tags
  [ "$(git rev-parse HEAD)" = "$(git rev-parse "$r/main")" ] || fail "main differs from $r/main; pull or push first"
done

current="$(cat VERSION)"
case "$part" in
  current) next="$current" ;;
  major | minor | patch) next="$("$here/semver.sh" "$part" "$current")" ;;
  *) fail "unknown release part '$part' (major, minor, patch, current)" ;;
esac
tag="v$next"
git rev-parse -q --verify "refs/tags/$tag" >/dev/null && fail "tag $tag already exists"
echo "[*] releasing $tag (VERSION $current -> $next)"

echo "[*] gate: $gate"
$gate

if [ "$next" != "$current" ]; then
  printf '%s\n' "$next" > VERSION
  git add VERSION
  git commit -S -q -m "release: $tag"
fi
git tag -s "$tag" -m "mcp-taskfile $tag"
git verify-tag "$tag" >/dev/null 2>&1 || fail "tag $tag is not validly signed"

for r in $remotes; do
  git push --atomic --quiet "$r" main "refs/tags/$tag"
  echo "[+] pushed main and $tag to $r"
done
echo "[+] released $tag"
