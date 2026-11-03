package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestBackup(t *testing.T) {
	db := testDB(t, WithWAL())
	exec(t, db, "CREATE TABLE t(id INTEGER PRIMARY KEY,b BLOB)")
	exec(t, db, "INSERT INTO t(b) VALUES(zeroblob(1000000))")
	source, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	path := filepath.Join(t.TempDir(), "backup.db")
	if err := Backup(t.Context(), source, path); err != nil {
		t.Fatal(err)
	}
	copyDB := sql.OpenDB(testConnector(t, path))
	defer copyDB.Close()
	var n int
	if err := copyDB.QueryRow("SELECT length(b) FROM t").Scan(&n); err != nil || n != 1000000 {
		t.Fatal(n, err)
	}
	integrity(t, copyDB)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Backup(ctx, source, filepath.Join(t.TempDir(), "canceled.db")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}
