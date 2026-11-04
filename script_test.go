package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecScriptPragmasAndResult(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "PRAGMA busy_timeout=1234; PRAGMA trusted_schema=OFF")
	var busy, trusted int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA trusted_schema").Scan(&trusted); err != nil {
		t.Fatal(err)
	}
	if busy != 1234 || trusted != 0 {
		t.Fatalf("script pragmas: %d %d", busy, trusted)
	}
	result := exec(t, db, `CREATE TABLE data(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO data(value) VALUES('first'); INSERT INTO data(value) VALUES('second'),('third'); ; -- tail
 /* finished */`)
	id, err := result.LastInsertId()
	if err != nil || id != 3 {
		t.Fatalf("last insert: %d %v", id, err)
	}
	changes, err := result.RowsAffected()
	if err != nil || changes != 2 {
		t.Fatalf("last changes: %d %v", changes, err)
	}
	exec(t, db, "SELECT * FROM data; UPDATE data SET value='updated' WHERE id=1")
	var value string
	if err := db.QueryRow("SELECT value FROM data WHERE id=1").Scan(&value); err != nil || value != "updated" {
		t.Fatalf("rows not drained: %q %v", value, err)
	}
	integrity(t, db)
}

func TestExecScriptUsesSQLiteStatementBoundaries(t *testing.T) {
	db := testDB(t)
	exec(t, db, `-- a comment ;
 CREATE TABLE "source;data"(value TEXT);
 CREATE TABLE audit(value TEXT);
 CREATE TRIGGER capture AFTER INSERT ON "source;data" BEGIN
  INSERT INTO audit(value) VALUES('trigger;first');
  INSERT INTO audit(value) VALUES(new.value);
 END;
 /* another ; comment */ INSERT INTO "source;data" VALUES('quoted;🌍');
 INSERT INTO "source;data" VALUES('it''s;quoted');`)
	var joined string
	if err := db.QueryRow("SELECT group_concat(value,'|') FROM audit").Scan(&joined); err != nil || joined != "trigger;first|quoted;🌍|trigger;first|it's;quoted" {
		t.Fatalf("script boundaries: %q %v", joined, err)
	}
}

func TestExecScriptTailSurvivesSQLBufferReuse(t *testing.T) {
	db := testDB(t)
	exec(t, db, "CREATE TABLE data(value TEXT)")
	for _, size := range []int{16, 4096, 8192} {
		value := strings.Repeat(";🌍", size)
		exec(t, db, "INSERT INTO data VALUES('"+value+"'); ; /* skip ; */ ; UPDATE data SET value=value || 'tail' WHERE rowid=last_insert_rowid()")
		var got string
		if err := db.QueryRow("SELECT value FROM data WHERE rowid=last_insert_rowid()").Scan(&got); err != nil || got != value+"tail" {
			t.Fatalf("script tail at size %d: length %d, error %v", size, len(got), err)
		}
	}
}

func TestExecScriptStopsAtFirstError(t *testing.T) {
	db := testDB(t)
	exec(t, db, "CREATE TABLE data(value INTEGER UNIQUE)")
	if result, err := db.Exec("INSERT INTO data VALUES(1); INSERT INTO data VALUES(1); INSERT INTO data VALUES(2)"); result != nil || !errors.Is(err, ErrConstraint) {
		t.Fatalf("script constraint: %v %v", result, err)
	}
	var count, sum int
	if err := db.QueryRow("SELECT count(*),sum(value) FROM data").Scan(&count, &sum); err != nil || count != 1 || sum != 1 {
		t.Fatalf("script failure did not stop: %d %d %v", count, sum, err)
	}
	if _, err := db.Exec("INSERT INTO data VALUES(2); this is invalid SQL; INSERT INTO data VALUES(3)"); err == nil {
		t.Fatal("invalid later statement accepted")
	}
	if err := db.QueryRow("SELECT count(*) FROM data").Scan(&count); err != nil || count != 2 {
		t.Fatalf("script silently became atomic: %d %v", count, err)
	}
}

func TestExecScriptParametersAndSingleStatementAPIs(t *testing.T) {
	db := testDB(t)
	exec(t, db, "CREATE TABLE data(value INTEGER)")
	for _, script := range []string{"INSERT INTO data VALUES(?); INSERT INTO data VALUES(2)", "INSERT INTO data VALUES(1); INSERT INTO data VALUES(?)"} {
		if _, err := db.Exec(script, 3); !errors.Is(err, ErrMultipleStatements) {
			t.Fatalf("ambiguous script bindings accepted: %v", err)
		}
	}
	if _, err := db.Query("INSERT INTO data VALUES(1); SELECT 1"); !errors.Is(err, ErrMultipleStatements) {
		t.Fatalf("multi-statement query: %v", err)
	}
	if _, err := db.Prepare("INSERT INTO data VALUES(1); SELECT 1"); !errors.Is(err, ErrMultipleStatements) {
		t.Fatalf("multi-statement prepare: %v", err)
	}
	if _, err := db.Exec("INSERT INTO data VALUES(1); SELECT 2\x00"); err == nil {
		t.Fatal("NUL in script accepted")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM data").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected script changed database: %d %v", count, err)
	}
	exec(t, db, "INSERT INTO data VALUES(?); /* still one statement */", 4)
	if _, err := db.Exec("INSERT INTO data VALUES(5); INSERT INTO data VALUES(?); INSERT INTO data VALUES(6)"); err == nil {
		t.Fatal("missing script binding accepted")
	}
	if err := db.QueryRow("SELECT count(*) FROM data").Scan(&count); err != nil || count != 2 {
		t.Fatalf("missing binding continuation: %d %v", count, err)
	}
}

func TestExecScriptTransactions(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE data(value INTEGER UNIQUE)")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "INSERT INTO data VALUES(1); INSERT INTO data VALUES(2)")
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "BEGIN; INSERT INTO data VALUES(3); INSERT INTO data VALUES(4); COMMIT")
	if _, err := db.Exec("BEGIN; INSERT INTO data VALUES(5); INSERT INTO data VALUES(5); COMMIT"); !errors.Is(err, ErrConstraint) {
		t.Fatal(err)
	}
	// database/sql must discard a connection returned with an unfinished native
	// transaction; later borrowers cannot see or commit the failed script's work.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM data").Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed script leaked transaction: %d %v", count, err)
	}
}

func TestExecScriptCancellationAndConnectionReuse(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE data(value INTEGER)")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := db.ExecContext(ctx, `INSERT INTO data VALUES(1);
 WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n;
 INSERT INTO data VALUES(99)`)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("script cancellation: %v", err)
	}
	exec(t, db, "INSERT INTO data VALUES(2)")
	var count int
	if err := db.QueryRow("SELECT count(*) FROM data WHERE value=99").Scan(&count); err != nil || count != 0 {
		t.Fatalf("script ran after cancellation: %d %v", count, err)
	}
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Raw(func(value any) error {
		if c := value.(*Conn); len(c.stmts) != 0 || c.active != nil {
			t.Fatalf("script leaked native resources: %d %v", len(c.stmts), c.active)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
