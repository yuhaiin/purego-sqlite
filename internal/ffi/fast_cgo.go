//go:build cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package ffi

/*
#include <stdint.h>
typedef struct sqlite3 sqlite3;
typedef struct sqlite3_stmt sqlite3_stmt;
typedef long long sqlite3_int64;
typedef struct {
 uintptr_t step, reset, clear, readonly, changes, changes64, rowid;
} exec_calls;
typedef struct {
 int rc;
 sqlite3_int64 last_id, changes;
} exec_result;

// All symbols come from one retained library. No Go pointer is stored in C.
static exec_result exec_statement(const exec_calls *f, uintptr_t stmt, uintptr_t db) {
 exec_result r = {0, 0, 0};
 do { r.rc = ((int (*)(sqlite3_stmt*))f->step)((sqlite3_stmt*)stmt); } while (r.rc == 100);
 if (r.rc == 101) {
  // sqlite3_changes retains prior DML counts after SELECT. Preserve driver semantics.
  if (!((int (*)(sqlite3_stmt*))f->readonly)((sqlite3_stmt*)stmt)) {
   r.changes = f->changes64
    ? ((sqlite3_int64 (*)(sqlite3*))f->changes64)((sqlite3*)db)
    : ((int (*)(sqlite3*))f->changes)((sqlite3*)db);
  }
  r.last_id = ((sqlite3_int64 (*)(sqlite3*))f->rowid)((sqlite3*)db);
 }
 return r;
}
static int reset_clear(const exec_calls *f, uintptr_t stmt) {
 int rc = ((int (*)(sqlite3_stmt*))f->reset)((sqlite3_stmt*)stmt);
 ((int (*)(sqlite3_stmt*))f->clear)((sqlite3_stmt*)stmt);
 return rc;
}
*/
import "C"

import "github.com/yuhaiin/purego-sqlite/internal/loader"

func bindFast(a *API, h uintptr) {
	symbol := func(name string) C.uintptr_t { p, _ := loader.Symbol(h, name); return C.uintptr_t(p) }
	// Immutable scalar-only table; safe to borrow during concurrent CGO calls.
	f := C.exec_calls{
		step: symbol("sqlite3_step"), reset: symbol("sqlite3_reset"),
		clear: symbol("sqlite3_clear_bindings"), readonly: symbol("sqlite3_stmt_readonly"),
		changes: symbol("sqlite3_changes"), changes64: symbol("sqlite3_changes64"),
		rowid: symbol("sqlite3_last_insert_rowid"),
	}
	a.Exec = func(stmt, db uintptr, _ bool) (int32, int64, int64) {
		r := C.exec_statement(&f, C.uintptr_t(stmt), C.uintptr_t(db))
		return int32(r.rc), int64(r.last_id), int64(r.changes)
	}
	a.ResetClear = func(stmt uintptr) int32 { return int32(C.reset_clear(&f, C.uintptr_t(stmt))) }
}
