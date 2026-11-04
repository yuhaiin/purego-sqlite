// Package sqlitesource verifies SQLite's official stable-release source pins.
package sqlitesource

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const DownloadURL = "https://www.sqlite.org/download.html"

var sourcePath = regexp.MustCompile(`^\d{4}/sqlite-amalgamation-\d+\.zip$`)
var stableVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:\.\d+)?$`)
var sourceVersion = regexp.MustCompile(`#define\s+SQLITE_VERSION\s+"([^"]+)"`)
var Files = []string{"sqlite3.c", "sqlite3.h", "sqlite3ext.h"}

type Pin struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	SHA3    string `json:"sha3_256"`
}

type Release struct {
	Version string
	Path    string
	Size    int64
	SHA3    string
}

type Decision struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	Version         string `json:"version"`
	UpdateAvailable bool   `json:"update_available"`
	Changed         bool   `json:"changed"`
	Reason          string `json:"reason"`
	Pin             Pin    `json:"-"`
}

func Compare(a, b string) (int, error) {
	parse := func(s string) ([4]int, error) {
		var v [4]int
		if !stableVersion.MatchString(s) {
			return v, fmt.Errorf("invalid stable SQLite version %q", s)
		}
		for i, part := range strings.Split(s, ".") {
			n, err := strconv.Atoi(part)
			if err != nil {
				return v, err
			}
			v[i] = n
		}
		return v, nil
	}
	x, err := parse(a)
	if err != nil {
		return 0, err
	}
	y, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, nil
		}
		if x[i] > y[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

func Discover(page []byte) (Release, error) {
	var latest Release
	for line := range strings.SplitSeq(string(page), "\n") {
		if !strings.HasPrefix(line, "PRODUCT,") {
			continue
		}
		fields, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil {
			return Release{}, err
		}
		// SQLite permits new metadata columns to be appended in future.
		if len(fields) < 5 || !stableVersion.MatchString(fields[1]) || !sourcePath.MatchString(fields[2]) {
			continue
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || size <= 0 || !validHash(fields[4]) {
			return Release{}, errors.New("invalid official source size or SHA3-256")
		}
		candidate := Release{fields[1], fields[2], size, fields[4]}
		if latest.Version == "" {
			latest = candidate
			continue
		}
		cmp, err := Compare(candidate.Version, latest.Version)
		if err != nil {
			return Release{}, err
		}
		if cmp > 0 {
			latest = candidate
		}
	}
	if latest.Version == "" {
		return Release{}, errors.New("no stable amalgamation in official metadata")
	}
	return latest, nil
}

func ReadPin(filename string) (Pin, error) {
	var pin Pin
	b, err := os.ReadFile(filename)
	if err != nil {
		return pin, err
	}
	if err := json.Unmarshal(b, &pin); err != nil {
		return pin, err
	}
	_, err = Compare(pin.Version, pin.Version)
	if err != nil {
		return pin, err
	}
	if !strings.HasPrefix(pin.URL, "https://www.sqlite.org/") || !sourcePath.MatchString(strings.TrimPrefix(pin.URL, "https://www.sqlite.org/")) || !validHash(pin.SHA256) || !validHash(pin.SHA3) {
		return pin, errors.New("invalid official SQLite source pin")
	}
	return pin, nil
}

func Fetch(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", address, response.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 64<<20 {
		return nil, errors.New("SQLite download exceeds 64 MiB")
	}
	return b, nil
}

func VerifyArchive(pin Pin, data []byte) (map[string][]byte, error) {
	if fmt.Sprintf("%x", sha256.Sum256(data)) != pin.SHA256 || fmt.Sprintf("%x", sha3.Sum256(data)) != pin.SHA3 {
		return nil, errors.New("official SQLite source checksum mismatch")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSuffix(path.Base(pin.URL), ".zip") + "/"
	files := make(map[string][]byte, len(Files))
	for _, name := range Files {
		f, err := z.Open(prefix + name)
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		closeErr := f.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		if len(content) > 64<<20 {
			return nil, fmt.Errorf("%s exceeds 64 MiB", name)
		}
		if name != "sqlite3ext.h" {
			match := sourceVersion.FindSubmatch(content)
			if len(match) != 2 || string(match[1]) != pin.Version {
				return nil, fmt.Errorf("%s header version differs from source pin", name)
			}
		}
		files[name] = content
	}
	return files, nil
}

func Check(ctx context.Context, client *http.Client, metadataURL string, current Pin, force bool) (Decision, error) {
	page, err := Fetch(ctx, client, metadataURL)
	if err != nil {
		return Decision{}, err
	}
	latest, err := Discover(page)
	if err != nil {
		return Decision{}, err
	}
	cmp, err := Compare(latest.Version, current.Version)
	if err != nil {
		return Decision{}, err
	}
	if cmp < 0 {
		return Decision{}, errors.New("official metadata is older than the pin; refusing downgrade")
	}
	d := Decision{CurrentVersion: current.Version, LatestVersion: latest.Version, Version: current.Version, Changed: force, Reason: "up-to-date", Pin: current}
	if force {
		d.Reason = "forced-rebuild"
	}
	if cmp == 0 {
		return d, nil
	}
	base, err := url.Parse(metadataURL)
	if err != nil {
		return Decision{}, err
	}
	data, err := Fetch(ctx, client, base.ResolveReference(&url.URL{Path: "/" + latest.Path}).String())
	if err != nil {
		return Decision{}, err
	}
	if int64(len(data)) != latest.Size || fmt.Sprintf("%x", sha3.Sum256(data)) != latest.SHA3 {
		return Decision{}, errors.New("downloaded official source size/SHA3-256 mismatch")
	}
	d.Pin = Pin{Version: latest.Version, URL: "https://www.sqlite.org/" + latest.Path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), SHA3: latest.SHA3}
	if _, err := VerifyArchive(d.Pin, data); err != nil {
		return Decision{}, err
	}
	d.Version, d.UpdateAvailable, d.Changed, d.Reason = latest.Version, true, true, "new-release"
	return d, nil
}

func WriteFile(filename string, data []byte) error {
	old, err := os.ReadFile(filename)
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".sqlite-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	modeErr := f.Chmod(0o644)
	closeErr := f.Close()
	if err := errors.Join(writeErr, modeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

func WriteJSON(filename string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(filename, append(b, '\n'))
}

func (d Decision) Report() error {
	if filename := os.Getenv("GITHUB_OUTPUT"); filename != "" {
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintf(f, "changed=%t\nupdate_available=%t\ncurrent_version=%s\nlatest_version=%s\nversion=%s\nreason=%s\n", d.Changed, d.UpdateAvailable, d.CurrentVersion, d.LatestVersion, d.Version, d.Reason)
		if err := errors.Join(writeErr, f.Close()); err != nil {
			return err
		}
	}
	if filename := os.Getenv("GITHUB_STEP_SUMMARY"); filename != "" {
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		message := map[string]string{"up-to-date": "No new release. Build and PR publication are skipped.", "forced-rebuild": "Manual rebuild requested; the SQLite version is unchanged.", "new-release": "A newer stable release was verified; update validation will run.", "build-inputs-changed": "Validate changed build inputs with the pinned SQLite version; do not publish a PR."}[d.Reason]
		_, writeErr := fmt.Fprintf(f, "### SQLite release check\n\nPinned: %s; upstream stable: %s.\n\n%s\n", d.CurrentVersion, d.LatestVersion, message)
		if err := errors.Join(writeErr, f.Close()); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(d)
}
