//go:build (!linux && !darwin && !windows) || (!amd64 && !arm64)

package ffi

func register(any, uintptr)                 { panic("unreachable on unsupported platform") }
func splitSymbol(tag string) (string, bool) { return tag, false }
