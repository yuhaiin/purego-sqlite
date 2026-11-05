package benchmarks

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	mattn "github.com/mattn/go-sqlite3"
	sqlite "github.com/yuhaiin/purego-sqlite"
)

type paritySum struct{ value int64 }

func (a *paritySum) Step(value int64) { a.value += value }
func (a *paritySum) Done() int64      { return a.value }

func TestMattnParityTypesAndDSN(t *testing.T) {
	var options []sqlite.Option
	if library := os.Getenv("BENCH_SQLITE_LIBRARY"); library != "" {
		options = append(options, sqlite.WithLibrary(library))
	}
	options = append(options, sqlite.WithMattnTypes())
	dsn := "file:parity-validation?mode=memory&cache=private&_foreign_keys=on&_busy_timeout=17"
	connector, err := sqlite.NewConnector(dsn, options...)
	if err != nil {
		t.Fatal(err)
	}
	ours := sql.OpenDB(connector)
	defer ours.Close()
	reference, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reference.Close()
	var values [][]any
	for _, db := range []*sql.DB{ours, reference} {
		db.SetMaxOpenConns(1)
		for _, pragma := range []struct {
			name     string
			expected int
		}{{"foreign_keys", 1}, {"busy_timeout", 17}} {
			var value int
			if err := db.QueryRow("PRAGMA " + pragma.name).Scan(&value); err != nil || value != pragma.expected {
				t.Fatalf("%s=%d: %v", pragma.name, value, err)
			}
		}
		exec(t, db, "CREATE TABLE parity(ts TIMESTAMP,b BOOLEAN)")
		instant := time.Date(2026, 11, 5, 3, 4, 5, 123456789, time.UTC)
		exec(t, db, "INSERT INTO parity VALUES(?,?)", instant, true)
		exec(t, db, "INSERT INTO parity VALUES('invalid',-1),(0,0),(NULL,NULL)")
		rows, err := db.Query("SELECT ts,b FROM parity ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.ColumnTypes()
		if err != nil {
			t.Fatal(err)
		}
		if columns[0].ScanType() != reflect.TypeFor[sql.NullTime]() || columns[1].ScanType() != reflect.TypeFor[sql.NullBool]() {
			t.Fatal("scan types differ")
		}
		var result []any
		for rows.Next() {
			var timestamp, boolean any
			if err := rows.Scan(&timestamp, &boolean); err != nil {
				t.Fatal(err)
			}
			if value, ok := timestamp.(time.Time); ok {
				timestamp = value.Format(time.RFC3339Nano)
			}
			result = append(result, timestamp, boolean)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		values = append(values, result)
	}
	if !reflect.DeepEqual(values[0], values[1]) {
		t.Fatalf("type semantics differ: ours=%#v mattn=%#v", values[0], values[1])
	}
}

func installParityFunctions(b testing.TB, db *sql.DB, implementation string) {
	b.Helper()
	conn, err := db.Conn(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	err = conn.Raw(func(raw any) error {
		scalar := func(value int64) int64 { return value + 1 }
		aggregate := func() *paritySum { return &paritySum{} }
		switch c := raw.(type) {
		case *sqlite.Conn:
			if err := c.RegisterFunc("go_increment", scalar, true); err != nil {
				return err
			}
			if err := c.RegisterAggregator("go_sum", aggregate, true); err != nil {
				return err
			}
			return c.RegisterCollation("go_reverse", func(a, b string) int { return strings.Compare(b, a) })
		case *mattn.SQLiteConn:
			if err := c.RegisterFunc("go_increment", scalar, true); err != nil {
				return err
			}
			if err := c.RegisterAggregator("go_sum", aggregate, true); err != nil {
				return err
			}
			return c.RegisterCollation("go_reverse", func(a, b string) int { return strings.Compare(b, a) })
		default:
			return fmt.Errorf("unsupported native driver %T (%s)", raw, implementation)
		}
	})
	if err != nil {
		b.Fatal(err)
	}
}

// Both drivers use the same engine selected by benchmark.sh. Modernc remains in
// BenchmarkSQLite; this group compares their per-connection Go extension APIs.
func BenchmarkMattnParity(b *testing.B) {
	for _, workload := range []string{"Scalar", "Aggregate1000", "CollationSort1000", "TimeBind", "TypedRows1000"} {
		b.Run(workload, func(b *testing.B) {
			for _, implementation := range []string{backendName, "mattn"} {
				b.Run(implementation, func(b *testing.B) {
					db := setup(b, implementation, 1, sqlite.WithMattnTypes())
					if workload == "TypedRows1000" {
						exec(b, db, "CREATE TABLE dates(ts TIMESTAMP,b BOOLEAN); WITH RECURSIVE seq(v) AS (VALUES(1) UNION ALL SELECT v+1 FROM seq WHERE v<1000) INSERT INTO dates SELECT '2026-11-05 00:00:00',v%2 FROM seq")
					}
					installParityFunctions(b, db, implementation)
					query := map[string]string{"Scalar": "SELECT go_increment(?)", "Aggregate1000": "SELECT go_sum(value) FROM items", "CollationSort1000": "SELECT name FROM items ORDER BY name COLLATE go_reverse", "TimeBind": "SELECT ?", "TypedRows1000": "SELECT ts,b FROM dates"}[workload]
					stmt, err := db.Prepare(query)
					if err != nil {
						b.Fatal(err)
					}
					defer stmt.Close()
					instant := time.Date(2026, 11, 5, 0, 0, 0, 123456789, time.UTC)
					b.ReportAllocs()
					for b.Loop() {
						switch workload {
						case "Scalar", "Aggregate1000":
							var value int64
							var err error
							if workload == "Scalar" {
								err = stmt.QueryRow(41).Scan(&value)
								if value != 42 && err == nil {
									b.Fatal(value)
								}
							} else {
								err = stmt.QueryRow().Scan(&value)
								if value != 499500 && err == nil {
									b.Fatal(value)
								}
							}
							if err != nil {
								b.Fatal(err)
							}
						case "CollationSort1000":
							rows, err := stmt.Query()
							if err != nil {
								b.Fatal(err)
							}
							count := 0
							for rows.Next() {
								var name string
								if err := rows.Scan(&name); err != nil {
									b.Fatal(err)
								}
								count++
							}
							if err := rows.Err(); err != nil {
								b.Fatal(err)
							}
							rows.Close()
							if count != 1000 {
								b.Fatal(count)
							}
						case "TimeBind":
							var value string
							if err := stmt.QueryRow(instant).Scan(&value); err != nil {
								b.Fatal(err)
							}
						case "TypedRows1000":
							rows, err := stmt.Query()
							if err != nil {
								b.Fatal(err)
							}
							count := 0
							for rows.Next() {
								var timestamp time.Time
								var flag bool
								if err := rows.Scan(&timestamp, &flag); err != nil {
									b.Fatal(err)
								}
								count++
							}
							if err := rows.Err(); err != nil {
								b.Fatal(err)
							}
							rows.Close()
							if count != 1000 {
								b.Fatal(count)
							}
						}
					}
					throughput(b)
				})
			}
		})
	}
}
