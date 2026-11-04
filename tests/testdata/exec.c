/* Optional scalar execution bridge. Compile beside unmodified sqlite3.c.
 * ABI documentation: purego-sqlite-embed/bridge/README.md. */
#include "sqlite3.h"
#include <stdint.h>
#include <string.h>

typedef struct {
  uint64_t bits;
  uint64_t type;
} purego_sqlite_scalar_v1;

typedef struct {
  uint64_t last_id;
  uint64_t changes;
} purego_sqlite_result_v1;

_Static_assert(sizeof(purego_sqlite_scalar_v1) == 16, "scalar ABI size");
_Static_assert(sizeof(purego_sqlite_result_v1) == 16, "result ABI size");
_Static_assert(sizeof(sqlite3_int64) == 8, "scalar ABI integer");
_Static_assert(sizeof(double) == 8, "scalar ABI double");

SQLITE_API int purego_sqlite_exec_scalar_v1(sqlite3_stmt *stmt, void *frame,
                                          int32_t count, uint32_t stride) {
  if (!stmt || !frame || count < 0 || stride != 16 ||
      count != sqlite3_bind_parameter_count(stmt)) {
    return SQLITE_MISUSE;
  }
  purego_sqlite_result_v1 *result = (purego_sqlite_result_v1 *)frame;
  purego_sqlite_scalar_v1 *args = (purego_sqlite_scalar_v1 *)(result + 1);
  result->last_id = result->changes = 0;
  /* Validate every type before changing bindings or executing anything. */
  for (int32_t i = 0; i < count; i++) {
    if (args[i].type != SQLITE_INTEGER && args[i].type != SQLITE_FLOAT &&
        args[i].type != SQLITE_NULL) {
      return SQLITE_MISMATCH;
    }
  }
  int rc;
  for (int32_t i = 0; i < count; i++) {
    switch (args[i].type) {
    case SQLITE_INTEGER: {
      sqlite3_int64 value;
      memcpy(&value, &args[i].bits, sizeof(value));
      rc = sqlite3_bind_int64(stmt, i + 1, value);
      break;
    }
    case SQLITE_FLOAT: {
      double value;
      memcpy(&value, &args[i].bits, sizeof(value));
      rc = sqlite3_bind_double(stmt, i + 1, value);
      break;
    }
    default:
      rc = sqlite3_bind_null(stmt, i + 1);
    }
    if (rc != SQLITE_OK) return rc;
  }
  do { rc = sqlite3_step(stmt); } while (rc == SQLITE_ROW);
  /* On failure leave the error untouched. Go captures it before reset/clear. */
  if (rc != SQLITE_DONE) return rc;
  sqlite3 *db = sqlite3_db_handle(stmt);
  result->last_id = (uint64_t)sqlite3_last_insert_rowid(db);
  if (!sqlite3_stmt_readonly(stmt)) {
    result->changes = (uint64_t)sqlite3_changes64(db);
  }
  rc = sqlite3_reset(stmt);
  return rc == SQLITE_OK ? SQLITE_DONE : rc;
}
