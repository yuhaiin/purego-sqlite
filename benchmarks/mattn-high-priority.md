# High-priority mattn compatibility benchmarks

Measured on Linux amd64, `go1.27.3-X:nodwarf5`, GOMAXPROCS=4; three 150ms samples per native backend. SQLite source identity and compile options match each run's mattn control. PureGo and dynamic CGO use the optional row/scalar helpers from `.native/bridges`; static CGO uses the unmodified linked engine's public API. These local samples describe this machine and workload, not a cross-platform speed guarantee.

Cells show median ns/op, matching-run mattn time ratio, and B/op / allocs/op. Mattn/modernc medians pool controls from all three runs. Existing published full-workload results remain in the benchmark README. New extension cases use `WithMattnTypes` on this driver and mattn's default type policy. Modernc has no measurement in this per-connection callback API group.

| Workload | PureGo | CGO dynamic | CGO static | mattn | modernc |
|---|---:|---:|---:|---:|---:|
| SelectConstant | 2,904 (1.04x); 520 / 15 | 2,522 (0.92x); 472 / 15 | 2,538 (0.94x); 472 / 15 | 2,738; 536 / 14 | 2,880; 480 / 13 |
| PreparedInsert | 9,297 (1.04x); 112 / 4 | 9,047 (1.02x); 112 / 4 | 9,036 (1.02x); 112 / 4 | 8,891; 128 / 5 | 11,394; 116 / 5 |
| FullScan1000 | 684,692 (1.21x); 82,472 / 7304 | 599,831 (0.99x); 82,408 / 7303 | 701,932 (1.27x); 82,408 / 7303 | 564,061; 82,520 / 7308 | 549,239; 82,440 / 7305 |
| Scalar | 3,776 (1.77x); 640 / 23 | 2,452 (1.16x); 520 / 19 | 2,441 (1.17x); 520 / 19 | 2,109; 528 / 14 | — |
| Aggregate1000 | 1,156,498 (1.48x); 166,817 / 7772 | 500,257 (0.64x); 46,717 / 3769 | 490,600 (0.63x); 46,712 / 3769 | 779,932; 126,546 / 4762 | — |
| CollationSort1000 | 3,172,775 (3.56x); 1,018,505 / 28273 | 955,488 (1.12x); 210,024 / 13117 | 968,642 (1.11x); 210,024 / 13117 | 874,772; 210,056 / 13112 | — |
| TimeBind | 2,557 (1.36x); 648 / 19 | 2,098 (1.11x); 601 / 19 | 2,116 (1.15x); 601 / 19 | 1,875; 632 / 15 | — |
| TypedRows1000 | 1,146,776 (1.22x); 312,509 / 10014 | 1,066,127 (1.11x); 312,440 / 10015 | 1,133,984 (1.22x); 312,440 / 10015 | 943,078; 296,409 / 9008 | — |

Go callbacks incur native-to-Go dispatch/reflection costs. Collation sorts invoke the comparator many times and are slower on PureGo; these measurements do not claim timing parity. Aggregation caches each group's reflected methods and releases native-keyed state on finalization. Basic reads keep the existing storage-class policy unless compatibility conversion is requested.

Reproduce from the repository root:

```sh
go run ./internal/cmd/build-native --row-bridge --exec-bridge --output .native/bridges
for backend in purego cgo-dynamic cgo; do
  BENCH_BACKEND="$backend" BENCH_NATIVE_DIR="$PWD/.native/bridges" \
    scripts/benchmark.sh \
      -bench 'BenchmarkSQLite/(SelectConstant|PreparedInsert|FullScan1000)$|BenchmarkMattnParity' \
      -benchtime=150ms -count=3
done
```

Raw samples: [PureGo](results/mattn-high-purego.txt), [dynamic CGO](results/mattn-high-cgo-dynamic.txt), [static CGO](results/mattn-high-cgo.txt).
