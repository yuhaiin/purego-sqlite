# Medium-priority mattn compatibility benchmarks

Linux amd64, AMD Ryzen 5 5600G, `go1.27.3-X:nodwarf5`, GOMAXPROCS=4;
three 150ms samples per backend. Each native run verifies SQLite source identity
and compile options against its mattn control. PureGo and dynamic CGO use the
optional row/scalar helpers; static CGO uses the linked engine's standard APIs.
Modernc has the same source version but different compilation options, recorded
in the logs. These are local measurements, rather than platform guarantees.

Cells show median ns/op, time / matching-run mattn, and B/op / allocs/op.
Mattn/modernc columns pool controls across the three runs. All five implementations
share one table; a dash means that this extension API/cache setting was not
measured for modernc.

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc |
|---|---:|---:|---:|---:|---:|
| SelectConstant | 3002 (1.10x); 536 / 15 | 2549 (0.95x); 488 / 15 | 2538 (0.93x); 488 / 15 | 2722; 536 / 14 | 2854; 480 / 13 |
| PreparedInsert | 9107 (1.02x); 112 / 4 | 9172 (1.02x); 112 / 4 | 9054 (1.00x); 112 / 4 | 8972; 128 / 5 | 11311; 116 / 5 |
| FullScan1000 | 697314 (1.21x); 82489 / 7304 | 620039 (1.05x); 82424 / 7303 | 728668 (1.29x); 82424 / 7303 | 574169; 82520 / 7308 | 573030; 82440 / 7305 |
| RepeatedQuery/Cache0 | 4288 (0.99x); 768 / 21 | 3900 (0.88x); 721 / 20 | 3873 (0.89x); 720 / 20 | 4341; 777 / 21 | 5428; 720 / 20 |
| RepeatedQuery/Cache16 | 2393 (1.28x); 744 / 18 | 2045 (1.07x); 744 / 18 | 2011 (1.07x); 744 / 18 | 1888; 616 / 14 | — |
| RepeatedExec/Cache0 | 2618 (1.00x); 327 / 10 | 2378 (0.91x); 279 / 9 | 2381 (0.93x); 279 / 9 | 2583; 255 / 9 | 3484; 159 / 7 |
| RepeatedExec/Cache16 | 1208 (1.38x); 304 / 7 | 1036 (1.20x); 303 / 7 | 1026 (1.19x); 303 / 7 | 864; 143 / 6 | — |
| CacheMisses/Cache0 | 4412 (1.01x); 768 / 21 | 3957 (0.93x); 720 / 20 | 3862 (0.90x); 720 / 20 | 4298; 776 / 21 | 5411; 720 / 20 |
| CacheMisses/Cache16 | 5256 (1.05x); 1009 / 24 | 4680 (0.98x); 960 / 23 | 4774 (0.97x); 960 / 23 | 4936; 832 / 23 | — |
| Serialize1MiB | 150067 (1.07x); 1057345 / 17 | 96712 (0.78x); 1056779 / 2 | 99264 (0.64x); 1056778 / 2 | 137451; 1056778 / 2 | — |
| Deserialize1MiB | 41866 (1.06x); 368 / 19 | 39420 (1.00x); 0 / 0 | 39640 (1.00x); 0 / 0 | 39405; 0 / 0 | — |
| Backup1MiB | 1342874 (1.01x); 1408 / 43 | 1346968 (1.02x); 672 / 6 | 1328932 (1.00x); 672 / 6 | 1328857; 8 / 1 | — |
| CommitHook | 16514 (1.12x); 280 / 9 | 15654 (1.06x); 200 / 6 | 15541 (1.05x); 200 / 6 | 14763; 72 / 4 | — |
| Authorizer | 16726 (1.20x); 1248 / 33 | 13550 (0.97x); 488 / 20 | 13572 (0.98x); 488 / 20 | 13951; 536 / 21 | — |
| Trace | 16130 (1.13x); 872 / 25 | 13507 (0.94x); 512 / 15 | 13616 (0.98x); 512 / 15 | 14231; 568 / 16 | — |

The optional 16-entry cache reduced PureGo repeated-query time from 4,288 to
2,393 ns/op (44%) and repeated-exec time from 2,618 to 1,208 ns/op (54%). A
32-query rotation intentionally exceeds its capacity: caching increased that
workload from 4,412 to 5,256 ns/op (19%). The cache therefore stays disabled by
default. Cache-off comparisons include modernc. Explicitly prepared insert and
FullScan controls remain separate from workloads that can benefit from caching.

Snapshots and backup compare the same valid database contents. This driver's
deserializer additionally validates the header, pads the native copy and allows
buffer growth; mattn uses a fixed-size copy. Both backup paths step 128 pages.
Hook/trace tests invoke real SQLite events. Authorizer and trace payloads differ
between the public APIs; the benchmarks use no-op counters rather than logging,
parameter expansion, or application work. TraceProfile follows SQLite's reported
timing resolution and is not a nanosecond-accurate timer.

These measurements use the released PureGo callback dispatcher. A separate
Linux-amd64 prototype showed that typed callback dispatch can substantially
reduce collation overhead, but it is not part of either PR or these samples.
It requires dependency work and platform validation; do not treat prototype
results as delivered driver performance.

Reproduce from the repository root:

```sh
go run ./internal/cmd/build-native --row-bridge --exec-bridge --output .native/bridges
for backend in purego cgo-dynamic cgo; do
  BENCH_BACKEND="$backend" BENCH_NATIVE_DIR="$PWD/.native/bridges" \
    scripts/benchmark.sh \
      -bench 'BenchmarkSQLite/(SelectConstant|PreparedInsert|FullScan1000)$|BenchmarkMattnMedium' \
      -benchtime=150ms -count=3
done
```

The script enables mattn's `sqlite_serialize` and `sqlite_trace` Go wrappers;
these flags do not rebuild or change the shared SQLite engine. Engine identity,
per-connection settings, compatible type semantics, and snapshot round trips
are checked before measurement.

Raw samples: [PureGo](results/mattn-medium-purego.txt),
[dynamic CGO](results/mattn-medium-cgo-dynamic.txt), and
[static CGO](results/mattn-medium-cgo.txt).
