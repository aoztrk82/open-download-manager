package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Prefs are program-wide user choices made in the UI (not daemon flags).
type Prefs struct {
	// Lang is the UI language ("en", "tr"); empty = follow the system.
	Lang string `json:"lang,omitempty"`
}

// Languages the Web UI and the tray have strings for.
var Languages = []string{"en", "tr"}

func KnownLang(l string) bool {
	for _, x := range Languages {
		if x == l {
			return true
		}
	}
	return false
}

var prefsMu sync.Mutex

func prefsPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "prefs.json"), nil
}

// LoadPrefs reads prefs.json; a missing or broken file gives zero Prefs.
func LoadPrefs() Prefs {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	var p Prefs
	if path, err := prefsPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &p)
		}
	}
	return p
}

// SavePrefs writes prefs.json atomically (temp file + rename).
func SavePrefs(p Prefs) error {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	path, err := prefsPath()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
