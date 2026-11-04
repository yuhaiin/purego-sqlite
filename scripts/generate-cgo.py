#!/usr/bin/env python3
"""Generate typed CGO trampolines for API's native SQLite signatures.

The C compiler marshals every call, including 64-bit integers/doubles on 32-bit
architectures. No SQLite engine or third-party Go driver is included. The opaque
objects, integer typedef and signatures follow SQLite's public C API; no native
headers or link-time SQLite library are needed for this dynamic-library backend.
"""
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
TAG = "cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)"
# Exact native C signatures, independent of the Go bridge's uintptr handles.
SIGNATURES = {
    "Open": ("int", "const char*,sqlite3**,int,const char*"),
    "Close": ("int", "sqlite3*"),
    "Errmsg": ("const char*", "sqlite3*"),
    "Errcode": ("int", "sqlite3*"),
    "ExtendedErrcode": ("int", "sqlite3*"),
    "Errstr": ("const char*", "int"),
    "ExtendedResultCodes": ("int", "sqlite3*,int"),
    "DBReadonly": ("int", "sqlite3*,const char*"),
    "DBFilename": ("const char*", "sqlite3*,const char*"),
    "Prepare": ("int", "sqlite3*,const char*,int,unsigned int,sqlite3_stmt**,const char**"),
    "Step": ("int", "sqlite3_stmt*"),
    "Reset": ("int", "sqlite3_stmt*"),
    "Finalize": ("int", "sqlite3_stmt*"),
    "ClearBindings": ("int", "sqlite3_stmt*"),
    "StmtReadonly": ("int", "sqlite3_stmt*"),
    "SQL": ("const char*", "sqlite3_stmt*"),
    "BindNull": ("int", "sqlite3_stmt*,int"),
    "BindInt64": ("int", "sqlite3_stmt*,int,sqlite3_int64"),
    "BindDouble": ("int", "sqlite3_stmt*,int,double"),
    "BindText": ("int", "sqlite3_stmt*,int,const char*,int,sqlite_destructor"),
    "BindBlob": ("int", "sqlite3_stmt*,int,const void*,int,sqlite_destructor"),
    "BindParameterCount": ("int", "sqlite3_stmt*"),
    "BindParameterIndex": ("int", "sqlite3_stmt*,const char*"),
    "BindParameterName": ("const char*", "sqlite3_stmt*,int"),
    "ColumnCount": ("int", "sqlite3_stmt*"),
    "ColumnName": ("const char*", "sqlite3_stmt*,int"),
    "ColumnDecltype": ("const char*", "sqlite3_stmt*,int"),
    "ColumnType": ("int", "sqlite3_stmt*,int"),
    "ColumnInt64": ("sqlite3_int64", "sqlite3_stmt*,int"),
    "ColumnDouble": ("double", "sqlite3_stmt*,int"),
    "ColumnText": ("const unsigned char*", "sqlite3_stmt*,int"),
    "ColumnBlob": ("const void*", "sqlite3_stmt*,int"),
    "ColumnBytes": ("int", "sqlite3_stmt*,int"),
    "Changes": ("int", "sqlite3*"),
    "Changes64": ("sqlite3_int64", "sqlite3*"),
    "LastInsertRowid": ("sqlite3_int64", "sqlite3*"),
    "Autocommit": ("int", "sqlite3*"),
    "BusyTimeout": ("int", "sqlite3*,int"),
    "Interrupt": ("void", "sqlite3*"),
    "Libversion": ("const char*", ""),
    "LibversionNumber": ("int", ""),
    "Threadsafe": ("int", ""),
    "CompileOptionUsed": ("int", "const char*"),
    "CompileOptionGet": ("const char*", "int"),
    "Free": ("void", "void*"),
    "EnableLoadExtension": ("int", "sqlite3*,int"),
    "LoadExtension": ("int", "sqlite3*,const char*,const char*,char**"),
    "BackupInit": ("sqlite3_backup*", "sqlite3*,const char*,sqlite3*,const char*"),
    "BackupStep": ("int", "sqlite3_backup*,int"),
    "BackupFinish": ("int", "sqlite3_backup*"),
}
BRIDGE = {"uintptr": "uintptr_t", "int32": "int", "uint32": "unsigned int", "int64": "sqlite3_int64", "float64": "double", "string": "const char*", "*uintptr": "uintptr_t*", "*byte": "void*", "**byte": "char**", "": "void"}
GO_CAST = {"uintptr": "C.uintptr_t", "int32": "C.int", "uint32": "C.uint", "int64": "C.sqlite3_int64", "float64": "C.double"}


def generate():
    fields = re.findall(r'^\s*(\w+)\s+func\((.*?)\)(?:\s+(\w+|\*byte))?\s+`sqlite:"([^",]+)(?:,optional)?"`', (ROOT / "internal/ffi/api.go").read_text(), re.M)
    if {f[0] for f in fields} != set(SIGNATURES):
        raise SystemExit("API fields and C signatures differ; update SIGNATURES")
    c = ["#include <stdint.h>", "#include <stdlib.h>", "typedef struct sqlite3 sqlite3;", "typedef struct sqlite3_stmt sqlite3_stmt;", "typedef struct sqlite3_backup sqlite3_backup;", "typedef long long sqlite3_int64;", "typedef void (*sqlite_destructor)(void*);",
         '_Static_assert(sizeof(int) == 4, "SQLite C int must be 32 bits");',
         '_Static_assert(sizeof(sqlite3_int64) == 8, "SQLite integer must be 64 bits");',
         '_Static_assert(sizeof(uintptr_t) == sizeof(void*), "handle width must match pointers");']
    go = ["func registerAPI(a *API, _ any, name string, symbol uintptr) {", "switch name {"]
    for field, params, result, symbol in fields:
        types = params.split(", ") if params else []
        native_result, native_params = SIGNATURES[field]
        native_types = native_params.split(",") if native_params else []
        args = [f"p{i}" for i in range(len(types))]
        cast_args = [f"({nt}){arg}" for nt, arg in zip(native_types, args)]
        bridge_params = [f"{BRIDGE[t]} {arg}" for t, arg in zip(types, args)]
        native_fn = f"(({native_result} (*)({native_params or 'void'}))fn)"
        prefix = []
        suffix = []
        if field == "Open":
            prefix = ["sqlite3 *db = NULL;"]
            cast_args[1] = "&db"
            suffix = ["*p1 = (uintptr_t)db;"]
        if field == "Prepare":
            prefix = ["sqlite3_stmt *stmt = NULL;", "const char *tail = NULL;"]
            cast_args[4:6] = ["&stmt", "&tail"]
            bridge_params[5] = "int *p5"
            suffix = ["*p4 = (uintptr_t)stmt;", "*p5 = tail ? (int)(tail - (const char*)p1) : 0;"]
        call = native_fn + "(" + ", ".join(cast_args) + ")"
        if suffix:
            body = prefix + [f"int result = {call};"] + suffix + ["return result;"]
        elif result:
            body = [f"return ({BRIDGE[result]}){call};"]
        else:
            body = [call + ";"]
        c += [f"static {BRIDGE[result]} call_{symbol}(uintptr_t fn" + (", " + ", ".join(bridge_params) if types else "") + ") {", *body, "}"]
        go += [f'case "{symbol}":', f"a.{field} = func(" + ", ".join(f"{arg} {t}" for arg, t in zip(args, types)) + ")" + (" " + result if result else "") + " {"]
        conversions = []
        for arg, t in zip(args, types):
            if t == "string":
                go += [f"c{arg} := C.CString({arg})", f"defer C.free(unsafe.Pointer(c{arg}))"]
                conversions.append("c" + arg)
            elif t == "*uintptr":
                conversions.append(f"(*C.uintptr_t)(unsafe.Pointer({arg}))")
            elif t == "*byte":
                conversions.append(f"unsafe.Pointer({arg})")
            elif t == "**byte":
                if field == "Prepare":
                    go += ["var offset C.int"]
                    conversions.append("&offset")
                else:
                    go += [f"var c{arg} *C.char"]
                    conversions.append("&c" + arg)
            else:
                conversions.append(f"{GO_CAST[t]}({arg})")
        call = f"C.call_{symbol}(C.uintptr_t(symbol)" + (", " + ", ".join(conversions) if types else "") + ")"
        go += [("r := " if result else "") + call]
        if field == "Prepare":
            # Compute a Go pointer in Go, never write/retain one in C memory.
            go += ["*p5 = (*byte)(unsafe.Add(unsafe.Pointer(p1), int(offset)))"]
        if field == "LoadExtension":
            go += ["*p3 = (*byte)(unsafe.Pointer(cp3))"]
        if result == "string":
            go += ["return C.GoString(r)"]
        elif result == "*byte":
            go += ["return (*byte)(unsafe.Pointer(r))"]
        elif result:
            go += [f"return {result}(r)"]
        go += ["}"]
    go += ['default: panic("sqlite: missing CGO binding for " + name)', "}", "}"]
    c += ['''// Use the real C variadic ABI, including on 32-bit MIPS and Apple arm64.
static int config_extension(uintptr_t fn, uintptr_t db, int enabled) {
 return ((int (*)(sqlite3*, int, ...))fn)((sqlite3*)db, 1005, enabled, (int*)NULL);
}''']
    go += ['''func extensionConfig(h uintptr) func(uintptr, int32) int32 {
 p, err := loader.Symbol(h, "sqlite3_db_config")
 if err != nil { return nil }
 return func(db uintptr, enabled int32) int32 { return int32(C.config_extension(C.uintptr_t(p), C.uintptr_t(db), C.int(enabled))) }
}''']
    path = ROOT / "internal/ffi/register_cgo.go"
    path.write_text("// Code generated by scripts/generate-cgo.py; DO NOT EDIT.\n//go:build " + TAG + "\n\npackage ffi\n\n/*\n" + "\n".join(c) + '\n*/\nimport "C"\n\nimport (\n"unsafe"\n"github.com/yuhaiin/purego-sqlite/internal/loader"\n)\n\n' + "\n".join(go) + "\n")
    subprocess.run(["gofmt", "-w", str(path)], check=True)
    symbols = [symbol for _, _, _, symbol in fields] + ["sqlite3_db_config"]
    (ROOT / "internal/sqlite3/symbols.h").write_text(
        "/* Code generated by scripts/generate-cgo.py; DO NOT EDIT. */\n"
        "static uintptr_t linked_symbol(const char *name) {\n" +
        "\n".join(f' if (!strcmp(name, "{name}")) return (uintptr_t)&{name};' for name in symbols) +
        "\n return 0;\n}\n")



if __name__ == "__main__":
    generate()
