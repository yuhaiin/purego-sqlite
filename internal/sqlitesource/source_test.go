package sqlitesource

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"crypto/sha3"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func archiveFor(t *testing.T, version string) ([]byte, Pin) {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range Files {
		f, err := z.Create("sqlite-amalgamation-3550000/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(f, "/* official source */\n#define SQLITE_VERSION %q\n", version); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	return b, Pin{Version: version, URL: "https://www.sqlite.org/2026/sqlite-amalgamation-3550000.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(b)), SHA3: fmt.Sprintf("%x", sha3.Sum256(b))}
}

func TestDiscoverStableNumericRelease(t *testing.T) {
	digest := strings.Repeat("a", 64)
	page := fmt.Sprintf("PRODUCT,VERSION,RELATIVE-URL,SIZE-IN-BYTES,SHA3-HASH\nPRODUCT,3.9.0,2020/sqlite-amalgamation-3090000.zip,100,%s\nPRODUCT,3.54.1.2,2026/sqlite-amalgamation-3540102.zip,100,%s,appended\nPRODUCT,3.55.0-beta,2026/sqlite-amalgamation-3550000.zip,100,%s\nPRODUCT,99.0.0,https://evil.example/2026/sqlite-amalgamation-9990000.zip,100,%s\nPRODUCT,99.0.0,2026/../sqlite-amalgamation-9990000.zip,100,%s", digest, digest, digest, digest, digest)
	r, err := Discover([]byte(page))
	if err != nil || r.Version != "3.54.1.2" {
		t.Fatalf("%+v %v", r, err)
	}
	cmp, err := Compare("3.54.1", "3.54.1.0")
	if err != nil || cmp != 0 {
		t.Fatalf("%d %v", cmp, err)
	}
	for _, page := range []string{"", "PRODUCT,3.54.1,2026/sqlite-amalgamation-3540100.zip,100,invalid"} {
		if _, err := Discover([]byte(page)); err == nil {
			t.Fatal("accepted invalid stable metadata")
		}
	}
}

func TestCheckUpdateDecision(t *testing.T) {
	for _, test := range []struct {
		name, latest              string
		force, changed, wantError bool
		reason                    string
	}{
		{"same", "3.54.1", false, false, false, "up-to-date"},
		{"same-normalized", "3.54.1.0", false, false, false, "up-to-date"},
		{"forced", "3.54.1", true, true, false, "forced-rebuild"},
		{"new", "3.55.0", false, true, false, "new-release"},
		{"old", "3.9.0", true, false, true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, pin := archiveFor(t, test.latest)
			var archives atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/download.html" {
					fmt.Fprintf(w, "PRODUCT,%s,2026/sqlite-amalgamation-3550000.zip,%d,%s", test.latest, len(data), pin.SHA3)
					return
				}
				archives.Add(1)
				_, _ = w.Write(data)
			}))
			defer s.Close()
			current := Pin{Version: "3.54.1"}
			d, err := Check(t.Context(), s.Client(), s.URL+"/download.html", current, test.force)
			if (err != nil) != test.wantError {
				t.Fatal(err)
			}
			if test.wantError {
				if archives.Load() != 0 {
					t.Fatal("downgrade downloaded an archive")
				}
				return
			}
			if d.Changed != test.changed || d.Reason != test.reason || d.CurrentVersion != current.Version {
				t.Fatalf("%+v", d)
			}
			if d.UpdateAvailable {
				if d.Pin != pin || archives.Load() != 1 {
					t.Fatalf("new source not verified: %+v", d)
				}
			} else if archives.Load() != 0 || d.Pin != current {
				t.Fatal("unchanged version downloaded source or modified the pin")
			}
		})
	}
}

func TestRejectTamperedOrMismatchedSource(t *testing.T) {
	data, pin := archiveFor(t, "3.55.0")
	if _, err := VerifyArchive(pin, append(bytes.Clone(data), 'x')); err == nil {
		t.Fatal("accepted corrupt archive")
	}
	pin.Version = "3.55.1"
	if _, err := VerifyArchive(pin, data); err == nil {
		t.Fatal("accepted wrong source header version")
	}
	for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/download.html" {
				fmt.Fprintf(w, "PRODUCT,3.55.0,2026/sqlite-amalgamation-3550000.zip,%d,%s", len(data), pin.SHA3)
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte("corruption"))
		}))
		_, err := Check(t.Context(), s.Client(), s.URL+"/download.html", Pin{Version: "3.54.1"}, false)
		s.Close()
		if err == nil {
			t.Fatal("accepted corrupt/failed source download")
		}
	}
}

func TestWriteFileSkipsUnchangedPin(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "source.json")
	data := []byte("exact original bytes\n")
	if err := WriteFile(filename, data); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filename, data); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("unchanged source pin was rewritten")
	}
}
