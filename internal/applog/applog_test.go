package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogOnOffRotateTail(t *testing.T) {
	dir := t.TempDir()
	Setup(dir, false)
	L().Info("hidden", "url", "https://a/x")
	if _, err := os.Stat(filepath.Join(dir, fileName)); err == nil {
		t.Fatal("a log file was written while logging was off")
	}
	SetEnabled(true)
	L().Info("add", "url", "https://example.com/a b.mp4", "id", "abc")
	lines, err := Tail(10)
	if err != nil || len(lines) != 1 || !strings.Contains(lines[0], `url="https://example.com/a b.mp4"`) || !strings.Contains(lines[0], "msg=add") {
		t.Fatalf("tail: %q %v", lines, err)
	}

	// Rotation keeps the total bounded and Tail still sees the newest lines.
	big := strings.Repeat("x", 64<<10)
	for i := 0; i < 200; i++ { // ~12.8 MB
		L().Info("fill", "pad", big)
	}
	L().Info("last")
	for _, f := range []string{fileName, "odm.1.log", "odm.2.log"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil || fi.Size() > maxSize+(70<<10) {
			t.Fatalf("%s: %v %v", f, fi, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "odm.3.log")); err == nil {
		t.Fatal("kept more than 2 old files")
	}
	lines, _ = Tail(3)
	if len(lines) != 3 || !strings.Contains(lines[2], "msg=last") {
		t.Fatalf("tail after rotation: %d lines, last %q", len(lines), lines[len(lines)-1][:40])
	}
	SetEnabled(false)
}
