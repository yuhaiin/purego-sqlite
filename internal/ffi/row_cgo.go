//go:build cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package ffi

/*
#include <stdint.h>
typedef struct sqlite3_stmt sqlite3_stmt;
static int read_row(uintptr_t fn, uintptr_t stmt, void *values, int32_t count, uint32_t stride) {
 return ((int (*)(sqlite3_stmt*, void*, int32_t, uint32_t))fn)((sqlite3_stmt*)stmt, values, count, stride);
}
*/
import "C"

import (
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

func bindRow(a *API, h uintptr) {
	p, err := loader.Symbol(h, "purego_sqlite_read_row_v1")
	if err != nil {
		return
	}
	a.ReadRow = func(stmt uintptr, values []RowValue) int32 {
		return int32(C.read_row(C.uintptr_t(p), C.uintptr_t(stmt), unsafe.Pointer(unsafe.SliceData(values)), C.int32_t(len(values)), C.uint32_t(rowValueSize)))
	}
}
