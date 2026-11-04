//go:build cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package ffi

/*
#include <stdint.h>
typedef struct sqlite3_stmt sqlite3_stmt;
static int exec_scalar(uintptr_t fn, uintptr_t stmt, void *frame, int32_t count) {
 return ((int (*)(sqlite3_stmt*, void*, int32_t, uint32_t))fn)((sqlite3_stmt*)stmt, frame, count, 16);
}
*/
import "C"

import (
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

func bindExecScalar(a *API, h uintptr) {
	p, err := loader.Symbol(h, "purego_sqlite_exec_scalar_v1")
	if err != nil {
		return
	}
	a.ExecScalar = func(stmt uintptr, frame []uint64) int32 {
		return int32(C.exec_scalar(C.uintptr_t(p), C.uintptr_t(stmt), unsafe.Pointer(unsafe.SliceData(frame)), C.int32_t(len(frame)/2-1)))
	}
}
