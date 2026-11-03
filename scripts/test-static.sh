#!/usr/bin/env bash
# Use a musl C toolchain (Alpine gcc or musl-gcc). No SQLite headers/libs needed.
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_dir"
mkdir -p .native/static
export CGO_ENABLED=1
export CC="${CC:-musl-gcc}"
go test -c -ldflags='-linkmode external -extldflags "-static"' -o .native/static/driver.test .
readelf -l .native/static/driver.test > .native/static/program-headers.txt
readelf -d .native/static/driver.test > .native/static/dynamic-section.txt
if rg 'INTERP' .native/static/program-headers.txt; then
  echo 'static executable unexpectedly has a dynamic interpreter' >&2
  exit 1
fi
if rg 'NEEDED' .native/static/dynamic-section.txt; then
  echo 'static executable unexpectedly has shared dependencies' >&2
  exit 1
fi
SQLITE_REQUIRE_FEATURES=1 .native/static/driver.test -test.v -test.timeout=3m
