// Generate compiler-typed trampolines from API's tagged Go function fields.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
)

const tag = "cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)"

type signature struct{ result, params string }

// Exact C signatures are independent of Go's opaque uintptr handles.
// The C compiler handles integer/double calling conventions on every target.
var signatures = map[string]signature{
	"Open":                {"int", "const char*,sqlite3**,int,const char*"},
	"Close":               {"int", "sqlite3*"},
	"Errmsg":              {"const char*", "sqlite3*"},
	"Errcode":             {"int", "sqlite3*"},
	"ExtendedErrcode":     {"int", "sqlite3*"},
	"Errstr":              {"const char*", "int"},
	"ExtendedResultCodes": {"int", "sqlite3*,int"},
	"DBReadonly":          {"int", "sqlite3*,const char*"},
	"DBFilename":          {"const char*", "sqlite3*,const char*"},
	"Prepare":             {"int", "sqlite3*,const char*,int,unsigned int,sqlite3_stmt**,const char**"},
	"Step":                {"int", "sqlite3_stmt*"},
	"Reset":               {"int", "sqlite3_stmt*"},
	"Finalize":            {"int", "sqlite3_stmt*"},
	"ClearBindings":       {"int", "sqlite3_stmt*"},
	"StmtReadonly":        {"int", "sqlite3_stmt*"},
	"SQL":                 {"const char*", "sqlite3_stmt*"},
	"BindNull":            {"int", "sqlite3_stmt*,int"},
	"BindInt64":           {"int", "sqlite3_stmt*,int,sqlite3_int64"},
	"BindDouble":          {"int", "sqlite3_stmt*,int,double"},
	"BindText":            {"int", "sqlite3_stmt*,int,const char*,int,sqlite_destructor"},
	"BindBlob":            {"int", "sqlite3_stmt*,int,const void*,int,sqlite_destructor"},
	"BindParameterCount":  {"int", "sqlite3_stmt*"},
	"BindParameterIndex":  {"int", "sqlite3_stmt*,const char*"},
	"BindParameterName":   {"const char*", "sqlite3_stmt*,int"},
	"ColumnCount":         {"int", "sqlite3_stmt*"},
	"ColumnName":          {"const char*", "sqlite3_stmt*,int"},
	"ColumnDecltype":      {"const char*", "sqlite3_stmt*,int"},
	"ColumnType":          {"int", "sqlite3_stmt*,int"},
	"ColumnInt64":         {"sqlite3_int64", "sqlite3_stmt*,int"},
	"ColumnDouble":        {"double", "sqlite3_stmt*,int"},
	"ColumnText":          {"const unsigned char*", "sqlite3_stmt*,int"},
	"ColumnBlob":          {"const void*", "sqlite3_stmt*,int"},
	"ColumnBytes":         {"int", "sqlite3_stmt*,int"},
	"Changes":             {"int", "sqlite3*"},
	"Changes64":           {"sqlite3_int64", "sqlite3*"},
	"LastInsertRowid":     {"sqlite3_int64", "sqlite3*"},
	"Autocommit":          {"int", "sqlite3*"},
	"BusyTimeout":         {"int", "sqlite3*,int"},
	"Limit":               {"int", "sqlite3*,int,int"},
	"Interrupt":           {"void", "sqlite3*"},
	"Libversion":          {"const char*", ""},
	"LibversionNumber":    {"int", ""},
	"Threadsafe":          {"int", ""},
	"CompileOptionUsed":   {"int", "const char*"},
	"CompileOptionGet":    {"const char*", "int"},
	"Free":                {"void", "void*"},
	"Malloc":              {"void*", "int"},
	"EnableLoadExtension": {"int", "sqlite3*,int"},
	"LoadExtension":       {"int", "sqlite3*,const char*,const char*,char**"},
	"BackupInit":          {"sqlite3_backup*", "sqlite3*,const char*,sqlite3*,const char*"},
	"BackupStep":          {"int", "sqlite3_backup*,int"},
	"BackupFinish":        {"int", "sqlite3_backup*"},
	"CreateFunction":      {"int", "sqlite3*,const char*,int,int,void*,sqlite_scalar_callback,sqlite_scalar_callback,sqlite_final_callback,sqlite_destroy_callback"},
	"CreateCollation":     {"int", "sqlite3*,const char*,int,void*,sqlite_compare_callback,sqlite_destroy_callback"},
	"UserData":            {"void*", "sqlite3_context*"},
	"AggregateContext":    {"void*", "sqlite3_context*,int"},
	"ValueType":           {"int", "sqlite3_value*"},
	"ValueInt64":          {"sqlite3_int64", "sqlite3_value*"},
	"ValueDouble":         {"double", "sqlite3_value*"},
	"ValueText":           {"const unsigned char*", "sqlite3_value*"},
	"ValueBlob":           {"const void*", "sqlite3_value*"},
	"ValueBytes":          {"int", "sqlite3_value*"},
	"ResultNull":          {"void", "sqlite3_context*"},
	"ResultInt64":         {"void", "sqlite3_context*,sqlite3_int64"},
	"ResultDouble":        {"void", "sqlite3_context*,double"},
	"ResultText":          {"void", "sqlite3_context*,const char*,int,sqlite_destructor"},
	"ResultBlob":          {"void", "sqlite3_context*,const void*,int,sqlite_destructor"},
	"ResultError":         {"void", "sqlite3_context*,const char*,int"},
}

var bridge = map[string]string{"uintptr": "uintptr_t", "int32": "int", "uint32": "unsigned int", "int64": "sqlite3_int64", "float64": "double", "string": "const char*", "*uintptr": "uintptr_t*", "*byte": "void*", "**byte": "char**", "": "void"}
var goCast = map[string]string{"uintptr": "C.uintptr_t", "int32": "C.int", "uint32": "C.uint", "int64": "C.sqlite3_int64", "float64": "C.double"}

type field struct {
	name, symbol, result string
	params               []string
}

func apiFields(filename string) ([]field, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, err
	}
	var fields []field
	var parseErr error
	ast.Inspect(f, func(n ast.Node) bool {
		t, ok := n.(*ast.TypeSpec)
		if !ok || t.Name.Name != "API" {
			return true
		}
		s, ok := t.Type.(*ast.StructType)
		if !ok {
			parseErr = fmt.Errorf("API is not a struct")
			return false
		}
		typeName := func(expr ast.Expr) string {
			var b bytes.Buffer
			if err := format.Node(&b, fset, expr); err != nil {
				parseErr = err
			}
			return b.String()
		}
		for _, f := range s.Fields.List {
			if f.Tag == nil {
				continue
			}
			raw, err := strconv.Unquote(f.Tag.Value)
			if err != nil {
				parseErr = err
				return false
			}
			symbol, _, _ := strings.Cut(reflect.StructTag(raw).Get("sqlite"), ",")
			if symbol == "" {
				continue
			}
			fn, ok := f.Type.(*ast.FuncType)
			if !ok || len(f.Names) != 1 {
				parseErr = fmt.Errorf("invalid SQLite API field")
				return false
			}
			x := field{name: f.Names[0].Name, symbol: symbol}
			for _, p := range fn.Params.List {
				if len(p.Names) > 0 {
					parseErr = fmt.Errorf("tagged function parameters must be unnamed")
					return false
				}
				x.params = append(x.params, typeName(p.Type))
			}
			if fn.Results != nil {
				if len(fn.Results.List) != 1 {
					parseErr = fmt.Errorf("tagged function has multiple results")
					return false
				}
				x.result = typeName(fn.Results.List[0].Type)
			}
			fields = append(fields, x)
		}
		return false
	})
	if parseErr != nil {
		return nil, parseErr
	}
	if len(fields) != len(signatures) {
		return nil, fmt.Errorf("API fields and C signatures differ; update signatures")
	}
	return fields, nil
}

func generate(root string) error {
	fields, err := apiFields(filepath.Join(root, "internal/ffi/api.go"))
	if err != nil {
		return err
	}
	c := []string{"#include <stdint.h>", "#include <stdlib.h>", "typedef struct sqlite3 sqlite3;", "typedef struct sqlite3_stmt sqlite3_stmt;", "typedef struct sqlite3_backup sqlite3_backup;", "typedef long long sqlite3_int64;", "typedef void (*sqlite_destructor)(void*);", `_Static_assert(sizeof(int) == 4, "SQLite C int must be 32 bits");`, `_Static_assert(sizeof(sqlite3_int64) == 8, "SQLite integer must be 64 bits");`, `_Static_assert(sizeof(uintptr_t) == sizeof(void*), "handle width must match pointers");`}
	c = append(c, "typedef struct sqlite3_context sqlite3_context;", "typedef struct sqlite3_value sqlite3_value;", "typedef void (*sqlite_scalar_callback)(sqlite3_context*,int,sqlite3_value**);", "typedef void (*sqlite_final_callback)(sqlite3_context*);", "typedef void (*sqlite_destroy_callback)(void*);", "typedef int (*sqlite_compare_callback)(void*,int,const void*,int,const void*);")
	g := []string{"func registerAPI(a *API, _ any, name string, symbol uintptr) {", "switch name {"}
	for _, f := range fields {
		s, ok := signatures[f.name]
		if !ok {
			return fmt.Errorf("missing C signature for %s", f.name)
		}
		var nativeTypes []string
		if s.params != "" {
			nativeTypes = strings.Split(s.params, ",")
		}
		if len(nativeTypes) != len(f.params) {
			return fmt.Errorf("parameter count mismatch for %s", f.name)
		}
		var args, castArgs, bridgeParams, goParams []string
		for i, t := range f.params {
			arg := fmt.Sprintf("p%d", i)
			bt, ok := bridge[t]
			if !ok {
				return fmt.Errorf("unknown bridge type %s", t)
			}
			args = append(args, arg)
			castArgs = append(castArgs, "("+nativeTypes[i]+")"+arg)
			bridgeParams = append(bridgeParams, bt+" "+arg)
			goParams = append(goParams, arg+" "+t)
		}
		nativeParams := s.params
		if nativeParams == "" {
			nativeParams = "void"
		}
		nativeFn := "((" + s.result + " (*)(" + nativeParams + "))fn)"
		var prefix, suffix []string
		switch f.name {
		case "Open":
			prefix = []string{"sqlite3 *db = NULL;"}
			castArgs[1] = "&db"
			suffix = []string{"*p1 = (uintptr_t)db;"}
		case "Prepare":
			prefix = []string{"sqlite3_stmt *stmt = NULL;", "const char *tail = NULL;"}
			castArgs[4], castArgs[5] = "&stmt", "&tail"
			bridgeParams[5] = "int *p5"
			suffix = []string{"*p4 = (uintptr_t)stmt;", "*p5 = tail ? (int)(tail - (const char*)p1) : 0;"}
		}
		call := nativeFn + "(" + strings.Join(castArgs, ", ") + ")"
		var body []string
		if len(suffix) > 0 {
			body = append(body, prefix...)
			body = append(body, "int result = "+call+";")
			body = append(body, suffix...)
			body = append(body, "return result;")
		} else if f.result != "" {
			body = []string{"return (" + bridge[f.result] + ")" + call + ";"}
		} else {
			body = []string{call + ";"}
		}
		params := ""
		if len(bridgeParams) > 0 {
			params = ", " + strings.Join(bridgeParams, ", ")
		}
		c = append(c, "static "+bridge[f.result]+" call_"+f.symbol+"(uintptr_t fn"+params+") {")
		c = append(c, body...)
		c = append(c, "}")
		result := ""
		if f.result != "" {
			result = " " + f.result
		}
		g = append(g, "case "+strconv.Quote(f.symbol)+":")
		g = append(g, "a."+f.name+" = func("+strings.Join(goParams, ", ")+")"+result+" {")
		var conversions []string
		for i, t := range f.params {
			arg := args[i]
			switch t {
			case "string":
				g = append(g, "c"+arg+" := C.CString("+arg+")", "defer C.free(unsafe.Pointer(c"+arg+"))")
				conversions = append(conversions, "c"+arg)
			case "*uintptr":
				conversions = append(conversions, "(*C.uintptr_t)(unsafe.Pointer("+arg+"))")
			case "*byte":
				conversions = append(conversions, "unsafe.Pointer("+arg+")")
			case "**byte":
				if f.name == "Prepare" {
					g = append(g, "var offset C.int")
					conversions = append(conversions, "&offset")
				} else {
					g = append(g, "var c"+arg+" *C.char")
					conversions = append(conversions, "&c"+arg)
				}
			default:
				cast, ok := goCast[t]
				if !ok {
					return fmt.Errorf("unknown Go cast %s", t)
				}
				conversions = append(conversions, cast+"("+arg+")")
			}
		}
		params = ""
		if len(conversions) > 0 {
			params = ", " + strings.Join(conversions, ", ")
		}
		call = "C.call_" + f.symbol + "(C.uintptr_t(symbol)" + params + ")"
		if f.result != "" {
			call = "r := " + call
		}
		g = append(g, call)
		if f.name == "Prepare" {
			g = append(g, "*p5 = (*byte)(unsafe.Add(unsafe.Pointer(p1), int(offset)))")
		}
		if f.name == "LoadExtension" {
			g = append(g, "*p3 = (*byte)(unsafe.Pointer(cp3))")
		}
		switch f.result {
		case "string":
			g = append(g, "return C.GoString(r)")
		case "*byte":
			g = append(g, "return (*byte)(unsafe.Pointer(r))")
		case "":
		default:
			g = append(g, "return "+f.result+"(r)")
		}
		g = append(g, "}")
	}
	g = append(g, `default: panic("sqlite: missing CGO binding for " + name)`, "}", "}")
	c = append(c, `// Use the real C variadic ABI, including on 32-bit MIPS and Apple arm64.
static int config_extension(uintptr_t fn, uintptr_t db, int enabled) {
 return ((int (*)(sqlite3*, int, ...))fn)((sqlite3*)db, 1005, enabled, (int*)NULL);
}`)
	g = append(g, `func extensionConfig(h uintptr) func(uintptr, int32) int32 {
 p, err := loader.Symbol(h, "sqlite3_db_config")
 if err != nil { return nil }
 return func(db uintptr, enabled int32) int32 { return int32(C.config_extension(C.uintptr_t(p), C.uintptr_t(db), C.int(enabled))) }
}`)
	source := "// Code generated by go generate; DO NOT EDIT.\n//go:build " + tag + "\n\npackage ffi\n\n/*\n" + strings.Join(c, "\n") + "\n*/\nimport \"C\"\n\nimport (\n\"unsafe\"\n\"github.com/yuhaiin/purego-sqlite/internal/loader\"\n)\n\n" + strings.Join(g, "\n") + "\n"
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return err
	}
	if err := sqlitesource.WriteFile(filepath.Join(root, "internal/ffi/register_cgo.go"), formatted); err != nil {
		return err
	}
	var symbols strings.Builder
	symbols.WriteString("/* Code generated by go generate; DO NOT EDIT. */\nstatic uintptr_t linked_symbol(const char *name) {\n")
	for _, f := range fields {
		fmt.Fprintf(&symbols, " if (!strcmp(name, %q)) return (uintptr_t)&%s;\n", f.symbol, f.symbol)
	}
	symbols.WriteString(" if (!strcmp(name, \"sqlite3_db_config\")) return (uintptr_t)&sqlite3_db_config;\n return 0;\n}\n")
	if err := sqlitesource.WriteFile(filepath.Join(root, "internal/sqlite3/symbols.h"), []byte(symbols.String())); err != nil {
		return err
	}
	return generateCallbackSlots(root)
}

// A context callback has no library argument. Each fixed engine slot uses that
// engine's own sqlite3_user_data, never another SQLite version's private layout.
func generateCallbackSlots(root string) error {
	var out strings.Builder
	out.WriteString(`/* Code generated by go generate; DO NOT EDIT. */
#include <stdint.h>
typedef struct sqlite3_context sqlite3_context;
typedef struct sqlite3_value sqlite3_value;
extern void goSQLiteScalar(uintptr_t, uintptr_t, int, uintptr_t);
extern void goSQLiteStep(uintptr_t, uintptr_t, int, uintptr_t);
extern void goSQLiteFinal(uintptr_t, uintptr_t);
extern int goSQLiteCompare(uintptr_t, int, uintptr_t, int, uintptr_t);
extern void goSQLiteDestroy(uintptr_t);
static uintptr_t callback_userdata[64];
static void set_callback_userdata(int slot, uintptr_t fn) { callback_userdata[slot] = fn; }
static void destroy_callback(void *token) { goSQLiteDestroy((uintptr_t)token); }
static int compare_callback(void *token, int n1, const void *p1, int n2, const void *p2) {
 return goSQLiteCompare((uintptr_t)token, n1, (uintptr_t)p1, n2, (uintptr_t)p2);
}
static uintptr_t destroy_callback_address(void) { return (uintptr_t)&destroy_callback; }
static uintptr_t compare_callback_address(void) { return (uintptr_t)&compare_callback; }
`)
	for slot := range 64 {
		for _, kind := range []string{"Scalar", "Step"} {
			fmt.Fprintf(&out, "static void %s_%d(sqlite3_context *ctx, int argc, sqlite3_value **argv) { goSQLite%s((uintptr_t)((void*(*)(sqlite3_context*))callback_userdata[%d])(ctx), (uintptr_t)ctx, argc, (uintptr_t)argv); }\n", strings.ToLower(kind), slot, kind, slot)
		}
		fmt.Fprintf(&out, "static void final_%d(sqlite3_context *ctx) { goSQLiteFinal((uintptr_t)((void*(*)(sqlite3_context*))callback_userdata[%d])(ctx), (uintptr_t)ctx); }\n", slot, slot)
	}
	for _, kind := range []string{"scalar", "step", "final"} {
		fmt.Fprintf(&out, "static const uintptr_t %s_callbacks[64] = {", kind)
		for slot := range 64 {
			fmt.Fprintf(&out, "(uintptr_t)&%s_%d,", kind, slot)
		}
		fmt.Fprintf(&out, "};\nstatic uintptr_t %s_callback_address(int slot) { return %s_callbacks[slot]; }\n", kind, kind)
	}
	return sqlitesource.WriteFile(filepath.Join(root, "internal/ffi/callback_slots.h"), []byte(out.String()))
}

func main() {
	if err := generate("."); err != nil {
		log.Fatal(err)
	}
}
