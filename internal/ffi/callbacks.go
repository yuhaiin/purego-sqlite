package ffi

import (
	"fmt"
	"sync"
	"unsafe"
)

// CallbackHandlers belong to one SQLite registration. Native code retains an
// integer token only. Destruction runs after SQLite has stopped using it.
type CallbackHandlers struct {
	Scalar  func(uintptr, int32, uintptr)
	Step    func(uintptr, int32, uintptr)
	Final   func(uintptr)
	Compare func(int32, uintptr, int32, uintptr) int32
	Destroy func()
}

type CallbackPointers struct{ Scalar, Step, Final, Compare, Destroy uintptr }

var callbackRegistry = struct {
	sync.RWMutex
	next    uintptr
	entries map[uintptr]CallbackHandlers
}{entries: make(map[uintptr]CallbackHandlers)}

func NewCallbackToken(handlers CallbackHandlers) (uintptr, error) {
	callbackRegistry.Lock()
	defer callbackRegistry.Unlock()
	if callbackRegistry.next == ^uintptr(0) {
		return 0, fmt.Errorf("sqlite: callback token space exhausted")
	}
	callbackRegistry.next++
	callbackRegistry.entries[callbackRegistry.next] = handlers
	return callbackRegistry.next, nil
}

func callbackHandlers(token uintptr) CallbackHandlers {
	callbackRegistry.RLock()
	handlers := callbackRegistry.entries[token]
	callbackRegistry.RUnlock()
	return handlers
}

func ReleaseCallbackToken(token uintptr) {
	callbackRegistry.Lock()
	handlers := callbackRegistry.entries[token]
	delete(callbackRegistry.entries, token)
	callbackRegistry.Unlock()
	if handlers.Destroy != nil {
		handlers.Destroy()
	}
}

// CallbackCount supports lifecycle verification without exposing native handles.
func CallbackCount() int {
	callbackRegistry.RLock()
	defer callbackRegistry.RUnlock()
	return len(callbackRegistry.entries)
}

var callbackLibraries = struct {
	sync.Mutex
	entries map[uintptr]callbackLibrary
}{entries: make(map[uintptr]callbackLibrary)}

type callbackLibrary struct {
	pointers CallbackPointers
	err      error
}

func (a *API) Callbacks() (CallbackPointers, error) {
	callbackLibraries.Lock()
	defer callbackLibraries.Unlock()
	if entry, ok := callbackLibraries.entries[a.handle]; ok {
		return entry.pointers, entry.err
	}
	pointers, err := newCallbackPointers(a)
	callbackLibraries.entries[a.handle] = callbackLibrary{pointers, err}
	return pointers, err
}

// NativePointer decodes an address returned by SQLite, never a Go allocation.
func NativePointer(address uintptr) *byte { return *(**byte)(unsafe.Pointer(&address)) }
