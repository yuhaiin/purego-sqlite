# SQLite driver benchmarks

The results below compare **PureGo, dynamic CGO, static CGO, mattn/go-sqlite3,
and modernc.org/sqlite** across all 17 workloads. This is a separate Go module:
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

From the repository root on Linux, with Go 1.26+, Python 3 and a C compiler:

```sh
python3 scripts/build-native.py
for backend in purego cgo-dynamic cgo; do
  BENCH_BACKEND="$backend" scripts/benchmark.sh -benchtime=300ms -count=5
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend.txt"
done
python3 benchmarks/compare.py \
  purego=benchmarks/results/local-purego.txt \
  cgo-dynamic=benchmarks/results/local-cgo-dynamic.txt \
  cgo=benchmarks/results/local-cgo.txt \
  > benchmarks/results/local-five-way.md
```

The wrapper builds a pinned shared library if needed and sets the C header,
linker and runtime search paths. It checks native source/options and verifies
WAL, synchronous, cache and busy-timeout settings on all four simultaneously open
connections for all drivers before measuring. Run one backend by setting
`BENCH_BACKEND=purego`, `cgo-dynamic` or `cgo`. Each run includes mattn and modernc.

To smoke test every workload, use `scripts/benchmark.sh -benchtime=1x -count=1`.
One-iteration smoke results do not constitute performance measurements. CI runs
all three backend selections and retains their logs as artifacts.

## Measurement environment and methodology

Measured on **2026-11-04**, implementation commit `612ecc2`, Linux/amd64,
AMD Ryzen 5 5600G, Go `go1.27.3-X:nodwarf5`, GCC 16.2.1 and **GOMAXPROCS=4**.
The [environment manifest](results/2026-11-04-five-way-environment.json) includes
exact commands, build tags, versions and the pinned native source/library hashes.

- Each backend run has five sequential 300ms samples per implementation and
  workload. The three runs execute sequentially. Our three backend columns each
  contain five samples; the mattn and modernc controls pool all **15 samples**
  across those executables. Each native backend ratio uses its own five matching
  mattn controls; modernc uses the pooled controls. The report also lists all
  matching control medians so the ratios can be checked directly.
- Databases are files under `/tmp`, which is **tmpfs** on this host. Results
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
- Large TEXT/BLOB values contain 1,048,575 bytes. Writes update one row to the
  same value, measuring binding, comparison/update and copying rather than
  changed-content storage bandwidth or database growth.
- Open/close includes connector creation, Ping, per-connection settings and Close
  on an existing file. Other setup/seeding and teardown are excluded.
- All numbers are medians. No CPU affinity, frequency lock, background-load
  isolation or confidence intervals were used. Large transactions have only a
  few iterations per sample; contention results can vary with scheduling.
- B/op and allocs/op count **Go heap** allocations. They are not total engine
  memory: native and translated-engine allocators are not accounted for equally.
- Throughput counts benchmark units per second: one whole batch/transaction or
  scan in those workloads, and one SQL operation in the other workloads.

The initial PureGo run showed simultaneous control slowdowns during scans and
large payloads. We repeated the whole PureGo run with identical settings and
retained the [initial exploratory log](results/2026-11-04-exploratory-purego-linux-amd64.txt).
The repeat still showed a late-run slowdown in JSON for both PureGo and mattn:
11,904ns versus its matching 8,360ns control (1.42x), while the pooled mattn
median is 4,435ns. This is why ratios use matching controls. Small differences
and absolute rankings across runs should be treated as exploratory.

## Results

Raw five-sample runs:
[PureGo](results/2026-11-04-optimized-purego-linux-amd64.txt),
[CGO dynamic](results/2026-11-04-optimized-cgo-dynamic-linux-amd64.txt),
[CGO static](results/2026-11-04-optimized-cgo-linux-amd64.txt).
The [generated full report](results/2026-11-04-five-way-linux-amd64.md) is reproduced
below, including every workload's time, ratio, allocations, bytes and throughput.

<!-- BEGIN GENERATED RESULTS -->
Medians of repeated samples. Time ratio = implementation / matching-run mattn (lower is better).
mattn and modernc columns pool controls across runs; each native backend's ratio uses its own run's mattn median.

- PureGo: 5 samples per workload
- CGO dynamic: 5 samples per workload
- CGO static: 5 samples per workload
- mattn: 15 samples per workload
- modernc.org/sqlite: 15 samples per workload

### Time: ns/op (ratio to matching-run mattn)

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 73,987 (0.98x) | 68,488 (0.89x) | 65,505 (0.88x) | 76,480 | 114,179 (1.49x) |
| SelectConstant | 3,822 (1.37x) | 2,661 (0.94x) | 2,656 (0.96x) | 2,794 | 2,887 (1.03x) |
| SingleInsert | 12,729 (1.13x) | 11,355 (0.99x) | 11,362 (1.01x) | 11,252 | 14,345 (1.27x) |
| PreparedInsert | 9,783 (1.09x) | 9,203 (1.02x) | 9,276 (1.03x) | 9,035 | 11,412 (1.26x) |
| BatchInsert1000 | 1,193,649 (1.53x) | 761,394 (0.97x) | 734,045 (0.98x) | 779,943 | 1,008,234 (1.29x) |
| TransactionInsert100000 | 118,860,162 (1.54x) | 74,258,070 (0.95x) | 71,847,474 (0.97x) | 77,100,370 | 100,996,005 (1.31x) |
| SingleRowSelect | 10,439 (1.35x) | 7,470 (0.98x) | 7,676 (1.01x) | 7,667 | 10,401 (1.36x) |
| FullScan1000 | 1,148,494 (1.88x) | 752,637 (1.23x) | 718,289 (1.27x) | 606,664 | 564,790 (0.93x) |
| IndexedLookup | 9,844 (1.24x) | 7,804 (0.99x) | 8,173 (0.95x) | 8,071 | 11,641 (1.44x) |
| LargeTextRead | 154,788 (1.15x) | 135,491 (1.09x) | 134,721 (1.04x) | 129,261 | 158,788 (1.23x) |
| LargeTextWrite | 80,827 (1.01x) | 80,682 (1.01x) | 81,465 (1.01x) | 80,207 | 104,092 (1.30x) |
| LargeBlobRead | 395,718 (1.01x) | 253,772 (0.99x) | 284,365 (1.02x) | 278,360 | 293,786 (1.06x) |
| LargeBlobWrite | 81,664 (1.02x) | 80,554 (1.01x) | 80,875 (1.00x) | 80,323 | 104,943 (1.31x) |
| WALReaders | 2,099 (0.94x) | 2,081 (0.93x) | 2,067 (0.93x) | 2,226 | 3,657 (1.64x) |
| WALWriterContention | 13,696 (0.95x) | 13,289 (1.02x) | 14,101 (1.10x) | 13,307 | 19,618 (1.47x) |
| FTS5Search | 41,833 (1.07x) | 39,781 (0.99x) | 38,897 (0.98x) | 39,711 | 83,939 (2.11x) |
| JSONQuery | 11,904 (1.42x) | 4,282 (1.00x) | 4,532 (1.01x) | 4,435 | 5,708 (1.29x) |

### Go memory: B/op / allocs/op

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 3,905 / 92 | 2,314 / 39 | 2,290 / 39 | 2,973 / 64 | 4,593 / 83 |
| SelectConstant | 704 / 26 | 496 / 18 | 496 / 18 | 536 / 14 | 480 / 13 |
| SingleInsert | 408 / 14 | 280 / 9 | 280 / 9 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,309 / 5,524 | 125,078 / 5,517 | 125,071 / 5,517 | 140,778 / 6,508 | 131,648 / 6,502 |
| TransactionInsert100000 | 12,799,666 / 599,533 | 12,797,062 / 599,517 | 12,797,084 / 599,517 | 14,396,785 / 699,508 | 13,595,700 / 699,502 |
| SingleRowSelect | 1,184 / 49 | 736 / 28 | 736 / 28 | 736 / 26 | 672 / 24 |
| FullScan1000 | 83,016 / 7,333 | 82,488 / 7,308 | 82,488 / 7,308 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 944 / 36 | 608 / 22 | 608 / 22 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,645 / 29 | 1,049,130 / 19 | 1,049,130 / 19 | 1,049,138 / 16 | 1,049,091 / 16 |
| LargeTextWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,490 / 31 | 2,097,733 / 20 | 2,097,732 / 20 | 2,097,740 / 17 | 2,097,690 / 17 |
| LargeBlobWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 760 / 29 | 544 / 20 | 544 / 20 | 568 / 18 | 512 / 17 |
| WALWriterContention | 264 / 8 | 216 / 7 | 216 / 7 | 72 / 4 | 76 / 4 |
| FTS5Search | 960 / 35 | 624 / 21 | 624 / 21 | 600 / 17 | 568 / 18 |
| JSONQuery | 952 / 33 | 616 / 19 | 616 / 19 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 13,516 | 14,601 | 15,266 | 13,075 | 8,758 |
| SelectConstant | 261,654 | 375,737 | 376,476 | 357,962 | 346,409 |
| SingleInsert | 78,564 | 88,068 | 88,011 | 88,875 | 69,710 |
| PreparedInsert | 102,222 | 108,659 | 107,803 | 110,678 | 87,623 |
| BatchInsert1000 | 838 | 1,313 | 1,362 | 1,282 | 992 |
| TransactionInsert100000 | 8 | 13 | 14 | 13 | 10 |
| SingleRowSelect | 95,793 | 133,866 | 130,283 | 130,434 | 96,148 |
| FullScan1000 | 871 | 1,329 | 1,392 | 1,648 | 1,771 |
| IndexedLookup | 101,582 | 128,142 | 122,353 | 123,903 | 85,907 |
| LargeTextRead | 6,460 | 7,381 | 7,423 | 7,736 | 6,298 |
| LargeTextWrite | 12,372 | 12,394 | 12,275 | 12,468 | 9,607 |
| LargeBlobRead | 2,527 | 3,941 | 3,517 | 3,592 | 3,404 |
| LargeBlobWrite | 12,245 | 12,414 | 12,365 | 12,450 | 9,529 |
| WALReaders | 476,466 | 480,651 | 483,874 | 449,196 | 273,466 |
| WALWriterContention | 73,014 | 75,252 | 70,919 | 75,146 | 50,973 |
| FTS5Search | 23,905 | 25,138 | 25,709 | 25,182 | 11,913 |
| JSONQuery | 84,003 | 233,517 | 220,631 | 225,458 | 175,180 |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo run | CGO dynamic run | CGO static run |
|---|---:|---:|---:|
| OpenClose | 75,871 | 77,259 | 74,535 |
| SelectConstant | 2,794 | 2,823 | 2,779 |
| SingleInsert | 11,237 | 11,444 | 11,234 |
| PreparedInsert | 8,991 | 9,021 | 9,049 |
| BatchInsert1000 | 778,673 | 783,462 | 751,747 |
| TransactionInsert100000 | 77,100,370 | 77,775,754 | 74,103,350 |
| SingleRowSelect | 7,714 | 7,643 | 7,611 |
| FullScan1000 | 609,298 | 609,755 | 565,131 |
| IndexedLookup | 7,966 | 7,916 | 8,562 |
| LargeTextRead | 135,015 | 124,729 | 129,261 |
| LargeTextWrite | 79,833 | 80,188 | 80,785 |
| LargeBlobRead | 391,887 | 255,537 | 278,360 |
| LargeBlobWrite | 79,861 | 79,970 | 80,484 |
| WALReaders | 2,244 | 2,232 | 2,218 |
| WALWriterContention | 14,462 | 12,988 | 12,843 |
| FTS5Search | 39,184 | 40,016 | 39,711 |
| JSONQuery | 8,360 | 4,299 | 4,477 |
<!-- END GENERATED RESULTS -->

## Measured optimization and remaining overhead

CGO previously crossed the Go/C boundary seven times per prepared integer INSERT:
bind, step, statement-readonly check, changes, last-row ID, reset and clear.
Execution/result collection and reset/clear now run in two typed C helpers,
reducing the total to **three crossings per row**, versus mattn's four. The helpers
still drain SELECT/RETURNING rows, propagate late errors before reset, preserve
result metadata and clear bindings after failures. The complete table above
shows the current batch results; the older 20% gap is not a fixed SQLite cost.

PureGo now reuses a private scalar argument frame under the connection mutex,
avoiding escaping argument-slice allocations on each integer FFI call. Prepare,
TEXT/BLOB binding and column-pointer reads also avoid reflective conversion.
Go buffer addresses retain direct `SyscallN` lifetime tracking, SQLite copies
bindings with `SQLITE_TRANSIENT`, and rows copy native data before step/reset.
Floating-point calls retain their typed ABI. Cancellation uses a separate
interrupt binding, so it cannot overwrite an executing statement's arguments.

The earlier five-sample scan baseline used **19,348 Go allocations per 1000 rows**;
the optimized path uses about **7,333**, close to mattn's 7,308. PureGo still has
per-call native transition costs, particularly in batches and scans that invoke
several SQLite functions per row. Lower allocations do not imply timing parity;
the workload measurements above show the remaining differences.

Compared with the archived pre-optimization run, the PureGo scan median moved
from 1.66ms to 1.15ms (about 31% less time) and batch INSERT from 1.30ms to 1.19ms.
CGO's batch ratio moved from 1.20x mattn time to 0.97–0.98x in these runs. These
are exploratory measurements with the controls and limitations described above.

## Historical measurements

These files record earlier implementations and are retained for comparison;
they are not the current result table:

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
