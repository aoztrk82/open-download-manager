package extbundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncChrome(t *testing.T) {
	if ChromeVersion() == "" {
		t.Skip("Chrome extension not bundled (run pnpm build in extension/)")
	}
	base := t.TempDir()
	dir, hash := filepath.Join(base, "chrome"), filepath.Join(base, "chrome.hash")

	wrote, err := SyncChrome(dir, hash)
	if err != nil || !wrote {
		t.Fatalf("first sync: wrote=%v err=%v", wrote, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	// Unchanged bundle: nothing is rewritten while the browser may have it open.
	if wrote, err := SyncChrome(dir, hash); err != nil || wrote {
		t.Fatalf("second sync: wrote=%v err=%v", wrote, err)
	}
	// A file from an older version is removed, a modified one restored.
	stale := filepath.Join(dir, "old.js")
	os.WriteFile(stale, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0o644)
	os.Remove(hash)
	if _, err := SyncChrome(dir, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file kept: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if len(b) < 10 {
		t.Fatalf("manifest not restored: %q", b)
	}
	// Chrome rejects unpacked extensions with names starting with "_".
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name()[0] == '_' {
			t.Fatalf("reserved name in extension folder: %s", e.Name())
		}
	}
}
