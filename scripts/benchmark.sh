#!/usr/bin/env bash
# Linux same-library comparison. Native compilation is separate from Go CGO use.
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ ! -f "$repo_dir/.native/libsqlite3.so" ]]; then
  python3 "$repo_dir/scripts/build-native.py"
fi
export CGO_ENABLED=1
export CGO_CFLAGS="-O2 -g -I$repo_dir/.native/src"
export CGO_LDFLAGS="-L$repo_dir/.native -Wl,-rpath,$repo_dir/.native"
export LD_LIBRARY_PATH="$repo_dir/.native${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
bench_backend="${BENCH_BACKEND:-purego}"
case "$bench_backend" in
  purego) bench_tags="libsqlite3,sqlite_purego"; export BENCH_SQLITE_LIBRARY="$repo_dir/.native/libsqlite3.so" ;;
  cgo) bench_tags="libsqlite3"; unset BENCH_SQLITE_LIBRARY ;;
  cgo-dynamic) bench_tags="libsqlite3,sqlite_dynamic"; export BENCH_SQLITE_LIBRARY="$repo_dir/.native/libsqlite3.so" ;;
  *) echo "BENCH_BACKEND must be purego, cgo or cgo-dynamic" >&2; exit 1 ;;
esac
export GOMAXPROCS="${GOMAXPROCS:-4}"
cd "$repo_dir/benchmarks"
mkdir -p results
# -run checks native source/options and all drivers' per-connection settings.
if (( $# == 0 )); then
  set -- -benchtime=200ms -count=5
fi
bench_command=(go test -tags "$bench_tags" -run 'TestSameNativeSQLite|TestConnectionSettings' -v -bench . -benchmem -timeout 20m "$@")
if [[ -n "${BENCH_CPUSET:-}" ]]; then
  bench_command=(taskset -c "$BENCH_CPUSET" "${bench_command[@]}")
fi
"${bench_command[@]}" | tee results/local-raw.txt
python3 compare.py "$bench_backend=results/local-raw.txt" | tee results/local-summary.md
