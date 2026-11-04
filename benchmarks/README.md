# SQLite driver benchmarks

The results below compare **PureGo, dynamic CGO, static CGO, mattn/go-sqlite3,
and modernc.org/sqlite** across all 18 workloads. Standard and embed bridge
paths appear side by side in one combined comparison. This is a separate Go module:
mattn and modernc remain benchmark-only dependencies.

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
Set `SQLITE_EMBED_NATIVE_DIR` to a directory containing the embed asset
`libsqlite3.so` and its matching `src/sqlite3.h` before running the commands:

```sh
export TMPDIR=/dev/shm GOMAXPROCS=4 BENCH_CPUSET=0-3
python3 scripts/build-native.py
for backend in purego cgo-dynamic cgo; do
  BENCH_BACKEND="$backend" scripts/benchmark.sh -benchtime=300ms -count=5
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend.txt"
done
for backend in purego cgo-dynamic; do
  BENCH_BACKEND="$backend" BENCH_NATIVE_DIR="$SQLITE_EMBED_NATIVE_DIR" \
    scripts/benchmark.sh -benchtime=300ms -count=5
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

Measured on **2026-11-04**, Linux/amd64, **AMD EPYC 7763 64-Core Processor**, Go
`go1.27.3` and **GOMAXPROCS=4** in a dedicated GitHub Actions job.
All five configurations run sequentially on the same runner, pinned to logical
CPUs **0, 1, 2, 3**. The [workflow run](https://github.com/yuhaiin/purego-sqlite/actions/runs/41794719883)
uses driver commit `73cd1c6` and embed commit
`d721bc5`. The standard shared/static engine uses the runner's
C compiler; the embed asset uses Zig 0.17.0 / Clang 22.1.8. The
[environment manifest](results/2026-11-04-scalar-exec-environment.json) records CPU topology,
compiler flags, source/binary hashes, exact inputs and all raw-log names.
These replace the earlier Ryzen host measurements; absolute times from the two
machines must not be compared as a before/after result.

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

Earlier local diagnostic runs were affected by recurring video-transcoding
load and are excluded from this table. The formal measurements use one hosted
runner and matching controls. This does not guarantee exclusive physical-host
resources: small differences and cross-configuration absolute rankings remain
exploratory. The benchmark wrapper retains Go's default CGO optimization flags
(`-O2 -g`); older experiments that overrode those defaults are historical.

## Results

Both paths are in the **same table**. `standard` uses public SQLite APIs and
works with system libraries or older prebuilts. `embed` uses both optional
`purego_sqlite_read_row_v1` and `purego_sqlite_exec_scalar_v1` exports in the
companion's actual CI-built library. Static CGO uses the stock linked engine.
For FullScan1000, PureGo takes **1,427,835ns standard**
and **834,058ns embed**.

| Column | Native library / API path | Raw five-sample run |
| --- | --- | --- |
| PureGo standard | Official shared library, standard APIs | [raw](results/2026-11-04-scalar-exec-purego-linux-amd64.txt) |
| PureGo embed | CI-built embed library, row reader and scalar executor v1 | [raw](results/2026-11-04-scalar-exec-purego-bridge-linux-amd64.txt) |
| CGO dynamic standard | Official shared library, standard APIs | [raw](results/2026-11-04-scalar-exec-cgo-dynamic-linux-amd64.txt) |
| CGO dynamic embed | CI-built embed library, row reader and scalar executor v1 | [raw](results/2026-11-04-scalar-exec-cgo-dynamic-bridge-linux-amd64.txt) |
| CGO static | Stock official engine linked into the executable | [raw](results/2026-11-04-scalar-exec-cgo-linux-amd64.txt) |

The [generated combined report](results/2026-11-04-scalar-exec-combined-linux-amd64.md) is reproduced
below: all 18 workloads, seven configuration/control columns, time, Go memory and
throughput. mattn/modernc display columns pool **25 samples** per workload. Every
native ratio uses its own run's five matching controls; matching control medians
are included. Compiler and engine differences remain in the full comparison.

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
| OpenClose | 116,482 (1.00x) | 115,538 (0.98x) | 105,995 (0.87x) | 105,661 (0.92x) | 104,703 (0.90x) | 116,789 | 185,491 (1.59x) |
| SelectConstant | 3,511 (1.10x) | 3,461 (1.07x) | 2,964 (0.90x) | 3,095 (0.91x) | 2,924 (0.90x) | 3,254 | 3,438 (1.06x) |
| SingleInsert | 13,312 (1.04x) | 13,245 (1.04x) | 12,583 (0.98x) | 12,803 (0.99x) | 12,685 (0.99x) | 12,827 | 16,909 (1.32x) |
| PreparedInsert | 10,784 (1.06x) | 10,503 (1.05x) | 10,353 (1.01x) | 10,301 (1.01x) | 10,134 (1.00x) | 10,180 | 13,345 (1.31x) |
| BatchInsert1000 | 1,289,197 (1.36x) | 919,011 (0.97x) | 938,805 (0.95x) | 883,438 (0.90x) | 902,249 (0.97x) | 953,197 | 1,242,374 (1.30x) |
| BatchInsert1000Values | 405,834 (1.23x) | 301,324 (0.90x) | 333,284 (1.01x) | 301,358 (0.89x) | 346,286 (1.01x) | 336,485 | 577,094 (1.72x) |
| TransactionInsert100000 | 122,811,867 (1.35x) | 86,976,446 (0.96x) | 87,931,772 (0.93x) | 82,326,803 (0.88x) | 83,786,734 (0.94x) | 91,089,861 | 119,996,430 (1.32x) |
| SingleRowSelect | 9,168 (1.06x) | 8,869 (1.01x) | 8,113 (0.95x) | 8,195 (0.93x) | 8,110 (0.96x) | 8,628 | 12,498 (1.45x) |
| FullScan1000 | 1,427,835 (1.97x) | 834,058 (1.18x) | 883,841 (1.20x) | 715,752 (1.01x) | 884,529 (1.27x) | 713,206 | 696,488 (0.98x) |
| IndexedLookup | 9,087 (1.04x) | 9,443 (1.03x) | 8,369 (0.96x) | 8,669 (0.94x) | 8,267 (0.94x) | 8,830 | 14,074 (1.59x) |
| LargeTextRead | 226,560 (0.78x) | 262,312 (0.87x) | 357,331 (1.08x) | 338,473 (1.02x) | 313,999 (1.00x) | 308,290 | 288,638 (0.94x) |
| LargeTextWrite | 98,794 (1.01x) | 99,148 (0.86x) | 103,244 (1.05x) | 95,470 (0.99x) | 98,538 (0.86x) | 98,414 | 132,913 (1.35x) |
| LargeBlobRead | 697,134 (0.99x) | 649,204 (1.10x) | 659,079 (1.20x) | 709,540 (0.93x) | 746,521 (1.13x) | 640,678 | 686,876 (1.07x) |
| LargeBlobWrite | 122,231 (1.28x) | 123,120 (0.97x) | 124,033 (1.23x) | 95,525 (0.98x) | 121,477 (1.04x) | 100,799 | 131,096 (1.30x) |
| WALReaders | 4,599 (0.99x) | 4,467 (0.98x) | 4,213 (0.93x) | 4,276 (0.94x) | 4,224 (0.96x) | 4,491 | 5,995 (1.33x) |
| WALWriterContention | 15,119 (1.12x) | 15,588 (1.09x) | 14,185 (1.06x) | 15,096 (1.10x) | 16,434 (1.12x) | 13,891 | 22,816 (1.64x) |
| FTS5Search | 51,561 (1.01x) | 57,646 (1.00x) | 49,681 (1.00x) | 57,531 (0.99x) | 50,201 (1.00x) | 51,063 | 100,471 (1.97x) |
| JSONQuery | 5,538 (1.06x) | 5,594 (1.07x) | 4,871 (0.94x) | 5,009 (0.94x) | 4,761 (0.96x) | 5,222 | 6,886 (1.32x) |

### Go memory: B/op / allocs/op

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|---:|---:|
| OpenClose | 4,373 / 95 | 4,556 / 104 | 2,583 / 36 | 2,584 / 36 | 2,571 / 36 | 2,978 / 64 | 4,600 / 83 |
| SelectConstant | 520 / 15 | 520 / 15 | 472 / 15 | 472 / 15 | 472 / 15 | 536 / 14 | 480 / 13 |
| SingleInsert | 280 / 8 | 280 / 8 | 232 / 7 | 232 / 7 | 232 / 7 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,196 / 5,516 | 125,190 / 5,516 | 125,020 / 5,513 | 125,014 / 5,513 | 125,012 / 5,513 | 140,774 / 6,508 | 131,643 / 6,502 |
| BatchInsert1000Values | 42,296 / 32 | 42,290 / 32 | 42,135 / 29 | 42,139 / 29 | 42,142 / 29 | 41,902 / 25 | 41,791 / 19 |
| TransactionInsert100000 | 12,800,008 / 599,526 | 12,799,650 / 599,526 | 12,797,016 / 599,513 | 12,797,020 / 599,513 | 12,797,020 / 599,513 | 14,396,768 / 699,508 | 13,595,650 / 699,502 |
| SingleRowSelect | 704 / 24 | 704 / 24 | 640 / 23 | 640 / 23 | 640 / 23 | 736 / 26 | 672 / 24 |
| FullScan1000 | 82,470 / 7,304 | 82,474 / 7,304 | 82,408 / 7,303 | 82,408 / 7,303 | 82,408 / 7,303 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 640 / 20 | 640 / 20 | 536 / 18 | 536 / 18 | 536 / 18 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,371 / 18 | 1,049,376 / 18 | 1,049,082 / 17 | 1,049,084 / 17 | 1,049,085 / 17 | 1,049,141 / 16 | 1,049,094 / 16 |
| LargeTextWrite | 160 / 5 | 160 / 5 | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,220 / 20 | 2,098,210 / 20 | 2,097,682 / 18 | 2,097,681 / 18 | 2,097,684 / 18 | 2,097,743 / 17 | 2,097,692 / 17 |
| LargeBlobWrite | 160 / 5 | 160 / 5 | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 544 / 18 | 544 / 18 | 496 / 17 | 496 / 17 | 496 / 17 | 568 / 18 | 512 / 17 |
| WALWriterContention | 232 / 7 | 232 / 7 | 184 / 6 | 184 / 6 | 184 / 6 | 72 / 4 | 76 / 4 |
| FTS5Search | 640 / 20 | 640 / 20 | 544 / 18 | 544 / 18 | 544 / 18 | 600 / 17 | 568 / 18 |
| JSONQuery | 648 / 18 | 648 / 18 | 552 / 16 | 552 / 16 | 552 / 16 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|---:|---:|
| OpenClose | 8,585 | 8,655 | 9,434 | 9,464 | 9,551 | 8,562 | 5,391 |
| SelectConstant | 284,801 | 288,939 | 337,407 | 323,052 | 342,001 | 307,303 | 290,872 |
| SingleInsert | 75,118 | 75,502 | 79,472 | 78,107 | 78,834 | 77,960 | 59,140 |
| PreparedInsert | 92,729 | 95,207 | 96,587 | 97,078 | 98,676 | 98,235 | 74,932 |
| BatchInsert1000 | 776 | 1,088 | 1,065 | 1,132 | 1,108 | 1,049 | 805 |
| BatchInsert1000Values | 2,464 | 3,319 | 3,000 | 3,318 | 2,888 | 2,972 | 1,733 |
| TransactionInsert100000 | 8 | 12 | 11 | 12 | 12 | 11 | 8 |
| SingleRowSelect | 109,079 | 112,752 | 123,262 | 122,021 | 123,300 | 115,901 | 80,012 |
| FullScan1000 | 700 | 1,199 | 1,131 | 1,397 | 1,131 | 1,402 | 1,436 |
| IndexedLookup | 110,051 | 105,895 | 119,489 | 115,360 | 120,962 | 113,250 | 71,052 |
| LargeTextRead | 4,414 | 3,812 | 2,799 | 2,954 | 3,185 | 3,244 | 3,465 |
| LargeTextWrite | 10,122 | 10,086 | 9,686 | 10,474 | 10,148 | 10,161 | 7,524 |
| LargeBlobRead | 1,434 | 1,540 | 1,517 | 1,409 | 1,340 | 1,561 | 1,456 |
| LargeBlobWrite | 8,181 | 8,122 | 8,062 | 10,468 | 8,232 | 9,921 | 7,628 |
| WALReaders | 217,423 | 223,881 | 237,344 | 233,883 | 236,720 | 222,689 | 166,801 |
| WALWriterContention | 66,143 | 64,150 | 70,495 | 66,242 | 60,849 | 71,990 | 43,828 |
| FTS5Search | 19,395 | 17,347 | 20,128 | 17,382 | 19,920 | 19,584 | 9,953 |
| JSONQuery | 180,572 | 178,768 | 205,306 | 199,647 | 210,048 | 191,486 | 145,215 |

### Native time / matching-run modernc

Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static |
|---|---:|---:|---:|---:|---:|
| OpenClose | 0.65x (178,524) | 0.62x (185,502) | 0.57x (186,614) | 0.57x (186,023) | 0.56x (185,491) |
| SelectConstant | 1.02x (3,441) | 1.01x (3,443) | 0.86x (3,440) | 0.89x (3,461) | 0.87x (3,371) |
| SingleInsert | 0.81x (16,510) | 0.78x (16,908) | 0.73x (17,339) | 0.73x (17,632) | 0.77x (16,526) |
| PreparedInsert | 0.81x (13,242) | 0.80x (13,099) | 0.76x (13,565) | 0.76x (13,502) | 0.76x (13,310) |
| BatchInsert1000 | 1.07x (1,199,451) | 0.76x (1,213,012) | 0.75x (1,249,421) | 0.71x (1,244,474) | 0.72x (1,248,372) |
| BatchInsert1000Values | 0.68x (596,483) | 0.54x (555,818) | 0.58x (573,117) | 0.52x (577,094) | 0.60x (577,245) |
| TransactionInsert100000 | 1.06x (115,504,156) | 0.74x (117,170,010) | 0.73x (120,802,111) | 0.68x (121,535,230) | 0.70x (120,368,158) |
| SingleRowSelect | 0.75x (12,221) | 0.72x (12,402) | 0.65x (12,567) | 0.64x (12,904) | 0.67x (12,172) |
| FullScan1000 | 2.10x (680,705) | 1.21x (690,819) | 1.24x (710,079) | 1.01x (708,746) | 1.27x (696,488) |
| IndexedLookup | 0.66x (13,751) | 0.67x (14,074) | 0.58x (14,389) | 0.60x (14,465) | 0.59x (13,921) |
| LargeTextRead | 0.78x (288,638) | 0.79x (333,723) | 1.37x (261,248) | 1.03x (328,378) | 1.15x (272,243) |
| LargeTextWrite | 0.78x (127,348) | 0.70x (141,035) | 0.87x (118,219) | 0.72x (132,583) | 0.71x (138,183) |
| LargeBlobRead | 0.87x (799,440) | 0.89x (727,834) | 0.97x (677,825) | 1.06x (671,891) | 1.13x (658,703) |
| LargeBlobWrite | 0.84x (144,688) | 0.88x (140,516) | 1.04x (118,952) | 0.73x (131,735) | 1.03x (118,456) |
| WALReaders | 0.77x (6,001) | 0.74x (6,052) | 0.74x (5,701) | 0.72x (5,979) | 0.70x (6,067) |
| WALWriterContention | 0.62x (24,578) | 0.71x (22,079) | 0.62x (22,816) | 0.63x (24,099) | 0.74x (22,317) |
| FTS5Search | 0.53x (97,768) | 0.58x (99,855) | 0.49x (100,982) | 0.57x (100,829) | 0.50x (101,015) |
| JSONQuery | 0.83x (6,685) | 0.81x (6,927) | 0.71x (6,869) | 0.73x (6,898) | 0.69x (6,887) |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo standard run | PureGo embed run | CGO dynamic standard run | CGO dynamic embed run | CGO static run |
|---|---:|---:|---:|---:|---:|
| OpenClose | 116,585 | 117,669 | 121,456 | 115,343 | 116,367 |
| SelectConstant | 3,200 | 3,230 | 3,296 | 3,416 | 3,243 |
| SingleInsert | 12,794 | 12,742 | 12,827 | 12,873 | 12,776 |
| PreparedInsert | 10,128 | 9,968 | 10,292 | 10,234 | 10,118 |
| BatchInsert1000 | 950,897 | 949,404 | 993,372 | 984,479 | 927,318 |
| BatchInsert1000Values | 330,857 | 336,485 | 329,598 | 340,357 | 343,441 |
| TransactionInsert100000 | 90,715,522 | 90,967,956 | 94,382,388 | 93,728,142 | 88,864,160 |
| SingleRowSelect | 8,628 | 8,747 | 8,539 | 8,829 | 8,492 |
| FullScan1000 | 726,507 | 705,775 | 738,569 | 710,274 | 698,407 |
| IndexedLookup | 8,719 | 9,183 | 8,707 | 9,243 | 8,790 |
| LargeTextRead | 288,769 | 301,981 | 329,992 | 333,116 | 313,809 |
| LargeTextWrite | 97,476 | 115,735 | 98,522 | 96,383 | 114,820 |
| LargeBlobRead | 701,691 | 590,981 | 551,365 | 759,552 | 662,476 |
| LargeBlobWrite | 95,792 | 127,186 | 100,799 | 97,475 | 116,643 |
| WALReaders | 4,636 | 4,554 | 4,516 | 4,543 | 4,396 |
| WALWriterContention | 13,455 | 14,281 | 13,359 | 13,763 | 14,610 |
| FTS5Search | 51,043 | 57,591 | 49,564 | 58,292 | 50,258 |
| JSONQuery | 5,236 | 5,230 | 5,182 | 5,303 | 4,973 |

<!-- END GENERATED COMBINED RESULTS -->

## Optional row-reader evidence

The embed columns use the actual Linux amd64 glibc asset from embed commit
`d721bc5`, built by [candidate workflow run](https://github.com/yuhaiin/purego-sqlite-embed/actions/runs/41793502034).
Both embed configurations report row and scalar bridge features; standard/static
configurations report neither. The [manifest](results/2026-11-04-scalar-exec-environment.json)
records the exact binary hash and compiler. glibc 2.17 runtime compatibility is
validated independently from these performance measurements.

A [same-library CGO-free comparison](results/2026-11-04-scalar-exec-matched-purego.txt)
uses the identical distributed asset, connection settings and database/sql scan
loop, disabling only ReadRow. Seven 500ms samples give FullScan1000 medians of
**1,359,325ns standard** and **828,726ns with the reader**: approximately
**39.0% less time**, or **1.64x throughput**.
This isolates the reader from compiler/engine differences in the full table.

In the full embed run, PureGo takes 834,058ns versus matching mattn
705,775ns and modernc 690,819ns, time ratios **1.18x** and **1.21x**.
Universal performance parity is not claimed.

The reader reduces the three-column row from about eight boundary crossings to
**two**: step plus one read of every column. Storage classes are checked per row;
strings and BLOBs are copied before advancing/resetting/finalizing. Each
connection owns reusable SQLite-allocated scratch, 24 bytes per cached column
plus allocator overhead, freed on Close. It is excluded from Go heap counters.
This is not a zero-copy payload API.

An initial [variadic-frame probe](results/2026-11-04-row-bridge-matched-purego.txt)
added about 1000 Go allocations per scan. A [Go-buffer pinning probe](results/2026-11-04-row-bridge-pinned-buffer-purego.txt)
removed these but increased time, and was rejected. The final implementation
uses native scratch with a private scalar frame per connection. A preliminary
[full GCC-fixture run](results/2026-11-04-row-bridge-fixture-purego-linux-amd64.txt)
is retained separately and excluded from the combined report.

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
python3 scripts/build-native.py --row-bridge --exec-bridge --output .native/row-bridge
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

A [same-library A/B run](results/2026-11-04-scalar-exec-matched-purego.txt) uses the identical
distributed asset, file database, WAL settings, CPU affinity and database/sql
transaction loop, disabling only ExecScalar in the standard case. With
CGO_ENABLED=0 and seven 500ms samples per case, median times are:

- `BatchInsert1000`: 1,304,440 → 934,988 ns (28.3% less time).
- `BatchInsert1000Values`: 397,078 → 298,606 ns (24.8% less time).
- `TransactionInsert100000`: 125,327,532 → 88,825,819 ns (29.1% less time).

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

[Targeted before/after probes and rejected experiments](results/2026-11-04-optimization-probes.md)
record the intermediate measurements; they are excluded from the final table.

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
- Column declaration types are loaded only when `ColumnTypes` requests them,
  and refreshed after each query's initial step/reprepare. Parameter names are
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
  Prepared scalar INSERTs using the standard API need **five native calls per row** instead of seven. Libraries with the scalar executor below combine them into one.

Regression tests cover metadata copies after native finalization, Unicode names,
NULL declaration types, automatic schema reprepare, preserved SQL after scratch
reuse, positional/named mode changes, partial rebinds, constraint failures,
RETURNING/SELECT draining, late errors, cancellation and concurrent connections.

The archived pre-optimization scan used **19,348 Go allocations per 1000 rows**.
The latest path uses about **7,304**, comparable to mattn's 7,308 and modernc's
7,305. Ordinary single-row SELECT allocations fall from 74 to about 24. Lower
allocation counts do not guarantee timing parity.

Without the optional bridge, the three-column scan needs about **eight native calls per row**: step,
three type reads, two integer reads, TEXT pointer and TEXT length. The profile
attributes about 66% of CPU samples to the `SyscallN` call stack, including
native execution; this must not all be interpreted as boundary overhead. The
remaining transition/marshalling cost matters when engine work per call is small.
An experiment removing one Go call-frame closure showed no convincing scan
improvement and was not retained. No correctness checks, native data copies or
connection lifetime synchronization were removed to improve the numbers.

The 1000-row VALUES case and the original 1000-execution case deliberately remain
separate. They illustrate different SQL shapes, including parse/binding costs;
the driver does not rewrite statements or silently batch operations. The results
support workload-specific comparisons, not an assumption that a native engine
must win every `database/sql` workload.

## Historical measurements

These files record earlier implementations and are retained for comparison;
they are not the current result table:

- [Earlier combined Ryzen results](results/2026-11-04-combined-linux-amd64.md)
  and [environment](results/2026-11-04-combined-environment.json), before the scalar executor.

- [2026-11-04 pre-optimization CGO](results/2026-11-04-cgo-linux-amd64.md)
  and [raw samples](results/2026-11-04-cgo-linux-amd64.txt).
- [2026-11-04 pre-optimization PureGo](results/2026-11-04-purego-linux-amd64.md)
  and [raw samples](results/2026-11-04-purego-linux-amd64.txt).
- [2026-11-03 PureGo](results/2026-11-03-linux-amd64.md),
  [raw samples](results/2026-11-03-linux-amd64.txt), and
  [native manifest](results/2026-11-03-native-manifest.json).
- The first integer FFI experiment used GOMAXPROCS=12:
  [before](results/2026-11-03-before-fast-ffi.txt) and
  [after](results/2026-11-03-after-fast-ffi.txt). Its concurrency timings are not
  directly comparable to GOMAXPROCS=4 runs.

The first five-way iteration, before the additional metadata/cleanup work, is
also retained as raw logs:
[PureGo](results/2026-11-04-optimized-purego-linux-amd64.txt),
[dynamic CGO](results/2026-11-04-optimized-cgo-dynamic-linux-amd64.txt),
[static CGO](results/2026-11-04-optimized-cgo-linux-amd64.txt), and the
[earlier noisy PureGo experiment](results/2026-11-04-exploratory-purego-linux-amd64.txt).
Those runs contain the original 17 workloads.

The unpinned 18-workload investigation before restoring default CGO CFLAGS is
retained separately:
[PureGo](results/2026-11-04-final-purego-linux-amd64.txt),
[dynamic CGO](results/2026-11-04-final-cgo-dynamic-linux-amd64.txt), and
[static CGO](results/2026-11-04-final-cgo-linux-amd64.txt).
