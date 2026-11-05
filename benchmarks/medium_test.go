package benchmarks

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	mattn "github.com/mattn/go-sqlite3"
	sqlite "github.com/yuhaiin/purego-sqlite"
)

// Cache configuration is applied to every physical connection on both native
// drivers. modernc participates in cache-off SQL workloads; no equivalent
// transparent-cache configuration is assumed for it.
func mediumDB(b testing.TB, implementation string, cache int) *sql.DB {
	b.Helper()
	path := filepath.Join(b.TempDir(), "medium.db")
	if implementation == "modernc" {
		return open(b, implementation, path, 1)
	}
	dsn := "file:" + filepath.ToSlash(path) + fmt.Sprintf("?_synchronous=NORMAL&_cache_size=-2000&_busy_timeout=5000&_stmt_cache_size=%d", cache)
	var db *sql.DB
	var err error
	if implementation == "mattn" {
		db, err = sql.Open("sqlite3", dsn)
	} else {
		var options []sqlite.Option
		if library := os.Getenv("BENCH_SQLITE_LIBRARY"); library != "" {
			options = append(options, sqlite.WithLibrary(library))
		}
		connector, e := sqlite.NewConnector(dsn, options...)
		if e != nil {
			b.Fatal(e)
		}
		db = sql.OpenDB(connector)
	}
	if err != nil {
		b.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		b.Fatal(err)
	}
	return db
}

func TestMattnParityMedium(t *testing.T) {
	for _, implementation := range []string{backendName, "mattn"} {
		t.Run(implementation, func(t *testing.T) {
			db := mediumDB(t, implementation, 4)
			defer db.Close()
			exec(t, db, "CREATE TABLE medium(id INTEGER PRIMARY KEY,value); INSERT INTO medium VALUES(1,'original')")
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			err = conn.Raw(func(raw any) error {
				var image []byte
				var serialize func(string) ([]byte, error)
				var deserialize func([]byte, string) error
				switch c := raw.(type) {
				case *sqlite.Conn:
					serialize, deserialize = c.Serialize, c.Deserialize
				case *mattn.SQLiteConn:
					serialize, deserialize = c.Serialize, c.Deserialize
				}
				image, err = serialize("main")
				if err != nil {
					return err
				}
				return deserialize(image, "main")
			})
			if err != nil {
				t.Fatal(err)
			}
			var value string
			if err := conn.QueryRowContext(t.Context(), "SELECT value FROM medium WHERE id=?", 1).Scan(&value); err != nil || value != "original" {
				t.Fatal(value, err)
			}
		})
	}
}

func BenchmarkMattnMedium(b *testing.B) {
	for _, workload := range []string{"RepeatedQuery", "RepeatedExec", "CacheMisses"} {
		b.Run(workload, func(b *testing.B) {
			for _, cache := range []int{0, 16} {
				b.Run(fmt.Sprintf("Cache%d", cache), func(b *testing.B) {
					for _, implementation := range drivers {
						if cache != 0 && implementation == "modernc" {
							continue
						}
						b.Run(implementation, func(b *testing.B) {
							db := mediumDB(b, implementation, cache)
							defer db.Close()
							exec(b, db, "CREATE TABLE medium(id INTEGER PRIMARY KEY,value INTEGER); INSERT INTO medium VALUES(1,0)")
							tx, err := db.Begin()
							if err != nil {
								b.Fatal(err)
							}
							defer tx.Rollback()
							queries := make([]string, 32)
							for i := range queries {
								queries[i] = fmt.Sprintf("SELECT value FROM medium WHERE id=? /* query %d */", i)
							}
							i := 0
							b.ReportAllocs()
							for b.Loop() {
								if workload == "RepeatedExec" {
									if _, err := tx.Exec("UPDATE medium SET value=? WHERE id=1", i); err != nil {
										b.Fatal(err)
									}
								} else {
									query := "SELECT value FROM medium WHERE id=?"
									if workload == "CacheMisses" {
										query = queries[i%len(queries)]
									}
									var value int64
									if err := tx.QueryRow(query, 1).Scan(&value); err != nil || value != 0 {
										b.Fatal(value, err)
									}
								}
								i++
							}
							throughput(b)
						})
					}
				})
			}
		})
	}
	for _, workload := range []string{"Serialize1MiB", "Deserialize1MiB", "Backup1MiB", "CommitHook", "Authorizer", "Trace"} {
		b.Run(workload, func(b *testing.B) {
			for _, implementation := range []string{backendName, "mattn"} {
				b.Run(implementation, func(b *testing.B) { benchmarkMediumNative(b, implementation, workload) })
			}
		})
	}
}

func benchmarkMediumNative(b *testing.B, implementation, workload string) {
	b.Helper()
	db := mediumDB(b, implementation, 0)
	defer db.Close()
	exec(b, db, "CREATE TABLE medium(id INTEGER PRIMARY KEY,value); INSERT INTO medium VALUES(1,zeroblob(1048576))")
	conn, err := db.Conn(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()
	if workload == "Serialize1MiB" || workload == "Deserialize1MiB" {
		err = conn.Raw(func(raw any) error {
			var serialize func(string) ([]byte, error)
			var deserialize func([]byte, string) error
			switch c := raw.(type) {
			case *sqlite.Conn:
				serialize, deserialize = c.Serialize, c.Deserialize
			case *mattn.SQLiteConn:
				serialize, deserialize = c.Serialize, c.Deserialize
			}
			image, err := serialize("main")
			if err != nil {
				return err
			}
			b.SetBytes(int64(len(image)))
			b.ReportAllocs()
			for b.Loop() {
				if workload == "Serialize1MiB" {
					output, err := serialize("main")
					if err != nil {
						return err
					}
					if len(output) != len(image) {
						return fmt.Errorf("wrong image length")
					}
				} else {
					if err := deserialize(image, "main"); err != nil {
						return err
					}
				}
			}
			throughput(b)
			return nil
		})
	} else if workload == "Backup1MiB" {
		destDB := mediumDB(b, implementation, 0)
		defer destDB.Close()
		dest, err := destDB.Conn(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		defer dest.Close()
		err = conn.Raw(func(source any) error {
			return dest.Raw(func(destination any) error {
				b.SetBytes(1048576)
				b.ReportAllocs()
				for b.Loop() {
					switch c := destination.(type) {
					case *sqlite.Conn:
						if err := c.BackupFrom(ctx, source.(*sqlite.Conn), sqlite.BackupOptions{}); err != nil {
							return err
						}
					case *mattn.SQLiteConn:
						backup, err := c.Backup("main", source.(*mattn.SQLiteConn), "main")
						if err != nil {
							return err
						}
						for {
							done, err := backup.Step(128)
							if err != nil {
								backup.Finish()
								return err
							}
							if done {
								break
							}
						}
						if err := backup.Finish(); err != nil {
							return err
						}
					}
				}
				throughput(b)
				return nil
			})
		})
	} else {
		calls := 0
		err = conn.Raw(func(raw any) error {
			switch c := raw.(type) {
			case *sqlite.Conn:
				switch workload {
				case "CommitHook":
					return c.RegisterCommitHook(func() int { calls++; return 0 })
				case "Authorizer":
					return c.RegisterAuthorizer(func(sqlite.Authorization) sqlite.AuthResult { calls++; return sqlite.AuthAllow })
				case "Trace":
					return c.SetTrace(&sqlite.TraceConfig{Mask: sqlite.TraceStatement | sqlite.TraceProfile, Callback: func(sqlite.TraceEvent) { calls++ }})
				}
			case *mattn.SQLiteConn:
				switch workload {
				case "CommitHook":
					c.RegisterCommitHook(func() int { calls++; return 0 })
				case "Authorizer":
					c.RegisterAuthorizer(func(int, string, string, string) int { calls++; return 0 })
				case "Trace":
					return c.SetTrace(&mattn.TraceConfig{EventMask: mattn.TraceStmt | mattn.TraceProfile, Callback: func(mattn.TraceInfo) int { calls++; return 0 }})
				}
			}
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if workload == "CommitHook" {
				_, err = conn.ExecContext(ctx, "UPDATE medium SET value=1 WHERE id=1")
			} else {
				var value int
				err = conn.QueryRowContext(ctx, "SELECT id FROM medium WHERE id=1").Scan(&value)
				if err == nil && value != 1 {
					b.Fatal(value)
				}
			}
			if err != nil {
				b.Fatal(err)
			}
		}
		throughput(b)
		if calls == 0 {
			b.Fatal("callback was not invoked")
		}
	}
	if err != nil {
		b.Fatal(err)
	}
}
