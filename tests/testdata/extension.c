/* A locally compiled fixture, loaded through SQLite's official extension ABI. */
#include "sqlite3ext.h"
SQLITE_EXTENSION_INIT1

static void twice(sqlite3_context *ctx, int argc, sqlite3_value **argv) {
    (void)argc;
    sqlite3_result_int64(ctx, sqlite3_value_int64(argv[0]) * 2);
}

#ifdef _WIN32
__declspec(dllexport)
#endif
int sqlite3_extension_init(sqlite3 *db, char **error, const sqlite3_api_routines *api) {
    (void)error;
    SQLITE_EXTENSION_INIT2(api);
    return sqlite3_create_function(db, "test_twice", 1, SQLITE_UTF8 | SQLITE_DETERMINISTIC,
                                  0, twice, 0, 0);
}
