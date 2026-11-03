# Same-library mattn benchmark

This directory is a separate Go module so `mattn/go-sqlite3` and CGO are never
production dependencies of the driver. The pinned comparison is mattn v1.14.52
versus PureGo v0.11.3 with the driver implementation at `e547a78`.

## Reproduce (Linux)

From the repository root:

```sh
python3 scripts/build-native.py
scripts/benchmark.sh
```

Python 3, a C compiler and Go 1.26+ are required. The native source archive is
pinned and verified against SHA-256 and SQLite's published SHA3-256. The script
sets the header/linker/runtime search paths so mattn's `libsqlite3` build tag uses
**the identical SQLite binary** selected by PureGo's `WithLibrary`. Before timing,
`TestSameNativeSQLite` checks native source ID and all compile options; a mismatch
fails. Running without the `libsqlite3` tag is not a valid same-engine comparison.

Results are written to ignored `results/local-raw.txt` and `local-summary.md`.
To smoke test all workloads without producing useful performance measurements:

```sh
scripts/benchmark.sh -benchtime=1x -count=1
```

For custom runs, keep the same environment from `scripts/benchmark.sh`. The shell
wrapper is Linux-specific; the Go workloads also work elsewhere if the linker,
runtime and explicit PureGo paths select the same local native library.

## Measurement environment and controls

Measured on 2026-11-03 on Linux/amd64, AMD Ryzen 5 5600G, Go
`go1.27.3-X:nodwarf5`, GCC 16.2.1, with **GOMAXPROCS=4**. SQLite 3.54.1 source ID:

```text
2026-10-16 14:45:20 555e07ef33f69a0957d24d8e555b702464a131d6f1607f4c3c1e4551e8e6ca76
```

The [native manifest](results/2026-11-03-native-manifest.json) records the source
URL, checksums, compiler, compile flags and shared-library hash. Full options
appear in the [raw log](results/2026-11-03-linux-amd64.txt).

- File-backed databases under `/tmp`, which was **tmpfs** on this machine. These
  numbers emphasize driver/CPU overhead, not physical-disk durability throughput.
  Set `TMPDIR` to a real deployment filesystem for storage measurements.
- WAL, `synchronous=NORMAL`, `cache_size=-2000` (2 MiB), busy timeout 5000ms.
  Cache/synchronous/timeout configuration is applied on every physical connection.
- One connection except WAL concurrency cases, which use four connections and
  four workers (`RunParallel` with GOMAXPROCS=4).
- Dataset: 1000 rows with integer primary key, secondary TEXT index, TEXT name
  and INTEGER value. Full scans read all three columns into Go values.
- Unprepared cases prepare on each call. Prepared INSERT and payload writes reuse
  statements. Batch cases use a newly prepared statement inside one committed
  transaction of 1000 or 100000 rows. Table clearing is outside the timed region.
- Large TEXT/BLOB values are 1,048,575 bytes; writes update the same row to the same
  value. They measure binding, SQLite comparison/update and copying paths, not
  sustained database growth or changed-content storage bandwidth.
- Open/close includes `sql.Open`/connector creation, Ping, matching per-connection
  configuration and Close on an existing file. Ordinary setup/seeding and final
  teardown are excluded from timing.
- Five sequential 200ms samples per driver/case. Very large transactions have
  only a few iterations per sample. No CPU pinning, frequency lock or confidence
  interval analysis was applied; background load and thermal state can affect
  these exploratory results. Medians summarize observed samples, not a promise.
- B/op and allocs/op are Go allocations only; native SQLite heap allocations are
  not counted by Go's benchmark harness. ops/s counts completed benchmark units:
  one batch, entire scan, or one SQL operation depending on the workload.

## Results

[Full median table](results/2026-11-03-linux-amd64.md) includes ns/op, B/op,
allocs/op and ops/s for both implementations. Ratio below is PureGo time divided
by mattn time; values above 1 mean PureGo took longer.

| Workload | PureGo ns/op | mattn ns/op | Ratio |
|---|---:|---:|---:|
| Open/close | 76,169 | 75,913 | 1.00x |
| SELECT constant | 5,014 | 2,832 | 1.77x |
| Single INSERT | 13,849 | 11,193 | 1.24x |
| Prepared INSERT | 10,044 | 9,119 | 1.10x |
| 1000-row batch INSERT | 1,285,996 | 778,138 | 1.65x |
| 100000-row transaction INSERT | 125,861,514 | 76,882,093 | 1.64x |
| Single-row SELECT | 12,043 | 7,578 | 1.59x |
| Full-table scan (1000 rows) | 1,625,794 | 617,686 | 2.63x |
| Indexed lookup | 11,466 | 7,707 | 1.49x |
| Large TEXT read | 139,542 | 130,698 | 1.07x |
| Large TEXT write | 82,504 | 79,887 | 1.03x |
| Large BLOB read | 301,063 | 276,077 | 1.09x |
| Large BLOB write | 83,066 | 81,247 | 1.02x |
| WAL concurrent readers | 2,350 | 2,202 | 1.07x |
| WAL writer contention | 14,987 | 12,296 | 1.22x |
| FTS5 search | 45,495 | 39,994 | 1.14x |
| JSON query | 7,543 | 4,261 | 1.77x |

On this setup, open/close and large-value operations were close, and prepared
INSERT was about 10% slower. Small queries, batches and row-by-row scans still
show material overhead. In particular, scan allocations remain higher
(19,348 vs 7,308 per 1000-row scan). Performance parity is **not** claimed.

## Measured optimization

The initial reflective integer FFI path used 74 Go allocations for constant SELECT,
39 for prepared INSERT and 39,395 for a 1000-row scan. A three-sample experiment
with GOMAXPROCS=12 and the same native library is preserved in
[before](results/2026-11-03-before-fast-ffi.txt) and
[after](results/2026-11-03-after-fast-ffi.txt) logs. Integer-only `SyscallN` wrappers
reduced the scan median from 3.33ms to 1.63ms and prepared INSERT from 12.68µs to
10.20µs. Subsequent removal of duplicate reset/metadata work produced the final
GOMAXPROCS=4 results above. Do not directly compare concurrency cases across
these differently configured experiments.

Further optimization should target typed FFI conversion and per-row allocation
cost, while preserving copies of native column memory, transient binding
ownership, floating-point ABIs and deterministic cancellation/cleanup. There is
no native buffer cache or skipped correctness check for benchmark-only paths.
