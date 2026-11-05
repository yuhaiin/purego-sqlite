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
