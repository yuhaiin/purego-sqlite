package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDSNAppliedToEveryConnectionAndOptionsOverride(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "dsn.db")
	connector := testConnector(t, filename+"?_foreign_keys=on&_busy_timeout=17&_txlock=immediate&_synchronous=normal&_cache_size=-99", WithBusyTimeout(23*time.Millisecond))
	db := sql.OpenDB(connector)
	defer db.Close()
	var connections []*sql.Conn
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	for range 3 {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		for _, expected := range []struct {
			pragma string
			value  int
		}{{"foreign_keys", 1}, {"busy_timeout", 23}, {"synchronous", 1}, {"cache_size", -99}} {
			var value int
			if err := conn.QueryRowContext(t.Context(), "PRAGMA "+expected.pragma).Scan(&value); err != nil || value != expected.value {
				t.Fatalf("%s=%d, want %d: %v", expected.pragma, value, expected.value, err)
			}
		}
		var sequence int
		var schema, actual string
		if err := conn.QueryRowContext(t.Context(), "PRAGMA database_list").Scan(&sequence, &schema, &actual); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(actual, "?") || filepath.Base(actual) != "dsn.db" {
			t.Fatalf("wrong filename %q", actual)
		}
	}
}

func TestDSNValidationAndNativeURI(t *testing.T) {
	for _, query := range []string{"_fk=invalid", "_busy_timeout=-1", "_busy_timeout=2147483648", "_journal=WAL;DROP TABLE x", "_txlock=unsafe", "_loc=No/Such_Zone", "_fk=1&_foreign_keys=0", "_fk=1&_fk=1", "_cache_size=NaN"} {
		if _, err := NewConnector("file:test.db?" + query); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	for _, query := range []string{"_mutex=no", "_auth=1", "_writable_schema=on"} {
		if _, err := NewConnector("file:test.db?" + query); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%s: %v", query, err)
		}
	}
	cfg := config{}
	got, err := parseDSN("file:foo%3Fbar?mode=ro&_fk=1&vfs=custom&extra=one%26two#fragment", &cfg)
	if err != nil || got != "file:foo%3Fbar?extra=one%26two&mode=ro&vfs=custom#fragment" {
		t.Fatalf("URI: %q %v", got, err)
	}
	got, err = parseDSN("file:db#fragment?_fk=invalid", &config{})
	if err != nil || got != "file:db#fragment?_fk=invalid" {
		t.Fatalf("fragment: %q %v", got, err)
	}
	db := sql.OpenDB(testConnector(t, "file:shared-compat?mode=memory&cache=shared&_fk=on"))
	defer db.Close()
	exec(t, db, "CREATE TABLE parent(id PRIMARY KEY); CREATE TABLE child(pid REFERENCES parent(id))")
	if _, err := db.Exec("INSERT INTO child VALUES (99)"); !errors.Is(err, ErrConstraint) {
		t.Fatalf("foreign keys not enforced: %v", err)
	}
}

func TestTimeAndBooleanCompatibility(t *testing.T) {
	location := time.FixedZone("offset", 90*60)
	for _, compatible := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "mattn"}[compatible], func(t *testing.T) {
			var options []Option
			if compatible {
				options = []Option{WithMattnTypes(), WithTimeLocation(location)}
			}
			db := testDB(t, options...)
			exec(t, db, "CREATE TABLE data(ts TIMESTAMP, b BOOLEAN, text VARCHAR(50))")
			instant := time.Date(2026, 11, 5, 1, 2, 3, 123456789, time.FixedZone("stored", -5*3600))
			exec(t, db, "INSERT INTO data VALUES(?,?,?)", instant, true, "text")
			rows, err := db.Query("SELECT ts,b,text FROM data")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			columns, err := rows.ColumnTypes()
			if err != nil {
				t.Fatal(err)
			}
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			var timestamp, boolean any
			var text string
			if err := rows.Scan(&timestamp, &boolean, &text); err != nil {
				t.Fatal(err)
			}
			if compatible {
				parsed, ok := timestamp.(time.Time)
				if !ok || !parsed.Equal(instant) || parsed.Location() != location || boolean != true {
					t.Fatalf("converted values %v %T %v", timestamp, timestamp, boolean)
				}
				if columns[0].ScanType() != reflect.TypeFor[sql.NullTime]() || columns[1].ScanType() != reflect.TypeFor[sql.NullBool]() || columns[2].DatabaseTypeName() != "VARCHAR(50)" {
					t.Fatal("compatibility metadata")
				}
			} else {
				if _, ok := timestamp.(string); !ok || boolean != int64(1) {
					t.Fatalf("raw storage changed: %T %T", timestamp, boolean)
				}
				if columns[1].ScanType() != reflect.TypeFor[sql.NullInt64]() || columns[2].DatabaseTypeName() != "VARCHAR" {
					t.Fatal("raw metadata")
				}
			}
		})
	}
	db := testDB(t, WithMattnTypes())
	exec(t, db, "CREATE TABLE dates(d DATETIME, b BOOLEAN); INSERT INTO dates VALUES (0,-1),(1700000000000,1),('invalid',0),(NULL,NULL)")
	rows, err := db.Query("SELECT d,b FROM dates ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantTimes := []any{time.Unix(0, 0).UTC(), time.UnixMilli(1700000000000).UTC(), time.Time{}, nil}
	wantBools := []any{false, true, false, nil}
	for i := range 4 {
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		var date, flag any
		if err := rows.Scan(&date, &flag); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(date, wantTimes[i]) || flag != wantBools[i] {
			t.Fatalf("row %d: %v %v", i, date, flag)
		}
	}
}

func TestCompatibleTypesRefreshAfterSchemaChange(t *testing.T) {
	db := testDB(t, WithMattnTypes())
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE evolving(v INTEGER); INSERT INTO evolving VALUES(7)")
	statement, err := db.Prepare("SELECT * FROM evolving")
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close()
	rows, err := statement.Query()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rows.ColumnTypes(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	exec(t, db, "ALTER TABLE evolving ADD COLUMN ts TIMESTAMP; UPDATE evolving SET ts='2026-11-05'")
	rows, err = statement.Query()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.ColumnTypes()
	if err != nil || len(columns) != 2 || columns[1].ScanType() != reflect.TypeFor[sql.NullTime]() {
		t.Fatalf("metadata after reprepare: %v %v", columns, err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var value int
	var timestamp time.Time
	if err := rows.Scan(&value, &timestamp); err != nil || value != 7 || timestamp.Year() != 2026 {
		t.Fatal(value, timestamp, err)
	}
}
