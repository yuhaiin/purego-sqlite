//go:build (linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package ffi

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

func bindRow(a *API, h uintptr) {
	p, err := loader.Symbol(h, "purego_sqlite_read_row_v1")
	if err != nil {
		return
	}
	a.rowReader = func() func(uintptr, []RowValue) int32 {
		// ForConnection creates a private call frame, protected by Conn.mu.
		var args [4]uintptr
		return func(stmt uintptr, values []RowValue) int32 {
			// values borrows a SQLite allocation, never Go memory. This avoids
			// both pinning Go buffers and allocating a variadic frame per row.
			args = [4]uintptr{stmt, uintptr(unsafe.Pointer(unsafe.SliceData(values))), uintptr(len(values)), uintptr(rowValueSize)}
			r, _, _ := purego.SyscallN(p, args[:]...)
			args[1] = 0
			return int32(r)
		}
	}
	a.ReadRow = a.rowReader()
}
