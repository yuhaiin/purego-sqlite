package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

// Conn owns a native sqlite3*. Use it only inside WithInit or sql.Conn.Raw.
// Its mutex serializes native access except sqlite3_interrupt. Each native call
// stops and joins its cancellation callback before releasing that mutex.
type Conn struct {
	mu         sync.Mutex
	api        *ffi.API
	cfg        config
	db         uintptr
	stmts      map[*stmt]struct{}
	sqlbuf     []byte // Conn.mu protects a bounded prepare buffer; SQLite copies SQL.
	rowStorage *byte  // Native output scratch, owned until Close, protected by mu.
	rowValues  []ffi.RowValue
	active     *rows
	tx         *transaction
}

func (c *Conn) sqliteError(rc int32) error {
	if rc == ffi.OK {
		return nil
	}
	message := c.api.Errstr(rc)
	extended := rc
	if c.db != 0 {
		message = c.api.Errmsg(c.db)
		extended = c.api.ExtendedErrcode(c.db)
	}
	if extended&255 != rc&255 {
		extended = rc
		// Allocation/bridge failures need not update this connection's error.
		message = c.api.Errstr(rc)
	}
	return &Error{Code: int(rc & 255), ExtendedCode: int(extended), Message: message}
}

// runLocked must hold mu throughout, including stopping/joining AfterFunc.
// The callback never needs mu, so it can interrupt a blocked sqlite3_step.
func (c *Conn) runLocked(ctx context.Context, fn func() error) error {
	if c.db == 0 {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Done() == nil {
		return fn()
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		// interrupt is a no-op before step begins. Repeat until the operation
		// returns, covering cancellation between the context check and native call.
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			c.api.Interrupt(c.db)
			select {
			case <-finished:
				return
			case <-ticker.C:
			}
		}
	})
	err := fn()
	close(finished)
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (c *Conn) available() error {
	if c.db == 0 {
		return ErrClosed
	}
	if c.active != nil {
		return ErrActiveRows
	}
	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == 0 {
		return nil
	}
	for s := range c.stmts {
		_ = s.closeLocked()
	}
	rc := c.api.Close(c.db)
	if rc != ffi.OK {
		return c.sqliteError(rc)
	}
	c.db = 0
	c.sqlbuf = nil
	if c.rowStorage != nil {
		c.api.Free(c.rowStorage)
		c.rowStorage = nil
		c.rowValues = nil
	}
	return nil
}
func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *Conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return nil, err
	}
	var s *stmt
	err := c.runLocked(ctx, func() error { var err error; s, err = c.prepareLocked(query); return err })
	if err != nil {
		if s != nil {
			_ = s.closeLocked()
		}
		return nil, err
	}
	return s, nil
}
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return nil, err
	}
	var result driver.Result
	err := c.runLocked(ctx, func() error {
		s, err := c.prepareLocked(query)
		if err != nil {
			return err
		}
		defer s.closeLocked()
		result, err = s.execLocked(args, false)
		return err
	})
	return result, err
}
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return nil, err
	}
	var s *stmt
	var r *rows
	err := c.runLocked(ctx, func() error {
		var err error
		s, err = c.prepareLocked(query)
		if err != nil {
			return err
		}
		r, err = s.queryLocked(ctx, args, true)
		return err
	})
	if err != nil {
		if s != nil {
			_ = s.closeLocked()
		}
		return nil, err
	}
	return r, nil
}
func (c *Conn) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	return c.runLocked(ctx, func() error { return nil })
}
func (c *Conn) IsValid() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.db != 0 && c.active == nil && c.tx == nil && c.api.Autocommit(c.db) != 0
}
func (c *Conn) ResetSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.db == 0 || c.active != nil || c.tx != nil || c.api.Autocommit(c.db) == 0 {
		return driver.ErrBadConn
	}
	return nil
}
func (c *Conn) Begin() (driver.Tx, error) { return c.BeginTx(context.Background(), driver.TxOptions{}) }
func (c *Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// Isolation levels other than Default are not silently mapped to SQLite locks.
	if opts.Isolation != 0 {
		return nil, fmt.Errorf("%w: transaction isolation %d", ErrUnsupported, opts.Isolation)
	}
	if opts.ReadOnly {
		return nil, fmt.Errorf("%w: per-transaction read-only; use WithReadOnly", ErrUnsupported)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return nil, err
	}
	if c.tx != nil || c.api.Autocommit(c.db) == 0 {
		return nil, errors.New("sqlite: transaction already active")
	}
	err := c.runLocked(ctx, func() error { return c.execSQLLocked("BEGIN " + c.cfg.txMode) })
	if err != nil {
		// BEGIN may have succeeded immediately before cancellation.
		if c.api.Autocommit(c.db) == 0 {
			_ = c.execSQLLocked("ROLLBACK")
		}
		return nil, err
	}
	tx := &transaction{c: c}
	c.tx = tx
	return tx, nil
}
func (c *Conn) execSQLLocked(query string) error {
	s, err := c.prepareLocked(query)
	if err != nil {
		return err
	}
	defer s.closeLocked()
	_, err = s.execLocked(nil, false)
	return err
}
