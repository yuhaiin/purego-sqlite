package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestCloseFinalizeDuringCancellation(t *testing.T) {
	for range 30 {
		dc, err := testConnector(t, ":memory:").Connect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		c := dc.(*Conn)
		ds, err := c.Prepare(expensiveSQL)
		if err != nil {
			t.Fatal(err)
		}
		s := ds.(*stmt)
		ctx, cancel := context.WithCancel(t.Context())
		entered := make(chan struct{})
		completed := make(chan error, 1)
		go func() {
			c.mu.Lock()
			err := c.runLocked(ctx, func() error { close(entered); _, err := s.execLocked(nil); return err })
			c.mu.Unlock()
			completed <- err
		}()
		<-entered
		var wg sync.WaitGroup
		wg.Go(func() { cancel() })
		wg.Go(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
		if err := <-completed; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func TestCrashRecovery(t *testing.T) {
	// An isolated process exits without closing its live write transaction. The
	// parent reopens the real database and verifies both committed state and integrity.
	if path := os.Getenv("SQLITE_CRASH_FIXTURE"); path != "" {
		var opts []Option
		if os.Getenv("SQLITE_CRASH_WAL") == "1" {
			opts = append(opts, WithWAL())
		}
		db := testDBAt(t, path, opts...)
		exec(t, db, "CREATE TABLE t(x)")
		exec(t, db, "INSERT INTO t VALUES(1)")
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		exec(t, tx, "INSERT INTO t VALUES(2)")
		// Force enough dirty data to exercise real pager/journal or WAL recovery.
		exec(t, tx, "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000) INSERT INTO t SELECT zeroblob(4096) FROM n")
		os.Exit(0)
	}
	for _, wal := range []string{"0", "1"} {
		t.Run("wal="+wal, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "recovery.db")
			command := osexec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCrashRecovery$")
			command.Env = append(os.Environ(), "SQLITE_CRASH_FIXTURE="+path, "SQLITE_CRASH_WAL="+wal)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v\n%s", err, output)
			}
			db := testDBAt(t, path)
			var n int
			if err := db.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			integrity(t, db)
		})
	}
}

func testDBAt(t *testing.T, path string, opts ...Option) *sql.DB {
	t.Helper()
	db := sql.OpenDB(testConnector(t, path, opts...))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}
