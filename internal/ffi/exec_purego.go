//go:build (linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package ffi

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

func bindExecScalar(a *API, h uintptr) {
	p, err := loader.Symbol(h, "purego_sqlite_exec_scalar_v1")
	if err != nil {
		return
	}
	a.scalarExecutor = func() func(uintptr, []uint64) int32 {
		// Conn.mu protects this private frame and the SQLite-allocated buffer.
		var args [4]uintptr
		return func(stmt uintptr, frame []uint64) int32 {
			args = [4]uintptr{stmt, uintptr(unsafe.Pointer(unsafe.SliceData(frame))), uintptr(len(frame)/2 - 1), 16}
			r, _, _ := purego.SyscallN(p, args[:]...)
			args[1] = 0
			return int32(r)
		}
	}
	a.ExecScalar = a.scalarExecutor()
}
