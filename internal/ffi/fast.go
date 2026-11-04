//go:build ((linux && !android) || (darwin && !ios) || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package ffi

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

// Integer-only hot paths use PureGo's documented SyscallN API to avoid
// reflect.MakeFunc's per-call allocations. These arguments are opaque native
// handles and integers, plus synchronous byte buffers: no floats or varargs.
// All supported targets have 64-bit uintptr; signed values preserve their bits.
// Floating point retains typed RegisterFunc ABI conversion. Native column
// buffers are still copied by rows.Next before stepping/resetting the statement.
func bindFast(a *API, h uintptr) {
	// Resolve once per library, never per connection or execution.
	symbols := make(map[string]uintptr)
	for _, name := range []string{
		"sqlite3_step", "sqlite3_reset", "sqlite3_clear_bindings", "sqlite3_stmt_readonly",
		"sqlite3_column_type", "sqlite3_column_bytes", "sqlite3_bind_null", "sqlite3_get_autocommit",
		"sqlite3_finalize", "sqlite3_bind_parameter_count", "sqlite3_column_count",
		"sqlite3_column_text", "sqlite3_column_blob", "sqlite3_bind_text", "sqlite3_bind_blob",
		"sqlite3_bind_int64", "sqlite3_column_int64", "sqlite3_last_insert_rowid", "sqlite3_changes64",
		"sqlite3_prepare_v3",
		"sqlite3_column_name", "sqlite3_column_decltype", "sqlite3_bind_parameter_name",
	} {
		symbols[name], _ = loader.Symbol(h, name)
	}
	a.connection = func() *API {
		local := *a
		local.connection = nil
		bindConnectionCalls(&local, symbols)
		local.bindOperations()
		return &local
	}
}

func bindConnectionCalls(a *API, symbols map[string]uintptr) {
	// Conn.mu serializes these calls. Reusing scalar-only argument storage
	// avoids SyscallN's escaping variadic slice without another lock or pool.
	// Go buffer addresses never enter this frame. Interrupt uses a separate
	// typed binding and cannot overwrite a running Step's arguments.
	var args [3]uintptr
	callInts := func(fn uintptr, n int, x, y, z uintptr) uintptr {
		args[0], args[1], args[2] = x, y, z
		r, _, _ := purego.SyscallN(fn, args[:n]...)
		return r
	}
	symbol := func(name string) uintptr { return symbols[name] }
	one := func(name string) func(uintptr) int32 {
		p := symbol(name)
		return func(arg uintptr) int32 { return int32(callInts(p, 1, arg, 0, 0)) }
	}
	two := func(name string) func(uintptr, int32) int32 {
		p := symbol(name)
		return func(arg uintptr, index int32) int32 {
			return int32(callInts(p, 2, arg, uintptr(index), 0))
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
	a.Finalize = one("sqlite3_finalize")
	a.BindParameterCount = one("sqlite3_bind_parameter_count")
	a.ColumnCount = one("sqlite3_column_count")
	nativePointer := func(name string) func(uintptr, int32) *byte {
		p := symbol(name)
		return func(stmt uintptr, index int32) *byte {
			r := callInts(p, 2, stmt, uintptr(index), 0)
			// r is exclusively a native allocation, never a hidden Go pointer.
			// Reinterpret the pointer-sized return register just as PureGo's
			// RegisterFunc does for pointer results (func.go, reflect.Pointer).
			return *(**byte)(unsafe.Pointer(&r))
		}
	}
	a.ColumnText = nativePointer("sqlite3_column_text")
	a.ColumnBlob = nativePointer("sqlite3_column_blob")
	metadata := func(name string) func(uintptr, int32) string {
		pointer := nativePointer(name)
		return func(stmt uintptr, index int32) string {
			return copyCString(pointer(stmt, index))
		}
	}
	a.ColumnName = metadata("sqlite3_column_name")
	a.ColumnDecltype = metadata("sqlite3_column_decltype")
	a.BindParameterName = metadata("sqlite3_bind_parameter_name")
	bindBytes := func(name string) func(uintptr, int32, *byte, int32, uintptr) int32 {
		p := symbol(name)
		return func(stmt uintptr, index int32, data *byte, n int32, destructor uintptr) int32 {
			// Keep the conversion in SyscallN's argument expression: its
			// go:uintptrescapes directive keeps Go data stable across the call.
			// The driver always uses SQLITE_TRANSIENT, so SQLite copies now.
			r, _, _ := purego.SyscallN(p, stmt, uintptr(index), uintptr(unsafe.Pointer(data)), uintptr(n), destructor)
			runtime.KeepAlive(data)
			return int32(r)
		}
	}
	a.BindText = bindBytes("sqlite3_bind_text")
	a.BindBlob = bindBytes("sqlite3_bind_blob")
	prepare := symbol("sqlite3_prepare_v3")
	a.Prepare = func(db uintptr, sql *byte, n int32, flags uint32, stmt *uintptr, tail **byte) int32 {
		// Native outputs contain pointer-sized integers only. Reconstruct the
		// borrowed SQL tail from its original Go allocation, preserving provenance.
		var end uintptr
		r, _, _ := purego.SyscallN(prepare, db, uintptr(unsafe.Pointer(sql)), uintptr(n), uintptr(flags), uintptr(unsafe.Pointer(stmt)), uintptr(unsafe.Pointer(&end)))
		if end == 0 {
			*tail = nil
		} else {
			*tail = (*byte)(unsafe.Add(unsafe.Pointer(sql), end-uintptr(unsafe.Pointer(sql))))
		}
		runtime.KeepAlive(sql)
		runtime.KeepAlive(stmt)
		return int32(r)
	}
	bindInt := symbol("sqlite3_bind_int64")
	a.BindInt64 = func(stmt uintptr, index int32, value int64) int32 {
		return int32(callInts(bindInt, 3, stmt, uintptr(index), uintptr(value)))
	}
	columnInt := symbol("sqlite3_column_int64")
	a.ColumnInt64 = func(stmt uintptr, index int32) int64 {
		return int64(callInts(columnInt, 2, stmt, uintptr(index), 0))
	}
	rowid := symbol("sqlite3_last_insert_rowid")
	a.LastInsertRowid = func(db uintptr) int64 { return int64(callInts(rowid, 1, db, 0, 0)) }
	if p := symbol("sqlite3_changes64"); p != 0 {
		a.Changes64 = func(db uintptr) int64 { return int64(callInts(p, 1, db, 0, 0)) }
	}
}

// Metadata is native NUL-terminated text owned by the statement. Return a copy
// while Conn.mu still protects it, including after SQLite's automatic reprepare.
func copyCString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
