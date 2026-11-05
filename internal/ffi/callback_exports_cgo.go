//go:build cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package ffi

/*
#include <stdint.h>
*/
import "C"

//export goSQLiteScalar
func goSQLiteScalar(token, ctx C.uintptr_t, argc C.int, argv C.uintptr_t) {
	if fn := callbackHandlers(uintptr(token)).Scalar; fn != nil {
		fn(uintptr(ctx), int32(argc), uintptr(argv))
	}
}

//export goSQLiteStep
func goSQLiteStep(token, ctx C.uintptr_t, argc C.int, argv C.uintptr_t) {
	if fn := callbackHandlers(uintptr(token)).Step; fn != nil {
		fn(uintptr(ctx), int32(argc), uintptr(argv))
	}
}

//export goSQLiteFinal
func goSQLiteFinal(token, ctx C.uintptr_t) {
	if fn := callbackHandlers(uintptr(token)).Final; fn != nil {
		fn(uintptr(ctx))
	}
}

//export goSQLiteCompare
func goSQLiteCompare(token C.uintptr_t, n1 C.int, p1 C.uintptr_t, n2 C.int, p2 C.uintptr_t) C.int {
	if fn := callbackHandlers(uintptr(token)).Compare; fn != nil {
		return C.int(fn(int32(n1), uintptr(p1), int32(n2), uintptr(p2)))
	}
	return 0
}

//export goSQLiteDestroy
func goSQLiteDestroy(token C.uintptr_t) { ReleaseCallbackToken(uintptr(token)) }

//export goSQLiteUpdate
func goSQLiteUpdate(token C.uintptr_t, op C.int, db, table C.uintptr_t, rowID C.int64_t) {
	if fn := callbackHandlers(uintptr(token)).Update; fn != nil {
		fn(int32(op), uintptr(db), uintptr(table), int64(rowID))
	}
}

//export goSQLiteCommit
func goSQLiteCommit(token C.uintptr_t) C.int {
	if fn := callbackHandlers(uintptr(token)).Commit; fn != nil {
		return C.int(fn())
	}
	return 0
}

//export goSQLiteRollback
func goSQLiteRollback(token C.uintptr_t) {
	if fn := callbackHandlers(uintptr(token)).Rollback; fn != nil {
		fn()
	}
}

//export goSQLiteAuthorize
func goSQLiteAuthorize(token C.uintptr_t, op C.int, a, b, db, origin C.uintptr_t) C.int {
	if fn := callbackHandlers(uintptr(token)).Authorize; fn != nil {
		return C.int(fn(int32(op), uintptr(a), uintptr(b), uintptr(db), uintptr(origin)))
	}
	return 1
}

//export goSQLiteTrace
func goSQLiteTrace(mask C.uint, token, p, x C.uintptr_t) {
	if fn := callbackHandlers(uintptr(token)).Trace; fn != nil {
		fn(uint32(mask), uintptr(p), uintptr(x))
	}
}
