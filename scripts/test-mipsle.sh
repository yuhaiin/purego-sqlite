#!/usr/bin/env bash
# Debian/Ubuntu: gcc-mipsel-linux-gnu libc6-dev-mipsel-cross qemu-user python3.
# GOMIPS and the C toolchain must use the same floating-point ABI.
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_dir"
export CC="${CC:-mipsel-linux-gnu-gcc}"
export CGO_ENABLED=1 GOOS=linux GOARCH=mipsle GOMIPS=hardfloat
export QEMU_LD_PREFIX="${QEMU_LD_PREFIX:-/usr/mipsel-linux-gnu}"
export SQLITE_TEST_EMULATOR="${SQLITE_TEST_EMULATOR:-qemu-mipsel}"
export SQLITE_TEST_LIBRARY="$repo_dir/.native/mipsle/libsqlite3.so"
export SQLITE_TEST_EXTENSION="$repo_dir/.native/mipsle/test_extension.so"
export SQLITE_REQUIRE_FEATURES=1
python3 scripts/build-native.py --output .native/mipsle --embed
go test -c -o .native/mipsle/driver.test .
"$SQLITE_TEST_EMULATOR" .native/mipsle/driver.test -test.v -test.timeout=5m
go test -c -tags sqlite_bundle -o .native/mipsle/bundled.test .
SQLITE_TEST_LIBRARY='' SQLITE_TEST_BUNDLED=1 "$SQLITE_TEST_EMULATOR" .native/mipsle/bundled.test -test.v -test.timeout=5m
