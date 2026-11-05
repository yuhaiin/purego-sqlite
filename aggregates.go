package sqlite

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

type aggregateState struct {
	value reflect.Value
	step  reflect.Value
	done  reflect.Value
	err   error
}

// RegisterAggregator installs a constructor returning a pointer/interface with
// Step(args...) [error] and Done() (value [, error]). Each SQLite group owns one
// instance, including empty input. The constructor may also return an error.
func (c *Conn) RegisterAggregator(name string, implementation any, pure bool, flags ...FunctionFlag) error {
	constructor := reflect.ValueOf(implementation)
	if !constructor.IsValid() || constructor.Kind() != reflect.Func || constructor.IsNil() {
		return errors.New("sqlite: aggregator requires a nonnil constructor")
	}
	typ := constructor.Type()
	if typ.NumIn() != 0 || typ.NumOut() < 1 || typ.NumOut() > 2 || typ.NumOut() == 2 && !typ.Out(1).Implements(errorType) {
		return errors.New("sqlite: constructor must have no inputs and return an aggregate and optional error")
	}
	result := typ.Out(0)
	if result.Kind() != reflect.Pointer && result.Kind() != reflect.Interface {
		return errors.New("sqlite: aggregate must be a pointer or interface")
	}
	step, ok := result.MethodByName("Step")
	if !ok {
		return errors.New("sqlite: aggregate needs Step")
	}
	done, ok := result.MethodByName("Done")
	if !ok {
		return errors.New("sqlite: aggregate needs Done")
	}
	skip := 0
	if result.Kind() == reflect.Pointer {
		skip = 1
	}
	stepSignature, err := newFunctionSignature(step.Type, skip, true)
	if err != nil {
		return err
	}
	doneSignature, err := newFunctionSignature(done.Type, skip, false)
	if err != nil {
		return err
	}
	if len(doneSignature.inputs) != 0 {
		return errors.New("sqlite: aggregate Done must have no inputs")
	}
	return c.registerFunction(name, stepSignature.arity(), pure, flags, func() ffi.CallbackHandlers {
		states := make(map[uintptr]*aggregateState)
		newState := func() *aggregateState {
			state := &aggregateState{}
			values, err := invoke(constructor, nil)
			if err == nil && len(values) == 2 {
				err = resultError(values[1])
			}
			if err == nil {
				state.value = values[0]
				value := state.value
				for value.Kind() == reflect.Interface && !value.IsNil() {
					value = value.Elem()
				}
				if value.IsNil() {
					err = errors.New("sqlite: aggregate constructor returned nil")
				} else {
					state.step, state.done = state.value.MethodByName("Step"), state.value.MethodByName("Done")
				}
			}
			state.err = err
			return state
		}
		return ffi.CallbackHandlers{
			Step: func(ctx uintptr, argc int32, argv uintptr) {
				c.callback(ctx, func() error {
					pointer := c.api.AggregateContext(ctx, 1)
					if pointer == nil {
						return errors.New("sqlite: aggregate state allocation failed")
					}
					key := uintptr(unsafe.Pointer(pointer))
					state := states[key]
					if state == nil {
						state = newState()
						states[key] = state
					}
					if state.err != nil {
						return state.err
					}
					arguments, err := stepSignature.arguments(c.api, argc, argv)
					if err != nil {
						state.err = err
						return err
					}
					values, err := invoke(state.step, arguments)
					if err == nil && len(values) == 1 {
						err = resultError(values[0])
					}
					state.err = err
					return err
				})
			},
			Final: func(ctx uintptr) {
				c.callback(ctx, func() error {
					key := uintptr(unsafe.Pointer(c.api.AggregateContext(ctx, 0)))
					state := states[key]
					delete(states, key)
					if state == nil {
						state = newState()
					}
					if state.err != nil {
						return state.err
					}
					values, err := invoke(state.done, nil)
					if err != nil {
						return err
					}
					return c.functionResult(ctx, values)
				})
			},
			Destroy: func() { clear(states) },
		}
	}, true)
}

// RegisterCollation installs a UTF-8 comparator, or removes it when compare is
// nil. Results are normalized to SQLite's -1/0/+1 convention. A panic interrupts
// the operation and is returned as an error, rather than silently equating keys.
func (c *Conn) RegisterCollation(name string, compare func(string, string) int) error {
	if err := validateFunctionName(name); err != nil {
		return err
	}
	if c.inCallback.Load() != 0 {
		return ErrCallbackReentry
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if compare == nil {
		return c.sqliteError(c.api.CreateCollation(c.db, name, 1, 0, 0, 0))
	}
	pointers, err := c.api.Callbacks()
	if err != nil {
		return err
	}
	token, err := ffi.NewCallbackToken(ffi.CallbackHandlers{Compare: func(n1 int32, p1 uintptr, n2 int32, p2 uintptr) (result int32) {
		c.inCallback.Add(1)
		defer c.inCallback.Add(-1)
		defer func() {
			if value := recover(); value != nil {
				c.callbackErr = fmt.Errorf("sqlite: collation panic: %v", value)
				c.api.Interrupt(c.db)
			}
		}()
		left := string(unsafe.Slice(ffi.NativePointer(p1), int(n1)))
		right := string(unsafe.Slice(ffi.NativePointer(p2), int(n2)))
		switch value := compare(left, right); {
		case value < 0:
			return -1
		case value > 0:
			return 1
		default:
			return 0
		}
	}})
	if err != nil {
		return err
	}
	if rc := c.api.CreateCollation(c.db, name, 1, token, pointers.Compare, pointers.Destroy); rc != ffi.OK {
		ffi.ReleaseCallbackToken(token) // Unlike function_v2, collation_v2 does not destroy on failure.
		return c.sqliteError(rc)
	}
	return nil
}
