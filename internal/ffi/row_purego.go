//go:build (linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package ffi

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

func bindRow(a *API, h uintptr) {
	p, err := loader.Symbol(h, "purego_sqlite_read_row_v1")
	if err != nil {
		return
	}
	a.ReadRow = func(stmt uintptr, values []RowValue) int32 {
		// Keep the Go address in SyscallN's uintptrescapes argument expression.
		r, _, _ := purego.SyscallN(p, stmt, uintptr(unsafe.Pointer(unsafe.SliceData(values))), uintptr(len(values)), uintptr(rowValueSize))
		runtime.KeepAlive(values)
		return int32(r)
	}
}
