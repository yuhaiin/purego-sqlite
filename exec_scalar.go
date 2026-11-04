package sqlite

import (
	"database/sql/driver"
	"math"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

// Complete positional scalar binds can execute in one optional native call.
// Named parameters, payloads and Query retain the standard binding path.
func (s *stmt) scalarArgs(args []driver.NamedValue) bool {
	if len(args) != s.paramCount || s.boundBuffers {
		return false
	}
	for i, arg := range args {
		if arg.Name != "" || arg.Ordinal != i+1 {
			return false
		}
		switch arg.Value.(type) {
		case nil, int64, float64, bool:
		default:
			return false
		}
	}
	return true
}

func (s *stmt) execScalarLocked(args []driver.NamedValue) (driver.Result, error) {
	c, a := s.c, s.c.api
	// One 16-byte result followed by one 16-byte record per parameter. Only
	// scalar bits enter this native allocation; no Go pointer is hidden in it.
	if len(args) > math.MaxInt32/16-1 {
		return nil, c.sqliteError(18) // SQLITE_TOOBIG.
	}
	words := 2 * (len(args) + 1)
	if len(c.execFrame) < words {
		p := a.Malloc(int32(words * 8))
		if p == nil {
			return nil, c.sqliteError(7) // SQLITE_NOMEM; no binds have run.
		}
		if c.execStorage != nil {
			a.Free(c.execStorage)
		}
		c.execStorage = p
		c.execFrame = unsafe.Slice((*uint64)(unsafe.Pointer(p)), words)
	}
	frame := c.execFrame[:words]
	for i, arg := range args {
		var bits, kind uint64
		switch value := arg.Value.(type) {
		case nil:
			kind = 5
		case int64:
			bits, kind = uint64(value), 1
		case float64:
			bits, kind = math.Float64bits(value), 2
		case bool:
			kind = 1
			if value {
				bits = 1
			}
		}
		frame[2+i*2], frame[3+i*2] = bits, kind
	}
	if rc := a.ExecScalar(s.ptr, frame); rc != ffi.Done {
		// Reset/clear can replace SQLite's detailed error. Capture it first;
		// never retry a bridge call that may already have changed the database.
		err := c.sqliteError(rc)
		a.ResetClear(s.ptr)
		s.boundBuffers = false
		return nil, err
	}
	return result{lastID: int64(frame[0]), changes: int64(frame[1])}, nil
}
