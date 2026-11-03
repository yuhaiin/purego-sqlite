//go:build !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)) || sqlite_dynamic

package loader

import "errors"

func linkedLibrary() (uintptr, string) { return 0, "" }
func linkedSymbol(string) (uintptr, error) {
	return 0, errors.New("sqlite: linked SQLite is unavailable")
}
