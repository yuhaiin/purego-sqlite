# Go SQL extension benchmarks

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
| Scalar | 3,689 (1.78x); 640 / 23 | 3,465 (1.76x); 640 / 23 | 2,184 (1.13x); 520 / 19 | 2,246 (1.16x); 520 / 19 | 2,200 (1.14x); 520 / 19 | 1,947; 528 / 14 | — |
| Aggregate1000 | 1,204,552 (1.64x); 166,819 / 7,772 | 1,189,296 (1.66x); 166,824 / 7,772 | 434,836 (0.64x); 46,712 / 3,769 | 477,062 (0.68x); 46,712 / 3,769 | 446,552 (0.64x); 46,712 / 3,769 | 704,373; 126,545 / 4,762 | — |
| CollationSort1000 | 3,273,377 (3.93x); 1,018,525 / 28,273 | 2,964,497 (3.74x); 1,018,544 / 28,273 | 867,928 (1.09x); 210,024 / 13,117 | 879,737 (1.07x); 210,024 / 13,117 | 890,309 (1.10x); 210,024 / 13,117 | 806,203; 210,056 / 13,112 | — |
| TimeBind | 2,566 (1.43x); 648 / 19 | 2,315 (1.35x); 648 / 19 | 1,852 (1.10x); 601 / 19 | 1,907 (1.11x); 601 / 19 | 1,892 (1.14x); 601 / 19 | 1,708; 632 / 15 | — |
| TypedRows1000 | 1,361,396 (1.50x); 312,517 / 10,014 | 1,050,721 (1.23x); 312,519 / 10,014 | 978,224 (1.21x); 312,440 / 10,015 | 985,867 (1.16x); 312,440 / 10,015 | 1,015,257 (1.25x); 312,440 / 10,015 | 846,552; 296,409 / 9,008 | — |

Go callback cases use `WithMattnTypes` on this driver and mattn's default
type policy. The workloads cover scalar functions, grouped aggregates, custom
collation sorting, timestamp binding and typed row decoding. Modernc participates
in the SQL controls; no equivalent per-connection extension API is assumed here.

Collation sorts invoke the comparator many times, making native-to-Go callback
dispatch and reflection significant. Native row/scalar helpers do not remove
that callback cost. Aggregation caches each group's reflected methods and
releases native-keyed state on finalization.

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
  -bench 'BenchmarkSQLite/(SelectConstant|PreparedInsert|FullScan1000)$|BenchmarkMattnParity' \
  -benchtime=300ms -count=5
```

Build the stock native library first with `go run ./internal/cmd/build-native`.
Prepare the embed directory and matching header as described in the main README.
The benchmark wrapper verifies source/options, per-connection settings and
compatible type behavior before measuring.

Historical implementation-stage samples: [PureGo](results/mattn-high-purego.txt),
[dynamic CGO](results/mattn-high-cgo-dynamic.txt), and
[static CGO](results/mattn-high-cgo.txt). Their older host, sample counts and local
bridge build differ from this report.
