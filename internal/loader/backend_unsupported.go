//go:build !(((linux && !android) || (darwin && !ios) || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo) && !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly))

package loader

const backendAvailable = false
