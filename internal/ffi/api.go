// Package ffi contains SQLite C signatures. All C ints are explicitly 32-bit.
// Handles belong to SQLite; successful libraries are retained for process life.
package ffi

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

const (
	OK   = 0
	Row  = 100
	Done = 101
	// SQLITE_TRANSIENT is (sqlite3_destructor_type)-1 on the supported pointer-width ABIs.
	Transient = ^uintptr(0)
)

type API struct {
	connection func() *API
	Path       string
	// Exec drains rows and reads execution metadata before reset can change errors.
	// ResetClear returns the reset status; clearing bindings must still run on error.
	Exec                func(stmt, db uintptr) (rc int32, lastID, changes int64)
	ResetClear          func(uintptr) int32
	Open                func(string, *uintptr, int32, uintptr) int32                `sqlite:"sqlite3_open_v2"`
	Close               func(uintptr) int32                                         `sqlite:"sqlite3_close"`
	Errmsg              func(uintptr) string                                        `sqlite:"sqlite3_errmsg"`
	Errcode             func(uintptr) int32                                         `sqlite:"sqlite3_errcode"`
	ExtendedErrcode     func(uintptr) int32                                         `sqlite:"sqlite3_extended_errcode"`
	Errstr              func(int32) string                                          `sqlite:"sqlite3_errstr"`
	ExtendedResultCodes func(uintptr, int32) int32                                  `sqlite:"sqlite3_extended_result_codes"`
	DBReadonly          func(uintptr, string) int32                                 `sqlite:"sqlite3_db_readonly"`
	DBFilename          func(uintptr, string) string                                `sqlite:"sqlite3_db_filename"`
	Prepare             func(uintptr, *byte, int32, uint32, *uintptr, **byte) int32 `sqlite:"sqlite3_prepare_v3"`
	Step                func(uintptr) int32                                         `sqlite:"sqlite3_step"`
	Reset               func(uintptr) int32                                         `sqlite:"sqlite3_reset"`
	Finalize            func(uintptr) int32                                         `sqlite:"sqlite3_finalize"`
	ClearBindings       func(uintptr) int32                                         `sqlite:"sqlite3_clear_bindings"`
	StmtReadonly        func(uintptr) int32                                         `sqlite:"sqlite3_stmt_readonly"`
	SQL                 func(uintptr) string                                        `sqlite:"sqlite3_sql"`
	BindNull            func(uintptr, int32) int32                                  `sqlite:"sqlite3_bind_null"`
	BindInt64           func(uintptr, int32, int64) int32                           `sqlite:"sqlite3_bind_int64"`
	BindDouble          func(uintptr, int32, float64) int32                         `sqlite:"sqlite3_bind_double"`
	BindText            func(uintptr, int32, *byte, int32, uintptr) int32           `sqlite:"sqlite3_bind_text"`
	BindBlob            func(uintptr, int32, *byte, int32, uintptr) int32           `sqlite:"sqlite3_bind_blob"`
	BindParameterCount  func(uintptr) int32                                         `sqlite:"sqlite3_bind_parameter_count"`
	BindParameterIndex  func(uintptr, string) int32                                 `sqlite:"sqlite3_bind_parameter_index"`
	BindParameterName   func(uintptr, int32) string                                 `sqlite:"sqlite3_bind_parameter_name"`
	ColumnCount         func(uintptr) int32                                         `sqlite:"sqlite3_column_count"`
	ColumnName          func(uintptr, int32) string                                 `sqlite:"sqlite3_column_name"`
	ColumnDecltype      func(uintptr, int32) string                                 `sqlite:"sqlite3_column_decltype"`
	ColumnType          func(uintptr, int32) int32                                  `sqlite:"sqlite3_column_type"`
	ColumnInt64         func(uintptr, int32) int64                                  `sqlite:"sqlite3_column_int64"`
	ColumnDouble        func(uintptr, int32) float64                                `sqlite:"sqlite3_column_double"`
	ColumnText          func(uintptr, int32) *byte                                  `sqlite:"sqlite3_column_text"`
	ColumnBlob          func(uintptr, int32) *byte                                  `sqlite:"sqlite3_column_blob"`
	ColumnBytes         func(uintptr, int32) int32                                  `sqlite:"sqlite3_column_bytes"`
	Changes             func(uintptr) int32                                         `sqlite:"sqlite3_changes"`
	Changes64           func(uintptr) int64                                         `sqlite:"sqlite3_changes64,optional"`
	LastInsertRowid     func(uintptr) int64                                         `sqlite:"sqlite3_last_insert_rowid"`
	Autocommit          func(uintptr) int32                                         `sqlite:"sqlite3_get_autocommit"`
	BusyTimeout         func(uintptr, int32) int32                                  `sqlite:"sqlite3_busy_timeout"`
	Interrupt           func(uintptr)                                               `sqlite:"sqlite3_interrupt"`
	Libversion          func() string                                               `sqlite:"sqlite3_libversion"`
	LibversionNumber    func() int32                                                `sqlite:"sqlite3_libversion_number"`
	Threadsafe          func() int32                                                `sqlite:"sqlite3_threadsafe"`
	CompileOptionUsed   func(string) int32                                          `sqlite:"sqlite3_compileoption_used"`
	CompileOptionGet    func(int32) string                                          `sqlite:"sqlite3_compileoption_get"`
	Free                func(*byte)                                                 `sqlite:"sqlite3_free"`
	EnableLoadExtension func(uintptr, int32) int32                                  `sqlite:"sqlite3_enable_load_extension,optional"`
	LoadExtension       func(uintptr, string, string, **byte) int32                 `sqlite:"sqlite3_load_extension,optional"`
	// ConfigExtension is a platform-specialized call, never a general variadic binding.
	ConfigExtension func(uintptr, int32) int32
	BackupInit      func(uintptr, string, uintptr, string) uintptr `sqlite:"sqlite3_backup_init"`
	BackupStep      func(uintptr, int32) int32                     `sqlite:"sqlite3_backup_step"`
	BackupFinish    func(uintptr) int32                            `sqlite:"sqlite3_backup_finish"`
}

var libraries = struct {
	sync.Mutex
	apis map[string]*API
}{apis: make(map[string]*API)}

func Load(path string) (*API, error) {
	libraries.Lock()
	defer libraries.Unlock()
	if a := libraries.apis[path]; a != nil {
		return a, nil
	}
	h, actual, err := loader.Open(path)
	if err != nil {
		return nil, err
	}
	a := &API{Path: actual}
	if err = bind(a, h); err != nil {
		_ = loader.Close(h)
		return nil, err
	}
	if a.LibversionNumber() < 3020000 || a.Threadsafe() == 0 {
		version := a.Libversion()
		_ = loader.Close(h)
		return nil, fmt.Errorf("sqlite: need SQLite >= 3.20.0 with THREADSAFE enabled (loaded %s)", version)
	}
	a.bindOperations()
	bindFast(a, h)
	a.ConfigExtension = extensionConfig(h)
	libraries.apis[path] = a
	return a, nil
}

// ForConnection gives the driver a private scalar call frame when the backend
// supports it. Its hot calls require the owning Conn mutex; Interrupt remains
// independent so cancellation can run concurrently with Step.
func (a *API) ForConnection() *API {
	if a.connection != nil {
		return a.connection()
	}
	return a
}

func (a *API) bindOperations() {
	a.Exec = func(stmt, db uintptr) (rc int32, lastID, changes int64) {
		for rc = a.Step(stmt); rc == Row; rc = a.Step(stmt) {
		}
		if rc == Done {
			if a.StmtReadonly(stmt) == 0 {
				if a.Changes64 != nil {
					changes = a.Changes64(db)
				} else {
					changes = int64(a.Changes(db))
				}
			}
			lastID = a.LastInsertRowid(db)
		}
		return
	}
	a.ResetClear = func(stmt uintptr) int32 {
		rc := a.Reset(stmt)
		a.ClearBindings(stmt)
		return rc
	}
}

func bind(a *API, h uintptr) error {
	v := reflect.ValueOf(a).Elem()
	for i := range v.NumField() {
		tag := v.Type().Field(i).Tag.Get("sqlite")
		if tag == "" {
			continue
		}
		name, optional := splitSymbol(tag)
		symbol, err := loader.Symbol(h, name)
		if err != nil {
			if optional {
				continue
			}
			return fmt.Errorf("sqlite: missing required symbol %s: %w", name, err)
		}
		registerAPI(a, v.Field(i).Addr().Interface(), name, symbol)
	}
	return nil
}

func splitSymbol(tag string) (string, bool) {
	name, _, optional := strings.Cut(tag, ",")
	return name, optional
}
