package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRemuxArgs(t *testing.T) {
	got := RemuxArgs([]string{`C:\dl\x\index.m3u8`}, `C:\dl\x.mp4`)
	want := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-allowed_extensions", "ALL", "-protocol_whitelist", "file,crypto", "-i", `C:\dl\x\index.m3u8`,
		"-map", "0", "-c", "copy", "-movflags", "+faststart", `C:\dl\x.mp4`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hls:\n got %q\nwant %q", got, want)
	}

	// DASH-style: separate video and audio files, every stream of both kept.
	got = RemuxArgs([]string{"v.mp4", "a.webm"}, "out.mkv")
	want = []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-i", "v.mp4", "-i", "a.webm", "-map", "0", "-map", "1", "-c", "copy", "out.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dash:\n got %q\nwant %q", got, want)
	}
}

func TestFindEnv(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "my-ffmpeg.exe")
	if err := os.WriteFile(fake, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvPath, fake)
	if p, err := Find(); err != nil || p != fake {
		t.Fatalf("Find() = %q, %v; want %q", p, err, fake)
	}
	t.Setenv(EnvPath, filepath.Join(dir, "missing.exe"))
	if _, err := Find(); err == nil {
		t.Fatal("a wrong ODM_FFMPEG must be an error, not a silent fallback")
	}
}

// Runs the real ffmpeg (ODM_FFMPEG, next to the test binary, or PATH) on a
// 2 s AES-128 HLS fixture: decryption + MPEG-TS -> MP4 in one call.
func TestRemuxEncryptedHLS(t *testing.T) {
	path, err := Find()
	if err != nil {
		t.Skip("no ffmpeg:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	v, err := Version(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(path, v)

	in, err := filepath.Abs("testdata/hls-aes/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	if err := Remux(ctx, path, []string{in}, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// An MP4 starts with an "ftyp" box; garbage from a wrong key would not mux at all.
	if len(b) < 1000 || string(b[4:8]) != "ftyp" {
		t.Fatalf("output is not an MP4 (%d bytes, % x)", len(b), b[:min(len(b), 16)])
	}

	// A missing input fails with ffmpeg's message and leaves no partial file.
	bad := filepath.Join(t.TempDir(), "bad.mp4")
	if err := Remux(ctx, path, []string{filepath.Join(t.TempDir(), "nope.ts")}, bad); err == nil {
		t.Fatal("expected an error for a missing input")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("partial output was left behind")
	}
}
