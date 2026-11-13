package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/ffmpeg"
	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// cdn serves n random "segments" named like tranimeizle's disguised pieces
// (file000.jpeg, served as image/jpeg) and records the Cookie header it saw.
type cdn struct {
	*httptest.Server
	pieces  [][]byte
	hits    atomic.Int64
	mu      sync.Mutex
	cookies []string
	delay   atomic.Int64 // nanoseconds; changed while old requests may still run
}

func newCDN(t *testing.T, n int) *cdn {
	c := &cdn{}
	for i := 0; i < n; i++ {
		b := make([]byte, 50_000+i*1000)
		_, _ = rand.Read(b)
		c.pieces = append(c.pieces, b)
	}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var i int
		if _, err := fmt.Sscanf(r.URL.Path, "/v1/file%03d.jpeg", &i); err != nil || i >= len(c.pieces) {
			http.NotFound(w, r)
			return
		}
		c.hits.Add(1)
		c.mu.Lock()
		c.cookies = append(c.cookies, r.Header.Get("Cookie"))
		c.mu.Unlock()
		if d := c.delay.Load(); d > 0 {
			time.Sleep(time.Duration(d))
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(c.pieces[i])
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *cdn) playlist() string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	for i := range c.pieces {
		fmt.Fprintf(&b, "#EXTINF:2.002000,\n%s/v1/file%03d.jpeg\n", c.URL, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func noFFmpeg(t *testing.T) {
	t.Setenv(ffmpeg.EnvPath, filepath.Join(t.TempDir(), "missing-ffmpeg.exe"))
}

// The browser hands over the playlist text (its URL is a session-bound token
// ODM could not fetch); pieces come from a CDN on another host.
func TestHLSPlainTSWithoutFFmpeg(t *testing.T) {
	noFFmpeg(t)
	c := newCDN(t, 12)
	dir := t.TempDir()
	req := proto.Request{
		URL:     "https://page.invalid/w/TOKEN/v1", // never fetched: the playlist is inline
		Cookies: "session=secret",
		Media:   &proto.Media{Kind: proto.MediaHLS, Playlist: c.playlist()},
	}
	var st State
	var progress []int64
	err := New().Run(context.Background(), &st, Options{Req: req, Dir: dir, Filename: "Bölüm 1 (720p).mp4", Connections: 4},
		func(s State) { progress = append(progress, s.Downloaded()) })
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Join(c.pieces, nil)
	if filepath.Base(st.Path) != "Bölüm 1 (720p).ts" {
		t.Fatalf("path %q: without ffmpeg the pieces are joined as MPEG-TS", st.Path)
	}
	got, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("joined file differs (%d vs %d bytes)", len(got), len(want))
	}
	if st.Size != int64(len(want)) || st.Downloaded() != st.Size {
		t.Fatalf("size %d downloaded %d, want %d", st.Size, st.Downloaded(), len(want))
	}
	if _, err := os.Stat(st.Path[:len(st.Path)-3] + ".ts.parts"); !os.IsNotExist(err) {
		t.Fatal("the .parts folder must be gone after joining")
	}
	for _, ck := range c.cookies {
		if ck != "" {
			t.Fatalf("page cookie leaked to the CDN: %q", ck)
		}
	}
	if len(progress) == 0 || progress[len(progress)-1] != st.Size {
		t.Fatalf("progress %v", progress)
	}
}

func TestHLSPauseResumeKeepsPieces(t *testing.T) {
	noFFmpeg(t)
	c := newCDN(t, 30)
	c.delay.Store(int64(20 * time.Millisecond))
	dir := t.TempDir()
	o := Options{Dir: dir, Filename: "v.mp4", Connections: 2, Req: proto.Request{
		URL: c.URL + "/index.m3u8", Media: &proto.Media{Kind: proto.MediaHLS, Playlist: c.playlist()},
	}}
	var st State
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for c.hits.Load() < 8 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	if err := New().Run(ctx, &st, o, nil); err == nil {
		t.Fatal("expected cancellation")
	}
	done := 0
	for _, d := range st.HLS.Done {
		if d {
			done++
		}
	}
	if done == 0 || done == len(c.pieces) {
		t.Fatalf("pause should leave some pieces done, got %d/%d", done, len(c.pieces))
	}
	if !strings.HasSuffix(st.Path, ".parts") {
		t.Fatalf("paused path %q should be the pieces folder", st.Path)
	}

	before := c.hits.Load()
	c.delay.Store(0)
	if err := New().Run(context.Background(), &st, o, nil); err != nil {
		t.Fatal(err)
	}
	if again := c.hits.Load() - before; again > int64(len(c.pieces)-done)+2 { // +2: pieces cut off mid-flight
		t.Fatalf("resume fetched %d pieces, %d were already done", again, done)
	}
	got, _ := os.ReadFile(st.Path)
	if !bytes.Equal(got, bytes.Join(c.pieces, nil)) {
		t.Fatal("resumed file differs")
	}
}

func TestHLSMasterPicksBestVariant(t *testing.T) {
	noFFmpeg(t)
	c := newCDN(t, 3)
	low := newCDN(t, 3)
	mux := http.NewServeMux()
	mux.HandleFunc("/hi.m3u8", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.playlist())) })
	mux.HandleFunc("/lo.m3u8", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(low.playlist())) })
	site := httptest.NewServer(mux)
	defer site.Close()
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=854x480\nlo.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=4000000,RESOLUTION=1920x1080\nhi.m3u8\n"
	var st State
	err := New().Run(context.Background(), &st, Options{Dir: t.TempDir(), Connections: 2, Req: proto.Request{
		URL: site.URL + "/master.m3u8", Media: &proto.Media{Kind: proto.MediaHLS, Playlist: master},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(st.Path)
	if !bytes.Equal(got, bytes.Join(c.pieces, nil)) || low.hits.Load() != 0 {
		t.Fatal("expected the 1080p variant's pieces only")
	}
}

func TestHLSMissingPieceFails(t *testing.T) {
	noFFmpeg(t)
	c := newCDN(t, 3)
	pl := strings.Replace(c.playlist(), "file002.jpeg", "file099.jpeg", 1)
	var st State
	err := New().Run(context.Background(), &st, Options{Dir: t.TempDir(), Connections: 2, Retries: 2, Req: proto.Request{
		URL: c.URL + "/i.m3u8", Media: &proto.Media{Kind: proto.MediaHLS, Playlist: pl},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want a 404 error, got %v", err)
	}
}

// With a real ffmpeg: AES-128 encrypted MPEG-TS over HTTP -> one MP4.
func TestHLSEncryptedWithFFmpeg(t *testing.T) {
	if _, err := ffmpeg.Find(); err != nil {
		t.Skip("no ffmpeg:", err)
	}
	fixture := filepath.Join("..", "ffmpeg", "testdata", "hls-aes")
	srv := httptest.NewServer(http.FileServer(http.Dir(fixture)))
	defer srv.Close()
	pl, err := os.ReadFile(filepath.Join(fixture, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	var st State
	err = New().Run(context.Background(), &st, Options{Dir: t.TempDir(), Filename: "Ep. 5 (720p)", Connections: 2, Req: proto.Request{
		URL: srv.URL + "/index.m3u8", Media: &proto.Media{Kind: proto.MediaHLS, Playlist: string(pl)},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(st.Path) != "Ep. 5 (720p).mp4" {
		t.Fatalf("path %q", st.Path)
	}
	b, _ := os.ReadFile(st.Path)
	if len(b) < 1000 || string(b[4:8]) != "ftyp" {
		t.Fatalf("not an MP4: %d bytes", len(b))
	}
}

func TestStreamName(t *testing.T) {
	for _, c := range []struct {
		in   string
		ff   bool
		want string
	}{
		{"Bölüm 1 (720p).mp4", true, "Bölüm 1 (720p).mp4"},
		{"Bölüm 1 (720p).mp4", false, "Bölüm 1 (720p).ts"},
		{"Ep. 5 (720p)", true, "Ep. 5 (720p).mp4"},
		{"movie.mkv", true, "movie.mkv"},
		{"v1", true, "v1.mp4"},
	} {
		if got := streamName(c.in, c.ff); got != c.want {
			t.Errorf("streamName(%q, %v) = %q, want %q", c.in, c.ff, got, c.want)
		}
	}
}
