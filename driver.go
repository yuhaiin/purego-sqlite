// Package sqlite is a database/sql driver for the official native SQLite engine.
// CGO builds use the linked official SQLite engine by default; CGO-disabled
// builds use PureGo to load a native SQLite shared library.
// It registers the driver name "sqlite3purego".
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
	"github.com/yuhaiin/purego-sqlite/internal/native"
)

const DriverName = "sqlite3purego"

type Driver struct{}

func init() { sql.Register(DriverName, &Driver{}) }
func (d *Driver) Open(dsn string) (driver.Conn, error) {
	c, err := NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return c.Connect(context.Background())
}
func (*Driver) OpenConnector(dsn string) (driver.Connector, error) { return NewConnector(dsn) }

type Connector struct {
	dsn string
	cfg config
	api *ffi.API
}

func NewConnector(dsn string, options ...Option) (*Connector, error) {
	if strings.ContainsRune(dsn, 0) {
		return nil, errors.New("sqlite: filename contains NUL")
	}
	cfg := config{busyMS: 5000, txMode: "DEFERRED"}
	var err error
	dsn, err = parseDSN(dsn, &cfg)
	if err != nil {
		return nil, err
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("sqlite: nil option")
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}
	path := cfg.library
	if path == "" && cfg.bundled {
		var cleanup func()
		var err error
		path, cleanup, err = native.Path()
		if err != nil {
			return nil, err
		}
		defer cleanup()
	}
	a, err := ffi.Load(path)
	if err != nil {
		return nil, err
	}
	if cfg.extensions && (a.ConfigExtension == nil || a.LoadExtension == nil || a.EnableLoadExtension == nil) {
		return nil, errors.Join(ErrUnsupported, errors.New("secure extension loading is unavailable on this library/platform"))
	}
	return &Connector{dsn: dsn, cfg: cfg, api: a}, nil
}
func (*Connector) Driver() driver.Driver { return &Driver{} }
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	flags := int32(0x2 | 0x4 | 0x40 | 0x10000) // READWRITE|CREATE|URI|FULLMUTEX
	if c.cfg.readOnly {
		flags = 0x1 | 0x40 | 0x10000
	}
	conn := &Conn{id: nextConnectionID.Add(1), api: c.api.ForConnection(), cfg: c.cfg, stmts: make(map[*stmt]struct{})}
	rc := c.api.Open(c.dsn, &conn.db, flags, 0)
	if rc != ffi.OK {
		err := conn.sqliteError(rc)
		if conn.db != 0 {
			c.api.Close(conn.db)
		}
		return nil, err
	}
	fail := func(err error) (driver.Conn, error) { _ = conn.Close(); return nil, err }
	if rc = c.api.ExtendedResultCodes(conn.db, 1); rc != ffi.OK {
		return fail(conn.sqliteError(rc))
	}
	if rc = c.api.BusyTimeout(conn.db, c.cfg.busyMS); rc != ffi.OK {
		return fail(conn.sqliteError(rc))
	}
	// Disable both SQL and C extension loading first, even on nonstandard builds.
	if c.api.EnableLoadExtension != nil {
		if rc = c.api.EnableLoadExtension(conn.db, 0); rc != ffi.OK {
			return fail(conn.sqliteError(rc))
		}
	}
	if c.cfg.wal {
		rows, err := conn.QueryContext(ctx, "PRAGMA journal_mode=WAL", nil)
		if err != nil {
			return fail(err)
		}
		values := make([]driver.Value, 1)
		err = rows.Next(values)
		closeErr := rows.Close()
		if err != nil {
			return fail(err)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		if values[0] != "wal" {
			return fail(errors.New("sqlite: WAL unavailable for this database"))
		}
	}
	if c.cfg.journal != "" && !c.cfg.wal {
		if _, err := conn.ExecContext(ctx, "PRAGMA journal_mode="+c.cfg.journal, nil); err != nil {
			return fail(err)
		}
	}
	for _, pragma := range c.cfg.pragmas {
		if _, err := conn.ExecContext(ctx, pragma, nil); err != nil {
			return fail(err)
		}
	}
	if c.cfg.init != nil {
		if err := c.cfg.init(ctx, conn); err != nil {
			return fail(err)
		}
		conn.mu.Lock()
		conn.clearStatementCacheLocked()
		leaked := len(conn.stmts) != 0 || conn.db == 0 || c.api.Autocommit(conn.db) == 0
		conn.mu.Unlock()
		if leaked {
			return fail(errors.New("sqlite: init hook left open statements/transaction or closed the connection"))
		}
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return conn, nil
}
