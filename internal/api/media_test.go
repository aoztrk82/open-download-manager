package api

import (
	"path/filepath"
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/ytdlp"
)

// Media requests are validated before they reach the queue (and yt-dlp).
func TestMediaValidation(t *testing.T) {
	h := newServer(t, "")
	const self = "127.0.0.1:6800"
	js := map[string]string{"Content-Type": "application/json"}
	cases := []struct {
		name, path, body string
		want             int
	}{
		{"unknown kind", "/v1/downloads", `{"request":{"url":"https://x/v","media":{"kind":"exec"}}}`, 400},
		{"ytdlp file url", "/v1/downloads", `{"request":{"url":"file:///C:/Windows/win.ini","media":{"kind":"ytdlp"}}}`, 400},
		{"ytdlp bad container", "/v1/downloads", `{"request":{"url":"https://youtu.be/x","media":{"kind":"ytdlp","container":"exe"}}}`, 400},
		{"ytdlp ok", "/v1/downloads", `{"request":{"url":"https://youtu.be/x","media":{"kind":"ytdlp","format":"bv*+ba/b","container":"mkv"}},"paused":true}`, 201},
		{"hls ok", "/v1/downloads", `{"request":{"url":"https://x/i.m3u8","media":{"kind":"hls","playlist":"#EXTM3U"}},"paused":true}`, 201},
		{"probe file url", "/v1/media/probe", `{"url":"file:///etc/passwd"}`, 400},
		{"probe option", "/v1/media/probe", `{"url":"--exec=calc"}`, 400},
	}
	for _, c := range cases {
		if got := do(h, "POST", c.path, self, js, c.body).Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	// Same guards as every other endpoint: no cross-site form posts.
	if got := do(h, "POST", "/v1/media/probe", self, map[string]string{"Content-Type": "text/plain"}, `{"url":"https://youtu.be/x"}`).Code; got != 415 {
		t.Errorf("text/plain probe: got %d, want 415", got)
	}
	if got := do(h, "POST", "/v1/media/update", self, map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"}, `{}`).Code; got != 403 {
		t.Errorf("foreign origin update: got %d, want 403", got)
	}
}

func TestMediaInfoWithoutYtdlp(t *testing.T) {
	t.Setenv(ytdlp.EnvPath, filepath.Join(t.TempDir(), "missing.exe"))
	h := newServer(t, "")
	r := do(h, "GET", "/v1/media", "127.0.0.1:6800", nil, "")
	if r.Code != 200 || r.Body.String() != "{\"ytdlp\":false}\n" {
		t.Fatalf("got %d %q", r.Code, r.Body.String())
	}
	if got := do(h, "POST", "/v1/media/probe", "127.0.0.1:6800", map[string]string{"Content-Type": "application/json"}, `{"url":"https://youtu.be/x"}`).Code; got != 501 {
		t.Fatalf("probe without yt-dlp: got %d, want 501", got)
	}
}
