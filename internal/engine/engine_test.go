package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

func testData(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func serve(data []byte, ranges bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ranges {
			r.Header.Del("Range")
			w.Header().Set("Content-Length", itoa(len(data)))
			_, _ = w.Write(data)
			return
		}
		http.ServeContent(w, r, "file.bin", timeZero, bytes.NewReader(data))
	}))
}

func TestSegmentedDownload(t *testing.T) {
	data := testData(3<<20 + 123)
	srv := serve(data, true)
	defer srv.Close()

	dir := t.TempDir()
	var st State
	err := New().Run(context.Background(), &st, Options{
		Req: proto.Request{URL: srv.URL + "/file.bin"}, Dir: dir, Connections: 4,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "file.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	if len(st.Segments) < 2 {
		t.Fatalf("want parallel segments, got %d", len(st.Segments))
	}
	if err := verify(&st); err != nil {
		t.Fatal(err)
	}
}

func TestNoRangeFallback(t *testing.T) {
	data := testData(2 << 20)
	srv := serve(data, false)
	defer srv.Close()

	dir := t.TempDir()
	var st State
	if err := New().Run(context.Background(), &st, Options{
		Req: proto.Request{URL: srv.URL + "/x.bin"}, Dir: dir,
	}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "x.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	if len(st.Segments) != 1 {
		t.Fatalf("want 1 segment, got %d", len(st.Segments))
	}
}

// Cancel mid-way, then resume from the saved state and verify we only fetch the rest.
func TestResume(t *testing.T) {
	data := testData(4 << 20)
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(&countWriter{ResponseWriter: w, n: &served}, r, "r.bin", timeZero, bytes.NewReader(data))
	}))
	defer srv.Close()

	dir := t.TempDir()
	o := Options{Req: proto.Request{URL: srv.URL + "/r.bin"}, Dir: dir, Connections: 2}

	ctx, cancel := context.WithCancel(context.Background())
	var st State
	_ = New().Run(ctx, &st, o, func(s State) {
		if s.Downloaded() > 512<<10 {
			cancel()
		}
	})
	if st.Downloaded() == 0 || st.Downloaded() >= int64(len(data)) {
		t.Skipf("did not get a partial download (%d bytes)", st.Downloaded())
	}
	before := served.Load()
	have := st.Downloaded()

	if err := New().Run(context.Background(), &st, o, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "r.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch after resume")
	}
	if fetched := served.Load() - before; fetched > int64(len(data))-have+1024 {
		t.Fatalf("resume refetched too much: %d bytes (had %d)", fetched, have)
	}
}
