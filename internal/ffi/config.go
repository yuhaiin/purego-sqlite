//go:build (linux || windows || (darwin && amd64)) && (amd64 || arm64)

package ffi

import "github.com/yuhaiin/purego-sqlite/internal/loader"

func extensionConfig(h uintptr) func(uintptr, int32) int32 {
	p, err := loader.Symbol(h, "sqlite3_db_config")
	if err != nil {
		return nil
	}
	// Only (sqlite3*, int, int, int*) with integer/pointer arguments.
	// On SysV amd64, AAPCS64, Win64 and Windows ARM64 these occupy the same
	// registers as fixed arguments. No floats, aggregates or generic varargs.
	// PureGo sets the SysV vector-register count to zero. Apple ARM64 is
	// deliberately excluded: its variadic arguments must be on the stack.
	var f func(uintptr, int32, int32, *int32) int32
	register(&f, p)
	return func(db uintptr, enabled int32) int32 { return f(db, 1005, enabled, nil) }
}
