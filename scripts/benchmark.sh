#!/usr/bin/env bash
# Linux same-library comparison. Native compilation is separate from Go CGO use.
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ ! -f "$repo_dir/.native/libsqlite3.so" ]]; then
  python3 "$repo_dir/scripts/build-native.py"
fi
export CGO_ENABLED=1
export CGO_CFLAGS="-I$repo_dir/.native/src"
export CGO_LDFLAGS="-L$repo_dir/.native -Wl,-rpath,$repo_dir/.native"
export LD_LIBRARY_PATH="$repo_dir/.native${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export BENCH_SQLITE_LIBRARY="$repo_dir/.native/libsqlite3.so"
export GOMAXPROCS="${GOMAXPROCS:-4}"
cd "$repo_dir/benchmarks"
mkdir -p results
# -run validates SQLite source ID and compile options before timing anything.
if (( $# == 0 )); then
  set -- -benchtime=200ms -count=5
fi
go test -tags libsqlite3 -run TestSameNativeSQLite -v -bench . -benchmem -timeout 20m "$@" | tee results/local-raw.txt
python3 compare.py results/local-raw.txt | tee results/local-summary.md
