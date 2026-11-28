#!/usr/bin/env bash
# Checks that database migrations are added in version order.
#
# Usage: check-migration-order.sh <base> [<head>]
#
# golang-migrate only applies versions above a database's current version, so a
# migration numbered at or below one already in <base> is silently skipped by
# every database that has passed that version. For each migrations directory
# this fails when:
#   - an *.up.sql added between <base> and <head> has a version at or below the
#     highest version in <base>, or
#   - two migrations in <head> share a version and direction.
# Versions are compared as integers, as golang-migrate does, so 80_ and 080_
# are the same version. <head> defaults to HEAD.
#
# MIGRATION_DIRS overrides the space-separated directories to check.

set -euo pipefail

MIGRATION_DIRS=${MIGRATION_DIRS:-"broker/migrations directory/migrations"}

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: $0 <base> [<head>]" >&2
  exit 2
fi
if ! base=$(git rev-parse --verify --quiet "$1^{commit}"); then
  echo "unknown base revision: $1" >&2
  exit 2
fi
if ! head=$(git rev-parse --verify --quiet "${2:-HEAD}^{commit}"); then
  echo "unknown head revision: ${2:-HEAD}" >&2
  exit 2
fi

# git ls-tree resolves paths against the working directory.
cd "$(git rev-parse --show-toplevel)"

failures=0

# fail reports a problem as a GitHub Actions annotation on the file.
fail() {
  echo "::error file=$1::$2"
  failures=$((failures + 1))
}

# version prints the integer version of a migration path, or nothing when its
# name has no numeric prefix.
version() {
  local name=${1##*/}
  local digits=${name%%_*}
  case $digits in
    '' | *[!0-9]*) return 0 ;;
  esac
  echo $((10#$digits))
}

# direction prints up or down for a migration path, or nothing for other files.
direction() {
  case $1 in
    *.up.sql) echo up ;;
    *.down.sql) echo down ;;
  esac
}

for dir in $MIGRATION_DIRS; do
  max=0
  while IFS= read -r path; do
    [ "$(direction "$path")" = up ] || continue
    v=$(version "$path")
    if [ -n "$v" ] && [ "$v" -gt "$max" ]; then
      max=$v
    fi
  done < <(git ls-tree --name-only "$base" -- "$dir/")

  while IFS= read -r path; do
    [ "$(direction "$path")" = up ] || continue
    v=$(version "$path")
    [ -n "$v" ] || continue
    if [ "$v" -le "$max" ]; then
      fail "$path" "Migration version $v is not above $max, the highest $dir version in ${base:0:8}. Databases already at $max would skip it; renumber it above $max."
    fi
  done < <(git diff --no-renames --diff-filter=A --name-only "$base" "$head" -- "$dir/")

  # Lines of "<version> <direction> <path>" for every migration in head.
  entries=$(git ls-tree --name-only "$head" -- "$dir/" | while IFS= read -r path; do
    d=$(direction "$path")
    v=$(version "$path")
    if [ -n "$d" ] && [ -n "$v" ]; then
      echo "$v $d $path"
    fi
  done)
  duplicates=$(cut -d' ' -f1,2 <<<"$entries" | sort | uniq -d)
  while read -r v d path; do
    [ -n "$path" ] || continue
    if grep -qxF "$v $d" <<<"$duplicates"; then
      fail "$path" "Another $dir $d migration also has version $v; give each migration its own version."
    fi
  done <<<"$entries"
done

if [ "$failures" -gt 0 ]; then
  echo "Found $failures migration ordering problem(s) between ${base:0:8} and ${head:0:8}." >&2
  exit 1
fi
echo "Migrations are in order between ${base:0:8} and ${head:0:8}."
