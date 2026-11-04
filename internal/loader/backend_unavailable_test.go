//go:build !((linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo) && !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly))

package loader

import (
	"strings"
	"testing"
)

func TestBackendRequiresCGO(t *testing.T) {
	if _, _, err := Open(""); err == nil || !strings.Contains(err.Error(), "CGO_ENABLED=1") {
		t.Fatalf("expected actionable backend error, got %v", err)
	}
}
