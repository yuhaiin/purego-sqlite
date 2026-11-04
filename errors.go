package sqlite

import (
	"errors"
	"fmt"
)

var (
	ErrClosed             = errors.New("sqlite: object is closed")
	ErrActiveRows         = errors.New("sqlite: close active rows before using the same physical connection")
	ErrMultipleStatements = errors.New("sqlite: multiple statements require an Exec call without parameters")
	ErrExtensionDisabled  = errors.New("sqlite: extension loading requires WithExtensions")
	ErrUnsupported        = errors.New("sqlite: unsupported capability")
)

// Error preserves SQLite's primary and extended result codes.
type Error struct {
	Code, ExtendedCode int
	Message            string
}

func (e *Error) Error() string {
	return fmt.Sprintf("sqlite: %s (code %d/%d)", e.Message, e.Code, e.ExtendedCode)
}

// Is matches codes. ExtendedCode zero matches any extension of Code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && e.Code == t.Code && (t.ExtendedCode == 0 || e.ExtendedCode == t.ExtendedCode)
}

var (
	ErrBusy       = &Error{Code: 5}
	ErrLocked     = &Error{Code: 6}
	ErrConstraint = &Error{Code: 19}
	ErrReadOnly   = &Error{Code: 8}
)
