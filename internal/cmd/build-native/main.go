// Build checksum-pinned native SQLite and optional Go embed fixtures.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/format"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
)

var compileFlags = []string{"-DSQLITE_THREADSAFE=1", "-DSQLITE_ENABLE_FTS5", "-DSQLITE_ENABLE_RTREE", "-DSQLITE_ENABLE_DBSTAT_VTAB", "-DSQLITE_ENABLE_MATH_FUNCTIONS", "-DSQLITE_USE_URI=1"}

type options struct {
	embed, rowBridge, execBridge bool
	goos, arch, output           string
}

func main() {
	var o options
	flag.BoolVar(&o.embed, "embed", false, "generate an optional sqlite_bundle fixture")
	flag.BoolVar(&o.rowBridge, "row-bridge", false, "include optional row-reader helper")
	flag.BoolVar(&o.execBridge, "exec-bridge", false, "include optional scalar-execution helper")
	flag.StringVar(&o.goos, "goos", runtime.GOOS, "target GOOS")
	flag.StringVar(&o.arch, "goarch", runtime.GOARCH, "target GOARCH")
	flag.StringVar(&o.output, "output", ".native", "output directory")
	flag.Parse()
	if err := build(o); err != nil {
		log.Fatal(err)
	}
}

// CC commonly contains a compiler plus flags, including quoted executable paths.
func commandWords(s string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	started := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in CC")
	}
	if started {
		words = append(words, word.String())
	}
	if len(words) == 0 || words[0] == "" {
		return nil, fmt.Errorf("empty C compiler command")
	}
	return words, nil
}

func run(args []string) error {
	fmt.Printf("%q\n", args)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func build(o options) error {
	pin, err := sqlitesource.ReadPin("scripts/source.json")
	if err != nil {
		return err
	}
	out, err := filepath.Abs(o.output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(out, path.Base(pin.URL))
	data, err := os.ReadFile(archive)
	if os.IsNotExist(err) {
		data, err = sqlitesource.Fetch(context.Background(), &http.Client{Timeout: time.Minute}, pin.URL)
	}
	if err != nil {
		return err
	}
	files, err := sqlitesource.VerifyArchive(pin, data)
	if err != nil {
		return err
	}
	if err := sqlitesource.WriteFile(archive, data); err != nil {
		return err
	}
	src := filepath.Join(out, "src")
	for _, name := range sqlitesource.Files {
		if err := sqlitesource.WriteFile(filepath.Join(src, name), files[name]); err != nil {
			return err
		}
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
		if o.goos == "windows" {
			cc = "clang"
		}
	}
	compiler, err := commandWords(cc)
	if err != nil {
		return err
	}
	library, extension := "libsqlite3.so", "test_extension.so"
	if o.goos == "darwin" {
		library = "libsqlite3.dylib"
	}
	if o.goos == "windows" {
		library, extension = "sqlite3.dll", "test_extension.dll"
	}
	shared := "-shared"
	if o.goos == "darwin" {
		shared = "-dynamiclib"
	}
	common := []string{"-O2", shared}
	if o.goos != "windows" {
		common = append(common, "-fPIC")
	}
	libs := []string{"-lm", "-lpthread"}
	if o.goos == "linux" {
		libs = append(libs, "-ldl")
	}
	if o.goos == "windows" {
		libs = nil
	}
	flags := append([]string{}, compileFlags...)
	if o.goos == "windows" {
		flags = append(flags, "-DSQLITE_API=__declspec(dllexport)")
	}
	var bridge []string
	if o.rowBridge || o.execBridge {
		bridge = append(bridge, "-I"+src)
		if o.rowBridge {
			bridge = append(bridge, "tests/testdata/row.c")
		}
		if o.execBridge {
			bridge = append(bridge, "tests/testdata/exec.c")
		}
		if o.goos == "linux" || o.goos == "freebsd" {
			bridge = append(bridge, "-Wl,-Bsymbolic")
		}
	}
	args := append([]string{}, compiler...)
	args = append(args, common...)
	args = append(args, flags...)
	args = append(args, filepath.Join(src, "sqlite3.c"))
	args = append(args, bridge...)
	args = append(args, "-o", filepath.Join(out, library))
	args = append(args, libs...)
	if err := run(args); err != nil {
		return err
	}
	args = append([]string{}, compiler...)
	args = append(args, common...)
	args = append(args, "-I"+src, "tests/testdata/extension.c", "-o", filepath.Join(out, extension))
	if err := run(args); err != nil {
		return err
	}
	data, err = os.ReadFile(filepath.Join(out, library))
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	args = append(append([]string{}, compiler...), "--version")
	version, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return err
	}
	rowABI, execABI := 0, 0
	if o.rowBridge {
		rowABI = 1
	}
	if o.execBridge {
		execABI = 1
	}
	m := map[string]any{"sqlite": pin.Version, "source": pin.URL, "source_sha256": pin.SHA256, "source_sha3_256": pin.SHA3, "library_sha256": digest, "goos": o.goos, "goarch": o.arch, "flags": flags, "row_bridge_abi": rowABI, "exec_bridge_abi": execABI, "compiler": strings.SplitN(string(version), "\n", 2)[0]}
	if err := sqlitesource.WriteJSON(filepath.Join(out, "manifest.json"), m); err != nil {
		return err
	}
	if o.embed {
		asset := o.goos + "_" + o.arch + "_" + library
		if err := sqlitesource.WriteFile(filepath.Join("internal/native/assets", asset), data); err != nil {
			return err
		}
		source := fmt.Sprintf(`// Code generated by build-native; DO NOT EDIT.
//go:build sqlite_bundle && %s && %s

package native

import _ "embed"

//go:embed assets/%s
var platformAsset []byte

func init() {
 asset = platformAsset
 digest = %q
 filename = %q
}
`, o.goos, o.arch, asset, digest, library)
		formatted, err := format.Source([]byte(source))
		if err != nil {
			return err
		}
		if err := sqlitesource.WriteFile(filepath.Join("internal/native", "asset_"+o.goos+"_"+o.arch+".go"), formatted); err != nil {
			return err
		}
	}
	fmt.Println("Native build manifest: " + filepath.Join(out, "manifest.json"))
	return nil
}
