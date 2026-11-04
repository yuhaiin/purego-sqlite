/* Optional driver bridge. SQLite's amalgamation is compiled unchanged.
 * ABI documentation: bridge/README.md. */
#include "sqlite3.h"
#include <stddef.h>
#include <stdint.h>
#include <string.h>

typedef struct {
  uint64_t bits;
  uint64_t data;
  int32_t size;
  int32_t type;
} purego_sqlite_value_v1;

_Static_assert(sizeof(purego_sqlite_value_v1) == 24, "row ABI size");
_Static_assert(offsetof(purego_sqlite_value_v1, data) == 8, "row ABI pointer");
_Static_assert(offsetof(purego_sqlite_value_v1, size) == 16, "row ABI length");
_Static_assert(offsetof(purego_sqlite_value_v1, type) == 20, "row ABI type");
_Static_assert(sizeof(double) == 8, "row ABI double");
_Static_assert(sizeof(uintptr_t) <= 8, "row ABI address width");

SQLITE_API int purego_sqlite_read_row_v1(sqlite3_stmt *stmt, void *out,
                                       int32_t count, uint32_t stride) {
  if (!stmt || count < 0 || stride != sizeof(purego_sqlite_value_v1) ||
      (count && !out) || count != sqlite3_column_count(stmt)) {
    return SQLITE_MISUSE;
  }
  sqlite3 *db = sqlite3_db_handle(stmt);
  purego_sqlite_value_v1 *cols = (purego_sqlite_value_v1 *)out;
  for (int32_t i = 0; i < count; i++) {
    purego_sqlite_value_v1 *col = &cols[i];
    col->bits = 0;
    col->data = 0;
    col->size = 0;
    col->type = sqlite3_column_type(stmt, i);
    const void *p;
    switch (col->type) {
    case SQLITE_INTEGER:
      col->bits = (uint64_t)sqlite3_column_int64(stmt, i);
      break;
    case SQLITE_FLOAT: {
      double value = sqlite3_column_double(stmt, i);
      memcpy(&col->bits, &value, sizeof(value));
      break;
    }
    case SQLITE_TEXT:
      p = sqlite3_column_text(stmt, i);
      if (!p) return SQLITE_NOMEM;
      col->size = sqlite3_column_bytes(stmt, i);
      col->data = (uint64_t)(uintptr_t)p;
      break;
    case SQLITE_BLOB:
      p = sqlite3_column_blob(stmt, i);
      if (!p && sqlite3_errcode(db) == SQLITE_NOMEM) return SQLITE_NOMEM;
      col->size = sqlite3_column_bytes(stmt, i);
      col->data = (uint64_t)(uintptr_t)p;
      break;
    case SQLITE_NULL:
      break;
    default:
      return SQLITE_MISMATCH;
    }
  }
  return SQLITE_OK;
}
