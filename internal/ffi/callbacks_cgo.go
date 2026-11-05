//go:build cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package ffi

/*
#include <stdint.h>
#include "callback_slots.h"
#include "hook_callbacks.h"
*/
import "C"

import (
	"fmt"
	"github.com/yuhaiin/purego-sqlite/internal/loader"
)

var cgoCallbackLibraries = make(map[uintptr]int)

func newCallbackPointers(a *API) (CallbackPointers, error) {
	userData, err := loader.Symbol(a.handle, "sqlite3_user_data")
	if err != nil {
		return CallbackPointers{}, err
	}
	slot, ok := cgoCallbackLibraries[userData]
	if !ok {
		slot = len(cgoCallbackLibraries)
		if slot >= 64 {
			return CallbackPointers{}, fmt.Errorf("sqlite: callbacks support at most 64 distinct native engines per process")
		}
		C.set_callback_userdata(C.int(slot), C.uintptr_t(userData))
		cgoCallbackLibraries[userData] = slot
	}
	return CallbackPointers{Scalar: uintptr(C.scalar_callback_address(C.int(slot))), Step: uintptr(C.step_callback_address(C.int(slot))), Final: uintptr(C.final_callback_address(C.int(slot))), Compare: uintptr(C.compare_callback_address()), Destroy: uintptr(C.destroy_callback_address()), Update: uintptr(C.update_callback_address()), Commit: uintptr(C.commit_callback_address()), Rollback: uintptr(C.rollback_callback_address()), Authorize: uintptr(C.authorize_callback_address()), Trace: uintptr(C.trace_callback_address())}, nil
}
