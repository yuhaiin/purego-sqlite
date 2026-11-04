# Optional scalar executor fixture

`exec.c` mirrors `bridge/exec.c` in purego-sqlite-embed. Keep both copies identical.
It is compiled only by `go run ./internal/cmd/build-native --exec-bridge`.

The ABI is `int purego_sqlite_exec_scalar_v1(sqlite3_stmt*, void*, int32_t, uint32_t)`.
The native frame starts with a 16-byte output (uint64 last row ID, uint64 changes),
then one 16-byte input per parameter (uint64 bits, uint64 SQLite type). Byte order
is native on both 32-bit and 64-bit targets. Count must equal parameter count;
stride is 16. Supported types are INTEGER (1), FLOAT (2), NULL (5).

The helper validates all types, binds every parameter, drains SQLITE_ROW,
captures results and resets on success. It returns SQLITE_DONE after successful
reset. On failure, the caller must capture the connection error before resetting
and clearing bindings; it must never retry execution. Only native scalar memory
is passed, with no Go pointers, strings, blobs or callbacks. The connection lock
and cancellation lifecycle remain the driver's responsibility.
