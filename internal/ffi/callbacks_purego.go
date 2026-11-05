//go:build ((linux && !android) || (darwin && !ios) || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package ffi

import (
	"fmt"
	"github.com/ebitengine/purego"
)

var commonCallbacks struct {
	pointers    CallbackPointers
	initialized bool
}

// Calls are serialized by callbackLibraries. Entrypoints are allocated once per
// loaded library, not per connection/function. Capture only immutable bindings:
// connection-local fast argument frames must never be used from a callback.
func newCallbackPointers(a *API) (p CallbackPointers, err error) {
	if len(callbackLibraries.entries) >= 64 {
		return p, fmt.Errorf("sqlite: callbacks support at most 64 distinct native engines per process")
	}
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("sqlite: callback ABI unavailable: %v", value)
		}
	}()
	if !commonCallbacks.initialized {
		commonCallbacks.initialized = true
		commonCallbacks.pointers.Destroy = purego.NewCallback(func(token uintptr) uintptr { ReleaseCallbackToken(token); return 0 })
		commonCallbacks.pointers.Compare = purego.NewCallback(func(token uintptr, n1 int32, p1 uintptr, n2 int32, p2 uintptr) uintptr {
			if fn := callbackHandlers(token).Compare; fn != nil {
				return uintptr(fn(n1, p1, n2, p2))
			}
			return 0
		})
	}
	if commonCallbacks.pointers.Destroy == 0 || commonCallbacks.pointers.Compare == 0 {
		return p, fmt.Errorf("sqlite: callback entrypoints unavailable")
	}
	p = commonCallbacks.pointers
	userData := a.UserData
	p.Scalar = purego.NewCallback(func(ctx uintptr, argc int32, argv uintptr) uintptr {
		if fn := callbackHandlers(userData(ctx)).Scalar; fn != nil {
			fn(ctx, argc, argv)
		}
		return 0
	})
	p.Step = purego.NewCallback(func(ctx uintptr, argc int32, argv uintptr) uintptr {
		if fn := callbackHandlers(userData(ctx)).Step; fn != nil {
			fn(ctx, argc, argv)
		}
		return 0
	})
	p.Final = purego.NewCallback(func(ctx uintptr) uintptr {
		if fn := callbackHandlers(userData(ctx)).Final; fn != nil {
			fn(ctx)
		}
		return 0
	})
	return p, nil
}
