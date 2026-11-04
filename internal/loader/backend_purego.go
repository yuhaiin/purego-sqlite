//go:build ((linux && !android) || (darwin && !ios) || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package loader

const backendAvailable = true
