package sqlite

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

type rows struct {
	s                               *stmt
	ctx                             context.Context
	own, closed, pending, exhausted bool
}

func (r *rows) Columns() []string {
	r.s.c.mu.Lock()
	defer r.s.c.mu.Unlock()
	return append([]string(nil), r.s.columns...)
}
func (r *rows) ColumnTypeDatabaseTypeName(index int) string {
	r.s.c.mu.Lock()
	defer r.s.c.mu.Unlock()
	name := strings.ToUpper(r.s.decltypes[index])
	name, _, _ = strings.Cut(name, "(")
	return strings.TrimSpace(name)
}

// SQLite allows NULL for most declarations and expression columns. Unknown
// nullability is more accurate than inferring constraints from a type name.
func (*rows) ColumnTypeNullable(int) (bool, bool) { return true, false }
func (r *rows) Close() error                      { r.s.c.mu.Lock(); defer r.s.c.mu.Unlock(); return r.closeLocked() }
func (r *rows) closeLocked() error {
	if r.closed {
		return nil
	}
	r.closed = true
	s := r.s
	if s.c.active == r {
		s.c.active = nil
	}
	if s.ptr == 0 {
		return nil
	}
	if r.own {
		return s.closeLocked()
	}
	rc := s.c.api.Reset(s.ptr)
	s.c.api.ClearBindings(s.ptr)
	return s.c.sqliteError(rc)
}
func (r *rows) Next(dest []driver.Value) error {
	c := r.s.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.closed {
		return io.EOF
	}
	err := c.runLocked(r.ctx, func() error {
		s := r.s
		a := c.api
		if s.ptr == 0 {
			return ErrClosed
		}
		rc := int32(ffi.Row)
		if r.exhausted {
			return io.EOF
		}
		if r.pending {
			r.pending = false
		} else {
			rc = a.Step(s.ptr)
		}
		if rc == ffi.Done {
			return io.EOF
		}
		if rc != ffi.Row {
			return c.sqliteError(rc)
		}

		if len(dest) != len(s.columns) {
			return io.ErrShortBuffer
		}
		for i := range dest {
			col := int32(i)
			switch a.ColumnType(s.ptr, col) {
			case 1:
				dest[i] = a.ColumnInt64(s.ptr, col)
			case 2:
				dest[i] = a.ColumnDouble(s.ptr, col)
			case 3:
				p := a.ColumnText(s.ptr, col)
				n := int(a.ColumnBytes(s.ptr, col))
				if p == nil {
					return c.sqliteError(7)
				} // SQLITE_NOMEM; NULL handled separately.
				dest[i] = string(unsafe.Slice(p, n)) // Copy before step/reset/finalize.
			case 4:
				p := a.ColumnBlob(s.ptr, col)
				n := int(a.ColumnBytes(s.ptr, col))
				if p == nil && n != 0 {
					return c.sqliteError(7)
				}
				data := make([]byte, n)
				copy(data, unsafe.Slice(p, n))
				dest[i] = data
			case 5:
				dest[i] = nil
			}
		}
		return nil
	})
	if err != nil {
		_ = r.closeLocked()
	}
	return err
}
