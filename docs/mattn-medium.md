# Snapshots, backup, caching, and hooks

These APIs operate on a physical `*sqlite.Conn`. Use it inside `WithInit` or
`sql.Conn.Raw`; do not retain it after either callback returns. All native
operations remain compatible with the public SQLite ABI. Optional symbols do not
raise the driver's minimum system-library requirement.

## Database images

`Serialize(schema)` returns an independent Go byte slice. `Deserialize(data,
schema)` replaces that schema with a private, writable in-memory copy; it leaves
the caller's slice and the original database file unchanged. An empty schema
selects `main`. Both also have Context variants. End transactions and close
prepared statements before deserialization; idle cached statements are cleared.
SQLite does not support deserializing into `temp`.

```go
err := conn.Raw(func(raw any) error {
    c := raw.(*sqlite.Conn)
    image, err := c.Serialize("main")
    if err != nil {
        return err
    }
    return c.Deserialize(image, "main")
})
```

Deserialization validates the SQLite header and rejects WAL images. Use online
backup for a live WAL database instead of treating its main file as a complete
image. The driver copies input into padded SQLite-allocated memory, transfers
ownership with FREEONCLOSE, and permits SQLite to grow that buffer. SQLite owns
and frees it even when the native deserialize call fails. This API does not
promise byte-for-byte behavior for malformed input or match mattn's fixed-size
buffer restriction.

`Connector.HasFeature("serialize")` checks both native symbols. Missing symbols
produce `ErrUnsupported` for image operations while ordinary SQL remains usable.
The selected library determines SQLite's format and validation behavior. See the
[serialize](https://www.sqlite.org/c3ref/serialize.html) and
[deserialize](https://www.sqlite.org/c3ref/deserialize.html) contracts.

## Backup and restore

`destination.BackupFrom(ctx, source, options)` copies between two different
physical connections using the same native SQLite library instance. Matching
version numbers alone do not establish engine identity. Empty schema names
select `main`; attached schemas and in-memory databases are supported. Restore
uses the same operation with source and destination reversed.

```go
err := sourceConn.Raw(func(src any) error {
    return destinationConn.Raw(func(dst any) error {
        return dst.(*sqlite.Conn).BackupFrom(ctx, src.(*sqlite.Conn),
            sqlite.BackupOptions{
                SourceSchema: "main",
                DestinationSchema: "main",
                PagesPerStep: 128,
                Progress: func(p sqlite.BackupProgress) error {
                    // p.Remaining, p.PageCount, p.Done
                    return nil
                },
            })
    })
})
```

Acquire the two `sql.Conn` reservations before nesting Raw calls. Pools must have
sufficient capacity; do not concurrently reserve or nest the same connections in
opposite orders through database/sql. Internally, native connection locks use a
fixed order. Callbacks must not use either reserved connection or their pools.

Both connections must have no active transaction/rows, and destination prepared
statements must be closed. Existing destination contents are replaced; copying
into the same database file is rejected. Progress runs synchronously after each
step. Returning an error or panicking aborts an incomplete backup and rolls back
its destination. Once `Done` is reported, SQLite has completed the copy; a
subsequent progress error cannot undo it. External writers may restart a live
backup, so page counts and remaining pages need not be monotonic.

Cancellation is checked between steps; native lock waits use the configured busy
timeout. Busy/locked errors are returned rather than retried indefinitely. Do not
access the destination or its shared cache through another connection during the
copy. The existing `BackupTo(ctx, filename)` and package-level `Backup` helper
use the same implementation. See the
[online backup contract](https://www.sqlite.org/c3ref/backup_finish.html).

## Optional statement cache

`WithStatementCache(size)` or `_stmt_cache_size=size` enables a connection-local
LRU with 0 through 1024 entries. It defaults to zero. Explicit Options override
the DSN. Only one-shot single-statement SELECT, WITH, INSERT, UPDATE, DELETE, and
REPLACE calls with at most 4096 SQL bytes are eligible. Leading comments can
cause a conservative cache miss. PRAGMAs, transaction controls, DDL, scripts, and
explicit `sql.Stmt` lifetimes are not retained by this cache.

Idle entries hold no parameter bindings. Eviction, connection closure, failed
execution/binding, and cancellation release native handles. A closed Rows object
cannot regain ownership of a handle reused by a later query. Query metadata is
refreshed after stepping, including schema-triggered reprepare. Deserialization,
destination backup, and authorizer replacement discard idle entries first.

Repeated `db.Exec`/`db.Query` SQL can save prepare/finalize work. Explicitly
prepared loops already avoid that work, and this does not optimize per-row column
reads. Cache misses add bookkeeping and retained native memory; enable the cache
only when the application workload benefits. The bound limits statement count
and SQL text size, rather than every byte SQLite may allocate for query plans.

## Transaction hooks and authorization

Install hooks on every physical connection through `WithInit`:

- `RegisterUpdateHook(func(operation int, database, table string, rowID int64))`
  reports INSERT (18), UPDATE (23), and DELETE (9).
- `RegisterCommitHook(func() int)` permits commit on zero and vetoes it otherwise.
- `RegisterRollbackHook(func())` observes rollbacks.
- `RegisterAuthorizer(func(Authorization) AuthResult)` receives Action,
  Argument1/Argument2, Database, and Origin, preserving SQLite's trigger/view
  metadata. Return `AuthAllow`, `AuthDeny`, or action-specific `AuthIgnore`.

Nil removes a registration. Replacements and connection closure release integer
tokens and Go references without consuming new native callback slots. All hooks
run synchronously; same-connection reentry is rejected. Do not call the invoking
connection or its database/sql pool from a callback. A blocking callback also
delays the invoking operation and cancellation.

Panics become operation errors. Commit and authorizer panics fail closed;
invalid authorizer results deny access. Update/rollback/trace errors do not
reverse already applied changes: use explicit transactions when callers need
rollback. Authorization runs during prepare or automatic reprepare; replacing it
also clears idle cached programs. This is a SQL authorization callback, rather
than a sandbox around privileged APIs such as backup or deserialization.

Update hooks are not complete change data capture: WITHOUT ROWID tables,
REPLACE deletions, and truncate optimizations have exceptions. Refer to SQLite's
[update](https://www.sqlite.org/c3ref/update_hook.html),
[commit/rollback](https://www.sqlite.org/c3ref/commit_hook.html), and
[authorizer](https://www.sqlite.org/c3ref/set_authorizer.html) contracts.
`HasFeature("authorizer")` detects libraries that omit authorization.

## Trace and diagnostics

`SetTrace(&TraceConfig{Mask: ..., Callback: ...})` supports `TraceStatement`,
`TraceProfile`, `TraceRow`, and `TraceClose`. `TraceEvent` owns its SQL strings;
profile events include Runtime. Nil or a zero mask disables tracing.
`ExpandedSQL` is explicitly opt-in because bound parameters may contain secrets.
Trigger events can contain SQLite's descriptive comments. Close events do not
inspect a closing native database or statement. Trace panics are returned by the
invoking operation, including Close, without unwinding across FFI.

`HasFeature("trace")` reports native trace availability. See
[trace_v2](https://www.sqlite.org/c3ref/trace_v2.html). Trace adds synchronous
native-to-Go callbacks; do not assume that enabling it is free.

`Conn.GetFilename(schema)`, `Conn.AutoCommit()`, prepared statement `Readonly()`,
and `Connector.SourceID()` expose native diagnostics without raw handles. Access
Readonly through a driver statement inside Raw; database/sql.Stmt does not expose
this optional driver method. Unknown schemas and closed handles return errors.
Readonly describes SQLite's direct database writes, rather than the absence of
application callback side effects.

Named error sentinels include constraint variants, busy/snapshot/timeout,
read-only variants, authorization, corruption, I/O, and open failures. Match them
with `errors.Is`; a specific constraint also matches `ErrConstraint`. `*Error`
retains Code, ExtendedCode, Message, and optional SystemErrno. The saved OS error
is populated for CANTOPEN/IOERR where available, excluding IOERR_NOMEM, and is
unwrapped for `errors.Is`. Zero denotes an unavailable system cause. Error codes
are preserved even on platforms without a saved errno.

## Validation and measurement

Regression coverage includes independent snapshot ownership and growth, attached
schemas, incomplete-backup rollback, progress panic/cancellation, opposite lock
orders, cache reuse/LRU/binding cleanup, schema changes, authorization changes,
64-bit rowids, hook vetoes/panics, close tracing, and callback registration churn.
The suite runs across the existing PureGo/CGO/platform matrix. Optional API tests
also verify continued SQL use when image, authorization, or trace bindings are
unavailable.

`BenchmarkMattnMedium` measures cache hits/misses, snapshots, backup, and hook
costs against mattn. Modernc participates in ordinary SQL cache-off controls;
no equivalent extension API or cache setting is assumed. See the
[measured results](../benchmarks/mattn-medium-priority.md) for samples and limits.
