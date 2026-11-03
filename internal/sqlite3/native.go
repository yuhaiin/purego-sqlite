// Package sqlite3 contains the unmodified, checksum-pinned official SQLite engine.
//go:build cgo && !sqlite_purego && !sqlite_dynamic && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package sqlite3

/*
#cgo CFLAGS: -O2 -DSQLITE_THREADSAFE=1 -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_USE_URI=1
#cgo !windows CFLAGS: -fvisibility=hidden
#cgo linux LDFLAGS: -lm -lpthread -ldl
#cgo darwin freebsd openbsd netbsd dragonfly LDFLAGS: -lm -lpthread
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "sqlite3.h"
#include "symbols.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const Version = "3.54.1"

// Symbol returns a compiler-typed address of a function in the linked engine.
func Symbol(name string) (uintptr, error) {
	p := C.CString(name)
	defer C.free(unsafe.Pointer(p))
	address := uintptr(C.linked_symbol(p))
	if address == 0 {
		return 0, fmt.Errorf("sqlite: linked symbol %s is unavailable", name)
	}
	return address, nil
}
