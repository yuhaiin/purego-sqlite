package sqlite

import (
	"database/sql"
	"errors"
)

type transaction struct {
	c    *Conn
	done bool
}

func (t *transaction) Commit() error   { return t.finish(true) }
func (t *transaction) Rollback() error { return t.finish(false) }
func (t *transaction) finish(commit bool) error {
	c := t.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.done {
		return sql.ErrTxDone
	}
	if err := c.available(); err != nil {
		return err
	}
	t.done = true
	c.tx = nil
	if c.api.Autocommit(c.db) != 0 {
		if commit {
			return errors.New("sqlite: transaction was already rolled back by SQLite")
		}
		return nil
	}
	command := "ROLLBACK"
	if commit {
		command = "COMMIT"
	}
	err := c.execSQLLocked(command)
	// database/sql cannot retry a failed Commit. Ensure a busy/failed COMMIT
	// never returns an open transaction to the pool.
	if err != nil && c.api.Autocommit(c.db) == 0 {
		_ = c.execSQLLocked("ROLLBACK")
	}
	return err
}
