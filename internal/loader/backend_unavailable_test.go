//go:build (!cgo || (!linux && !darwin && !windows && !freebsd && !openbsd && !netbsd && !dragonfly)) && (sqlite_cgo || (!linux && !darwin && !windows) || (!amd64 && !arm64))

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
