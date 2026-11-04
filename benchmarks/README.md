# SQLite driver benchmarks

The results below compare **PureGo, dynamic CGO, static CGO, mattn/go-sqlite3,
and modernc.org/sqlite** across all 18 workloads. This is a separate Go module:
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
  BENCH_CPUSET=2-5 BENCH_BACKEND="$backend" scripts/benchmark.sh -benchtime=300ms -count=5
  cp benchmarks/results/local-raw.txt "benchmarks/results/local-$backend.txt"
done
python3 benchmarks/compare.py \
  purego=benchmarks/results/local-purego.txt \
  cgo-dynamic=benchmarks/results/local-cgo-dynamic.txt \
  cgo=benchmarks/results/local-cgo.txt \
  > benchmarks/results/local-five-way.md
```

The published run pins logical CPUs 2–5, which are four distinct physical cores
on this host. Choose suitable CPUs on your machine, or omit `BENCH_CPUSET`.
Affinity applies only to the benchmark process and its children.

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

Measured on **2026-11-04**, implementation commit `45a273e`, Linux/amd64,
AMD Ryzen 5 5600G, Go `go1.27.3-X:nodwarf5`, GCC 16.2.1 and **GOMAXPROCS=4**.
The [environment manifest](results/2026-11-04-five-way-environment.json) includes
exact commands, build tags, versions and the pinned native source/library hashes.

- Each backend run has five sequential 300ms samples per implementation and
  workload. The three runs execute sequentially on the same CPU set. Our backend columns each
  contain five samples; the mattn and modernc controls pool all **15 samples**
  across those executables. Each native backend ratio uses its own five matching
  mattn controls; modernc uses the pooled controls. The report also lists all
  matching control medians so the ratios can be checked directly. A separate
  table also compares each native backend with its matching modernc controls.
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
  `BatchInsert1000Values` separately prepares one 1000-placeholder INSERT per
  transaction, executes it once and commits. Its 1000 int64 arguments are built
  outside timing; SQL preparation, binding, execution, statement close and commit
  are timed. The original 1000 separate executions remain in the results.
- Large TEXT/BLOB values contain 1,048,575 bytes. Writes update one row to the
  same value, measuring binding, comparison/update and copying rather than
  changed-content storage bandwidth or database growth.
- Open/close includes connector creation, Ping, per-connection settings and Close
  on an existing file. Other setup/seeding and teardown are excluded.
- All numbers are medians. Process affinity pins four physical cores; frequency
  is not locked and the host still has background load. No confidence intervals
  or exclusive CPU isolation were used. Large transactions have only a
  few iterations per sample; contention results can vary with scheduling.
- B/op and allocs/op count **Go heap** allocations. They are not total engine
  memory: native and translated-engine allocators are not accounted for equally.
- Throughput counts benchmark units per second: one whole batch/transaction or
  scan in those workloads, and one SQL operation in the other workloads.

Earlier experiments showed simultaneous control slowdowns, including large
values and JSON. Their raw logs are retained in the historical results. Final
results below use fresh complete runs after the standard-API optimizations.
The published run also restores Go's default CGO optimization flags (`-O2 -g`)
and pins the process to four physical cores. Earlier scripts overrode the
CFLAGS defaults while adding the header path; SQLite itself was still built
with `-O2`, but CGO bridges were not. Older raw logs are historical and should
not be treated as identical build/CPU configurations. Ratios use matching
controls; small differences and cross-run absolute rankings remain exploratory.

## Results: standard SQLite API

Raw five-sample runs:
[PureGo](results/2026-11-04-pinned-purego-linux-amd64.txt),
[CGO dynamic](results/2026-11-04-pinned-cgo-dynamic-linux-amd64.txt),
[CGO static](results/2026-11-04-pinned-cgo-linux-amd64.txt).
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
| OpenClose | 73,861 (0.97x) | 64,772 (0.86x) | 64,820 (0.86x) | 75,026 | 113,703 (1.52x) |
| SelectConstant | 3,026 (1.07x) | 2,560 (0.93x) | 2,566 (0.92x) | 2,800 | 2,875 (1.03x) |
| SingleInsert | 11,674 (1.04x) | 11,214 (1.00x) | 10,893 (0.98x) | 11,184 | 14,560 (1.30x) |
| PreparedInsert | 9,587 (1.06x) | 9,279 (1.02x) | 9,156 (1.01x) | 9,053 | 11,625 (1.28x) |
| BatchInsert1000 | 1,073,860 (1.36x) | 731,747 (0.93x) | 722,785 (0.95x) | 785,633 | 1,031,094 (1.31x) |
| BatchInsert1000Values | 299,306 (1.24x) | 246,959 (1.00x) | 244,759 (1.01x) | 242,045 | 426,239 (1.76x) |
| TransactionInsert100000 | 107,865,040 (1.39x) | 72,119,588 (0.93x) | 71,940,162 (0.95x) | 77,246,321 | 102,853,750 (1.33x) |
| SingleRowSelect | 8,278 (1.10x) | 7,188 (0.96x) | 7,116 (0.96x) | 7,513 | 10,317 (1.37x) |
| FullScan1000 | 1,176,862 (1.98x) | 742,007 (1.24x) | 700,569 (1.22x) | 594,134 | 554,980 (0.93x) |
| IndexedLookup | 8,283 (1.06x) | 7,298 (0.98x) | 7,376 (0.94x) | 7,761 | 11,799 (1.52x) |
| LargeTextRead | 147,013 (0.98x) | 159,411 (1.15x) | 155,590 (1.19x) | 138,165 | 163,250 (1.18x) |
| LargeTextWrite | 81,437 (1.02x) | 80,865 (1.01x) | 80,105 (1.00x) | 80,158 | 109,424 (1.37x) |
| LargeBlobRead | 419,996 (1.01x) | 423,993 (1.04x) | 411,154 (0.99x) | 413,358 | 418,906 (1.01x) |
| LargeBlobWrite | 86,402 (1.07x) | 80,910 (1.00x) | 80,339 (0.99x) | 81,053 | 109,586 (1.35x) |
| WALReaders | 2,009 (0.96x) | 2,017 (0.97x) | 2,025 (0.95x) | 2,112 | 3,328 (1.58x) |
| WALWriterContention | 13,481 (1.15x) | 12,398 (0.96x) | 12,686 (1.08x) | 12,095 | 19,119 (1.58x) |
| FTS5Search | 40,023 (1.00x) | 38,478 (0.98x) | 39,450 (0.98x) | 39,747 | 83,559 (2.10x) |
| JSONQuery | 4,866 (1.08x) | 4,150 (0.96x) | 4,144 (0.96x) | 4,341 | 5,662 (1.30x) |

### Go memory: B/op / allocs/op

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 4,234 / 95 | 2,503 / 36 | 2,484 / 36 | 2,977 / 64 | 4,583 / 83 |
| SelectConstant | 520 / 15 | 472 / 15 | 472 / 15 | 536 / 14 | 480 / 13 |
| SingleInsert | 280 / 8 | 232 / 7 | 232 / 7 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,201 / 5,516 | 125,018 / 5,513 | 125,018 / 5,513 | 140,780 / 6,508 | 131,645 / 6,502 |
| BatchInsert1000Values | 42,295 / 32 | 42,137 / 29 | 42,135 / 29 | 41,901 / 25 | 41,786 / 19 |
| TransactionInsert100000 | 12,799,917 / 599,527 | 12,797,017 / 599,513 | 12,797,020 / 599,513 | 14,396,780 / 699,508 | 13,595,650 / 699,502 |
| SingleRowSelect | 704 / 24 | 640 / 23 | 640 / 23 | 736 / 26 | 672 / 24 |
| FullScan1000 | 82,473 / 7,304 | 82,408 / 7,303 | 82,408 / 7,303 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 640 / 20 | 536 / 18 | 536 / 18 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,401 / 18 | 1,049,082 / 17 | 1,049,082 / 17 | 1,049,139 / 16 | 1,049,091 / 16 |
| LargeTextWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,286 / 21 | 2,097,689 / 18 | 2,097,687 / 18 | 2,097,744 / 17 | 2,097,692 / 17 |
| LargeBlobWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 544 / 18 | 496 / 17 | 496 / 17 | 568 / 18 | 512 / 17 |
| WALWriterContention | 232 / 7 | 184 / 6 | 184 / 6 | 72 / 4 | 76 / 4 |
| FTS5Search | 640 / 20 | 544 / 18 | 544 / 18 | 600 / 17 | 568 / 18 |
| JSONQuery | 648 / 18 | 552 / 16 | 552 / 16 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 13,539 | 15,439 | 15,427 | 13,329 | 8,795 |
| SelectConstant | 330,439 | 390,595 | 389,753 | 357,122 | 347,795 |
| SingleInsert | 85,662 | 89,171 | 91,799 | 89,417 | 68,680 |
| PreparedInsert | 104,312 | 107,772 | 109,218 | 110,458 | 86,020 |
| BatchInsert1000 | 931 | 1,367 | 1,384 | 1,273 | 970 |
| BatchInsert1000Values | 3,341 | 4,049 | 4,086 | 4,131 | 2,346 |
| TransactionInsert100000 | 9 | 14 | 14 | 13 | 10 |
| SingleRowSelect | 120,800 | 139,127 | 140,538 | 133,095 | 96,928 |
| FullScan1000 | 850 | 1,348 | 1,427 | 1,683 | 1,802 |
| IndexedLookup | 120,725 | 137,031 | 135,583 | 128,844 | 84,750 |
| LargeTextRead | 6,802 | 6,273 | 6,427 | 7,238 | 6,126 |
| LargeTextWrite | 12,279 | 12,366 | 12,484 | 12,475 | 9,139 |
| LargeBlobRead | 2,381 | 2,359 | 2,432 | 2,419 | 2,387 |
| LargeBlobWrite | 11,574 | 12,359 | 12,447 | 12,338 | 9,125 |
| WALReaders | 497,741 | 495,813 | 493,877 | 473,516 | 300,457 |
| WALWriterContention | 74,180 | 80,659 | 78,825 | 82,676 | 52,305 |
| FTS5Search | 24,985 | 25,989 | 25,348 | 25,159 | 11,968 |
| JSONQuery | 205,512 | 240,982 | 241,309 | 230,337 | 176,602 |

### Native time / matching-run modernc

Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.

| Workload | PureGo | CGO dynamic | CGO static |
|---|---:|---:|---:|
| OpenClose | 0.66x (111,196) | 0.57x (114,573) | 0.57x (113,703) |
| SelectConstant | 1.05x (2,873) | 0.89x (2,863) | 0.89x (2,877) |
| SingleInsert | 0.81x (14,473) | 0.76x (14,736) | 0.76x (14,349) |
| PreparedInsert | 0.83x (11,592) | 0.78x (11,872) | 0.79x (11,609) |
| BatchInsert1000 | 1.06x (1,008,861) | 0.71x (1,031,094) | 0.67x (1,071,368) |
| BatchInsert1000Values | 0.69x (433,130) | 0.58x (423,951) | 0.58x (424,767) |
| TransactionInsert100000 | 1.06x (101,411,038) | 0.70x (102,839,921) | 0.67x (107,407,740) |
| SingleRowSelect | 0.83x (10,026) | 0.70x (10,286) | 0.67x (10,553) |
| FullScan1000 | 2.12x (554,044) | 1.19x (622,248) | 1.27x (553,664) |
| IndexedLookup | 0.73x (11,294) | 0.62x (11,828) | 0.61x (12,114) |
| LargeTextRead | 0.90x (163,719) | 0.97x (164,839) | 0.96x (161,387) |
| LargeTextWrite | 0.76x (107,420) | 0.74x (109,459) | 0.72x (110,672) |
| LargeBlobRead | 1.00x (421,242) | 1.02x (414,175) | 1.00x (412,767) |
| LargeBlobWrite | 0.81x (106,557) | 0.74x (109,641) | 0.72x (111,455) |
| WALReaders | 0.64x (3,163) | 0.60x (3,376) | 0.60x (3,352) |
| WALWriterContention | 0.65x (20,766) | 0.64x (19,251) | 0.68x (18,585) |
| FTS5Search | 0.48x (83,157) | 0.47x (82,365) | 0.47x (83,885) |
| JSONQuery | 0.86x (5,662) | 0.75x (5,499) | 0.72x (5,716) |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo run | CGO dynamic run | CGO static run |
|---|---:|---:|---:|
| OpenClose | 75,756 | 75,026 | 74,965 |
| SelectConstant | 2,825 | 2,762 | 2,800 |
| SingleInsert | 11,188 | 11,208 | 11,165 |
| PreparedInsert | 9,003 | 9,069 | 9,053 |
| BatchInsert1000 | 787,615 | 787,206 | 759,237 |
| BatchInsert1000Values | 241,083 | 247,923 | 242,045 |
| TransactionInsert100000 | 77,766,240 | 77,246,321 | 75,362,496 |
| SingleRowSelect | 7,546 | 7,476 | 7,446 |
| FullScan1000 | 593,986 | 597,865 | 575,315 |
| IndexedLookup | 7,800 | 7,459 | 7,847 |
| LargeTextRead | 150,143 | 138,165 | 131,107 |
| LargeTextWrite | 79,938 | 80,158 | 80,212 |
| LargeBlobRead | 415,095 | 407,107 | 414,915 |
| LargeBlobWrite | 81,067 | 81,053 | 80,791 |
| WALReaders | 2,097 | 2,077 | 2,127 |
| WALWriterContention | 11,730 | 12,912 | 11,769 |
| FTS5Search | 39,971 | 39,188 | 40,056 |
| JSONQuery | 4,489 | 4,306 | 4,337 |
<!-- END GENERATED RESULTS -->

## Results: optional embedded row reader

This second complete comparison uses the actual CI-built Linux amd64 glibc
prebuilt from the companion embed repository (build commit `d484be3`, workflow
run `41778591366`). **PureGo and dynamic CGO detect `row_read_v1=true`; static
CGO uses the stock linked engine and reports false.** The standard-library table
above remains the comparison for system SQLite and older prebuilts.

The [environment manifest](results/2026-11-04-row-bridge-environment.json) records
the exact asset/compiler/source hashes and commands. These are five 300ms
samples for every implementation/workload, GOMAXPROCS=4, CPUs 2–5, with databases
under `/dev/shm`. Native versions/options match mattn within each executable;
modernc retains its own options. The prebuilt uses Zig/Clang; the static engine
uses GCC. CPU frequency and background load are not fixed, so compare matching
controls rather than treating cross-run absolute rankings as isolated driver
costs. An initial dynamic-CGO run with correlated control slowdowns was retained
as [exploratory data](results/2026-11-04-row-bridge-exploratory-cgo-dynamic-linux-amd64.txt)
and excluded from this table; a complete repeat supplies the selected run.

Raw logs: [PureGo](results/2026-11-04-row-bridge-purego-linux-amd64.txt),
[dynamic CGO](results/2026-11-04-row-bridge-cgo-dynamic-linux-amd64.txt),
[static CGO](results/2026-11-04-row-bridge-cgo-linux-amd64.txt).
The [generated full report](results/2026-11-04-row-bridge-five-way-linux-amd64.md)
is reproduced below. It contains all 18 workloads and all five implementations.

### Isolating the row reader

A separate [same-library comparison](results/2026-11-04-row-bridge-native-buffer-purego.txt)
uses the identical GCC-built library, connection configuration and database/sql
scan loop for both cases; only the connection's optional reader is disabled.
Seven 500ms samples with CGO disabled give these medians:

| FullScan1000 path | ns/op | Go allocs/op |
| --- | ---: | ---: |
| Standard SQLite APIs | 1,186,209 | 7,304 |
| Optional row reader | 712,269 | 7,304 |

Elapsed time falls about **40%**, or roughly **1.67x throughput**, in this controlled
comparison. In the full prebuilt run PureGo takes 712,731ns versus matching
mattn 605,028ns and modernc 590,916ns. It still takes about **18% more time than
mattn and 21% more than modernc** for this scan; no universal performance lead
is claimed. Dynamic CGO with the bridge takes 564,454ns, close to its matching
mattn 561,155ns and modernc 550,619ns.

The bridge reduces the three-column row from about eight boundary crossings to
**two**: step plus one read of every column. Storage classes are still checked
per row. Strings and BLOBs are copied before advancing/resetting/finalizing.
The connection owns one reusable SQLite-allocated output buffer, 24 bytes per
cached column plus allocator overhead, freed on connection Close. This native
scratch is excluded from Go B/op/allocs/op; it is not a zero-copy payload API.
An initial [variadic-frame probe](results/2026-11-04-row-bridge-matched-purego.txt)
added 1000 Go allocations per scan. A [Go-buffer pinning probe](results/2026-11-04-row-bridge-pinned-buffer-purego.txt)
removed those allocations but increased scan time, and was rejected. The final
implementation uses native scratch with a private scalar call frame per connection.
A preliminary [full GCC-fixture run](results/2026-11-04-row-bridge-fixture-purego-linux-amd64.txt)
is also retained separately and excluded from the prebuilt table.

To reproduce the controlled comparison:

```sh
python3 scripts/build-native.py --row-bridge --output .native/row-bridge
TMPDIR=/dev/shm GOMAXPROCS=4 CGO_ENABLED=0 \
  SQLITE_TEST_LIBRARY="$PWD/.native/row-bridge/libsqlite3.so" \
  taskset -c 2-5 go test -run '^$' -bench '^BenchmarkRowReader$' \
  -benchtime=500ms -count=7 -benchmem
```

For the full comparison, prepare a directory containing `libsqlite3.so` from the
embed asset plus `src/sqlite3.h` from the matching pinned source, then set
`BENCH_NATIVE_DIR` to it for PureGo and dynamic CGO. Keep the normal stock native
directory for static CGO. The same report command works with the three raw logs.
Using the root fixture build above is another reproducible option, but its GCC
engine is not the distributed Zig/Clang asset. Runtime glibc compatibility is
validated separately; it is not inferred from benchmark success on this host.

<!-- BEGIN GENERATED ROW BRIDGE RESULTS -->
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
| OpenClose | 79,569 (0.98x) | 67,441 (0.88x) | 65,538 (0.88x) | 76,245 | 117,241 (1.54x) |
| SelectConstant | 3,112 (1.07x) | 2,567 (0.92x) | 2,563 (0.91x) | 2,807 | 2,909 (1.04x) |
| SingleInsert | 11,828 (1.05x) | 10,969 (1.00x) | 10,984 (0.99x) | 11,099 | 14,420 (1.30x) |
| PreparedInsert | 9,600 (1.07x) | 9,152 (1.02x) | 8,924 (1.00x) | 8,980 | 11,571 (1.29x) |
| BatchInsert1000 | 1,120,592 (1.31x) | 734,101 (0.93x) | 717,278 (0.96x) | 789,456 | 1,004,860 (1.27x) |
| BatchInsert1000Values | 297,544 (1.23x) | 245,738 (1.01x) | 242,500 (1.00x) | 241,825 | 429,549 (1.78x) |
| TransactionInsert100000 | 111,401,667 (1.35x) | 72,941,950 (0.92x) | 69,712,722 (0.95x) | 78,963,517 | 100,786,062 (1.28x) |
| SingleRowSelect | 7,872 (1.03x) | 7,041 (0.95x) | 7,026 (0.95x) | 7,427 | 10,318 (1.39x) |
| FullScan1000 | 712,731 (1.18x) | 564,454 (1.01x) | 689,726 (1.24x) | 563,593 | 552,132 (0.98x) |
| IndexedLookup | 8,399 (1.03x) | 7,393 (0.97x) | 7,235 (0.95x) | 7,661 | 11,485 (1.50x) |
| LargeTextRead | 155,661 (1.04x) | 157,532 (1.14x) | 155,835 (1.10x) | 145,045 | 168,299 (1.16x) |
| LargeTextWrite | 80,497 (1.01x) | 79,226 (1.00x) | 80,594 (1.00x) | 79,980 | 105,216 (1.32x) |
| LargeBlobRead | 302,034 (1.02x) | 422,449 (1.03x) | 256,190 (1.01x) | 294,975 | 313,470 (1.06x) |
| LargeBlobWrite | 80,301 (1.00x) | 80,860 (1.00x) | 80,030 (1.00x) | 80,196 | 106,187 (1.32x) |
| WALReaders | 2,103 (0.95x) | 2,014 (0.95x) | 1,999 (0.96x) | 2,130 | 3,545 (1.66x) |
| WALWriterContention | 16,002 (0.94x) | 14,680 (1.30x) | 12,645 (1.04x) | 12,956 | 20,036 (1.55x) |
| FTS5Search | 70,992 (1.04x) | 46,084 (1.01x) | 38,828 (0.96x) | 45,490 | 82,663 (1.82x) |
| JSONQuery | 4,885 (1.08x) | 4,092 (0.95x) | 4,163 (0.96x) | 4,327 | 5,799 (1.34x) |

### Go memory: B/op / allocs/op

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 4,343 / 97 | 2,552 / 36 | 2,531 / 36 | 2,970 / 64 | 4,597 / 83 |
| SelectConstant | 520 / 15 | 472 / 15 | 472 / 15 | 536 / 14 | 480 / 13 |
| SingleInsert | 280 / 8 | 232 / 7 | 232 / 7 | 224 / 7 | 124 / 5 |
| PreparedInsert | 112 / 4 | 112 / 4 | 112 / 4 | 128 / 5 | 116 / 5 |
| BatchInsert1000 | 125,203 / 5,516 | 125,026 / 5,513 | 125,024 / 5,513 | 140,779 / 6,508 | 131,647 / 6,502 |
| BatchInsert1000Values | 42,292 / 32 | 42,134 / 29 | 42,133 / 29 | 41,899 / 25 | 41,790 / 19 |
| TransactionInsert100000 | 12,799,837 / 599,526 | 12,797,081 / 599,513 | 12,797,017 / 599,513 | 14,396,785 / 699,508 | 13,595,698 / 699,502 |
| SingleRowSelect | 704 / 24 | 640 / 23 | 640 / 23 | 736 / 26 | 672 / 24 |
| FullScan1000 | 82,475 / 7,304 | 82,408 / 7,303 | 82,408 / 7,303 | 82,520 / 7,308 | 82,440 / 7,305 |
| IndexedLookup | 640 / 20 | 536 / 18 | 536 / 18 | 616 / 19 | 568 / 19 |
| LargeTextRead | 1,049,419 / 19 | 1,049,082 / 17 | 1,049,084 / 17 | 1,049,139 / 16 | 1,049,090 / 16 |
| LargeTextWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| LargeBlobRead | 2,098,279 / 21 | 2,097,689 / 18 | 2,097,684 / 18 | 2,097,741 / 17 | 2,097,692 / 17 |
| LargeBlobWrite | 160 / 5 | 112 / 4 | 112 / 4 | 128 / 5 | 132 / 6 |
| WALReaders | 544 / 18 | 496 / 17 | 496 / 17 | 568 / 18 | 512 / 17 |
| WALWriterContention | 232 / 7 | 184 / 6 | 184 / 6 | 72 / 4 | 76 / 4 |
| FTS5Search | 640 / 20 | 544 / 18 | 544 / 18 | 600 / 17 | 568 / 18 |
| JSONQuery | 648 / 18 | 552 / 16 | 552 / 16 | 608 / 15 | 576 / 16 |

### Throughput: benchmark units/s

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|
| OpenClose | 12,568 | 14,828 | 15,258 | 13,116 | 8,529 |
| SelectConstant | 321,379 | 389,527 | 390,219 | 356,212 | 343,811 |
| SingleInsert | 84,545 | 91,164 | 91,038 | 90,094 | 69,348 |
| PreparedInsert | 104,172 | 109,265 | 112,052 | 111,362 | 86,420 |
| BatchInsert1000 | 892 | 1,362 | 1,394 | 1,267 | 995 |
| BatchInsert1000Values | 3,361 | 4,069 | 4,124 | 4,135 | 2,328 |
| TransactionInsert100000 | 9 | 14 | 14 | 13 | 10 |
| SingleRowSelect | 127,033 | 142,035 | 142,329 | 134,652 | 96,915 |
| FullScan1000 | 1,403 | 1,772 | 1,450 | 1,774 | 1,811 |
| IndexedLookup | 119,067 | 135,262 | 138,225 | 130,536 | 87,072 |
| LargeTextRead | 6,424 | 6,348 | 6,417 | 6,894 | 5,942 |
| LargeTextWrite | 12,423 | 12,622 | 12,408 | 12,503 | 9,504 |
| LargeBlobRead | 3,311 | 2,367 | 3,903 | 3,390 | 3,190 |
| LargeBlobWrite | 12,453 | 12,367 | 12,495 | 12,470 | 9,417 |
| WALReaders | 475,520 | 496,454 | 500,155 | 469,406 | 282,068 |
| WALWriterContention | 62,491 | 68,120 | 79,080 | 77,182 | 49,911 |
| FTS5Search | 14,086 | 21,699 | 25,755 | 21,983 | 12,097 |
| JSONQuery | 204,728 | 244,365 | 240,198 | 231,090 | 172,454 |

### Native time / matching-run modernc

Each cell shows the time ratio and matching modernc median in ns/op; lower ratios are better.

| Workload | PureGo | CGO dynamic | CGO static |
|---|---:|---:|---:|
| OpenClose | 0.67x (119,041) | 0.58x (117,241) | 0.57x (114,854) |
| SelectConstant | 1.06x (2,949) | 0.88x (2,924) | 0.89x (2,876) |
| SingleInsert | 0.82x (14,492) | 0.77x (14,233) | 0.76x (14,477) |
| PreparedInsert | 0.83x (11,570) | 0.80x (11,478) | 0.77x (11,647) |
| BatchInsert1000 | 1.11x (1,013,027) | 0.74x (998,110) | 0.71x (1,005,080) |
| BatchInsert1000Values | 0.70x (425,154) | 0.55x (446,667) | 0.57x (428,526) |
| TransactionInsert100000 | 1.11x (100,786,062) | 0.72x (101,287,794) | 0.70x (100,299,874) |
| SingleRowSelect | 0.72x (10,866) | 0.70x (10,095) | 0.68x (10,318) |
| FullScan1000 | 1.21x (590,916) | 1.03x (550,619) | 1.25x (549,717) |
| IndexedLookup | 0.69x (12,105) | 0.64x (11,477) | 0.63x (11,427) |
| LargeTextRead | 0.88x (176,181) | 0.94x (168,299) | 0.96x (162,745) |
| LargeTextWrite | 0.78x (103,810) | 0.75x (105,216) | 0.73x (109,911) |
| LargeBlobRead | 0.96x (313,470) | 1.07x (394,963) | 0.94x (273,479) |
| LargeBlobWrite | 0.76x (105,160) | 0.76x (106,187) | 0.72x (110,523) |
| WALReaders | 0.47x (4,460) | 0.61x (3,294) | 0.56x (3,545) |
| WALWriterContention | 0.58x (27,464) | 0.82x (17,841) | 0.63x (19,971) |
| FTS5Search | 0.86x (82,737) | 0.56x (82,663) | 0.47x (82,413) |
| JSONQuery | 0.83x (5,903) | 0.71x (5,799) | 0.73x (5,713) |

### Matching mattn controls: ns/op

These medians are the denominators for the native backend ratios above.

| Workload | PureGo run | CGO dynamic run | CGO static run |
|---|---:|---:|---:|
| OpenClose | 80,986 | 76,245 | 74,384 |
| SelectConstant | 2,907 | 2,789 | 2,807 |
| SingleInsert | 11,275 | 11,008 | 11,094 |
| PreparedInsert | 8,997 | 8,996 | 8,896 |
| BatchInsert1000 | 852,168 | 789,456 | 748,638 |
| BatchInsert1000Values | 242,210 | 243,414 | 241,768 |
| TransactionInsert100000 | 82,265,091 | 78,963,517 | 73,666,549 |
| SingleRowSelect | 7,679 | 7,381 | 7,415 |
| FullScan1000 | 605,028 | 561,155 | 554,074 |
| IndexedLookup | 8,187 | 7,647 | 7,621 |
| LargeTextRead | 150,318 | 138,784 | 141,880 |
| LargeTextWrite | 79,856 | 79,363 | 80,593 |
| LargeBlobRead | 294,975 | 410,080 | 252,500 |
| LargeBlobWrite | 80,068 | 80,712 | 80,312 |
| WALReaders | 2,206 | 2,124 | 2,087 |
| WALWriterContention | 17,064 | 11,326 | 12,178 |
| FTS5Search | 68,029 | 45,490 | 40,501 |
| JSONQuery | 4,507 | 4,305 | 4,322 |
<!-- END GENERATED ROW BRIDGE RESULTS -->

## Standard-API optimization and remaining overhead

[Targeted before/after probes and rejected experiments](results/2026-11-04-optimization-probes.md)
record the intermediate measurements; they are excluded from the final table.

The standard-API results above use **standard public SQLite symbols**. They work with system
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
  Prepared scalar INSERTs now use **five native calls per row** instead of seven.

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
