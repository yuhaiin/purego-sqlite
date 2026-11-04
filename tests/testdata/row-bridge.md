# Optional row-reader fixture

`row.c` mirrors `bridge/row.c` in the companion purego-sqlite-embed repository.
It is compiled only by `go run ./internal/cmd/build-native --row-bridge`, never by a normal
driver build. Keep both copies identical when changing the versioned interface.

The ABI is `int purego_sqlite_read_row_v1(sqlite3_stmt*, void*, int32_t, uint32_t)`.
The output record is 24 bytes: uint64 bits at offset 0, uint64 native payload
address at 8, int32 length at 16, int32 SQLite storage class at 20, native byte
order. INTEGER bits preserve signed values; FLOAT bits preserve IEEE 754 double
values. Payloads are borrowed until step/reset/finalize; the driver copies them.
See the companion's `bridge/README.md` for the complete caller contract.
