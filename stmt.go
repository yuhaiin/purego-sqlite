package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

// stmt owns sqlite3_stmt*. The parent connection mutex protects its lifecycle.
type stmt struct {
	c            *Conn
	ptr          uintptr
	paramCount   int
	boundBuffers bool
	knownWrite   bool
	names        []string
	columns      []string
	decltypes    []string
}

func (c *Conn) prepareLocked(query string) (*stmt, error) {
	if strings.ContainsRune(query, 0) || len(query) >= math.MaxInt32 {
		return nil, errors.New("sqlite: SQL contains NUL or exceeds C int length")
	}
	var sql []byte
	if n := len(query) + 1; n <= 4096 {
		if cap(c.sqlbuf) < n {
			c.sqlbuf = make([]byte, max(256, n))
		}
		sql = c.sqlbuf[:n]
		copy(sql, query)
		sql[len(query)] = 0
	} else {
		// Do not retain unusually large SQL in an idle pooled connection.
		sql = append([]byte(query), 0)
	}
	var ptr uintptr
	var tail *byte
	rc := c.api.Prepare(c.db, &sql[0], int32(len(sql)), 0, &ptr, &tail)
	if rc != ffi.OK {
		return nil, c.sqliteError(rc)
	}
	// SQLite owns ptr; tail borrows sql and must be consumed before KeepAlive.
	if ptr == 0 {
		return nil, errors.New("sqlite: SQL contains no statement")
	}
	offset := int(uintptr(unsafe.Pointer(tail)) - uintptr(unsafe.Pointer(&sql[0])))
	runtime.KeepAlive(sql)
	// Only whitespace, semicolons and comments may follow. Parsing this lexical
	// tail avoids executing or preparing a second statement with schema effects.
	if offset < 0 || offset > len(query) || !emptyTail(query[offset:]) {
		c.api.Finalize(ptr)
		return nil, ErrMultipleStatements
	}
	s := &stmt{c: c, ptr: ptr, paramCount: int(c.api.BindParameterCount(ptr)), knownWrite: hasDMLPrefix(query)}
	c.stmts[s] = struct{}{}
	return s, nil
}

// SQLite documents INSERT/UPDATE/DELETE as non-readonly even for no-op writes.
// REPLACE is an INSERT alias. Only recognize an unquoted leading DML keyword;
// CTEs, comments, PRAGMAs and EXPLAIN still use sqlite3_stmt_readonly at execution.
// Keep general readonly checks native, including after automatic reprepare.
func hasDMLPrefix(query string) bool {
	query = strings.TrimLeft(query, " \t\r\n\f\v")
	n := 0
	for n < len(query) && (query[n] >= 'a' && query[n] <= 'z' || query[n] >= 'A' && query[n] <= 'Z') {
		n++
	}
	if n < len(query) && (query[n] == '_' || query[n] == '$' || query[n] >= '0' && query[n] <= '9' || query[n] >= 0x80) {
		return false
	}
	word := query[:n]
	return strings.EqualFold(word, "INSERT") || strings.EqualFold(word, "UPDATE") || strings.EqualFold(word, "DELETE") || strings.EqualFold(word, "REPLACE")
}
func emptyTail(s string) bool {
	for len(s) > 0 {
		switch s[0] {
		case ' ', '\t', '\r', '\n', '\f', ';':
			s = s[1:]
		case '-':
			if !strings.HasPrefix(s, "--") {
				return false
			}
			_, rest, found := strings.Cut(s, "\n")
			if !found {
				return true
			}
			s = rest
		case '/':
			if !strings.HasPrefix(s, "/*") {
				return false
			}
			_, rest, found := strings.Cut(s[2:], "*/")
			if !found {
				return true
			}
			s = rest
		default:
			return false
		}
	}
	return true
}
func (s *stmt) refreshColumns() {
	n := int(s.c.api.ColumnCount(s.ptr))
	s.columns = make([]string, n)
	// Most callers only Scan values. Fetch declaration metadata on demand.
	s.decltypes = nil
	for i := range n {
		s.columns[i] = s.c.api.ColumnName(s.ptr, int32(i))
	}
}
func (s *stmt) NumInput() int { return -1 } // bindLocked validates names and positional counts.
func (s *stmt) Close() error  { s.c.mu.Lock(); defer s.c.mu.Unlock(); return s.closeLocked() }
func (s *stmt) closeLocked() error {
	if s.ptr == 0 {
		return nil
	}
	if r := s.c.active; r != nil && r.s == s {
		r.closed = true
		s.c.active = nil
	}
	rc := s.c.api.Finalize(s.ptr)
	s.ptr = 0
	delete(s.c.stmts, s)
	return s.c.sqliteError(rc)
}
func values(args []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return named
}
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), values(args))
}
func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), values(args))
}
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	c := s.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return nil, err
	}
	if s.ptr == 0 {
		return nil, ErrClosed
	}
	var result driver.Result
	err := c.runLocked(ctx, func() error { var err error; result, err = s.execLocked(args, true); return err })
	return result, err
}
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	c := s.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return nil, err
	}
	if s.ptr == 0 {
		return nil, ErrClosed
	}
	var r *rows
	err := c.runLocked(ctx, func() error { var err error; r, err = s.queryLocked(ctx, args, false); return err })
	if err != nil {
		if r != nil {
			_ = r.closeLocked()
		}
		return nil, err
	}
	return r, nil
}

func (s *stmt) bindLocked(args []driver.NamedValue) error {
	// Every slot must be rebound and validated before Step. Payloads and failed
	// executions clear bindings; scalar-only successes may retain scalar values.
	positional := len(args) == s.paramCount
	for i, v := range args {
		if v.Name != "" || v.Ordinal != i+1 {
			positional = false
			break
		}
	}
	if positional {
		for i, v := range args {
			if err := s.bindValue(int32(i+1), v.Value); err != nil {
				return err
			}
		}
		return nil
	}
	// Parameter names are immutable SQL metadata, only needed for named
	// arguments or detailed validation errors. Positional execution skips them.
	if s.names == nil {
		s.names = make([]string, s.paramCount)
		for i := range s.names {
			s.names[i] = s.c.api.BindParameterName(s.ptr, int32(i+1))
		}
	}
	bound := make([]bool, s.paramCount)
	for _, v := range args {
		found := false
		for i, name := range s.names {
			match := v.Name == "" && i+1 == v.Ordinal
			if v.Name != "" {
				match = len(name) > 1 && (name[0] == ':' || name[0] == '@' || name[0] == '$') && name[1:] == v.Name
			}
			if !match {
				continue
			}
			if bound[i] {
				return fmt.Errorf("sqlite: parameter %d supplied twice", i+1)
			}
			if err := s.bindValue(int32(i+1), v.Value); err != nil {
				return err
			}
			bound[i] = true
			found = true
		}
		if !found {
			return fmt.Errorf("sqlite: unknown parameter name %q or ordinal %d", v.Name, v.Ordinal)
		}
	}
	for i, ok := range bound {
		if !ok {
			return fmt.Errorf("sqlite: missing parameter %d (%s)", i+1, s.names[i])
		}
	}
	return nil
}
func (s *stmt) bindValue(index int32, value any) error {
	a := s.c.api
	var rc int32
	switch v := value.(type) {
	case nil:
		rc = a.BindNull(s.ptr, index)
	case int64:
		rc = a.BindInt64(s.ptr, index, v)
	case float64:
		rc = a.BindDouble(s.ptr, index, v)
	case bool:
		var n int64
		if v {
			n = 1
		}
		rc = a.BindInt64(s.ptr, index, n)
	case string:
		s.boundBuffers = true
		if len(v) > math.MaxInt32 {
			return errors.New("sqlite: TEXT exceeds C int length")
		}
		p := unsafe.StringData(v)
		if len(v) == 0 {
			p = new(byte)
		} // NULL must remain distinct from empty TEXT.
		rc = a.BindText(s.ptr, index, p, int32(len(v)), ffi.Transient)
		runtime.KeepAlive(v)
	case []byte:
		if v == nil {
			rc = a.BindNull(s.ptr, index)
			break
		}
		s.boundBuffers = true
		if len(v) > math.MaxInt32 {
			return errors.New("sqlite: BLOB exceeds C int length")
		}
		p := unsafe.SliceData(v)
		if len(v) == 0 {
			p = new(byte)
		}
		rc = a.BindBlob(s.ptr, index, p, int32(len(v)), ffi.Transient)
		runtime.KeepAlive(v)
	default:
		return fmt.Errorf("sqlite: unsupported parameter type %T; encode times explicitly as TEXT or INTEGER", value)
	}
	return s.c.sqliteError(rc)
}
func (s *stmt) execLocked(args []driver.NamedValue, reuse bool) (driver.Result, error) {
	complete := false
	if reuse {
		defer func() {
			// Scalar values own no native payload. A complete validated rebind is
			// mandatory next time; failed binds/steps always clear all slots.
			if complete && !s.boundBuffers {
				s.c.api.Reset(s.ptr)
			} else {
				s.c.api.ResetClear(s.ptr)
				s.boundBuffers = false
			}
		}()
	} // One-shot callers finalize on every path, releasing bindings as well.
	if err := s.bindLocked(args); err != nil {
		return nil, err
	}
	rc, lastID, changes := s.c.api.Exec(s.ptr, s.c.db, s.knownWrite)
	if rc != ffi.Done {
		return nil, s.c.sqliteError(rc)
	}
	complete = true
	return result{lastID: lastID, changes: changes}, nil
}
func (s *stmt) queryLocked(ctx context.Context, args []driver.NamedValue, own bool) (*rows, error) {
	if err := s.bindLocked(args); err != nil {
		s.c.api.ClearBindings(s.ptr)
		s.boundBuffers = false
		return nil, err
	}
	// Step once before returning metadata: automatic schema reprepare can change
	// SELECT * columns. No borrowed column data escapes; active rows reserve c.
	rc := s.c.api.Step(s.ptr)
	if rc != ffi.Row && rc != ffi.Done {
		err := s.c.sqliteError(rc)
		s.c.api.ResetClear(s.ptr)
		s.boundBuffers = false
		return nil, err
	}
	s.refreshColumns()
	r := &rows{s: s, ctx: ctx, own: own, pending: rc == ffi.Row, exhausted: rc == ffi.Done}
	s.c.active = r
	return r, nil
}

type result struct{ lastID, changes int64 }

func (r result) LastInsertId() (int64, error) { return r.lastID, nil }
func (r result) RowsAffected() (int64, error) { return r.changes, nil }
