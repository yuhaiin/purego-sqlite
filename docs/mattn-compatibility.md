# mattn compatibility and Go extensions

The registered driver remains `sqlite3purego`. Existing `database/sql` code can
keep its pooling and transaction handling; concrete mattn driver types must be
adapted to this package. These APIs work with ordinary system SQLite, the embed
companion and linked CGO. They require no custom SQLite exports or patched so.

## Connection strings

Recognized driver parameters are removed before opening the database and applied
to **every new physical connection**, before `WithInit`. Explicit connector
Options take precedence. A plain `app.db?_foreign_keys=on` opens `app.db`, rather
than a filename containing the query. To name a file containing `?`, use a
percent-encoded `file:` URI. Native URI/VFS parameters and URI fragments retain
SQLite semantics.

| Parameters | Values |
|---|---|
| `_busy_timeout`, `_timeout` | Nonnegative milliseconds fitting a C int |
| `_txlock` | DEFERRED, IMMEDIATE, EXCLUSIVE |
| `_journal_mode`, `_journal` | DELETE, TRUNCATE, PERSIST, MEMORY, WAL, OFF |
| `_synchronous`, `_sync` | OFF/NORMAL/FULL/EXTRA or 0/1/2/3 |
| `_foreign_keys`, `_fk` | Boolean |
| `_defer_foreign_keys`, `_defer_fk` | Boolean; SQLite resets it at transaction end |
| `_ignore_check_constraints`, `_query_only` | Boolean |
| `_recursive_triggers`, `_rt` | Boolean |
| `_locking_mode`, `_locking` | NORMAL, EXCLUSIVE |
| `_auto_vacuum`, `_vacuum` | NONE/FULL/INCREMENTAL or 0/1/2 |
| `_secure_delete` | Boolean or FAST |
| `_cache_size` | Signed C int, including negative KiB budgets |
| `_loc` | IANA time zone, UTC, Local, or `auto` for time.Local |
| `_mutex` | `full`; disabling SQLite's mutex is unsupported |

Booleans accept 0/1, false/true, off/on, no/yes, case-insensitively. Enumeration
values are case-insensitive. Duplicate parameters and conflicting aliases fail
before opening a library. Invalid values cannot become interpolated SQL.
The obsolete `_auth*`, writable-schema and case-sensitive-like switches, and
the not-yet-supported `_stmt_cache_size`, return `ErrUnsupported`. Unknown
custom URI parameters pass through for VFS compatibility. `mode`, `cache`,
`immutable` and `vfs` remain native SQLite parameters. Requested WAL must actually
be available, including when selected by DSN; in-memory databases cannot use WAL.

## Dates, booleans and metadata

All modes accept `time.Time` parameters, encoded as TEXT using
`2006-01-02 15:04:05.999999999-07:00`. Writes preserve the supplied UTC offset.
No global mutable format list is exposed.

Default reads retain SQLite storage classes. `WithMattnTypes()` opts into:

- DATE/DATETIME/TIMESTAMP text parsing using mattn's standard timestamp formats.
  Invalid date text returns zero time. INTEGER timestamps use seconds, or
  milliseconds when outside the inclusive range -1e12..1e12; NULL remains NULL.
- BOOLEAN INTEGER decoding using `value > 0`, including mattn's negative-value
  behavior. BLOB/TEXT in a BOOLEAN declaration retain their storage classes.
- Unmodified declaration strings in `ColumnTypes().DatabaseTypeName()`.

`WithTimeLocation(loc)` enables date parsing and converts results into that
location. `_loc` has the same effect; `auto` selects time.Local. Offset-free text
is interpreted in UTC before conversion. `WithBooleanConversion()` enables only
the BOOLEAN behavior. Neither changes arbitrary TEXT or expression columns.
`ScanType()` uses nullable Go types matching the selected mode. Nullability stays
unknown instead of claiming that every SQLite expression has known constraints.
Statement metadata refreshes after schema reprepare, including the row helper.

## Functions, aggregates and collations

Register on every pooled connection:

```go
connector, err := sqlite.NewConnector("file:app.db",
    sqlite.WithInit(func(ctx context.Context, c *sqlite.Conn) error {
        return c.RegisterFunc("normalize_name", strings.ToLower, true)
    }),
)
```

`RegisterFunc(name, implementation, pure, flags...)` supports integer types,
float32/64, bool, string, []byte, named aliases of these types and `any` inputs
and results. Variadic functions and `(value, error)` results work. INTEGER is
required for integer/bool arguments, REAL for float arguments, and TEXT/BLOB for
string/byte arguments. SQL NULL requires an `any` input and is represented as
a nil []byte, matching mattn. A nil result or nil []byte produces SQL NULL;
non-nil empty []byte produces an empty BLOB. Embedded NUL is retained. Native
input buffers are copied; returned TEXT/BLOB is copied by SQLite before return.

`RegisterAggregator(name, constructor, pure, flags...)` accepts a no-argument
constructor returning a pointer or interface with `Step(args...) [error]` and
`Done() (value [, error])`. Constructors can return an additional error. Each
SQLite group, including empty input, gets an independent instance. Cancellation,
failed steps, finalization, replacement and connection close release state.

`RegisterCollation(name, compare)` accepts `func(string,string) int`; comparison
results are normalized to -1/0/+1. Comparators must define consistent ordering.
Passing nil removes the collation. A panic interrupts the statement and returns
an error rather than treating the two keys as equal.

`pure` sets SQLITE_DETERMINISTIC. Optional `FunctionDirectOnly` and
`FunctionInnocuous` flags require SQLite 3.31+. They allow callers to choose the
appropriate policy for schema use with `trusted_schema=OFF`; deterministic does
not imply innocuous. Unsupported flags/versions fail explicitly.

Callbacks run synchronously while the physical connection is reserved. Do not
call any method on that connection, its rows/statements/transaction, or its
database/sql pool from the callback. Direct driver reentry is rejected; a pool
wait can occur before control reaches the driver. Go code that blocks forever
also delays cancellation. Scalar/aggregate errors and panics become SQLite
errors. A failed statement does not leave a callback active on the next query.

The native library retains integer tokens, never Go pointers. SQLite's v2
destructors release Go registrations on replacement/close. Entrypoints are
shared per native engine, so connection churn does not consume callback slots.
CGO uses compiler-typed C entrypoints on 32-bit targets too. At most 64 distinct
native engines can supply Go callbacks in a process; ordinary SQL connections do
not consume these slots. The selected engine's own user-data API is always used.

## Tests and benchmarks

The normal suite includes per-connection DSN application, invalid/aliased URI
parameters, timezones and raw/compatible values, nullable values, callback
argument/result types, errors/panics, deterministic/security flags, grouped and
empty aggregates, cancellation/reuse, replacement, and 10,000 connection cycles.
Run it with CGO disabled, default CGO, forced PureGo/race and cgocheck2. CI keeps
the native OS/CPU matrix and the MIPS CGO execution checks.

The separate benchmarks module adds `BenchmarkMattnParity` for scalar calls,
1,000-row Go aggregation, 1,000-row Go collation sorting, time binding and
1,000-row declared date/boolean decoding.
`TestMattnParityTypesAndDSN` compares actual values and scan types against mattn.
`scripts/benchmark.sh` checks native engine identity before measuring. Its normal
core workloads still include modernc. Go callback dispatch has additional ABI
and reflection costs; functionality parity does not imply equal timing.
