//go:build (linux || darwin || windows) && (amd64 || arm64) && !sqlite_cgo

package ffi

import (
	"github.com/ebitengine/purego"
	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

// Integer-only hot paths use PureGo's documented SyscallN API to avoid
// reflect.MakeFunc's per-call allocations. These arguments are opaque native
// handles and integers only: no Go pointers, strings, floats or varargs.
// All supported targets have 64-bit uintptr; signed values preserve their bits.
// Floating point and pointer-returning functions retain typed RegisterFunc ABI
// conversion. Native column buffers are still copied by rows.Next.
func bindFast(a *API, h uintptr) {
	symbol := func(name string) uintptr { p, _ := loader.Symbol(h, name); return p }
	one := func(name string) func(uintptr) int32 {
		p := symbol(name)
		return func(arg uintptr) int32 { r, _, _ := purego.SyscallN(p, arg); return int32(r) }
	}
	two := func(name string) func(uintptr, int32) int32 {
		p := symbol(name)
		return func(arg uintptr, index int32) int32 {
			r, _, _ := purego.SyscallN(p, arg, uintptr(index))
			return int32(r)
		}
	}
	a.Step = one("sqlite3_step")
	a.Reset = one("sqlite3_reset")
	a.ClearBindings = one("sqlite3_clear_bindings")
	a.StmtReadonly = one("sqlite3_stmt_readonly")
	a.ColumnType = two("sqlite3_column_type")
	a.ColumnBytes = two("sqlite3_column_bytes")
	a.BindNull = two("sqlite3_bind_null")
	a.Autocommit = one("sqlite3_get_autocommit")
	bindInt := symbol("sqlite3_bind_int64")
	a.BindInt64 = func(stmt uintptr, index int32, value int64) int32 {
		r, _, _ := purego.SyscallN(bindInt, stmt, uintptr(index), uintptr(value))
		return int32(r)
	}
	columnInt := symbol("sqlite3_column_int64")
	a.ColumnInt64 = func(stmt uintptr, index int32) int64 {
		r, _, _ := purego.SyscallN(columnInt, stmt, uintptr(index))
		return int64(r)
	}
	rowid := symbol("sqlite3_last_insert_rowid")
	a.LastInsertRowid = func(db uintptr) int64 { r, _, _ := purego.SyscallN(rowid, db); return int64(r) }
	if p := symbol("sqlite3_changes64"); p != 0 {
		a.Changes64 = func(db uintptr) int64 { r, _, _ := purego.SyscallN(p, db); return int64(r) }
	}
}
