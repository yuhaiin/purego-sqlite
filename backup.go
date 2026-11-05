package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// BackupTo copies main into filename using SQLite's online backup API. The
// destination is opened with this same native library, without init hooks. Its
// existing contents are replaced. Do not use the source database as destination.
// Other source connections may continue to run; this physical connection is
// reserved until completion. Busy/locked destinations return an SQLite error.
func (c *Conn) BackupTo(ctx context.Context, filename string) error {
	if c.inCallback.Load() != 0 {
		return ErrCallbackReentry
	}
	if filename == "" || strings.ContainsRune(filename, 0) {
		return errors.New("sqlite: backup destination must be a nonempty filename without NUL")
	}
	c.mu.Lock()
	if err := c.available(); err != nil {
		c.mu.Unlock()
		return err
	}
	connector := &Connector{dsn: filename, api: c.api, cfg: config{busyMS: c.cfg.busyMS, txMode: "DEFERRED"}}
	c.mu.Unlock()
	dc, err := connector.Connect(ctx)
	if err != nil {
		return err
	}
	dest := dc.(*Conn)
	defer dest.Close()
	return dest.BackupFrom(ctx, c, BackupOptions{})
}
func Backup(ctx context.Context, source *sql.Conn, filename string) error {
	return source.Raw(func(dc any) error {
		c, ok := dc.(*Conn)
		if !ok {
			return errors.New("sqlite: physical connection belongs to another driver")
		}
		return c.BackupTo(ctx, filename)
	})
}

// BackupOptions selects schemas and the number of pages copied per step.
// Zero values select main and 128 pages. Progress runs synchronously while both
// connections are reserved. It must not use either connection or their pools;
// returning an error aborts an incomplete copy and rolls its destination back.
// SQLite may restart a live copy, so Remaining need not decrease monotonically.
type BackupOptions struct {
	SourceSchema, DestinationSchema string
	PagesPerStep                    int
	Progress                        func(BackupProgress) error
}

type BackupProgress struct {
	Remaining, PageCount int
	Done                 bool
}

// BackupFrom replaces the selected destination schema on c with source's data.
// This supports file/memory copies and restore without exposing native handles.
// Both connections must use the same native SQLite engine. End transactions and
// close destination statements first. Busy/locked errors are returned to callers.
// Cancellation is checked between steps; a native busy wait uses WithBusyTimeout.
func (c *Conn) BackupFrom(ctx context.Context, source *Conn, options BackupOptions) error {
	if source == nil || source == c {
		return errors.New("sqlite: backup requires two distinct connections")
	}
	if c.inCallback.Load() != 0 || source.inCallback.Load() != 0 {
		return ErrCallbackReentry
	}
	destSchema, err := schemaName(options.DestinationSchema)
	if err != nil {
		return err
	}
	srcSchema, err := schemaName(options.SourceSchema)
	if err != nil {
		return err
	}
	pages := options.PagesPerStep
	if pages < 0 || int64(pages) > 2147483647 {
		return errors.New("sqlite: backup pages per step must be a nonnegative C int")
	}
	if pages == 0 {
		pages = 128
	}
	first, second := c, source
	if first.id > second.id {
		first, second = second, first
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if err := source.available(); err != nil {
		return err
	}
	if !c.api.SameEngine(source.api) {
		return errors.New("sqlite: backup connections use different native engines")
	}
	if c.tx != nil || c.api.Autocommit(c.db) == 0 || source.tx != nil || source.api.Autocommit(source.db) == 0 {
		return errors.New("sqlite: backup requires connections without active transactions")
	}
	if sameDatabaseFile(c.api.DBFilename(c.db, destSchema), source.api.DBFilename(source.db, srcSchema)) {
		return errors.New("sqlite: cannot back up a database into the same file")
	}
	c.clearStatementCacheLocked()
	if len(c.stmts) != 0 {
		return errors.New("sqlite: close destination prepared statements before backup")
	}
	// Only the source may be passed to other SQLite APIs during a backup.
	// In particular, do not interrupt/read errors on the destination until finish.
	return source.runLocked(ctx, func() (err error) {
		backup := c.api.BackupInit(c.db, destSchema, source.db, srcSchema)
		if backup == 0 {
			return c.sqliteError(c.api.Errcode(c.db))
		}
		defer func() {
			rc := c.api.BackupFinish(backup)
			err = errors.Join(err, c.sqliteError(rc))
			err = c.takeCallbackError(err)
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			rc := c.api.BackupStep(backup, int32(pages))
			if rc != 0 && rc != 101 {
				return &Error{Code: int(rc & 255), ExtendedCode: int(rc), Message: c.api.Errstr(rc)}
			}
			if options.Progress != nil {
				progress := BackupProgress{Remaining: int(c.api.BackupRemaining(backup)), PageCount: int(c.api.BackupPageCount(backup)), Done: rc == 101}
				if err := c.backupProgress(source, options.Progress, progress); err != nil {
					return err
				}
			}
			if rc == 101 {
				return nil
			}
		}
	})
}

func sameDatabaseFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}

func (c *Conn) backupProgress(source *Conn, fn func(BackupProgress) error, progress BackupProgress) (err error) {
	c.inCallback.Add(1)
	source.inCallback.Add(1)
	defer c.inCallback.Add(-1)
	defer source.inCallback.Add(-1)
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("sqlite: backup progress panic: %v", value)
		}
	}()
	return fn(progress)
}
