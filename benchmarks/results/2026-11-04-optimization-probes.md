# Standard-API optimization probes

These targeted probes used five 300ms samples per implementation on the same
Ryzen 5600G host, GOMAXPROCS=4 and the pinned SQLite library. They predate the
final CPU affinity/CFLAGS correction, so they are exploratory historical probes,
not additional samples in the final five-way table. Compare their matching
controls and allocation counts rather than mixing their absolute timings with
other runs.

## Metadata and argument discovery

[Before](2026-11-04-probe-metadata-before.txt) and
[after direct metadata conversion plus lazy declaration/parameter discovery](2026-11-04-probe-metadata-lazy-parameters.txt):

| Workload | Before PureGo ns/op | After PureGo ns/op | Before Go allocs/op | After Go allocs/op |
|---|---:|---:|---:|---:|
| SelectConstant | 3,858 | 2,988 | 26 | 17 |
| SingleRowSelect | 10,224 | 7,878 | 49 | 25 |
| IndexedLookup | 9,724 | 7,829 | 36 | 21 |
| JSONQuery | 5,956 | 4,535 | 33 | 19 |

Declared types are not needed by ordinary Scan, and positional arguments do not
need parameter names. Both remain available on demand, with schema reprepare
and named/positional mode-change regression tests.

## Prepared scalar cleanup and write metadata

[Before scalar cleanup](2026-11-04-probe-scalar-before.txt): 1000-row batch
1,207,059ns, matching mattn 776,312ns, modernc 1,027,689ns.

[After scalar cleanup](2026-11-04-probe-scalar-after.txt): 1,119,010ns,
matching mattn 793,945ns, modernc 1,012,767ns.

[After one-shot finalization and conservative DML hint](2026-11-04-probe-write-hint-after.txt):
1,063,361ns, matching mattn 767,615ns, modernc 993,823ns.

Scalar bindings own no native buffers. They can remain after a successful reset
because all slots must be validated and rebound before the next Step. Errors and
TEXT/BLOB payloads still clear all slots. One-shot callers always finalize.

Plain leading INSERT/UPDATE/DELETE/REPLACE statements are non-readonly according
to SQLite's public contract, including no-op DML. Other SQL still asks SQLite,
including CTEs, leading comments, PRAGMAs and EXPLAIN. No general readonly flag is
cached across automatic reprepare.

## Bounded SQL prepare scratch space

[Before](2026-11-04-probe-sql-buffer-before.txt) and
[after](2026-11-04-probe-sql-buffer-after.txt) show smaller Go allocation counts:
constant SELECT 17 to 15, ordinary single-row SELECT 25 to 24, indexed lookup 21
to 20 and JSON 19 to 18. Host slowdown affected the later probe's absolute times,
including its controls; those absolute times are not an optimization speedup
estimate.

SQLite retains its own SQL copy. The connection mutex protects up to 4 KiB of
reusable NUL-terminated input, large inputs are temporary, and Close releases
scratch storage. A regression test overwrites the input with other short, long
and rejected SQL, changes schema, then executes the original prepared statement.

## Rejected call-frame micro-optimization

[Before](2026-11-04-probe-frame-before.txt) and
[direct per-wrapper calls](2026-11-04-probe-frame-after.txt) produced scan medians
1,129,355ns and 1,138,934ns respectively. Matching mattn controls were 587,664ns
and 602,024ns. This did not demonstrate a meaningful improvement, so the extra
wrapper duplication was reverted.

## Remaining scan cost

A two-second CPU profile of the three-column scan, after the metadata changes,
attributed about 66% of samples cumulatively to PureGo's SyscallN stack. That
includes native SQLite execution, not just boundary overhead. About 14% of flat
samples were in syscall_SyscallN and 8% in SyscallN itself; marshalling and pool
work remain visible after removing reflection from this workload.

The stable fact is the API call count: roughly eight native calls per row for
step, three type reads, two integers, TEXT pointer and TEXT byte count. Copies,
type checks, floating-point ABI conversion and connection/cancellation lifetime
protection were preserved. No SQLite private structures or custom native exports
were used to bypass that public API.
