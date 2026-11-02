#!/usr/bin/env bash
# Regenerates all generated code in a temporary copy of the repository and fails if the result differs from
# the working tree, so stale or hand-edited generated files are caught without depending on git state.
set -euo pipefail

generated_dirs=(
  internal/datasources/mocks
  internal/datasources/mysql/queries
)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir "$tmp/repo"
tar --exclude=./.mysql-local -cf - . | tar -xf - -C "$tmp/repo"
for dir in "${generated_dirs[@]}"; do
  rm -rf "${tmp:?}/repo/$dir"
done

make -C "$tmp/repo" --no-print-directory generate

status=0
for dir in "${generated_dirs[@]}"; do
  diff -r "$dir" "$tmp/repo/$dir" || status=1
done

if [ "$status" -ne 0 ]; then
  echo "generated code is out of date: run make generate" >&2
fi
exit "$status"
