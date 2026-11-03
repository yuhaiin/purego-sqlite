//go:build windows && (amd64 || arm64)

package loader

import "golang.org/x/sys/windows"

func systemPaths() []string { return nil }
func open(path string) (uintptr, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, err := windows.LoadLibraryEx(p, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	return uintptr(h), err
}
func Symbol(handle uintptr, name string) (uintptr, error) {
	return windows.GetProcAddress(windows.Handle(handle), name)
}
func Close(handle uintptr) error { return windows.FreeLibrary(windows.Handle(handle)) }
