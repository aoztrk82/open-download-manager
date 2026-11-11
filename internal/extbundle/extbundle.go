// Package extbundle carries the browser extensions inside odm.exe, so the
// extension a user installs always matches the program they run.
//
// files/chrome        unpacked Chrome/Edge build, copied here by `pnpm build`
// files/odm-firefox.xpi  Mozilla-signed (unlisted) Firefox build, copied by `pnpm sign`
//
// Both are optional: a Go-only build still compiles and reports "not bundled".
package extbundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed all:files
var files embed.FS

const (
	chromeRoot = "files/chrome"
	firefoxXPI = "files/odm-firefox.xpi"
)

type manifest struct {
	Version string `json:"version"`
}

// ChromeVersion is the bundled Chrome/Edge extension version, "" if not bundled.
func ChromeVersion() string {
	b, err := files.ReadFile(chromeRoot + "/manifest.json")
	if err != nil {
		return ""
	}
	var m manifest
	_ = json.Unmarshal(b, &m)
	return m.Version
}

// FirefoxXPI returns the signed Firefox package, or nil if not bundled.
func FirefoxXPI() []byte {
	b, err := files.ReadFile(firefoxXPI)
	if err != nil {
		return nil
	}
	return b
}

// FirefoxVersion reads the version from the bundled xpi, "" if not bundled.
func FirefoxVersion() string {
	b := FirefoxXPI()
	if b == nil {
		return ""
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return ""
	}
	f, err := zr.Open("manifest.json")
	if err != nil {
		return ""
	}
	defer f.Close()
	var m manifest
	_ = json.NewDecoder(f).Decode(&m)
	return m.Version
}

// chromeFiles lists the bundled Chrome files relative to chromeRoot.
func chromeFiles() ([]string, error) {
	var out []string
	err := fs.WalkDir(files, chromeRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, strings.TrimPrefix(p, chromeRoot+"/"))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func chromeHash(names []string) (string, error) {
	h := sha256.New()
	for _, n := range names {
		b, err := files.ReadFile(chromeRoot + "/" + n)
		if err != nil {
			return "", err
		}
		h.Write([]byte(n + "\x00"))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ErrNotBundled means this odm.exe was built without that extension.
var ErrNotBundled = errors.New("this ODM build does not include the extension")

// SyncChrome makes dir hold exactly the bundled Chrome extension. Chrome loads
// an unpacked extension from a fixed folder, so the folder is updated in place
// (write every file, then delete stale ones) rather than deleted and recreated,
// which can fail halfway on Windows while Chrome has it open. The content hash
// lives next to the folder in hashFile: Chrome refuses to load an unpacked
// extension containing names that start with "_".
// It returns whether anything was written.
func SyncChrome(dir, hashFile string) (bool, error) {
	names, err := chromeFiles()
	if err != nil || len(names) == 0 {
		return false, ErrNotBundled
	}
	sum, err := chromeHash(names)
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(hashFile); err == nil && string(old) == sum {
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil {
			return false, nil
		}
	}
	want := map[string]bool{}
	for _, n := range names {
		want[filepath.FromSlash(n)] = true
		b, err := files.ReadFile(chromeRoot + "/" + n)
		if err != nil {
			return false, err
		}
		dst := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return false, err
		}
	}
	// Remove files a previous version had but this one does not.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr == nil && !want[rel] {
			_ = os.Remove(p)
		}
		return nil
	})
	return true, os.WriteFile(hashFile, []byte(sum), 0o644)
}

// WriteFirefoxXPI saves the signed xpi into dir and returns its path.
func WriteFirefoxXPI(dir string) (string, error) {
	b := FirefoxXPI()
	if b == nil {
		return "", ErrNotBundled
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := "odm-firefox-" + FirefoxVersion() + ".xpi"
	p := filepath.Join(dir, path.Base(name))
	return p, os.WriteFile(p, b, 0o644)
}
