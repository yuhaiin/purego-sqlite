#!/usr/bin/env bash
# Complete comparison, sequentially on one host. Supply the native embed asset
# and its matching sqlite3.h in BENCH_EMBED_DIR; output files stay git-ignored.
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_dir"
embed_dir=$(realpath "${BENCH_EMBED_DIR:?set BENCH_EMBED_DIR to the embedded library directory}")
mkdir -p benchmarks/results
export GOMAXPROCS="${GOMAXPROCS:-4}"
if (( $# == 0 )); then
  set -- -benchtime=300ms -count=5
fi
for backend in purego cgo-dynamic cgo; do
  BENCH_BACKEND="$backend" BENCH_NATIVE_DIR="$repo_dir/.native" scripts/benchmark.sh "$@"
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend.txt"
done
for backend in purego cgo-dynamic; do
  BENCH_BACKEND="$backend" BENCH_NATIVE_DIR="$embed_dir" scripts/benchmark.sh "$@"
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend-bridge.txt"
done
python3 benchmarks/compare.py \
  purego=benchmarks/results/local-purego.txt \
  purego-bridge=benchmarks/results/local-purego-bridge.txt \
  cgo-dynamic=benchmarks/results/local-cgo-dynamic.txt \
  cgo-dynamic-bridge=benchmarks/results/local-cgo-dynamic-bridge.txt \
  cgo=benchmarks/results/local-cgo.txt > benchmarks/results/local-combined.md
