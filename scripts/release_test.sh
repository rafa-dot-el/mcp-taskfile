#!/usr/bin/env bash
# Local-only tests for scripts/release.sh against throwaway bare remotes.
# Needs the GPG signing key (CI has none), so it is not part of `task ci`.
# The conditions passed to check() are single-quoted on purpose: eval expands them.
# shellcheck disable=SC2016
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'chmod -R u+w "$work"; rm -rf -- "$work"' EXIT
fails=0
check() { if eval "$2"; then echo "[+] $1"; else echo "[-] $1"; fails=$((fails + 1)); fi; }

git init -q --bare "$work/a.git"; git init -q --bare "$work/b.git"
git init -q -b main "$work/repo"
cd "$work/repo" || exit 1
mkdir scripts && cp "$here/semver.sh" "$here/release.sh" scripts/
echo 0.1.0 > VERSION
git add -A && git commit -S -q -m init
git remote add origin "$work/a.git"; git remote add mirror "$work/b.git"
git push -q origin main; git push -q mirror main
export RELEASE_GATE=true

scripts/release.sh minor >/dev/null 2>&1
check "minor: VERSION is 0.2.0" '[ "$(cat VERSION)" = 0.2.0 ]'
check "minor: signed tag v0.2.0" 'git verify-tag v0.2.0 >/dev/null 2>&1'
check "minor: commit is signed" 'git verify-commit HEAD >/dev/null 2>&1'
check "minor: both remotes have v0.2.0 and main" '[ "$(git --git-dir="$work/a.git" rev-parse v0.2.0^{commit})" = "$(git rev-parse HEAD)" ] && [ "$(git --git-dir="$work/b.git" rev-parse main)" = "$(git rev-parse HEAD)" ]'

scripts/release.sh minor >/dev/null 2>&1; true
check "minor again bumps to 0.3.0" '[ "$(cat VERSION)" = 0.3.0 ]'
check "current refuses an existing tag" '! scripts/release.sh current >/dev/null 2>&1'

echo dirty > extra.txt
check "dirty tree is refused" '! scripts/release.sh patch >/dev/null 2>&1'
rm extra.txt

git commit -S -q --allow-empty -m unpushed
check "main ahead of remotes is refused" '! scripts/release.sh patch >/dev/null 2>&1'
git reset -q --hard HEAD~1

check "failing gate writes nothing" '! RELEASE_GATE=false scripts/release.sh patch >/dev/null 2>&1 && [ "$(cat VERSION)" = 0.3.0 ] && ! git rev-parse -q --verify refs/tags/v0.3.1 >/dev/null'
check "unknown part is refused" '! scripts/release.sh build >/dev/null 2>&1'

[ "$fails" = 0 ] || { echo "[-] $fails release test(s) failed"; exit 1; }
echo "[+] release tests passed"
