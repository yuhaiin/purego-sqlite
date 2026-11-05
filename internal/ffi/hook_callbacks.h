/* Compiler-typed SQLite hooks. Only integer tokens cross into Go. */
#include <stdint.h>
extern void goSQLiteUpdate(uintptr_t, int, uintptr_t, uintptr_t, int64_t);
extern int goSQLiteCommit(uintptr_t);
extern void goSQLiteRollback(uintptr_t);
extern int goSQLiteAuthorize(uintptr_t, int, uintptr_t, uintptr_t, uintptr_t, uintptr_t);
extern void goSQLiteTrace(unsigned int, uintptr_t, uintptr_t, uintptr_t);
static void update_callback(void *token, int op, const char *db, const char *table, long long rowid) {
 goSQLiteUpdate((uintptr_t)token, op, (uintptr_t)db, (uintptr_t)table, (int64_t)rowid);
}
static int commit_callback(void *token) { return goSQLiteCommit((uintptr_t)token); }
static void rollback_callback(void *token) { goSQLiteRollback((uintptr_t)token); }
static int authorize_callback(void *token, int op, const char *a, const char *b, const char *db, const char *origin) {
 return goSQLiteAuthorize((uintptr_t)token, op, (uintptr_t)a, (uintptr_t)b, (uintptr_t)db, (uintptr_t)origin);
}
static int trace_callback(unsigned int mask, void *token, void *p, void *x) {
 goSQLiteTrace(mask, (uintptr_t)token, (uintptr_t)p, (uintptr_t)x); return 0;
}
static uintptr_t update_callback_address(void) { return (uintptr_t)&update_callback; }
static uintptr_t commit_callback_address(void) { return (uintptr_t)&commit_callback; }
static uintptr_t rollback_callback_address(void) { return (uintptr_t)&rollback_callback; }
static uintptr_t authorize_callback_address(void) { return (uintptr_t)&authorize_callback; }
static uintptr_t trace_callback_address(void) { return (uintptr_t)&trace_callback; }
