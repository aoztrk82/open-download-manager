// Package ffmpeg finds and runs the ffmpeg ODM uses to join downloaded media
// pieces (HLS/DASH segments, separate audio and video) into one file.
//
// ffmpeg is a separate program, never linked into ODM. The installer ships a
// small LGPL build next to odm.exe (tools/ffmpeg/build.sh); a developer
// checkout falls back to an ffmpeg on PATH. Only stream copy ("-c copy") is
// used: no transcoding, so the small build without encoders is enough.
package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvPath overrides discovery, e.g. ODM_FFMPEG=D:\tools\ffmpeg.exe.
const EnvPath = "ODM_FFMPEG"

// ErrNotFound means no usable ffmpeg was found.
var ErrNotFound = errors.New("ffmpeg not found (reinstall ODM, or set " + EnvPath + ")")

func exeName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

// Find returns the ffmpeg to use: $ODM_FFMPEG, then the one installed next to
// odm.exe, then PATH. The copy next to the exe wins over PATH so every user
// gets the build ODM was tested with, whatever else they have installed.
func Find() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvPath)); p != "" {
		if isFile(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s=%s: no such file", EnvPath, p)
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), exeName()); isFile(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	return "", ErrNotFound
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd) // ffmpeg is a console program; the tray daemon has no console
	return cmd
}

// Version returns ffmpeg's first "-version" line, e.g. "ffmpeg version 9.0.2 ...".
func Version(ctx context.Context, path string) (string, error) {
	out, err := command(ctx, path, "-hide_banner", "-version").Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), nil
}

// RemuxArgs builds the ffmpeg arguments that copy every stream of all inputs
// into output. An .m3u8 input is a local HLS playlist whose segments (and AES
// key) ODM already downloaded; it may only read local files.
func RemuxArgs(inputs []string, output string) []string {
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}
	for _, in := range inputs {
		if strings.EqualFold(filepath.Ext(in), ".m3u8") {
			args = append(args, "-allowed_extensions", "ALL", "-protocol_whitelist", "file,crypto")
		}
		args = append(args, "-i", in)
	}
	for i := range inputs {
		args = append(args, "-map", fmt.Sprintf("%d", i))
	}
	args = append(args, "-c", "copy")
	switch strings.ToLower(filepath.Ext(output)) {
	case ".mp4", ".m4a", ".m4v", ".mov":
		// Index at the front: the file plays while still being copied/streamed.
		args = append(args, "-movflags", "+faststart")
	}
	return append(args, output)
}

// Remux joins inputs into output without re-encoding. On failure the error
// carries ffmpeg's own message and a partial output is removed.
func Remux(ctx context.Context, path string, inputs []string, output string) error {
	if len(inputs) == 0 {
		return errors.New("ffmpeg: no inputs")
	}
	var stderr bytes.Buffer
	cmd := command(ctx, path, RemuxArgs(inputs, output)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(output)
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = "..." + msg[len(msg)-500:]
		}
		if msg == "" {
			return fmt.Errorf("ffmpeg: %w", err)
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, msg)
	}
	return nil
}
