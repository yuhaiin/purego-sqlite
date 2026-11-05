# Feature parity assessment: mattn/go-sqlite3

Research date: 2026-11-05. This document assesses API coverage and migration semantics; it does not commit to implementing every API listed here.

This is a historical assessment of the main snapshot before implementation.
See the [compatibility guide](mattn-compatibility.md) for the implemented DSN,
type-conversion and Go SQL extension APIs, and
[snapshots, backup, caching and hooks](mattn-medium.md) for the implemented
connection APIs and their validation. The gaps below describe that baseline;
consult the [current capability matrix](../README.md#native-features-and-capabilities)
for present support.

## Comparison scope

- mattn/go-sqlite3: source pinned to [45f73f261e58ede2a09b91a392ef13a5ec0bb0fa][mattn-tree], rather than relying only on its README or older releases.
- The latest release at the time of this assessment was [v1.14.52][mattn-release] (2026-09-05, source `b0be46fa28d17ee0b65c79774ac0dad84b6db068`), also used by this repository's benchmarks/go.mod. Its statement cache was checked separately, so the cache recommendations are not limited to unreleased master code.
- purego-sqlite: remote main [550d58b7e757e5dbc83fe128fe27a7cdc1421948][ours-tree].
- The research checkout was `eedc669e5bf73d34b5ba1ca0d37d112dea0e2760` on `codex/connection-limits`. Its differences from that main snapshot concern Go code generation, automatic updates, and related CI/documentation. The connection, binding, rows, backup, and limits implementations assessed here are identical.

Prioritize DSN and type semantics that change application behavior during migration, followed by Go scalar functions/collations, in-memory snapshots, and backup/restore. FTS, JSON, WAL, and RETURNING already provided by the SQL engine do not require new driver wrappers. Callbacks can use the public SQLite ABI without modifying system libsqlite3; the main cost is validating backend ABIs, connection locking, and ownership.

## Existing capabilities

| Capability | Baseline implementation | Remaining gap |
|---|---|---|
| Initialization of each physical connection | `WithInit` covers the use case of mattn's `SQLiteDriver.ConnectHook` | Document that initialization runs for every new connection; a different name does not imply a missing feature |
| Runtime resource limits | `Conn.SetLimit(category, value)`; negative values query the current limit | mattn exposes separate `GetLimit`/`SetLimit` methods; the existing API already covers both operations |
| Online backup | `BackupTo(ctx, filename)` / `Backup(ctx, *sql.Conn, filename)` | Copies main to a separately opened file connection, replacing existing contents; lacks arbitrary connections, attached schemas, restore, and progress control |
| Native extension loading | `WithExtensions` + `LoadExtension`, with absolute paths and explicit authorization | Preserve system-library, platform, and static-musl restrictions rather than relaxing them for API naming parity |
| SQL scripts without parameters | `Exec` accepts semicolon-separated statements; SQLite parses their boundaries | Parameterized scripts and multi-statement Query are unsupported |
| Metadata | Declared type names and unknown nullability | Missing `ColumnTypeScanType`; type names are uppercased and size parameters removed |
| Native SQL | Savepoints, WAL, JSON, FTS, RTree, window SQL, and other features of the selected library | Distinguish a module being compiled into SQLite from a Go wrapper being available |

Sources: this driver's [initialization][ours-driver], [limits][ours-limits], [backup][ours-backup], [extensions][ours-extensions], [script execution][ours-conn], [row metadata][ours-rows], and [capability detection][ours-capabilities]; mattn's [ConnectHook][mattn-driver], [limits][mattn-limits], and [backup][mattn-backup].

## Compatibility gaps with the greatest migration impact

### 1. DSN configuration must not silently disappear

Besides native SQLite URI parameters, mattn parses driver-specific parameters and aliases:

| Category | mattn parameters |
|---|---|
| Busy handling, transactions, timezone, connection | `_busy_timeout` / `_timeout`, `_txlock`, `_loc`, `_mutex` |
| Common PRAGMAs | `_foreign_keys` / `_fk`, `_journal_mode` / `_journal`, `_synchronous` / `_sync`, `_cache_size` |
| Other PRAGMAs | `_auto_vacuum` / `_vacuum`, `_defer_foreign_keys` / `_defer_fk`, `_ignore_check_constraints`, `_locking_mode` / `_locking`, `_query_only`, `_recursive_triggers` / `_rt`, `_secure_delete` |
| Legacy options requiring separate assessment | `_case_sensitive_like` / `_cslike`, `_writable_schema`, `_auth*` |
| Statement reuse | `_stmt_cache_size`, default 0; confirmed in v1.14.52 and the pinned master, without assuming support in earlier releases |

These come from [mattn's parameter parser][mattn-dsn]. In contrast, `mode`, `cache`, `immutable`, and `vfs` are [native SQLite URI parameters][sqlite-uri]. This driver already passes `SQLITE_OPEN_URI`; the absence of an identically named Go option does not make them unavailable.

At the baseline, this driver passes DSNs directly to SQLite without parsing mattn-specific parameters. For `file:app.db?_foreign_keys=on&_busy_timeout=100`, standard SQLite/VFS implementations do not apply those PRAGMAs or change the timeout based on these names; the driver's default remains 5000ms. A plain `app.db?_foreign_keys=on` has an additional migration risk: SQLite recognizes only names starting with `file:` as URIs. Without mattn-style DSN parsing, the query can become part of the filename. See the [connection implementation][ours-driver] and [SQLite URI recognition rules][sqlite-uri].

Provide an explicit compatibility policy: support common, verifiable parameters and reject known unsupported mattn parameters with an error and a typed-option or `WithInit` alternative. Do not reject all unknown URI parameters, because custom VFS implementations may need them. Define precedence relative to typed options, conflicting aliases, invalid values, and application order on every physical connection. `_defer_foreign_keys` resets at transaction boundaries; setting it once at connection creation does not make it permanent. This is a driver behavior gap, rather than missing foreign-key or WAL support in SQLite.

Do not copy legacy options indiscriminately. mattn's `sqlite_userauth` implementation explicitly reports that it is no longer supported, and SQLite marks [`case_sensitive_like` as deprecated][sqlite-pragma]. Keeping FULLMUTEX and explicitly rejecting `_mutex=no` is clearer than silently accepting it. See [mattn user authentication][mattn-auth].

### 2. time.Time, BOOLEAN, and column metadata

| Operation | Pinned mattn implementation | Pinned purego-sqlite main |
|---|---|---|
| Bind `time.Time` | Encodes TEXT using `SQLiteTimestampFormats[0]`; the exported format array is mutable | Rejects the value; callers must encode it |
| Read TEXT from DATE / DATETIME / TIMESTAMP | Tries the exported formats, returns zero `time.Time` on failure, supports `_loc` | Returns the original string |
| Read INTEGER from those declared types | Values above `1e12` or below `-1e12` are milliseconds; others are seconds; converts to `time.Time` | Returns int64 |
| Read INTEGER from BOOLEAN | Converts using `val > 0`; negative values become false | Returns int64; binding bool is already supported |
| `Rows.ColumnTypes().ScanType()` | Maps declared types to types such as sql.NullTime, sql.NullBool, and sql.NullInt64 | Optional interface absent; database/sql uses its default type |
| Declared type name | Original `sqlite3_column_decltype` string | Uppercases and removes size parameters, e.g. VARCHAR(50) becomes VARCHAR |
| Nullability | Hardcodes `(true, true)` | Returns `(true, false)`, explicitly reporting that nullability is unknown |

Sources: mattn's [timestamp formats][mattn-time-formats], [binding][mattn-time-bind], [read conversion][mattn-time-read], and [metadata][mattn-types]; this driver's [binding][ours-stmt] and [rows][ours-rows]. SQLite itself has storage classes and type affinity; automatic date/boolean conversion is driver policy. See [SQLite's type system][sqlite-types].

Design time binding, date reading, and boolean reading separately. An explicit mattn compatibility mode can preserve raw values for existing users. `ScanType` is relatively inexpensive but must agree with actual returned values. Do not present unknown nullability as known, or copy a globally mutable format table into a concurrent driver solely for parity. Validate negative booleans, invalid dates, integer second/millisecond boundaries, NULL, expression columns, timezone/DST, schema-triggered reprepare, and the optional row bridge.

### Temporary comparison probe

An independent temporary Go module on Linux used local replacements for the two pinned sources. mattn used CGO; this driver was tested with both `CGO_ENABLED=1 go run -tags=sqlite_purego .` and `CGO_ENABLED=1 go run -tags=sqlite_dynamic .`. Both driver backends produced the same results. This probe is not a repository regression test and does not establish behavior on other platforms.

| Probe | purego-sqlite | mattn |
|---|---|---|
| Read PRAGMAs after opening `file:...?_foreign_keys=on&_busy_timeout=17` | foreign_keys=0, busy_timeout=5000 | foreign_keys=1, busy_timeout=17 |
| Bind `time.Time` | Explicit rejection | Successful insert |
| Scan BOOLEAN / TIMESTAMP into any | int64 / string | bool / time.Time |
| `ScanType()` for those columns | interface{} / interface{} | sql.NullBool / sql.NullTime |
| Plain absolute path `/tmp/.../name.db?_foreign_keys=on` | Filename includes `?_foreign_keys=on` | Removes parameters and creates name.db |

The last result follows the driver's existing native-URI contract and is a migration trap, rather than sufficient evidence of a driver bug. Adding `file:` fixes filename recognition only; it does not apply mattn-specific options. Those still require typed options, `WithInit`, or a new compatibility parser.

## Public mattn APIs missing from the baseline

| Suggested priority | Feature and mattn API | Value, implementation cost, and system-library requirements |
|---|---|---|
| High | `RegisterFunc(name, impl, pure)` | Migrates Go scalar functions, regular expressions, and application calculations. Supports reflection, variadic arguments, and `(value, error)` returns; `pure` maps to DETERMINISTIC. Requires shared callback/value/result infrastructure, without a custom shared library. |
| High | `RegisterCollation(name, cmp)` | Custom ordering and string indexes; can reuse callback ownership infrastructure. Comparators must define a stable total order. |
| Medium-high | `Serialize(schema)` / `Deserialize(bytes, schema)` | In-memory loading, test fixtures, and snapshots; no Go callbacks, making this an independent increment. Bind symbols optionally and return ErrUnsupported when absent, preserving older system libraries. mattn's default amalgamation implements this; its `libsqlite3` mode additionally requires the `sqlite_serialize` tag. |
| Medium-high | `Backup(destSchema, srcConn, srcSchema)` + `Step/Remaining/PageCount/Finish/Close` | Adds restore, memory-to-memory copies, ATTACH schemas, and progress. A context-managed connection-to-connection copy with progress options can provide this without exposing manually managed native handles. Both connections must belong to the same SQLite library instance. |
| Medium | `RegisterAggregator(name, ctor, pure)` | Go Step/Done aggregation; each group needs independent cleanup, including empty input, Step errors, cancellation, and final destruction. Add after callback infrastructure is stable. |
| Medium | `RegisterUpdateHook` / `RegisterCommitHook` / `RegisterRollbackHook` | Connection-local write notifications, commit vetoes, and transaction events. Callbacks cannot modify the invoking connection. Update notifications are not complete CDC: WITHOUT ROWID tables, REPLACE, and truncate-delete optimizations have exceptions. |
| Medium | `RegisterAuthorizer` | Controls table, column, and operation access for untrusted SQL during prepare or automatic reprepare; wrapping Exec alone is insufficient. mattn passes an action and three strings; a new API can preserve SQLite's complete metadata. |
| Medium | `SetTrace(*TraceConfig)`, `TraceInfo` | SQL and performance observability; mattn requires `sqlite_trace` or `trace`. Expanded SQL must be opt-in to avoid recording parameter values unexpectedly. Reuse hook ownership infrastructure. |
| Medium-low | `GetFilename`, `AutoCommit`, `SQLiteStmt.Readonly` | Lightweight diagnostics. This driver already binds the native APIs internally; expose locked methods as needed without raw handles. |
| Medium-low | `SetFileControlInt` / `SetFileControlInt64` | VFS configuration and persistent WAL. The opcode determines the actual `void*` type, so arbitrary opcodes cannot safely receive an int pointer. Prefer typed, allowlisted operations with correct NOTFOUND and direct-return-code handling. |
| Medium-low | `SQLiteStmt.ColumnTableName` | The `sqlite_column_metadata` tag exposes source tables for result columns; requires COLUMN_METADATA in the library and differs from declared type names. |
| Medium, relatively low cost | `Error.SystemErrno`, named extended error constants | mattn adds system errno to some CANTOPEN/IOERR errors and provides constants such as ErrConstraintUnique, ErrConstraintForeignKey, and ErrBusySnapshot. This driver has Code/ExtendedCode/Message and errors.Is support for Busy/Locked/Constraint/ReadOnly, but lacks equivalent diagnostic convenience. Preserve native codes and do not misreport unknown errno as a definite system cause. |
| Low, demand-driven | `CreateModule` / `DeclareVTab`, Module/VTab/VTabCursor | mattn's `sqlite_vtable` / `vtable` tags expose Go virtual tables. Requires substantial function-pointer and structure ABI work, cursor/error memory ownership, and lifecycle management; much more involved than running native FTS5 SQL. |
| Low, demand-driven | `RegisterPreUpdateHook`, `SQLitePreUpdateData.Old/New/Depth/Count` | mattn has a real implementation only with `sqlite_preupdate_hook`; the default method is a no-op. Requires PREUPDATE_HOOK in SQLite and cannot be promised for every system library. |

API sources: mattn's [functions, aggregators, collations, and hooks][mattn-callbacks], [serialization][mattn-serialize], [system-library serialization stub][mattn-serialize-omit], [backup][mattn-backup], [trace][mattn-trace], [connection state and file control][mattn-limits], [statement Readonly][mattn-readonly], [column metadata][mattn-column-metadata], [system errno][mattn-errno], [named error codes][mattn-errors], [virtual tables][mattn-vtable], [preupdate][mattn-preupdate], and [default preupdate stub][mattn-preupdate-omit].

Native contracts: [function registration/destruction][sqlite-functions], [online backup][sqlite-backup], [serialize][sqlite-serialize], [deserialize][sqlite-deserialize], [update hooks][sqlite-update], [commit/rollback hooks][sqlite-commit], [authorizer][sqlite-authorizer], [trace_v2][sqlite-trace], and [file_control][sqlite-filecontrol]. These are public ABIs and generally require no shared-library rebuild. Selecting another library or changing build options is necessary only when the selected library omits an API or module.

### Multi-statement execution and statement caching

mattn's Exec parses each SQL tail and consumes parameters per statement. Query also processes tails, but returns only the final rows. It does not implement `RowsNextResultSet`, so it does not expose every result through NextResultSet. At the baseline, this driver accepts only parameterless Exec scripts; Prepare, Query, and parameterized Exec require a single statement. See [mattn's execution logic][mattn-scripts], this driver's [execution][ours-conn], and [single-statement checks][ours-stmt].

Parameterized scripts have migration value, but require defined parameter allocation across statements, named-parameter reuse, partial execution after an error, and final-result semantics. Traversable multiple result sets are a separate feature. The pinned mattn version offers an optional connection cache through `_stmt_cache_size`; this driver lacks transparent caching, while callers can already reuse `sql.Stmt`. Validate benefits on actual workloads, including schema changes, driver state, connection closure, and eviction after errors. See [mattn's cache configuration][mattn-dsn].

A bounded, default-off cache per physical connection can reduce prepare/finalize overhead when applications repeatedly call `db.Exec` / `db.Query` with the same SQL without reusing a statement. It does not imply improvements for explicitly prepared loops or per-row FullScan reads. Measure repeated SQL, distinct SQL, schema changes, and memory retention after large parameters before enabling it. mattn v1.14.52's [configuration][mattn-cache-stable] and [LRU/cleanup implementation][mattn-cache-impl] are useful references. No custom SQLite library is required.

## Native SQLite APIs outside the confirmed mattn gaps

Inspection of the pinned public Go API and optional implementations did not establish mattn parity requirements for incremental BLOB handles, sessions/changesets, custom Go window functions with xInverse/xValue, standalone Go FTS5 tokenizer registration, comprehensive db_status/stmt_status statistics, or custom Go VFS implementations. Native SQLite capabilities and use through C extensions do not establish equivalent built-in Go wrappers. Sources: [pinned mattn tree][mattn-tree], [SQLite BLOB API][sqlite-blob], [Session extension][sqlite-session], [window function registration][sqlite-functions], and [FTS5 tokenizers][sqlite-fts5].

Historical `sqlite_userauth` types/APIs and every mattn compile switch should not automatically become roadmap items. Diagnose library build features through Connector CompileOptions/HasFeature first. The driver should continue supporting official system shared libraries. See [capability detection][ours-capabilities].

## Callback infrastructure and validation

PureGo 0.11.3 provides `NewCallback`, with different Unix and Windows calling conventions. Callback slots are finite process-wide resources and are not reclaimed. Allocating a new closure trampoline for every connection or registration would exhaust them during normal connection churn. Prefer a small number of process-wide trampolines per signature and an integer-token registry for connection-owned Go callbacks. Release tokens and Go references on closure, replacement, and failure. See [PureGo Unix callbacks][purego-unix] and [Windows callbacks][purego-windows].

Validate the following constraints:

1. Execute native tests for PureGo amd64/arm64 on Linux, macOS, Windows, and the supported BSD targets. Cross-compilation alone does not validate Windows cdecl, integer widths, void returns, macOS arm64 callbacks, or race/cgocheck behavior.
2. CGO backends such as MIPS need compiler-typed C trampolines and safe token registration. CGO support does not automatically provide PureGo callbacks on unsupported architectures.
3. Native code must retain tokens rather than Go pointers. SQLite value/context/text/blob pointers usually remain valid only during the callback. Returned TEXT/BLOB values need explicit copying or native ownership.
4. Execution currently holds `Conn.mu`. Callbacks are typically invoked synchronously by `sqlite3_step/prepare` while the lock is held. Same-connection reentry can deadlock or violate SQLite restrictions; define and test a rejection contract rather than unlocking around native calls.
5. Panics must not cross the FFI boundary. Convert scalar/aggregate panics to SQLite errors where possible; authorizer/commit callbacks should fail closed. Hook and trace error reporting requires an explicit API contract.
6. Test replacement, failed registration, closure, cancellation, aggregate destruction, GC pressure, and 10,000 connection cycles. Respect `create_function_v2` destructor ordering and retain tokens while callbacks are active.
7. Go functions can support float64: SQLite xFunc receives context/argc/argv pointers, with values transferred through sqlite3_value_double and sqlite3_result_double. Arbitrary Go signatures need not become native callback signatures. A floating-point return ABI concern alone does not make scalar functions impossible. See the [SQLite function ABI][sqlite-functions].

These are design and validation requirements; the baseline research does not establish that a complete cross-platform callback implementation is safe. Benchmark native-to-Go callback costs separately rather than presenting functional compatibility as a performance advantage.

## Suggested implementation sequence and acceptance criteria

1. **Migration behavior:** common DSNs with explicit errors for unsupported options, time.Time input, optional date/boolean conversion, and ScanType/declared-type contracts. Compare identical data and SQL with mattn while preserving raw-mode behavior.
2. **Independent additions without callbacks:** named error codes/SystemErrno, optional serialization/deserialization, and connection-to-connection backup/restore; add GetFilename/AutoCommit where needed. Missing symbols in older system libraries must not disable unrelated features. Evaluate transparent statement caching separately on ordinary repeated-SQL workloads.
3. **Shared callback ownership:** scalar functions and collations first, then aggregators, authorizer, transaction hooks, and trace. Provide `WithInit` registration examples and correct pool usage. Test both system libraries and embed without requiring helper symbols.
4. **Advanced features driven by callers:** virtual tables, preupdate, file control, and parameterized scripts. Avoid exposing every historical switch or raw C pointer merely to complete a feature list.

## Baseline validation

- Confirmed that the research checkout and pinned main share the assessed core implementation; `CGO_ENABLED=0 go test -count=1 ./...` passed.
- Ran the temporary DSN, time.Time, BOOLEAN/TIMESTAMP, ScanType, and filename probes against the pinned mattn source using both PureGo and dynamic CGO backends.
- Checked public mattn APIs, optional build tags, and official SQLite ABI documentation to distinguish existing capabilities, actual gaps, implementation constraints, and acceptance criteria.
- These checks predate implementation. Consult the compatibility guide and PR checks for subsequent implementation and validation coverage.

[mattn-tree]: https://github.com/mattn/go-sqlite3/tree/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa
[mattn-release]: https://github.com/mattn/go-sqlite3/releases/tag/v1.14.52
[mattn-cache-stable]: https://github.com/mattn/go-sqlite3/blob/b0be46fa28d17ee0b65c79774ac0dad84b6db068/sqlite3.go#L1581-L1594
[mattn-cache-impl]: https://github.com/mattn/go-sqlite3/blob/b0be46fa28d17ee0b65c79774ac0dad84b6db068/sqlite3.go#L1967-L2049
[ours-tree]: https://github.com/yuhaiin/purego-sqlite/tree/550d58b7e757e5dbc83fe128fe27a7cdc1421948
[mattn-driver]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L542-L547
[mattn-callbacks]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L770-L1096
[mattn-dsn]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L1375-L1770
[mattn-time-formats]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L409-L425
[mattn-time-bind]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L2450-L2478
[mattn-time-read]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L3108-L3179
[mattn-types]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_type.go
[mattn-limits]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L2280-L2379
[mattn-readonly]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L2795-L2800
[mattn-column-metadata]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_column_metadata.go
[mattn-errno]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L1112-L1135
[mattn-errors]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/error.go
[mattn-scripts]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3.go#L1142-L1257
[mattn-backup]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/backup.go
[mattn-serialize]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_serialize.go
[mattn-serialize-omit]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_serialize_omit.go
[mattn-trace]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_trace.go
[mattn-vtable]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_vtable.go
[mattn-preupdate]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_preupdate_hook.go
[mattn-preupdate-omit]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_preupdate_omit.go
[mattn-auth]: https://github.com/mattn/go-sqlite3/blob/45f73f261e58ede2a09b91a392ef13a5ec0bb0fa/sqlite3_opt_userauth.go#L30-L49
[ours-driver]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/driver.go
[ours-conn]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/conn.go
[ours-stmt]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/stmt.go
[ours-rows]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/rows.go
[ours-backup]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/backup.go
[ours-limits]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/limits.go
[ours-extensions]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/extensions.go
[ours-capabilities]: https://github.com/yuhaiin/purego-sqlite/blob/550d58b7e757e5dbc83fe128fe27a7cdc1421948/capabilities.go
[sqlite-uri]: https://www.sqlite.org/uri.html
[sqlite-types]: https://www.sqlite.org/datatype3.html
[sqlite-pragma]: https://www.sqlite.org/pragma.html#pragma_case_sensitive_like
[sqlite-functions]: https://www.sqlite.org/c3ref/create_function.html
[sqlite-backup]: https://www.sqlite.org/c3ref/backup_finish.html
[sqlite-serialize]: https://www.sqlite.org/c3ref/serialize.html
[sqlite-deserialize]: https://www.sqlite.org/c3ref/deserialize.html
[sqlite-update]: https://www.sqlite.org/c3ref/update_hook.html
[sqlite-commit]: https://www.sqlite.org/c3ref/commit_hook.html
[sqlite-authorizer]: https://www.sqlite.org/c3ref/set_authorizer.html
[sqlite-trace]: https://www.sqlite.org/c3ref/trace_v2.html
[sqlite-filecontrol]: https://www.sqlite.org/c3ref/file_control.html
[sqlite-blob]: https://www.sqlite.org/c3ref/blob_open.html
[sqlite-session]: https://www.sqlite.org/sessionintro.html
[sqlite-fts5]: https://www.sqlite.org/fts5.html#custom_tokenizers
[purego-unix]: https://github.com/ebitengine/purego/blob/3fc0c5fee75d22b9514d778832e130f2697698a8/syscall_unix.go#L22-L104
[purego-windows]: https://github.com/ebitengine/purego/blob/3fc0c5fee75d22b9514d778832e130f2697698a8/syscall_windows.go#L19-L43
