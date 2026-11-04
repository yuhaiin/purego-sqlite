# purego-sqlite

A `database/sql` driver for the **official native SQLite engine**. With
`CGO_ENABLED=1`, it uses compiler-typed CGO calls and statically links the bundled
official SQLite source by default. With `CGO_ENABLED=0`, it uses
[PureGo](https://github.com/ebitengine/purego) to load a native shared library.
This is an original implementation, not a fork of mattn/go-sqlite3 or a Go/WASM
SQLite engine.

Requires **Go 1.26+**, PureGo **v0.11.3**, and a thread-safe SQLite **3.20.0+**.
The CI toolchain is Go 1.27.3. Prefer a maintained SQLite release; the minimum
version describes API compatibility, not a security recommendation.

## Usage

```go
package main

import (
    "database/sql"
    "log"

    _ "github.com/yuhaiin/purego-sqlite"
)

func main() {
    db, err := sql.Open("sqlite3purego", "file:app.db")
    if err != nil { log.Fatal(err) }
    defer db.Close()
    if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
        id INTEGER PRIMARY KEY, name TEXT NOT NULL
    )`); err != nil { log.Fatal(err) }
    if _, err := db.Exec("INSERT INTO users(name) VALUES(?)", "Alice"); err != nil {
        log.Fatal(err)
    }
}
```

```sh
CGO_ENABLED=0 go build ./...
```

`sql.Open` is lazy about opening connections. Use `PingContext` to check access.
The driver is registered as `sqlite3purego`, so it can coexist with mattn's
`sqlite3` registration. In CGO builds importing both drivers, use mattn's
`libsqlite3` tag to share the linked engine, or `sqlite_dynamic` to exclude this
package's amalgamation; do not link two copies defining the same SQLite symbols.

## Backend selection and platforms

Backend selection happens at build time; imports, connectors and the registered
`sqlite3purego` driver name remain the same.

| Build | Backend | SQLite engine |
|---|---|---|
| `CGO_ENABLED=1` on Linux/macOS/Windows/BSD | CGO | Bundled official SQLite 3.54.1, linked into the program |
| `CGO_ENABLED=0` on Linux/macOS/Windows amd64/arm64 | PureGo | Explicit, embedded or system shared library |
| `CGO_ENABLED=0` on FreeBSD/NetBSD amd64/arm64 | PureGo | Shared library; extra compiler flag below |
| `CGO_ENABLED=1 -tags sqlite_dynamic` | CGO | Explicit, embedded or system shared library; excludes bundled C source |
| `CGO_ENABLED=1 -tags sqlite_purego` on a supported PureGo target | PureGo | Shared library; useful for backend comparisons |
| Other targets without a supported backend | Unavailable | Connector returns an actionable error |

`sqlite_cgo` remains a compatibility alias for the default CGO selection and
requires CGO enabled. Do not combine it with `sqlite_purego`. CGO uses typed C
trampolines for 32-bit pointers, 64-bit INTEGERs, floating-point values,
destructors and varargs. MIPS, OpenBSD and DragonFly require CGO. Cross compiling
CGO requires a matching C compiler/sysroot and ABI; SQLite headers or an installed
SQLite development package are unnecessary.

PureGo v0.11.3 supports FreeBSD and NetBSD as Tier 2 targets. Its documented
CGO-disabled build requires this flag:

```sh
CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 \
  go build -gcflags=github.com/ebitengine/purego/internal/fakecgo=-std ./...
```

FreeBSD amd64 has native CGO and PureGo VM tests; Linux mipsle hardfloat has QEMU
CGO tests. NetBSD amd64/arm64 are cross-build checked. Other implemented CGO
combinations require validation on the intended target.

For dynamic builds, install a library matching the process architecture/libc:
Linux uses fixed absolute `/lib`, `/usr/lib`, multiarch and `lib64` paths for
`libsqlite3.so.0`; macOS uses `/usr/lib/libsqlite3.dylib`; BSD searches fixed
system/package-manager directories. Windows uses an explicit `WithLibrary` or
embedded DLL and never implicitly searches `PATH`.

### Static SQLite and fully static Alpine binaries

A normal CGO build compiles the pinned, unmodified official amalgamation in
`internal/sqlite3` into the executable. It needs no SQLite shared library and
performs no native extraction or runtime download. `scripts/vendor-sqlite.py`
verifies source hashes; `--update` reimports the archive after checking both its
pinned SHA-256 and SQLite's SHA3-256. `LibraryPath()` identifies the default engine
as `builtin:sqlite3-3.54.1`.

Linking SQLite into the executable does not automatically make the C runtime
static. For a fully static Linux program that runs on Alpine without a glibc
installation, use a matching musl toolchain and the external linker's static flag:

```sh
CGO_ENABLED=1 CC=musl-gcc go build \
  -ldflags='-linkmode external -extldflags "-static"' ./cmd/yourapp
```

Inside Alpine, install `go gcc musl-dev` and use `CC=gcc`. No `sqlite_musl` tag or
prebuilt embed module is required for this mode. CI builds amd64/arm64 musl
executables, verifies they contain neither ELF INTERP nor NEEDED entries, and
runs the complete database tests in Alpine and an empty scratch container.
Fully static musl cannot load shared libraries/extensions: use default linked
SQLite for such binaries, and use a dynamic build when native extension loading
or `WithLibrary`/`WithBundled` is needed. Those explicit options remain supported
by ordinary CGO builds and always take precedence.

For MIPS, match `GOMIPS` to the compiler and firmware ABI, for example:

```sh
CGO_ENABLED=1 GOOS=linux GOARCH=mipsle GOMIPS=hardfloat \
  CC=mipsel-linux-gnu-gcc go build ./...
```

Library precedence is deterministic: **explicit `WithLibrary` > requested
`WithBundled` > linked SQLite (default CGO) / system (PureGo or `sqlite_dynamic`)**. An explicitly selected library never silently falls
back. The loader never searches the working directory or `PATH`. Unix loader
environment variables and native dependencies remain under the host's control;
deploy in a trusted environment. Windows restricts dependency search to the DLL's
directory and System32. Missing required symbols and incompatible versions fail
at connector creation. Successfully initialized libraries stay loaded for the
process lifetime; there is no unsafe unload or global `sqlite3_shutdown`.

```go
connector, err := sqlite.NewConnector("file:app.db",
    sqlite.WithLibrary("/trusted/absolute/path/libsqlite3.so"),
    sqlite.WithBusyTimeout(2*time.Second),
    sqlite.WithWAL(),
    sqlite.WithTransactionMode("IMMEDIATE"),
)
if err != nil { return err }
db := sql.OpenDB(connector)
defer db.Close()
```

Import the package as `sqlite "github.com/yuhaiin/purego-sqlite"` for typed APIs.
Additional options are `WithReadOnly`, `WithExtensions` and `WithInit`.
`WithWAL` checks SQLite's returned journal mode and fails if WAL is unavailable
(for example, an in-memory database). SQLite URI filenames and their native
parameters are accepted; there is no driver-specific DSN PRAGMA interpolation.

## Transactions, SQL and values

Use `db.BeginTx(ctx, nil)` and deterministic `Commit`/`Rollback`. The connector's
transaction mode is `DEFERRED` by default; `IMMEDIATE` and `EXCLUSIVE` are explicit
options. Savepoints work through SQL inside a transaction. Unsupported isolation
levels and per-transaction `ReadOnly` return errors; open a read-only connector
instead. SQLite may automatically roll back an interrupted or conflicting
transaction. Commit checks the actual native state; a failed commit attempts
rollback before the connection can return to the pool.

Each Prepare, Exec and Query accepts **exactly one statement**, with leading and
trailing comments allowed. Multiple statements fail before the first executes;
run scripts as separate calls, optionally inside a transaction. Empty/comment-only
SQL and SQL containing NUL are rejected. Semicolons in string literals and
triggers are parsed by SQLite. Exec drains result rows (including RETURNING);
use Query to read them. `RowsAffected` and `LastInsertId` follow native SQLite
metadata (DDL may retain SQLite's previous changes count).

Bindings support `nil`, integers accepted by `database/sql`, `float64`, `bool`,
`string` and `[]byte`; empty TEXT, empty BLOB and NULL remain distinct. Named
`:name`, `@name`, `$name`, positional `?` and numbered `?NNN` parameters work.
`sql.Named("name", value)` binds all three matching named prefixes; repeated or
unknown arguments and missing bindings fail. Sparse numbered placeholders retain
SQLite's parameter-count semantics. Bindings use `SQLITE_TRANSIENT`, so SQLite
copies Go data before returning. Result TEXT/BLOB data is copied before native
buffers can expire. Embedded NUL in bound TEXT is preserved; SQLite's SQL text
functions themselves may have NUL-specific semantics.

Dates are not inferred: encode `time.Time` explicitly as a chosen TEXT format or
INTEGER epoch. INTEGER/BOOLEAN columns return `int64`; REAL returns `float64`;
TEXT returns `string`; BLOB returns `[]byte`. Declared type metadata is exposed;
nullability is reported as unknown. Structured `*sqlite.Error` includes `Code`,
`ExtendedCode` and `Message`; use `errors.AsType[*sqlite.Error]` or
`errors.Is(err, sqlite.ErrBusy/ErrLocked/ErrConstraint/ErrReadOnly)`.

## Concurrency and cancellation

Connections and statements have deterministic ownership and cleanup. SQLite runs
with `FULLMUTEX`, while the driver also serializes each connection's native calls.
Close rows before running another operation on the **same physical connection**;
otherwise the driver returns `ErrActiveRows`. Different pooled connections run
concurrently. Keep write transactions short; WAL improves reader/writer overlap
but does not remove SQLite's single-writer restriction. Configure the pool for
your workload (`SetMaxOpenConns`, `SetMaxIdleConns`).

Context cancellation interrupts preparation, execution and row stepping with
`sqlite3_interrupt`. A cancellation callback is stopped and joined before reuse
or connection destruction; repeated interrupts cover the race before a native
operation starts. No watcher goroutine is created for background contexts, and
no canceled watcher survives its operation. Native busy-handler sleeps and native
extensions can delay cancellation; the configured busy timeout bounds ordinary
lock waits, and extensions must cooperate with SQLite interruption. The default
busy timeout is 5 seconds. The driver does not install Go callbacks in SQLite.

`WithInit(func(context.Context, *sqlite.Conn) error)` runs on **each new physical
connection** before pooling. Use it for per-connection PRAGMAs and extensions.
Close any statements/rows and leave no transaction open before returning. Never
retain the connection or launch goroutines using it. Hook failures close native
resources. For later physical-connection work, reserve `*sql.Conn` and use `Raw`.

## Native features and capabilities

Features are executed by SQLite itself. Check the **selected** connector, not
just the default system library:

```go
fmt.Println(connector.Version(), connector.LibraryPath())
fmt.Println(connector.CompileOptions())
hasFTS5, err := connector.HasFeature("fts5")
```

Package-level `Version`, `CompileOptions` and `HasFeature` describe the default
system library and return errors. Feature probes use an isolated in-memory
connection, never modifying your application database or invoking init hooks.
Unknown feature names return `ErrUnsupported`.

| Capability | Native prerequisite | Driver support |
|---|---|---|
| CRUD, prepared statements, savepoints, WAL, backup | SQLite 3.20+; thread-safe build | Yes |
| FTS3 / FTS4 / FTS5 | Corresponding native module | Yes; detected separately |
| JSON | JSON enabled (built in by default since 3.38) | Yes; runtime probe |
| JSONB | SQLite 3.45+ with JSON | Yes; runtime probe |
| RTree / Geopoly / DBSTAT / math | Corresponding compile options | Yes; runtime probe |
| Window functions / UPSERT / generated columns / RETURNING / STRICT | SQLite 3.25 / 3.24 / 3.31 / 3.35 / 3.37+ respectively | Native SQL |
| Trusted extension loading | Load-extension symbols and supported secure ABI | Opt-in; Apple arm64 requires the CGO backend |
| Custom Go SQL functions, collations, hooks, FTS5 tokenizers | Audited native-to-Go callback lifetimes and ABIs | Not implemented |
| Session/changesets, incremental BLOB I/O, serialization | Additional native APIs | Not implemented |

The optional bundled build enables FTS5, RTree, DBSTAT, math and native JSON/JSONB.
It does not enable every optional SQLite compile flag.

```sql
CREATE VIRTUAL TABLE documents USING fts5(title, content, tokenize='unicode61');
-- Execute each statement in its own call.
INSERT INTO documents VALUES ('SQLite', 'PureGo FFI SQLite driver');
SELECT title, bm25(documents), highlight(documents, 1, '[', ']')
FROM documents WHERE documents MATCH 'SQLite' ORDER BY bm25(documents);

SELECT json_extract('{"answer":42}', '$.answer');
CREATE VIRTUAL TABLE boxes USING rtree(id, minX, maxX, minY, maxY);
```

FTS5 uses native tokenizers, phrase/prefix queries, BM25, snippets and Unicode
processing. There is no separate Go search implementation.

## Trusted runtime extensions

Disabled by default. The caller must trust the native code and use an absolute
path. `WithExtensions` opts in; only the C API is temporarily enabled. SQL's
`load_extension()` stays disabled, including after failures.

```go
connector, err := sqlite.NewConnector("file:app.db",
    sqlite.WithExtensions(),
    sqlite.WithInit(func(ctx context.Context, c *sqlite.Conn) error {
        return c.LoadExtension(ctx, "/trusted/extension.so", "sqlite3_extension_init")
    }),
)
```

For one reserved `*sql.Conn`, use
`sqlite.LoadExtension(ctx, conn, absolutePath, entryPoint)`. This intentionally
does not accept `*sql.DB`: loading one connection does not initialize a pool.
Native error messages are freed with SQLite's allocator. SQLite retains the
loaded extension until its owning connection closes (extensions that explicitly
request permanent loading follow SQLite's own semantics).

`sqlite3_db_config` is variadic. The binding specializes the integer/pointer-only
`SQLITE_DBCONFIG_ENABLE_LOAD_EXTENSION` call on Linux amd64/arm64, Windows
amd64/arm64 and macOS amd64. It is **not** a general variadic binding. Apple arm64
passes unnamed arguments differently; `WithExtensions` returns `ErrUnsupported`
on the CGO-disabled PureGo backend.
The default CGO backend uses a C variadic trampoline and supports this
operation on Apple arm64 as well. Native extensions can
register functions/tokenizers; Go callback APIs remain deferred pending separate
ABI, panic, reentrancy, foreign-thread and callback-lifetime validation.

## Online backup

```go
source, err := db.Conn(ctx)
if err != nil { return err }
defer source.Close()
err = sqlite.Backup(ctx, source, "/absolute/path/snapshot.db")
```

Uses the native online backup API in bounded page batches with context checks.
The destination's existing main database is **replaced**. Use a different database
file. Destination locks return a native busy/locked error; retry at the application
level. Checkpoints are available through `PRAGMA wal_checkpoint(PASSIVE)` (and
SQLite's other checkpoint modes); no Go hook API is claimed.

## Optional embedded SQLite

Prebuilt libraries can optionally export `purego_sqlite_read_row_v1`. The driver
detects this versioned interface once when loading each library and reads all
columns of a row in one native call. `connector.HasFeature("row_read_v1")`
reports availability. System SQLite and older prebuilts without this export
continue through standard SQLite APIs. No custom export is required to open a
database; a failed bridge read returns an error and is never silently retried.
Native TEXT/BLOB buffers are copied before the next step/reset/finalize.
The row output uses connection-owned SQLite memory (24 bytes per cached column),
reused under the connection lock and freed on Close. Go buffers are never hidden
inside a reusable native call frame.

Libraries can also export `purego_sqlite_exec_scalar_v1` to bind all positional
scalar parameters, execute to completion, capture results and reset in one native
call. `connector.HasFeature("exec_scalar_v1")` reports availability. INTEGER,
REAL, BOOLEAN and NULL use this path for Exec; TEXT/BLOB, named arguments and
Query use standard bindings. The driver validates every argument before using
the helper. It preserves detailed errors before cleanup and never retries a
failed execution. Native scalar scratch uses 16 bytes plus 16 bytes per cached
parameter, grows on demand under the connection lock and is freed on Close.
These native buffers are additional to Go's benchmark B/op measurements.

The companion's bridge is compiled alongside the unchanged SQLite amalgamation
into the same shared library. Applications still support `CGO_ENABLED=0` and do
not need an additional helper library. The linked CGO engine continues to use
standard APIs unless an explicitly selected library provides the bridge.

For committed prebuilt binaries, use the companion
[purego-sqlite-embed](https://github.com/yuhaiin/purego-sqlite-embed) module:

```go
import sqliteembed "github.com/yuhaiin/purego-sqlite-embed"

library, err := sqliteembed.Path()
if err != nil {
    return err
}
connector, err := sqlite.NewConnector("file:app.db", sqlite.WithLibrary(library))
```

The companion embeds only the matching target library, verifies its hash and
extracts it to a private directory. No runtime download or local SQLite build is
required. See its platform table for libc, MIPS floating-point ABI, minimum OS
versions and the `sqlite_musl` build tag. CGO builds already link SQLite by default; the companion is useful for
CGO-disabled builds or explicit shared-library selection. The two modules remain independent; using the system
library does not add the prebuilt assets to your dependency graph.

To build your own assets inside the driver repository, build the pinned
**official SQLite 3.54.1 amalgamation** on each target:

```sh
python3 scripts/build-native.py --embed
CGO_ENABLED=0 go test -tags sqlite_bundle ./...
CGO_ENABLED=0 go build -tags sqlite_bundle ./...
```

Select `sqlite.WithBundled()` in the application. The script requires Python 3
and a native C compiler (`cc` on Unix, `clang` plus Visual Studio tools on Windows).
`CC` may override the compiler. It verifies both pinned SHA-256 and SQLite's
published SHA3-256 before compiling. Generated platform-specific embed files and
assets are ignored by Git; CI uploads them with a compiler/flags/source/hash
manifest. These are locally generated assets, not downloaded executable binaries.
Copy the matching generated source and assets into your build tree to reuse a CI
artifact. By default the script selects `go env GOOS/GOARCH`. Cross builds use `CC` plus
`--goos`, `--goarch` and `--output` to select matching assets, as demonstrated by
`scripts/test-mipsle.sh`. The script does not supply a cross toolchain/sysroot or
promise bit-identical outputs across C toolchains.

At runtime, the embedded SHA-256 is verified before extraction to a unique
private temporary directory (0700, library file 0500). Extraction is synchronized
and reused for all bundled connectors in the process. The library and directory
remain for process life; particularly Windows cannot delete a loaded DLL.
After processes exit, deployments may remove stale `purego-sqlite-*` directories
from their configured temporary location. This avoids deleting another running
process's library. Read-only/no-exec temp environments should use an explicit
preinstalled native path. A bundled Go binary is **not fully statically linked**.

The driver is MIT licensed; SQLite is [public domain](https://www.sqlite.org/copyright.html).
PureGo is Apache-2.0 and x/sys is BSD-3-Clause. Distribute the applicable Go
licenses and check your platform C-runtime licensing when redistributing assets.

## Tests and CI

```sh
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
python3 scripts/build-native.py --embed
SQLITE_TEST_LIBRARY="$PWD/.native/libsqlite3.so" \
SQLITE_TEST_EXTENSION="$PWD/.native/test_extension.so" \
SQLITE_REQUIRE_FEATURES=1 CGO_ENABLED=0 go test ./...
SQLITE_TEST_BUNDLED=1 CGO_ENABLED=0 go test -tags sqlite_bundle ./...
CGO_ENABLED=1 go test -race ./...
```

Adjust filenames for macOS/Windows. Go's race detector requires `CGO_ENABLED=1`;
`CGO_ENABLED=0 go test -race` is rejected by Go itself. CGO-enabled race tests
exercise the default CGO backend; add `-tags sqlite_purego` to test PureGo with
the race runtime. Race testing is supplementary: it cannot prove native memory safety.

CI checks formatting, vet, pinned staticcheck, the minimum Go version, the
CGO-free dependency graph, file-backed integration tests, native capabilities,
locally compiled extensions, embedded integrity and benchmark smoke tests.
Native jobs use all six primary OS/architecture combinations; race jobs cover
Linux and macOS. The CGO backend also runs under strict `cgocheck2` and race checks
on those Unix runners. Additional jobs exercise CGO and PureGo on FreeBSD amd64,
CGO on Linux mipsle/QEMU and fully static musl executables on Alpine amd64/arm64 (`scripts/test-mipsle.sh`). Optional system features may skip when absent; bundled feature jobs require
FTS5, JSON/JSONB and RTree. Apple arm64's secure extension limitation is reported
separately. Tests cover repeated cancellation/reuse, lifecycle races, connection
churn, empty/NUL/large bindings, metadata changes, transaction error recovery,
WAL contention, backups and `PRAGMA integrity_check`.

## Performance and comparison

[Benchmark methodology and measured results](benchmarks/README.md) include all
15 requested categories (18 read/write and batching cases), ns/op, B/op, allocs/op,
ops/s, relative timings, native source identity and compile options.

```sh
scripts/benchmark.sh                  # PureGo, mattn and modernc; five 200ms samples
BENCH_BACKEND=cgo scripts/benchmark.sh # Linked CGO, mattn and modernc
BENCH_BACKEND=cgo-dynamic scripts/benchmark.sh # Dynamic CGO, mattn and modernc
scripts/benchmark.sh -benchtime=1x -count=1  # smoke only
```

The comparison lives in its **own Go module**; mattn and modernc.org/sqlite are
benchmark-only dependencies. CGO is used by mattn and by the default CGO backend. Same-engine checks compare
source IDs and all compile options; PureGo uses the same shared library, while
the static CGO comparison resolves both drivers to the linked official engine,
verified by source ID and compile-option equality. Results are workload- and
machine-specific, and memory/storage-heavy work must be tested on deployment
hardware. modernc uses its own Go translation and compile options, recorded
separately in the logs. Reusing statements avoids prepare overhead. PureGo hot
calls use the documented SyscallN API with a private scalar argument frame per
connection; Go buffer arguments retain compiler lifetime tracking, and floating
point retains typed ABI bindings. Native column data is copied before step or
reset. CGO combines execution/result reads and reset/clear to reduce crossings.

With a compatible embed prebuilt, the optional row reader reduces the
three-column scan to two native calls per row. A same-library, CGO-disabled
comparison reduces FullScan1000 time by about 40%, with the same Go allocation
count. The published prebuilt run takes about 0.713ms, versus matching mattn
0.605ms and modernc 0.591ms. The benchmark README puts all five implementations
in one combined table with standard-library and embed columns; the bridge does not
make every workload faster than modernc.

Compared with mattn, this driver removes the application's CGO build requirement
on the six primary PureGo targets but still requires native-library deployment.
CGO builds add a statically linked official SQLite engine. mattn has a much longer production
history and a broader Go callback API. Performance parity and universal feature
parity are not claimed. Compared with pure-Go/WASM engines, this driver uses the
official native engine and its native extension ecosystem.

## Status and limitations

This is a new implementation. Core driver, context/lifecycle handling, native
features, platform loaders, optional asset builds and comprehensive benchmarks
are implemented. Platform CI results are available on the pull request; compiling
a cross-target binary alone does not validate its runtime ABI.

Go callbacks, custom Go FTS5 tokenizers, hook APIs, session/changesets,
serialization and incremental BLOB APIs remain future work. Secure runtime
extensions are unavailable on Apple arm64's CGO-disabled PureGo backend; use
CGO when needed. Embedded assets require a separate
native build step and process-lifetime temp cleanup as described above. Busy
waits and extension work may delay cancellation. There is no statement cache,
no multi-result/multi-statement execution, no automatic date conversion and no
bundled third-party extension such as sqlite-vec. Validate workload correctness,
performance and deployed native-library provenance before production adoption.

Implementation references: [SQLite C API](https://www.sqlite.org/c3ref/intro.html),
[interrupt lifecycle](https://www.sqlite.org/c3ref/interrupt.html),
[transient bindings](https://www.sqlite.org/c3ref/bind_blob.html),
[secure extension configuration](https://www.sqlite.org/c3ref/c_dbconfig_defensive.html#sqlitedbconfigenableloadextension),
[FTS5 tokenizer API](https://www.sqlite.org/fts5.html#custom_tokenizers).
