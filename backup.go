package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// BackupTo copies main into filename using SQLite's online backup API. The
// destination is opened with this same native library, without init hooks. Its
// existing contents are replaced. Do not use the source database as destination.
// Other source connections may continue to run; this physical connection is
// reserved until completion. Busy/locked destinations return an SQLite error.
func (c *Conn) BackupTo(ctx context.Context, filename string) error {
	if filename == "" || strings.ContainsRune(filename, 0) {
		return errors.New("sqlite: backup destination must be a nonempty filename without NUL")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	connector := &Connector{dsn: filename, api: c.api, cfg: config{busyMS: c.cfg.busyMS, txMode: "DEFERRED"}}
	dc, err := connector.Connect(ctx)
	if err != nil {
		return err
	}
	dest := dc.(*Conn)
	defer dest.Close()
	return c.runLocked(ctx, func() (err error) {
		backup := c.api.BackupInit(dest.db, "main", c.db, "main")
		if backup == 0 {
			return dest.sqliteError(c.api.Errcode(dest.db))
		}
		defer func() { err = errors.Join(err, dest.sqliteError(c.api.BackupFinish(backup))) }()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			rc := c.api.BackupStep(backup, 128)
			if rc == 101 {
				return nil
			}
			if rc != 0 {
				return dest.sqliteError(rc)
			}
		}
	})
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
