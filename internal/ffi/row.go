package ffi

import "unsafe"

// RowValue is the pointer-free, 24-byte purego_sqlite_read_row_v1 record.
// Native payload addresses are borrowed until the next step/reset/finalize.
// Keep this layout in sync with the embed repository's bridge/README.md.
type RowValue struct {
	Bits uint64
	Data uint64
	Size int32
	Type int32
}

const rowValueSize = uint32(unsafe.Sizeof(RowValue{}))

// Pointer only decodes a native TEXT/BLOB address, never a Go allocation.
func (v RowValue) Pointer() *byte {
	p := uintptr(v.Data)
	return *(**byte)(unsafe.Pointer(&p))
}
