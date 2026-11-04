// Package vendoredsqlite keeps the linked amalgamation and its provenance together.
package vendoredsqlite

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
)

type manifest struct {
	Version string            `json:"version"`
	URL     string            `json:"source_url"`
	SHA256  string            `json:"source_sha256"`
	SHA3    string            `json:"source_sha3_256"`
	Files   map[string]string `json:"files"`
}

var versionConstant = regexp.MustCompile(`(?m)^const Version = "[^"]+"$`)

func Import(root string, pin sqlitesource.Pin, data []byte) error {
	files, err := sqlitesource.VerifyArchive(pin, data)
	if err != nil {
		return err
	}
	target := filepath.Join(root, "internal/sqlite3")
	native := filepath.Join(target, "native.go")
	goSource, err := os.ReadFile(native)
	if err != nil {
		return err
	}
	if len(versionConstant.FindAll(goSource, -1)) != 1 {
		return errors.New("expected exactly one linked SQLite version constant")
	}
	updated := versionConstant.ReplaceAllLiteral(goSource, []byte(fmt.Sprintf("const Version = %q", pin.Version)))
	m := manifest{Version: pin.Version, URL: pin.URL, SHA256: pin.SHA256, SHA3: pin.SHA3, Files: make(map[string]string)}
	// Check every archive entry and version before replacing any file.
	for _, name := range sqlitesource.Files {
		content := files[name]
		if err := sqlitesource.WriteFile(filepath.Join(target, name), content); err != nil {
			return err
		}
		m.Files[name] = fmt.Sprintf("%x", sha256.Sum256(content))
	}
	if err := sqlitesource.WriteFile(native, updated); err != nil {
		return err
	}
	return sqlitesource.WriteJSON(filepath.Join(target, "source.json"), m)
}

func Verify(root string, pin sqlitesource.Pin) error {
	target := filepath.Join(root, "internal/sqlite3")
	b, err := os.ReadFile(filepath.Join(target, "source.json"))
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m.Version != pin.Version || m.URL != pin.URL || m.SHA256 != pin.SHA256 || m.SHA3 != pin.SHA3 {
		return errors.New("vendored source pin does not match native build pin")
	}
	if len(m.Files) != len(sqlitesource.Files) {
		return errors.New("incomplete vendored SQLite source manifest")
	}
	for _, name := range sqlitesource.Files {
		b, err := os.ReadFile(filepath.Join(target, name))
		if err != nil {
			return err
		}
		if m.Files[name] != fmt.Sprintf("%x", sha256.Sum256(b)) {
			return fmt.Errorf("vendored source integrity failure: %s", name)
		}
	}
	b, err = os.ReadFile(filepath.Join(target, "native.go"))
	if err != nil {
		return err
	}
	if string(versionConstant.Find(b)) != fmt.Sprintf("const Version = %q", pin.Version) {
		return errors.New("linked SQLite version constant does not match source pin")
	}
	return nil
}
