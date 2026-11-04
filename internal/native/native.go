// Package native supports opt-in, locally built, embedded official SQLite assets.
package native

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Set only by generated, platform-specific sqlite_bundle files.
var asset []byte
var digest, filename string
var extract = sync.OnceValues(func() (string, error) {
	if len(asset) == 0 {
		return "", errors.New("sqlite: no bundled asset; generate it with go run ./internal/cmd/build-native -embed and build with -tags sqlite_bundle")
	}
	sum := sha256.Sum256(asset)
	if hex.EncodeToString(sum[:]) != digest {
		return "", errors.New("sqlite: bundled library SHA-256 mismatch")
	}
	dir, err := os.MkdirTemp("", "purego-sqlite-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, asset, 0500); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return path, nil
})

func Path() (string, func(), error) {
	path, err := extract()
	// Libraries remain mapped until exit. Keep the unique directory for process
	// life so repeated connectors resolve the same cached path. On Windows loaded
	// DLLs cannot be unlinked; remove stale purego-sqlite-* dirs when no process uses them.
	return path, func() {}, err
}

// PlatformFilename is shared with tests and documentation.
func PlatformFilename() string {
	switch runtime.GOOS {
	case "windows":
		return "sqlite3.dll"
	case "darwin":
		return "libsqlite3.dylib"
	default:
		return "libsqlite3.so"
	}
}
