package sqlite

import (
	"errors"
	"fmt"
	"syscall"
)

var (
	ErrClosed             = errors.New("sqlite: object is closed")
	ErrActiveRows         = errors.New("sqlite: close active rows before using the same physical connection")
	ErrMultipleStatements = errors.New("sqlite: multiple statements require an Exec call without parameters")
	ErrExtensionDisabled  = errors.New("sqlite: extension loading requires WithExtensions")
	ErrUnsupported        = errors.New("sqlite: unsupported capability")
	ErrCallbackReentry    = errors.New("sqlite: callbacks must not reenter their physical connection")
)

// Error preserves SQLite's primary and extended result codes.
type Error struct {
	Code, ExtendedCode int
	Message            string
	// SystemErrno is SQLite's saved OS error for CANTOPEN/IOERR, when available.
	// Zero means that SQLite did not supply a system cause.
	SystemErrno syscall.Errno
}

func (e *Error) Error() string {
	if e.SystemErrno != 0 {
		return fmt.Sprintf("sqlite: %s (code %d/%d, system error %d)", e.Message, e.Code, e.ExtendedCode, e.SystemErrno)
	}
	return fmt.Sprintf("sqlite: %s (code %d/%d)", e.Message, e.Code, e.ExtendedCode)
}

func (e *Error) Unwrap() error {
	if e.SystemErrno != 0 {
		return e.SystemErrno
	}
	return nil
}

// Is matches codes. ExtendedCode zero matches any extension of Code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && e.Code == t.Code && (t.ExtendedCode == 0 || e.ExtendedCode == t.ExtendedCode)
}

var (
	ErrBusy                 = &Error{Code: 5}
	ErrLocked               = &Error{Code: 6}
	ErrConstraint           = &Error{Code: 19}
	ErrReadOnly             = &Error{Code: 8}
	ErrAbort                = &Error{Code: 4}
	ErrInterrupt            = &Error{Code: 9}
	ErrIOErr                = &Error{Code: 10}
	ErrCorrupt              = &Error{Code: 11}
	ErrFull                 = &Error{Code: 13}
	ErrCantOpen             = &Error{Code: 14}
	ErrSchema               = &Error{Code: 17}
	ErrTooBig               = &Error{Code: 18}
	ErrAuth                 = &Error{Code: 23}
	ErrNotADB               = &Error{Code: 26}
	ErrBusyRecovery         = &Error{Code: 5, ExtendedCode: 5 | 1<<8}
	ErrBusySnapshot         = &Error{Code: 5, ExtendedCode: 5 | 2<<8}
	ErrBusyTimeout          = &Error{Code: 5, ExtendedCode: 5 | 3<<8}
	ErrLockedSharedCache    = &Error{Code: 6, ExtendedCode: 6 | 1<<8}
	ErrReadOnlyRecovery     = &Error{Code: 8, ExtendedCode: 8 | 1<<8}
	ErrReadOnlyCantLock     = &Error{Code: 8, ExtendedCode: 8 | 2<<8}
	ErrReadOnlyRollback     = &Error{Code: 8, ExtendedCode: 8 | 3<<8}
	ErrReadOnlyDBMoved      = &Error{Code: 8, ExtendedCode: 8 | 4<<8}
	ErrReadOnlyCantInit     = &Error{Code: 8, ExtendedCode: 8 | 5<<8}
	ErrReadOnlyDirectory    = &Error{Code: 8, ExtendedCode: 8 | 6<<8}
	ErrAbortRollback        = &Error{Code: 4, ExtendedCode: 4 | 2<<8}
	ErrConstraintCheck      = &Error{Code: 19, ExtendedCode: 19 | 1<<8}
	ErrConstraintCommitHook = &Error{Code: 19, ExtendedCode: 19 | 2<<8}
	ErrConstraintForeignKey = &Error{Code: 19, ExtendedCode: 19 | 3<<8}
	ErrConstraintFunction   = &Error{Code: 19, ExtendedCode: 19 | 4<<8}
	ErrConstraintNotNull    = &Error{Code: 19, ExtendedCode: 19 | 5<<8}
	ErrConstraintPrimaryKey = &Error{Code: 19, ExtendedCode: 19 | 6<<8}
	ErrConstraintTrigger    = &Error{Code: 19, ExtendedCode: 19 | 7<<8}
	ErrConstraintUnique     = &Error{Code: 19, ExtendedCode: 19 | 8<<8}
	ErrConstraintVTab       = &Error{Code: 19, ExtendedCode: 19 | 9<<8}
	ErrConstraintRowID      = &Error{Code: 19, ExtendedCode: 19 | 10<<8}
	ErrConstraintPinned     = &Error{Code: 19, ExtendedCode: 19 | 11<<8}
	ErrConstraintDataType   = &Error{Code: 19, ExtendedCode: 19 | 12<<8}
)
