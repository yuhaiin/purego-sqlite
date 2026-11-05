# SQLite driver benchmarks

The results below compare **PureGo, dynamic CGO, static CGO, mattn/go-sqlite3,
and modernc.org/sqlite** across all 18 workloads. Standard and embed bridge
paths appear side by side in one combined comparison. This is a separate Go module:
mattn and modernc remain benchmark-only dependencies.

The [mattn compatibility benchmark report](mattn-high-priority.md) adds scalar
functions, grouped Go aggregates, custom collation sorting, timestamp binding and
typed row decoding. Both compatibility reports use the same CI run and seven
configuration/control columns as the SQL comparison below.

The [medium-priority compatibility report](mattn-medium-priority.md) measures
optional statement caching, snapshots, connection backup, hooks and tracing,
alongside the five-implementation SQL controls. The full run covers 35 workloads:
18 SQL cases and 17 compatibility cases.

## Implementations and controls

| Implementation | Version / selection | SQLite engine |
|---|---|---|
| PureGo | purego v0.11.3; `sqlite_purego` | Official native SQLite shared library |
| CGO dynamic | `sqlite_dynamic` | Official native SQLite shared library |
| CGO static | Default CGO backend | Official SQLite compiled into the executable |
| mattn/go-sqlite3 | v1.14.52; `libsqlite3` | Native SQLite selected by each benchmark executable |
| modernc.org/sqlite | v1.61.1 | modernc's Go translation, with its own compile options |

All five report SQLite **3.54.1**, with source ID:

```text
2026-10-16 14:45:20 555e07ef33f69a0957d24d8e555b702464a131d6f1607f4c3c1e4551e8e6ca76
```

`TestSameNativeSQLite` requires our selected backend and mattn to have identical
source IDs and compile options in every run. PureGo and dynamic CGO use the same
shared library as mattn. In the static CGO run, SQLite symbols used by both native
drivers resolve to the executable's linked engine. mattn's bundled amalgamation
is excluded with `libsqlite3` to avoid duplicate symbols and engine differences.

modernc is measured as a separate implementation. Its source version matches,
but its compile options differ: for example, it enables STAT4, column metadata
and several additional extensions, and disables default memory-status tracking.
Its complete options are recorded alongside the native options in every raw log;
this comparison does not isolate driver overhead between different engine builds.

**Static CGO here describes SQLite linkage.** These timings use glibc and do not
measure a fully static musl/Alpine executable. The PureGo backend is forced with
`sqlite_purego` because mattn requires CGO in the same benchmark executable;
production PureGo also builds and is tested with `CGO_ENABLED=0`.

## Reproduce the complete comparison

From the repository root on Linux, with Go 1.26+, Python 3 and a C compiler.
The commands below select the 18 SQL workloads. Omit the `-bench` filter to
include the compatibility cases in the complete report.
Set `SQLITE_EMBED_NATIVE_DIR` to a directory containing the embed asset
`libsqlite3.so` and its matching `src/sqlite3.h` before running the commands:

```sh
export TMPDIR=/dev/shm GOMAXPROCS=4 BENCH_CPUSET=0-3
go run ./internal/cmd/build-native
for backend in purego cgo-dynamic cgo; do
  BENCH_BACKEND="$backend" scripts/benchmark.sh \
    -bench '^BenchmarkSQLite$' -benchtime=300ms -count=5
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend.txt"
done
for backend in purego cgo-dynamic; do
  BENCH_BACKEND="$backend" BENCH_NATIVE_DIR="$SQLITE_EMBED_NATIVE_DIR" \
    scripts/benchmark.sh -bench '^BenchmarkSQLite$' -benchtime=300ms -count=5
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend-bridge.txt"
done
python3 benchmarks/compare.py \
  purego=benchmarks/results/local-purego.txt \
  purego-bridge=benchmarks/results/local-purego-bridge.txt \
  cgo-dynamic=benchmarks/results/local-cgo-dynamic.txt \
  cgo-dynamic-bridge=benchmarks/results/local-cgo-dynamic-bridge.txt \
  cgo=benchmarks/results/local-cgo.txt \
  > benchmarks/results/local-combined.md
```

The published run uses the CPU affinity recorded in its environment manifest.
The examples use CPUs 0–3; choose suitable CPUs on your machine, or omit
`BENCH_CPUSET`. Affinity applies only to the benchmark process and its children.
For the same-library probes below, set `BENCH_CPUSET` to the CPUs you select.

The [Full benchmark comparison](../.github/workflows/benchmark.yml) workflow runs
all five configurations sequentially on one runner. Its `embed_ref` input pins a
companion commit; source/options checks must pass before measuring. It also runs
the CGO-free same-library row/scalar probes and uploads all logs and environment
metadata. Locally, run the complete comparison with:

```sh
BENCH_EMBED_DIR="$SQLITE_EMBED_NATIVE_DIR" bash scripts/benchmark-all.sh
```

The wrapper builds a pinned shared library if needed and sets the C header,
linker and runtime search paths. It explicitly preserves Go's default
`CGO_CFLAGS=-O2 -g` before adding the header include path. It checks native
source/options and verifies
WAL, synchronous, cache and busy-timeout settings on all four simultaneously open
connections for all drivers before measuring. Run one backend by setting
`BENCH_BACKEND=purego`, `cgo-dynamic` or `cgo`. Each run includes mattn and modernc.

To smoke test every workload, use `scripts/benchmark.sh -benchtime=1x -count=1`.
One-iteration smoke results do not constitute performance measurements. CI runs
all three backend selections and retains their logs as artifacts.

## Measurement environment and methodology

Measured on **2026-11-05**, Linux/amd64, **AMD EPYC 9V74 80-Core Processor**, Go
`go1.27.3` and **GOMAXPROCS=4** in a dedicated GitHub Actions job.
All five configurations ran sequentially on the same runner, pinned to logical
CPUs **0, 1, 2, 3**. The [workflow run](https://github.com/yuhaiin/purego-sqlite/actions/runs/42004475183)
uses driver commit `0a021fd` (the current main implementation
plus capability documentation) and embed commit `d721bc5`.
The standard shared/static engine uses GCC 13.3; the embed asset uses
Zig 0.17.0 / Clang 22.1.8. The
[environment manifest](results/2026-11-05-current-environment.json) records CPU topology,
compiler flags, source/binary hashes, exact inputs and raw-log names.

These results supersede the November 4 table. The previous EPYC 7763 runner
and local Ryzen runs used different CPUs; differences in absolute times are
not a before/after measure of driver improvements. Ratios use this run's controls.

- Each backend run has five sequential 300ms samples per implementation and
  workload. Runs execute sequentially on the same CPU set. Our backend columns each
  contain five samples; the mattn and modernc controls pool all **25 samples**
  across the five executables. Each native backend ratio uses its own five matching
  mattn controls; modernc uses the pooled controls. The report also lists all
  matching control medians so the ratios can be checked directly. A separate
  table also compares each native backend with its matching modernc controls.
- All runs use `/dev/shm` (**tmpfs**). Results
  emphasize driver/CPU overhead; they do not measure physical-disk durability.
  Set `TMPDIR` to your deployment filesystem to measure its storage behavior.
- WAL, `synchronous=NORMAL`, `cache_size=-2000` (2 MiB) and busy timeout 5000ms
  apply to every physical connection. One connection is used except the WAL
  concurrency cases, with four connections and four `RunParallel` workers.
- The dataset contains 1000 rows with an integer primary key, a TEXT name with
  a secondary index, and an INTEGER value. Scans read all three columns.
- Unprepared cases prepare every call. Prepared INSERT and payload writes reuse
  statements. Batch cases prepare once inside a committed transaction of 1000 or
  100000 rows. Clearing the table happens outside the timed region.
  `BatchInsert1000Values` separately prepares one 1000-placeholder INSERT per
  transaction, executes it once and commits. Its 1000 int64 arguments are built
  outside timing; SQL preparation, binding, execution, statement close and commit
  are timed. The original 1000 separate executions remain in the results.
- Large TEXT/BLOB values contain 1,048,575 bytes. Writes update one row to the
  same value, measuring binding, comparison/update and copying rather than
  changed-content storage bandwidth or database growth.
- Open/close includes connector creation, Ping, per-connection settings and Close
  on an existing file. Other setup/seeding and teardown are excluded.
- All numbers are medians. Process affinity is recorded; frequency and physical-host
  isolation are not guaranteed on hosted runners. No confidence intervals were
  used. Large transactions have only a few iterations per sample; contention
  results can vary with scheduling.
- B/op and allocs/op count **Go heap** allocations. They are not total engine
  memory: native and translated-engine allocators are not accounted for equally.
- Throughput counts benchmark units per second: one whole batch/transaction or
  scan in those workloads, and one SQL operation in the other workloads.

The formal measurements use one hosted runner and matching controls. This does
not guarantee exclusive physical-host resources: small differences and
cross-configuration absolute rankings remain exploratory. The benchmark wrapper
retains Go's default CGO optimization flags (`-O2 -g`).

## Results

Both paths are in the **same table**. `standard` uses public SQLite APIs and
works with system libraries or older prebuilts. `embed` uses both optional
`purego_sqlite_read_row_v1` and `purego_sqlite_exec_scalar_v1` exports in the
companion's actual CI-built library. Static CGO uses the stock linked engine.
For FullScan1000, PureGo takes **1,004,743ns standard**
and **597,124ns embed**.

| Column | Native library / API path | Raw five-sample run |
| --- | --- | --- |
| PureGo standard | Official shared library, standard APIs | [raw](results/2026-11-05-current-purego-linux-amd64.txt) |
| PureGo embed | CI-built embed library, row reader and scalar executor v1 | [raw](results/2026-11-05-current-purego-bridge-linux-amd64.txt) |
| CGO dynamic standard | Official shared library, standard APIs | [raw](results/2026-11-05-current-cgo-dynamic-linux-amd64.txt) |
| CGO dynamic embed | CI-built embed library, row reader and scalar executor v1 | [raw](results/2026-11-05-current-cgo-dynamic-bridge-linux-amd64.txt) |
| CGO static | Stock official engine linked into the executable | [raw](results/2026-11-05-current-cgo-linux-amd64.txt) |

The [generated combined report](results/2026-11-05-current-combined-linux-amd64.md) is reproduced
below: all 18 workloads, seven configuration/control columns, time, Go memory and
throughput. mattn/modernc display columns pool **25 samples** per workload. Every
native ratio uses its own run's five matching controls; matching control medians
are included. The [complete 35-workload report](results/2026-11-05-current-all-workloads-linux-amd64.md)
also contains all compatibility cases from the same raw logs. Compiler and
engine differences remain in the full comparison.

<!-- BEGIN GENERATED COMBINED RESULTS -->

Medians of repeated samples. Time ratio = implementation / matching-run mattn (lower is better).
mattn and modernc columns pool controls across runs; each native backend's ratio uses its own run's mattn median.

- PureGo standard: 5 samples per workload
- PureGo embed: 5 samples per workload
- CGO dynamic standard: 5 samples per workload
- CGO dynamic embed: 5 samples per workload
- CGO static: 5 samples per workload
- mattn: 25 samples per workload
- modernc.org/sqlite: 25 samples per workload

### Time: ns/op (ratio to matching-run mattn)

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|---:|---:|
| OpenClose | 79,753 (1.01x) | 82,143 (1.00x) | 70,563 (0.86x) | 73,371 (0.90x) | 68,887 (0.86x) | 80,621 | 125,354 (1.55x) |
| SelectConstant | 2,652 (1.09x) | 2,522 (1.08x) | 2,243 (0.92x) | 2,165 (0.92x) | 2,173 (0.92x) | 2,370 | 2,500 (1.05x) |
| SingleInsert | 11,523 (1.04x) | 11,185 (1.02x) | 11,042 (0.99x) | 10,932 (0.99x) | 10,890 (0.98x) | 11,080 | 14,091 (1.27x) |
| PreparedInsert | 9,874 (1.06x) | 9,591 (1.03x) | 9,388 (1.02x) | 9,373 (1.01x) | 9,322 (1.01x) | 9,280 | 12,085 (1.30x) |
| BatchInsert1000 | 967,138 (1.41x) | 712,355 (1.04x) | 640,406 (0.95x) | 630,679 (0.90x) | 651,612 (0.97x) | 683,390 | 945,045 (1.38x) |
| BatchInsert1000Values | 296,404 (1.23x) | 214,334 (0.89x) | 244,823 (1.02x) | 214,319 (0.91x) | 244,622 (1.01x) | 241,579 | 410,514 (1.70x) |
| TransactionInsert100000 | 94,712,238 (1.43x) | 68,821,052 (1.02x) | 61,771,654 (0.95x) | 61,288,382 (0.90x) | 62,115,014 (0.97x) | 66,182,226 | 95,049,711 (1.44x) |
| SingleRowSelect | 7,041 (1.07x) | 6,685 (1.03x) | 6,631 (0.97x) | 6,180 (0.95x) | 6,448 (0.96x) | 6,560 | 8,391 (1.28x) |
| FullScan1000 | 1,004,743 (2.05x) | 597,124 (1.22x) | 610,874 (1.25x) | 512,133 (1.04x) | 612,684 (1.35x) | 489,380 | 465,437 (0.95x) |
| IndexedLookup | 6,862 (1.05x) | 6,802 (1.05x) | 6,537 (0.97x) | 6,337 (0.97x) | 6,514 (0.98x) | 6,557 | 8,607 (1.31x) |
| LargeTextRead | 193,265 (1.08x) | 155,403 (0.99x) | 185,676 (1.09x) | 219,526 (1.05x) | 230,467 (1.04x) | 181,408 | 194,340 (1.07x) |
| LargeTextWrite | 90,189 (0.48x) | 91,380 (1.02x) | 88,545 (1.00x) | 88,808 (1.02x) | 88,611 (1.00x) | 88,619 | 103,457 (1.17x) |
| LargeBlobRead | 392,937 (1.05x) | 509,115 (1.09x) | 429,488 (1.18x) | 473,831 (1.08x) | 451,190 (1.04x) | 420,232 | 473,127 (1.13x) |
| LargeBlobWrite | 98,177 (1.10x) | 88,776 (1.02x) | 90,144 (1.02x) | 89,621 (1.00x) | 91,002 (1.02x) | 88,941 | 103,707 (1.17x) |
| WALReaders | 3,276 (1.00x) | 3,221 (0.99x) | 3,137 (0.95x) | 3,111 (0.93x) | 3,210 (0.97x) | 3,301 | 3,619 (1.10x) |
| WALWriterContention | 13,193 (1.10x) | 13,287 (1.15x) | 12,833 (1.12x) | 12,046 (1.03x) | 12,324 (1.06x) | 11,752 | 18,194 (1.55x) |
| FTS5Search | 39,368 (0.99x) | 43,812 (1.02x) | 38,971 (0.99x) | 42,721 (0.98x) | 38,399 (1.00x) | 40,545 | 71,678 (1.77x) |
| JSONQuery | 4,053 (1.10x) | 3,806 (1.08x) | 3,440 (0.97x) | 3,414 (0.96x) | 3,389 (0.96x) | 3,549 | 4,653 (1.31x) |

### Go memory: B/op / allocs/op

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|---:|---:|
| OpenClose | 5,120 / 105 | 5,304 / 114 | 2,910 / 36 | 2,911 / 36 | 2,894 / 36 | 2,979 / 64 | 4,610 / 84 |
| SelectConstant | 536 / 15 | 536 / 15 | 488 / 15 | 488 / 15 | 488 / 15 | 536 / 14 | 480 / 13 |
| SingleInsert | 296 / 8 | 296 / 8 | 248 / 7 | 248 / 7 | 248 / 7 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,239 / 5,516 | 125,233 / 5,516 | 125,063 / 5,513 | 125,077 / 5,513 | 125,073 / 5,513 | 140,778 / 6,508 | 131,643 / 6,502 |
| BatchInsert1000Values | 42,335 / 32 | 42,334 / 32 | 42,184 / 29 | 42,181 / 29 | 42,186 / 29 | 41,901 / 25 | 41,788 / 19 |
| TransactionInsert100000 | 12,799,644 / 599,525 | 12,799,870 / 599,527 | 12,797,062 / 599,513 | 12,797,068 / 599,513 | 12,797,062 / 599,513 | 14,396,769 / 699,508 | 13,595,652 / 699,502 |
| SingleRowSelect | 720 / 24 | 720 / 24 | 656 / 23 | 656 / 23 | 656 / 23 | 736 / 26 | 672 / 24 |
| FullScan1000 | 82,490 / 7,304 | 82,490 / 7,304 | 82,424 / 7,303 | 82,424 / 7,303 | 82,424 / 7,303 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 656 / 20 | 656 / 20 | 552 / 18 | 552 / 18 | 552 / 18 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,393 / 18 | 1,049,364 / 18 | 1,049,101 / 17 | 1,049,102 / 17 | 1,049,103 / 17 | 1,049,140 / 16 | 1,049,092 / 16 |
| LargeTextWrite | 160 / 5 | 160 / 5 | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,223 / 20 | 2,098,216 / 20 | 2,097,699 / 18 | 2,097,700 / 18 | 2,097,699 / 18 | 2,097,741 / 17 | 2,097,690 / 17 |
| LargeBlobWrite | 160 / 5 | 160 / 5 | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 560 / 18 | 560 / 18 | 512 / 17 | 512 / 17 | 512 / 17 | 568 / 18 | 512 / 17 |
| WALWriterContention | 248 / 7 | 248 / 7 | 200 / 6 | 200 / 6 | 200 / 6 | 72 / 4 | 76 / 4 |
| FTS5Search | 656 / 20 | 656 / 20 | 560 / 18 | 560 / 18 | 560 / 18 | 600 / 17 | 568 / 18 |
| JSONQuery | 664 / 18 | 664 / 18 | 568 / 16 | 568 / 16 | 568 / 16 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|---:|---:|
| OpenClose | 12,539 | 12,174 | 14,172 | 13,629 | 14,517 | 12,404 | 7,977 |
| SelectConstant | 377,064 | 396,471 | 445,771 | 461,958 | 460,111 | 421,893 | 400,009 |
| SingleInsert | 86,785 | 89,406 | 90,567 | 91,473 | 91,831 | 90,253 | 70,966 |
| PreparedInsert | 101,271 | 104,264 | 106,519 | 106,688 | 107,271 | 107,756 | 82,747 |
| BatchInsert1000 | 1,034 | 1,404 | 1,562 | 1,586 | 1,535 | 1,463 | 1,058 |
| BatchInsert1000Values | 3,374 | 4,666 | 4,085 | 4,666 | 4,088 | 4,139 | 2,436 |
| TransactionInsert100000 | 11 | 15 | 16 | 16 | 16 | 15 | 11 |
| SingleRowSelect | 142,024 | 149,588 | 150,818 | 161,818 | 155,097 | 152,448 | 119,168 |
| FullScan1000 | 995 | 1,675 | 1,637 | 1,953 | 1,632 | 2,043 | 2,149 |
| IndexedLookup | 145,728 | 147,017 | 152,985 | 157,805 | 153,515 | 152,510 | 116,188 |
| LargeTextRead | 5,174 | 6,435 | 5,386 | 4,555 | 4,339 | 5,512 | 5,146 |
| LargeTextWrite | 11,088 | 10,943 | 11,294 | 11,260 | 11,285 | 11,284 | 9,666 |
| LargeBlobRead | 2,545 | 1,964 | 2,328 | 2,110 | 2,216 | 2,380 | 2,114 |
| LargeBlobWrite | 10,186 | 11,264 | 11,093 | 11,158 | 10,989 | 11,243 | 9,643 |
| WALReaders | 305,235 | 310,494 | 318,752 | 321,436 | 311,532 | 302,917 | 276,330 |
| WALWriterContention | 75,796 | 75,261 | 77,923 | 83,012 | 81,141 | 85,091 | 54,962 |
| FTS5Search | 25,401 | 22,825 | 25,660 | 23,408 | 26,043 | 24,664 | 13,951 |
| JSONQuery | 246,761 | 262,720 | 290,739 | 292,883 | 295,099 | 281,792 | 214,932 |

### Native time / matching-run modernc

Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static |
|---|---:|---:|---:|---:|---:|
| OpenClose | 0.64x (125,354) | 0.67x (122,983) | 0.56x (125,102) | 0.58x (126,709) | 0.55x (125,024) |
| SelectConstant | 1.05x (2,529) | 1.00x (2,533) | 0.93x (2,423) | 0.88x (2,448) | 0.87x (2,500) |
| SingleInsert | 0.82x (13,999) | 0.80x (14,047) | 0.78x (14,171) | 0.78x (14,099) | 0.77x (14,096) |
| PreparedInsert | 0.83x (11,968) | 0.80x (12,034) | 0.78x (12,113) | 0.78x (12,089) | 0.77x (12,095) |
| BatchInsert1000 | 1.02x (944,613) | 0.75x (951,270) | 0.68x (938,976) | 0.65x (963,417) | 0.70x (928,040) |
| BatchInsert1000Values | 0.71x (415,614) | 0.54x (400,540) | 0.60x (408,556) | 0.53x (405,110) | 0.57x (426,323) |
| TransactionInsert100000 | 1.01x (93,717,855) | 0.72x (95,247,937) | 0.65x (95,751,215) | 0.63x (96,713,115) | 0.67x (93,290,920) |
| SingleRowSelect | 0.84x (8,384) | 0.80x (8,357) | 0.78x (8,493) | 0.74x (8,380) | 0.76x (8,474) |
| FullScan1000 | 1.90x (528,462) | 1.32x (452,853) | 1.32x (463,993) | 1.11x (462,710) | 1.29x (475,830) |
| IndexedLookup | 0.80x (8,590) | 0.79x (8,605) | 0.76x (8,626) | 0.74x (8,576) | 0.75x (8,726) |
| LargeTextRead | 0.95x (203,181) | 0.76x (205,559) | 1.08x (172,397) | 1.14x (192,367) | 1.17x (196,948) |
| LargeTextWrite | 0.86x (104,981) | 0.87x (104,925) | 0.88x (100,929) | 0.86x (103,457) | 0.89x (99,992) |
| LargeBlobRead | 1.07x (366,280) | 0.95x (534,870) | 1.26x (340,373) | 0.92x (512,773) | 0.93x (485,396) |
| LargeBlobWrite | 0.94x (104,662) | 0.85x (104,467) | 0.89x (100,912) | 0.86x (103,707) | 0.91x (99,619) |
| WALReaders | 0.89x (3,662) | 0.89x (3,619) | 0.91x (3,450) | 0.85x (3,646) | 0.88x (3,668) |
| WALWriterContention | 0.68x (19,465) | 0.73x (18,162) | 0.66x (19,328) | 0.73x (16,416) | 0.70x (17,730) |
| FTS5Search | 0.55x (71,606) | 0.61x (71,729) | 0.55x (70,454) | 0.57x (74,609) | 0.53x (71,841) |
| JSONQuery | 0.85x (4,762) | 0.81x (4,687) | 0.75x (4,575) | 0.73x (4,653) | 0.73x (4,638) |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo standard run | PureGo embed run | CGO dynamic standard run | CGO dynamic embed run | CGO static run |
|---|---:|---:|---:|---:|---:|
| OpenClose | 79,163 | 82,214 | 81,731 | 81,445 | 80,377 |
| SelectConstant | 2,426 | 2,346 | 2,438 | 2,347 | 2,369 |
| SingleInsert | 11,099 | 10,970 | 11,189 | 11,080 | 11,058 |
| PreparedInsert | 9,277 | 9,352 | 9,232 | 9,284 | 9,223 |
| BatchInsert1000 | 687,215 | 683,390 | 674,157 | 701,361 | 669,220 |
| BatchInsert1000Values | 241,579 | 241,704 | 240,419 | 236,597 | 243,336 |
| TransactionInsert100000 | 66,182,226 | 67,399,237 | 65,321,080 | 68,217,387 | 64,278,950 |
| SingleRowSelect | 6,560 | 6,510 | 6,846 | 6,479 | 6,694 |
| FullScan1000 | 489,496 | 488,461 | 490,027 | 493,096 | 452,646 |
| IndexedLookup | 6,557 | 6,497 | 6,772 | 6,515 | 6,622 |
| LargeTextRead | 179,027 | 157,057 | 170,724 | 209,101 | 221,291 |
| LargeTextWrite | 188,974 | 89,674 | 88,216 | 87,301 | 88,533 |
| LargeBlobRead | 373,618 | 465,574 | 364,308 | 440,513 | 432,662 |
| LargeBlobWrite | 88,979 | 87,299 | 88,346 | 89,730 | 88,941 |
| WALReaders | 3,281 | 3,252 | 3,301 | 3,342 | 3,314 |
| WALWriterContention | 11,990 | 11,575 | 11,491 | 11,752 | 11,644 |
| FTS5Search | 39,702 | 42,928 | 39,286 | 43,555 | 38,517 |
| JSONQuery | 3,671 | 3,520 | 3,557 | 3,558 | 3,534 |

<!-- END GENERATED COMBINED RESULTS -->

## Optional row-reader evidence

The embed columns use the actual Linux amd64 glibc asset from embed commit
`d721bc5`, built by [asset workflow run](https://github.com/yuhaiin/purego-sqlite-embed/actions/runs/41793502034).
Both embed configurations report row and scalar bridge features; standard/static
configurations report neither. The [manifest](results/2026-11-05-current-environment.json)
records the exact binary hash and compiler. glibc 2.17 runtime compatibility is
validated independently from these performance measurements.

A [same-library CGO-free comparison](results/2026-11-05-current-matched-purego.txt)
uses the identical distributed asset, connection settings and database/sql scan
loop, disabling only ReadRow. Seven 500ms samples give FullScan1000 medians of
**1,016,805ns standard** and **582,408ns with the reader**: approximately
**42.7% less time**, or **1.75x throughput**.
This isolates the reader from compiler/engine differences in the full table.

In the full embed run, PureGo takes 597,124ns versus matching mattn
488,461ns and modernc 452,853ns, time ratios **1.22x** and **1.32x**.
Universal performance parity is not claimed.

The reader reduces the three-column row from about eight boundary crossings to
**two**: step plus one read of every column. Storage classes are checked per row;
strings and BLOBs are copied before advancing/resetting/finalizing. Each
connection owns reusable SQLite-allocated scratch, 24 bytes per cached column
plus allocator overhead, freed on Close. It is excluded from Go heap counters.
This is not a zero-copy payload API.

To reproduce the controlled comparison:

```sh
TMPDIR=/dev/shm GOMAXPROCS=4 CGO_ENABLED=0 \
  SQLITE_TEST_LIBRARY="$SQLITE_EMBED_NATIVE_DIR/libsqlite3.so" \
  taskset -c "$BENCH_CPUSET" go test -run '^$' -bench '^BenchmarkRowReader$' \
  -benchtime=500ms -count=7 -benchmem
```

For the full comparison, prepare `SQLITE_EMBED_NATIVE_DIR` with `libsqlite3.so`
from the embed asset and `src/sqlite3.h` from its matching pinned source, then
use the commands above. To build a local fixture instead:

```sh
go run ./internal/cmd/build-native --row-bridge --exec-bridge --output .native/row-bridge
```

Select that directory for the library. Its GCC engine differs from the
distributed Zig/Clang asset. glibc runtime compatibility
is validated separately; it is not inferred from timing on this host.

## Scalar execution evidence

The optional `purego_sqlite_exec_scalar_v1` bridge handles complete positional
INTEGER, REAL, BOOLEAN and NULL parameters for Exec. It binds every slot,
drains results, captures the row ID and changes, and resets in one native call.
It applies to ordinary single statements as well as batches: the driver does not
recognize benchmark names, table names, SQL templates or batch sizes. TEXT/BLOB,
named arguments and Query use the existing path. The official engine is unchanged.

A [same-library A/B run](results/2026-11-05-current-matched-purego.txt) uses the identical
distributed asset, file database, WAL settings, CPU affinity and database/sql
transaction loop, disabling only ExecScalar in the standard case. With
CGO_ENABLED=0 and seven 500ms samples per case, median times are:

- `BatchInsert1000`: 1,000,004 → 702,699 ns (29.7% less time).
- `BatchInsert1000Values`: 299,818 → 216,763 ns (27.7% less time).
- `TransactionInsert100000`: 97,282,504 → 68,121,248 ns (30.0% less time).

This isolates the scalar executor from compiler/engine differences. Reproduce
against the selected asset with:

```sh
CGO_ENABLED=0 SQLITE_TEST_LIBRARY="$SQLITE_EMBED_NATIVE_DIR/libsqlite3.so" \
  GOMAXPROCS=4 TMPDIR=/dev/shm taskset -c "$BENCH_CPUSET" \
  go test -run '^$' -bench '^BenchmarkScalarExec$' -benchmem -benchtime=500ms -count=7
```

The three workloads use approximately 1,000 / 1 / 100,000 execution calls in
this path instead of 5,000 / 1,004 / 500,000 calls (excluding prepare/transaction
setup). Go allocation counts are unchanged within sampling variation. Native
scratch is additional: 16 bytes for results plus 16 bytes per cached parameter,
reused under the connection lock and freed on Close. It contains no Go pointers.

Regression coverage interleaves scalar and payload operations and exercises
UPSERT, UPDATE, DELETE, triggers, rollback, constraint recovery, RETURNING,
read-only Exec metadata, invalid arguments, interrupted writes and subsequent
statement reuse. A failed helper never falls through to another execution.

## Standard-API optimization and remaining overhead

The standard columns above use **standard public SQLite symbols**. They work with system
SQLite; no custom exports, patched `.so`, SQLite private layouts or additional
native helper library are required. The system library at `/usr/lib/libsqlite3.so.0`
was also tested with CGO disabled and with the dynamic CGO backend. The native
library used for timing retains its original manifest hash.

CGO previously crossed the Go/C boundary seven times per prepared integer INSERT:
bind, step, statement-readonly check, changes, last-row ID, reset and clear.
Execution/result collection now uses a typed C bridge **compiled into the Go
CGO driver**, calling the original library through its public function pointers.
Reset/clear is combined for payloads and error recovery. Scalar-only successes
need reset only. The resulting count is **three crossings per row**, versus
mattn's four; SQLite itself is unchanged.

PureGo improvements:

- A private scalar argument frame under the connection mutex avoids allocating
  variadic argument slices on each integer FFI call. Native pointer results and
  NUL-terminated metadata are copied into Go-owned values.
- Prepare, TEXT/BLOB binding and metadata reads avoid reflective conversion.
  Go buffer arguments retain direct `SyscallN` lifetime tracking. Floating-point
  arguments/returns keep their typed ABI bindings.
- Column declaration types are loaded when `ColumnTypes` or opt-in
  `WithMattnTypes` decoding needs them, and refreshed after each query's initial
  step/reprepare. Parameter names are
  loaded only for named arguments or detailed validation errors.
- Prepare reuses at most 4 KiB of NUL-terminated SQL scratch space per connection.
  SQLite retains its own SQL copy. Large inputs use temporary storage, and Close
  releases the scratch buffer.
- Every execution validates and rebinds all parameter slots before stepping.
  Successful scalar-only executions may retain scalar bindings; TEXT/BLOB payloads
  and every failed bind/step still clear bindings. One-shot executions finalize
  immediately, so a separate reset/clear is redundant.
- A conservative leading DML keyword hint avoids repeatedly asking whether a
  plain INSERT/UPDATE/DELETE/REPLACE is readonly. Other SQL, including leading
  comments, CTEs, PRAGMAs and EXPLAIN, still uses the native readonly check.
  Prepared scalar INSERTs using the standard API need **five native calls per row** instead of seven. Libraries with the scalar executor described above combine them into one.

Regression tests cover metadata copies after native finalization, Unicode names,
NULL declaration types, automatic schema reprepare, preserved SQL after scratch
reuse, positional/named mode changes, partial rebinds, constraint failures,
RETURNING/SELECT draining, late errors, cancellation and concurrent connections.

The current SQL table uses **7,304 Go allocations per 1000 rows**, compared
with mattn's 7,308 and modernc's 7,305. Ordinary single-row SELECT uses 24
allocations in this run. Allocation counts alone do not establish timing parity.

Without the optional bridge, the three-column scan needs about **eight native calls per row**: step,
three type reads, two integer reads, TEXT pointer and TEXT length. Transition
and marshalling costs matter when the engine does little work per native call.
The driver retains storage-class checks, owned data copies and connection
lifetime synchronization.

The 1000-row VALUES case and the original 1000-execution case deliberately remain
separate. They illustrate different SQL shapes, including parse/binding costs;
the driver does not rewrite statements or silently batch operations. The results
support workload-specific comparisons, not an assumption that a native engine
must win every `database/sql` workload.

## Results retention

`results/` retains the current published reports, their raw samples, the
same-library A/B log and the environment manifest. Superseded runs and
intermediate experiments remain available in Git history.
