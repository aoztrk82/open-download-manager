package engine

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// slowWriter delays every write so a small file stays in flight long enough
// for progress snapshots to see the connections working in parallel.
type slowWriter struct{ http.ResponseWriter }

func (s slowWriter) Write(b []byte) (int, error) {
	time.Sleep(50 * time.Millisecond) // 16 writes per segment: ~0.8 s, past the 0.5 s progress tick
	return s.ResponseWriter.Write(b)
}

func TestConnectionStates(t *testing.T) {
	data := testData(2 << 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(slowWriter{w}, r, "file.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	var mu sync.Mutex
	maxActive := 0
	var st State
	err := New().Run(context.Background(), &st, Options{
		Req: proto.Request{URL: srv.URL + "/file.bin"}, Dir: t.TempDir(), Connections: 4,
	}, func(s State) {
		if len(s.Conns) != len(s.Segments) {
			t.Errorf("conns %d != segments %d", len(s.Conns), len(s.Segments))
		}
		n := 0
		for _, c := range s.Conns {
			if c.State == ConnActive {
				n++
			}
		}
		mu.Lock()
		maxActive = max(maxActive, n)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if maxActive < 2 {
		t.Fatalf("never saw parallel connections (max active %d)", maxActive)
	}
	for i, c := range st.Conns {
		if c.State != ConnDone {
			t.Fatalf("conn %d ended %q, want done", i, c.State)
		}
	}
}
