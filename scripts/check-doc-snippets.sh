#!/usr/bin/env bash
#
# Guard README.md and docs/*.md against API drift (IMPROVEMENTS §1.2).
#
# Two checks, because the docs contain two kinds of ```go block:
#
#   1. compile — blocks that open with `package` are complete programs. They
#      are written into a throwaway module whose `replace` directives point at
#      this checkout, and built with `go build ./...`.
#
#   2. symbols — most blocks are fragments: they open with `import (...)` and
#      then use variables (`app`, `router`, `dbStore`) that only exist in the
#      surrounding prose, so they can never compile standalone. For those, every
#      `pkg.Symbol` reference into a microjet package is resolved with `go doc`.
#      That is exactly the drift §1.1 was about — `core.NewNotFoundError`,
#      `gormx.NamedModule`, `middleware.NewCachedTenantStore` — and it needs no
#      change to how the docs are written.
#
# Import aliases are collected per file, not per block, since a fragment often
# uses a package that an earlier block in the same file imported.
#
# Fence conventions:
#   ```go          checked (compiled if it starts with `package`)
#   ```go ignore   skipped entirely — pseudo-code
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
mkdir -p "$WORK/_raw"
trap 'rm -rf "$WORK"' EXIT

MODPREFIX="github.com/hatami57/microjet"

cd "$ROOT"

# Preflight: prove `go doc` can resolve a microjet package that certainly exists
# before attributing anything to the docs. Without this, an environment that
# cannot load the workspace at all reports every documented symbol as missing,
# which reads as catastrophic API drift instead of a broken toolchain.
if ! probe=$(go doc "$MODPREFIX/core" 2>&1); then
  echo "error: go doc cannot resolve $MODPREFIX/core, so no symbol can be checked." >&2
  echo "       This is a toolchain/workspace problem, not doc drift:" >&2
  printf '%s\n' "$probe" | sed 's/^/  /' >&2
  exit 1
fi

FILES=(README.md)
while IFS= read -r f; do FILES+=("$f"); done < <(find docs -name '*.md' | sort)

# extract_blocks <file> <outdir_prefix> -> lines of "<startline>\t<rawfile>"
extract_blocks() {
  awk -v work="$WORK/_raw" -v base="$1" '
    /^```go[ \t]*$/        { inb = 1; start = NR; body = ""; next }
    /^```go[ \t]+ignore/   { skipb = 1; next }
    skipb && /^```[ \t]*$/ { skipb = 0; next }
    skipb                  { next }
    inb && /^```[ \t]*$/ {
      inb = 0
      out = work "/raw" (base + (++c)) ".go"
      printf "%s", body > out
      close(out)
      print start "\t" out
      next
    }
    inb { body = body $0 "\n" }
  ' "$2"
}

# Collect "alias<TAB>importpath" for microjet imports in a raw block.
imports_of() {
  awk '
    /^import[ \t]+\(/ { grp = 1; next }
    grp && /^\)/      { grp = 0; next }
    grp || /^import[ \t]/ {
      line = $0
      sub(/^import[ \t]+/, "", line)
      gsub(/[ \t]+$/, "", line)
      if (line == "" || line ~ /^\/\//) next
      alias = ""
      if (line ~ /^[A-Za-z_.][A-Za-z0-9_]*[ \t]+"/) { alias = line; sub(/[ \t]+".*$/, "", alias) }
      path = line
      sub(/^[^"]*"/, "", path)
      sub(/".*$/, "", path)
      if (path == "") next
      if (alias == "") {
        alias = path
        # Go infers the package name from the path, ignoring a major-version
        # suffix: ".../golang-migrate/migrate/v4" is package `migrate`, not `v4`.
        sub(/\/v[0-9]+$/, "", alias)
        sub(/^.*\//, "", alias)
      }
      print alias "\t" path
    }
  ' "$1"
}

nblocks=0
nsyms=0
failed=0
declare -A SEEN   # cache: "path Symbol" -> 1 ok / 2 missing
compile_dirs=0

for f in "${FILES[@]}"; do
  mapfile -t blocks < <(extract_blocks "$nblocks" "$f")
  [ "${#blocks[@]}" -eq 0 ] && continue

  # Pass 1: file-wide alias -> import path map (microjet packages only).
  # A fragment often uses a package an earlier block in the same file imported.
  declare -A ALIAS=()
  for b in "${blocks[@]}"; do
    raw="${b#*$'\t'}"
    while IFS=$'\t' read -r alias path; do
      case "$path" in "$MODPREFIX"* | "$MODPREFIX") ALIAS["$alias"]="$path" ;; esac
    done < <(imports_of "$raw")
  done

  # Pass 2: compile complete programs, symbol-check every block.
  for b in "${blocks[@]}"; do
    line="${b%%$'\t'*}"
    raw="${b#*$'\t'}"
    nblocks=$((nblocks + 1))

    if head -n1 "$raw" | grep -q '^package '; then
      compile_dirs=$((compile_dirs + 1))
      dir="$WORK/block$(printf '%03d' "$compile_dirs")"
      mkdir -p "$dir"
      cp "$raw" "$dir/snippet.go"
      echo "$f:$line" >"$dir/origin.txt"
      # `//go:embed x/*.sql` is a compile error when nothing matches.
      { grep -oE '^//go:embed[ \t]+.*' "$dir/snippet.go" 2>/dev/null || true; } |
        sed 's|^//go:embed[ \t]*||' | tr ' ' '\n' | while read -r pat; do
        case "$pat" in
        */*)
          sub="${pat%/*}"
          glob="${pat##*/}"
          mkdir -p "$dir/$sub"
          touch "$dir/$sub/placeholder.${glob##*.}"
          ;;
        esac
      done
    fi

    # A block's own imports shadow the file-wide map. Crucially this includes
    # non-microjet imports: docs/migrations.md imports golang-migrate as
    # `migrate` and `postgres`, which must not be resolved against microjet's
    # gormx/migrate and gormx/postgres.
    declare -A LOCAL=()
    while IFS=$'\t' read -r alias path; do
      LOCAL["$alias"]="$path"
    done < <(imports_of "$raw")

    for alias in "${!ALIAS[@]}"; do
      path="${ALIAS[$alias]}"
      if [ -n "${LOCAL[$alias]+x}" ]; then
        path="${LOCAL[$alias]}"
        case "$path" in "$MODPREFIX"*) ;; *) continue ;; esac
      fi
      # Exported selectors on this alias: alias.Symbol
      while IFS= read -r sym; do
        [ -z "$sym" ] && continue
        key="$path $sym"
        if [ -z "${SEEN[$key]+x}" ]; then
          # `go doc` exits non-zero both for real drift ("no symbol X in package
          # Y") and for anything that stops it loading the package at all — a
          # toolchain, proxy, checksum or workspace failure. Reporting the latter
          # as drift is a lie that sends you hunting through the docs for symbols
          # that exist, so only "no symbol" counts; every other error aborts with
          # the message the go command actually printed.
          if out=$(go doc "$path" "$sym" 2>&1); then
            SEEN[$key]=1
          elif printf '%s' "$out" | grep -q "^doc: no symbol "; then
            SEEN[$key]=2
          else
            echo "error: go doc could not load $path — this is not doc drift:" >&2
            printf '%s\n' "$out" | sed 's/^/  /' >&2
            exit 1
          fi
        fi
        nsyms=$((nsyms + 1))
        if [ "${SEEN[$key]}" = "2" ]; then
          echo "  $f:$line: undefined: $alias.$sym  (no symbol $sym in $path)" >&2
          failed=1
        fi
      done < <(grep -oE "\\b${alias}\\.[A-Z][A-Za-z0-9_]*" "$raw" 2>/dev/null | sed "s|^${alias}\\.||" | sort -u)
    done
    unset LOCAL
  done
  unset ALIAS
done

if [ "$nblocks" -eq 0 ]; then
  echo "no go blocks found — extraction is broken" >&2
  exit 1
fi

if [ "$failed" -ne 0 ]; then
  echo "" >&2
  echo "doc symbols do not exist in the code above" >&2
  exit 1
fi
echo "==> symbols ok ($nsyms references across $nblocks blocks)"

# --- compile the self-contained programs -----------------------------------
if [ "$compile_dirs" -gt 0 ]; then
  {
    echo "module docsnippets"
    echo
    echo "go 1.26.2"
    echo
    while IFS= read -r m; do
      mod=$(awk '/^module /{print $2; exit}' "$m")
      echo "replace $mod => $ROOT/$(dirname "$m" | sed 's|^\./||')"
    done < <(find . -name go.mod -not -path './examples/*' -not -path '*/vendor/*' | sort)
  } >"$WORK/go.mod"

  # Standalone module: the workspace would otherwise resolve these imports and
  # hide a missing require.
  export GOWORK=off
  cd "$WORK"
  if ! go mod tidy >"$WORK/tidy.txt" 2>&1; then
    echo "go mod tidy failed for doc snippets:" >&2
    sed 's/^/  /' "$WORK/tidy.txt" >&2
    exit 1
  fi
  if ! go build ./... >"$WORK/err.txt" 2>&1; then
    echo "doc snippets failed to compile:" >&2
    while IFS= read -r l; do
      blk=$(printf '%s' "$l" | grep -oE 'block[0-9]{3}' | head -n1 || true)
      if [ -n "$blk" ] && [ -f "$WORK/$blk/origin.txt" ]; then
        echo "  [from $(cat "$WORK/$blk/origin.txt")] ${l##*snippet.go:}" >&2
      else
        echo "  $l" >&2
      fi
    done <"$WORK/err.txt"
    exit 1
  fi
  echo "==> compiled $compile_dirs self-contained snippet(s)"
fi
