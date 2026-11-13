// Package applog is ODM's download log: what was asked (URL, where from, which
// kind), what the server answered (status, type, size) and why a download
// failed. Settings > General switches it; the file lives in <config>/logs and
// rotates, so it never grows without bound.
package applog

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	fileName = "odm.log"
	maxSize  = 5 << 20 // per file; odm.log + odm.1.log + odm.2.log
	keep     = 2
)

var (
	enabled atomic.Bool
	mu      sync.Mutex
	dir     string
	file    *os.File
	size    int64
	logger  = slog.New(gate{slog.NewTextHandler(writer{}, &slog.HandlerOptions{Level: slog.LevelDebug})})
)

// Setup points the log at dir (created when needed) and switches it.
func Setup(d string, on bool) {
	mu.Lock()
	dir = d
	closeLocked()
	mu.Unlock()
	SetEnabled(on)
}

// SetEnabled switches logging at once (Settings change).
func SetEnabled(on bool) {
	enabled.Store(on)
	if !on {
		mu.Lock()
		closeLocked()
		mu.Unlock()
	}
}

func Enabled() bool { return enabled.Load() }

// L is the logger; when logging is off it drops everything cheaply.
func L() *slog.Logger { return logger }

// Path is the current log file ("" before Setup).
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fileName)
}

// Tail returns up to n last lines of the current and previous log file.
func Tail(n int) ([]string, error) {
	p := Path()
	if p == "" {
		return nil, nil
	}
	var lines []string
	for _, f := range []string{rotated(p, 1), p} {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			lines = append(lines, sc.Text())
			if len(lines) > 4*n {
				lines = lines[len(lines)-n:]
			}
		}
		fh.Close()
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func rotated(p string, i int) string {
	return strings.TrimSuffix(p, ".log") + "." + string(rune('0'+i)) + ".log"
}

func closeLocked() {
	if file != nil {
		file.Close()
		file = nil
	}
}

// openLocked opens (or rotates) the file for a write of n bytes.
func openLocked(n int) error {
	if dir == "" {
		return os.ErrNotExist
	}
	p := filepath.Join(dir, fileName)
	if file != nil && size+int64(n) > maxSize {
		closeLocked()
		for i := keep; i >= 1; i-- {
			src := p
			if i > 1 {
				src = rotated(p, i-1)
			}
			_ = os.Rename(src, rotated(p, i))
		}
	}
	if file == nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		fi, _ := f.Stat()
		file, size = f, 0
		if fi != nil {
			size = fi.Size()
		}
	}
	return nil
}

type writer struct{}

func (writer) Write(b []byte) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	if err := openLocked(len(b)); err != nil {
		return len(b), nil // logging never breaks a download
	}
	n, err := file.Write(b)
	size += int64(n)
	return n, err
}

// gate drops records while logging is off.
type gate struct{ slog.Handler }

func (g gate) Enabled(ctx context.Context, l slog.Level) bool {
	return enabled.Load() && g.Handler.Enabled(ctx, l)
}
func (g gate) WithAttrs(a []slog.Attr) slog.Handler { return gate{g.Handler.WithAttrs(a)} }
func (g gate) WithGroup(n string) slog.Handler      { return gate{g.Handler.WithGroup(n)} }

// Discard is for code paths without a logger (tests, the CLI).
var Discard = slog.New(slog.NewTextHandler(io.Discard, nil))
