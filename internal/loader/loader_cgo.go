//go:build !windows && cgo && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly) && (sqlite_cgo || (!linux && !darwin && !windows) || (!amd64 && !arm64))

package loader

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Capture and copy dlerror on the same C thread as the operation. Go may
// migrate between C calls; dlerror's storage is owned by the dynamic loader.
typedef struct { uintptr_t value; char *error; } load_result;
static load_result load_library(const char *path) {
 dlerror();
 void *h = dlopen(path, RTLD_NOW | RTLD_LOCAL);
 load_result r = {(uintptr_t)h, NULL};
 if (!h) { const char *e = dlerror(); r.error = strdup(e ? e : "dlopen failed"); }
 return r;
}
static load_result load_symbol(uintptr_t handle, const char *name) {
 dlerror();
 void *p = dlsym((void *)handle, name);
 const char *e = dlerror();
 load_result r = {(uintptr_t)p, NULL};
 if (e || !p) r.error = strdup(e ? e : "null symbol");
 return r;
}
static char *close_library(uintptr_t handle) {
 if (!dlclose((void *)handle)) return NULL;
 const char *e = dlerror();
 return strdup(e ? e : "dlclose failed");
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func loadError(message *C.char) error {
	if message == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(message))
	return errors.New(C.GoString(message))
}
func open(path string) (uintptr, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	r := C.load_library(p)
	if r.value == 0 && r.error == nil {
		return 0, errors.New("sqlite: dlopen failed without an error message")
	}
	return uintptr(r.value), loadError(r.error)
}
func Symbol(handle uintptr, name string) (uintptr, error) {
	p := C.CString(name)
	defer C.free(unsafe.Pointer(p))
	r := C.load_symbol(C.uintptr_t(handle), p)
	if r.value == 0 && r.error == nil {
		return 0, errors.New("sqlite: dlsym failed without an error message")
	}
	return uintptr(r.value), loadError(r.error)
}
func Close(handle uintptr) error { return loadError(C.close_library(C.uintptr_t(handle))) }
