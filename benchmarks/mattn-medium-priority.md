# Snapshots, caching and hook benchmarks

Measured on **2026-11-05** in the [same CI run](https://github.com/yuhaiin/purego-sqlite/actions/runs/42004475183)
as the [SQL comparison](README.md): driver `0a021fd`, Linux amd64,
AMD EPYC 9V74, Go 1.27.3, GOMAXPROCS=4, logical CPUs 0–3.
Each native configuration has five 300ms samples per workload. Native source IDs
and compile options match that executable's mattn control. Standard paths use
public SQLite APIs; embed paths use the distributed row/scalar helpers from
embed commit `d721bc5`. Static CGO uses the stock linked engine.

Cells show median ns/op, time / matching-run mattn for native columns, and
B/op / allocs/op. Mattn and modernc display medians pool controls from all five
executables (25 samples per measured workload). A dash means the API/cache setting
was not measured for modernc, rather than establishing that it is unsupported.
The first three rows are the same SQL controls as the main table. Host differences
prevent interpreting changes from the older Ryzen report as a before/after result.

| Workload | PureGo standard | PureGo embed | CGO dynamic standard | CGO dynamic embed | CGO static | mattn | modernc.org/sqlite |
|---|---:|---:|---:|---:|---:|---:|---:|
| SelectConstant | 2,652 (1.09x); 536 / 15 | 2,522 (1.08x); 536 / 15 | 2,243 (0.92x); 488 / 15 | 2,165 (0.92x); 488 / 15 | 2,173 (0.92x); 488 / 15 | 2,370; 536 / 14 | 2,500; 480 / 13 |
| PreparedInsert | 9,874 (1.06x); 112 / 4 | 9,591 (1.03x); 112 / 4 | 9,388 (1.02x); 112 / 4 | 9,373 (1.01x); 112 / 4 | 9,322 (1.01x); 112 / 4 | 9,280; 128 / 5 | 12,085; 116 / 5 |
| FullScan1000 | 1,004,743 (2.05x); 82,490 / 7,304 | 597,124 (1.22x); 82,490 / 7,304 | 610,874 (1.25x); 82,424 / 7,303 | 512,133 (1.04x); 82,424 / 7,303 | 612,684 (1.35x); 82,424 / 7,303 | 489,380; 82,520 / 7,308 | 465,437; 82,440 / 7,305 |
| RepeatedQuery/Cache0 | 4,433 (1.02x); 768 / 21 | 4,175 (1.02x); 768 / 21 | 3,832 (0.91x); 720 / 20 | 3,797 (0.89x); 720 / 20 | 3,849 (0.90x); 720 / 20 | 4,255; 777 / 21 | 4,984; 720 / 20 |
| RepeatedQuery/Cache16 | 2,561 (1.25x); 744 / 18 | 2,494 (1.29x); 744 / 18 | 2,006 (1.08x); 744 / 18 | 2,160 (1.10x); 744 / 18 | 2,073 (1.08x); 744 / 18 | 1,927; 616 / 14 | — |
| RepeatedExec/Cache0 | 2,540 (1.12x); 328 / 10 | 2,202 (1.03x); 328 / 10 | 2,089 (0.94x); 279 / 9 | 2,026 (0.92x); 279 / 9 | 2,076 (0.92x); 279 / 9 | 2,212; 255 / 9 | 2,907; 159 / 7 |
| RepeatedExec/Cache16 | 1,386 (1.68x); 304 / 7 | 1,145 (1.44x); 304 / 7 | 902 (1.16x); 303 / 7 | 988 (1.21x); 303 / 7 | 937 (1.22x); 303 / 7 | 796; 143 / 6 | — |
| CacheMisses/Cache0 | 4,437 (1.01x); 768 / 21 | 4,182 (1.02x); 768 / 21 | 3,828 (0.90x); 720 / 20 | 3,828 (0.90x); 720 / 20 | 3,850 (0.90x); 720 / 20 | 4,260; 777 / 21 | 5,039; 720 / 20 |
| CacheMisses/Cache16 | 5,400 (1.09x); 1,009 / 24 | 5,106 (1.09x); 1,009 / 24 | 4,627 (0.97x); 961 / 23 | 4,662 (0.97x); 960 / 23 | 4,686 (0.97x); 961 / 23 | 4,819; 833 / 23 | — |
| Serialize1MiB | 154,243 (0.67x); 1,057,274 / 17 | 159,989 (0.69x); 1,057,264 / 17 | 153,484 (0.57x); 1,056,781 / 2 | 162,697 (0.74x); 1,056,779 / 2 | 161,998 (0.76x); 1,056,778 / 2 | 230,084; 1,056,780 / 2 | — |
| Deserialize1MiB | 35,177 (1.06x); 368 / 19 | 34,491 (1.07x); 368 / 19 | 32,931 (1.04x); 0 / 0 | 31,472 (1.01x); 0 / 0 | 50,513 (1.53x); 0 / 0 | 32,033; 0 / 0 | — |
| Backup1MiB | 1,500,640 (1.00x); 1,411 / 43 | 1,499,184 (0.99x); 1,411 / 43 | 1,484,925 (1.00x); 736 / 6 | 1,487,530 (0.99x); 736 / 6 | 1,497,181 (0.98x); 736 / 6 | 1,504,175; 8 / 1 | — |
| CommitHook | 16,706 (1.09x); 280 / 9 | 16,435 (1.08x); 280 / 9 | 15,808 (1.03x); 200 / 6 | 15,686 (1.02x); 200 / 6 | 15,858 (1.03x); 200 / 6 | 15,304; 72 / 4 | — |
| Authorizer | 16,492 (1.20x); 1,248 / 33 | 16,001 (1.19x); 1,248 / 33 | 13,508 (0.99x); 488 / 20 | 13,411 (0.99x); 488 / 20 | 13,657 (0.99x); 488 / 20 | 13,685; 536 / 21 | — |
| Trace | 15,287 (1.13x); 872 / 25 | 15,094 (1.13x); 872 / 25 | 13,301 (0.98x); 512 / 15 | 13,147 (0.98x); 512 / 15 | 13,350 (0.97x); 512 / 15 | 13,561; 568 / 16 | — |

For PureGo standard, the optional 16-entry cache changes repeated-query time
from 4,433 to 2,561 ns/op
(42.2% less time), and repeated-exec time
from 2,540 to 1,386 ns/op
(45.4% less time).
A 32-query rotation exceeds the cache capacity and changes miss-heavy time from
4,437 to 5,400 ns/op
(21.7% more time).
The cache remains disabled by default. Explicitly prepared INSERT and FullScan
controls do not benefit from one-shot statement caching.

Snapshots and backup compare the same valid database contents. This driver's
deserializer additionally validates the header, pads the native copy and allows
buffer growth; mattn uses a fixed-size copy. Both backup paths step 128 pages.
Hook/trace cases invoke real SQLite events with no-op counters, without logging,
parameter expansion or application work. Authorizer/trace payloads differ
between APIs. TraceProfile follows SQLite's reported resolution.

Raw five-configuration logs, matching control medians, manifests and the full
35-workload report are linked from the [SQL benchmark README](README.md#results).
The [environment manifest](results/2026-11-05-current-environment.json)
records exact inputs. These results use the repository's released
`github.com/ebitengine/purego v0.11.3` dependency.
The [upstream callback optimization](https://github.com/ebitengine/purego/pull/562)
is outside the measured dependency and callback declarations; its experimental
timings are not delivered-driver results.

Reproduce all five configurations with:

```sh
BENCH_EMBED_DIR="$SQLITE_EMBED_NATIVE_DIR" bash scripts/benchmark-all.sh \
  -bench 'BenchmarkSQLite/(SelectConstant|PreparedInsert|FullScan1000)$|BenchmarkMattnMedium' \
  -benchtime=300ms -count=5
```

Build the stock native library first with `go run ./internal/cmd/build-native`.
Prepare the embed directory and matching header as described in the main README.
The script enables mattn's `sqlite_serialize` and `sqlite_trace` Go wrappers;
these flags do not rebuild or change the shared SQLite engine. Source/options,
connection settings, compatible type semantics and snapshot round trips are
checked before measurement.
