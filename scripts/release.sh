#!/usr/bin/env bash
#
# release.sh — cut a lockstep release of every MicroJet module.
#
# Usage: MODULES="core aws ..." scripts/release.sh <patch|minor>
#
# Flow (the order is deliberate — see the README/CHANGELOG notes on releasing):
#   1. Guard: on main, clean tree, then `git pull --ff-only`.
#   2. Derive the current version from the latest core/vX.Y.Z tag and compute the next.
#   3. Bump every internal `hatami57/microjet/... vCURRENT` require to vNEXT (all
#      go.mod files, including examples) by string replacement — NOT `go get`, which
#      cannot resolve vNEXT until its tags exist.
#   4. Stamp the CHANGELOG (rename a `## [Unreleased]` section, or insert a stub).
#   5. PAUSE: show the diff and the tags to be created, and wait for confirmation.
#      Edit the CHANGELOG during this pause if needed.
#   6. On confirm: commit, push main, create + push all module tags, then verify
#      (build/test) — which only resolves now that the tags exist.
#
set -euo pipefail

BUMP="${1:-}"
case "$BUMP" in
	patch | minor) ;;
	*)
		echo "usage: MODULES=\"...\" $0 <patch|minor>" >&2
		exit 2
		;;
esac

if [ -z "${MODULES:-}" ]; then
	echo "error: MODULES env var is empty (the Makefile should set it)" >&2
	exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# --- 1. Guards -------------------------------------------------------------
branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$branch" != "main" ]; then
	echo "error: releases are cut from main, but you are on '$branch'" >&2
	exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
	echo "error: working tree is dirty — commit or stash before releasing" >&2
	exit 1
fi

echo "==> git pull --ff-only origin main"
git pull --ff-only origin main

# --- 2. Versions -----------------------------------------------------------
current="$(git tag --list 'core/v*' --sort=-v:refname | head -1 | sed 's|^core/v||')"
if [ -z "$current" ]; then
	echo "error: could not find a core/vX.Y.Z tag to derive the current version" >&2
	exit 1
fi
IFS=. read -r major minor patch <<<"$current"

case "$BUMP" in
	patch) patch=$((patch + 1)) ;;
	minor)
		minor=$((minor + 1))
		patch=0
		;;
esac
next="${major}.${minor}.${patch}"

echo "==> current version v$current  ->  next version v$next"

if git rev-parse -q --verify "refs/tags/core/v$next" >/dev/null; then
	echo "error: tag core/v$next already exists" >&2
	exit 1
fi

# --- 3. Bump internal requires --------------------------------------------
mapfile -t gomods < <(git ls-files '*go.mod')
for f in "${gomods[@]}"; do
	if grep -q "hatami57/microjet" "$f"; then
		sed -i -E "s#(hatami57/microjet[^ ]*) v${current}([[:space:]]|\$)#\1 v${next}\2#g" "$f"
	fi
done

# Sanity: no internal ref should still point at the old version.
if git grep -q "hatami57/microjet[^ ]* v${current}\b" -- '*go.mod'; then
	echo "error: some go.mod still references v$current after the bump:" >&2
	git grep "hatami57/microjet[^ ]* v${current}\b" -- '*go.mod' >&2
	exit 1
fi

# --- 4. CHANGELOG ----------------------------------------------------------
today="$(date +%F)"
if grep -q '^## \[Unreleased\]' CHANGELOG.md; then
	sed -i "s|^## \[Unreleased\].*|## [$next] - $today|" CHANGELOG.md
else
	awk -v ver="$next" -v d="$today" '
		!done && /^## \[/ {
			print "## [" ver "] - " d "\n\n### Changed\n\n- TODO: describe this release\n"
			done = 1
		}
		{ print }
	' CHANGELOG.md >CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md
fi

# --- 5. Pause for review ---------------------------------------------------
echo
echo "================= release v$next ================="
git --no-pager diff --stat
echo
echo "Tags to be created:"
for m in $MODULES; do echo "  $m/v$next"; done
echo
echo "Review the diff above. Edit CHANGELOG.md now if the entry needs work."
printf "Proceed to commit, push, and tag v%s? [y/N] " "$next"
read -r answer </dev/tty
case "$answer" in
	y | Y | yes | YES) ;;
	*)
		echo "Aborted. Your changes are left in the working tree; run 'git checkout -- .' to discard." >&2
		exit 1
		;;
esac

# --- 6. Commit, push, tag, push tags, verify -------------------------------
git add -A
git commit -m "release: v$next"
echo "==> push main"
git push origin main

for m in $MODULES; do git tag "$m/v$next"; done
echo "==> push tags"
git push origin $(for m in $MODULES; do echo "$m/v$next"; done)

echo "==> verifying build/test against the published tags"
for m in $MODULES; do
	echo "  ==> $m"
	(cd "$m" && go build ./... && go test ./...) || {
		echo "error: verification failed for $m (tags are already pushed)" >&2
		exit 1
	}
done

echo "Released v$next."
