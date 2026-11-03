//go:build cgo && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly) && (sqlite_cgo || (!linux && !darwin && !windows) || (!amd64 && !arm64))

package loader

const backendAvailable = true
