//go:build cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly) && !sqlite_dynamic

package loader

import "github.com/yuhaiin/purego-sqlite/internal/sqlite3"

func linkedLibrary() (uintptr, string)          { return linkedHandle, "builtin:sqlite3-" + sqlite3.Version }
func linkedSymbol(name string) (uintptr, error) { return sqlite3.Symbol(name) }
