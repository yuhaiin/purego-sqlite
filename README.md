# purego-sqlite

A `database/sql` driver for the **official native SQLite engine**, called through
[PureGo](https://github.com/ebitengine/purego) on the six primary desktop/server
targets, with a **CGO fallback** on other supported targets. The primary targets
build with `CGO_ENABLED=0`; SQLite still runs as a native shared library. This is an original
implementation, not a fork of mattn/go-sqlite3 or a Go/WASM SQLite engine.

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
`sqlite3` registration.

## Platforms and native installation

The runtime CI matrix covers Linux, macOS and Windows, each on amd64 and arm64.
Cross-compiling those six PureGo targets requires no C compiler. Running the
driver always requires a SQLite library matching the process architecture.
Other Linux architectures and BSD targets select CGO as described below.

| Platform | Default library selection | Installation / explicit alternative |
|---|---|---|
| Linux amd64/arm64 | Fixed absolute `/usr/lib`, `/lib`, multiarch and `lib64` paths for `libsqlite3.so.0` | Debian/Ubuntu: `libsqlite3-0`; Fedora: `sqlite-libs`; Arch: `sqlite` |
| macOS amd64/arm64 | `/usr/lib/libsqlite3.dylib` (including dyld shared cache) | System SQLite, or absolute Homebrew/custom path |
| Windows amd64/arm64 | No implicit DLL search | Build SQLite with the supplied script, then use `WithLibrary`, or embed it |

### Automatic CGO fallback

Backend selection happens at **build time**, without changing imports, driver
name, connectors or database/sql behavior:

| Targets | Default backend | Build requirement |
|---|---|---|
| Linux/macOS/Windows amd64 and arm64 | PureGo | `CGO_ENABLED=0` works |
| Linux mipsle, mips, mips64(le), 386, arm, ppc64(le), riscv64, s390x, loong64 | CGO | `CGO_ENABLED=1`, matching C compiler and libc |
| FreeBSD, OpenBSD, NetBSD, DragonFly BSD (Go/CGO-supported architectures) | CGO | `CGO_ENABLED=1`, native or cross C toolchain |
| Other Windows Go/CGO targets | CGO | Matching Windows C toolchain; not covered by runtime CI |
| Other OS targets | Unavailable | Connector returns an explicit unsupported-backend error |

FreeBSD and NetBSD have experimental support upstream in PureGo; this driver
conservatively defaults to CGO there. FreeBSD amd64 has native VM CI, and Linux
mipsle has QEMU runtime CI, including native features, extensions and embedded
libraries. Other fallback combinations are implemented but not claimed as
runtime-validated. On a fallback target, `CGO_ENABLED=0` produces an actionable
connector error instead of trying an incompatible FFI ABI.

The fallback consists of C trampolines around the **same runtime-loaded SQLite
C API**. It does not delegate to mattn or include another SQLite engine. Only the
C compiler and target libc/dynamic loader are required at Go build time; SQLite
development headers and a link-time `-lsqlite3` dependency are not needed. The
existing explicit-library and embedded-library options still work. The compiler
handles 32-bit pointers, 64-bit INTEGERs, doubles, destructors and C varargs.

On FreeBSD, install the runtime library (for example, `pkg install sqlite3`) and
build normally with CGO enabled:

```sh
CGO_ENABLED=1 go build ./...
```

Cross-compiling Linux mipsle with a matching hard-float toolchain:

```sh
CGO_ENABLED=1 GOOS=linux GOARCH=mipsle GOMIPS=hardfloat \
  CC=mipsel-linux-gnu-gcc go build ./...
```

Deploy a matching **mipsle** SQLite shared library and libc on the device. For a
soft-float/musl router, use that device's SDK/compiler and matching `GOMIPS` and C
compiler flags; do not copy a hard-float/glibc library onto it. Cross-compiling
CGO requires the target C toolchain/sysroot even though PureGo cross-builds do not.

An explicit `-tags sqlite_cgo` selects CGO on a primary target too, for backend
testing or where the compiler's ABI handling is desired (including secure
extensions on Apple arm64). It requires `CGO_ENABLED=1`:

```sh
CGO_ENABLED=1 go test -tags sqlite_cgo ./...
```

Library precedence is deterministic: **explicit `WithLibrary` > requested
`WithBundled` > system**. An explicitly selected library never silently falls
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
on the default PureGo backend rather than enabling SQL loading as a fallback.
The optional `sqlite_cgo` backend uses a C variadic trampoline and supports this
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
versions and the `sqlite_musl` build tag. The driver's automatic CGO fallback still
applies on MIPS and FreeBSD. The two modules remain independent; using the system
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
`CGO_ENABLED=0 go test -race` is rejected by Go itself. Enabling CGO for that test
runtime does not select the fallback on primary targets unless `-tags sqlite_cgo` is
also supplied. Race testing is
supplementary: it cannot prove native memory safety.

CI checks formatting, vet, pinned staticcheck, the minimum Go version, the
CGO-free dependency graph, file-backed integration tests, native capabilities,
locally compiled extensions, embedded integrity and benchmark smoke tests.
Native jobs use all six primary OS/architecture combinations; race jobs cover
Linux and macOS. The CGO backend also runs under strict `cgocheck2` and race checks
on those Unix runners. Additional jobs run automatic fallback on FreeBSD amd64
and Linux mipsle/QEMU (`scripts/test-mipsle.sh`). Optional system features may skip when absent; bundled feature jobs require
FTS5, JSON/JSONB and RTree. Apple arm64's secure extension limitation is reported
separately. Tests cover repeated cancellation/reuse, lifecycle races, connection
churn, empty/NUL/large bindings, metadata changes, transaction error recovery,
WAL contention, backups and `PRAGMA integrity_check`.

## Performance and comparison

[Benchmark methodology and measured results](benchmarks/README.md) include all
15 requested categories (17 separate read/write cases), ns/op, B/op, allocs/op,
ops/s, relative timings, native source identity and compile options.

```sh
scripts/benchmark.sh                  # Linux: 200ms samples, five repetitions
scripts/benchmark.sh -benchtime=1x -count=1  # smoke only
```

The comparison lives in its **own Go module**; mattn is a benchmark-only
dependency, and CGO is used by mattn and by explicitly selected fallback builds. Both drivers use exactly the same built native SQLite library,
verified by source ID and compile-option equality. Results are workload- and
machine-specific, and memory/storage-heavy work must be tested on deployment
hardware. Reusing statements avoids prepare overhead. Integer-only hot FFI calls
use PureGo's documented SyscallN API; floating point and native pointer conversion
retain typed ABI bindings. No SQLite-owned buffer is cached beyond its lifetime.

Compared with mattn, this driver removes the application's CGO build requirement
on the six primary PureGo targets but still requires native-library deployment.
Fallback targets require CGO. mattn has a much longer production
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
extensions are unavailable on Apple arm64's default PureGo backend; use
`-tags sqlite_cgo` with CGO enabled when needed. Embedded assets require a separate
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
