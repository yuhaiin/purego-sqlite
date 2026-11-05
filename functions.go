package sqlite

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

// FunctionFlag adds restrictions/properties to an application SQL function.
type FunctionFlag int32

const (
	FunctionDirectOnly FunctionFlag = 0x80000
	FunctionInnocuous  FunctionFlag = 0x200000
)

// RegisterFunc installs a Go scalar function on this physical connection. Inputs
// and results support integers, floats, bool, string, []byte and any. A second
// result may be error; variadic inputs are supported. pure enables SQLite's
// DETERMINISTIC flag. Use WithInit to register on every pooled connection.
// Callbacks must not call back into the same connection or its database/sql pool.
func (c *Conn) RegisterFunc(name string, implementation any, pure bool, flags ...FunctionFlag) error {
	fn := reflect.ValueOf(implementation)
	if !fn.IsValid() || fn.Kind() != reflect.Func || fn.IsNil() {
		return errors.New("sqlite: RegisterFunc requires a nonnil function")
	}
	signature, err := newFunctionSignature(fn.Type(), 0, false)
	if err != nil {
		return err
	}
	return c.registerFunction(name, signature.arity(), pure, flags, func() ffi.CallbackHandlers {
		return ffi.CallbackHandlers{Scalar: func(ctx uintptr, argc int32, argv uintptr) {
			c.callback(ctx, func() error {
				args, err := signature.arguments(c.api, argc, argv)
				if err != nil {
					return err
				}
				results, err := invoke(fn, args)
				if err != nil {
					return err
				}
				return c.functionResult(ctx, results)
			})
		}}
	}, false)
}

func (c *Conn) registerFunction(name string, arity int32, pure bool, flags []FunctionFlag, handlers func() ffi.CallbackHandlers, aggregate bool) error {
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
	bits := int32(1) // SQLITE_UTF8
	if pure {
		bits |= 0x800
	}
	for _, flag := range flags {
		if flag != FunctionDirectOnly && flag != FunctionInnocuous {
			return fmt.Errorf("sqlite: invalid function flag %d", flag)
		}
		if c.api.LibversionNumber() < 3031000 {
			return fmt.Errorf("%w: function security flags need SQLite 3.31+", ErrUnsupported)
		}
		bits |= int32(flag)
	}
	pointers, err := c.api.Callbacks()
	if err != nil {
		return err
	}
	token, err := ffi.NewCallbackToken(handlers())
	if err != nil {
		return err
	}
	scalar, step, final := pointers.Scalar, uintptr(0), uintptr(0)
	if aggregate {
		scalar, step, final = 0, pointers.Step, pointers.Final
	}
	rc := c.api.CreateFunction(c.db, name, arity, bits, token, scalar, step, final, pointers.Destroy)
	if rc != ffi.OK {
		// SQLite v2 also destroys on failure; release is intentionally idempotent.
		ffi.ReleaseCallbackToken(token)
		return c.sqliteError(rc)
	}
	return nil
}

func validateFunctionName(name string) error {
	if len(name) == 0 || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return errors.New("sqlite: function/collation name must be 1..255 UTF-8 bytes without NUL")
	}
	return nil
}

func (c *Conn) callback(ctx uintptr, fn func() error) {
	c.inCallback.Add(1)
	defer c.inCallback.Add(-1)
	defer func() {
		if value := recover(); value != nil {
			c.functionError(ctx, fmt.Errorf("sqlite: callback panic: %v", value))
		}
	}()
	if err := fn(); err != nil {
		c.functionError(ctx, err)
	}
}

func (c *Conn) functionError(ctx uintptr, err error) {
	message := []byte(err.Error())
	if len(message) == 0 {
		message = []byte("sqlite: callback error")
	}
	if len(message) > math.MaxInt32 {
		message = message[:math.MaxInt32]
	}
	c.api.ResultError(ctx, &message[0], int32(len(message)))
	runtime.KeepAlive(message)
}

func invoke(fn reflect.Value, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("sqlite: callback panic: %v", value)
		}
	}()
	return fn.Call(args), nil
}

var errorType = reflect.TypeFor[error]()

type functionSignature struct {
	inputs   []reflect.Type
	variadic bool
}

func newFunctionSignature(typ reflect.Type, skip int, step bool) (functionSignature, error) {
	signature := functionSignature{variadic: typ.IsVariadic()}
	for i := skip; i < typ.NumIn(); i++ {
		input := typ.In(i)
		if signature.variadic && i == typ.NumIn()-1 {
			input = input.Elem()
		}
		if !functionValueType(input) {
			return signature, fmt.Errorf("sqlite: unsupported callback argument %s", input)
		}
		signature.inputs = append(signature.inputs, input)
	}
	if step {
		if typ.NumOut() > 1 || typ.NumOut() == 1 && !typ.Out(0).Implements(errorType) {
			return signature, errors.New("sqlite: aggregate Step must return nothing or error")
		}
	} else {
		if typ.NumOut() < 1 || typ.NumOut() > 2 || !functionValueType(typ.Out(0)) || typ.NumOut() == 2 && !typ.Out(1).Implements(errorType) {
			return signature, errors.New("sqlite: function must return a supported value and optional error")
		}
	}
	return signature, nil
}

func functionValueType(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Bool, reflect.String:
		return true
	case reflect.Interface:
		return typ.NumMethod() == 0
	case reflect.Slice:
		return typ.Elem() == reflect.TypeFor[byte]()
	default:
		return false
	}
}

func (s functionSignature) arity() int32 {
	if s.variadic {
		return -1
	}
	return int32(len(s.inputs))
}

func (s functionSignature) arguments(api *ffi.API, count int32, argv uintptr) ([]reflect.Value, error) {
	minimum := len(s.inputs)
	if s.variadic {
		minimum--
	}
	if int(count) < minimum || !s.variadic && int(count) != minimum {
		return nil, fmt.Errorf("sqlite: callback expected %d arguments, got %d", minimum, count)
	}
	values := unsafe.Slice((*uintptr)(unsafe.Pointer(ffi.NativePointer(argv))), int(count))
	arguments := make([]reflect.Value, len(values))
	for i, pointer := range values {
		index := i
		if s.variadic && i >= minimum {
			index = minimum
		}
		argument, err := callbackArgument(api, pointer, s.inputs[index])
		if err != nil {
			return nil, fmt.Errorf("sqlite: argument %d: %w", i+1, err)
		}
		arguments[i] = argument
	}
	return arguments, nil
}

func callbackArgument(api *ffi.API, pointer uintptr, typ reflect.Type) (reflect.Value, error) {
	storage := api.ValueType(pointer)
	var value any
	switch storage {
	case 1:
		value = api.ValueInt64(pointer)
	case 2:
		value = api.ValueDouble(pointer)
	case 3, 4:
		var p *byte
		if storage == 3 {
			p = api.ValueText(pointer)
		} else {
			p = api.ValueBlob(pointer)
		}
		size := int(api.ValueBytes(pointer))
		if p == nil && (size != 0 || storage == 3) {
			return reflect.Value{}, errors.New("sqlite: cannot allocate callback value")
		}
		if storage == 3 || typ.Kind() == reflect.String {
			value = string(unsafe.Slice(p, size))
		} else {
			data := make([]byte, size)
			copy(data, unsafe.Slice(p, size))
			value = data
		}
	case 5:
		if typ.Kind() != reflect.Interface {
			return reflect.Value{}, errors.New("NULL requires an any argument")
		}
		return reflect.ValueOf([]byte(nil)), nil // mattn's generic NULL representation.
	default:
		return reflect.Value{}, errors.New("sqlite: unknown callback value type")
	}
	if typ.Kind() == reflect.Interface {
		return reflect.ValueOf(value), nil
	}
	if typ.Kind() == reflect.Bool && storage == 1 {
		return reflect.ValueOf(value.(int64) != 0).Convert(typ), nil
	}
	if typ.Kind() == reflect.Slice && (storage == 3 || storage == 4) {
		if text, ok := value.(string); ok {
			value = []byte(text)
		}
	}
	valid := storage == 1 && typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Uint64 || storage == 2 && (typ.Kind() == reflect.Float32 || typ.Kind() == reflect.Float64) || (storage == 3 || storage == 4) && (typ.Kind() == reflect.String || typ.Kind() == reflect.Slice)
	if !valid {
		return reflect.Value{}, fmt.Errorf("cannot convert SQLite type %d to %s", storage, typ)
	}
	return reflect.ValueOf(value).Convert(typ), nil
}

func resultError(value reflect.Value) error {
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer || value.Kind() == reflect.Map || value.Kind() == reflect.Slice || value.Kind() == reflect.Func || value.Kind() == reflect.Chan {
		if value.IsNil() {
			return nil
		}
	}
	return value.Interface().(error)
}

func (c *Conn) functionResult(ctx uintptr, results []reflect.Value) error {
	if len(results) == 2 {
		if err := resultError(results[1]); err != nil {
			return err
		}
	}
	value := results[0]
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			c.api.ResultNull(ctx)
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		var integer int64
		if value.Bool() {
			integer = 1
		}
		c.api.ResultInt64(ctx, integer)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		c.api.ResultInt64(ctx, value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		c.api.ResultInt64(ctx, int64(value.Uint()))
	case reflect.Float32, reflect.Float64:
		c.api.ResultDouble(ctx, value.Float())
	case reflect.String:
		text := value.String()
		if len(text) > math.MaxInt32 {
			return errors.New("sqlite: callback TEXT exceeds C int length")
		}
		p := unsafe.StringData(text)
		if len(text) == 0 {
			p = new(byte)
		}
		c.api.ResultText(ctx, p, int32(len(text)), ffi.Transient)
		runtime.KeepAlive(text)
	case reflect.Slice:
		if value.Type().Elem().Kind() != reflect.Uint8 {
			return errors.New("sqlite: callback result must be []byte")
		}
		if value.IsNil() {
			c.api.ResultNull(ctx)
			return nil
		}
		data := value.Bytes()
		if len(data) > math.MaxInt32 {
			return errors.New("sqlite: callback BLOB exceeds C int length")
		}
		p := unsafe.SliceData(data)
		if len(data) == 0 {
			p = new(byte)
		}
		c.api.ResultBlob(ctx, p, int32(len(data)), ffi.Transient)
		runtime.KeepAlive(data)
	default:
		return fmt.Errorf("sqlite: unsupported dynamic callback result %s", value.Type())
	}
	return nil
}
